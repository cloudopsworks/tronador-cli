package versions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// atomicFinishFixture creates the exact locally-finished state immediately
// before publish-and-delete-remote. Remote targets remain at their old tips;
// local targets and the annotated tag are the planned transaction payload.
type atomicFinishFixture struct {
	root, repo        string
	w                 *Workflows
	j                 *journal
	path, source, tag string
	targets           []string
}

func newAtomicFinishFixture(t *testing.T, gitflow bool) *atomicFinishFixture {
	t.Helper()
	ctx := context.Background()
	root, repo := setupWorkflowRemote(t, gitflow)
	targets := []string{"main"}
	base := "main"
	source := "hotfix/v1.2.3"
	if gitflow {
		targets, base, source = []string{"main", "develop"}, "develop", "release/v1.2.3"
	}
	gitTest(t, repo, "checkout", "-b", source, base)
	gitTest(t, repo, "commit", "--allow-empty", "-m", "finished source")
	gitTest(t, repo, "push", "-u", "origin", source)
	sourceSHA := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	for _, target := range targets {
		gitTest(t, repo, "checkout", "--no-guess", target)
		gitTest(t, repo, "merge", "--no-ff", sourceSHA, "-m", "finish into "+target)
	}
	// The finish tag is deliberately on main even when develop is also a
	// transaction target, matching ReleaseFinish's documented topology.
	gitTest(t, repo, "checkout", "--no-guess", "main")
	gitTest(t, repo, "tag", "-a", "v1.2.3", "-m", "finish")

	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: map[bool]string{true: "gitflow", false: "githubflow"}[gitflow], MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	j := &journal{Source: source, SourceSHA: sourceSHA, TagTargetSHA: strings.TrimSpace(gitTest(t, repo, "rev-parse", "main"))}
	path := filepath.Join(root, "journal.json")
	if err := w.prepareFinishedRemotePlan(ctx, path, j, targets, "v1.2.3"); err != nil {
		t.Fatalf("prepare publication plan: %v", err)
	}
	return &atomicFinishFixture{root: root, repo: repo, w: w, j: j, path: path, source: source, tag: "v1.2.3", targets: targets}
}

func (f *atomicFinishFixture) remoteRef(t *testing.T, ref string) string {
	t.Helper()
	return strings.TrimSpace(gitTest(t, f.repo, "ls-remote", "origin", ref))
}

func (f *atomicFinishFixture) assertSourcePresent(t *testing.T) {
	t.Helper()
	if got := f.remoteRef(t, "refs/heads/"+f.source); got == "" {
		t.Fatalf("source was deleted: %s", f.source)
	}
}

func (f *atomicFinishFixture) assertPlanNotPublished(t *testing.T, before map[string]string) {
	t.Helper()
	for ref, want := range before {
		if got := f.remoteRef(t, ref); got != want {
			t.Fatalf("remote %s changed after rejected publication: got %q want %q", ref, got, want)
		}
	}
	f.assertSourcePresent(t)
	if got := f.remoteRef(t, "refs/tags/"+f.tag); got != "" {
		t.Fatalf("tag was published after rejected publication: %q", got)
	}
}

func TestPublishFinishedAndDeleteRemoteAtomicallyPublishesHotfixAndGitFlowRelease(t *testing.T) {
	ctx := context.Background()
	for _, gitflow := range []bool{false, true} {
		t.Run(map[bool]string{false: "hotfix", true: "gitflow release"}[gitflow], func(t *testing.T) {
			f := newAtomicFinishFixture(t, gitflow)
			if err := f.w.publishFinishedAndDeleteRemote(ctx, f.path, f.j, f.targets, f.tag); err != nil {
				t.Fatalf("atomic publication: %v", err)
			}
			if got := f.remoteRef(t, "refs/heads/"+f.source); got != "" {
				t.Fatalf("source remains after atomic publication: %q", got)
			}
			for _, target := range f.targets {
				remote := f.remoteRef(t, "refs/heads/"+target)
				if !strings.HasPrefix(remote, f.j.RemoteTargets[indexOfTarget(t, f.j.RemoteTargets, target)].DesiredSHA+"\t") {
					t.Fatalf("remote %s = %q, want planned desired SHA", target, remote)
				}
			}
			object, peeled, err := f.w.remoteAnnotatedTag(ctx, f.tag)
			if err != nil || object != f.j.TagObjectSHA || peeled != f.j.TagTargetSHA {
				t.Fatalf("remote annotated tag = object=%q peeled=%q err=%v; want %q %q", object, peeled, err, f.j.TagObjectSHA, f.j.TagTargetSHA)
			}
		})
	}
}

func indexOfTarget(t *testing.T, targets []finishRemoteTarget, name string) int {
	t.Helper()
	for i := range targets {
		if targets[i].Name == name {
			return i
		}
	}
	t.Fatalf("missing planned target %s", name)
	return -1
}

func TestPublishFinishedAndDeleteRemoteRejectsConcurrentRemoteChangesWithoutPartialPublication(t *testing.T) {
	ctx := context.Background()
	for _, change := range []string{"source", "target", "tag"} {
		t.Run(change, func(t *testing.T) {
			f := newAtomicFinishFixture(t, false)
			journalBefore, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			before := map[string]string{"refs/heads/main": f.remoteRef(t, "refs/heads/main")}
			switch change {
			case "source":
				gitTest(t, f.repo, "checkout", "--no-guess", f.source)
				gitTest(t, f.repo, "commit", "--allow-empty", "-m", "concurrent source")
				gitTest(t, f.repo, "push", "origin", "HEAD:refs/heads/"+f.source)
			case "target":
				gitTest(t, f.repo, "checkout", "--no-guess", "main")
				gitTest(t, f.repo, "commit", "--allow-empty", "-m", "concurrent target")
				gitTest(t, f.repo, "push", "origin", "HEAD:refs/heads/main")
				before["refs/heads/main"] = f.remoteRef(t, "refs/heads/main")
			case "tag":
				gitTest(t, f.repo, "push", "origin", "refs/tags/"+f.tag+":refs/tags/"+f.tag)
			}
			if err := f.w.publishFinishedAndDeleteRemote(ctx, f.path, f.j, f.targets, f.tag); err == nil {
				t.Fatal("concurrent remote change was accepted")
			}
			// The persisted plan is a transaction boundary; rejected attempts do
			// not advance the local finish cursor or rewrite planned identities.
			if len(f.j.RemoteTargets) != 1 || f.j.RemoteTargets[0].Name != "main" || f.j.TagObjectSHA == "" {
				t.Fatalf("publication plan changed after rejection: %#v", f.j)
			}
			journalAfter, err := os.ReadFile(f.path)
			if err != nil || string(journalAfter) != string(journalBefore) {
				t.Fatalf("journal advanced after rejected publication: err=%v before=%s after=%s", err, journalBefore, journalAfter)
			}
			f.assertSourcePresent(t)
			if change != "tag" {
				if got := f.remoteRef(t, "refs/tags/"+f.tag); got != "" {
					t.Fatalf("tag unexpectedly published: %q", got)
				}
			}
			for ref, want := range before {
				if got := f.remoteRef(t, ref); got != want {
					t.Fatalf("%s changed: got %q want %q", ref, got, want)
				}
			}
		})
	}
}

func TestPublishFinishedAndDeleteRemoteFailsClosedWhenAtomicUnsupported(t *testing.T) {
	ctx := context.Background()
	f := newAtomicFinishFixture(t, false)
	remote := filepath.Join(f.root, "remote.git")
	gitTest(t, f.root, "-C", remote, "config", "receive.advertiseAtomic", "false")
	before := map[string]string{"refs/heads/main": f.remoteRef(t, "refs/heads/main")}
	if err := f.w.publishFinishedAndDeleteRemote(ctx, f.path, f.j, f.targets, f.tag); err == nil {
		t.Fatal("server without atomic capability was accepted")
	}
	f.assertPlanNotPublished(t, before)
}

func TestPublishFinishedAndDeleteRemoteReplayAcceptsOnlyExactCompletedTransaction(t *testing.T) {
	ctx := context.Background()
	for _, mutation := range []string{"exact", "missing-tag", "wrong-tag", "non-descendant-target"} {
		t.Run(mutation, func(t *testing.T) {
			f := newAtomicFinishFixture(t, false)
			if err := f.w.publishFinishedAndDeleteRemote(ctx, f.path, f.j, f.targets, f.tag); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "missing-tag":
				gitTest(t, f.repo, "push", "origin", ":refs/tags/"+f.tag)
			case "wrong-tag":
				gitTest(t, f.repo, "tag", "-f", "-a", f.tag, f.j.SourceSHA, "-m", "wrong object")
				gitTest(t, f.repo, "push", "--force", "origin", "refs/tags/"+f.tag+":refs/tags/"+f.tag)
			case "non-descendant-target":
				gitTest(t, f.repo, "checkout", "--orphan", "displaced")
				gitTest(t, f.repo, "commit", "--allow-empty", "-m", "unrelated target")
				gitTest(t, f.repo, "push", "--force", "origin", "HEAD:refs/heads/main")
			}
			err := f.w.publishFinishedAndDeleteRemote(ctx, f.path, f.j, f.targets, f.tag)
			if mutation == "exact" {
				if err != nil {
					t.Fatalf("lost response replay rejected exact completed transaction: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("replay accepted %s mutation", mutation)
			}
		})
	}
}

func TestPrepareFinishedRemotePlanRejectsNoOpTargetBeforeSourceDeletion(t *testing.T) {
	ctx := context.Background()
	f := newAtomicFinishFixture(t, false)
	// Rebuild a fresh journal whose desired target is already the remote tip.
	j := &journal{Source: f.source, SourceSHA: f.j.SourceSHA, TagTargetSHA: f.j.TagTargetSHA}
	gitTest(t, f.repo, "reset", "--hard", "origin/main")
	gitTest(t, f.repo, "tag", "-d", f.tag)
	gitTest(t, f.repo, "tag", "-a", f.tag, "-m", "tag")
	if err := f.w.prepareFinishedRemotePlan(ctx, filepath.Join(f.root, "no-op.json"), j, []string{"main"}, f.tag); err == nil || !strings.Contains(err.Error(), "already published") {
		t.Fatalf("no-op target plan = %v, want fail-closed error", err)
	}
	f.assertSourcePresent(t)
}

func TestPublishFinishedAndDeleteRemoteUsesOneAtomicPushWithoutPrepublication(t *testing.T) {
	const (
		source    = "source-sha"
		before    = "target-before"
		desired   = "target-desired"
		tagObject = "tag-object"
		tagTarget = "tag-target"
	)
	f := &fakeRunner{replies: map[string]string{
		key("git", "ls-remote", "origin", "refs/heads/hotfix/v1.2.3"):                          source + "\trefs/heads/hotfix/v1.2.3\n",
		key("git", "rev-parse", "--verify", source+"^{commit}"):                                source + "\n",
		key("git", "rev-parse", "--verify", tagObject+"^{commit}"):                             tagTarget + "\n",
		key("git", "rev-parse", "--verify", before+"^{commit}"):                                before + "\n",
		key("git", "rev-parse", "--verify", desired+"^{commit}"):                               desired + "\n",
		key("git", "rev-parse", "--verify", "refs/tags/v1.2.3^{tag}"):                          tagObject + "\n",
		key("git", "ls-remote", "origin", "refs/heads/main"):                                   before + "\trefs/heads/main\n",
		key("git", "ls-remote", "--tags", "origin", "refs/tags/v1.2.3", "refs/tags/v1.2.3^{}"): "",
	}}
	w, err := NewWorkflows(WorkflowOptions{Runner: f, MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	j := &journal{Source: "hotfix/v1.2.3", SourceSHA: source, TagTargetSHA: tagTarget, TagObjectSHA: tagObject,
		RemoteTargets: []finishRemoteTarget{{Name: "main", BeforeSHA: before, DesiredSHA: desired}}}
	if err = w.publishFinishedAndDeleteRemote(context.Background(), "ignored", j, []string{"main"}, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	pushes := []call{}
	for _, c := range f.calls {
		if c.name == "git" && len(c.args) > 0 && c.args[0] == "push" {
			pushes = append(pushes, c)
		}
	}
	if len(pushes) != 1 {
		t.Fatalf("push count = %d, want exactly one atomic finalization: %#v", len(pushes), f.calls)
	}
	got := strings.Join(pushes[0].args, " ")
	for _, want := range []string{"--atomic", desired + ":refs/heads/main", tagObject + ":refs/tags/v1.2.3", ":refs/heads/hotfix/v1.2.3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("atomic finalization missing %q: %s", want, got)
		}
	}
}

func TestLocalFinishRejectsPreSchemaV4JournalBeforeRemoteMutation(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, false)
	gitTest(t, repo, "checkout", "-b", "hotfix/v1.2.3")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "source")
	gitTest(t, repo, "push", "-u", "origin", "hotfix/v1.2.3")
	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "githubflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	path, err := w.journalPath(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := w.journalWorktree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := w.journalRepository(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sourceSHA := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	if err = writeAtomic(path, &journal{
		Version: 3, WayOfWork: "githubflow", Repository: repository, Worktree: worktree,
		Operation: "hotfix-finish", Source: "hotfix/v1.2.3", SourceSHA: sourceSHA, Target: "main",
		Steps: w.localFinishSteps("hotfix-finish"),
	}); err != nil {
		t.Fatal(err)
	}
	mainBefore := strings.TrimSpace(gitTest(t, repo, "ls-remote", "origin", "refs/heads/main"))
	if err = w.HotfixFinish(ctx, "1.2.3", true); err == nil || !strings.Contains(err.Error(), "unfinished workflow journal") {
		t.Fatalf("pre-schema-v4 journal was accepted: %v", err)
	}
	if got := strings.TrimSpace(gitTest(t, repo, "ls-remote", "origin", "refs/heads/main")); got != mainBefore {
		t.Fatalf("target changed after old journal rejection: got %q want %q", got, mainBefore)
	}
	if got := gitTest(t, repo, "ls-remote", "origin", "refs/heads/hotfix/v1.2.3"); got == "" {
		t.Fatal("source was deleted after old journal rejection")
	}
}

func TestPublishFinishedAndDeleteRemoteRejectsCorruptGitFlowPlanAfterSourceDeletion(t *testing.T) {
	ctx := context.Background()
	for _, corrupt := range []string{"missing-develop", "wrong-desired"} {
		t.Run(corrupt, func(t *testing.T) {
			f := newAtomicFinishFixture(t, true)
			if err := f.w.publishFinishedAndDeleteRemote(ctx, f.path, f.j, f.targets, f.tag); err != nil {
				t.Fatalf("complete atomic publication: %v", err)
			}
			if got := f.remoteRef(t, "refs/heads/"+f.source); got != "" {
				t.Fatalf("fixture source remains remote: %q", got)
			}

			bad := *f.j
			bad.RemoteTargets = append([]finishRemoteTarget(nil), f.j.RemoteTargets...)
			switch corrupt {
			case "missing-develop":
				bad.RemoteTargets = bad.RemoteTargets[:1]
			case "wrong-desired":
				bad.RemoteTargets[1].DesiredSHA = bad.RemoteTargets[1].BeforeSHA
			}
			if err := writeAtomic(f.path, &bad); err != nil {
				t.Fatal(err)
			}
			journalBefore, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.w.publishFinishedAndDeleteRemote(ctx, f.path, &bad, f.targets, f.tag); err == nil {
				t.Fatalf("source-absent replay accepted %s plan corruption", corrupt)
			}
			journalAfter, err := os.ReadFile(f.path)
			if err != nil || string(journalAfter) != string(journalBefore) {
				t.Fatalf("corrupt replay advanced journal: err=%v before=%s after=%s", err, journalBefore, journalAfter)
			}
			if _, err := os.Stat(filepath.Join(f.repo, ".git", "refs", "heads", f.source)); err != nil {
				t.Fatalf("local source was removed after corrupt replay: %v", err)
			}
		})
	}
}

// newGitFlowReleaseReplayFixture persists a schema-v4 release journal at the
// publish cursor, then records the already-accepted atomic server transaction.
// Calling ReleaseFinish afterwards therefore exercises the production replay
// and cleanup path rather than the internal publication helper directly.
func newGitFlowReleaseReplayFixture(t *testing.T) *atomicFinishFixture {
	t.Helper()
	ctx := context.Background()
	f := newAtomicFinishFixture(t, true)
	steps := f.w.localFinishSteps("release-finish")
	j, path, err := f.w.startJournal(ctx, "release-finish", f.source, "main", f.j.SourceSHA, steps)
	if err != nil {
		t.Fatalf("start schema-v4 journal: %v", err)
	}
	cursor, err := journalStepBoundary(steps, "publish-and-delete-remote")
	if err != nil {
		t.Fatal(err)
	}
	j.Done = cursor
	j.TagTargetSHA = f.j.TagTargetSHA
	j.TagObjectSHA = f.j.TagObjectSHA
	j.RemoteTargets = append([]finishRemoteTarget(nil), f.j.RemoteTargets...)
	if err = writeAtomic(path, j); err != nil {
		t.Fatalf("persist release publication plan: %v", err)
	}
	if err = f.w.publishFinishedAndDeleteRemote(ctx, path, j, f.targets, f.tag); err != nil {
		t.Fatalf("simulate accepted atomic transaction: %v", err)
	}
	// The persisted cursor was not advanced because the client never received
	// the push response. The normal finish loop had already left HEAD on
	// develop after its local merge step.
	gitTest(t, f.repo, "checkout", "--no-guess", "develop")
	f.j, f.path = j, path
	return f
}

func TestReleaseFinishReplaysAcceptedAtomicGitFlowTransactionAndCleansUp(t *testing.T) {
	ctx := context.Background()
	f := newGitFlowReleaseReplayFixture(t)
	if got := f.remoteRef(t, "refs/heads/"+f.source); got != "" {
		t.Fatalf("fixture did not delete remote source atomically: %q", got)
	}
	if err := f.w.ReleaseFinish(ctx, "", true); err != nil {
		t.Fatalf("public release finish replay: %v", err)
	}
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatalf("journal remains after successful replay: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.repo, ".git", "refs", "heads", f.source)); !os.IsNotExist(err) {
		t.Fatalf("local release source remains after replay: %v", err)
	}
	for _, target := range f.targets {
		planned := f.j.RemoteTargets[indexOfTarget(t, f.j.RemoteTargets, target)].DesiredSHA
		if got := f.remoteRef(t, "refs/heads/"+target); !strings.HasPrefix(got, planned+"\t") {
			t.Fatalf("remote %s = %q, want planned %s", target, got, planned)
		}
	}
	object, peeled, err := f.w.remoteAnnotatedTag(ctx, f.tag)
	if err != nil || object != f.j.TagObjectSHA || peeled != f.j.TagTargetSHA {
		t.Fatalf("remote tag object=%q peeled=%q err=%v; want %q %q", object, peeled, err, f.j.TagObjectSHA, f.j.TagTargetSHA)
	}
}

func TestReleaseFinishRejectsCorruptPersistedGitFlowReplayPlanWithoutCleanup(t *testing.T) {
	ctx := context.Background()
	for _, corrupt := range []string{"missing-develop", "invalid-develop-desired"} {
		t.Run(corrupt, func(t *testing.T) {
			f := newGitFlowReleaseReplayFixture(t)
			bad := *f.j
			bad.RemoteTargets = append([]finishRemoteTarget(nil), f.j.RemoteTargets...)
			switch corrupt {
			case "missing-develop":
				bad.RemoteTargets = bad.RemoteTargets[:1]
			case "invalid-develop-desired":
				bad.RemoteTargets[1].DesiredSHA = "not-a-commit"
			}
			if err := writeAtomic(f.path, &bad); err != nil {
				t.Fatal(err)
			}
			journalBefore, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			refsBefore := map[string]string{
				"refs/heads/main":    f.remoteRef(t, "refs/heads/main"),
				"refs/heads/develop": f.remoteRef(t, "refs/heads/develop"),
				"refs/tags/" + f.tag: f.remoteRef(t, "refs/tags/"+f.tag),
			}
			if err = f.w.ReleaseFinish(ctx, "", true); err == nil {
				t.Fatalf("public release replay accepted %s persisted plan", corrupt)
			}
			journalAfter, readErr := os.ReadFile(f.path)
			if readErr != nil || string(journalAfter) != string(journalBefore) {
				t.Fatalf("journal changed after corrupt replay: read=%v before=%s after=%s", readErr, journalBefore, journalAfter)
			}
			if _, statErr := os.Stat(filepath.Join(f.repo, ".git", "refs", "heads", f.source)); statErr != nil {
				t.Fatalf("local release source removed after corrupt replay: %v", statErr)
			}
			for ref, want := range refsBefore {
				if got := f.remoteRef(t, ref); got != want {
					t.Fatalf("remote %s changed after corrupt replay: got %q want %q", ref, got, want)
				}
			}
		})
	}
}

func TestReleaseFinishRejectsNonCanonicalSourceIdentityAfterAcceptedAtomicTransaction(t *testing.T) {
	ctx := context.Background()
	for _, sourceKind := range []string{"symbolic", "abbreviated", "corrupt"} {
		t.Run(sourceKind, func(t *testing.T) {
			f := newGitFlowReleaseReplayFixture(t)
			bad := *f.j
			switch sourceKind {
			case "symbolic":
				bad.SourceSHA = "main"
			case "abbreviated":
				bad.SourceSHA = f.j.SourceSHA[:12]
			case "corrupt":
				bad.SourceSHA = "not-a-commit"
			}
			if err := writeAtomic(f.path, &bad); err != nil {
				t.Fatal(err)
			}
			journalBefore, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			refsBefore := map[string]string{
				"refs/heads/main":    f.remoteRef(t, "refs/heads/main"),
				"refs/heads/develop": f.remoteRef(t, "refs/heads/develop"),
				"refs/tags/" + f.tag: f.remoteRef(t, "refs/tags/"+f.tag),
			}
			if err = f.w.ReleaseFinish(ctx, "", true); err == nil || !strings.Contains(err.Error(), "journaled source") {
				t.Fatalf("non-canonical source identity was accepted: %v", err)
			}
			journalAfter, readErr := os.ReadFile(f.path)
			if readErr != nil || string(journalAfter) != string(journalBefore) {
				t.Fatalf("journal changed after source identity rejection: read=%v before=%s after=%s", readErr, journalBefore, journalAfter)
			}
			if _, statErr := os.Stat(filepath.Join(f.repo, ".git", "refs", "heads", f.source)); statErr != nil {
				t.Fatalf("local release source removed after source identity rejection: %v", statErr)
			}
			for ref, want := range refsBefore {
				if got := f.remoteRef(t, ref); got != want {
					t.Fatalf("remote %s changed after source identity rejection: got %q want %q", ref, got, want)
				}
			}
		})
	}
}
