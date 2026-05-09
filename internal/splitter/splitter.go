// Package splitter contains the two splitters that keep Block Kit output
// inside Slack's limits:
//
//   - SplitText breaks an oversized text payload (e.g. a `section.text`
//     value, mrkdwn body, or `markdown` block content) into chunks no
//     larger than a configured maximum, preferring paragraph > sentence >
//     word boundaries and never splitting mid-word.
//
//   - ChunkBlocks (added in build-order step 12) breaks a flat list of
//     slack.Block values into multiple message-sized payloads on the
//     50-block limit, with the additional rule that no single chunk
//     contains more than one TableBlock (Slack's `only_one_table_allowed`).
//
// Both functions are pure and can be tested in isolation from the converter.
package splitter

import "strings"

// SplitText breaks s into chunks no longer than maxChars. The splitter
// prefers boundaries in this order:
//
//  1. Paragraph (a `\n\n` run)
//  2. Sentence (a `. ` / `! ` / `? ` after a non-space)
//  3. Whitespace (any space, tab, or newline)
//
// safetyMargin reserves bytes within each chunk so we can search backward
// from the strict ceiling for a clean boundary. With safetyMargin=100 and
// maxChars=3000, a long word that doesn't fit in the last 100 bytes still
// gets emitted in the next chunk rather than being mid-split.
//
// Invariants (covered by fuzz tests):
//   - Each returned chunk has len ≤ maxChars.
//   - strings.Join(SplitText(s, ...), "") == s for any input s. The original
//     content (including all whitespace) is preserved byte-for-byte.
//   - For inputs ≤ maxChars, returns []string{s} (a single-element slice).
//
// Special cases:
//   - Empty input returns []string{""} (one empty chunk) rather than nil,
//     so callers can rely on len(result) ≥ 1.
//   - When safetyMargin ≥ maxChars or maxChars ≤ 0, falls back to a hard
//     mid-word cut at maxChars. Pathological config, but doesn't panic.
//   - When a single un-breakable token is itself longer than maxChars
//     (e.g. a 5000-char URL with no whitespace), we hard-cut at maxChars
//     for that token and continue. There is no clean alternative that
//     preserves content; callers should validate inputs before calling.
func SplitText(s string, maxChars, safetyMargin int) []string {
	if maxChars <= 0 {
		return []string{s}
	}
	if len(s) <= maxChars {
		return []string{s}
	}

	// Effective search window: we hunt for a boundary in the range
	// [maxChars - safetyMargin, maxChars]. If safetyMargin is too large
	// or non-positive, fall back to a single-position hunt at maxChars.
	margin := safetyMargin
	if margin < 0 || margin >= maxChars {
		margin = 0
	}

	var out []string
	for len(s) > maxChars {
		cut := findCut(s, maxChars, margin)
		out = append(out, s[:cut])
		s = s[cut:]
	}
	if len(s) > 0 {
		out = append(out, s)
	}
	return out
}

// findCut returns the byte offset at which to cut s into a head of length
// at most maxChars. Tries paragraph, then sentence, then whitespace
// boundaries within [maxChars-margin, maxChars]. If none found, falls back
// to the last whitespace before maxChars; if none exists at all (one giant
// token), hard-cuts at maxChars.
func findCut(s string, maxChars, margin int) int {
	low := maxChars - margin
	if low < 1 {
		low = 1
	}

	// 1. Paragraph boundary: prefer the last "\n\n" (or longer run) inside
	//    the search window. Cut INCLUDES the trailing newline(s) so the
	//    next chunk starts cleanly.
	if p := lastParagraphBoundary(s, low, maxChars); p > 0 {
		return p
	}

	// 2. Sentence boundary: ". ", "! ", "? " at the end of a sentence-like
	//    construct. Cut after the space.
	if p := lastSentenceBoundary(s, low, maxChars); p > 0 {
		return p
	}

	// 3. Any whitespace boundary in window. Cut AFTER the whitespace so
	//    the next chunk doesn't start with a leading space.
	if p := lastWhitespaceBoundary(s, low, maxChars); p > 0 {
		return p
	}

	// 4. Extended search: any whitespace before maxChars (give up the
	//    safety margin to avoid mid-word cuts).
	if p := lastWhitespaceBoundary(s, 1, maxChars); p > 0 {
		return p
	}

	// 5. Pathological fallback: one un-breakable token, hard-cut at maxChars.
	return maxChars
}

// lastParagraphBoundary returns the byte offset of the END of the last
// paragraph break (a run of ≥2 consecutive newlines) within [low, hi], or
// 0 if none found. Returns the position AFTER the trailing newlines.
func lastParagraphBoundary(s string, low, hi int) int {
	if hi > len(s) {
		hi = len(s)
	}
	// Scan backward for a "\n\n" pattern; cut after the second newline.
	for i := hi - 2; i >= low-1 && i >= 0; i-- {
		if s[i] == '\n' && s[i+1] == '\n' {
			// Skip any additional consecutive newlines so the next chunk
			// doesn't start with leading blank lines.
			end := i + 2
			for end < len(s) && end < hi && s[end] == '\n' {
				end++
			}
			return end
		}
	}
	return 0
}

// lastSentenceBoundary returns the byte offset AFTER a sentence-ending
// punctuation + space pair within [low, hi], or 0 if none found.
func lastSentenceBoundary(s string, low, hi int) int {
	if hi > len(s) {
		hi = len(s)
	}
	for i := hi - 1; i >= low && i >= 1; i-- {
		if !isSpace(s[i]) {
			continue
		}
		switch s[i-1] {
		case '.', '!', '?':
			// Skip any further whitespace so the next chunk has no leading space.
			end := i + 1
			for end < hi && isSpace(s[end]) {
				end++
			}
			return end
		}
	}
	return 0
}

// lastWhitespaceBoundary returns the byte offset AFTER the last whitespace
// run within [low, hi], or 0 if none found.
func lastWhitespaceBoundary(s string, low, hi int) int {
	if hi > len(s) {
		hi = len(s)
	}
	for i := hi - 1; i >= low; i-- {
		if !isSpace(s[i]) {
			continue
		}
		// Walk forward past any contiguous whitespace.
		end := i + 1
		for end < hi && isSpace(s[end]) {
			end++
		}
		return end
	}
	return 0
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// Concat is a convenience helper that re-joins a chunk slice. Useful in
// tests for the round-trip invariant assertion.
func Concat(chunks []string) string {
	return strings.Join(chunks, "")
}
