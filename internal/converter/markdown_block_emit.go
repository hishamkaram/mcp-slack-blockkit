package converter

import (
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

// emitMarkdownBlockText walks node (typically the document root) and
// returns a CommonMark text string suitable for Slack's `markdown` block
// (Feb 2025). The contract:
//
//   - Text content is HTML-entity-escaped for broadcast safety, exactly
//     like the rich_text path's sanitizer pipeline. `<!channel>` in prose
//     becomes `&lt;!channel&gt;` and cannot trigger Slack's mention parser.
//   - URLs inside `[text](url)`, `[url](url)`, and `![alt](url)` pass
//     through verbatim — URLs are syntactically bounded by `()` and never
//     need entity escaping.
//   - Autolink AST nodes (both explicit `<url>` and Linkify-detected bare
//     URLs) are emitted as `[url](url)` because Slack's markdown-block
//     documentation only lists `[text](url)` as supported link syntax.
//     Email autolinks become `[email](mailto:email)`.
//   - Code spans / fenced code blocks emit their content RAW. CommonMark
//     treats code content as literal; Slack's markdown block follows
//     CommonMark, so broadcast tokens inside code are not interpreted.
//   - opts.AllowBroadcasts bypasses every text-content escape.
//   - opts.PreserveMentionTokens uses escapePreservingTokens so the four
//     trusted Slack token shapes pass through while catastrophic
//     broadcasts still escape.
//
// This replaces the old blanket entityEscape over the raw input, which
// destroyed CommonMark autolink syntax. See plan F1–F4 in
// /home/hesham/.claude/plans/mighty-scribbling-planet.md for evidence.
func emitMarkdownBlockText(root ast.Node, source []byte, opts Options) string {
	e := &mdEmitter{source: source, opts: opts}
	e.walkBlock(root)
	return strings.TrimRight(e.b.String(), "\n")
}

type mdEmitter struct {
	b      strings.Builder
	source []byte
	opts   Options
}

// writeBlockBreak terminates the current block with a blank line. Calls
// collapse: consecutive calls do not stack extra newlines, so handlers
// can be conservative without producing huge whitespace gaps.
func (e *mdEmitter) writeBlockBreak() {
	cur := e.b.String()
	switch {
	case strings.HasSuffix(cur, "\n\n"):
		return
	case strings.HasSuffix(cur, "\n"):
		e.b.WriteByte('\n')
	case cur == "":
		// Nothing to break.
	default:
		e.b.WriteString("\n\n")
	}
}

func (e *mdEmitter) walkBlock(n ast.Node) {
	switch t := n.(type) {
	case *ast.Document:
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			e.walkBlock(c)
		}
	case *ast.Paragraph:
		e.emitInlineChildren(t)
		e.writeBlockBreak()
	case *ast.TextBlock:
		e.emitInlineChildren(t)
		e.writeBlockBreak()
	case *ast.Heading:
		level := t.Level
		if level < 1 {
			level = 1
		}
		if level > 6 {
			level = 6
		}
		e.b.WriteString(strings.Repeat("#", level))
		e.b.WriteByte(' ')
		e.emitInlineChildren(t)
		e.writeBlockBreak()
	case *ast.ThematicBreak:
		e.b.WriteString("---")
		e.writeBlockBreak()
	case *ast.Blockquote:
		e.emitBlockquote(t)
	case *ast.List:
		e.emitList(t, "")
		e.writeBlockBreak()
	case *ast.FencedCodeBlock:
		e.emitFencedCode(t)
		e.writeBlockBreak()
	case *ast.CodeBlock:
		e.emitIndentedCode(t)
		e.writeBlockBreak()
	case *extast.Table:
		e.emitTable(t)
		e.writeBlockBreak()
	case *ast.HTMLBlock:
		// Never trust raw HTML from LLM input — emit as escaped text.
		for i := 0; i < t.Lines().Len(); i++ {
			seg := t.Lines().At(i)
			e.writeEscapedText(string(seg.Value(e.source)))
		}
		e.writeBlockBreak()
	default:
		// Defensive fallback for any block-level node we didn't enumerate
		// (forward compatibility with future goldmark extensions).
		text := extractPlainText(n, e.source)
		if text != "" {
			e.writeEscapedText(text)
			e.writeBlockBreak()
		}
	}
}

// emitInlineChildren walks parent's inline children. Consecutive Text /
// String nodes are coalesced into a single buffer before escaping so
// escapePreservingTokens can match trusted Slack tokens that goldmark
// fragments across multiple Text nodes (it splits at `<` while looking
// for autolinks; when no autolink matches, the split sticks). Without
// coalescing, `<!subteam^S…|fb>` would arrive as two segments and
// neither would match the trusted-token regex.
func (e *mdEmitter) emitInlineChildren(parent ast.Node) {
	var buf strings.Builder
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		e.writeEscapedText(buf.String())
		buf.Reset()
	}
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Text:
			buf.Write(t.Segment.Value(e.source))
			if t.HardLineBreak() {
				flush()
				e.b.WriteString("  \n")
			} else if t.SoftLineBreak() {
				flush()
				e.b.WriteByte('\n')
			}
		case *ast.String:
			buf.Write(t.Value)
		default:
			flush()
			e.walkInline(c)
		}
	}
	flush()
}

// walkInline dispatches a single non-text inline node. The Text and
// String cases are intentionally absent — those are handled by
// emitInlineChildren's coalescing buffer so token merging works across
// goldmark's internal text fragmentation. The two cases here exist only
// for the defensive fallback path where an unenumerated node's plain
// text contains a Text/String descendant.
func (e *mdEmitter) walkInline(n ast.Node) {
	switch t := n.(type) {
	case *ast.Text:
		// Reached only via direct call from the fallback path.
		e.writeEscapedText(string(t.Segment.Value(e.source)))
	case *ast.String:
		e.writeEscapedText(string(t.Value))
	case *ast.Emphasis:
		marker := "*"
		if t.Level >= 2 {
			marker = "**"
		}
		e.b.WriteString(marker)
		e.emitInlineChildren(t)
		e.b.WriteString(marker)
	case *extast.Strikethrough:
		e.b.WriteString("~~")
		e.emitInlineChildren(t)
		e.b.WriteString("~~")
	case *ast.CodeSpan:
		e.emitCodeSpan(extractPlainText(t, e.source))
	case *ast.Link:
		e.emitLink(t)
	case *ast.AutoLink:
		e.emitAutoLink(t)
	case *ast.Image:
		e.emitImage(t)
	case *ast.RawHTML:
		// Defang any raw HTML the LLM tried to inject.
		for i := 0; i < t.Segments.Len(); i++ {
			seg := t.Segments.At(i)
			e.writeEscapedText(string(seg.Value(e.source)))
		}
	case *extast.TaskCheckBox:
		// The list-item handler emits the checkbox prefix; this path is
		// only reached for stray checkboxes outside a list. Render the
		// literal CommonMark form so content isn't dropped silently.
		if t.IsChecked {
			e.b.WriteString("[x] ")
		} else {
			e.b.WriteString("[ ] ")
		}
	default:
		// Unknown inline kind: degrade to escaped plain text.
		text := extractPlainText(n, e.source)
		if text != "" {
			e.writeEscapedText(text)
		}
	}
}

// writeEscapedText handles the broadcast-safety escape for plain text
// content. It mirrors the rich_text path's sanitizer so the policy is
// identical across modes.
func (e *mdEmitter) writeEscapedText(s string) {
	if s == "" {
		return
	}
	switch {
	case e.opts.AllowBroadcasts:
		e.b.WriteString(s)
	case e.opts.PreserveMentionTokens:
		e.b.WriteString(escapePreservingTokens(s))
	default:
		e.b.WriteString(entityEscape(s))
	}
}

// emitCodeSpan writes a CommonMark inline code span, choosing the
// shortest backtick run that doesn't collide with content. Content is
// emitted RAW — code spans are literal per CommonMark.
func (e *mdEmitter) emitCodeSpan(content string) {
	runs := longestBacktickRun(content)
	delim := strings.Repeat("`", runs+1)
	pad := ""
	if strings.HasPrefix(content, "`") || strings.HasSuffix(content, "`") {
		pad = " "
	}
	e.b.WriteString(delim)
	e.b.WriteString(pad)
	e.b.WriteString(content)
	e.b.WriteString(pad)
	e.b.WriteString(delim)
}

func longestBacktickRun(s string) int {
	maxRun, cur := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			cur++
			if cur > maxRun {
				maxRun = cur
			}
		} else {
			cur = 0
		}
	}
	return maxRun
}

// emitLink writes a CommonMark inline link. Link text is escaped (it
// can contain user-injected `<!channel>`); URL bytes pass through
// verbatim because they live inside parser-bounded `()`.
func (e *mdEmitter) emitLink(n *ast.Link) {
	e.b.WriteByte('[')
	e.emitInlineChildren(n)
	e.b.WriteString("](")
	e.b.Write(n.Destination)
	if len(n.Title) > 0 {
		e.b.WriteString(` "`)
		title := strings.ReplaceAll(string(n.Title), `"`, `\"`)
		e.b.WriteString(title)
		e.b.WriteByte('"')
	}
	e.b.WriteByte(')')
}

// emitAutoLink converts the AutoLink AST node — which goldmark emits for
// BOTH explicit `<url>` syntax AND bare URLs detected by the Linkify
// extension — into Slack's documented `[text](url)` form. See plan F3:
// the markdown-block docs list `[text](url)` as the only supported link
// syntax; emitting `<url>` would rely on undocumented behavior.
func (e *mdEmitter) emitAutoLink(n *ast.AutoLink) {
	url := string(n.URL(e.source))
	display := url
	target := url
	if n.AutoLinkType == ast.AutoLinkEmail {
		target = "mailto:" + url
	}
	e.b.WriteByte('[')
	e.b.WriteString(display)
	e.b.WriteString("](")
	e.b.WriteString(target)
	e.b.WriteByte(')')
}

// emitImage writes `![alt](url)` syntax. Slack docs say images in the
// markdown block render as hyperlinks (text becomes a clickable link to
// the image URL).
func (e *mdEmitter) emitImage(n *ast.Image) {
	alt := extractPlainText(n, e.source)
	e.b.WriteString("![")
	e.b.WriteString(alt)
	e.b.WriteString("](")
	e.b.Write(n.Destination)
	if len(n.Title) > 0 {
		e.b.WriteString(` "`)
		title := strings.ReplaceAll(string(n.Title), `"`, `\"`)
		e.b.WriteString(title)
		e.b.WriteByte('"')
	}
	e.b.WriteByte(')')
}

func (e *mdEmitter) emitFencedCode(n *ast.FencedCodeBlock) {
	e.b.WriteString("```")
	if lang := n.Language(e.source); len(lang) > 0 {
		e.b.Write(lang)
	}
	e.b.WriteByte('\n')
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		e.b.Write(seg.Value(e.source))
	}
	// Guarantee a newline before the closing fence so `\nfoo\n```\n`
	// renders correctly. goldmark always trails the last line with \n
	// when the source did, but defend against an unterminated input.
	if !strings.HasSuffix(e.b.String(), "\n") {
		e.b.WriteByte('\n')
	}
	e.b.WriteString("```")
}

func (e *mdEmitter) emitIndentedCode(n *ast.CodeBlock) {
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		e.b.WriteString("    ")
		e.b.Write(seg.Value(e.source))
	}
}

// emitBlockquote prefixes every line of the rendered children with `> `,
// matching CommonMark's quote syntax. Nested blockquotes accumulate
// prefixes naturally because their children include another Blockquote
// node that recurses into this method.
func (e *mdEmitter) emitBlockquote(n *ast.Blockquote) {
	sub := &mdEmitter{source: e.source, opts: e.opts}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		sub.walkBlock(c)
	}
	text := strings.TrimRight(sub.b.String(), "\n")
	if text == "" {
		// Empty blockquote — emit the marker on its own line so the
		// structure is still visible.
		e.b.WriteString(">")
		e.writeBlockBreak()
		return
	}
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			e.b.WriteString(">\n")
			continue
		}
		e.b.WriteString("> ")
		e.b.WriteString(line)
		e.b.WriteByte('\n')
	}
	e.writeBlockBreak()
}

// emitList renders an ordered or unordered list. parentIndent is the
// indent prefix accumulated by enclosing lists.
func (e *mdEmitter) emitList(n *ast.List, parentIndent string) {
	num := 1
	if n.IsOrdered() && n.Start >= 1 {
		num = n.Start
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		item, ok := c.(*ast.ListItem)
		if !ok {
			continue
		}
		var marker string
		if n.IsOrdered() {
			marker = strconv.Itoa(num) + ". "
			num++
		} else {
			marker = "- "
		}
		e.emitListItem(item, parentIndent, marker)
	}
}

func (e *mdEmitter) emitListItem(item *ast.ListItem, parentIndent, marker string) {
	// GFM task-list checkbox lives as the first inline child of the
	// item's first text block. Strip it out and turn it into a per-item
	// prefix so we don't render it mid-line.
	prefix := taskListPrefix(item)

	// Render the item's block-level children into a sub-emitter so we
	// can prefix the produced lines with marker + indent.
	sub := &mdEmitter{source: e.source, opts: e.opts}
	for bc := item.FirstChild(); bc != nil; bc = bc.NextSibling() {
		switch bn := bc.(type) {
		case *ast.TextBlock, *ast.Paragraph:
			sub.emitInlineChildrenSkippingCheckbox(bn)
			sub.b.WriteByte('\n')
		case *ast.List:
			// Continuation indent for nested lists: align the nested
			// items under the parent item's content. Add 2 spaces per
			// nesting level (matches CommonMark's tight-list rules).
			sub.emitList(bn, "  ")
		default:
			sub.walkBlock(bn)
		}
	}
	text := strings.TrimRight(sub.b.String(), "\n")
	if text == "" {
		// Empty item — still emit the marker so the structure is visible.
		e.b.WriteString(parentIndent)
		e.b.WriteString(marker)
		e.b.WriteString(prefix)
		e.b.WriteByte('\n')
		return
	}
	contIndent := strings.Repeat(" ", len(marker))
	for i, line := range strings.Split(text, "\n") {
		e.b.WriteString(parentIndent)
		if i == 0 {
			e.b.WriteString(marker)
			e.b.WriteString(prefix)
		} else {
			e.b.WriteString(contIndent)
		}
		e.b.WriteString(line)
		e.b.WriteByte('\n')
	}
}

// emitInlineChildrenSkippingCheckbox is like emitInlineChildren but
// skips a leading TaskCheckBox node — the parent list-item handler emits
// the checkbox as a per-item prefix instead. Uses the same Text/String
// coalescing as emitInlineChildren so trusted-token detection survives
// goldmark's text fragmentation.
func (e *mdEmitter) emitInlineChildrenSkippingCheckbox(parent ast.Node) {
	first := parent.FirstChild()
	if _, ok := first.(*extast.TaskCheckBox); ok {
		first = first.NextSibling()
	}
	var buf strings.Builder
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		e.writeEscapedText(buf.String())
		buf.Reset()
	}
	for c := first; c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Text:
			buf.Write(t.Segment.Value(e.source))
			if t.HardLineBreak() {
				flush()
				e.b.WriteString("  \n")
			} else if t.SoftLineBreak() {
				flush()
				e.b.WriteByte('\n')
			}
		case *ast.String:
			buf.Write(t.Value)
		default:
			flush()
			e.walkInline(c)
		}
	}
	flush()
}

func taskListPrefix(item *ast.ListItem) string {
	for bc := item.FirstChild(); bc != nil; bc = bc.NextSibling() {
		var firstInline ast.Node
		switch bn := bc.(type) {
		case *ast.TextBlock:
			firstInline = bn.FirstChild()
		case *ast.Paragraph:
			firstInline = bn.FirstChild()
		}
		if cb, ok := firstInline.(*extast.TaskCheckBox); ok {
			if cb.IsChecked {
				return "[x] "
			}
			return "[ ] "
		}
	}
	return ""
}

// emitTable reconstructs a standard markdown table from the goldmark AST.
// Alignments are emitted in the separator row using the same forms
// CommonMark recognizes for parsing. Cell contents flow through the
// inline emitter so links, code, etc. inside cells render correctly.
func (e *mdEmitter) emitTable(n *extast.Table) {
	var header []string
	var body [][]string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch row := c.(type) {
		case *extast.TableHeader:
			header = e.renderCells(row)
		case *extast.TableRow:
			body = append(body, e.renderCells(row))
		}
	}
	if len(header) == 0 {
		return
	}
	e.b.WriteString("| ")
	e.b.WriteString(strings.Join(header, " | "))
	e.b.WriteString(" |\n|")
	for i := range header {
		e.b.WriteString(alignmentMarker(n.Alignments, i))
	}
	e.b.WriteByte('\n')
	for _, row := range body {
		// Pad short rows with empty cells so the table stays valid.
		for len(row) < len(header) {
			row = append(row, "")
		}
		e.b.WriteString("| ")
		e.b.WriteString(strings.Join(row[:len(header)], " | "))
		e.b.WriteString(" |\n")
	}
}

func (e *mdEmitter) renderCells(row ast.Node) []string {
	var cells []string
	for c := row.FirstChild(); c != nil; c = c.NextSibling() {
		cell, ok := c.(*extast.TableCell)
		if !ok {
			continue
		}
		sub := &mdEmitter{source: e.source, opts: e.opts}
		for ic := cell.FirstChild(); ic != nil; ic = ic.NextSibling() {
			sub.walkInline(ic)
		}
		text := strings.ReplaceAll(sub.b.String(), "|", `\|`)
		text = strings.ReplaceAll(text, "\n", " ")
		cells = append(cells, text)
	}
	return cells
}

func alignmentMarker(aligns []extast.Alignment, idx int) string {
	a := extast.AlignNone
	if idx < len(aligns) {
		a = aligns[idx]
	}
	switch a {
	case extast.AlignLeft:
		return " :--- |"
	case extast.AlignCenter:
		return " :---: |"
	case extast.AlignRight:
		return " ---: |"
	default:
		return " --- |"
	}
}
