package versions

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	toolspkg "tronador-cli/internal/tools"
)

type operationToolEnsurer struct {
	calls []string
	errs  map[string]error
}

func (e *operationToolEnsurer) EnsureTool(_ context.Context, name, _, _ string) (toolspkg.ResolvedTool, error) {
	e.calls = append(e.calls, name)
	if err := e.errs[name]; err != nil {
		return toolspkg.ResolvedTool{}, err
	}
	return toolspkg.ResolvedTool{Name: name, Path: filepath.Join("/resolved", name)}, nil
}

type resolverAwareOperationRunner struct {
	delegate *fakeRunner
	resolver *commandToolResolver
	ensurer  *operationToolEnsurer
	actual   []call
}

func newResolverAwareOperationRunner(delegate *fakeRunner) *resolverAwareOperationRunner {
	ensurer := &operationToolEnsurer{}
	resolver := newCommandToolResolver(commandToolResolverOptions{
		newEnsurer: func() (toolEnsurer, error) { return ensurer, nil },
	})
	return &resolverAwareOperationRunner{delegate: delegate, resolver: resolver, ensurer: ensurer}
}

func newOperationWorkflow(t *testing.T, options WorkflowOptions) *Workflows {
	t.Helper()
	workflow, err := NewWorkflows(options)
	if err != nil {
		t.Fatal(err)
	}
	return workflow
}

func (r *resolverAwareOperationRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	original := name
	if provisionedVersionsTool(name) {
		resolved, err := r.resolver.resolve(ctx, name)
		if err != nil {
			return "", err
		}
		name = resolved
	}
	r.actual = append(r.actual, call{name: name, args: append([]string(nil), args...)})
	return r.delegate.Run(ctx, original, args...)
}

func (r *resolverAwareOperationRunner) resolvedCallCount(tool string) int {
	count := 0
	want := filepath.Join("/resolved", tool)
	for _, invocation := range r.actual {
		if invocation.name == want {
			count++
		}
	}
	return count
}

func TestOperationResolverDemandForCalculatedStarts(t *testing.T) {
	t.Run("hotfix", func(t *testing.T) {
		fake := &fakeRunner{replies: map[string]string{
			key("git", "branch", "--show-current"):                                             "support/v1.2.0\n",
			key("git", "rev-parse", "--verify", "refs/heads/support/v1.2.0^{commit}"):          "support-head\n",
			key("git", "rev-parse", "--verify", "refs/remotes/origin/support/v1.2.0^{commit}"): "support-head\n",
			gitVersionKey("MajorMinorPatch"):                                                   "1.2.3\n",
		}}
		runner := newResolverAwareOperationRunner(fake)
		workflow := newOperationWorkflow(t, WorkflowOptions{WayOfWork: "gitflow", MainBranch: "main", Runner: runner})
		if err := workflow.HotfixStart(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
		if got := runner.ensurer.calls; len(got) != 1 || got[0] != "gitversion" {
			t.Fatalf("resolved tools = %v; want gitversion once", got)
		}
	})

	t.Run("release", func(t *testing.T) {
		fake := &fakeRunner{replies: map[string]string{
			gitVersionKey("MajorMinorPatch"):                                  "1.2.3\n",
			key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"): "refs/remotes/origin/main\n",
		}}
		runner := newResolverAwareOperationRunner(fake)
		workflow := newOperationWorkflow(t, WorkflowOptions{WayOfWork: "githubflow", Runner: runner})
		if err := workflow.ReleaseStart(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := runner.ensurer.calls; len(got) != 1 || got[0] != "gitversion" {
			t.Fatalf("resolved tools = %v; want gitversion once", got)
		}
	})

	t.Run("explicit API hotfix version bypasses GitVersion", func(t *testing.T) {
		fake := &fakeRunner{replies: map[string]string{
			key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"): "refs/remotes/origin/main\n",
		}}
		runner := newResolverAwareOperationRunner(fake)
		workflow := newOperationWorkflow(t, WorkflowOptions{WayOfWork: "githubflow", Runner: runner})
		if err := workflow.HotfixStart(context.Background(), "1.2.3"); err != nil {
			t.Fatal(err)
		}
		if len(runner.ensurer.calls) != 0 {
			t.Fatalf("explicit-version hotfix resolved tools: %v", runner.ensurer.calls)
		}
	})
}

func TestOperationResolverDemandForPullRequestFinishes(t *testing.T) {
	t.Run("feature", func(t *testing.T) {
		fake := &fakeRunner{replies: map[string]string{
			key("git", "rev-parse", "--verify", "refs/heads/feature/a^{commit}"): "sha\n",
			key("git", "ls-remote", "origin", "refs/heads/feature/a"):            "sha\trefs/heads/feature/a\n",
			key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):    "refs/remotes/origin/main\n",
		}}
		runner := newResolverAwareOperationRunner(fake)
		workflow := newOperationWorkflow(t, WorkflowOptions{WayOfWork: "githubflow", Runner: runner})
		if err := workflow.FeatureFinish(context.Background(), "a"); err != nil {
			t.Fatal(err)
		}
		if got := runner.ensurer.calls; len(got) != 1 || got[0] != "gh" {
			t.Fatalf("resolved tools = %v; want gh once", got)
		}
	})

	t.Run("hotfix", func(t *testing.T) {
		branch := "hotfix/v1.2.3"
		fake := &fakeRunner{replies: map[string]string{
			key("git", "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}"): "sha\n",
			key("git", "ls-remote", "origin", "refs/heads/"+branch):               "sha\trefs/heads/" + branch + "\n",
			key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):     "refs/remotes/origin/main\n",
		}}
		runner := newResolverAwareOperationRunner(fake)
		workflow := newOperationWorkflow(t, WorkflowOptions{WayOfWork: "githubflow", Runner: runner})
		if err := workflow.HotfixFinish(context.Background(), "1.2.3", false); err != nil {
			t.Fatal(err)
		}
		if got := runner.ensurer.calls; len(got) != 1 || got[0] != "gh" {
			t.Fatalf("resolved tools = %v; want gh once", got)
		}
	})

	t.Run("GitFlow release reuses gh across list and create calls", func(t *testing.T) {
		branch := "release/v1.2.3"
		fake := &fakeRunner{replies: map[string]string{
			key("git", "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}"):                                                   "sha\n",
			key("git", "ls-remote", "origin", "refs/heads/"+branch):                                                                 "sha\trefs/heads/" + branch + "\n",
			key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):                                                       "refs/remotes/origin/main\n",
			key("gh", "pr", "list", "--head", branch, "--base", "main", "--state", "open", "--json", "number", "--jq", "length"):    "0\n",
			key("gh", "pr", "list", "--head", branch, "--base", "develop", "--state", "open", "--json", "number", "--jq", "length"): "0\n",
		}, errs: map[string]error{
			key("git", "merge-base", "--is-ancestor", "refs/heads/"+branch, "refs/remotes/origin/main"):    exitStatusOne(t),
			key("git", "merge-base", "--is-ancestor", "refs/heads/"+branch, "refs/remotes/origin/develop"): exitStatusOne(t),
		}}
		runner := newResolverAwareOperationRunner(fake)
		workflow := newOperationWorkflow(t, WorkflowOptions{WayOfWork: "gitflow", Runner: runner})
		if err := workflow.ReleaseFinish(context.Background(), "1.2.3", false); err != nil {
			t.Fatal(err)
		}
		if got := runner.ensurer.calls; len(got) != 1 || got[0] != "gh" {
			t.Fatalf("EnsureTool calls = %v; want one gh resolution", got)
		}
		if count := runner.resolvedCallCount("gh"); count != 4 {
			t.Fatalf("resolved gh invocations = %d; want two list and two create calls", count)
		}
	})
}

func TestOperationResolverNoGhExceptions(t *testing.T) {
	t.Run("fully contained release", func(t *testing.T) {
		branch := "release/v1.2.3"
		fake := &fakeRunner{replies: map[string]string{
			key("git", "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}"): "sha\n",
			key("git", "ls-remote", "origin", "refs/heads/"+branch):               "sha\trefs/heads/" + branch + "\n",
			key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):     "refs/remotes/origin/main\n",
		}}
		runner := newResolverAwareOperationRunner(fake)
		workflow := newOperationWorkflow(t, WorkflowOptions{WayOfWork: "gitflow", Runner: runner})
		if err := workflow.ReleaseFinish(context.Background(), "1.2.3", false); err != nil {
			t.Fatal(err)
		}
		if len(runner.ensurer.calls) != 0 {
			t.Fatalf("contained release resolved tools: %v", runner.ensurer.calls)
		}
	})

	for _, operation := range []string{"hotfix", "release"} {
		t.Run("local "+operation, func(t *testing.T) {
			journal := filepath.Join(t.TempDir(), "journal.json")
			fake := &fakeRunner{replies: map[string]string{
				key("git", "rev-parse", "--git-path", "tronador/versions-journal.json"): journal,
			}, errs: map[string]error{
				key("git", "fetch", "origin", "--prune", "--tags"): errors.New("stop fixture after local entry"),
			}}
			runner := newResolverAwareOperationRunner(fake)
			workflow := newOperationWorkflow(t, WorkflowOptions{Dir: t.TempDir(), WayOfWork: "githubflow", Runner: runner})
			var err error
			if operation == "hotfix" {
				err = workflow.HotfixFinish(context.Background(), "1.2.3", true)
			} else {
				err = workflow.ReleaseFinish(context.Background(), "1.2.3", true)
			}
			if err == nil || !strings.Contains(err.Error(), "stop fixture") {
				t.Fatalf("local finish error = %v", err)
			}
			if len(runner.ensurer.calls) != 0 {
				t.Fatalf("local finish resolved tools: %v", runner.ensurer.calls)
			}
		})
	}
}

func TestOperationResolverPropagatesGhExecutionFailure(t *testing.T) {
	branch := "feature/a"
	fake := &fakeRunner{replies: map[string]string{
		key("git", "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}"): "sha\n",
		key("git", "ls-remote", "origin", "refs/heads/"+branch):               "sha\trefs/heads/" + branch + "\n",
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):     "refs/remotes/origin/main\n",
	}, errs: map[string]error{
		key("gh", "pr", "create", "--head", branch, "-B", "main", "-b", `Feature "feature/a" finish, will merge into "main".`, "-t", "chore: Feature Finish from feature/a"): errors.New("gh auth required"),
	}}
	runner := newResolverAwareOperationRunner(fake)
	workflow := newOperationWorkflow(t, WorkflowOptions{WayOfWork: "githubflow", Runner: runner})
	err := workflow.FeatureFinish(context.Background(), "a")
	if err == nil || !strings.Contains(err.Error(), "gh auth required") {
		t.Fatalf("FeatureFinish error = %v", err)
	}
	if got := runner.ensurer.calls; len(got) != 1 || got[0] != "gh" {
		t.Fatalf("resolved tools = %v; want gh once", got)
	}
}
