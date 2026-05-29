#!/usr/bin/env sh
set -eu

usage() {
  cat <<'USAGE'
Usage: package.sh --artifact PATH --artifact-name NAME --version VERSION --tag TAG --out DIR

Builds the tronador-cli Chocolatey package from a GoReleaser Windows amd64
archive. Non-Windows-amd64 artifacts are ignored so this script can be used as a
GoReleaser custom publisher over the full archive set.
USAGE
}

artifact=""
artifact_name=""
version=""
tag=""
out_dir="dist/chocolatey"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --artifact) artifact="$2"; shift 2 ;;
    --artifact-name) artifact_name="$2"; shift 2 ;;
    --version) version="$2"; shift 2 ;;
    --tag) tag="$2"; shift 2 ;;
    --out) out_dir="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$artifact" ] || { echo "--artifact is required" >&2; exit 2; }
[ -n "$artifact_name" ] || artifact_name=$(basename "$artifact")
[ -n "$version" ] || { echo "--version is required" >&2; exit 2; }
[ -n "$tag" ] || tag="v$version"

case "$artifact_name" in
  *windows_amd64.zip) ;;
  *)
    echo "Skipping Chocolatey package for non-windows-amd64 artifact: $artifact_name"
    exit 0
    ;;
esac

[ -f "$artifact" ] || { echo "Artifact not found: $artifact" >&2; exit 1; }
command -v zip >/dev/null 2>&1 || { echo "zip is required to build the Chocolatey nupkg" >&2; exit 1; }

case "$tag" in
  v*) release_tag="$tag" ;;
  *) release_tag="v$tag" ;;
esac
package_version=${version#v}
if command -v sha256sum >/dev/null 2>&1; then
  checksum=$(sha256sum "$artifact" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  checksum=$(shasum -a 256 "$artifact" | awk '{print $1}')
else
  echo "sha256sum or shasum is required to checksum the Chocolatey artifact" >&2
  exit 1
fi
archive_url="https://github.com/cloudopsworks/tronador-cli/releases/download/${release_tag}/${artifact_name}"

work_dir=$(mktemp -d)
cleanup() { rm -rf "$work_dir"; }
trap cleanup EXIT INT TERM

pkg_root="$work_dir/package"
mkdir -p "$pkg_root/tools" "$out_dir"

cat > "$pkg_root/tronador-cli.nuspec" <<NUSPEC
<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2015/06/nuspec.xsd">
  <metadata>
    <id>tronador-cli</id>
    <version>${package_version}</version>
    <packageSourceUrl>https://github.com/cloudopsworks/tronador-cli/tree/master/packaging/chocolatey</packageSourceUrl>
    <owners>CloudOpsWorks</owners>
    <title>tronador-cli</title>
    <authors>CloudOpsWorks</authors>
    <projectUrl>https://github.com/cloudopsworks/tronador-cli</projectUrl>
    <licenseUrl>https://github.com/cloudopsworks/tronador-cli/blob/master/.cloudopsworks/LICENSE</licenseUrl>
    <requireLicenseAcceptance>false</requireLicenseAcceptance>
    <projectSourceUrl>https://github.com/cloudopsworks/tronador-cli</projectSourceUrl>
    <docsUrl>https://github.com/cloudopsworks/tronador-cli/blob/master/docs/installation.md</docsUrl>
    <bugTrackerUrl>https://github.com/cloudopsworks/tronador-cli/issues</bugTrackerUrl>
    <tags>tronador cli cloudopsworks aws devops</tags>
    <summary>CLI tool for CloudOps Works Tronador workflows and AWS automation.</summary>
    <description>tronador-cli provides repository and AWS automation commands for CloudOps Works Tronador workflows.</description>
    <releaseNotes>https://github.com/cloudopsworks/tronador-cli/releases/tag/${release_tag}</releaseNotes>
  </metadata>
</package>
NUSPEC

cat > "$pkg_root/tools/chocolateyInstall.ps1" <<PS1
\$ErrorActionPreference = 'Stop'
\$packageName = 'tronador-cli'
\$toolsDir = Split-Path -Parent \$MyInvocation.MyCommand.Definition
\$installDir = Join-Path \$toolsDir 'tronador-cli'
\$packageArgs = @{
  packageName   = \$packageName
  unzipLocation = \$installDir
  url64bit      = '${archive_url}'
  checksum64    = '${checksum}'
  checksumType64 = 'sha256'
}
Install-ChocolateyZipPackage @packageArgs
\$binary = Get-ChildItem -Path \$installDir -Filter 'tronador-cli*.exe' -File -Recurse | Select-Object -First 1
if (\$null -eq \$binary) {
  throw 'Unable to locate tronador-cli.exe after extraction.'
}
\$target = Join-Path \$installDir 'tronador-cli.exe'
if (\$binary.FullName -ne \$target) {
  Copy-Item -Path \$binary.FullName -Destination \$target -Force
}
Install-BinFile -Name 'tronador-cli' -Path \$target
PS1

cat > "$pkg_root/tools/chocolateyUninstall.ps1" <<'PS1'
$ErrorActionPreference = 'Stop'
$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
Uninstall-BinFile -Name 'tronador-cli' -ErrorAction SilentlyContinue
Remove-Item -Path (Join-Path $toolsDir 'tronador-cli') -Recurse -Force -ErrorAction SilentlyContinue
PS1

cat > "$pkg_root/tools/VERIFICATION.txt" <<VERIFY
VERIFICATION

Package: tronador-cli ${package_version}
Artifact: ${artifact_name}
URL: ${archive_url}
SHA256: ${checksum}

The package installs the upstream GitHub Release archive produced by GoReleaser.
The SHA256 checksum above is computed from the GoReleaser archive artifact used
for this package.
VERIFY

nupkg="$out_dir/tronador-cli.${package_version}.nupkg"
case "$nupkg" in
  /*) nupkg_path="$nupkg" ;;
  *) nupkg_path="$PWD/$nupkg" ;;
esac
(
  cd "$pkg_root"
  zip -qr "$nupkg_path" tronador-cli.nuspec tools
)
echo "Generated Chocolatey package: $nupkg"

publish_flag=${TRONADOR_CHOCOLATEY_PUBLISH:-false}
case "$publish_flag" in
  1|true|TRUE|yes|YES)
    [ -n "${CHOCOLATEY_API_KEY:-}" ] || { echo "CHOCOLATEY_API_KEY is required when TRONADOR_CHOCOLATEY_PUBLISH=true" >&2; exit 1; }
    if command -v dotnet >/dev/null 2>&1; then
      dotnet nuget push "$nupkg_path" --api-key "$CHOCOLATEY_API_KEY" --source https://push.chocolatey.org/
    elif command -v choco >/dev/null 2>&1; then
      choco push "$nupkg_path" --source https://push.chocolatey.org/ --api-key "$CHOCOLATEY_API_KEY"
    else
      echo "dotnet or choco is required to publish Chocolatey packages" >&2
      exit 1
    fi
    ;;
  *)
    echo "Chocolatey publish skipped; set TRONADOR_CHOCOLATEY_PUBLISH=true with CHOCOLATEY_API_KEY to push."
    ;;
esac
