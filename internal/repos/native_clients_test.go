package repos

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
)

func TestParseOwnerRepoSupportsCommonRemoteForms(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "https", input: "https://github.com/cloudopsworks/tronador-cli.git"},
		{name: "ssh scp", input: "git@github.com:cloudopsworks/tronador-cli.git"},
		{name: "ssh url", input: "ssh://git@github.com/cloudopsworks/tronador-cli.git"},
		{name: "owner repo", input: "cloudopsworks/tronador-cli"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseOwnerRepo(tt.input)
			if err != nil {
				t.Fatalf("parseOwnerRepo() error = %v", err)
			}
			if owner != "cloudopsworks" || repo != "tronador-cli" {
				t.Fatalf("parseOwnerRepo() = %s/%s", owner, repo)
			}
		})
	}
}

func TestRunnerFetchTagsUsesNativeGitHubClient(t *testing.T) {
	client := &fakeGitHubClient{tags: []string{"v5.10.1", "v5.10.2"}}
	runner := &Runner{
		Opts:         Options{WorkDir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard},
		githubClient: client,
	}

	tags, err := runner.fetchTags(context.Background(), "cloudopsworks/tronador-template")
	if err != nil {
		t.Fatalf("fetchTags() error = %v", err)
	}
	if !reflect.DeepEqual(tags, client.tags) {
		t.Fatalf("fetchTags() = %#v, want %#v", tags, client.tags)
	}
	if client.repository != "cloudopsworks/tronador-template" {
		t.Fatalf("native client repository = %q", client.repository)
	}
}

func TestRunnerGitRemoteOwnerRepoUsesNativeGitClient(t *testing.T) {
	client := &fakeGitClient{owner: "cloudopsworks", repo: "tronador-cli"}
	runner := &Runner{
		Opts:      Options{WorkDir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard},
		gitClient: client,
	}

	owner, repo, err := runner.gitRemoteOwnerRepo(context.Background())
	if err != nil {
		t.Fatalf("gitRemoteOwnerRepo() error = %v", err)
	}
	if owner != "cloudopsworks" || repo != "tronador-cli" {
		t.Fatalf("gitRemoteOwnerRepo() = %s/%s", owner, repo)
	}
	if client.workdir != runner.Opts.WorkDir {
		t.Fatalf("native client workdir = %q, want %q", client.workdir, runner.Opts.WorkDir)
	}
}

func TestGoGitClientOriginOwnerRepoParsesRemote(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "https", url: "https://github.com/cloudopsworks/tronador-cli.git"},
		{name: "ssh scp", url: "git@github.com:cloudopsworks/tronador-cli.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			repository, err := git.PlainInit(dir, false)
			if err != nil {
				t.Fatalf("PlainInit() error = %v", err)
			}
			if _, err := repository.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{tt.url}}); err != nil {
				t.Fatalf("CreateRemote() error = %v", err)
			}

			owner, repo, err := goGitClient{}.OriginOwnerRepo(context.Background(), dir)
			if err != nil {
				t.Fatalf("OriginOwnerRepo() error = %v", err)
			}
			if owner != "cloudopsworks" || repo != "tronador-cli" {
				t.Fatalf("OriginOwnerRepo() = %s/%s", owner, repo)
			}
		})
	}
}

func TestListGitHubTagsPaginatesWithGoGHRestShape(t *testing.T) {
	client := &fakeRESTClient{pages: [][]githubTag{
		makeTags(100, "v1.0."),
		{{Name: "v1.0.100"}, {Name: "v1.0.101"}},
	}}

	tags, err := listGitHubTags(context.Background(), client, "cloudopsworks/tronador-template")
	if err != nil {
		t.Fatalf("listGitHubTags() error = %v", err)
	}
	if len(tags) != 102 {
		t.Fatalf("len(tags) = %d, want 102", len(tags))
	}
	wantCalls := []string{
		"repos/cloudopsworks/tronador-template/tags?per_page=100&page=1",
		"repos/cloudopsworks/tronador-template/tags?per_page=100&page=2",
	}
	if !reflect.DeepEqual(client.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", client.calls, wantCalls)
	}
}

type fakeGitHubClient struct {
	repository string
	tags       []string
	err        error
}

func (f *fakeGitHubClient) ListTags(_ context.Context, repository string) ([]string, error) {
	f.repository = repository
	if f.err != nil {
		return nil, f.err
	}
	return f.tags, nil
}

type fakeGitClient struct {
	owner   string
	repo    string
	workdir string
	err     error
}

func (f *fakeGitClient) Clone(context.Context, string, string) error { return errors.New("unused") }
func (f *fakeGitClient) RemoveRemote(context.Context, string, string) error {
	return errors.New("unused")
}
func (f *fakeGitClient) Checkout(context.Context, string, string) (string, error) {
	return "", errors.New("unused")
}
func (f *fakeGitClient) OriginOwnerRepo(_ context.Context, workdir string) (string, string, error) {
	f.workdir = workdir
	if f.err != nil {
		return "", "", f.err
	}
	return f.owner, f.repo, nil
}

type fakeRESTClient struct {
	pages [][]githubTag
	calls []string
}

func (f *fakeRESTClient) DoWithContext(_ context.Context, method string, path string, _ io.Reader, response interface{}) error {
	if method != http.MethodGet {
		return errors.New("unexpected method")
	}
	f.calls = append(f.calls, path)
	pageIndex := len(f.calls) - 1
	if pageIndex >= len(f.pages) {
		return errors.New("unexpected page")
	}
	out, ok := response.(*[]githubTag)
	if !ok {
		return errors.New("unexpected response type")
	}
	*out = append((*out)[:0], f.pages[pageIndex]...)
	return nil
}

func makeTags(count int, prefix string) []githubTag {
	tags := make([]githubTag, count)
	for i := range tags {
		tags[i] = githubTag{Name: prefix + string(rune('0'+i%10))}
	}
	return tags
}
