#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

if command -v bash >/dev/null 2>&1; then
  bash -n scripts/install.sh
else
  sh -n scripts/install.sh
fi

if command -v pwsh >/dev/null 2>&1; then
  powershell_cmd=pwsh
elif command -v powershell >/dev/null 2>&1; then
  powershell_cmd=powershell
else
  echo "PowerShell parser (pwsh/powershell) is required to validate scripts/install.ps1" >&2
  exit 1
fi
"$powershell_cmd" -NoProfile -NonInteractive -Command "[scriptblock]::Create((Get-Content -Raw -LiteralPath 'scripts/install.ps1')) | Out-Null"

sh -n packaging/chocolatey/publish.sh
packaging/chocolatey/test-publish.sh
"$powershell_cmd" -NoProfile -NonInteractive -File scripts/test-install.ps1

printf 'Installer scripts validated: scripts/install.sh, scripts/install.ps1\n'
