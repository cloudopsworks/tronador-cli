package repos

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestStripStaleTemplateComments(t *testing.T) {
	previous := "goreleaser:\n" +
		"#  chocolatey: true    # needs .goreleaser-windows.yaml\n" +
		"region: dev # old region note\n" +
		"legacy: x # deprecated: use region\n" +
		"stage_variables:  # old stage wording\n" +
		"schedule: x # Enable the standalone; old wording\n"
	target := "goreleaser:\n" +
		"#  chocolatey: true    # needs windows.goreleaser.yml\n" +
		"region: dev # deployment region\n" +
		"stage_variables:  # comment out if not needed\n" +
		"schedule: x # Enable schedule; can be combined\n"
	owned := templateOwnedCommentFragments(map[string][][]byte{"vars/inputs-global.yaml": {[]byte(previous), []byte(target)}})
	for _, tc := range []struct{ name, merged, want string }{
		{
			name:   "stale_wording_removed_alignment_kept",
			merged: "goreleaser:\n  chocolatey: true    # needs windows.goreleaser.yml; # needs .goreleaser-windows.yaml\n",
			want:   "goreleaser:\n  chocolatey: true    # needs windows.goreleaser.yml\n",
		},
		{
			name:   "operator_note_kept",
			merged: "goreleaser:\n  chocolatey: true # needs windows.goreleaser.yml; # needs .goreleaser-windows.yaml; # signed by ops\n",
			want:   "goreleaser:\n  chocolatey: true # needs windows.goreleaser.yml; # signed by ops\n",
		},
		{
			// The renderer keeps some lines (such as mapping headers) verbatim
			// and never re-applies the target comment. Wording the template
			// still uses must survive there.
			name:   "verbatim_line_with_current_wording_untouched",
			merged: "stage_variables:  # comment out if not needed\n  A: b\n",
			want:   "stage_variables:  # comment out if not needed\n  A: b\n",
		},
		{
			// A verbatim-kept line whose local comment already starts with
			// the current wording still gets its appended stale fragment
			// removed: the prefix ends at a fragment boundary.
			name:   "verbatim_line_with_current_wording_and_stale_fragment_cleaned",
			merged: "stage_variables:  # comment out if not needed; # old stage wording\n  A: b\n",
			want:   "stage_variables:  # comment out if not needed\n  A: b\n",
		},
		{
			name:   "verbatim_line_with_stale_wording_untouched",
			merged: "goreleaser:\n  chocolatey: true # needs .goreleaser-windows.yaml\n",
			want:   "goreleaser:\n  chocolatey: true # needs .goreleaser-windows.yaml\n",
		},
		{
			// legacy is gone from the target: its old wording is the only
			// remaining documentation for a key the operator still sets.
			name:   "key_dropped_by_target_untouched",
			merged: "legacy: y # deprecated: use region\n",
			want:   "legacy: y # deprecated: use region\n",
		},
		{
			name:   "target_semicolons_kept_verbatim",
			merged: "schedule: x # Enable schedule; can be combined; # old wording\n",
			want:   "schedule: x # Enable schedule; can be combined\n",
		},
		{
			name:   "crlf_and_trailing_newlines",
			merged: "region: prod # deployment region; # old region note\r\n\r\n",
			want:   "region: prod # deployment region\n\n",
		},
		{
			// "deployment region" is a prefix of the operator's word, not a
			// whole fragment, so the line was not built by the merge.
			name:   "target_prefix_inside_operator_word_untouched",
			merged: "region: prod # deployment regionally pinned; # old region note\n",
			want:   "region: prod # deployment regionally pinned; # old region note\n",
		},
		{
			name:   "unrelated_appended_note_untouched",
			merged: "region: prod # deployment region; # our own note\n",
			want:   "region: prod # deployment region; # our own note\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := string(stripStaleTemplateComments([]byte(tc.merged), []byte(target), owned))
			if got != tc.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestStaleTemplateCommentsReplacedOnUpgrade(t *testing.T) {
	previous := "goreleaser:\n" +
		"#  enabled: true       # build release binaries\n" +
		"#  chocolatey: true    # needs .goreleaser-windows.yaml\n" +
		"stage_variables:  # comment out if not needed\n" +
		"  A: b\n"
	target := "goreleaser:\n" +
		"#  enabled: true       # build release binaries\n" +
		"#  chocolatey: true    # needs windows.goreleaser.yml\n" +
		"stage_variables:  # comment out if not needed\n" +
		"  A: b\n"
	owned := templateOwnedCommentFragments(map[string][][]byte{"vars/inputs-global.yaml": {[]byte(previous), []byte(target)}})
	for _, tc := range []struct{ name, local, want string }{
		{
			name:  "stale_template_wording_replaced",
			local: "goreleaser:\n  enabled: true       # build release binaries\n  chocolatey: true    # needs .goreleaser-windows.yaml\nstage_variables:  # comment out if not needed\n  A: c\n",
			want:  "goreleaser:\n  enabled: true       # build release binaries\n  chocolatey: true    # needs windows.goreleaser.yml\nstage_variables:  # comment out if not needed\n  A: c\n",
		},
		{
			name:  "operator_note_survives_after_new_wording",
			local: "goreleaser:\n  enabled: true\n  chocolatey: true # needs .goreleaser-windows.yaml; # signed by ops\nstage_variables:\n  A: c\n",
			want:  "goreleaser:\n  enabled: true       # build release binaries\n  chocolatey: true    # needs windows.goreleaser.yml; # signed by ops\nstage_variables:  # comment out if not needed\n  A: c\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			targetDoc, err := parseYAMLDocument("inputs-global.yaml", []byte(target))
			if err != nil {
				t.Fatal(err)
			}
			localDoc, err := parseYAMLDocument("inputs-global.yaml", []byte(tc.local))
			if err != nil {
				t.Fatal(err)
			}
			content, warnings, err := mergeYAMLDocument(targetDoc, localDoc, "inputs-global.yaml")
			if err != nil || len(warnings) != 0 {
				t.Fatalf("warnings=%v err=%v", warnings, err)
			}
			content = withoutStaleTemplateComments("inputs-global.yaml", content, targetDoc.data, owned)
			if string(content) != tc.want {
				t.Fatalf("got:\n%swant:\n%s", content, tc.want)
			}
		})
	}
}

func TestStripStaleTemplateCommentsUsesEveryHistoricalVersion(t *testing.T) {
	target := []byte("#  chocolatey: true    # needs windows.goreleaser.yml v3\n")
	owned := templateOwnedCommentFragments(map[string][][]byte{"vars/inputs-global.yaml": {
		[]byte("#  chocolatey: true    # needs .goreleaser-windows.yaml\n"),
		[]byte("#  chocolatey: true    # needs windows.goreleaser.yml\n"),
		target,
	}})
	// The local wording lags several releases behind _VERSION: it matches only
	// the oldest historical template, which must still mark it template-owned.
	merged := "chocolatey: true # needs windows.goreleaser.yml v3; # needs .goreleaser-windows.yaml; # signed by ops\n"
	want := "chocolatey: true # needs windows.goreleaser.yml v3; # signed by ops\n"
	if got := string(stripStaleTemplateComments([]byte(merged), target, owned)); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStripStaleTemplateCommentsPoolsWordingAcrossScaffoldFiles(t *testing.T) {
	// The local env file merges against the Beanstalk scaffold but carries
	// the wording the Kubernetes scaffold used on the same key path.
	target := []byte("aws:\n#  build_sts_role_arn: \"ARN\" # Optional: when roles differ\n")
	owned := templateOwnedCommentFragments(map[string][][]byte{
		"vars/inputs-BEANSTALK-ENV.yaml":  {target},
		"vars/inputs-KUBERNETES-ENV.yaml": {[]byte("aws:\n#  build_sts_role_arn: \"ARN\" # Optional: set this if roles differ\n")},
		"vars/inputs-OTHER.yaml":          {[]byte("gcp:\n  build_sts_role_arn: x # unrelated parent wording\n")},
	})
	merged := "aws:\n  build_sts_role_arn: \"arn:x\" # Optional: when roles differ; # Optional: set this if roles differ; # unrelated parent wording\n"
	// Ownership is per structural key path: wording from gcp.build_sts_role_arn
	// does not make the aws key's appended fragment template-owned.
	want := "aws:\n  build_sts_role_arn: \"arn:x\" # Optional: when roles differ; # unrelated parent wording\n"
	if got := string(stripStaleTemplateComments([]byte(merged), target, owned)); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWithoutStaleTemplateCommentsWithoutHistoryIsUnchanged(t *testing.T) {
	merged := []byte("region: prod # deployment region; # old region note\n")
	if got := withoutStaleTemplateComments("inputs-global.yaml", merged, merged, nil); string(got) != string(merged) {
		t.Fatalf("merged changed without history: %q", got)
	}
}

func TestCommentOnlyChange(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		want                bool
	}{
		{"comment_removed", "a: 1 # x; # y\n", "a: 1 # x\n", true},
		{"value_changed", "a: 1 # x\n", "a: 2 # x\n", false},
		{"quoted_value_changed", "a: \"b # c\" # d\n", "a: \"b\n", false},
		{"invalid_before", "a: [\n", "a: 1\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := commentOnlyChange("inputs-global.yaml", []byte(tc.before), []byte(tc.after)); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReadTemplateHistoryConfigReadsEveryReleaseTag(t *testing.T) {
	dir := t.TempDir()
	repository, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	signature := &object.Signature{Name: "test", Email: "test@example.invalid", When: time.Unix(0, 0)}
	commit := func(content, tag string) {
		t.Helper()
		vars := filepath.Join(dir, ".cloudopsworks", "vars")
		if err := os.MkdirAll(vars, 0o755); err != nil {
			t.Fatal(err)
		}
		// A byte-identical file at another path must get its own history.
		for _, name := range []string{"inputs-global.yaml", "inputs-twin.yaml"} {
			if err := os.WriteFile(filepath.Join(vars, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, ".cloudopsworks", "_VERSION"), []byte(tag), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := worktree.Add("."); err != nil {
			t.Fatal(err)
		}
		hash, err := worktree.Commit("template", &git.CommitOptions{Author: signature})
		if err != nil {
			t.Fatal(err)
		}
		if tag == "" {
			return
		}
		// Exercise both lightweight and annotated release tags.
		var opts *git.CreateTagOptions
		if tag == "v1.1.0" {
			opts = &git.CreateTagOptions{Message: tag, Tagger: signature}
		}
		if _, err := repository.CreateTag(tag, hash, opts); err != nil {
			t.Fatal(err)
		}
	}
	commit("region: dev # oldest wording\n", "v1.0.0")
	commit("region: dev # newer wording\n", "v1.1.0")
	commit("region: dev # unreleased wording\n", "")

	runner := &Runner{Config: &Config{TemplateDirectory: "."}, Opts: Options{WorkDir: dir}}
	history := runner.readTemplateHistoryConfig()
	want := map[string]bool{"region: dev # oldest wording\n": true, "region: dev # newer wording\n": true}
	for _, name := range []string{"vars/inputs-global.yaml", "vars/inputs-twin.yaml"} {
		got := make(map[string]bool)
		for _, data := range history[name] {
			got[string(data)] = true
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("history[%s] = %q, want only the two tagged versions", name, history[name])
		}
	}
	if len(history) != 2 {
		t.Fatalf("history paths = %d, want 2", len(history))
	}

	noGit := &Runner{Config: &Config{TemplateDirectory: "."}, Opts: Options{WorkDir: t.TempDir()}}
	if history := noGit.readTemplateHistoryConfig(); history != nil {
		t.Fatalf("history without git metadata = %q, want nil", history)
	}
}

func TestConfigPlanRemovesStaleTemplateCommentsFromAnotherScaffold(t *testing.T) {
	for _, tc := range []struct {
		name  string
		owned bool
		want  string
	}{
		{"with_template_history", true, "aws:\n  build_sts_role_arn: \"arn:local\" # Optional: when roles differ\n"},
		// Control: without history the same plan duplicates, which proves the
		// test exercises the config-plan wiring rather than the renderer alone.
		{"without_template_history", false, "aws:\n  build_sts_role_arn: \"arn:local\" # Optional: when roles differ; # Optional: set this if roles differ\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, runner, _ := configUpgradeRunner(t, false)
			target := "aws:\n#  build_sts_role_arn: \"ARN\" # Optional: when roles differ\n"
			writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-global.yaml", target)
			writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "aws:\n  build_sts_role_arn: \"arn:local\" # Optional: set this if roles differ\n")
			if tc.owned {
				runner.templateOwnedComments = templateOwnedCommentFragments(map[string][][]byte{
					"vars/inputs-global.yaml":         {[]byte(target)},
					"vars/inputs-KUBERNETES-ENV.yaml": {[]byte("aws:\n#  build_sts_role_arn: \"ARN\" # Optional: set this if roles differ\n")},
				})
			}
			state := RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks", VersionFile: ".cloudopsworks/_VERSION"}
			plan, err := runner.buildCloudOpsworksConfigPlan(state)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runner.applyCloudOpsworksConfigPlan(plan); err != nil {
				t.Fatal(err)
			}
			if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "vars", "inputs-global.yaml")); got != tc.want {
				t.Fatalf("got:\n%swant:\n%s", got, tc.want)
			}
		})
	}
}
