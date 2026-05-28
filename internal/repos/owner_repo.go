package repos

import (
	"fmt"
	"net/url"
	"strings"
)

func parseOwnerRepo(raw string) (string, string, error) {
	remote := strings.TrimSpace(raw)
	if remote == "" {
		return "", "", fmt.Errorf("empty repository remote")
	}

	path := remote
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err != nil {
			return "", "", fmt.Errorf("parse repository remote %q: %w", raw, err)
		}
		path = parsed.Path
	} else if strings.Contains(remote, "@") && strings.Contains(remote, ":") {
		parts := strings.SplitN(remote, ":", 2)
		path = parts[1]
	}

	path = strings.TrimSpace(path)
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("cannot parse repository owner/name from %q", raw)
	}
	owner := strings.TrimSpace(parts[len(parts)-2])
	repo := strings.TrimSpace(parts[len(parts)-1])
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("cannot parse repository owner/name from %q", raw)
	}
	return owner, repo, nil
}
