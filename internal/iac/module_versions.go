package iac

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ModuleVersionsOptions controls the iac module workflow.
type ModuleVersionsOptions struct {
	WorkDir             string
	SearchPath          string
	Upgrade             bool
	Minor               bool
	Major               bool
	AllowAlpha          bool
	AllowBeta           bool
	FixPrefix           bool
	DryRun              bool
	ReportGitHubActions bool
	CommentPRNumber     string
	Stdout              io.Writer
	Stderr              io.Writer
	TagLister           TagLister
	Commenter           PRCommenter
}

// PRCommenter writes an external PR comment when explicitly requested.
type PRCommenter interface {
	CommentPR(ctx context.Context, workdir, prNumber, body string) error
}

// Runner executes IaC workflows.
type Runner struct {
	opts       ModuleVersionsOptions
	searchRoot string
}

// ModuleEntry describes a source line discovered in a terragrunt.hcl file.
type ModuleEntry struct {
	File       string
	LineNumber int
	Line       string
	Source     string
}

// SourceInfo is the parsed form of a Terraform/Terragrunt module source.
type SourceInfo struct {
	Raw             string
	Kind            string
	Repository      string
	Subdir          string
	Ref             string
	HasGitPrefix    bool
	PrefixFixNeeded bool
	Supported       bool
	UnsupportedWhy  string
}

type moduleResult struct {
	Entry                ModuleEntry
	Info                 SourceInfo
	Targets              versionTargets
	LatestTag            string
	SelectedTag          string
	HasCurrentSemver     bool
	HasEligibleSemverTag bool
	UpgradeNeeded        bool
	NewSource            string
	Status               string
	LookupErr            error
}

type versionTargets struct {
	Patch string
	Minor string
	Major string
}

func (t versionTargets) any() bool {
	return t.Patch != "" || t.Minor != "" || t.Major != ""
}

func (t versionTargets) selected(minor, major bool) string {
	switch {
	case major:
		return t.Major
	case minor:
		return t.Minor
	default:
		return t.Patch
	}
}

type semverTag struct {
	Tag                 string
	Major, Minor, Patch int
	Prerelease          []string
	Build               []string
}

const (
	kindSupported = "supported-github-git"
	kindRegistry  = "registry"
	kindLocal     = "local"
	kindSSH       = "ssh"
	kindOther     = "unsupported"
)

var (
	sourceLineRe = regexp.MustCompile(`^(\s*source\s*=\s*)(["'])([^"']+)(["'])(.*)$`)
	semverTagRe  = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
	refQueryRe   = regexp.MustCompile(`([?&]ref=)([^&]+)`)
)

// NewRunner prepares an IaC runner.
func NewRunner(opts ModuleVersionsOptions) (*Runner, error) {
	if opts.Minor && opts.Major {
		return nil, errors.New("--minor and --major are mutually exclusive")
	}
	if (opts.Minor || opts.Major) && !opts.Upgrade {
		return nil, errors.New("--minor and --major require --upgrade")
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	abs, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("inspect workdir %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workdir %s is not a directory", abs)
	}
	opts.WorkDir = abs
	searchRoot, err := resolveSearchRoot(opts.WorkDir, opts.SearchPath)
	if err != nil {
		return nil, err
	}
	searchInfo, err := os.Stat(searchRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect --path %s: %w", searchRoot, err)
	}
	if !searchInfo.IsDir() {
		return nil, fmt.Errorf("--path %s is not a directory", searchRoot)
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.TagLister == nil {
		opts.TagLister = NewGitHubTagLister()
	}
	if opts.Commenter == nil {
		opts.Commenter = ghPRCommenter{}
	}
	return &Runner{opts: opts, searchRoot: searchRoot}, nil
}

func resolveSearchRoot(workdir, searchPath string) (string, error) {
	if strings.TrimSpace(searchPath) == "" {
		return workdir, nil
	}
	root := searchPath
	if !filepath.IsAbs(root) {
		root = filepath.Join(workdir, root)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve --path: %w", err)
	}
	absRoot = filepath.Clean(absRoot)
	rel, err := filepath.Rel(workdir, absRoot)
	if err != nil {
		return "", fmt.Errorf("compare --path with --workdir: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("resolved --path %s is outside --workdir %s", absRoot, workdir)
	}
	return absRoot, nil
}

// ModuleVersions scans terragrunt.hcl source pins, reports available release-tier
// targets, and optionally rewrites an eligible ref or missing git:: prefix.
func (r *Runner) ModuleVersions(ctx context.Context) error {
	if err := r.requireIACWorkspace(); err != nil {
		return err
	}

	fmt.Fprintln(r.opts.Stdout, "🔍 Searching for terragrunt.hcl files...")
	files, err := r.findTerragruntFiles()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintln(r.opts.Stdout, "No terragrunt.hcl files found.")
		return nil
	}

	for _, file := range files {
		if err := r.processFile(ctx, file); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) requireIACWorkspace() error {
	marker := filepath.Join(r.opts.WorkDir, ".cloudopsworks", ".iac")
	info, err := os.Stat(marker)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("iac commands require %s marker in workdir %s", filepath.Join(".cloudopsworks", ".iac"), r.opts.WorkDir)
		}
		return fmt.Errorf("inspect iac marker %s: %w", marker, err)
	}
	if info.IsDir() {
		return fmt.Errorf("iac marker %s is a directory, want file", marker)
	}
	return nil
}

func (r *Runner) findTerragruntFiles() ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(r.searchRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".terraform", ".terragrunt-cache":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "terragrunt.hcl" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("find terragrunt.hcl files: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func (r *Runner) processFile(ctx context.Context, file string) error {
	rel := r.rel(file)
	fmt.Fprintf(r.opts.Stdout, "\n📄 Processing: %s\n", rel)

	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read %s: %w", rel, err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	entries := sourceEntries(file, lines)
	if len(entries) == 0 {
		fmt.Fprintf(r.opts.Stdout, "⚠️  No source found in %s\n", rel)
		return nil
	}

	changed := false
	for _, entry := range entries {
		result := r.processEntry(ctx, entry)
		r.printResult(ctx, result)
		if result.NewSource != "" && result.NewSource != entry.Source {
			updated, ok := replaceSourceInLine(entry.Line, result.NewSource)
			if !ok {
				return fmt.Errorf("replace source in %s:%d", rel, entry.LineNumber)
			}
			lines[entry.LineNumber-1] = updated
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if r.opts.DryRun {
		fmt.Fprintf(r.opts.Stdout, "🧪 Dry-run: would update %s\n", rel)
		return nil
	}
	if err := os.WriteFile(file, []byte(strings.Join(lines, "")), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	fmt.Fprintf(r.opts.Stdout, "✏️  Updated %s\n", rel)
	return nil
}

func sourceEntries(file string, lines []string) []ModuleEntry {
	entries := make([]ModuleEntry, 0)
	for idx, line := range lines {
		if source, ok := sourceFromLine(line); ok {
			entries = append(entries, ModuleEntry{
				File:       file,
				LineNumber: idx + 1,
				Line:       line,
				Source:     source,
			})
		}
	}
	return entries
}

func sourceFromLine(line string) (string, bool) {
	body, _ := splitLineEnding(line)
	matches := sourceLineRe.FindStringSubmatch(body)
	if matches == nil {
		return "", false
	}
	return matches[3], true
}

func replaceSourceInLine(line, source string) (string, bool) {
	body, ending := splitLineEnding(line)
	matches := sourceLineRe.FindStringSubmatchIndex(body)
	if matches == nil {
		return "", false
	}
	prefix := body[:matches[6]]
	suffix := body[matches[7]:]
	return prefix + source + suffix + ending, true
}

func splitLineEnding(line string) (body, ending string) {
	if strings.HasSuffix(line, "\r\n") {
		return strings.TrimSuffix(line, "\r\n"), "\r\n"
	}
	if strings.HasSuffix(line, "\n") {
		return strings.TrimSuffix(line, "\n"), "\n"
	}
	return line, ""
}

func (r *Runner) processEntry(ctx context.Context, entry ModuleEntry) moduleResult {
	info := ParseModuleSource(entry.Source)
	result := moduleResult{Entry: entry, Info: info, NewSource: entry.Source}
	if !info.Supported {
		result.Status = info.UnsupportedWhy
		return result
	}

	tags, err := r.opts.TagLister.ListTags(ctx, info.Repository)
	if err != nil {
		result.LookupErr = err
		result.Status = "tag-lookup-failed"
	} else {
		current, currentOK := parseSemverTag(info.Ref)
		result.HasCurrentSemver = currentOK
		if !currentOK {
			result.Status = "non-semver-current-ref"
		} else {
			result.Targets, result.LatestTag, result.HasEligibleSemverTag = findVersionTargets(current, tags, r.opts.AllowAlpha, r.opts.AllowBeta)
			result.SelectedTag = result.Targets.selected(r.opts.Minor, r.opts.Major)
			result.UpgradeNeeded = result.SelectedTag != ""
			switch {
			case !result.HasEligibleSemverTag:
				result.Status = "no-semver-tags"
			case result.UpgradeNeeded:
				result.Status = "outdated"
			case result.Targets.any():
				result.Status = "broader-updates-available"
			default:
				result.Status = "up-to-date"
			}
		}
	}

	if info.PrefixFixNeeded && result.Status == "up-to-date" {
		result.Status = "prefix-fix-available"
	} else if info.PrefixFixNeeded && result.UpgradeNeeded {
		result.Status = "outdated-prefix-fix-available"
	}

	if r.shouldFixPrefix(info) {
		result.NewSource = "git::" + result.NewSource
	}
	if r.shouldUpgrade(result) {
		result.NewSource = replaceRefValue(result.NewSource, result.SelectedTag)
	}
	return result
}

func (r *Runner) shouldFixPrefix(info SourceInfo) bool {
	return info.Supported && info.PrefixFixNeeded && (r.opts.Upgrade || r.opts.FixPrefix) && !r.opts.DryRun
}

func (r *Runner) shouldUpgrade(result moduleResult) bool {
	return result.Info.Supported && r.opts.Upgrade && result.UpgradeNeeded && result.SelectedTag != "" && !r.opts.DryRun
}

func (r *Runner) printResult(ctx context.Context, result moduleResult) {
	rel := r.rel(result.Entry.File)
	if !result.Info.Supported {
		fmt.Fprintf(r.opts.Stdout, "⚠️  Unsupported source in %s:%d (%s)\n", rel, result.Entry.LineNumber, result.Status)
		fmt.Fprintf(r.opts.Stdout, "    Found: %s\n", result.Entry.Source)
		return
	}

	fmt.Fprintf(r.opts.Stdout, "🔗 GitHub Repo: %s\n", result.Info.Repository)
	fmt.Fprintf(r.opts.Stdout, "📌 Current Ref: %s\n", result.Info.Ref)
	if result.Info.PrefixFixNeeded {
		fmt.Fprintln(r.opts.Stdout, "🛠️  Missing git:: prefix: fix available")
	}
	if result.LookupErr != nil {
		fmt.Fprintf(r.opts.Stdout, "❌ Failed to fetch tags for %s: %v\n", result.Info.Repository, result.LookupErr)
		return
	}
	if !result.HasCurrentSemver {
		fmt.Fprintf(r.opts.Stdout, "⚠️  Current ref %s is not a semantic version; no automatic upgrade target selected\n", result.Info.Ref)
		return
	}
	if !result.HasEligibleSemverTag {
		fmt.Fprintf(r.opts.Stdout, "⚠️  No semantic version tags found for %s\n", result.Info.Repository)
		return
	}
	printVersionTarget(r.opts.Stdout, "patch", result.Targets.Patch)
	printVersionTarget(r.opts.Stdout, "minor", result.Targets.Minor)
	printVersionTarget(r.opts.Stdout, "major", result.Targets.Major)
	if result.UpgradeNeeded {
		scope := selectedScope(r.opts.Minor, r.opts.Major)
		fmt.Fprintf(r.opts.Stdout, "🚨 Module in %s is outdated for %s upgrades:\n", rel, scope)
		fmt.Fprintf(r.opts.Stdout, "    Current: %s\n", result.Info.Ref)
		fmt.Fprintf(r.opts.Stdout, "    Selected: %s\n", result.SelectedTag)
		r.reportGitHubAction(ctx, result, "outdated")
	} else if result.Targets.any() {
		fmt.Fprintf(r.opts.Stdout, "ℹ️  Module in %s has no %s upgrade target; other release-tier targets are available.\n", rel, selectedScope(r.opts.Minor, r.opts.Major))
		r.reportGitHubAction(ctx, result, "updates-available")
	} else {
		fmt.Fprintf(r.opts.Stdout, "✅ Module in %s is up to date.\n", rel)
	}

	if result.NewSource != result.Entry.Source {
		action := "Updating"
		if r.opts.DryRun {
			action = "Would update"
		}
		fmt.Fprintf(r.opts.Stdout, "✏️  %s %s source\n", action, rel)
	} else if r.opts.DryRun && (r.opts.Upgrade || r.opts.FixPrefix) {
		if result.Info.PrefixFixNeeded || result.UpgradeNeeded {
			fmt.Fprintf(r.opts.Stdout, "🧪 Dry-run: would update %s source\n", rel)
		}
	}
}

func printVersionTarget(w io.Writer, scope, tag string) {
	if tag == "" {
		fmt.Fprintf(w, "    Next %s: none\n", scope)
		return
	}
	fmt.Fprintf(w, "    Next %s: %s\n", scope, tag)
}

func selectedScope(minor, major bool) string {
	switch {
	case major:
		return "major"
	case minor:
		return "minor"
	default:
		return "patch"
	}
}

func findVersionTargets(current semverTag, tags []string, allowAlpha, allowBeta bool) (versionTargets, string, bool) {
	var targets versionTargets
	latest := ""
	hasEligible := false
	for _, raw := range tags {
		candidate, ok := parseSemverTag(raw)
		if !ok || !isEligibleSemverTag(candidate, allowAlpha, allowBeta) {
			continue
		}
		hasEligible = true
		if compareSemverTags(candidate, current) <= 0 {
			continue
		}
		latest = higherSemverTag(latest, candidate)
		switch {
		case candidate.Major == current.Major && candidate.Minor == current.Minor:
			targets.Patch = higherSemverTag(targets.Patch, candidate)
		case candidate.Major == current.Major && candidate.Minor > current.Minor:
			targets.Minor = higherSemverTag(targets.Minor, candidate)
		case candidate.Major > current.Major:
			targets.Major = higherSemverTag(targets.Major, candidate)
		}
	}
	return targets, latest, hasEligible
}

func higherSemverTag(existing string, candidate semverTag) string {
	if existing == "" {
		return candidate.Tag
	}
	current, ok := parseSemverTag(existing)
	if !ok {
		return candidate.Tag
	}
	comparison := compareSemverTags(candidate, current)
	if comparison > 0 || (comparison == 0 && candidate.Tag > existing) {
		return candidate.Tag
	}
	return existing
}

func (r *Runner) reportGitHubAction(ctx context.Context, result moduleResult, reason string) {
	if !r.opts.ReportGitHubActions {
		return
	}
	rel := r.rel(result.Entry.File)
	body := fmt.Sprintf("🚨 Module in %s is %s: %s | %s | Current: %s | Latest: %s", rel, reason, rel, result.Info.Repository, result.Info.Ref, result.LatestTag)
	if result.SelectedTag != "" {
		body += fmt.Sprintf(" | Selected %s: %s", selectedScope(r.opts.Minor, r.opts.Major), result.SelectedTag)
	}
	fmt.Fprintf(r.opts.Stdout, "::warning:: %s\n", body)
	if r.opts.CommentPRNumber != "" {
		if err := r.opts.Commenter.CommentPR(ctx, r.opts.WorkDir, r.opts.CommentPRNumber, body); err != nil {
			fmt.Fprintf(r.opts.Stderr, "failed to comment on PR %s: %v\n", r.opts.CommentPRNumber, err)
		}
	}
}

func (r *Runner) rel(path string) string {
	rel, err := filepath.Rel(r.opts.WorkDir, path)
	if err != nil {
		return path
	}
	if rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

// ParseModuleSource classifies and extracts supported direct GitHub HTTPS module sources.
func ParseModuleSource(raw string) SourceInfo {
	trimmed := strings.TrimSpace(raw)
	info := SourceInfo{Raw: trimmed}
	withoutPrefix := trimmed
	if strings.HasPrefix(withoutPrefix, "git::") {
		info.HasGitPrefix = true
		withoutPrefix = strings.TrimPrefix(withoutPrefix, "git::")
	}
	if strings.HasPrefix(withoutPrefix, "http://") || strings.HasPrefix(withoutPrefix, "https://") {
		parsed, err := url.Parse(withoutPrefix)
		if err != nil {
			info.Kind = kindOther
			info.UnsupportedWhy = "unparseable-url"
			return info
		}
		if parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") {
			info.Kind = kindOther
			info.UnsupportedWhy = "unsupported-http-source"
			return info
		}
		return parseGitHubURLSource(trimmed, withoutPrefix, parsed, info)
	}
	info.Kind, info.UnsupportedWhy = classifyUnsupported(withoutPrefix)
	return info
}

func parseGitHubURLSource(raw, withoutPrefix string, parsed *url.URL, info SourceInfo) SourceInfo {
	path := strings.TrimPrefix(parsed.EscapedPath(), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		info.Kind = kindOther
		info.UnsupportedWhy = "unparseable-github-source"
		return info
	}
	owner := parts[0]
	repoSegment, err := url.PathUnescape(parts[1])
	if err != nil || repoSegment == "" {
		info.Kind = kindOther
		info.UnsupportedWhy = "unparseable-github-source"
		return info
	}
	repo := strings.TrimSuffix(repoSegment, ".git")
	if repo == "" || strings.Contains(repo, ":") {
		info.Kind = kindOther
		info.UnsupportedWhy = "unparseable-github-source"
		return info
	}
	ref := parsed.Query().Get("ref")
	if ref == "" || !refQueryRe.MatchString(withoutPrefix) {
		info.Kind = kindOther
		info.UnsupportedWhy = "missing-ref-pin"
		return info
	}
	prefixPath := "/" + owner + "/" + parts[1]
	subdir := strings.TrimPrefix(parsed.EscapedPath(), prefixPath)
	if subdir != "" && !strings.HasPrefix(subdir, "//") {
		info.Kind = kindOther
		info.UnsupportedWhy = "unsupported-github-path"
		return info
	}
	if decoded, err := url.PathUnescape(subdir); err == nil {
		subdir = decoded
	}
	info.Kind = kindSupported
	info.Repository = owner + "/" + repo
	info.Subdir = subdir
	info.Ref = ref
	info.Supported = true
	info.PrefixFixNeeded = !info.HasGitPrefix
	return info
}

func classifyUnsupported(source string) (kind, why string) {
	switch {
	case strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") || strings.HasPrefix(source, "/"):
		return kindLocal, "local-source"
	case strings.HasPrefix(source, "git@") || strings.HasPrefix(source, "ssh://") || strings.Contains(source, "@github.com:"):
		return kindSSH, "ssh-source"
	case looksLikeRegistryAddress(source):
		return kindRegistry, "registry-source"
	default:
		return kindOther, "unparseable-source"
	}
}

func looksLikeRegistryAddress(source string) bool {
	if strings.Contains(source, "://") || strings.Contains(source, "::") || strings.Contains(source, "?") {
		return false
	}
	parts := strings.Split(source, "/")
	return len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != ""
}

// LatestSemverTag returns the highest stable v?-prefixed x.y.z tag.
func LatestSemverTag(tags []string) string {
	return LatestSemverTagWithChannels(tags, false, false)
}

// LatestSemverTagWithChannels returns the highest eligible semantic-version tag.
// Stable tags are always eligible; alpha and beta prereleases are eligible only
// when their corresponding channel is enabled. Prereleases from other channels
// are never eligible.
func LatestSemverTagWithChannels(tags []string, allowAlpha, allowBeta bool) string {
	matches := make([]semverTag, 0, len(tags))
	for _, tag := range tags {
		parsed, ok := parseSemverTag(tag)
		if !ok || !isEligibleSemverTag(parsed, allowAlpha, allowBeta) {
			continue
		}
		matches = append(matches, parsed)
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool {
		if cmp := compareSemverTags(matches[i], matches[j]); cmp != 0 {
			return cmp < 0
		}
		return matches[i].Tag < matches[j].Tag
	})
	return matches[len(matches)-1].Tag
}

func parseSemverTag(tag string) (semverTag, bool) {
	parts := semverTagRe.FindStringSubmatch(tag)
	if parts == nil {
		return semverTag{}, false
	}
	major, errMajor := strconv.Atoi(parts[1])
	minor, errMinor := strconv.Atoi(parts[2])
	patch, errPatch := strconv.Atoi(parts[3])
	if errMajor != nil || errMinor != nil || errPatch != nil {
		return semverTag{}, false
	}
	parsed := semverTag{Tag: tag, Major: major, Minor: minor, Patch: patch}
	if parts[4] != "" {
		parsed.Prerelease = strings.Split(parts[4], ".")
	}
	if parts[5] != "" {
		parsed.Build = strings.Split(parts[5], ".")
	}
	return parsed, true
}

func isEligibleSemverTag(tag semverTag, allowAlpha, allowBeta bool) bool {
	if len(tag.Build) > 0 {
		return false
	}
	if len(tag.Prerelease) == 0 {
		return true
	}
	channel := tag.Prerelease[0]
	if (channel != "alpha" || !allowAlpha) && (channel != "beta" || !allowBeta) {
		return false
	}
	for _, identifier := range tag.Prerelease[1:] {
		if !isSemverNumericIdentifier(identifier) {
			return false
		}
	}
	return true
}

func isSemverNumericIdentifier(identifier string) bool {
	if identifier == "" || (len(identifier) > 1 && identifier[0] == '0') {
		return false
	}
	for _, char := range identifier {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func compareSemverTags(a, b semverTag) int {
	if a.Major != b.Major {
		return compareInts(a.Major, b.Major)
	}
	if a.Minor != b.Minor {
		return compareInts(a.Minor, b.Minor)
	}
	if a.Patch != b.Patch {
		return compareInts(a.Patch, b.Patch)
	}
	if len(a.Prerelease) == 0 && len(b.Prerelease) == 0 {
		return 0
	}
	if len(a.Prerelease) == 0 {
		return 1
	}
	if len(b.Prerelease) == 0 {
		return -1
	}
	for index := 0; index < len(a.Prerelease) && index < len(b.Prerelease); index++ {
		if cmp := compareSemverIdentifiers(a.Prerelease[index], b.Prerelease[index]); cmp != 0 {
			return cmp
		}
	}
	return compareInts(len(a.Prerelease), len(b.Prerelease))
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func compareSemverIdentifiers(a, b string) int {
	aNumeric := isSemverNumericIdentifier(a)
	bNumeric := isSemverNumericIdentifier(b)
	if aNumeric && bNumeric {
		if len(a) != len(b) {
			return compareInts(len(a), len(b))
		}
		return strings.Compare(a, b)
	}
	if aNumeric != bNumeric {
		if aNumeric {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

func replaceRefValue(source, ref string) string {
	return refQueryRe.ReplaceAllString(source, "${1}"+ref)
}

type ghPRCommenter struct{}

func (ghPRCommenter) CommentPR(ctx context.Context, workdir, prNumber, body string) error {
	cmd := exec.CommandContext(ctx, "gh", "pr", "comment", prNumber, "--body", body)
	cmd.Dir = workdir
	cmd.Stdout = io.Discard
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, message)
	}
	return nil
}
