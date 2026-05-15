# mcp-slack-block-kit

[![CI](https://img.shields.io/github/actions/workflow/status/hishamkaram/mcp-slack-block-kit/ci.yml?branch=main&label=ci)](https://github.com/hishamkaram/mcp-slack-block-kit/actions/workflows/ci.yml)
[![CodeQL](https://img.shields.io/github/actions/workflow/status/hishamkaram/mcp-slack-block-kit/codeql.yml?branch=main&label=codeql)](https://github.com/hishamkaram/mcp-slack-block-kit/actions/workflows/codeql.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/hishamkaram/mcp-slack-block-kit.svg)](https://pkg.go.dev/github.com/hishamkaram/mcp-slack-block-kit)
[![Go Report Card](https://goreportcard.com/badge/github.com/hishamkaram/mcp-slack-block-kit)](https://goreportcard.com/report/github.com/hishamkaram/mcp-slack-block-kit)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/hishamkaram/mcp-slack-block-kit/badge)](https://scorecard.dev/viewer/?uri=github.com/hishamkaram/mcp-slack-block-kit)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/12792/badge)](https://www.bestpractices.dev/projects/12792)
[![License: MIT](https://img.shields.io/github/license/hishamkaram/mcp-slack-block-kit)](LICENSE)
[![Latest Release](https://img.shields.io/github/v/release/hishamkaram/mcp-slack-block-kit?sort=semver)](https://github.com/hishamkaram/mcp-slack-block-kit/releases/latest)
[![Cosign verified](https://img.shields.io/badge/cosign-verified-brightgreen?logo=sigstore)](https://github.com/hishamkaram/mcp-slack-block-kit/releases/latest)

> A single-binary [Model Context Protocol][mcp] server (and CLI) that
> converts AI-generated markdown into valid [Slack Block Kit][block-kit] JSON.
> Credential-free, ships as a static Go binary, speaks stdio / streamable
> HTTP / SSE.

[mcp]: https://modelcontextprotocol.io/
[block-kit]: https://docs.slack.dev/block-kit/

---

## What it does

Six MCP tools your AI assistant can call:

| Tool | What it does |
|---|---|
| **`convert_markdown_to_block_kit`** | Markdown → Block Kit JSON. Auto mode picks between Slack's new (Feb 2025) `markdown` block and full deterministic decomposition into `rich_text` / `section` / `header` / `image` / `divider` / `table`. |
| **`block_kit_to_markdown`** | The inverse — Block Kit JSON → Markdown. Best-effort and lossy; constructs with no Markdown equivalent (buttons, accessories, colors) are approximated and reported in `warnings`. |
| **`validate_block_kit`** | Validates a payload against the documented Slack constraints (per-block char limits, count limits, button/context/table element limits, XOR rules, `only_one_table_allowed`, the 12k-char `markdown_block` cap, etc.) with structured violations + fix hints. Pass `surface` (`message` / `modal` / `home`) to set the block ceiling. |
| **`preview_block_kit`** | Returns a Block Kit Builder URL — one click to a live visual preview in Slack's own builder. No workspace credentials needed. |
| **`lint_block_kit`** | Warns on near-limit content, deprecated patterns, and accessibility gaps (e.g. missing image `alt_text`). Always advisory. |
| **`split_blocks`** | Splits an oversized payload into multiple Slack-API-compliant chunks on the >50-block axis, with `only_one_table_allowed` enforcement. |

The server also exposes an MCP **resource** (`block-kit-cheatsheet` — the
conversion modes, supported Markdown, Slack limits, and mention-safety
model) and a **prompt** (`format_for_slack`) so MCP clients can discover
how to use the tools.

Plus a **`convert` CLI** for offline testing without an MCP client.

### Conversion modes

`convert_markdown_to_block_kit` accepts a `mode` parameter (CLI: `--mode`):

| Mode | What it produces | When to use |
|---|---|---|
| **`auto`** (default) | One Slack `markdown` block when the input is short, image-free, and contains no nested-block patterns. Otherwise full `rich_text` decomposition. | Most LLM workflows — let the converter pick. |
| **`rich_text`** | Always full decomposition into typed `rich_text` / `section` / `header` / `image` / `divider` / `table` blocks. | When you want explicit, deterministic block shapes (e.g. for downstream styling, validation, or because you don't want to delegate rendering to Slack's `markdown` parser). |
| **`markdown_block`** | Single Slack `markdown` block — Slack's server-side parser owns the rendering. | When the input is known-good markdown and you want the smallest possible payload. Errors if input >12,000 chars. |
| **`section_mrkdwn`** | `section` blocks with `mrkdwn` text. | Downstream consumers that need the older `section`-based shape. |

### Supported Markdown

Headings, bold, italic, strikethrough, inline code, fenced code blocks,
ordered/unordered lists (including nesting), block quotes, thematic
breaks, links, images, GFM tables, task lists, and `:emoji:` shortcodes;
bare URLs are auto-linked. **Not** supported: footnotes and definition
lists (emitted as plain text); raw HTML is entity-escaped to literal
text. In `rich_text` mode, Markdown link *titles* are dropped — Slack's
rich-text link element has no title field (they survive in
`markdown_block` mode).

### Nested elements

Slack's `rich_text` element schema doesn't allow code blocks, lists, or
tables inside a `rich_text_quote` or `rich_text_list` (those containers
take inline elements only). When markdown contains one of these
patterns:

- code in a blockquote / list item
- table in a blockquote / list item
- list in a blockquote

…the converter **decomposes** the construct into adjacent top-level blocks
rather than silently flattening to plain text. Ordered lists set `Offset`
on the post-split sibling so numbering continues across the gap. In `auto`
mode this triggers a one-line warning in the response so callers know the
visual rendering won't look exactly like CommonMark embedding — the inner
block is *adjacent* to the quote, not visually nested inside it.

### Security

Every text run is HTML-entity-escaped by default so AI-generated content
can't broadcast `<!channel>` / `<!here>` / `<@U…>` to your workspace.
Two narrowing knobs:

- `preserve_mention_tokens: true` — already-typed Slack mention tokens
  (`<@U…>`, `<#C…>`, `<!subteam^S…>`, `<!date^…|fallback>`) pass through
  as typed elements, while catastrophic broadcasts (`<!channel>`,
  `<!here>`, `<!everyone>`) **still escape**. Use this when the markdown
  comes from an upstream Slack tool result (e.g. `get_slack_user_info`).
- `allow_broadcasts: true` — disables sanitization entirely. Don't use
  this unless the input is fully trusted.

See [SECURITY.md](SECURITY.md) for the full threat model.

## Install

```sh
# Go install
go install github.com/hishamkaram/mcp-slack-block-kit/cmd/mcp-slack-block-kit@latest

# Or grab a prebuilt binary from Releases (signed via cosign keyless)
# https://github.com/hishamkaram/mcp-slack-block-kit/releases/latest
```

> A Homebrew tap (`brew install hishamkaram/tap/mcp-slack-block-kit`)
> is planned; tap publishing is currently disabled in the GoReleaser
> config pending tap-repo + publishing-PAT setup.

Verify a release with [cosign](https://docs.sigstore.dev/cosign/overview/):

```sh
cosign verify-blob \
  --certificate-identity-regexp 'https://github\.com/hishamkaram/mcp-slack-block-kit/.+' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  --certificate checksums.txt.pem --signature checksums.txt.sig \
  checksums.txt
```

Release tags are SSH-signed (ed25519); GitHub displays a green
"Verified" badge on each tag page (e.g.
[v0.2.0](https://github.com/hishamkaram/mcp-slack-block-kit/releases/tag/v0.2.0)).

## Use it from Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`
(macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "block_kit": {
      "command": "mcp-slack-block-kit",
      "args": []
    }
  }
}
```

The same shape works in **Cursor**, **Continue.dev**, **Zed**, **Cline**,
and any other MCP-compatible client that supports the stdio transport.

### Transports

The default `mcp-slack-block-kit` (or `mcp-slack-block-kit server`)
invocation runs the **stdio** transport — what Claude Desktop, Cursor,
and Continue.dev launch. For HTTP-based MCP clients (remote runners,
multi-tenant hosts, containerized deployments), two HTTP transports are
also available via flags. Both default to localhost-only binds and the
SDK's DNS-rebinding protection stays on:

```sh
# Streamable HTTP (spec 2025-03 — preferred for new MCP clients):
mcp-slack-block-kit server --http-addr 127.0.0.1:7777

# Legacy Server-Sent Events (spec 2024-11 — older clients):
mcp-slack-block-kit server --sse-addr 127.0.0.1:7778

# With a bearer token (applies to both --http-addr and --sse-addr):
mcp-slack-block-kit server --http-addr 127.0.0.1:7777 --http-token s3cret
# The token can also come from the MCPSBK_HTTP_TOKEN environment variable.
```

When auth is enabled the server requires
`Authorization: Bearer <token>` on every incoming request and replies
with `401 Unauthorized` otherwise. `--http-addr` and `--sse-addr` are
mutually exclusive. For exposure beyond localhost, run behind a reverse
proxy that terminates TLS and enforces auth — the binary intentionally
ships without built-in TLS.

### Library usage (embed the server in your own binary)

External Go consumers can import `block_kit` and embed the MCP server
the same way they embed the converter:

```go
import "github.com/hishamkaram/mcp-slack-block-kit/block_kit"

srv, _ := block_kit.NewServer("v1.2.3")
// Stdio (matches the default binary launch):
_ = block_kit.RunStdio(ctx, srv)
// Or streamable HTTP with optional bearer auth:
_ = block_kit.RunHTTP(ctx, srv, "127.0.0.1:7777",
    block_kit.HTTPOptions{Token: "s3cret"})
```

## Use it from the CLI

````sh
cat <<'EOF' | mcp-slack-block-kit convert --mode rich_text --pretty
# Hello

A paragraph with **bold**, *italic*, `code`, and a [link](https://example.com).

- list item 1
- list item 2 with :wave:

```go
func main() {}
```
EOF
````

Stdout receives the Block Kit JSON only — pipe straight into `jq` or
`chat.postMessage`. Stderr carries logs and the optional `--preview`
Block Kit Builder URL:

```sh
echo '# title' | mcp-slack-block-kit convert --preview
# stdout: {"blocks":[{"type":"header",...}]}
# stderr: preview: https://app.slack.com/block-kit-builder/#%7B...%7D
```

Other useful flags: `--mode={auto|rich_text|markdown_block|section_mrkdwn}`,
`--allow-broadcasts`, `--preserve-mention-tokens`,
`--block-id-prefix=<str>`, `--max-input-bytes=<n>`, `--pretty`. Full
help: `mcp-slack-block-kit convert --help`.

## Use it from Go

```go
import "github.com/hishamkaram/mcp-slack-block-kit/block_kit"

r, err := block_kit.NewConverter(block_kit.DefaultOptions())
if err != nil { panic(err) }

// ConvertWithWarnings returns blocks plus any fallback notes (e.g. when
// auto mode routed away from markdown_block because the input contains
// code-in-blockquote). Use Convert() if you want to drop warnings.
blocks, warnings, err := r.ConvertWithWarnings("# Title\n\nbody **bold** text.")
if err != nil { panic(err) }
for _, w := range warnings {
    log.Printf("converter warning: %s", w)
}

// Validate before sending:
result := block_kit.NewValidator().Validate(blocks)
if !result.Valid {
    for _, e := range result.Errors {
        fmt.Println(e.Path, e.Code, e.Message)
    }
}

// Visual QA via Block Kit Builder:
pr, _ := block_kit.PreviewURL(blocks)
fmt.Println("preview:", pr.URL)
```

Full API reference: [pkg.go.dev](https://pkg.go.dev/github.com/hishamkaram/mcp-slack-block-kit/block_kit).

## Why this and not...

| Project | What we share | What's different |
|---|---|---|
| [`navidemad/md2slack`][md2] | goldmark-based markdown→Block Kit | Library only, no MCP server, no validation/preview/lint, hardcoded block-id prefix. We re-implemented the patterns rather than depending on it. |
| [`takara2314/slack-go-util`][slgu] | Same shape | Library only; missing tables, hr, images, strike, autolinks. |
| [`tryfabric/mack`][mack] | TS markdown → Block Kit | TS, last commit 2022 (stale). Ours: Go static binary, MCP-native. |
| [Other Slack MCP servers][servers] | Workspace integration | They send messages; we *generate* them. Pair with one of those for the full pipeline. |

[md2]: https://github.com/navidemad/md2slack
[slgu]: https://github.com/takara2314/slack-go-util
[mack]: https://github.com/tryfabric/mack
[servers]: https://registry.modelcontextprotocol.io/v0/servers?search=slack

## Contributing

Pull requests welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) first —
short version: Conventional Commits, ≥80% test coverage on changed
packages, `make setup` once after clone to wire the lefthook hooks.

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

[MIT](LICENSE) © 2026 Hesham Karm.
[Third-party notices](NOTICE).
