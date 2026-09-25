package repos

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// This mirrors the clean cognito-lambda-auth upgrade: the target owns an empty
// flow-style lambda.iam followed by documented defaults, while the legacy local
// configuration has a block-style IAM mapping with four-space outer indentation.
// That combination must not force yaml.Marshal fallback, which loses the target
// layout and root defaults.
func TestRenderYAMLTargetBaselineCognitoLambdaDefaults(t *testing.T) {
	target := `# This file contains the input variables for the Lambda environment.
# Agents: cloud=aws ; cloud_type=lambda
environment: "dev"
#runner_set: "RUNNER-ENV" # Optional: when using self-hosted runners
#disable_deploy: true
# Optional Node.JS Additional ENV variables
aws:
  region: us-east-1
  build_sts_role_arn: BUILD_AWS_STS_ROLE_ARN
lambda:
  iam: {}
  #  enabled: true
  #  execRole:
  #    enabled: true
  #    principals:
  #      - lambda.amazonaws.com
  #  statements:
  #    - effect: Allow
  #      action:
  #        - logs:CreateLogGroup
  #      resource:
  #        - "*"
  handler: index.handler
  runtime: nodejs24.x
  #timeout: 3
  #memory_size: 128
  #reserved_concurrency: -1
  # Schedule values can be enabled when this function needs a cron trigger.
  schedule:
    enabled: false        # (optional) Enable or disable singe schedule
  #  schedule_group: "my-schedule-group" # (optional) Schedule group name
  #  flexible:
  #    enabled: true
    expression: "rate(1 hour)"  # (optional) Schedule expression, can be cron or rate, required if enabled is true
`
	local := `environment: "dev"
runner_set: "finconecta-dev-runners"
disable_deploy: false
aws:
    region: us-east-1
    build_sts_role_arn: runtime-build-role
lambda:
    iam:
      enabled: true
      execRole:
        enabled: true
        principals:
          - lambda.amazonaws.com
          - apigateway.amazonaws.com
      statements:
        - effect: Allow
          action:
            - logs:CreateLogGroup
          resource:
            - "*"
    handler: index.handler
    runtime: nodejs24.x
    timeout: 10
    #memory_size: 256
    #reserved_concurrency: 2
    #schedule:
    #  enabled: true
    schedule:
      enabled: false        # (optional) Enable or disable singe schedule
      expression: "rate(1 hour)"  # (optional) Schedule expression, can be cron or rate, required if enabled is true
`
	// The clean Cognito base is CRLF while the target template is LF.
	local = strings.ReplaceAll(local, "\n", "\r\n")

	out := renderCognitoYAML(t, target, local)
	layout := strings.ReplaceAll(out, "\r\n", "\n")
	assertContainsAll(t, layout,
		"environment: \"dev\"\nrunner_set: \"finconecta-dev-runners\"\ndisable_deploy: false",
		"aws:\n  region: us-east-1\n  build_sts_role_arn: runtime-build-role",
		"lambda:\n  iam:",
		"  timeout: 10",
	)
	if strings.Contains(layout, "    region:") || strings.Contains(layout, "    timeout:") {
		t.Fatalf("four-space local hierarchy leaked into two-space Lambda template:\n%s", out)
	}
	if strings.Count(layout, "runner_set:") != 1 || strings.Count(layout, "disable_deploy:") != 1 || strings.Count(layout, "timeout:") != 1 {
		t.Fatalf("commented target defaults were not activated exactly once:\n%s", out)
	}
	if !strings.Contains(layout, "# Optional Node.JS Additional ENV variables") || strings.Contains(layout, "\nOptional Node.JS Additional ENV variables") {
		t.Fatalf("ordinary prose comment was not retained as prose:\n%s", out)
	}
	assertCognitoSecondRenderIsStable(t, target, out)
	for _, documentedDefault := range []string{
		"#  enabled: true",
		"#  execRole:",
		"#  statements:",
		"#memory_size: 128",
		"#reserved_concurrency: -1",
		"# Schedule values can be enabled when this function needs a cron trigger.",
		"# (optional) Enable or disable singe schedule",
	} {
		if got := strings.Count(layout, documentedDefault); got != 1 {
			t.Fatalf("documented Cognito default %q occurrences = %d, want 1:\n%s", documentedDefault, got, out)
		}
	}
}

func TestRenderYAMLTargetBaselineCognitoKeepsCommentedRootDefaults(t *testing.T) {
	target := "# runner_set: \"RUNNER-ENV\" # Optional: when using self-hosted runners\n#disable_deploy: true\nnext: template\n"
	local := "#runner_set: \"finconecta-dev-runners\"\n# disable_deploy: false\nnext: runtime\n"

	out := renderCognitoYAML(t, target, local)
	want := "# runner_set: \"RUNNER-ENV\" # Optional: when using self-hosted runners\n#disable_deploy: true\nnext: runtime\n"
	if out != want {
		t.Fatalf("commented Cognito defaults =\n%s\nwant:\n%s", out, want)
	}
	if strings.Contains(out, "runner_set: \"finconecta-dev-runners\"") || strings.Contains(out, "disable_deploy: false") {
		t.Fatalf("commented local defaults were promoted or duplicated:\n%s", out)
	}
}

func TestRenderYAMLTargetBaselineCognitoDropsStaleCommentedLambdaDefaults(t *testing.T) {
	target := "lambda:\n  handler: index.handler\n  runtime: nodejs24.x\n  #memory_size: 128\n  #reserved_concurrency: -1\n  #timeout: 3\n"
	local := "lambda:\n  handler: index.handler\n  runtime: nodejs24.x\n  #  memory_size: 128\n  #  reserved_concurrency: -1\n  timeout: 10\n"

	first := renderCognitoYAML(t, target, local)
	want := "lambda:\n  handler: index.handler\n  runtime: nodejs24.x\n  #memory_size: 128\n  #reserved_concurrency: -1\n  timeout: 10\n"
	if first != want {
		t.Fatalf("stale commented lambda defaults survived first render:\n%s\nwant:\n%s", first, want)
	}
	if strings.Contains(first, "#  memory_size") || strings.Contains(first, "#  reserved_concurrency") {
		t.Fatalf("stale locally spaced lambda defaults survived first render:\n%s", first)
	}
	assertCognitoSecondRenderIsStable(t, target, first)
}

func TestRenderYAMLTargetBaselineCognitoDropsStaleScheduleCommentBlock(t *testing.T) {
	target := "lambda:\n  schedule:\n    enabled: false # optional\n  #  schedule_group: \"x\" # docs\n  #  flexible:\n  #    enabled: true\n    expression: rate # docs\n"
	local := "lambda:\n  schedule:\n        enabled: false # optional\n        #  schedule_group: \"x\" # docs\n        #  flexible:\n        #    enabled: true\n        expression: rate # docs\n"

	first := renderCognitoYAML(t, target, local)
	if first != target {
		t.Fatalf("stale schedule comments survived or displaced target docs:\n%s\nwant:\n%s", first, target)
	}
	for _, stale := range []string{"    #  schedule_group", "    #  flexible:", "    #    enabled: true"} {
		if strings.Contains(first, stale) {
			t.Fatalf("stale local schedule block %q survived first render:\n%s", stale, first)
		}
	}
	for _, targetDoc := range []string{"  #  schedule_group", "  #  flexible:", "  #    enabled: true"} {
		if got := strings.Count(first, targetDoc); got != 1 {
			t.Fatalf("target schedule doc %q occurrences = %d, want 1:\n%s", targetDoc, got, first)
		}
	}
	assertCognitoSecondRenderIsStable(t, target, first)
}

func TestRenderYAMLTargetBaselineCognitoCollapsesRepeatedInlineComments(t *testing.T) {
	target := "lambda:\n  schedule:\n    enabled: false # optional\n    expression: rate # Required\n  frontend: template # frontend comment\n"
	local := strings.ReplaceAll("lambda:\n  schedule:\n    enabled: false # optional; # optional\n    expression: rate # Required; # Required\n  frontend: runtime # frontend comment; local rationale\n", "\n", "\r\n")

	first := renderCognitoYAML(t, target, local)
	stable := "lambda:\n  schedule:\n    enabled: false # optional\n    expression: rate # Required\n  frontend: runtime # frontend comment; # local rationale\n"
	if strings.ReplaceAll(first, "\r\n", "\n") != stable {
		t.Fatalf("repeated target/local inline comments were not collapsed:\n%s\nwant:\n%s", first, stable)
	}
	if got := strings.Count(first, "local rationale"); got != 1 {
		t.Fatalf("distinct local inline comment occurrences = %d, want 1:\n%s", got, first)
	}
	assertCognitoSecondRenderIsStable(t, target, first)
}

func TestRenderYAMLTargetBaselinePreservesInlineCommentLexicalSpacing(t *testing.T) {
	for _, test := range []struct {
		name, comment string
	}{
		{name: "compact", comment: "#sample regex: ^[a-z]+$"},
		{name: "spaced", comment: "# sample regex: ^[a-z]+$"},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := "pattern: template " + test.comment + "\n"
			local := "pattern: runtime " + test.comment + "\n"

			first := renderCognitoYAML(t, target, local)
			want := "pattern: runtime " + test.comment + "\n"
			if first != want {
				t.Fatalf("inline comment spelling changed:\n%s\nwant:\n%s", first, want)
			}
			assertCognitoSecondRenderIsStable(t, target, first)
		})
	}
}

func TestRenderYAMLTargetBaselineSelectsCompatibleCommentedAlternative(t *testing.T) {
	target := `golang:
  main_file: .
#  goreleaser: true # deprecated scalar form
#  goreleaser:      # replacement mapping form
#    enabled: true
#semgrep:
#  enabled: true
cloud: aws | none
`
	local := `golang:
  main_file: .
  goreleaser: true
semgrep:
  enabled: true
cloud: none
`

	first := renderCognitoYAML(t, target, local)
	assertContainsAll(t, first,
		"golang:\n  main_file: .\n  goreleaser: true",
		"#  goreleaser:      # replacement mapping form\n#    enabled: true",
		"semgrep:\n  enabled: true",
		"cloud: none",
	)
	if strings.Contains(first, "\n    goreleaser:") || strings.Contains(first, "\n    enabled:") {
		t.Fatalf("fallback indentation leaked into compatible commented alternative:\n%s", first)
	}
	assertCognitoSecondRenderIsStable(t, target, first)
}

func renderCognitoYAML(t *testing.T, target, local string) string {
	t.Helper()
	var targetNode, localNode yaml.Node
	if err := yaml.Unmarshal([]byte(target), &targetNode); err != nil {
		t.Fatalf("parse target: %v", err)
	}
	if err := yaml.Unmarshal([]byte(local), &localNode); err != nil {
		t.Fatalf("parse local: %v", err)
	}
	out, ok := renderYAMLTargetBaseline([]byte(target), []byte(local), &targetNode, &localNode)
	if !ok {
		t.Fatalf("renderer unexpectedly fell back:\ntarget:\n%s\nlocal:\n%s", target, local)
	}
	if !renderedYAMLSemanticallyMatches(out, &targetNode, &localNode) {
		t.Fatalf("raw render changed merged Cognito values:\n%s", out)
	}
	return string(out)
}

func assertCognitoSecondRenderIsStable(t *testing.T, target, first string) {
	t.Helper()
	var targetNode, firstNode yaml.Node
	if err := yaml.Unmarshal([]byte(target), &targetNode); err != nil {
		t.Fatalf("parse target for second render: %v", err)
	}
	if err := yaml.Unmarshal([]byte(first), &firstNode); err != nil {
		t.Fatalf("parse first render: %v", err)
	}
	second, ok := renderYAMLTargetBaseline([]byte(target), []byte(first), &targetNode, &firstNode)
	if !ok {
		t.Fatalf("second Cognito render unexpectedly fell back:\n%s", first)
	}
	if string(second) != first {
		t.Fatalf("second Cognito render was not byte-identical:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
