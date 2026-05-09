package converter

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// --- Sanitization conformance suite ----------------------------------------
//
// Every form of Slack broadcast / mention pre-formed in the input must be
// HTML-entity-escaped by default so it doesn't ping or broadcast.
// Opting in via AllowBroadcasts: true bypasses the escape.

func TestSanitization_BroadcastForms_AllEscapedByDefault(t *testing.T) {
	cases := []struct {
		name, in string
	}{
		{"!channel", "alert <!channel> please"},
		{"!here", "ping <!here> right now"},
		{"!everyone", "<!everyone> heads up"},
		{"user mention", "see <@U012AB3CD> for context"},
		{"channel reference", "in <#C123ABC456> we discussed"},
		{"subteam", "cc <!subteam^S012ABC>"},
		{"nested angle brackets", "use <<weird>> brackets"},
		{"ampersand alone", "AT&T is a company"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocks, _ := renderForTest(t, Options{}, tc.in)
			if len(blocks) == 0 {
				t.Fatal("got no blocks")
			}
			// Check the typed block content directly. JSON marshaling
			// would re-escape `&` as `&` (HTML-safe JSON), masking
			// the entity-escape we just produced.
			text := concatTextElements(blocks)
			for _, raw := range []string{"<!channel>", "<!here>", "<!everyone>", "<!subteam", "<@U012AB3CD>", "<#C123ABC456>"} {
				if strings.Contains(text, raw) {
					t.Errorf("raw broadcast %q survived in text: %q", raw, text)
				}
			}
			if !strings.Contains(text, "&lt;") && !strings.Contains(text, "&amp;") {
				t.Errorf("expected entity-escaped form in text: %q", text)
			}
		})
	}
}

// concatTextElements walks the converted blocks and joins all text payloads
// (rich_text section text + markdown block content) into one string.
// Used by sanitization tests so they can assert on raw text without
// JSON HTML-escape interference.
func concatTextElements(blocks []slack.Block) string {
	var sb strings.Builder
	for _, b := range blocks {
		switch v := b.(type) {
		case *slack.RichTextBlock:
			for _, el := range v.Elements {
				switch e := el.(type) {
				case *slack.RichTextSection:
					for _, inner := range e.Elements {
						if t, ok := inner.(*slack.RichTextSectionTextElement); ok {
							sb.WriteString(t.Text)
							sb.WriteByte('\n')
						}
					}
				case *slack.RichTextQuote:
					for _, inner := range e.Elements {
						if t, ok := inner.(*slack.RichTextSectionTextElement); ok {
							sb.WriteString(t.Text)
							sb.WriteByte('\n')
						}
					}
				}
			}
		case *slack.MarkdownBlock:
			sb.WriteString(v.Text)
			sb.WriteByte('\n')
		case *slack.SectionBlock:
			if v.Text != nil {
				sb.WriteString(v.Text.Text)
				sb.WriteByte('\n')
			}
		}
	}
	return sb.String()
}

func TestSanitization_AllowBroadcastsTrue_Passthrough(t *testing.T) {
	opts := Options{AllowBroadcasts: true, Mode: ModeRichText}
	blocks, payload := renderForTest(t, opts, "alert <!channel> please")
	if len(blocks) == 0 {
		t.Fatal("got no blocks")
	}
	// AllowBroadcasts=true: raw text should pass through.
	// (CommonMark may strip the `<...>` if it's not a valid autolink,
	// but the literal content "channel" should be present in some form.)
	if strings.Contains(payload, "&lt;!channel&gt;") {
		t.Errorf("AllowBroadcasts=true should NOT entity-escape; payload=%s", payload)
	}
}

func TestSanitization_NormalText_Untouched(t *testing.T) {
	blocks, payload := renderForTest(t, Options{}, "no special characters here")
	if len(blocks) == 0 {
		t.Fatal("got no blocks")
	}
	if strings.Contains(payload, "&amp;") || strings.Contains(payload, "&lt;") {
		t.Errorf("plain text wrongly escaped: %s", payload)
	}
	if !strings.Contains(payload, "no special characters here") {
		t.Errorf("text missing from payload: %s", payload)
	}
}

func TestSanitization_AmpersandEscapedToEntity(t *testing.T) {
	blocks, _ := renderForTest(t, Options{}, "Tom & Jerry")
	if len(blocks) == 0 {
		t.Fatal("got no blocks")
	}
	text := concatTextElements(blocks)
	if !strings.Contains(text, "Tom &amp; Jerry") {
		t.Errorf("`&` should escape to `&amp;`; text=%q", text)
	}
}

// --- Mention map ------------------------------------------------------------

func TestMentionMap_KnownHandle_BecomesUserElement(t *testing.T) {
	opts := Options{
		Mode:       ModeRichText,
		MentionMap: map[string]string{"alice": "U123ABC"},
	}
	blocks, _ := renderForTest(t, opts, "ping @alice please")
	sec := firstSection(t, blocks)

	var foundUser *slack.RichTextSectionUserElement
	for _, el := range sec.Elements {
		if u, ok := el.(*slack.RichTextSectionUserElement); ok {
			foundUser = u
		}
	}
	if foundUser == nil {
		t.Fatal("no user element emitted for mapped @alice")
	}
	if foundUser.UserID != "U123ABC" {
		t.Errorf("user_id = %q, want U123ABC", foundUser.UserID)
	}
}

func TestMentionMap_ChannelID_BecomesChannelElement(t *testing.T) {
	opts := Options{
		Mode:       ModeRichText,
		MentionMap: map[string]string{"general": "C12345"},
	}
	blocks, _ := renderForTest(t, opts, "see @general for updates")
	sec := firstSection(t, blocks)

	var ch *slack.RichTextSectionChannelElement
	for _, el := range sec.Elements {
		if c, ok := el.(*slack.RichTextSectionChannelElement); ok {
			ch = c
		}
	}
	if ch == nil {
		t.Fatal("no channel element emitted for handle mapped to C-prefix ID")
	}
	if ch.ChannelID != "C12345" {
		t.Errorf("channel_id = %q, want C12345", ch.ChannelID)
	}
}

func TestMentionMap_UserGroupID_BecomesUserGroupElement(t *testing.T) {
	opts := Options{
		Mode:       ModeRichText,
		MentionMap: map[string]string{"team": "S98765"},
	}
	blocks, _ := renderForTest(t, opts, "cc @team for review")
	sec := firstSection(t, blocks)

	var ug *slack.RichTextSectionUserGroupElement
	for _, el := range sec.Elements {
		if g, ok := el.(*slack.RichTextSectionUserGroupElement); ok {
			ug = g
		}
	}
	if ug == nil {
		t.Fatal("no usergroup element emitted for handle mapped to S-prefix ID")
	}
	if ug.UsergroupID != "S98765" {
		t.Errorf("usergroup_id = %q, want S98765", ug.UsergroupID)
	}
}

func TestMentionMap_UnknownHandle_StaysAsLiteralText(t *testing.T) {
	opts := Options{
		Mode:       ModeRichText,
		MentionMap: map[string]string{"alice": "U123"},
	}
	blocks, payload := renderForTest(t, opts, "ping @bob about the issue")
	if len(blocks) == 0 {
		t.Fatal("no blocks")
	}
	// @bob is not in the map; it should remain as literal text.
	if !strings.Contains(payload, "@bob") {
		t.Errorf("expected @bob to survive as literal text; payload=%s", payload)
	}
	// And no user element should be emitted.
	sec := firstSection(t, blocks)
	for _, el := range sec.Elements {
		if _, ok := el.(*slack.RichTextSectionUserElement); ok {
			t.Errorf("unmapped @bob produced a user element")
		}
	}
}

func TestMentionMap_EmailAddress_NotMatched(t *testing.T) {
	// `user@example.com` should NOT match — the leading-boundary rule
	// requires whitespace (or punctuation) before `@`. `r@e` has a
	// letter before the `@`, so no match.
	opts := Options{
		Mode:       ModeRichText,
		MentionMap: map[string]string{"example": "U99"},
	}
	blocks, payload := renderForTest(t, opts, "contact user@example.com")
	if len(blocks) == 0 {
		t.Fatal("no blocks")
	}
	sec := firstSection(t, blocks)
	for _, el := range sec.Elements {
		if u, ok := el.(*slack.RichTextSectionUserElement); ok {
			t.Errorf("email address produced a user element with id=%q; payload=%s",
				u.UserID, payload)
		}
	}
}

func TestMentionMap_AtStartOfText_Matches(t *testing.T) {
	opts := Options{
		Mode:       ModeRichText,
		MentionMap: map[string]string{"alice": "U123"},
	}
	blocks, _ := renderForTest(t, opts, "@alice please review")
	sec := firstSection(t, blocks)
	var found bool
	for _, el := range sec.Elements {
		if _, ok := el.(*slack.RichTextSectionUserElement); ok {
			found = true
		}
	}
	if !found {
		t.Error("@alice at start of text not matched")
	}
}

func TestMentionMap_PunctuationAfter_Matched(t *testing.T) {
	opts := Options{
		Mode:       ModeRichText,
		MentionMap: map[string]string{"alice": "U123"},
	}
	blocks, _ := renderForTest(t, opts, "ping @alice, please")
	sec := firstSection(t, blocks)
	var found bool
	for _, el := range sec.Elements {
		if _, ok := el.(*slack.RichTextSectionUserElement); ok {
			found = true
		}
	}
	if !found {
		t.Error("@alice followed by comma not matched")
	}
}

// --- markdown_block path with sanitization ---------------------------------

func TestSanitization_MarkdownBlockMode_EscapesByDefault(t *testing.T) {
	r, err := New(Options{Mode: ModeMarkdownBlock})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("alert <!channel> please")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	mb := firstMarkdownBlock(t, blocks)
	if !strings.Contains(mb.Text, "&lt;!channel&gt;") {
		t.Errorf("expected entity-escaped form in markdown block: %q", mb.Text)
	}
}

func TestSanitization_MarkdownBlockMode_AllowBroadcastsTrue_NoEscape(t *testing.T) {
	r, err := New(Options{Mode: ModeMarkdownBlock, AllowBroadcasts: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("alert <!channel> please")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	mb := firstMarkdownBlock(t, blocks)
	if !strings.Contains(mb.Text, "<!channel>") {
		t.Errorf("expected raw passthrough with AllowBroadcasts=true: %q", mb.Text)
	}
}

// --- entityEscape helper ----------------------------------------------------

func TestEntityEscape_TableCases(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"&", "&amp;"},
		{"<", "&lt;"},
		{">", "&gt;"},
		{"a&b", "a&amp;b"},
		{"<tag>", "&lt;tag&gt;"},
		{"a & <b> > c", "a &amp; &lt;b&gt; &gt; c"},
		{"<!channel>", "&lt;!channel&gt;"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := entityEscape(tc.in)
			if got != tc.want {
				t.Errorf("entityEscape(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
