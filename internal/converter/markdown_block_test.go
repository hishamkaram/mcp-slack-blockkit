package converter

import (
	"errors"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// firstMarkdownBlock returns the first slack.MarkdownBlock from the
// converted output, failing the test if none exists.
func firstMarkdownBlock(t *testing.T, blocks []slack.Block) *slack.MarkdownBlock {
	t.Helper()
	for _, b := range blocks {
		if mb, ok := b.(*slack.MarkdownBlock); ok {
			return mb
		}
	}
	t.Fatal("no MarkdownBlock in output")
	return nil
}

// --- ModeMarkdownBlock: explicit ---

func TestMarkdownBlock_ExplicitMode_EmitsSingleMarkdownBlock(t *testing.T) {
	r, err := New(Options{Mode: ModeMarkdownBlock})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("# Title\n\n**bold** body with [link](https://x.com).")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want exactly 1 markdown block", len(blocks))
	}
	mb := firstMarkdownBlock(t, blocks)
	if !strings.Contains(mb.Text, "# Title") {
		t.Errorf("markdown block text missing heading: %q", mb.Text)
	}
	if !strings.Contains(mb.Text, "**bold**") {
		t.Errorf("markdown block text missing bold marker: %q", mb.Text)
	}
}

func TestMarkdownBlock_ExplicitMode_OverLimitReturnsError(t *testing.T) {
	r, err := New(Options{Mode: ModeMarkdownBlock, MaxInputBytes: MaxMarkdownBlockSum * 2})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tooBig := strings.Repeat("a", MaxMarkdownBlockSum+1)
	_, err = r.Convert(tooBig)
	if err == nil {
		t.Fatal("expected error for input >12k chars in markdown_block mode")
	}
	if !errors.Is(err, ErrMarkdownBlockTooLarge) {
		t.Errorf("error %v is not ErrMarkdownBlockTooLarge", err)
	}
}

func TestMarkdownBlock_ExplicitMode_EscapesAngleBrackets(t *testing.T) {
	// Even in markdown_block mode, raw `<!channel>` should not pass
	// through (would broadcast). Step 11 will replace this with a more
	// nuanced policy; for now the basic escape is the safety net.
	r, err := New(Options{Mode: ModeMarkdownBlock})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("alert <!channel> please")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	mb := firstMarkdownBlock(t, blocks)
	if strings.Contains(mb.Text, "<!channel>") {
		t.Errorf("raw <!channel> survived basic escape: %q", mb.Text)
	}
	if !strings.Contains(mb.Text, "&lt;!channel&gt;") {
		t.Errorf("expected entity-escaped form, got %q", mb.Text)
	}
}

func TestEntityEscape_HandlesAmpersandFirst(t *testing.T) {
	// `&` must be escaped before `<` and `>` so we don't double-encode.
	got := entityEscape("a & <b> & c")
	want := "a &amp; &lt;b&gt; &amp; c"
	if got != want {
		t.Errorf("entityEscape = %q, want %q", got, want)
	}
}

func TestEntityEscape_NoOpWhenNoSpecials(t *testing.T) {
	in := "plain text with no special chars"
	if got := entityEscape(in); got != in {
		t.Errorf("entityEscape = %q, want %q (no-op)", got, in)
	}
}

// --- ModeAuto: picker -------------------------------------------------------

func TestAutoMode_ShortPlainProse_PicksMarkdownBlock(t *testing.T) {
	r, err := New(Options{Mode: ModeAuto})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("Just some short prose.")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	if _, ok := blocks[0].(*slack.MarkdownBlock); !ok {
		t.Errorf("auto picker chose %T for short prose, want *MarkdownBlock", blocks[0])
	}
}

func TestAutoMode_InputWithImage_FallsThroughToDecomposition(t *testing.T) {
	r, err := New(Options{Mode: ModeAuto})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("# Title\n\n![alt](https://example.com/x.png)\n")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	for _, b := range blocks {
		if _, ok := b.(*slack.MarkdownBlock); ok {
			t.Errorf("auto picker chose markdown_block when input contains an image")
		}
	}
	// Should have header and image as separate blocks (decomposition path).
	var hasHeader, hasImage bool
	for _, b := range blocks {
		if _, ok := b.(*slack.HeaderBlock); ok {
			hasHeader = true
		}
		if _, ok := b.(*slack.ImageBlock); ok {
			hasImage = true
		}
	}
	if !hasHeader || !hasImage {
		t.Errorf("expected header + image blocks, got %d blocks", len(blocks))
	}
}

func TestAutoMode_VeryLongInput_FallsThroughToDecomposition(t *testing.T) {
	r, err := New(Options{Mode: ModeAuto, MaxInputBytes: MaxMarkdownBlockSum * 2})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	long := strings.Repeat("paragraph text. ", 1000) // ~16k chars > 12k
	blocks, err := r.Convert(long)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	for _, b := range blocks {
		if _, ok := b.(*slack.MarkdownBlock); ok {
			t.Errorf("auto picker chose markdown_block for >12k input")
		}
	}
}

func TestAutoMode_LargeTable_FallsThroughToDecomposition(t *testing.T) {
	// A table with > maxTableRows data rows triggers the
	// "needs TableBlock chunking" branch in the picker.
	// Use DefaultOptions to ensure EnableTables=true (the GFM extension
	// only attaches to the goldmark parser when EnableTables is set, and
	// the zero-value bool would silently disable it).
	var sb strings.Builder
	sb.WriteString("| col |\n|---|\n")
	for i := 0; i <= maxTableRows; i++ {
		sb.WriteString("| row |\n")
	}
	opts := DefaultOptions()
	opts.Mode = ModeAuto
	r, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert(sb.String())
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	for _, b := range blocks {
		if _, ok := b.(*slack.MarkdownBlock); ok {
			t.Errorf("auto picker chose markdown_block for table over row limit")
		}
	}
	// Should have got TableBlocks instead.
	var tableCount int
	for _, b := range blocks {
		if _, ok := b.(*slack.TableBlock); ok {
			tableCount++
		}
	}
	if tableCount == 0 {
		t.Errorf("expected TableBlocks via decomposition path")
	}
}

func TestAutoMode_SmallTable_PicksMarkdownBlock(t *testing.T) {
	// A small table fits inside the markdown block budget; auto picker
	// should still choose markdown_block (Slack renders the table inside
	// the markdown block client-side). Use DefaultOptions so EnableTables
	// is on and the picker actually sees a Table AST node.
	opts := DefaultOptions()
	opts.Mode = ModeAuto
	r, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("| a | b |\n|---|---|\n| 1 | 2 |\n")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	if _, ok := blocks[0].(*slack.MarkdownBlock); !ok {
		t.Errorf("auto picker chose %T for small table, want MarkdownBlock", blocks[0])
	}
}

// --- ModeRichText: opts out of the picker ----------------------------------

func TestRichTextMode_AlwaysDecomposesEvenWhenAutoWouldPickMarkdown(t *testing.T) {
	r, err := New(Options{Mode: ModeRichText})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("# Title\n\nshort body.")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	for _, b := range blocks {
		if _, ok := b.(*slack.MarkdownBlock); ok {
			t.Errorf("rich_text mode emitted MarkdownBlock; should always decompose")
		}
	}
	// Two blocks expected: header + paragraph.
	if len(blocks) != 2 {
		t.Errorf("got %d blocks, want 2 (header + paragraph)", len(blocks))
	}
}
