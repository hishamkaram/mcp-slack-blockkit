# Architecture reference (lazy-loaded by CLAUDE.md)

This file is imported from the project [`CLAUDE.md`](../CLAUDE.md) via the
`@docs/CLAUDE-architecture.md` directive. Lazy-loading means it's only
pulled into Claude Code's context when relevant — it doesn't count against
the main CLAUDE.md's <200-line budget.

## Package responsibilities

### `internal/converter/`

Walks goldmark's AST and emits `[]slack.Block`. Files:

- `options.go` — `Options` struct + `DefaultOptions()` + `validate()`.
  Single source of truth for tunables (Mode, BlockIDPrefix, MaxInputBytes,
  AllowBroadcasts, MentionMap, EnableTables, etc.).
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
- `markdown_block.go` — auto-mode picker (`shouldUseMarkdownBlock`) +
  `emitMarkdownBlock` (single Slack `markdown` block, ≤12k chars).

### `internal/validator/`

Hand-rolled validator (no external dep — `go-playground/validator` was
considered but not adopted; the constraint set is small enough that
struct-tag indirection adds more reading cost than it saves).

- `validator.go` — single file. Six cross-block rules (50-block ceiling,
  unique block_id, multiple-tables rule, markdown-block 12k cumulative,
  per-block dispatch). Per-block validators for section/header/image/
  actions. `ValidateStrict()` adds deprecated-pattern flagging.

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
  registers all five tools, exposes `RunStdio(ctx)`.
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

### `cmd/mcp-slack-block-kit/`

Cobra entry point. `main.go` (root + version + slog setup), `server.go`
(default `server` subcommand → `internal/server.RunStdio`), `convert.go`
(CLI subcommand → `internal/converter` directly).

### `block_kit/`

Public Go library re-exports. External consumers get a single stable
import path: `github.com/hishamkaram/mcp-slack-block-kit/blockkit`. Type
aliases for `Converter`, `Options`, `Mode`, `Validator`, etc. Functions
delegate to `internal/`. Tests live in `block_kit_test` (external
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
| `<!channel>` literal text | text | entity-escaped (or passthrough w/ `AllowBroadcasts`) |
| `@alice` (in MentionMap) | text | `rich_text_section_user` element with U… ID |

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

- The 5-tool MCP surface (`convert_markdown_to_block_kit`,
  `validate_block_kit`, `preview_block_kit`, `lint_block_kit`,
  `split_blocks`). Adding tools is fine; renaming or removing breaks
  every existing client config.
- The `Options` struct's field names. They appear in user-facing tool
  schemas via the SDK's `jsonschema-go` reflection. Renaming a field
  silently breaks every caller.
- The MIT license + the BSD-2-Clause notice for `slack-go/slack` in
  `NOTICE`. License changes need maintainer + downstream consultation.
