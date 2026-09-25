package repos

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Options controls repository command execution.
type Options struct {
	WorkDir    string
	ConfigPath string
	GitPath    string
	GHPath     string
	PullBranch string
	DryRun     bool
	Stdout     io.Writer
	Stderr     io.Writer
}

// Runner executes repos commands using the JSON configuration catalog.
type Runner struct {
	Config           *Config
	Opts             Options
	gitClient        GitClient
	githubClient     GitHubClient
	effects          *upgradeEffects
	suppressStaging  bool
	templateCheckout string
}

type upgradeEffects struct {
	paths map[string]struct{}
	err   error
}

func newUpgradeEffects() *upgradeEffects { return &upgradeEffects{paths: make(map[string]struct{})} }

func (e *upgradeEffects) add(paths ...string) error {
	if e.err != nil {
		return e.err
	}
	normalized, err := safeRelativePaths(paths)
	if err != nil {
		e.err = err
		return err
	}
	for _, path := range normalized {
		e.paths[path] = struct{}{}
	}
	return nil
}

func (e *upgradeEffects) list() ([]string, error) {
	if e.err != nil {
		return nil, e.err
	}
	out := make([]string, 0, len(e.paths))
	for path := range e.paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

// RepositoryState is the detected local repository layout.
type RepositoryState struct {
	WorkDir       string
	BlueprintPath string
	VersionFile   string
	Pre510        bool
	Version       string
	Templates     []Template
}

// NewRunner loads configuration and prepares a command runner.
func NewRunner(opts Options) (*Runner, error) {
	cfg, err := LoadConfig(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	abs, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	opts.WorkDir = abs
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.PullBranch == "" {
		opts.PullBranch = cfg.DefaultPullBranch
	}
	return &Runner{Config: cfg, Opts: opts}, nil
}

// Detect inspects the target repository for blueprint layout and template markers.
func (r *Runner) Detect() (RepositoryState, error) {
	state := RepositoryState{WorkDir: r.Opts.WorkDir}
	if exists(r.path(".cloudopsworks/_VERSION")) {
		state.BlueprintPath = ".cloudopsworks"
		state.VersionFile = ".cloudopsworks/_VERSION"
		state.Pre510 = false
	} else {
		state.BlueprintPath = ".github"
		state.VersionFile = ".github/_VERSION"
		state.Pre510 = true
	}
	if data, err := os.ReadFile(r.path(state.VersionFile)); err == nil {
		state.Version = strings.TrimSpace(string(data))
	}

	for _, tmpl := range r.Config.Templates {
		if r.templateMarkerExists(state.BlueprintPath, tmpl) {
			state.Templates = append(state.Templates, tmpl)
		}
	}
	return state, nil
}

// templateMarkerExists retains the legacy Flutter marker used by older
// fluttermobile repositories while the catalog standardizes on .flutter.
func (r *Runner) templateMarkerExists(blueprintPath string, tmpl Template) bool {
	if exists(r.path(blueprintPath, tmpl.Marker)) {
		return true
	}
	return tmpl.Name == "flutter" && tmpl.Marker == ".flutter" && exists(r.path(blueprintPath, ".fluttermobile"))
}

// ActiveTemplate returns the single supported template marker for a repository.
func (r *Runner) ActiveTemplate() (Template, RepositoryState, error) {
	state, err := r.Detect()
	if err != nil {
		return Template{}, state, err
	}
	if len(state.Templates) == 0 {
		return Template{}, state, fmt.Errorf("no supported repository template marker found in %s", state.BlueprintPath)
	}
	if len(state.Templates) > 1 {
		names := make([]string, 0, len(state.Templates))
		for _, tmpl := range state.Templates {
			names = append(names, tmpl.Name)
		}
		return Template{}, state, fmt.Errorf("multiple repository template markers found in %s: %s", state.BlueprintPath, strings.Join(names, ", "))
	}
	return state.Templates[0], state, nil
}

// TemplateInit prepares the active template checkout and pulls its repository.
func (r *Runner) TemplateInit(ctx context.Context) (Template, RepositoryState, error) {
	if err := r.CleanTemplate(ctx); err != nil {
		return Template{}, RepositoryState{}, err
	}
	if err := r.prepareTemplateCheckout(); err != nil {
		return Template{}, RepositoryState{}, err
	}
	tmpl, state, err := r.ActiveTemplate()
	if err != nil {
		return Template{}, state, err
	}
	return tmpl, state, r.CloneTemplate(ctx, tmpl)
}

// CloneTemplate clones a configured template into the current template checkout.
func (r *Runner) CloneTemplate(ctx context.Context, tmpl Template) error {
	label := tmpl.Description
	if label == "" {
		label = tmpl.Name + " template"
	}
	fmt.Fprintf(r.Opts.Stdout, "%s will be pulled\n", label)
	url := fmt.Sprintf("https://github.com/%s.git", tmpl.Repository)
	return r.cloneTemplateRepository(ctx, url)
}

// CleanTemplate removes the temporary template checkout.
func (r *Runner) CleanTemplate(_ context.Context) error {
	if r.Opts.DryRun {
		if r.templateCheckout == "" {
			return nil
		}
		if err := os.RemoveAll(r.templateCheckout); err != nil {
			return err
		}
		r.templateCheckout = ""
		return nil
	}
	templatePath := r.templatePath()
	if exists(templatePath) {
		fmt.Fprintln(r.Opts.Stdout, "Cleaning up template repository")
		if err := r.removeAll(templatePath); err != nil {
			return err
		}
	}
	return nil
}

// Clean removes generated GitHub workflows and recreates the directory.
func (r *Runner) Clean() error {
	fmt.Fprintln(r.Opts.Stdout, "Cleaning up repository")
	if err := r.removeAll(r.path(".github/workflows")); err != nil {
		return err
	}
	return r.mkdirAll(r.path(".github/workflows"), 0o755)
}

// Available prints the latest patch and major-compatible template tags.
func (r *Runner) Available(ctx context.Context) (err error) {
	defer r.cleanupTemplateOnExit(ctx, &err)

	tmpl, state, err := r.ActiveTemplate()
	if err != nil {
		return err
	}
	major, minor, err := parseMajorMinor(state.Version)
	if err != nil {
		return err
	}
	tags, err := r.fetchTags(ctx, tmpl.Repository)
	if err != nil {
		return err
	}
	latestMinor := latestMatchingTag(tags, currentMinorTagPattern(major, minor))
	latestMajor := latestMatchingTag(tags, currentMajorTagPattern(major))

	fmt.Fprintf(r.Opts.Stdout, "Repo: %s\n", tmpl.Repository)
	fmt.Fprintf(r.Opts.Stdout, "Version: %s = %s.%s\n", state.Version, major, minor)
	fmt.Fprintf(r.Opts.Stdout, "Latest Minor Version: %s\n", latestMinor)
	fmt.Fprintf(r.Opts.Stdout, "Repo: %s\n", tmpl.Repository)
	fmt.Fprintf(r.Opts.Stdout, "Version: %s = %s.%s\n", state.Version, major, minor)
	fmt.Fprintf(r.Opts.Stdout, "Latest Major Version: %s\n", latestMajor)
	return nil
}

// Upgrade mirrors make repos/upgrade: run the full upgrade workflow against the
// latest tag in the current major/minor line.
func (r *Runner) Upgrade(ctx context.Context) (err error) {
	defer r.cleanupTemplateOnExit(ctx, &err)

	tmpl, state, err := r.TemplateInit(ctx)
	if err != nil {
		return err
	}
	tag, major, minor, err := r.resolveDefaultUpgradeTarget(ctx, tmpl, state)
	if err != nil {
		return err
	}
	fmt.Fprintf(r.Opts.Stdout, "Repo: %s\n", tmpl.Repository)
	fmt.Fprintf(r.Opts.Stdout, "Version: %s = %s.%s\n", state.Version, major, minor)
	fmt.Fprintf(r.Opts.Stdout, "Last Version: %s\n", tag)
	return r.Stack(ctx, StackOptions{Template: tmpl, State: state, PullBranch: tag})
}

// UpgradeVersion upgrades from a user-specified tag or branch.
func (r *Runner) UpgradeVersion(ctx context.Context, version string) (err error) {
	defer r.cleanupTemplateOnExit(ctx, &err)

	tmpl, state, err := r.TemplateInit(ctx)
	if err != nil {
		return err
	}
	pullBranch, err := r.resolveExplicitUpgradeTarget(ctx, tmpl, state, version)
	if err != nil {
		return err
	}
	return r.Stack(ctx, StackOptions{Template: tmpl, State: state, PullBranch: pullBranch})
}

func (r *Runner) cleanupTemplateOnExit(ctx context.Context, errp *error) {
	if cleanupErr := r.CleanTemplate(ctx); cleanupErr != nil && *errp == nil {
		*errp = cleanupErr
	}
}

// prepareTemplateCheckout creates the isolated target-analysis checkout used
// by dry-run upgrades. The caller lifecycle removes it through CleanTemplate.
func (r *Runner) prepareTemplateCheckout() error {
	if !r.Opts.DryRun || r.templateCheckout != "" {
		return nil
	}
	path, err := os.MkdirTemp("", "tronador-template-")
	if err != nil {
		return err
	}
	r.templateCheckout = path
	return nil
}

func (r *Runner) resolveDefaultUpgradeTarget(ctx context.Context, tmpl Template, state RepositoryState) (tag, major, minor string, err error) {
	major, minor, err = parseMajorMinor(state.Version)
	if err != nil {
		return "", "", "", err
	}
	tags, err := r.fetchTags(ctx, tmpl.Repository)
	if err != nil {
		return "", "", "", err
	}
	tag = latestMatchingTag(tags, currentMinorTagPattern(major, minor))
	if tag == "" {
		return "", "", "", fmt.Errorf("no matching tags found for %s in %s.%s", tmpl.Repository, major, minor)
	}
	return tag, major, minor, nil
}

func (r *Runner) resolveExplicitUpgradeTarget(ctx context.Context, tmpl Template, state RepositoryState, version string) (string, error) {
	target := strings.TrimSpace(version)
	switch {
	case strings.EqualFold(target, "major"):
		tag, major, err := r.resolveMajorUpgradeTarget(ctx, tmpl, state)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(r.Opts.Stdout, "Repo: %s\n", tmpl.Repository)
		fmt.Fprintf(r.Opts.Stdout, "Version: %s = %s.x\n", state.Version, major)
		fmt.Fprintf(r.Opts.Stdout, "Latest Major Version: %s\n", tag)
		return tag, nil
	case strings.EqualFold(target, "master"):
		return "master", nil
	case target == "":
		tag, _, _, err := r.resolveDefaultUpgradeTarget(ctx, tmpl, state)
		return tag, err
	default:
		return target, nil
	}
}

func (r *Runner) resolveMajorUpgradeTarget(ctx context.Context, tmpl Template, state RepositoryState) (string, string, error) {
	major, _, err := parseMajorMinor(state.Version)
	if err != nil {
		return "", "", err
	}
	tags, err := r.fetchTags(ctx, tmpl.Repository)
	if err != nil {
		return "", "", err
	}
	tag := latestMatchingTag(tags, currentMajorTagPattern(major))
	if tag == "" {
		return "", "", fmt.Errorf("no matching tags found for %s in major %s", tmpl.Repository, major)
	}
	return tag, major, nil
}

// Fetch checks out the requested branch/tag in the template checkout and returns
// its commit hash. It is an internal full-upgrade workflow step, not a public
// CLI subcommand.
func (r *Runner) Fetch(ctx context.Context, pullBranch string) (string, error) {
	if pullBranch == "" {
		pullBranch = r.Opts.PullBranch
	}
	if pullBranch == "" {
		return "", fmt.Errorf("pull branch is required")
	}
	fmt.Fprintf(r.Opts.Stdout, "Fetching template repository from branch: %s\n", pullBranch)
	hash, err := r.checkoutTemplateRepository(ctx, pullBranch)
	if err != nil {
		return "", err
	}
	if err := r.removeAll(r.templatePath(".git")); err != nil {
		return "", err
	}
	fmt.Fprintf(r.Opts.Stdout, "Template repository hash: %s\n", hash)
	return hash, nil
}

// EvalTemplateVersion detects the blueprint generation from fetched workflow
// references. Application-template _VERSION is deliberately not a generation
// signal: it is only a release marker copied after a successful upgrade.
func (r *Runner) EvalTemplateVersion() (string, error) {
	workflows := r.templatePath(".github", "workflows")
	versions := make(map[string]struct{})
	leaves, err := r.regularLeaves(workflows)
	if err != nil {
		return "", err
	}
	for _, path := range leaves {
		if !isYAMLFile(path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		var document yaml.Node
		if err := yaml.Unmarshal(data, &document); err != nil {
			return "", fmt.Errorf("parse target workflow %s: %w", filepath.ToSlash(path), err)
		}
		collectBlueprintGenerations(&document, "", versions)
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("target workflows contain no active CloudOps blueprint generation reference")
	}
	if len(versions) > 1 {
		return "", fmt.Errorf("target workflows contain mixed CloudOps blueprint generations")
	}
	if _, ok := versions["v5.10"]; ok {
		fmt.Fprintln(r.Opts.Stdout, "Target blueprint generation detected: v5.10")
		return "v5.10", nil
	}
	fmt.Fprintln(r.Opts.Stdout, "Target blueprint generation detected: v5.9")
	return "", nil
}

var blueprintUseReference = regexp.MustCompile(`^cloudopsworks/blueprints/[^@\s]+@(v5\.(?:9|10))$`)
var blueprintRefValue = regexp.MustCompile(`^v5\.(?:9|10)$`)

func collectBlueprintGenerations(node *yaml.Node, key string, versions map[string]struct{}) {
	if node == nil {
		return
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			collectBlueprintGenerations(node.Content[i+1], node.Content[i].Value, versions)
		}
		return
	}
	if node.Kind == yaml.SequenceNode || node.Kind == yaml.DocumentNode {
		for _, child := range node.Content {
			collectBlueprintGenerations(child, key, versions)
		}
		return
	}
	if node.Kind != yaml.ScalarNode {
		return
	}
	if key == "uses" {
		if match := blueprintUseReference.FindStringSubmatch(node.Value); len(match) == 2 {
			versions[match[1]] = struct{}{}
		}
	}
	if key == "blueprint_ref" {
		if match := blueprintRefValue.FindStringSubmatch(node.Value); len(match) == 1 {
			versions[node.Value] = struct{}{}
		}
	}
}

// postMigrationState is the deterministic v5.10 destination state used by
// real and dry-run upgrades alike; dry-run must not Detect an unmodified tree.
func postMigrationState(state RepositoryState) RepositoryState {
	state.BlueprintPath = ".cloudopsworks"
	state.VersionFile = ".cloudopsworks/_VERSION"
	state.Pre510 = false
	return state
}

// StackOptions carries state between upgrade target equivalents.
type StackOptions struct {
	Template         Template
	State            RepositoryState
	PullBranch       string
	TemplateHash     string
	V510Plus         string
	migrationOps     []Operation
	migrationPlan    *stackMutationPlan
	migrationApplied bool
}

const (
	gitignoreManagedStartMarker = "# BEGIN TRONADOR TEMPLATE MANAGED BLOCK"
	gitignoreManagedEndMarker   = "# END TRONADOR TEMPLATE MANAGED BLOCK"
)

// Stack applies the fetched template into the target repository and commits it.
// It is an internal full-upgrade workflow step, not a public CLI subcommand.
func (r *Runner) Stack(ctx context.Context, opts StackOptions) (err error) {
	defer func() {
		if r.Opts.DryRun && r.templateCheckout != "" {
			if cleanupErr := r.CleanTemplate(ctx); cleanupErr != nil && err == nil {
				err = cleanupErr
			}
		}
	}()
	if opts.Template.Name == "" || opts.State.WorkDir == "" {
		tmpl, state, err := r.TemplateInit(ctx)
		if err != nil {
			return err
		}
		opts.Template = tmpl
		opts.State = state
	}
	if opts.PullBranch == "" {
		opts.PullBranch = r.Opts.PullBranch
	}
	if opts.TemplateHash == "" {
		hash, err := r.Fetch(ctx, opts.PullBranch)
		if err != nil {
			return err
		}
		opts.TemplateHash = hash
	}
	// Workflow blueprint references are the target-generation authority. A
	// caller hint must agree with the fetched target rather than bypass it.
	detectedVersion, err := r.EvalTemplateVersion()
	if err != nil {
		return err
	}
	if opts.V510Plus != "" && opts.V510Plus != detectedVersion {
		return fmt.Errorf("provided target generation %q conflicts with active target workflow generation %q", opts.V510Plus, detectedVersion)
	}
	opts.V510Plus = detectedVersion
	if opts.State.WorkDir != "" && filepath.Clean(opts.State.WorkDir) != filepath.Clean(r.Opts.WorkDir) {
		return fmt.Errorf("repository state workdir %q does not match runner workdir %q", opts.State.WorkDir, r.Opts.WorkDir)
	}
	targetVersion := ""
	var targetVersionData []byte
	var targetVersionMode os.FileMode
	if opts.V510Plus != "" {
		var versionErr error
		targetVersionData, targetVersionMode, targetVersion, versionErr = r.targetCloudOpsVersionMarker()
		if versionErr != nil {
			return versionErr
		}
	}

	if !opts.Template.Merge {
		fmt.Fprintln(r.Opts.Stdout, "No templates supported to upgrade by this script, skipping upgrade")
		return nil
	}

	r.effects = newUpgradeEffects()
	defer func() { r.effects = nil }()
	// Construct and validate every configuration write before any repository or
	// external-default mutation. The resulting plan is immutable for this run.
	var configPlan cloudOpsworksConfigPlan
	if opts.V510Plus != "" {
		// Reject a symlinked target configuration before its YAML can be parsed.
		// This is still pre-mutation: Clean has not run and no target state moved.
		if _, sourceErr := r.regularLeaves(r.templatePath(".cloudopsworks")); sourceErr != nil {
			return sourceErr
		}
		configPlan, err = r.buildCloudOpsworksConfigPlanExcluding(opts.State, opts.Template.BoilerplatePathV510Plus)
		if err != nil {
			return err
		}
	}
	if opts.V510Plus != "" && opts.State.Pre510 {
		route, version, routeErr := templateMigrationRoute(opts.Template)
		if routeErr != nil {
			return routeErr
		}
		_, ops, routeErr := r.migrationOperations(route, version)
		if routeErr != nil {
			return routeErr
		}
		// Stack always requires this directory. Seed it into the compiled plan so
		// moves targeting .cloudopsworks resolve before any filesystem mutation.
		opts.migrationOps = append([]Operation{{Action: "ensureDir", Destination: ".cloudopsworks"}}, r.selectMigrationOperations(ops)...)
		migrationPlan, resolveErr := r.resolveMigrationPlanSelected(opts.migrationOps)
		if resolveErr != nil {
			return resolveErr
		}
		opts.migrationPlan = &migrationPlan
	}
	if err := r.preflightStackDestinations(opts, configPlan); err != nil {
		return err
	}
	// The compiled migration must consume legacy sources before Clean replaces
	// workflows or any later Stack step changes the filesystem it was planned on.
	if opts.migrationPlan != nil {
		route, version, routeErr := templateMigrationRoute(opts.Template)
		if routeErr != nil {
			return routeErr
		}
		if err := r.migrateResolved(route, version, opts.migrationPlan); err != nil {
			return err
		}
		opts.migrationApplied = true
	}

	fmt.Fprintf(r.Opts.Stdout, "Upgrading repository, current Version: %s from %s\n", opts.State.Version, opts.PullBranch)
	org, repo, err := r.gitRemoteOwnerRepo(ctx)
	if r.Opts.DryRun {
		fmt.Fprintln(r.Opts.Stdout, "Skipping gh repo set-default in dry-run")
	} else if err == nil && org != "" && repo != "" {
		fmt.Fprintf(r.Opts.Stdout, "Setting default repository: %s/%s\n", org, repo)
		if err := r.setDefaultRepository(ctx, org, repo); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(r.Opts.Stderr, "Skipping gh repo set-default: %v\n", err)
	}
	fmt.Fprintln(r.Opts.Stdout, "Updating from template repository")
	previousWorkflowPaths, err := treeFiles(r.path(".github/workflows"))
	if err != nil {
		return err
	}
	if err := r.Clean(); err != nil {
		return err
	}
	if err := r.gitStageExact(ctx, relativePaths(r.Opts.WorkDir, previousWorkflowPaths)...); err != nil {
		return err
	}

	originalPre510 := opts.State.Pre510
	if opts.Template.Versioned {
		if err := r.applyVersionedTemplateWithPlan(opts.Template, opts.State, opts.V510Plus, configPlan, false, opts.migrationApplied); err != nil {
			return err
		}
	} else {
		if err := r.applyUnversionedTemplate(); err != nil {
			return err
		}
	}

	updatedState := opts.State
	if opts.V510Plus != "" {
		updatedState = postMigrationState(opts.State)
	}
	if opts.Template.Boilerplate {
		if opts.V510Plus != "" {
			// The target generation selects its boilerplate, even when the source
			// repository started in the pre-v5.10 layout.
			if err := r.applyMergedBoilerplate(opts.Template); err != nil {
				return err
			}
		} else if err := r.applyBoilerplate(opts.Template, originalPre510); err != nil {
			return err
		}
	}
	if opts.Template.CICD {
		if opts.Template.Versioned && opts.V510Plus != "" {
			err = r.cicdUpdateWithVersion(updatedState, opts.TemplateHash, targetVersion)
			if err != nil {
				return err
			}
		} else if err := r.CICDUpdate(updatedState, opts.TemplateHash); err != nil {
			return err
		}
	}
	if opts.Template.Versioned && opts.V510Plus != "" {
		// Remove the old marker first. The captured target marker write is the
		// final managed worktree mutation, after all earlier work has succeeded.
		if originalPre510 {
			legacyVersion := r.path(".github/_VERSION")
			if exists(legacyVersion) || r.Opts.DryRun {
				if err := r.removeAll(legacyVersion); err != nil {
					return err
				}
				if err := r.gitStageExact(ctx, ".github/_VERSION"); err != nil {
					return err
				}
			}
		}
		versionPath := r.path(".cloudopsworks/_VERSION")
		if _, err := r.writeFileIfChanged(versionPath, targetVersionData, targetVersionMode); err != nil {
			return err
		}
		if err := r.gitAdd(ctx, ".cloudopsworks/_VERSION"); err != nil {
			return err
		}
	}
	if err := r.removeAll(r.templatePath()); err != nil {
		return err
	}
	if err := r.commitUpgradeEffects(ctx, updatedState, targetVersion); err != nil {
		return err
	}
	fmt.Fprintln(r.Opts.Stdout, "Please review changes and push to remote repository")
	return nil
}

func (r *Runner) copyFileTrackingChange(src, dst string) (bool, error) {
	if err := r.preflightSourceAncestors(src); err != nil {
		return false, err
	}
	if _, err := os.Lstat(src); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	before, beforeErr := os.ReadFile(dst)
	if beforeErr != nil && !errors.Is(beforeErr, os.ErrNotExist) {
		return false, beforeErr
	}
	if err := r.copyFile(src, dst); err != nil {
		return false, err
	}
	if r.Opts.DryRun {
		return true, nil
	}
	after, err := os.ReadFile(dst)
	if err != nil {
		return false, err
	}
	return beforeErr != nil || !bytes.Equal(before, after), nil
}

// stackMutationPlan is the immutable, filesystem-only view of every leaf Stack
// may change. Building it performs no repository mutation, so Stack can reject
// unsafe trees before Clean removes the workflow directory.
type stackMutationPlan struct {
	sources      []string
	destinations []string
	deletions    []string
	ensureDirs   []string
	// moveRoots are directory rename targets. They matter even for empty
	// source directories, which have no leaf effects but still mutate a path.
	moveRoots []string
	// migrationActions are fully resolved before mutation and replayed without
	// inspecting the changing worktree at runtime.
	migrationActions []migrationAction
}

type migrationAction struct {
	action      string
	source      string
	destination string
	effects     []string
}

func (p *stackMutationPlan) addDestination(path string) {
	p.destinations = append(p.destinations, path)
}
func (p *stackMutationPlan) addSource(path string)   { p.sources = append(p.sources, path) }
func (p *stackMutationPlan) addDeletion(path string) { p.deletions = append(p.deletions, path) }

// preflightStackDestinations resolves the complete Stack mutation surface before
// Clean. Keep this resolver aligned with the execution helpers: it models
// optional copies only when their template source exists, ISSUE_TEMPLATE's
// .disabled rename and reserved filtering, v5.10 migration operations, and
// post-migration CICD/version locations.
func (r *Runner) preflightStackDestinations(opts StackOptions, configPlan cloudOpsworksConfigPlan) error {
	plan, err := r.resolveStackMutationPlan(opts, configPlan)
	if err != nil {
		return err
	}
	for _, source := range normalizedAbsolutePaths(plan.sources) {
		if err := r.preflightSourceLeaf(source); err != nil {
			return err
		}
	}
	for _, path := range normalizedAbsolutePaths(append(plan.destinations, plan.deletions...)) {
		if err := r.preflightDestination(path); err != nil {
			return err
		}
	}
	for _, dir := range normalizedAbsolutePaths(plan.ensureDirs) {
		if err := r.preflightDirectory(dir); err != nil {
			return err
		}
	}
	for _, root := range normalizedAbsolutePaths(plan.moveRoots) {
		if err := r.preflightMoveRoot(root); err != nil {
			return err
		}
	}
	all := append(append([]string{}, plan.destinations...), plan.deletions...)
	_, err = safeRelativePaths(relativePaths(r.Opts.WorkDir, all))
	return err
}

func (r *Runner) resolveStackMutationPlan(opts StackOptions, configPlan cloudOpsworksConfigPlan) (stackMutationPlan, error) {
	var plan stackMutationPlan
	addTree := func(src, dst string, stale bool) error {
		leaves, err := r.regularLeaves(src)
		if err != nil {
			return err
		}
		for _, leaf := range leaves {
			rel, _ := filepath.Rel(src, leaf)
			plan.addSource(leaf)
			plan.addDestination(filepath.Join(dst, rel))
		}
		if stale {
			leaves, err := r.managedLeaves(dst, false)
			if err != nil {
				return err
			}
			for _, leaf := range leaves {
				plan.addDeletion(leaf)
			}
		}
		return nil
	}
	template := r.templatePath()
	if err := addTree(filepath.Join(template, ".github/workflows"), r.path(".github/workflows"), true); err != nil {
		return plan, err
	}

	// Issue templates are copied only when their final destination is missing.
	// Do not validate an existing destination that execution deliberately skips.
	issueSrc := filepath.Join(template, ".github/ISSUE_TEMPLATE")
	if err := r.preflightSourceDirectoryIfExists(issueSrc); err != nil {
		return plan, err
	}
	entries, err := os.ReadDir(issueSrc)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return plan, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		reserved := false
		for _, prefix := range reservedIssueTemplatePrefixes {
			if strings.HasPrefix(name, prefix) {
				reserved = true
				break
			}
		}
		if reserved {
			continue
		}
		src := filepath.Join(issueSrc, name)
		dst := r.path(".github/ISSUE_TEMPLATE", templateIssueDestinationName(name))
		if !exists(dst) {
			plan.addSource(src)
			plan.addDestination(dst)
		}
	}

	// Exact refresh of target non-YAML hooks; stale non-YAML leaves are deletes.
	hooksSrc, hooksDst := filepath.Join(template, ".cloudopsworks/hooks"), r.path(".cloudopsworks/hooks")
	if opts.V510Plus != "" {
		if err := r.addNonYAMLTreePlan(&plan, hooksSrc, hooksDst, true); err != nil {
			return plan, err
		}
	}

	// All optional standalone files follow the execution branches. A source is
	// included only when it exists, avoiding accidental staging of local files.
	if err := r.addMissingFilePlan(&plan, filepath.Join(template, ".github/PULL_REQUEST_TEMPLATE.md"), r.path(".github/PULL_REQUEST_TEMPLATE.md")); err != nil {
		return plan, err
	}
	for _, rel := range []string{".github/dependabot.yml", ".github/secret_scanning.yml", ".github/codeql/codeql-config.yml"} {
		if err := r.addAbsentPathFilePlan(&plan, filepath.Join(template, rel), r.path(rel)); err != nil {
			return plan, err
		}
	}
	if err := r.addOptionalFilePlan(&plan, filepath.Join(template, ".gitignore"), r.path(".gitignore")); err != nil {
		return plan, err
	}
	if !opts.Template.Versioned || opts.V510Plus == "" {
		for _, rel := range []string{".cloudopsworks/auto-assign.yml", ".github/auto-assign.yml"} {
			if err := r.addAbsentPathFilePlan(&plan, filepath.Join(template, rel), r.path(rel)); err != nil {
				return plan, err
			}
		}
	}
	rootFiles := []string{"Makefile"}
	if opts.Template.Versioned {
		rootFiles = append(rootFiles, "AGENTS.md", "CLAUDE.md", "README-TEMPLATE.md", ".helmignore", ".dockerignore")
	}
	for _, rel := range rootFiles {
		if err := r.addOptionalFilePlan(&plan, filepath.Join(template, rel), r.path(rel)); err != nil {
			return plan, err
		}
	}
	if opts.Template.Versioned && opts.V510Plus != "" {
		plan.ensureDirs = append(plan.ensureDirs, r.path(".cloudopsworks"))
		for _, write := range configPlan.writes {
			plan.addDestination(write.path)
			if write.removeSource != "" {
				plan.addDeletion(write.removeSource)
			}
		}
		if opts.State.Pre510 {
			ops := opts.migrationOps
			if ops == nil {
				route, version, err := templateMigrationRoute(opts.Template)
				if err != nil {
					return plan, err
				}
				_, raw, err := r.migrationOperations(route, version)
				if err != nil {
					return plan, err
				}
				ops = r.selectMigrationOperations(raw)
			}
			migration := opts.migrationPlan
			if migration == nil {
				resolved, err := r.resolveMigrationPlanSelected(ops)
				if err != nil {
					return plan, err
				}
				migration = &resolved
			}
			plan.sources = append(plan.sources, migration.sources...)
			plan.destinations = append(plan.destinations, migration.destinations...)
			plan.deletions = append(plan.deletions, migration.deletions...)
			plan.ensureDirs = append(plan.ensureDirs, migration.ensureDirs...)
			plan.moveRoots = append(plan.moveRoots, migration.moveRoots...)
		}
		if err := r.addOptionalFilePlan(&plan, filepath.Join(template, ".cloudopsworks/Makefile"), r.path(".cloudopsworks/Makefile")); err != nil {
			return plan, err
		}
		if err := r.addOptionalFilePlan(&plan, filepath.Join(template, ".cloudopsworks/_VERSION"), r.path(".cloudopsworks/_VERSION")); err != nil {
			return plan, err
		}
		if opts.State.Pre510 {
			plan.addDeletion(r.path(".github/_VERSION"))
		}
		// CICD writes the post-migration layout, whether or not it existed.
		if opts.Template.CICD {
			plan.addDestination(r.path(postMigrationState(opts.State).BlueprintPath, "cloudopsworks-ci.yaml"))
		}
	} else if opts.Template.Versioned {
		for _, rel := range []string{"_VERSION", "labeler.yml", "Makefile"} {
			if err := r.addOptionalFilePlan(&plan, filepath.Join(template, ".github", rel), r.path(".github", rel)); err != nil {
				return plan, err
			}
		}
		if opts.Template.CICD {
			plan.addDestination(r.path(opts.State.BlueprintPath, "cloudopsworks-ci.yaml"))
		}
	} else if opts.Template.CICD {
		plan.addDestination(r.path(opts.State.BlueprintPath, "cloudopsworks-ci.yaml"))
	}

	if opts.Template.Boilerplate {
		path := boilerplatePath(opts.Template, opts.V510Plus == "")
		source := filepath.Join(template, path)
		if path != "" && exists(source) {
			if err := addTree(source, r.path(path), true); err != nil {
				return plan, err
			}
		}
	}
	return plan, nil
}

func (r *Runner) addOptionalFilePlan(plan *stackMutationPlan, src, dst string) error {
	if err := r.preflightSourceAncestors(src); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("unsafe template source %q", src)
	}
	plan.addSource(src)
	plan.addDestination(dst)
	return nil
}

// addMissingFilePlan models the non-destructive copy helpers, which leave an
// existing destination untouched.
func (r *Runner) addMissingFilePlan(plan *stackMutationPlan, src, dst string) error {
	if exists(dst) {
		return nil
	}
	return r.addOptionalFilePlan(plan, src, dst)
}

// addAbsentPathFilePlan matches helpers that use Lstat semantics: an existing
// path, including a broken symlink, is intentionally left untouched.
func (r *Runner) addAbsentPathFilePlan(plan *stackMutationPlan, src, dst string) error {
	if pathExists(dst) {
		return nil
	}
	return r.addOptionalFilePlan(plan, src, dst)
}

func (r *Runner) addNonYAMLTreePlan(plan *stackMutationPlan, src, dst string, stale bool) error {
	if !exists(src) {
		return nil
	}
	leaves, err := r.regularLeaves(src)
	if err != nil {
		return err
	}
	for _, leaf := range leaves {
		if isYAMLFile(leaf) || filepath.Base(leaf) == "_VERSION" {
			continue
		}
		rel, _ := filepath.Rel(src, leaf)
		plan.addSource(leaf)
		plan.addDestination(filepath.Join(dst, rel))
	}
	if stale {
		leaves, err := r.managedLeaves(dst, true)
		if err != nil {
			return err
		}
		for _, leaf := range leaves {
			plan.addDeletion(leaf)
		}
	}
	return nil
}

// resolveMigrationPlan resolves operations exactly as runOperations will, but
// performs no mkdir/rename. Sources and destination leaves are therefore safe
// to validate while the legacy layout still exists.
func (r *Runner) resolveMigrationPlan(templateName, version string) (stackMutationPlan, error) {
	_, ops, err := r.migrationOperations(templateName, version)
	if err != nil {
		return stackMutationPlan{}, err
	}
	return r.resolveMigrationPlanSelected(r.selectMigrationOperations(ops))
}

func (r *Runner) resolveMigrationPlanSelected(ops []Operation) (stackMutationPlan, error) {
	var out stackMutationPlan
	// Runtime operations are ordered: a preceding ensureDir or directory move
	// changes whether a later move destination receives the source basename.
	// Record that state now so execution never re-interprets a changed tree.
	establishedDirs := map[string]struct{}{}
	for _, op := range ops {
		switch op.Action {
		case "ensureDir":
			dir := r.path(op.Destination)
			if err := r.preflightDirectory(dir); err != nil {
				return out, err
			}
			out.ensureDirs = append(out.ensureDirs, dir)
			out.migrationActions = append(out.migrationActions, migrationAction{action: "ensureDir", destination: dir})
			r.recordEnsuredDirectories(establishedDirs, dir)
		case "gitAdd":
			continue
		case "unsupported":
			return out, fmt.Errorf("not supported: %s", op.Message)
		case "move":
			src := r.path(op.Source)
			dst, err := r.resolveMoveOneDestination(src, r.path(op.Destination), establishedDirs)
			if err != nil {
				return out, err
			}
			isDir, err := r.resolveMigrationMove(&out, src, dst, op.Optional)
			if err != nil {
				return out, err
			}
			if isDir {
				r.recordEnsuredDirectories(establishedDirs, dst)
			}
		case "moveMany":
			matched := false
			for _, pattern := range op.Sources {
				if err := r.preflightGlobSource(pattern); err != nil {
					return out, err
				}
				paths, err := filepath.Glob(r.path(pattern))
				if err != nil {
					return out, err
				}
				if len(paths) == 0 {
					if _, err := os.Lstat(r.path(pattern)); err == nil {
						paths = []string{r.path(pattern)}
					}
				}
				for _, src := range paths {
					matched = true
					dst := filepath.Join(r.path(op.Destination), filepath.Base(src))
					isDir, err := r.resolveMigrationMove(&out, src, dst, false)
					if err != nil {
						return out, err
					}
					if isDir {
						r.recordEnsuredDirectories(establishedDirs, dst)
					}
				}
			}
			if !matched && !op.Optional {
				return out, fmt.Errorf("no sources matched for moveMany to %s", op.Destination)
			}
		default:
			return out, fmt.Errorf("unsupported migration action %q", op.Action)
		}
	}
	if err := validateMigrationClaims(out.migrationActions); err != nil {
		return out, err
	}
	return out, nil
}

// recordEnsuredDirectories tracks every directory mkdirAll or a directory
// move makes available, including missing ancestors created by mkdirAll.
func (r *Runner) recordEnsuredDirectories(directories map[string]struct{}, path string) {
	root := filepath.Clean(r.Opts.WorkDir)
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		directories[current] = struct{}{}
		if current == root || filepath.Dir(current) == current {
			return
		}
	}
}

// validateMigrationClaims rejects action overlaps that would either make a
// compiled destination ambiguous or remove a later action's source. This makes
// the precompiled sequence executable without re-reading the changing tree.
func validateMigrationClaims(actions []migrationAction) error {
	for i, action := range actions {
		switch action.action {
		case "ensureDir":
			for j, other := range actions {
				if other.action != "move" {
					continue
				}
				if j < i && (migrationPathsOverlap(action.destination, other.destination) || migrationPathsOverlap(action.destination, other.source)) {
					return fmt.Errorf("migration move/ensure dependency conflict %q, %q, and %q", other.source, other.destination, action.destination)
				}
				if j > i {
					// An ensured ancestor of an existing source is a no-op. Equal to or
					// below that source would create/change the tree being moved.
					if sameOrAncestorPath(other.source, action.destination) {
						return fmt.Errorf("migration ensure/source dependency conflict %q and %q", action.destination, other.source)
					}
					// The concrete destination was resolved with all earlier ensures.
					// Only a strict parent ensure is safe; equal or descendant claims
					// would turn the move target into a directory at execution time.
					if migrationPathsOverlap(action.destination, other.destination) && !strictAncestorPath(action.destination, other.destination) {
						return fmt.Errorf("migration ensure/destination dependency conflict %q and %q", action.destination, other.destination)
					}
				}
			}
		case "move":
			if migrationPathsOverlap(action.source, action.destination) {
				return fmt.Errorf("migration source/destination dependency conflict %q and %q", action.source, action.destination)
			}
			for _, prior := range actions[:i] {
				if prior.action != "move" {
					continue
				}
				if migrationPathsOverlap(prior.destination, action.destination) {
					return fmt.Errorf("migration destination claim conflict %q and %q", prior.destination, action.destination)
				}
				if migrationPathsOverlap(prior.source, action.source) {
					return fmt.Errorf("migration source claim conflict %q and %q", prior.source, action.source)
				}
				if migrationPathsOverlap(prior.destination, action.source) || migrationPathsOverlap(prior.source, action.destination) {
					return fmt.Errorf("migration source/destination dependency conflict %q, %q, %q, and %q", prior.source, prior.destination, action.source, action.destination)
				}
			}
		}
	}
	return nil
}

func migrationPathsOverlap(first, second string) bool {
	return sameOrAncestorPath(first, second) || sameOrAncestorPath(second, first)
}

func strictAncestorPath(ancestor, descendant string) bool {
	return filepath.Clean(ancestor) != filepath.Clean(descendant) && sameOrAncestorPath(ancestor, descendant)
}

func sameOrAncestorPath(ancestor, descendant string) bool {
	ancestor, descendant = filepath.Clean(ancestor), filepath.Clean(descendant)
	if ancestor == descendant {
		return true
	}
	rel, err := filepath.Rel(ancestor, descendant)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveMoveOneDestination mirrors moveOneEffects: an existing directory,
// including one made by a prior ensureDir operation, receives the source base.
func (r *Runner) resolveMoveOneDestination(src, dst string, established map[string]struct{}) (string, error) {
	if _, ok := established[filepath.Clean(dst)]; ok {
		return filepath.Join(dst, filepath.Base(src)), nil
	}
	info, err := os.Lstat(dst)
	if errors.Is(err, os.ErrNotExist) {
		return dst, nil
	}
	if err != nil {
		return "", err
	}
	// Runtime Stat follows symlinks, but preflight must reject the symlink at
	// its original destination rather than accidentally validating its target.
	if info.Mode()&os.ModeSymlink != 0 {
		return dst, nil
	}
	if info.IsDir() {
		return filepath.Join(dst, filepath.Base(src)), nil
	}
	return dst, nil
}

func (r *Runner) resolveMigrationMove(plan *stackMutationPlan, src, dst string, optional bool) (bool, error) {
	if err := r.preflightSourceAncestors(src); err != nil {
		return false, err
	}
	info, err := os.Lstat(src)
	if errors.Is(err, os.ErrNotExist) {
		if optional {
			return false, nil
		}
		return false, fmt.Errorf("source does not exist: %s", src)
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("unsafe migration source %q", src)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return false, fmt.Errorf("unsafe migration source %q", src)
	}
	leaves, err := r.regularLeaves(src)
	if err != nil {
		return false, err
	}
	if err := r.preflightMigrationDestination(dst); err != nil {
		return false, err
	}
	if !info.IsDir() {
		leaves = []string{src}
	} else {
		// An empty directory has no file effects, but os.Rename still changes its
		// destination. Validate that collision before runtime reaches it.
		plan.moveRoots = append(plan.moveRoots, dst)
	}
	effects := make([]string, 0, len(leaves)*2)
	for _, leaf := range leaves {
		rel, _ := filepath.Rel(src, leaf)
		if !info.IsDir() {
			rel = ""
		}
		resolvedDestination := filepath.Join(dst, rel)
		plan.addSource(leaf)
		plan.addDeletion(leaf)
		plan.addDestination(resolvedDestination)
		effects = append(effects, leaf, resolvedDestination)
	}
	plan.migrationActions = append(plan.migrationActions, migrationAction{
		action:      "move",
		source:      src,
		destination: dst,
		effects:     effects,
	})
	return info.IsDir(), nil
}

// preflightMigrationDestination rejects replacement by rename. The only
// permitted existing directory is the operation's parent, which has already
// been incorporated by resolveMoveOneDestination into the final destination.
func (r *Runner) preflightMigrationDestination(path string) error {
	if err := r.safeFileDestination(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode().IsRegular() || info.IsDir() {
		return fmt.Errorf("migration destination collision %q", path)
	}
	return fmt.Errorf("invalid migration destination %q", path)
}

func (r *Runner) preflightMoveRoot(path string) error {
	return r.preflightMigrationDestination(path)
}

func (r *Runner) regularLeaves(root string) ([]string, error) {
	if err := r.preflightSourceAncestors(root); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("unsafe template source %q", root)
	}
	if info.Mode().IsRegular() {
		return []string{root}, nil
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("unsafe template source %q", root)
	}
	var leaves []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe template source %q", path)
		}
		leaves = append(leaves, path)
		return nil
	})
	return leaves, err
}

// managedLeaves validates existing stale files too. nonYAML limits hook stale
// deletion to the exact files replaceDirNonYAMLIfExists will remove.
func (r *Runner) managedLeaves(root string, nonYAML bool) ([]string, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("unsafe managed tree %q", root)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("invalid managed tree %q", root)
	}
	var leaves []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe managed leaf %q", path)
		}
		if nonYAML && (isYAMLFile(path) || filepath.Base(path) == "_VERSION") {
			return nil
		}
		leaves = append(leaves, path)
		return nil
	})
	return leaves, err
}

func (r *Runner) preflightSourceLeaf(path string) error {
	clean := filepath.Clean(path)
	if err := r.preflightSourceAncestors(clean); err != nil {
		return err
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("unsafe source leaf %q", path)
	}
	return nil
}

// preflightSourceAncestors rejects source paths that reach a file through a
// symlinked ancestor. Lstat on only the leaf is insufficient because ReadDir,
// WalkDir, Glob, and ReadFile otherwise follow a directory symlink outside the
// checkout before the leaf can be validated.
func (r *Runner) preflightSourceAncestors(path string) error {
	clean := filepath.Clean(path)
	root := ""
	for _, candidate := range []string{filepath.Clean(r.Opts.WorkDir), filepath.Clean(r.templateCheckout)} {
		if candidate == "." || candidate == "" {
			continue
		}
		rel, err := filepath.Rel(candidate, clean)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && (root == "" || len(candidate) > len(root)) {
			root = candidate
		}
	}
	if root == "" {
		return fmt.Errorf("source outside workdir: %s", path)
	}
	for dir := filepath.Dir(clean); ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if dir == root {
					return err
				}
				continue
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("unsafe source ancestor %q", dir)
		}
		if dir == root {
			return nil
		}
	}
}

func (r *Runner) preflightSourceDirectoryIfExists(path string) error {
	if err := r.preflightSourceAncestors(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("unsafe template source %q", path)
	}
	return nil
}

func (r *Runner) preflightGlobSource(pattern string) error {
	// Catalog validation permits globs only in migration sources. Validate the
	// literal directory prefix before filepath.Glob can follow an ancestor.
	path := filepath.FromSlash(pattern)
	hasGlob := strings.ContainsAny(path, "*?[")
	for strings.ContainsAny(filepath.Base(path), "*?[") {
		path = filepath.Dir(path)
	}
	if !hasGlob {
		return r.preflightSourceAncestors(r.path(path))
	}
	return r.preflightSourceDirectoryIfExists(r.path(path))
}

func (r *Runner) preflightDirectory(path string) error {
	if err := r.safeFileDestination(filepath.Join(path, ".tronador-preflight")); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return fmt.Errorf("invalid managed directory %q", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (r *Runner) preflightDestination(path string) error {
	root := filepath.Clean(r.Opts.WorkDir)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe managed destination %q", path)
	}
	if err := r.safeFileDestination(path); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && (info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("invalid managed destination %q", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (r *Runner) replaceTreeExact(src, dst string) ([]string, error) {
	before, err := treeFiles(dst)
	if err != nil {
		return nil, err
	}
	if err := r.copyDir(src, dst); err != nil {
		return nil, err
	}
	after, err := treeFiles(src)
	if err != nil {
		return nil, err
	}
	paths := append(before, after...)
	for i, path := range after {
		rel, _ := filepath.Rel(src, path)
		paths[len(before)+i] = filepath.Join(dst, rel)
	}
	return normalizedAbsolutePaths(paths), nil
}

func treeFiles(root string) ([]string, error) {
	if !exists(root) {
		return nil, nil
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func (r *Runner) applyVersionedTemplate(tmpl Template, state RepositoryState, templateVersion string) error {
	plan, err := r.buildCloudOpsworksConfigPlan(state)
	if err != nil {
		return err
	}
	return r.applyVersionedTemplateWithPlan(tmpl, state, templateVersion, plan, true, false)
}

func (r *Runner) applyVersionedTemplateWithPlan(tmpl Template, state RepositoryState, templateVersion string, configPlan cloudOpsworksConfigPlan, writeVersion, migrationApplied bool) error {
	// Direct helper callers do not pass through Stack. Compile and execute their
	// migration before replacing workflows or creating its destination root.
	if templateVersion != "" && state.Pre510 && !migrationApplied {
		route, migrationVersion, err := templateMigrationRoute(tmpl)
		if err != nil {
			return err
		}
		_, ops, err := r.migrationOperations(route, migrationVersion)
		if err != nil {
			return err
		}
		selected := append([]Operation{{Action: "ensureDir", Destination: ".cloudopsworks"}}, r.selectMigrationOperations(ops)...)
		resolved, err := r.resolveMigrationPlanSelected(selected)
		if err != nil {
			return err
		}
		if err := r.migrateResolved(route, migrationVersion, &resolved); err != nil {
			return err
		}
	}
	workflowPaths, err := r.replaceTreeExact(r.templatePath(".github/workflows"), r.path(".github/workflows"))
	if err != nil {
		return err
	}
	if err := r.gitStageExact(context.Background(), relativePaths(r.Opts.WorkDir, workflowPaths)...); err != nil {
		return err
	}
	if err := r.copyIssueTemplatesIfExists(context.Background()); err != nil {
		return err
	}
	if err := r.copyPullRequestTemplateIfExists(context.Background()); err != nil {
		return err
	}
	if err := r.copyDependabotIfExists(context.Background()); err != nil {
		return err
	}
	if err := r.copyGitHubSecurityConfigsIfExists(context.Background()); err != nil {
		return err
	}
	if err := r.mergeGitignoreIfExists(context.Background()); err != nil {
		return err
	}
	rootTemplateFiles := []string{"Makefile", "AGENTS.md", "CLAUDE.md", "README-TEMPLATE.md", ".helmignore", ".dockerignore"}
	changedRoot := make([]string, 0, len(rootTemplateFiles))
	for _, file := range rootTemplateFiles {
		changed, err := r.copyFileTrackingChange(r.templatePath(file), r.path(file))
		if err != nil {
			return err
		}
		if changed {
			changedRoot = append(changedRoot, file)
		}
	}
	if err := r.gitAdd(context.Background(), changedRoot...); err != nil {
		return err
	}

	if templateVersion != "" {
		fmt.Fprintln(r.Opts.Stdout, "Detected template version v5.10+")
		if err := r.mkdirAll(r.path(".cloudopsworks"), 0o755); err != nil {
			return err
		}
		// YAML is value-aware: target YAML supplies structure and documentation,
		// while local active configuration wins. This must happen after migration
		// and before _VERSION establishes the successful target release.
		changedConfig, err := r.applyCloudOpsworksConfigPlan(configPlan)
		if err != nil {
			return err
		}
		if err := r.gitAdd(context.Background(), changedConfig...); err != nil {
			return err
		}
		hookPaths, err := r.replaceDirNonYAMLIfExists(r.templatePath(".cloudopsworks", "hooks"), r.path(".cloudopsworks", "hooks"))
		if err != nil {
			return err
		}
		if err := r.gitStageExact(context.Background(), relativePaths(r.Opts.WorkDir, hookPaths)...); err != nil {
			return err
		}
		makefilePath := r.path(".cloudopsworks/Makefile")
		if exists(r.templatePath(".cloudopsworks/Makefile")) {
			if err := r.copyFile(r.templatePath(".cloudopsworks/Makefile"), makefilePath); err != nil {
				return err
			}
			if err := r.gitAdd(context.Background(), ".cloudopsworks/Makefile"); err != nil {
				return err
			}
		}
		if writeVersion {
			// Standalone callers retain historical Stack API behavior; Stack itself
			// defers this marker until every downstream upgrade step succeeds.
			if err := r.copyFile(r.templatePath(".cloudopsworks/_VERSION"), r.path(".cloudopsworks/_VERSION")); err != nil {
				return err
			}
			if err := r.gitAdd(context.Background(), ".cloudopsworks/_VERSION"); err != nil {
				return err
			}
		}

	} else {
		fmt.Fprintln(r.Opts.Stdout, "Detected template version < v5.10")
		changed := make([]string, 0, 3)
		for _, file := range []string{"_VERSION", "labeler.yml", "Makefile"} {
			didChange, err := r.copyFileTrackingChange(r.templatePath(".github", file), r.path(".github", file))
			if err != nil {
				return err
			}
			if didChange {
				changed = append(changed, filepath.ToSlash(filepath.Join(".github", file)))
			}
		}
		if err := r.gitAdd(context.Background(), changed...); err != nil {
			return err
		}
		if err := r.copyAutoAssignIfExists(context.Background()); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) applyUnversionedTemplate() error {
	workflowPaths, err := r.replaceTreeExact(r.templatePath(".github/workflows"), r.path(".github/workflows"))
	if err != nil {
		return err
	}
	if err := r.gitStageExact(context.Background(), relativePaths(r.Opts.WorkDir, workflowPaths)...); err != nil {
		return err
	}
	if err := r.copyIssueTemplatesIfExists(context.Background()); err != nil {
		return err
	}
	if err := r.copyPullRequestTemplateIfExists(context.Background()); err != nil {
		return err
	}
	if err := r.mergeGitignoreIfExists(context.Background()); err != nil {
		return err
	}
	if err := r.copyAutoAssignIfExists(context.Background()); err != nil {
		return err
	}
	if err := r.copyDependabotIfExists(context.Background()); err != nil {
		return err
	}
	if err := r.copyGitHubSecurityConfigsIfExists(context.Background()); err != nil {
		return err
	}

	changed, err := r.copyFileTrackingChange(r.templatePath("Makefile"), r.path("Makefile"))
	if err != nil {
		return err
	}
	if changed {
		return r.gitAdd(context.Background(), "Makefile")
	}
	return nil
}

func (r *Runner) mergeGitignoreIfExists(ctx context.Context) error {
	src := r.templatePath(".gitignore")
	if !exists(src) {
		return nil
	}
	templateContent, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read template .gitignore: %w", err)
	}
	managedBlock, err := buildGitignoreManagedBlock(templateContent)
	if err != nil {
		return fmt.Errorf("build managed .gitignore block: %w", err)
	}

	dst := r.path(".gitignore")
	existing, err := os.ReadFile(dst)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		existing = nil
	default:
		return fmt.Errorf("read .gitignore: %w", err)
	}
	merged := mergeGitignoreContent(existing, managedBlock)
	changed, err := r.writeFileIfChanged(dst, merged, 0o644)
	if err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}
	if !changed {
		fmt.Fprintln(r.Opts.Stdout, "Not modifying .gitignore")
		return nil
	}
	return r.gitAdd(ctx, ".gitignore")
}

func buildGitignoreManagedBlock(templateContent []byte) ([]byte, error) {
	if start, end, ok := findGitignoreManagedBlock(templateContent); ok {
		body := templateContent[start+len(gitignoreManagedStartMarker) : end-len(gitignoreManagedEndMarker)]
		body = trimOneLineEnding(body)
		body = trimTrailingLineEnding(body)
		return formatGitignoreManagedBlock(body), nil
	}
	if bytes.Contains(templateContent, []byte(gitignoreManagedStartMarker)) || bytes.Contains(templateContent, []byte(gitignoreManagedEndMarker)) {
		return nil, fmt.Errorf("template contains malformed managed markers")
	}
	return formatGitignoreManagedBlock(templateContent), nil
}

func mergeGitignoreContent(existing, managedBlock []byte) []byte {
	if start, end, ok := findGitignoreManagedBlock(existing); ok {
		replacement := managedBlock
		if startsWithLineEnding(existing[end:]) {
			replacement = trimTrailingLineEnding(replacement)
		}
		merged := make([]byte, 0, len(existing)-end+start+len(replacement))
		merged = append(merged, existing[:start]...)
		merged = append(merged, replacement...)
		merged = append(merged, existing[end:]...)
		return merged
	}
	merged := make([]byte, 0, len(existing)+1+len(managedBlock))
	merged = append(merged, existing...)
	if len(merged) > 0 && merged[len(merged)-1] != '\n' {
		merged = append(merged, '\n')
	}
	merged = append(merged, managedBlock...)
	return merged
}

func findGitignoreManagedBlock(content []byte) (start, end int, ok bool) {
	startMarker := []byte(gitignoreManagedStartMarker)
	endMarker := []byte(gitignoreManagedEndMarker)
	starts := make([]int, 0, 1)
	var pairs [][2]int
	for lineStart := 0; lineStart < len(content); {
		lineEnd := bytes.IndexByte(content[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(content) - lineStart
		}
		line := content[lineStart : lineStart+lineEnd]
		if bytes.HasSuffix(line, []byte("\r")) {
			line = line[:len(line)-1]
		}
		switch {
		case bytes.Equal(line, startMarker):
			starts = append(starts, lineStart)
		case bytes.Equal(line, endMarker) && len(starts) > 0:
			start := starts[len(starts)-1]
			starts = starts[:len(starts)-1]
			pairs = append(pairs, [2]int{start, lineStart + len(line)})
		}
		lineStart += lineEnd
		if lineStart < len(content) {
			lineStart++
		}
	}
	if len(pairs) == 0 {
		return 0, 0, false
	}
	last := pairs[len(pairs)-1]
	return last[0], last[1], true
}

func formatGitignoreManagedBlock(body []byte) []byte {
	block := make([]byte, 0, len(body)+len(gitignoreManagedStartMarker)+len(gitignoreManagedEndMarker)+4)
	block = append(block, gitignoreManagedStartMarker...)
	block = append(block, '\n')
	block = append(block, body...)
	if len(block) == 0 || block[len(block)-1] != '\n' {
		block = append(block, '\n')
	}
	block = append(block, gitignoreManagedEndMarker...)
	block = append(block, '\n')
	return block
}

func trimOneLineEnding(content []byte) []byte {
	if bytes.HasPrefix(content, []byte("\r\n")) {
		return content[2:]
	}
	if bytes.HasPrefix(content, []byte("\n")) {
		return content[1:]
	}
	return content
}

func trimTrailingLineEnding(content []byte) []byte {
	if bytes.HasSuffix(content, []byte("\r\n")) {
		return content[:len(content)-2]
	}
	if bytes.HasSuffix(content, []byte("\n")) {
		return content[:len(content)-1]
	}
	return content
}

func startsWithLineEnding(content []byte) bool {
	return bytes.HasPrefix(content, []byte("\r\n")) || bytes.HasPrefix(content, []byte("\n"))
}

// gitAddCloudopsworks is retained for the standalone compatibility command.
// Upgrade paths use operation effects and never call this broad helper.
func (r *Runner) gitAddCloudopsworks(ctx context.Context) error {
	if r.suppressStaging {
		return nil
	}
	root := r.path(".cloudopsworks")
	if !exists(root) {
		return nil
	}
	var paths []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(r.Opts.WorkDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel != ".cloudopsworks/auto-assign.yml" {
			paths = append(paths, rel)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Strings(paths)
	return r.gitAdd(ctx, paths...)
}

func (r *Runner) copyIssueTemplatesIfExists(ctx context.Context) error {
	src := r.templatePath(".github/ISSUE_TEMPLATE")
	if err := r.preflightSourceDirectoryIfExists(src); err != nil {
		return err
	}
	if !exists(src) {
		return nil
	}
	fmt.Fprintln(r.Opts.Stdout, "Copying missing issue templates, excluding reserved 98_* and 99_* files")
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := r.mkdirAll(r.path(".github/ISSUE_TEMPLATE"), 0o755); err != nil {
		return err
	}
	copied := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		reserved := false
		for _, pfx := range reservedIssueTemplatePrefixes {
			if strings.HasPrefix(name, pfx) {
				reserved = true
				break
			}
		}
		if reserved {
			continue
		}
		if entry.IsDir() {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(".github/ISSUE_TEMPLATE", templateIssueDestinationName(name)))
		dst := r.path(rel)
		if exists(dst) {
			fmt.Fprintf(r.Opts.Stdout, "Not modifying %s\n", rel)
			continue
		}
		if err := r.copyFile(filepath.Join(src, name), dst); err != nil {
			return err
		}
		copied = append(copied, rel)
	}
	if len(copied) == 0 {
		return nil
	}
	return r.gitAdd(ctx, copied...)
}

func (r *Runner) copyPullRequestTemplateIfExists(ctx context.Context) error {
	src := r.templatePath(".github/PULL_REQUEST_TEMPLATE.md")
	if !exists(src) {
		return nil
	}
	fmt.Fprintln(r.Opts.Stdout, "Copying missing pull request template")
	dst := filepath.ToSlash(filepath.Join(".github", "PULL_REQUEST_TEMPLATE.md"))
	if exists(r.path(dst)) {
		fmt.Fprintf(r.Opts.Stdout, "Not modifying %s\n", dst)
		return nil
	}
	if err := r.copyFile(src, r.path(dst)); err != nil {
		return err
	}
	return r.gitAdd(ctx, dst)
}

func (r *Runner) copyDependabotIfExists(ctx context.Context) error {
	const rel = ".github/dependabot.yml"
	src := r.templatePath(rel)
	if !exists(src) {
		return nil
	}
	fmt.Fprintln(r.Opts.Stdout, "Copying missing Dependabot configuration")
	dst := r.path(rel)
	if pathExists(dst) {
		fmt.Fprintf(r.Opts.Stdout, "Not modifying %s\n", rel)
		return nil
	}
	if err := r.copyFileAtomically(src, dst); err != nil {
		return fmt.Errorf("copy %s: %w", rel, err)
	}
	return r.gitAdd(ctx, rel)
}

func (r *Runner) copyGitHubSecurityConfigsIfExists(ctx context.Context) error {
	for _, rel := range []string{
		".github/secret_scanning.yml",
		".github/codeql/codeql-config.yml",
	} {
		src := r.templatePath(rel)
		if !exists(src) {
			continue
		}
		dst := r.path(rel)
		if pathExists(dst) {
			fmt.Fprintf(r.Opts.Stdout, "Not modifying %s\n", rel)
			continue
		}
		fmt.Fprintf(r.Opts.Stdout, "Copying missing %s\n", rel)
		if err := r.copyFileAtomically(src, dst); err != nil {
			return fmt.Errorf("copy %s: %w", rel, err)
		}
		if err := r.gitAdd(ctx, rel); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) copyAutoAssignIfExists(ctx context.Context) error {
	for _, rel := range []string{".cloudopsworks/auto-assign.yml", ".github/auto-assign.yml"} {
		src := r.templatePath(rel)
		if !exists(src) {
			continue
		}
		dst := r.path(rel)
		if pathExists(dst) {
			fmt.Fprintf(r.Opts.Stdout, "Not modifying %s\n", rel)
			continue
		}
		fmt.Fprintf(r.Opts.Stdout, "Copying missing %s\n", rel)
		if err := r.copyFileAtomically(src, dst); err != nil {
			return fmt.Errorf("copy %s: %w", rel, err)
		}
		if err := r.gitAdd(ctx, rel); err != nil {
			return err
		}
	}
	return nil
}

// reservedIssueTemplatePrefixes lists the filename prefixes that are
// template-repo-only and must never be copied into an implementation repo.
var reservedIssueTemplatePrefixes = []string{"98_", "99_"}

func templateIssueDestinationName(name string) string {
	if strings.HasSuffix(name, ".disabled") {
		return strings.TrimSuffix(name, ".disabled")
	}
	return name
}

func (r *Runner) applyBoilerplate(tmpl Template, pre510 bool) error {
	path := boilerplatePath(tmpl, pre510)
	if path == "" {
		return nil
	}
	src, dest := r.templatePath(path), r.path(path)
	if !exists(src) {
		return nil
	}
	effects, err := r.replaceTreeExact(src, dest)
	if err != nil {
		return err
	}
	return r.gitStageExact(context.Background(), relativePaths(r.Opts.WorkDir, effects)...)
}

// applyMergedBoilerplate exact-refreshes opaque v5.10 boilerplate after the
// configuration merge has excluded that subtree.
func (r *Runner) applyMergedBoilerplate(tmpl Template) error {
	path := boilerplatePath(tmpl, false)
	if path == "" {
		return nil
	}
	effects, err := r.replaceTreeExceptVersion(r.templatePath(path), r.path(path))
	if err != nil {
		return err
	}
	return r.gitStageExact(context.Background(), relativePaths(r.Opts.WorkDir, effects)...)
}

// replaceTreeExceptVersion exact-refreshes opaque boilerplate assets (including
// Go-template YAML). Only the repository release marker itself is reserved for
// Stack's final write; nested boilerplate _VERSION files are ordinary assets.
func (r *Runner) replaceTreeExceptVersion(src, dst string) ([]string, error) {
	if !exists(src) {
		return nil, nil
	}
	before, err := treeFiles(dst)
	if err != nil {
		return nil, err
	}
	reserveReleaseMarker := filepath.Clean(dst) == filepath.Clean(r.path(".cloudopsworks"))
	for _, file := range before {
		if !(reserveReleaseMarker && filepath.Clean(file) == filepath.Join(filepath.Clean(dst), "_VERSION")) {
			if err := r.removeAll(file); err != nil {
				return nil, err
			}
		}
	}
	after, err := treeFiles(src)
	if err != nil {
		return nil, err
	}
	var effects []string
	for _, source := range after {
		rel, _ := filepath.Rel(src, source)
		destination := filepath.Join(dst, rel)
		if reserveReleaseMarker && filepath.Clean(destination) == filepath.Join(filepath.Clean(dst), "_VERSION") {
			continue
		}
		if err := r.copyFile(source, destination); err != nil {
			return nil, err
		}
		effects = append(effects, destination)
	}
	for _, file := range before {
		if !(reserveReleaseMarker && filepath.Clean(file) == filepath.Join(filepath.Clean(dst), "_VERSION")) {
			effects = append(effects, file)
		}
	}
	return normalizedAbsolutePaths(effects), nil
}

func boilerplatePath(tmpl Template, pre510 bool) string {
	path := tmpl.BoilerplatePathV510Plus
	if pre510 || path == "" {
		path = tmpl.BoilerplatePathPre510
	}
	return path
}

// CICDUpdate updates the workflow-version-tag footer in cloudopsworks-ci.yaml.
func (r *Runner) CICDUpdate(state RepositoryState, templateHash string) error {
	versionData, err := os.ReadFile(r.path(state.BlueprintPath, "_VERSION"))
	if err != nil {
		return fmt.Errorf("read %s/_VERSION: %w", state.BlueprintPath, err)
	}
	return r.cicdUpdateWithVersion(state, templateHash, strings.TrimSpace(string(versionData)))
}

// cicdUpdateWithVersion lets Stack use the target release in the footer before
// copying the release marker, preserving _VERSION-last sequencing.
func (r *Runner) cicdUpdateWithVersion(state RepositoryState, templateHash, version string) error {
	fmt.Fprintln(r.Opts.Stdout, "Updating CICD Pipeline cloudopsworks-ci.yaml")
	path := r.path(state.BlueprintPath, "cloudopsworks-ci.yaml")
	content := ""
	if data, err := os.ReadFile(path); err == nil {
		content = string(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !strings.Contains(content, "#workflow-version-tag") {
		content += "\n\n############ DO NOT MODIFY BELOW ############\n#workflow-version-tag: 1.0.0"
	}
	re := regexp.MustCompile(`#workflow-version-tag.*`)
	content = re.ReplaceAllString(content, fmt.Sprintf("#workflow-version-tag: %s - hash: %s", version, templateHash))
	changed, err := r.writeFileIfChanged(path, []byte(content), 0o644)
	if err != nil || !changed {
		return err
	}
	rel, relErr := filepath.Rel(r.Opts.WorkDir, path)
	if relErr != nil {
		return relErr
	}
	return r.gitAdd(context.Background(), filepath.ToSlash(rel))
}

// targetCloudOpsVersionMarker captures the validated target release marker
// before Stack mutates the repository. The final marker write then cannot fail
// because the temporary checkout was changed or removed by an earlier step.
func (r *Runner) targetCloudOpsVersionMarker() ([]byte, os.FileMode, string, error) {
	path := r.templatePath(".cloudopsworks/_VERSION")
	if err := r.preflightSourceAncestors(path); err != nil {
		return nil, 0, "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, "", fmt.Errorf("read target %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, 0, "", fmt.Errorf("unsafe target %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, "", fmt.Errorf("read target %s: %w", path, err)
	}
	version := strings.TrimSpace(string(data))
	if version == "" {
		return nil, 0, "", fmt.Errorf("target %s is empty", path)
	}
	return data, info.Mode().Perm(), version, nil
}

func (r *Runner) commitUpgradeEffects(ctx context.Context, state RepositoryState, targetVersions ...string) error {
	targetVersion := ""
	if len(targetVersions) > 0 {
		targetVersion = targetVersions[0]
	}
	paths, err := r.effects.list()
	if err != nil {
		return fmt.Errorf("record upgrade effects: %w", err)
	}
	if len(paths) == 0 {
		return nil
	}
	// Resolve the final leaf set once. An effect can disappear before the
	// transaction boundary (for example an untracked temporary workflow), so
	// staging and commit must use the same existing-or-tracked paths.
	if !r.Opts.DryRun {
		paths, err = r.existingOrTrackedPaths(ctx, paths)
		if err != nil {
			return err
		}
	}
	if len(paths) == 0 {
		return nil
	}
	// Bypass recording exactly once at the transaction boundary.
	effects := r.effects
	r.effects = nil
	err = r.gitStagePaths(ctx, paths)
	r.effects = effects
	if err != nil {
		return err
	}
	if !r.Opts.DryRun {
		changed, err := r.upgradeEffectPathsChanged(ctx, paths)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
	}
	version := state.Version
	if targetVersion != "" {
		version = targetVersion
	}
	if data, err := os.ReadFile(r.path(state.VersionFile)); err == nil {
		if targetVersion == "" {
			version = strings.TrimSpace(string(data))
		}
	}
	args := append([]string{"--literal-pathspecs", "commit", "--only", "-m", fmt.Sprintf("chore: Upgrade repository from template, version: %s", version), "--"}, paths...)
	if _, err := r.output(ctx, r.gitPath(), args...); err != nil {
		return fmt.Errorf("commit upgrade effects: %w", err)
	}
	fmt.Fprintf(r.Opts.Stdout, "Repository upgraded to version: %s\n", version)
	return nil
}

// upgradeEffectPathsChanged compares the exact transaction paths with HEAD in
// both the index and worktree. The commit uses --only with those paths, so it
// must not be attempted when neither view contains an effect; unrelated staged
// changes are intentionally outside both pathspecs.
func (r *Runner) upgradeEffectPathsChanged(ctx context.Context, paths []string) (bool, error) {
	for _, args := range [][]string{
		{"--literal-pathspecs", "diff", "--quiet", "--cached", "HEAD", "--"},
		{"--literal-pathspecs", "diff", "--quiet", "HEAD", "--"},
	} {
		args = append(args, paths...)
		cmd := exec.CommandContext(ctx, r.gitPath(), args...)
		cmd.Dir = r.Opts.WorkDir
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
				return true, nil
			}
			return false, fmt.Errorf("check upgrade effect paths %q: %w", paths, err)
		}
	}
	return false, nil
}

// Push is the standalone compatibility entrypoint. Callers must stage only
// template-owned paths. It validates that contract, then commits precisely the
// caller's index; an ordinary index commit intentionally preserves staged bytes
// even when the worktree has since changed.
func (r *Runner) Push(ctx context.Context, tmpl Template, state RepositoryState) error {
	paths, err := r.stagedUpgradePaths(ctx)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	fmt.Fprintln(r.Opts.Stdout, "Committing changes")
	version := state.Version
	if data, err := os.ReadFile(r.path(state.VersionFile)); err == nil {
		version = strings.TrimSpace(string(data))
	}
	if err := r.run(ctx, r.gitPath(), "commit", "-m", fmt.Sprintf("chore: Upgrade repository from template, version: %s", version)); err != nil {
		return err
	}
	fmt.Fprintf(r.Opts.Stdout, "Repository upgraded to version: %s\n", version)
	return nil
}

func (r *Runner) stagedUpgradePaths(ctx context.Context) ([]string, error) {
	output, err := r.output(ctx, r.gitPath(), "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return nil, err
	}
	paths := strings.Split(output, "\x00")
	if len(paths) > 0 && paths[len(paths)-1] == "" {
		paths = paths[:len(paths)-1]
	}
	for _, path := range paths {
		// Git paths are opaque bytes carried through NUL delimiters. Do not let
		// the generic normalizer turn a whitespace-prefixed path into an allowed
		// owned path and then commit the original index entry.
		if strings.TrimSpace(path) != path {
			return nil, fmt.Errorf("refusing whitespace-altered staged path %q", path)
		}
	}
	paths, err = safeRelativePaths(paths)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if !isUpgradeOwnedPath(path) {
			return nil, fmt.Errorf("refusing to commit non-upgrade staged path %q", path)
		}
	}
	return paths, nil
}

func isUpgradeOwnedPath(path string) bool {
	if strings.HasPrefix(path, ".cloudopsworks/") ||
		strings.HasPrefix(path, ".github/workflows/") ||
		strings.HasPrefix(path, ".github/ISSUE_TEMPLATE/") ||
		strings.HasPrefix(path, ".github/codeql/") ||
		strings.HasPrefix(path, ".github/vars/") ||
		strings.HasPrefix(path, ".github/values/") {
		return true
	}
	switch path {
	case ".gitignore", "Makefile", "AGENTS.md", "CLAUDE.md", "README-TEMPLATE.md", ".helmignore", ".dockerignore",
		".github/PULL_REQUEST_TEMPLATE.md", ".github/dependabot.yml", ".github/auto-assign.yml", ".github/secret_scanning.yml",
		".github/_VERSION", ".github/labeler.yml", ".github/Makefile", ".github/LICENSE", ".github/.iac", ".github/.inputs_cicd", ".github/.provider", ".github/.terraform-module", ".github/.dotnet", ".github/.golang", ".github/.rust", ".github/.xcode", ".github/.android", ".github/.flutter", ".github/.fluttermobile", ".github/.java", ".github/.node", ".github/.docker", ".github/.python", ".github/.argocd",
		".github/cloudopsworks-ci.yaml", ".github/gitversion_trunkbased.yaml":
		return true
	default:
		return false
	}
}

// Recover overlays the fetched template checkout over the repository without committing.
func (r *Runner) Recover(ctx context.Context) (err error) {
	previousSuppression := r.suppressStaging
	r.suppressStaging = true
	defer func() { r.suppressStaging = previousSuppression }()
	defer r.cleanupTemplateOnExit(ctx, &err)

	tmpl, state, err := r.TemplateInit(ctx)
	if err != nil {
		return err
	}
	if _, err := r.Fetch(ctx, r.Opts.PullBranch); err != nil {
		return err
	}
	if !tmpl.Merge {
		fmt.Fprintln(r.Opts.Stdout, "No templates supported to upgrade by this script, skipping upgrade")
		return nil
	}
	fmt.Fprintf(r.Opts.Stdout, "Upgrading repository, current Version: %s from %s\n", state.Version, r.Opts.PullBranch)
	org, repo, err := r.gitRemoteOwnerRepo(ctx)
	if r.Opts.DryRun {
		fmt.Fprintln(r.Opts.Stdout, "Skipping gh repo set-default in dry-run")
	} else if err == nil && org != "" && repo != "" {
		fmt.Fprintf(r.Opts.Stdout, "Setting default repository: %s/%s\n", org, repo)
		if err := r.setDefaultRepository(ctx, org, repo); err != nil {
			return err
		}
	}
	fmt.Fprintln(r.Opts.Stdout, "Fetching template repository")
	if err := r.Clean(); err != nil {
		return err
	}
	if err := r.copyDirContents(r.templatePath(), r.Opts.WorkDir); err != nil {
		return err
	}
	if tmpl.Boilerplate {
		if err := r.applyBoilerplate(tmpl, state.Pre510); err != nil {
			return err
		}
	}
	if tmpl.CICD {
		if err := r.CICDUpdate(state, ""); err != nil {
			return err
		}
	}
	if err := r.removeAll(r.templatePath()); err != nil {
		return err
	}
	fmt.Fprintln(r.Opts.Stdout, "!!WARNING!! Some files may have been overwritten, please review changes and push to remote repository")
	return nil
}

// Migrate runs a configured migration and stages only the files the migration moved.
func (r *Runner) Migrate(templateName, version string) error {
	effects, err := r.migrateEffects(templateName, version)
	if err != nil {
		return err
	}
	return r.gitStageExact(context.Background(), relativePaths(r.Opts.WorkDir, effects)...)
}

func (r *Runner) migrateEffects(templateName, version string) ([]string, error) {
	if version == "" {
		version, templateName = templateName, ""
	}
	plan, ops, err := r.migrationOperations(templateName, version)
	if err != nil {
		return nil, err
	}
	if templateName == "" {
		fmt.Fprintf(r.Opts.Stdout, "Migrating Common repository structure to v%s+\n", displayMigrationVersion(plan.Version))
	} else {
		fmt.Fprintf(r.Opts.Stdout, "Migrating %s repository structure to v%s+\n", templateName, displayMigrationVersion(plan.Version))
	}
	return r.runOperations(ops)
}

func (r *Runner) migrateResolved(templateName, version string, migrationPlan *stackMutationPlan) error {
	plan, _, err := r.migrationOperations(templateName, version)
	if err != nil {
		return err
	}
	fmt.Fprintf(r.Opts.Stdout, "Migrating %s repository structure to v%s+\n", templateName, displayMigrationVersion(plan.Version))
	effects, err := r.executeMigrationPlan(migrationPlan)
	if err != nil {
		return err
	}
	return r.gitStageExact(context.Background(), relativePaths(r.Opts.WorkDir, effects)...)
}

// selectMigrationOperations snapshots initial-filesystem conditions. It returns
// a non-nil empty slice when no operation applies, which Stack uses to
// distinguish an immutable empty selection from an unspecified selection.
func (r *Runner) selectMigrationOperations(ops []Operation) []Operation {
	selected := make([]Operation, 0, len(ops))
	for _, op := range ops {
		if r.shouldRun(op.When) {
			selected = append(selected, op)
		}
	}
	return selected
}

// migrationOperations resolves a catalog migration exactly once for both the
// preflight interpreter and the runtime executor. Unsupported routes are
// explicitly terminal; every executable route receives the plan's common
// operations before its route-specific operations.
func (r *Runner) migrationOperations(templateName, version string) (MigrationPlan, []Operation, error) {
	plan, ok := r.Config.FindMigrationPlan(version)
	if !ok {
		return MigrationPlan{}, nil, fmt.Errorf("migration version %q is not configured", version)
	}
	if templateName == "" {
		return plan, plan.Common, nil
	}
	route := normalizeKey(templateName)
	if route == "terraform-module" {
		route = "terraform"
	}
	routeOperations, ok := plan.Templates[route]
	if !ok {
		return MigrationPlan{}, nil, fmt.Errorf("migration %s/%s is not configured", templateName, version)
	}
	if isUnsupportedMigration(routeOperations) {
		return plan, routeOperations, nil
	}
	operations := make([]Operation, 0, len(plan.Common)+len(routeOperations))
	operations = append(operations, plan.Common...)
	return plan, append(operations, routeOperations...), nil
}

func isUnsupportedMigration(operations []Operation) bool {
	return len(operations) == 1 && operations[0].Action == "unsupported"
}

// migrationRoute resolves the declarative template route/version. The fallback
// preserves direct callers that build a legacy Template value in tests or API
// code; catalog-backed Stack calls use Template.Migration.
func templateMigrationRoute(tmpl Template) (string, string, error) {
	if strings.TrimSpace(tmpl.Migration) == "" {
		if strings.TrimSpace(tmpl.Name) == "" {
			return "", "", fmt.Errorf("template migration is required")
		}
		return tmpl.Name, "510", nil
	}
	return parseMigrationRoute(tmpl.Migration)
}

// runOperations returns exact source/deletion and destination/addition paths.
func (r *Runner) runOperations(ops []Operation) ([]string, error) {
	// Conditions deliberately observe the initial migration filesystem. Resolve
	// every concrete action before mkdir or rename can change that view.
	return r.runSelectedOperations(r.selectMigrationOperations(ops))
}

func (r *Runner) runSelectedOperations(selected []Operation) ([]string, error) {
	plan, err := r.resolveMigrationPlanSelected(selected)
	if err != nil {
		return nil, err
	}
	return r.executeMigrationPlan(&plan)
}

func (r *Runner) executeMigrationPlan(plan *stackMutationPlan) ([]string, error) {
	var effects []string
	for _, action := range plan.migrationActions {
		switch action.action {
		case "ensureDir":
			if err := r.mkdirAll(action.destination, 0o755); err != nil {
				return nil, err
			}
		case "move":
			if err := r.movePath(action.source, action.destination); err != nil {
				return nil, err
			}
			effects = append(effects, action.effects...)
		default:
			return nil, fmt.Errorf("unsupported migration action %q", action.action)
		}
	}
	return normalizedAbsolutePaths(effects), nil
}

func (r *Runner) moveManyEffects(patterns []string, destination string, optional bool) ([]string, error) {
	var effects []string
	matched := false
	for _, pattern := range patterns {
		if err := r.preflightGlobSource(pattern); err != nil {
			return nil, err
		}
		paths, err := filepath.Glob(r.path(pattern))
		if err != nil {
			return nil, err
		}
		if len(paths) == 0 && exists(r.path(pattern)) {
			paths = []string{r.path(pattern)}
		}
		for _, source := range paths {
			matched = true
			moved, err := r.movePathEffects(source, r.path(destination, filepath.Base(source)))
			if err != nil {
				return nil, err
			}
			effects = append(effects, moved...)
		}
	}
	if !matched && !optional {
		return nil, fmt.Errorf("no sources matched for moveMany to %s", destination)
	}
	return effects, nil
}

func (r *Runner) moveOneEffects(source, destination string, optional bool) ([]string, error) {
	src := r.path(source)
	if err := r.preflightSourceAncestors(src); err != nil {
		return nil, err
	}
	if !exists(src) {
		if optional {
			return nil, nil
		}
		return nil, fmt.Errorf("source does not exist: %s", src)
	}
	dst := r.path(destination)
	if info, err := os.Stat(dst); err == nil && info.IsDir() {
		dst = filepath.Join(dst, filepath.Base(src))
	}
	return r.movePathEffects(src, dst)
}

func (r *Runner) movePathEffects(src, dst string) ([]string, error) {
	files, err := migrationFiles(src)
	if err != nil {
		return nil, err
	}
	if err := r.movePath(src, dst); err != nil {
		return nil, err
	}
	effects := make([]string, 0, len(files)*2)
	for _, file := range files {
		rel, _ := filepath.Rel(src, file)
		effects = append(effects, file, filepath.Join(dst, rel))
	}
	return effects, nil
}

func migrationFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var files []string
	err = filepath.WalkDir(path, func(entry string, dir fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !dir.IsDir() {
			files = append(files, entry)
		}
		return nil
	})
	return files, err
}

func (r *Runner) movePath(src, dst string) error {
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN mv %s %s\n", src, dst)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func normalizedAbsolutePaths(paths []string) []string {
	sort.Strings(paths)
	out := paths[:0]
	var previous string
	for _, path := range paths {
		if path != previous {
			out = append(out, path)
			previous = path
		}
	}
	return out
}

func (r *Runner) shouldRun(when string) bool {
	when = strings.TrimSpace(when)
	if when == "" {
		return true
	}
	if strings.HasPrefix(when, "missing:") {
		return !exists(r.path(strings.TrimPrefix(when, "missing:")))
	}
	if strings.HasPrefix(when, "exists:") {
		return exists(r.path(strings.TrimPrefix(when, "exists:")))
	}
	return true
}

func (r *Runner) fetchTags(ctx context.Context, repository string) ([]string, error) {
	tags, err := r.github().ListTags(ctx, repository)
	if err == nil {
		return tags, nil
	}
	fmt.Fprintf(r.Opts.Stderr, "Native GitHub tag lookup failed, falling back to gh: %v\n", err)
	return r.fetchTagsFromShell(ctx, repository)
}

func (r *Runner) fetchTagsFromShell(ctx context.Context, repository string) ([]string, error) {
	out, err := r.output(ctx, r.ghPath(), "api", fmt.Sprintf("repos/%s/tags", repository), "--jq", ".[].name")
	if err != nil {
		return nil, err
	}
	var tags []string
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		tag := strings.TrimSpace(scanner.Text())
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags, scanner.Err()
}

func latestMatchingTag(tags []string, pattern *regexp.Regexp) string {
	var matches []string
	for _, tag := range tags {
		if pattern.MatchString(tag) {
			matches = append(matches, tag)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return compareSemverTag(matches[i], matches[j]) < 0
	})
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func currentMinorTagPattern(major, minor string) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(`^v?%s\.%s\.[0-9]+$`, regexp.QuoteMeta(major), regexp.QuoteMeta(minor)))
}

func currentMajorTagPattern(major string) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(`^v?%s\.[0-9]+\.[0-9]+$`, regexp.QuoteMeta(major)))
}

func parseMajorMinor(version string) (string, string, error) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(v, ".")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("cannot parse major/minor from version %q", version)
	}
	return parts[0], parts[1], nil
}

func compareSemverTag(a, b string) int {
	ap := semverInts(a)
	bp := semverInts(b)
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return strings.Compare(a, b)
}

func semverInts(tag string) [3]int {
	var out [3]int
	clean := strings.TrimPrefix(tag, "v")
	parts := strings.Split(clean, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		part := parts[i]
		for j, r := range part {
			if r < '0' || r > '9' {
				part = part[:j]
				break
			}
		}
		fmt.Sscanf(part, "%d", &out[i])
	}
	return out
}

func displayMigrationVersion(version string) string {
	v := normalizeVersion(version)
	if len(v) == 3 {
		return v[:1] + "." + v[1:]
	}
	return version
}

func (r *Runner) gitRemoteOwnerRepo(ctx context.Context) (string, string, error) {
	if org, repo, err := r.git().OriginOwnerRepo(ctx, r.Opts.WorkDir); err == nil {
		return org, repo, nil
	}
	out, err := r.output(ctx, r.gitPath(), "remote", "get-url", "origin")
	if err != nil {
		return "", "", err
	}
	return parseOwnerRepo(out)
}

// gitStageExact stages explicit additions/modifications/deletions without widening
// to parent directories that may contain implementation-owned work.
func (r *Runner) gitStageExact(ctx context.Context, paths ...string) error {
	if r.suppressStaging {
		return nil
	}
	if r.effects != nil {
		return r.effects.add(paths...)
	}
	paths, err := safeRelativePaths(paths)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	if !r.Opts.DryRun {
		paths, err = r.existingOrTrackedPaths(ctx, paths)
		if err != nil {
			return err
		}
	}
	return r.gitStagePaths(ctx, paths)
}

func (r *Runner) gitStagePaths(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"--literal-pathspecs", "add", "-A", "--"}, paths...)
	if err := r.run(ctx, r.gitPath(), args...); err != nil {
		return fmt.Errorf("stage exact paths %q: %w", paths, err)
	}
	return nil
}

func (r *Runner) existingOrTrackedPaths(ctx context.Context, paths []string) ([]string, error) {
	stagePaths := make([]string, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(r.path(path))
		switch {
		case err == nil:
			if info.IsDir() {
				return nil, fmt.Errorf("refusing to stage directory %q", path)
			}
			stagePaths = append(stagePaths, path)
		case !errors.Is(err, os.ErrNotExist):
			return nil, fmt.Errorf("stat staged path %q: %w", path, err)
		default:
			output, err := r.output(ctx, r.gitPath(), "--literal-pathspecs", "ls-files", "--", path)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(output) != "" {
				stagePaths = append(stagePaths, path)
			}
		}
	}
	return stagePaths, nil
}

func (r *Runner) gitAdd(ctx context.Context, paths ...string) error {
	if r.suppressStaging {
		return nil
	}
	if r.effects != nil {
		return r.effects.add(paths...)
	}
	paths, err := safeRelativePaths(paths)
	if err != nil {
		return err
	}
	if !r.Opts.DryRun {
		paths = existingRelativePaths(r.Opts.WorkDir, paths)
	}
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"--literal-pathspecs", "add", "--"}, paths...)
	return r.run(ctx, r.gitPath(), args...)
}

func relativePaths(workdir string, paths []string) []string {
	relative := make([]string, 0, len(paths))
	for _, path := range paths {
		rel, err := filepath.Rel(workdir, path)
		if err != nil {
			continue
		}
		relative = append(relative, filepath.ToSlash(rel))
	}
	return relative
}

func safeRelativePaths(paths []string) ([]string, error) {
	for _, raw := range paths {
		if raw != "" && strings.TrimSpace(raw) != raw {
			return nil, fmt.Errorf("invalid repository-relative path %q", raw)
		}
	}
	normalized := normalizedRelativePaths(paths)
	for _, path := range normalized {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		if filepath.IsAbs(path) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != path {
			return nil, fmt.Errorf("invalid repository-relative path %q", path)
		}
	}
	return normalized, nil
}

func normalizedRelativePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func existingRelativePaths(workdir string, paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		if exists(filepath.Join(workdir, filepath.FromSlash(p))) {
			out = append(out, p)
			seen[p] = struct{}{}
		}
	}
	return out
}

func (r *Runner) run(ctx context.Context, name string, args ...string) error {
	return r.runInDir(ctx, r.Opts.WorkDir, name, args...)
}

func (r *Runner) runInDir(ctx context.Context, dir, name string, args ...string) error {
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN (cd %s && %s %s)\n", dir, name, strings.Join(args, " "))
		return nil
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = r.Opts.Stdout
	cmd.Stderr = r.Opts.Stderr
	return cmd.Run()
}

func (r *Runner) output(ctx context.Context, name string, args ...string) (string, error) {
	return r.outputInDir(ctx, r.Opts.WorkDir, name, args...)
}

func (r *Runner) outputInDir(ctx context.Context, dir, name string, args ...string) (string, error) {
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN (cd %s && %s %s)\n", dir, name, strings.Join(args, " "))
		return "", nil
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return stdout.String(), nil
}

func (r *Runner) gitPath() string {
	if r.Opts.GitPath != "" {
		return r.Opts.GitPath
	}
	if env := os.Getenv("GIT"); env != "" {
		return env
	}
	if path, err := exec.LookPath("git"); err == nil {
		return path
	}
	return "git"
}

func (r *Runner) ghPath() string {
	if r.Opts.GHPath != "" {
		return r.Opts.GHPath
	}
	if env := os.Getenv("GH"); env != "" {
		return env
	}
	if path, err := exec.LookPath("gh"); err == nil {
		return path
	}
	return "gh"
}

func (r *Runner) path(parts ...string) string {
	out := r.Opts.WorkDir
	for _, part := range parts {
		if part == "" {
			continue
		}
		if filepath.IsAbs(part) {
			out = part
		} else {
			out = filepath.Join(out, filepath.FromSlash(part))
		}
	}
	return out
}

func (r *Runner) templatePath(parts ...string) string {
	root := r.templateCheckout
	if root == "" {
		root = r.path(r.Config.TemplateDirectory)
	}
	for _, part := range parts {
		if part != "" {
			root = filepath.Join(root, filepath.FromSlash(part))
		}
	}
	return root
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (r *Runner) mkdirAll(path string, mode os.FileMode) error {
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN mkdir -p %s\n", path)
		return nil
	}
	return os.MkdirAll(path, mode)
}

func (r *Runner) removeAll(path string) error {
	if !exists(path) {
		return nil
	}
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN rm -rf %s\n", path)
		return nil
	}
	return os.RemoveAll(path)
}

func (r *Runner) removeContents(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := r.removeAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// Template clones a named template when the target repository marker enables it.
func (r *Runner) Template(ctx context.Context, name string) error {
	tmpl, ok := r.Config.FindTemplate(name)
	if !ok {
		return fmt.Errorf("unknown template %q", name)
	}
	state, err := r.Detect()
	if err != nil {
		return err
	}
	for _, active := range state.Templates {
		if normalizeKey(active.Name) == normalizeKey(tmpl.Name) {
			return r.CloneTemplate(ctx, tmpl)
		}
	}
	fmt.Fprintf(r.Opts.Stdout, "%s template marker %s not found in %s; skipping\n", tmpl.Name, tmpl.Marker, state.BlueprintPath)
	return nil
}
