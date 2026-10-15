[CmdletBinding()]
param(
  [string]$Version = $env:TRONADOR_CLI_VERSION,
  [string]$InstallDir = $env:TRONADOR_CLI_INSTALL_DIR,
  [string]$Repository = $(if ($env:TRONADOR_CLI_REPOSITORY) { $env:TRONADOR_CLI_REPOSITORY } else { 'cloudopsworks/tronador-cli' }),
  [string]$Architecture = $env:TRONADOR_CLI_ARCH,
  [switch]$NoVerify,
  [switch]$NoPathUpdate,
  [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$ProjectName = 'tronador-cli'
$BinaryName = 'tronador.exe'

function Resolve-TronadorArch {
  param([string]$RequestedArchitecture)
  if (-not [string]::IsNullOrWhiteSpace($RequestedArchitecture)) {
    $arch = $RequestedArchitecture.ToLowerInvariant()
  } else {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
  }
  switch ($arch) {
    'x64' { return 'amd64' }
    'amd64' { return 'amd64' }
    'arm64' { return 'arm64' }
    default { throw "Unsupported Windows architecture: $arch" }
  }
}

function Resolve-TronadorTag {
  param([string]$RequestedVersion, [string]$Repo)
  if ([string]::IsNullOrWhiteSpace($RequestedVersion)) {
    # GitHub's latest endpoint resolves the latest stable, non-draft, non-prerelease release.
    return (Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest").tag_name
  }
  if ($RequestedVersion.StartsWith('v')) { return $RequestedVersion }
  return "v$RequestedVersion"
}

function Get-ChecksumFromManifest {
  param([string]$ManifestPath, [string]$ArchiveName)
  foreach ($line in Get-Content -Path $ManifestPath) {
    $parts = $line.Trim() -split '\s+'
    if ($parts.Count -ge 2 -and [System.IO.Path]::GetFileName($parts[-1].TrimStart('*')) -eq $ArchiveName) {
      return $parts[0]
    }
  }
  throw "Checksum for $ArchiveName not found in $(Split-Path -Leaf $ManifestPath)"
}

$arch = Resolve-TronadorArch -RequestedArchitecture $Architecture
$tag = Resolve-TronadorTag -RequestedVersion $Version -Repo $Repository
$assetVersion = $tag.TrimStart('v')
$archive = "${ProjectName}_${assetVersion}_windows_${arch}.zip"
$checksums = "${ProjectName}_${assetVersion}_windows_SHA256SUMS"
$legacyChecksums = "${ProjectName}_${assetVersion}_SHA256SUMS"
$baseUrl = "https://github.com/$Repository/releases/download/$tag"
$archiveUrl = "$baseUrl/$archive"
$checksumsUrl = "$baseUrl/$checksums"
$legacyChecksumsUrl = "$baseUrl/$legacyChecksums"

if ([string]::IsNullOrWhiteSpace($InstallDir)) {
  $localAppData = $env:LOCALAPPDATA
  if ([string]::IsNullOrWhiteSpace($localAppData)) {
    $localAppData = Join-Path $HOME 'AppData/Local'
  }
  $InstallDir = Join-Path $localAppData 'Programs\tronador-cli\bin'
}
$installPath = Join-Path $InstallDir $BinaryName

if ($DryRun) {
  Write-Host "Would install tronador-cli:"
  Write-Host "  release: $tag"
  Write-Host "  target:  windows/$arch"
  Write-Host "  archive: $archiveUrl"
  Write-Host "  path:    $installPath"
  exit 0
}

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("tronador-cli-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempDir | Out-Null
try {
  $archivePath = Join-Path $tempDir $archive
  $checksumsPath = Join-Path $tempDir $checksums
  Invoke-WebRequest -Uri $archiveUrl -OutFile $archivePath
  if (-not $NoVerify) {
    try {
      Invoke-WebRequest -Uri $checksumsUrl -OutFile $checksumsPath
    } catch {
      if ($null -eq $_.Exception.Response -or [int]$_.Exception.Response.StatusCode -ne 404) {
        throw
      }
      # Releases before the split used a single checksum manifest.
      $checksumsPath = Join-Path $tempDir $legacyChecksums
      Invoke-WebRequest -Uri $legacyChecksumsUrl -OutFile $checksumsPath
    }
    $expected = Get-ChecksumFromManifest -ManifestPath $checksumsPath -ArchiveName $archive
    $actual = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($expected.ToLowerInvariant() -ne $actual) {
      throw "Checksum mismatch for $archive"
    }
  }

  $extractDir = Join-Path $tempDir 'extract'
  Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force
  $binary = Get-ChildItem -Path $extractDir -Filter $BinaryName -File -Recurse | Select-Object -First 1
  if ($null -eq $binary) {
    throw "Unable to locate $BinaryName inside $archive"
  }

  New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
  Copy-Item -Path $binary.FullName -Destination $installPath -Force

  if (-not $NoPathUpdate) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = @($userPath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    if ($entries -notcontains $InstallDir) {
      [Environment]::SetEnvironmentVariable('Path', ($entries + $InstallDir -join ';'), 'User')
      Write-Host "Added $InstallDir to the user PATH. Open a new terminal to use tronador-cli."
    }
  }

  Write-Host "Installed tronador-cli $tag to $installPath"
}
finally {
  Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
}
