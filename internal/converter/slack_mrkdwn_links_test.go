package converter

import "testing"

// TestRewriteSlackURLForms_TableCases exercises the pre-parse rewriter
// for the Slack mrkdwn `<URL|label>` extension. The rows that MUST NOT
// match (broadcasts, typed mentions, …) protect the security model:
// rewriting them into `[label](URL)` would let LLM-injected mention
// tokens survive the sanitizer.
func TestRewriteSlackURLForms_TableCases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Positive matches.
		{
			name: "basic",
			in:   "<https://x.com|click>",
			want: "[click](https://x.com)",
		},
		{
			name: "users real Google Drive example with URL-encoded pipe",
			in:   "<https://docs.google.com/spreadsheets/d/1m%7CfaCRBkq1nWJQ2leb8Y-fCHEW1QSno4WftP3CuMwHE/edit%20usp=drivesdk%7CRefa%20UGC%20v3%20shared-drive|Refa UGC v3 shared-drive>",
			want: "[Refa UGC v3 shared-drive](https://docs.google.com/spreadsheets/d/1m%7CfaCRBkq1nWJQ2leb8Y-fCHEW1QSno4WftP3CuMwHE/edit%20usp=drivesdk%7CRefa%20UGC%20v3%20shared-drive)",
		},
		{
			name: "mailto scheme",
			in:   "<mailto:foo@bar.com|email me>",
			want: "[email me](mailto:foo@bar.com)",
		},
		{
			name: "label contains spaces",
			in:   "<https://x.com|click here please>",
			want: "[click here please](https://x.com)",
		},
		{
			name: "two on one line",
			in:   "<https://a.com|a> and <https://b.com|b>",
			want: "[a](https://a.com) and [b](https://b.com)",
		},
		{
			name: "wrapped in prose",
			in:   "see <https://x.com|the docs> for more",
			want: "see [the docs](https://x.com) for more",
		},

		// Idempotency.
		{
			name: "already-rewritten passes through",
			in:   "[click](https://x.com)",
			want: "[click](https://x.com)",
		},

		// Security: must NOT match catastrophic broadcasts.
		{name: "channel broadcast", in: "<!channel>", want: "<!channel>"},
		{name: "channel broadcast with fallback", in: "<!channel|fb>", want: "<!channel|fb>"},
		{name: "here broadcast", in: "<!here>", want: "<!here>"},
		{name: "everyone broadcast", in: "<!everyone>", want: "<!everyone>"},

		// Security: must NOT match typed Slack tokens.
		{name: "user mention", in: "<@U012ABCDEF>", want: "<@U012ABCDEF>"},
		{name: "user mention with fallback", in: "<@U012ABCDEF|alice>", want: "<@U012ABCDEF|alice>"},
		{name: "channel reference", in: "<#C012ABC|general>", want: "<#C012ABC|general>"},
		{name: "subteam reference", in: "<!subteam^S012ABC|frontend>", want: "<!subteam^S012ABC|frontend>"},
		{name: "date token", in: "<!date^1672531200^{date}|Jan 1>", want: "<!date^1672531200^{date}|Jan 1>"},

		// Non-matches that fall back to the entity-escape layer.
		{name: "plain CommonMark autolink (no label)", in: "<https://x.com>", want: "<https://x.com>"},
		{name: "empty label", in: "<https://x.com|>", want: "<https://x.com|>"},
		{name: "empty URL", in: "<|label>", want: "<|label>"},
		{name: "URL with space (invalid)", in: "<https://x .com|click>", want: "<https://x .com|click>"},
		{name: "label contains nested angle", in: "<https://x.com|outer <inner> end>", want: "<https://x.com|outer <inner> end>"},
		{name: "label contains broadcast", in: "<https://x.com|<!channel>>", want: "<https://x.com|<!channel>>"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rewriteSlackURLForms(tc.in)
			if got != tc.want {
				t.Errorf("rewriteSlackURLForms(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRewriteSlackURLForms_Idempotent checks that running the rewriter
// twice produces the same output as running it once.
func TestRewriteSlackURLForms_Idempotent(t *testing.T) {
	in := "alpha <https://a.com|a> beta <https://b.com|b> gamma"
	once := rewriteSlackURLForms(in)
	twice := rewriteSlackURLForms(once)
	if once != twice {
		t.Errorf("not idempotent:\nfirst:  %q\nsecond: %q", once, twice)
	}
}

// TestRewriteSlackURLForms_InsideCodeSpan_RewrittenAnyway pins the known
// limitation: the rewriter is not AST-aware at pre-parse time. A Slack
// URL-form inside a CommonMark code span will still be rewritten. Real
// Slack tool outputs never emit `<URL|label>` inside code spans, so the
// limitation is acceptable. If a maintainer ever wants to fix it, this
// test will fail and document the intended new behavior.
func TestRewriteSlackURLForms_InsideCodeSpan_RewrittenAnyway(t *testing.T) {
	in := "use `<https://x.com|y>` form"
	want := "use `[y](https://x.com)` form"
	got := rewriteSlackURLForms(in)
	if got != want {
		t.Errorf("rewriteSlackURLForms(%q) = %q, want %q", in, got, want)
	}
}
