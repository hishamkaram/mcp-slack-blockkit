<!--
Thanks for the PR! Please:
- Use a Conventional Commits title (e.g. `feat(converter): support task lists`).
- Make sure CI is green before requesting review.
- Keep changes focused — one logical change per PR.
-->

## Summary

<!-- One or two sentences. What changes and why. -->

## Type of change

- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds capability)
- [ ] Breaking change (would cause existing functionality not to work as expected)
- [ ] Documentation update
- [ ] Internal / refactor (no public API change)

## Related issue

<!-- e.g. Fixes #123 -->

## Test plan

- [ ] `make test` passes
- [ ] `make lint` clean
- [ ] `make vet` clean
- [ ] `make vuln` clean
- [ ] Coverage on changed packages stays ≥80%
- [ ] Added or updated tests cover the change
- [ ] If touching the converter or server: ran `make fuzz` smoke

## Checklist

- [ ] Commit messages follow [Conventional Commits 1.0](https://www.conventionalcommits.org/)
- [ ] Public API changes documented in godoc comments
- [ ] If user-facing change: added an entry to `CHANGELOG.md` under `[Unreleased]`
- [ ] If touching mention sanitization: cross-checked `.claude/rules/security.md`
