package converter

import (
	"testing"

	"github.com/slack-go/slack"

	"github.com/hishamkaram/mcp-slack-block-kit/internal/preview"
)

// TestLinks_PrintBuilderURLs is a maintainer-facing visual-QA helper.
// It does NOT make assertions — it prints Block Kit Builder URLs that a
// maintainer with a Slack workspace can click through to confirm each
// link form renders as a clickable link (and not as literal text with
// visible brackets).
//
// Run with:
//
//	go test -v -run TestLinks_PrintBuilderURLs ./internal/converter/
//
// What to verify in the Builder preview:
//   - Each `[…](…)` should render as an underlined / colored link.
//   - The autolink pairs (`[url](url)` vs raw `<url>`) live side by side
//     so you can confirm Slack supports the form our walker emits.
//   - The `<!channel>` row must NOT highlight or notify — it should
//     appear as literal entity-escaped text.
//
// The four user-reported defects (autolink, email, bare URL, Slack
// URL-form) appear first; the head-to-head comparison cases follow.
func TestLinks_PrintBuilderURLs(t *testing.T) {
	cases := []struct {
		name  string
		input string
		note  string
	}{
		{
			name:  "1-plain-link",
			input: "[ProposalDeck_202605_CegahDBD Publishers Campaign May 2026](https://docs.google.com/presentation/d/1W6HaAFUL7jwAn8Eq90MRbFFTSaipGKEONaEB7zOYuX0/edit?usp=drivesdk)",
			note:  "Standard CommonMark link. Should render as clickable label.",
		},
		{
			name:  "2-url-autolink",
			input: "<https://example.com>",
			note:  "DEFECT 1 fixed. Walker emits [url](url); preview should show a clickable URL.",
		},
		{
			name:  "3-email-autolink",
			input: "<user@example.com>",
			note:  "DEFECT 2 fixed. Walker emits [email](mailto:email); preview should show a clickable mailto link.",
		},
		{
			name:  "4-bare-url-via-linkify",
			input: "Check out https://example.com today",
			note:  "DEFECT 4 fixed. Walker promotes bare URL to [url](url); preview should show a clickable URL between 'Check out' and 'today'.",
		},
		{
			name:  "5-slack-url-form",
			input: "<https://docs.google.com/spreadsheets/d/1m%7CfaCRBkq1nWJQ2leb8Y/edit|Refa UGC v3 shared-drive>",
			note:  "DEFECT 3 fixed (user's real-world example). Pre-parse rewriter converts to [label](URL); preview should show 'Refa UGC v3 shared-drive' as a clickable link.",
		},
		{
			name:  "6-url-with-ampersand",
			input: "[click](https://x.com?a=1&b=2)",
			note:  "URL with `&` in query string. Preview should show a clickable 'click' link to the URL with both query params intact.",
		},
		{
			name:  "7-channel-broadcast-safety",
			input: "alert <!channel> please",
			note:  "SAFETY. Preview should show LITERAL text `<!channel>` (not a highlighted broadcast).",
		},
		{
			name:  "8-multi-link-paragraph",
			input: "see [docs](https://example.com/docs), bare https://example.com/bare, or autolink <https://example.com/auto>",
			note:  "Mixed paragraph. All three link forms should render clickable.",
		},
	}

	for _, c := range cases {
		blocks, _, err := newAutoRenderer(t).ConvertWithWarnings(c.input)
		if err != nil {
			t.Errorf("ConvertWithWarnings %s: %v", c.name, err)
			continue
		}
		pr, err := preview.BuilderURL(blocks)
		if err != nil {
			t.Errorf("BuilderURL %s: %v", c.name, err)
			continue
		}
		t.Logf("\n=== %s ===\nnote : %s\ninput: %q\nblock: %s\nURL  : %s\n",
			c.name, c.note, c.input, mdBlockText(blocks), pr.URL)
	}

	// Head-to-head comparison for the UNVERIFIED claim in the plan: does
	// Slack's markdown block render `<https://x.com>` as a clickable
	// autolink, or as literal text? Our walker chose the safer
	// `[url](url)` form. Both URLs are printed below; if the LEFT (raw
	// `<url>`) renders clickable, we could simplify the walker in a
	// follow-up. If only the RIGHT renders clickable, the walker's
	// current choice is correct.
	t.Log("\n--- AUTOLINK FORM COMPARISON (manual visual check) ---")
	left := []slack.Block{slack.NewMarkdownBlock("", "Raw autolink: <https://example.com>")}
	right := []slack.Block{slack.NewMarkdownBlock("", "Bracketed: [https://example.com](https://example.com)")}
	if pr, err := preview.BuilderURL(left); err == nil {
		t.Logf("\nLEFT  (raw `<url>` form — UNVERIFIED by docs):\n%s", pr.URL)
	}
	if pr, err := preview.BuilderURL(right); err == nil {
		t.Logf("\nRIGHT (`[url](url)` form — what our walker emits):\n%s", pr.URL)
	}
}

func newAutoRenderer(t *testing.T) *Renderer {
	t.Helper()
	opts := DefaultOptions()
	opts.Mode = ModeAuto
	r, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func mdBlockText(blocks []slack.Block) string {
	for _, b := range blocks {
		if mb, ok := b.(*slack.MarkdownBlock); ok {
			return mb.Text
		}
	}
	return "(no markdown block in output)"
}
