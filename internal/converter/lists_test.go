package converter

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// extractLists pulls every rich_text_list element from the converted output
// in document order. Tests use this to make per-list assertions without
// caring about the surrounding RichTextBlock wrapping.
func extractLists(blocks []slack.Block) []*slack.RichTextList {
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

// listItemTexts returns the concatenated text of every rich_text_section in a
// rich_text_list, one per list item, in order. Convenient assertion shape.
func listItemTexts(l *slack.RichTextList) []string {
	var out []string
	for _, el := range l.Elements {
		sec, ok := el.(*slack.RichTextSection)
		if !ok {
			out = append(out, "")
			continue
		}
		var sb strings.Builder
		for _, inner := range sec.Elements {
			if text, ok := inner.(*slack.RichTextSectionTextElement); ok {
				sb.WriteString(text.Text)
			}
		}
		out = append(out, sb.String())
	}
	return out
}

// --- Flat lists --------------------------------------------------------------

func TestHandleList_FlatBullet_EmitsOneList(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "- one\n- two\n- three\n")
	lists := extractLists(blocks)
	if len(lists) != 1 {
		t.Fatalf("got %d lists, want 1", len(lists))
	}
	l := lists[0]
	if l.Style != slack.RTEListBullet {
		t.Errorf("style = %q, want %q", l.Style, slack.RTEListBullet)
	}
	if l.Indent != 0 {
		t.Errorf("indent = %d, want 0", l.Indent)
	}
	got := listItemTexts(l)
	want := []string{"one", "two", "three"}
	if !equalStrings(got, want) {
		t.Errorf("items = %v, want %v", got, want)
	}
}

func TestHandleList_FlatOrdered_EmitsOrderedList(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "1. alpha\n2. beta\n3. gamma\n")
	lists := extractLists(blocks)
	if len(lists) != 1 {
		t.Fatalf("got %d lists, want 1", len(lists))
	}
	if lists[0].Style != slack.RTEListOrdered {
		t.Errorf("style = %q, want %q", lists[0].Style, slack.RTEListOrdered)
	}
	if lists[0].Offset != 0 {
		t.Errorf("default-start ordered list got offset %d, want 0", lists[0].Offset)
	}
	if !equalStrings(listItemTexts(lists[0]), []string{"alpha", "beta", "gamma"}) {
		t.Errorf("items = %v", listItemTexts(lists[0]))
	}
}

func TestHandleList_OrderedWithCustomStart_SetsOffset(t *testing.T) {
	// "5." should map to offset=4 so Slack renders 5/6/7.
	blocks, _ := renderForTest(t, Options{}, "5. fifth\n6. sixth\n")
	lists := extractLists(blocks)
	if len(lists) != 1 {
		t.Fatalf("got %d lists, want 1", len(lists))
	}
	if lists[0].Offset != 4 {
		t.Errorf("offset = %d, want 4 (Start=5 → offset=4)", lists[0].Offset)
	}
}

func TestHandleList_PlusAndAsteriskMarkers_BothBullet(t *testing.T) {
	for _, src := range []string{"+ a\n+ b\n", "* a\n* b\n"} {
		t.Run(src, func(t *testing.T) {
			lists := extractLists(mustConvert(t, src))
			if len(lists) != 1 {
				t.Fatalf("got %d lists for %q", len(lists), src)
			}
			if lists[0].Style != slack.RTEListBullet {
				t.Errorf("marker %q produced style %q, want bullet", src[0], lists[0].Style)
			}
		})
	}
}

// --- Nested lists ------------------------------------------------------------

func TestHandleList_NestedTwoLevels_EmitsSiblings(t *testing.T) {
	input := "- outer 1\n- outer 2\n  - nested a\n  - nested b\n- outer 3\n"
	blocks, _ := renderForTest(t, Options{}, input)
	lists := extractLists(blocks)
	if len(lists) != 3 {
		t.Fatalf("got %d sibling lists, want 3 (outer-prefix, nested, outer-suffix)", len(lists))
	}

	// First sibling: outer items above the nested list, indent=0.
	if lists[0].Indent != 0 || lists[0].Style != slack.RTEListBullet {
		t.Errorf("lists[0] indent=%d style=%q, want indent=0 style=bullet",
			lists[0].Indent, lists[0].Style)
	}
	if !equalStrings(listItemTexts(lists[0]), []string{"outer 1", "outer 2"}) {
		t.Errorf("lists[0] items = %v", listItemTexts(lists[0]))
	}

	// Second sibling: the nested list, indent=1.
	if lists[1].Indent != 1 {
		t.Errorf("lists[1] indent = %d, want 1", lists[1].Indent)
	}
	if !equalStrings(listItemTexts(lists[1]), []string{"nested a", "nested b"}) {
		t.Errorf("lists[1] items = %v", listItemTexts(lists[1]))
	}

	// Third sibling: remaining outer items after the nested list, back to indent=0.
	if lists[2].Indent != 0 {
		t.Errorf("lists[2] indent = %d, want 0", lists[2].Indent)
	}
	if !equalStrings(listItemTexts(lists[2]), []string{"outer 3"}) {
		t.Errorf("lists[2] items = %v", listItemTexts(lists[2]))
	}
}

func TestHandleList_NestedThreeLevels_EmitsEscalatingIndents(t *testing.T) {
	input := "- L1\n  - L2\n    - L3\n  - back to L2\n- back to L1\n"
	blocks, _ := renderForTest(t, Options{}, input)
	lists := extractLists(blocks)
	// Expected sibling sequence (one rich_text_list per indent transition):
	//   L1 (indent 0), L2 (indent 1), L3 (indent 2), L2 (indent 1), L1 (indent 0)
	wantIndents := []int{0, 1, 2, 1, 0}
	if len(lists) != len(wantIndents) {
		t.Fatalf("got %d sibling lists, want %d (indents %v)",
			len(lists), len(wantIndents), wantIndents)
	}
	for i, l := range lists {
		if l.Indent != wantIndents[i] {
			t.Errorf("lists[%d].Indent = %d, want %d", i, l.Indent, wantIndents[i])
		}
	}
}

func TestHandleList_NestedOrderedUnderBullet_StyleSwitches(t *testing.T) {
	input := "- bullet outer\n  1. ordered inner\n  2. second inner\n- bullet outer 2\n"
	blocks, _ := renderForTest(t, Options{}, input)
	lists := extractLists(blocks)
	if len(lists) != 3 {
		t.Fatalf("got %d sibling lists, want 3", len(lists))
	}
	if lists[0].Style != slack.RTEListBullet {
		t.Errorf("outer style = %q, want bullet", lists[0].Style)
	}
	if lists[1].Style != slack.RTEListOrdered {
		t.Errorf("nested style = %q, want ordered", lists[1].Style)
	}
	if lists[1].Indent != 1 {
		t.Errorf("nested indent = %d, want 1", lists[1].Indent)
	}
}

// --- Task lists --------------------------------------------------------------

func TestHandleList_TaskList_RendersCheckboxesLiterally(t *testing.T) {
	input := "- [x] done\n- [ ] todo\n- [X] also done\n"
	blocks, _ := renderForTest(t, Options{}, input)
	lists := extractLists(blocks)
	if len(lists) != 1 {
		t.Fatalf("got %d lists, want 1", len(lists))
	}
	got := listItemTexts(lists[0])
	want := []string{"[x] done", "[ ] todo", "[x] also done"}
	if !equalStrings(got, want) {
		t.Errorf("items = %v, want %v", got, want)
	}
}

func TestHandleList_TaskList_NestedUnderBullet_PreservesCheckbox(t *testing.T) {
	input := "- parent\n  - [x] child done\n  - [ ] child todo\n"
	blocks, _ := renderForTest(t, Options{}, input)
	lists := extractLists(blocks)
	if len(lists) != 2 {
		t.Fatalf("got %d sibling lists, want 2", len(lists))
	}
	if !equalStrings(listItemTexts(lists[1]), []string{"[x] child done", "[ ] child todo"}) {
		t.Errorf("nested task items = %v", listItemTexts(lists[1]))
	}
}

// --- Edge cases --------------------------------------------------------------

func TestHandleList_EmptyList_EmitsNoBlock(t *testing.T) {
	// Pathological: a list marker followed by no item content. Goldmark may
	// produce an empty ListItem; we should drop the whole block rather than
	// emit an empty rich_text.
	blocks, _ := renderForTest(t, Options{}, "-\n")
	for _, b := range blocks {
		if rt, ok := b.(*slack.RichTextBlock); ok {
			lists := extractLists([]slack.Block{rt})
			for _, l := range lists {
				if len(l.Elements) == 0 {
					t.Error("emitted an empty rich_text_list")
				}
			}
		}
	}
}

func TestHandleList_FollowedByParagraph_BothEmitted(t *testing.T) {
	input := "- list item\n\nA paragraph after.\n"
	blocks, _ := renderForTest(t, Options{}, input)
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2 (list + paragraph)", len(blocks))
	}
	if _, ok := blocks[0].(*slack.RichTextBlock); !ok {
		t.Errorf("blocks[0] = %T, want RichTextBlock (list)", blocks[0])
	}
	if _, ok := blocks[1].(*slack.RichTextBlock); !ok {
		t.Errorf("blocks[1] = %T, want RichTextBlock (paragraph)", blocks[1])
	}
}

// --- Test helpers ------------------------------------------------------------

func mustConvert(t *testing.T, input string) []slack.Block {
	t.Helper()
	// Use ModeRichText so list-shape assertions work — auto mode would
	// route most short list inputs through the markdown_block path.
	opts := DefaultOptions()
	opts.Mode = ModeRichText
	r, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert(input)
	if err != nil {
		t.Fatalf("Convert(%q): %v", input, err)
	}
	return blocks
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
