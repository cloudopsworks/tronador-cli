# Contributing to tronador-cli

(c) 2021-2025 Cloud Ops Works LLC — Distributed under the [Apache 2.0 License](LICENSE).

---

## Before you start

- Check open issues and pull requests before opening a duplicate.
- For significant changes, open an issue first to align on scope before writing code.
- All contributions are subject to the Apache 2.0 License. By submitting a PR you agree that your contribution is licensed under those terms.

---

## Development setup

**Requirements**

- Go 1.26.3 or later (matches `go.mod` toolchain target)
- `make` / `gmake`
- AWS credentials with appropriate permissions for `aws` subcommands
- `gh` CLI authenticated via `GH_TOKEN`, `GITHUB_TOKEN`, or `gh auth login` for `repos` subcommands

**Build from source**

```bash
git clone https://github.com/cloudopsworks/tronador-cli.git
cd tronador-cli
make build
```

**Run tests**

```bash
go test ./...
```

**Compute version**

```bash
make version
```

This writes a `VERSION` file using GitVersion semantics. On a tagged commit it uses the tag; otherwise it derives the version from branch history.

---

## Branch model

This repository uses **GitHub Flow**.

- `master` is the stable, always-releasable branch.
- Work on feature branches cut from `master` (`feature/…`, `fix/…`, `chore/…`).
- Open a PR against `master` when your branch is ready for review.
- At least **one approved review** is required before merge.
- Branch protection is enforced — direct pushes to `master` are not permitted.

---

## Commit messages and semver annotations

Commits must carry a semver annotation so the release pipeline can compute the next version:

| Intent | Annotation |
|--------|-----------|
| Bug fix or patch | `+semver: fix` or `+semver: patch` |
| New feature (backward-compatible) | `+semver: feature` or `+semver: minor` |
| Breaking change | `+semver: major` |

Example:

```
fix: handle missing AWS region gracefully +semver: fix
```

Commits without an annotation are treated as non-version-bumping chores.

---

## Pull request checklist

Use the PR template (`.github/PULL_REQUEST_TEMPLATE.md`) and fill in all three sections:

- **what** — what changed in plain English
- **why** — the business or technical justification
- **references** — linked issues (`closes #123`) or relevant documentation

Before requesting review:

- [ ] `go test ./...` passes locally
- [ ] `make build` produces a clean binary
- [ ] Semver annotation is present in at least one commit or the PR title
- [ ] New flags or subcommands are reflected in `README.md` or `docs/`

---

## Code style

- Follow standard Go formatting (`gofmt`/`goimports`).
- Keep new commands under `internal/` behind the existing Cobra/Viper pattern.
- Do not add comments that restate what the code already says — comment only non-obvious invariants or workarounds.
- Do not introduce new dependencies without discussion.

---

## Releases

Releases are owned by CI after merge to `master`. Do **not** create tags, GitHub Releases, or release branches manually.

Maintainers use the `cw-release` skill to drive the full release lifecycle (branch, PR, tag, GitHub Release). See `CLAUDE.md` for invocation details if you are a maintainer.

---

## Reporting bugs

Open a GitHub Issue with:

1. `tronador-cli --version` output
2. OS and architecture
3. The exact command you ran
4. Expected vs. actual behaviour
5. Relevant error output or logs

---

## Security issues

Do **not** open a public issue for security vulnerabilities. Email [cristian@cloudopsworks.co](mailto:cristian@cloudopsworks.co) directly.

---

## License

By contributing you agree that your work is licensed under the [Apache License, Version 2.0](LICENSE) and that Cloud Ops Works LLC may distribute it under those terms.
