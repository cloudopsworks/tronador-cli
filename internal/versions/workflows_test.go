package versions

import (
	"context"
	"fmt"
	"os/exec"
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
	f := &fakeRunner{replies: map[string]string{key("git", "branch", "--show-current"): "feature/a\n", key("git", "rev-parse", "--verify", "refs/heads/feature/a^{commit}"): "abc\n", key("git", "ls-remote", "origin", "refs/heads/feature/a"): "abc\trefs/heads/feature/a\n", key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"): "refs/remotes/origin/main\n", key("gitversion", "-showvariable", "SemVer"): "1.2.3-alpha.1\n", key("git", "rev-parse", "--verify", "HEAD^{commit}"): "abc\n"}, errs: map[string]error{key("git", "rev-parse", "--verify", "refs/tags/v1.2.3-alpha.1+deploy-test^{commit}"): fmt.Errorf("missing")}}
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
