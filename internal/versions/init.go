package versions

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"tronador-cli/internal/replacement"
)

const cloudOpsWorksDir = ".cloudopsworks"

var wowHeader = regexp.MustCompile(`(?mi)^\s*#\s*Agents:\s*WayOfWork\s*=\s*([A-Za-z]+)\s*$`)

// SelectWayOfWork presents the dependency-free interactive selector used when
// CI configuration explicitly disables GitFlow and the caller did not choose a
// workflow. It deliberately accepts only one numeric selection.
func SelectWayOfWork(in io.Reader, out io.Writer) (WayOfWork, error) {
	if in == nil || out == nil {
		return "", errors.New("interactive workflow selection needs input and output")
	}
	fmt.Fprintln(out, "Select a way of work:")
	fmt.Fprintln(out, "  1) gitflow")
	fmt.Fprintln(out, "  2) githubflow")
	fmt.Fprintln(out, "  3) trunkbased")
	fmt.Fprint(out, "Selection [1-3]: ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read workflow selection: %w", err)
	}
	switch strings.TrimSpace(line) {
	case "1":
		return WayOfWorkGitFlow, nil
	case "2":
		return WayOfWorkGitHubFlow, nil
	case "3":
		return WayOfWorkTrunkBased, nil
	default:
		return "", errors.New("invalid workflow selection; enter 1, 2, or 3")
	}
}

// Init validates the complete workflow configuration set before making any
// change, selects a workflow, atomically installs its GitVersion config, and
// creates develop only for GitFlow.
func (r *Runner) Init(ctx context.Context, options InitOptions) (InitResult, error) {
	if r == nil {
		return InitResult{}, errors.New("versions runner is nil")
	}
	if err := ctx.Err(); err != nil {
		return InitResult{}, err
	}
	selectorPaths, err := r.validateSelectorFiles()
	if err != nil {
		return InitResult{}, err
	}
	chosen, err := r.chooseWayOfWork(options.WayOfWork)
	if err != nil {
		return InitResult{}, err
	}
	result := InitResult{WayOfWork: chosen}

	target := filepath.Join(r.workDir, cloudOpsWorksDir, "gitversion.yaml")
	if data, readErr := os.ReadFile(target); readErr == nil {
		old := currentWayOfWork(data)
		warning := fmt.Sprintf("warning: replacing existing %s (current WayOfWork=%s) with %s", filepath.Join(cloudOpsWorksDir, "gitversion.yaml"), old, chosen)
		result.Warnings = append(result.Warnings, warning)
		fmt.Fprintln(r.stderr, warning)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return InitResult{}, fmt.Errorf("read current gitversion config: %w", readErr)
	}

	// A GitFlow init must prove repository safety before writing a tracked
	// configuration file, because that write itself would make the worktree dirty.
	if chosen == WayOfWorkGitFlow {
		created, err := r.ensureDevelop(ctx)
		if err != nil {
			return InitResult{}, err
		}
		result.DevelopCreated = created
	}
	if !r.dryRun {
		changed, copyErr := copyAtomically(selectorPaths[chosen], target)
		if copyErr != nil {
			return InitResult{}, copyErr
		}
		result.Changed = changed || result.DevelopCreated
		if chosen == WayOfWorkGitFlow {
			// Selecting GitFlow makes CI's corresponding capability explicit.
			ciChanged, ciErr := r.setGitFlowEnabled(true)
			if ciErr != nil {
				return InitResult{}, ciErr
			}
			result.Changed = result.Changed || ciChanged
		}
	} else {
		result.Changed = true
	}
	return result, nil
}

func (r *Runner) validateSelectorFiles() (map[WayOfWork]string, error) {
	paths := make(map[WayOfWork]string, 3)
	for _, wow := range []WayOfWork{WayOfWorkGitFlow, WayOfWorkGitHubFlow, WayOfWorkTrunkBased} {
		path := filepath.Join(r.workDir, cloudOpsWorksDir, wow.selectorFileName())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("required workflow config %s: %w", filepath.Join(cloudOpsWorksDir, wow.selectorFileName()), err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("required workflow config %s must be a regular non-symlink file", filepath.Join(cloudOpsWorksDir, wow.selectorFileName()))
		}
		paths[wow] = path
	}
	return paths, nil
}

func (r *Runner) chooseWayOfWork(requested WayOfWork) (WayOfWork, error) {
	if requested != "" {
		wow, err := ParseWayOfWork(string(requested))
		return wow, err
	}
	enabled, supported, err := r.gitFlowConfig()
	if err != nil {
		return "", err
	}
	if !supported || enabled {
		return WayOfWorkGitFlow, nil
	}
	wow, err := r.selectWOW(r.stdin, r.stdout)
	if err != nil {
		return "", err
	}
	wow, err = ParseWayOfWork(string(wow))
	return wow, err
}

func (r *Runner) gitFlowConfig() (enabled, supported bool, err error) {
	path := filepath.Join(r.workDir, cloudOpsWorksDir, "cloudopsworks-ci.yaml")
	data, readErr := os.ReadFile(path)
	if errors.Is(readErr, os.ErrNotExist) {
		return false, false, nil
	}
	if readErr != nil {
		return false, false, fmt.Errorf("read cloudopsworks-ci.yaml: %w", readErr)
	}
	match, ok := findGitFlowEnabled(data)
	if !ok {
		return false, false, nil
	}
	return match.value == "true", true, nil
}

func (r *Runner) setGitFlowEnabled(enabled bool) (bool, error) {
	path := filepath.Join(r.workDir, cloudOpsWorksDir, "cloudopsworks-ci.yaml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read cloudopsworks-ci.yaml: %w", err)
	}
	match, ok := findGitFlowEnabled(data)
	if !ok {
		return false, nil
	}
	value := "false"
	if enabled {
		value = "true"
	}
	// Preserve all surrounding bytes, including comments and indentation.
	updated := append([]byte{}, data[:match.valueStart]...)
	updated = append(updated, value...)
	updated = append(updated, data[match.valueEnd:]...)
	if string(updated) == string(data) {
		return false, nil
	}
	if err := writeAtomically(path, updated, fileMode(path)); err != nil {
		return false, err
	}
	return true, nil
}

// gitFlowEnabledMatch points only at the scalar value belonging to
// config.gitFlow.enabled. The scanner intentionally avoids a YAML rewriter so
// comments, blank lines, sibling ordering, and indentation are byte-preserved.
type gitFlowEnabledMatch struct {
	valueStart int
	valueEnd   int
	value      string
}

type yamlMappingLine struct {
	indent int
	key    string
	value  string
	start  int
	end    int
}

// findGitFlowEnabled recognizes the exact mapping path config.gitFlow.enabled.
// It accepts comments and blank lines between siblings, but only follows direct
// children at each indentation level so unrelated gitFlow/enabled keys cannot
// be changed.
func findGitFlowEnabled(data []byte) (gitFlowEnabledMatch, bool) {
	lines := yamlMappingLines(data)
	for configIndex, config := range lines {
		// Only an unindented mapping is the document's root config. A nested
		// other.config mapping may happen to contain the same keys, but must
		// never select or rewrite the repository workflow setting.
		if config.indent != 0 || config.key != "config" || config.value != "" {
			continue
		}
		configChildIndent := childIndent(lines, configIndex, config.indent)
		if configChildIndent < 0 {
			continue
		}
		for gitFlowIndex := configIndex + 1; gitFlowIndex < len(lines); gitFlowIndex++ {
			gitFlow := lines[gitFlowIndex]
			if gitFlow.indent <= config.indent {
				break
			}
			if gitFlow.indent != configChildIndent || gitFlow.key != "gitFlow" || gitFlow.value != "" {
				continue
			}
			gitFlowChildIndent := childIndent(lines, gitFlowIndex, gitFlow.indent)
			if gitFlowChildIndent < 0 {
				break
			}
			for enabledIndex := gitFlowIndex + 1; enabledIndex < len(lines); enabledIndex++ {
				enabled := lines[enabledIndex]
				if enabled.indent <= gitFlow.indent {
					break
				}
				if enabled.indent != gitFlowChildIndent || enabled.key != "enabled" {
					continue
				}
				valueStart, valueEnd, value, ok := yamlBooleanValue(data, enabled)
				if ok {
					return gitFlowEnabledMatch{valueStart: valueStart, valueEnd: valueEnd, value: value}, true
				}
				return gitFlowEnabledMatch{}, false
			}
		}
	}
	return gitFlowEnabledMatch{}, false
}

func childIndent(lines []yamlMappingLine, parentIndex, parentIndent int) int {
	for index := parentIndex + 1; index < len(lines); index++ {
		if lines[index].indent <= parentIndent {
			return -1
		}
		return lines[index].indent
	}
	return -1
}

func yamlMappingLines(data []byte) []yamlMappingLine {
	var result []yamlMappingLine
	for start := 0; start < len(data); {
		end := start + len(data[start:])
		if newline := strings.IndexByte(string(data[start:]), '\n'); newline >= 0 {
			end = start + newline + 1
		}
		lineEnd := end
		if lineEnd > start && data[lineEnd-1] == '\n' {
			lineEnd--
		}
		if lineEnd > start && data[lineEnd-1] == '\r' {
			lineEnd--
		}
		if line, ok := parseYAMLMappingLine(data, start, lineEnd); ok {
			result = append(result, line)
		}
		if end == len(data) {
			break
		}
		start = end
	}
	return result
}

func parseYAMLMappingLine(data []byte, start, end int) (yamlMappingLine, bool) {
	line := string(data[start:end])
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "...") {
		return yamlMappingLine{}, false
	}
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if indent == len(line) || strings.Contains(line[:indent], "\t") {
		return yamlMappingLine{}, false
	}
	colon := strings.IndexByte(line[indent:], ':')
	if colon < 1 {
		return yamlMappingLine{}, false
	}
	colon += indent
	key := strings.TrimSpace(line[indent:colon])
	if key == "" || strings.ContainsAny(key, " []{}\t") {
		return yamlMappingLine{}, false
	}
	return yamlMappingLine{indent: indent, key: key, value: yamlStructuralValue(line[colon+1:]), start: start, end: end}, true
}

func yamlStructuralValue(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "#") {
		return ""
	}
	return value
}

func yamlBooleanValue(data []byte, line yamlMappingLine) (int, int, string, bool) {
	colon := bytesIndexByte(data[line.start:line.end], ':')
	if colon < 0 {
		return 0, 0, "", false
	}
	valueStart := line.start + colon + 1
	for valueStart < line.end && (data[valueStart] == ' ' || data[valueStart] == '\t') {
		valueStart++
	}
	valueEnd := valueStart
	for valueEnd < line.end && ((data[valueEnd] >= 'a' && data[valueEnd] <= 'z') || (data[valueEnd] >= 'A' && data[valueEnd] <= 'Z')) {
		valueEnd++
	}
	value := string(data[valueStart:valueEnd])
	if (value != "true" && value != "false") || (valueEnd < line.end && data[valueEnd] != ' ' && data[valueEnd] != '\t' && data[valueEnd] != '#') {
		return 0, 0, "", false
	}
	return valueStart, valueEnd, value, true
}

func bytesIndexByte(data []byte, target byte) int {
	for index, value := range data {
		if value == target {
			return index
		}
	}
	return -1
}

func currentWayOfWork(data []byte) WayOfWork {
	matches := wowHeader.FindSubmatch(data)
	if len(matches) == 2 {
		if wow, err := ParseWayOfWork(string(matches[1])); err == nil {
			return wow
		}
	}
	return WayOfWorkGitFlow
}

func copyAtomically(source, target string) (bool, error) {
	data, err := os.ReadFile(source)
	if err != nil {
		return false, fmt.Errorf("read selected workflow config: %w", err)
	}
	if current, readErr := os.ReadFile(target); readErr == nil && string(current) == string(data) {
		return false, nil
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return false, fmt.Errorf("read current workflow config: %w", readErr)
	}
	if err := writeAtomically(target, data, fileMode(source)); err != nil {
		return false, err
	}
	return true, nil
}

func fileMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0o644
}

type atomicTempFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Close() error
}

var createAtomicTempFile = func(dir, pattern string) (atomicTempFile, error) {
	return os.CreateTemp(dir, pattern)
}

var replaceAtomicFile = replacement.Replace

func writeAtomically(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	file, err := createAtomicTempFile(dir, ".tronador-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return fmt.Errorf("prepare temporary config: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := replaceAtomicFile(temporary, path); err != nil {
		return fmt.Errorf("replace config atomically: %w", err)
	}
	return nil
}

func (r *Runner) git(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, r.gitPath, args...)
	command.Dir = r.workDir
	out, err := command.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// ensureDevelop is intentionally conservative: it only creates a branch when
// the checked-out primary branch exactly equals its origin tracking ref.
func (r *Runner) ensureDevelop(ctx context.Context) (bool, error) {
	if r.dryRun {
		return false, nil
	}
	status, err := r.git(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if status != "" {
		return false, errors.New("refusing to initialize GitFlow with a dirty worktree")
	}
	if _, err := r.git(ctx, "remote", "get-url", "origin"); err != nil {
		return false, errors.New("refusing to initialize GitFlow without an origin remote")
	}
	if _, err := r.git(ctx, "fetch", "origin", "--prune"); err != nil {
		return false, err
	}
	// A configured primary is an explicit safety contract, even if origin/develop
	// already exists and initialization would otherwise be a no-op. Do not let a
	// stale, divergent, or wrong checkout silently pass that contract.
	primary, remote := "", ""
	if r.mainBranch != "" {
		var validateErr error
		primary, remote, validateErr = r.validateConfiguredPrimary(ctx)
		if validateErr != nil {
			return false, validateErr
		}
	}
	if _, err := r.git(ctx, "show-ref", "--verify", "--quiet", "refs/remotes/origin/develop"); err == nil {
		// A remote develop branch normally means initialization is already done.
		// If this clone also has a local develop branch, do not silently leave a
		// divergent branch behind: publishing/merging it later would be unsafe.
		if _, localErr := r.git(ctx, "show-ref", "--verify", "--quiet", "refs/heads/develop"); localErr == nil {
			localDevelop, localRevErr := r.git(ctx, "rev-parse", "refs/heads/develop")
			if localRevErr != nil {
				return false, localRevErr
			}
			remoteDevelop, remoteRevErr := r.git(ctx, "rev-parse", "refs/remotes/origin/develop")
			if remoteRevErr != nil {
				return false, remoteRevErr
			}
			if localDevelop != remoteDevelop {
				return false, errors.New("refusing to initialize GitFlow: local develop differs from origin/develop")
			}
		} else if !isExitStatus(localErr, 1) {
			return false, localErr
		}
		return false, nil
	} else if !isExitStatus(err, 1) {
		return false, err
	}
	if primary == "" {
		branch, branchErr := r.git(ctx, "branch", "--show-current")
		if branchErr != nil {
			return false, branchErr
		}
		if branch != "main" && branch != "master" {
			return false, fmt.Errorf("refusing to initialize GitFlow from %q; check out main or master first", branch)
		}
		primary = branch
		local, localErr := r.git(ctx, "rev-parse", "HEAD")
		if localErr != nil {
			return false, localErr
		}
		remote, err = r.git(ctx, "rev-parse", "refs/remotes/origin/"+primary)
		if err != nil {
			return false, fmt.Errorf("refusing to initialize GitFlow without origin/%s: %w", primary, err)
		}
		if local != remote {
			return false, fmt.Errorf("refusing to initialize GitFlow: %s is not equal to origin/%s", primary, primary)
		}
	}
	if _, err := r.git(ctx, "show-ref", "--verify", "--quiet", "refs/heads/develop"); err == nil {
		localDevelop, localErr := r.git(ctx, "rev-parse", "refs/heads/develop")
		if localErr != nil {
			return false, localErr
		}
		if localDevelop != remote {
			return false, fmt.Errorf("refusing to publish local develop: it is not equal to origin/%s", primary)
		}
		if _, err := r.git(ctx, "push", "--set-upstream", "origin", "develop"); err != nil {
			return false, err
		}
		return true, nil
	} else if !isExitStatus(err, 1) {
		return false, err
	}
	if _, err := r.git(ctx, "checkout", "-b", "develop", primary); err != nil {
		return false, err
	}
	if _, err := r.git(ctx, "push", "--set-upstream", "origin", "develop"); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Runner) validateConfiguredPrimary(ctx context.Context) (string, string, error) {
	primary := r.mainBranch
	if !safeRef(primary) {
		return "", "", fmt.Errorf("invalid configured main branch %q", primary)
	}
	branch, err := r.git(ctx, "branch", "--show-current")
	if err != nil {
		return "", "", err
	}
	if branch != primary {
		return "", "", fmt.Errorf("refusing to initialize GitFlow from %q; check out configured main branch %q first", branch, primary)
	}
	local, err := r.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	remote, err := r.git(ctx, "rev-parse", "refs/remotes/origin/"+primary)
	if err != nil {
		return "", "", fmt.Errorf("refusing to initialize GitFlow without origin/%s: %w", primary, err)
	}
	if local != remote {
		return "", "", fmt.Errorf("refusing to initialize GitFlow: %s is not equal to origin/%s", primary, primary)
	}
	return primary, remote, nil
}

func isExitStatus(err error, code int) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == code
}
