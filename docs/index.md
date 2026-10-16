---
layout: default
title: Tronador CLI documentation
description: Cross-platform automation for Cloud Ops Works workflows.
---

# tronador-cli

A cross-platform CLI for Cloud Ops Works Tronador workflows, AWS resource
automation, IaC module auditing, and repository template lifecycle management.

Tronador brings the repeatable DevOps foundations featured on
[cloudopsworks.co](https://cloudopsworks.co/) to your terminal. The repository
and packages are named `tronador-cli`; the installed command is `tronador`.

[Install Tronador](installation.md) · [Browse commands](commands.md) ·
[View on GitHub](https://github.com/cloudopsworks/tronador-cli)

## Get started with Tronador

Install the latest stable release on Linux or macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/cloudopsworks/tronador-cli/master/scripts/install.sh | sh
```

For Windows, Homebrew, Chocolatey, and native Linux packages, see the
[installation guide](installation.md). Once installed:

```bash
tronador --help
tronador version
```

## What you can do

- **Automate AWS resources:** apply organization tags, copy secrets, remove
  default VPCs, and remediate S3 and EC2 security settings.
- **Audit infrastructure modules:** inspect Terragrunt source pins and preview
  safe patch, minor, or major upgrades.
- **Maintain repositories:** initialize, upgrade, recover, and migrate
  template-derived projects.
- **Run guarded version workflows:** configure GitVersion and manage feature,
  hotfix, release, and tag operations.
- **Generate project documentation:** build README files and Terraform or Make
  target references from the CLI.

Release artifacts include the `tronador` executable for Linux, macOS, Windows,
and FreeBSD. The project is [open source under Apache 2.0](https://github.com/cloudopsworks/tronador-cli/blob/master/LICENSE).

## Explore the command guides

- [Command reference](commands.md) — top-level commands, aliases, global flags,
  and links to the detailed guides.
- [AWS](aws-command.md) — tagging, secret copying, default VPC cleanup, and
  security remediation.
- [IaC](iac-command.md) — guarded Terraform/Terragrunt module checks and
  version updates.
- [Project](project-command.md) — implementation detection and namespace-free
  project capabilities.
- [Repositories](repos-command.md) — template initialization, upgrade,
  recovery, and migration workflows.
- [Versions](versions-command.md) — GitVersion configuration, branching,
  releases, and guarded tags.
- [README and docs](readme-docs-command.md) — README generation, Make target
  docs, Terraform docs, and template assets.

## For maintainers

- [CLI architecture](architecture.md) — package responsibilities,
  request flow, and testing boundaries.
- [Build and publish these docs](github-pages.md) — local Jekyll preview, the
  generated `docs-pages/` output, and the `gh-pages` deployment flow.

## Cloud Ops Works and project links

- [GitHub repository](https://github.com/cloudopsworks/tronador-cli)
- [Cloud Ops Works](https://cloudopsworks.co)
- [Tronador on Cloud Ops Works](https://cloudopsworks.co/resources/tronador-cli-installation/)
