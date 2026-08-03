package iac

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeTagLister map[string][]string

func (f fakeTagLister) ListTags(_ context.Context, repository string) ([]string, error) {
	tags, ok := f[repository]
	if !ok {
		return nil, errors.New("unexpected repository " + repository)
	}
	return tags, nil
}

type discardCommenter struct{}

func (discardCommenter) CommentPR(context.Context, string, string, string) error { return nil }

func TestModuleVersionsRequiresIACMarker(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "env", "terragrunt.hcl"), `terraform {
  source = "git::https://github.com/cloudopsworks/terraform-module//main?ref=v1.0.0"
}
`)
	runner := newTestRunner(t, ModuleVersionsOptions{WorkDir: dir, TagLister: fakeTagLister{}})
	err := runner.ModuleVersions(context.Background())
	if err == nil || !strings.Contains(err.Error(), ".cloudopsworks/.iac") {
		t.Fatalf("ModuleVersions() error = %v, want missing marker error", err)
	}
}

func TestReportModeAcceptsMissingGitPrefixWithoutMutating(t *testing.T) {
	dir := newIACWorkspace(t)
	path := filepath.Join(dir, "env", "terragrunt.hcl")
	original := `terraform {
  source = "https://github.com/cloudopsworks/terraform-module//main?ref=v1.0.0"
}
`
	writeFile(t, path, original)

	var out bytes.Buffer
	runner := newTestRunner(t, ModuleVersionsOptions{
		WorkDir:   dir,
		Stdout:    &out,
		TagLister: fakeTagLister{"cloudopsworks/terraform-module": {"v1.0.0", "v1.1.0"}},
	})
	if err := runner.ModuleVersions(context.Background()); err != nil {
		t.Fatalf("ModuleVersions() error = %v", err)
	}
	if got := readFile(t, path); got != original {
		t.Fatalf("report mode mutated file:\n%s", got)
	}
	output := out.String()
	for _, want := range []string{"Missing git:: prefix", "outdated", "Latest:  v1.1.0"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestUpgradeUpdatesRefAndAddsMissingGitPrefix(t *testing.T) {
	dir := newIACWorkspace(t)
	path := filepath.Join(dir, "env", "terragrunt.hcl")
	writeFile(t, path, `terraform {
  source = "https://github.com/cloudopsworks/terraform-module.git//main?ref=v1.0.0"
}
`)

	runner := newTestRunner(t, ModuleVersionsOptions{
		WorkDir:   dir,
		Upgrade:   true,
		TagLister: fakeTagLister{"cloudopsworks/terraform-module": {"v1.0.0", "v1.2.3"}},
	})
	if err := runner.ModuleVersions(context.Background()); err != nil {
		t.Fatalf("ModuleVersions() error = %v", err)
	}
	got := readFile(t, path)
	want := `terraform {
  source = "git::https://github.com/cloudopsworks/terraform-module.git//main?ref=v1.2.3"
}
`
	if got != want {
		t.Fatalf("updated file =\n%s\nwant =\n%s", got, want)
	}
}

func TestUpgradeSelectsAllowedPrereleaseChannels(t *testing.T) {
	tests := []struct {
		name       string
		allowAlpha bool
		allowBeta  bool
		want       string
	}{
		{name: "stable only", want: "v1.2.1"},
		{name: "alpha", allowAlpha: true, want: "v1.3.0-alpha.2"},
		{name: "beta", allowBeta: true, want: "v1.3.0-beta.2"},
		{name: "alpha and beta", allowAlpha: true, allowBeta: true, want: "v1.3.0-beta.2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newIACWorkspace(t)
			path := filepath.Join(dir, "env", "terragrunt.hcl")
			writeFile(t, path, `terraform {
  source = "git::https://github.com/cloudopsworks/terraform-module.git//main?ref=v1.2.0"
}
`)

			runner := newTestRunner(t, ModuleVersionsOptions{
				WorkDir:    dir,
				Upgrade:    true,
				AllowAlpha: tt.allowAlpha,
				AllowBeta:  tt.allowBeta,
				TagLister: fakeTagLister{"cloudopsworks/terraform-module": {
					"v1.2.1", "v1.3.0-alpha.1", "v1.3.0-alpha.2", "v1.3.0-beta.1", "v1.3.0-beta.2",
					"v1.3.0-rc.1", "v1.3.0-preview.1", "v1.3.0-beta-2", "v1.3.0-alpha.foo",
				}},
			})
			if err := runner.ModuleVersions(context.Background()); err != nil {
				t.Fatalf("ModuleVersions() error = %v", err)
			}
			var got string
			for _, line := range strings.Split(readFile(t, path), "\n") {
				if source, ok := sourceFromLine(line); ok {
					got = ParseModuleSource(source).Ref
					break
				}
			}
			if got != tt.want {
				t.Fatalf("updated ref = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestUpgradeAddsMissingGitPrefixWhenRefAlreadyCurrent(t *testing.T) {
	dir := newIACWorkspace(t)
	path := filepath.Join(dir, "env", "terragrunt.hcl")
	writeFile(t, path, `terraform {
  source = "https://github.com/cloudopsworks/terraform-module//main?ref=v1.2.3"
}
`)

	runner := newTestRunner(t, ModuleVersionsOptions{
		WorkDir:   dir,
		Upgrade:   true,
		TagLister: fakeTagLister{"cloudopsworks/terraform-module": {"v1.2.3"}},
	})
	if err := runner.ModuleVersions(context.Background()); err != nil {
		t.Fatalf("ModuleVersions() error = %v", err)
	}
	if got := readFile(t, path); !strings.Contains(got, `source = "git::https://github.com/cloudopsworks/terraform-module//main?ref=v1.2.3"`) {
		t.Fatalf("upgrade did not add git:: prefix when ref current:\n%s", got)
	}
}

func TestFixPrefixDoesNotUpgradeRef(t *testing.T) {
	dir := newIACWorkspace(t)
	path := filepath.Join(dir, "env", "terragrunt.hcl")
	writeFile(t, path, `terraform {
  source = "https://github.com/cloudopsworks/terraform-module//main?ref=v1.0.0"
}
`)

	runner := newTestRunner(t, ModuleVersionsOptions{
		WorkDir:   dir,
		FixPrefix: true,
		TagLister: fakeTagLister{"cloudopsworks/terraform-module": {"v1.0.0", "v1.2.3"}},
	})
	if err := runner.ModuleVersions(context.Background()); err != nil {
		t.Fatalf("ModuleVersions() error = %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, `git::https://github.com/cloudopsworks/terraform-module//main?ref=v1.0.0`) {
		t.Fatalf("fix-prefix should add git:: and keep ref unchanged:\n%s", got)
	}
	if strings.Contains(got, "v1.2.3") {
		t.Fatalf("fix-prefix unexpectedly upgraded ref:\n%s", got)
	}
}

func TestDryRunDoesNotWriteMutations(t *testing.T) {
	dir := newIACWorkspace(t)
	path := filepath.Join(dir, "env", "terragrunt.hcl")
	original := `terraform {
  source = "https://github.com/cloudopsworks/terraform-module//main?ref=v1.0.0"
}
`
	writeFile(t, path, original)

	var out bytes.Buffer
	runner := newTestRunner(t, ModuleVersionsOptions{
		WorkDir:   dir,
		Upgrade:   true,
		DryRun:    true,
		Stdout:    &out,
		TagLister: fakeTagLister{"cloudopsworks/terraform-module": {"v1.0.0", "v1.2.3"}},
	})
	if err := runner.ModuleVersions(context.Background()); err != nil {
		t.Fatalf("ModuleVersions() error = %v", err)
	}
	if got := readFile(t, path); got != original {
		t.Fatalf("dry-run mutated file:\n%s", got)
	}
	if !strings.Contains(out.String(), "Dry-run: would update") {
		t.Fatalf("dry-run output missing mutation intent:\n%s", out.String())
	}
}

func TestUnsupportedSourcesAreReportedAndNotMutated(t *testing.T) {
	dir := newIACWorkspace(t)
	path := filepath.Join(dir, "env", "terragrunt.hcl")
	original := `terraform {
  source = "terraform-aws-modules/vpc/aws"
}
`
	writeFile(t, path, original)

	var out bytes.Buffer
	runner := newTestRunner(t, ModuleVersionsOptions{
		WorkDir:   dir,
		Upgrade:   true,
		Stdout:    &out,
		TagLister: fakeTagLister{},
	})
	if err := runner.ModuleVersions(context.Background()); err != nil {
		t.Fatalf("ModuleVersions() error = %v", err)
	}
	if got := readFile(t, path); got != original {
		t.Fatalf("unsupported source mutated:\n%s", got)
	}
	if !strings.Contains(out.String(), "registry-source") {
		t.Fatalf("unsupported source not reported clearly:\n%s", out.String())
	}
}

func TestSearchPathDoesNotReplaceWorkdirMarkerGuard(t *testing.T) {
	workdir := t.TempDir()
	writeFile(t, filepath.Join(workdir, "search", ".cloudopsworks", ".iac"), "")
	writeFile(t, filepath.Join(workdir, "search", "terragrunt.hcl"), `terraform {
  source = "terraform-aws-modules/vpc/aws"
}
`)

	runner := newTestRunner(t, ModuleVersionsOptions{
		WorkDir:    workdir,
		SearchPath: "search",
		TagLister:  fakeTagLister{},
	})
	err := runner.ModuleVersions(context.Background())
	if err == nil || !strings.Contains(err.Error(), ".cloudopsworks/.iac") {
		t.Fatalf("ModuleVersions() error = %v, want missing marker at workdir", err)
	}
}

func TestSearchPathStartsDiscoveryWithoutChangingWorkdir(t *testing.T) {
	dir := newIACWorkspace(t)
	writeFile(t, filepath.Join(dir, "terragrunt.hcl"), `terraform {
  source = "git::https://github.com/cloudopsworks/root-module//main?ref=v1.0.0"
}
`)
	writeFile(t, filepath.Join(dir, "env", "terragrunt.hcl"), `terraform {
  source = "terraform-aws-modules/vpc/aws"
}
`)

	var out bytes.Buffer
	runner := newTestRunner(t, ModuleVersionsOptions{
		WorkDir:    dir,
		SearchPath: "env",
		Stdout:     &out,
		TagLister:  fakeTagLister{},
	})
	if err := runner.ModuleVersions(context.Background()); err != nil {
		t.Fatalf("ModuleVersions() error = %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "Processing: env/terragrunt.hcl") {
		t.Fatalf("search path output missing env terragrunt processing:\n%s", output)
	}
	if strings.Contains(output, "root-module") || strings.Contains(output, "Processing: terragrunt.hcl") {
		t.Fatalf("search path fell back to workdir scan:\n%s", output)
	}
}

func TestSearchPathOutsideWorkdirIsRejected(t *testing.T) {
	dir := newIACWorkspace(t)
	writeFile(t, filepath.Join(dir, "terragrunt.hcl"), `terraform {
  source = "terraform-aws-modules/vpc/aws"
}
`)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "terragrunt.hcl"), `terraform {
  source = "terraform-aws-modules/vpc/aws"
}
`)

	_, err := NewRunner(ModuleVersionsOptions{
		WorkDir:    dir,
		SearchPath: outside,
		Stdout:     io.Discard,
		Stderr:     io.Discard,
		TagLister:  fakeTagLister{},
		Commenter:  discardCommenter{},
	})
	if err == nil {
		t.Fatalf("NewRunner() succeeded for outside --path")
	}
	if !strings.Contains(err.Error(), "resolved --path") || !strings.Contains(err.Error(), "outside --workdir") {
		t.Fatalf("NewRunner() error = %v, want outside workdir message", err)
	}
}

func TestLatestSemverTagSortsNumerically(t *testing.T) {
	tags := []string{"v1.9.0", "v1.10.0", "v1.2.10", "not-semver", "2.0.0"}
	if got := LatestSemverTag(tags); got != "2.0.0" {
		t.Fatalf("LatestSemverTag() = %s, want 2.0.0", got)
	}
}

func TestLatestSemverTagWithChannelsUsesSemverPrecedence(t *testing.T) {
	tags := []string{
		"v1.2.1",
		"v1.3.0-alpha.1",
		"v1.3.0-alpha.2",
		"v1.3.0-beta.1",
		"v1.3.0-beta.2",
		"v1.3.0",
		"v1.4.0-rc.1",
		"v9.9.9+build.1",
	}

	if got := LatestSemverTag(tags); got != "v1.3.0" {
		t.Fatalf("LatestSemverTag() = %s, want v1.3.0", got)
	}
	if got := LatestSemverTagWithChannels(tags, true, false); got != "v1.3.0" {
		t.Fatalf("alpha selection = %s, want v1.3.0", got)
	}
	if got := LatestSemverTagWithChannels([]string{"v1.2.1", "v1.3.0-alpha.2"}, true, false); got != "v1.3.0-alpha.2" {
		t.Fatalf("alpha selection = %s, want v1.3.0-alpha.2", got)
	}
	if got := LatestSemverTagWithChannels([]string{"v1.2.1", "v1.3.0-beta.2"}, false, true); got != "v1.3.0-beta.2" {
		t.Fatalf("beta selection = %s, want v1.3.0-beta.2", got)
	}
	if got := LatestSemverTagWithChannels(tags, true, true); got != "v1.3.0" {
		t.Fatalf("both-channel selection = %s, want v1.3.0", got)
	}
}

func TestLatestSemverTagWithChannelsRejectsNonExactChannels(t *testing.T) {
	tags := []string{
		"v01.3.0",
		"v1.3.0-alpha.01",
		"v1.3.0-alpha.foo",
		"v1.3.0-beta-2",
		"v1.3.0-rc.1",
		"v1.3.0-preview.1",
		"v1.3.0-a.1",
		"v1.3.0-b.1",
	}
	if got := LatestSemverTagWithChannels(tags, true, true); got != "" {
		t.Fatalf("invalid prerelease selection = %s, want empty", got)
	}
}

func TestParseModuleSourceSupportsOnlyDirectGitHubHTTPSRefPins(t *testing.T) {
	supported := ParseModuleSource("https://github.com/org/repo.git//subdir?ref=v1.0.0")
	if !supported.Supported || supported.Repository != "org/repo" || supported.Subdir != "//subdir" || !supported.PrefixFixNeeded {
		t.Fatalf("supported parse = %+v", supported)
	}
	ssh := ParseModuleSource("git::ssh://git@github.com/org/repo.git//subdir?ref=v1.0.0")
	if ssh.Supported {
		t.Fatalf("ssh source parsed as supported: %+v", ssh)
	}
	registry := ParseModuleSource("terraform-aws-modules/vpc/aws")
	if registry.Kind != kindRegistry {
		t.Fatalf("registry kind = %+v", registry)
	}
}

func newIACWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".cloudopsworks", ".iac"), "")
	return dir
}

func newTestRunner(t *testing.T, opts ModuleVersionsOptions) *Runner {
	t.Helper()
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.Commenter == nil {
		opts.Commenter = discardCommenter{}
	}
	runner, err := NewRunner(opts)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return runner
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}
