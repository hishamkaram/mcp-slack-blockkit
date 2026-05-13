---
description: Security-critical mention sanitization rule for the converter and MCP server packages. Lazy-loaded only when editing files that emit text into Slack Block Kit fields.
paths:
  - "internal/converter/**"
  - "internal/server/**"
  - "block_kit/**"
---

# Mention sanitization is mandatory

Slack uses `&`, `<`, `>` as control characters. Any AI-generated content
that contains literal `<!channel>`, `<!here>`, `<!everyone>`, `<@U…>`,
`<#C…>`, or `<!subteam^…>` would broadcast or ping the workspace when
the Block Kit message is sent.

**Rule**: every text run emitted into a Slack `text` field MUST be
HTML-entity-escaped (`&` → `&amp;`, `<` → `&lt;`, `>` → `&gt;`) UNLESS
`Options.AllowBroadcasts == true`.

The implementation lives in `internal/converter/mentions.go`:

- `entityEscape(s string) string` — the actual escape; `&` first to
  avoid double-encoding.
- `sanitizeBroadcasts(elements, allow bool)` — applies `entityEscape`
  to every `*RichTextSectionTextElement` in the slice unless `allow` is
  true.
- `applyMentionMap(elements, mentions)` — runs BEFORE sanitization so
  intentional `@handle → <@U…>` substitutions produce typed elements
  (which are immune to the escape).

The pipeline order in `renderInlinesWithOpts`:

```
1. visit()                  // build raw elements from AST
2. applyMentionMap()        // intentional mentions → typed elements
3. expandTrustedMentions()  // pre-typed Slack tokens → typed elements (only when PreserveMentionTokens)
4. resolveEmoji()           // :name: → emoji elements
5. sanitizeBroadcasts()     // entity-escape any remaining stray <, >, &
```

**For the markdown_block path** (`emitMarkdownBlock` in
`markdown_block.go`), emission is **AST-driven** via
`emitMarkdownBlockText` (`markdown_block_emit.go`). The walker re-emits
each AST node as Slack-supported CommonMark and applies escaping per
node type:

- **Text/String content** (paragraphs, headings, list items, table
  cells, blockquote text, link text) is HTML-entity-escaped by the
  walker's `writeEscapedText` helper. When `PreserveMentionTokens` is
  true, it calls `escapePreservingTokens` instead (trusted-token spans
  pass through, everything else escapes). When `AllowBroadcasts` is
  true, no escape is applied. Consecutive Text/String siblings are
  coalesced before escaping so trusted-token detection survives
  goldmark's text fragmentation at `<` characters.
- **URL bytes** in `[text](url)`, `[url](url)` (from autolinks), and
  `![alt](url)` are emitted verbatim. URLs are parser-bounded by `(...)`
  and never need entity escaping; the browser/Slack handle any `&` in
  the URL at render time.
- **Code spans and fenced code blocks** emit content RAW (no escape).
  CommonMark treats code as literal — broadcast tokens inside backticks
  are not interpreted by Slack.
- **Slack mrkdwn URL-form `<URL|label>`** is rewritten to CommonMark
  `[label](URL)` by `rewriteSlackURLForms` in
  `slack_mrkdwn_links.go` BEFORE goldmark parses, so both the rich_text
  and markdown_block paths see a regular Link node. The rewriter's
  regex starts with a URI scheme (`[a-zA-Z]…:`), so it cannot match
  `<!channel>`, `<@U…>`, `<#C…>`, or `<!subteam^…>` — those still flow
  through the text-escape layer above.

Don't reintroduce the old blanket `entityEscape` over the raw input —
it destroyed CommonMark autolink syntax (`<https://x.com>` became
literal `&lt;https://x.com&gt;` text in Slack's renderer). The AST
walker emits a documented `[url](url)` form instead.

## PreserveMentionTokens — the safer escape hatch

`Options.PreserveMentionTokens` (default `false`) lets already-typed
Slack tokens survive the escape pass without granting blanket
passthrough. Only four token shapes are trusted, with strict character
classes:

| Trusted | Class |
|---|---|
| `<@U…>` / `<@W…>` (optional `\|fallback`) | `[UW][A-Z0-9]{2,}` |
| `<#C…>` (optional `\|name`) | `C[A-Z0-9]{2,}` |
| `<!subteam^S…>` (optional `\|handle`) | `S[A-Z0-9]{2,}` |
| `<!date^TS^tokens[^link]\|fallback>` | `\d{1,15}` timestamp |

**NOT trusted** even with `PreserveMentionTokens=true`: `<!channel>`,
`<!here>`, `<!everyone>`, URL-form tokens (`<https://…|label>`), or
anything with whitespace inside the brackets, lowercase IDs, or `<`/
`>`/`&`/`|` inside the fallback body. Those still get
`entityEscape`-d unless `AllowBroadcasts=true`.

If you add a new trusted shape, update both the regex in
`internal/converter/mention_tokens.go::trustedSlackToken` and the
conformance suites
(`TestSanitization_BroadcastForms_WithPreserveMentionTokens` plus
`TestPreserveTokens_AdversarialInputs_NotPromoted`).

## How to forget this (and why you must not)

1. Adding a new code path that builds a text element directly:
   ```go
   // BAD — bypasses the inline pipeline
   slack.NewRichTextSectionTextElement(userInput, nil)
   ```
   Either route the text through `renderInlinesWithOpts` so it picks
   up the pipeline, or call `entityEscape` explicitly.

2. Setting `AllowBroadcasts: true` as a default. Don't. The default is
   `false` precisely because LLM-generated content is the threat model.

3. Adding a new emission path (e.g. a new block type in
   `internal/server/`) without running its text content through the
   sanitizer. The conformance suite in
   `internal/converter/mentions_test.go` covers the existing paths;
   add cases there for any new path.

## Tests

Whenever you touch this rule's domain, ensure the conformance suite
still covers all six broadcast / mention forms. The default-off suite
is `TestSanitization_BroadcastForms_AllEscapedByDefault` in
`internal/converter/mentions_test.go`. The
`PreserveMentionTokens=true` parallel is
`TestSanitization_BroadcastForms_WithPreserveMentionTokens` and asserts
that broadcasts still escape while typed mentions promote. The
adversarial-input table in
`TestPreserveTokens_AdversarialInputs_NotPromoted` (in
`mention_tokens_test.go`) covers attempts to bypass via lowercase IDs,
fallback smuggling, whitespace, and URL-form. Subtests:

- `!channel`, `!here`, `!everyone`
- `user mention` (`<@U012AB3CD>`)
- `channel reference` (`<#C123ABC456>`)
- `subteam` (`<!subteam^S012ABC>`)
- `nested angle brackets`
- `ampersand alone`

If you change behavior here, update the suite first, then the code.
