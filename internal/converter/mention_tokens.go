package converter

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/slack-go/slack"
)

// trustedSlackToken matches the four shapes Slack itself emits when it
// returns a message: user mentions, channel references, subteam (usergroup)
// references, and date tokens. Each may carry an optional `|fallback`.
//
// Character classes are deliberately strict: uppercase letters and digits
// only for IDs, no whitespace inside the brackets, and fallback bodies
// reject the four Slack control characters (`<`, `>`, `&`, `|`) — those
// would be entity-escaped in a real Slack-emitted token, so a literal
// occurrence in input means the token was constructed by something other
// than Slack and should not be trusted.
//
// Catastrophic broadcasts (`<!channel>` / `<!here>` / `<!everyone>`) are
// deliberately NOT in this set; they still get entity-escaped unless
// Options.AllowBroadcasts is true.
//
// RE2 has no lookbehind, but we don't need any: the `<` anchor at the
// start of every alternative is enough to ensure we don't match across
// arbitrary substrings.
var trustedSlackToken = regexp.MustCompile(
	`<(?:` +
		`@(?P<user>[UW][A-Z0-9]{2,})(?:\|(?P<ufb>[^<>&|]*))?` +
		`|` +
		`#(?P<channel>C[A-Z0-9]{2,})(?:\|(?P<cfb>[^<>&|]*))?` +
		`|` +
		`!subteam\^(?P<usergroup>S[A-Z0-9]{2,})(?:\|(?P<sfb>[^<>&|]*))?` +
		`|` +
		`!date\^(?P<ts>\d{1,15})\^(?P<dtokens>[^<>&^|]+)(?:\^(?P<dlink>[^<>&|]*))?\|(?P<dfallback>[^<>&|]*)` +
		`)>`,
)

// tokenKind tags each match so callers can dispatch on the typed-element
// to emit without re-parsing the bracketed content.
type tokenKind int

const (
	tokenKindUser tokenKind = iota
	tokenKindChannel
	tokenKindUsergroup
	tokenKindDate
)

type tokenMatch struct {
	start, end int
	kind       tokenKind
	id         string // user, channel, or usergroup ID; unused for date
	// date-token-only fields
	ts       int64
	format   string
	link     string // optional URL segment for date tokens
	fallback string // shared by user/channel/usergroup/date tokens; "" when omitted
}

// extractTrustedMentionTokens returns every trusted-token occurrence in src,
// in source order. The slice is empty when src contains no trusted tokens.
// Returned ranges are non-overlapping (RE2 guarantees this for FindAll).
func extractTrustedMentionTokens(src string) []tokenMatch {
	all := trustedSlackToken.FindAllStringSubmatchIndex(src, -1)
	if len(all) == 0 {
		return nil
	}
	idx := trustedSlackToken.SubexpNames()
	groupIdx := map[string]int{}
	for i, n := range idx {
		if n != "" {
			groupIdx[n] = i
		}
	}
	out := make([]tokenMatch, 0, len(all))
	for _, m := range all {
		match := tokenMatch{start: m[0], end: m[1]}
		switch {
		case capStart(m, groupIdx["user"]) >= 0:
			match.kind = tokenKindUser
			match.id = capStr(src, m, groupIdx["user"])
			match.fallback = capStr(src, m, groupIdx["ufb"])
		case capStart(m, groupIdx["channel"]) >= 0:
			match.kind = tokenKindChannel
			match.id = capStr(src, m, groupIdx["channel"])
			match.fallback = capStr(src, m, groupIdx["cfb"])
		case capStart(m, groupIdx["usergroup"]) >= 0:
			match.kind = tokenKindUsergroup
			match.id = capStr(src, m, groupIdx["usergroup"])
			match.fallback = capStr(src, m, groupIdx["sfb"])
		case capStart(m, groupIdx["ts"]) >= 0:
			match.kind = tokenKindDate
			ts, err := strconv.ParseInt(capStr(src, m, groupIdx["ts"]), 10, 64)
			if err != nil {
				// Shouldn't happen — regex restricts to digits — but skip
				// rather than panic.
				continue
			}
			match.ts = ts
			match.format = capStr(src, m, groupIdx["dtokens"])
			match.link = capStr(src, m, groupIdx["dlink"])
			match.fallback = capStr(src, m, groupIdx["dfallback"])
		}
		out = append(out, match)
	}
	return out
}

// capStart returns m[2*group], or -1 when group is out of range or unmatched.
func capStart(m []int, group int) int {
	if group <= 0 || 2*group+1 > len(m) {
		return -1
	}
	return m[2*group]
}

// capStr returns the substring referenced by submatch index `group`, or ""
// when that submatch didn't participate in the match.
func capStr(src string, m []int, group int) string {
	if group <= 0 || 2*group+1 > len(m) {
		return ""
	}
	s, e := m[2*group], m[2*group+1]
	if s < 0 || e < 0 {
		return ""
	}
	return src[s:e]
}

// expandTrustedMentions walks text elements and replaces trusted-token
// substrings with typed Slack rich_text elements. Non-text elements pass
// through unchanged. Style (bold/italic/etc.) carries to surrounding text
// fragments and to user/channel/usergroup elements; date elements have no
// style field on slack-go, so style is preserved on neighboring text only.
func expandTrustedMentions(elements []slack.RichTextSectionElement) []slack.RichTextSectionElement {
	var out []slack.RichTextSectionElement
	expanded := false
	for _, el := range elements {
		text, ok := el.(*slack.RichTextSectionTextElement)
		if !ok {
			out = append(out, el)
			continue
		}
		matches := extractTrustedMentionTokens(text.Text)
		if len(matches) == 0 {
			out = append(out, el)
			continue
		}
		expanded = true
		out = append(out, splitTextOnTokens(text, matches)...)
	}
	if !expanded {
		return elements
	}
	return out
}

// splitTextOnTokens converts one text element into an alternating slice of
// text / typed-mention / text … given a non-empty, non-overlapping match
// slice in source order.
func splitTextOnTokens(text *slack.RichTextSectionTextElement, matches []tokenMatch) []slack.RichTextSectionElement {
	src := text.Text
	style := text.Style
	out := make([]slack.RichTextSectionElement, 0, 2*len(matches)+1)
	cursor := 0
	for _, m := range matches {
		if m.start > cursor {
			out = append(out, &slack.RichTextSectionTextElement{
				Type:  slack.RTSEText,
				Text:  src[cursor:m.start],
				Style: style,
			})
		}
		out = append(out, buildTypedMention(m, style))
		cursor = m.end
	}
	if cursor < len(src) {
		out = append(out, &slack.RichTextSectionTextElement{
			Type:  slack.RTSEText,
			Text:  src[cursor:],
			Style: style,
		})
	}
	return out
}

// buildTypedMention turns a tokenMatch into the corresponding slack-go
// rich_text element. Style propagates to user/channel/usergroup elements;
// date elements omit it (the slack-go struct has no Style field).
func buildTypedMention(m tokenMatch, style *slack.RichTextSectionTextStyle) slack.RichTextSectionElement {
	switch m.kind {
	case tokenKindUser:
		return &slack.RichTextSectionUserElement{
			Type:   slack.RTSEUser,
			UserID: m.id,
			Style:  style,
		}
	case tokenKindChannel:
		return &slack.RichTextSectionChannelElement{
			Type:      slack.RTSEChannel,
			ChannelID: m.id,
			Style:     style,
		}
	case tokenKindUsergroup:
		return slack.NewRichTextSectionUserGroupElement(m.id)
	case tokenKindDate:
		var urlPtr, fbPtr *string
		if m.link != "" {
			urlPtr = &m.link
		}
		if m.fallback != "" {
			fbPtr = &m.fallback
		}
		return slack.NewRichTextSectionDateElement(m.ts, m.format, urlPtr, fbPtr)
	}
	// Defensive default — keep the literal text so we don't drop content
	// silently if a new kind is added without updating this switch.
	return &slack.RichTextSectionTextElement{
		Type:  slack.RTSEText,
		Text:  "",
		Style: style,
	}
}

// escapePreservingTokens is the markdown_block-path counterpart to
// entityEscape: it entity-escapes every span of src EXCEPT the substrings
// matching trustedSlackToken, which pass through verbatim. The Slack
// markdown-block parser then sees the original token bytes and renders the
// mention correctly, while LLM-injected `<!channel>` (which doesn't match
// the trusted-token regex) still becomes `&lt;!channel&gt;`.
func escapePreservingTokens(src string) string {
	matches := trustedSlackToken.FindAllStringIndex(src, -1)
	if len(matches) == 0 {
		return entityEscape(src)
	}
	var b strings.Builder
	b.Grow(len(src) + 16)
	cursor := 0
	for _, m := range matches {
		if m[0] > cursor {
			b.WriteString(entityEscape(src[cursor:m[0]]))
		}
		b.WriteString(src[m[0]:m[1]])
		cursor = m[1]
	}
	if cursor < len(src) {
		b.WriteString(entityEscape(src[cursor:]))
	}
	return b.String()
}
