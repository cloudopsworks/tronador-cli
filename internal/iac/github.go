package iac

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

func parseRepository(repository string) (owner, repo string, err error) {
	parts := strings.Split(strings.Trim(repository, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid GitHub repository %q, want owner/repo", repository)
	}
	return parts[0], parts[1], nil
}
