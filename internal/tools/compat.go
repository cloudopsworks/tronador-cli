package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	versionProbeTimeout   = 10 * time.Second
	versionProbeMaxOutput = 64 * 1024
)

// defaultVersionPattern matches the first dotted version that is not glued to a
// word or another version component (so "go1.26" or "x1.2" never match).
var defaultVersionPattern = regexp.MustCompile(`(?:^|[^\w.])(v?\d+\.\d+(?:\.\d+)?)`)

var leadingVersionPattern = regexp.MustCompile(`^v?(\d+)`)

// ErrIncompatibleVersion is returned (wrapped) when only locally installed
// executables with an unsupported major version exist and installation is
// disabled.
var ErrIncompatibleVersion = errors.New("incompatible tool version")

// incompatibleTool records a locally available executable rejected because its
// version is outside the supported compatibility range.
type incompatibleTool struct {
	Path     string
	Detected string
}

// CompatibilityKey returns the major version of version, the unit Tronador
// uses to decide whether a locally installed tool is supported.
func CompatibilityKey(version string) (string, bool) {
	match := leadingVersionPattern.FindStringSubmatch(strings.TrimSpace(version))
	if match == nil {
		return "", false
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return "", false
	}
	return strconv.Itoa(major), true
}

// ExtractVersion returns the version reported in output. A custom pattern's
// first capture group (or whole match without groups) is used when provided.
func ExtractVersion(output, pattern string) (string, error) {
	re := defaultVersionPattern
	if pattern != "" {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("invalid version_regex %q: %w", pattern, err)
		}
		re = compiled
	}
	match := re.FindStringSubmatch(output)
	if match == nil {
		return "", errors.New("no version found in output")
	}
	version := match[0]
	if len(match) > 1 {
		version = match[1]
	}
	version = strings.TrimSpace(version)
	if _, ok := CompatibilityKey(version); !ok {
		return "", fmt.Errorf("unparseable version %q", version)
	}
	return version, nil
}

// fallbackVersionArgs is tried when a tool's declared probe fails, so a tool
// whose CLI syntax changed across majors (e.g. GitVersion 7 dropped "/version")
// still reports its version.
var fallbackVersionArgs = []string{"--version"}

// checkCompatibility reports whether the executable at path is usable as the
// supported version of tool. The declared probe runs first, then the generic
// fallback. Only a reported version with the supported major is compatible:
// every other major, and a tool whose version cannot be read at all, is
// rejected. The check is skipped only when the supported version itself has
// no parseable major (e.g. "latest").
func (p *Provisioner) checkCompatibility(ctx context.Context, tool Tool, path, desired string) (string, bool) {
	if p.Opts.SkipVersionCheck || tool.SkipVersionCheck || len(tool.VersionArgs) == 0 {
		return "", true
	}
	want, ok := CompatibilityKey(desired)
	if !ok {
		return "", true
	}
	probes := [][]string{tool.VersionArgs}
	if !slices.Equal(tool.VersionArgs, fallbackVersionArgs) {
		probes = append(probes, fallbackVersionArgs)
	}
	for _, args := range probes {
		detected, err := probeToolVersion(ctx, p.Opts.WorkDir, path, args, tool.VersionRegex)
		if err != nil {
			continue
		}
		got, _ := CompatibilityKey(detected)
		return detected, got == want
	}
	return "", false
}

// probeToolVersion runs one probe and extracts the reported version.
func probeToolVersion(ctx context.Context, dir, path string, args []string, pattern string) (string, error) {
	output, err := runVersionProbe(ctx, dir, path, args)
	if err != nil {
		return "", err
	}
	return ExtractVersion(output, pattern)
}

// runVersionProbe runs the probe in dir (the command's workdir) so version
// managers (tfenv, asdf, mise) report the version that would actually run.
func runVersionProbe(ctx context.Context, dir, path string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CHECKPOINT_DISABLE=1",    // terraform/tofu: no update check
		"GH_NO_UPDATE_NOTIFIER=1", // gh: no update check
		"NO_COLOR=1",
		"DOTNET_CLI_TELEMETRY_OPTOUT=1",
	)
	out := &limitedBuffer{limit: versionProbeMaxOutput}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("probe %s version: %w", path, err)
	}
	return out.String(), nil
}

func describeIncompatible(name, desired string, rejected []incompatibleTool) string {
	want, _ := CompatibilityKey(desired)
	parts := make([]string, 0, len(rejected))
	for _, r := range rejected {
		detected := r.Detected
		if detected == "" {
			detected = "(version unreadable)"
		}
		parts = append(parts, fmt.Sprintf("%s %s at %s", name, detected, r.Path))
	}
	return fmt.Sprintf("%s is incompatible with supported %s %s (%s.x)", strings.Join(parts, " and "), name, desired, want)
}

// limitedBuffer keeps at most limit bytes and silently discards the rest so a
// misbehaving probe cannot exhaust memory.
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if remaining := b.limit - b.buf.Len(); remaining > 0 {
		if len(data) > remaining {
			b.buf.Write(data[:remaining])
		} else {
			b.buf.Write(data)
		}
	}
	return len(data), nil
}

func (b *limitedBuffer) String() string { return b.buf.String() }

// ResolveTrusted resolves name for commands that have no network-consent flag
// (repos, iac, docs). It ignores project-scoped tools.json, never downloads in
// dry-run, and downloads the supported version only when the environment
// explicitly opts in (TRONADOR_TOOLS_INSTALL=true). An incompatible local copy
// is never used as a fallback; the caller gets an actionable error instead.
func ResolveTrusted(ctx context.Context, name, envOverride, workDir string, dryRun bool, stderr io.Writer) (string, error) {
	if stderr == nil {
		stderr = io.Discard
	}
	provisioner, err := NewProvisioner(Options{
		WorkDir:             workDir,
		SkipInstall:         true,
		IgnoreProjectConfig: true,
		Stdout:              stderr,
		Stderr:              stderr,
	})
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", name, err)
	}
	// Only an explicit TRONADOR_TOOLS_INSTALL=true opts in; environment
	// normalization (e.g. TRONADOR_TOOLS_SKIP_INSTALL=false) cannot.
	install, _ := ParseBoolEnv("TRONADOR_TOOLS_INSTALL", os.Getenv("TRONADOR_TOOLS_INSTALL"))
	provisioner.Opts.SkipInstall = !install || dryRun
	resolved, err := provisioner.EnsureTool(ctx, name, "", "")
	if err != nil {
		return "", fmt.Errorf("resolve %s (set %s=<path>, or TRONADOR_TOOLS_INSTALL=true to provision the supported version): %w", name, envOverride, err)
	}
	return resolved.Path, nil
}
