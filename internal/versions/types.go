// Package versions manages repository GitVersion workflow configuration.
package versions

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// WayOfWork identifies the repository branching/versioning workflow.
type WayOfWork string

const (
	WayOfWorkGitFlow    WayOfWork = "gitflow"
	WayOfWorkGitHubFlow WayOfWork = "githubflow"
	WayOfWorkTrunkBased WayOfWork = "trunkbased"
)

// ParseWayOfWork validates a workflow name. trunk is an alias for trunkbased.
func ParseWayOfWork(value string) (WayOfWork, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(WayOfWorkGitFlow):
		return WayOfWorkGitFlow, nil
	case string(WayOfWorkGitHubFlow):
		return WayOfWorkGitHubFlow, nil
	case string(WayOfWorkTrunkBased), "trunk":
		return WayOfWorkTrunkBased, nil
	default:
		return "", fmt.Errorf("unsupported way of work %q (expected gitflow, githubflow, or trunkbased)", value)
	}
}

// Options configures a Runner. SelectWayOfWork permits callers to replace the
// standard-library-only numeric selector (for tests or another UI).
type Options struct {
	WorkDir         string
	GitPath         string
	MainBranch      string
	DryRun          bool
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
	SelectWayOfWork func(io.Reader, io.Writer) (WayOfWork, error)
}

// InitOptions controls versions init.
type InitOptions struct {
	// WayOfWork is optional. An empty value uses configuration/default selection.
	WayOfWork WayOfWork
}

// InitResult records the safe, observable effects of Init.
type InitResult struct {
	WayOfWork      WayOfWork
	Changed        bool
	DevelopCreated bool
	Warnings       []string
}

// Runner performs versions operations in one repository.
type Runner struct {
	workDir    string
	gitPath    string
	mainBranch string
	dryRun     bool
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	selectWOW  func(io.Reader, io.Writer) (WayOfWork, error)
}

// NewRunner creates a configured versions runner.
func NewRunner(options Options) (*Runner, error) {
	workDir := strings.TrimSpace(options.WorkDir)
	if workDir == "" {
		workDir = "."
	}
	absolute, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	if info, err := os.Stat(absolute); err != nil {
		return nil, fmt.Errorf("stat workdir: %w", err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("workdir %s is not a directory", absolute)
	}
	gitPath := strings.TrimSpace(options.GitPath)
	mainBranch := strings.TrimSpace(options.MainBranch)
	if gitPath == "" {
		gitPath = "git"
	}
	stdin, stdout, stderr := options.Stdin, options.Stdout, options.Stderr
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	selector := options.SelectWayOfWork
	if selector == nil {
		selector = SelectWayOfWork
	}
	return &Runner{workDir: absolute, gitPath: gitPath, mainBranch: mainBranch, dryRun: options.DryRun, stdin: stdin, stdout: stdout, stderr: stderr, selectWOW: selector}, nil
}

func (w WayOfWork) selectorFileName() string { return "gitversion_" + string(w) + ".yaml" }
