---
layout: default
title: Tronador CLI Documentation
description: Installation, command references, and maintainer guides for the Tronador CLI.
---

# Tronador CLI

Cross-platform automation for Cloud Ops Works workflows. Use Tronador to manage
AWS resources, inspect IaC module pins, run project capabilities, and automate
repository-template and release workflows.

[Install Tronador](installation.md) · [Browse all commands](commands.md) ·
[View the source on GitHub](https://github.com/cloudopsworks/tronador-cli)

## Get started

Install the latest stable release for your operating system, then check the
available commands:

```bash
tronador --help
tronador version
```

The [installation guide](installation.md) covers shell and PowerShell
installers, Linux packages, Homebrew, Chocolatey, and release selection.

## Command guides

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

## Maintainer guides

- [CLI architecture](architecture.md) — package responsibilities,
  request flow, and testing boundaries.
- [Build and publish these docs](github-pages.md) — local Jekyll preview, the
  generated `docs-pages/` output, and the `gh-pages` deployment flow.

## Project links

- [GitHub repository](https://github.com/cloudopsworks/tronador-cli)
- [Cloud Ops Works](https://cloudopsworks.co)
