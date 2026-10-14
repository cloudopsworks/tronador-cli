#!/usr/bin/env sh
set -eu

usage() {
  cat <<'USAGE'
Usage: publish.sh --version VERSION [--tag TAG]

Pushes the native GoReleaser Chocolatey package only after its GitHub release
is public. Requires gh authentication, choco, and CHOCOLATEY_API_KEY.
USAGE
}

version=""
tag=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) version="$2"; shift 2 ;;
    --tag) tag="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$version" ] || { echo "--version is required" >&2; exit 2; }
package_version=${version#v}
[ -n "$tag" ] || tag="v$package_version"
[ "$tag" = "v$package_version" ] || { echo "Tag must match version: v$package_version" >&2; exit 2; }
[ -n "${CHOCOLATEY_API_KEY:-}" ] || { echo "CHOCOLATEY_API_KEY is required to publish Chocolatey packages" >&2; exit 1; }
command -v gh >/dev/null 2>&1 || { echo "gh is required to verify and download the GitHub release" >&2; exit 1; }
command -v choco >/dev/null 2>&1 || { echo "choco is required to publish Chocolatey packages" >&2; exit 1; }

repo="cloudopsworks/tronador-cli"
draft=$(gh api -X GET "repos/$repo/releases/tags/$tag" --jq '.draft')
[ "$draft" = false ] || { echo "GitHub release $tag is not confirmed public (draft=$draft)" >&2; exit 1; }

artifact_name="tronador-cli.${package_version}.nupkg"
work_dir=$(mktemp -d)
cleanup() { rm -rf "$work_dir"; }
trap cleanup EXIT INT TERM

gh release download "$tag" --repo "$repo" --pattern "$artifact_name" --dir "$work_dir"
artifact="$work_dir/$artifact_name"
[ -f "$artifact" ] || { echo "Chocolatey package not found in release $tag: $artifact_name" >&2; exit 1; }

choco push "$artifact" --source https://push.chocolatey.org/ --api-key "$CHOCOLATEY_API_KEY"
