# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.1](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed (BREAKING — pre-1.0; no installed binaries in the wild)
- Project renamed from `mcp-slack-blockkit` to `mcp-slack-block-kit` to match
  Slack's own product naming ("Block Kit" is two words). Affects the GitHub
  repo URL, Go module path (`github.com/hishamkaram/mcp-slack-block-kit`),
  binary name (`mcp-slack-block-kit`), public Go package
  (`block_kit`, in `block_kit/`), four MCP tool names
  (`convert_markdown_to_block_kit`, `validate_block_kit`,
  `preview_block_kit`, `lint_block_kit`; `split_blocks` unchanged), and the
  cosign release-signature identity regex. GitHub auto-redirects the old
  repo URL for ~12 months. External MCP client configs with
  `"command": "mcp-slack-blockkit"` need to update to the new binary name.

### Added
- `Renderer.ConvertWithWarnings(input) ([]slack.Block, []string, error)` —
  the full converter API, returning fallback warnings alongside blocks.
  `Renderer.Convert(input)` is now a thin wrapper that drops warnings,
  preserving the v0.1 signature.
- `MCP convert_markdown_to_block_kit` tool: response `warnings` field now
  surfaces converter-side fallback notes (auto-mode routing decisions etc.),
  not just chunker/preview notes.
- New nested-pattern detection in the auto-mode picker
  (`shouldUseMarkdownBlock`): code-in-blockquote, code-in-list,
  table-in-blockquote, table-in-list, list-in-blockquote. When detected,
  auto mode routes the input through rich_text decomposition (predictable
  visual outcome) rather than the `markdown` block path (Slack's rendering
  of these combinations is undocumented and unverified).
- `internal/converter/nested_test.go`: 6 patterns × 3 modes test matrix +
  cross-cutting ordered-list `Offset`-continuation test +
  `TestNested_PrintBuilderURLs` fixture that prints Block Kit Builder
  URLs for manual visual verification of each pattern × mode combination.

### Changed
- `internal/converter/blocks.go::handleBlockquote`: refactored from
  "single rich_text_quote with plain-text fallback for non-representable
  children" to "split-emit on encountering a code block, list, or table —
  the quote becomes a sequence of adjacent rich_text blocks with the
  inner block emitted between."
- `internal/converter/lists.go::handleList`: same refactor for list
  items containing code blocks or tables. Ordered lists set `Offset` on
  the post-split sibling list so numbering continues
  (`Offset = N → first number = N+1`).

### Fixed
- Code blocks, lists, and tables nested inside blockquotes are no longer
  silently flattened to plain text in `rich_text` mode. They emit as
  proper standalone blocks adjacent to the quote.
- Code blocks and tables nested inside list items are no longer silently
  flattened to plain text. They emit as proper standalone blocks; the
  list splits with continued numbering for ordered lists.

---

## [0.1.0] - 2026-05-09

### Added
- Initial release.
- Five MCP tools registered on top of `modelcontextprotocol/go-sdk` v1.6.0:
  - `convert_markdown_to_block_kit` — markdown → Slack Block Kit, with
    auto-mode picker between the new `markdown` block (Feb 2025) and
    deterministic decomposition.
  - `validate_block_kit` — full Slack constraint validation with
    structured violations.
  - `preview_block_kit` — Block Kit Builder URL generation for one-click
    visual QA.
  - `lint_block_kit` — near-limit and deprecated-pattern warnings.
  - `split_blocks` — enforce 50-block-per-message + `only_one_table_allowed`.
- `convert` CLI subcommand for offline testing.
- `block_kit/` public Go library re-exports for embedded use.
- Mention-sanitization layer that entity-escapes broadcast strings
  (`<!channel>`, `<@U…>`, etc.) by default — opt-in passthrough via
  `AllowBroadcasts`.
- 293 tests with stdlib-fuzz-tested splitter; coverage ≥80% across all
  shipped packages.
- GoReleaser pipeline with multi-arch builds, cosign keyless signing,
  CycloneDX SBOMs, and Homebrew tap publishing.

### Security
- Mention sanitization is the documented security-critical path.
  See [SECURITY.md](SECURITY.md) and `.claude/rules/security.md`.

[Unreleased]: https://github.com/hishamkaram/mcp-slack-block-kit/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/hishamkaram/mcp-slack-block-kit/releases/tag/v0.1.0
