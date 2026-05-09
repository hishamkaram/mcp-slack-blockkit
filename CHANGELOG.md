# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.1](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- _your change here_

### Changed

### Deprecated

### Removed

### Fixed

### Security

---

## [0.1.0] - 2026-05-09

### Added
- Initial release.
- Five MCP tools registered on top of `modelcontextprotocol/go-sdk` v1.6.0:
  - `convert_markdown_to_blockkit` — markdown → Slack Block Kit, with
    auto-mode picker between the new `markdown` block (Feb 2025) and
    deterministic decomposition.
  - `validate_blockkit` — full Slack constraint validation with
    structured violations.
  - `preview_blockkit` — Block Kit Builder URL generation for one-click
    visual QA.
  - `lint_blockkit` — near-limit and deprecated-pattern warnings.
  - `split_blocks` — enforce 50-block-per-message + `only_one_table_allowed`.
- `convert` CLI subcommand for offline testing.
- `blockkit/` public Go library re-exports for embedded use.
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

[Unreleased]: https://github.com/hishamkaram/mcp-slack-blockkit/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/hishamkaram/mcp-slack-blockkit/releases/tag/v0.1.0
