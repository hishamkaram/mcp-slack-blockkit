---
name: release
description: Cut a new release of mcp-slack-block-kit. Bump the version, move CHANGELOG `[Unreleased]` to a new version section, create a signed tag, push it, and confirm GoReleaser publishes successfully. Use when the maintainer asks for a release, a tag, or a version bump.
allowed-tools: Read, Edit, Bash, Grep
---

# Release a new version

Follow this checklist exactly — releases are tag-driven and the
GoReleaser workflow does the heavy lifting once the tag lands.

## 1. Pre-flight (in this order)

1. Confirm `git status` is clean. Refuse to continue if not.
2. Confirm we're on `main` and up-to-date with `origin/main`.
3. Confirm CI is green on the latest `main` commit:
   `gh run list --branch main --limit 1` — must show `completed success`
   for both `ci` and `CodeQL`.
4. Run `make test-race lint vet vuln` locally. Must all pass.
5. Run `make snapshot` (GoReleaser dry build). Must produce artifacts in
   `dist/` without errors.

## 2. Pick the version

Read the current latest tag (`git tag --sort=-v:refname | head -1`).
Apply [SemVer](https://semver.org/) against the changes since that tag
(use `git log --oneline LATEST_TAG..HEAD` and the `[Unreleased]` section
of `CHANGELOG.md` to decide):

- `MAJOR` if any breaking change (we're at `0.x` — breaking changes are
  allowed in minor bumps; reserve major for `1.0.0` GA).
- `MINOR` for new features.
- `PATCH` for fixes, docs, internal changes.

Confirm the version number with the user before tagging.

## 3. Update CHANGELOG.md

Open `CHANGELOG.md`. The current shape:

```markdown
## [Unreleased]
### Added
- ...
### Fixed
- ...
```

Move `[Unreleased]` content into a new `[X.Y.Z] - YYYY-MM-DD` section
above it, and re-add an empty `[Unreleased]` heading at the top with
the standard subsection placeholders. Update the link references at
the bottom of the file.

Commit with: `git commit -m "chore(release): vX.Y.Z"`

## 4. Tag and push

```bash
git tag -s vX.Y.Z -m "vX.Y.Z"
git push origin main
git push origin vX.Y.Z
```

The signed-tag step requires `git config user.signingkey` to be set;
fail loudly if it isn't rather than producing an unsigned tag.

## 5. Verify the release workflow

`gh run watch` — wait for the `Release` workflow to complete.

Then verify the published release:

```bash
gh release view vX.Y.Z
```

Check that the release page shows:
- All multi-arch archives (linux/darwin/windows × amd64/arm64).
- `checksums.txt` + `checksums.txt.sig` + `checksums.txt.pem`.
- SBOMs.
- The Homebrew tap PR (or merged formula bump).

If anything is missing, investigate the workflow logs before announcing
the release.

## 6. Post-release

- Verify `brew upgrade mcp-slack-block-kit` (or first-time install) works.
- Verify `go install github.com/hishamkaram/mcp-slack-block-kit/cmd/mcp-slack-block-kit@vX.Y.Z` works.
- Update the MCP Registry / Smithery / Glama listings with the new tag.

## Don't

- Don't tag without a signed key (no `git tag` without `-s`).
- Don't push a tag without first pushing the merge commit it points at.
- Don't `git push --force` to delete a botched tag — cut a new patch tag instead.
