package repos

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
	Config       *Config
	Opts         Options
	gitClient    GitClient
	githubClient GitHubClient
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
		if exists(r.path(state.BlueprintPath, tmpl.Marker)) {
			state.Templates = append(state.Templates, tmpl)
		}
	}
	return state, nil
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

// TemplateInit mirrors make repos/template/init: clean .template and pull the active template.
func (r *Runner) TemplateInit(ctx context.Context) (Template, RepositoryState, error) {
	if err := r.CleanTemplate(ctx); err != nil {
		return Template{}, RepositoryState{}, err
	}
	tmpl, state, err := r.ActiveTemplate()
	if err != nil {
		return Template{}, state, err
	}
	return tmpl, state, r.CloneTemplate(ctx, tmpl)
}

// CloneTemplate clones a configured template into the configured template directory.
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
func (r *Runner) CleanTemplate(ctx context.Context) error {
	fmt.Fprintln(r.Opts.Stdout, "Cleaning up template repository")
	if err := r.removeAll(r.path(r.Config.TemplateDirectory)); err != nil {
		return err
	}
	if r.Opts.DryRun {
		return r.runIgnoreError(ctx, r.gitPath(), "remote", "remove", "template")
	}
	_ = r.git().RemoveRemote(ctx, r.Opts.WorkDir, "template")
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
func (r *Runner) Available(ctx context.Context) error {
	tmpl, state, err := r.TemplateInit(ctx)
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
func (r *Runner) Upgrade(ctx context.Context) error {
	tmpl, state, err := r.TemplateInit(ctx)
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
	pattern := currentMinorTagPattern(major, minor)
	tag := latestMatchingTag(tags, pattern)
	if tag == "" {
		return fmt.Errorf("no matching tags found for %s", tmpl.Repository)
	}
	fmt.Fprintf(r.Opts.Stdout, "Repo: %s\n", tmpl.Repository)
	fmt.Fprintf(r.Opts.Stdout, "Version: %s = %s.%s\n", state.Version, major, minor)
	fmt.Fprintf(r.Opts.Stdout, "Last Version: %s\n", tag)
	return r.Stack(ctx, StackOptions{Template: tmpl, State: state, PullBranch: tag})
}

// UpgradeVersion upgrades from a user-specified tag or branch.
func (r *Runner) UpgradeVersion(ctx context.Context, version string) error {
	tmpl, state, err := r.TemplateInit(ctx)
	if err != nil {
		return err
	}
	return r.Stack(ctx, StackOptions{Template: tmpl, State: state, PullBranch: version})
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
	if err := r.removeAll(r.path(r.Config.TemplateDirectory, ".git")); err != nil {
		return "", err
	}
	fmt.Fprintf(r.Opts.Stdout, "Template repository hash: %s\n", hash)
	return hash, nil
}

// EvalTemplateVersion reports whether the fetched template uses v5.10+ layout.
// It is an internal full-upgrade workflow step, not a public CLI subcommand.
func (r *Runner) EvalTemplateVersion() (string, error) {
	versionPath := r.path(r.Config.TemplateDirectory, ".cloudopsworks/_VERSION")
	version := ""
	if data, err := os.ReadFile(versionPath); err == nil {
		version = strings.TrimSpace(string(data))
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	fmt.Fprintf(r.Opts.Stdout, "Template version v5.10+ detected?: '%s'\n", version)
	return version, nil
}

// StackOptions carries state between upgrade target equivalents.
type StackOptions struct {
	Template     Template
	State        RepositoryState
	PullBranch   string
	TemplateHash string
	V510Plus     string
}

// Stack applies the fetched template into the target repository and commits it.
// It is an internal full-upgrade workflow step, not a public CLI subcommand.
func (r *Runner) Stack(ctx context.Context, opts StackOptions) error {
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
	if opts.V510Plus == "" {
		version, err := r.EvalTemplateVersion()
		if err != nil {
			return err
		}
		opts.V510Plus = version
	}

	if !opts.Template.Merge {
		fmt.Fprintln(r.Opts.Stdout, "No templates supported to upgrade by this script, skipping upgrade")
		return nil
	}

	fmt.Fprintf(r.Opts.Stdout, "Upgrading repository, current Version: %s from %s\n", opts.State.Version, opts.PullBranch)
	org, repo, err := r.gitRemoteOwnerRepo(ctx)
	if err == nil && org != "" && repo != "" {
		fmt.Fprintf(r.Opts.Stdout, "Setting default repository: %s/%s\n", org, repo)
		if err := r.setDefaultRepository(ctx, org, repo); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(r.Opts.Stderr, "Skipping gh repo set-default: %v\n", err)
	}
	fmt.Fprintln(r.Opts.Stdout, "Updating from template repository")
	if err := r.Clean(); err != nil {
		return err
	}

	originalPre510 := opts.State.Pre510
	if opts.Template.Versioned {
		if err := r.applyVersionedTemplate(opts.Template, opts.State, opts.V510Plus); err != nil {
			return err
		}
	} else {
		if err := r.applyUnversionedTemplate(); err != nil {
			return err
		}
	}

	updatedState := opts.State
	if opts.V510Plus != "" {
		if detected, err := r.Detect(); err == nil {
			updatedState = detected
		}
	}
	if opts.Template.Boilerplate {
		if err := r.applyBoilerplate(opts.Template, originalPre510); err != nil {
			return err
		}
	}
	if opts.Template.CICD {
		if err := r.CICDUpdate(updatedState, opts.TemplateHash); err != nil {
			return err
		}
	}
	if err := r.removeAll(r.path(r.Config.TemplateDirectory)); err != nil {
		return err
	}
	if err := r.Push(ctx, opts.Template, updatedState); err != nil {
		return err
	}
	fmt.Fprintln(r.Opts.Stdout, "Please review changes and push to remote repository")
	return nil
}

func (r *Runner) applyVersionedTemplate(tmpl Template, state RepositoryState, templateVersion string) error {
	if err := r.copyDir(r.path(r.Config.TemplateDirectory, ".github/workflows"), r.path(".github/workflows")); err != nil {
		return err
	}
	if err := r.copyIssueTemplatesIfExists(context.Background()); err != nil {
		return err
	}
	if err := r.copyPullRequestTemplateIfExists(context.Background()); err != nil {
		return err
	}
	for _, file := range []string{"Makefile", ".gitignore"} {
		if err := r.copyFile(r.path(r.Config.TemplateDirectory, file), r.path(file)); err != nil {
			return err
		}
	}
	for _, file := range []string{"AGENTS.md", "CLAUDE.md", "README-TEMPLATE.md", ".helmignore", ".dockerignore"} {
		if err := r.copyFileIfExists(r.path(r.Config.TemplateDirectory, file), r.path(file)); err != nil {
			return err
		}
	}

	if templateVersion != "" {
		fmt.Fprintln(r.Opts.Stdout, "Detected template version v5.10+")
		if err := r.mkdirAll(r.path(".cloudopsworks"), 0o755); err != nil {
			return err
		}
		if state.Pre510 {
			if err := r.Migrate(tmpl.Name, "510"); err != nil {
				return err
			}
		}
		if err := r.copyFile(r.path(r.Config.TemplateDirectory, ".cloudopsworks/_VERSION"), r.path(".cloudopsworks/_VERSION")); err != nil {
			return err
		}
		for _, dir := range []string{"hooks"} {
			if err := r.copyDirIfExists(r.path(r.Config.TemplateDirectory, ".cloudopsworks", dir), r.path(".cloudopsworks", dir)); err != nil {
				return err
			}
		}
		for _, file := range []string{"labeler.yml", "auto-label.yml", "auto-assign.yml", "gitversion.yaml", "gitversion_gitflow.yaml", "gitversion_githubflow.yaml"} {
			if err := r.copyFileIfExists(r.path(r.Config.TemplateDirectory, ".cloudopsworks", file), r.path(".cloudopsworks", file)); err != nil {
				return err
			}
		}
		for _, file := range []string{"inputs-jira.yaml", "inputs.yaml"} {
			dest := r.path(".cloudopsworks", file)
			if !exists(dest) {
				if err := r.copyFileIfExists(r.path(r.Config.TemplateDirectory, ".cloudopsworks", file), dest); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(r.Opts.Stdout, "Not modifying .cloudopsworks/%s\n", file)
			}
		}
		if err := r.copyFileIfExists(r.path(r.Config.TemplateDirectory, ".cloudopsworks/Makefile"), r.path(".cloudopsworks/Makefile")); err != nil {
			return err
		}
		if err := r.gitAdd(context.Background(), ".cloudopsworks"); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(r.Opts.Stdout, "Detected template version < v5.10")
		if err := r.copyFile(r.path(r.Config.TemplateDirectory, ".github/_VERSION"), r.path(".github/_VERSION")); err != nil {
			return err
		}
		for _, file := range []string{"labeler.yml", "Makefile"} {
			if err := r.copyFileIfExists(r.path(r.Config.TemplateDirectory, ".github", file), r.path(".github", file)); err != nil {
				return err
			}
		}
	}
	return r.gitAdd(context.Background(), ".gitignore", ".github/workflows")
}

func (r *Runner) applyUnversionedTemplate() error {
	if err := r.copyDir(r.path(r.Config.TemplateDirectory, ".github/workflows"), r.path(".github/workflows")); err != nil {
		return err
	}
	if err := r.copyIssueTemplatesIfExists(context.Background()); err != nil {
		return err
	}
	if err := r.copyPullRequestTemplateIfExists(context.Background()); err != nil {
		return err
	}
	if err := r.gitAdd(context.Background(), ".github/workflows"); err != nil {
		return err
	}
	if exists(r.path(".cloudopsworks")) {
		if err := r.gitAdd(context.Background(), ".cloudopsworks"); err != nil {
			return err
		}
	}
	return r.copyFile(r.path(r.Config.TemplateDirectory, "Makefile"), r.path("Makefile"))
}

func (r *Runner) copyIssueTemplatesIfExists(ctx context.Context) error {
	src := r.path(r.Config.TemplateDirectory, ".github/ISSUE_TEMPLATE")
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
	src := r.path(r.Config.TemplateDirectory, ".github/PULL_REQUEST_TEMPLATE.md")
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
	path := tmpl.BoilerplatePathV510Plus
	if pre510 || path == "" {
		path = tmpl.BoilerplatePathPre510
	}
	if path == "" {
		return nil
	}
	dest := r.path(path)
	if err := r.mkdirAll(dest, 0o755); err != nil {
		return err
	}
	if err := r.removeContents(dest); err != nil {
		return err
	}
	return r.copyDirContentsIfExists(r.path(r.Config.TemplateDirectory, path), dest)
}

// CICDUpdate updates the workflow-version-tag footer in cloudopsworks-ci.yaml.
func (r *Runner) CICDUpdate(state RepositoryState, templateHash string) error {
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
	versionData, err := os.ReadFile(r.path(state.BlueprintPath, "_VERSION"))
	if err != nil {
		return fmt.Errorf("read %s/_VERSION: %w", state.BlueprintPath, err)
	}
	version := strings.TrimSpace(string(versionData))
	re := regexp.MustCompile(`#workflow-version-tag.*`)
	content = re.ReplaceAllString(content, fmt.Sprintf("#workflow-version-tag: %s - hash: %s", version, templateHash))
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN write %s\n", path)
		return nil
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// Push stages and commits the upgrade result.
func (r *Runner) Push(ctx context.Context, tmpl Template, state RepositoryState) error {
	fmt.Fprintln(r.Opts.Stdout, "Committing changes")
	paths := []string{".github", "Makefile", filepath.ToSlash(filepath.Join(state.BlueprintPath, "_VERSION"))}
	if tmpl.CICD {
		paths = append(paths, filepath.ToSlash(filepath.Join(state.BlueprintPath, "Makefile")), filepath.ToSlash(filepath.Join(state.BlueprintPath, "cloudopsworks-ci.yaml")))
	}
	paths = existingRelativePaths(r.Opts.WorkDir, paths)
	if len(paths) > 0 {
		args := append([]string{"add", "-u"}, paths...)
		if err := r.run(ctx, r.gitPath(), args...); err != nil {
			return err
		}
	}
	if tmpl.Boilerplate {
		boilerplatePath := tmpl.BoilerplatePathV510Plus
		if state.Pre510 || boilerplatePath == "" {
			boilerplatePath = tmpl.BoilerplatePathPre510
		}
		if boilerplatePath != "" {
			if err := r.gitAdd(ctx, boilerplatePath); err != nil {
				return err
			}
		}
	}
	version := state.Version
	if data, err := os.ReadFile(r.path(state.VersionFile)); err == nil {
		version = strings.TrimSpace(string(data))
	}
	if err := r.run(ctx, r.gitPath(), "commit", "-am", fmt.Sprintf("chore: Upgrade repository from template, version: %s", version)); err != nil {
		return err
	}
	fmt.Fprintf(r.Opts.Stdout, "Repository upgraded to version: %s\n", version)
	return nil
}

// Recover overlays the fetched template checkout over the repository without committing.
func (r *Runner) Recover(ctx context.Context) error {
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
	if err == nil && org != "" && repo != "" {
		fmt.Fprintf(r.Opts.Stdout, "Setting default repository: %s/%s\n", org, repo)
		if err := r.setDefaultRepository(ctx, org, repo); err != nil {
			return err
		}
	}
	fmt.Fprintln(r.Opts.Stdout, "Fetching template repository")
	if err := r.Clean(); err != nil {
		return err
	}
	if err := r.copyDirContents(r.path(r.Config.TemplateDirectory), r.Opts.WorkDir); err != nil {
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
	if err := r.removeAll(r.path(r.Config.TemplateDirectory)); err != nil {
		return err
	}
	fmt.Fprintln(r.Opts.Stdout, "!!WARNING!! Some files may have been overwritten, please review changes and push to remote repository")
	return nil
}

// Migrate runs a configured migration for all templates or one template.
func (r *Runner) Migrate(templateName, version string) error {
	if version == "" {
		version = templateName
		templateName = ""
	}
	plan, ok := r.Config.FindMigrationPlan(version)
	if !ok {
		return fmt.Errorf("migration version %q is not configured", version)
	}
	if templateName == "" {
		fmt.Fprintf(r.Opts.Stdout, "Migrating Common repository structure to v%s+\n", displayMigrationVersion(plan.Version))
		return r.runOperations(plan.Common)
	}
	key := normalizeKey(templateName)
	if key == "terraform-module" {
		key = "terraform"
	}
	ops, ok := plan.Templates[key]
	if !ok {
		return fmt.Errorf("migration %s/%s is not configured", templateName, version)
	}
	if usesCommonMigration(key) {
		if err := r.Migrate("", version); err != nil {
			return err
		}
	}
	fmt.Fprintf(r.Opts.Stdout, "Migrating %s repository structure to v%s+\n", templateName, displayMigrationVersion(plan.Version))
	return r.runOperations(ops)
}

func usesCommonMigration(templateName string) bool {
	switch normalizeKey(templateName) {
	case "terragrunt", "terraform", "androidsdk", "flutter":
		return false
	default:
		return true
	}
}

func (r *Runner) runOperations(ops []Operation) error {
	for _, op := range ops {
		if !r.shouldRun(op.When) {
			continue
		}
		switch op.Action {
		case "ensureDir":
			if err := r.mkdirAll(r.path(op.Destination), 0o755); err != nil {
				return err
			}
		case "move":
			if err := r.moveOne(op.Source, op.Destination, op.Optional); err != nil {
				return err
			}
		case "moveMany":
			if err := r.moveMany(op.Sources, op.Destination, op.Optional); err != nil {
				return err
			}
		case "gitAdd":
			if err := r.gitAdd(context.Background(), op.Sources...); err != nil {
				return err
			}
		case "unsupported":
			return fmt.Errorf("not supported: %s", op.Message)
		default:
			return fmt.Errorf("unsupported migration action %q", op.Action)
		}
	}
	return nil
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

func (r *Runner) moveMany(patterns []string, destination string, optional bool) error {
	matched := false
	for _, pattern := range patterns {
		paths, err := filepath.Glob(r.path(pattern))
		if err != nil {
			return err
		}
		if len(paths) == 0 && exists(r.path(pattern)) {
			paths = []string{r.path(pattern)}
		}
		for _, source := range paths {
			matched = true
			if err := r.movePath(source, r.path(destination, filepath.Base(source))); err != nil {
				return err
			}
		}
	}
	if !matched && !optional {
		return fmt.Errorf("no sources matched for moveMany to %s", destination)
	}
	return nil
}

func (r *Runner) moveOne(source, destination string, optional bool) error {
	src := r.path(source)
	if !exists(src) {
		if optional {
			return nil
		}
		return fmt.Errorf("source does not exist: %s", source)
	}
	dst := r.path(destination)
	if info, err := os.Stat(dst); err == nil && info.IsDir() {
		dst = filepath.Join(dst, filepath.Base(src))
	}
	return r.movePath(src, dst)
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

func (r *Runner) gitAdd(ctx context.Context, paths ...string) error {
	paths = existingRelativePaths(r.Opts.WorkDir, paths)
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"add"}, paths...)
	return r.run(ctx, r.gitPath(), args...)
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

func (r *Runner) runIgnoreError(ctx context.Context, name string, args ...string) error {
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN %s %s\n", name, strings.Join(args, " "))
		return nil
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = r.Opts.WorkDir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
	return nil
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
