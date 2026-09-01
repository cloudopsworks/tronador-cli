# Command reference

This page indexes the public `tronador` command surface and links to the
command-specific guides.

## Binary names

Installed releases expose the command as `tronador`. The repository and package
name remains `tronador-cli`; a plain `go build` from this checkout emits
`./tronador-cli` unless you pass `-o tronador`.

## Global flags

| Flag | Description |
| --- | --- |
| `--dry-run` | Show supported changes without applying them. |
| `-v, --verbose` | Enable verbose output. |
| `-h, --help` | Show command help. |

## Top-level commands

| Command | Purpose | Documentation |
| --- | --- | --- |
| `tronador aws` | AWS tagging, secret copy, default VPC cleanup, and remediation helpers. | [AWS command](aws-command.md) |
| `tronador iac` | Infrastructure-as-code helpers guarded by `.cloudopsworks/.iac`. | [IaC command](iac-command.md) |
| `tronador project` | Detection-driven, implementation-neutral project capabilities. | [Project command](project-command.md) |
| `tronador repos` / `repo` | Repository template lifecycle workflows ported from Tronador Make targets. | [Repos command](repos-command.md) |
| `tronador readme` | Generate, lint, initialize, and manage README generator assets. | [README/docs command](readme-docs-command.md) |
| `tronador docs` | Generate Make target docs, Terraform docs, and copyright headers. | [README/docs command](readme-docs-command.md) |
| `tronador version` | Print the release binary version. | This page |
| `tronador completion` | Generate shell completion scripts from Cobra. | Cobra-generated help |

## `version`

Prints the current executable version.

```bash
tronador version
```

Release builds populate the version from GoReleaser/linker metadata. Development
builds may print the development default.

## `completion`

Generates shell completion scripts for supported shells. Use Cobra help for the
shell-specific target:

```bash
tronador completion --help
tronador completion zsh --help
tronador completion bash --help
```

## `readme` and `docs`

README and documentation generation commands are documented in
[README/docs command](readme-docs-command.md).

Quick examples:

```bash
tronador readme init --workdir .
tronador readme assets sync --project --workdir .
tronador readme build --workdir .
tronador readme build terraform --workdir ./terraform-module
tronador readme lint --workdir .
tronador docs targets --workdir . --all
tronador docs terraform --workdir ./terraform-module
```
