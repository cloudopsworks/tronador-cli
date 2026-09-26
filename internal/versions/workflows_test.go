package versions

import (
	"context"
	"fmt"
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
func TestFeatureStartUsesWOWBase(t *testing.T) {
	for _, tc := range []struct{ wow, base string }{{"gitflow", "develop"}, {"githubflow", "main"}, {"trunk", "main"}} {
		t.Run(tc.wow, func(t *testing.T) {
			f := &fakeRunner{replies: map[string]string{key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"): "origin/main\n"}}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: tc.wow, Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			if err = w.FeatureStart(context.Background(), "one"); err != nil {
				t.Fatal(err)
			}
			if !f.saw("git", "checkout", "-b", "feature/one", tc.base) {
				t.Fatalf("calls: %#v", f.calls)
			}
		})
	}
}
func TestFeatureFinishRequiresExactRemoteParity(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{key("git", "branch", "--show-current"): "feature/a\n", key("git", "rev-parse", "--verify", "feature/a^{commit}"): "abc\n", key("git", "ls-remote", "origin", "refs/heads/feature/a"): "def\trefs/heads/feature/a\n"}}
	w, _ := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", Runner: f})
	e := w.FeatureFinish(context.Background(), "")
	if e == nil || !strings.Contains(e.Error(), "exactly published") {
		t.Fatalf("got %v", e)
	}
	if f.saw("gh", "pr", "create") {
		t.Fatal("PR created despite parity failure")
	}
}
func TestTagUsesQualifierAndPushesExactTag(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{key("git", "branch", "--show-current"): "feature/a\n", key("git", "rev-parse", "--verify", "feature/a^{commit}"): "abc\n", key("git", "ls-remote", "origin", "refs/heads/feature/a"): "abc\trefs/heads/feature/a\n", key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"): "origin/main\n", key("gitversion", "-showvariable", "SemVer"): "1.2.3-alpha.1\n"}, errs: map[string]error{key("git", "rev-parse", "--verify", "v1.2.3-alpha.1+deploy-test^{commit}"): fmt.Errorf("missing")}}
	w, _ := NewWorkflows(WorkflowOptions{Runner: f})
	tag, e := w.Tag(context.Background(), "test", true)
	if e != nil {
		t.Fatal(e)
	}
	if tag != "v1.2.3-alpha.1+deploy-test" {
		t.Fatal(tag)
	}
	if !f.saw("git", "push", "origin", tag) {
		t.Fatalf("not exact tag: %#v", f.calls)
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
		key("git", "rev-parse", "--verify", "feature/named^{commit}"): "named-sha\n",
		key("git", "ls-remote", "origin", "refs/heads/feature/named"): "named-sha\trefs/heads/feature/named\n",
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
		key("git", "rev-parse", "--verify", "v1.2.3^{commit}"): "abc\n",
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
		if got == "git rev-parse --verify v1.2.3^{commit}" {
			verify = i
		}
	}
	if fetch < 0 || verify < 0 || fetch > verify {
		t.Fatalf("tag validation did not follow fetch: %#v", f.calls)
	}
}
