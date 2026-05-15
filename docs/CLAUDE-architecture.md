# Architecture reference (lazy-loaded by CLAUDE.md)

This file is imported from the project [`CLAUDE.md`](../CLAUDE.md) via the
`@docs/CLAUDE-architecture.md` directive. Lazy-loading means it's only
pulled into Claude Code's context when relevant — it doesn't count against
the main CLAUDE.md's <200-line budget.

## Package responsibilities

### `internal/converter/`

Walks goldmark's AST and emits `[]slack.Block`. Files:

- `options.go` — `Options` struct + `DefaultOptions()` + `validate()`.
  Single source of truth for tunables (Mode, BlockIDPrefix,
  MaxInputBytes, AllowBroadcasts, PreserveMentionTokens, MentionMap,
  EnableTables, etc.).
- `renderer.go` — `Renderer` struct + `New(opts) → *Renderer` + the
  top-level `Convert(input) → []slack.Block` mode-dispatching entry point.
  Reuses one `goldmark.Markdown` per Renderer (safe per goldmark docs).
- `blocks.go` — block-level handlers (paragraph, heading, divider,
  blockquote, image). Includes the `walker` struct holding per-call
  state (source bytes, blockID counter, accumulating blocks slice).
- `lists.go` — list flattener with the
  nested-as-sibling-with-incrementing-`indent` pattern that Slack
  requires (rich_text_list children of children are rejected).
- `inlines.go` — inline visitor with the style stack (push on
  Emphasis/Strikethrough/CodeSpan, pop on exit, OR-merge for
  `***bold-italic***`).
- `code.go` — fenced + indented code blocks → `rich_text_preformatted`.
- `tables.go` — GFM table → `slack.TableBlock` with row/col truncation +
  header replication on overflow.
- `emoji.go` — post-processor that merges fragmented text (goldmark
  splits at `_`) and extracts `:name:` shortcodes with non-digit boundary
  filtering (kills `19:49:41` false positives).
- `mentions.go` — entity-escape sanitizer + `MentionMap` `@handle`
  resolver. `applyMentionMap` runs before `resolveEmoji` runs before
  `sanitizeBroadcasts`.
- `mention_tokens.go` — `trustedSlackToken` regex + `expandTrustedMentions`
  (rich_text path) and `escapePreservingTokens` (markdown_block path).
  Gated on `Options.PreserveMentionTokens`. Promotes already-typed
  `<@U…>`/`<#C…>`/`<!subteam^S…>`/`<!date^…|fb>` to typed elements
  while catastrophic broadcasts still escape.
- `markdown_block.go` — auto-mode picker (`shouldUseMarkdownBlock`) +
  `emitMarkdownBlock` (single Slack `markdown` block, ≤12k chars).
- `markdown_block_emit.go` — AST-driven walker that produces the
  CommonMark text payload for the `markdown` block. Re-emits each AST
  node as Slack-supported syntax: `[text](url)` for links (including
  promoted autolinks and Linkify-detected bare URLs, since `<url>` is
  not documented as supported by Slack's markdown block); HTML-entity
  escape for text content (broadcast safety); raw passthrough for code
  spans and fenced code blocks. Replaced the previous "blanket
  `entityEscape` over raw input" approach, which corrupted CommonMark
  autolink syntax.
- `slack_mrkdwn_links.go` — pre-parse regex rewriter that converts
  Slack's mrkdwn `<URL|label>` URL-form (emitted by Slack tool results;
  not recognized by goldmark, not supported by the new `markdown`
  block) into CommonMark `[label](URL)`. Runs once in
  `renderer.go::ConvertWithWarnings` before goldmark sees the input,
  so both modes benefit. Known limitation: not AST-aware at pre-parse
  time, so `<url|label>` inside a code span is still rewritten.

### `internal/converter/` (cont.)

- `options.go` also carries `MaxNestingDepth` (default 100).
  `ConvertWithWarnings` runs an iterative DFS (`maxASTDepth` in
  `renderer.go`) after the parse and rejects over-depth input with
  `ErrInputTooDeeplyNested` — `MaxInputBytes` bounds bytes, not the
  structural depth that drives the recursive block/inline walkers.

### `internal/reverse/`

The inverse of the converter: `ToMarkdown([]slack.Block) → (markdown,
warnings, error)`. Best-effort and lossy — Block Kit can express
styling and interactive elements (buttons, accessories, colors) with no
Markdown equivalent. Every lossy decision is recorded in `warnings`;
warnings are deduplicated by message. No dependency on the converter.
Wired into the `block_kit_to_markdown` MCP tool and re-exported as
`block_kit.BlockKitToMarkdown`.

### `internal/validator/`

Hand-rolled validator (no external dep — `go-playground/validator` was
considered but not adopted; the constraint set is small enough that
struct-tag indirection adds more reading cost than it saves).

- `validator.go` — single file. Cross-block rules (50/100-block ceiling,
  unique block_id, multiple-tables rule, markdown-block 12k cumulative,
  per-block dispatch). Per-block validators for section (incl.
  accessory), header, image (incl. title), actions/buttons, context,
  and table. `Validate` defaults to the 50-block message ceiling;
  `ValidateForSurface(blocks, Surface)` raises it to 100 for
  `SurfaceModal` / `SurfaceHomeTab`. `NewStrict()` adds
  deprecated-pattern flagging.

### `internal/splitter/`

Two pure functions, no dep on the converter.

- `splitter.go` — `SplitText(s, max, margin)` whitespace-aware splitter.
  Prefers paragraph > sentence > word boundaries. Round-trip preserves
  content byte-for-byte (fuzz-tested).
- `chunks.go` — `ChunkBlocks(blocks, maxPerChunk)` enforces the
  50-block-per-message rule + `only_one_table_allowed` (a TableBlock
  always opens a new chunk when the current chunk already has one).

### `internal/preview/`

- `preview.go` — `BuilderURL(blocks) → Result` produces
  `https://app.slack.com/block-kit-builder/#<URL_ENCODED_JSON>`. Marks
  URLs above ~8KB as `Truncated: true` (browsers/Slack get unreliable).

### `internal/server/`

Adapters — no markdown parsing or block construction here. Each tool
file is a small handler that translates between MCP wire types and the
internal packages.

- `server.go` — `New(version) → *Server` constructs the MCP server,
  registers all six tools plus the cheat-sheet resource and the
  `format_for_slack` prompt, exposes `RunStdio(ctx)`. All tools carry
  read-only MCP annotations via `readOnlyToolAnnotations`.
- `http.go` — `RunHTTP` / `RunSSE`. Shares one `*mcp.Server` across
  sessions (idiomatic per SDK docs). Hardened wrapping `http.Server`
  (ReadHeaderTimeout, IdleTimeout, MaxHeaderBytes, MaxBytesHandler, no
  WriteTimeout). Graceful shutdown via `RegisterOnShutdown` →
  `session.Close()` for every active session. Optional bearer-token
  middleware via `HTTPOptions{Token}` with constant-time compare.
- `convert_tool.go` — `convert_markdown_to_block_kit`. Wires
  converter + splitter + preview into a single response.
- `validate_tool.go` — `validate_block_kit`. Includes the shared
  `decodeBlocksInput` helper that handles both blocks-form and
  payload-form input via `slack.Blocks`'s per-element UnmarshalJSON.
- `preview_tool.go` — `preview_block_kit`. Thin wrapper over
  `internal/preview`.
- `lint_tool.go` — `lint_block_kit`. Always advisory (only warnings).
  Configurable thresholds (default 90%) for near-limit checks.
- `split_tool.go` — `split_blocks`. Thin wrapper over
  `internal/splitter.ChunkBlocks`.
- `reverse_tool.go` — `block_kit_to_markdown`. Thin wrapper over
  `internal/reverse.ToMarkdown`; reuses `decodeBlocksInput`.
- `resources.go` — registers the `block-kit-cheatsheet` MCP resource;
  content embedded from `cheatsheet.md` via `//go:embed`.
- `prompts.go` — registers the `format_for_slack` MCP prompt.

### `cmd/mcp-slack-block-kit/`

Cobra entry point. `main.go` (root + version + slog setup), `server.go`
(default `server` subcommand: dispatches to `RunStdio` / `RunHTTP` /
`RunSSE` based on `--http-addr` / `--sse-addr`; mutual-exclusion
validated; `--http-token` flag with `MCPSBK_HTTP_TOKEN` env fallback),
`convert.go` (CLI subcommand → `internal/converter` directly).

### `block_kit/`

Public Go library re-exports. External consumers get a single stable
import path: `github.com/hishamkaram/mcp-slack-block-kit/block_kit`. Type
aliases for `Converter`, `Options`, `Mode`, `Validator`, `Server`,
`HTTPOptions`, etc. Functions delegate to `internal/`. `server.go` adds
`NewServer` + `RunStdio` / `RunHTTP` / `RunSSE` so embedders can run the
MCP server in their own binary. Tests live in `block_kit_test` (external
package) so any leak of internal-only behavior fails compilation here.

## AST → Block Kit mapping

See [research.md §4](internal/research.md) for the full table. Quick
reference for the high-traffic cases:

| Markdown | goldmark | Block Kit output |
|---|---|---|
| Paragraph (text only) | `*ast.Paragraph` | `rich_text` block with one `rich_text_section` |
| Paragraph (only image child) | `*ast.Paragraph` w/ `*ast.Image` | `image` block |
| `# H1` (≤150 chars, no inline-formatting children) | `*ast.Heading` level 1 | `header` block (plain_text) |
| `# H1` (long or has links/images/code) | `*ast.Heading` level 1 | bold `section` (mrkdwn fallback) |
| `## H2`–`###### H6` | `*ast.Heading` level 2–6 | bold `section` (always) |
| `---` / `***` / `___` | `*ast.ThematicBreak` | `divider` |
| `> quote` | `*ast.Blockquote` | `rich_text` block w/ `rich_text_quote` |
| `- a\n- b` | `*ast.List` | `rich_text_list` (style: bullet) |
| Nested list | `*ast.List` w/ child `*ast.List` | sibling `rich_text_list` w/ `indent + 1` |
| ` ```go\n…\n``` ` | `*ast.FencedCodeBlock` | `rich_text_preformatted` (language preserved) |
| GFM table | `extast.Table` (auto mode) | single `markdown` block |
| GFM table (rich_text mode or large) | `extast.Table` | native `slack.TableBlock` |
| `:wave:` | text after merge | `rich_text_section_emoji` element |
| `<!channel>` / `<!here>` / `<!everyone>` literal text | text | entity-escaped (passthrough only w/ `AllowBroadcasts`, never with `PreserveMentionTokens` alone) |
| `<@U…>` / `<#C…>` / `<!subteam^S…>` / `<!date^…\|fb>` literal text | text | entity-escaped by default; typed `rich_text_section_user` / `_channel` / `_usergroup` / `_date` w/ `PreserveMentionTokens` |
| `@alice` (in MentionMap) | text | `rich_text_section_user` element with U… ID |
| `[text](url)` | `*ast.Link` | rich_text: `rich_text_section_link`. markdown_block: pass-through `[text](url)`. URL bytes preserved verbatim in both. A Markdown link *title* (`[t](u "title")`) is dropped in rich_text mode — slack-go's `RichTextSectionLinkElement` has no title field, and Slack's schema documents none; it survives in markdown_block mode. |
| `<https://x.com>` / `<u@x.com>` | `*ast.AutoLink` | rich_text: `rich_text_section_link` (email gets `mailto:` prefix). markdown_block: re-emitted as `[url](url)` / `[email](mailto:email)` because `<url>` is not documented as supported by Slack's markdown block. |
| bare URL in prose | `*ast.AutoLink` (via GFM Linkify) | Same as `<https://x.com>` — Linkify produces the same AST node as an explicit autolink, so both paths handle them identically. |
| `<URL\|label>` (Slack mrkdwn URL-form) | unrecognized by goldmark | Pre-parse `rewriteSlackURLForms` converts to CommonMark `[label](URL)` before goldmark sees it; both modes then handle as a regular Link. Slack's `markdown` block does NOT accept `<URL\|label>` natively (that form is mrkdwn-only). |

## Splitter rules

- **Paragraph splitter** (`SplitText`): only fires for text payloads
  bound to `section.text` (3000-char) or `markdown` block (12k cumulative).
  rich_text has no documented per-string limit, so we don't split it.
- **Block chunker** (`ChunkBlocks`): enforces 50-block per-message
  ceiling AND opens a new chunk before any second `TableBlock`
  (Slack's `only_one_table_allowed`). Greedy walk; preserves order.

## Nested-element handling (split-emit)

Slack's rich_text element schema is restrictive about what containers can
hold. Verified against the official spec, node-slack-sdk types,
slack-go/slack source, and python-slack-sdk:

| Container | Allowed direct children |
|---|---|
| `rich_text` (block) | `rich_text_section`, `rich_text_list`, `rich_text_preformatted`, `rich_text_quote` (any combination, any order) |
| `rich_text_section.elements` | inline-only (text, link, user, channel, usergroup, team, emoji, broadcast, color, date) |
| `rich_text_list.elements` | `rich_text_section` items only |
| `rich_text_preformatted.elements` | `text` or `link` only — no emphasis |
| `rich_text_quote.elements` | inline-only (same union as section) |

So CommonMark patterns like "code block inside a blockquote" or "table
inside a list item" are **not representable** as nested children in
rich_text. We handle them in two layers:

1. **Auto-mode picker** (`internal/converter/markdown_block.go::shouldUseMarkdownBlock`)
   walks the AST and detects five non-representable nesting patterns:
   - code-in-blockquote
   - code-in-list
   - table-in-blockquote
   - table-in-list
   - list-in-blockquote

   When any is detected, the picker returns `false` so the input flows
   through rich_text decomposition (Layer 2). Slack's `markdown` block
   *might* render these combinations correctly, but the docs only
   document features individually — the combination behavior is
   UNVERIFIED. Routing through rich_text gives a predictable visual
   outcome instead of betting on Slack's parser.

2. **rich_text decomposition (split-emit)** in
   `internal/converter/blocks.go::handleBlockquote` and
   `internal/converter/lists.go::handleList`. When the handler hits a
   non-representable child, it flushes the in-progress
   quote/list as its own top-level rich_text block, dispatches the inner
   block via the walker (which emits a separate top-level block), then
   opens a new sibling quote/list for the remaining children. Ordered
   lists set `Offset` on the post-split sibling so numbering continues
   (Slack: `Offset = N → first number = N+1`).

When auto mode triggers Layer 2 because of a detected pattern,
`Renderer.ConvertWithWarnings` emits one warning naming the patterns so
the MCP caller can flag the visual-fidelity tradeoff. Explicit
`mode=rich_text` callers get no warning — they opted in.

### Visual-rendering caveats (SCHEMA-ONLY / UNVERIFIED)

Three claims in the design are derived from typed schemas and spec
language but lack empirical screenshot verification:

- **Sibling rich_text decomposition rendering**: emitting
  `[rich_text(quote_prefix), rich_text(preformatted), rich_text(quote_suffix)]`
  as adjacent top-level blocks. The quote-bar visually does NOT span
  the embedded code; readers see three stacked decorations. This is
  derived from the slack-go canonical fixture; the visual claim itself
  is unscreenshotted.
- **Cross-block ordered-list numbering continuation via `Offset`**:
  spec defines `Offset` as a single-list starting-number control; the
  cross-block visual continuity is not documented but follows from the
  off-by-one math.
- **`markdown` block rendering of code-in-quote / list-in-quote /
  code-in-list**: not documented for combinations.

The test suite includes `TestNested_PrintBuilderURLs` which prints a
Block Kit Builder URL for each pattern × mode. A maintainer with a
Slack workspace can click through and verify visually. Run with
`go test -v -run TestNested_PrintBuilderURLs ./internal/converter/`.

## Things to never silently change

- The 6-tool MCP surface (`convert_markdown_to_block_kit`,
  `block_kit_to_markdown`, `validate_block_kit`, `preview_block_kit`,
  `lint_block_kit`, `split_blocks`). Adding tools is fine; renaming or
  removing breaks every existing client config. The `block-kit-cheatsheet`
  resource URI (`slackblockkit://reference/cheatsheet`) and the
  `format_for_slack` prompt name are part of the same contract.
- The `Options` struct's field names. They appear in user-facing tool
  schemas via the SDK's `jsonschema-go` reflection. Renaming a field
  silently breaks every caller.
- The MIT license + the BSD-2-Clause notice for `slack-go/slack` in
  `NOTICE`. License changes need maintainer + downstream consultation.
