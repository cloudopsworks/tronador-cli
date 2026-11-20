package iac

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
)

// TagLister lists GitHub repository tags.
type TagLister interface {
	ListTags(ctx context.Context, repository string) ([]string, error)
}

type goGHTagLister struct{}

type githubRESTClient interface {
	DoWithContext(ctx context.Context, method string, path string, body io.Reader, response interface{}) error
}

type githubTag struct {
	Name string `json:"name"`
}

// NewGitHubTagLister returns the default tag lister backed by the user's gh auth/env.
func NewGitHubTagLister() TagLister {
	return goGHTagLister{}
}

func (goGHTagLister) ListTags(ctx context.Context, repository string) ([]string, error) {
	client, err := api.DefaultRESTClient()
	if err != nil {
		return nil, fmt.Errorf("create GitHub REST client: %w", err)
	}
	return listGitHubTags(ctx, client, repository)
}

func listGitHubTags(ctx context.Context, client githubRESTClient, repository string) ([]string, error) {
	owner, repo, err := parseRepository(repository)
	if err != nil {
		return nil, err
	}
	basePath := fmt.Sprintf("repos/%s/%s/tags", url.PathEscape(owner), url.PathEscape(repo))
	tags := make([]string, 0)
	for page := 1; ; page++ {
		var response []githubTag
		path := fmt.Sprintf("%s?per_page=100&page=%d", basePath, page)
		if err := client.DoWithContext(ctx, http.MethodGet, path, nil, &response); err != nil {
			return nil, fmt.Errorf("list GitHub tags for %s: %w", repository, err)
		}
		for _, tag := range response {
			if tag.Name != "" {
				tags = append(tags, tag.Name)
			}
		}
		if len(response) < 100 {
			break
		}
	}
	return tags, nil
}

var githubNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func parseRepository(repository string) (owner, repo string, err error) {
	parts := strings.Split(strings.Trim(repository, "/"), "/")
	if len(parts) != 2 || !validGitHubName(parts[0]) || !validGitHubName(parts[1]) {
		return "", "", fmt.Errorf("invalid GitHub repository %q, want owner/repo", repository)
	}
	return parts[0], parts[1], nil
}

func validGitHubName(name string) bool {
	return name != "." && name != ".." && githubNameRe.MatchString(name)
}

// ErrFileNotFound reports that a repository file does not exist at a ref.
var ErrFileNotFound = errors.New("file not found")

// FileFetcher reads a single file from a GitHub repository at a ref.
type FileFetcher interface {
	FetchFile(ctx context.Context, repository, path, ref string) ([]byte, error)
}

type goGHFileFetcher struct{}

// NewGitHubFileFetcher returns the default file fetcher backed by the user's gh auth/env.
func NewGitHubFileFetcher() FileFetcher {
	return goGHFileFetcher{}
}

type githubContent struct {
	Type     string `json:"type"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

func (goGHFileFetcher) FetchFile(ctx context.Context, repository, path, ref string) ([]byte, error) {
	client, err := api.DefaultRESTClient()
	if err != nil {
		return nil, fmt.Errorf("create GitHub REST client: %w", err)
	}
	return fetchGitHubFile(ctx, client, repository, path, ref)
}

func fetchGitHubFile(ctx context.Context, client githubRESTClient, repository, path, ref string) ([]byte, error) {
	owner, repo, err := parseRepository(repository)
	if err != nil {
		return nil, err
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	endpoint := fmt.Sprintf("repos/%s/%s/contents/%s?ref=%s", url.PathEscape(owner), url.PathEscape(repo), strings.Join(segments, "/"), url.QueryEscape(ref))
	var raw json.RawMessage
	if err := client.DoWithContext(ctx, http.MethodGet, endpoint, nil, &raw); err != nil {
		var httpErr *api.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%s@%s:%s: %w", repository, ref, path, ErrFileNotFound)
		}
		return nil, fmt.Errorf("fetch %s@%s:%s: %w", repository, ref, path, err)
	}
	// The contents API answers a directory path with a JSON array.
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '[' {
		return nil, fmt.Errorf("%s@%s:%s is a directory, want file: %w", repository, ref, path, ErrFileNotFound)
	}
	var response githubContent
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode %s@%s:%s response: %w", repository, ref, path, err)
	}
	if response.Type != "" && response.Type != "file" {
		return nil, fmt.Errorf("%s@%s:%s is a %s, want file: %w", repository, ref, path, response.Type, ErrFileNotFound)
	}
	if response.Encoding != "base64" {
		return nil, fmt.Errorf("fetch %s@%s:%s: unsupported content encoding %q", repository, ref, path, response.Encoding)
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(response.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("decode %s@%s:%s: %w", repository, ref, path, err)
	}
	return data, nil
}
