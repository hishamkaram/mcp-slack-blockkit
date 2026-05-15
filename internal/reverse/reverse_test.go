package reverse

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"

	"github.com/hishamkaram/mcp-slack-block-kit/internal/converter"
)

func mustMarkdown(t *testing.T, blocks []slack.Block) (string, []string) {
	t.Helper()
	md, warns, err := ToMarkdown(blocks)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	return md, warns
}

func TestToMarkdown_EmptyInput(t *testing.T) {
	md, warns := mustMarkdown(t, nil)
	if md != "" {
		t.Errorf("empty input should yield empty markdown, got %q", md)
	}
	if len(warns) != 0 {
		t.Errorf("empty input should yield no warnings, got %v", warns)
	}
}

func TestToMarkdown_Header(t *testing.T) {
	blocks := []slack.Block{
		slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, "Title", false, false)),
	}
	md, _ := mustMarkdown(t, blocks)
	if md != "# Title" {
		t.Errorf("header markdown = %q, want %q", md, "# Title")
	}
}

func TestToMarkdown_Divider(t *testing.T) {
	md, _ := mustMarkdown(t, []slack.Block{slack.NewDividerBlock()})
	if md != "---" {
		t.Errorf("divider markdown = %q, want %q", md, "---")
	}
}

func TestToMarkdown_RichTextStyles(t *testing.T) {
	sec := slack.NewRichTextSection(
		slack.NewRichTextSectionTextElement("plain ", nil),
		slack.NewRichTextSectionTextElement("bold", &slack.RichTextSectionTextStyle{Bold: true}),
		slack.NewRichTextSectionTextElement(" ", nil),
		slack.NewRichTextSectionTextElement("italic", &slack.RichTextSectionTextStyle{Italic: true}),
		slack.NewRichTextSectionTextElement(" ", nil),
		slack.NewRichTextSectionTextElement("code", &slack.RichTextSectionTextStyle{Code: true}),
		slack.NewRichTextSectionTextElement(" ", nil),
		slack.NewRichTextSectionTextElement("gone", &slack.RichTextSectionTextStyle{Strike: true}),
	)
	md, _ := mustMarkdown(t, []slack.Block{slack.NewRichTextBlock("", sec)})
	want := "plain **bold** _italic_ `code` ~~gone~~"
	if md != want {
		t.Errorf("styled markdown = %q, want %q", md, want)
	}
}

func TestToMarkdown_RichTextLink(t *testing.T) {
	sec := slack.NewRichTextSection(
		slack.NewRichTextSectionLinkElement("https://example.com", "docs", nil),
	)
	md, _ := mustMarkdown(t, []slack.Block{slack.NewRichTextBlock("", sec)})
	if md != "[docs](https://example.com)" {
		t.Errorf("link markdown = %q", md)
	}
}

func TestToMarkdown_RichTextMentions(t *testing.T) {
	sec := slack.NewRichTextSection(
		&slack.RichTextSectionUserElement{Type: slack.RTSEUser, UserID: "U123"},
		slack.NewRichTextSectionTextElement(" ", nil),
		&slack.RichTextSectionBroadcastElement{Type: slack.RTSEBroadcast, Range: "channel"},
		slack.NewRichTextSectionTextElement(" ", nil),
		&slack.RichTextSectionEmojiElement{Type: slack.RTSEEmoji, Name: "wave"},
	)
	md, _ := mustMarkdown(t, []slack.Block{slack.NewRichTextBlock("", sec)})
	want := "<@U123> <!channel> :wave:"
	if md != want {
		t.Errorf("mentions markdown = %q, want %q", md, want)
	}
}

func TestToMarkdown_RichTextBulletList(t *testing.T) {
	list := slack.NewRichTextList(
		slack.RTEListBullet, 0,
		slack.NewRichTextSection(slack.NewRichTextSectionTextElement("one", nil)),
		slack.NewRichTextSection(slack.NewRichTextSectionTextElement("two", nil)),
	)
	md, _ := mustMarkdown(t, []slack.Block{slack.NewRichTextBlock("", list)})
	if md != "- one\n- two" {
		t.Errorf("bullet list markdown = %q", md)
	}
}

func TestToMarkdown_RichTextOrderedListWithOffset(t *testing.T) {
	list := slack.NewRichTextList(
		slack.RTEListOrdered, 0,
		slack.NewRichTextSection(slack.NewRichTextSectionTextElement("a", nil)),
		slack.NewRichTextSection(slack.NewRichTextSectionTextElement("b", nil)),
	)
	list.Offset = 4 // continuation list — numbering should start at 5
	md, _ := mustMarkdown(t, []slack.Block{slack.NewRichTextBlock("", list)})
	if md != "5. a\n6. b" {
		t.Errorf("ordered list markdown = %q, want %q", md, "5. a\n6. b")
	}
}

func TestToMarkdown_RichTextQuote(t *testing.T) {
	q := &slack.RichTextQuote{
		Type:     slack.RTEQuote,
		Elements: []slack.RichTextSectionElement{slack.NewRichTextSectionTextElement("quoted", nil)},
	}
	md, _ := mustMarkdown(t, []slack.Block{slack.NewRichTextBlock("", q)})
	if md != "> quoted" {
		t.Errorf("quote markdown = %q", md)
	}
}

func TestToMarkdown_RichTextPreformatted(t *testing.T) {
	pre := &slack.RichTextPreformatted{
		Type:     slack.RTEPreformatted,
		Language: "go",
		Elements: []slack.RichTextSectionElement{slack.NewRichTextSectionTextElement("x := 1", nil)},
	}
	md, _ := mustMarkdown(t, []slack.Block{slack.NewRichTextBlock("", pre)})
	if md != "```go\nx := 1\n```" {
		t.Errorf("preformatted markdown = %q", md)
	}
}

func TestToMarkdown_MarkdownBlock_Verbatim(t *testing.T) {
	md, _ := mustMarkdown(t, []slack.Block{slack.NewMarkdownBlock("", "## Heading\n\ntext")})
	if md != "## Heading\n\ntext" {
		t.Errorf("markdown block = %q", md)
	}
}

func TestToMarkdown_Image(t *testing.T) {
	title := slack.NewTextBlockObject(slack.PlainTextType, "A cat", false, false)
	img := slack.NewImageBlock("https://example.com/c.png", "a cat", "", title)
	md, _ := mustMarkdown(t, []slack.Block{img})
	if md != `![a cat](https://example.com/c.png "A cat")` {
		t.Errorf("image markdown = %q", md)
	}
}

func TestToMarkdown_Table(t *testing.T) {
	cell := func(s string) *slack.RichTextBlock {
		return slack.NewRichTextBlock("",
			slack.NewRichTextSection(slack.NewRichTextSectionTextElement(s, nil)))
	}
	tbl := &slack.TableBlock{Rows: [][]*slack.RichTextBlock{
		{cell("H1"), cell("H2")},
		{cell("a"), cell("b")},
	}}
	md, _ := mustMarkdown(t, []slack.Block{tbl})
	want := "| H1 | H2 |\n| --- | --- |\n| a | b |"
	if md != want {
		t.Errorf("table markdown = %q, want %q", md, want)
	}
}

func TestToMarkdown_SectionMrkdwn_Warns(t *testing.T) {
	s := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, "*bold heading*", false, false), nil, nil,
	)
	md, warns := mustMarkdown(t, []slack.Block{s})
	if md != "*bold heading*" {
		t.Errorf("section markdown = %q", md)
	}
	if len(warns) == 0 {
		t.Error("expected a warning for mrkdwn section")
	}
}

func TestToMarkdown_ActionBlock_Warns(t *testing.T) {
	a := slack.NewActionBlock("", &slack.ButtonBlockElement{
		Type: slack.METButton,
		Text: slack.NewTextBlockObject(slack.PlainTextType, "Click", false, false),
	})
	_, warns := mustMarkdown(t, []slack.Block{a})
	if len(warns) == 0 {
		t.Error("expected a warning for an actions block")
	}
}

func TestToMarkdown_RichTextChannelUsergroupTeam(t *testing.T) {
	sec := slack.NewRichTextSection(
		&slack.RichTextSectionChannelElement{Type: slack.RTSEChannel, ChannelID: "C9"},
		slack.NewRichTextSectionTextElement(" ", nil),
		&slack.RichTextSectionUserGroupElement{Type: slack.RTSEUserGroup, UsergroupID: "S7"},
		slack.NewRichTextSectionTextElement(" ", nil),
		&slack.RichTextSectionTeamElement{Type: slack.RTSETeam, TeamID: "T1"},
	)
	md, _ := mustMarkdown(t, []slack.Block{slack.NewRichTextBlock("", sec)})
	want := "<#C9> <!subteam^S7> <!team^T1>"
	if md != want {
		t.Errorf("channel/usergroup/team markdown = %q, want %q", md, want)
	}
}

func TestToMarkdown_RichTextDateAndColor(t *testing.T) {
	fb := "Jan 1, 2026"
	sec := slack.NewRichTextSection(
		&slack.RichTextSectionDateElement{Type: slack.RTSEDate, Format: "{date}", Fallback: &fb},
		slack.NewRichTextSectionTextElement(" ", nil),
		&slack.RichTextSectionColorElement{Type: slack.RTSEColor, Value: "#ff0000"},
	)
	md, _ := mustMarkdown(t, []slack.Block{slack.NewRichTextBlock("", sec)})
	if md != "Jan 1, 2026 #ff0000" {
		t.Errorf("date/color markdown = %q", md)
	}
}

func TestToMarkdown_NestedList_Indented(t *testing.T) {
	outer := slack.NewRichTextList(slack.RTEListBullet, 0,
		slack.NewRichTextSection(slack.NewRichTextSectionTextElement("parent", nil)))
	inner := slack.NewRichTextList(slack.RTEListBullet, 1,
		slack.NewRichTextSection(slack.NewRichTextSectionTextElement("child", nil)))
	md, _ := mustMarkdown(t, []slack.Block{slack.NewRichTextBlock("", outer, inner)})
	if md != "- parent\n\n    - child" {
		t.Errorf("nested list markdown = %q", md)
	}
}

func TestToMarkdown_Image_SlackFileNoURL_Warns(t *testing.T) {
	img := &slack.ImageBlock{
		Type:      "image",
		AltText:   "internal",
		SlackFile: &slack.SlackFileObject{ID: "F123"},
	}
	md, warns := mustMarkdown(t, []slack.Block{img})
	if md != "![internal]()" {
		t.Errorf("slack_file image markdown = %q", md)
	}
	if len(warns) == 0 {
		t.Error("expected a warning for an image with no public URL")
	}
}

func TestToMarkdown_Context(t *testing.T) {
	ctx := slack.NewContextBlock(
		"",
		slack.NewTextBlockObject(slack.MarkdownType, "footnote text", false, false),
	)
	md, warns := mustMarkdown(t, []slack.Block{ctx})
	if md != "footnote text" {
		t.Errorf("context markdown = %q", md)
	}
	if len(warns) == 0 {
		t.Error("expected a warning for a context block")
	}
}

func TestToMarkdown_TableCell_EscapesPipe(t *testing.T) {
	cell := func(s string) *slack.RichTextBlock {
		return slack.NewRichTextBlock("",
			slack.NewRichTextSection(slack.NewRichTextSectionTextElement(s, nil)))
	}
	tbl := &slack.TableBlock{Rows: [][]*slack.RichTextBlock{
		{cell("a|b")},
	}}
	md, _ := mustMarkdown(t, []slack.Block{tbl})
	if !strings.Contains(md, `a\|b`) {
		t.Errorf("table cell should escape pipe; got %q", md)
	}
}

func TestToMarkdown_SectionFields(t *testing.T) {
	s := slack.NewSectionBlock(nil, []*slack.TextBlockObject{
		slack.NewTextBlockObject(slack.MarkdownType, "field one", false, false),
		slack.NewTextBlockObject(slack.MarkdownType, "field two", false, false),
	}, nil)
	md, _ := mustMarkdown(t, []slack.Block{s})
	if !strings.Contains(md, "field one") || !strings.Contains(md, "field two") {
		t.Errorf("section fields markdown = %q", md)
	}
}

func TestToMarkdown_SectionAccessory_Warns(t *testing.T) {
	acc := slack.NewAccessory(&slack.ButtonBlockElement{
		Type: slack.METButton,
		Text: slack.NewTextBlockObject(slack.PlainTextType, "Go", false, false),
	})
	s := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, "body", false, false), nil, acc,
	)
	_, warns := mustMarkdown(t, []slack.Block{s})
	if len(warns) == 0 {
		t.Error("expected a warning for a section accessory")
	}
}

func TestToMarkdown_RoundTrip_RichTextContent(t *testing.T) {
	input := "Hello **bold** and _italic_ and `code` text.\n\n" +
		"- first\n- second\n\n" +
		"> a quoted line\n\n" +
		"```go\nfmt.Println(1)\n```\n"

	opts := converter.DefaultOptions()
	opts.Mode = converter.ModeRichText
	r, err := converter.New(opts)
	if err != nil {
		t.Fatalf("converter.New: %v", err)
	}
	blocks, err := r.Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	md, _ := mustMarkdown(t, blocks)

	for _, want := range []string{
		"**bold**", "_italic_", "`code`",
		"- first", "- second",
		"> a quoted line",
		"```go", "fmt.Println(1)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("round-trip output missing %q\nfull output:\n%s", want, md)
		}
	}
}
