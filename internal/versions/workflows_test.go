package versions

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}
type fakeRunner struct {
	calls   []call
	replies map[string]string
	errs    map[string]error
}

func key(n string, a ...string) string { return n + " " + strings.Join(a, " ") }

type actionOutputRunner struct{}

func (actionOutputRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	if name == "git" && len(args) > 0 && args[0] == "checkout" {
		return "Switched to a new branch 'feature/demo'\n", nil
	}
	return "probe output\n", nil
}

func TestWorkflowsCaptureOnlyHumanUsefulActionOutput(t *testing.T) {
	w, err := NewWorkflows(WorkflowOptions{Runner: actionOutputRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.git(context.Background(), "show-ref", "--verify", "refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.git(context.Background(), "checkout", "-b", "feature/demo"); err != nil {
		t.Fatal(err)
	}
	results := w.ActionResults()
	if len(results) != 1 || results[0].Tool != "Git" || results[0].Output != "Switched to a new branch 'feature/demo'" {
		t.Fatalf("ActionResults() = %#v", results)
	}
}
func gitVersionKey(variable string) string {
	return key("gitversion", "-config", filepath.Join(cloudOpsWorksDir, "gitversion.yaml"), "-showvariable", variable)
}
func (f *fakeRunner) Run(_ context.Context, n string, a ...string) (string, error) {
	f.calls = append(f.calls, call{n, append([]string(nil), a...)})
	k := key(n, a...)
	if e := f.errs[k]; e != nil {
		return "", e
	}
	return f.replies[k], nil
}
func (f *fakeRunner) saw(parts ...string) bool {
	for _, c := range f.calls {
		if strings.Join(append([]string{c.name}, c.args...), " ") == strings.Join(parts, " ") {
			return true
		}
	}
	return false
}

func (f *fakeRunner) sawPrefix(parts ...string) bool {
	for _, c := range f.calls {
		callParts := append([]string{c.name}, c.args...)
		if len(callParts) < len(parts) {
			continue
		}
		matched := true
		for index, part := range parts {
			if callParts[index] != part {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func TestFakeRunnerSawPrefixKeepsExactSawSemantics(t *testing.T) {
	f := &fakeRunner{}
	if _, err := f.Run(context.Background(), "gh", "pr", "create", "--base", "develop"); err != nil {
		t.Fatal(err)
	}
	if f.saw("gh", "pr", "create") {
		t.Fatal("saw must remain exact")
	}
	if !f.sawPrefix("gh", "pr", "create") {
		t.Fatal("sawPrefix did not detect a command with additional arguments")
	}
}

func TestFeatureStartUsesWOWBase(t *testing.T) {
	for _, tc := range []struct{ wow, base string }{{"gitflow", "develop"}, {"githubflow", "main"}, {"trunk", "main"}} {
		t.Run(tc.wow, func(t *testing.T) {
			f := &fakeRunner{replies: map[string]string{
				key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):   "refs/remotes/origin/main\n",
				key("git", "ls-remote", "--exit-code", "origin", "refs/heads/main"): "abc\trefs/heads/main\n",
			}}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: tc.wow, Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			if err = w.FeatureStart(context.Background(), "one"); err != nil {
				t.Fatal(err)
			}
			if !f.saw("git", "checkout", "-b", "feature/one", "refs/heads/"+tc.base) {
				t.Fatalf("calls: %#v", f.calls)
			}
		})
	}
}
func TestFeatureFinishRequiresExactRemoteParity(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{key("git", "branch", "--show-current"): "feature/a\n", key("git", "rev-parse", "--verify", "refs/heads/feature/a^{commit}"): "abc\n", key("git", "ls-remote", "origin", "refs/heads/feature/a"): "def\trefs/heads/feature/a\n"}}
	w, _ := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", Runner: f})
	e := w.FeatureFinish(context.Background(), "")
	if e == nil || !strings.Contains(e.Error(), "exactly published") {
		t.Fatalf("got %v", e)
	}
	if f.sawPrefix("gh", "pr", "create") {
		t.Fatal("PR created despite parity failure")
	}
}

func TestFeatureAliasUsesExactCurrentBranch(t *testing.T) {
	branch := "feat/coexist"
	f := &fakeRunner{replies: map[string]string{
		key("git", "branch", "--show-current"):                                branch + "\n",
		key("git", "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}"): "abc\n",
		key("git", "ls-remote", "origin", "refs/heads/"+branch):               "abc\trefs/heads/" + branch + "\n",
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):     "refs/remotes/origin/main\n",
	}}
	w, _ := NewWorkflows(WorkflowOptions{WayOfWork: "githubflow", Runner: f})
	if err := w.FeaturePublish(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !f.saw("git", "checkout", "--no-guess", branch) || !f.saw("git", "push", "--set-upstream", "origin", "refs/heads/"+branch+":refs/heads/"+branch) {
		t.Fatalf("publish did not keep alias branch: %#v", f.calls)
	}
	if err := w.FeatureFinish(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !f.sawPrefix("gh", "pr", "create", "--head", branch) || f.sawPrefix("gh", "pr", "create", "--head", "feature/coexist") {
		t.Fatalf("finish did not keep alias branch: %#v", f.calls)
	}
}

func TestHotfixAliasUsesExactCurrentBranch(t *testing.T) {
	branch := "fix/v1.2.3"
	f := &fakeRunner{replies: map[string]string{
		key("git", "branch", "--show-current"):                                branch + "\n",
		key("git", "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}"): "abc\n",
		key("git", "ls-remote", "origin", "refs/heads/"+branch):               "abc\trefs/heads/" + branch + "\n",
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):     "refs/remotes/origin/main\n",
	}}
	w, _ := NewWorkflows(WorkflowOptions{WayOfWork: "githubflow", Runner: f})
	if err := w.HotfixPublish(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !f.saw("git", "checkout", "--no-guess", branch) || !f.saw("git", "push", "--set-upstream", "origin", "refs/heads/"+branch+":refs/heads/"+branch) {
		t.Fatalf("publish did not keep alias branch: %#v", f.calls)
	}
	if err := w.HotfixFinish(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
	if !f.sawPrefix("gh", "pr", "create", "--head", branch) || f.sawPrefix("gh", "pr", "create", "--head", "hotfix/v1.2.3") {
		t.Fatalf("finish did not keep alias branch: %#v", f.calls)
	}
}
func TestTagUsesQualifierAndPushesExactTag(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{key("git", "branch", "--show-current"): "feature/a\n", key("git", "rev-parse", "--verify", "refs/heads/feature/a^{commit}"): "abc\n", key("git", "ls-remote", "origin", "refs/heads/feature/a"): "abc\trefs/heads/feature/a\n", key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"): "refs/remotes/origin/main\n", gitVersionKey("SemVer"): "1.2.3-alpha.1\n", key("git", "rev-parse", "--verify", "HEAD^{commit}"): "abc\n"}, errs: map[string]error{key("git", "rev-parse", "--verify", "refs/tags/v1.2.3-alpha.1+deploy-test^{commit}"): fmt.Errorf("missing")}}
	w, _ := NewWorkflows(WorkflowOptions{Runner: f})
	tag, e := w.Tag(context.Background(), "test", true)
	if e != nil {
		t.Fatal(e)
	}
	if tag != "v1.2.3-alpha.1+deploy-test" {
		t.Fatal(tag)
	}
	if !f.saw("git", "push", "origin", "refs/tags/"+tag+":refs/tags/"+tag) {
		t.Fatalf("not exact tag: %#v", f.calls)
	}
	if !f.saw("git", "tag", "-a", tag, "HEAD", "-m", "chore: Version Tagging: "+tag) {
		t.Fatalf("annotated tag was not created: %#v", f.calls)
	}
}

func TestTagPublishPushesExistingUnpublishedHEADTagInsteadOfRecalculating(t *testing.T) {
	const tag = "v0.2.0-beta.3"
	f := &fakeRunner{replies: map[string]string{
		key("git", "branch", "--show-current"):                            "main\n",
		key("git", "rev-parse", "--verify", "refs/heads/main^{commit}"):   "commit-sha\n",
		key("git", "ls-remote", "origin", "refs/heads/main"):              "commit-sha\trefs/heads/main\n",
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"): "refs/remotes/origin/main\n",
		key("git", "tag", "--points-at", "HEAD"):                          tag + "\n",
		key("git", "rev-parse", "--verify", "refs/tags/"+tag):             "tag-object\n",
		key("git", "ls-remote", "--refs", "origin", "refs/tags/"+tag):     "",
	}}
	w, err := NewWorkflows(WorkflowOptions{Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.Tag(context.Background(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != tag {
		t.Fatalf("tag = %q, want existing local tag %q", got, tag)
	}
	if !f.saw("git", "push", "origin", "refs/tags/"+tag+":refs/tags/"+tag) {
		t.Fatalf("existing unpushed tag was not pushed: %#v", f.calls)
	}
	if f.sawPrefix("gitversion") || f.sawPrefix("git", "tag", "-a") {
		t.Fatalf("publish recalculated or recreated an existing local tag: %#v", f.calls)
	}
}

func TestTagPublishIsIdempotentForExistingPublishedHEADTag(t *testing.T) {
	const tag = "v0.2.0-beta.3"
	f := &fakeRunner{replies: map[string]string{
		key("git", "branch", "--show-current"):                            "main\n",
		key("git", "rev-parse", "--verify", "refs/heads/main^{commit}"):   "commit-sha\n",
		key("git", "ls-remote", "origin", "refs/heads/main"):              "commit-sha\trefs/heads/main\n",
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"): "refs/remotes/origin/main\n",
		key("git", "tag", "--points-at", "HEAD"):                          tag + "\n",
		key("git", "rev-parse", "--verify", "refs/tags/"+tag):             "tag-object\n",
		key("git", "ls-remote", "--refs", "origin", "refs/tags/"+tag):     "tag-object\trefs/tags/" + tag + "\n",
		key("git", "rev-parse", "--verify", "HEAD^{commit}"):              "commit-sha\n",
		key("git", "ls-remote", "--tags", "origin"):                       "tag-object\trefs/tags/" + tag + "\ncommit-sha\trefs/tags/" + tag + "^{}\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.Tag(context.Background(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != tag {
		t.Fatalf("tag = %q, want existing tag %q", got, tag)
	}
	if f.sawPrefix("gitversion") || f.sawPrefix("git", "tag", "-a") || f.sawPrefix("git", "push") {
		t.Fatalf("already-published tag was not treated as a no-op: %#v", f.calls)
	}
}

func TestTagPublishIsIdempotentForRemoteOnlyAnnotatedTagAtHead(t *testing.T) {
	const tag = "v0.2.0-beta.3"
	f := &fakeRunner{replies: map[string]string{
		key("git", "branch", "--show-current"):                            "main\n",
		key("git", "rev-parse", "--verify", "refs/heads/main^{commit}"):   "commit-sha\n",
		key("git", "ls-remote", "origin", "refs/heads/main"):              "commit-sha\trefs/heads/main\n",
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"): "refs/remotes/origin/main\n",
		gitVersionKey("MajorMinorPatch"):                                  "0.2.0-beta.3\n",
		key("git", "ls-remote", "--tags", "origin", "refs/tags/"+tag):     "tag-object\trefs/tags/" + tag + "\ncommit-sha\trefs/tags/" + tag + "^{}\n",
		key("git", "rev-parse", "--verify", "HEAD^{commit}"):              "commit-sha\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.Tag(context.Background(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != tag {
		t.Fatalf("tag = %q, want remote tag %q", got, tag)
	}
	if f.sawPrefix("git", "tag", "-a") || f.sawPrefix("git", "push") {
		t.Fatalf("remote-only tag was recreated or pushed: %#v", f.calls)
	}
}

func TestTagDoesNotCreateCandidateWhenRemoteSameNameTargetsElsewhere(t *testing.T) {
	const tag = "v0.2.0-beta.3"
	f := &fakeRunner{replies: map[string]string{
		key("git", "branch", "--show-current"):                            "main\n",
		key("git", "rev-parse", "--verify", "refs/heads/main^{commit}"):   "head-sha\n",
		key("git", "ls-remote", "origin", "refs/heads/main"):              "head-sha\trefs/heads/main\n",
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"): "refs/remotes/origin/main\n",
		gitVersionKey("MajorMinorPatch"):                                  "0.2.0-beta.3\n",
		key("git", "ls-remote", "--tags", "origin", "refs/tags/"+tag):     "tag-object\trefs/tags/" + tag + "\nother-sha\trefs/tags/" + tag + "^{}\n",
		key("git", "rev-parse", "--verify", "HEAD^{commit}"):              "head-sha\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Tag(context.Background(), "", true); err == nil {
		t.Fatal("expected conflicting remote tag to fail")
	}
	if f.sawPrefix("git", "tag", "-a") || f.sawPrefix("git", "push") {
		t.Fatalf("conflicting remote tag caused mutation: %#v", f.calls)
	}
}

func TestTagRejectsDifferentVersionOnSameCommitButAllowsDeploymentQualifier(t *testing.T) {
	for _, tc := range []struct {
		name, existing, calculated string
		wantErr                    bool
	}{
		{name: "different patch version rejected", existing: "v1.2.3", calculated: "1.2.4", wantErr: true},
		{name: "different prerelease version rejected", existing: "v1.2.3-beta.3", calculated: "1.2.3-beta.4", wantErr: true},
		{name: "same version with deploy qualifier allowed", existing: "v1.2.3-beta.3+deploy-test", calculated: "1.2.3-beta.3"},
		{name: "different deploy qualifiers allowed", existing: "v1.2.3+deploy-one", calculated: "1.2.3+deploy-two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := normalizeVersion(tc.calculated)
			f := &fakeRunner{replies: map[string]string{
				key("git", "branch", "--show-current"):                               "feature/a\n",
				key("git", "rev-parse", "--verify", "refs/heads/feature/a^{commit}"): "commit-sha\n",
				key("git", "ls-remote", "origin", "refs/heads/feature/a"):            "commit-sha\trefs/heads/feature/a\n",
				key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):    "refs/remotes/origin/main\n",
				key("git", "tag", "--points-at", "HEAD"):                             tc.existing + "\n",
				gitVersionKey("SemVer"):                                              tc.calculated + "\n",
				key("git", "rev-parse", "--verify", "HEAD^{commit}"):                 "commit-sha\n",
			}}
			f.errs = map[string]error{
				key("git", "rev-parse", "--verify", "refs/tags/"+candidate+"^{commit}"): fmt.Errorf("tag missing"),
			}
			w, err := NewWorkflows(WorkflowOptions{Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			_, err = w.Tag(context.Background(), "", false)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "different version") {
					t.Fatalf("Tag error = %v, want different version", err)
				}
				if f.sawPrefix("git", "tag", "-a") {
					t.Fatalf("tag was created after numeric version conflict: %#v", f.calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !f.saw("git", "tag", "-a", candidate, "HEAD", "-m", "chore: Version Tagging: "+candidate) {
				t.Fatalf("qualified tag was not created: %#v", f.calls)
			}
		})
	}
}

func TestTagRejectsRemoteDifferentVersionOnSameCommit(t *testing.T) {
	const tag = "v1.2.3"
	f := &fakeRunner{replies: map[string]string{
		key("git", "branch", "--show-current"):                               "feature/a\n",
		key("git", "rev-parse", "--verify", "refs/heads/feature/a^{commit}"): "commit-sha\n",
		key("git", "ls-remote", "origin", "refs/heads/feature/a"):            "commit-sha\trefs/heads/feature/a\n",
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):    "refs/remotes/origin/main\n",
		gitVersionKey("SemVer"):                                              "1.2.4\n",
		key("git", "tag", "--points-at", "HEAD"):                             "",
		key("git", "rev-parse", "--verify", "HEAD^{commit}"):                 "commit-sha\n",
		key("git", "ls-remote", "--tags", "origin"):                          "commit-sha\trefs/tags/" + tag + "\n",
	}}
	f.errs = map[string]error{
		key("git", "rev-parse", "--verify", "refs/tags/v1.2.4^{commit}"): fmt.Errorf("tag missing"),
	}
	w, err := NewWorkflows(WorkflowOptions{Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Tag(context.Background(), "", false); err == nil || !strings.Contains(err.Error(), "remote version tag") {
		t.Fatalf("Tag error = %v, want remote numeric version conflict", err)
	}
	if f.sawPrefix("git", "tag", "-a") {
		t.Fatalf("tag created despite remote numeric version conflict: %#v", f.calls)
	}
}

func TestTagUsesRepositoryGitVersionConfig(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "branch", "--show-current"):                               "feature/a\n",
		key("git", "rev-parse", "--verify", "refs/heads/feature/a^{commit}"): "abc\n",
		key("git", "ls-remote", "origin", "refs/heads/feature/a"):            "abc\trefs/heads/feature/a\n",
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):    "refs/remotes/origin/main\n",
		gitVersionKey("SemVer"):                                              "1.2.3-alpha.3\n",
		key("git", "rev-parse", "--verify", "HEAD^{commit}"):                 "abc\n",
	}, errs: map[string]error{
		key("git", "rev-parse", "--verify", "refs/tags/v1.2.3-alpha.3^{commit}"): fmt.Errorf("missing"),
	}}
	w, err := NewWorkflows(WorkflowOptions{Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Tag(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
	if !f.saw("gitversion", "-config", filepath.Join(cloudOpsWorksDir, "gitversion.yaml"), "-showvariable", "SemVer") {
		t.Fatalf("tag ignored repository GitVersion config: %#v", f.calls)
	}
}
func TestSupportIsGitflowOnly(t *testing.T) {
	w, _ := NewWorkflows(WorkflowOptions{WayOfWork: "githubflow", Runner: &fakeRunner{}})
	if e := w.SupportStart(context.Background(), "1.0.0"); e == nil || !strings.Contains(e.Error(), "gitflow") {
		t.Fatalf("got %v", e)
	}
}

func TestRequireParityChecksNamedBranchInsteadOfHEAD(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "rev-parse", "--verify", "refs/heads/feature/named^{commit}"): "named-sha\n",
		key("git", "ls-remote", "origin", "refs/heads/feature/named"):            "named-sha\trefs/heads/feature/named\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.RequireParity(context.Background(), "feature/named"); err != nil {
		t.Fatal(err)
	}
	if f.saw("git", "rev-parse", "HEAD") {
		t.Fatalf("parity must not inspect HEAD: %#v", f.calls)
	}
}

func TestSupportStartFetchesTagsBeforeValidatingTag(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "rev-parse", "--verify", "refs/tags/v1.2.3^{commit}"): "abc\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.SupportStart(context.Background(), "1.2.3"); err != nil {
		t.Fatal(err)
	}
	fetch, verify := -1, -1
	for i, call := range f.calls {
		got := strings.Join(append([]string{call.name}, call.args...), " ")
		if got == "git fetch origin --tags" {
			fetch = i
		}
		if got == "git rev-parse --verify refs/tags/v1.2.3^{commit}" {
			verify = i
		}
	}
	if fetch < 0 || verify < 0 || fetch > verify {
		t.Fatalf("tag validation did not follow fetch: %#v", f.calls)
	}
}

func TestMainFailsClosedForRemoteHeadErrorsAndMalformedRefs(t *testing.T) {
	operational := exec.Command("sh", "-c", "exit 2").Run()
	if operational == nil {
		t.Fatal("expected exit status 2")
	}
	f := &fakeRunner{errs: map[string]error{
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"): operational,
	}}
	w, err := NewWorkflows(WorkflowOptions{Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Main(context.Background()); err == nil {
		t.Fatal("Main fell back after an operational remote HEAD error")
	}
	if f.sawPrefix("git", "show-ref") {
		t.Fatalf("Main performed fallback discovery after operational error: %#v", f.calls)
	}

	for _, ref := range []string{"origin/main", "refs/remotes/other/main", "refs/remotes/origin/"} {
		t.Run(ref, func(t *testing.T) {
			f := &fakeRunner{replies: map[string]string{
				key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"): ref + "\n",
			}}
			w, err := NewWorkflows(WorkflowOptions{Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.Main(context.Background()); err == nil {
				t.Fatalf("Main accepted malformed remote HEAD %q", ref)
			}
			if f.sawPrefix("git", "show-ref") {
				t.Fatalf("Main fell back after malformed remote HEAD %q: %#v", ref, f.calls)
			}
		})
	}
}

func TestPublishUsesQualifiedBranchRefspecs(t *testing.T) {
	for _, tc := range []struct {
		name, wow, branch string
		publish           func(*Workflows) error
	}{
		{name: "feature", wow: "githubflow", branch: "feature/v1", publish: func(w *Workflows) error { return w.FeaturePublish(context.Background(), "v1") }},
		{name: "hotfix", wow: "githubflow", branch: "hotfix/v1.2.3", publish: func(w *Workflows) error { return w.HotfixPublish(context.Background(), "1.2.3") }},
		{name: "release", wow: "githubflow", branch: "release/v1.2.3", publish: func(w *Workflows) error { return w.ReleasePublish(context.Background(), "1.2.3") }},
		{name: "support", wow: "gitflow", branch: "support/v1.2.3", publish: func(w *Workflows) error { return w.SupportPublish(context.Background(), "1.2.3") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: tc.wow, Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			if err = tc.publish(w); err != nil {
				t.Fatal(err)
			}
			if !f.saw("git", "checkout", "--no-guess", tc.branch) || !f.saw("git", "push", "--set-upstream", "origin", "refs/heads/"+tc.branch+":refs/heads/"+tc.branch) {
				t.Fatalf("publish did not use qualified branch refspec: %#v", f.calls)
			}
		})
	}
}

func TestExactRemoteBranchAdvertisementRejectsNestedAndAmbiguousResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		out    string
		sha    string
		exists bool
		bad    bool
	}{
		{name: "absent", out: "", exists: false},
		{name: "exact", out: "sha\trefs/heads/feature/a\n", sha: "sha", exists: true},
		{name: "nested-tail-match", out: "sha\trefs/heads/refs/heads/feature/a\n", bad: true},
		{name: "multiple", out: "one\trefs/heads/feature/a\ntwo\trefs/heads/refs/heads/feature/a\n", bad: true},
		{name: "malformed", out: "sha\n", bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sha, exists, err := exactRemoteBranchAdvertisement(tc.out, "feature/a")
			if tc.bad {
				if err == nil {
					t.Fatal("malformed remote advertisement was accepted")
				}
				return
			}
			if err != nil || sha != tc.sha || exists != tc.exists {
				t.Fatalf("parse = sha=%q exists=%v err=%v; want sha=%q exists=%v", sha, exists, err, tc.sha, tc.exists)
			}
		})
	}
}

func TestDeleteRemoteBranchRejectsNestedAdvertisementWithoutPush(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "ls-remote", "origin", "refs/heads/feature/a"): "sha\trefs/heads/refs/heads/feature/a\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.deleteRemoteBranch(context.Background(), "feature/a", "sha"); err == nil {
		t.Fatal("nested remote advertisement was accepted for branch deletion")
	}
	if f.sawPrefix("git", "push") {
		t.Fatalf("branch deletion pushed after rejected advertisement: %#v", f.calls)
	}
}
