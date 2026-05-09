package converter

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// emojiNamesIn returns the names of every emoji element in the first
// rich_text_section of a converted output, in order.
func emojiNamesIn(t *testing.T, blocks []slack.Block) []string {
	t.Helper()
	sec := firstSection(t, blocks)
	var out []string
	for _, el := range sec.Elements {
		if e, ok := el.(*slack.RichTextSectionEmojiElement); ok {
			out = append(out, e.Name)
		}
	}
	return out
}

// textRunsIn returns the text content of every text element in the first
// rich_text_section.
func textRunsIn(t *testing.T, blocks []slack.Block) []string {
	t.Helper()
	sec := firstSection(t, blocks)
	var out []string
	for _, el := range sec.Elements {
		if e, ok := el.(*slack.RichTextSectionTextElement); ok {
			out = append(out, e.Text)
		}
	}
	return out
}

// --- Basic emoji extraction --------------------------------------------------

func TestEmoji_SimpleShortcode_Extracted(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "Hello :wave:")
	names := emojiNamesIn(t, blocks)
	if len(names) != 1 || names[0] != "wave" {
		t.Errorf("emoji names = %v, want [wave]", names)
	}
}

func TestEmoji_ThumbsUpPlusOne_Extracted(t *testing.T) {
	// `:+1:` is a Slack standard for thumbs-up.
	blocks, _ := renderForTest(t, Options{}, "nice :+1:")
	names := emojiNamesIn(t, blocks)
	if len(names) != 1 || names[0] != "+1" {
		t.Errorf("emoji names = %v, want [+1]", names)
	}
}

func TestEmoji_FragmentedByUnderscore_StillExtracted(t *testing.T) {
	// goldmark splits text at `_` (potential emphasis); the merge step
	// in resolveEmoji is what makes this work end-to-end.
	blocks, _ := renderForTest(t, Options{}, "look :bar_chart: stats")
	names := emojiNamesIn(t, blocks)
	if len(names) != 1 || names[0] != "bar_chart" {
		t.Errorf("emoji names = %v, want [bar_chart]", names)
	}
	// Verify the surrounding text is still intact.
	runs := textRunsIn(t, blocks)
	joined := strings.Join(runs, "")
	if !strings.Contains(joined, "look ") || !strings.Contains(joined, " stats") {
		t.Errorf("surrounding text fragmented; runs = %v", runs)
	}
}

func TestEmoji_MultipleEmojiInOneText_AllExtracted(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, ":fire: hot :tada: party :wave:")
	names := emojiNamesIn(t, blocks)
	want := []string{"fire", "tada", "wave"}
	if !equalStrings(names, want) {
		t.Errorf("emoji names = %v, want %v", names, want)
	}
}

func TestEmoji_AdjacentEmojis_BothExtracted(t *testing.T) {
	// `:wave::skin-tone-2:` — Slack composes these visually; we emit two
	// separate emoji elements.
	blocks, _ := renderForTest(t, Options{}, "hi :wave::skin-tone-2: there")
	names := emojiNamesIn(t, blocks)
	if !equalStrings(names, []string{"wave", "skin-tone-2"}) {
		t.Errorf("emoji names = %v, want [wave skin-tone-2]", names)
	}
}

// --- False-positive filtering -----------------------------------------------

func TestEmoji_TimeString_NotExtracted(t *testing.T) {
	// `19:49:41` would naively match `:49:` — the non-digit boundary check
	// is what saves us. No emoji element should be emitted.
	blocks, _ := renderForTest(t, Options{}, "the build started at 19:49:41 yesterday")
	if names := emojiNamesIn(t, blocks); len(names) != 0 {
		t.Errorf("time string produced spurious emoji elements: %v", names)
	}
}

func TestEmoji_MidWordColon_NotExtracted(t *testing.T) {
	// `URL:port` etc. should not produce emoji.
	blocks, _ := renderForTest(t, Options{}, "see http://example.com:8080/path for more")
	if names := emojiNamesIn(t, blocks); len(names) != 0 {
		t.Errorf("URL with port produced spurious emoji elements: %v", names)
	}
}

func TestEmoji_DigitOnlyName_NotExtracted(t *testing.T) {
	// `:42:` starts with a digit; rejected by isEmojiNameStartByte.
	// Also bordered-by-letter so the boundary check alone wouldn't catch.
	blocks, _ := renderForTest(t, Options{}, "answer :42: maybe")
	if names := emojiNamesIn(t, blocks); len(names) != 0 {
		t.Errorf("digit-only name produced spurious emoji: %v", names)
	}
}

func TestEmoji_UnclosedColon_NotExtracted(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, ":wave just a colon")
	if names := emojiNamesIn(t, blocks); len(names) != 0 {
		t.Errorf("unclosed colon produced emoji elements: %v", names)
	}
}

func TestEmoji_EmptyBetweenColons_NotExtracted(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "two :: colons")
	if names := emojiNamesIn(t, blocks); len(names) != 0 {
		t.Errorf("`::` produced emoji elements: %v", names)
	}
}

// --- Style preservation -----------------------------------------------------

func TestEmoji_InsideBold_PreservesSurroundingStyle(t *testing.T) {
	// `**hot :fire: stuff**` — the bold style applies to the prefix and
	// suffix text runs around the emoji, but the emoji itself has no style.
	blocks, _ := renderForTest(t, Options{}, "**hot :fire: stuff**")
	sec := firstSection(t, blocks)

	var prefix, suffix *slack.RichTextSectionTextElement
	var emoji *slack.RichTextSectionEmojiElement
	for _, el := range sec.Elements {
		switch e := el.(type) {
		case *slack.RichTextSectionTextElement:
			if strings.Contains(e.Text, "hot") {
				prefix = e
			}
			if strings.Contains(e.Text, "stuff") {
				suffix = e
			}
		case *slack.RichTextSectionEmojiElement:
			emoji = e
		}
	}
	if emoji == nil || emoji.Name != "fire" {
		t.Fatalf("expected fire emoji, got %+v", emoji)
	}
	if prefix == nil || prefix.Style == nil || !prefix.Style.Bold {
		t.Errorf("prefix style = %+v, want bold", prefix)
	}
	if suffix == nil || suffix.Style == nil || !suffix.Style.Bold {
		t.Errorf("suffix style = %+v, want bold", suffix)
	}
}

// --- Merge correctness ------------------------------------------------------

func TestMergeSameStyleText_MergesAdjacent(t *testing.T) {
	in := []slack.RichTextSectionElement{
		slack.NewRichTextSectionTextElement("a", nil),
		slack.NewRichTextSectionTextElement("b", nil),
		slack.NewRichTextSectionTextElement("c", nil),
	}
	out := mergeSameStyleText(in)
	if len(out) != 1 {
		t.Fatalf("got %d elements, want 1 merged", len(out))
	}
	if out[0].(*slack.RichTextSectionTextElement).Text != "abc" {
		t.Errorf("merged text = %q, want %q",
			out[0].(*slack.RichTextSectionTextElement).Text, "abc")
	}
}

func TestMergeSameStyleText_DoesNotMergeAcrossDifferentStyle(t *testing.T) {
	bold := &slack.RichTextSectionTextStyle{Bold: true}
	in := []slack.RichTextSectionElement{
		slack.NewRichTextSectionTextElement("plain ", nil),
		slack.NewRichTextSectionTextElement("bold", bold),
		slack.NewRichTextSectionTextElement(" plain", nil),
	}
	out := mergeSameStyleText(in)
	if len(out) != 3 {
		t.Errorf("got %d elements, want 3 (no merge across styles)", len(out))
	}
}

func TestMergeSameStyleText_DoesNotMergeAcrossNonTextElement(t *testing.T) {
	in := []slack.RichTextSectionElement{
		slack.NewRichTextSectionTextElement("a", nil),
		slack.NewRichTextSectionLinkElement("https://x.com", "x", nil),
		slack.NewRichTextSectionTextElement("b", nil),
	}
	out := mergeSameStyleText(in)
	if len(out) != 3 {
		t.Errorf("got %d elements, want 3 (link separates text)", len(out))
	}
}

func TestStyleEqual(t *testing.T) {
	bold := &slack.RichTextSectionTextStyle{Bold: true}
	bold2 := &slack.RichTextSectionTextStyle{Bold: true}
	italic := &slack.RichTextSectionTextStyle{Italic: true}

	if !styleEqual(nil, nil) {
		t.Error("styleEqual(nil, nil) should be true")
	}
	if styleEqual(nil, bold) {
		t.Error("styleEqual(nil, bold) should be false")
	}
	if styleEqual(bold, nil) {
		t.Error("styleEqual(bold, nil) should be false")
	}
	if !styleEqual(bold, bold2) {
		t.Error("styleEqual(bold, bold2) should be true (same fields)")
	}
	if styleEqual(bold, italic) {
		t.Error("styleEqual(bold, italic) should be false")
	}
}
