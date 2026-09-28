// Package integration exercises the installed CLI as it is used from a
// GitHub Actions step. Tests use throw-away repositories and local remotes;
// they need neither GitHub credentials nor a GitHub API connection.
package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	root := repoRoot()
	binDir, err := os.MkdirTemp("", "tronador-cli-integration-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	bin := filepath.Join(binDir, "tronador")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build CLI: %v\n%s", err, output)
		os.Exit(1)
	}
	cliBinary = bin
	code := m.Run()
	_ = os.RemoveAll(binDir)
	os.Exit(code)
}

var cliBinary string

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(file))
}

func TestVersionsTagInGitHubActions(t *testing.T) {
	repo := newRepo(t, false)
	gitVersion := filepath.Join(t.TempDir(), "gitversion")
	write(t, gitVersion, "#!/bin/sh\necho v0.2.0\n")
	if err := os.Chmod(gitVersion, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Dir(gitVersion) + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", path)
	output := runCLI(t, repo, path, "versions", "--workdir", repo, "--main-branch", "main", "tag")
	if got := git(t, repo, "tag", "--list", "v0.2.0"); strings.TrimSpace(got) != "v0.2.0" {
		t.Fatalf("expected stub-calculated local tag v0.2.0 (command output %q), got %q", output, got)
	}
	if got := git(t, repo, "ls-remote", "--tags", "origin", "refs/tags/v0.2.0"); got != "" {
		t.Fatalf("tag command unexpectedly published tag: %s", got)
	}
}

func TestProjectVersionInGitHubActions(t *testing.T) {
	repo := newRepo(t, false)
	write(t, filepath.Join(repo, ".cloudopsworks", ".golang"), "managed\n")
	write(t, filepath.Join(repo, "VERSION"), "old\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "add Go project marker")
	tool := filepath.Join(t.TempDir(), "gitversion")
	write(t, tool, "#!/bin/sh\necho '{\"FullSemVer\":\"1.8.0-beta.1+69\"}'\n")
	if err := os.Chmod(tool, 0o755); err != nil {
		t.Fatal(err)
	}
	runCLI(t, repo, os.Getenv("PATH"), "project", "--workdir", repo, "--no-install-tools", "--tool-path", "gitversion="+tool, "version")
	if got, err := os.ReadFile(filepath.Join(repo, "VERSION")); err != nil || string(got) != "1.8.0-beta.1-69\n" {
		t.Fatalf("VERSION = %q, %v", got, err)
	}
}

func TestVersionsPurgesInGitHubActions(t *testing.T) {
	cases := []struct {
		name, branch, command string
		mergeDevelop          bool
	}{
		{"feature", "feature/gha-check", "feature", true},
		{"hotfix", "hotfix/v1.2.3", "hotfix", false},
		{"release", "release/v1.2.3", "release", true},
		{"support", "support/v1.2", "support", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo(t, tc.mergeDevelop)
			git(t, repo, "checkout", "-b", tc.branch)
			write(t, filepath.Join(repo, "change.txt"), tc.name+"\n")
			git(t, repo, "add", "change.txt")
			git(t, repo, "commit", "-m", "branch change")
			git(t, repo, "push", "-u", "origin", tc.branch)
			sha := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
			bases := []string{"main"}
			if tc.mergeDevelop {
				bases = append(bases, "develop")
			}
			for _, base := range bases {
				git(t, repo, "checkout", base)
				git(t, repo, "merge", "--no-ff", tc.branch, "-m", "merge "+tc.branch)
				git(t, repo, "push", "origin", base)
			}
			git(t, repo, "checkout", tc.branch)
			args := []string{"versions", "--workdir", repo, "--main-branch", "main", tc.command, "purge"}
			runCLI(t, repo, os.Getenv("PATH"), args...)
			expectedBase := "main"
			if tc.name == "feature" {
				expectedBase = "develop"
			}
			if got := strings.TrimSpace(git(t, repo, "branch", "--show-current")); got != expectedBase {
				t.Fatalf("purge left current branch %q, expected %s", got, expectedBase)
			}
			if got := git(t, repo, "branch", "--list", tc.branch); got != "" {
				t.Fatalf("local source branch remains: %s", got)
			}
			if got := git(t, repo, "ls-remote", "--heads", "origin", "refs/heads/"+tc.branch); got != "" {
				t.Fatalf("remote source branch remains: %s", got)
			}
			if got := strings.TrimSpace(git(t, repo, "rev-parse", sha)); got != sha {
				t.Fatalf("purged branch commit unexpectedly changed: %s", got)
			}
		})
	}
}

func newRepo(t *testing.T, develop bool) string {
	t.Helper()
	dir := t.TempDir()
	remote := filepath.Join(dir, "origin.git")
	run(t, dir, "git", "init", "--bare", remote)
	repo := filepath.Join(dir, "repo")
	run(t, dir, "git", "clone", remote, repo)
	git(t, repo, "config", "user.name", "Tronador Integration")
	git(t, repo, "config", "user.email", "tronador@example.invalid")
	git(t, repo, "checkout", "-b", "main")
	write(t, filepath.Join(repo, ".cloudopsworks", "gitversion.yaml"), "# Agents: WayOfWork=gitflow\nbranches: {}\n")
	write(t, filepath.Join(repo, "README.md"), "fixture\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "push", "-u", "origin", "main")
	if develop {
		git(t, repo, "checkout", "-b", "develop")
		git(t, repo, "push", "-u", "origin", "develop")
		git(t, repo, "checkout", "main")
	}
	return repo
}

func runCLI(t *testing.T, dir, path string, args ...string) string {
	t.Helper()
	cmd := exec.Command(cliBinary, args...)
	cmd.Dir = dir
	cmd.Env = envWithout(os.Environ(), "PATH")
	cmd.Env = append(cmd.Env, "PATH="+path, "GITHUB_ACTIONS=true", "GITHUB_WORKSPACE="+dir, "GITHUB_RUN_ID=123456", "CI=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tronador %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func envWithout(env []string, key string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, key+"=") {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return run(t, dir, "git", args...)
}
func run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if name == "git" {
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
