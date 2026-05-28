# tronador-cli

A powerful CLI tool for managing AWS resources with consistent organization tagging and VPC cleanup capabilities.

## Features

- **Organization Tagging**: Apply consistent organization metadata to AWS resources
- **VPC Cleanup**: Remove default VPCs from all regions in your AWS account
- **Cross-platform Support**: Works on Linux, macOS, and Windows
- **Modular Architecture**: Clean separation of concerns for easy maintenance

## Getting Started

### Prerequisites
- Go 1.21 or later
- AWS CLI configured with appropriate permissions

### Installation

Install from published GitHub Release binaries using shell, PowerShell, Homebrew, or Chocolatey package assets.
See [docs/installation.md](docs/installation.md) for package-manager setup, version pinning, upgrade, and uninstall instructions.

```bash
# Linux/macOS latest stable
curl -fsSL https://raw.githubusercontent.com/cloudopsworks/tronador-cli/master/scripts/install.sh | sh
```

```powershell
# Windows latest stable
iwr https://raw.githubusercontent.com/cloudopsworks/tronador-cli/master/scripts/install.ps1 -UseB | iex
```

For development builds from source:

```bash
# Clone the repository
git clone https://github.com/cloudopsworks/tronador-cli.git
cd tronador-cli

# Build the application
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

## Make Command Support

The project now includes enhanced make command support that works with both `make` and `gmake`:

```bash
# Build the application (automatically detects available make tool)
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

1. **Shared AWS Configuration**: Common configuration handling for all CLI commands
2. **Cross-platform Make Support**: Automatic detection and use of gmake or make
3. **Better Package Organization**: Improved separation of concerns

For more details on these improvements, see [docs/modularity-improvements.md](docs/modularity-improvements.md)

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

### Contributing
1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests: `make test`
5. Submit a pull request

## License

This project is licensed under the Apache 2.0 License - see the [LICENSE](LICENSE) file for details.
