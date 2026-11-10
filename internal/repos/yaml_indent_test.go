package repos

import (
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestUpgradeCloudOpsworksConfigEnforcesTwoSpaceIndentation(t *testing.T) {
	for _, baseline := range []string{
		"# documentation\nprobe:\n    path: /default\nlist:\n    - name: default\n      nested:\n          enabled: false\n",
		// Ambiguous inactive alternatives force the semantic renderer fallback.
		"# documentation\n#probe:\n#    path: /one\n#probe:\n#    path: /two\n#list: []\n",
	} {
		t.Run(baseline, func(t *testing.T) {
			dir, runner, _ := configUpgradeRunner(t, false)
			name := ".cloudopsworks/vars/helm/values-dev.yaml"
			writeConfigFixture(t, dir, name, "probe:\n    path: /health\nlist:\n    - name: custom\n      nested:\n          enabled: true\n")
			writeConfigFixture(t, dir, ".template/"+name, baseline)
			state := RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}
			if err := runner.upgradeCloudOpsworksConfig(state); err != nil {
				t.Fatal(err)
			}
			first := mustRead(t, filepath.Join(dir, name))
			for _, want := range []string{"probe:\n  path: /health", "list:\n  - name: custom\n    nested:\n      enabled: true"} {
				if !strings.Contains(first, want) {
					t.Fatalf("missing hard two-space structure %q:\n%s", want, first)
				}
			}
			if err := runner.upgradeCloudOpsworksConfig(state); err != nil {
				t.Fatal(err)
			}
			if second := mustRead(t, filepath.Join(dir, name)); first != second {
				t.Fatalf("normalization is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
			}
		})
	}
}

func TestSanitizeYAMLIndentationPreservesPayloadAndLayout(t *testing.T) {
	for _, test := range []struct{ name, input, want string }{
		{"block hash payload", "root:\n    script: |\n        #!/bin/sh\n        echo ok\n    next: value\n", "root:\n  script: |\n      #!/bin/sh\n      echo ok\n  next: value\n"},
		{"own line scalar", "key:\n    value\nnext: done\n", "key:\n  value\nnext: done\n"},
		{"standalone dashes", "list:\n    -\n        name: one\n    -\n        name: two\n", "list:\n  -\n    name: one\n  -\n    name: two\n"},
		{"quoted hash payload", "root:\n    text: 'first\n        # content\n        last'\n    next: done\n", "root:\n  text: 'first\n      # content\n      last'\n  next: done\n"},
		{"tagged quoted hash payload", "root:\n    text: !!str 'first\n        # content\n        last'\n    next: done\n", "root:\n  text: !!str 'first\n      # content\n      last'\n  next: done\n"},
		{"flow collection", "root:\n    flow: {one: 1, two: [2, 3]}\n", "root:\n  flow: {one: 1, two: [2, 3]}\n"},
		{"indentless sequence", "root:\n    items:\n    - name: one\n      enabled: true\n", "root:\n  items:\n    - name: one\n      enabled: true\n"},
		{"root comments", "root:\n    deep:\n        value: true\n# Heading\nnext: done\n", "root:\n  deep:\n    value: true\n# Heading\nnext: done\n"},
		{"explicit block indent", "root:\n    script: |4-\n        text\n          extra spaces\n", "root:\n  script: |4-\n      text\n        extra spaces\n"},
		{"trailing newlines", "root:\r\n    value: true\r\n\r\n", "root:\n  value: true\n\n"},
		{"explicit mapping", "root:\n    ? key\n    : value\n", "root:\n  ? key\n  : value\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := sanitizeYAMLIndentation([]byte(test.input))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Fatalf("got: %q\nwant: %q", got, test.want)
			}
			second, err := sanitizeYAMLIndentation(got)
			if err != nil || string(second) != string(got) {
				t.Fatalf("second pass differs: %v\n%s", err, second)
			}
		})
	}
}

func TestDeduplicateYAMLRejectsConflictingActiveEntries(t *testing.T) {
	input := []byte("feature: true\nother: value\nfeature: false\n")
	if _, err := deduplicateYAMLTemplateEntries(input, input); err == nil {
		t.Fatal("conflicting active entries were silently accepted")
	}
}

func TestUpgradeCloudOpsworksConfigUsesTemplateSlotsWithoutDuplicates(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	name := ".cloudopsworks/vars/helm/values-dev.yaml"
	writeConfigFixture(t, dir, ".template/"+name, "# Deployment annotations\n#annotations: {}\n#  reloader: default\n# Pod annotations\n#podAnnotations: {}\n#  instrumentation: default\nprobe:\n  path: /default\n")
	writeConfigFixture(t, dir, name, "probe:\n    path: /health\n#annotations: {}\nannotations:\n    reloader: custom\n#podAnnotations: {}\npodAnnotations:\n    instrumentation: custom\n")
	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatal(err)
	}
	got := mustRead(t, filepath.Join(dir, name))
	want := "# Deployment annotations\nannotations:\n  reloader: custom\n# Pod annotations\npodAnnotations:\n  instrumentation: custom\nprobe:\n  path: /health\n"
	if got != want {
		t.Fatalf("template placement/duplicates changed:\n%s\nwant:\n%s", got, want)
	}
}

func TestDeduplicateYAMLTemplateEntries(t *testing.T) {
	for _, test := range []struct{ name, input, want string }{
		{"active default", "# heading\nannotations:\n  value: custom\nnext: true\n#annotations: {}\n", "# heading\nannotations:\n  value: custom\nnext: true\n"},
		{"repeated inactive", "#feature:\n#  enabled: false\nnext: true\n#feature:\n#  enabled: false\n", "#feature:\n#  enabled: false\nnext: true\n"},
		{"different parents", "one:\n  enabled: true\ntwo:\n  #enabled: false\n", "one:\n  enabled: true\ntwo:\n  #enabled: false\n"},
		{"intentional alternatives", "feature: true\n#feature:\n#  enabled: true\n", "feature: true\n#feature:\n#  enabled: true\n"},
		{"prose", "enabled: true\n# enabled: this is documentation\n", "enabled: true\n# enabled: this is documentation\n"},
		{"scalar payload", "root: |\n  #feature:\n  #  enabled: false\nfeature:\n  enabled: true\n", "root: |\n  #feature:\n  #  enabled: false\nfeature:\n  enabled: true\n"},
		{"sequence identities", "items:\n  - name: one\n    #feature: false\n  - name: two\n    #feature: false\n", "items:\n  - name: one\n    #feature: false\n  - name: two\n    #feature: false\n"},
		{"different inactive values", "#feature: false\nnext: true\n#feature: true\n", "#feature: false\nnext: true\n#feature: true\n"},
		{"saved operator descendants", "feature:\n  enabled: true\n#feature:\n#  enabled: false\n#  custom: saved-operator-value\n", "feature:\n  enabled: true\n#feature:\n#  enabled: false\n#  custom: saved-operator-value\n"},
		{"unconfigured template defaults", "feature:\n  enabled: true\n#feature:\n#  enabled: false\n#  custom: default\n", "feature:\n  enabled: true\n#feature:\n#  enabled: false\n#  custom: default\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Deliberately different forms are template-authored alternatives,
			// not duplicate configured settings.
			baseline := test.input
			if test.name == "intentional alternatives" {
				baseline = "#feature: false\n#feature:\n#  enabled: true\n"
			}
			if test.name == "saved operator descendants" {
				baseline = "#feature:\n#  enabled: false\n#  custom: default\n"
			}
			got, err := deduplicateYAMLTemplateEntries([]byte(baseline), []byte(test.input))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, test.want)
			}
			var before, after yaml.Node
			yaml.Unmarshal([]byte(test.input), &before)
			yaml.Unmarshal(got, &after)
			if !semanticYAMLNodeEqual(documentRoot(&before), documentRoot(&after)) {
				t.Fatal("deduplication changed configured values")
			}
		})
	}
}

func TestUpgradeCloudOpsworksConfigQuotedKeysRetainTemplatePlacement(t *testing.T) {
	for _, key := range []string{"'complex key'", "'key: with colon'", "\"key with \\\"quote\\\"\"", "'key with ''quote'''"} {
		t.Run(key, func(t *testing.T) {
			dir, runner, _ := configUpgradeRunner(t, false)
			name := ".cloudopsworks/vars/inputs-global.yaml"
			writeConfigFixture(t, dir, ".template/"+name, "#timeout: 3\n"+key+": default\nnext: default\n")
			writeConfigFixture(t, dir, name, "timeout: 10\n"+key+": custom\nnext: custom\n")
			if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
				t.Fatal(err)
			}
			got := mustRead(t, filepath.Join(dir, name))
			want := "timeout: 10\n" + key + ": custom\nnext: custom\n"
			if got != want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}
