package converter

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// --- Positive: each trusted token shape produces the right typed element --

func TestPreserveTokens_UserMention_PromotesToUserElement(t *testing.T) {
	opts := Options{Mode: ModeRichText, PreserveMentionTokens: true}
	blocks, _ := renderForTest(t, opts, "see <@U012AB3CD> for context")
	sec := firstSection(t, blocks)
	var u *slack.RichTextSectionUserElement
	for _, el := range sec.Elements {
		if x, ok := el.(*slack.RichTextSectionUserElement); ok {
			u = x
		}
	}
	if u == nil {
		t.Fatal("expected a user element")
	}
	if u.UserID != "U012AB3CD" {
		t.Errorf("UserID = %q, want U012AB3CD", u.UserID)
	}
}

func TestPreserveTokens_UserMentionWithFallback_KeepsID(t *testing.T) {
	opts := Options{Mode: ModeRichText, PreserveMentionTokens: true}
	blocks, _ := renderForTest(t, opts, "hi <@U012AB3CD|alice> there")
	sec := firstSection(t, blocks)
	var u *slack.RichTextSectionUserElement
	for _, el := range sec.Elements {
		if x, ok := el.(*slack.RichTextSectionUserElement); ok {
			u = x
		}
	}
	if u == nil || u.UserID != "U012AB3CD" {
		t.Fatalf("user element not produced or wrong id; got %#v", u)
	}
}

func TestPreserveTokens_EnterpriseGridWUser_Promoted(t *testing.T) {
	opts := Options{Mode: ModeRichText, PreserveMentionTokens: true}
	blocks, _ := renderForTest(t, opts, "see <@W12345678> for context")
	sec := firstSection(t, blocks)
	var u *slack.RichTextSectionUserElement
	for _, el := range sec.Elements {
		if x, ok := el.(*slack.RichTextSectionUserElement); ok {
			u = x
		}
	}
	if u == nil || u.UserID != "W12345678" {
		t.Fatalf("expected Enterprise Grid W… promoted; got %#v", u)
	}
}

func TestPreserveTokens_ChannelRef_PromotesToChannelElement(t *testing.T) {
	opts := Options{Mode: ModeRichText, PreserveMentionTokens: true}
	blocks, _ := renderForTest(t, opts, "in <#C123ABC456> we discussed")
	sec := firstSection(t, blocks)
	var c *slack.RichTextSectionChannelElement
	for _, el := range sec.Elements {
		if x, ok := el.(*slack.RichTextSectionChannelElement); ok {
			c = x
		}
	}
	if c == nil || c.ChannelID != "C123ABC456" {
		t.Fatalf("channel element not produced; got %#v", c)
	}
}

func TestPreserveTokens_ChannelRefWithFallback_PromotesAndDropsFallback(t *testing.T) {
	opts := Options{Mode: ModeRichText, PreserveMentionTokens: true}
	blocks, _ := renderForTest(t, opts, "in <#C123ABC456|general> we discussed")
	sec := firstSection(t, blocks)
	var c *slack.RichTextSectionChannelElement
	for _, el := range sec.Elements {
		if x, ok := el.(*slack.RichTextSectionChannelElement); ok {
			c = x
		}
	}
	if c == nil || c.ChannelID != "C123ABC456" {
		t.Fatalf("channel element not produced; got %#v", c)
	}
}

func TestPreserveTokens_Subteam_PromotesToUsergroupElement(t *testing.T) {
	opts := Options{Mode: ModeRichText, PreserveMentionTokens: true}
	blocks, _ := renderForTest(t, opts, "cc <!subteam^SAZ94GDB8> please")
	sec := firstSection(t, blocks)
	var g *slack.RichTextSectionUserGroupElement
	for _, el := range sec.Elements {
		if x, ok := el.(*slack.RichTextSectionUserGroupElement); ok {
			g = x
		}
	}
	if g == nil || g.UsergroupID != "SAZ94GDB8" {
		t.Fatalf("usergroup element not produced; got %#v", g)
	}
}

func TestPreserveTokens_SubteamWithFallback_Promoted(t *testing.T) {
	opts := Options{Mode: ModeRichText, PreserveMentionTokens: true}
	blocks, _ := renderForTest(t, opts, "cc <!subteam^SAZ94GDB8|team> please")
	sec := firstSection(t, blocks)
	var g *slack.RichTextSectionUserGroupElement
	for _, el := range sec.Elements {
		if x, ok := el.(*slack.RichTextSectionUserGroupElement); ok {
			g = x
		}
	}
	if g == nil || g.UsergroupID != "SAZ94GDB8" {
		t.Fatalf("usergroup element not produced; got %#v", g)
	}
}

func TestPreserveTokens_DateToken_PromotesToDateElement(t *testing.T) {
	opts := Options{Mode: ModeRichText, PreserveMentionTokens: true}
	blocks, _ := renderForTest(t, opts, "meeting at <!date^1392734382^{date_long}|2014 February 18>")
	sec := firstSection(t, blocks)
	var d *slack.RichTextSectionDateElement
	for _, el := range sec.Elements {
		if x, ok := el.(*slack.RichTextSectionDateElement); ok {
			d = x
		}
	}
	if d == nil {
		t.Fatal("expected a date element")
	}
	if int64(d.Timestamp) != 1392734382 {
		t.Errorf("Timestamp = %d, want 1392734382", int64(d.Timestamp))
	}
	if d.Format != "{date_long}" {
		t.Errorf("Format = %q, want {date_long}", d.Format)
	}
	if d.Fallback == nil || *d.Fallback != "2014 February 18" {
		t.Errorf("Fallback = %v, want pointer to 2014 February 18", d.Fallback)
	}
}

func TestPreserveTokens_DateTokenWithLink_PromotesAndKeepsURL(t *testing.T) {
	opts := Options{Mode: ModeRichText, PreserveMentionTokens: true}
	blocks, _ := renderForTest(t, opts,
		"meeting <!date^1392734382^{date_short} at {time}^https://example.com/event|2014-02-18 link>")
	sec := firstSection(t, blocks)
	var d *slack.RichTextSectionDateElement
	for _, el := range sec.Elements {
		if x, ok := el.(*slack.RichTextSectionDateElement); ok {
			d = x
		}
	}
	if d == nil {
		t.Fatal("expected a date element")
	}
	if d.URL == nil || *d.URL != "https://example.com/event" {
		t.Errorf("URL = %v, want pointer to https://example.com/event", d.URL)
	}
}

// --- The critical safety case ---------------------------------------------

func TestPreserveTokens_SafetyContract_BroadcastsStillEscape(t *testing.T) {
	// PreserveMentionTokens=true, AllowBroadcasts=false: typed-mention
	// tokens pass through; catastrophic broadcasts still escape.
	opts := Options{Mode: ModeRichText, PreserveMentionTokens: true}
	in := "Ping <@U012AB3CD> in <#C123ABC456>, then <!channel>"
	blocks, _ := renderForTest(t, opts, in)
	sec := firstSection(t, blocks)

	var sawUser, sawChannel bool
	for _, el := range sec.Elements {
		switch e := el.(type) {
		case *slack.RichTextSectionUserElement:
			if e.UserID == "U012AB3CD" {
				sawUser = true
			}
		case *slack.RichTextSectionChannelElement:
			if e.ChannelID == "C123ABC456" {
				sawChannel = true
			}
		}
	}
	if !sawUser {
		t.Error("typed user element missing")
	}
	if !sawChannel {
		t.Error("typed channel element missing")
	}

	// The broadcast must NOT survive as a raw <!channel> token.
	text := concatTextElements(blocks)
	if strings.Contains(text, "<!channel>") {
		t.Errorf("raw <!channel> survived: %q", text)
	}
	if !strings.Contains(text, "&lt;!channel&gt;") {
		t.Errorf("expected escaped <!channel>: %q", text)
	}
}

// --- Negative: adversarial inputs must NOT be promoted ---------------------

func TestPreserveTokens_AdversarialInputs_NotPromoted(t *testing.T) {
	cases := []struct {
		name, in string
	}{
		{"lowercase user id", "see <@u012ab3cd>"},
		{"fake user @channel", "see <@channel>"},
		{"capital-C broadcast variant", "ping <!Channel>"},
		{"broadcast smuggled in fallback", "see <@U012AB3CD|<!channel>>"},
		{"double-pipe smuggle", "see <@U012AB3CD||@here>"},
		{"whitespace inside brackets", "see < @U012AB3CD >"},
		{"channel G prefix", "in <#G123ABC456>"},
		{"channel D prefix", "in <#D123ABC456>"},
		{"subteam missing caret", "cc <!subteam_S012ABC>"},
		{"no brackets", "user@U012AB3CD"},
		{"url-form not trusted", "click <javascript:alert(1)|here>"},
	}
	opts := Options{Mode: ModeRichText, PreserveMentionTokens: true}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocks, _ := renderForTest(t, opts, tc.in)
			sec := firstSection(t, blocks)
			for _, el := range sec.Elements {
				switch el.(type) {
				case *slack.RichTextSectionUserElement,
					*slack.RichTextSectionChannelElement,
					*slack.RichTextSectionUserGroupElement,
					*slack.RichTextSectionDateElement:
					t.Errorf("adversarial input promoted to typed element: %q produced %T",
						tc.in, el)
				}
			}
		})
	}
}

// --- markdown_block mode parity --------------------------------------------

func TestPreserveTokens_MarkdownBlock_KeepsTokenEscapesBroadcast(t *testing.T) {
	r, err := New(Options{Mode: ModeMarkdownBlock, PreserveMentionTokens: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("Ping <@U012AB3CD> then <!channel>")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	mb := firstMarkdownBlock(t, blocks)
	if !strings.Contains(mb.Text, "<@U012AB3CD>") {
		t.Errorf("trusted token must survive: %q", mb.Text)
	}
	if !strings.Contains(mb.Text, "&lt;!channel&gt;") {
		t.Errorf("broadcast must escape: %q", mb.Text)
	}
}

func TestPreserveTokens_MarkdownBlock_DefaultOffStillEscapes(t *testing.T) {
	r, err := New(Options{Mode: ModeMarkdownBlock})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("Ping <@U012AB3CD>")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	mb := firstMarkdownBlock(t, blocks)
	if strings.Contains(mb.Text, "<@U012AB3CD>") {
		t.Errorf("raw token survived without flag: %q", mb.Text)
	}
	if !strings.Contains(mb.Text, "&lt;@U012AB3CD&gt;") {
		t.Errorf("expected escaped form: %q", mb.Text)
	}
}

// --- Default behavior unchanged --------------------------------------------

func TestPreserveTokens_DefaultOff_TokensStillEscape(t *testing.T) {
	opts := Options{Mode: ModeRichText} // PreserveMentionTokens defaults to false
	blocks, _ := renderForTest(t, opts, "see <@U012AB3CD> please")
	sec := firstSection(t, blocks)
	for _, el := range sec.Elements {
		if _, ok := el.(*slack.RichTextSectionUserElement); ok {
			t.Errorf("token wrongly promoted when flag is off")
		}
	}
	text := concatTextElements(blocks)
	if !strings.Contains(text, "&lt;@U012AB3CD&gt;") {
		t.Errorf("token should be entity-escaped by default: %q", text)
	}
}

// --- extractTrustedMentionTokens regex pathology check ---------------------
//
// Loop the regex over every adversarial input plus realistic prose to catch
// catastrophic backtracking. The RE2 engine guarantees linear time, so this
// is a smoke test more than a true fuzz — but it documents that we expect
// the regex to terminate cleanly on each shape.

func TestExtractTrustedMentionTokens_Pathology_NoHang(t *testing.T) {
	inputs := []string{
		strings.Repeat("<", 4096),
		strings.Repeat("<@U", 1024) + ">",
		strings.Repeat("<!subteam^", 1024) + "S012ABC>",
		strings.Repeat("a<@U012AB3CD>b", 256),
		strings.Repeat("<@U012AB3CD|x", 256) + ">",
		"plain prose with no tokens at all just text",
	}
	for i, in := range inputs {
		// Bound each FindAll call; the suite times out if any one hangs.
		_ = extractTrustedMentionTokens(in)
		if t.Failed() {
			t.Fatalf("case %d failed", i)
		}
	}
}
