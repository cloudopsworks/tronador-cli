# Versions command

`tronador versions` manages a repository's branching workflow and its
GitVersion configuration. It is the CLI replacement for the supported
GitFlow-oriented Make targets; it does **not** generate an application's
`VERSION` file. Use [`tronador project version`](project-command.md#version-generation)
for application-version metadata.

The root aliases `gitflow`, `gf`, `githubflow`, and `flow` all select this
same command tree. For example, `tronador gf init --gitflow` and
`tronador versions init --gitflow` are equivalent.

```bash
tronador versions --help
tronador versions init --githubflow
tronador versions feature start add-metrics
tronador versions feature publish
```

## Repository requirements and shared guards

All commands operate on `--workdir` (default `.`). `--git <path>` selects the
Git executable when it is not available on `PATH`; Git operations otherwise
use `git`. `--main-branch <name>` overrides primary-branch discovery. The
normal root `--dry-run` flag is mutation-free for every `versions` operation:
it reports the selected action without changing configuration, fetching,
checking out, merging, creating branches or tags, pushing, deleting, or opening
a pull request.

The command first reads the `# Agents: WayOfWork=<workflow>` header in
`.cloudopsworks/gitversion.yaml`. For a headerless legacy config, it can also
detect the workflow when that file byte-matches exactly one unchanged selector
checked into `HEAD` (`gitversion_gitflow.yaml`, `gitversion_githubflow.yaml`, or
`gitversion_trunkbased.yaml`). Non-purge commands retain the historical
GitFlow fallback when neither method is reliable. Destructive `purge` commands
do not: the active config itself must also be unchanged from `HEAD`, and an
ambiguous match, no match, modified/untracked selector, malformed header, or
duplicate header stops before fetch, checkout, or deletion.
Branching behavior is then workflow-sensitive:

| Way of work | Feature/release base | `develop` | Support branches |
| --- | --- | --- | --- |
| GitFlow | `develop` | Required | Supported |
| GitHub Flow | detected primary branch | Not used | Rejected |
| Trunk-based | detected primary branch | Not used | Rejected |

The primary branch is resolved from `origin/HEAD`, then `origin/main` or
`origin/master`; operations stop if none can be identified unless
`--main-branch` supplies an override. An override must be a safe ref name and
must exist as a branch at `origin`; a local-only or misspelled override is
rejected. Commands reject unsafe branch and tag input; they invoke Git with
typed arguments rather than shell interpolation.

Publishing checks out the named branch and pushes it with `origin` as its
upstream. Commands that create a pull request or tag require the named local
branch to exactly equal its branch published at `origin`; publish first if that
guard fails. Pull-request finishes require `gh` authentication.

Purge is deliberately stricter than a branch deletion. It fetches `origin`,
requires a remote branch to have a matching local branch for parity checking,
and proves the source branch is merged into the freshly fetched workflow base
before changing branches or deleting anything. An unmerged local-only branch
is rejected; a branch absent both locally and remotely is already purged and
is a successful no-op. Deletion uses Git's safe merged-branch delete, never a
force delete.

## Initialize a way of work

`init` first verifies that these three regular, non-symlink files exist:

```text
.cloudopsworks/gitversion_gitflow.yaml
.cloudopsworks/gitversion_githubflow.yaml
.cloudopsworks/gitversion_trunkbased.yaml
```

It copies the selected file atomically to `.cloudopsworks/gitversion.yaml`.
When that target already exists, it warns with its detected WayOfWork; an
unrecognised or missing header is reported as the compatible GitFlow default.

```bash
tronador versions init --gitflow
tronador versions init --githubflow
tronador versions init --trunkbased
tronador versions init --trunk       # alias for --trunkbased
```

Only one workflow flag may be used. With no flag, `init` reads
`.cloudopsworks/cloudopsworks-ci.yaml` when its `config.gitFlow.enabled`
setting is available:

- `true`, or an unsupported/missing setting, selects GitFlow.
- `false` opens a numeric interactive selector for GitFlow, GitHub Flow, or
  trunk-based development.
- Selecting GitFlow changes the supported `config.gitFlow.enabled` scalar to
  `true` while preserving surrounding YAML comments and layout. Selecting a
  non-GitFlow workflow does not enable it.

GitFlow alone creates and pushes `develop`. Before doing so it requires a
clean worktree, an `origin` remote, a successful `origin` fetch, and a primary
branch whose local commit exactly matches `origin/<primary>`. If `develop`
already exists at `origin`, it is retained only when any local `develop` is
identical; a divergent local and remote `develop` fails closed. When `origin`
does not yet have `develop`, an existing local `develop` can be published only
when it matches the synchronized primary branch. GitHub Flow and trunk-based
selection never create `develop`.

## Feature branches

```bash
tronador versions feature start <name>
tronador versions feature publish [name]
tronador versions feature finish [name]
tronador versions feature purge [name]
```

`start` creates `feature/<name>` from `develop` under GitFlow and from the
primary branch otherwise. The other operations infer `<name>` from the current
`feature/<name>` (or `feat/<name>`) branch when it is omitted. `finish` creates
a guarded GitHub pull request into `develop` for GitFlow or the primary branch
for GitHub Flow and trunk-based repositories; it does not merge the pull
request. `purge` switches away from the branch before deleting its local and
remote forms, selecting `develop` for GitFlow or the primary branch otherwise.

## Hotfix branches

```bash
tronador versions hotfix start
tronador versions hotfix publish
tronador versions hotfix finish
tronador versions hotfix finish --local
tronador versions hotfix purge [version]
```

`start` obtains GitVersion's `MajorMinorPatch` and increments the patch
component. It creates `hotfix/vX.Y.Z` from the primary branch; when started
from a `support/*` branch, that branch is retained as the base. `publish` and
`finish` operate on the current hotfix branch. `purge` accepts an optional
version and otherwise infers it from the current `hotfix/*` or `fix/*` branch.

The default `finish` creates a guarded pull request to the primary branch.
`--local` journals the merge and annotated `vX.Y.Z` tag, then publishes the
required target, that exact annotated tag, and the source-branch deletion in
one server-side atomic push with leases. In a GitFlow repository, a matching
`support/vX.Y` branch receives the local hotfix when one exists; otherwise the
primary branch receives it. GitHub Flow and trunk-based repositories always
target the primary branch.

## Release branches

```bash
tronador versions release start
tronador versions release start --patch
tronador versions release start --minor
tronador versions release start --major
tronador versions release publish [version]
tronador versions release finish
tronador versions release finish --local
tronador versions release purge [version]
```

`start` defaults to a minor bump and calculates the next GitVersion
`MajorMinorPatch` before creating `release/vX.Y.Z`. Pass one of `--patch`,
`--minor`, or `--major` to select a different bump; combining multiple bump
flags is an error. Its base is `develop` in GitFlow and the primary branch in
GitHub Flow or trunk-based repositories. `publish` and `purge` infer the
release version from the current `release/*` branch when omitted. `finish`
operates on the current release branch.

The normal finish creates a guarded pull request into the primary branch. In
GitFlow it also creates a second guarded pull request from the release branch
to `develop`, so both integration lines are explicit. With `--local`, Tronador
journals the local merges and annotated version tag, then publishes every
required target (`main` and `develop` for GitFlow), that exact annotated tag,
and the source-branch deletion in one server-side atomic push with leases. The
other workflows publish only the primary branch target.

### Resuming a local finish

Before a new local hotfix or release finish writes its schema-v4 journal,
Tronador fetches the source and requires it to exactly match `origin`. This
prevents a local finish from merging unpublished source work. Local finishes
use a journal at Git's `tronador/versions-journal.json` path plus a short-lived
exclusive lock to prevent simultaneous finish invocations in the same
worktree. The lock is not a recovery mechanism.

If a merge conflict interrupts the operation, resolve the conflict and rerun
the same `finish --local` command. The workflow continues `git merge
--continue` when a merge is in progress and then follows the recorded next
step. A resume journal is accepted only when its schema, WayOfWork, repository,
worktree, operation, source/target, step plan, and cursor match the current
operation; otherwise Tronador stops for manual resolution. Pre-schema-v4
journals are incompatible and fail closed before a remote mutation. A matching
resume may skip the source-parity check because a later completed step can
already have removed the source branch.

The atomic publication plan records each required remote target's observed and
desired commit plus the annotated tag object. A remote without atomic-push
support, a changed remote ref, or a prepublished/no-op target or tag fails
closed: the journal and source branch remain for resolution or retry. If the
server accepted the atomic transaction but the client did not receive its
result, rerunning the same command validates that every target contains the
planned result and that the remote annotated tag has the exact recorded object
and target before it deletes the local source and clears the journal.

## Support branches (GitFlow only)

```bash
tronador versions support start <tag>
tronador versions support publish [tag]
tronador versions support purge [tag]
```

Support branches are permanent maintenance lines and are available only when
the selected WayOfWork is GitFlow. `start` fetches tags from `origin`, requires
the requested version tag after that fetch, and creates `support/<tag>` at the
tag. This allows a tag that initially exists only on the remote. Publish and
purge infer the tag from the current `support/*` branch when omitted. Other
workflows reject all support operations rather than silently creating a
`develop`-style maintenance branch.

## Version tags

```bash
tronador versions tag
tronador versions tag <qualifier>
tronador versions tag <qualifier> --publish
```

`tag` preserves the legacy GitFlow version-tag behavior. On the primary branch
it tags GitVersion's `MajorMinorPatch`; on every other branch it uses
GitVersion's `SemVer`, retaining prerelease information. Tronador normalizes
the value to a `v`-prefixed annotated tag. An optional `<qualifier>` appends
`+deploy-<qualifier>` (for example,
`v1.2.3-alpha.1+deploy-test`). This qualifier is deployment metadata, not a
new semantic version.

`--publish` pushes the exact tag to `origin` in the same invocation. Before
calculating a new version, it looks for a unique local SemVer tag already
pointing at `HEAD` and publishes it if it is not yet on `origin`; if it is
already published, the command is an idempotent no-op. This also checks the
remote's advertised target before creating a tag, so a remote-only tag in a
clone without fetched tags is not recreated with a conflicting annotated-tag
object. This avoids silently calculating and creating the next version after
`tag` has already tagged the commit locally. If multiple local version tags
point at `HEAD`, one unpushed tag is preferred; multiple unpushed candidates
fail rather than guessing (a supplied deployment qualifier can disambiguate).
A same-named local or remote tag that points elsewhere is rejected before
local mutation.
One commit may have multiple deployment-qualified aliases of the same version
(for example, `v1.2.3+deploy-test` and `v1.2.3+deploy-prod`). Prerelease
identifiers are part of the version: `v1.2.3-beta.3` and `v1.2.3-beta.4` are
different versions and cannot both tag one commit. Tagging has the same exact
remote-parity guard as a finish.

## Related project-version marker generation

`versions` never writes a repository upgrade marker. Use the separate guarded
project command:

```bash
tronador project version --generate --yes
tronador project version --generate --dry-run
```

`--generate` calculates GitVersion's `MajorMinorPatch` and writes
`vX.Y.Z` to `.cloudopsworks/_VERSION` (or a supported legacy
`.github/_VERSION`). It is deliberately restricted to one unambiguous,
catalog-managed versioned template layout or an explicitly registered private
source marker. Private `.cloudopsworks/.blueprint` and
`.cloudopsworks/.skills` markers are eligible without becoming public catalog
templates; `.github/.skills` is not eligible. An ordinary project marker,
`_VERSION`, or `.cloudopsworks` directory is not sufficient. A missing version
marker may be created atomically only after the layout and its active regular
eligibility marker have been verified. Symlinked or non-regular
layouts/markers, conflicting markers, or multiple active layouts are rejected.
Outside a dry-run it requires `--yes`, warns before replacing an existing
marker because the marker controls template upgrades, and never creates a Git
tag, commit, or push. It cannot be combined with `--plain` or `--snapshot`.
