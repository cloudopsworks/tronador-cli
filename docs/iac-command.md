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
GitHub API and selects the highest eligible semantic-version tag. Stable tags
always qualify. Alpha and beta prerelease tags qualify only when enabled with
`--alpha` or `--beta`; their optional dot-separated suffixes must be numeric.
Other prerelease channels are ignored. SemVer precedence is used after
filtering, so a later allowed prerelease can be selected over an older stable
release, while a stable release beats a prerelease of the same version.
Findings such as outdated refs or missing prefixes are report results and do not
by themselves cause a non-zero exit code.

Operational failures, such as an invalid workdir, missing `.cloudopsworks/.iac`,
out-of-scope `--path`, file read/write errors, or unhandled tag lookup failures,
return non-zero.

### Flags

| Flag | Description |
| --- | --- |
| `--workdir <dir>` | IaC workspace root. Defaults to `.` and must contain `.cloudopsworks/.iac`. |
| `-p, --path <dir>` | Module discovery path relative to `--workdir`, or an absolute path inside `--workdir`. |
| `-u, --upgrade` | Update eligible `?ref=` pins to the latest semantic-version tag and also add missing `git::` prefixes. |
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

Scan only one environment folder while still validating the workspace marker at
`--workdir`:

```bash
tronador iac module --workdir ../my-iac-workspace --path env/dev
```

Preview ref upgrades and prefix fixes without writing files:

```bash
tronador iac module --workdir ../my-iac-workspace --path env/dev --upgrade --dry-run
```

Apply latest ref upgrades and normalize missing `git::` prefixes:

```bash
tronador iac module --workdir ../my-iac-workspace --path env/dev --upgrade
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
