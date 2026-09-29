package versions

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	toolspkg "tronador-cli/internal/tools"
)

type toolEnsurer interface {
	EnsureTool(context.Context, string, string, string) (toolspkg.ResolvedTool, error)
}

type commandToolResolverOptions struct {
	workDir        string
	toolsDir       string
	toolsConfig    string
	noInstallTools bool
	allowNetwork   bool
	versions       map[string]string
	paths          map[string]string
	stderr         io.Writer
	newEnsurer     func() (toolEnsurer, error)
}

// commandToolResolver is scoped to one Runner.Workflow call. It constructs the
// shared provisioner only when GitVersion or gh is actually invoked and caches
// successful resolutions for later invocations in the same workflow.
type commandToolResolver struct {
	opts     commandToolResolverOptions
	ensurer  toolEnsurer
	resolved map[string]string
}

func newCommandToolResolver(opts commandToolResolverOptions) *commandToolResolver {
	return &commandToolResolver{opts: opts, resolved: make(map[string]string)}
}

func provisionedVersionsTool(name string) bool {
	return name == "gitversion" || name == "gh"
}

func (r *commandToolResolver) resolve(ctx context.Context, name string) (string, error) {
	if path := r.resolved[name]; path != "" {
		return path, nil
	}
	if !provisionedVersionsTool(name) {
		return name, nil
	}
	if r.ensurer == nil {
		factory := r.opts.newEnsurer
		if factory == nil {
			factory = r.newProvisioner
		}
		ensurer, err := factory()
		if err != nil {
			return "", fmt.Errorf("resolve %s tool: %w", name, err)
		}
		r.ensurer = ensurer
	}

	configuredPath := strings.TrimSpace(r.opts.paths[name])
	expandedPath, err := toolspkg.ExpandHomePath(configuredPath)
	if err != nil {
		return "", fmt.Errorf("resolve %s explicit tool path %q: %w", name, configuredPath, err)
	}
	configuredPath = expandedPath
	tool, err := r.ensurer.EnsureTool(ctx, name, configuredPath, strings.TrimSpace(r.opts.versions[name]))
	if err != nil {
		hint := "pass --tool-path " + name + "=<path> or use --tools-dir"
		if r.opts.noInstallTools {
			hint += "; remove --no-install-tools and pass --allow-network to permit provisioning"
		} else if !r.opts.allowNetwork {
			hint += "; pass --allow-network to permit provisioning"
		}
		return "", fmt.Errorf("resolve %s tool (%s): %w", name, hint, err)
	}
	path := tool.Path
	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve absolute %s tool path %q: %w", name, tool.Path, err)
		}
	}
	r.resolved[name] = path
	return path, nil
}

func (r *commandToolResolver) newProvisioner() (toolEnsurer, error) {
	skipInstall := r.opts.noInstallTools || !r.opts.allowNetwork
	p, err := toolspkg.NewProvisioner(toolspkg.Options{
		ToolsDir:    r.opts.toolsDir,
		WorkDir:     r.opts.workDir,
		ConfigPath:  r.opts.toolsConfig,
		SkipInstall: skipInstall,
		Stdout:      r.opts.stderr,
		Stderr:      r.opts.stderr,
	})
	if err != nil {
		return nil, err
	}
	// Environment normalization may otherwise turn installation back on. CLI
	// policy is a hard veto: both network permission and installation permission
	// are required before the provisioner may download anything.
	p.Opts.SkipInstall = p.Opts.SkipInstall || skipInstall
	return p, nil
}
