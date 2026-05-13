package converter

import (
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// emitForTest parses input with the same goldmark setup as the Renderer
// and returns the markdown-block text the walker produces. The input is
// run through rewriteSlackURLForms first so the tests mirror the
// renderer's actual pipeline.
func emitForTest(t *testing.T, opts Options, input string) string {
	t.Helper()
	input = rewriteSlackURLForms(input)
	gm := goldmark.New(goldmark.WithExtensions(extension.GFM))
	src := []byte(input)
	root := gm.Parser().Parse(text.NewReader(src))
	if root == nil {
		t.Fatalf("goldmark returned nil AST for %q", input)
	}
	return emitMarkdownBlockText(root, src, opts)
}

// TestEmitMarkdownBlockText_TableCases is the primary contract test for
// the AST walker. One row per AST node type / link variant / safety case.
// The `want` values are exact strings — change them deliberately, not
// reflexively.
func TestEmitMarkdownBlockText_TableCases(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		in   string
		want string
	}{
		// --- Plain text & emphasis -----------------------------------------
		{name: "plain text", in: "hello", want: "hello"},
		{name: "heading H1", in: "# Title", want: "# Title"},
		{name: "heading H3", in: "### sub", want: "### sub"},
		{name: "bold", in: "**b**", want: "**b**"},
		{name: "italic", in: "*i*", want: "*i*"},
		{name: "strikethrough", in: "~~s~~", want: "~~s~~"},
		{name: "inline code", in: "use `Convert()`", want: "use `Convert()`"},
		{name: "code span with backtick inside", in: "show ``a`b`` ok", want: "show ``a`b`` ok"},
		{name: "thematic break", in: "---", want: "---"},

		// --- Link variants (the headline fix) ------------------------------
		{
			name: "inline link [text](url)",
			in:   "see [docs](https://x.com)",
			want: "see [docs](https://x.com)",
		},
		{
			name: "CommonMark URL autolink becomes [url](url) — defect 1",
			in:   "<https://example.com>",
			want: "[https://example.com](https://example.com)",
		},
		{
			name: "email autolink becomes [email](mailto:email) — defect 2",
			in:   "<user@example.com>",
			want: "[user@example.com](mailto:user@example.com)",
		},
		{
			name: "bare URL via Linkify becomes [url](url) — defect 4",
			in:   "Check out https://example.com today",
			want: "Check out [https://example.com](https://example.com) today",
		},
		{
			name: "Slack URL-form rewritten to CommonMark — defect 3",
			in:   "<https://x.com|click>",
			want: "[click](https://x.com)",
		},
		{
			name: "users real Slack URL-form example",
			in:   "<https://docs.google.com/spreadsheets/d/1m%7Cfa/edit%7Cv3|Refa UGC v3 shared-drive>",
			want: "[Refa UGC v3 shared-drive](https://docs.google.com/spreadsheets/d/1m%7Cfa/edit%7Cv3)",
		},
		{
			name: "URL with ampersand in query preserves byte",
			in:   "[click](https://x.com?a=1&b=2)",
			want: "[click](https://x.com?a=1&b=2)",
		},
		{
			name: "link with title attribute",
			in:   `[text](https://x.com "tooltip")`,
			want: `[text](https://x.com "tooltip")`,
		},

		// --- Safety: broadcasts must escape --------------------------------
		{
			name: "channel broadcast escapes",
			in:   "alert <!channel> please",
			want: "alert &lt;!channel&gt; please",
		},
		{
			name: "here broadcast escapes",
			in:   "ping <!here>",
			want: "ping &lt;!here&gt;",
		},
		{
			name: "everyone broadcast escapes",
			in:   "team <!everyone>",
			want: "team &lt;!everyone&gt;",
		},

		// --- Typed mentions: 3-way policy ---------------------------------
		{
			name: "user mention escapes by default",
			in:   "ping <@U012ABCDEF>",
			want: "ping &lt;@U012ABCDEF&gt;",
		},
		{
			name: "user mention preserved with PreserveMentionTokens",
			opts: Options{PreserveMentionTokens: true},
			in:   "ping <@U012ABCDEF>",
			want: "ping <@U012ABCDEF>",
		},
		{
			name: "user mention preserved with AllowBroadcasts",
			opts: Options{AllowBroadcasts: true},
			in:   "ping <@U012ABCDEF>",
			want: "ping <@U012ABCDEF>",
		},
		{
			name: "channel reference preserved with PreserveMentionTokens",
			opts: Options{PreserveMentionTokens: true},
			in:   "see <#C012ABCDEF|general>",
			want: "see <#C012ABCDEF|general>",
		},
		{
			name: "subteam preserved with PreserveMentionTokens",
			opts: Options{PreserveMentionTokens: true},
			in:   "page <!subteam^S012ABCDEF|frontend>",
			want: "page <!subteam^S012ABCDEF|frontend>",
		},
		{
			name: "broadcast still escapes even with PreserveMentionTokens",
			opts: Options{PreserveMentionTokens: true},
			in:   "alert <!channel> now",
			want: "alert &lt;!channel&gt; now",
		},

		// --- Code blocks: literal, no escape, no rewrite -------------------
		{
			name: "autolink inside code span stays literal",
			in:   "see `<https://x.com>` syntax",
			want: "see `<https://x.com>` syntax",
		},
		{
			name: "broadcast inside code span is left literal",
			in:   "show `<!channel>` form",
			want: "show `<!channel>` form",
		},
		{
			name: "fenced code preserves angle brackets",
			in:   "```\n<https://x.com>\n```",
			want: "```\n<https://x.com>\n```",
		},
		{
			name: "fenced code preserves language and content",
			in:   "```go\nfmt.Println(\"hi\")\n```",
			want: "```go\nfmt.Println(\"hi\")\n```",
		},

		// --- Lists ----------------------------------------------------------
		{
			name: "unordered list",
			in:   "- one\n- two",
			want: "- one\n- two",
		},
		{
			name: "ordered list",
			in:   "1. one\n2. two",
			want: "1. one\n2. two",
		},
		{
			name: "ordered list with custom start",
			in:   "3. three\n4. four",
			want: "3. three\n4. four",
		},
		{
			name: "task list checked and unchecked",
			in:   "- [ ] todo\n- [x] done",
			want: "- [ ] todo\n- [x] done",
		},
		{
			name: "list item containing a link",
			in:   "- visit [docs](https://x.com)",
			want: "- visit [docs](https://x.com)",
		},

		// --- Blockquote -----------------------------------------------------
		{
			name: "blockquote single line",
			in:   "> quote",
			want: "> quote",
		},
		{
			name: "blockquote with link inside",
			in:   "> see [docs](https://x.com)",
			want: "> see [docs](https://x.com)",
		},

		// --- Image ----------------------------------------------------------
		{
			name: "image emitted as ![alt](url)",
			in:   "![alt](https://x.com/i.png)",
			want: "![alt](https://x.com/i.png)",
		},

		// --- Tables ---------------------------------------------------------
		{
			name: "simple table",
			in:   "| h1 | h2 |\n|---|---|\n| a | b |\n| c | d |",
			want: "| h1 | h2 |\n| --- | --- |\n| a | b |\n| c | d |",
		},
		{
			name: "table with alignment",
			in:   "| L | C | R |\n|:---|:---:|---:|\n| a | b | c |",
			want: "| L | C | R |\n| :--- | :---: | ---: |\n| a | b | c |",
		},

		// --- Empty / whitespace --------------------------------------------
		{name: "empty input", in: "", want: ""},
		{name: "only spaces", in: "   ", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := emitForTest(t, tc.opts, tc.in)
			if got != tc.want {
				t.Errorf("emit(%q) =\n%q\nwant\n%q", tc.in, got, tc.want)
			}
		})
	}
}

// TestEmitMarkdownBlockText_AllowBroadcasts confirms the bypass: every
// shape that normally escapes passes through unchanged when the option
// is set.
func TestEmitMarkdownBlockText_AllowBroadcasts(t *testing.T) {
	opts := Options{AllowBroadcasts: true}
	inputs := []string{
		"alert <!channel> please",
		"ping <@U012ABCDEF>",
		"see <#C012ABCDEF|general>",
	}
	for _, in := range inputs {
		got := emitForTest(t, opts, in)
		if got != in {
			t.Errorf("AllowBroadcasts: emit(%q) = %q, want pass-through", in, got)
		}
	}
}

// TestEmitMarkdownBlockText_FallbackForUnknownNodes asserts the
// defensive fallback path: if we ever encounter a node type we didn't
// enumerate, content is not dropped silently — it falls through to
// extractPlainText + entityEscape.
func TestEmitMarkdownBlockText_FallbackForUnknownNodes(t *testing.T) {
	// HTMLBlock is an enumerated node; raw HTML should be defanged.
	// This serves as the canary that confirms HTML never leaks through.
	in := "<div>hello <!channel></div>"
	got := emitForTest(t, Options{}, in)
	// goldmark parses the raw HTML as an HTMLBlock; the walker escapes it.
	if strings.Contains(got, "<div>") {
		t.Errorf("raw HTML survived: %q", got)
	}
	if strings.Contains(got, "<!channel>") {
		t.Errorf("broadcast survived inside raw HTML: %q", got)
	}
	if !strings.Contains(got, "&lt;!channel&gt;") {
		t.Errorf("expected entity-escaped broadcast, got %q", got)
	}
}

// TestLongestBacktickRun is a small helper sanity test that the code-span
// delimiter math handles ties and runs correctly.
func TestLongestBacktickRun(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"no backticks here", 0},
		{"one ` here", 1},
		{"two `` together", 2},
		{"mix ` and `` and ```", 3},
		{"```alone", 3},
	}
	for _, tc := range cases {
		if got := longestBacktickRun(tc.in); got != tc.want {
			t.Errorf("longestBacktickRun(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
