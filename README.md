# tronador-cli

A cross-platform CLI for CloudOps Works Tronador workflows, AWS resource automation, and repository template lifecycle management.

## Features

- **AWS resource automation**: Apply consistent organization metadata, remediate S3/EC2 resources, copy secrets, and remove default VPCs.
- **Repository template lifecycle**: Run the Tronador `make repos/*` workflow from the CLI with `tronador-cli repos`, including template detection, latest-tag upgrades, explicit version upgrades, recovery, migration, CICD metadata updates, and push helpers.
- **Config-driven upgrade paths**: Repository templates and migration plans are loaded from JSON, so future upgrade paths such as `5.11` and `5.12` can be added without rewriting command dispatch code.
- **Release packages**: GoReleaser publishes archives plus native Linux packages (`.deb`, `.rpm`, `.apk`), Homebrew casks, Chocolatey packages, and shell/PowerShell installers from the same release pipeline.
- **Cross-platform support**: Linux, macOS, Windows, and FreeBSD builds are produced from a static `CGO_ENABLED=0` binary.

## Getting Started

### Prerequisites

For installed binaries:

- AWS credentials with the required permissions for `tronador-cli aws ...` commands.
- GitHub authentication through `GH_TOKEN`, `GITHUB_TOKEN`, or `gh auth login` when running `tronador-cli repos` commands that query GitHub or set repository defaults.

For source builds:

- Go 1.25 or later, matching the module toolchain target.
- `make` or `gmake` if you use the repository Makefile targets.

### Installation

Install from published GitHub Release artifacts. The release pipeline publishes shell and PowerShell installers, Homebrew and Chocolatey metadata, native Linux packages, and zip archives. See [docs/installation.md](docs/installation.md) for version pinning, upgrade, uninstall, and maintainer workflow details.

| Platform / manager | Install command |
| --- | --- |
| Linux/macOS shell | `curl -fsSL https://raw.githubusercontent.com/cloudopsworks/tronador-cli/master/scripts/install.sh \| sh` |
| Windows PowerShell | `iwr https://raw.githubusercontent.com/cloudopsworks/tronador-cli/master/scripts/install.ps1 -UseB \| iex` |
| Homebrew | `brew install cloudopsworks/tap/tronador-cli` |
| Chocolatey | `choco install tronador-cli` |

Native Linux packages are attached to each release. Download the asset for your version and architecture, then install it locally:

```bash
# Debian / Ubuntu
sudo apt install ./tronador-cli_<version>_<arch>.deb

# RHEL / Fedora / CentOS
sudo dnf install ./tronador-cli-<version>-1.<arch>.rpm
# or: sudo rpm -i ./tronador-cli-<version>-1.<arch>.rpm

# Alpine Linux
sudo apk add --allow-untrusted ./tronador-cli_<version>_<arch>.apk
```

Direct zip archives remain available for every supported OS/architecture through GitHub Releases.

For development builds from source:

```bash
git clone https://github.com/cloudopsworks/tronador-cli.git
cd tronador-cli
make build
```

### Usage Examples

#### Tag AWS Resources

```bash
tronador-cli aws tag \
  --organization "MyOrg" \
  --organization-unit "DevOps" \
  --application-name "WebApp" \
  --application-type "Service" \
  --target resources
```

#### Remove Default VPCs

```bash
tronador-cli aws remove-default-vpc \
  --exclude-regions "us-west-2,eu-west-1"
```

#### Upgrade repository templates

```bash
# Show available template versions for the detected repository type
tronador-cli repos available --workdir ../my-service

# Run the default full upgrade workflow using the latest tag in the current major/minor line
tronador-cli repos upgrade --workdir ../my-service

# Run the same full workflow against an explicit tag or branch
tronador-cli repos upgrade v5.10.12 --workdir ../my-service
```

`repos upgrade` intentionally exposes a single public workflow. Internal Makefile stages such as fetch, eval, and stack are handled inside the command instead of being separate subcommands.

## Repository Template Commands

`tronador-cli repos` ports the supported public Tronador `make repos/*` targets into the CLI:

- `repos available` / `repos avail` — list latest compatible template tags.
- `repos template init` and `repos template <kind>` — pull configured template repositories.
- `repos clean`, `repos clean template`, and `repos template clean` — clean generated or temporary template files.
- `repos upgrade [version]` — run the full template upgrade workflow; `[version]` is optional.
- `repos recover` — overlay template files without committing.
- `repos push` — stage and commit template upgrade results.
- `repos migrate [template] [version]` — run configured layout migrations.
- `repos cicd update` — update the workflow-version metadata footer.

The command uses the embedded JSON catalog at `internal/repos/default_config.json` by default. Override it with `--config path/to/repos-config.json` when testing new repository types or future migration plans.

For the full command mapping and architecture notes, see [docs/repos-command.md](docs/repos-command.md).

## Make Command Support

The project includes make command support that works with both `make` and `gmake` where the Tronador Makefile provides the target:

```bash
# Build the application
make build

# Build for all platforms
make build-all

# Run tests with coverage
make test-cover

# Clean build artifacts
make clean
```

## Code Modularity Improvements

The application has been refactored to improve code modularity:

1. **Shared AWS Configuration**: Common configuration handling for all CLI commands.
2. **Repository command adapters**: The repos workflow uses native Go Git/GitHub clients for read/fetch operations, with shell fallback where CLI side effects are still intentional.
3. **Cross-platform Make Support**: Automatic detection and use of `gmake` or `make`.
4. **Better Package Organization**: Improved separation of concerns.

For more details on these improvements, see [docs/modularity-improvements.md](docs/modularity-improvements.md).

## Development

### Building

```bash
# Build for current platform
make build

# Build for all platforms
make build-all

# Run tests
make test

# Run tests with coverage
make test-cover
```

### Release validation

```bash
# Validate installer scripts
scripts/validate-installers.sh

# Validate GoReleaser config, including archives, Homebrew, Chocolatey, and nFPM packages
goreleaser check

# Build a local snapshot without publishing
goreleaser release --snapshot --clean --skip=before --skip=publish --skip=sign
```

### Contributing

1. Fork the repository.
2. Create a feature branch.
3. Make your changes.
4. Run tests: `make test`.
5. Submit a pull request.

## License

This project is licensed under the Apache 2.0 License - see the [LICENSE](LICENSE) file for details.
