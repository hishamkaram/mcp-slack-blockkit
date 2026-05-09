package converter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// renderForTest is the shared helper for block-handler tests: build a
// renderer with the caller's options merged onto DefaultOptions (so callers
// only need to set the fields they care about), convert the input, and
// return both the typed blocks and their JSON form so assertions can pick
// the level of detail they need.
//
// The helper defaults to ModeRichText (rather than DefaultOptions's
// ModeAuto) because most block-handler tests assert on the deterministic
// decomposition shapes — auto mode would route many of those inputs to a
// single markdown block instead. Auto-mode behavior has its own dedicated
// test file (markdown_block_test.go).
func renderForTest(t *testing.T, opts Options, input string) ([]slack.Block, string) {
	t.Helper()
	merged := DefaultOptions()
	merged.Mode = ModeRichText // override: keep decomposition shape for these tests
	if opts.Mode != "" {
		merged.Mode = opts.Mode
	}
	if opts.BlockIDPrefix != "" {
		merged.BlockIDPrefix = opts.BlockIDPrefix
	}
	if opts.MaxBlocksPerChunk != 0 {
		merged.MaxBlocksPerChunk = opts.MaxBlocksPerChunk
	}
	if opts.ParagraphCharLimit != 0 {
		merged.ParagraphCharLimit = opts.ParagraphCharLimit
	}
	if opts.MaxInputBytes != 0 {
		merged.MaxInputBytes = opts.MaxInputBytes
	}
	merged.EmitStandaloneLinkAsButton = opts.EmitStandaloneLinkAsButton
	merged.AllowBroadcasts = opts.AllowBroadcasts
	if opts.MentionMap != nil {
		merged.MentionMap = opts.MentionMap
	}
	// EnableTables: caller's value wins only when explicitly false; default true.
	if !opts.EnableTables {
		// We can't tell a zero-value bool from an explicit-false in Go; use
		// the convention that callers wanting tables disabled must say so.
		// For now, callers tweaking EnableTables=false call New() directly.
	}

	r, err := New(merged)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert(input)
	if err != nil {
		t.Fatalf("Convert(%q): %v", input, err)
	}
	payload, err := json.Marshal(slack.Blocks{BlockSet: blocks})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return blocks, string(payload)
}

// --- Heading handler ---------------------------------------------------------

func TestHandleHeading_H1Short_EmitsHeaderBlock(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "# Hello world")
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	hdr, ok := blocks[0].(*slack.HeaderBlock)
	if !ok {
		t.Fatalf("got %T, want *slack.HeaderBlock", blocks[0])
	}
	if hdr.Text == nil || hdr.Text.Type != slack.PlainTextType {
		t.Errorf("header text must be plain_text, got %+v", hdr.Text)
	}
	if hdr.Text.Text != "Hello world" {
		t.Errorf("header text = %q, want %q", hdr.Text.Text, "Hello world")
	}
}

func TestHandleHeading_H1ExactlyAtLimit_StaysHeaderBlock(t *testing.T) {
	text := strings.Repeat("a", MaxHeaderTextChars)
	blocks, _ := renderForTest(t, Options{}, "# "+text)
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	if _, ok := blocks[0].(*slack.HeaderBlock); !ok {
		t.Errorf("got %T, want *slack.HeaderBlock for text exactly at %d-char limit",
			blocks[0], MaxHeaderTextChars)
	}
}

func TestHandleHeading_H1Over150_FallsBackToBoldSection(t *testing.T) {
	text := strings.Repeat("x", MaxHeaderTextChars+1)
	blocks, payload := renderForTest(t, Options{}, "# "+text)
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	if _, ok := blocks[0].(*slack.HeaderBlock); ok {
		t.Errorf("expected fallback (not HeaderBlock) for >%d-char heading", MaxHeaderTextChars)
	}
	sec, ok := blocks[0].(*slack.SectionBlock)
	if !ok {
		t.Fatalf("got %T, want *slack.SectionBlock fallback", blocks[0])
	}
	if sec.Text.Type != slack.MarkdownType {
		t.Errorf("fallback must use mrkdwn, got %s", sec.Text.Type)
	}
	if !strings.HasPrefix(sec.Text.Text, "*") || !strings.HasSuffix(sec.Text.Text, "*") {
		t.Errorf("fallback must be bold-wrapped; got %q", sec.Text.Text)
	}
	if !strings.Contains(payload, `"type":"section"`) {
		t.Errorf("payload missing section type: %s", payload)
	}
}

func TestHandleHeading_H1WithLink_FallsBackToSection(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "# See [docs](https://example.com)")
	if _, ok := blocks[0].(*slack.HeaderBlock); ok {
		t.Error("heading containing a link should fall back to section, not header")
	}
}

func TestHandleHeading_H1WithImage_FallsBackToSection(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "# Logo ![alt](https://example.com/x.png)")
	if _, ok := blocks[0].(*slack.HeaderBlock); ok {
		t.Error("heading containing an image should fall back to section, not header")
	}
}

func TestHandleHeading_H1WithInlineCode_FallsBackToSection(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "# Use `Convert()`")
	if _, ok := blocks[0].(*slack.HeaderBlock); ok {
		t.Error("heading containing inline code should fall back to section, not header")
	}
}

func TestHandleHeading_H1Empty_EmitsDivider(t *testing.T) {
	// An empty `#` line produces a heading node with no content.
	blocks, _ := renderForTest(t, Options{}, "# \n")
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	if _, ok := blocks[0].(*slack.DividerBlock); !ok {
		t.Errorf("empty heading should degenerate to divider; got %T", blocks[0])
	}
}

func TestHandleHeading_H2_EmitsBoldSection(t *testing.T) {
	blocks, payload := renderForTest(t, Options{}, "## Subtitle")
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	sec, ok := blocks[0].(*slack.SectionBlock)
	if !ok {
		t.Fatalf("h2 should emit section; got %T", blocks[0])
	}
	if sec.Text.Text != "*Subtitle*" {
		t.Errorf("h2 text = %q, want %q", sec.Text.Text, "*Subtitle*")
	}
	if !strings.Contains(payload, `"text":"*Subtitle*"`) {
		t.Errorf("payload should contain bold text: %s", payload)
	}
}

func TestHandleHeading_H6_EmitsBoldSection(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "###### Tiny")
	if _, ok := blocks[0].(*slack.SectionBlock); !ok {
		t.Errorf("h6 should emit section; got %T", blocks[0])
	}
}

func TestHandleHeading_BoldFallback_EscapesEmphasisChars(t *testing.T) {
	// User-supplied `*` and `_` inside a bold-wrapped heading must be escaped
	// or they'd close our wrapping `*` early. The escapeMrkdwnEmphasis helper
	// guarantees this.
	blocks, _ := renderForTest(t, Options{}, "## Pricing: 10*x + 5_y")
	sec := blocks[0].(*slack.SectionBlock)
	want := `*Pricing: 10\*x + 5\_y*`
	if sec.Text.Text != want {
		t.Errorf("bold-section text = %q, want %q", sec.Text.Text, want)
	}
}

// --- Paragraph handler -------------------------------------------------------

func TestHandleParagraph_PlainText_EmitsRichText(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "This is a paragraph.")
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	rt, ok := blocks[0].(*slack.RichTextBlock)
	if !ok {
		t.Fatalf("got %T, want *slack.RichTextBlock", blocks[0])
	}
	if len(rt.Elements) != 1 {
		t.Fatalf("rich_text has %d elements, want 1", len(rt.Elements))
	}
	sec, ok := rt.Elements[0].(*slack.RichTextSection)
	if !ok {
		t.Fatalf("rich_text element is %T, want *RichTextSection", rt.Elements[0])
	}
	if len(sec.Elements) != 1 {
		t.Fatalf("rich_text_section has %d elements, want 1", len(sec.Elements))
	}
	textEl, ok := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if !ok {
		t.Fatalf("section element is %T, want *RichTextSectionTextElement", sec.Elements[0])
	}
	if textEl.Text != "This is a paragraph." {
		t.Errorf("text = %q, want %q", textEl.Text, "This is a paragraph.")
	}
}

func TestHandleParagraph_EmptyInput_EmitsNoBlocks(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "   \n  ")
	if len(blocks) != 0 {
		t.Errorf("whitespace-only input should produce 0 blocks, got %d", len(blocks))
	}
}

// --- Standalone-image handling ----------------------------------------------

func TestHandleParagraph_StandaloneImage_EmitsImageBlock(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "![Logo](https://example.com/logo.png)")
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	img, ok := blocks[0].(*slack.ImageBlock)
	if !ok {
		t.Fatalf("got %T, want *slack.ImageBlock", blocks[0])
	}
	if img.ImageURL != "https://example.com/logo.png" {
		t.Errorf("image_url = %q, want %q", img.ImageURL, "https://example.com/logo.png")
	}
	if img.AltText != "Logo" {
		t.Errorf("alt_text = %q, want %q", img.AltText, "Logo")
	}
}

func TestHandleParagraph_StandaloneImageWithTitle_EmitsImageBlockWithTitle(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, `![Diagram](https://example.com/d.png "Architecture diagram")`)
	img, ok := blocks[0].(*slack.ImageBlock)
	if !ok {
		t.Fatalf("got %T, want *slack.ImageBlock", blocks[0])
	}
	if img.Title == nil || img.Title.Text != "Architecture diagram" {
		t.Errorf("expected title 'Architecture diagram', got %+v", img.Title)
	}
}

func TestHandleParagraph_MixedTextAndImage_DoesNotEmitImageBlock(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "See ![pic](https://example.com/p.png) here.")
	if _, ok := blocks[0].(*slack.ImageBlock); ok {
		t.Error("paragraph mixing text and image should NOT emit an image block")
	}
	if _, ok := blocks[0].(*slack.RichTextBlock); !ok {
		t.Errorf("expected rich_text block for mixed text+image, got %T", blocks[0])
	}
}

func TestHandleParagraph_ImageWithEmptyURL_DropsBlock(t *testing.T) {
	// goldmark accepts `![alt]()` and parses it as an image with empty URL.
	// We drop these silently rather than emitting an invalid image block.
	blocks, _ := renderForTest(t, Options{}, "![alt]()")
	if len(blocks) != 0 {
		t.Errorf("image with empty URL should drop block, got %d blocks", len(blocks))
	}
}

func TestHandleParagraph_StandaloneImageWithSurroundingWhitespace_StillEmitsImageBlock(t *testing.T) {
	// Goldmark may surround the image node with whitespace text nodes (e.g.
	// trailing newline). Whitespace-only siblings should not disqualify.
	blocks, _ := renderForTest(t, Options{}, "  ![Logo](https://example.com/x.png)  ")
	if _, ok := blocks[0].(*slack.ImageBlock); !ok {
		t.Errorf("expected image block, got %T", blocks[0])
	}
}

// --- ThematicBreak / divider ------------------------------------------------

func TestHandleThematicBreak_EmitsDivider(t *testing.T) {
	for _, src := range []string{"---", "***", "___"} {
		t.Run(src, func(t *testing.T) {
			blocks, _ := renderForTest(t, Options{}, src+"\n")
			if len(blocks) != 1 {
				t.Fatalf("got %d blocks for %q", len(blocks), src)
			}
			if _, ok := blocks[0].(*slack.DividerBlock); !ok {
				t.Errorf("got %T, want *slack.DividerBlock", blocks[0])
			}
		})
	}
}

// --- Blockquote -------------------------------------------------------------

func TestHandleBlockquote_PlainText_EmitsRichTextWithQuote(t *testing.T) {
	blocks, payload := renderForTest(t, Options{}, "> This is a quoted line.")
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	rt, ok := blocks[0].(*slack.RichTextBlock)
	if !ok {
		t.Fatalf("got %T, want *slack.RichTextBlock", blocks[0])
	}
	if len(rt.Elements) != 1 {
		t.Fatalf("rich_text has %d elements", len(rt.Elements))
	}
	if _, ok := rt.Elements[0].(*slack.RichTextQuote); !ok {
		t.Errorf("got %T, want *slack.RichTextQuote", rt.Elements[0])
	}
	if !strings.Contains(payload, `"type":"rich_text_quote"`) {
		t.Errorf("payload missing rich_text_quote type: %s", payload)
	}
	if !strings.Contains(payload, "quoted line") {
		t.Errorf("quote text missing from payload: %s", payload)
	}
}

func TestHandleBlockquote_NestedQuote_FlattensIntoParent(t *testing.T) {
	// Slack has no nested-quote support — extracted text should appear in
	// the single top-level quote element.
	blocks, _ := renderForTest(t, Options{}, "> outer\n> > nested\n")
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	rt := blocks[0].(*slack.RichTextBlock)
	if len(rt.Elements) != 1 {
		t.Errorf("expected exactly 1 rich_text element (no nested quote), got %d", len(rt.Elements))
	}
}

func TestHandleBlockquote_Empty_EmitsNothing(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, ">\n")
	if len(blocks) != 0 {
		t.Errorf("empty blockquote should emit 0 blocks, got %d", len(blocks))
	}
}

// --- Multi-block sequence ---------------------------------------------------

func TestConvert_MultipleConstructs_PreservesOrder(t *testing.T) {
	input := `# Title

A paragraph here.

---

> A quote.
`
	blocks, _ := renderForTest(t, Options{}, input)
	if len(blocks) != 4 {
		t.Fatalf("got %d blocks, want 4", len(blocks))
	}
	if _, ok := blocks[0].(*slack.HeaderBlock); !ok {
		t.Errorf("blocks[0] = %T, want HeaderBlock", blocks[0])
	}
	if _, ok := blocks[1].(*slack.RichTextBlock); !ok {
		t.Errorf("blocks[1] = %T, want RichTextBlock (paragraph)", blocks[1])
	}
	if _, ok := blocks[2].(*slack.DividerBlock); !ok {
		t.Errorf("blocks[2] = %T, want DividerBlock", blocks[2])
	}
	if _, ok := blocks[3].(*slack.RichTextBlock); !ok {
		t.Errorf("blocks[3] = %T, want RichTextBlock (quote)", blocks[3])
	}
}

// --- Block-ID prefix --------------------------------------------------------

func TestBlockIDPrefix_AppliedSequentially(t *testing.T) {
	blocks, _ := renderForTest(t, Options{BlockIDPrefix: "test"}, "# A\n\nB\n\n---")
	if len(blocks) != 3 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	wantPrefixes := []string{"test-1", "test-2", "test-3"}
	for i, b := range blocks {
		got := b.ID()
		if got != wantPrefixes[i] {
			t.Errorf("blocks[%d].ID() = %q, want %q", i, got, wantPrefixes[i])
		}
	}
}

func TestBlockIDPrefix_EmptyMeansEmptyIDs(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "# A\n\nB\n")
	for i, b := range blocks {
		if b.ID() != "" {
			t.Errorf("blocks[%d].ID() = %q, want empty", i, b.ID())
		}
	}
}

// --- Helper unit tests ------------------------------------------------------

func TestEscapeMrkdwnEmphasis(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain text", "plain text"},
		{"a*b", `a\*b`},
		{"a_b", `a\_b`},
		{"a~b", `a\~b`},
		{"a`b", "a\\`b"},
		{"all *_~`", "all \\*\\_\\~\\`"},
		{"mixed ✓ unicode *", "mixed ✓ unicode \\*"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := escapeMrkdwnEmphasis(tc.in)
			if got != tc.want {
				t.Errorf("escapeMrkdwnEmphasis(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// --- JSON-shape regression --------------------------------------------------

func TestConvert_ResultMarshalsToValidJSON(t *testing.T) {
	cases := []struct {
		name, in string
	}{
		{"heading", "# Title"},
		{"paragraph", "Body text"},
		{"divider", "---"},
		{"blockquote", "> A quote"},
		{"image", "![alt](https://example.com/x.png)"},
		{"mixed", "# T\n\nbody\n\n---\n\n> q\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocks, payload := renderForTest(t, Options{}, tc.in)
			if len(blocks) == 0 && tc.in != "" {
				t.Fatal("got 0 blocks for non-empty input")
			}
			// Must round-trip through json.Unmarshal back into Blocks.
			var rt slack.Blocks
			if err := json.Unmarshal([]byte(payload), &rt); err != nil {
				t.Errorf("output JSON does not round-trip: %v\npayload=%s", err, payload)
			}
		})
	}
}
