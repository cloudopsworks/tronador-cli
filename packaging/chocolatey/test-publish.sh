#!/usr/bin/env sh
set -eu

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT INT TERM
mkdir -p "$work_dir/bin" "$work_dir/source"

artifact_name="tronador-cli.0.0.1.nupkg"
printf 'fixture\n' > "$work_dir/source/tronador-cli.nuspec"
(cd "$work_dir/source" && zip -q "$work_dir/$artifact_name" tronador-cli.nuspec)

cat > "$work_dir/bin/gh" <<'SH'
#!/usr/bin/env sh
set -eu
case "$1 $2" in
  'api -X') printf '%s\n' "$GH_FIXTURE_DRAFT" ;;
  'release download')
    [ "$GH_FIXTURE_DRAFT" = false ] || exit 1
    while [ "$#" -gt 0 ]; do
      if [ "$1" = --dir ]; then
        cp "$GH_FIXTURE_ARCHIVE" "$2/"
        exit 0
      fi
      shift
    done
    exit 1
    ;;
  *) exit 1 ;;
esac
SH
cat > "$work_dir/bin/choco" <<'SH'
#!/usr/bin/env sh
set -eu
[ "$1" = push ] || exit 1
[ -f "$2" ] || exit 1
[ "$3" = --source ] || exit 1
[ "$4" = https://push.chocolatey.org/ ] || exit 1
[ "$5" = --api-key ] || exit 1
[ "$6" = "$CHOCOLATEY_API_KEY" ] || exit 1
printf 'push\n' >> "$GH_FIXTURE_PUSH_LOG"
SH
chmod +x "$work_dir/bin/gh" "$work_dir/bin/choco"

export GH_FIXTURE_ARCHIVE="$work_dir/$artifact_name"
export GH_FIXTURE_PUSH_LOG="$work_dir/push.log"
export CHOCOLATEY_API_KEY='fixture-secret'
export PATH="$work_dir/bin:$PATH"

GH_FIXTURE_DRAFT=true "$script_dir/publish.sh" --version 0.0.1 >"$work_dir/draft.out" 2>&1 && {
  echo 'Draft release was incorrectly published' >&2
  exit 1
}
[ ! -f "$GH_FIXTURE_PUSH_LOG" ] || { echo 'Draft release reached Chocolatey push' >&2; exit 1; }

GH_FIXTURE_DRAFT=false "$script_dir/publish.sh" --version 0.0.1 >"$work_dir/public.out" 2>&1
[ "$(wc -l < "$GH_FIXTURE_PUSH_LOG" | tr -d ' ')" = 1 ] || { echo 'Expected exactly one Chocolatey push' >&2; exit 1; }
if grep -F 'fixture-secret' "$work_dir/draft.out" "$work_dir/public.out" >/dev/null; then
  echo 'Chocolatey API key leaked to output' >&2
  exit 1
fi

printf 'Chocolatey post-release publication fixture passed\n'
