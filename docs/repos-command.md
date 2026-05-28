# `repos` command architecture

`tronador-cli repos` ports the Tronador `make repos/*` targets into Cobra commands.
The command is intentionally configuration-driven so future repository layouts can
be added by editing JSON instead of branching the CLI dispatcher.

## Configuration

The embedded default lives at `internal/repos/default_config.json` and can be
overridden with:

```bash
tronador-cli repos --config path/to/repos-config.json ...
```

The JSON catalog contains:

- `templates[]`: marker file, GitHub template repository, upgrade flags, boilerplate
  paths, and the migration key for each supported repository type.
- `migrationPlans[]`: declarative file operations keyed by version. The default
  config includes `510` plus reserved `511` and `512` slots for future upgrade
  paths.

## Command mapping

| Make target | CLI equivalent |
| --- | --- |
| `repos/avail`, `repos/available` | `tronador-cli repos available` (`avail` alias) |
| `repos/template/init` | `tronador-cli repos template init` |
| `repos/template/<kind>` | `tronador-cli repos template <kind>` |
| `repos/clean/template` | `tronador-cli repos clean template` (`repos template clean` also works) |
| `repos/clean` | `tronador-cli repos clean` |
| `repos/cicd/update` | `tronador-cli repos cicd update` |
| `repos/upgrade` | `tronador-cli repos upgrade` |
| `repos/upgrade/dev` | `tronador-cli repos upgrade dev` |
| `repos/upgrade/major` | `tronador-cli repos upgrade major` |
| `repos/upgrade/master` | `tronador-cli repos upgrade master` |
| `repos/upgrade/<version>` | `tronador-cli repos upgrade <version>` |
| `repos/upgrade/fetch` | `tronador-cli repos upgrade fetch --pull-branch <ref>` |
| `repos/upgrade/eval` | `tronador-cli repos upgrade eval` |
| `repos/upgrade/stack` | `tronador-cli repos upgrade stack` |
| `repos/recover` | `tronador-cli repos recover` |
| `repos/push` | `tronador-cli repos push` |
| `repos/migrate/510` | `tronador-cli repos migrate 510` |
| `repos/migrate/<kind>/510` | `tronador-cli repos migrate <kind> 510` |

All commands support the root `--dry-run` flag and the `repos` persistent flags
`--workdir`, `--config`, `--git`, `--gh`, and `--pull-branch`.
