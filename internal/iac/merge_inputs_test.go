package iac

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
)

type fetchCall struct {
	repository, path, ref string
}

type fakeFileFetcher struct {
	files map[string]string // key: repository@ref:path
	err   error
	calls []fetchCall
}

func (f *fakeFileFetcher) FetchFile(_ context.Context, repository, path, ref string) ([]byte, error) {
	f.calls = append(f.calls, fetchCall{repository, path, ref})
	if f.err != nil {
		return nil, f.err
	}
	content, ok := f.files[repository+"@"+ref+":"+path]
	if !ok {
		return nil, ErrFileNotFound
	}
	return []byte(content), nil
}

const mergeTestHCL = `terraform {
  source = "git::https://github.com/cloudopsworks/terraform-module.git//?ref=v1.0.0"
}
`

const mergeTestBoilerplate = `# Module configuration
# Required variables
domain_zone: "example.com" # (Required) Primary DNS zone.
#domain_alias: "app"       # (Optional) Host label. Default: ""
#private_zone: false       # (Optional) Private zone. Default: false
#new_option: true          # (Optional) Added in v1.0.1. Default: true
`

const mergeTestLocal = `# Module configuration
# Required variables
domain_zone: "cloudops.works"
domain_alias: "api"
`

func newMergeWorkspace(t *testing.T, withInputs bool) (dir, hcl, inputs string) {
	t.Helper()
	dir = newIACWorkspace(t)
	hcl = filepath.Join(dir, "env", "mod", "terragrunt.hcl")
	inputs = filepath.Join(dir, "env", "mod", "inputs.yaml")
	writeFile(t, hcl, mergeTestHCL)
	if withInputs {
		writeFile(t, inputs, mergeTestLocal)
	}
	return dir, hcl, inputs
}

func runMerge(t *testing.T, opts ModuleVersionsOptions) string {
	t.Helper()
	var out bytes.Buffer
	opts.Stdout = &out
	if opts.TagLister == nil {
		opts.TagLister = fakeTagLister{"cloudopsworks/terraform-module": {"v1.0.0", "v1.0.1"}}
	}
	runner := newTestRunner(t, opts)
	if err := runner.ModuleVersions(context.Background()); err != nil {
		t.Fatalf("ModuleVersions() error = %v\n%s", err, out.String())
	}
	return out.String()
}

func TestMergeInputsUsesUpgradedRefBoilerplate(t *testing.T) {
	dir, hcl, inputs := newMergeWorkspace(t, true)
	fetcher := &fakeFileFetcher{files: map[string]string{
		"cloudopsworks/terraform-module@v1.0.1:.boilerplate/inputs.yaml": mergeTestBoilerplate,
	}}
	out := runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, FileFetcher: fetcher})

	if len(fetcher.calls) != 1 || fetcher.calls[0].ref != "v1.0.1" || fetcher.calls[0].path != ".boilerplate/inputs.yaml" {
		t.Fatalf("fetch calls = %+v, want one call for v1.0.1 .boilerplate/inputs.yaml", fetcher.calls)
	}
	if !strings.Contains(readFile(t, hcl), "ref=v1.0.1") {
		t.Fatalf("terragrunt.hcl not upgraded:\n%s", readFile(t, hcl))
	}
	got := readFile(t, inputs)
	for _, want := range []string{`domain_zone: "cloudops.works"`, `domain_alias: "api"`, `#new_option: true`, `#private_zone: false`} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged inputs.yaml missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `#domain_alias: "app"`) {
		t.Fatalf("merged inputs.yaml kept the inactive duplicate of a configured key:\n%s", got)
	}
	if !strings.Contains(out, "Merged env/mod/inputs.yaml with cloudopsworks/terraform-module@v1.0.1 .boilerplate/inputs.yaml") {
		t.Fatalf("output missing merge confirmation:\n%s", out)
	}

	// A second run against the same boilerplate is a no-op.
	out = runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, FileFetcher: fetcher,
		TagLister: fakeTagLister{"cloudopsworks/terraform-module": {"v1.0.0", "v1.0.1"}}})
	if readFile(t, inputs) != got {
		t.Fatalf("second merge changed inputs.yaml:\n%s\nwant\n%s", readFile(t, inputs), got)
	}
	if !strings.Contains(out, "already matches") {
		t.Fatalf("second run output missing no-op notice:\n%s", out)
	}
}

func TestMergeInputsUsesCurrentRefWhenUpToDate(t *testing.T) {
	dir, _, inputs := newMergeWorkspace(t, true)
	fetcher := &fakeFileFetcher{files: map[string]string{
		"cloudopsworks/terraform-module@v1.0.0:.boilerplate/inputs.yaml": mergeTestBoilerplate,
	}}
	runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, FileFetcher: fetcher,
		TagLister: fakeTagLister{"cloudopsworks/terraform-module": {"v1.0.0"}}})
	if len(fetcher.calls) != 1 || fetcher.calls[0].ref != "v1.0.0" {
		t.Fatalf("fetch calls = %+v, want current ref v1.0.0", fetcher.calls)
	}
	if !strings.Contains(readFile(t, inputs), "#new_option: true") {
		t.Fatalf("inputs.yaml not merged:\n%s", readFile(t, inputs))
	}
}

func TestMergeInputsWarnsWhenInputsMissing(t *testing.T) {
	dir, hcl, inputs := newMergeWorkspace(t, false)
	fetcher := &fakeFileFetcher{}
	out := runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, FileFetcher: fetcher})

	if !strings.Contains(out, "No inputs.yaml found for env/mod/terragrunt.hcl") {
		t.Fatalf("output missing missing-inputs warning:\n%s", out)
	}
	if len(fetcher.calls) != 0 {
		t.Fatalf("boilerplate fetched without a local inputs.yaml: %+v", fetcher.calls)
	}
	if _, err := os.Stat(inputs); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inputs.yaml was created: %v", err)
	}
	if !strings.Contains(readFile(t, hcl), "ref=v1.0.1") {
		t.Fatalf("terragrunt.hcl not upgraded:\n%s", readFile(t, hcl))
	}
}

func TestMergeInputsWarnsAndSkipsGoTemplateBoilerplate(t *testing.T) {
	dir, hcl, inputs := newMergeWorkspace(t, true)
	fetcher := &fakeFileFetcher{files: map[string]string{
		"cloudopsworks/terraform-module@v1.0.1:.boilerplate/inputs.yaml": "# Module configuration\n{{- range .requiredVariables }}\n{{ .Name }}: {{ .DefaultValuePlaceholder }}\n{{- end }}\n",
	}}
	out := runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, FileFetcher: fetcher})

	if !strings.Contains(out, "uses Go templates; Terragrunt scaffolding for env/mod should be generated again") {
		t.Fatalf("output missing scaffolding warning:\n%s", out)
	}
	if got := readFile(t, inputs); got != mergeTestLocal {
		t.Fatalf("inputs.yaml changed:\n%s", got)
	}
	if !strings.Contains(readFile(t, hcl), "ref=v1.0.1") {
		t.Fatalf("terragrunt.hcl not upgraded:\n%s", readFile(t, hcl))
	}
}

func TestMergeInputsWarnsWhenBoilerplateMissingOrUnfetchable(t *testing.T) {
	for name, fetcher := range map[string]*fakeFileFetcher{
		"missing":   {files: map[string]string{}},
		"api-error": {err: errors.New("boom")},
	} {
		t.Run(name, func(t *testing.T) {
			dir, hcl, inputs := newMergeWorkspace(t, true)
			out := runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, FileFetcher: fetcher})
			want := map[string]string{"missing": "cloudopsworks/terraform-module@v1.0.1 .boilerplate/inputs.yaml not found; env/mod/inputs.yaml left unchanged",
				"api-error": "Failed to fetch cloudopsworks/terraform-module@v1.0.1 .boilerplate/inputs.yaml: boom; env/mod/inputs.yaml left unchanged"}[name]
			if !strings.Contains(out, want) {
				t.Fatalf("output missing %q:\n%s", want, out)
			}
			if got := readFile(t, inputs); got != mergeTestLocal {
				t.Fatalf("inputs.yaml changed:\n%s", got)
			}
			if !strings.Contains(readFile(t, hcl), "ref=v1.0.1") {
				t.Fatalf("terragrunt.hcl not upgraded:\n%s", readFile(t, hcl))
			}
		})
	}
}

func TestMergeInputsWarnsOnInvalidLocalYAML(t *testing.T) {
	dir, _, inputs := newMergeWorkspace(t, true)
	invalid := "a: 1\na: 2\n"
	writeFile(t, inputs, invalid)
	fetcher := &fakeFileFetcher{files: map[string]string{
		"cloudopsworks/terraform-module@v1.0.1:.boilerplate/inputs.yaml": mergeTestBoilerplate,
	}}
	out := runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, FileFetcher: fetcher})
	if !strings.Contains(out, "Could not merge env/mod/inputs.yaml") {
		t.Fatalf("output missing merge failure warning:\n%s", out)
	}
	if got := readFile(t, inputs); got != invalid {
		t.Fatalf("inputs.yaml changed:\n%s", got)
	}
}

func TestMergeInputsDryRunDoesNotWrite(t *testing.T) {
	dir, hcl, inputs := newMergeWorkspace(t, true)
	fetcher := &fakeFileFetcher{files: map[string]string{
		"cloudopsworks/terraform-module@v1.0.1:.boilerplate/inputs.yaml": mergeTestBoilerplate,
	}}
	out := runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, DryRun: true, FileFetcher: fetcher})
	if !strings.Contains(out, "Dry-run: would merge env/mod/inputs.yaml with cloudopsworks/terraform-module@v1.0.1") {
		t.Fatalf("output missing dry-run notice:\n%s", out)
	}
	if got := readFile(t, inputs); got != mergeTestLocal {
		t.Fatalf("dry-run changed inputs.yaml:\n%s", got)
	}
	if got := readFile(t, hcl); got != mergeTestHCL {
		t.Fatalf("dry-run changed terragrunt.hcl:\n%s", got)
	}
}

func TestMergeInputsDisabledByDefault(t *testing.T) {
	dir, _, inputs := newMergeWorkspace(t, true)
	fetcher := &fakeFileFetcher{}
	runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, FileFetcher: fetcher})
	if len(fetcher.calls) != 0 {
		t.Fatalf("boilerplate fetched without --merge-inputs: %+v", fetcher.calls)
	}
	if got := readFile(t, inputs); got != mergeTestLocal {
		t.Fatalf("inputs.yaml changed:\n%s", got)
	}
}

func TestMergeInputsRequiresUpgrade(t *testing.T) {
	_, err := NewRunner(ModuleVersionsOptions{WorkDir: t.TempDir(), MergeInputs: true})
	if err == nil || !strings.Contains(err.Error(), "--merge-inputs requires --upgrade") {
		t.Fatalf("NewRunner() error = %v, want --merge-inputs requires --upgrade", err)
	}
}

type fakeRESTClient struct {
	path string
	err  error
	body githubContent
}

func (f *fakeRESTClient) DoWithContext(_ context.Context, _ string, path string, _ io.Reader, response interface{}) error {
	f.path = path
	if f.err != nil {
		return f.err
	}
	data, _ := json.Marshal(f.body)
	return json.Unmarshal(data, response)
}

func TestFetchGitHubFileDecodesContent(t *testing.T) {
	content := base64.StdEncoding.EncodeToString([]byte("key: value\n"))
	client := &fakeRESTClient{body: githubContent{Type: "file", Encoding: "base64", Content: content[:4] + "\n" + content[4:]}}
	got, err := fetchGitHubFile(context.Background(), client, "cloudopsworks/terraform-module", ".boilerplate/inputs.yaml", "v1.0.0+build")
	if err != nil {
		t.Fatalf("fetchGitHubFile() error = %v", err)
	}
	if string(got) != "key: value\n" {
		t.Fatalf("content = %q", got)
	}
	if want := "repos/cloudopsworks/terraform-module/contents/.boilerplate/inputs.yaml?ref=v1.0.0%2Bbuild"; client.path != want {
		t.Fatalf("path = %q, want %q", client.path, want)
	}
}

func TestFetchGitHubFileMapsNotFound(t *testing.T) {
	client := &fakeRESTClient{err: &api.HTTPError{StatusCode: http.StatusNotFound}}
	_, err := fetchGitHubFile(context.Background(), client, "cloudopsworks/terraform-module", ".boilerplate/inputs.yaml", "v1.0.0")
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("error = %v, want ErrFileNotFound", err)
	}
	client = &fakeRESTClient{body: githubContent{Type: "dir"}}
	_, err = fetchGitHubFile(context.Background(), client, "cloudopsworks/terraform-module", ".boilerplate/inputs.yaml", "v1.0.0")
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("directory error = %v, want ErrFileNotFound", err)
	}
}

func TestMergeInputsScopesBoilerplateToSubdirectory(t *testing.T) {
	dir, hcl, _ := newMergeWorkspace(t, true)
	writeFile(t, hcl, strings.Replace(mergeTestHCL, ".git//?ref", ".git//modules/dns?ref", 1))
	fetcher := &fakeFileFetcher{files: map[string]string{
		"cloudopsworks/terraform-module@v1.0.1:modules/dns/.boilerplate/inputs.yaml": mergeTestBoilerplate,
	}}
	runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, FileFetcher: fetcher})
	if len(fetcher.calls) != 1 || fetcher.calls[0].path != "modules/dns/.boilerplate/inputs.yaml" {
		t.Fatalf("fetch calls = %+v, want subdirectory boilerplate", fetcher.calls)
	}
}

func TestModuleBoilerplatePath(t *testing.T) {
	for subdir, want := range map[string]string{
		"":              ".boilerplate/inputs.yaml",
		"//":            ".boilerplate/inputs.yaml",
		"//modules/a":   "modules/a/.boilerplate/inputs.yaml",
		"//modules/a/":  "modules/a/.boilerplate/inputs.yaml",
		"//./modules/a": "modules/a/.boilerplate/inputs.yaml",
	} {
		if got, ok := moduleBoilerplatePath(subdir); !ok || got != want {
			t.Fatalf("moduleBoilerplatePath(%q) = %q, %v; want %q", subdir, got, ok, want)
		}
	}
	for _, subdir := range []string{"//..", "//../x", "//a/../../x"} {
		if got, ok := moduleBoilerplatePath(subdir); ok {
			t.Fatalf("moduleBoilerplatePath(%q) = %q, want rejection", subdir, got)
		}
	}
}

func TestMergeInputsRequiresExactlyOneSupportedSource(t *testing.T) {
	for name, hclContent := range map[string]string{
		"none":     "terraform {\n  source = \"../local\"\n}\n",
		"multiple": mergeTestHCL + strings.Replace(mergeTestHCL, "terraform-module.git", "terraform-module.git//x", 1),
		"no-line":  "include \"root\" {\n  path = find_in_parent_folders()\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir, hcl, inputs := newMergeWorkspace(t, true)
			writeFile(t, hcl, hclContent)
			fetcher := &fakeFileFetcher{}
			out := runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, FileFetcher: fetcher})
			if !strings.Contains(out, "needs exactly one supported GitHub module source") {
				t.Fatalf("output missing source warning:\n%s", out)
			}
			if len(fetcher.calls) != 0 || readFile(t, inputs) != mergeTestLocal {
				t.Fatalf("merge ran: calls=%+v inputs=\n%s", fetcher.calls, readFile(t, inputs))
			}
		})
	}
}

func TestMergeInputsSkipsSymlinkedInputs(t *testing.T) {
	dir, _, inputs := newMergeWorkspace(t, false)
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	writeFile(t, outside, mergeTestLocal)
	if err := os.Symlink(outside, inputs); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	fetcher := &fakeFileFetcher{}
	out := runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, FileFetcher: fetcher})
	if !strings.Contains(out, "is not a regular file") || len(fetcher.calls) != 0 {
		t.Fatalf("symlinked inputs.yaml not skipped (calls=%+v):\n%s", fetcher.calls, out)
	}
	if readFile(t, outside) != mergeTestLocal {
		t.Fatal("symlink target changed")
	}
}

func TestMergeInputsPreservesModeAndWarnsOnAddedValues(t *testing.T) {
	dir, _, inputs := newMergeWorkspace(t, true)
	if err := os.Chmod(inputs, 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher := &fakeFileFetcher{files: map[string]string{
		"cloudopsworks/terraform-module@v1.0.1:.boilerplate/inputs.yaml": mergeTestBoilerplate + "required_new: \"CHANGEME\" # (Required) New in v1.0.1\n",
	}}
	out := runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, FileFetcher: fetcher})
	if !strings.Contains(out, "WARNING adding boilerplate value env/mod/inputs.yaml path required_new") {
		t.Fatalf("output missing added-value warning:\n%s", out)
	}
	info, err := os.Stat(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(inputs))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestMergeInputsWarnsWhenLocalHasNoActiveValues(t *testing.T) {
	dir, _, inputs := newMergeWorkspace(t, true)
	writeFile(t, inputs, "# nothing configured\n")
	fetcher := &fakeFileFetcher{files: map[string]string{
		"cloudopsworks/terraform-module@v1.0.1:.boilerplate/inputs.yaml": mergeTestBoilerplate,
	}}
	out := runMerge(t, ModuleVersionsOptions{WorkDir: dir, Upgrade: true, MergeInputs: true, FileFetcher: fetcher})
	if !strings.Contains(out, "has no active values; replacing it with the boilerplate baseline") {
		t.Fatalf("output missing comment-only warning:\n%s", out)
	}
	if !strings.Contains(out, "path domain_zone; review it before applying") {
		t.Fatalf("output missing added-value warning:\n%s", out)
	}
}

func TestFetchGitHubFileErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := fetchGitHubFile(ctx, &fakeRESTClient{err: &api.HTTPError{StatusCode: http.StatusForbidden}}, "o/r", "f", "v1"); err == nil || errors.Is(err, ErrFileNotFound) {
		t.Fatalf("403 error = %v, want non-not-found error", err)
	}
	if _, err := fetchGitHubFile(ctx, &fakeRESTClient{body: githubContent{Type: "file", Encoding: "none"}}, "o/r", "f", "v1"); err == nil || !strings.Contains(err.Error(), "unsupported content encoding") {
		t.Fatalf("encoding none error = %v", err)
	}
	if _, err := fetchGitHubFile(ctx, &fakeRESTClient{body: githubContent{Type: "file", Encoding: "base64", Content: "!!!"}}, "o/r", "f", "v1"); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("bad base64 error = %v", err)
	}
	if _, err := fetchGitHubFile(ctx, &rawRESTClient{raw: `[{"name":"inputs.yaml"}]`}, "o/r", "f", "v1"); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("directory listing error = %v, want ErrFileNotFound", err)
	}
	for _, repository := range []string{"x/..", "../r", "o/r/extra", "o/", "o/r?x"} {
		if _, err := fetchGitHubFile(ctx, &fakeRESTClient{}, repository, "f", "v1"); err == nil {
			t.Fatalf("repository %q accepted", repository)
		}
	}
}

type rawRESTClient struct{ raw string }

func (c *rawRESTClient) DoWithContext(_ context.Context, _ string, _ string, _ io.Reader, response interface{}) error {
	return json.Unmarshal([]byte(c.raw), response)
}
