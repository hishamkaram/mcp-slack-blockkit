package converter

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

// walker is the per-call state shared by every block handler. We construct
// one each time Convert is called so concurrent calls cannot collide on the
// block-id counter.
type walker struct {
	opts   Options
	source []byte // original markdown bytes; required by many ast.Node methods
	blocks []slack.Block
	idSeq  int
}

// nextBlockID returns a fresh block_id with the configured prefix.
// Empty BlockIDPrefix produces empty string (slack-go omits the field).
func (w *walker) nextBlockID() string {
	if w.opts.BlockIDPrefix == "" {
		return ""
	}
	w.idSeq++
	return w.opts.BlockIDPrefix + "-" + strconv.Itoa(w.idSeq)
}

// walkDocument iterates over the document's direct children and dispatches
// each block-level node to its handler. We intentionally do NOT use
// ast.Walk here because block emission needs precise control over which
// subtrees we descend into (e.g. for headings we extract text directly
// rather than letting an inline visitor produce text elements).
func (w *walker) walkDocument(doc ast.Node) error {
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		if err := w.dispatchBlock(child); err != nil {
			return err
		}
	}
	return nil
}

// dispatchBlock routes a single block-level node to the right handler.
// Unknown kinds fall through to a best-effort "extract text and wrap in
// rich_text" path so the converter never silently drops content.
func (w *walker) dispatchBlock(node ast.Node) error {
	switch n := node.(type) {
	case *ast.Heading:
		return w.handleHeading(n)
	case *ast.Paragraph:
		return w.handleParagraph(n)
	case *ast.ThematicBreak:
		return w.handleThematicBreak(n)
	case *ast.Blockquote:
		return w.handleBlockquote(n)
	case *ast.List:
		return w.handleList(n)
	case *ast.FencedCodeBlock:
		return w.handleFencedCode(n)
	case *ast.CodeBlock:
		return w.handleIndentedCode(n)
	case *extast.Table:
		return w.handleTable(n)
	default:
		// HTML blocks and unknown extension nodes: fall through to plain-text
		// emission so test fixtures don't lose content.
		return w.handleFallback(node)
	}
}

// handleHeading emits a header block when the heading is short and contains
// no inline-formatting children (links/images/code). Otherwise it falls back
// to a bold section.mrkdwn block. h2-h6 always go to the bold section path
// because mrkdwn only has one heading style.
func (w *walker) handleHeading(h *ast.Heading) error {
	text := extractPlainText(h, w.source)
	hasUnsupported := containsUnsupportedHeadingChild(h)

	useHeaderBlock := h.Level == 1 &&
		!hasUnsupported &&
		len(text) > 0 &&
		len(text) <= MaxHeaderTextChars

	if useHeaderBlock {
		header := slack.NewHeaderBlock(
			slack.NewTextBlockObject(slack.PlainTextType, text, false, false),
		)
		header.BlockID = w.nextBlockID()
		w.blocks = append(w.blocks, header)
		return nil
	}

	// Fallback: bold section.mrkdwn. Empty text degenerates to nothing
	// useful — emit a divider as a structural placeholder so the heading
	// position is preserved in output.
	if text == "" {
		w.blocks = append(w.blocks, slack.NewDividerBlock())
		return nil
	}

	mrkdwnText := "*" + escapeMrkdwnEmphasis(text) + "*"
	if len(mrkdwnText) > MaxSectionTextChars {
		mrkdwnText = mrkdwnText[:MaxSectionTextChars-1] + "*"
	}
	section := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, mrkdwnText, false, false),
		nil, nil,
	)
	section.BlockID = w.nextBlockID()
	w.blocks = append(w.blocks, section)
	return nil
}

// handleParagraph emits a rich_text block. If the paragraph contains exactly
// one child that is an Image, we emit an image block instead — this is the
// "standalone image as paragraph" case from research.md §4. Mixed text+image
// paragraphs treat the image inline (URL becomes plain text for now;
// proper inline link handling lands in step 5).
func (w *walker) handleParagraph(p *ast.Paragraph) error {
	if img, ok := standaloneImage(p, w.source); ok {
		return w.emitImageBlock(img)
	}

	elements := renderInlinesWithOpts(p, w.source, w.opts)
	if len(elements) == 0 {
		return nil
	}

	rt := slack.NewRichTextBlock(
		w.nextBlockID(),
		slack.NewRichTextSection(elements...),
	)
	w.blocks = append(w.blocks, rt)
	return nil
}

// handleThematicBreak emits a divider block. Trivial — no payload to extract.
func (w *walker) handleThematicBreak(_ *ast.ThematicBreak) error {
	div := slack.NewDividerBlock()
	div.BlockID = w.nextBlockID()
	w.blocks = append(w.blocks, div)
	return nil
}

// handleBlockquote emits one or more top-level blocks representing the
// markdown blockquote. The simple case (only paragraphs and nested quotes
// containing only paragraphs) produces a single rich_text block with one
// rich_text_quote element — same as before. The complex case (a child or
// transitive child is a code block, list, or table) is split into adjacent
// top-level blocks: rich_text(quote_prefix), inner_block, rich_text(quote_suffix),
// and so on. Slack's rich_text_quote schema only accepts inline elements
// as direct children, so this is the only way to preserve the inner block's
// formatting.
//
// Nested blockquotes are flattened into the containing quote element when
// they are inline-only (Slack has no nested rich_text_quote). When a nested
// blockquote contains a non-representable child, it is dispatched as its
// own handleBlockquote call (which itself may split), with the parent's
// pending buffer flushed first to preserve document order.
func (w *walker) handleBlockquote(bq *ast.Blockquote) error {
	var pending []slack.RichTextSectionElement

	flush := func() {
		if len(pending) == 0 {
			return
		}
		quote := &slack.RichTextQuote{Type: slack.RTEQuote, Elements: pending}
		rt := slack.NewRichTextBlock(w.nextBlockID(), quote)
		w.blocks = append(w.blocks, rt)
		pending = nil
	}

	appendParagraph := func(p *ast.Paragraph) {
		els := renderInlinesWithOpts(p, w.source, w.opts)
		if len(pending) > 0 && len(els) > 0 {
			pending = append(pending, slack.NewRichTextSectionTextElement(" ", nil))
		}
		pending = append(pending, els...)
	}

	for c := bq.FirstChild(); c != nil; c = c.NextSibling() {
		switch n := c.(type) {
		case *ast.Paragraph:
			appendParagraph(n)
		case *ast.Blockquote:
			// If the nested quote subtree contains any non-representable
			// child (code/list/table), it can't be flattened safely — recurse
			// so the nested handleBlockquote call performs its own splits.
			// Flush the parent's pending first to preserve document order.
			if len(containsBlockInBlock(n)) > 0 {
				flush()
				if err := w.handleBlockquote(n); err != nil {
					return err
				}
				continue
			}
			collectInlineOnlyQuote(n, w.source, w.opts, &pending)
		case *ast.FencedCodeBlock, *ast.CodeBlock, *ast.List, *extast.Table:
			// Non-representable in rich_text_quote.elements — split.
			flush()
			if err := w.dispatchBlock(n); err != nil {
				return err
			}
		default:
			// Unknown block kind in a quote: keep its content as plain text
			// in the current buffer so nothing is silently dropped.
			text := extractPlainText(c, w.source)
			if text == "" {
				continue
			}
			if len(pending) > 0 {
				pending = append(pending, slack.NewRichTextSectionTextElement(" ", nil))
			}
			pending = append(pending, slack.NewRichTextSectionTextElement(text, nil))
		}
	}
	flush()
	return nil
}

// collectInlineOnlyQuote gathers inline content from an inline-only
// blockquote subtree (a quote whose subtree contains no FencedCodeBlock /
// CodeBlock / List / Table) and appends to *out. Used by handleBlockquote
// to flatten a nested blockquote into the parent's buffer when it's safe.
//
// The caller MUST verify the subtree is inline-only via containsBlockInBlock
// before calling — this helper does not check, and producing a flat result
// from a quote that contains a code block would silently drop the code's
// formatting (the old behavior we are explicitly fixing).
func collectInlineOnlyQuote(bq *ast.Blockquote, source []byte, opts Options, out *[]slack.RichTextSectionElement) {
	for c := bq.FirstChild(); c != nil; c = c.NextSibling() {
		switch n := c.(type) {
		case *ast.Paragraph:
			els := renderInlinesWithOpts(n, source, opts)
			if len(*out) > 0 && len(els) > 0 {
				*out = append(*out, slack.NewRichTextSectionTextElement(" ", nil))
			}
			*out = append(*out, els...)
		case *ast.Blockquote:
			// Recurse — nested-nested blockquotes still flatten into the
			// outermost quote element.
			collectInlineOnlyQuote(n, source, opts, out)
		}
	}
}

// handleFallback emits a rich_text block with the node's plain-text content,
// guaranteeing no content is silently dropped while step-3 only covers the
// listed block kinds. Lists/code/tables get proper handlers in steps 4-7.
func (w *walker) handleFallback(node ast.Node) error {
	text := extractPlainText(node, w.source)
	if text == "" {
		return nil
	}
	rt := slack.NewRichTextBlock(
		w.nextBlockID(),
		slack.NewRichTextSection(
			slack.NewRichTextSectionTextElement(text, nil),
		),
	)
	w.blocks = append(w.blocks, rt)
	return nil
}

// emitImageBlock emits an image block from a goldmark Image node. alt_text
// is required by Slack but may be empty (we lint-warn in step 18 instead of
// erroring here). Title is optional.
func (w *walker) emitImageBlock(img *ast.Image) error {
	url := string(img.Destination)
	if url == "" {
		return nil // nothing useful to emit
	}
	alt := extractPlainText(img, w.source)
	title := string(img.Title)

	if len(url) > MaxSectionTextChars {
		// Slack's image_url limit is 3000 chars per §3 — same constant.
		return fmt.Errorf("image_url length %d exceeds Slack limit %d",
			len(url), MaxSectionTextChars)
	}

	var titleObj *slack.TextBlockObject
	if title != "" {
		titleObj = slack.NewTextBlockObject(slack.PlainTextType, title, false, false)
	}

	imgBlock := slack.NewImageBlock(url, alt, w.nextBlockID(), titleObj)
	w.blocks = append(w.blocks, imgBlock)
	return nil
}

// --- helpers -----------------------------------------------------------------

// extractPlainText returns the concatenated text content of node's subtree
// in source order. Inline formatting nodes (Emphasis, Link, etc.) become
// their literal text; this is the step-3 simplification before proper inline
// handlers land in step 5.
func extractPlainText(node ast.Node, source []byte) string {
	var buf bytes.Buffer
	collectPlainText(&buf, node, source)
	return buf.String()
}

func collectPlainText(buf *bytes.Buffer, node ast.Node, source []byte) {
	switch n := node.(type) {
	case *ast.Text:
		// ast.Text exposes its segment via Segment, not Lines.
		buf.Write(n.Segment.Value(source))
		// Soft-line-break preservation: goldmark sets HardLineBreak when
		// the line is terminated by two trailing spaces. We render both
		// soft and hard breaks as a single space here; step 5's proper
		// inline handler will distinguish them properly.
		if n.SoftLineBreak() || n.HardLineBreak() {
			buf.WriteByte(' ')
		}
		return
	case *ast.String:
		buf.Write(n.Value)
		return
	case *ast.AutoLink:
		buf.Write(n.URL(source))
		return
	case *ast.Link:
		// Recurse into children to pick up the link text; URL itself is
		// dropped at this stage (step 5 emits a proper link element).
	case *ast.Image:
		// At step 3 we render an image either as a standalone image block
		// (handled above) or as its alt text in a paragraph context.
	case *ast.CodeSpan:
		// Step 6 emits these as styled text elements; for now show literal text.
	case *extast.TaskCheckBox:
		// The list-item handler emits "[x] " / "[ ] " explicitly; skip here so
		// fallback paths don't double-render the checkbox.
		return
	}

	// Recurse into children.
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		collectPlainText(buf, child, source)
	}
}

// containsUnsupportedHeadingChild reports whether the heading's subtree
// contains any inline node we cannot losslessly render in a Slack header
// block (which only accepts plain_text). When true, we fall back to a
// section.mrkdwn block instead.
func containsUnsupportedHeadingChild(h *ast.Heading) bool {
	found := false
	walk(h, func(n ast.Node) (stop bool) {
		switch n.(type) {
		case *ast.Link, *ast.AutoLink, *ast.Image, *ast.CodeSpan:
			found = true
			return true
		}
		return false
	})
	return found
}

// standaloneImage reports whether the paragraph is a "single-image" paragraph
// (one Image child, no other meaningful content). Only such paragraphs
// become image blocks; mixed text+image stays as a paragraph. The source
// byte slice is needed to inspect text-node contents (whitespace-only text
// children do not disqualify a paragraph from being treated as standalone).
func standaloneImage(p *ast.Paragraph, source []byte) (*ast.Image, bool) {
	var img *ast.Image
	count := 0
	for c := p.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Image:
			img = t
			count++
		case *ast.Text:
			if len(bytes.TrimSpace(t.Segment.Value(source))) > 0 {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	return img, count == 1 && img != nil
}

// walk is a small DFS helper so we don't have to wire up goldmark's full
// ast.Walk machinery for one-shot subtree queries.
func walk(node ast.Node, visit func(ast.Node) (stop bool)) {
	if visit(node) {
		return
	}
	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		walk(c, visit)
	}
}

// escapeMrkdwnEmphasis defangs the four mrkdwn emphasis markers (`*`, `_`,
// `~`, “ ` “) so user text inside our bold-wrapped headings doesn't
// accidentally start a new style run. The proper full mrkdwn escape (which
// also handles `<`, `>`, `&` for mention safety) is layered on top in step
// 11; this function is intentionally narrower so heading fallback works
// today without depending on the sanitization layer.
func escapeMrkdwnEmphasis(s string) string {
	if s == "" {
		return ""
	}
	var b bytes.Buffer
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '*', '_', '~', '`':
			// Escape with a leading backslash; Slack's mrkdwn parser honours
			// the backslash escape per their formatting docs.
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
