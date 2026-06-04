# `repos` command architecture

`tronador-cli repos` ports the public Tronador `make repos/*` targets into Cobra
commands. The command is intentionally configuration-driven so future repository
layouts can be added by editing JSON instead of branching the CLI dispatcher.

## Configuration

The embedded default lives at `internal/repos/default_config.json` and can be
overridden with:

```bash
tronador-cli repos --config path/to/repos-config.json ...
```

The JSON catalog contains:

- `templates[]`: marker file, GitHub template repository, upgrade flags,
  boilerplate paths, and the migration key for each supported repository type.
- `migrationPlans[]`: declarative file operations keyed by version. The default
  config includes `510` plus reserved `511` and `512` slots for future upgrade
  paths.

## Command mapping

| Make target | CLI equivalent |
| --- | --- |
| `repos/avail`, `repos/available` | `tronador-cli repos available` (`avail` alias; `repo available` also works) |
| `repos/template/init` | `tronador-cli repos template init` |
| `repos/template/<kind>` | `tronador-cli repos template <kind>` |
| `repos/clean/template` | `tronador-cli repos clean template` (`repos template clean` also works) |
| `repos/clean` | `tronador-cli repos clean` |
| `repos/cicd/update` | `tronador-cli repos cicd update` |
| `repos/upgrade` | `tronador-cli repos upgrade` |
| `repos/upgrade/<version>` | `tronador-cli repos upgrade <version>`; `major` resolves the latest same-major tag and `master` uses the template master branch tip |
| `repos/recover` | `tronador-cli repos recover` |
| `repos/push` | `tronador-cli repos push` |
| `repos/migrate/510` | Internal workflow step only; not exposed as a public CLI command |
| `repos/migrate/<kind>/510` | Internal workflow step only; not exposed as a public CLI command |

`tronador-cli repos upgrade` is the only public upgrade command. With no
argument, it mirrors the Makefile `repos/upgrade` target: initialize the detected
template checkout, query tags through the native GitHub API adapter (with `gh` fallback), select the latest tag in the current
major/minor line, fetch that tag, evaluate the template layout, apply the upgrade
stack, update CICD metadata, and commit the result.

`tronador-cli repos upgrade <version>` mirrors the Makefile `repos/upgrade/%`
target and runs the same full workflow against the explicit tag or branch. The
special `major` value resolves the latest available semantic version tag within
the same major line as the local `_VERSION`; the special `master` value upgrades
from the template repository's `master` branch tip.

During the upgrade stack, `.github/workflows/` remains template-owned and is
replaced. Issue and pull request templates are intentionally non-destructive:
`.github/ISSUE_TEMPLATE/*` files, including `config.yml`, and
`.github/PULL_REQUEST_TEMPLATE.md` are copied only when the implementation
repository does not already have the destination file. Template repositories may
store implementation issue forms with a disabled suffix such as
`01_bug_report.yml.disabled`; the upgrade strips that suffix when copying into an
implementation repository while still skipping reserved `98_*` and `99_*`
template-only issue forms.

The Makefile's `repos/upgrade/fetch`, `repos/upgrade/eval`,
`repos/upgrade/stack`, and `repos/migrate/*` targets are internal workflow
stages. They are intentionally not exposed as CLI subcommands. Public repos
workflows remove the temporary `.template` checkout on completion and on
recoverable failures; `repos recover` fetches its requested branch or tag instead
of relying on a stale local `.template` directory.

All commands support the root `--dry-run` flag and the `repos` persistent flags
`--workdir`, `--config`, `--git`, `--gh`, and `--pull-branch` (`recover` only;
`upgrade` uses optional `[version]`).
