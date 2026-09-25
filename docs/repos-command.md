# `repos` command architecture

`tronador repos` ports the public Tronador `make repos/*` targets into Cobra
commands. The command is intentionally configuration-driven so future repository
layouts can be added by editing JSON instead of branching the CLI dispatcher.

## Configuration

The embedded default lives at `internal/repos/default_config.json` and can be
overridden with:

```bash
tronador repos --config path/to/repos-config.json ...
```

The JSON catalog contains:

- `templates[]`: marker file, GitHub template repository, upgrade flags,
  boilerplate paths, and the migration key for each supported repository type.
- `migrationPlans[]`: declarative file operations keyed by version. The default
  config includes `510` plus reserved `511` and `512` slots for future upgrade
  paths.

The default catalog includes the in-progress
`cloudopsworks/argocd-project-template`. Repositories are detected through
`.cloudopsworks/.argocd`; the template participates in versioned upgrades but
does not enable CICD footer or boilerplate handling. Its root `Makefile` is
template-owned and is copied during upgrades.

## Command mapping

| Make target | CLI equivalent |
| --- | --- |
| `repos/avail`, `repos/available` | `tronador repos available` (`avail` alias; `repo available` also works) |
| `repos/template/init` | `tronador repos template init` |
| `repos/template/<kind>` | `tronador repos template <kind>` |
| `repos/clean/template` | `tronador repos clean template` (`repos template clean` also works) |
| `repos/clean` | `tronador repos clean` |
| `repos/cicd/update` | `tronador repos cicd update` |
| `repos/upgrade` | `tronador repos upgrade` |
| `repos/upgrade/<version>` | `tronador repos upgrade <version>`; `major` resolves the latest same-major tag and `master` uses the template master branch tip |
| `repos/recover` | `tronador repos recover` |
| `repos/push` | `tronador repos push` |
| `repos/migrate/510` | Internal workflow step only; not exposed as a public CLI command |
| `repos/migrate/<kind>/510` | Internal workflow step only; not exposed as a public CLI command |

`repos push` validates that every caller-staged path is upgrade-owned, then commits
that existing index only. It does not discover or stage working-tree content.
`repos upgrade` uses its own isolated exact-effect commit transaction.

`tronador repos upgrade` is the only public upgrade command. With no
argument, it mirrors the Makefile `repos/upgrade` target: initialize the detected
template checkout, query tags through the native GitHub API adapter (with `gh` fallback), select the latest tag in the current
major/minor line, fetch that tag, evaluate the template layout, apply the upgrade
stack, update CICD metadata, and commit the result.

`tronador repos upgrade <version>` mirrors the Makefile `repos/upgrade/%`
target and runs the same full workflow against the explicit tag or branch. The
special `major` value resolves the latest available semantic version tag within
the same major line as the local `_VERSION`; the special `master` value upgrades
from the template repository's `master` branch tip.

During the upgrade stack, `.github/workflows/` remains template-owned and is
replaced. For a v5.10 target, the configuration upgrade refreshes an
already-v5.10 repository and first migrates only target-matching legacy root
CloudOps YAML from a pre-v5.10 `.github` layout; arbitrary GitHub operational YAML
is not migrated as configuration. It then reconciles the v5.10 `.cloudopsworks/`
layout. Issue and pull request templates are intentionally non-destructive:
`.github/ISSUE_TEMPLATE/*` files, including `config.yml`, and
`.github/PULL_REQUEST_TEMPLATE.md` are copied only when the implementation
repository does not already have the destination file. Template repositories may
store implementation issue forms with a disabled suffix such as
`01_bug_report.yml.disabled`; the upgrade strips that suffix when copying into an
implementation repository while still skipping reserved `98_*` and `99_*`
template-only issue forms. `.github/dependabot.yml` follows the same
non-destructive rule: it is copied when present in the template and absent from
the implementation repository, and is left unchanged when already present. The
same rule applies to `.github/secret_scanning.yml` and
`.github/codeql/codeql-config.yml` for every repository type. For v5.10,
`.cloudopsworks/auto-assign.yml` is part of the value-aware YAML merge: active
local reviewer and assignee values are retained while template defaults and
documentation are refreshed. A v5.9-layout-to-v5.10 upgrade migrates only
exact target-matching legacy root CloudOps YAML, including
`.github/auto-assign.yml`; it does not treat arbitrary GitHub operational YAML
as configuration. Targets that remain pre-v5.10 copy auto-assign files only when
missing.
The template `.gitignore` is merged through a stable managed block delimited by
`# BEGIN TRONADOR TEMPLATE MANAGED BLOCK` and
`# END TRONADOR TEMPLATE MANAGED BLOCK`. A valid existing block is replaced with
the current template content while all bytes outside it remain unchanged. An
unmarked or malformed destination is treated entirely as user-owned and receives
a new managed block appended to it. Unchanged merge results are not rewritten or
staged.

### v5.10 CloudOps configuration refresh

Tronador detects target generation from CloudOps workflow blueprint references
(or `blueprint_ref`), rather than the application-template `_VERSION`. Missing
or contradictory active workflow evidence stops the upgrade. For a v5.10 target,
it value-aware merges configuration YAML under the target `.cloudopsworks/`
directory. That includes root policy YAML such as `cloudopsworks-ci.yaml`,
labeler configuration, GitVersion profiles, and auto-assignment configuration,
as well as applicable `vars/`, `vars/helm/`, `vars/apigw/`, and `vars/preview/`
YAML. Target YAML supplies the baseline, retaining its active structure, comments,
and defaults; active repository values overlay it recursively.

Only a template's explicitly configured opaque boilerplate subtree is
byte-for-byte exact-refreshed. Root policy YAML is not opaque boilerplate and is
therefore value-aware merged.

Environment files keep their local filenames. For a local top-level
`vars/inputs-*.yaml` file, Tronador selects an input scaffold in this order:

1. When present, one or more local `# Agents:` declarations matched to exactly one same template
   baseline. A declaration may use `|` alternatives for `cloud` or `cloud_type`;
   every declared combination must resolve to that same baseline. Otherwise, the
   local file is preserved with a warning and Tronador does not fall back to an
   exact filename, mobile, or global selection.
2. When no local `# Agents:` metadata is present, an exact relative filename in the
   target `.cloudopsworks/` tree.
3. One active mobile family from `vars/inputs-global.yaml`: Android, XCode, or
   a Flutter platform selection. Both Android and XCode active is ambiguous and
   does not select a scaffold.
4. The `cloud` and `cloud_type` context from `vars/inputs-global.yaml`, mapped to
   Kubernetes (`kubernetes`, `eks`, `aks`, or `gke`), AWS Lambda, AWS Beanstalk,
   GCP App Engine, GCP Cloud Run, or a library/no-deployment scaffold.

Within `vars/helm/`, `vars/apigw/`, and `vars/preview/`, an exact filename match
is preferred. Otherwise, a single same-family `dev`, `uat`, or `prod` target is
used, so `values-prod-live.yaml` can retain its name and values while adopting
the `values-prod.yaml` baseline. Helm applies to Kubernetes targets or existing
Helm YAML; API-gateway and preview configuration apply when present locally or
enabled in the global inputs. A missing or ambiguous baseline leaves the local
file in place and emits a preservation warning.

Tronador preserves any local-only active YAML file or path and emits a warning
instead of discarding it. It stops before changing `_VERSION` when it encounters
unsafe YAML: duplicate mapping keys, aliases/anchors, or multiple documents.
`_VERSION` is copied only after the configuration refresh succeeds. With
`--dry-run`, Tronador acquires and analyses the selected temporary target checkout,
then reports planned YAML writes and warnings without modifying repository files. Existing non-YAML behavior, including workflow replacement and
non-YAML hook refreshes, remains unchanged.

The Makefile's `repos/upgrade/fetch`, `repos/upgrade/eval`,
`repos/upgrade/stack`, and `repos/migrate/*` targets are internal workflow
stages. They are intentionally not exposed as CLI subcommands. Public repos
workflows remove the temporary `.template` checkout on completion and on
recoverable failures; `repos recover` fetches its requested branch or tag instead
of relying on a stale local `.template` directory.

All commands support the root `--dry-run` flag and the `repos` persistent flags
`--workdir`, `--config`, `--git`, `--gh`, and `--pull-branch` (`recover` only;
`upgrade` uses optional `[version]`).
