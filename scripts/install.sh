#!/usr/bin/env sh
set -eu

PROJECT_NAME="tronador-cli"
BINARY_NAME="tronador-cli"
REPOSITORY="${TRONADOR_CLI_REPOSITORY:-cloudopsworks/tronador-cli}"
VERSION="${TRONADOR_CLI_VERSION:-}"
INSTALL_DIR="${TRONADOR_CLI_INSTALL_DIR:-}"
VERIFY_CHECKSUM="true"
DRY_RUN="false"

usage() {
  cat <<USAGE
Install ${BINARY_NAME} from GitHub Releases.

Usage: install.sh [--version vX.Y.Z] [--install-dir DIR] [--repo OWNER/REPO] [--no-verify] [--dry-run]

Defaults to the latest stable GitHub Release. Passing --version explicitly may
install a prerelease tag if that tag exists.
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --install-dir) INSTALL_DIR="$2"; shift 2 ;;
    --repo) REPOSITORY="$2"; shift 2 ;;
    --no-verify) VERIFY_CHECKSUM="false"; shift ;;
    --dry-run) DRY_RUN="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "Missing required command: $1" >&2; exit 1; }
}

need curl
need unzip

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) echo "Unsupported OS: $os" >&2; exit 1 ;;
esac

machine=$(uname -m)
case "$machine" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "Unsupported architecture: $machine" >&2; exit 1 ;;
esac

if [ -z "$VERSION" ]; then
  tag=$(curl -fsSL "https://api.github.com/repos/${REPOSITORY}/releases/latest" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
  if [ -z "$tag" ]; then
    echo "Unable to resolve latest stable release for ${REPOSITORY}" >&2
    exit 1
  fi
else
  case "$VERSION" in
    v*) tag="$VERSION" ;;
    *) tag="v${VERSION}" ;;
  esac
fi
asset_version=${tag#v}
archive="${PROJECT_NAME}_${asset_version}_${os}_${arch}.zip"
checksums="${PROJECT_NAME}_${asset_version}_SHA256SUMS"
base_url="https://github.com/${REPOSITORY}/releases/download/${tag}"
archive_url="${base_url}/${archive}"
checksums_url="${base_url}/${checksums}"

if [ -z "$INSTALL_DIR" ]; then
  if [ -w /usr/local/bin ]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="${HOME}/.local/bin"
  fi
fi

if [ "$DRY_RUN" = "true" ]; then
  cat <<PLAN
Would install ${BINARY_NAME}:
  release: ${tag}
  target:  ${os}/${arch}
  archive: ${archive_url}
  path:    ${INSTALL_DIR}/${BINARY_NAME}
PLAN
  exit 0
fi

tmp=$(mktemp -d)
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT INT TERM

curl -fL "$archive_url" -o "$tmp/$archive"
if [ "$VERIFY_CHECKSUM" = "true" ]; then
  curl -fL "$checksums_url" -o "$tmp/$checksums"
  expected=$(grep "[[:space:]]\*\?${archive}$" "$tmp/$checksums" | awk '{print $1}' | head -n 1)
  if [ -z "$expected" ]; then
    echo "Checksum for ${archive} not found in ${checksums}" >&2
    exit 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
  else
    actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
  fi
  if [ "$expected" != "$actual" ]; then
    echo "Checksum mismatch for ${archive}" >&2
    exit 1
  fi
fi

unzip -q "$tmp/$archive" -d "$tmp/extract"
binary=$(find "$tmp/extract" -type f \( -name "${PROJECT_NAME}" -o -name "${PROJECT_NAME}_v${asset_version}" -o -name "${PROJECT_NAME}*" \) | head -n 1)
if [ -z "$binary" ]; then
  echo "Unable to locate ${PROJECT_NAME} binary inside ${archive}" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
install -m 0755 "$binary" "${INSTALL_DIR}/${BINARY_NAME}"
printf 'Installed %s %s to %s\n' "$BINARY_NAME" "$tag" "${INSTALL_DIR}/${BINARY_NAME}"
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *) printf 'Note: %s is not currently in PATH. Add it to use %s directly.\n' "$INSTALL_DIR" "$BINARY_NAME" ;;
esac
