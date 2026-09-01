package readme

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	yamlv3 "go.yaml.in/yaml/v3"
)

const maxReadmeYAMLSize = 4 << 20

// IncludeAction generates or refreshes one README include entry.
type IncludeAction func(context.Context, *Runner, string) error

// IncludeGate dispatches README include entries to exact registered actions and
// an optional fallback. Callers can provide a custom gate through Options or
// register additional entries on a gate before constructing a Runner.
type IncludeGate struct {
	actions  map[string]IncludeAction
	fallback IncludeAction
}

// NewIncludeGate creates an extensible include dispatcher.
func NewIncludeGate(fallback IncludeAction) *IncludeGate {
	return &IncludeGate{actions: make(map[string]IncludeAction), fallback: fallback}
}

// Register assigns an exact normalized include entry to an action.
func (g *IncludeGate) Register(entry string, action IncludeAction) {
	if g == nil || action == nil {
		return
	}
	entry = normalizeInclude(entry)
	if entry != "" {
		if g.actions == nil {
			g.actions = make(map[string]IncludeAction)
		}
		g.actions[entry] = action
	}
}

// Process dispatches include entries in source order, once per normalized entry.
func (g *IncludeGate) Process(ctx context.Context, runner *Runner, entries []string) error {
	if g == nil {
		return errors.New("README include gate is not configured")
	}
	type preparedInclude struct {
		entry string
		path  string
	}
	prepared := make([]preparedInclude, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		entry = normalizeInclude(entry)
		if entry == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		path, err := runner.includePath(entry)
		if err != nil {
			return err
		}
		prepared = append(prepared, preparedInclude{entry: entry, path: path})
	}

	seenDirs := make(map[string]bool, len(prepared))
	for _, include := range prepared {
		dir := filepath.Dir(include.path)
		if seenDirs[dir] {
			continue
		}
		seenDirs[dir] = true
		if runner.Opts.DryRun {
			fmt.Fprintf(runner.Opts.Stdout, "DRY-RUN: mkdir -p %s\n", dir)
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create README include directory %s: %w", dir, err)
		}
	}

	for _, include := range prepared {
		action := g.actions[include.entry]
		if action == nil {
			action = g.fallback
		}
		if action == nil {
			return fmt.Errorf("README include %q has no registered action", include.entry)
		}
		if err := action(ctx, runner, include.entry); err != nil {
			return fmt.Errorf("prepare README include %q: %w", include.entry, err)
		}
	}
	return nil
}

// DefaultIncludeGate returns the built-in registry. Callers may register more
// exact handlers on the returned gate before passing it through Options.
func DefaultIncludeGate() *IncludeGate {
	gate := NewIncludeGate(func(ctx context.Context, runner *Runner, entry string) error {
		return runner.runOptionalMakeInclude(ctx, entry)
	})
	gate.Register("docs/terraform.md", func(ctx context.Context, runner *Runner, entry string) error {
		isModule, err := runner.DetectTerraformModule()
		if err != nil {
			return err
		}
		if !isModule {
			return fmt.Errorf("%s is not a Terraform module", runner.Opts.WorkDir)
		}
		return runner.buildTerraformTo(ctx, entry, true)
	})
	gate.Register("docs/targets.md", func(ctx context.Context, runner *Runner, entry string) error {
		return runner.runRequiredMakeInclude(ctx, entry)
	})
	return gate
}

// ReadmeIncludes reads, normalizes, and deduplicates the top-level include list
// from README.yaml. Local paths and HTTP(S) README data sources are supported.
func (r *Runner) ReadmeIncludes(ctx context.Context) ([]string, error) {
	data, err := r.readReadmeYAML(ctx)
	if err != nil {
		return nil, err
	}
	var config struct {
		Include []string `yaml:"include"`
	}
	decoder := yamlv3.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("parse %s include entries: %w", r.Opts.ReadmeYAML, err)
	}
	entries := make([]string, 0, len(config.Include))
	seen := make(map[string]bool, len(config.Include))
	for _, entry := range config.Include {
		entry = normalizeInclude(entry)
		if entry == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		entries = append(entries, entry)
	}
	return entries, nil
}

func (r *Runner) readReadmeYAML(ctx context.Context) ([]byte, error) {
	if strings.HasPrefix(r.Opts.ReadmeYAML, "http://") || strings.HasPrefix(r.Opts.ReadmeYAML, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.Opts.ReadmeYAML, nil)
		if err != nil {
			return nil, fmt.Errorf("create README data request: %w", err)
		}
		client := &http.Client{Timeout: 2 * time.Minute}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("read README data %s: %w", r.Opts.ReadmeYAML, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("read README data %s: HTTP %s", r.Opts.ReadmeYAML, resp.Status)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxReadmeYAMLSize+1))
		if err != nil {
			return nil, fmt.Errorf("read README data %s: %w", r.Opts.ReadmeYAML, err)
		}
		if len(data) > maxReadmeYAMLSize {
			return nil, fmt.Errorf("README data %s exceeds %d bytes", r.Opts.ReadmeYAML, maxReadmeYAMLSize)
		}
		return data, nil
	}
	data, err := os.ReadFile(r.workPath(r.Opts.ReadmeYAML))
	if err != nil {
		return nil, fmt.Errorf("read README data %s: %w", r.Opts.ReadmeYAML, err)
	}
	if len(data) > maxReadmeYAMLSize {
		return nil, fmt.Errorf("README data %s exceeds %d bytes", r.Opts.ReadmeYAML, maxReadmeYAMLSize)
	}
	return data, nil
}

func (r *Runner) runRequiredMakeInclude(ctx context.Context, entry string) error {
	makePath, err := r.resolveMake()
	if err != nil {
		return err
	}
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN: %s %s\n", makePath, entry)
		return nil
	}
	cmd := exec.CommandContext(ctx, makePath, entry) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd.Dir = r.Opts.WorkDir
	cmd.Stdout = r.Opts.Stdout
	cmd.Stderr = r.Opts.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("run make target %s: %w", entry, err)
	}
	path, err := r.includePath(entry)
	if err != nil {
		return err
	}
	if regular, err := regularFile(path); err != nil {
		return err
	} else if !regular {
		return fmt.Errorf("make target %s did not generate %s", entry, path)
	}
	return nil
}

func (r *Runner) runOptionalMakeInclude(ctx context.Context, entry string) error {
	makePath, err := r.resolveMake()
	if err != nil {
		return r.writeUnsupportedInclude(entry, err)
	}
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN: %s %s (create a warning placeholder if unsupported)\n", makePath, entry)
		return nil
	}
	cmd := exec.CommandContext(ctx, makePath, entry) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd.Dir = r.Opts.WorkDir
	cmd.Stdout = r.Opts.Stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if stderr.Len() > 0 {
			_, _ = io.Copy(r.Opts.Stderr, &stderr)
		}
		return r.writeUnsupportedInclude(entry, fmt.Errorf("make target failed: %w", err))
	}
	path, err := r.includePath(entry)
	if err != nil {
		return err
	}
	if regular, err := regularFile(path); err != nil {
		return err
	} else if !regular {
		return r.writeUnsupportedInclude(entry, errors.New("make target completed without generating the include"))
	}
	return nil
}

func (r *Runner) writeUnsupportedInclude(entry string, cause error) error {
	warning := fmt.Sprintf("WARNING: README include %q is not supported yet: %v", entry, cause)
	fmt.Fprintln(r.Opts.Stderr, warning)
	if r.Opts.DryRun {
		return nil
	}
	path, err := r.includePath(entry)
	if err != nil {
		return err
	}
	comment := "<!-- " + strings.ReplaceAll(warning, "--", "—") + " -->\n"
	if err := os.WriteFile(path, []byte(comment), 0o644); err != nil {
		return fmt.Errorf("write unsupported README include placeholder %s: %w", path, err)
	}
	return nil
}

func (r *Runner) resolveMake() (string, error) {
	for _, candidate := range []string{r.Opts.MakePath, os.Getenv("SELF"), os.Getenv("MAKE"), "make", "gmake"} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if filepath.IsAbs(candidate) || strings.ContainsAny(candidate, `/\`) {
			if regular, err := regularFile(candidate); err == nil && regular {
				return candidate, nil
			}
			if candidate == r.Opts.MakePath {
				return "", fmt.Errorf("make executable not found at %s", candidate)
			}
			continue
		}
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved, nil
		}
		if candidate == r.Opts.MakePath {
			return "", fmt.Errorf("make executable %s not found", candidate)
		}
	}
	return "", errors.New("make executable not found; install make/gmake or pass --make")
}

func (r *Runner) includePath(entry string) (string, error) {
	entry = normalizeInclude(entry)
	path := filepath.Clean(filepath.FromSlash(entry))
	if entry == "" || path == "." || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("README include %q must be a relative path inside the work directory", entry)
	}
	return filepath.Join(r.Opts.WorkDir, path), nil
}

func normalizeInclude(entry string) string {
	return filepath.ToSlash(strings.TrimSpace(entry))
}
