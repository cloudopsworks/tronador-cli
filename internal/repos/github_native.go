package repos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/cli/go-gh/v2/pkg/api"
)

type goGHClient struct{}

type githubRESTClient interface {
	DoWithContext(ctx context.Context, method string, path string, body io.Reader, response interface{}) error
}

type githubTag struct {
	Name string `json:"name"`
}

func (goGHClient) ListTags(ctx context.Context, repository string) ([]string, error) {
	client, err := api.DefaultRESTClient()
	if err != nil {
		return nil, fmt.Errorf("create GitHub REST client: %w", err)
	}
	return listGitHubTags(ctx, client, repository)
}

func listGitHubTags(ctx context.Context, client githubRESTClient, repository string) ([]string, error) {
	owner, repo, err := parseOwnerRepo(repository)
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
