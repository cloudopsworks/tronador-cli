package readme

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	toolspkg "tronador-cli/internal/tools"
)

const (
	DefaultAssetRepo     = "cloudopsworks/tronador"
	DefaultAssetRef      = "master"
	DefaultAssetBasePath = "templates"
)

// Options controls README generation and validation.
type Options struct {
	WorkDir             string
	ReadmeFile          string
	ReadmeYAML          string
	TemplateFile        string
	TemplateYAML        string
	IncludesURI         string
	GomplatePath        string
	GomplateVersion     string
	ToolsDir            string
	ToolsConfigPath     string
	SkipGomplateInstall bool
	AssetRepo           string
	AssetRef            string
	DryRun              bool
	Stdout              io.Writer
	Stderr              io.Writer
}

// Runner executes README commands.
type Runner struct {
	Opts Options
}

// AssetLocation describes where a README generator asset was resolved from.
type AssetLocation struct {
	Name     string
	Path     string
	Source   string
	Embedded bool
}

// NewRunner normalizes README command options.
func NewRunner(opts Options) (*Runner, error) {
	if opts.WorkDir == "" {
		opts.WorkDir = envDefault("TRONADOR_WORKDIR", ".")
	}
	abs, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	opts.WorkDir = abs
	if opts.ReadmeFile == "" {
		opts.ReadmeFile = envDefault("README_FILE", "README.md")
	}
	if opts.ReadmeYAML == "" {
		opts.ReadmeYAML = envDefault("README_YAML", "README.yaml")
	}
	if opts.TemplateFile == "" {
		opts.TemplateFile = os.Getenv("README_TEMPLATE_FILE")
	}
	if opts.TemplateYAML == "" {
		opts.TemplateYAML = os.Getenv("README_TEMPLATE_YAML")
	}
	if opts.IncludesURI == "" {
		opts.IncludesURI = envDefault("README_INCLUDES", defaultIncludesURI(opts.WorkDir))
	}
	if opts.GomplatePath == "" {
		opts.GomplatePath = os.Getenv("GOMPLATE")
	}
	if value := os.Getenv("TRONADOR_README_INSTALL_GOMPLATE"); value != "" {
		install, err := toolspkg.ParseBoolEnv("TRONADOR_README_INSTALL_GOMPLATE", value)
		if err != nil {
			return nil, err
		}
		opts.SkipGomplateInstall = !install
	}
	if value := os.Getenv("TRONADOR_README_SKIP_GOMPLATE_INSTALL"); value != "" {
		skip, err := toolspkg.ParseBoolEnv("TRONADOR_README_SKIP_GOMPLATE_INSTALL", value)
		if err != nil {
			return nil, err
		}
		opts.SkipGomplateInstall = skip
	}
	if opts.AssetRepo == "" {
		opts.AssetRepo = DefaultAssetRepo
	}
	if opts.AssetRef == "" {
		opts.AssetRef = DefaultAssetRef
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	return &Runner{Opts: opts}, nil
}

// Build generates README.md from README.yaml using gomplate.
func (r *Runner) Build(ctx context.Context) error {
	if err := r.requireReadmeYAML(); err != nil {
		return err
	}
	gomplate, err := r.ensureGomplate(ctx)
	if err != nil {
		return err
	}
	template, err := r.ResolveTemplate()
	if err != nil {
		return err
	}
	templatePath, cleanup, err := materializeAsset(template)
	if err != nil {
		return err
	}
	defer cleanup()

	args := []string{"--file", templatePath, "--out", r.Opts.ReadmeFile}
	env := append(os.Environ(),
		"README_YAML="+r.Opts.ReadmeYAML,
		"README_INCLUDES="+r.Opts.IncludesURI,
		"README_FILE="+r.Opts.ReadmeFile,
		"README_TEMPLATE_FILE="+templatePath,
	)
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN: %s %s\n", gomplate, strings.Join(args, " "))
		return nil
	}
	cmd := exec.CommandContext(ctx, gomplate, args...)
	cmd.Dir = r.Opts.WorkDir
	cmd.Env = env
	cmd.Stdout = r.Opts.Stdout
	cmd.Stderr = r.Opts.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run gomplate: %w", err)
	}
	fmt.Fprintf(r.Opts.Stdout, "Generated %s from %s using data from %s\n", r.Opts.ReadmeFile, template.DisplayPath(), r.Opts.ReadmeYAML)
	return nil
}

// Init creates README.yaml from the resolved template YAML when it is missing.
func (r *Runner) Init() error {
	dst := r.workPath(r.Opts.ReadmeYAML)
	if exists(dst) {
		fmt.Fprintf(r.Opts.Stdout, "%s already exists!\n", r.Opts.ReadmeYAML)
		return nil
	}
	asset, err := r.ResolveTemplateYAML()
	if err != nil {
		return err
	}
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN: create %s from %s\n", r.Opts.ReadmeYAML, asset.DisplayPath())
		return nil
	}
	data, err := asset.Read()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(r.Opts.Stdout, "%s created!\n", r.Opts.ReadmeYAML)
	return nil
}

// Lint verifies README.md is up to date with README.yaml.
func (r *Runner) Lint(ctx context.Context) error {
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN: build %s to a temporary file and compare with %s\n", r.Opts.ReadmeYAML, r.Opts.ReadmeFile)
		return nil
	}
	tmp, err := os.CreateTemp("", "tronador-readme-*.md")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpName)

	lintOpts := r.Opts
	lintOpts.ReadmeFile = tmpName
	lintOpts.Stdout = io.Discard
	lintRunner := &Runner{Opts: lintOpts}
	if err := lintRunner.Build(ctx); err != nil {
		return err
	}

	generated, err := os.ReadFile(tmpName)
	if err != nil {
		return err
	}
	currentPath := r.workPath(r.Opts.ReadmeFile)
	current, err := os.ReadFile(currentPath)
	if err != nil {
		return err
	}
	if bytes.Equal(generated, current) {
		return nil
	}
	fmt.Fprint(r.Opts.Stderr, simpleDiff(tmpName, currentPath, generated, current))
	return fmt.Errorf("%s is out of date; run tronador readme build", r.Opts.ReadmeFile)
}

// Deps ensures gomplate can be resolved or provisioned.
func (r *Runner) Deps(ctx context.Context) error {
	gomplate, err := r.ensureGomplate(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(r.Opts.Stdout, "gomplate: %s\n", gomplate)
	return nil
}

func (r *Runner) ensureGomplate(ctx context.Context) (string, error) {
	provisioner, err := toolspkg.NewProvisioner(toolspkg.Options{
		ToolsDir:    r.Opts.ToolsDir,
		WorkDir:     r.Opts.WorkDir,
		ConfigPath:  r.Opts.ToolsConfigPath,
		SkipInstall: r.Opts.SkipGomplateInstall,
		DryRun:      r.Opts.DryRun,
		Stdout:      r.Opts.Stdout,
		Stderr:      r.Opts.Stderr,
	})
	if err != nil {
		return "", err
	}
	resolved, err := provisioner.EnsureTool(ctx, "gomplate", r.Opts.GomplatePath, r.Opts.GomplateVersion)
	if err != nil {
		return "", err
	}
	return resolved.Path, nil
}

// ResolveTemplate resolves the README gomplate template using runtime-overridable precedence.
func (r *Runner) ResolveTemplate() (AssetLocation, error) {
	return r.resolveAsset("README.md.gotmpl", r.Opts.TemplateFile, "README_TEMPLATE_FILE", defaultTemplateAsset)
}

// ResolveTemplateYAML resolves the README.yaml initialization template using runtime-overridable precedence.
func (r *Runner) ResolveTemplateYAML() (AssetLocation, error) {
	return r.resolveAsset("README.yaml", r.Opts.TemplateYAML, "README_TEMPLATE_YAML", defaultYAMLAsset)
}

// AssetPaths returns the active template and YAML asset locations.
func (r *Runner) AssetPaths() ([]AssetLocation, error) {
	template, err := r.ResolveTemplate()
	if err != nil {
		return nil, err
	}
	yamlAsset, err := r.ResolveTemplateYAML()
	if err != nil {
		return nil, err
	}
	return []AssetLocation{template, yamlAsset}, nil
}

func (r *Runner) resolveAsset(name, explicit, envName, embeddedPath string) (AssetLocation, error) {
	if explicit != "" {
		p := r.workPath(explicit)
		if !exists(p) {
			return AssetLocation{}, fmt.Errorf("%s points to missing asset %s", envName, p)
		}
		return AssetLocation{Name: name, Path: p, Source: "explicit", Embedded: false}, nil
	}
	if env := os.Getenv(envName); env != "" {
		p := r.workPath(env)
		if !exists(p) {
			return AssetLocation{}, fmt.Errorf("%s points to missing asset %s", envName, p)
		}
		return AssetLocation{Name: name, Path: p, Source: "env:" + envName, Embedded: false}, nil
	}

	candidates := []struct {
		source string
		path   string
	}{
		{"project:.tronador", filepath.Join(r.Opts.WorkDir, ".tronador", "readme", name)},
		{"project:.cloudopsworks", filepath.Join(r.Opts.WorkDir, ".cloudopsworks", "readme", name)},
	}
	if configDir, err := os.UserConfigDir(); err == nil && configDir != "" {
		candidates = append(candidates, struct {
			source string
			path   string
		}{"user-config", filepath.Join(configDir, "tronador", "readme", name)})
	}
	if cacheRoot, err := CacheDir(r.Opts.AssetRepo, r.Opts.AssetRef); err == nil {
		candidates = append(candidates, struct {
			source string
			path   string
		}{"github-cache", filepath.Join(cacheRoot, name)})
	}
	for _, shared := range sharedAssetDirs() {
		candidates = append(candidates, struct {
			source string
			path   string
		}{"shared", filepath.Join(shared, name)})
	}
	for _, candidate := range candidates {
		if exists(candidate.path) {
			return AssetLocation{Name: name, Path: candidate.path, Source: candidate.source, Embedded: false}, nil
		}
	}
	return AssetLocation{Name: name, Path: embeddedPath, Source: "embedded", Embedded: true}, nil
}

func (r *Runner) requireReadmeYAML() error {
	if strings.HasPrefix(r.Opts.ReadmeYAML, "http://") || strings.HasPrefix(r.Opts.ReadmeYAML, "https://") {
		return nil
	}
	p := r.workPath(r.Opts.ReadmeYAML)
	if !exists(p) {
		return fmt.Errorf("README data file %s does not exist", p)
	}
	return nil
}

func (r *Runner) workPath(value string) string {
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return filepath.Join(r.Opts.WorkDir, value)
}

// Read reads an asset from disk or embedded fallback.
func (a AssetLocation) Read() ([]byte, error) {
	if a.Embedded {
		return defaultAssets.ReadFile(a.Path)
	}
	return os.ReadFile(a.Path)
}

// DisplayPath returns a human-friendly path for diagnostics.
func (a AssetLocation) DisplayPath() string {
	if a.Embedded {
		return "embedded:" + a.Path
	}
	return a.Path
}

func materializeAsset(asset AssetLocation) (string, func(), error) {
	if !asset.Embedded {
		return asset.Path, func() {}, nil
	}
	data, err := defaultAssets.ReadFile(asset.Path)
	if err != nil {
		return "", func() {}, err
	}
	dir, err := os.MkdirTemp("", "tronador-readme-assets-*")
	if err != nil {
		return "", func() {}, err
	}
	out := filepath.Join(dir, filepath.Base(asset.Path))
	if err := os.WriteFile(out, data, 0o644); err != nil {
		os.RemoveAll(dir)
		return "", func() {}, err
	}
	return out, func() { _ = os.RemoveAll(dir) }, nil
}

// InitAssets copies fallback assets into a runtime override location.
func InitAssets(workdir string, global, force, dryRun bool, stdout io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	var dest string
	if global {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		dest = filepath.Join(configDir, "tronador", "readme")
	} else {
		if workdir == "" {
			workdir = "."
		}
		abs, err := filepath.Abs(workdir)
		if err != nil {
			return err
		}
		dest = filepath.Join(abs, ".tronador", "readme")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, asset := range []struct{ embedded, name string }{{defaultTemplateAsset, "README.md.gotmpl"}, {defaultYAMLAsset, "README.yaml"}} {
		data, err := defaultAssets.ReadFile(asset.embedded)
		if err != nil {
			return err
		}
		out := filepath.Join(dest, asset.name)
		if exists(out) && !force {
			fmt.Fprintf(stdout, "%s already exists; not overwriting\n", out)
			continue
		}
		if dryRun {
			fmt.Fprintf(stdout, "DRY-RUN: write %s\n", out)
			continue
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Wrote %s\n", out)
	}
	return nil
}

// SyncOptions controls GitHub-backed asset sync.
type SyncOptions struct {
	WorkDir      string
	Repo         string
	Ref          string
	BasePath     string
	ManifestPath string
	Project      bool
	Force        bool
	DryRun       bool
	HTTPClient   *http.Client
	Stdout       io.Writer
}

// CacheEntry describes one synced GitHub cache entry.
type CacheEntry struct {
	Repo string
	Ref  string
	Path string
}

type assetManifest struct {
	Version       string          `json:"version,omitempty"`
	MinCliVersion string          `json:"minCliVersion,omitempty"`
	Assets        []manifestAsset `json:"assets"`
}

type manifestAsset struct {
	Name   string `json:"name"`
	Path   string `json:"path,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

// SyncAssets downloads README assets from GitHub raw content into cache and optionally project overrides.
func SyncAssets(ctx context.Context, opts SyncOptions) error {
	if opts.Repo == "" {
		opts.Repo = DefaultAssetRepo
	}
	if opts.Ref == "" {
		opts.Ref = DefaultAssetRef
	}
	if opts.BasePath == "" {
		opts.BasePath = DefaultAssetBasePath
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.DryRun {
		ref := opts.Ref
		if ref == "" {
			ref = DefaultAssetRef
		}
		fmt.Fprintf(opts.Stdout, "DRY-RUN: sync README assets from %s/%s/%s into cache", opts.Repo, ref, opts.BasePath)
		if opts.Project {
			fmt.Fprint(opts.Stdout, " and project .tronador/readme")
		}
		fmt.Fprintln(opts.Stdout)
		return nil
	}

	manifest, manifestSource, err := fetchManifestIfConfigured(ctx, opts)
	if err != nil {
		return err
	}
	if len(manifest.Assets) == 0 {
		manifest.Assets = []manifestAsset{
			{Name: "README.yaml", Path: path.Join(opts.BasePath, "README.yaml")},
			{Name: "README.md.gotmpl", Path: path.Join(opts.BasePath, "README.md.gotmpl")},
		}
	}
	cacheDir, err := CacheDir(opts.Repo, opts.Ref)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	for i, asset := range manifest.Assets {
		if asset.Name == "" {
			return errors.New("manifest asset name is required")
		}
		assetPath := asset.Path
		if assetPath == "" {
			assetPath = path.Join(opts.BasePath, asset.Name)
		}
		data, err := downloadGitHubRaw(ctx, opts.HTTPClient, opts.Repo, opts.Ref, assetPath)
		if err != nil {
			return err
		}
		sha := sha256Hex(data)
		if asset.SHA256 != "" && !strings.EqualFold(asset.SHA256, sha) {
			return fmt.Errorf("checksum mismatch for %s: got %s, want %s", asset.Name, sha, asset.SHA256)
		}
		manifest.Assets[i].SHA256 = sha
		if err := writeAsset(filepath.Join(cacheDir, asset.Name), data, opts.Force); err != nil {
			return err
		}
		fmt.Fprintf(opts.Stdout, "Synced %s from %s/%s/%s\n", asset.Name, opts.Repo, opts.Ref, assetPath)
	}
	manifest.Version = strings.TrimSpace(manifest.Version)
	if manifest.Version == "" {
		manifest.Version = "generated"
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "manifest.json"), append(manifestBytes, '\n'), 0o644); err != nil {
		return err
	}
	if opts.Project {
		projectDir, err := projectAssetDir(opts.WorkDir)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(projectDir, 0o755); err != nil {
			return err
		}
		for _, asset := range manifest.Assets {
			data, err := os.ReadFile(filepath.Join(cacheDir, asset.Name))
			if err != nil {
				return err
			}
			if err := writeAsset(filepath.Join(projectDir, asset.Name), data, opts.Force); err != nil {
				return err
			}
		}
		fmt.Fprintf(opts.Stdout, "Copied synced assets into %s\n", projectDir)
	}
	if manifestSource != "" {
		fmt.Fprintf(opts.Stdout, "Manifest: %s\n", manifestSource)
	} else {
		fmt.Fprintln(opts.Stdout, "Manifest: generated from default asset list")
	}
	return nil
}

// CacheDir returns the versioned GitHub asset cache directory.
func CacheDir(repo, ref string) (string, error) {
	if repo == "" {
		repo = DefaultAssetRepo
	}
	if ref == "" {
		ref = DefaultAssetRef
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "tronador", "readme", sanitizePathPart(repo), sanitizePathPart(ref)), nil
}

// ListCache returns known readme asset cache entries.
func ListCache() ([]CacheEntry, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(root, "tronador", "readme")
	var entries []CacheEntry
	repos, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, repoEntry := range repos {
		if !repoEntry.IsDir() {
			continue
		}
		refs, err := os.ReadDir(filepath.Join(base, repoEntry.Name()))
		if err != nil {
			return nil, err
		}
		for _, refEntry := range refs {
			if !refEntry.IsDir() {
				continue
			}
			entries = append(entries, CacheEntry{
				Repo: unsanitizePathPart(repoEntry.Name()),
				Ref:  unsanitizePathPart(refEntry.Name()),
				Path: filepath.Join(base, repoEntry.Name(), refEntry.Name()),
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// CleanCache removes all README asset cache entries.
func CleanCache() error {
	root, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(root, "tronador", "readme"))
}

func fetchManifestIfConfigured(ctx context.Context, opts SyncOptions) (assetManifest, string, error) {
	if opts.ManifestPath == "" {
		return assetManifest{}, "", nil
	}
	data, err := downloadGitHubRaw(ctx, opts.HTTPClient, opts.Repo, opts.Ref, opts.ManifestPath)
	if err != nil {
		return assetManifest{}, "", err
	}
	var manifest assetManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return assetManifest{}, "", fmt.Errorf("parse manifest %s: %w", opts.ManifestPath, err)
	}
	return manifest, opts.ManifestPath, nil
}

func downloadGitHubRaw(ctx context.Context, client *http.Client, repo, ref, assetPath string) ([]byte, error) {
	u := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", strings.Trim(repo, "/"), strings.Trim(ref, "/"), strings.TrimLeft(assetPath, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %s", u, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func writeAsset(path string, data []byte, force bool) error {
	if exists(path) && !force {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func projectAssetDir(workdir string) (string, error) {
	if workdir == "" {
		workdir = "."
	}
	abs, err := filepath.Abs(workdir)
	if err != nil {
		return "", err
	}
	return filepath.Join(abs, ".tronador", "readme"), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func envDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func defaultIncludesURI(workdir string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(workdir) + "/", RawQuery: "type=text/plain"}
	return u.String()
}

func simpleDiff(generatedPath, currentPath string, generated, current []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", generatedPath, currentPath)
	genLines := strings.Split(string(generated), "\n")
	curLines := strings.Split(string(current), "\n")
	max := len(genLines)
	if len(curLines) > max {
		max = len(curLines)
	}
	for i := 0; i < max; i++ {
		var g, c string
		if i < len(genLines) {
			g = genLines[i]
		}
		if i < len(curLines) {
			c = curLines[i]
		}
		if g != c {
			fmt.Fprintf(&b, "@@ line %d @@\n- %s\n+ %s\n", i+1, g, c)
			break
		}
	}
	return b.String()
}

func exists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func sharedAssetDirs() []string {
	if runtime.GOOS == "windows" {
		programData := os.Getenv("ProgramData")
		if programData == "" {
			return nil
		}
		return []string{filepath.Join(programData, "tronador", "readme")}
	}
	return []string{"/usr/local/share/tronador/readme", "/usr/share/tronador/readme"}
}

func sanitizePathPart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\\", "__")
	value = strings.ReplaceAll(value, "/", "__")
	value = strings.ReplaceAll(value, ":", "_")
	return value
}

func unsanitizePathPart(value string) string {
	return strings.ReplaceAll(value, "__", "/")
}
