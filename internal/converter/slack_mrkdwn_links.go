package converter

import "regexp"

// slackURLForm matches Slack's mrkdwn `<URL|label>` link extension. Slack
// emits this shape in tool results when an existing message contains a
// labeled link; goldmark does NOT recognize it natively (CommonMark
// autolinks have no `|label` form), so the whole construct falls into a
// Text node and gets entity-escaped downstream, which renders as visible
// `&lt;…&gt;` in the output.
//
// Pre-processing the raw input is the cheapest way to repair both modes:
//   - rich_text path: goldmark then sees a regular `[label](URL)` and
//     produces a normal Link element.
//   - markdown_block path: the AST walker emits documented `[label](URL)`
//     syntax — Slack's markdown block doesn't accept `<URL|label>` either
//     (that form is mrkdwn-only, used by section blocks).
//
// Catastrophic broadcasts (`<!channel>`, `<!here>`, `<!everyone>`) and
// typed mentions (`<@U…>`, `<#C…>`, `<!subteam^…>`, `<!date^…>`) cannot
// match because every alternative requires a `[a-zA-Z]…:` URI scheme at
// position 1, and those tokens start with `!`, `@`, or `#`.
//
// Character classes:
//   - URL: scheme (letter + 1–31 of [A-Za-z0-9+.-]) + `:` + at least one
//     byte that is not `<`, `>`, `|`, or whitespace. URL-encoded `|`
//     (`%7C`) is allowed (real Google Drive links in the wild contain it).
//   - label: one or more bytes that are not `<` or `>`. Empty labels do
//     not match (would degrade to a plain autolink anyway).
var slackURLForm = regexp.MustCompile(
	`<([a-zA-Z][a-zA-Z0-9+.\-]{1,31}:[^<>|\s]+)\|([^<>]+)>`,
)

// rewriteSlackURLForms replaces every `<URL|label>` substring in s with
// the CommonMark `[label](URL)` equivalent. Idempotent: the output never
// contains the source pattern, so subsequent calls return the input
// unchanged.
//
// Known limitation: the rewrite is NOT context-aware at this layer. A
// Slack URL-form appearing inside a CommonMark code span or fenced code
// block will still be rewritten to [label](URL). The user then sees a
// clickable-ish artifact inside what was meant to be literal code.
// Slack tool results never emit the URL-form inside code, and the
// alternative — AST-aware rewriting — requires re-parsing after each
// rewrite. Pinned by TestRewriteSlackURLForms_InsideCodeSpan_RewrittenAnyway.
func rewriteSlackURLForms(s string) string {
	if !slackURLForm.MatchString(s) {
		return s
	}
	return slackURLForm.ReplaceAllString(s, "[$2]($1)")
}
