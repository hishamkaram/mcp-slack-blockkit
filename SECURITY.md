# Security policy

## Reporting a vulnerability

**Please do not open public GitHub issues for security vulnerabilities.**

Report privately through one of:

- GitHub Private Vulnerability Reporting (preferred):
  https://github.com/hishamkaram/mcp-slack-block-kit/security/advisories/new
- Email: hishamwaleedkaram@gmail.com (subject: `[mcp-slack-block-kit security]`)

The PVR form (Security tab → "Report a vulnerability") creates a
private advisory only the maintainer can see, with a built-in
discussion thread, CVE coordination, and a controlled disclosure
timeline.

If for some reason GitHub PVR isn't an option, email
[hishamwaleedkaram@gmail.com](mailto:hishamwaleedkaram@gmail.com?subject=%5Bmcp-slack-block-kit%20security%5D)
with subject line `[mcp-slack-block-kit security]`. Use
[age](https://age-encryption.org/) or PGP if your report contains
exploit details (key on request).

## Response timeline

- **Acknowledgement**: within **72 hours** of receipt.
- **Triage decision** (severity, in-scope or not): within **7 days**.
- **Patch released** for high/critical severity: within **30 days** of triage.
- **CVE coordination**: handled through GitHub's built-in process
  ([CVE program](https://docs.github.com/en/code-security/security-advisories/working-with-global-security-advisories-from-the-github-advisory-database/about-the-github-advisory-database#about-cve-identification-numbers)).

## Supported versions

Only the **latest minor version** receives security fixes. While the
project is in `0.x` (pre-1.0), any new minor release supersedes the
previous one. After `1.0.0`, the support window expands to the
current and immediately previous minor.

## Security-critical paths

For triage purposes, these are the parts of the codebase most likely
to be the locus of a real vulnerability:

### Mention sanitization (HIGH RISK)

`internal/converter/mentions.go` and the `internal/server/` tool
handlers MUST entity-escape `&`, `<`, `>` in every text run before it
lands inside a Slack `text` field, unless `Options.AllowBroadcasts`
is explicitly set to `true`.

A bypass here would let AI-generated content containing literal
`<!channel>`, `<!here>`, `<!everyone>`, `<@U…>`, `<#C…>`, or
`<!subteam^…>` broadcast or ping an entire Slack workspace when sent.
This is the single most likely vector for a CVE-worthy report — any
finding in this area gets fast-tracked and warrants CVE coordination.

The conformance suite that guards this rule is in
`internal/converter/mentions_test.go` (`TestSanitization_*`).

### Input bounding

`Options.MaxInputBytes` (default 256 KiB) caps the markdown input
size before goldmark allocates an AST. Bypassing this limit on a
public-facing MCP server would enable trivial memory exhaustion.

### Block Kit Builder URL injection

`internal/preview/preview.go` URL-encodes the entire `{"blocks":[...]}`
payload via `url.QueryEscape` before appending to the Builder URL.
Any future edit that bypasses `QueryEscape` would let a hostile block
payload break out of the URL fragment.

## Out of scope

- Issues that require a malicious MCP client (the trust model assumes
  the client is the one that invoked the server).
- Issues that require local filesystem access on the maintainer's
  machine (not a network attack surface).
- Denial-of-service via resource exhaustion when `MaxInputBytes` is
  set to a value larger than the documented default.

## Recognition

Reports that lead to a coordinated fix are credited (with permission)
in the release notes and the [CHANGELOG.md](CHANGELOG.md) Security
section. We do not currently run a paid bug-bounty.
