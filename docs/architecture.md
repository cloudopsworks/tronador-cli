# CLI architecture

[Documentation home](index.md) · [Command reference](commands.md)

Tronador CLI keeps command parsing, workflow logic, and integrations in separate
packages so command behavior can be tested without coupling every feature to
the Cobra command tree.

## Request flow

1. [`main.go`](https://github.com/cloudopsworks/tronador-cli/blob/master/main.go) sets the release version and starts the CLI.
2. `internal/cli` defines the Cobra command tree, flags, and user-facing help.
3. Domain packages under `internal/` implement workflows and return errors or
   structured results to the command layer.
4. Shared helpers such as `internal/tools`, `internal/utils`, and
   `internal/replacement` handle tool resolution, common utilities, and
   cross-platform file replacement.

## Feature packages

| Package | Responsibility |
| --- | --- |
| `internal/aws` | AWS configuration, clients, resource discovery, and AWS operations. |
| `internal/iac` | Terraform/Terragrunt module source inspection and guarded updates. |
| `internal/project` | Implementation-marker detection and project capability execution. |
| `internal/repos` | Repository template configuration, GitHub/Git operations, and upgrade workflows. |
| `internal/versions` | Branching, release, tag, and GitVersion workflows. |
| `internal/readme` | README generation, include preparation, and runtime template assets. |
| `internal/docs` | Make target documentation, Terraform docs, and copyright-header operations. |

The command reference links to the public interface for each feature:
[AWS](aws-command.md), [IaC](iac-command.md), [project](project-command.md),
[repositories](repos-command.md), [versions](versions-command.md), and
[README/docs generation](readme-docs-command.md).

## Testing and change boundaries

Command packages cover flag wiring and user-facing behavior; domain packages
contain focused tests for workflow logic. Prefer adding behavior to the owning
domain package and keep Cobra handlers thin. Avoid invoking cloud APIs or
mutating repositories from tests unless a test explicitly uses a controlled
fixture or fake client.
