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
1. visit()             // build raw elements from AST
2. applyMentionMap()   // intentional mentions → typed elements
3. resolveEmoji()      // :name: → emoji elements
4. sanitizeBroadcasts() // entity-escape any remaining stray <, >, &
```

**For the markdown_block path** (`emitMarkdownBlock` in
`markdown_block.go`), the same `entityEscape` runs over the input
unless `AllowBroadcasts` is true. Don't reintroduce the old
`basicEscapeForMarkdownBlock` stub — it was deleted in step 11 in favor
of the unified sanitizer.

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
still covers all six broadcast / mention forms. The suite is
`TestSanitization_BroadcastForms_AllEscapedByDefault` in
`internal/converter/mentions_test.go`. Subtests:

- `!channel`, `!here`, `!everyone`
- `user mention` (`<@U012AB3CD>`)
- `channel reference` (`<#C123ABC456>`)
- `subteam` (`<!subteam^S012ABC>`)
- `nested angle brackets`
- `ampersand alone`

If you change behavior here, update the suite first, then the code.
