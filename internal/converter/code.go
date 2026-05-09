package converter

import (
	"bytes"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark/ast"
)

// handleFencedCode emits a rich_text block containing one rich_text_preformatted
// element. The fenced-code language tag (e.g. ```go) is preserved on the
// element's `language` field — slack-go's RichTextPreformatted carries it
// via `json:"language,omitempty"`. Whether Slack itself syntax-highlights is
// a render-side concern; we always include the tag when present so downstream
// consumers (Slack clients, custom integrations) can use it.
func (w *walker) handleFencedCode(node *ast.FencedCodeBlock) error {
	body := codeBlockText(node, w.source)
	if body == "" {
		return nil
	}
	pre := &slack.RichTextPreformatted{
		Type: slack.RTEPreformatted,
		Elements: []slack.RichTextSectionElement{
			slack.NewRichTextSectionTextElement(body, nil),
		},
		Language: string(node.Language(w.source)),
	}
	rt := slack.NewRichTextBlock(w.nextBlockID(), pre)
	w.blocks = append(w.blocks, rt)
	return nil
}

// handleIndentedCode emits a rich_text_preformatted from an indented code
// block (4-space indent). No language tag — indented code blocks have no
// way to specify one in CommonMark.
func (w *walker) handleIndentedCode(node *ast.CodeBlock) error {
	body := codeBlockText(node, w.source)
	if body == "" {
		return nil
	}
	pre := &slack.RichTextPreformatted{
		Type: slack.RTEPreformatted,
		Elements: []slack.RichTextSectionElement{
			slack.NewRichTextSectionTextElement(body, nil),
		},
	}
	rt := slack.NewRichTextBlock(w.nextBlockID(), pre)
	w.blocks = append(w.blocks, rt)
	return nil
}

// codeBlockText reconstructs the literal source of a code block by
// concatenating its text segments. Both FencedCodeBlock and CodeBlock store
// their content as text-segment lines; we rejoin them preserving the
// trailing `\n` on each line that goldmark already includes.
func codeBlockText(node ast.Node, source []byte) string {
	var buf bytes.Buffer
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		// Segment.Value is a pointer receiver; take the address explicitly.
		seg := lines.At(i)
		buf.Write(seg.Value(source))
	}
	return buf.String()
}
