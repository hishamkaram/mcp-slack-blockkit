package converter

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// firstPreformatted returns the first rich_text_preformatted element from
// the converted output, failing the test if none exists.
func firstPreformatted(t *testing.T, blocks []slack.Block) *slack.RichTextPreformatted {
	t.Helper()
	for _, b := range blocks {
		rt, ok := b.(*slack.RichTextBlock)
		if !ok {
			continue
		}
		for _, el := range rt.Elements {
			if pre, ok := el.(*slack.RichTextPreformatted); ok {
				return pre
			}
		}
	}
	t.Fatal("no rich_text_preformatted in output")
	return nil
}

// preText concatenates the text content of a rich_text_preformatted's
// elements. Convenient for body assertions.
func preText(pre *slack.RichTextPreformatted) string {
	var sb strings.Builder
	for _, el := range pre.Elements {
		if t, ok := el.(*slack.RichTextSectionTextElement); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}

// --- Fenced code blocks ------------------------------------------------------

func TestHandleFencedCode_WithLanguageTag_PreservesLanguage(t *testing.T) {
	input := "```go\nfunc main() {\n\tprintln(\"hi\")\n}\n```\n"
	blocks, _ := renderForTest(t, Options{}, input)
	pre := firstPreformatted(t, blocks)
	if pre.Language != "go" {
		t.Errorf("language = %q, want %q", pre.Language, "go")
	}
	if !strings.Contains(preText(pre), "func main()") {
		t.Errorf("body missing 'func main()': %q", preText(pre))
	}
	if !strings.Contains(preText(pre), `println("hi")`) {
		t.Errorf("body missing println line: %q", preText(pre))
	}
}

func TestHandleFencedCode_NoLanguage_OmitsLanguageField(t *testing.T) {
	input := "```\nbare code\n```\n"
	blocks, _ := renderForTest(t, Options{}, input)
	pre := firstPreformatted(t, blocks)
	if pre.Language != "" {
		t.Errorf("language should be empty for un-tagged fence, got %q", pre.Language)
	}
	if !strings.Contains(preText(pre), "bare code") {
		t.Errorf("body missing content: %q", preText(pre))
	}
}

func TestHandleFencedCode_PreservesNewlines(t *testing.T) {
	input := "```\nline 1\nline 2\nline 3\n```\n"
	blocks, _ := renderForTest(t, Options{}, input)
	body := preText(firstPreformatted(t, blocks))
	want := "line 1\nline 2\nline 3\n"
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestHandleFencedCode_MultiWordLanguage_KeepsFirstWord(t *testing.T) {
	// CommonMark spec: only the first word after the fence is the info
	// string; goldmark exposes Language() returning that first word.
	input := "```python script\nprint('hi')\n```\n"
	blocks, _ := renderForTest(t, Options{}, input)
	pre := firstPreformatted(t, blocks)
	if pre.Language != "python" {
		t.Errorf("language = %q, want %q (first word only)", pre.Language, "python")
	}
}

func TestHandleFencedCode_EmptyBody_EmitsNothing(t *testing.T) {
	input := "```\n```\n"
	blocks, _ := renderForTest(t, Options{}, input)
	for _, b := range blocks {
		rt, ok := b.(*slack.RichTextBlock)
		if !ok {
			continue
		}
		for _, el := range rt.Elements {
			if _, ok := el.(*slack.RichTextPreformatted); ok {
				t.Error("empty fenced block should not produce a preformatted element")
			}
		}
	}
}

// --- Indented code blocks ----------------------------------------------------

func TestHandleIndentedCode_FourSpaceIndent_EmitsPreformatted(t *testing.T) {
	// CommonMark: 4-space indent makes a code block. We need a blank line
	// before it (otherwise it joins the prior paragraph).
	input := "Some prose.\n\n    indented code\n    more code\n"
	blocks, _ := renderForTest(t, Options{}, input)

	// Two blocks: paragraph + preformatted.
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(blocks))
	}
	pre := firstPreformatted(t, blocks)
	if pre.Language != "" {
		t.Errorf("indented blocks have no language; got %q", pre.Language)
	}
	if !strings.Contains(preText(pre), "indented code") {
		t.Errorf("body missing content: %q", preText(pre))
	}
	if !strings.Contains(preText(pre), "more code") {
		t.Errorf("body missing second line: %q", preText(pre))
	}
}

func TestHandleIndentedCode_TabIndent_EmitsPreformatted(t *testing.T) {
	// CommonMark accepts a tab as 4-column equivalent for code blocks.
	input := "intro\n\n\tcode line\n"
	blocks, _ := renderForTest(t, Options{}, input)
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	pre := firstPreformatted(t, blocks)
	if !strings.Contains(preText(pre), "code line") {
		t.Errorf("body = %q", preText(pre))
	}
}

// --- Round-trip + JSON shape ------------------------------------------------

func TestCodeBlock_JSONShape_HasPreformattedType(t *testing.T) {
	input := "```js\nconsole.log('x')\n```\n"
	_, payload := renderForTest(t, Options{}, input)
	if !strings.Contains(payload, `"type":"rich_text_preformatted"`) {
		t.Errorf("payload missing rich_text_preformatted type: %s", payload)
	}
	if !strings.Contains(payload, `"language":"js"`) {
		t.Errorf("payload missing language tag: %s", payload)
	}
}

func TestCodeBlock_LongBody_NotTruncated(t *testing.T) {
	// 200 lines of code; should pass through completely. (Splitter will
	// chunk by block count later, but a single preformatted should not be
	// internally truncated.)
	var b strings.Builder
	b.WriteString("```\n")
	for i := 0; i < 200; i++ {
		b.WriteString("line ")
		b.WriteString(string(rune('0' + (i % 10))))
		b.WriteByte('\n')
	}
	b.WriteString("```\n")

	blocks, _ := renderForTest(t, Options{}, b.String())
	pre := firstPreformatted(t, blocks)
	body := preText(pre)
	if strings.Count(body, "\n") < 200 {
		t.Errorf("expected ≥200 newlines in body; got %d", strings.Count(body, "\n"))
	}
}
