$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("tronador-install-test-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $workDir | Out-Null

try {
  $archiveName = 'tronador-cli_0.0.1_windows_amd64.zip'
  $archivePath = Join-Path $workDir $archiveName
  $binaryDir = Join-Path $workDir 'source'
  New-Item -ItemType Directory -Path $binaryDir | Out-Null
  Set-Content -Path (Join-Path $binaryDir 'tronador.exe') -Value 'fixture'
  Compress-Archive -Path (Join-Path $binaryDir 'tronador.exe') -DestinationPath $archivePath
  $hash = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
  $manifestPath = Join-Path $workDir 'manifest'
  Set-Content -Path $manifestPath -Value "$hash  $archiveName"
  $global:tronadorFixtureArchiveName = $archiveName
  $global:tronadorFixtureArchivePath = $archivePath
  $global:tronadorFixtureManifestPath = $manifestPath

  function Invoke-WebRequest {
    param([string]$Uri, [string]$OutFile)
    if ($Uri.EndsWith($global:tronadorFixtureArchiveName)) {
      Copy-Item -Path $global:tronadorFixtureArchivePath -Destination $OutFile
    } elseif ($Uri.EndsWith('_windows_SHA256SUMS') -and $global:tronadorFixtureMode -eq 'legacy') {
      $error404 = [System.Exception]::new('Not Found')
      $error404 | Add-Member -NotePropertyName Response -NotePropertyValue ([pscustomobject]@{ StatusCode = 404 })
      throw $error404
    } elseif ($Uri.EndsWith('_windows_SHA256SUMS') -or $Uri.EndsWith('_SHA256SUMS')) {
      Copy-Item -Path $global:tronadorFixtureManifestPath -Destination $OutFile
    } else {
      throw "Unexpected download: $Uri"
    }
  }

  foreach ($mode in @('windows', 'legacy')) {
    $global:tronadorFixtureMode = $mode
    $installDir = Join-Path $workDir $mode
    & (Join-Path $repoRoot 'scripts/install.ps1') -Version v0.0.1 -Architecture amd64 -InstallDir $installDir -NoPathUpdate
    $installedBinary = Join-Path $installDir 'tronador.exe'
    if (-not (Test-Path $installedBinary) -or (Get-Content -Path $installedBinary -Raw).Trim() -ne 'fixture') {
      throw "Installer failed with $mode checksum manifest"
    }
  }
  Write-Host 'PowerShell installer fixture passed: Windows and legacy checksum manifests'
}
finally {
  Remove-Item -Path $workDir -Recurse -Force -ErrorAction SilentlyContinue
}
