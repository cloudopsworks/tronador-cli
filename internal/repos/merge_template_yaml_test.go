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
	if !contains(result.Unmapped, "extra") {
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

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
