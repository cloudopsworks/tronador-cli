# `iac` command

`tronador iac` contains infrastructure-as-code helpers for CloudOps Works
workspaces. IaC commands are guarded by a workspace marker: the selected
`--workdir` must contain `.cloudopsworks/.iac`.

```bash
tronador iac --workdir <workspace> <command>
```

## Workspace guard and path semantics

- `--workdir` is the guarded IaC workspace root. Marker validation always checks
  `<workdir>/.cloudopsworks/.iac`.
- `--path` on subcommands is a discovery start path, not a replacement for
  `--workdir`.
- Relative `--path` values resolve under `--workdir`.
- Absolute `--path` values are accepted only when they remain inside `--workdir`.
- A path that resolves outside `--workdir` is rejected as an operational error;
  the command does not fall back to scanning the whole workspace.

## `iac module`

`iac module` reports Terraform/Terragrunt module sources in `terragrunt.hcl`
files and can optionally update supported GitHub module source pins.

Compatibility aliases remain available:

- `iac module-versions`
- `iac module_versions`

### Supported module source forms

The command only parses direct GitHub HTTPS sources that include an explicit
`?ref=` pin:

```hcl
source = "git::https://github.com/org/repo.git//subdir?ref=v1.2.3"
source = "git::https://github.com/org/repo//subdir?ref=v1.2.3"
source = "https://github.com/org/repo.git//subdir?ref=v1.2.3" # missing git::
```

Sources missing `git::` are accepted in report mode and marked as prefix-fix
available. The `git::` prefix follows Terraform's generic Git source syntax.

The command deliberately does **not** mutate Terraform registry addresses, local
paths, SSH/scp-style Git addresses, or sources without explicit `?ref=` pins.
Unsupported or unparseable sources are reported instead of guessed.

### Version lookup

For each supported GitHub source, the command lists repository tags through the
GitHub API and calculates three independent upgrade targets:

- **Patch**: the highest eligible tag in the current major and minor series.
- **Minor**: the highest eligible tag in a later minor series within the current
  major.
- **Major**: the highest eligible tag in a later major series.

Without `--upgrade`, all available targets are reported and files are not
changed. With `-u, --upgrade`, the patch target is selected by default. Use
`--minor` or `--major` with `--upgrade` to select the corresponding broader
target; those flags are mutually exclusive. If the selected tier has no target,
the command leaves the ref unchanged and reports any broader targets that are
available. Release-tier lines are printed only for concrete available semantic
version tags; unavailable tiers are omitted. When no tier has a target, the
surrounding status summary is still printed unchanged.

When `--major` is selected for an upgrade and no eligible major target exists,
the command falls back to the highest eligible minor target in the current
major version line. If neither a major nor same-major minor target is available,
the ref remains unchanged.

Stable tags always qualify. Alpha and beta prerelease tags qualify only when
enabled with `--alpha` or `--beta`; their optional dot-separated suffixes must
be numeric. Other prerelease channels are ignored. SemVer precedence is used
after filtering, so a current prerelease can promote to a stable tag at the
same version. Non-SemVer refs are reported but are never automatically
rewritten. Findings such as outdated refs or missing prefixes are report results
and do not by themselves cause a non-zero exit code.

Operational failures, such as an invalid workdir, missing `.cloudopsworks/.iac`,
out-of-scope `--path`, file read/write errors, or unhandled tag lookup failures,
return non-zero.

### Flags

| Flag | Description |
| --- | --- |
| `--workdir <dir>` | IaC workspace root. Defaults to `.` and must contain `.cloudopsworks/.iac`. |
| `-p, --path <dir>` | Module discovery path relative to `--workdir`, or an absolute path inside `--workdir`. |
| `-u, --upgrade` | Update eligible `?ref=` pins to the highest eligible patch target (same major/minor) and also add missing `git::` prefixes. |
| `--minor` | With `--upgrade`, select the highest eligible later minor target in the current major series. Mutually exclusive with `--major`. |
| `--major` | With `--upgrade`, select the highest eligible later major target. Mutually exclusive with `--minor`. |
| `--alpha` | Allow alpha prerelease tags when selecting an update. |
| `--beta` | Allow beta prerelease tags when selecting an update. |
| `--fix-prefix` | Add missing `git::` prefixes for eligible GitHub HTTPS sources without changing refs. |
| `--dry-run` | Analyze and print intended mutations without writing files. |
| `-r, --report-ghaction` | Emit GitHub Actions warning annotations for outdated modules. |
| `-c, --comment-pr-num <number>` | Comment on a pull request when `--report-ghaction` is enabled. |

### Examples

Report module status for the whole workspace:

```bash
tronador iac module --workdir ../my-iac-workspace
```

Report all available patch, minor, and major targets for one environment folder
while still validating the workspace marker at `--workdir`:

```bash
tronador iac module --workdir ../my-iac-workspace --path env/dev
```

Preview the default patch upgrades and prefix fixes without writing files:

```bash
tronador iac module --workdir ../my-iac-workspace --path env/dev --upgrade --dry-run
```

Apply the default patch ref upgrades and normalize missing `git::` prefixes:

```bash
tronador iac module --workdir ../my-iac-workspace --path env/dev --upgrade
```

Apply the latest eligible minor or major target instead of the default patch
target:

```bash
tronador iac module --workdir ../my-iac-workspace --path env/dev --upgrade --minor
tronador iac module --workdir ../my-iac-workspace --path env/dev --upgrade --major
```

Allow alpha and beta prereleases when applying updates:

```bash
tronador iac module --workdir ../my-iac-workspace --upgrade --alpha --beta
```

Only add missing `git::` prefixes without changing `?ref=` pins:

```bash
tronador iac module --workdir ../my-iac-workspace --fix-prefix
```

Emit CI warnings for outdated modules:

```bash
tronador iac module --workdir . --report-ghaction
```
