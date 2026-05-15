# Slack Block Kit conversion cheat sheet

This MCP server converts AI-generated Markdown into Slack Block Kit JSON.
Read this before calling `convert_markdown_to_block_kit` so you pass the
right `mode` and options.

## Conversion modes

| `mode` | Behavior |
|---|---|
| `auto` (default) | Picks per input: a single Slack `markdown` block for short, simple output; full `rich_text` decomposition otherwise. |
| `rich_text` | Always decomposes into `rich_text` / `section` / `header` / `image` / `divider` / `table` blocks. |
| `markdown_block` | Always emits one Slack `markdown` block. Errors if input exceeds 12,000 characters. |
| `section_mrkdwn` | Always emits `section` blocks with `mrkdwn` text (older shape). |

## Supported Markdown

Headings, bold, italic, strikethrough, inline code, fenced code blocks,
ordered/unordered lists (including nesting), block quotes, thematic breaks
(`---`), links `[text](url)`, images `![alt](url)`, GFM tables, task lists,
and `:emoji:` shortcodes. Bare URLs are auto-linked.

## Not supported

Footnotes and definition lists are not recognized (emitted as plain text).
Raw HTML is entity-escaped to literal text, not rendered. Rich-text link
elements have no tooltip/title field, so Markdown link titles are dropped
in `rich_text` mode (kept in `markdown_block` mode).

## Mention safety (important)

By default every literal `<!channel>`, `<!here>`, `<!everyone>`, `<@U…>`,
`<#C…>`, and `<!subteam^…>` in the input is HTML-entity-escaped so it
cannot broadcast or ping the workspace. Keep it that way for
LLM-generated content.

- `mention_map` — resolve bare `@handle` text to a Slack ID safely. The
  preferred way to produce real mentions.
- `preserve_mention_tokens` — let already-typed Slack tokens
  (`<@U…>`, `<#C…>`, `<!subteam^S…>`, `<!date^…>`) pass through while
  catastrophic broadcasts still escape. Use when the Markdown came from a
  trusted upstream Slack tool result.
- `allow_broadcasts` — disables all escaping. Only set this when the user
  explicitly intends to ping a channel.

## Slack-documented limits

| Constraint | Limit |
|---|---|
| Blocks per message | 50 |
| Blocks per modal / App Home tab | 100 |
| `section` text | 3,000 chars |
| `section` fields | 10 fields, 2,000 chars each |
| `header` text | 150 chars (plain_text only) |
| `image` alt_text / title | 2,000 chars |
| `image` URL | 3,000 chars |
| `context` block | 10 elements |
| `actions` block | 25 elements |
| Button text / value / url | 75 / 2,000 / 3,000 chars |
| `table` block | 100 rows, 20 columns, one table per message |
| `markdown` blocks (cumulative) | 12,000 chars |
| `block_id` | 255 chars |

## Companion tools

- `validate_block_kit` — check a payload against the limits above; pass
  `surface` (`message` / `modal` / `home`) to set the block ceiling.
- `lint_block_kit` — advisory near-limit and accessibility warnings.
- `split_blocks` — chunk an oversized payload to the 50-block limit.
- `preview_block_kit` — get a Block Kit Builder URL for visual QA.
- `block_kit_to_markdown` — the inverse conversion (lossy).
