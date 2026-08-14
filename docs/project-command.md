# Project command

`tronador project` detects the implementation from a regular marker file under
`.cloudopsworks/` and runs a logical capability without dispatching through a
Makefile.

```bash
tronador project detect
tronador project capabilities
tronador project init
tronador project init aws       # Terraform module provider
tronador project version
tronador project lint
tronador project format
tronador project clean --yes    # Terragrunt only
```

The command is namespace-free: do not write `project go init` or
`project terraform-module lint`. Use `project capabilities` to see the valid
capabilities for the detected marker.

## Detection

Only `<workdir>/.cloudopsworks/` is inspected. The supported markers are
`.android`, `.docker`, `.dotnet`, `.fluttermobile`, `.golang`, `.java`,
`.node`, `.python`, `.rust`, `.terraform-module`, `.iac`, and `.xcode`.
Markers must be regular files. Unknown files are ignored; no marker or markers
from different implementations produce structured errors.

Use `--workdir` to target another repository and `--json` for stable machine-
readable output:

```bash
tronador project detect --workdir ../service --json
tronador project capabilities --workdir ../service --json
```

## Tools and safety

Tool-backed capabilities resolve only their declared tools through the shared
Tronador catalog: PATH, the provisioned cache, then direct provisioning. A
download requires `--allow-network`; `--no-install-tools` disables installation.
Use `--tools-dir`, `--tools-config`, `--tool-version name=version`, and
`--tool-path name=path` to control resolution. Terraform/OpenTofu lint and
format operations use OpenTofu by default and accept
`--engine terraform|tofu|auto`; `auto` prefers OpenTofu when only one
candidate is available and fails when both are available.
Initialization does not resolve an IaC engine.

Every mutating capability except `project version` supports the global `--dry-run`
operation plan without tool resolution. `project version --dry-run` is the explicit
evaluated-preview exception: it selects an exact HEAD tag first, otherwise calculates
GitVersion, but never changes project files. It prints deterministic unified file
patches (or `No file changes for version <version>.`) and JSON exposes only actual
`file_changes`; unchanged and optional missing metadata files are omitted. GitVersion
provisioning is allowed only to a tools directory outside the workdir, and successful
preview suppresses provisioner/tool progress. Destructive Terragrunt cleanup requires
`--yes`. Tool calls use typed argument arrays and never invoke Make, shell bootstrap
scripts, or tag-management workflows.

## Initialization mappings

Initialization reproduces the body of the referenced template target as an
ordered typed plan. It never dispatches `make init`, nor does it infer an
engine subcommand from the word `init`.

- Application markers execute their template's `code/init` steps: repository
  owner/version captures, native JSON/YAML/XML/TOML metadata edits, .NET
  renames, Go module commands, and template-specific source rewrites. These
  structured-file mutations run in Go and do not require the external `yq`
  executable.
- Terraform modules remove `provider.temp.tf`, then run Boilerplate with the
  validated provider variable. They do not run `terraform init` or `tofu init`.
- Terragrunt projects run Boilerplate with the existing `.inputs` var files,
  derive `iac_project` from the workdir basename, and then run
  `terragrunt hcl format --exclude-dir .cloudopsworks`. They do not run
  `terragrunt init`.

Non-version dry-run output exposes each native and tool step, including its ordered
arguments, so callers can inspect the polymorphic pipeline before mutation. Version
dry-run instead emits only its actual file delta.

### Native metadata updates

Application initialization and versioning use format-aware Go libraries:

- JSON uses a path-scoped update, preserving unrelated keys.
- YAML uses `yaml.Node` traversal for dotted selectors.
- XML uses token-based path matching, including attributes and sibling indexes.
- TOML uses the `go-toml` parser AST to replace values in the selected table
  without rewriting unrelated tables, comments, or formatting.

The Java version selector is the exact `/project/version` element. Parent,
dependency, plugin, and other nested Maven versions are not changed.
