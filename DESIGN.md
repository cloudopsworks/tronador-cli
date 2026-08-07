# Design Reference: Implicit Project Capabilities

**Status:** Approved for implementation; implementation is complete.

**Date:** 2026-08-03

**Canonical command:** `tronador project`

This document is the sole design reference for the command. It defines a
detection-driven, capability-oriented interface over the supported CloudOps
Works repository implementations. Makefiles are legacy evidence only and are
not an execution dependency for this command.

## Decision summary

Use one public command grammar and make the implementation namespace implicit.
Execution is CLI-native: the command resolves declared tools and invokes them
directly, or performs a small native filesystem operation. It never dispatches
through a repository Makefile:

```text
tronador project detect
tronador project capabilities
tronador project <capability> [validated-arguments...]
```

Examples:

```text
tronador project init
tronador project init aws
tronador project version
tronador project lint
tronador project format
tronador project clean --yes
```

The selected implementation is detected from `.cloudopsworks/` before
capability lookup. The user does not repeat `go`, `terraform-module`, or
`terragrunt` in the command. `capabilities` reports which logical subcommands
are valid for the detected implementation and the argument/tool/side-effect
contract for each one.

The public command is polymorphic at the capability boundary, not at a
Make-target spelling boundary:

```text
project init
  ├─ application profile     → profile-specific code/init operation/tool pipeline
  ├─ Terraform module profile → provider cleanup plus Boilerplate render
  └─ Terragrunt project       → Boilerplate render plus HCL formatting

project lint
  ├─ Terraform module profile → terraform/tofu validation tool pipeline
  └─ Terragrunt project       → terragrunt plus underlying IaC tools

project format
  ├─ Terraform module profile → terraform/tofu fmt
  └─ Terragrunt project       → terragrunt HCL formatter
```

Similar commands are common logical capabilities only when their input,
observable output, safety policy, and result semantics are shared. Different
tools and execution pipelines are expected behind one logical capability.

## Goals

1. Provide one neutral command that transparently operates on every supported
   implementation repository.
2. Detect the implementation exclusively from `<workdir>/.cloudopsworks/`.
3. Make valid subcommands and argument schemas discoverable with
   `tronador project capabilities`.
4. Make `init` polymorphic, including implementation-specific arguments such
   as a Terraform module provider.
5. Include version generation/update operations without exposing tag management.
6. Expose common logical capabilities such as `lint` and `format` while
   retaining implementation-specific tool pipelines.
7. Keep the extension point data-driven and small enough that adding a
   profile does not require a new public command tree or dispatcher branch.
8. Preserve existing `tronador repos` and `tronador repo` behavior.
9. Make the project command runnable in repositories without a Makefile.
10. Resolve every external tool through the existing versioned Tronador tool
    installation/provisioning process rather than ad hoc shell installation.

## Non-goals and review exclusions

This approved design excludes:

- changes to existing `repos`/`repo` commands or their upgrade/recovery state
  machine;
- tag creation, tag deletion, tag pushing, branch checkout for tagging, or
  release behavior;
- arbitrary Make target passthrough;
- invoking or discovering repository Makefile targets at runtime;
- requiring a repository Makefile to be present;
- `.github` fallback detection for legacy layouts;
- dynamic capability discovery from Make or another child process;
- a runtime plugin marketplace or large inheritance hierarchy; or
- a root-level `init`, `version`, `lint`, or `format` command outside the
  `project` command.

Version generation is in scope. Tag-management targets are not. A legacy
`version` recipe may have inspected GitVersion tag state as an input, but the
CLI operation must not create, push, delete, or infer a tag-management
operation. The legacy Makefile itself is deprecated for this capability and
is not evaluated.

## Compatibility and migration

The following existing commands remain unchanged:

```text
tronador repos ...
tronador repo ...
```

The earlier design examples `tronador project go init` and
`tronador project terraform-module lint` were never implemented and are not
backward-compatible forms. They must be rejected as an unsupported capability
with a migration hint:

```text
Use `tronador project capabilities` to list valid capabilities for this
detected implementation; do not include an implementation namespace.
```

No root-level shorthand such as `tronador init` is introduced. This avoids
collisions with existing command domains and keeps detection scoped to the
explicit `project` command.

Template Makefiles are deprecated execution surfaces. During migration they
may remain in repositories for compatibility with older workflows, but
`tronador project` neither requires them nor treats their targets as a public
API. The CLI owns the replacement behavior and may report that a legacy
Makefile is present only as an informational migration warning.

## Command grammar

### `project detect`

Read-only. Detects exactly one implementation and reports:

- absolute workdir;
- implementation id for diagnostics;
- canonical marker and any accepted alias markers;
- registry descriptor version; and
- detection warnings, if any.

It does not invoke Make, tools, network access, or a child process.

### `project capabilities`

Read-only. Detects the implementation and reports the exact logical
capabilities valid for that project. Each capability entry includes:

- command name and aliases;
- positional and flag argument schema;
- required/optional/default values;
- CLI executor and declared tool dependency graph;
- toolchain and prerequisite checks, including installation policy;
- mutation class and expected generated files;
- network policy;
- confirmation policy;
- dry-run support; and
- human and JSON result fields.

This is the authoritative discovery surface. It is not merely help text and
must not list internal prerequisites, tag targets, or unsupported operations.

### `project <capability> [validated-arguments...]`

The dispatcher performs these steps in order:

1. resolve and validate `--workdir`;
2. detect one implementation;
3. find the logical capability in the selected implementation profile;
4. validate all arguments against that capability's schema;
5. build an immutable CLI operation plan without spawning Make or a shell;
6. resolve the plan's declared tool dependencies through the Tronador tool
   provisioner;
7. enforce execution, network, prerequisite, and confirmation policy; and
8. execute native operations or direct tool calls and preserve their result.

Detection or validation failure always happens before child-process start.

## Detection contract

### Marker rules

1. Resolve `--workdir` to an absolute directory.
2. Inspect only `<workdir>/.cloudopsworks/`.
3. A marker must be a regular file. A directory or symlink at a recognized
   marker path is invalid, not a valid match.
4. Marker contents are not interpreted for identity; existence is sufficient.
5. Multiple aliases belonging to one implementation count as one match and are
   reported together.
6. Markers belonging to different implementations are ambiguous and stop
   execution.
7. Unknown files under `.cloudopsworks/` do not identify an implementation.
8. The detector never infers identity from repository name, language files,
   Git remote, Make comments, or `.github`.

### Current marker registry

| Implementation id | Canonical marker | Compatibility marker | Publicly visible diagnostic name |
|---|---|---|---|
| `androidsdk` | `.android` | — | Android SDK |
| `docker` | `.docker` | — | Docker |
| `dotnet` | `.dotnet` | — | .NET |
| `flutter` | `.fluttermobile` | — | Flutter |
| `go` | `.golang` | — | Go |
| `java` | `.java` | — | Java |
| `node` | `.node` | — | Node |
| `python` | `.python` | — | Python |
| `rust` | `.rust` | — | Rust |
| `terraform-module` | `.terraform-module` | — | Terraform module |
| `terragrunt` | `.iac` | — | Terragrunt project |
| `xcode` | `.xcode` | — | Xcode |

The implementation treats `.fluttermobile` as the canonical Flutter marker and
does not accept `.flutter` as an alias.

### Detection outcomes

| Condition | Error code | Required behavior |
|---|---|---|
| Exactly one implementation match | — | Return the detected profile and proceed to capability lookup. |
| No recognized marker | `project_implementation_unknown` | Stop; list the inspected path and recognized marker names. |
| Different implementation markers match | `project_implementation_ambiguous` | Stop; list every matched implementation and marker. |
| Recognized marker is a directory or symlink | `project_marker_invalid` | Stop; report the path and regular-file requirement. |
| Registry has duplicate marker/profile identity | `project_registry_invalid` | Stop before detection result is accepted. |

There is no namespace-mismatch error in the new grammar because the user does
not supply an implementation namespace. A capability that is not in the
detected profile returns `project_capability_unsupported`.

## Capability model: profiles, definitions, and bindings

Use a small **capability profile** pattern rather than a namespace command
tree or class hierarchy.

### Shared logical definition

A logical capability defines the stable user-facing contract once:

```text
CapabilityDefinition {
  id: init | version | lint | format | clean | clean-inputs
  aliases: []string
  arguments: []ArgumentDefinition
  semantics: string
  result_schema: ResultSchema
  safety: SafetyPolicy
}
```

### Implementation binding

An implementation profile binds only the capabilities it supports:

```text
ImplementationProfile {
  id: string
  markers: []MarkerDefinition
  capabilities: []CapabilityBinding
}

CapabilityBinding {
  capability: string
  executor: native | tool_pipeline
  operation: string
  arguments: []ArgumentDefinition
  tools: []ToolRequirement
  prerequisites: []Prerequisite
  mutation_class: read_only | workspace_files | generated_files | destructive
  generated_artifacts: []string
  network_policy: forbidden | declared
  confirmation_policy: none | yes_for_noninteractive
  dry_run: required | metadata_only | unsupported
}

ToolRequirement {
  name: string
  executable: string
  alternatives: []ToolCandidate
  version: string | version_from_config
  configured_path_flag: string | null
  install_policy: path | cache | provision | unavailable
  required_for: []string
}

ToolCandidate {
  name: string
  executable: string
}
```

The shared definition owns argument validation and result semantics. The
binding owns the implementation-specific operation, tools, prerequisites, and
side effects. A `tool_pipeline` is an ordered list of typed tool and native
steps; each tool call has an executable name and an argument vector, never a
shell command string. Native steps cover constrained file removal, renames,
metadata edits, and text rewrites. This keeps mixed pipelines inspectable and
prevents a repeated `version` or `lint` entry from becoming an accidental
claim of equivalence.

### Polymorphic resolution examples

`tronador project init` resolves to the binding for the detected profile:

| Detected profile | Arguments | CLI operation/tool dependencies |
|---|---|---|
| Android SDK, Docker, .NET, Flutter, Go, Java, Node, Python, Rust, Xcode | none | exact profile `code/init` body, including its declared prerequisites and typed native/tool steps |
| Terraform module | `provider` required | remove `provider.temp.tf`, typed Boilerplate render with the provider variable, then typed `git add .cloudopsworks/.provider *.tf` |
| Terragrunt project | none | typed Boilerplate render with existing input var files, followed by `terragrunt hcl format --exclude-dir .cloudopsworks` |

`tronador project init aws` is valid only when the detected profile is
Terraform module. The provider argument is validated as a single allowlisted
identifier before tool argument construction; it cannot contain `/`,
whitespace, shell metacharacters, or an extra executable argument.

The same logical command can therefore have different argument schemas. A
missing or extra argument is `project_argument_invalid`, not an attempted
fallback.

## Capability matrix

The matrix is the contract for the first supported profiles. `version` means
version generation/update, not tagging.

| Detected implementation | Valid logical capabilities | Binding summary |
|---|---|---|
| Android SDK | `init`, `version` | Exact `code/init` prerequisite/assertion/owner pipeline; no invented Boilerplate mutation. |
| Docker | `init`, `version` | Exact `code/init` owner/version captures and scoped `package.json` edits. |
| .NET | `init`, `version` | Exact `code/init` solution/project renames, XML/YAML updates, and solution rewrite. |
| Flutter | `init`, `version` | Exact `code/init` `pubspec.yaml` name and FullSemVer edits. |
| Go | `init`, `version` | Exact `code/init` module removal, `go mod init`, `go mod tidy`, and import rewrite. |
| Java | `init`, `version` | Exact `code/init` Maven artifact and snapshot-version edits. |
| Node | `init`, `version` | Exact `code/init` owner/version captures and scoped `package.json` edits. |
| Python | `init`, `version` | Exact metadata effect of `code/init` without installing its legacy TOML helper. |
| Rust | `init`, `version` | Exact metadata and crate-reference effect of `code/init` without installing its legacy TOML helper. |
| Terraform module | `init`, `lint`, `format` | Boilerplate provider render for init; OpenTofu-default typed lint/format calls. |
| Terragrunt project | `init`, `lint`, `format`, `clean`, `clean-inputs` | Boilerplate plus HCL format for init; typed Terragrunt/IaC calls and native cleanup. |
| Xcode | `init`, `version` | Exact `code/init` prerequisite/assertion/owner pipeline; no invented Boilerplate mutation. |

The Terragrunt `format` binding is intentionally typed and independent from
`init`: it invokes only the supported Terragrunt HCL formatter and never runs
project initialization. If that tool call is not approved, `format` is
removed from the Terragrunt profile and `capabilities` says it is unsupported.

Terraform and Terragrunt therefore share the logical names `lint` and
`format`, but not a toolchain contract:

| Capability | Terraform module binding | Terragrunt binding |
|---|---|---|
| `lint` | Typed Terraform/OpenTofu validation and formatter-check calls | Typed Terragrunt HCL validation plus underlying IaC validation calls |
| `format` | Typed Terraform/OpenTofu formatter call | Typed `terragrunt hcl format` call, independent of `init` |

No common root-level `project lint` or `project format` is exposed outside a
detected profile that advertises the capability.

## Version generation contract

`tronador project version` is a public logical capability for the ten
application profiles in the matrix. It is a generation/update operation:

- resolve the declared GitVersion tool through the Tronador tool provisioner;
- calculate the version using a direct typed GitVersion call and the profile's
  generation policy;
- write the declared generated file and implementation metadata; and
- return the generated version and changed-artifact summary in the result.

The implementation must not invoke `git tag`, `git push`, branch checkout for
tag management, Make, or any release workflow. A tagged working tree may
affect GitVersion's calculated input, but that behavior is reported as an
input condition and tested separately from tag management.

| Profile | Generated or updated artifacts | Primary prerequisites |
|---|---|---|
| Android SDK | `VERSION` | GitVersion |
| Docker | `VERSION`, package metadata | `boilerplate` for init; `gitversion`; native metadata writer |
| .NET | `VERSION`, project metadata | `boilerplate` for init; `gitversion`; native metadata writer |
| Flutter | `VERSION`, `pubspec.yaml` | `boilerplate` for init; `gitversion`; native metadata writer |
| Go | `VERSION` | GitVersion |
| Java | `VERSION`, Maven metadata | `boilerplate` for init; `gitversion`; native metadata writer |
| Node | `VERSION`, package metadata | `boilerplate` for init; `gitversion`; native metadata writer |
| Python | `VERSION`, `pyproject.toml` | `boilerplate` for init; `gitversion`; native metadata writer |
| Rust | `VERSION`, Cargo metadata | `boilerplate` for init; `gitversion`; native metadata writer |
| Xcode | `VERSION` | GitVersion |

Terraform module and Terragrunt do not expose `version` in the first profile
matrix. Their legacy `get_version` helpers calculated Make-scoped values for
tag recipes and did not provide a stable standalone user-facing result. They
are not listed by `project capabilities` and are not dispatchable. A future
public generation query requires a separate output contract and approval.

## Legacy Makefile review evidence and CLI migration

The pinned target list is retained as historical evidence for the initial CLI
parity review. It includes version-generation behavior and excludes
tag-management behavior. Each listed root Makefile dynamically includes
`https://cowk.io/acc`; this bootstrap is never evaluated by `tronador project`.
The table records the replacement CLI capability, not a runtime target.

| Repository | Pinned legacy Makefile | Historical behavior | CLI replacement |
|---|---|---|---|
| `androidsdk-app-template` | [`c8276bf`](https://github.com/cloudopsworks/androidsdk-app-template/blob/c8276bfb880bb3a662b7163d8b7c8c1a70099af3/Makefile) | `version`, `code/init` | `version`, `init` CLI operations |
| `docker-app-template` | [`1068b7c`](https://github.com/cloudopsworks/docker-app-template/blob/1068b7c5eb63ba0745ef92e77c6e8e81d3fe810d/Makefile) | `version`, `code/init` | `version`, `init` CLI operations |
| `dotnet-app-template` | [`6ec41be`](https://github.com/cloudopsworks/dotnet-app-template/blob/6ec41bebbc3706081f7b384bc295d5ca81f306f1/Makefile) | `version`, `code/init` | `version`, `init` CLI operations |
| `fluttermobile-app-template` | [`f60933a`](https://github.com/cloudopsworks/fluttermobile-app-template/blob/f60933ab3e0972a89f6c5e25a8f4d1c2975ff608/Makefile) | `version`, `code/init` | `version`, `init` CLI operations; marker decision required |
| `go-app-template` | [`a343cb4`](https://github.com/cloudopsworks/go-app-template/blob/a343cb45e883e076fd6e62e4e42f90a40d928900/Makefile) | `version`, `code/init` | `version`, `init` CLI operations |
| `java-app-template` | [`18a03e6`](https://github.com/cloudopsworks/java-app-template/blob/18a03e6be6c1860856dc4f8b84cfc4c03aad1c95/Makefile) | `version`, `code/init` | `version`, `init` CLI operations |
| `node-app-template` | [`63620dd`](https://github.com/cloudopsworks/node-app-template/blob/63620dd21757aba71d7e83d3933b4efdf9f1bf59/Makefile) | `version`, `code/init` | `version`, `init` CLI operations |
| `python-app-template` | [`3e8f511`](https://github.com/cloudopsworks/python-app-template/blob/3e8f5117b93ddcbbbdc754f635e12c2d2ce2266c/Makefile) | `version`, `code/install/tomlcli`, `code/init` | `version`, `init` CLI operations; metadata helper is internal |
| `rust-app-template` | [`2952a32`](https://github.com/cloudopsworks/rust-app-template/blob/2952a321c313a3f8d3ee2805047d93f0dc6d14b2/Makefile) | `version`, `code/init` | `version`, `init` CLI operations |
| `terraform-module-template` | [`49e8d24`](https://github.com/cloudopsworks/terraform-module-template/blob/49e8d241baad3883d4f42cc06313b78d0c401a00/Makefile) | `get_version`, `temp_provider`, `lint`, `fmt`, `init/%` | `init`, `lint`, `format` typed tool pipelines |
| `terragrunt-project-template` | [`75a628c`](https://github.com/cloudopsworks/terragrunt-project-template/blob/75a628cd7a2734aad10fbed9ba9cb11eeb48a420/Makefile) | `get_version`, `lint`, `clean`, `init/project`, `clean/project` | `init`, `lint`, `format`, `clean`, `clean-inputs` CLI operations |
| `xcode-app-template` | [`bac3e04`](https://github.com/cloudopsworks/xcode-app-template/blob/bac3e0412cdf504d1e6c0e3a1a5082a262923487/Makefile) | `version`, `code/init` | `version`, `init` CLI operations |

The inherited Tronador repository Makefile provides lifecycle and tag-related
targets. Those are outside this capability surface. The registry is explicit
and versioned; it must never derive public capabilities from a Makefile or
dynamic target listing.

## CLI-native execution design

The implementation should contain five small responsibilities:

```text
Detector → ProfileRegistry → CapabilityResolver → DependencyResolver → NativeExecutor
```

- **Detector** reads `.cloudopsworks` and returns one profile id.
- **ProfileRegistry** validates profile markers and capability bindings.
- **CapabilityResolver** validates the logical capability and arguments, then
  produces an immutable `OperationPlan`.
- **DependencyResolver** resolves each declared tool using the shared Tronador
  tool catalog and provisioner.
- **NativeExecutor** performs profile-native file operations or executes typed
  tool calls with resolved executable paths.

The Cobra layer owns only `project`, `detect`, `capabilities`, capability
lookup, shared flags, and result formatting. It does not contain a switch for
each implementation and it does not interpret Makefiles.

### Operation plan

Before mutation or child-process start, every plan contains:

```text
OperationPlan {
  implementation: string
  marker: string
  capability: string
  normalized_arguments: object
  executor: native | tool_pipeline
  native_operation: string | null
  tool_requirements: []ToolRequirement
  tool_calls: []ToolCall
  prerequisites: []Prerequisite
  mutation_class: ...
  generated_artifacts: []string
  network_policy: forbidden | declared
  confirmation_policy: ...
  dry_run: ...
}

ToolCall {
  tool_name: string
  resolved_executable: string
  arguments: []string
  working_directory: string
}
```

`tool_calls` are executed with `os/exec`-style argument arrays. Shell parsing,
Make expansion, target concatenation, and `sh -c` are out of scope. `--dry-run`
prints the operation, required tools, resolution sources, and intended calls;
it does not install tools, invoke tools, or mutate files. This is mandatory for
every mutating capability exposed by the registry.

### Tool dependency resolution and installation

The command reuses the existing Tronador tool provisioner contract used by
`tronador readme`: a JSON-backed catalog, platform-aware download metadata,
version selection, checksum verification when configured, and an isolated
user tool directory. The project command extends that catalog with the tools
needed by its profiles; it does not invent a second installer.

For each `ToolRequirement`, resolution occurs in this order:

1. an explicit command flag or profile-approved environment override;
2. an executable found on `PATH`;
3. the provisioned executable in `--tools-dir` (default
   `~/.cloudopsworks/tronador` or `TRONADOR_TOOLS_DIR`); and
4. on-demand installation of exactly that missing tool from its effective
   catalog entry when installation is enabled and network access is allowed.

The effective catalog merges, in order, embedded defaults, the user
`~/.cloudopsworks/tronador/tools.json`, project
`.cloudopsworks/tronador/tools.json`, project `.tronador/tools.json`, the
`TRONADOR_TOOLS_CONFIG`/`TRONADOR_TOOLS_CONFIG_PATH` override, and explicit
`--tools-config`. Platform-specific entries and explicit tool-version flags or
environment variables follow the existing provisioner precedence.

Installation rules are explicit:

- only tools required by the selected operation are resolved or installed;
- each downloaded artifact is version-selected, platform-selected, and
  checksum-verified when a checksum is declared;
- installation is atomic into the tool cache and safe for concurrent CLI
  processes;
- `--no-install-tools` or `TRONADOR_TOOLS_INSTALL=false` converts a missing
  tool into `project_tool_unavailable`;
- a download never installs a package manager, shell script, Makefile, or
  transitive dependency; the catalog entry must describe the executable that
  Tronador can provision directly; and
- a missing catalog entry, invalid config, failed download, or checksum failure
  stops before the operation starts and identifies the exact tool.

Tool installation is the only declared network activity for an otherwise local
operation. The resolver requires `--allow-network` before downloading; local
PATH/cache resolution remains network-free. `detect`, `capabilities`, and
`--dry-run` never download or invoke tools.

The executor must:

1. resolve and validate the complete dependency graph before the first
   mutation or tool call;
2. preserve child exit status, stdout, and stderr;
3. prevent executable, argument, and working-directory injection;
4. refuse any tool call not present in the selected profile binding; and
5. never fall back to a Makefile, an unregistered executable, or another
   implementation after an error.

### Initial tool dependency catalog

The first registry revision should use these logical requirements. The
`terraform`/`tofu` row is an alternative set: OpenTofu is the default
candidate, an explicit engine flag selects one candidate, and an ambiguous
`auto` selection never gets chosen silently.

| Logical requirement | Catalog candidates | Capabilities | Provisioning rule |
|---|---|---|---|
| application initializer | `gitversion`, `gh` plus typed native/file steps | application `code/init` | reproduce each profile's target body with format-aware native Go metadata updates; do not install legacy package helpers |
| version engine | `gitversion` | application `version` | direct versioned binary; Git metadata is input only |
| IaC engine | `terraform`, `tofu` | Terraform `lint`, `format`; Terragrunt underlying validation | OpenTofu is the default; explicit selection resolves through PATH/cache/provisioner |
| Terragrunt engine | `terragrunt` | Terragrunt HCL format and validation | direct versioned binary; its child IaC engine is resolved separately |

Existing catalog entries such as `boilerplate` and `gitversion` are reused.
Entries for `terraform`, `tofu`, and `terragrunt` must be added to the same
embedded/runtime JSON catalog before implementation. Each entry must define a
platform URL, archive format, binary path, default or required version source,
and checksum policy. The project command does not install `npm`, Maven,
Cargo, a package manager, or a shell-based installer as a side effect. If a
future profile truly requires one, it becomes a separately declared catalog
tool and dependency with the same resolution contract.

Version selection for an external tool is independent from project version
generation. The tool resolver uses, in order, an explicit tool-version flag,
the profile/project tool configuration, the documented environment override,
and the catalog default. A generated project version comes only from the
`gitversion` operation and must never be confused with a tool version or a
repository tag.

### Dependency and engine flags

The common execution flags are part of the capability schema and are reported
by `project capabilities`:

| Flag | Scope | Meaning |
|---|---|---|
| `--tools-dir` | tool-backed capabilities | Override the provisioned tool cache. |
| `--tools-config` | tool-backed capabilities | Add an explicit JSON catalog override. |
| `--no-install-tools` | tool-backed capabilities | Resolve only explicit paths, `PATH`, and cache. |
| `--allow-network` | tool-backed capabilities | Permit missing tools to be downloaded. |
| `--tool-version name=version` | tool-backed capabilities | Pin one resolved tool version for this invocation. |
| `--engine terraform|tofu|auto` | Terraform/Terragrunt lint/format capabilities | Select the IaC engine; OpenTofu is the default. |

With `--engine auto`, exactly one configured or already available candidate is
selected. If both candidates are available, the command fails clearly with
`project_tool_selection_ambiguous` rather than silently choosing one. If
neither is available, auto falls back to OpenTofu for provisioning. Project
initialization does not resolve or invoke an IaC engine.

## Safety policy

The descriptor records `dry_run`, `network_policy`, `confirmation_policy`,
`mutation_class`, `executor`, and tool dependency requirements for every
capability.

- `detect` and `capabilities` are read-only and network-free.
- Mutating operations support plan-only `--dry-run` without installing or
  evaluating tools.
- Workspace operations are local by default. Tool downloads require explicit
  `--allow-network`; otherwise a missing tool fails before the operation starts.
- `init` and `version` are explicit workspace mutations and do not require a
  second confirmation.
- Destructive operations such as `clean` require `--yes` in non-interactive
  environments and never prompt there.
- Interactive confirmation is not used to hide missing prerequisites,
  unavailable tools, ambiguous detection, unsupported capabilities, or invalid
  arguments.

## Result and error contract

Human output names the detected implementation and logical capability without
requiring a namespace. JSON output uses stable fields and no ANSI or progress
noise.

Example success:

```json
{
  "command": "project",
  "implementation": "terraform-module",
  "detected_marker": ".cloudopsworks/.terraform-module",
  "capability": "init",
  "arguments": {"provider": "aws"},
  "executor": "tool_pipeline",
  "tools": [{"name": "boilerplate", "source": "path"}, {"name": "git", "source": "path"}],
  "calls": [
    {"tool": "boilerplate", "arguments": ["--template-url", ".cloudopsworks/boilerplate/main", "--output-folder", ".", "--var", "provider=aws"]},
    {"tool": "git", "arguments": ["add", ".cloudopsworks/.provider", "*.tf"]}
  ],
  "mutation_class": "workspace_files",
  "exit_status": 0
}
```

Required failure codes:

| Code | Meaning |
|---|---|
| `project_implementation_unknown` | No recognized marker. |
| `project_implementation_ambiguous` | Different implementations matched. |
| `project_marker_invalid` | Recognized marker is not a regular file. |
| `project_registry_invalid` | Registry descriptor is malformed or duplicated. |
| `project_capability_unsupported` | Detected profile does not advertise the logical capability. |
| `project_argument_invalid` | Arguments fail the selected capability schema. |
| `project_tool_catalog_invalid` | Effective tool configuration is malformed or incomplete. |
| `project_tool_unavailable` | A declared tool is missing and cannot be resolved or provisioned. |
| `project_tool_selection_ambiguous` | More than one alternative tool is available without an explicit selection. |
| `project_tool_install_failed` | Tool download, extraction, verification, or installation failed. |
| `project_prerequisite_missing` | A declared non-tool prerequisite is unavailable. |
| `project_network_not_allowed` | Declared network use lacks `--allow-network`. |
| `project_confirmation_required` | Destructive operation lacks `--yes` in non-interactive mode. |
| `project_operation_failed` | Native operation or typed tool call failed; child exit status is preserved. |

Every structured error includes `code`, `command`, `implementation` when
known, `detected_marker` when known, `capability` when known,
`requested_arguments` when safe, `tool` and attempted resolution source when
relevant, an actionable `hint`, and `exit_status`.
Unsupported capabilities list the valid alternatives from the detected
profile. Obsolete namespace forms point to `project capabilities` and the
namespace-free grammar.

Exit status `1` is sufficient for the first implementation. No Makefile target
fallback, unregistered tool fallback, or alternate implementation is attempted
after an error.

## Extension workflow

Adding a supported implementation requires:

1. add one profile with unique id and reviewed marker paths;
2. bind only logical capabilities whose native operation/tool behavior is documented;
3. define argument schemas, prerequisites, generated artifacts, mutation,
   network, confirmation, dry-run, and tool-installation policies;
4. validate registry uniqueness at load time;
5. add detection, capability advertisement, argument, dependency-resolution,
   executor, safety, success, unsupported, and failure tests; and
6. update this matrix and obtain approval if any external contract changes.

Adding a new profile must not require a new Cobra namespace or a dispatcher
switch. Adding a new logical capability requires updating shared definitions,
the profile matrix, result schema, and approval checklist.

## Acceptance matrix

The failure state matrix below is part of the approval contract; a design is
not complete if only successful capability dispatch is covered.

### Detection and compatibility

Create disposable fixtures for all 12 markers and assert that:

- `project detect` reports the expected implementation and marker;
- `project capabilities` reports only that profile's valid capabilities;
- no marker returns `project_implementation_unknown`;
- two different implementation markers return
  `project_implementation_ambiguous`;
- a recognized marker directory or symlink returns `project_marker_invalid`;
- both Flutter aliases count as one Flutter match if the alias is approved;
- `project go init` is rejected with namespace-removal guidance; and
- existing `repos` and `repo` command behavior remains unchanged.

### Capability and dispatch

Use fake executors/tool resolvers or disposable repositories to assert:

| Command | Expected binding |
|---|---|
| `project init` in Android SDK, Docker, .NET, Flutter, Go, Java, Node, Python, Rust, Xcode | profile-native init operation and declared tool calls |
| `project init aws` in Terraform module | provider-temp removal plus typed Boilerplate render after provider validation |
| `project init` in Terragrunt | typed Boilerplate render followed by `terragrunt hcl format --exclude-dir .cloudopsworks` |
| `project version` in each application profile | profile-specific generation operation and artifacts |
| `project lint` in Terraform module | Terraform/OpenTofu tool pipeline |
| `project lint` in Terragrunt | Terragrunt/IaC tool pipeline |
| `project format` in Terraform module | typed Terraform/OpenTofu `fmt` call |
| `project format` in Terragrunt | typed `terragrunt hcl format` call, if approved |
| `project clean` in Terragrunt | `clean` with confirmation policy |
| `project clean-inputs` in Terragrunt | native cleanup operation |

Assert that Terraform/Terragrunt legacy `get_version` helpers, Python's legacy
TOML installation helper, Terraform's temporary-provider helper, all
Makefile targets, and all tag-management behavior are not public capabilities.

### Safety and result behavior

Assert that:

- detection and capability listing never invoke Make, tools, or network;
- every mutating capability's dry-run prints an operation plan and invokes no
  child process or tool installation;
- a missing or malformed tool catalog fails before operation start;
- PATH/cache resolution works without network, while missing tools require
  `--allow-network` before direct provisioning;
- only tools declared by the selected binding are resolved or installed;
- OpenTofu is the default IaC engine; explicit `--engine auto` fails clearly
  when both Terraform and OpenTofu candidates are available;
- explicit paths, `PATH`, cache, catalog version overrides, and direct
  provisioning follow the documented precedence;
- checksum, archive extraction, and atomic cache failures return the declared
  tool error before workspace mutation;
- dynamic `curl` bootstrap, package-manager installation, and Makefile fallback
  are never attempted;
- `clean` without `--yes` fails in a non-interactive environment;
- invalid Terraform provider values cannot inject tool arguments or shell syntax;
- child exit status, stdout, and stderr are preserved;
- JSON errors have stable required fields; and
- a failed operation never falls back to another capability or profile.

## Human approval record

The design was approved with this statement:

> Approved for implementation: implicit project command grammar, detection/ambiguity contract, capability schemas and mappings, version-generation semantics, safety policy, error/migration contract, and acceptance matrix.

Approval must cover the complete externally observable contract, including:

- namespace-free grammar and unchanged `repos` compatibility;
- marker precedence, Flutter alias policy, and ambiguity behavior;
- capability definitions, argument schemas, profile bindings, and typed
  Terraform/Terragrunt tool pipelines;
- version generated artifacts, prerequisites, and tagged-tree behavior without
  tag management;
- dry-run, tool resolution/installation, network, prerequisite, and confirmation
  policies;
- human/JSON result and migration/error contracts; and
- acceptance and extension-test scope.

Any future edit to an approved external contract reopens review until the
approval statement is renewed.

## Review checklist

- [x] Public grammar is namespace-free and detection-driven.
- [x] `init` is polymorphic with schema-validated provider handling.
- [x] Version generation is included and tag management remains excluded.
- [x] Terraform/Terragrunt logical lint and format bindings are documented.
- [x] Makefile execution is deprecated and CLI-native execution is specified.
- [x] Tool calls use the shared versioned installation/provisioning process.
- [x] Per-implementation capability validity is explicit.
- [x] Detection, ambiguity, safety, errors, migration, extension, and
      acceptance behavior are documented.
- [x] Human approval wording and review-reopening rule are explicit.
- [x] Human approval recorded.
