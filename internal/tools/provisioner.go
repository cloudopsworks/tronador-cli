package tools

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Options controls on-demand Tronador tool provisioning.
type Options struct {
	ToolsDir    string
	WorkDir     string
	ConfigPath  string
	SkipInstall bool
	DryRun      bool
	HTTPClient  *http.Client
	Stdout      io.Writer
	Stderr      io.Writer
}

// Tool describes a binary that can be resolved locally or provisioned on demand.
type Tool struct {
	Name           string
	Executable     string
	ConfiguredPath string
	Version        string
	Download       DownloadSpec
}

// DownloadSpec describes a direct downloadable upstream tool artifact.
type DownloadSpec struct {
	URLTemplate       string
	Format            string
	BinaryName        string
	ArchiveBinaryPath string
	ExpectedSHA256    string
	DefaultVersion    string
	VersionEnvNames   []string
}

// ResolvedTool describes how a tool was made available.
type ResolvedTool struct {
	Name      string
	Path      string
	Source    string
	Installed bool
}

// Provisioner resolves tools and installs only the requested missing binary into
// ~/.cloudopsworks/tronador by default.
type Provisioner struct {
	Opts   Options
	Config Config
}

// NewProvisioner normalizes tool provisioning options.
func NewProvisioner(opts Options) (*Provisioner, error) {
	if opts.ToolsDir == "" {
		opts.ToolsDir = os.Getenv("TRONADOR_TOOLS_DIR")
	}
	if opts.ToolsDir == "" {
		toolsDir, err := DefaultToolsDir()
		if err != nil {
			return nil, fmt.Errorf("resolve default tools dir: %w", err)
		}
		opts.ToolsDir = toolsDir
	} else {
		toolsDir, err := ExpandHomePath(opts.ToolsDir)
		if err != nil {
			return nil, fmt.Errorf("resolve tools dir: %w", err)
		}
		if !filepath.IsAbs(toolsDir) {
			toolsDir, err = filepath.Abs(toolsDir)
			if err != nil {
				return nil, fmt.Errorf("resolve tools dir: %w", err)
			}
		}
		opts.ToolsDir = toolsDir
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = firstSetEnv("TRONADOR_TOOLS_CONFIG", "TRONADOR_TOOLS_CONFIG_PATH")
	}
	if value := os.Getenv("TRONADOR_TOOLS_SKIP_INSTALL"); value != "" {
		skip, err := ParseBoolEnv("TRONADOR_TOOLS_SKIP_INSTALL", value)
		if err != nil {
			return nil, err
		}
		opts.SkipInstall = skip
	}
	if value := os.Getenv("TRONADOR_TOOLS_INSTALL"); value != "" {
		install, err := ParseBoolEnv("TRONADOR_TOOLS_INSTALL", value)
		if err != nil {
			return nil, err
		}
		opts.SkipInstall = !install
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 2 * time.Minute}
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	config, err := LoadConfig(opts.WorkDir, opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	return &Provisioner{Opts: opts, Config: config}, nil
}

// Ensure returns a usable executable path, installing the requested tool only
// when it is not already available and installation is enabled.
func (p *Provisioner) Ensure(ctx context.Context, tool Tool) (ResolvedTool, error) {
	tool = normalizeTool(tool)
	if tool.Download.URLTemplate == "" {
		configuredPath := tool.ConfiguredPath
		version := tool.Version
		if configTool, ok := p.Config.Tool(tool.Name); ok {
			tool = configTool.toTool(configuredPath, version)
		}
	}
	if tool.ConfiguredPath != "" {
		path, err := ResolveExecutable(tool.ConfiguredPath, tool.Executable)
		if err != nil {
			return ResolvedTool{}, err
		}
		return ResolvedTool{Name: tool.Name, Path: path, Source: "configured"}, nil
	}
	if path, err := exec.LookPath(tool.Executable); err == nil {
		return ResolvedTool{Name: tool.Name, Path: path, Source: "path"}, nil
	}

	toolPath := p.ToolPath(tool.Executable)
	if ExecutableExists(toolPath) {
		return ResolvedTool{Name: tool.Name, Path: toolPath, Source: "tools-dir"}, nil
	}
	if p.Opts.SkipInstall {
		return ResolvedTool{}, fmt.Errorf("%s executable not found; install it, pass an explicit path, or enable on-demand Tronador tool provisioning", tool.Executable)
	}
	path, err := p.Install(ctx, tool)
	if err != nil {
		return ResolvedTool{}, err
	}
	return ResolvedTool{Name: tool.Name, Path: path, Source: "download", Installed: !p.Opts.DryRun}, nil
}

// EnsureTool resolves or provisions the named configured tool.
func (p *Provisioner) EnsureTool(ctx context.Context, name, configuredPath, version string) (ResolvedTool, error) {
	configTool, ok := p.Config.Tool(name)
	if !ok {
		return ResolvedTool{}, fmt.Errorf("tool %s is not configured", name)
	}
	return p.Ensure(ctx, configTool.toTool(configuredPath, version))
}

// Install downloads and installs exactly one requested tool into the configured tools directory.
func (p *Provisioner) Install(ctx context.Context, tool Tool) (string, error) {
	tool = normalizeTool(tool)
	if tool.Download.URLTemplate == "" {
		return "", fmt.Errorf("%s has no download source configured", tool.Name)
	}
	version := resolveToolVersion(tool)
	if version == "" {
		return "", fmt.Errorf("%s version is required for on-demand provisioning", tool.Name)
	}
	toolPath := p.ToolPath(tool.Executable)
	url := expandURLTemplate(tool.Download.URLTemplate, tool, version)
	if p.Opts.DryRun {
		fmt.Fprintf(p.Opts.Stdout, "DRY-RUN: mkdir -p %s\n", p.Opts.ToolsDir)
		fmt.Fprintf(p.Opts.Stdout, "DRY-RUN: download %s %s from %s to %s\n", tool.Name, version, url, toolPath)
		return toolPath, nil
	}
	if err := os.MkdirAll(p.Opts.ToolsDir, 0o755); err != nil {
		return "", fmt.Errorf("create tools dir %s: %w", p.Opts.ToolsDir, err)
	}
	fmt.Fprintf(p.Opts.Stdout, "Installing %s %s into %s...\n", tool.Name, version, p.Opts.ToolsDir)
	data, err := p.download(ctx, url)
	if err != nil {
		return "", err
	}
	if tool.Download.ExpectedSHA256 != "" {
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if !strings.EqualFold(got, tool.Download.ExpectedSHA256) {
			return "", fmt.Errorf("%s checksum mismatch: got %s, want %s", tool.Name, got, tool.Download.ExpectedSHA256)
		}
	}
	binary, err := extractToolBinary(data, tool.Download, tool)
	if err != nil {
		return "", err
	}
	if err := writeExecutableAtomic(toolPath, binary); err != nil {
		return "", err
	}
	return toolPath, nil
}

// ToolPath returns the expected provisioned path for an executable.
func (p *Provisioner) ToolPath(executable string) string {
	return filepath.Join(p.Opts.ToolsDir, ExecutableName(executable))
}

func (p *Provisioner) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.Opts.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func normalizeTool(tool Tool) Tool {
	if tool.Name == "" {
		tool.Name = tool.Executable
	}
	if tool.Executable == "" {
		tool.Executable = tool.Name
	}
	if tool.Name == "" {
		tool.Name = tool.Executable
	}
	return tool
}

func resolveToolVersion(tool Tool) string {
	if tool.Version != "" {
		return tool.Version
	}
	for _, name := range tool.Download.VersionEnvNames {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return tool.Download.DefaultVersion
}

func expandURLTemplate(template string, tool Tool, version string) string {
	values := map[string]string{
		"{version}": version,
		"{os}":      platformOS(),
		"{arch}":    platformArch(),
		"{exe}":     ExecutableName(tool.Executable),
		"{ext}":     executableExt(),
	}
	out := template
	for old, newValue := range values {
		out = strings.ReplaceAll(out, old, newValue)
	}
	return out
}

func extractToolBinary(data []byte, spec DownloadSpec, tool Tool) ([]byte, error) {
	format := strings.ToLower(strings.TrimSpace(spec.Format))
	if format == "" || format == "binary" {
		return data, nil
	}
	binaryName := spec.BinaryName
	if binaryName == "" {
		binaryName = ExecutableName(tool.Executable)
	}
	switch format {
	case "tar.gz", "tgz":
		return extractTarGZ(data, binaryName, spec.ArchiveBinaryPath)
	case "zip":
		return extractZip(data, binaryName, spec.ArchiveBinaryPath)
	default:
		return nil, fmt.Errorf("unsupported download format %q for %s", spec.Format, tool.Name)
	}
}

func extractTarGZ(data []byte, binaryName, archivePath string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg && archivePathMatches(header.Name, binaryName, archivePath) {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("binary %s not found in tar.gz", binaryName)
}

func extractZip(data []byte, binaryName, archivePath string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, file := range reader.File {
		if !archivePathMatches(file.Name, binaryName, archivePath) {
			continue
		}
		r, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	return nil, fmt.Errorf("binary %s not found in zip", binaryName)
}

func archivePathMatches(name, binaryName, archivePath string) bool {
	name = strings.TrimPrefix(filepath.ToSlash(name), "./")
	if archivePath != "" {
		pattern := strings.TrimPrefix(filepath.ToSlash(archivePath), "./")
		if ok, _ := pathpkg.Match(pattern, name); ok {
			return true
		}
		return name == pattern
	}
	return filepath.Base(name) == binaryName
}

func writeExecutableAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-tool-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// DefaultToolsDir returns the default user-level Tronador tools directory.
func DefaultToolsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", errors.New("user home directory is empty")
	}
	return filepath.Join(home, ".cloudopsworks", "tronador"), nil
}

// ExpandHomePath expands ~/ paths.
func ExpandHomePath(value string) (string, error) {
	if value == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, value[2:]), nil
	}
	return value, nil
}

// ResolveExecutable resolves an explicit executable name/path.
func ResolveExecutable(configured, fallback string) (string, error) {
	name := configured
	if name == "" {
		name = fallback
	}
	if HasPathSeparator(name) || filepath.IsAbs(name) {
		if ExecutableExists(name) {
			return name, nil
		}
		return "", fmt.Errorf("%s executable not found at %s", fallback, name)
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s executable not found; install it or pass --%s", fallback, fallback)
	}
	return path, nil
}

// ExecutableName returns the platform-specific executable filename.
func ExecutableName(name string) string {
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		return name + ".exe"
	}
	return name
}

// HasPathSeparator reports whether value is path-like.
func HasPathSeparator(value string) bool {
	return strings.Contains(value, "/") || strings.Contains(value, `\`)
}

// ExecutableExists reports whether path points to an executable file.
func ExecutableExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

// Exists reports whether path exists.
func Exists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// EnvDefault returns the environment value or fallback.
func EnvDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// ParseBoolEnv parses common boolean environment variable values.
func ParseBoolEnv(name, value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean value, got %q", name, value)
	}
}

func platformOS() string {
	return strings.ToLower(runtime.GOOS)
}

func platformKey() string {
	return platformOS() + "/" + platformArch()
}

func platformArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "386":
		return "386"
	default:
		return runtime.GOARCH
	}
}

func executableExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
