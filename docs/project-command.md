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

Every mutating capability supports the global `--dry-run` plan. Destructive
Terragrunt cleanup requires `--yes`. Tool calls use typed argument arrays and
never invoke Make, shell bootstrap scripts, or tag-management workflows.

## Initialization mappings

Initialization reproduces the body of the referenced template target as an
ordered typed plan. It never dispatches `make init`, nor does it infer an
engine subcommand from the word `init`.

- Application markers execute their template's `code/init` steps: repository
  owner/version captures, metadata edits, .NET renames, Go module commands,
  Python/Rust native metadata edits, and template-specific source rewrites.
- Terraform modules remove `provider.temp.tf`, then run Boilerplate with the
  validated provider variable. They do not run `terraform init` or `tofu init`.
- Terragrunt projects run Boilerplate with the existing `.inputs` var files,
  derive `iac_project` from the workdir basename, and then run
  `terragrunt hcl format --exclude-dir .cloudopsworks`. They do not run
  `terragrunt init`.

Dry-run output exposes each native and tool step, including its ordered
arguments, so callers can inspect the polymorphic pipeline before mutation.
