package converter

import (
	"strings"

	"github.com/slack-go/slack"
)

// sanitizeBroadcasts entity-escapes the three Slack control characters
// (`&`, `<`, `>`) in every text element, unless the caller has opted in to
// passthrough via Options.AllowBroadcasts.
//
// Why this is mandatory: AI-generated markdown that contains literal
// `<!channel>`, `<!here>`, `<@U012AB3CD>`, or `<#C123>` would broadcast or
// ping the workspace when sent — a real attack vector when the LLM is
// summarising untrusted user content. Escaping the three control chars
// renders such payloads as inert literal text. Real mentions are added
// via Options.MentionMap (see applyMentionMap), which produces typed
// user/channel/usergroup elements that the renderer composes safely.
//
// Escaping order matters: `&` must run first so we don't double-encode
// the `&` in `&lt;` etc.
func sanitizeBroadcasts(elements []slack.RichTextSectionElement, allow bool) []slack.RichTextSectionElement {
	if allow {
		return elements
	}
	for i, el := range elements {
		text, ok := el.(*slack.RichTextSectionTextElement)
		if !ok {
			continue
		}
		if !strings.ContainsAny(text.Text, "&<>") {
			continue
		}
		// Take a defensive copy — the caller may share the original element
		// with other code paths (e.g. test fixtures).
		cp := *text
		cp.Text = entityEscape(text.Text)
		elements[i] = &cp
	}
	return elements
}

// entityEscape replaces &, <, > with the matching HTML entity references.
// & first (otherwise we'd double-encode the entities we just emitted).
func entityEscape(s string) string {
	if !strings.ContainsAny(s, "&<>") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// applyMentionMap walks text elements and replaces `@handle` substrings
// with proper Slack user/channel/usergroup elements when handle appears in
// the map. Handles map to one of three Slack entity prefixes:
//
//	"U…"  → RichTextSectionUserElement       (user)
//	"C…"  → RichTextSectionChannelElement    (channel)
//	"S…"  → RichTextSectionUserGroupElement  (usergroup)
//
// Anything else is treated as a user ID by default. Handles not in the
// map are left as literal text — they'll be entity-escaped by
// sanitizeBroadcasts later if the user wrote `<@…>` syntax explicitly.
//
// The matcher is intentionally narrow: `@handle` matches only when:
//   - The `@` is at the start of the text run, or preceded by whitespace.
//   - The handle is `[a-zA-Z0-9_.-]+` (Slack username allowed chars).
//   - The character after the handle is whitespace, punctuation, or end.
//
// This avoids false positives like email addresses (`user@example.com`).
func applyMentionMap(elements []slack.RichTextSectionElement, mentions map[string]string) []slack.RichTextSectionElement {
	if len(mentions) == 0 {
		return elements
	}
	var out []slack.RichTextSectionElement
	for _, el := range elements {
		text, ok := el.(*slack.RichTextSectionTextElement)
		if !ok || !strings.Contains(text.Text, "@") {
			out = append(out, el)
			continue
		}
		out = append(out, splitMentionsInText(text, mentions)...)
	}
	return out
}

// splitMentionsInText scans one text element for @handle matches and
// returns a slice of elements: alternating text / mention / text / ... .
// Style is preserved on every emitted text element. Returns the original
// element unchanged when no matches are found.
func splitMentionsInText(text *slack.RichTextSectionTextElement, mentions map[string]string) []slack.RichTextSectionElement {
	src := text.Text
	var out []slack.RichTextSectionElement
	i := 0
	for i < len(src) {
		match := findNextMentionMatch(src, i, mentions)
		if match == nil {
			if i < len(src) {
				out = append(out, &slack.RichTextSectionTextElement{
					Type:  slack.RTSEText,
					Text:  src[i:],
					Style: text.Style,
				})
			}
			break
		}
		if match.start > i {
			out = append(out, &slack.RichTextSectionTextElement{
				Type:  slack.RTSEText,
				Text:  src[i:match.start],
				Style: text.Style,
			})
		}
		out = append(out, makeMentionElement(match.id, text.Style))
		i = match.end
	}
	if len(out) == 0 {
		return []slack.RichTextSectionElement{text}
	}
	return out
}

type mentionMatch struct {
	start, end int
	id         string // resolved Slack ID (U…, C…, or S…)
}

// findNextMentionMatch returns the next valid `@handle` match in src at or
// after offset where handle is in the mentions map, or nil when none exists.
func findNextMentionMatch(src string, offset int, mentions map[string]string) *mentionMatch {
	for i := offset; i < len(src); i++ {
		if src[i] != '@' {
			continue
		}
		// Leading boundary: start of string or whitespace before.
		if i > 0 && !isMentionLeadBoundary(src[i-1]) {
			continue
		}
		// Find the end of the handle.
		end := i + 1
		for end < len(src) && isHandleByte(src[end]) {
			end++
		}
		if end == i+1 {
			continue // no handle characters
		}
		// Trailing boundary: end of string or non-handle byte.
		if end < len(src) && !isMentionTailBoundary(src[end]) {
			continue
		}
		handle := src[i+1 : end]
		id, ok := mentions[handle]
		if !ok {
			continue
		}
		return &mentionMatch{start: i, end: end, id: id}
	}
	return nil
}

func makeMentionElement(id string, style *slack.RichTextSectionTextStyle) slack.RichTextSectionElement {
	if id == "" {
		return slack.NewRichTextSectionTextElement("@?", style)
	}
	switch id[0] {
	case 'C':
		return &slack.RichTextSectionChannelElement{
			Type:      slack.RTSEChannel,
			ChannelID: id,
			Style:     style,
		}
	case 'S':
		return slack.NewRichTextSectionUserGroupElement(id)
	default: // U… or anything else: treat as a user
		return &slack.RichTextSectionUserElement{
			Type:   slack.RTSEUser,
			UserID: id,
			Style:  style,
		}
	}
}

func isHandleByte(c byte) bool {
	return isAsciiLetter(c) || isAsciiDigit(c) || c == '_' || c == '.' || c == '-'
}

func isMentionLeadBoundary(c byte) bool {
	// `@` is a mention only at start-of-text or after whitespace/punctuation
	// that wouldn't form an email or other compound (digits and letters
	// would). The conservative rule is: only after whitespace.
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '(' || c == '['
}

func isMentionTailBoundary(c byte) bool {
	return !isHandleByte(c)
}
