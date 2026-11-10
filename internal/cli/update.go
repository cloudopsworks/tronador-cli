package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	updateCheckTimeout = 300 * time.Millisecond
	latestReleaseAPI   = "https://api.github.com/repos/cloudopsworks/tronador-cli/releases/latest"
	latestReleaseURL   = "https://github.com/cloudopsworks/tronador-cli/releases/latest"
)

// Check before Cobra parses arguments: even help and usage errors must follow
// the notice. Stderr keeps version/JSON output on stdout usable by scripts.
func executeWithUpdateNotice(cmd *cobra.Command, version string, client *http.Client, endpoint string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	notifyUpdate(ctx, cmd.ErrOrStderr(), version, client, endpoint)
	return cmd.Execute()
}

// This is deliberately best-effort: no credentials, retries, or error output.
// The deadline covers connecting, redirects, headers, and reading the body.
func notifyUpdate(ctx context.Context, out io.Writer, current string, client *http.Client, endpoint string) {
	installed := releaseVersionParts(current)
	if installed == nil {
		return // Development/unknown builds have no release version to compare.
	}
	ctx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "tronador-cli/"+current)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	const maxBody = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if json.Unmarshal(body, &release) != nil || release.Draft || release.Prerelease {
		return
	}
	latest := releaseVersionParts(release.Tag)
	if latest == nil || latest[4] != "" || !newerStableRelease(latest, installed) {
		return
	}
	fmt.Fprintf(out, "A new version of tronador is available: %s (current: %s). %s\n", release.Tag, current, latestReleaseURL)
}

var releaseVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func releaseVersionParts(version string) []string {
	parts := releaseVersionPattern.FindStringSubmatch(version)
	if parts == nil {
		return nil
	}
	for _, id := range strings.Split(parts[4], ".") {
		if len(id) > 1 && id[0] == '0' && strings.Trim(id, "0123456789") == "" {
			return nil // SemVer numeric prerelease identifiers cannot have leading zeros.
		}
	}
	return parts
}

// The candidate is always stable, so only core numbers and the installed
// prerelease status matter. Compare decimal strings without integer overflow.
func newerStableRelease(latest, installed []string) bool {
	for i := 1; i <= 3; i++ {
		if len(latest[i]) != len(installed[i]) {
			return len(latest[i]) > len(installed[i])
		}
		if latest[i] != installed[i] {
			return latest[i] > installed[i]
		}
	}
	return installed[4] != ""
}
