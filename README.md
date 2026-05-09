# mcp-slack-blockkit

[![CI](https://img.shields.io/github/actions/workflow/status/hishamkaram/mcp-slack-blockkit/ci.yml?branch=main&label=ci)](https://github.com/hishamkaram/mcp-slack-blockkit/actions/workflows/ci.yml)
[![CodeQL](https://img.shields.io/github/actions/workflow/status/hishamkaram/mcp-slack-blockkit/codeql.yml?branch=main&label=codeql)](https://github.com/hishamkaram/mcp-slack-blockkit/actions/workflows/codeql.yml)
[![codecov](https://codecov.io/gh/hishamkaram/mcp-slack-blockkit/branch/main/graph/badge.svg)](https://codecov.io/gh/hishamkaram/mcp-slack-blockkit)
[![Go Reference](https://pkg.go.dev/badge/github.com/hishamkaram/mcp-slack-blockkit.svg)](https://pkg.go.dev/github.com/hishamkaram/mcp-slack-blockkit)
[![Go Report Card](https://goreportcard.com/badge/github.com/hishamkaram/mcp-slack-blockkit)](https://goreportcard.com/report/github.com/hishamkaram/mcp-slack-blockkit)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/hishamkaram/mcp-slack-blockkit/badge)](https://scorecard.dev/viewer/?uri=github.com/hishamkaram/mcp-slack-blockkit)
[![License: MIT](https://img.shields.io/github/license/hishamkaram/mcp-slack-blockkit)](LICENSE)
[![Latest Release](https://img.shields.io/github/v/release/hishamkaram/mcp-slack-blockkit?sort=semver)](https://github.com/hishamkaram/mcp-slack-blockkit/releases/latest)
[![Cosign verified](https://img.shields.io/badge/cosign-verified-brightgreen?logo=sigstore)](https://github.com/hishamkaram/mcp-slack-blockkit/releases/latest)

> A single-binary [Model Context Protocol][mcp] server (and CLI) that
> converts AI-generated markdown into valid [Slack Block Kit][blockkit] JSON.
> Credential-free, runs over stdio, ships as a static Go binary.

[mcp]: https://modelcontextprotocol.io/
[blockkit]: https://docs.slack.dev/block-kit/

---

## What it does

Five MCP tools your AI assistant can call:

| Tool | What it does |
|---|---|
| **`convert_markdown_to_blockkit`** | Markdown → Block Kit JSON. Auto mode picks between Slack's new (Feb 2025) `markdown` block and full deterministic decomposition into `rich_text` / `section` / `header` / `image` / `divider`. |
| **`validate_blockkit`** | Validates a payload against the documented Slack constraints (per-block char limits, count limits, XOR rules, `only_one_table_allowed`, the 12k-char `markdown_block` cap, etc.) with structured violations + fix hints. |
| **`preview_blockkit`** | Returns a Block Kit Builder URL — one click to a live visual preview in Slack's own builder. No workspace credentials needed. |
| **`lint_blockkit`** | Warns on near-limit content, deprecated patterns, and accessibility gaps (e.g. missing image `alt_text`). Always advisory. |
| **`split_blocks`** | Splits an oversized payload into multiple Slack-API-compliant chunks on the >50-block axis, with `only_one_table_allowed` enforcement. |

Plus a **`convert` CLI** for offline testing without an MCP client.

**Security:** every text run is HTML-entity-escaped by default so
AI-generated content can't broadcast `<!channel>` / `<!here>` / `<@U…>`
to your workspace. See [SECURITY.md](SECURITY.md) for the full threat
model.

## Install

```sh
# Homebrew (macOS / Linux)
brew install hishamkaram/tap/mcp-slack-blockkit

# Go install
go install github.com/hishamkaram/mcp-slack-blockkit/cmd/mcp-slack-blockkit@latest

# Or grab a prebuilt binary from Releases
# https://github.com/hishamkaram/mcp-slack-blockkit/releases/latest
```

Verify a release with [cosign](https://docs.sigstore.dev/cosign/overview/):

```sh
cosign verify-blob \
  --certificate-identity-regexp 'https://github\.com/hishamkaram/mcp-slack-blockkit/.+' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  --certificate checksums.txt.pem --signature checksums.txt.sig \
  checksums.txt
```

## Use it from Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`
(macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "blockkit": {
      "command": "mcp-slack-blockkit",
      "args": []
    }
  }
}
```

The same shape works in **Cursor**, **Continue.dev**, **Zed**, **Cline**,
and any other MCP-compatible client that supports the stdio transport.

## Use it from the CLI

```sh
echo '# Hello

A paragraph with **bold**, *italic*, `code`, and a [link](https://example.com).

- list item 1
- list item 2 with :wave:

```go
func main() {}
```' | mcp-slack-blockkit convert --mode rich_text --pretty
```

Add `--preview` to also get a Block Kit Builder URL on stderr (stdout
stays JSON-only so you can pipe straight into `jq` or `chat.postMessage`).

```sh
mcp-slack-blockkit convert --help
```

## Use it from Go

```go
import "github.com/hishamkaram/mcp-slack-blockkit/blockkit"

r, err := blockkit.NewConverter(blockkit.DefaultOptions())
if err != nil { panic(err) }

blocks, err := r.Convert("# Title\n\nbody **bold** text.")
if err != nil { panic(err) }

// Validate before sending:
result := blockkit.NewValidator().Validate(blocks)
if !result.Valid {
    for _, e := range result.Errors {
        fmt.Println(e.Path, e.Code, e.Message)
    }
}

// Visual QA via Block Kit Builder:
pr, _ := blockkit.PreviewURL(blocks)
fmt.Println("preview:", pr.URL)
```

Full API reference: [pkg.go.dev](https://pkg.go.dev/github.com/hishamkaram/mcp-slack-blockkit/blockkit).

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

[MIT](LICENSE) © 2026 Hesham Waleed Karam.
[Third-party notices](NOTICE).

---

> **Note**: some badges may take 24–48 hours to populate after the
> first release (Codecov, OSSF Scorecard, Go Report Card all crawl
> on their own schedules).
