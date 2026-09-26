// Package versions implements repository git-flow operations.  It deliberately
// uses git's argv interface instead of a shell, so branch names are never
// interpolated into commands.
package versions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type CommandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}
type ExecRunner struct{ Dir string }

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = r.Dir
	b, e := c.CombinedOutput()
	if e != nil {
		return string(b), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), e, strings.TrimSpace(string(b)))
	}
	return string(b), nil
}

// WorkflowOptions contains only repository workflow settings. CLI/init code
// translates user flags and configuration into this small stable contract.
type WorkflowOptions struct {
	Dir        string
	WayOfWork  string
	Remote     string
	MainBranch string
	Runner     CommandRunner
}
type Workflows struct {
	dir, wow, remote, main string
	run                    CommandRunner
}

func NewWorkflows(o WorkflowOptions) (*Workflows, error) {
	if o.Dir == "" {
		o.Dir = "."
	}
	if o.Remote == "" {
		o.Remote = "origin"
	}
	if o.WayOfWork == "" {
		o.WayOfWork = "gitflow"
	}
	switch normalizeWOW(o.WayOfWork) {
	case "gitflow", "githubflow", "trunkbased":
	default:
		return nil, fmt.Errorf("unsupported way of work %q", o.WayOfWork)
	}
	if o.MainBranch != "" && !safeRef(o.MainBranch) {
		return nil, fmt.Errorf("invalid main branch %q", o.MainBranch)
	}
	if o.Runner == nil {
		o.Runner = ExecRunner{Dir: o.Dir}
	}
	return &Workflows{dir: o.Dir, wow: normalizeWOW(o.WayOfWork), remote: o.Remote, main: o.MainBranch, run: o.Runner}, nil
}
func normalizeWOW(v string) string {
	v = strings.ToLower(strings.ReplaceAll(v, "-", ""))
	if v == "trunk" {
		return "trunkbased"
	}
	return v
}
func (w *Workflows) git(ctx context.Context, args ...string) (string, error) {
	return w.run.Run(ctx, "git", args...)
}
func (w *Workflows) gh(ctx context.Context, args ...string) (string, error) {
	return w.run.Run(ctx, "gh", args...)
}
func (w *Workflows) hasDevelop() bool { return w.wow == "gitflow" }
func (w *Workflows) Main(ctx context.Context) (string, error) {
	if w.main != "" {
		if err := w.ensureSafeRef(w.main); err != nil {
			return "", err
		}
		// A syntactically-valid override must still name a branch advertised by
		// the selected remote.  Without this guard a typo could create a PR or
		// direct a destructive workflow at an unrelated local ref.
		if _, err := w.git(ctx, "show-ref", "--verify", "--quiet", "refs/remotes/"+w.remote+"/"+w.main); err != nil {
			return "", fmt.Errorf("configured main branch %q is not available on %s: %w", w.main, w.remote, err)
		}
		return w.main, nil
	}
	out, err := w.git(ctx, "symbolic-ref", "--quiet", "--short", "refs/remotes/"+w.remote+"/HEAD")
	if err == nil {
		if b := strings.TrimPrefix(strings.TrimSpace(out), w.remote+"/"); b != "" {
			w.main = b
			return b, nil
		}
	}
	for _, b := range []string{"main", "master"} {
		if _, e := w.git(ctx, "show-ref", "--verify", "--quiet", "refs/remotes/"+w.remote+"/"+b); e == nil {
			w.main = b
			return b, nil
		}
	}
	return "", errors.New("cannot determine main branch; pass --main-branch")
}
func (w *Workflows) Current(ctx context.Context) (string, error) {
	o, e := w.git(ctx, "branch", "--show-current")
	if e != nil {
		return "", e
	}
	b := strings.TrimSpace(o)
	if b == "" {
		return "", errors.New("detached HEAD is not supported")
	}
	return b, nil
}
func (w *Workflows) RequireParity(ctx context.Context, branch string) error {
	_, err := w.remoteParitySHA(ctx, branch)
	return err
}

// remoteParitySHA proves the named local branch is exactly the commit
// published by the selected remote and returns that immutable expectation for
// a subsequent compare-and-swap delete.
func (w *Workflows) remoteParitySHA(ctx context.Context, branch string) (string, error) {
	if err := w.ensureSafeRef(branch); err != nil {
		return "", err
	}
	local, e := w.git(ctx, "rev-parse", "--verify", branch+"^{commit}")
	if e != nil {
		return "", fmt.Errorf("local branch %s is required for parity verification: %w", branch, e)
	}
	remote, e := w.git(ctx, "ls-remote", w.remote, "refs/heads/"+branch)
	if e != nil {
		return "", e
	}
	fields := strings.Fields(remote)
	if len(fields) == 0 || fields[0] != strings.TrimSpace(local) {
		return "", fmt.Errorf("%s is not exactly published at HEAD; publish it before continuing", branch)
	}
	return fields[0], nil
}
func (w *Workflows) branchExists(ctx context.Context, b string) bool {
	_, e := w.git(ctx, "show-ref", "--verify", "--quiet", "refs/heads/"+b)
	return e == nil
}
func (w *Workflows) checkoutBase(ctx context.Context, b string) error {
	if _, e := w.git(ctx, "fetch", w.remote, "--prune"); e != nil {
		return e
	}
	return w.checkoutFetchedBase(ctx, b)
}

// checkoutFetchedBase requires fetch to have completed in the current operation.
// It is used where a base must be selected from freshly fetched remote refs before
// checking it out, avoiding a second fetch between selection and synchronization.
func (w *Workflows) checkoutFetchedBase(ctx context.Context, b string) error {
	if w.branchExists(ctx, b) {
		if _, e := w.git(ctx, "checkout", b); e != nil {
			return e
		}
	} else {
		// A freshly cloned repository can see a support branch only as
		// origin/support/*; establish the local tracking base before syncing it.
		if _, e := w.git(ctx, "rev-parse", "--verify", "refs/remotes/"+w.remote+"/"+b+"^{commit}"); e != nil {
			return fmt.Errorf("remote base %s/%s is unavailable: %w", w.remote, b, e)
		}
		if _, e := w.git(ctx, "checkout", "--track", "-b", b, w.remote+"/"+b); e != nil {
			return e
		}
	}
	if _, e := w.git(ctx, "pull", "--ff-only", w.remote, b); e != nil {
		return e
	}
	local, e := w.git(ctx, "rev-parse", "--verify", b+"^{commit}")
	if e != nil {
		return fmt.Errorf("resolve local base %s after synchronization: %w", b, e)
	}
	remote, e := w.git(ctx, "rev-parse", "--verify", "refs/remotes/"+w.remote+"/"+b+"^{commit}")
	if e != nil {
		return fmt.Errorf("resolve remote base %s/%s after synchronization: %w", w.remote, b, e)
	}
	if strings.TrimSpace(local) != strings.TrimSpace(remote) {
		return fmt.Errorf("base branch %s is not exactly synchronized with %s/%s; publish or reset it before continuing", b, w.remote, b)
	}
	return nil
}
func branchValue(branch, prefix string) (string, bool) {
	for _, p := range []string{prefix + "/", shortPrefix(prefix) + "/"} {
		if strings.HasPrefix(branch, p) && len(branch) > len(p) {
			return strings.TrimPrefix(branch, p), true
		}
	}
	return "", false
}
func shortPrefix(p string) string {
	if p == "feature" {
		return "feat"
	}
	if p == "hotfix" {
		return "fix"
	}
	return p
}
func normalizeVersion(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

var semverRE = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$`)

func bump(v, kind string) (string, error) {
	m := semverRE.FindStringSubmatch(v)
	if m == nil {
		return "", fmt.Errorf("invalid semver %q", v)
	}
	var a, b, c int
	fmt.Sscanf(m[1], "%d", &a)
	fmt.Sscanf(m[2], "%d", &b)
	fmt.Sscanf(m[3], "%d", &c)
	switch kind {
	case "major":
		a++
		b = 0
		c = 0
	case "minor":
		b++
		c = 0
	case "patch", "":
		c++
	default:
		return "", fmt.Errorf("invalid bump %q", kind)
	}
	return fmt.Sprintf("v%d.%d.%d", a, b, c), nil
}
func (w *Workflows) gitPath(ctx context.Context, name string) (string, error) {
	o, e := w.git(ctx, "rev-parse", "--git-path", name)
	if e != nil {
		return "", e
	}
	p := strings.TrimSpace(o)
	if !filepath.IsAbs(p) {
		p = filepath.Join(w.dir, p)
	}
	return p, nil
}
func safeRef(name string) bool {
	return strings.TrimSpace(name) != "" && !strings.ContainsAny(name, " \t\n~^:?*[") && !strings.HasPrefix(name, "-")
}
func (w *Workflows) ensureSafeRef(name string) error {
	if !safeRef(name) {
		return fmt.Errorf("invalid branch or tag name %q", name)
	}
	return nil
}
func (w *Workflows) isDryRun() bool {
	r, ok := w.run.(runnerCommand)
	return ok && r.dryRun
}

// ensureAnnotatedTag creates tag at expected (normally HEAD or a finished
// target branch). An existing tag is reusable only when it resolves to exactly
// that commit; silently accepting a same-named tag on another commit could turn
// a finish into a destructive deletion of untagged work.
func (w *Workflows) ensureAnnotatedTag(ctx context.Context, tag, message, expected string) error {
	expectedCommit, err := w.git(ctx, "rev-parse", "--verify", expected+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve expected tag target %s: %w", expected, err)
	}
	expectedCommit = strings.TrimSpace(expectedCommit)
	// An existing lightweight tag is a valid already-created tag too.  The
	// important safety invariant is its target, not its object type: users may
	// use the legacy command solely to publish a tag created elsewhere.
	if tagCommit, resolveErr := w.git(ctx, "rev-parse", "--verify", tag+"^{commit}"); resolveErr == nil {
		if strings.TrimSpace(tagCommit) != expectedCommit {
			return fmt.Errorf("existing tag %s points to %s, not expected commit %s", tag, strings.TrimSpace(tagCommit), expectedCommit)
		}
		return nil
	}
	_, err = w.git(ctx, "tag", "-a", tag, expected, "-m", message)
	return err
}

func (w *Workflows) mergeContinue(ctx context.Context) error {
	p, e := w.gitPath(ctx, "MERGE_HEAD")
	if e == nil {
		if _, se := os.Stat(p); se == nil {
			_, e = w.git(ctx, "merge", "--continue")
			return e
		}
	}
	return nil
}

// Workflow returns the native repository workflow service for a selected WOW.
// It is intentionally separate from Init so callers can execute actions after
// configuration selection without re-reading mutable YAML.
func (r *Runner) Workflow(wow WayOfWork) (*Workflows, error) {
	return NewWorkflows(WorkflowOptions{Dir: r.workDir, WayOfWork: string(wow), MainBranch: r.mainBranch, Runner: runnerCommand{dir: r.workDir, gitPath: r.gitPath, dryRun: r.dryRun}})
}

type runnerCommand struct {
	dir, gitPath string
	dryRun       bool
}

func (r runnerCommand) Run(ctx context.Context, name string, args ...string) (string, error) {
	if r.dryRun && isWorkflowMutation(name, args) {
		return "", nil
	}
	if name == "git" {
		name = r.gitPath
	}
	return ExecRunner{Dir: r.dir}.Run(ctx, name, args...)
}

// isWorkflowMutation is intentionally conservative: dry-run never creates,
// deletes, checks out, fetches, merges, tags, pushes, or opens a PR.
func isWorkflowMutation(name string, args []string) bool {
	if name == "gh" {
		return len(args) >= 2 && args[0] == "pr" && args[1] == "create"
	}
	if name != "git" || len(args) == 0 {
		return false
	}
	switch args[0] {
	case "checkout", "switch", "fetch", "pull", "push", "merge", "tag":
		return true
	case "branch":
		for _, arg := range args[1:] {
			if arg == "-d" || arg == "-D" || arg == "-m" {
				return true
			}
		}
	}
	return false
}

// CurrentWayOfWork reads the installed GitVersion config header. Missing or
// legacy headers deliberately retain the historical GitFlow default.
func (r *Runner) CurrentWayOfWork() (WayOfWork, error) {
	data, err := os.ReadFile(filepath.Join(r.workDir, cloudOpsWorksDir, "gitversion.yaml"))
	if err != nil {
		return "", fmt.Errorf("read current gitversion config: %w", err)
	}
	return currentWayOfWork(data), nil
}

func (w *Workflows) deleteLocalBranch(ctx context.Context, branch string) error {
	if !w.branchExists(ctx, branch) {
		return nil
	}
	_, err := w.git(ctx, "branch", "-d", branch)
	return err
}

func (w *Workflows) deleteRemoteBranch(ctx context.Context, branch, expectedSHA string) error {
	if err := w.ensureSafeRef(branch); err != nil {
		return err
	}
	out, err := w.git(ctx, "ls-remote", w.remote, "refs/heads/"+branch)
	if err != nil {
		return err
	}
	if len(strings.Fields(out)) == 0 {
		return nil
	}
	if expectedSHA == "" {
		return fmt.Errorf("cannot safely delete remote branch %s without its expected SHA", branch)
	}
	_, err = w.git(ctx, "push", "--force-with-lease=refs/heads/"+branch+":"+expectedSHA, w.remote, ":refs/heads/"+branch)
	return err
}
