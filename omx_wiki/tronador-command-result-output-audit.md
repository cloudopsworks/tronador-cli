---
title: "Tronador command-result output audit"
tags: ["cli", "command-output", "audit"]
created: 2026-10-04T13:54:12.635Z
updated: 2026-10-04T13:54:12.635Z
sources: []
links: []
category: reference
confidence: medium
schemaVersion: 1
---

# Tronador command-result output audit

# Tronador command result-output audit

**Status:** report only; no product behavior, source, tests, configuration, README, Git state, cloud state, or remote state was changed by this audit.

## Executive decision

The public CLI has **75 individually identified canonical action rows**. Seventeen are adequate, 10 partial, 22 silent, and 26 misleading. The most urgent next patch is not a broad output framework: it is a set of narrow fixes for false-success parent dispatch, ignored positional input on fixed-arity actions, unsafe or past-tense dry-run wording, swallowed per-item failures, and mutation claims on no-op paths. The resulting backlog contains **34 unique proposals: P0 17, P1 12, P2 5**. Shared proposal IDs are counted once even when they affect many actions.

## Method, baseline, and safety boundary

- Baseline: commit `63b031a227d5943a33975626c2339376990a7afa`, tag `v0.5.3`.
- Inventory sources: Cobra registration under `internal/cli/`, dynamic project profiles in `internal/project/profiles.go`, and the 13 embedded repository templates in `internal/repos/default_config.json`.
- **Observed** means an exact safe process probe or screened focused test was run for this audit. **Source-inferred** means the transcript/result was reconstructed from cited current source or tests; placeholders such as `<path>` and `<n>` are deliberately not fabricated.
- No AWS mutation, authenticated GitHub mutation, remote Git/branch/tag operation, upgrade/recovery/push, destructive cleanup, external publication, release, or network provisioning was executed. Safe tests used temporary fixtures/fakes. Framework probes used the installed `tronador 0.5.3`, which exactly matched the baseline tag, because a separate offline build could not resolve every module from cache. One other bounded audit lane successfully built from its available cache; neither lane enabled downloads.
- Common Cobra behavior: returned errors are rendered on stderr and `Execute` exits 1 (`internal/cli/root.go:24-30`). Help is stdout/status 0; usage/error routing follows Cobra v1.10.2 (`go.mod:26`). Project deliberately suppresses Cobra duplication; its handler owns human/JSON errors, but flag parsing and the parent `MinimumNArgs(1)` check happen before that handler and currently leave zero-byte stdout and stderr with status 1 (`internal/cli/project.go:30-35,99-128,271-286`).
- Evidence fragments retained under `.omx/context/`: `command-output-audit-framework-probes.md`, `command-output-audit-aws-iac-readme-docs.md`, and `command-output-audit-project-repos-versions.md`.

## Counting convention and inventory reconciliation

Aliases are mapped to one canonical action. Behavior-identical profile/template variants share a narrative but every public spelling is listed. Behavior-identical completion generators and configured-template children share narrative entries, but each executable action retains an individual matrix ID and counts once. Aliases do not. Framework/help-only parents are reported separately and do not inflate the 75-action total. No required action was absent and no additional handler-bearing public action was found.

**Hidden completion-transport exclusion (source-inferred):** Cobra conditionally registers hidden `__complete` with alias `__completeNoDesc`; these are shell-to-program transport endpoints, not public user actions, so the PRD explicitly permits their documented exclusion and they do not change canonical or framework totals (`github.com/spf13/cobra@v1.10.2/completions.go:28-35,230-305`; `.omx/plans/prd-command-output-audit.md:48,59-62`). Their result contract is protocol bytes: completion candidates followed by the final `:<directive>` record on stdout, with explanatory diagnostics on stderr (`github.com/spf13/cobra@v1.10.2/completions.go:242-293`). Any patch must preserve those bytes and channels exactly—no appended result prose—even though these transports are excluded from the public action matrix.

### Canonical action summary matrix

| ID | Canonical command (exact aliases/variants) | Verdict | Proposal / priority |
|---|---|---|---|
| [FC-01B](#fc-01b) | `completion bash` | adequate | none |
| [FC-01F](#fc-01f) | `completion fish` | adequate | none |
| [FC-01P](#fc-01p) | `completion powershell` | adequate | none |
| [FC-01Z](#fc-01z) | `completion zsh` | adequate | none |
| [FC-02](#fc-02) | `version` | partial | FW-05 / P2 |
| [AWS-01](#aws-01) | `aws tag` | misleading | AWS-P01 / P0; ARG-P01 / P0 |
| [AWS-02](#aws-02) | `aws copysecret` | partial | AWS-P02 / P2; ARG-P01 / P0 |
| [AWS-03](#aws-03) | `aws remove-default-vpc` | misleading | AWS-P03 / P0; ARG-P01 / P0 |
| [AWS-04](#aws-04) | `aws remediation s3` | misleading | AWS-P04 / P0; ARG-P01 / P0 |
| [AWS-05](#aws-05) | `aws remediation ec2` | misleading | AWS-P04 / P0; ARG-P01 / P0 |
| [IAC-01](#iac-01) | `iac module`; aliases `module-versions`, `module_versions` | partial | IAC-P01 / P1 |
| [P01](#p01) | `project detect` | adequate | none |
| [P02](#p02) | `project capabilities` | adequate | none |
| [P03](#p03) | `project init` (12 supported profiles) | adequate | none |
| [P04](#p04) | `project version` (10 app profiles) | misleading | PRJ-01 / P0 |
| [P05](#p05) | `project lint`; alias `validate` | adequate | none |
| [P06](#p06) | `project format`; alias `fmt` | adequate | none |
| [P07](#p07) | `project clean` | partial | PRJ-02 / P1 |
| [P08](#p08) | `project clean-inputs`; alias `clean_inputs` | partial | PRJ-02 / P1 |
| [R01](#r01) | `repos available`; `repo available`; alias `avail` | partial | REP-01 / P2 |
| [R02](#r02) | `repos clean` | misleading | REP-02 / P0; ARG-P01 / P0 |
| [R03](#r03) | `repos clean template` | partial | REP-03 / P1; ARG-P01 / P0 |
| [R04](#r04) | `repos cicd update` | misleading | REP-04 / P0; ARG-P01 / P0 |
| [R05](#r05) | `repos template [name]` (generic/custom-config lookup) | misleading | REP-05 / P0 |
| [R05-DOTNET](#r05-dotnet) | `repos template dotnet` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R05-GO](#r05-go) | `repos template go` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R05-RUST](#r05-rust) | `repos template rust` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R05-XCODE](#r05-xcode) | `repos template xcode` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R05-ANDROIDSDK](#r05-androidsdk) | `repos template androidsdk` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R05-FLUTTER](#r05-flutter) | `repos template flutter` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R05-JAVA](#r05-java) | `repos template java` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R05-NODE](#r05-node) | `repos template node` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R05-DOCKER](#r05-docker) | `repos template docker` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R05-PYTHON](#r05-python) | `repos template python` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R05-ARGOCD](#r05-argocd) | `repos template argocd` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R05-TERRAGRUNT](#r05-terragrunt) | `repos template terragrunt` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R05-TFMODULE](#r05-tfmodule) | `repos template terraform-module` | misleading | REP-05 / P0; ARG-P01 / P0 |
| [R06](#r06) | `repos template init` | misleading | REP-06 / P0; ARG-P01 / P0 |
| [R07](#r07) | `repos template clean`; alias `clean-template` | partial | REP-03 / P1; ARG-P01 / P0 |
| [R08](#r08) | `repos upgrade [version]` | misleading | REP-07 / P0 |
| [R09](#r09) | `repos recover` | misleading | REP-08 / P0; ARG-P01 / P0 |
| [R10](#r10) | `repos push` | silent | REP-09 / P1; ARG-P01 / P0 |
| [RM-01](#rm-01) | action-bearing `readme` parent | adequate | none |
| [RM-02](#rm-02) | `readme build` | adequate | none |
| [RM-03](#rm-03) | `readme build terraform`; aliases `tf`, `tofu` | adequate | none |
| [RM-04](#rm-04) | `readme init` | adequate | none |
| [RM-05](#rm-05) | `readme lint` | silent | RM-P01 / P1 |
| [RM-06](#rm-06) | `readme deps` | adequate | none |
| [RM-07](#rm-07) | `readme assets path` | adequate | none |
| [RM-08](#rm-08) | `readme assets init` | misleading | RM-P02 / P0 |
| [RM-09](#rm-09) | `readme assets validate` | adequate | none |
| [RM-10](#rm-10) | `readme assets sync` | adequate | none |
| [RM-11](#rm-11) | `readme assets cache ls` | silent | RM-P03 / P1 |
| [RM-12](#rm-12) | `readme assets cache clean` | silent | RM-P04 / P1 |
| [DOC-01](#doc-01) | `docs init` | silent | DOC-P01 / P1 |
| [DOC-02](#doc-02) | `docs targets` | silent | DOC-P02 / P1 |
| [DOC-03](#doc-03) | `docs terraform` | silent | DOC-P03 / P1 |
| [DOC-04](#doc-04) | `docs copyright-add` | partial | DOC-P04 / P1 |
| [V01](#v01) | `versions init`; root aliases `gitflow`, `gf`, `githubflow`, `flow` | misleading | VER-01 / P0 |
| [V02](#v02) | `versions feature start` | silent | VER-02 / P1; VER-03 / P2 |
| [V03](#v03) | `versions feature publish` | silent | VER-02 / P1; VER-03 / P2 |
| [V04](#v04) | `versions feature finish` | silent | VER-02 / P1; VER-03 / P2 |
| [V05](#v05) | `versions feature purge` | silent | VER-02 / P1; VER-03 / P2 |
| [V06](#v06) | `versions hotfix start` | silent | VER-02 / P1; VER-03 / P2 |
| [V07](#v07) | `versions hotfix publish` | silent | VER-02 / P1; VER-03 / P2 |
| [V08](#v08) | `versions hotfix finish` | silent | VER-02 / P1; VER-03 / P2 |
| [V09](#v09) | `versions hotfix purge` | silent | VER-02 / P1; VER-03 / P2 |
| [V10](#v10) | `versions release start` | silent | VER-02 / P1; VER-03 / P2 |
| [V11](#v11) | `versions release publish` | silent | VER-02 / P1; VER-03 / P2 |
| [V12](#v12) | `versions release finish` | silent | VER-02 / P1; VER-03 / P2 |
| [V13](#v13) | `versions release purge` | silent | VER-02 / P1; VER-03 / P2 |
| [V14](#v14) | `versions support start` | silent | VER-02 / P1; VER-03 / P2 |
| [V15](#v15) | `versions support publish` | silent | VER-02 / P1; VER-03 / P2 |
| [V16](#v16) | `versions support purge` | silent | VER-02 / P1; VER-03 / P2 |
| [V17](#v17) | `versions tag [qualifier] [--publish]` | partial | VER-04 / P2 |

### Framework/help-only rows (excluded from canonical action totals)

| ID | Invocation | Current outcome | Verdict | Proposal |
|---|---|---|---|---|
| [F-N01](#f-n01) | root `tronador` | full help stdout/0 | adequate | none |
| [F-N02](#f-n02) | generated `help [command]` | unknown topic stderr but status 0 | misleading | FW-01 / P0 |
| [F-N03](#f-n03) | `aws` | help/0; unknown child also help/0 | misleading | FW-03 / P0 |
| [F-N04](#f-n04) | `aws remediation` | help/0; unknown child also help/0 | misleading | FW-03 / P0 |
| [F-N05](#f-n05) | `iac` | help/0; unknown child error/1 | adequate | none |
| [F-N06](#f-n06) | bare `project` / `project --json` | zero-byte failure/status 1 | misleading | FW-04 / P0 |
| [F-N07](#f-n07) | `repos` (`repo`) | help/0; unknown child error/1 | adequate | none |
| [F-N08](#f-n08) | `repos cicd` | help/0; unknown child also help/0 | misleading | FW-03 / P0 |
| [F-N09](#f-n09) | `docs` | help/0; unknown child error/1 | adequate | none |
| [F-N10](#f-n10) | `readme assets` | help/0; unknown child also help/0 | misleading | FW-03 / P0 |
| [F-N11](#f-n11) | `readme assets cache` | help/0; unknown child also help/0 | misleading | FW-03 / P0 |
| [F-N12](#f-n12) | `versions` and aliases | help/0; unknown child error/1 | adequate | none |
| [F-N13](#f-n13) | `versions feature` | help/0; unknown child also help/0 | misleading | FW-03 / P0 |
| [F-N14](#f-n14) | `versions hotfix` | same registration behavior as feature | misleading | FW-03 / P0 |
| [F-N15](#f-n15) | `versions release` | same registration behavior as feature | misleading | FW-03 / P0 |
| [F-N16](#f-n16) | `versions support` | same registration behavior as feature | misleading | FW-03 / P0 |
| [F-N17](#f-n17) | generated `completion` parent | help/0; unsupported child also help/0 | misleading | FW-02 / P0 |

## Shared path contracts

<a id="project-shared"></a>

### PROJECT-SHARED

Plain tool actions stream child stdout/stderr once and end `project <profile> <capability> completed`; JSON suppresses child passthrough and emits a result object with command, implementation, tools/calls/steps, mutation, artifacts/removals/version/tag, dry-run plan, captured streams, exit status, and file changes (`internal/project/runner.go:115-137,583-608,630-652,830-887`). For every Project action entry P01-P08, errors emitted after `runProjectCommand` begins—including unsupported capability, handler-level positional validation, missing or ambiguous markers, missing tools, denied provisioning, and child failures—use the typed contract: plain errors on stderr or a JSON error object on stdout, always status 1 (`internal/cli/project.go:99-128,131-179,234-286`). **Exception:** Cobra parses flags and enforces the parent `MinimumNArgs(1)` before `RunE`; because Project sets `SilenceErrors` and `SilenceUsage`, an unknown flag or missing capability currently produces zero stdout, zero stderr, and status 1, including with `--json`. Those pre-handler failures do not emit JSON and are the cross-cutting [FW-04](#fw-04) gap represented by [F-N06](#f-n06). Pipelines fail fast, so aggregate partial success is N/A. **Observed:** Terragrunt init streamed output once and retained child status 23 on failure; JSON did not leak child output (`internal/project/runner_test.go:730-832`).

<a id="repos-shared"></a>

### REPOS-SHARED

No structured mode exists (`internal/cli/repos.go:34-64`). Unknown template, marker/config/state errors, unsafe paths, and Git/GitHub/child failures return through Cobra stderr/status 1; workflows fail fast, so aggregate partial success is N/A (`internal/repos/runner.go:147-163,2843-2879,2976-2992`). Dry-run filesystem wrappers emit prospective lines, while some analysis paths actually run Git in isolated temporary checkouts (`internal/repos/clients.go:39-99`, `internal/repos/fileops.go:123-151`). Screened tests observed target preservation for upgrade/recover and no `gh` invocation for recover (`internal/repos/preflight_test.go:638-820`). Positional validation is not uniform: only the generic `template [name]` and `upgrade [version]` handlers declare `MaximumNArgs(1)` and reject a second token with Cobra usage on stderr/status 1. `available`, `clean`, `clean template`, `cicd update`, every registered template child, `template init`, `template clean`, `recover`, and `push` omit `Args`; Cobra therefore accepts arbitrary tokens and their handlers ignore them (`internal/cli/repos.go:67-105,108-128,131-255`; Cobra `command.go:1172-1176`).

<a id="arg-p01"></a>

### ARG-P01 — fixed-arity action positional guard

Add `cobra.NoArgs` to the fixed-arity mutating or externally active AWS actions AWS-01 through AWS-05 and repository actions R02, R03, R04, all 13 registered R05 children, R06, R07, R09, and R10. Preserve the generic R05 and R08 `MaximumNArgs(1)` contracts. Unexpected tokens must stop before handler setup, print Cobra's argument error and usage on stderr, and exit 1; add command-level tests proving the handler/client/Git paths are not invoked. R01 is read-only but has the same omission and should receive `NoArgs` under REP-01. **Priority:** P0 because most affected handlers can mutate cloud, repository, or filesystem state.

<a id="versions-shared"></a>

### VERSIONS-SHARED

Real workflow methods return only `error`; success child output is captured and discarded, so feature/hotfix/release/support successes are silent (`internal/cli/versions.go:182-318`; `internal/versions/workflows.go:18-31,514-527`). Dry-run short-circuits before validation/action and prints `dry-run: would run versions <Use>`, retaining placeholders rather than actual arguments (**Observed**, `internal/cli/versions_test.go:237-280`). Arity, repository/config/branch/ref guards, missing tools, and child errors return stderr/status 1; provisioning hints name `--tool-path`, `--tools-dir`, and `--allow-network` (`internal/versions/tools.go:65-109`). Actions fail fast; JSON is N/A.

## Detailed framework entries

<a id="f-n01"></a>

### F-N01 — root `tronador`

1. **Command/aliases:** `tronador`; none. 2. **Semantics:** discover commands. 3. **Paths:** no args, `--help`, and `help` print identical help; bad command prints a short error/hint; bad flag prints error plus usage. No mutation/no-op/dry-run/partial/prerequisite/structured action is applicable. 4. **Channel/status:** help stdout/0; errors stderr/1. 5. **Evidence:** **Observed:** all stated process-output and status paths; **Source-inferred:** root command registration at `internal/cli/root.go:13-39`. 6. **Verdict:** adequate. 7. **Gap:** none. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="f-n02"></a>

### F-N02 — generated `help [command]`

1. **Command/aliases:** `help [command]`; none. 2. **Semantics:** print selected help or reject nonexistent topics. 3. **Paths:** valid help stdout/0; `help nope` prints `Unknown help topic [nope]` plus usage but returns 0; other path classes N/A. 4. **Channel/status:** invalid topic stderr/**0**. 5. **Evidence:** **Observed:** valid and nonexistent-topic process probes; **Source-inferred:** Cobra help-command implementation at `github.com/spf13/cobra@v1.10.2/command.go:1260-1307`. 6. **Verdict:** misleading. 7. **Gap:** textual failure is process success. 8. **FW-01:** register a small explicit help command; preserve valid bytes; invalid example `Error: unknown help topic "<topic>"` stderr/1. Test root, nested, and nonexistent topics at process level. 9. **Priority:** P0; compatibility correction to invalid status must be documented.

<a id="fc-01b"></a><a id="fc-01f"></a><a id="fc-01p"></a><a id="fc-01z"></a>

### FC-01B / FC-01F / FC-01P / FC-01Z — generated completion actions

1. **Command/aliases:** `completion bash|fish|powershell|zsh`; no aliases; behavior-identical stream generators grouped. 2. **Semantics:** emit a complete redirectable shell script. 3. **Paths:** all four emit nonempty valid scripts and reject extra positional arguments; no mutation/no-op/dry-run/partial/prerequisite path applies. Parent dispatch and unsupported-shell selection are assessed separately as [F-N17](#f-n17). 4. **Channel/status:** valid script stdout/0 with empty stderr; child extra-argument error stderr/1. 5. **Evidence:** **Observed:** all four valid generator script sizes/hashes and the `completion bash extra` argument-error process probe; **Source-inferred:** equivalent extra-argument rejection for `fish`, `powershell`, and `zsh`, plus the common Cobra generator registration/implementation at `github.com/spf13/cobra@v1.10.2/completions.go:740-927`. 6. **Verdict:** adequate for each generator. 7. **Gap:** none in the generator actions. 8. **Proposal:** none; preserve valid script bytes. 9. **Priority:** N/A.

<a id="fc-02"></a>

### FC-02 — `version`

1. **Command/aliases:** `version`; none. 2. **Semantics:** emit release version or `dev`. 3. **Paths:** `0.5.3\n` observed; `--dry-run` is the same read-only query; unknown flags fail; arbitrary positional args are silently ignored. No no-op/partial/prerequisite path; the single-token stdout is the structured/plain artifact. 4. **Channel/status:** process stdout/0; errors stderr/1; uses process-global `fmt.Println`. 5. **Evidence:** **Observed:** version, inherited dry-run, unknown-flag, and extra-position process probes; **Source-inferred:** version wiring and process-global writer at `version.go:5-21`, `main.go:8-10`, `internal/cli/version.go:10-38`. 6. **Verdict:** partial. 7. **Gap:** permissive args and non-injectable writer. 8. **FW-05:** add `cobra.NoArgs` and `fmt.Fprintln(cmd.OutOrStdout(), currentVersion())`, preserving success bytes; test injected writer and extra arg in `internal/cli/version_test.go`. 9. **Priority:** P2; note positional compatibility.

<a id="f-n17"></a>

### F-N17 — generated `completion` parent

1. **Command/aliases:** `completion`; none. 2. **Semantics:** show available generators with no arguments and reject an unsupported shell token. 3. **Paths:** no arguments and `--help` print completion help; `completion nope` prints the same parent help and returns 0 instead of rejecting the shell. Mutation/no-op/dry-run/partial/prerequisite/structured action paths are N/A. 4. **Channel/status:** help and unsupported child both stdout/0 with empty stderr. 5. **Evidence:** **Observed:** no-argument, explicit-help, and unsupported-child process probes; **Source-inferred:** Cobra parent dispatch implementation at `github.com/spf13/cobra@v1.10.2/completions.go:740-927`. 6. **Verdict:** misleading. 7. **Gap:** a misspelled shell is false-success parent dispatch and can redirect help where a script was expected. 8. **FW-02:** keep valid generator bytes and no-argument parent help unchanged; unsupported example `Error: unsupported completion shell "<shell>"; expected bash, fish, powershell, or zsh` stderr/1. Test parent help, unsupported token, all four parsable/nonempty streams with no trailing prose, and extra generator args. 9. **Priority:** P0; compatibility correction to invalid status must be documented.

<a id="f-n03"></a><a id="f-n04"></a><a id="f-n05"></a><a id="f-n06"></a><a id="f-n07"></a><a id="f-n08"></a><a id="f-n09"></a><a id="f-n10"></a><a id="f-n11"></a><a id="f-n12"></a><a id="f-n13"></a><a id="f-n14"></a><a id="f-n15"></a><a id="f-n16"></a><a id="fw-04"></a>

### Framework namespace group — F-N03 through F-N16

1. **Commands/aliases:** exactly the framework matrix spellings. 2. **Semantics:** namespace parents should show help on no args and reject unknown children. 3. **Paths:** no-arg help is stdout/0 for the help-producing parents. `iac`, `repos`, `docs`, and `versions` reject unknown children stderr/1; non-runnable `aws`, `aws remediation`, `repos cicd`, `readme assets`, `readme assets cache`, and versions workflow groups return the same parent help on stdout/status 0 with empty stderr for unknown children. Bare `project`, `project --json`, and unknown Project flags fail during Cobra flag/argument processing before the handler with empty stdout, empty stderr, and status 1; by contrast, unsupported capabilities and other errors reached inside `runProjectCommand` use its human/JSON emitter. Mutation/no-op/dry-run/partial/prerequisite/structured results are N/A because these rows are dispatch only. 4. **Channel/status:** as matrix. 5. **Evidence:** **Observed:** no-argument and unknown-child probes for `aws`, `aws remediation`, `iac`, `repos`, `repos cicd`, `docs`, `readme assets`, `readme assets cache`, `versions`, and `versions feature`, plus bare `project` and `project --json`; the README invalid-child probes used the exact baseline. **Source-inferred:** root-alias equivalence and equivalent no-argument/unknown-child behavior for F-N14 through F-N16 (`versions hotfix`, `release`, and `support`) from registrations at `internal/cli/versions.go:28-42,238-317`; Project pre-handler versus handler routing from `internal/cli/project.go:30-35,99-128,234-286`; other corroborating registrations at `internal/cli/aws.go:12-23`, `internal/cli/sub/aws/s3_remediation.go:18-28`, `internal/cli/repos.go:20-32,108-128`, `internal/cli/docs.go:35-45`, and `internal/cli/readme.go:190-195,282-287`. 6. **Verdict:** matrix. 7. **Gap:** false-success child selection or silent Project pre-handler validation. 8. **FW-03:** shared P0 fix for non-runnable groups: preserve no-arg help/0; unknown child stderr/1; table-test `aws`, `aws remediation`, `repos cicd`, `readme assets`, `readme assets cache`, and all four versions workflow groups. **FW-04:** add Project-aware handling around Cobra's pre-handler parse/arity failures without changing handler-emitted errors; human failures should be one error on stderr/status 1, while `--json` should produce one clean error object on stdout/status 1. Test bare, `--json` bare, unknown flag in plain and JSON modes, and unsupported capability in both modes; the current behavior must not be described as JSON for the pre-handler cases. 9. **Priority:** P0 for FW-03/FW-04; none for adequate parents.

## AWS and IaC

<a id="aws-01"></a>

### AWS-01 — `aws tag`

1. **Aliases:** none. 2. **Semantics:** report tagged/skipped/failed totals. 3. **Paths:** real success ends `Resources tagged: <n>` and skipped/failed counts; no-op explicitly says `No resources need tagging.`; dry-run lists `Would tag` but final summary wrongly says tagged; invalid `--target <other>` can return zero totals/0; API failures usually abort before summary; aggregate partial success N/A; JSON N/A. The command declares no `Args`, so after required flags are satisfied any positional tokens are accepted, logged only in verbose mode, otherwise ignored, and the normal AWS handler proceeds. 4. **Channel/status:** direct stdout/0; returned errors stderr/1; unexpected positions have no argument-specific output/status. 5. **Evidence:** **Source-inferred**, registration/argument use `internal/cli/sub/aws/tag.go:16-34,76-88`, paths `internal/cli/sub/aws/tag.go:53-73,101-119,147-189,273-348`; Cobra `command.go:1172-1176`. 6. **Verdict:** misleading. 7. **Gap:** applied wording in preview, unvalidated target, and ignored positional input before a cloud mutation path. 8. **AWS-P01:** validate `resources|iam|all`; preview `Tagging preview: resources_to_tag=<n> skipped=<n> failed=<n>`; test invalid target and absence of `Resources tagged:` in `tag_test.go`. Apply [ARG-P01](#arg-p01). 9. **Priority:** P0.

<a id="aws-02"></a>

### AWS-02 — `aws copysecret`

1. **Aliases:** none. 2. **Semantics:** copy/create destination secret or add a version. 3. **Paths:** real success duplicates an awkward inner `Secret successfully %sd` and outer copied summary; destination existence chooses create/update rather than no-op; dry-run states no changes, `Would <operation>`, then validation success; missing source/SDK failures error; one destination means aggregate partial N/A; JSON N/A. The command declares no `Args`, so after `--source` is supplied extra positions are accepted, logged only in verbose mode, otherwise ignored, and the copy handler proceeds. 4. **Channel/status:** direct stdout/0; errors stderr/1; unexpected positions have no argument-specific output/status. 5. **Evidence:** **Source-inferred**, registration/argument use `internal/cli/sub/aws/copysecret.go:17-42,68-80`, paths `internal/cli/sub/aws/copysecret.go:52-66,93-196,238-242,281-404`; Cobra `command.go:1172-1176`. 6. **Verdict:** partial. 7. **Gap:** duplicate, unstable grammar; result kind unclear; positional input is ignored before a secret mutation path. 8. **AWS-P02:** one line `Secret copy complete: source=<source> destination=<dest> operation=<created|new-version>`; preview uses `would-*`; table-test output. Apply [ARG-P01](#arg-p01). Classifying non-not-found Describe errors is a separate behavior-risk follow-up. 9. **Priority:** P2 for output wording; P0 for ARG-P01.

<a id="aws-03"></a>

### AWS-03 — `aws remove-default-vpc`

1. **Aliases:** none. 2. **Semantics:** inspect regions, remove defaults/dependencies, summarize. 3. **Paths:** real/no-op give regional results and removed/skipped/failed totals; preview says `Would remove` but increments and reports `VPCs removed`; per-region failure continues and overall status remains 0; global discovery error returns 1; JSON N/A. The command declares no `Args`; extra positions are accepted, logged only in verbose mode, otherwise ignored, and region discovery/removal proceeds. 4. **Channel/status:** direct stdout; partial failures still 0; fatal stderr/1; unexpected positions have no argument-specific output/status. 5. **Evidence:** **Source-inferred**, registration/argument use `internal/cli/sub/aws/remove_vpc.go:18-40,53-65`, paths `internal/cli/sub/aws/remove_vpc.go:72-77,99-236`; Cobra `command.go:1172-1176`. 6. **Verdict:** misleading. 7. **Gap:** false applied preview, success-looking partial failure, and ignored positional input before destructive cloud work. 8. **AWS-P03:** preview `Default VPC removal preview: would_remove=<n> skipped=<n> failed=<n>`; partial `completed with failures...`; preserve exit 0 in patch unless separately approved; inject outcome tests. Apply [ARG-P01](#arg-p01). 9. **Priority:** P0.

<a id="aws-04"></a><a id="aws-05"></a>

### AWS-04 / AWS-05 — `aws remediation s3` / `ec2`

1. **Aliases:** none; two individual action IDs share the common result printer. 2. **Semantics:** remediate S3 SSL policies or EC2 default-security-group rules. 3. **Paths:** success/no-op print configuration, explicit empty result, and processed/skipped/failed summary; dry-run `Would ...` still reports processed; item failures continue, show success-icon summary, and return 0; discovery/client errors return 1; JSON N/A. Both leaves declare no `Args`; extra positions are accepted and ignored, then the selected remediation handler proceeds. 4. **Channel/status:** direct stdout, continued item failures status 0; fatal stderr/1; unexpected positions have no argument-specific output/status. 5. **Evidence:** **Source-inferred**, registrations/handlers `internal/cli/sub/aws/s3_remediation.go:30-57`, `internal/cli/sub/aws/ec2_remediation.go:17-38`, paths `internal/cli/sub/aws/remediation_common.go:60-84`, `internal/cli/sub/aws/s3_remediation.go:55-88,124-218`, `internal/cli/sub/aws/ec2_remediation.go:36-84,104-178`; Cobra `command.go:1172-1176`. 6. **Verdict:** misleading for both. 7. **Gap:** mode and partial-failure state absent; positional input is ignored before cloud mutation paths. 8. **AWS-P04:** common printer emits `Security remediation preview: would_process=...` or `completed with failures: processed=...`; keep exit semantics unless separately approved; common-printer plus S3/EC2 tests. Apply [ARG-P01](#arg-p01) to both leaves. 9. **Priority:** P0.

<a id="iac-01"></a>

### IAC-01 — `iac module`

1. **Aliases:** `module-versions`, `module_versions`. 2. **Semantics:** scan pins; optionally update/fix and annotate/comment. 3. **Paths:** scan progress and per-file states; empty says `No terragrunt.hcl files found.`; dry-run says `would update` and leaves files unchanged; lookup failure prints and continues; requested PR-comment failure is stderr but overall 0; mutation prints `Updated <file>`; invalid tier, marker, read/write failures return 1; `::warning::` is an annotation stream, not JSON. 4. **Channel/status:** human/annotation stdout, comment warning stderr/0, fatal stderr/1. 5. **Evidence:** **Observed:** invalid-tier probe and focused marker, dry-run, and up-to-date test paths; **Source-inferred:** mutation, lookup-failure, optional-comment-failure, and remaining fatal paths from `internal/iac/module_versions.go:214-318,423-475,529-542` and `internal/cli/iac.go:61-98`. 6. **Verdict:** partial. 7. **Gap:** no aggregate result; optional comment failure not in terminal outcome. 8. **IAC-P01:** append `IaC module scan complete: files=<n> sources=<n> updated=<n> up_to_date=<n> unsupported=<n> lookup_failed=<n> comment_failed=<n> dry_run=<bool>`; keep annotation stream compatible; test all counters. 9. **Priority:** P1.

## Project

Exact profile variants: `androidsdk`, `docker`, `dotnet`, `flutter`, `go`, `java`, `node`, `python`, `rust`, `xcode` support init/version; `terraform-module` supports init/lint/format; `terragrunt` supports init/lint/format/clean/clean-inputs (`internal/project/profiles.go:105-153,214-237`). Unsupported pairs are validation errors.

<a id="p01"></a>

### P01 — `project detect`

1. **Aliases:** none. 2. **Semantics:** detected workdir/profile/marker/registry. 3. **Paths:** plain four labeled lines; JSON detection object; marker/arg errors use [PROJECT-SHARED](#project-shared). No-op/dry-run/partial N/A (read-only). 4. **Channel/status:** stdout/0; structured error contract uses [PROJECT-SHARED](#project-shared). 5. **Evidence:** **Source-inferred**, `internal/cli/project.go:131-144`. 6. **Verdict:** adequate. 7. **Gap:** none. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="p02"></a>

### P02 — `project capabilities`

1. **Aliases:** none. 2. **Semantics:** list actions, aliases, arguments, executor, mutation, tools, flags, dry-run behavior. 3. **Paths:** detailed plain lines or JSON detection/descriptions; detection/arg errors use [PROJECT-SHARED](#project-shared); no-op/dry-run/partial N/A. 4. **Channel/status:** stdout/0; errors use [PROJECT-SHARED](#project-shared). 5. **Evidence:** **Source-inferred**, `internal/cli/project.go:145-179`. 6. **Verdict:** adequate. 7. **Gap:** none. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="p03"></a>

### P03 — `project init`

1. **Variants:** all 12 supported profiles listed above. 2. **Semantics:** initialize using profile tool. 3. **Paths:** child stream plus stable completed line; dry-run plan; JSON clean; tool idempotence still completes; fail-fast errors use [PROJECT-SHARED](#project-shared); aggregate partial N/A. 4. **Channel/status:** [PROJECT-SHARED](#project-shared). 5. **Evidence:** **Observed:** Terragrunt init success and exit-23 failure stream/status paths at `internal/project/runner_test.go:730-832`; **Source-inferred:** the other 11 profiles and their dry-run, JSON, idempotent-completion, validation, and prerequisite paths from `internal/project/profiles.go:133-187,214-237` and `internal/project/runner.go:115-137,583-608,630-652,830-887`. 6. **Verdict:** adequate. 7. **Gap:** none. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="p04"></a>

### P04 — `project version`

1. **Variants:** ten app profiles; `--plain`, `--snapshot`, guarded `--generate`. 2. **Semantics:** calculate/write version artifacts and state whether changed. 3. **Paths:** real output says `Generated version <v> in VERSION` even with empty changes or multiple metadata files; dry-run patches or `No file changes...`; JSON fields remain clean; GitVersion output suppressed on success, replayed on plain failure, captured in JSON; fail-fast. 4. **Channel/status:** result stdout, warnings stderr/0; errors use [PROJECT-SHARED](#project-shared). 5. **Evidence:** **Observed:** normal success output, captured/suppressed GitVersion streams, plain and JSON failures, dry-run file-change preview/non-mutation, and guarded-generate paths at `internal/project/runner_test.go:504-578,1675-1780`; **Source-inferred:** already-current real wording, real multi-artifact scope, and remaining branches from `internal/project/runner.go:591-605,915-1009`. 6. **Verdict:** misleading. 7. **Gap:** mutation claim on no-op and incomplete artifact scope. 8. **PRJ-01:** branch on changes: `Version <v> already current; no file changes.` or `Generated version <v>; updated <n> file(s): <paths> (tag_created=false)`; preserve JSON; test identical/multi-artifact/guarded/JSON. 9. **Priority:** P0.

<a id="p05"></a>

### P05 — `project lint`

1. **Alias:** `validate`; profiles terraform-module/terragrunt. 2. **Semantics:** validate. 3. **Paths:** child stream, completion, dry-run, JSON, and errors use [PROJECT-SHARED](#project-shared); validation success is result, so separate no-op N/A; partial N/A. 4. **Channel/status:** [PROJECT-SHARED](#project-shared). 5. **Evidence:** **Source-inferred**, `internal/project/profiles.go:116-123,214-237`, `internal/project/runner.go:294-365`. 6. **Verdict:** adequate. 7. **Gap:** none. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="p06"></a>

### P06 — `project format`

1. **Alias:** `fmt`; profiles terraform-module/terragrunt. 2. **Semantics:** format. 3. **Paths:** child stream plus completed, including already-formatted tool state; dry-run/JSON/errors use [PROJECT-SHARED](#project-shared); partial N/A. 4. **Channel/status:** [PROJECT-SHARED](#project-shared). 5. **Evidence:** **Source-inferred**, `internal/project/profiles.go:120-123,214-237`, `internal/project/runner.go:641-648,830-887`. 6. **Verdict:** adequate. 7. **Gap:** none. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="p07"></a><a id="p08"></a>

### P07 / P08 — `project clean` / `clean-inputs`

1. **Aliases/variants:** P08 alias `clean_inputs`; Terragrunt only. 2. **Semantics:** remove generated paths or generated inputs. 3. **Paths:** JSON records `removed`; plain always says completed whether zero/many; dry-run/invalid/prerequisite/runtime behavior uses [PROJECT-SHARED](#project-shared); partial N/A. 4. **Channel/status:** stdout/0; failures use [PROJECT-SHARED](#project-shared). 5. **Evidence:** **Source-inferred**, `internal/project/profiles.go:128-130,226-237`, `internal/project/runner.go:530-580,899-912`. 6. **Verdict:** partial for both. 7. **Gap:** no count/path and no empty-set distinction. 8. **PRJ-02:** `project terragrunt <action> removed <n> path(s)` or `found no generated paths; no changes needed`; test present/empty/dry-run/JSON and alias equivalence. 9. **Priority:** P1.

## Repositories

<a id="r01"></a>

### R01 — `repos available`

1. **Aliases:** root `repo`; action `avail`. 2. **Semantics:** current template/version and latest compatible tags. 3. **Paths:** success duplicates Repo/Version around latest lines; dry-run remains live read-only tag lookup; errors use [REPOS-SHARED](#repos-shared); no-op/partial/JSON N/A. With no `Args` validator, extra positions are accepted and ignored before the normal lookup. 4. **Channel/status:** stdout/0, errors stderr/1; unexpected positions produce no argument error and take the normal path. 5. **Evidence:** **Source-inferred**, registration `internal/cli/repos.go:67-79`, behavior `internal/repos/runner.go:223-248`; Cobra `command.go:1172-1176`. 6. **Verdict:** partial. 7. **Gap:** duplicate fields, unclear dry-run meaning, and ignored positional input. 8. **REP-01:** exact four-line `Template`, `Current`, `Latest same-minor`, `Latest same-major`; add `cobra.NoArgs`; fixture output and rejected-extra-argument tests, no live network. 9. **Priority:** P2.

<a id="r02"></a>

### R02 — `repos clean`

1. **Aliases:** root `repo`. 2. **Semantics:** reset `.github/workflows`. 3. **Paths:** always starts `Cleaning up repository`; dry-run then low-level prospective rm/mkdir; absent path still previews mkdir; errors use [REPOS-SHARED](#repos-shared); JSON/partial N/A. With no `Args` validator, extra positions are accepted and ignored before cleanup. 4. **Channel/status:** stdout/0, errors stderr/1; unexpected positions produce no argument error and take the normal path. 5. **Evidence:** **Source-inferred**, registration `internal/cli/repos.go:82-105`, behavior `internal/repos/runner.go:214-221`; Cobra `command.go:1172-1176`. 6. **Verdict:** misleading. 7. **Gap:** present-tense preview, no applied/no-op result, and ignored positional input before filesystem mutation. 8. **REP-02:** preview `would reset ... (existing=<bool>)`; real removed count or recreated-missing no-op; test present/absent/dry-run. Apply [ARG-P01](#arg-p01). 9. **Priority:** P0.

<a id="r03"></a><a id="r07"></a>

### R03 / R07 — template cleanup spellings

1. **Commands/aliases:** R03 `repos clean template`; R07 `repos template clean`, alias `clean-template`. 2. **Semantics:** remove template checkout. 3. **Paths:** existing real only progress; absent and ordinary dry-run can be silent; errors use [REPOS-SHARED](#repos-shared); partial/JSON N/A. Both leaves omit `Args`, so extra positions are accepted and ignored before cleanup. 4. **Channel/status:** stdout/0 or silence/0; errors stderr/1; unexpected positions produce no argument error and take the normal path. 5. **Evidence:** **Source-inferred**, registrations `internal/cli/repos.go:94-104,147-170`, behavior `internal/repos/runner.go:192-212`; Cobra `command.go:1172-1176`. 6. **Verdict:** partial for each. 7. **Gap:** no terminal result/no-op/preview; ignored positional input can reach filesystem cleanup. 8. **REP-03:** `Removed ...`, `... absent; no changes needed`, or `Dry-run: would remove ...`; test every spelling. Apply [ARG-P01](#arg-p01) to both leaves. 9. **Priority:** P1 for result output; P0 for ARG-P01.

<a id="r04"></a>

### R04 — `repos cicd update`

1. **Aliases:** root `repo`. 2. **Semantics:** update workflow only when content differs. 3. **Paths:** says `Updating...` before comparison; identical returns 0 with no correction; changed dry-run adds low-level write; prerequisite/error behavior uses [REPOS-SHARED](#repos-shared); partial/JSON N/A. The `update` leaf omits `Args`, so extra positions are accepted and ignored before detection/update. 4. **Channel/status:** stdout/0; errors stderr/1; unexpected positions produce no argument error and take the normal path. 5. **Evidence:** **Source-inferred**, registration `internal/cli/repos.go:108-128`, behavior `internal/repos/runner.go:2068-2102`, `internal/repos/fileops.go:123-151`; Cobra `command.go:1172-1176`. 6. **Verdict:** misleading. 7. **Gap:** no-op claims mutation, no final applied result, and ignored positional input before workflow mutation. 8. **REP-04:** `already current...`, `Updated...`, or `Dry-run: would update...`; test changed/identical/missing prerequisite. Apply [ARG-P01](#arg-p01). 9. **Priority:** P0.

<a id="r05"></a><a id="r05-dotnet"></a><a id="r05-go"></a><a id="r05-rust"></a><a id="r05-xcode"></a><a id="r05-androidsdk"></a><a id="r05-flutter"></a><a id="r05-java"></a><a id="r05-node"></a><a id="r05-docker"></a><a id="r05-python"></a><a id="r05-argocd"></a><a id="r05-terragrunt"></a><a id="r05-tfmodule"></a>

### R05 and R05-DOTNET through R05-TFMODULE — configured template actions

1. **Commands/variants:** generic `repos template [name]` and all 13 matrix children. Registered child names originate from embedded defaults, but both generic and registered-child handlers call `newReposRunner`, which passes the runtime `--config` path to `repos.NewRunner`; therefore template lookup for either form uses the selected runtime configuration (`internal/cli/repos.go:52-64,131-194`; `internal/repos/runner.go:88-101`). 2. **Semantics:** show parent help with no name, or select an active template and clone its blueprint. 3. **Paths:** zero arguments print template help stdout/0; missing marker explicitly skips; active real only says `<description> will be pulled`; active dry-run can execute real Git clone into `.template`; a generic custom-only name can resolve through custom config, while a registered default child resolves that same name in custom config or errors if absent; other errors use [REPOS-SHARED](#repos-shared); partial/JSON N/A. The generic action enforces `MaximumNArgs(1)`, so a second token produces an argument error plus usage on stderr/status 1. Each registered child omits `Args`: extra positions are accepted and ignored, and the fixed registered template name—not the supplied token—is executed. 4. **Channel/status:** zero-argument help and normal progress stdout/0; generic arity/lookup/clone errors stderr/1; registered-child unexpected positions have no argument-specific output/status; dry-run may mutate/network. 5. **Evidence:** **Observed:** exact-baseline generic two-argument rejection and the safe missing-marker path at `internal/repos/config_test.go:372-382`; **Source-inferred:** zero-argument help, registered-child positional behavior, runtime-config wiring, active real/dry-run clone behavior, and remaining failures from `internal/cli/repos.go:52-64,131-194`, Cobra `command.go:1172-1176`, and `internal/repos/runner.go:88-101,2923-2933,2976-2992`, `internal/repos/clients.go:39-42,80-85`. 6. **Verdict:** misleading. 7. **Gap:** unsafe preview and no completion; all 13 registered children ignore positional input before clone-capable handlers. 8. **REP-05:** never clone in preview; `Dry-run: would clone template <name> from <repo> into .template.`; real `Cloned ...`; add a zero-invocation fake-Git preview test, a zero-argument help/status test, and command tests proving generic custom-only lookup plus registered-child lookup both honor `--config`; retain a table-registration test for all 13 embedded child names. Apply [ARG-P01](#arg-p01) to the registered children only; preserve generic `MaximumNArgs(1)`. 9. **Priority:** P0.

<a id="r06"></a>

### R06 — `repos template init`

1. **Aliases:** root `repo`. 2. **Semantics:** select active template and initialize checkout. 3. **Paths:** real only cleanup/prospective clone progress; dry-run performs Git analysis and can leak temp checkout; marker selection/errors use [REPOS-SHARED](#repos-shared); no no-op/partial/JSON. The leaf omits `Args`, so extra positions are accepted and ignored before initialization. 4. **Channel/status:** stdout/0 or errors stderr/1; unexpected positions produce no argument error and take the normal path. 5. **Evidence:** **Source-inferred**, registration `internal/cli/repos.go:147-158`, behavior `internal/repos/runner.go:166-179,285-303`; Cobra `command.go:1172-1176`. 6. **Verdict:** misleading. 7. **Gap:** external preview work/temp leak, no result, and ignored positional input before clone-capable work. 8. **REP-06:** plan-only preview, always defer cleanup if retained; real initialized line; test no Git/no leak and marker/failure cases. Apply [ARG-P01](#arg-p01). 9. **Priority:** P0.

<a id="r08"></a>

### R08 — `repos upgrade [version]`

1. **Aliases:** root `repo`. 2. **Semantics:** apply/commit template upgrade or explain unsupported/no-op. 3. **Paths:** progress and unconditional `Please review changes...`; no-effect still implies changes; dry-run preserves target but uses present-tense/review wording; unsupported template skips; errors use [REPOS-SHARED](#repos-shared); partial/JSON N/A. Its declared `MaximumNArgs(1)` rejects a second token with an argument error plus usage on stderr/status 1; one token is intentionally forwarded as the requested version. 4. **Channel/status:** stdout/0, stderr/1 on errors including excess arity. 5. **Evidence:** **Observed:** isolated dry-run target preservation/output at `internal/repos/preflight_test.go:638-727` and exact-baseline two-argument rejection; **Source-inferred:** real, no-effect, unsupported-template, remaining failure, and one-token forwarding paths from `internal/cli/repos.go:196-222` and `internal/repos/runner.go:251-283,479-675,2130-2185`. 6. **Verdict:** misleading. 7. **Gap:** preview/no-op imply applied changes; positional arity is already enforced. 8. **REP-07:** count effects and emit `would upgrade`, `already current`, or `Upgraded... committed <n>`; test all terminal states and preserve `MaximumNArgs(1)`. 9. **Priority:** P0.

<a id="r09"></a>

### R09 — `repos recover`

1. **Aliases:** root `repo`. 2. **Semantics:** recover template-owned files or explain no-op/unsupported. 3. **Paths:** unsupported skips; dry-run preserves target/skips gh but ends warning that files may have been overwritten; real has no count; errors use [REPOS-SHARED](#repos-shared); partial/JSON N/A. The leaf omits `Args`, so extra positions are accepted and ignored before recovery. 4. **Channel/status:** stdout/0, errors stderr/1; unexpected positions produce no argument error and take the normal path. 5. **Evidence:** **Observed:** dry-run target preservation, skipped `gh`, and false overwrite warning at `internal/repos/preflight_test.go:729-820`; **Source-inferred:** unsupported, real, remaining failure, and ignored-position paths from `internal/cli/repos.go:225-237`, `internal/repos/runner.go:2284-2333`, and Cobra `command.go:1172-1176`. 6. **Verdict:** misleading. 7. **Gap:** false mutation warning, absent change count, and ignored positional input before overwrite-capable work. 8. **REP-08:** preview `would overwrite/update <n>; target was not changed`; real recovered count; zero already matches; test exact endings. Apply [ARG-P01](#arg-p01). 9. **Priority:** P0.

<a id="r10"></a>

### R10 — `repos push`

1. **Aliases:** root `repo`. 2. **Semantics:** commit staged template-owned paths. 3. **Paths:** staged case prints progress/version; empty index returns silent 0; dry-run prints only low-level diff query then silent; unsafe stage/commit errors use [REPOS-SHARED](#repos-shared); partial/JSON N/A. The leaf omits `Args`, so extra positions are accepted and ignored before active-template detection and commit logic. 4. **Channel/status:** stdout or silence/0; errors stderr/1; unexpected positions produce no argument error and take the normal path. 5. **Evidence:** **Source-inferred**, registration `internal/cli/repos.go:239-255`, behavior `internal/repos/runner.go:2211-2232,2859-2879`; Cobra `command.go:1172-1176`. 6. **Verdict:** silent. 7. **Gap:** core no-op and useful preview absent; ignored positional input can reach Git staging/commit. 8. **REP-09:** `No staged template upgrade paths; no commit created.`, real/dry-run counts/version; test empty/staged/dry-run/unsafe. Apply [ARG-P01](#arg-p01). 9. **Priority:** P1 for result output; P0 for ARG-P01.

## README

<a id="rm-01"></a><a id="rm-02"></a>

### RM-01 / RM-02 — parent build and `readme build`

1. **Aliases:** none; two individual IDs share the same build. 2. **Semantics:** generate README from template/YAML. 3. **Paths:** success ends `Generated <README> from <template> using data from <yaml>`; dry-run shows gomplate; child streams pass through; missing inputs/tool/child fail; optional include warning can substitute placeholder; successful build always regenerates, so no-op N/A; structured N/A. 4. **Channel/status:** stdout/0, warning stderr/0, fatal stderr/1. 5. **Evidence:** **Observed:** include dry-run path at `internal/readme/include_test.go:60-93`; **Source-inferred:** real success, child passthrough, missing-input/tool/child failures, optional-include warning, and no-op classification from `internal/cli/readme.go:43-56,116-127` and `internal/readme/runner.go:151-198,492-498`. 6. **Verdict:** adequate. 7. **Gap:** none. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="rm-03"></a>

### RM-03 — `readme build terraform`

1. **Aliases:** `tf`, `tofu`. 2. **Semantics:** generate Terraform README artifact. 3. **Paths:** success names output; preview prints command/redirection; non-module/tool/child errors fail; one artifact means no-op/partial N/A; JSON N/A. 4. **Channel/status:** stdout/0; child stderr then returned error/1. 5. **Evidence:** **Source-inferred**, `internal/readme/runner.go:230-268`, `internal/cli/readme.go:129-141`, `internal/docs/runner.go:134-173`. 6. **Verdict:** adequate. 7. **Gap:** none. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="rm-04"></a>

### RM-04 — `readme init`

1. **Aliases:** none. 2. **Semantics:** create YAML without overwrite. 3. **Paths:** created, already-exists no-op, and prospective dry-run are explicit; IO errors fail; partial/JSON N/A. 4. **Channel/status:** stdout/0, errors stderr/1. 5. **Evidence:** **Observed:** creation and existing-file non-overwrite effects at `internal/readme/runner_test.go:24-59`; **Source-inferred:** their exact output, the dry-run output, and IO-error paths from `internal/readme/runner.go:271-297`. 6. **Verdict:** adequate. 7. **Gap:** none. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="rm-05"></a>

### RM-05 — `readme lint`

1. **Aliases:** none. 2. **Semantics:** confirm generated README is current. 3. **Paths:** current success is zero bytes; preview says it would build/compare; stale diff stderr then error/1; prerequisites fail; partial/JSON N/A. 4. **Channel/status:** silent/0 current; stale stderr/1. 5. **Evidence:** **Observed:** stale-result failure at `internal/readme/runner_test.go:279-301`; **Source-inferred:** silent current success, preview, exact stale stderr/error behavior, and prerequisite failures from `internal/readme/runner.go:300-335`. 6. **Verdict:** silent. 7. **Gap:** validated target/result absent. 8. **RM-P01:** `README.md is up to date (validated against README.yaml)` stdout; exact success test. 9. **Priority:** P1.

<a id="rm-06"></a>

### RM-06 — `readme deps`

1. **Aliases:** none. 2. **Semantics:** resolve/provision required binaries. 3. **Paths:** emits tool paths; dry-run emits planned provision/path; resolved installed dependency is explicit no-op; provisioning errors fail; partial/JSON N/A. 4. **Channel/status:** stdout/0, errors stderr/1. 5. **Evidence:** **Observed:** dry-run provision/path output and resolved gomplate/conditional terraform-docs paths at `internal/readme/runner_test.go:253-277` and `internal/readme/include_test.go:199-223`; **Source-inferred:** remaining missing-input and provisioning-error branches from `internal/readme/runner.go:338-364`. 6. **Verdict:** adequate. 7. **Gap:** none. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="rm-07"></a>

### RM-07 — `readme assets path`

1. **Aliases:** none. 2. **Semantics:** emit tabular resolved assets. 3. **Paths:** one `<name>\t<source>\t<path>` per asset; resolution error fails; read-only so dry-run/no-op/partial N/A; intentional plain stream, no JSON. 4. **Channel/status:** stdout/0, errors stderr/1. 5. **Evidence:** **Source-inferred**, `internal/cli/readme.go:195-212`. 6. **Verdict:** adequate. 7. **Gap:** none. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="rm-08"></a>

### RM-08 — `readme assets init`

1. **Aliases:** none. 2. **Semantics:** materialize embedded assets. 3. **Paths:** per-file wrote or already-exists; preview says write, but creates destination directory before preview; a later file failure can leave earlier writes; JSON N/A. 4. **Channel/status:** stdout/0; errors stderr/1 after possible partial mutation. 5. **Evidence:** **Source-inferred**, `internal/readme/runner.go:545-588`. 6. **Verdict:** misleading. 7. **Gap:** dry-run violates no-change promise; no aggregate partial result. 8. **RM-P02:** move mkdir after preview; aggregate `preview: would_write=<n> skipped=<n> destination=<dir>` / real wrote/skipped; test absent destination after dry-run and partial error. 9. **Priority:** P0.

<a id="rm-09"></a>

### RM-09 — `readme assets validate`

1. **Aliases:** none. 2. **Semantics:** validate each embedded asset. 3. **Paths:** per asset `ok (<bytes> bytes from <source>)`; first error fails; read-only dry-run N/A; embedded defaults mean empty no-op N/A; JSON N/A. 4. **Channel/status:** stdout/0, stderr/1. 5. **Evidence:** **Source-inferred**, `internal/cli/readme.go:225-246`. 6. **Verdict:** adequate. 7. **Gap:** none. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="rm-10"></a>

### RM-10 — `readme assets sync`

1. **Aliases:** none. 2. **Semantics:** cache/copy assets and write manifest. 3. **Paths:** preview returns before HTTP/cache mutation; success per asset and closes with manifest source/generation; earlier assets may exist before later error; internal unchanged writes not separately labeled; JSON N/A. 4. **Channel/status:** stdout/0, errors stderr/1. 5. **Evidence:** **Observed:** fake-HTTP real sync and resulting cache artifact at `internal/readme/runner_test.go:320-348`; **Source-inferred:** preview, exact per-asset/manifest output, project copy, later-error partial state, and unchanged-write behavior from `internal/readme/runner.go:641-728,831-836`. 6. **Verdict:** adequate (manifest is terminal result). 7. **Gap:** unchanged precision is optional future P2, not next-patch requirement. 8. **Proposal:** none. 9. **Priority:** N/A.

<a id="rm-11"></a>

### RM-11 — `readme assets cache ls`

1. **Aliases:** none. 2. **Semantics:** list cache entries. 3. **Paths:** populated output is the plain three-field `<repo>\t<ref>\t<path>` stream; absent/empty cache is zero-byte stdout success; read-only dry-run N/A; filesystem error fails; partial/JSON N/A. 4. **Channel/status:** populated stdout/0; empty stdout plus no diagnostic/0; errors stderr/1. 5. **Evidence:** **Source-inferred**, `internal/cli/readme.go:287-300`, `internal/readme/runner.go:746-781`. 6. **Verdict:** silent. 7. **Gap:** empty-set state is not identifiable. 8. **RM-P03:** retain zero-byte stdout for an empty cache and emit `No README asset cache entries found.` on stderr/status 0; preserve populated stdout as exactly three TSV fields per row with empty stderr. Add isolated-cache tests for empty stdout plus the exact stderr diagnostic/status 0 and for unchanged populated TSV bytes plus empty stderr. 9. **Priority:** P1.

<a id="rm-12"></a>

### RM-12 — `readme assets cache clean`

1. **Aliases:** none. 2. **Semantics:** remove cache or report absent. 3. **Paths:** preview generic clean line; real removed/absent both zero bytes/0; error fails; partial/JSON N/A. 4. **Channel/status:** stdout preview or silence/0; stderr/1. 5. **Evidence:** **Source-inferred**, `internal/cli/readme.go:302-312`, `internal/readme/runner.go:784-790`. 6. **Verdict:** silent. 7. **Gap:** applied/no-op and path absent. 8. **RM-P04:** inspect existence and report removed/already absent; preview includes path/state; isolated-cache tests. 9. **Priority:** P1.

## Docs

<a id="doc-01"></a>

### DOC-01 — `docs init`

1. **Aliases:** none. 2. **Semantics:** create docs directory. 3. **Paths:** preview mkdir; real created/already-existing both silent; error fails; partial/JSON N/A. 4. **Channel/status:** stdout preview or silence/0; stderr/1. 5. **Evidence:** **Source-inferred**, `internal/docs/runner.go:82-90`. 6. **Verdict:** silent. 7. **Gap:** applied/no-op unknown. 8. **DOC-P01:** created/already-exists lines; test both and preview. 9. **Priority:** P1.

<a id="doc-02"></a>

### DOC-02 — `docs targets`

1. **Aliases:** none. 2. **Semantics:** generate target-help artifact. 3. **Paths:** preview command; real atomic write then silence; missing make/child/write fail; always rewrites so no-op N/A; partial N/A; JSON N/A. 4. **Channel/status:** preview stdout/0; success silence/0; child stderr plus error/1. 5. **Evidence:** **Observed:** real generated artifact content at `internal/docs/runner_test.go:19-39`; **Source-inferred:** preview output, silent real terminal result, atomic write, and missing-tool/child/write failures from `internal/docs/runner.go:92-131`. 6. **Verdict:** silent. 7. **Gap:** generated path absent. 8. **DOC-P02:** `Generated docs targets: <output> (target=<help-target>)`; assert line/content. 9. **Priority:** P1.

<a id="doc-03"></a>

### DOC-03 — `docs terraform`

1. **Aliases:** none. 2. **Semantics:** generate Terraform docs artifact. 3. **Paths:** preview command; atomic write then silence; tool/child/write errors; no-op/partial/JSON N/A. 4. **Channel/status:** as DOC-02. 5. **Evidence:** **Observed:** real generated artifact content and dry-run command output at `internal/docs/runner_test.go:41-75`; **Source-inferred:** silent real terminal result, atomic write, and missing-tool/child/write failures from `internal/docs/runner.go:134-174`. 6. **Verdict:** silent. 7. **Gap:** generated path absent. 8. **DOC-P03:** `Generated Terraform docs: <output> (format=<format>)`; exact test. 9. **Priority:** P1.

<a id="doc-04"></a>

### DOC-04 — `docs copyright-add`

1. **Aliases:** none. 2. **Semantics:** delegate header tool and confirm completion. 3. **Paths:** missing description errors; preview command; real passes child stdout/stderr and status but has no Tronador line, so silent child means silent success; child owns no-op/partial; JSON N/A. 4. **Channel/status:** passthrough; success 0, child nonzero returned. 5. **Evidence:** **Observed:** missing-description validation and dry-run command output at `internal/docs/runner_test.go:78-94`; **Source-inferred:** real child-stream/status passthrough, absence of a Tronador terminal line, and child-defined no-op/partial behavior from `internal/docs/runner.go:176-229`. 6. **Verdict:** partial. 7. **Gap:** variable child output cannot reliably confirm scope. 8. **DOC-P04:** after child 0 print `Copyright header command completed: workdir=<dir> output_dir=<dir>`; retain child output; fake silent/nonzero tests. 9. **Priority:** P1.

## Versions

<a id="v01"></a>

### V01 — `versions init`

1. **Aliases:** root `gitflow`, `gf`, `githubflow`, `flow`. 2. **Semantics:** select WayOfWork and establish develop when needed. 3. **Paths:** actual says selected and maybe created/pushed develop; identical existing config still warns replacing and says selected; preview says would select but skips develop inspection; conflicts/repo/Git errors use [VERSIONS-SHARED](#versions-shared); partial/JSON N/A. 4. **Channel/status:** result stdout, warning stderr/0; errors stderr/1. 5. **Evidence:** **Observed:** identical-selection no-change result state at `internal/versions/init_test.go:711-723`; **Source-inferred:** the CLI's replacement warning/selected wording for that state, changed selection/develop paths, preview, and failures from `internal/cli/versions.go:65-112` and `internal/versions/init.go:52-121,593-601`. 6. **Verdict:** misleading. 7. **Gap:** false replacement/mutation and incomplete preview. 8. **VER-01:** warn only on differing content; already-selected no-op; changed paths; preview explicitly says develop not inspected (or inspect safely); alias tests. 9. **Priority:** P0.

<a id="v02"></a><a id="v03"></a><a id="v04"></a><a id="v05"></a>

### V02–V05 — feature actions

1. **Commands:** `feature start <name>`, `publish [name]`, `finish [name]`, `purge [name]`. 2. **Semantics:** create, publish, finish/PR, or purge feature branch. 3. **Paths by ID:** V02 real created success silent; collision errors. V03 publish/already-published outcome silent. V04 PR/guarded completion silent. V05 deleted and already-absent no-op silent (`internal/versions/feature.go:140-142`). All previews/errors/structured behavior use [VERSIONS-SHARED](#versions-shared); partial/JSON N/A. 4. **Channel/status:** real silence/0; preview stdout/0; errors stderr/1. 5. **Evidence:** **Observed:** shared workflow dry-run short-circuit and placeholder output at `internal/cli/versions_test.go:237-255`; **Source-inferred:** all feature real success/no-op/error outcomes and silence from `internal/cli/versions.go:211-235` and `internal/versions/feature.go:9-97,118-190`. 6. **Verdict:** silent for each ID. 7. **Gap:** no stable applied/no-op result; preview placeholders. 8. **VER-02:** exact created/published/PR/purged/already-absent lines backed by minimal result metadata; table-test each. **VER-03:** interpolate real args/flags in preview. 9. **Priority:** P1 and shared P2.

<a id="v06"></a><a id="v07"></a><a id="v08"></a><a id="v09"></a>

### V06–V09 — hotfix actions

1. **Commands:** `hotfix start|publish|finish|purge`. 2. **Semantics:** calculated hotfix branch lifecycle. 3. **Paths by ID:** V06 calculated branch success silent/collision error; V07 published/already state silent; V08 PR or journaled `--local` completion silent, multi-step errors fail; V09 deletion/already-absent silent. Preview/error paths use [VERSIONS-SHARED](#versions-shared); partial/JSON N/A. 4. **Channel/status:** [VERSIONS-SHARED](#versions-shared). 5. **Evidence:** **Source-inferred**, `internal/cli/versions.go:238-260`, `internal/versions/hotfix.go:22-207`. 6. **Verdict:** silent each. 7. **Gap:** lifecycle state absent; placeholder preview. 8. **VER-02:** analogous exact `hotfix/vX.Y.Z` results incl local/resume/no-op; **VER-03:** actual args/`--local`; tests. 9. **Priority:** P1/P2.

<a id="v10"></a><a id="v11"></a><a id="v12"></a><a id="v13"></a>

### V10–V13 — release actions

1. **Commands:** `release start|publish|finish|purge`. 2. **Semantics:** release branch lifecycle. 3. **Paths by ID:** V10 calculated branch silent; conflicting bump error. V11 publish/already state silent. V12 PR/local, already-contained, or existing-PR success silent. V13 delete/already-absent silent. Preview/error paths use [VERSIONS-SHARED](#versions-shared); partial/JSON N/A. 4. **Channel/status:** [VERSIONS-SHARED](#versions-shared). 5. **Evidence:** **Source-inferred**, `internal/cli/versions.go:263-297`, `internal/versions/release.go:10-133`. 6. **Verdict:** silent each. 7. **Gap:** outcome/no-op absent; placeholders. 8. **VER-02:** exact `release/vX.Y.Z` and per-target state; **VER-03:** actual bump/local flags; tests. 9. **Priority:** P1/P2.

<a id="v14"></a><a id="v15"></a><a id="v16"></a>

### V14–V16 — support actions

1. **Commands:** `support start|publish|purge`; GitFlow-only. 2. **Semantics:** branch-from-tag support lifecycle. 3. **Paths by ID:** V14 created silent or unsupported-WOW/tag error; V15 published/already state silent; V16 delete/already-absent silent. Preview/error paths use [VERSIONS-SHARED](#versions-shared); partial/JSON N/A. 4. **Channel/status:** [VERSIONS-SHARED](#versions-shared). 5. **Evidence:** **Source-inferred**, `internal/cli/versions.go:299-318`, `internal/versions/support.go:14-80`. 6. **Verdict:** silent each. 7. **Gap:** result/no-op absent; placeholders. 8. **VER-02:** exact support created/published/purged/already lines; **VER-03:** actual args; tests. 9. **Priority:** P1/P2.

<a id="v17"></a>

### V17 — `versions tag`

1. **Command/flags:** optional qualifier, `--publish`; root aliases apply. 2. **Semantics:** calculate/create/publish tag while preserving script-friendly tag payload. 3. **Paths:** every success/no-op prints same bare tag; preview exactly `dry-run: would run versions tag`, omitting qualifier/publish; qualifier/tag/tool/Git errors use [VERSIONS-SHARED](#versions-shared); partial/JSON N/A. 4. **Channel/status:** bare tag stdout/0; errors stderr/1. 5. **Evidence:** **Observed:** dry-run placeholder output and non-mutation at `internal/cli/versions_test.go:257-280`; **Source-inferred:** real created/published/already-success payload and qualifier/tag/tool/Git failure paths from `internal/cli/versions.go:320-355` and `internal/versions/tag.go:19-81,207-226`. 6. **Verdict:** partial. 7. **Gap:** created/published/already state indistinguishable; incomplete preview. 8. **VER-04:** preserve first stdout tag exactly; expose state only under verbose or an explicitly accepted diagnostic channel: `tag result: created-local|published|already-published <tag>`; preview names qualifier/publish; lock bytes/state/no-mutation tests. 9. **Priority:** P2 due stdout compatibility risk.

## Ranked next-patch backlog

### P0 — correctness and safety (17 unique proposals)

1. `REP-05`, `REP-06`, `RM-P02`: make dry-run genuinely non-mutating/no-network for template and README asset initialization.
2. `FW-01`, `FW-02`, `FW-03`: invalid help/completion/group selection must be stderr/status 1, while preserving valid help/scripts.
3. `FW-04`: eliminate silent human and JSON project validation failures.
4. `AWS-P01`, `AWS-P03`, `AWS-P04`: distinguish preview from applied work and partial failures; validate tag target.
5. `PRJ-01`, `REP-02`, `REP-04`, `REP-07`, `REP-08`, `VER-01`: stop claiming mutations on preview/no-op paths and report the actual terminal state.
6. `ARG-P01`: reject unexpected positional tokens before every fixed-arity mutating or externally active AWS/repository handler; R01 receives the same read-only guard under `REP-01`.

### P1 — silent or incomplete core result (12 unique proposals)

`IAC-P01`, `PRJ-02`, `REP-03`, `REP-09`, `RM-P01`, `RM-P03`, `RM-P04`, `DOC-P01`, `DOC-P02`, `DOC-P03`, `DOC-P04`, and shared `VER-02` add narrow terminal results/counts without redesigning command architecture.

### P2 — precision/testability (5 unique proposals)

`FW-05`, `AWS-P02`, `REP-01`, shared `VER-03`, and `VER-04` preserve current successful payload contracts while improving validation, wording, or opt-in state visibility.

## Deferred or compatibility-sensitive work

- Changing AWS per-item failure from status 0 to nonzero may be desirable, but is a behavior contract change; the patch-safe recommendation first makes partial failure explicit.
- `cobra.NoArgs` will intentionally turn previously accepted-but-ignored positional tokens into stderr/status 1. Treat ARG-P01 and the R01 guard as documented invalid-input compatibility corrections; preserve every valid invocation's output.
- Adding human prose to completion scripts or hidden completion-transport stdout, project JSON, IaC annotation streams, repository plain data, the `readme assets cache ls` TSV stdout stream, or the first `versions tag` stdout line is prohibited. Preserve machine streams; RM-P03 therefore uses stderr/status 0 for the empty-cache diagnostic while keeping stdout zero-byte.
- A global output/result abstraction, schema version, logging dependency, and broad command rewrite are intentionally deferred.
- Classifying every Secrets Manager `DescribeSecret` error rather than treating it as absence is correctness work adjacent to AWS-P02 but should have its own behavior review.

## Final reconciliation

- **Canonical actions:** 75.
- **Verdicts:** adequate 17 + partial 10 + silent 22 + misleading 26 = 75.
- **Framework/help-only rows:** 17, separately counted; adequate 5, misleading 12.
- **Unique proposals:** P0 17 + P1 12 + P2 5 = 34. Common IDs (`ARG-P01`, `AWS-P04`, `PRJ-02`, `REP-03`, `VER-02`, `VER-03`, `FW-03`) are counted once, not once per affected action.
- **Configured template coverage:** all 13 embedded names are present in R05. Their registration names come from embedded defaults, while both generic and registered-child execution use the runtime configuration selected by `--config`.
- **Report-only outcome:** recommendations were not implemented and no patch release was created.
