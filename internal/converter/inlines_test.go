package converter

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// firstSection returns the first rich_text_section element from the first
// rich_text block. Test helpers built for inline assertions live here so the
// per-test code stays narrow.
func firstSection(t *testing.T, blocks []slack.Block) *slack.RichTextSection {
	t.Helper()
	for _, b := range blocks {
		rt, ok := b.(*slack.RichTextBlock)
		if !ok {
			continue
		}
		for _, el := range rt.Elements {
			if sec, ok := el.(*slack.RichTextSection); ok {
				return sec
			}
		}
	}
	t.Fatal("no rich_text_section found in output")
	return nil
}

// styleOf returns the merged style on the first text element matching the
// given substring, or nil if no such element exists.
func styleOf(sec *slack.RichTextSection, contains string) *slack.RichTextSectionTextStyle {
	for _, el := range sec.Elements {
		if t, ok := el.(*slack.RichTextSectionTextElement); ok && strings.Contains(t.Text, contains) {
			return t.Style
		}
	}
	return nil
}

// --- Bold / italic / strike --------------------------------------------------

func TestRenderInlines_Bold(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "**bold** plain")
	sec := firstSection(t, blocks)

	s := styleOf(sec, "bold")
	if s == nil || !s.Bold {
		t.Errorf("expected style.bold=true on 'bold' element, got %+v", s)
	}
	if s != nil && (s.Italic || s.Strike || s.Code) {
		t.Errorf("expected ONLY bold, got %+v", s)
	}

	plain := styleOf(sec, "plain")
	if plain != nil {
		t.Errorf("plain text element should have nil style, got %+v", plain)
	}
}

func TestRenderInlines_Italic(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "*italic*")
	s := styleOf(firstSection(t, blocks), "italic")
	if s == nil || !s.Italic {
		t.Errorf("expected style.italic=true, got %+v", s)
	}
	if s != nil && s.Bold {
		t.Errorf("italic should not also be bold, got %+v", s)
	}
}

func TestRenderInlines_BoldItalic_OrMerged(t *testing.T) {
	// `***x***` parses as Emphasis(level=2, [Emphasis(level=1, [Text("x")])])
	// — bold containing italic. The merged style must be {bold,italic}.
	blocks, _ := renderForTest(t, Options{}, "***bolditalic***")
	s := styleOf(firstSection(t, blocks), "bolditalic")
	if s == nil {
		t.Fatal("expected merged bold+italic style, got nil")
	}
	if !s.Bold || !s.Italic {
		t.Errorf("expected bold AND italic, got %+v", s)
	}
}

func TestRenderInlines_BoldUnderscoreItalic_OrMerged(t *testing.T) {
	// Mixed delimiters: `**bold _both_ bold**` — outer bold, inner italic.
	// "both" should carry both styles via OR-merge.
	blocks, _ := renderForTest(t, Options{}, "**bold _both_ bold**")
	sec := firstSection(t, blocks)
	if s := styleOf(sec, "both"); s == nil || !s.Bold || !s.Italic {
		t.Errorf("'both' style = %+v, want bold && italic", s)
	}
	if s := styleOf(sec, "bold "); s == nil || !s.Bold || s.Italic {
		t.Errorf("outer 'bold' style = %+v, want bold-only", s)
	}
}

func TestRenderInlines_Strikethrough(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "~~struck~~ ok")
	s := styleOf(firstSection(t, blocks), "struck")
	if s == nil || !s.Strike {
		t.Errorf("expected style.strike=true, got %+v", s)
	}
}

func TestRenderInlines_BoldStrike_OrMerged(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "**bold ~~and struck~~ done**")
	s := styleOf(firstSection(t, blocks), "and struck")
	if s == nil || !s.Bold || !s.Strike {
		t.Errorf("'and struck' style = %+v, want bold && strike", s)
	}
}

// --- Inline code ------------------------------------------------------------

func TestRenderInlines_InlineCode_Style(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "Use `Convert()` like so")
	s := styleOf(firstSection(t, blocks), "Convert()")
	if s == nil || !s.Code {
		t.Errorf("inline code should carry style.code=true, got %+v", s)
	}
}

func TestRenderInlines_InlineCodeInsideBold_KeepsBoth(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "**run `make test` first**")
	s := styleOf(firstSection(t, blocks), "make test")
	if s == nil || !s.Bold || !s.Code {
		t.Errorf("nested bold+code style = %+v, want both bold && code", s)
	}
}

// --- Links ------------------------------------------------------------------

func TestRenderInlines_InlineLink_EmitsLinkElement(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "See [the docs](https://example.com/docs).")
	sec := firstSection(t, blocks)
	var link *slack.RichTextSectionLinkElement
	for _, el := range sec.Elements {
		if l, ok := el.(*slack.RichTextSectionLinkElement); ok {
			link = l
			break
		}
	}
	if link == nil {
		t.Fatal("no rich_text_section link element emitted")
	}
	if link.URL != "https://example.com/docs" {
		t.Errorf("link URL = %q", link.URL)
	}
	if link.Text != "the docs" {
		t.Errorf("link text = %q, want %q", link.Text, "the docs")
	}
}

func TestRenderInlines_AutoLink_EmitsLinkWithoutText(t *testing.T) {
	// Linkify (part of GFM) auto-detects bare URLs, but explicit autolinks
	// using <...> are the canonical CommonMark form.
	blocks, _ := renderForTest(t, Options{}, "Visit <https://example.com>")
	sec := firstSection(t, blocks)
	var link *slack.RichTextSectionLinkElement
	for _, el := range sec.Elements {
		if l, ok := el.(*slack.RichTextSectionLinkElement); ok {
			link = l
		}
	}
	if link == nil {
		t.Fatal("no link element emitted for autolink")
	}
	if link.URL != "https://example.com" {
		t.Errorf("autolink URL = %q", link.URL)
	}
	if link.Text != "" {
		t.Errorf("autolink should have empty Text, got %q", link.Text)
	}
}

func TestRenderInlines_EmailAutoLink_PrefixesMailto(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "Mail <user@example.com> please")
	sec := firstSection(t, blocks)
	var link *slack.RichTextSectionLinkElement
	for _, el := range sec.Elements {
		if l, ok := el.(*slack.RichTextSectionLinkElement); ok {
			link = l
		}
	}
	if link == nil {
		t.Fatal("no link element for email autolink")
	}
	if link.URL != "mailto:user@example.com" {
		t.Errorf("email autolink URL = %q, want mailto: prefix", link.URL)
	}
}

func TestRenderInlines_LinkWithBoldStyle_AppliedToLinkElement(t *testing.T) {
	// `*[text](url)*` — italic wraps the link. The link element's style
	// should reflect the active outer style at emission time.
	blocks, _ := renderForTest(t, Options{}, "*[boldlink](https://example.com)*")
	sec := firstSection(t, blocks)
	var link *slack.RichTextSectionLinkElement
	for _, el := range sec.Elements {
		if l, ok := el.(*slack.RichTextSectionLinkElement); ok {
			link = l
		}
	}
	if link == nil {
		t.Fatal("no link element")
	}
	if link.Style == nil || !link.Style.Italic {
		t.Errorf("link.style should reflect outer italic, got %+v", link.Style)
	}
}

// --- Reference links --------------------------------------------------------

func TestRenderInlines_ReferenceLink_Resolved(t *testing.T) {
	input := "See [the docs][ref] for more.\n\n[ref]: https://example.com/r\n"
	blocks, _ := renderForTest(t, Options{}, input)
	sec := firstSection(t, blocks)
	var link *slack.RichTextSectionLinkElement
	for _, el := range sec.Elements {
		if l, ok := el.(*slack.RichTextSectionLinkElement); ok {
			link = l
		}
	}
	if link == nil {
		t.Fatal("reference link not emitted")
	}
	if link.URL != "https://example.com/r" {
		t.Errorf("reference link URL = %q, want resolved value", link.URL)
	}
	if link.Text != "the docs" {
		t.Errorf("reference link text = %q", link.Text)
	}
}

// --- Slack mrkdwn URL-form ---------------------------------------------------

// TestRenderInlines_SlackURLForm_BecomesLink confirms that the
// pre-parse rewriter in renderer.go turns Slack's `<URL|label>` mrkdwn
// extension into a CommonMark `[label](URL)` link, which goldmark then
// emits as a regular Link AST node, which the inline visitor emits as a
// rich_text_section_link. This is the rich_text counterpart of
// emitMarkdownBlockText's Slack-URL-form coverage.
func TestRenderInlines_SlackURLForm_BecomesLink(t *testing.T) {
	in := "<https://docs.google.com/spreadsheets/d/1m%7CfaCR/edit%7Cv3|Refa UGC v3 shared-drive>"
	blocks, _ := renderForTest(t, Options{}, in)
	sec := firstSection(t, blocks)
	var link *slack.RichTextSectionLinkElement
	for _, el := range sec.Elements {
		if l, ok := el.(*slack.RichTextSectionLinkElement); ok {
			link = l
		}
	}
	if link == nil {
		t.Fatalf("expected a link element, got elements: %#v", sec.Elements)
	}
	if link.URL != "https://docs.google.com/spreadsheets/d/1m%7CfaCR/edit%7Cv3" {
		t.Errorf("link URL = %q", link.URL)
	}
	if link.Text != "Refa UGC v3 shared-drive" {
		t.Errorf("link text = %q", link.Text)
	}
}

// --- Mixed -------------------------------------------------------------------

func TestRenderInlines_MixedStylesPlainText_AllElementsRoundTrip(t *testing.T) {
	input := "plain **bold** _italic_ ~~strike~~ `code` end."
	blocks, _ := renderForTest(t, Options{}, input)
	sec := firstSection(t, blocks)

	cases := []struct {
		text     string
		bold     bool
		italic   bool
		strike   bool
		code     bool
		nilStyle bool
	}{
		{"bold", true, false, false, false, false},
		{"italic", false, true, false, false, false},
		{"strike", false, false, true, false, false},
		{"code", false, false, false, true, false},
		{"plain ", false, false, false, false, true},
		{"end.", false, false, false, false, true},
	}
	for _, c := range cases {
		t.Run(strings.TrimSpace(c.text), func(t *testing.T) {
			s := styleOf(sec, c.text)
			if c.nilStyle {
				if s != nil {
					t.Errorf("expected nil style for %q, got %+v", c.text, s)
				}
				return
			}
			if s == nil {
				t.Fatalf("expected style for %q, got nil", c.text)
			}
			if s.Bold != c.bold || s.Italic != c.italic ||
				s.Strike != c.strike || s.Code != c.code {
				t.Errorf("style for %q = %+v, want bold=%v italic=%v strike=%v code=%v",
					c.text, s, c.bold, c.italic, c.strike, c.code)
			}
		})
	}
}

// --- Heading h2-h6 with inline content (still uses bold-fallback path) ------

// (h2-h6 always emit bold section.mrkdwn so inline formatting in their text
// is rendered as escaped plain text inside the bold wrapper. Verified in
// blocks_test.go::TestHandleHeading_BoldFallback_EscapesEmphasisChars.)

// --- Blockquote inline preservation ----------------------------------------

func TestHandleBlockquote_PreservesInlineFormatting(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "> a **bold** word and `code`.")
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	rt := blocks[0].(*slack.RichTextBlock)
	quote, ok := rt.Elements[0].(*slack.RichTextQuote)
	if !ok {
		t.Fatalf("got %T, want RichTextQuote", rt.Elements[0])
	}

	var bold, code *slack.RichTextSectionTextStyle
	for _, el := range quote.Elements {
		if t, ok := el.(*slack.RichTextSectionTextElement); ok {
			if strings.Contains(t.Text, "bold") {
				bold = t.Style
			}
			if strings.Contains(t.Text, "code") {
				code = t.Style
			}
		}
	}
	if bold == nil || !bold.Bold {
		t.Errorf("blockquote 'bold' style = %+v, want bold", bold)
	}
	if code == nil || !code.Code {
		t.Errorf("blockquote 'code' style = %+v, want code", code)
	}
}

// --- List item inline preservation -----------------------------------------

func TestHandleList_PreservesInlineFormatting(t *testing.T) {
	input := "- **bold** item\n- with [link](https://example.com)\n- with `code`\n"
	blocks, _ := renderForTest(t, Options{}, input)
	lists := extractLists(blocks)
	if len(lists) != 1 {
		t.Fatalf("got %d lists", len(lists))
	}
	if len(lists[0].Elements) != 3 {
		t.Fatalf("got %d items, want 3", len(lists[0].Elements))
	}

	// Item 1: bold style on "bold"
	sec1 := lists[0].Elements[0].(*slack.RichTextSection)
	if s := styleOf(sec1, "bold"); s == nil || !s.Bold {
		t.Errorf("list item 1 'bold' style = %+v", s)
	}

	// Item 2: link element
	sec2 := lists[0].Elements[1].(*slack.RichTextSection)
	var foundLink bool
	for _, el := range sec2.Elements {
		if _, ok := el.(*slack.RichTextSectionLinkElement); ok {
			foundLink = true
		}
	}
	if !foundLink {
		t.Error("list item 2 missing link element")
	}

	// Item 3: code style on "code"
	sec3 := lists[0].Elements[2].(*slack.RichTextSection)
	if s := styleOf(sec3, "code"); s == nil || !s.Code {
		t.Errorf("list item 3 'code' style = %+v", s)
	}
}

// --- inlineElementsToText helper -------------------------------------------

func TestInlineElementsToText_RoundTrip(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "hello **world** [docs](https://x.com)")
	sec := firstSection(t, blocks)
	got := inlineElementsToText(sec.Elements)
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") || !strings.Contains(got, "docs") {
		t.Errorf("inlineElementsToText = %q, missing expected text fragments", got)
	}
}
