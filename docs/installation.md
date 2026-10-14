# Installing `tronador-cli`

[Documentation home](index.md)

The supported installation sources are published GitHub Release artifacts from
`cloudopsworks/tronador-cli`. Installers never build from source.

## Binary names

The repository, release artifacts, cask, and package names use `tronador-cli`,
but GoReleaser builds the released executable as `tronador` (`binary: tronador`
in `.goreleaser.yml`). A plain source build from the repository root still
emits `./tronador-cli` because Go uses the module directory name by default. Use
`go build -o tronador .` to create a local binary with the same name as release
automation.

Use `tronador ...` for installed release examples. Use `./tronador-cli ...` only
when running the default local source build output.

## POSIX shell installer: Linux and macOS

Supported targets:

- macOS `amd64`, `arm64`
- Linux `amd64`, `arm64`

Install latest stable release:

```bash
curl -fsSL https://raw.githubusercontent.com/cloudopsworks/tronador-cli/master/scripts/install.sh | sh
```

Install an explicit version (explicit versions may point to prereleases):

```bash
curl -fsSL https://raw.githubusercontent.com/cloudopsworks/tronador-cli/master/scripts/install.sh | sh -s -- --version v1.2.3
```

Useful options:

```bash
scripts/install.sh --install-dir "$HOME/.local/bin"
scripts/install.sh --repo cloudopsworks/tronador-cli --version v1.2.3 --dry-run
```

Upgrade by rerunning the installer with no version for latest stable, or with a
new explicit version.

Uninstall a shell install by removing the installed binary. Release archives expose
`tronador`; legacy local source builds may be named `tronador-cli`:

```bash
rm -f /usr/local/bin/tronador
rm -f /usr/local/bin/tronador-cli
# or, if installed for the current user:
rm -f "$HOME/.local/bin/tronador"
rm -f "$HOME/.local/bin/tronador-cli"
```

## PowerShell installer: Windows

Supported target for the first release: Windows `amd64`. The installer detects
`arm64` as a first-class architecture so Windows ARM support can be enabled once
matching release artifacts are published.

Install latest stable release:

```powershell
iwr https://raw.githubusercontent.com/cloudopsworks/tronador-cli/master/scripts/install.ps1 -UseB | iex
```

Install an explicit version:

```powershell
& .\scripts\install.ps1 -Version v1.2.3
```

Maintainers can validate URL resolution without downloading artifacts:

```powershell
& .\scripts\install.ps1 -Version v1.2.3 -Architecture amd64 -DryRun
```

By default, the installer writes to:

```text
%LOCALAPPDATA%\Programs\tronador-cli\bin
```

and adds that directory to the user `PATH` unless `-NoPathUpdate` is provided.

Upgrade by rerunning the installer. Uninstall a PowerShell install by deleting
`tronador.exe` (or legacy `tronador-cli.exe`) from the install directory and removing the directory from the
user `PATH` if it is no longer needed.

## Native Linux packages: DEB, RPM, and APK

GoReleaser generates native Linux packages from the same release binaries using
its `nfpms` pipeline. Packages are attached to each GitHub Release as assets;
there is no separate apt/yum/apk repository required.

Download the package matching your version and architecture, then install it
locally:

```bash
# Debian / Ubuntu
sudo apt install ./tronador-cli_<version>_<arch>.deb

# RHEL / Fedora / CentOS
sudo dnf install ./tronador-cli-<version>-1.<arch>.rpm
# or: sudo rpm -i ./tronador-cli-<version>-1.<arch>.rpm

# Alpine Linux
sudo apk add --allow-untrusted ./tronador-cli_<version>_<arch>.apk
```

The packages install the `tronador` executable into `/usr/bin` and include README/license
documentation under `/usr/share/doc/tronador-cli/`.

Upgrade by installing a newer package from a later release. Uninstall with the
platform package manager, for example `sudo apt remove tronador-cli`,
`sudo dnf remove tronador-cli`, or `sudo apk del tronador-cli`.

## Homebrew

Homebrew metadata is owned by the GoReleaser pipeline, not by a separate local
generator. The `.goreleaser.yml` `homebrew_casks` section derives the cask from
the release archives and checksum manifest, then publishes it to the configured
Homebrew tap when `HOMEBREW_TAP_TOKEN` is available in the release environment.

User-facing install command once the tap is published:

```bash
brew install cloudopsworks/tap/tronador-cli
```

Homebrew handles upgrades and uninstall:

```bash
brew update && brew upgrade tronador-cli
brew uninstall tronador-cli
```

## Chocolatey

Chocolatey package metadata is owned by the native `chocolateys` section in
`windows.goreleaser.yml`. GoReleaser uses the Windows ZIP archives to generate
`tronador-cli.<version>.nupkg`; the package installs the `tronador` command.
The Windows pass uploads this package to the GitHub draft but does not push to
the Chocolatey feed. After the default pass publishes the GitHub release,
`packaging/chocolatey/publish.sh` verifies the release is public, downloads the
native package, and pushes it with `choco` and `CHOCOLATEY_API_KEY`.

User-facing install command once the package is published:

```powershell
choco install tronador-cli
```

Chocolatey handles upgrades and uninstall:

```powershell
choco upgrade tronador-cli
choco uninstall tronador-cli
```

## Release-selection policy

- Scripts default to the GitHub `latest` release endpoint, which resolves stable
  releases.
- Scripts allow prerelease installation only when the user explicitly passes a
  version/tag.
- Homebrew, Chocolatey, and native Linux package metadata is generated by the
  GoReleaser pipeline for stable release jobs, using the same archive names,
  `tronador` release binary, and checksums as the GitHub Release.

## Maintainer workflow

1. Run `windows.goreleaser.yml` first to upload Windows archives, its distinct
   `*_windows_SHA256SUMS` manifest, `install.ps1`, and native `.nupkg` to the
   existing draft. A `choco` executable is required for this pass.
   Then run `.goreleaser.yml` on the same tag to add other platforms, packages,
   Homebrew, and `install.sh`, and publish the draft. Do not push the Chocolatey
   package until the GitHub release is public.
2. Validate installer syntax locally when editing release scripts:
   ```bash
   scripts/validate-installers.sh
   ```
3. Validate GoReleaser configuration before merging release changes:
   ```bash
   goreleaser check --config windows.goreleaser.yml
   goreleaser check --config .goreleaser.yml
   packaging/chocolatey/test-native.sh
   ```
4. The release workflow must run both configurations in that order. They reuse
   the same GitHub draft without replacing previously uploaded assets. Only
   after the second pass succeeds should the native Chocolatey package be
   pushed to the feed.
