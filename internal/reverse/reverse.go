// Package reverse converts Slack Block Kit payloads back into Markdown.
//
// It is the inverse of internal/converter and is wired into the
// block_kit_to_markdown MCP tool. The conversion is best-effort and lossy
// by nature: Block Kit can express styling, structure, and interactive
// elements (buttons, accessories, colors, context blocks) that have no
// exact Markdown equivalent. Such constructs are rendered as closely as
// possible and every lossy decision is recorded in the warnings slice
// returned alongside the Markdown.
package reverse

import (
	"fmt"
	"strings"

	"github.com/slack-go/slack"
)

// ToMarkdown converts a Slack Block Kit block list into Markdown text. The
// returned warnings slice names every construct that could not be
// represented faithfully. The error is reserved for internal failures; an
// empty block list yields ("", nil, nil).
func ToMarkdown(blocks []slack.Block) (string, []string, error) {
	w := &writer{warned: map[string]bool{}}
	for _, b := range blocks {
		w.block(b)
	}
	return strings.TrimRight(w.b.String(), "\n"), w.warnings, nil
}

type writer struct {
	b        strings.Builder
	warnings []string
	warned   map[string]bool
}

// warn records a lossy-conversion note, deduplicated by message so a
// payload with 50 mrkdwn sections produces one warning, not 50.
func (w *writer) warn(msg string) {
	if w.warned[msg] {
		return
	}
	w.warned[msg] = true
	w.warnings = append(w.warnings, msg)
}

// endBlock ensures the buffer ends with a blank-line separator so the next
// block starts cleanly. No-op on an empty buffer.
func (w *writer) endBlock() {
	s := w.b.String()
	switch {
	case s == "":
	case strings.HasSuffix(s, "\n\n"):
	case strings.HasSuffix(s, "\n"):
		w.b.WriteByte('\n')
	default:
		w.b.WriteString("\n\n")
	}
}

func (w *writer) block(b slack.Block) {
	switch t := b.(type) {
	case *slack.HeaderBlock:
		if t.Text != nil && t.Text.Text != "" {
			w.b.WriteString("# ")
			w.b.WriteString(t.Text.Text)
			w.endBlock()
		}
	case *slack.DividerBlock:
		w.b.WriteString("---")
		w.endBlock()
	case *slack.SectionBlock:
		w.section(t)
	case *slack.RichTextBlock:
		for _, el := range t.Elements {
			w.richElement(el)
		}
	case *slack.MarkdownBlock:
		// markdown blocks already carry CommonMark text — emit verbatim.
		if txt := strings.TrimRight(t.Text, "\n"); txt != "" {
			w.b.WriteString(txt)
			w.endBlock()
		}
	case *slack.ImageBlock:
		w.image(t)
	case *slack.TableBlock:
		w.table(t)
	case *slack.ContextBlock:
		w.context(t)
	case *slack.ActionBlock:
		w.warn("actions blocks (buttons, menus) have no Markdown equivalent and were omitted")
	default:
		w.warn(fmt.Sprintf("block type %T has no Markdown equivalent and was omitted", b))
	}
}

func (w *writer) section(s *slack.SectionBlock) {
	if s.Text != nil && s.Text.Text != "" {
		if s.Text.Type == slack.MarkdownType {
			w.warn("section blocks carry Slack mrkdwn, which differs from CommonMark " +
				"(e.g. *bold*, <url|label> links); text emitted verbatim and may need review")
		}
		w.b.WriteString(s.Text.Text)
		w.endBlock()
	}
	for _, f := range s.Fields {
		if f == nil || f.Text == "" {
			continue
		}
		w.b.WriteString(f.Text)
		w.endBlock()
	}
	if s.Accessory != nil {
		w.warn("section accessory elements (buttons, images, menus) have no Markdown equivalent and were omitted")
	}
}

func (w *writer) richElement(el slack.RichTextElement) {
	switch t := el.(type) {
	case *slack.RichTextSection:
		if txt := w.inlines(t.Elements); txt != "" {
			w.b.WriteString(txt)
			w.endBlock()
		}
	case *slack.RichTextList:
		w.list(t)
	case *slack.RichTextQuote:
		w.quote(t)
	case *slack.RichTextPreformatted:
		w.preformatted(t)
	default:
		w.warn(fmt.Sprintf("rich_text element %T was omitted", el))
	}
}

func (w *writer) list(l *slack.RichTextList) {
	indent := strings.Repeat("    ", l.Indent) // 4 spaces per nesting level
	for i, child := range l.Elements {
		sec, ok := child.(*slack.RichTextSection)
		if !ok {
			continue
		}
		w.b.WriteString(indent)
		if l.Style == slack.RTEListOrdered {
			fmt.Fprintf(&w.b, "%d. ", l.Offset+i+1)
		} else {
			w.b.WriteString("- ")
		}
		w.b.WriteString(w.inlines(sec.Elements))
		w.b.WriteByte('\n')
	}
	w.endBlock()
}

func (w *writer) quote(q *slack.RichTextQuote) {
	text := w.inlines(q.Elements)
	if text == "" {
		return
	}
	for _, line := range strings.Split(text, "\n") {
		w.b.WriteString("> ")
		w.b.WriteString(line)
		w.b.WriteByte('\n')
	}
	w.endBlock()
}

func (w *writer) preformatted(p *slack.RichTextPreformatted) {
	w.b.WriteString("```")
	w.b.WriteString(p.Language)
	w.b.WriteByte('\n')
	body := w.inlinesRaw(p.Elements)
	w.b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		w.b.WriteByte('\n')
	}
	w.b.WriteString("```")
	w.endBlock()
}

func (w *writer) image(img *slack.ImageBlock) {
	url := img.ImageURL
	if url == "" && img.SlackFile != nil {
		url = img.SlackFile.URL
	}
	if url == "" {
		w.warn("image block has no public URL (slack_file reference); emitted with an empty link")
	}
	title := ""
	if img.Title != nil && img.Title.Text != "" {
		title = ` "` + strings.ReplaceAll(img.Title.Text, `"`, `\"`) + `"`
	}
	fmt.Fprintf(&w.b, "![%s](%s%s)", img.AltText, url, title)
	w.endBlock()
}

func (w *writer) context(c *slack.ContextBlock) {
	var parts []string
	for _, el := range c.ContextElements.Elements {
		switch t := el.(type) {
		case *slack.TextBlockObject:
			if t.Text != "" {
				parts = append(parts, t.Text)
			}
		case *slack.ImageBlockElement:
			url := ""
			if t.ImageURL != nil {
				url = *t.ImageURL
			}
			parts = append(parts, fmt.Sprintf("![%s](%s)", t.AltText, url))
		}
	}
	if len(parts) == 0 {
		return
	}
	w.warn("context blocks render as small secondary text in Slack; emitted as a plain line")
	w.b.WriteString(strings.Join(parts, " "))
	w.endBlock()
}

func (w *writer) table(tbl *slack.TableBlock) {
	if len(tbl.Rows) == 0 {
		return
	}
	for r, row := range tbl.Rows {
		w.b.WriteString("|")
		for _, cell := range row {
			w.b.WriteByte(' ')
			w.b.WriteString(w.tableCell(cell))
			w.b.WriteString(" |")
		}
		w.b.WriteByte('\n')
		if r == 0 {
			w.b.WriteString("|")
			for range row {
				w.b.WriteString(" --- |")
			}
			w.b.WriteByte('\n')
		}
	}
	w.endBlock()
}

// tableCell flattens a table cell (itself a rich_text block) to a single
// Markdown line, escaping the `|` column delimiter and collapsing newlines.
func (w *writer) tableCell(cell *slack.RichTextBlock) string {
	if cell == nil {
		return ""
	}
	var sb strings.Builder
	for _, el := range cell.Elements {
		if sec, ok := el.(*slack.RichTextSection); ok {
			sb.WriteString(w.inlines(sec.Elements))
		}
	}
	s := strings.ReplaceAll(sb.String(), "\n", " ")
	return strings.ReplaceAll(s, "|", `\|`)
}

// inlines renders a run of inline elements with Markdown styling applied.
func (w *writer) inlines(els []slack.RichTextSectionElement) string {
	var sb strings.Builder
	for _, el := range els {
		sb.WriteString(w.inline(el))
	}
	return sb.String()
}

func (w *writer) inline(el slack.RichTextSectionElement) string {
	switch t := el.(type) {
	case *slack.RichTextSectionTextElement:
		return applyStyle(t.Text, t.Style)
	case *slack.RichTextSectionLinkElement:
		label := t.Text
		if label == "" {
			label = t.URL
		}
		return fmt.Sprintf("[%s](%s)", label, t.URL)
	case *slack.RichTextSectionUserElement:
		return "<@" + t.UserID + ">"
	case *slack.RichTextSectionChannelElement:
		return "<#" + t.ChannelID + ">"
	case *slack.RichTextSectionUserGroupElement:
		return "<!subteam^" + t.UsergroupID + ">"
	case *slack.RichTextSectionTeamElement:
		return "<!team^" + t.TeamID + ">"
	case *slack.RichTextSectionBroadcastElement:
		return "<!" + t.Range + ">"
	case *slack.RichTextSectionEmojiElement:
		return ":" + t.Name + ":"
	case *slack.RichTextSectionDateElement:
		if t.Fallback != nil && *t.Fallback != "" {
			return *t.Fallback
		}
		return t.Format
	case *slack.RichTextSectionColorElement:
		return t.Value
	default:
		w.warn(fmt.Sprintf("rich_text inline element %T was omitted", el))
		return ""
	}
}

// inlinesRaw renders inline elements WITHOUT Markdown styling — used inside
// fenced code blocks, where CommonMark treats content as literal.
func (w *writer) inlinesRaw(els []slack.RichTextSectionElement) string {
	var sb strings.Builder
	for _, el := range els {
		switch t := el.(type) {
		case *slack.RichTextSectionTextElement:
			sb.WriteString(t.Text)
		case *slack.RichTextSectionLinkElement:
			if t.Text != "" {
				sb.WriteString(t.Text)
			} else {
				sb.WriteString(t.URL)
			}
		default:
			sb.WriteString(w.inline(el))
		}
	}
	return sb.String()
}

// applyStyle wraps text in the Markdown emphasis markers matching the Slack
// rich-text style flags. Code wins outright: CommonMark code spans are
// literal, so bold/italic cannot nest inside them.
func applyStyle(text string, style *slack.RichTextSectionTextStyle) string {
	if style == nil || text == "" {
		return text
	}
	if style.Code {
		return "`" + text + "`"
	}
	if style.Italic {
		text = "_" + text + "_"
	}
	if style.Bold {
		text = "**" + text + "**"
	}
	if style.Strike {
		text = "~~" + text + "~~"
	}
	return text
}
