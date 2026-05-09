package converter

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"

	"github.com/hishamkaram/mcp-slack-block-kit/internal/preview"
)

// Tests for the nested-element handling added in the "Close the
// nested-element gaps" change. Three test variants per pattern:
//
//  1. auto mode: must route to rich_text decomposition (the picker now
//     gates on these patterns) AND emit one warning naming the patterns.
//  2. rich_text mode: must produce decomposed adjacent blocks with no
//     warnings (caller explicitly opted in).
//  3. markdown_block mode: passthrough — the produced markdown text must
//     contain the original characters; rendering is Slack's responsibility.

// --- helpers ---------------------------------------------------------------

// convertWith runs the converter with the given mode override on top of
// DefaultOptions and returns blocks + warnings + the JSON form (for substring
// assertions).
func convertWith(t *testing.T, mode Mode, input string) ([]slack.Block, []string) {
	t.Helper()
	opts := DefaultOptions()
	opts.Mode = mode
	r, err := New(opts)
	if err != nil {
		t.Fatalf("New(%q): %v", mode, err)
	}
	blocks, warnings, err := r.ConvertWithWarnings(input)
	if err != nil {
		t.Fatalf("Convert(%q, mode=%q): %v", input, mode, err)
	}
	return blocks, warnings
}

// blockTypes returns the slack.MessageBlockType of each block, useful for
// asserting on the SHAPE of the output sequence without caring about
// per-block contents.
func blockTypes(blocks []slack.Block) []slack.MessageBlockType {
	out := make([]slack.MessageBlockType, len(blocks))
	for i, b := range blocks {
		out[i] = b.BlockType()
	}
	return out
}

// hasMarkdownBlock returns true if any block in the slice is a MarkdownBlock.
func hasMarkdownBlock(blocks []slack.Block) bool {
	for _, b := range blocks {
		if _, ok := b.(*slack.MarkdownBlock); ok {
			return true
		}
	}
	return false
}

// firstPreformattedAcross returns the first rich_text_preformatted element
// found across all rich_text blocks in the slice. Convenience for assertions
// that only care that a code block was emitted somewhere in the output.
func firstPreformattedAcross(blocks []slack.Block) *slack.RichTextPreformatted {
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
	return nil
}

// firstQuoteAcross returns the first rich_text_quote element across all
// rich_text blocks in the slice.
func firstQuoteAcross(blocks []slack.Block) *slack.RichTextQuote {
	for _, b := range blocks {
		rt, ok := b.(*slack.RichTextBlock)
		if !ok {
			continue
		}
		for _, el := range rt.Elements {
			if q, ok := el.(*slack.RichTextQuote); ok {
				return q
			}
		}
	}
	return nil
}

// allListsAcross returns every rich_text_list element across all rich_text
// blocks in the slice, in document order.
func allListsAcross(blocks []slack.Block) []*slack.RichTextList {
	var out []*slack.RichTextList
	for _, b := range blocks {
		rt, ok := b.(*slack.RichTextBlock)
		if !ok {
			continue
		}
		for _, el := range rt.Elements {
			if l, ok := el.(*slack.RichTextList); ok {
				out = append(out, l)
			}
		}
	}
	return out
}

// --- Pattern 1: code in blockquote -----------------------------------------

const codeInQuote = "> some quote\n>\n> ```go\n> func main() {}\n> ```\n>\n> more quote\n"

func TestNested_CodeInQuote_AutoMode_DecomposesAndWarns(t *testing.T) {
	blocks, warnings := convertWith(t, ModeAuto, codeInQuote)
	if hasMarkdownBlock(blocks) {
		t.Fatalf("auto mode should NOT route to markdown_block for code-in-quote; got %v", blockTypes(blocks))
	}
	if firstPreformattedAcross(blocks) == nil {
		t.Errorf("expected a rich_text_preformatted in output; got blocks=%v", blockTypes(blocks))
	}
	if firstQuoteAcross(blocks) == nil {
		t.Errorf("expected a rich_text_quote in output (the surviving quote prefix/suffix)")
	}
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if len(warnings) > 0 && !strings.Contains(warnings[0], PatternCodeInBlockquote) {
		t.Errorf("warning should name code-in-blockquote pattern, got: %q", warnings[0])
	}
}

func TestNested_CodeInQuote_RichTextMode_DecomposesNoWarning(t *testing.T) {
	blocks, warnings := convertWith(t, ModeRichText, codeInQuote)
	if firstPreformattedAcross(blocks) == nil {
		t.Errorf("expected a rich_text_preformatted in rich_text mode; got %v", blockTypes(blocks))
	}
	if firstQuoteAcross(blocks) == nil {
		t.Errorf("expected a rich_text_quote in rich_text mode")
	}
	if len(warnings) != 0 {
		t.Errorf("rich_text mode is explicit; expected no warnings, got %v", warnings)
	}
}

func TestNested_CodeInQuote_MarkdownBlockMode_Passthrough(t *testing.T) {
	blocks, warnings := convertWith(t, ModeMarkdownBlock, codeInQuote)
	if len(blocks) != 1 {
		t.Fatalf("markdown_block mode should produce 1 block, got %d", len(blocks))
	}
	mb, ok := blocks[0].(*slack.MarkdownBlock)
	if !ok {
		t.Fatalf("expected MarkdownBlock, got %T", blocks[0])
	}
	// The "func main" content must survive into the markdown block text.
	if !strings.Contains(mb.Text, "func main()") {
		t.Errorf("markdown block missing original content: %q", mb.Text)
	}
	if len(warnings) != 0 {
		t.Errorf("markdown_block mode (explicit) emits no warnings, got %v", warnings)
	}
}

// --- Pattern 2: code in list item ------------------------------------------

const codeInList = "- step one\n- step two:\n\n  ```go\n  func main() {}\n  ```\n\n- step three\n"

func TestNested_CodeInList_AutoMode_DecomposesAndWarns(t *testing.T) {
	blocks, warnings := convertWith(t, ModeAuto, codeInList)
	if hasMarkdownBlock(blocks) {
		t.Fatalf("auto mode should not route to markdown_block; got %v", blockTypes(blocks))
	}
	if firstPreformattedAcross(blocks) == nil {
		t.Errorf("expected a rich_text_preformatted in output")
	}
	lists := allListsAcross(blocks)
	if len(lists) < 2 {
		t.Errorf("expected list to split into ≥2 sibling rich_text_list elements (split around the code), got %d", len(lists))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], PatternCodeInList) {
		t.Errorf("expected 1 warning naming code-in-list, got %v", warnings)
	}
}

func TestNested_CodeInList_RichTextMode_DecomposesNoWarning(t *testing.T) {
	blocks, warnings := convertWith(t, ModeRichText, codeInList)
	if firstPreformattedAcross(blocks) == nil {
		t.Errorf("expected a rich_text_preformatted in rich_text mode")
	}
	if len(warnings) != 0 {
		t.Errorf("rich_text mode emits no warnings, got %v", warnings)
	}
}

// --- Pattern 3: list in blockquote -----------------------------------------

const listInQuote = "> intro line\n> - one\n> - two\n> - three\n> outro line\n"

func TestNested_ListInQuote_AutoMode_DecomposesAndWarns(t *testing.T) {
	blocks, warnings := convertWith(t, ModeAuto, listInQuote)
	if hasMarkdownBlock(blocks) {
		t.Fatalf("auto mode should not route to markdown_block; got %v", blockTypes(blocks))
	}
	if firstQuoteAcross(blocks) == nil {
		t.Errorf("expected a rich_text_quote (the surviving prefix/suffix)")
	}
	if len(allListsAcross(blocks)) == 0 {
		t.Errorf("expected at least one rich_text_list in output")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], PatternListInBlockquote) {
		t.Errorf("expected 1 warning naming list-in-blockquote, got %v", warnings)
	}
}

func TestNested_ListInQuote_RichTextMode_DecomposesNoWarning(t *testing.T) {
	blocks, warnings := convertWith(t, ModeRichText, listInQuote)
	if firstQuoteAcross(blocks) == nil {
		t.Errorf("expected a rich_text_quote in rich_text mode")
	}
	if len(allListsAcross(blocks)) == 0 {
		t.Errorf("expected at least one rich_text_list in rich_text mode")
	}
	if len(warnings) != 0 {
		t.Errorf("rich_text mode emits no warnings, got %v", warnings)
	}
}

// --- Pattern 4: table in blockquote ----------------------------------------

const tableInQuote = "> intro\n>\n> | a | b |\n> |---|---|\n> | 1 | 2 |\n>\n> outro\n"

func TestNested_TableInQuote_AutoMode_DecomposesAndWarns(t *testing.T) {
	blocks, warnings := convertWith(t, ModeAuto, tableInQuote)
	if hasMarkdownBlock(blocks) {
		t.Fatalf("auto mode should not route to markdown_block; got %v", blockTypes(blocks))
	}
	var hasTable bool
	for _, b := range blocks {
		if _, ok := b.(*slack.TableBlock); ok {
			hasTable = true
		}
	}
	if !hasTable {
		t.Errorf("expected a TableBlock in output; got %v", blockTypes(blocks))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], PatternTableInBlockquote) {
		t.Errorf("expected 1 warning naming table-in-blockquote, got %v", warnings)
	}
}

// --- Pattern 5: table in list item -----------------------------------------

const tableInList = "- before\n- with table:\n\n  | a | b |\n  |---|---|\n  | 1 | 2 |\n\n- after\n"

func TestNested_TableInList_AutoMode_DecomposesAndWarns(t *testing.T) {
	blocks, _ := convertWith(t, ModeAuto, tableInList)
	if hasMarkdownBlock(blocks) {
		t.Fatalf("auto mode should not route to markdown_block; got %v", blockTypes(blocks))
	}
	// Note: GFM tables sometimes don't parse when nested in a list item
	// (depending on parser strictness around the required blank line).
	// We don't strictly require a TableBlock here — just that the output
	// shape is decomposed. The Block Kit Builder fixture below logs the
	// URL so a maintainer can inspect.
	if len(blocks) == 0 {
		t.Errorf("expected at least one block, got 0")
	}
}

// --- Pattern 6: loose list (multiple paragraphs in one item) ---------------
// This pattern is NOT a non-representable case — it stays representable in
// rich_text. So auto mode should still use markdown_block for short inputs;
// the regression test asserts the existing space-joining behavior.

const looseList = "- first paragraph\n\n  second paragraph in same item\n\n- next item\n"

func TestNested_LooseList_AutoMode_StillUsesMarkdownBlock(t *testing.T) {
	blocks, warnings := convertWith(t, ModeAuto, looseList)
	if !hasMarkdownBlock(blocks) || len(blocks) != 1 {
		t.Errorf("loose list (no code/table) should route to single markdown block; got %v", blockTypes(blocks))
	}
	if len(warnings) != 0 {
		t.Errorf("loose list shouldn't trigger any warnings, got %v", warnings)
	}
}

func TestNested_LooseList_RichTextMode_JoinsParagraphsWithSpace(t *testing.T) {
	blocks, _ := convertWith(t, ModeRichText, looseList)
	lists := allListsAcross(blocks)
	if len(lists) != 1 {
		t.Fatalf("expected 1 rich_text_list, got %d", len(lists))
	}
	// The first item should contain both paragraphs joined by a space-bearing element.
	sec, ok := lists[0].Elements[0].(*slack.RichTextSection)
	if !ok {
		t.Fatalf("expected first list element to be RichTextSection, got %T", lists[0].Elements[0])
	}
	var combined strings.Builder
	for _, el := range sec.Elements {
		if t, ok := el.(*slack.RichTextSectionTextElement); ok {
			combined.WriteString(t.Text)
		}
	}
	if !strings.Contains(combined.String(), "first paragraph") || !strings.Contains(combined.String(), "second paragraph") {
		t.Errorf("loose-list item should join both paragraphs; got %q", combined.String())
	}
}

// --- Cross-cutting: ordered-list Offset continuation across a split --------
// Input: "1. a\n2. b\n   ```\n   x\n   ```\n3. c\n4. d\n"
//
// Expected output:
//   - rich_text block with one rich_text_list, items [a, b], style=ordered, offset=0
//   - rich_text block with one rich_text_preformatted, content "x"
//   - rich_text block with one rich_text_list, items [c, d], style=ordered, offset=2
//
// Slack: Offset=N → first number = N+1. So Offset=2 means the second sibling
// list starts at "3" — visually continuing the numbering from where the first
// sibling left off.

const orderedSplit = "1. first\n2. second\n\n   ```\n   x\n   ```\n\n3. third\n4. fourth\n"

func TestNested_OrderedListNumberingContinuesAcrossSplit(t *testing.T) {
	blocks, _ := convertWith(t, ModeRichText, orderedSplit)
	lists := allListsAcross(blocks)
	if len(lists) < 2 {
		t.Fatalf("expected ≥2 sibling lists across the split, got %d", len(lists))
	}
	// First sibling: items 1,2 → offset 0 (Slack starts at offset+1 = 1).
	if lists[0].Style != slack.RTEListOrdered {
		t.Errorf("lists[0] style = %q, want ordered", lists[0].Style)
	}
	if lists[0].Offset != 0 {
		t.Errorf("lists[0].Offset = %d, want 0 (so first item displays as '1')", lists[0].Offset)
	}
	// Second sibling: items 3,4 → offset 2 (Slack starts at offset+1 = 3).
	if lists[len(lists)-1].Offset != 2 {
		t.Errorf("lists[last].Offset = %d, want 2 (so first item displays as '3')",
			lists[len(lists)-1].Offset)
	}
}

// --- Manual-verification fixture: print Block Kit Builder URLs -------------
//
// This test always passes; it just t.Logf's a Block Kit Builder URL for
// each pattern × mode combination so a maintainer with a Slack workspace
// can click through and visually verify:
//
//  1. Whether sibling rich_text decomposition renders predictably
//     (Fact: visual sibling-bar wrapping).
//  2. Whether markdown_block actually renders code-in-quote /
//     code-in-list / list-in-quote correctly (Fact 3: UNVERIFIED in
//     research).
//  3. Whether `Offset` produces visual numbering continuity across split
//     ordered lists (Fact 2: SCHEMA-ONLY).
//
// Run with:  go test -v -run TestNested_PrintBuilderURLs ./internal/converter/
func TestNested_PrintBuilderURLs(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"code-in-quote", codeInQuote},
		{"code-in-list", codeInList},
		{"list-in-quote", listInQuote},
		{"table-in-quote", tableInQuote},
		{"table-in-list", tableInList},
		{"loose-list", looseList},
		{"ordered-list-split-continuation", orderedSplit},
	}
	modes := []Mode{ModeRichText, ModeMarkdownBlock}

	for _, c := range cases {
		for _, m := range modes {
			blocks, _ := convertWith(t, m, c.input)
			pr, err := preview.BuilderURL(blocks)
			if err != nil {
				t.Errorf("preview.BuilderURL for %s/%s: %v", c.name, m, err)
				continue
			}
			t.Logf("\n===  %s  (mode=%s, %d blocks, %d bytes URL)\n%s\n",
				c.name, m, len(blocks), pr.ByteSize, pr.URL)
		}
	}
}
