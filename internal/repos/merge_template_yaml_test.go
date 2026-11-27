package repos

import (
	"reflect"
	"strings"
	"testing"
)

func TestMergeTemplateYAMLOverlaysLocalValuesOnTemplate(t *testing.T) {
	template := `# Header
name: "" # (Required) Name.
#size: 1 # (Optional) Size.
tags: # (Optional) Tags.
  team: platform
`
	local := `name: app
size: 3
extra: true
`
	result, err := MergeTemplateYAML("inputs.yaml", []byte(template), []byte(local))
	if err != nil {
		t.Fatalf("MergeTemplateYAML() error = %v", err)
	}
	got := string(result.Content)
	for _, want := range []string{"# Header", "name: app # (Required) Name.", "size: 3", "extra: true", "team: platform"} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged content missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "#size: 1") {
		t.Fatalf("inactive duplicate of configured key kept:\n%s", got)
	}
	if !reflect.DeepEqual(result.Added, []string{"tags"}) {
		t.Fatalf("Added = %v, want [tags]", result.Added)
	}
	if result.CommentOnly {
		t.Fatal("CommentOnly = true")
	}
	if !reflect.DeepEqual(result.Unmapped, []string{"extra"}) {
		t.Fatalf("Unmapped = %v, want extra", result.Unmapped)
	}

	again, err := MergeTemplateYAML("inputs.yaml", []byte(template), result.Content)
	if err != nil {
		t.Fatalf("second merge error = %v", err)
	}
	if string(again.Content) != got {
		t.Fatalf("merge is not idempotent:\n%s\nwant\n%s", again.Content, got)
	}
}

func TestMergeTemplateYAMLOnlyWarnsForUnmappedPlacements(t *testing.T) {
	template := `#feature:
#  enabled: false # Default flag.
#  timeout: 5
# endpoint: descriptive prose, not a template field
settings:
  known: default
  # legacy: descriptive prose, not a template field
`
	local := `feature:
  enabled: true
  operator_override:
    mode: custom
endpoint: https://example.test
settings:
  known: custom
  legacy: configured
  extra: true
`
	result, err := MergeTemplateYAML("inputs.yaml", []byte(template), []byte(local))
	if err != nil {
		t.Fatal(err)
	}
	if result.RendererFallback {
		t.Fatalf("renderer fell back:\n%s", result.Content)
	}
	want := []string{"feature.operator_override", "endpoint", "settings.legacy", "settings.extra"}
	if !reflect.DeepEqual(result.Unmapped, want) {
		t.Fatalf("Unmapped = %v, want %v", result.Unmapped, want)
	}
}

func TestMergeTemplateYAMLFallbackDoesNotClaimPartialPlacement(t *testing.T) {
	template := "active: true\n#known: false\n#choice: 1\n#choice: 2\n"
	local := "known: true\nchoice: 3\n"
	result, err := MergeTemplateYAML("inputs.yaml", []byte(template), []byte(local))
	if err != nil {
		t.Fatal(err)
	}
	if !result.RendererFallback {
		t.Fatalf("expected an ambiguous candidate to trigger fallback:\n%s", result.Content)
	}
	if !reflect.DeepEqual(result.Unmapped, []string{"known", "choice"}) {
		t.Fatalf("Unmapped = %v, want all paths without a verified placement", result.Unmapped)
	}
}

func TestMergeTemplateYAMLCommentOnlyLocalUsesTemplate(t *testing.T) {
	template := "a: 1\n"
	result, err := MergeTemplateYAML("inputs.yaml", []byte(template), []byte("# a: 2\n"))
	if err != nil {
		t.Fatalf("MergeTemplateYAML() error = %v", err)
	}
	if !result.CommentOnly || string(result.Content) != template {
		t.Fatalf("result = %+v (content %q), want comment-only template copy", result, result.Content)
	}
}

func TestMergeTemplateYAMLRejectsUnsafeYAML(t *testing.T) {
	for name, local := range map[string]string{
		"duplicate": "a: 1\na: 2\n",
		"anchor":    "a: &x 1\nb: *x\n",
		"multidoc":  "a: 1\n---\nb: 2\n",
		"invalid":   "a: [\n",
	} {
		if _, err := MergeTemplateYAML("inputs.yaml", []byte("a: 0\n"), []byte(local)); err == nil {
			t.Fatalf("%s: MergeTemplateYAML() succeeded, want error", name)
		}
	}
}

func TestMergeTemplateYAMLCommentMarkerAlignment(t *testing.T) {
	const nested = "settings:\n  rotation:\n    provider:\n"
	for _, tc := range []struct{ name, prefix, indent, marker string }{
		{"root_hash_no_pad", "", "", "#"},
		{"root_hash_one_pad", "", "", "# "},
		{"indented_hash_no_pad", nested, "      ", "      #"},
		{"indented_hash_one_pad", nested, "      ", "      # "},
		{"column_one_six_spaces", nested, "      ", "#      "},
		{"column_one_seven_spaces", nested, "      ", "#       "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, i := tc.marker, tc.indent
			const annotation = " # (Optional) Low for development; higher for production."
			target := tc.prefix + m + "retries: 3" + annotation + "\n" +
				m + "network:\n" + m + "  subnets:\n" + m + "    - example\n" + m + "  unused: true\n" + i + "region: dev\n"
			local := tc.prefix + i + "region: dev\n" + i + "retries: 7\n" + i + "network:\n" + i + "  subnets:\n" + i + "    - custom\n"
			want := tc.prefix + i + "retries: 7" + annotation + "\n" +
				i + "network:\n" + i + "  subnets:\n" + i + "    - custom\n" + m + "  unused: true\n" + i + "region: dev\n"
			for range 2 {
				result, err := MergeTemplateYAML("inputs.yaml", []byte(target), []byte(local))
				if err != nil || result.RendererFallback || len(result.Unmapped) != 0 || string(result.Content) != want {
					t.Fatalf("fallback=%v, unmapped=%v, err=%v\ngot:\n%swant:\n%s", result.RendererFallback, result.Unmapped, err, result.Content, want)
				}
				local = string(result.Content)
			}
		})
	}
}

func TestMergeTemplateYAMLDoesNotConsumePaddedProse(t *testing.T) {
	target := "settings:\n  # status: see issue #123\n  # reviewed: 2026-01-01\n  region: dev\n"
	local := "settings:\n  region: dev\n  status: active\n  reviewed: today\n"
	result, err := MergeTemplateYAML("inputs.yaml", []byte(target), []byte(local))
	if err != nil || !strings.Contains(string(result.Content), "# status: see issue #123\n  # reviewed: 2026-01-01") ||
		!reflect.DeepEqual(result.Unmapped, []string{"settings.status", "settings.reviewed"}) {
		t.Fatalf("unmapped=%v, err=%v\n%s", result.Unmapped, err, result.Content)
	}
}

func TestMergeTemplateYAMLIndividuallyCommentedChildren(t *testing.T) {
	const target = `settings:
  # enabled: false
  # network:
    # subnets:
      # - default
    # unused: true
`
	const local = `settings:
  enabled: true
  network:
    subnets:
      - custom
`
	const want = local + "    # unused: true\n"
	result, err := MergeTemplateYAML("inputs.yaml", []byte(target), []byte(local))
	if err != nil || result.RendererFallback || len(result.Unmapped) != 0 || string(result.Content) != want {
		t.Fatalf("fallback=%v, unmapped=%v, err=%v\ngot:\n%swant:\n%s", result.RendererFallback, result.Unmapped, err, result.Content, want)
	}
}

func TestMergeTemplateYAMLPreservesInlineCommentAlignment(t *testing.T) {
	target := "goreleaser:           # enable goreleaser\n" +
		"#  enabled: true       # build release binaries\n" +
		"#  chocolatey: true    # windows packages\n" +
		"#  macos: true         # macOS packages\n" +
		"region: dev # single-space annotation\n"
	local := "goreleaser:\n" +
		"  enabled: true # build release binaries\n" +
		"  chocolatey: false\n" +
		"  macos: a-value-wider-than-the-column\n" +
		"region: production\n"
	want := "goreleaser:           # enable goreleaser\n" +
		"  enabled: true       # build release binaries\n" +
		"  chocolatey: false   # windows packages\n" +
		"  macos: a-value-wider-than-the-column # macOS packages\n" +
		"region: production # single-space annotation\n"
	for range 2 {
		result, err := MergeTemplateYAML("inputs-global.yaml", []byte(target), []byte(local))
		if err != nil || result.RendererFallback || len(result.Unmapped) != 0 || string(result.Content) != want {
			t.Fatalf("fallback=%v, unmapped=%v, err=%v\ngot:\n%swant:\n%s", result.RendererFallback, result.Unmapped, err, result.Content, want)
		}
		local = string(result.Content)
	}
}
