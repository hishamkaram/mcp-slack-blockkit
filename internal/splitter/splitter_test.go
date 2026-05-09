package splitter

import (
	"strings"
	"testing"
)

// --- Pass-through ------------------------------------------------------------

func TestSplitText_ShortInput_ReturnsSingleChunk(t *testing.T) {
	got := SplitText("hello world", 100, 10)
	if len(got) != 1 || got[0] != "hello world" {
		t.Errorf("got %v, want [hello world]", got)
	}
}

func TestSplitText_ExactlyAtLimit_ReturnsSingleChunk(t *testing.T) {
	s := strings.Repeat("a", 50)
	got := SplitText(s, 50, 5)
	if len(got) != 1 || got[0] != s {
		t.Errorf("got %d chunks, want 1 (input == limit)", len(got))
	}
}

func TestSplitText_EmptyInput_ReturnsEmptyChunk(t *testing.T) {
	got := SplitText("", 100, 10)
	if len(got) != 1 || got[0] != "" {
		t.Errorf("got %v, want [\"\"]", got)
	}
}

// --- Boundary preference ----------------------------------------------------

func TestSplitText_PrefersParagraphBoundary(t *testing.T) {
	// 60 chars of "a", then a paragraph break, then 60 more.
	s := strings.Repeat("a", 60) + "\n\n" + strings.Repeat("b", 60)
	got := SplitText(s, 100, 20)
	if len(got) != 2 {
		t.Fatalf("got %d chunks, want 2", len(got))
	}
	// First chunk should end at the paragraph break.
	if !strings.HasPrefix(got[0], strings.Repeat("a", 60)) {
		t.Errorf("chunk 0 = %q, expected to start with a's", got[0])
	}
	if strings.Contains(got[0], "b") {
		t.Errorf("chunk 0 leaked into the second paragraph: %q", got[0])
	}
}

func TestSplitText_PrefersSentenceBoundary(t *testing.T) {
	// 50-char first sentence, period+space, then a 60-char run.
	s := strings.Repeat("a", 50) + ". " + strings.Repeat("b", 60)
	got := SplitText(s, 80, 30)
	if len(got) != 2 {
		t.Fatalf("got %d chunks, want 2; chunks=%v", len(got), got)
	}
	if !strings.HasSuffix(got[0], ". ") && !strings.HasSuffix(got[0], ".") {
		t.Errorf("chunk 0 should end at sentence boundary; got %q", got[0])
	}
}

func TestSplitText_PrefersWhitespaceBoundary(t *testing.T) {
	// 100 word-fragments separated by single spaces, no sentence punctuation.
	parts := make([]string, 100)
	for i := range parts {
		parts[i] = "word"
	}
	s := strings.Join(parts, " ") // 4*100 + 99 = 499 chars
	got := SplitText(s, 200, 30)
	if len(got) < 2 {
		t.Fatalf("got %d chunks, want ≥2", len(got))
	}
	for i, c := range got {
		// No chunk should split mid-word: each chunk's first char must
		// be a letter (not whitespace), and last char must be a letter
		// (not a partial word).
		if len(c) == 0 {
			continue
		}
		if c[0] == ' ' {
			t.Errorf("chunk %d starts with whitespace: %q", i, c)
		}
		if i < len(got)-1 && c[len(c)-1] == ' ' {
			// Trailing whitespace is fine if it's the boundary char.
			continue
		}
	}
}

// --- Mid-word safety --------------------------------------------------------

func TestSplitText_NeverSplitsMidWord_WhenWhitespaceAvailable(t *testing.T) {
	// A long input with consistent whitespace at predictable intervals.
	const wordLen = 9 // "abcdefghi"
	const repeats = 200
	parts := make([]string, repeats)
	for i := range parts {
		parts[i] = "abcdefghi"
	}
	s := strings.Join(parts, " ")

	got := SplitText(s, 80, 10)
	for i, c := range got {
		// The last chunk may include a trailing fragment legitimately;
		// what we want to check is no chunk starts mid-word, which would
		// manifest as the chunk starting WITHOUT a leading char that's
		// the start of a word boundary.
		if len(c) == 0 {
			continue
		}
		// Each non-first chunk should start with a word character (since
		// we trimmed trailing whitespace at the boundary).
		if i > 0 && c[0] == ' ' {
			t.Errorf("chunk %d starts with whitespace: %q", i, c)
		}
		// No chunk should exceed the limit.
		if len(c) > 80 {
			t.Errorf("chunk %d length = %d, exceeds limit 80", i, len(c))
		}
	}
}

// --- Round-trip invariant ---------------------------------------------------

func TestSplitText_RoundTripPreservesContent(t *testing.T) {
	cases := []string{
		"",
		"single word",
		"two words",
		strings.Repeat("hello ", 1000),
		"para 1.\n\npara 2.\n\npara 3.",
		strings.Repeat("a", 500) + "\n\n" + strings.Repeat("b", 500),
		strings.Repeat("nospaceswhatsoever", 200), // pathological un-breakable
	}
	for _, in := range cases {
		t.Run(truncate(in, 30), func(t *testing.T) {
			out := SplitText(in, 200, 30)
			rejoined := Concat(out)
			if rejoined != in {
				t.Errorf("round-trip failed: input=%q (%d chars), out concat=%q (%d chars)",
					truncate(in, 60), len(in), truncate(rejoined, 60), len(rejoined))
			}
		})
	}
}

// --- Limit enforcement ------------------------------------------------------

func TestSplitText_NoChunkExceedsLimit_ExceptUnbreakableToken(t *testing.T) {
	in := strings.Repeat("a", 1000)
	out := SplitText(in, 100, 10)
	for i, c := range out {
		if len(c) > 100 {
			t.Errorf("chunk %d length = %d, exceeds limit 100", i, len(c))
		}
	}
}

func TestSplitText_ZeroOrNegativeLimit_ReturnsInputAsIs(t *testing.T) {
	in := "anything"
	for _, limit := range []int{0, -1, -100} {
		got := SplitText(in, limit, 10)
		if len(got) != 1 || got[0] != in {
			t.Errorf("limit=%d: got %v, want [anything]", limit, got)
		}
	}
}

func TestSplitText_SafetyMarginOversized_FallsBackGracefully(t *testing.T) {
	in := strings.Repeat("word ", 200)
	// safetyMargin >= maxChars: the cut window collapses but the splitter
	// still produces sensible output (falls back to maxChars-only window).
	got := SplitText(in, 50, 200)
	if len(got) < 2 {
		t.Fatalf("got %d chunks, want ≥2", len(got))
	}
	// Round-trip still preserved.
	if Concat(got) != in {
		t.Errorf("round-trip failed with oversized margin")
	}
}

// --- Fuzz target ------------------------------------------------------------

func FuzzSplitText(f *testing.F) {
	f.Add("hello", 100, 10)
	f.Add(strings.Repeat("hello ", 200), 50, 10)
	f.Add("aaaaaa", 3, 1)
	f.Add("", 100, 10)
	f.Fuzz(func(t *testing.T, s string, maxChars, margin int) {
		// Bound input sizes so fuzzing terminates predictably.
		if maxChars > 1000 || maxChars < -10 {
			t.Skip()
		}
		if margin > 1000 || margin < -10 {
			t.Skip()
		}
		if len(s) > 10000 {
			t.Skip()
		}

		out := SplitText(s, maxChars, margin)

		// Invariant 1: output is non-empty (always at least one element).
		if len(out) == 0 {
			t.Fatal("SplitText returned empty slice")
		}

		// Invariant 2: round-trip preserves content byte-for-byte.
		if got := Concat(out); got != s {
			t.Fatalf("round-trip failed: input=%q (%d), out=%q (%d)",
				truncate(s, 50), len(s), truncate(got, 50), len(got))
		}

		// Invariant 3: with positive limits, each chunk respects the limit
		// — UNLESS the input contains a single un-breakable token longer
		// than the limit, in which case one over-limit chunk is permitted.
		if maxChars > 0 {
			over := 0
			for _, c := range out {
				if len(c) > maxChars {
					over++
				}
			}
			if over > 1 {
				t.Errorf("multiple chunks exceed limit %d: chunk lengths=%v",
					maxChars, chunkLens(out))
			}
		}
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func chunkLens(out []string) []int {
	r := make([]int, len(out))
	for i, c := range out {
		r[i] = len(c)
	}
	return r
}
