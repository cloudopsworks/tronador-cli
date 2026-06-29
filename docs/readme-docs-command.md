# README and docs commands

`tronador readme` and `tronador docs` port the Tronador `readme/*` and
`docs/*` Makefile targets into the release binary while keeping template assets
runtime-configurable.

## `tronador readme`

```bash
tronador readme build
tronador readme init
tronador readme lint
tronador readme deps
```

Common flags:

| Flag | Description |
| --- | --- |
| `--workdir` | Target repository directory. |
| `--readme-file`, `--output` | README output file, default `README.md` or `README_FILE`. |
| `--readme-yaml` | README data YAML file, default `README.yaml` or `README_YAML`. |
| `--template-file` | gomplate template override. |
| `--template-yaml` | initialization YAML template override. |
| `--includes-uri` | gomplate include datasource URI. |
| `--gomplate` | gomplate executable path. |
| `--gomplate-version` | gomplate version to download on demand, overriding the configured default/version env vars. |
| `--tools-dir` | User tool cache/install directory, default `~/.cloudopsworks/tronador` or `TRONADOR_TOOLS_DIR`. |
| `--tools-config` | Tool provisioner JSON override file, default `TRONADOR_TOOLS_CONFIG` / `TRONADOR_TOOLS_CONFIG_PATH` plus project/user overrides. |
| `--no-install-gomplate` | Fail instead of downloading gomplate when it is missing. |

`readme build` invokes gomplate with the selected template and writes the README
output. `readme lint` builds to a temporary file and fails if the committed
README differs. `readme init` creates `README.yaml` only when it is missing.

### On-demand tool provisioning

README build/lint/deps resolve gomplate in this order:

1. explicit `--gomplate` or `GOMPLATE`
2. `PATH`
3. the user tool directory, default `~/.cloudopsworks/tronador/gomplate`
4. direct on-demand download of the requested gomplate version from the upstream
   GitHub release into `~/.cloudopsworks/tronador`

Provisioning is per-tool and on-demand: the CLI downloads only the missing tool
needed by the command being executed. It does not clone or use
`tronador-packages`. Set `TRONADOR_TOOLS_INSTALL=false`,
`TRONADOR_TOOLS_SKIP_INSTALL=true`, or pass `--no-install-gomplate` to require a
preinstalled executable.

Tool metadata is JSON-driven. The binary ships an embedded `tools.json` with
entries for:

- `gomplate`
- `gh`
- `boilerplate`
- `gitversion`
- `yq`

Runtime overrides are merged field-by-field by tool name in this order:

1. embedded defaults
2. user `~/.cloudopsworks/tronador/tools.json`
3. project `.cloudopsworks/tronador/tools.json`
4. project `.tronador/tools.json`
5. `TRONADOR_TOOLS_CONFIG` / `TRONADOR_TOOLS_CONFIG_PATH`
6. explicit `--tools-config`

Empty or omitted fields inherit lower-precedence values. Platform overrides are
also merged by key, with OS-wide keys such as `darwin` or `linux` applied before
exact keys such as `darwin/arm64`.

Supported JSON fields per tool:

```json
{
  "name": "yq",
  "executable": "yq",
  "default_version": "4.47.2",
  "version_env_vars": ["TRONADOR_YQ_VERSION", "YQ_VERSION"],
  "url_template": "https://github.com/mikefarah/yq/releases/download/v{version}/yq_{os}_{arch}{ext}",
  "format": "binary",
  "binary_name": "yq",
  "archive_binary_path": "*/bin/yq",
  "checksum_sha256": "",
  "platform_overrides": {}
}
```

`format` supports `binary`, `zip`, and `tar.gz`. Checksums are optional and are
validated only when `checksum_sha256` is configured.

## Runtime README assets

README generator assets are resolved in this order:

1. explicit flags (`--template-file`, `--template-yaml`)
2. environment (`README_TEMPLATE_FILE`, `README_TEMPLATE_YAML`)
3. project-local `.tronador/readme/*`
4. project-local `.cloudopsworks/readme/*`
5. user config, for example `~/.config/tronador/readme/*`
6. GitHub sync cache
7. shared install paths
8. embedded fallback assets

Template asset refresh is explicit. Use asset commands when you want to manage
the runtime templates:

```bash
# Show the active asset source for each file
tronador readme assets path

# Copy embedded fallback assets into editable project-local files
tronador readme assets init --workdir .

# Sync canonical assets from GitHub into the local cache
tronador readme assets sync

# Sync a pinned release/ref and copy it into the project override directory
tronador readme assets sync --version vX.Y.Z --project --workdir .

# Inspect or clear the cache
tronador readme assets cache ls
tronador readme assets cache clean
```

By default, sync downloads from `cloudopsworks/tronador` at `master` under the
`templates/` path. Use `--repo`, `--ref`, `--version`, `--base-path`, or
`--manifest-path` for alternate sources. When a manifest is provided, SHA-256
checksums are verified before assets are stored.

## `tronador docs`

```bash
tronador docs init
tronador docs targets
tronador docs terraform
tronador docs copyright-add --software-description "My service"
```

Common flags:

| Flag | Description |
| --- | --- |
| `--workdir` | Target repository directory. |
| `--docs-dir` | Documentation output directory, default `docs` or `DOCS_DIR`. |

### `docs targets`

Generates `docs/targets.md` from Make help output, stripping ANSI color codes and
wrapping the result in a Markdown code block.

```bash
tronador docs targets --workdir .
tronador docs targets --workdir . --all
tronador docs targets --make gmake --help-target help/all
```

### `docs terraform`

Runs `terraform-docs md .` from `--workdir` and writes `docs/terraform.md`.

```bash
tronador docs terraform --workdir ./modules/vpc
tronador docs terraform --terraform-docs /usr/local/bin/terraform-docs
```

### `docs copyright-add`

Runs the configured copyright-header command. `--software-description` (or
`COPYRIGHT_SOFTWARE_DESCRIPTION`) is required. Use global `--dry-run` to preview
before mutating source files.

```bash
tronador docs copyright-add \
  --software-description "Repository automation CLI" \
  --dry-run
```
