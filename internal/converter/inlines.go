package converter

import (
	"bytes"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

// renderInlines walks node's inline-children subtree and produces a flat
// slice of rich_text_section elements with proper Slack styling.
//
// Style stack with OR-merge: each Emphasis/Strikethrough/CodeSpan we enter
// pushes a frame; we pop on exit. The "current" style for any text element
// emitted at depth N is the bitwise-OR of every frame in the stack — so
// `***bold-italic***` (markdown's `**` then `*` nesting) resolves to
// `{bold: true, italic: true}` correctly, regardless of which order the
// markdown chose to nest them.
//
// renderInlinesWithOpts is the only public-ish entry. There's no
// no-options variant because every caller in this package has access
// to a walker and its Options; threading them through avoids reaching
// into a global default that could drift from the renderer's actual
// configuration.
//
// The mention/emoji/sanitize pipeline fires in this exact order:
//
//  1. visit() builds raw RichTextSectionElement values from the AST.
//  2. applyMentionMap replaces `@handle` substrings with typed user/
//     channel/usergroup elements when handle is in Options.MentionMap.
//     This MUST run before sanitization, otherwise its output (which it
//     marks as already-safe) would be re-escaped.
//  3. expandTrustedMentions replaces already-typed Slack tokens
//     (`<@U…>`, `<#C…>`, `<!subteam^S…>`, `<!date^…|fallback>`) with
//     typed elements when Options.PreserveMentionTokens is true. Like
//     applyMentionMap, this runs before sanitization so its output
//     survives the entity-escape pass; the catastrophic broadcast forms
//     (`<!channel>` etc.) are deliberately not in the trusted set, so
//     they still escape.
//  4. resolveEmoji merges fragmented text and extracts `:name:` shortcodes.
//  5. sanitizeBroadcasts entity-escapes any remaining &/</> in text
//     elements (skipped when Options.AllowBroadcasts is true).
//
//nolint:revive // exported-only-for-package-internal usage; tests may import in a future split.
func renderInlinesWithOpts(node ast.Node, source []byte, opts Options) []slack.RichTextSectionElement {
	rc := &inlineCtx{source: source}
	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		rc.visit(c)
	}
	rc.flushPending()
	out := rc.out
	out = applyMentionMap(out, opts.MentionMap)
	if opts.PreserveMentionTokens {
		out = expandTrustedMentions(out)
	}
	out = resolveEmoji(out)
	out = sanitizeBroadcasts(out, opts.AllowBroadcasts)
	return out
}

type styleFrame struct {
	bold, italic, strike, code bool
}

type inlineCtx struct {
	source  []byte
	stack   []styleFrame
	out     []slack.RichTextSectionElement
	pending bytes.Buffer // accumulating text under the current style
}

// pushStyle activates a style frame for the duration of one inline subtree
// and returns the depth so the caller can pop with confidence.
func (c *inlineCtx) pushStyle(f styleFrame) {
	c.flushPending() // any text under the previous style must be emitted first
	c.stack = append(c.stack, f)
}

func (c *inlineCtx) popStyle() {
	c.flushPending()
	if len(c.stack) == 0 {
		return
	}
	c.stack = c.stack[:len(c.stack)-1]
}

// currentStyle returns the OR-merge of every active style frame, or nil if
// no styling is active. nil keeps the JSON output free of empty {} objects.
func (c *inlineCtx) currentStyle() *slack.RichTextSectionTextStyle {
	var s slack.RichTextSectionTextStyle
	for _, f := range c.stack {
		if f.bold {
			s.Bold = true
		}
		if f.italic {
			s.Italic = true
		}
		if f.strike {
			s.Strike = true
		}
		if f.code {
			s.Code = true
		}
	}
	if (s == slack.RichTextSectionTextStyle{}) {
		return nil
	}
	return &s
}

// flushPending emits any buffered text as a single rich_text_section text
// element under the current style, then clears the buffer. We buffer to
// avoid splitting consecutive Text nodes (e.g. when goldmark fragments
// text around `_` candidates) into many tiny elements.
func (c *inlineCtx) flushPending() {
	if c.pending.Len() == 0 {
		return
	}
	c.out = append(
		c.out,
		slack.NewRichTextSectionTextElement(c.pending.String(), c.currentStyle()),
	)
	c.pending.Reset()
}

// emitText appends text to the pending buffer under the current style.
// Used for Text/String nodes; styled text from CodeSpan/Emphasis follows
// the same path because the style is set via the stack, not per-call.
func (c *inlineCtx) emitText(b []byte) {
	c.pending.Write(b)
}

func (c *inlineCtx) visit(node ast.Node) {
	switch n := node.(type) {
	case *ast.Text:
		c.emitText(n.Segment.Value(c.source))
		if n.SoftLineBreak() || n.HardLineBreak() {
			c.emitText([]byte(" "))
		}

	case *ast.String:
		c.emitText(n.Value)

	case *ast.Emphasis:
		// Level 1 = italic (`*x*` or `_x_`), Level 2 = bold (`**x**` or `__x__`).
		// Level 3+ is rare (`***bold-italic***` parses as nested level-2+level-1).
		f := styleFrame{}
		switch n.Level {
		case 1:
			f.italic = true
		case 2:
			f.bold = true
		default:
			// Treat as bold + italic for any higher level.
			f.bold = true
			f.italic = true
		}
		c.pushStyle(f)
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			c.visit(child)
		}
		c.popStyle()

	case *extast.Strikethrough:
		c.pushStyle(styleFrame{strike: true})
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			c.visit(child)
		}
		c.popStyle()

	case *ast.CodeSpan:
		// CodeSpan content is the literal byte text of its Text children.
		// We push the code style and walk; goldmark stores the inner code
		// as Text nodes, so the existing Text path handles it.
		c.pushStyle(styleFrame{code: true})
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			c.visit(child)
		}
		c.popStyle()

	case *ast.Link:
		// Slack's link element has a single text field plus optional style.
		// Use plain-text extraction for the display text — emphasis inside
		// link text is uncommon and Slack's element schema can't represent
		// it directly. Apply the current outer style to the link element so
		// `*[text](url)*` produces a bold link.
		c.flushPending()
		text := extractPlainText(n, c.source)
		link := slack.NewRichTextSectionLinkElement(string(n.Destination), text, c.currentStyle())
		c.out = append(c.out, link)

	case *ast.AutoLink:
		c.flushPending()
		url := string(n.URL(c.source))
		// Email autolinks (RFC 5322) get a `mailto:` prefix so Slack
		// renders them as openable links rather than plain text.
		if n.AutoLinkType == ast.AutoLinkEmail {
			url = "mailto:" + url
		}
		link := slack.NewRichTextSectionLinkElement(url, "", c.currentStyle())
		c.out = append(c.out, link)

	case *ast.Image:
		// In an inline context (mixed text+image paragraph), the image's
		// alt-text is the natural fallback — Slack has no inline-image
		// element. Standalone-image paragraphs are caught upstream and
		// emit a separate ImageBlock instead, so we never reach this for
		// "image alone in a paragraph" cases.
		c.emitText([]byte(extractPlainText(n, c.source)))

	case *extast.TaskCheckBox:
		// The list-item handler emits "[x] " / "[ ] " explicitly; do nothing
		// here so we don't double-render the checkbox in a list item that
		// also routes through renderInlines.
		return

	default:
		// Unknown inline kind: fall back to its plain-text content so we
		// don't silently drop characters. Mentions handling lives in
		// step 11 and will intercept text content here.
		c.emitText([]byte(extractPlainText(node, c.source)))
	}
}

// inlineElementsToText is a convenience used by callers that need the
// concatenated plain-text form of a rendered inline run (e.g. blockquotes,
// when we eventually want to emit the styled version into a rich_text_quote).
// Exported for test reuse.
func inlineElementsToText(elements []slack.RichTextSectionElement) string {
	var sb bytes.Buffer
	for _, el := range elements {
		switch e := el.(type) {
		case *slack.RichTextSectionTextElement:
			sb.WriteString(e.Text)
		case *slack.RichTextSectionLinkElement:
			if e.Text != "" {
				sb.WriteString(e.Text)
			} else {
				sb.WriteString(e.URL)
			}
		}
	}
	return sb.String()
}
