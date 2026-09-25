package repos

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// upgradeCloudOpsworksConfig is the deliberately small internal seam exercised
// by these regression tests. It reads the fetched template below .template and
// reconciles its .cloudopsworks YAML into the repository described by state.
// applyVersionedTemplate must call it before copying _VERSION.

func TestUpgradeCloudOpsworksConfigPreservesValuesAndAddsTargetStructure(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "cloud: aws\ncloud_type: lambda\nteam: payments\n")
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-dev.yaml", "name: local-dev\nlegacy_flag: keep-me\n")
	writeConfigFixture(t, dir, ".cloudopsworks/cloudopsworks-ci.yaml", "pipeline: local\n")
	writeConfigFixture(t, dir, ".cloudopsworks/labeler.yml", "labels:\n  local: backend\n")
	writeConfigFixture(t, dir, ".cloudopsworks/gitversion.yaml", "mode: ContinuousDeployment\n")

	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-global.yaml", "# target global documentation\ncloud: aws\ncloud_type: lambda\nteam: platform\nnew_default: enabled\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-LAMBDA-ENV.yaml", "# lambda target scaffold\nname: lambda-default\nruntime: nodejs20\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/cloudopsworks-ci.yaml", "# target CI policy\npipeline: template\nconcurrency: 2\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/labeler.yml", "# target labels\nlabels:\n  local: template\n  added: triage\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/gitversion.yaml", "# target GitVersion\nmode: Mainline\nnext-version: 5.10.0\n")

	state := RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks", VersionFile: ".cloudopsworks/_VERSION"}
	if err := runner.upgradeCloudOpsworksConfig(state); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}

	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-global.yaml")), "# target global documentation", "team: payments", "new_default: enabled")
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-dev.yaml")), "# lambda target scaffold", "name: local-dev", "runtime: nodejs20", "legacy_flag: keep-me")
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/cloudopsworks-ci.yaml")), "# target CI policy", "pipeline: local", "concurrency: 2")
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/labeler.yml")), "# target labels", "local: backend", "added: triage")
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/gitversion.yaml")), "# target GitVersion", "mode: ContinuousDeployment", "next-version: 5.10.0")
	assertContainsAll(t, out.String(), "inputs-dev.yaml", "legacy_flag")
}

func TestUpgradeCloudOpsworksConfigMapsKubernetesCustomEnvironmentSubdirectories(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "cloud: aws\ncloud_type: eks\n")
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-prod-live.yaml", "replicas: 7\n")
	writeConfigFixture(t, dir, ".cloudopsworks/vars/helm/inputs-prod-live.yaml", "image_tag: local\n")
	writeConfigFixture(t, dir, ".cloudopsworks/vars/apigw/inputs-prod-live.yaml", "authorizer: local-auth\n")
	writeConfigFixture(t, dir, ".cloudopsworks/vars/preview/inputs-prod-live.yaml", "enabled: true\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-KUBERNETES-ENV.yaml", "# kubernetes baseline\nreplicas: 2\nnamespace: default\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/helm/inputs-prod.yaml", "# helm prod baseline\nimage_tag: stable\nchart: app\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/apigw/inputs-prod.yaml", "# gateway prod baseline\nauthorizer: template\ntimeout: 30\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/preview/inputs-prod.yaml", "# preview prod baseline\nenabled: false\nttl: 24h\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-prod-live.yaml")), "# kubernetes baseline", "replicas: 7", "namespace: default")
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/helm/inputs-prod-live.yaml")), "# helm prod baseline", "image_tag: local", "chart: app")
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/apigw/inputs-prod-live.yaml")), "# gateway prod baseline", "authorizer: local-auth", "timeout: 30")
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/preview/inputs-prod-live.yaml")), "# preview prod baseline", "enabled: true", "ttl: 24h")
}

func TestApplyVersionedTemplateMigratesV59ConfigBeforeV510Merge(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	mustWrite(t, filepath.Join(dir, ".github", "_VERSION"), "v5.9.8\n")
	mustWrite(t, filepath.Join(dir, ".github", ".golang"), "")
	writeConfigFixture(t, dir, ".github/vars/inputs-global.yaml", "cloud: gcp\ncloud_type: cloudrun\n")
	writeConfigFixture(t, dir, ".github/vars/inputs-dev.yaml", "service: legacy-service\n")
	mustWrite(t, filepath.Join(dir, ".github", "workflows", "build.yml"), "name: old\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "build.yml"), "name: target\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.4\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-CLOUDRUN.yaml", "# cloud run baseline\nservice: template\nregion: us-central1\n")

	state := RepositoryState{WorkDir: dir, BlueprintPath: ".github", VersionFile: ".github/_VERSION", Pre510: true, Version: "v5.9.8"}
	if err := runner.applyVersionedTemplate(Template{Name: "go", Versioned: true}, state, "v5.10.4"); err != nil {
		t.Fatalf("applyVersionedTemplate() error = %v", err)
	}
	if exists(filepath.Join(dir, ".github", "vars")) {
		t.Fatal("v5.9 .github/vars was not migrated before configuration upgrade")
	}
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-dev.yaml")), "# cloud run baseline", "service: legacy-service", "region: us-central1")
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks/_VERSION")); got != "v5.10.4\n" {
		t.Fatalf("successful configuration upgrade did not update _VERSION: %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".github/workflows/build.yml")); got != "name: target\n" {
		t.Fatalf("workflow replacement = %q, want target-owned workflow", got)
	}
}

func TestUpgradeCloudOpsworksConfigMapsRemainingCloudFamilies(t *testing.T) {
	for _, test := range []struct {
		name, cloud, cloudType, scaffold string
	}{
		{name: "beanstalk", cloud: "aws", cloudType: "beanstalk", scaffold: "inputs-BEANSTALK-ENV.yaml"},
		{name: "app-engine", cloud: "gcp", cloudType: "appengine", scaffold: "inputs-APPENGINE.yaml"},
		{name: "cloud-run", cloud: "gcp", cloudType: "cloudrun", scaffold: "inputs-CLOUDRUN.yaml"},
		{name: "library", cloud: "none", cloudType: "library", scaffold: "inputs-LIB-ENV.yaml"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, runner, _ := configUpgradeRunner(t, false)
			writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "cloud: "+test.cloud+"\ncloud_type: "+test.cloudType+"\n")
			writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-dev.yaml", "configured: local\n")
			writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/"+test.scaffold, "# "+test.name+" scaffold\nconfigured: template\ntarget_default: kept\n")

			if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
				t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
			}
			assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-dev.yaml")), "# "+test.name+" scaffold", "configured: local", "target_default: kept")
		})
	}
}

func TestUpgradeCloudOpsworksConfigDryRunDoesNotMutateBytes(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, true)
	local := "cloud: aws\ncloud_type: lambda\nvalue: local\n"
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", local)
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-global.yaml", "cloud: aws\ncloud_type: lambda\nvalue: template\nnew_key: added\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-global.yaml")); got != local {
		t.Fatalf("dry-run mutated configuration: %q", got)
	}
	if !strings.Contains(out.String(), "DRY-RUN") {
		t.Fatalf("dry-run did not report planned configuration write: %s", out.String())
	}
}

func TestApplyVersionedTemplateLeavesVersionUntouchedWhenConfigUpgradeFails(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.1\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.2\n")
	// Duplicate YAML keys are unsafe: accepting them would make the overlay non-deterministic.
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-global.yaml", "cloud: aws\ncloud: gcp\n")

	err := runner.applyVersionedTemplate(Template{Name: "go", Versioned: true}, RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks", VersionFile: ".cloudopsworks/_VERSION"}, "v5.10.2")
	if err == nil {
		t.Fatal("applyVersionedTemplate() succeeded with unsafe target YAML")
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "_VERSION")); got != "v5.10.1\n" {
		t.Fatalf("_VERSION changed before configuration success: %q", got)
	}
}

func TestUpgradeCloudOpsworksConfigKeepsTargetCommentsWhenReplacingValues(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/cloudopsworks-ci.yaml", "scalar: local # local scalar rationale\nitems:\n  - local\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/cloudopsworks-ci.yaml", "scalar: template # scalar target documentation\nitems:\n  - template # sequence target documentation\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	content := mustRead(t, filepath.Join(dir, ".cloudopsworks/cloudopsworks-ci.yaml"))
	assertContainsAll(t, content, "scalar: local", "- local", "# scalar target documentation", "# sequence target documentation")
	if !strings.Contains(content, "# local scalar rationale") && !(strings.Contains(out.String(), "cloudopsworks-ci.yaml") && strings.Contains(out.String(), "scalar")) {
		t.Fatalf("local scalar comment was silently lost; content=%q warnings=%q", content, out.String())
	}
}

func TestUpgradeCloudOpsworksConfigCopiesTargetOnlyFeatureConfigsOnlyWhenNestedEnabled(t *testing.T) {
	for _, test := range []struct {
		name, global           string
		wantAPIGW, wantPreview bool
	}{
		{name: "nested-enabled", global: "apis:\n  enabled: true\npreview:\n  enabled: true\n", wantAPIGW: true, wantPreview: true},
		{name: "nested-false", global: "apis:\n  enabled: false\npreview:\n  enabled: false\n", wantAPIGW: false, wantPreview: false},
		{name: "commented", global: "cloud: aws\n# apis:\n#   enabled: true\n# preview:\n#   enabled: true\n", wantAPIGW: false, wantPreview: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, runner, _ := configUpgradeRunner(t, false)
			writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", test.global)
			writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/apigw/values-dev.yaml", "# target gateway\ntimeout: 30\n")
			writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/preview/values-dev.yaml", "# target preview\nttl: 24h\n")

			if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
				t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
			}
			if got := exists(filepath.Join(dir, ".cloudopsworks/vars/apigw/values-dev.yaml")); got != test.wantAPIGW {
				t.Fatalf("apigw target copied = %v, want %v", got, test.wantAPIGW)
			}
			if got := exists(filepath.Join(dir, ".cloudopsworks/vars/preview/values-dev.yaml")); got != test.wantPreview {
				t.Fatalf("preview target copied = %v, want %v", got, test.wantPreview)
			}
		})
	}
}

func TestEnvironmentTargetUsesDelimitedEnvironmentAndFilenameFamily(t *testing.T) {
	templates := map[string]yamlDocument{
		"vars/helm/values-prod.yaml":    {},
		"vars/helm/apis-prod.yaml":      {},
		"vars/helm/inputs-prod.yaml":    {},
		"vars/helm/values-product.yaml": {},
	}
	got, ok := environmentTarget("vars/helm", "values-prod-live.yaml", templates)
	if !ok || got != "vars/helm/values-prod.yaml" {
		t.Fatalf("prod-live target = %q, %v; want values-prod.yaml, true", got, ok)
	}
	if got, ok := environmentTarget("vars/helm", "values-product.yaml", templates); ok || got != "" {
		t.Fatalf("product must not classify as prod environment: %q, %v", got, ok)
	}
}

func TestUpgradeCloudOpsworksConfigUsesAgentsHeaderBeforeScaffoldFilename(t *testing.T) {
	for _, test := range []struct {
		name, cloud, cloudType, header, marker string
	}{
		{name: "semicolon-kubernetes", cloud: "aws", cloudType: "kubernetes", header: "# Agents: cloud=aws|gcp ; cloud_type=kubernetes\n", marker: "kubernetes"},
		{name: "comma-library", cloud: "none", cloudType: "lib", header: "# Agents: cloud=none, cloud_type=lib\n", marker: "library"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, runner, _ := configUpgradeRunner(t, false)
			writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "cloud: "+test.cloud+"\ncloud_type: "+test.cloudType+"\n")
			writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-custom.yaml", "value: local\n")
			writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-special.yaml", test.header+"# "+test.marker+" baseline\nvalue: template\n")
			writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-LAMBDA-ENV.yaml", "# lambda baseline\nvalue: template\n")

			if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
				t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
			}
			content := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-custom.yaml"))
			assertContainsAll(t, content, "# "+test.marker+" baseline", "value: local")
			if strings.Contains(content, "# lambda baseline") {
				t.Fatalf("Agents metadata did not override fallback scaffold filename:\n%s", content)
			}
		})
	}
}

func TestUpgradeCloudOpsworksConfigRejectsAliasAndMultiDocumentBeforeWrites(t *testing.T) {
	for _, test := range []struct {
		name, invalid string
	}{
		{name: "alias", invalid: "defaults: &defaults\n  value: target\nuse: *defaults\n"},
		{name: "multiple-documents", invalid: "value: first\n---\nvalue: second\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, runner, _ := configUpgradeRunner(t, false)
			local := "value: local\n"
			writeConfigFixture(t, dir, ".cloudopsworks/cloudopsworks-ci.yaml", local)
			writeConfigFixture(t, dir, ".template/.cloudopsworks/cloudopsworks-ci.yaml", "value: target\nnew_value: target\n")
			writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-global.yaml", test.invalid)

			err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"})
			if err == nil {
				t.Fatal("upgradeCloudOpsworksConfig() succeeded with unsafe YAML")
			}
			if got := mustRead(t, filepath.Join(dir, ".cloudopsworks/cloudopsworks-ci.yaml")); got != local {
				t.Fatalf("configuration changed before unsafe YAML failed: %q", got)
			}
		})
	}
}

func TestUpgradeCloudOpsworksConfigPre510DryRunPlansCloudOpsDestinationWithoutMutation(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, true)
	if err := os.RemoveAll(filepath.Join(dir, ".cloudopsworks")); err != nil {
		t.Fatalf("remove v5.10 fixture root: %v", err)
	}
	local := "cloud: aws\ncloud_type: lambda\nvalue: local\n"
	writeConfigFixture(t, dir, ".github/vars/inputs-global.yaml", local)
	writeConfigFixture(t, dir, ".github/workflows/build.yaml", "name: workflow\n")
	writeConfigFixture(t, dir, ".github/workflows/auto-assign.yml", "name: workflow auto assign\n")
	writeConfigFixture(t, dir, ".github/dependabot.yml", "version: 2\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-global.yaml", "cloud: aws\ncloud_type: lambda\nvalue: template\nnew_value: target\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".github", Pre510: true}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".github/vars/inputs-global.yaml")); got != local {
		t.Fatalf("dry-run mutated pre-v5.10 source: %q", got)
	}
	if exists(filepath.Join(dir, ".cloudopsworks/vars/inputs-global.yaml")) {
		t.Fatal("dry-run created v5.10 configuration destination")
	}
	assertContainsAll(t, out.String(), "DRY-RUN", ".cloudopsworks")
	if strings.Contains(out.String(), "workflows/build.yaml") || strings.Contains(out.String(), "workflows/auto-assign.yml") || strings.Contains(out.String(), "dependabot.yml") {
		t.Fatalf("dry-run inspected non-CloudOps GitHub YAML: %s", out.String())
	}
}

func TestApplyVersionedTemplateStagesMergedAndNewCloudOpsYAML(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.1\n")
	writeConfigFixture(t, dir, ".cloudopsworks/cloudopsworks-ci.yaml", "pipeline: local\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.2\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/cloudopsworks-ci.yaml", "pipeline: template\nnew_setting: target\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/auto-assign.yml", "reviewers:\n  - platform\n")

	if err := runner.applyVersionedTemplate(Template{Name: "go", Versioned: true}, RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks", VersionFile: ".cloudopsworks/_VERSION"}, "v5.10.2"); err != nil {
		t.Fatalf("applyVersionedTemplate() error = %v", err)
	}
	staged := runGit(t, dir, "diff", "--cached", "--name-only")
	for _, path := range []string{".cloudopsworks/cloudopsworks-ci.yaml", ".cloudopsworks/auto-assign.yml"} {
		if !strings.Contains(staged, path+"\n") {
			t.Fatalf("CloudOps YAML %s not staged:\n%s", path, staged)
		}
	}
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/cloudopsworks-ci.yaml")), "pipeline: local", "new_setting: target")
}

func TestApplyVersionedTemplateKeepsHookReplacementButMergesHookYAML(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.1\n")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "hooks", "install.sh"), "old hook\n")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "hooks", "stale.sh"), "stale hook\n")
	if err := os.Chmod(filepath.Join(dir, ".cloudopsworks", "hooks", "install.sh"), 0o644); err != nil {
		t.Fatalf("chmod existing hook: %v", err)
	}
	writeConfigFixture(t, dir, ".cloudopsworks/hooks/config.yaml", "enabled: true\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.2\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "hooks", "install.sh"), "new hook\n")
	if err := os.Chmod(filepath.Join(dir, ".template", ".cloudopsworks", "hooks", "install.sh"), 0o755); err != nil {
		t.Fatalf("chmod template hook: %v", err)
	}
	writeConfigFixture(t, dir, ".template/.cloudopsworks/hooks/config.yaml", "# hook defaults\nenabled: false\nretries: 3\n")

	if err := runner.applyVersionedTemplate(Template{Name: "go", Versioned: true}, RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks", VersionFile: ".cloudopsworks/_VERSION"}, "v5.10.2"); err != nil {
		t.Fatalf("applyVersionedTemplate() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks/hooks/install.sh")); got != "new hook\n" {
		t.Fatalf("non-YAML hook replacement = %q, want template hook", got)
	}
	if exists(filepath.Join(dir, ".cloudopsworks", "hooks", "stale.sh")) {
		t.Fatal("stale non-YAML hook was not deleted")
	}
	info, err := os.Stat(filepath.Join(dir, ".cloudopsworks", "hooks", "install.sh"))
	if err != nil {
		t.Fatalf("stat refreshed hook: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("refreshed hook mode = %o, want 755", info.Mode().Perm())
	}
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/hooks/config.yaml")), "# hook defaults", "enabled: true", "retries: 3")
}

func TestApplyVersionedTemplateRejectsLaterInvalidYAMLBeforeWritesOrVersion(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.1\n")
	local := "pipeline: local\n"
	writeConfigFixture(t, dir, ".cloudopsworks/cloudopsworks-ci.yaml", local)
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.2\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/cloudopsworks-ci.yaml", "pipeline: target\nnew_setting: target\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/zzz-invalid.yaml", "value: first\n---\nvalue: second\n")

	err := runner.applyVersionedTemplate(Template{Name: "go", Versioned: true}, RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks", VersionFile: ".cloudopsworks/_VERSION"}, "v5.10.2")
	if err == nil {
		t.Fatal("applyVersionedTemplate() succeeded with later invalid YAML")
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks/cloudopsworks-ci.yaml")); got != local {
		t.Fatalf("earlier CloudOps YAML changed before later validation failed: %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks/_VERSION")); got != "v5.10.1\n" {
		t.Fatalf("_VERSION changed before all configuration validation succeeded: %q", got)
	}
}

func TestUpgradeCloudOpsworksConfigUsesConfiguredTemplateDirectory(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	runner.Config.TemplateDirectory = ".upstream-template"
	writeConfigFixture(t, dir, ".cloudopsworks/cloudopsworks-ci.yaml", "pipeline: local\n")
	writeConfigFixture(t, dir, ".upstream-template/.cloudopsworks/cloudopsworks-ci.yaml", "# configured template root\npipeline: target\nnew_setting: target\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/cloudopsworks-ci.yaml")), "# configured template root", "pipeline: local", "new_setting: target")
}

func TestApplyBoilerplatePreservesMergedYAML(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	boilerplate := ".cloudopsworks/boilerplate"
	writeConfigFixture(t, dir, boilerplate+"/vars.yaml", "value: local\n")
	mustWrite(t, filepath.Join(dir, boilerplate, "stale.sh"), "stale\n")
	writeConfigFixture(t, dir, ".template/"+boilerplate+"/vars.yaml", "value: template\ntarget_default: kept\n")
	mustWrite(t, filepath.Join(dir, ".template", boilerplate, "install.sh"), "template\n")
	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}

	if err := runner.applyMergedBoilerplate(Template{BoilerplatePathV510Plus: boilerplate}); err != nil {
		t.Fatalf("applyMergedBoilerplate() error = %v", err)
	}
	assertContainsAll(t, mustRead(t, filepath.Join(dir, boilerplate, "vars.yaml")), "value: template", "target_default: kept")
	if exists(filepath.Join(dir, boilerplate, "stale.sh")) {
		t.Fatal("stale non-YAML boilerplate file was not removed")
	}
	if got := mustRead(t, filepath.Join(dir, boilerplate, "install.sh")); got != "template\n" {
		t.Fatalf("template boilerplate file = %q, want template content", got)
	}
}

func TestApplyBoilerplateReplacesYAMLBeforeV510(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	boilerplate := ".cloudopsworks"
	writeConfigFixture(t, dir, boilerplate+"/legacy.yaml", "value: local\n")
	mustWrite(t, filepath.Join(dir, boilerplate, "stale.sh"), "stale\n")
	writeConfigFixture(t, dir, ".template/"+boilerplate+"/legacy.yaml", "value: template\n")
	mustWrite(t, filepath.Join(dir, ".template", boilerplate, "install.sh"), "template\n")

	if err := runner.applyBoilerplate(Template{BoilerplatePathPre510: boilerplate}, true); err != nil {
		t.Fatalf("applyBoilerplate() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, boilerplate, "legacy.yaml")); got != "value: template\n" {
		t.Fatalf("pre-v5.10 boilerplate YAML = %q, want template replacement", got)
	}
	if exists(filepath.Join(dir, boilerplate, "stale.sh")) {
		t.Fatal("pre-v5.10 stale boilerplate file was not removed")
	}
}

func TestUpgradeCloudOpsworksConfigRetainsTargetForInactiveLocalYAML(t *testing.T) {
	for _, test := range []struct {
		name, local, warning string
	}{
		{name: "empty", local: ""},
		{name: "comment-only", local: "# retained as a warning only\n", warning: "comment-only"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, runner, out := configUpgradeRunner(t, false)
			path := ".cloudopsworks/cloudopsworks-ci.yaml"
			target := "# target policy\npipeline: template\n"
			writeConfigFixture(t, dir, path, test.local)
			writeConfigFixture(t, dir, ".template/"+path, target)

			if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
				t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
			}
			if got := mustRead(t, filepath.Join(dir, path)); got != target {
				t.Fatalf("inactive local YAML result = %q, want target baseline %q", got, target)
			}
			if test.warning != "" && !strings.Contains(out.String(), "cloudopsworks-ci.yaml path "+test.warning) {
				t.Fatalf("missing deterministic warning %q: %s", test.warning, out.String())
			}
		})
	}
}

func TestUpgradeCloudOpsworksConfigCopiesInactiveTargetYAMLBytes(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	for name, content := range map[string]string{
		"empty.yaml":   "",
		"comment.yaml": "# target-only comment\n",
	} {
		writeConfigFixture(t, dir, ".template/.cloudopsworks/"+name, content)
	}

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	for name, want := range map[string]string{
		"empty.yaml":   "",
		"comment.yaml": "# target-only comment\n",
	} {
		if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", name)); got != want {
			t.Fatalf("target-only %s = %q, want %q", name, got, want)
		}
	}
}

func TestCICDUpdateWritesOnlyChangesAndPreservesMode(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	path := filepath.Join(dir, ".cloudopsworks", "cloudopsworks-ci.yaml")
	mustWrite(t, path, "pipeline: local\n")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.2\n")
	if err := os.Chmod(path, 0o750); err != nil {
		t.Fatalf("chmod CICD configuration: %v", err)
	}
	state := RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}
	if err := runner.CICDUpdate(state, "template-hash"); err != nil {
		t.Fatalf("first CICDUpdate() error = %v", err)
	}
	first, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat updated CICD configuration: %v", err)
	}
	if first.Mode().Perm() != 0o750 {
		t.Fatalf("updated CICD configuration mode = %o, want 750", first.Mode().Perm())
	}
	if err := runner.CICDUpdate(state, "template-hash"); err != nil {
		t.Fatalf("second CICDUpdate() error = %v", err)
	}
	second, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat unchanged CICD configuration: %v", err)
	}
	if !second.ModTime().Equal(first.ModTime()) {
		t.Fatal("unchanged CICD configuration was rewritten")
	}
}

func TestApplyVersionedTemplateStagesOnlyNewTemplateOwnedCloudOpsFiles(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.1\n")
	writeConfigFixture(t, dir, ".cloudopsworks/local-only.yaml", "user_setting: keep\n")
	staleHook := filepath.Join(dir, ".cloudopsworks", "hooks", "stale.sh")
	mustWrite(t, staleHook, "old hook\n")
	runGit(t, dir, "config", "user.email", "tronador-cli-test@example.com")
	runGit(t, dir, "config", "user.name", "tronador-cli test")
	runGit(t, dir, "add", ".cloudopsworks/hooks/stale.sh")
	runGit(t, dir, "commit", "-m", "initial hook")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.2\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "Makefile"), "template target\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "hooks", "install.sh"), "template hook\n")

	state := RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks", VersionFile: ".cloudopsworks/_VERSION"}
	if err := runner.applyVersionedTemplate(Template{Name: "go", Versioned: true}, state, "v5.10"); err != nil {
		t.Fatalf("applyVersionedTemplate() error = %v", err)
	}

	staged := runGit(t, dir, "diff", "--cached", "--name-only")
	for _, path := range []string{
		".cloudopsworks/_VERSION",
		".cloudopsworks/Makefile",
		".cloudopsworks/hooks/install.sh",
		".cloudopsworks/hooks/stale.sh",
	} {
		if !strings.Contains(staged, path+"\n") {
			t.Fatalf("template-owned %s was not staged:\n%s", path, staged)
		}
	}
	if strings.Contains(staged, ".cloudopsworks/local-only.yaml\n") {
		t.Fatalf("unrelated local configuration was staged:\n%s", staged)
	}
}

func TestCICDUpdateWithTargetVersionDefersVersionMarker(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	versionPath := filepath.Join(dir, ".cloudopsworks", "_VERSION")
	ciPath := filepath.Join(dir, ".cloudopsworks", "cloudopsworks-ci.yaml")
	mustWrite(t, versionPath, "v5.10.1\n")
	mustWrite(t, ciPath, "pipeline: local\n")
	state := RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}

	if err := runner.cicdUpdateWithVersion(state, "template-hash", "v5.10.2"); err != nil {
		t.Fatalf("cicdUpdateWithVersion() error = %v", err)
	}
	assertContainsAll(t, mustRead(t, ciPath), "#workflow-version-tag: v5.10.2 - hash: template-hash")
	if got := mustRead(t, versionPath); got != "v5.10.1\n" {
		t.Fatalf("_VERSION changed before final copy: %q", got)
	}

	if err := os.Remove(ciPath); err != nil {
		t.Fatalf("remove CICD path: %v", err)
	}
	if err := os.Symlink(versionPath, ciPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := runner.cicdUpdateWithVersion(state, "template-hash", "v5.10.2"); err == nil {
		t.Fatal("cicdUpdateWithVersion() accepted a symlink destination")
	}
	if got := mustRead(t, versionPath); got != "v5.10.1\n" {
		t.Fatalf("_VERSION changed after late CICD failure: %q", got)
	}
}

func TestTargetVersionMarkerIsCapturedSafelyBeforeFinalWrite(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	target := filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION")
	mustWrite(t, target, "v5.10.3\n")
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	data, mode, version, err := runner.targetCloudOpsVersionMarker()
	if err != nil {
		t.Fatalf("targetCloudOpsVersionMarker() error = %v", err)
	}
	if version != "v5.10.3" || string(data) != "v5.10.3\n" || mode.Perm() != 0o640 {
		t.Fatalf("captured marker = %q, %q, %o", string(data), version, mode.Perm())
	}
	// Stack removes its temporary checkout before commit. Its final write must
	// therefore use the captured bytes rather than rereading the source.
	if err := os.RemoveAll(filepath.Join(dir, ".template")); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.writeFileIfChanged(filepath.Join(dir, ".cloudopsworks", "_VERSION"), data, mode); err != nil {
		t.Fatalf("final captured marker write: %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "_VERSION")); got != "v5.10.3\n" {
		t.Fatalf("final marker = %q", got)
	}
}

func TestTargetVersionMarkerRejectsSymlinkedTemplateAncestor(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	mustWrite(t, filepath.Join(dir, "outside", "_VERSION"), "v5.10.3\n")
	if err := os.RemoveAll(filepath.Join(dir, ".template", ".cloudopsworks")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "outside"), filepath.Join(dir, ".template", ".cloudopsworks")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, _, err := runner.targetCloudOpsVersionMarker(); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("targetCloudOpsVersionMarker() error = %v, want unsafe ancestor rejection", err)
	}
}

func TestUpgradeCloudOpsworksConfigKeepsTopLevelHPAAndServiceAccountComments(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	// This is the ingress shape from forward-async-processor-ms before upgrade.
	writeConfigFixture(t, dir, ".cloudopsworks/vars/helm/values-dev.yaml", "ingress:\n  enabled: true\n  ingressClassName: nginx\n  rules:\n    - host: forward-async-processor.ms.dev.4wrd.tech\n      http:\n        paths:\n          - path: /api\n            pathType: Prefix\n            backend:\n              service:\n                name: forward-async-processor-helm\n                port:\n                  number: 80\nserviceAccount:\n  create: true\n  name: forward-async-processor\n  annotations:\n    eks.amazonaws.com/role-arn: arn:aws:iam::905418487951:role/async-trxs-federated-forward-main-dev-001-usea1\n")
	// This preserves the target node-app-template's raw top-level comment layout,
	// including the intentionally mixed indentation below #serviceAccount.
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/helm/values-dev.yaml", "ingress:\n  enabled: true\n  ingressClassName: nginx\n  rules:\n    - host: DEV-URL\n      http:\n        paths:\n          - path: /\n            pathType: Prefix\n            backend:\n              service:\n                name: PROJECT_NAME-helm\n                port:\n                  number: 80\n\n# HorizontalPodAutoscaler\n#hpa:\n#  enabled: false\n#  minReplicas: 2\n#  maxReplicas: 6\n#  cpuTargetAverageUtilization: 80\n#  memoryTargetAverageUtilization: 80\n#  # Enable Metrics\n#  cpuPercentage: true\n#  memoryPercentage: true\n#  # Optionals defaults\n#  stabilizationWindowSeconds: 300\n#  percentValueDown: 40\n#  percentPeriodDown: 60\n#  percentValueUp: 80\n#  percentPeriodUp: 60\n#  # External\n#  external:\n#    enabled: true\n#    name: external-metric\n#    labelSelector:\n#      labelKey: labelValue\n#    averageValue: 50\n\n# Service Account\n#serviceAccount:\n  # Specifies whether a service account should be used, if set true, the service account name should be set\n  #enabled: false\n  # Specifies whether a service account should be created\n  #create: false\n  #annotations: {}\n  #name: (optional)\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	content := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/helm/values-dev.yaml"))
	assertContainsAll(t, content, "pathType: Prefix", "number: 80", "# HorizontalPodAutoscaler", "#hpa:", "# Service Account", "serviceAccount:", "# Specifies whether a service account should be used", "#enabled: false", "annotations:", "eks.amazonaws.com/role-arn:")
	// The ingress sequence is an end-to-end sample shape: target indentation is
	// authoritative even when a legacy local file used a different hierarchy.
	assertContainsAll(t, content, "ingress:\n  enabled: true\n  ingressClassName: nginx\n  rules:\n    - host: forward-async-processor.ms.dev.4wrd.tech\n      http:\n        paths:\n          - path: /api\n            pathType: Prefix")
	numberAt, hpaAt, serviceAccountAt := strings.Index(content, "number: 80"), strings.Index(content, "#hpa:"), strings.Index(content, "serviceAccount:")
	if numberAt < 0 || hpaAt <= numberAt || serviceAccountAt <= hpaAt {
		t.Fatalf("target HPA and promoted serviceAccount order changed:\n%s", content)
	}
	if got := activeYAMLKeyCount(content, "serviceAccount"); got != 1 {
		t.Fatalf("active root serviceAccount keys = %d, want exactly one:\n%s", got, content)
	}
	if strings.Contains(content, "#serviceAccount:") {
		t.Fatalf("promoted serviceAccount retained duplicate structural placeholder:\n%s", content)
	}
	for _, duplicate := range []string{"#create: false", "#name: (optional)", "#annotations: {}"} {
		if strings.Contains(content, duplicate) {
			t.Fatalf("active serviceAccount retained duplicate conceptual child %q:\n%s", duplicate, content)
		}
	}
	for _, comment := range []string{"#hpa:", "# Service Account"} {
		if indent := indentationOf(content, comment); indent != 0 {
			t.Fatalf("comment %q indentation = %d, want root-aligned:\n%s", comment, indent, content)
		}
	}
	for _, comment := range []string{"# Specifies whether a service account should be used, if set true, the service account name should be set", "#enabled: false"} {
		if indent := indentationOf(content, comment); indent != 2 {
			t.Fatalf("serviceAccount child comment %q indentation = %d, want 2:\n%s", comment, indent, content)
		}
	}
}

func TestUpgradeCloudOpsworksConfigDoesNotPromoteProseCommentsToKeys(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "cloud: aws\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-global.yaml", "# Default node version is 24; uncomment to build with a different version\ncloud: template\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	content := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-global.yaml"))
	assertContainsAll(t, content, "# Default node version is 24; uncomment to build with a different version", "cloud: aws")
	if strings.Contains(content, "Default node version is 24; uncomment to build with a different version:") {
		t.Fatalf("ordinary prose comment was rendered as a YAML key:\n%s", content)
	}
}

func TestUpgradeCloudOpsworksConfigPromotesCommentedInputDefaultAtTemplatePosition(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "cloud: aws\nbuild:\n  node_version: \"22\"\nimage: local\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-global.yaml", "# Default node version is 24; uncomment to build with a different version\ncloud: aws\n# build:\n#   node_version: \"24\"\nimage: template\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	content := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-global.yaml"))
	assertContainsAll(t, content, "# Default node version is 24; uncomment to build with a different version", "build:", "node_version: \"22\"", "image: local")
	if strings.Contains(content, "# build:") || strings.Contains(content, "#   node_version: \"24\"") {
		t.Fatalf("active input default retained a duplicate commented placeholder:\n%s", content)
	}
	buildAt, imageAt := strings.Index(content, "build:"), strings.Index(content, "image: local")
	if buildAt < 0 || imageAt < 0 || buildAt > imageAt {
		t.Fatalf("promoted input default was not emitted at its template position:\n%s", content)
	}
	if strings.Contains(content, "Default node version is 24; uncomment to build with a different version:") {
		t.Fatalf("ordinary prose comment was rendered as a YAML key:\n%s", content)
	}
}

func TestUpgradeCloudOpsworksConfigRendersActiveServiceAccountWithoutCommentedPlaceholder(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/vars/helm/values-dev.yaml", "serviceAccount:\n  create: true\n  name: runtime-service-account\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/helm/values-dev.yaml", "# Service Account\n#serviceAccount:\n  # Specifies whether a service account should be used\n  #enabled: false\n  # Specifies whether a service account should be created\n  #create: false\n  #annotations: {}\n  #name: (optional)\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	content := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/helm/values-dev.yaml"))
	if got := activeYAMLKeyCount(content, "serviceAccount"); got != 1 {
		t.Fatalf("active serviceAccount keys = %d, want exactly one:\n%s", got, content)
	}
	if strings.Contains(content, "#serviceAccount:") {
		t.Fatalf("duplicate commented serviceAccount placeholder remained:\n%s", content)
	}
	assertContainsAll(t, content, "# Specifies whether a service account should be used", "#annotations: {}", "create: true", "name: runtime-service-account")
}

func TestUpgradeCloudOpsworksConfigDoesNotConsumeProseColonCommentAsDefault(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "endpoint: https://runtime.example\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-global.yaml", "# endpoint: example documentation\n# This is prose, not an inactive YAML default.\nregion: us-east-1\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	content := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-global.yaml"))
	assertContainsAll(t, content, "# endpoint: example documentation", "# This is prose, not an inactive YAML default.", "endpoint: https://runtime.example")
	if strings.Contains(content, "# endpoint: https://runtime.example") {
		t.Fatalf("prose colon comment was consumed as a structural default:\n%s", content)
	}
}

func TestUpgradeCloudOpsworksConfigRetainsUnusedCommentedMappingDefaults(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "feature:\n  enabled: true\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-global.yaml", "#feature:\n#  enabled: false\n#  timeout: 30\n#  retries: 3\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	content := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-global.yaml"))
	if got := activeYAMLKeyCount(content, "feature"); got != 1 {
		t.Fatalf("active feature keys = %d, want one:\n%s", got, content)
	}
	if strings.Contains(content, "#feature:") || strings.Contains(content, "#  enabled: false") {
		t.Fatalf("activated feature retained duplicate structural placeholder:\n%s", content)
	}
	assertContainsAll(t, content, "enabled: true", "#  timeout: 30", "#  retries: 3")
}

func TestUpgradeCloudOpsworksConfigWarnsAndFallsBackForAmbiguousCommentedCandidates(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/cloudopsworks-ci.yaml", "feature:\n  enabled: true\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/cloudopsworks-ci.yaml", "#feature:\n#  enabled: false\n#feature:\n#  enabled: false\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	content := mustRead(t, filepath.Join(dir, ".cloudopsworks/cloudopsworks-ci.yaml"))
	if got := activeYAMLKeyCount(content, "feature"); got != 1 {
		t.Fatalf("fallback did not retain exactly one active local feature: %d\n%s", got, content)
	}
	if !strings.Contains(out.String(), "renderer-fallback: raw spans not provable") {
		t.Fatalf("missing explicit renderer-fallback warning: %s", out.String())
	}
}

func TestUpgradeCloudOpsworksConfigRendersSampleShapedGlobalInputsWithoutFallback(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "cloud: aws\ncloud_type: kubernetes\nsemgrep:\n  enabled: true\nsonarqube:\n  enabled: true\n  branch_disabled: true\ndependencyTrack:\n  enabled: true\n  type: Application\ncustom_install_command: npm ci\npreview:\n  enabled: true\n  domain: preview.dev.example\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-global.yaml", "# Set Semgrep processing to true if want to enable\n#semgrep:\n#  enabled: true\n# Set Sonarqube processing to true if want to enable\n#sonarqube:\n#  enabled: true\n#  quality_gate_enabled: false\n#  sources_path: /\n#  branch_disabled: true\n# JIRA Integration for Release Management\n#jira:\n#  enabled: true\n# Set DependencyTrack processing to false if want to disable\n#dependencyTrack:\n#  enabled: true\n#  type: Application\n# Custom NPM Install and Build commands\n#custom_install_command: npm install\n# Preview configuration\n#preview:\n#  enabled: true\n#  domain: example.com\n#  azure:\n#    resource_group: PREVIEW_RG\ncloud: aws\ncloud_type: kubernetes\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	content := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-global.yaml"))
	for _, placeholder := range []string{"#semgrep:", "#sonarqube:", "#dependencyTrack:", "#custom_install_command:", "#preview:"} {
		if strings.Contains(content, placeholder) {
			t.Fatalf("active global input retained structural placeholder %q:\n%s", placeholder, content)
		}
	}
	assertContainsAll(t, content, "semgrep:", "branch_disabled: true", "dependencyTrack:", "custom_install_command: npm ci", "preview:", "domain: preview.dev.example", "#  quality_gate_enabled: false", "#  sources_path: /", "# JIRA Integration for Release Management", "#  azure:")
	sonarAt, qualityAt, sourcesAt, jiraAt := strings.Index(content, "sonarqube:"), strings.Index(content, "#  quality_gate_enabled: false"), strings.Index(content, "#  sources_path: /"), strings.Index(content, "# JIRA Integration for Release Management")
	if sonarAt < 0 || qualityAt <= sonarAt || sourcesAt <= qualityAt || jiraAt <= sourcesAt {
		t.Fatalf("target-only Sonarqube comments escaped their owning section:\n%s", content)
	}
	if !(strings.Index(content, "semgrep:") < strings.Index(content, "sonarqube:") && strings.Index(content, "sonarqube:") < strings.Index(content, "dependencyTrack:") && strings.Index(content, "dependencyTrack:") < strings.Index(content, "custom_install_command:") && strings.Index(content, "custom_install_command:") < strings.Index(content, "preview:")) {
		t.Fatalf("active global inputs were not rendered at target positions:\n%s", content)
	}
	if strings.Contains(out.String(), "renderer-fallback") {
		t.Fatalf("global inputs renderer took fallback: %s", out.String())
	}
	assertContainsAll(t, content,
		"semgrep:\n  enabled: true",
		"sonarqube:\n  enabled: true", "branch_disabled: true",
		"preview:\n  enabled: true\n  domain: preview.dev.example",
	)
}

func TestUpgradeCloudOpsworksConfigRendersSampleShapedEnvironmentInputsWithoutFallback(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "cloud: aws\ncloud_type: kubernetes\n")
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-dev.yaml", "environment: dev\nrunner_set: finconecta-dev-runners\nsecret_files:\n  enabled: true\n  files_path: ./secrets\nconfig_map:\n  enabled: false\n  files_path: values/configmaps\nhelm_values_overrides:\n  'image.repository': 123.dkr.ecr.us-east-1.amazonaws.com/finconecta-team/forward-async-processor-ms\naws:\n  region: us-east-1\n  build_sts_role_arn: arn:aws:iam::123:role/build\n  secrets_path_filter:\n    - /forward/dev/ai-agents/openrouter\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-KUBERNETES-ENV.yaml", "environment: dev\n#runner_set: RUNNER-ENV\nsecret_files:\n  enabled: true\n  #files_path: values/secrets\n  mount_point: /app/secrets\nconfig_map:\n  enabled: false\n  #files_path: values/configmaps\n  mount_point: /app/configmap\nhelm_values_overrides:\n  'image.repository': HELM_IMAGE\n# For AWS\n#aws:\n#  region: AWS_REGION\n#  sts_role_arn: BUILD_AWS_STS_ROLE_ARN\n#  build_sts_role_arn: BUILD_AWS_STS_ROLE_ARN\n#  secrets_path_filter:\n#    - /secrets\n# For GCP\n#gcp:\n#  region: GCP_REGION\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	content := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-dev.yaml"))
	for _, placeholder := range []string{"#runner_set:", "#files_path:", "#aws:"} {
		if strings.Contains(content, placeholder) {
			t.Fatalf("active environment input retained structural placeholder %q:\n%s", placeholder, content)
		}
	}
	assertContainsAll(t, content, "runner_set: finconecta-dev-runners", "secret_files:", "files_path: ./secrets", "config_map:", "files_path: values/configmaps", "aws:", "build_sts_role_arn: arn:aws:iam::123:role/build", "mount_point: /app/secrets", "mount_point: /app/configmap", "helm_values_overrides:", "'image.repository': 123.dkr.ecr.us-east-1.amazonaws.com/finconecta-team/forward-async-processor-ms", "secrets_path_filter:", "- /forward/dev/ai-agents/openrouter")
	assertContainsAll(t, content, "#  sts_role_arn: BUILD_AWS_STS_ROLE_ARN", "# For GCP")
	awsHeaderAt, awsAt, stsAt, gcpAt := strings.Index(content, "# For AWS"), strings.Index(content, "aws:"), strings.Index(content, "#  sts_role_arn: BUILD_AWS_STS_ROLE_ARN"), strings.Index(content, "# For GCP")
	if awsHeaderAt < 0 || awsAt <= awsHeaderAt || stsAt <= awsAt || gcpAt <= stsAt {
		t.Fatalf("target-only AWS defaults escaped their owning section:\n%s", content)
	}
	if strings.Contains(out.String(), "renderer-fallback") {
		t.Fatalf("environment inputs renderer took fallback: %s", out.String())
	}
	assertContainsAll(t, content,
		"aws:\n  region: us-east-1",
		"\n  build_sts_role_arn: arn:aws:iam::123:role/build",
		"\n  secrets_path_filter:\n    - /forward/dev/ai-agents/openrouter",
	)
}

func TestUpgradeCloudOpsworksConfigRendersPadOneCommentedMappingWithNormalIndentation(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "feature:\n  enabled: true\n  options:\n    - runtime\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-global.yaml", "# feature:\n#   enabled: false\n#   options:\n#     - default\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatalf("upgradeCloudOpsworksConfig() error = %v", err)
	}
	content := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-global.yaml"))
	assertContainsAll(t, content, "feature:\n  enabled: true\n  options:\n    - runtime")
	if strings.Contains(content, "\n feature:") || strings.Contains(content, "\n   enabled:") || strings.Contains(content, "\n     - runtime") {
		t.Fatalf("pad-1 commented mapping rendered with residual comment padding:\n%s", content)
	}
	if strings.Contains(out.String(), "renderer-fallback") {
		t.Fatalf("pad-1 mapping renderer took fallback: %s", out.String())
	}
}

func configUpgradeRunner(t *testing.T, dryRun bool) (string, *Runner, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "build.yml"), "name: build\n")
	mustWrite(t, filepath.Join(dir, ".template", "Makefile"), "all:\n\t@true\n")
	out := &bytes.Buffer{}
	runner, err := NewRunner(Options{WorkDir: dir, DryRun: dryRun, Stdout: out, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return dir, runner, out
}

func writeConfigFixture(t *testing.T, dir, relative, content string) {
	mustWrite(t, filepath.Join(dir, filepath.FromSlash(relative)), content)
}

func activeYAMLKeyCount(content, key string) int {
	count := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == key+":" {
			count++
		}
	}
	return count
}

func indentationOf(content, wanted string) int {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == wanted {
			return len(line) - len(strings.TrimLeft(line, " "))
		}
	}
	return -1
}

func assertContainsAll(t *testing.T, content string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Fatalf("content missing %q:\n%s", want, content)
		}
	}
}

func TestEvalTemplateVersionUsesWorkflowBlueprintReferencesNotApplicationVersion(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v9.9.9\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "deploy.yml"), "uses: cloudopsworks/blueprints/cd/checkout@v5.9\n")
	version, err := runner.EvalTemplateVersion()
	if err != nil {
		t.Fatalf("EvalTemplateVersion() error = %v", err)
	}
	if version != "" {
		t.Fatalf("generation = %q, want pre-v5.10 from workflow v5.9", version)
	}
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "deploy.yml"), "blueprint_ref: v5.10\n")
	version, err = runner.EvalTemplateVersion()
	if err != nil || version != "v5.10" {
		t.Fatalf("generation = %q, %v; want v5.10", version, err)
	}
}

func TestEvalTemplateVersionRejectsMixedWorkflowGenerations(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "one.yml"), "uses: cloudopsworks/blueprints/cd/checkout@v5.9\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "two.yml"), "blueprint_ref: v5.10\n")
	if _, err := runner.EvalTemplateVersion(); err == nil {
		t.Fatal("mixed target references accepted")
	}
}

func TestUpgradeCloudOpsworksConfigMapsDeviceFarmScaffolds(t *testing.T) {
	for _, test := range []struct{ name, global, scaffold string }{
		{"android", "android: {}\n", "inputs-ANDROID-ENV.yaml"},
		{"xcode", "xcode: {}\n", "inputs-XCODE-ENV.yaml"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, runner, _ := configUpgradeRunner(t, false)
			writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", test.global)
			writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-dev.yaml", "configured: local\n")
			writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/"+test.scaffold, "# device baseline\nconfigured: target\ntarget_default: kept\n")
			if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
				t.Fatal(err)
			}
			assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks", "vars", "inputs-dev.yaml")), "# device baseline", "configured: local", "target_default: kept")
		})
	}
}

func TestTopLevelInputPriorityExactHeaderMobileThenGeneric(t *testing.T) {
	for _, test := range []struct{ name, local, global, want string }{
		{"exact", "value: local\n", "cloud: library\nandroid: {}\n", "# exact"},
		{"header", "# Agents: cloud=library ; cloud_type=header\nvalue: local\n", "cloud: library\nandroid: {}\n", "# header"},
		{"android", "value: local\n", "cloud: library\nandroid: {}\n", "# android"},
		{"xcode", "value: local\n", "cloud: library\nxcode: {}\n", "# xcode"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, runner, _ := configUpgradeRunner(t, false)
			writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", test.global)
			writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-dev.yaml", test.local)
			if test.name == "exact" {
				writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-dev.yaml", "# exact\nvalue: target\n")
			}
			writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-HEADER-ENV.yaml", "# Agents: cloud=library ; cloud_type=header\n# header\nvalue: target\n")
			writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-ANDROID-ENV.yaml", "# android\nvalue: target\n")
			writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-XCODE-ENV.yaml", "# xcode\nvalue: target\n")
			if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
				t.Fatal(err)
			}
			if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "vars", "inputs-dev.yaml")); !strings.Contains(got, test.want) {
				t.Fatalf("got %q, want %s", got, test.want)
			}
		})
	}
}

func TestApplyVersionedTemplateMigratesLegacyGitVersionYAML(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	mustWrite(t, filepath.Join(dir, ".github", "_VERSION"), "v5.9.0\n")
	mustWrite(t, filepath.Join(dir, ".github", ".golang"), "")
	writeConfigFixture(t, dir, ".github/gitversion_trunkbased.yaml", "mode: ContinuousDeployment\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.0\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/gitversion_trunkbased.yaml", "mode: Mainline\nnext-version: 5.10.0\n")
	state := RepositoryState{WorkDir: dir, BlueprintPath: ".github", VersionFile: ".github/_VERSION", Pre510: true}
	if err := runner.applyVersionedTemplate(Template{Name: "go", Versioned: true}, state, "v5.10"); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dir, ".github", "gitversion_trunkbased.yaml")) {
		t.Fatal("legacy GitVersion YAML was not migrated")
	}
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks", "gitversion_trunkbased.yaml")), "mode: ContinuousDeployment", "next-version: 5.10.0")
}

func TestApplyVersionedTemplateDryRunPrintsStagingForPlannedNewConfiguration(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, true)
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.1\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.2\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/cloudopsworks-ci.yaml", "pipeline: target\n")
	if err := runner.applyVersionedTemplate(Template{Name: "go", Versioned: true}, RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks", VersionFile: ".cloudopsworks/_VERSION"}, "v5.10"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "git --literal-pathspecs add -- .cloudopsworks/cloudopsworks-ci.yaml") {
		t.Fatalf("dry-run omitted staging for planned new YAML:\n%s", out.String())
	}
	if exists(filepath.Join(dir, ".cloudopsworks", "cloudopsworks-ci.yaml")) {
		t.Fatal("dry-run created planned configuration")
	}
}

func TestEvalTemplateVersionFailsClosedForMissingOrCommentedReferences(t *testing.T) {
	for _, workflow := range []string{
		"name: no blueprint\n",
		"# uses: cloudopsworks/blueprints/cd/checkout@v5.10\n# blueprint_ref: v5.10\n",
	} {
		dir, runner, _ := configUpgradeRunner(t, false)
		mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.9\n")
		mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "build.yml"), workflow)
		if _, err := runner.EvalTemplateVersion(); err == nil {
			t.Fatalf("EvalTemplateVersion accepted inactive/missing target evidence: %q", workflow)
		}
	}
}

func TestPostMigrationStateDoesNotDependOnDryRunFilesystem(t *testing.T) {
	state := postMigrationState(RepositoryState{BlueprintPath: ".github", VersionFile: ".github/_VERSION", Pre510: true, Version: "v5.9.0"})
	if state.BlueprintPath != ".cloudopsworks" || state.VersionFile != ".cloudopsworks/_VERSION" || state.Pre510 {
		t.Fatalf("post-migration state = %#v", state)
	}
}

func TestBoilerplateSubtreeIsExcludedFromBothYAMLInventoriesAndRefreshedExactly(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	boilerplate := ".cloudopsworks/boilerplate"
	invalidTemplate := "{{ if .Values.enabled }}\nvalue: target\n{{ end }}\n"
	invalidLocal := "{{ if .Values.local }}\nvalue: local\n{{ end }}\n"
	mustWrite(t, filepath.Join(dir, boilerplate, "rendered.yaml"), invalidLocal)
	mustWrite(t, filepath.Join(dir, boilerplate, "nested", "_VERSION"), "local nested marker\n")
	mustWrite(t, filepath.Join(dir, ".template", boilerplate, "rendered.yaml"), invalidTemplate)
	mustWrite(t, filepath.Join(dir, ".template", boilerplate, "nested", "_VERSION"), "target nested marker\n")
	writeConfigFixture(t, dir, ".cloudopsworks/cloudopsworks-ci.yaml", "pipeline: local\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/cloudopsworks-ci.yaml", "pipeline: target\n")

	state := RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks", VersionFile: ".cloudopsworks/_VERSION"}
	if _, err := runner.buildCloudOpsworksConfigPlanExcluding(state, boilerplate); err != nil {
		t.Fatalf("buildCloudOpsworksConfigPlanExcluding() parsed opaque boilerplate YAML: %v", err)
	}
	if err := runner.applyMergedBoilerplate(Template{BoilerplatePathV510Plus: boilerplate}); err != nil {
		t.Fatalf("applyMergedBoilerplate() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, boilerplate, "rendered.yaml")); got != invalidTemplate {
		t.Fatalf("opaque boilerplate YAML = %q, want exact target bytes", got)
	}
	if got := mustRead(t, filepath.Join(dir, boilerplate, "nested", "_VERSION")); got != "target nested marker\n" {
		t.Fatalf("nested _VERSION = %q, want exact target asset", got)
	}
}

func TestPre510RootYAMLIsMergedOnlyWhenTargetHasExactCounterpart(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".github/service.yaml", "local: kept\n")
	writeConfigFixture(t, dir, ".github/unmatched.yaml", "local: preserve\n")
	writeConfigFixture(t, dir, ".github/workflows/operational.yaml", "name: do-not-read\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/service.yaml", "local: target\ntarget: default\n")
	state := RepositoryState{WorkDir: dir, BlueprintPath: ".github", VersionFile: ".github/_VERSION", Pre510: true}
	plan, err := runner.buildCloudOpsworksConfigPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.applyCloudOpsworksConfigPlan(plan); err != nil {
		t.Fatal(err)
	}
	got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "service.yaml"))
	assertContainsAll(t, got, "local: kept", "target: default")
	if exists(filepath.Join(dir, ".cloudopsworks", "unmatched.yaml")) {
		t.Fatal("unmatched legacy root YAML was migrated")
	}
	if !strings.Contains(out.String(), "unmatched.yaml") {
		t.Fatalf("unmatched legacy root YAML did not warn: %s", out.String())
	}
}

func TestConfigPlanRejectsLaterInvalidDestinationBeforeAnyWrite(t *testing.T) {
	dir, _, _ := configUpgradeRunner(t, false)
	first := filepath.Join(dir, ".cloudopsworks", "first.yaml")
	blocked := filepath.Join(dir, ".cloudopsworks", "blocked.yaml")
	mustWrite(t, first, "original: true\n")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := cloudOpsworksConfigPlan{writes: []yamlWrite{
		{path: first, content: []byte("changed: true\n"), mode: 0o644, name: "first.yaml"},
		{path: blocked, content: []byte("blocked: true\n"), mode: 0o644, name: "blocked.yaml"},
	}}
	if err := validateYAMLWriteDestinations(dir, plan.writes); err == nil {
		t.Fatal("later directory destination was accepted")
	}
	if got := mustRead(t, first); got != "original: true\n" {
		t.Fatalf("earlier YAML changed before plan validation: %q", got)
	}
}

func TestUpgradeCloudOpsworksConfigRejectsYAMLSymlinkBeforeReadOrMerge(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	external := filepath.Join(t.TempDir(), "external-invalid.yaml")
	mustWrite(t, external, "invalid: [\n")
	writeConfigFixture(t, dir, ".cloudopsworks/cloudopsworks-ci.yaml", "local: unchanged\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/cloudopsworks-ci.yaml", "target: baseline\n")
	link := filepath.Join(dir, ".cloudopsworks", "vars", "inputs-global.yaml")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	state := RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}
	err := runner.upgradeCloudOpsworksConfig(state)
	if err == nil || !strings.Contains(err.Error(), "unsafe YAML source") {
		t.Fatalf("error = %v, want unsafe YAML source", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "cloudopsworks-ci.yaml")); got != "local: unchanged\n" {
		t.Fatalf("configuration changed after unsafe symlink: %q", got)
	}
}

func TestUpgradeCloudOpsworksConfigMobileAmbiguityPreservesLocalInsteadOfGeneric(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, false)
	local := "configured: local\n"
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "cloud: none\ncloud_type: library\nandroid: true\nxcode: {}\n")
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-dev.yaml", local)
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-ANDROID-ENV.yaml", "device: android\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-XCODE-ENV.yaml", "device: xcode\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-LIB-ENV.yaml", "device: generic\n")
	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-dev.yaml")); got != local {
		t.Fatalf("ambiguous mobile input = %q, want local bytes preserved", got)
	}
	if !strings.Contains(out.String(), "inputs-dev.yaml path mobile") {
		t.Fatalf("missing mobile ambiguity warning: %s", out.String())
	}
}

func TestUpgradeCloudOpsworksConfigInactiveMobileKeysFallBackToGeneric(t *testing.T) {
	for _, global := range []string{
		"cloud: none\ncloud_type: library\nandroid: false\nxcode: null\n",
		"cloud: none\ncloud_type: library\nandroid: disabled\nxcode: false\n",
	} {
		dir, runner, _ := configUpgradeRunner(t, false)
		writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", global)
		writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-dev.yaml", "configured: local\n")
		writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-LIB-ENV.yaml", "device: generic\n")
		if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
			t.Fatal(err)
		}
		assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-dev.yaml")), "device: generic", "configured: local")
	}
}

func TestUpgradeCloudOpsworksConfigRejectsSymlinkedTemplateRoot(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	externalRoot := filepath.Join(t.TempDir(), "external-cloudopsworks")
	mustWrite(t, filepath.Join(externalRoot, "vars", "inputs-global.yaml"), "invalid: [\n")
	templateRoot := filepath.Join(dir, ".template", ".cloudopsworks")
	if err := os.RemoveAll(templateRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalRoot, templateRoot); err != nil {
		t.Fatal(err)
	}
	writeConfigFixture(t, dir, ".cloudopsworks/cloudopsworks-ci.yaml", "local: unchanged\n")
	err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"})
	if err == nil || !strings.Contains(err.Error(), "unsafe YAML source") {
		t.Fatalf("error = %v, want unsafe YAML source", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "cloudopsworks-ci.yaml")); got != "local: unchanged\n" {
		t.Fatalf("configuration changed after unsafe root symlink: %q", got)
	}
}

func TestUpgradeCloudOpsworksConfigAmbiguousLocalAgentsHeaderPreservesLocal(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, false)
	local := "# Agents: cloud=library ; cloud_type=header\nconfigured: local\n"
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "cloud: none\ncloud_type: library\nandroid: true\n")
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-dev.yaml", local)
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-HEADER-A-ENV.yaml", "# Agents: cloud=library ; cloud_type=header\nvalue: first\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-HEADER-B-ENV.yaml", "# Agents: cloud=library ; cloud_type=header\nvalue: second\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-ANDROID-ENV.yaml", "value: mobile\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-LIB-ENV.yaml", "value: generic\n")
	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-dev.yaml")); got != local {
		t.Fatalf("ambiguous Agents input = %q, want local bytes preserved", got)
	}
	if !strings.Contains(out.String(), "inputs-dev.yaml path header") {
		t.Fatalf("missing Agents ambiguity warning: %s", out.String())
	}
}

func TestUpgradeCloudOpsworksConfigResolvesPipeDelimitedLocalAgentsHeader(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "cloud: none\ncloud_type: library\n")
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-dev.yaml", "# Agents: cloud=aws|gcp ; cloud_type=kubernetes\nconfigured: local\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-KUBERNETES-ENV.yaml", "# Agents: cloud=aws|gcp ; cloud_type=kubernetes\n# kubernetes baseline\nconfigured: target\nnamespace: default\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-LIB-ENV.yaml", "# library baseline\nconfigured: target\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatal(err)
	}
	content := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-dev.yaml"))
	assertContainsAll(t, content, "# kubernetes baseline", "configured: local", "namespace: default")
	if strings.Contains(content, "# library baseline") {
		t.Fatalf("pipe-delimited Agents metadata fell back to library baseline:\n%s", content)
	}
}

func TestUpgradeCloudOpsworksConfigConflictingLocalAgentsHeadersPreserveLocal(t *testing.T) {
	dir, runner, out := configUpgradeRunner(t, false)
	local := "# Agents: cloud=aws ; cloud_type=lambda\n# Agents: cloud=gcp ; cloud_type=cloudrun\nconfigured: local\n"
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-global.yaml", "cloud: none\ncloud_type: library\nandroid: true\n")
	writeConfigFixture(t, dir, ".cloudopsworks/vars/inputs-dev.yaml", local)
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-LAMBDA-ENV.yaml", "# Agents: cloud=aws ; cloud_type=lambda\nvalue: lambda\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-CLOUDRUN.yaml", "# Agents: cloud=gcp ; cloud_type=cloudrun\nvalue: cloudrun\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-ANDROID-ENV.yaml", "value: mobile\n")
	writeConfigFixture(t, dir, ".template/.cloudopsworks/vars/inputs-LIB-ENV.yaml", "value: library\n")

	if err := runner.upgradeCloudOpsworksConfig(RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks"}); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks/vars/inputs-dev.yaml")); got != local {
		t.Fatalf("conflicting Agents input = %q, want local bytes preserved", got)
	}
	if !strings.Contains(out.String(), "inputs-dev.yaml path header") {
		t.Fatalf("missing conflicting Agents warning: %s", out.String())
	}
}

// These direct renderer regressions cover raw-layout safety cases that are hard
// to express through target-file selection. They still parse the same YAML ASTs
// used by the upgrade path.
func TestRenderYAMLTargetBaselineAdvancedRawSafety(t *testing.T) {
	render := func(t *testing.T, target, local string) string {
		t.Helper()
		var targetNode, localNode yaml.Node
		if err := yaml.Unmarshal([]byte(target), &targetNode); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal([]byte(local), &localNode); err != nil {
			t.Fatal(err)
		}
		out, ok := renderYAMLTargetBaseline([]byte(target), []byte(local), &targetNode, &localNode)
		if !ok {
			t.Fatalf("renderer unexpectedly fell back:\ntarget:\n%s\nlocal:\n%s", target, local)
		}
		if !renderedYAMLSemanticallyMatches(out, &targetNode, &localNode) {
			t.Fatalf("raw output was not semantically merged:\n%s", out)
		}
		return string(out)
	}
	t.Run("inserts nested local-only value before target comments", func(t *testing.T) {
		out := render(t, "parent:\n  known: target\n  # documented optional default\n  #optional: false\nnext: target\n", "parent:\n  known: local\n  local_only: keep\nnext: local\n")
		if !strings.Contains(out, "  local_only: keep\n  # documented optional default") {
			t.Fatalf("local child displaced target comments:\n%s", out)
		}
	})
	t.Run("activates commented nested sequence atomically", func(t *testing.T) {
		out := render(t, "parent:\n  #items: # target note\n  #  - template\n  #  - second\n", "parent:\n  items:\n    - one\n    - two\n")
		assertContainsAll(t, out, "items:", "    - one", "    - two")
		if strings.Contains(out, "#items:") {
			t.Fatalf("commented sequence header survived:\n%s", out)
		}
	})
	t.Run("uses target indent for scalar replacement", func(t *testing.T) {
		out := render(t, "parent:\n    child: target # target note\n", "parent:\n  child: local\n")
		assertContainsAll(t, out, "    child: local # target note")
	})
	t.Run("replaces literal and folded scalar spans", func(t *testing.T) {
		out := render(t, "literal: | # keep literal comment\n  old\nfolded: >\n  old folded\n", "literal: |\n  first\n  second\nfolded: >\n  fresh folded\n  content\n")
		assertContainsAll(t, out, "literal: | # keep literal comment", "  first", "  second", "folded: >", "  fresh folded", "  content")
		if strings.Contains(out, "old folded") {
			t.Fatalf("old multiline tail survived:\n%s", out)
		}
	})
	t.Run("preserves target sequence key comments on type change", func(t *testing.T) {
		out := render(t, "items: # target key note\n  - old\n", "items:\n  named: replacement\n")
		assertContainsAll(t, out, "items: # target key note", "  named: replacement")
	})
	t.Run("does not consume nested prose", func(t *testing.T) {
		out := render(t, "parent:\n  # endpoint: this is documentation only\n  known: target\n", "parent:\n  endpoint: https://runtime.example\n  known: local\n")
		assertContainsAll(t, out, "# endpoint: this is documentation only", "endpoint: https://runtime.example")
		if strings.Contains(out, "# endpoint: https://runtime.example") {
			t.Fatalf("nested prose was consumed:\n%s", out)
		}
	})
	t.Run("distinguishes dotted keys from nested keys", func(t *testing.T) {
		out := render(t, "parent:\n  a.b: target-dot\n  a:\n    b: target-nested\n", "parent:\n  a.b: local-dot\n  a:\n    b: local-nested\n")
		assertContainsAll(t, out, "a.b: local-dot", "b: local-nested")
	})
	t.Run("prefers active spans over stale commented duplicates", func(t *testing.T) {
		target := "#ingress:\n#  enabled: false\n#aws:\n#  region: template-region\n"
		local := "ingress:\n  enabled: true\n#ingress:\n#  enabled: false\naws:\n  region: runtime-region\n#aws:\n#  region: template-region\n"
		out := render(t, target, local)
		assertContainsAll(t, out, "ingress:\n  enabled: true", "aws:\n  region: runtime-region")
		if strings.Contains(out, "#ingress:") || strings.Contains(out, "#aws:") {
			t.Fatalf("stale commented duplicate displaced active local span:\n%s", out)
		}
	})
	t.Run("reindents structural comments without removing their marker", func(t *testing.T) {
		got := reindentRawLines([]string{"  env:", "#        my-target: template"}, 0)
		want := []string{"env:", "#     my-target: template"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("comment-aware reindent = %#v, want %#v", got, want)
		}
	})
	t.Run("activates spaced nested commented mapping at its target position", func(t *testing.T) {
		target := "cd:\n  deployments:\n    release:\n#      targets:\n#        - template\nnext: template\n"
		local := "cd:\n  deployments:\n    release:\n      targets:\n        - runtime\nnext: local\n"
		out := render(t, target, local)
		if !strings.Contains(out, "      targets:\n        - runtime") || strings.Contains(out, "#      targets:") {
			t.Fatalf("spaced nested default was not activated in place:\n%s", out)
		}
	})
	t.Run("does not copy stale trailing commented mappings after active sequence", func(t *testing.T) {
		target := "env:\n#  - template\n#hpa:\n#  enabled: false\n#serviceAccount:\n#  create: false\nnext: template\n"
		local := "env:\n  - runtime # retained\n  #hpa:\n  #  enabled: true\n  #serviceAccount:\n  #  create: true\nnext: local\n"
		out := render(t, target, local)
		assertContainsAll(t, out, "env:\n  - runtime # retained", "#hpa:\n#  enabled: false", "#serviceAccount:\n#  create: false", "next: local")
		if strings.Contains(out, "  #hpa:") || strings.Contains(out, "  #serviceAccount:") {
			t.Fatalf("stale local comments were copied with env sequence:\n%s", out)
		}
	})
	t.Run("retains target examples after an active env sequence across two passes", func(t *testing.T) {
		target := "env:\n#  - template\n#hpa:\n#  enabled: false\n# Environment target examples.\nnext: template\n"
		local := "env:\n  - runtime\n  #hpa:\n  #  enabled: true\nnext: local\n"
		out := render(t, target, local)
		assertContainsAll(t, out, "env:\n  - runtime", "#hpa:\n#  enabled: false", "# Environment target examples.")
		var targetNode, localNode yaml.Node
		if err := yaml.Unmarshal([]byte(target), &targetNode); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal([]byte(out), &localNode); err != nil {
			t.Fatal(err)
		}
		second, ok := renderYAMLTargetBaseline([]byte(target), []byte(out), &targetNode, &localNode)
		if !ok || string(second) != out {
			t.Fatalf("two-pass render was not byte-idempotent:\nfirst:\n%s\nsecond:\n%s", out, second)
		}
	})
	t.Run("activates commented root sequence before following prose idempotently", func(t *testing.T) {
		target := "#env:\n#  - template\n# Environment values consumed by the CI job.\nnext: template\n"
		local := "env:\n  - runtime\nnext: local\n"
		out := render(t, target, local)
		sequenceAt, proseAt, nextAt := strings.Index(out, "env:"), strings.Index(out, "# Environment values"), strings.Index(out, "next: local")
		if sequenceAt < 0 || proseAt <= sequenceAt || nextAt <= proseAt {
			t.Fatalf("sequence/prose order was not retained at its target position:\n%s", out)
		}
		var nextLocal yaml.Node
		if err := yaml.Unmarshal([]byte(out), &nextLocal); err != nil {
			t.Fatal(err)
		}
		var nextTarget yaml.Node
		if err := yaml.Unmarshal([]byte(target), &nextTarget); err != nil {
			t.Fatal(err)
		}
		second, ok := renderYAMLTargetBaseline([]byte(target), []byte(out), &nextTarget, &nextLocal)
		if !ok || string(second) != out {
			t.Fatalf("second render was not idempotent:\nfirst:\n%s\nsecond:\n%s", out, second)
		}
	})
	t.Run("partially activates a mapping without replacing target-only children", func(t *testing.T) {
		target := "#release:\n#  targets:\n#    my-target:\n#      guide: template-only\n#      retention: keep\n"
		local := "release:\n  targets:\n    live:\n      guide: runtime\n"
		out := render(t, target, local)
		assertContainsAll(t, out, "release:\n", "  targets:\n", "    live:\n      guide: runtime", "#    my-target:", "#      guide: template-only", "#      retention: keep")
		if strings.Contains(out, "#    live:") {
			t.Fatalf("active local child retained a commented placeholder:\n%s", out)
		}
		var targetNode, firstNode yaml.Node
		if err := yaml.Unmarshal([]byte(target), &targetNode); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal([]byte(out), &firstNode); err != nil {
			t.Fatal(err)
		}
		second, ok := renderYAMLTargetBaseline([]byte(target), []byte(out), &targetNode, &firstNode)
		if !ok || string(second) != out {
			t.Fatalf("partial activation was not byte-idempotent:\nfirst:\n%s\nsecond:\n%s", out, second)
		}
	})
	t.Run("normalizes malformed local collection indentation to target hierarchy", func(t *testing.T) {
		target := "#aws:\n#  secrets_path_filter:\n#    - template\n"
		local := "aws:\n    secrets_path_filter:\n        - /runtime/secret\n"
		out := render(t, target, local)
		assertContainsAll(t, out, "aws:\n  secrets_path_filter:\n    - /runtime/secret")
		if strings.Contains(out, "    secrets_path_filter:\n        - /runtime/secret") {
			t.Fatalf("malformed local indentation leaked into target baseline:\n%s", out)
		}
	})
	t.Run("normalizes inserted malformed mapping indentation", func(t *testing.T) {
		target := "#serviceAccount:\n#  create: false\n"
		local := "serviceAccount:\n    annotations:\n        role: runtime\n"
		out := render(t, target, local)
		assertContainsAll(t, out, "serviceAccount:\n  annotations:\n    role: runtime", "#  create: false")
		if strings.Contains(out, "    annotations:\n        role: runtime") {
			t.Fatalf("inserted mapping retained malformed indentation:\n%s", out)
		}
	})
	Run := func(t *testing.T, target, local string) string { return render(t, target, local) }
	t.Run("normalizes same-kind helm ingress sequence at target width", func(t *testing.T) {
		target := "ingress:\n  rules:\n    - host: template # target host note\n      paths:\n        - /template\n"
		local := "ingress:\n    rules:\n        - host: runtime\n          paths:\n              - /runtime\n"
		out := Run(t, target, local)
		want := "ingress:\n  rules:\n    - host: runtime # target host note\n      paths:\n        - /runtime\n"
		if out != want {
			t.Fatalf("same-kind ingress sequence layout =\n%s\nwant:\n%s", out, want)
		}
	})
	t.Run("normalizes active environment sequence layout", func(t *testing.T) {
		target := "env:\n  - name: TEMPLATE\n    value: template\n"
		local := "env:\n    - name: RUNTIME\n      value: runtime # local value note\n"
		out := Run(t, target, local)
		want := "env:\n  - name: RUNTIME\n    value: runtime # local value note\n"
		if out != want {
			t.Fatalf("active env layout =\n%s\nwant:\n%s", out, want)
		}
	})
	t.Run("normalizes local-only nested mapping at target child indent", func(t *testing.T) {
		target := "parent:\n  known: target\n"
		local := "parent:\n    known: runtime\n    local_only:\n        nested:\n            enabled: true\n"
		out := Run(t, target, local)
		assertContainsAll(t, out, "parent:\n  known: runtime\n  local_only:\n    nested:\n      enabled: true")
		if strings.Contains(out, "    local_only:") || strings.Contains(out, "        nested:") {
			t.Fatalf("local-only mapping leaked local indentation:\n%s", out)
		}
	})
	t.Run("consumes commented mapping-item sequence continuation and preserves note", func(t *testing.T) {
		target := "#rules:\n#  - host: template # item note\n#    paths:\n#      - /template\nnext: template\n"
		local := "rules:\n  - host: runtime\n    paths:\n      - /runtime\nnext: runtime\n"
		out := Run(t, target, local)
		assertContainsAll(t, out, "rules:\n  - host: runtime # item note\n    paths:\n      - /runtime\nnext: runtime")
		if strings.Contains(out, "#    paths:") || strings.Contains(out, "#      - /template") {
			t.Fatalf("commented sequence continuation was orphaned:\n%s", out)
		}
	})
	t.Run("collection render is byte-idempotent", func(t *testing.T) {
		target := "ingress:\n  rules:\n    - host: template # target note\n      paths:\n        - /template\n"
		local := "ingress:\n    rules:\n        - host: runtime\n          paths:\n              - /runtime\n"
		first := Run(t, target, local)
		var targetNode, firstNode yaml.Node
		if err := yaml.Unmarshal([]byte(target), &targetNode); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal([]byte(first), &firstNode); err != nil {
			t.Fatal(err)
		}
		second, ok := renderYAMLTargetBaseline([]byte(target), []byte(first), &targetNode, &firstNode)
		if !ok || string(second) != first {
			t.Fatalf("second collection render was not byte-identical:\nfirst:\n%s\nsecond:\n%s", first, second)
		}
	})
}

func TestRenderedYAMLSemanticGateRejectsMismatch(t *testing.T) {
	var target, local yaml.Node
	if err := yaml.Unmarshal([]byte("value: target\n"), &target); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte("value: local\n"), &local); err != nil {
		t.Fatal(err)
	}
	if renderedYAMLSemanticallyMatches([]byte("value: wrong\n"), &target, &local) {
		t.Fatal("semantic gate accepted a raw value that differs from merged YAML")
	}
}

func TestRenderYAMLTargetBaselinePreservesTargetTrailingLineFeedsAtEOF(t *testing.T) {
	for _, suffix := range []string{"", "\n", "\n\n"} {
		t.Run(fmt.Sprintf("%d trailing line feeds", len(suffix)), func(t *testing.T) {
			target := "final_key: template" + suffix
			local := "final_key: runtime\n"
			var targetNode, localNode yaml.Node
			if err := yaml.Unmarshal([]byte(target), &targetNode); err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal([]byte(local), &localNode); err != nil {
				t.Fatal(err)
			}

			first, ok := renderYAMLTargetBaseline([]byte(target), []byte(local), &targetNode, &localNode)
			if !ok {
				t.Fatalf("renderer unexpectedly fell back for target %q", target)
			}
			if got := trailingLineFeedCount(first); got != len(suffix) {
				t.Fatalf("trailing line feeds = %d, want %d; output %q", got, len(suffix), first)
			}
			if !strings.HasPrefix(string(first), "final_key: runtime") {
				t.Fatalf("EOF scalar was not replaced: %q", first)
			}

			var renderedNode yaml.Node
			if err := yaml.Unmarshal(first, &renderedNode); err != nil {
				t.Fatal(err)
			}
			second, ok := renderYAMLTargetBaseline([]byte(target), first, &targetNode, &renderedNode)
			if !ok || !bytes.Equal(second, first) {
				t.Fatalf("second render was not byte-idempotent:\nfirst: %q\nsecond: %q", first, second)
			}
		})
	}
}
