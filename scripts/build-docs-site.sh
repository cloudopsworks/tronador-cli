#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

bundle exec jekyll build \
  --source "$repo_root/docs" \
  --destination "$repo_root/docs-pages" \
  --config "$repo_root/docs/_config.yml" \
  "$@"

# The optional-front-matter plugin creates rendered HTML pages while Jekyll
# also copies the original Markdown source. Publish only the rendered pages.
find "$repo_root/docs-pages" -type f -name '*.md' -delete
