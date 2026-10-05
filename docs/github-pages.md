# Build and publish these docs

[Documentation home](index.md) · [Command reference](commands.md)

The Markdown guides in `docs/` are the source for the Tronador CLI
documentation site. Jekyll renders them with the GitHub Pages Cayman theme into
`docs-pages/`. The theme's blue/slate palette and system-font styling follow the
current Cloud Ops Works website at [cloudopsworks.co](https://cloudopsworks.co)
and the [`cloudopsworks/main-website`](https://github.com/cloudopsworks/main-website)
design tokens (`#2563eb` blue, `#0f172a` slate, and `#f8fafc` surfaces).

## Maintain the source docs

Keep the command pages linked from [`commands.md`](commands.md) and update them
when public CLI behavior changes. Use Tronador's `docs init` command when
starting docs in a new worktree; `docs targets` is only for repositories that
want the Makefile target list generated from `make help`.

```bash
go run . docs init --workdir .
```

## Build locally

Install Ruby and Bundler, then install the locked Jekyll, Cayman theme, and
GitHub Pages-compatible plugin dependencies and build the site:

```bash
bundle install
./scripts/build-docs-site.sh
```

The generated static site is written to `docs-pages/`. To preview it locally
without the GitHub Pages project prefix:

```bash
./scripts/build-docs-site.sh --baseurl ""
bundle exec jekyll serve \
  --source docs \
  --destination docs-pages \
  --config docs/_config.yml \
  --baseurl ""
```

Then open <http://127.0.0.1:4000/>. The source config leaves `baseurl` empty
because the Pages site uses the repository's configured custom domain.

## Publish

The `Documentation Pages` workflow rebuilds the site and deploys the generated
artifact whenever documentation or its build configuration is merged into
`master`. It can also be started manually from the Actions tab. GitHub Pages
must use **GitHub Actions** as its publishing source. The workflow builds the
`docs-pages/` artifact and deploys it with GitHub's Pages deployment actions,
so it does not need write access to the protected `gh-pages` branch. The
generated static files are also published to `gh-pages` as a repository
snapshot when an authorized maintainer syncs that branch.

After a successful workflow run and DNS propagation, the site is available at
<https://tronador.cloudopsworks.co/>. Check the workflow run and the Pages URL
after publication; a successful local Jekyll build alone does not prove that
the remote Pages configuration or custom-domain DNS is active.
