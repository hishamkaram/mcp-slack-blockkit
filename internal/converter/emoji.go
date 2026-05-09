package converter

import (
	"strings"

	"github.com/slack-go/slack"
)

// resolveEmoji is the post-processor that turns `:name:` substrings inside
// rich_text_section text elements into proper RichTextSectionEmojiElement
// values. Two subtleties make this trickier than a regex replace:
//
//  1. **goldmark fragmentation**: goldmark splits text nodes at `_` characters
//     (treating them as potential emphasis delimiters). So `:bar_chart:`
//     arrives as three sibling Text nodes — `":bar"`, `"_"`, `"chart:"` —
//     and rendering them as separate elements would defeat any pattern match.
//     We first merge consecutive same-style text elements to reconstitute
//     the candidate string, then match.
//
//  2. **time-string false positives**: `19:49:41` would naively match `:49:`
//     and produce a bogus emoji. We require non-digit boundaries on both
//     sides of the candidate, which kills the time case while preserving
//     `:wave:`, `:+1:`, `:bar_chart:`, etc.
//
// Style is preserved across the split: a `:wave:` inside a bold run becomes
// `[bold-text-prefix, emoji, bold-text-suffix]`, with the same Style on
// every text element.
func resolveEmoji(elements []slack.RichTextSectionElement) []slack.RichTextSectionElement {
	merged := mergeSameStyleText(elements)
	var out []slack.RichTextSectionElement
	for _, el := range merged {
		text, isText := el.(*slack.RichTextSectionTextElement)
		if !isText {
			out = append(out, el)
			continue
		}
		out = append(out, splitEmojiInText(text)...)
	}
	return out
}

// mergeSameStyleText collapses consecutive *RichTextSectionTextElement values
// that carry the same style into a single element. Non-text elements (links,
// emoji, etc.) act as natural separators — we never merge across them.
func mergeSameStyleText(elements []slack.RichTextSectionElement) []slack.RichTextSectionElement {
	if len(elements) < 2 {
		return elements
	}
	out := make([]slack.RichTextSectionElement, 0, len(elements))
	var pending *slack.RichTextSectionTextElement
	flush := func() {
		if pending != nil {
			out = append(out, pending)
			pending = nil
		}
	}
	for _, el := range elements {
		text, isText := el.(*slack.RichTextSectionTextElement)
		if !isText {
			flush()
			out = append(out, el)
			continue
		}
		if pending == nil {
			// Take a copy so we don't mutate the caller's element in place
			// when we extend it below.
			cp := *text
			pending = &cp
			continue
		}
		if styleEqual(pending.Style, text.Style) {
			pending.Text += text.Text
			continue
		}
		flush()
		cp := *text
		pending = &cp
	}
	flush()
	return out
}

// styleEqual returns true when two style pointers describe the same merged
// style — both nil, or both non-nil with field-equal values.
func styleEqual(a, b *slack.RichTextSectionTextStyle) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// splitEmojiInText scans a single text element for :emoji: matches and
// returns a slice of elements: alternating text / emoji / text / ... .
// Style is preserved on every emitted text element. Returns the original
// element unchanged when no matches are found.
func splitEmojiInText(text *slack.RichTextSectionTextElement) []slack.RichTextSectionElement {
	src := text.Text
	if !strings.ContainsRune(src, ':') {
		return []slack.RichTextSectionElement{text}
	}

	var out []slack.RichTextSectionElement
	i := 0
	for i < len(src) {
		match := findNextEmojiMatch(src, i)
		if match == nil {
			// No more matches — emit the remainder.
			if i < len(src) {
				out = append(out, &slack.RichTextSectionTextElement{
					Type:  slack.RTSEText,
					Text:  src[i:],
					Style: text.Style,
				})
			}
			break
		}
		// Emit any text before the match.
		if match.start > i {
			out = append(out, &slack.RichTextSectionTextElement{
				Type:  slack.RTSEText,
				Text:  src[i:match.start],
				Style: text.Style,
			})
		}
		// Emit the emoji element.
		out = append(out, slack.NewRichTextSectionEmojiElement(match.name, 0, nil))
		i = match.end
	}

	// If we found no matches at all, return the original element so callers
	// don't get a one-element clone (cheaper and clearer).
	if len(out) == 1 {
		if t, ok := out[0].(*slack.RichTextSectionTextElement); ok && t.Text == src {
			return []slack.RichTextSectionElement{text}
		}
	}
	return out
}

// emojiMatch holds the byte offsets and extracted name for one emoji.
type emojiMatch struct {
	start, end int    // half-open byte range [start, end) of the full :name:
	name       string // emoji name (without the surrounding colons)
}

// findNextEmojiMatch returns the next valid `:name:` match in src at or
// after offset, or nil when no match exists. "Valid" means:
//   - The opening `:` is at the start of src, or preceded by a non-digit byte.
//   - The name matches `[a-z+][a-z0-9_+\-]*` (case-insensitive on the start).
//   - The closing `:` is at end of src, or followed by a non-digit byte.
//
// The non-digit boundary requirement filters out time strings like
// `19:49:41` while preserving real shortcodes (`:wave:` after a space,
// `:tada:` at the end of a sentence, `:+1:`, `:bar_chart:` after the
// fragmentation merge in mergeSameStyleText).
func findNextEmojiMatch(src string, offset int) *emojiMatch {
	for i := offset; i < len(src); i++ {
		if src[i] != ':' {
			continue
		}
		// Boundary check on the leading side.
		if i > 0 && isAsciiDigit(src[i-1]) {
			continue
		}
		// Find the closing colon.
		end := -1
		for j := i + 1; j < len(src); j++ {
			c := src[j]
			if c == ':' {
				end = j
				break
			}
			if !isEmojiNameByte(c) {
				break
			}
		}
		if end < 0 || end == i+1 {
			continue
		}
		// Boundary check on the trailing side.
		if end+1 < len(src) && isAsciiDigit(src[end+1]) {
			continue
		}
		// First char of the name must be a letter or `+` (filters numeric-
		// only candidates that slipped past the boundary check, like
		// hypothetical `:42:` at end-of-line).
		first := src[i+1]
		if !isEmojiNameStartByte(first) {
			continue
		}
		return &emojiMatch{
			start: i,
			end:   end + 1,
			name:  src[i+1 : end],
		}
	}
	return nil
}

func isAsciiDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isAsciiLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isEmojiNameStartByte reports whether c is a valid first character for a
// Slack emoji name. Slack accepts letters and `+` (for `:+1:`).
func isEmojiNameStartByte(c byte) bool {
	return isAsciiLetter(c) || c == '+'
}

// isEmojiNameByte reports whether c is a valid non-leading character of a
// Slack emoji name: letters, digits, underscore, plus, hyphen.
func isEmojiNameByte(c byte) bool {
	return isAsciiLetter(c) || isAsciiDigit(c) || c == '_' || c == '+' || c == '-'
}
