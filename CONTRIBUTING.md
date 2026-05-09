# Contributing to mcp-slack-blockkit

Thanks for your interest. This is a small but security-relevant project,
so the contribution bar emphasizes correctness and tests over volume.

## Quick start

After cloning:

```sh
make setup     # one-time: installs lefthook git hooks
make test      # run the test suite
make build     # build ./bin/mcp-slack-blockkit
```

If `make setup` fails because lefthook isn't on your `PATH`:

```sh
brew install lefthook   # macOS / Linux
# or grab a release binary from https://github.com/evilmartians/lefthook/releases
```

(`go install github.com/evilmartians/lefthook/v2@latest` currently
needs Go 1.26 to compile. The project itself targets Go 1.25.)

## Conventional Commits

Commit messages must follow [Conventional Commits 1.0](https://www.conventionalcommits.org/).
The lefthook `commit-msg` hook enforces this with a regex; bypass with
`LEFTHOOK=0 git commit ...` only in emergencies.

```
<type>(<scope>)?: <subject>
```

Allowed types: `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`,
`refactor`, `revert`, `style`, `test`. Examples:

- `feat(converter): support task-list checkboxes`
- `fix(server): handle nil mention map`
- `docs(readme): document Claude Desktop config`
- `chore(deps): bump goldmark to v1.8.3`

## Branch naming

Match the commit type:

```
feat/<slug>     fix/<slug>     docs/<slug>     chore/<slug>
```

## Testing requirements

Every change ships with tests. The CI coverage gate is **≥80% overall**
and we hold these packages to that bar individually:

- `blockkit/`
- `internal/converter/`
- `internal/validator/`
- `internal/splitter/`
- `internal/preview/`

Use the standard table-driven idiom (`[]struct{name, in, want}` + `t.Run`
loop). For the converter, prefer **golden-file** tests under
`testdata/<case>/{input.md,expected.json}` over inline JSON literals.

If you touch the splitter or converter, run a fuzz smoke before pushing:

```sh
make fuzz   # 30s
```

The full nightly fuzz time-budget runs in CI. Don't skip the smoke
locally just because CI exists — it catches the obvious cases fast.

## Security-critical paths

The mention-sanitization layer in `internal/converter/mentions.go` is
the security-critical path. See [`.claude/rules/security.md`](.claude/rules/security.md)
for the rule and [`SECURITY.md`](SECURITY.md) for the reporting process.
PRs that touch this code path should explicitly note in the description
how the sanitization invariants are preserved.

## Local dev workflow

Useful Make targets (run `make help` for the full list):

| Target | What it does |
|---|---|
| `make build` | Build to `./bin/mcp-slack-blockkit` |
| `make test` | Full test suite |
| `make test-race` | Race tests + coverage profile |
| `make cover` | HTML coverage report |
| `make lint` | golangci-lint v2 |
| `make vet` | `go vet ./...` |
| `make vuln` | `govulncheck` |
| `make fuzz` | 30s fuzz smoke |
| `make snapshot` | GoReleaser dry build |
| `make setup` | One-time hook install |
| `make clean` | Remove build artifacts |

## Pull request etiquette

- One logical change per PR. Split unrelated changes.
- Fill in the PR template (auto-loaded). Especially the test-plan section.
- Don't merge your own PR; wait for CI green and at least one review (or
  for the maintainer to self-merge if you are the maintainer).
- The repo uses **squash merge only** — your commit message becomes the
  merge commit. Keep it descriptive.

## What we won't merge

- Vendored copies of competitor libraries (`navidemad/md2slack`,
  `takara2314/slack-go-util`, etc.). Reimplement — see the project's
  history for the rationale.
- Code without tests, except for pure docs / formatting changes.
- Changes that drop coverage below the 80% gate.
- Co-author trailers that name an AI assistant. Write commits as if
  authored by you.

## License

By contributing you agree that your contributions are licensed under
the project's [MIT License](LICENSE). No CLA, no DCO — just the
standard inbound=outbound flow GitHub uses.
