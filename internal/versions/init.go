package versions

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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
	layout, err := r.validateCloudOpsWorksDir()
	if err != nil {
		return InitResult{}, err
	}
	defer layout.root.Close()
	selectorPaths, err := r.validateSelectorFiles(layout)
	if err != nil {
		return InitResult{}, err
	}
	chosen, err := r.chooseWayOfWorkInLayout(options.WayOfWork, layout)
	if err != nil {
		return InitResult{}, err
	}
	result := InitResult{WayOfWork: chosen}

	target := "gitversion.yaml"
	if err := layout.ensure(); err != nil {
		return InitResult{}, err
	}
	if data, readErr := layout.root.ReadFile(target); readErr == nil {
		if err := layout.ensure(); err != nil {
			return InitResult{}, err
		}
		old := currentWayOfWork(data)
		warning := fmt.Sprintf("warning: replacing existing %s (current WayOfWork=%s) with %s", filepath.Join(cloudOpsWorksDir, "gitversion.yaml"), old, chosen)
		result.Warnings = append(result.Warnings, warning)
		fmt.Fprintln(r.stderr, warning)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return InitResult{}, fmt.Errorf("read current gitversion config: %w", readErr)
	} else if err := layout.ensure(); err != nil {
		return InitResult{}, err
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
		changed, copyErr := copyAtomically(layout, selectorPaths[chosen], target)
		if copyErr != nil {
			return InitResult{}, copyErr
		}
		result.Changed = changed || result.DevelopCreated
		if chosen == WayOfWorkGitFlow {
			// Selecting GitFlow makes CI's corresponding capability explicit.
			ciChanged, ciErr := r.setGitFlowEnabledInLayout(layout, true)
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

// validateCloudOpsWorksDir rejects a symlinked configuration layout before any
// configuration read or Git mutation can follow it outside the selected workdir.
type cloudOpsWorksLayout struct {
	path string
	info os.FileInfo
	root *os.Root
}

type selectorFile struct {
	data []byte
	mode os.FileMode
}

func readRegularFile(root *os.Root, name string) ([]byte, os.FileMode, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	linkInfo, err := root.Lstat(name)
	if err != nil || linkInfo.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !os.SameFile(info, linkInfo) {
		return nil, 0, errors.New("must be a regular non-symlink file")
	}
	data, err := io.ReadAll(file)
	return data, info.Mode().Perm(), err
}

// ensure proves that .cloudopsworks is still the same non-symlink directory
// that Init validated. Each path-based operation is bracketed by this check so
// a directory-to-symlink replacement fails closed before later actions (such
// as Git) can use configuration outside the workdir.
func (layout cloudOpsWorksLayout) ensure() error {
	rootInfo, err := layout.root.Stat(".")
	if err != nil || !os.SameFile(layout.info, rootInfo) {
		return fmt.Errorf("required workflow config directory %s changed during initialization", cloudOpsWorksDir)
	}
	info, err := os.Lstat(layout.path)
	if err != nil {
		return fmt.Errorf("required workflow config directory %s: %w", cloudOpsWorksDir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !os.SameFile(layout.info, info) {
		return fmt.Errorf("required workflow config directory %s changed during initialization", cloudOpsWorksDir)
	}
	return nil
}

func (r *Runner) validateCloudOpsWorksDir() (cloudOpsWorksLayout, error) {
	if runtime.GOOS == "js" || runtime.GOOS == "plan9" {
		return cloudOpsWorksLayout{}, fmt.Errorf("required workflow config directory %s cannot be safely contained on %s", cloudOpsWorksDir, runtime.GOOS)
	}
	path := filepath.Join(r.workDir, cloudOpsWorksDir)
	info, err := os.Lstat(path)
	if err != nil {
		return cloudOpsWorksLayout{}, fmt.Errorf("required workflow config directory %s: %w", cloudOpsWorksDir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return cloudOpsWorksLayout{}, fmt.Errorf("required workflow config directory %s must be a non-symlink directory", cloudOpsWorksDir)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return cloudOpsWorksLayout{}, fmt.Errorf("open required workflow config directory %s: %w", cloudOpsWorksDir, err)
	}
	layout := cloudOpsWorksLayout{path: path, info: info, root: root}
	if err := layout.ensure(); err != nil {
		_ = root.Close()
		return cloudOpsWorksLayout{}, err
	}
	return layout, nil
}

func (r *Runner) validateSelectorFiles(layout cloudOpsWorksLayout) (map[WayOfWork]selectorFile, error) {
	paths := make(map[WayOfWork]selectorFile, 3)
	for _, wow := range []WayOfWork{WayOfWorkGitFlow, WayOfWorkGitHubFlow, WayOfWorkTrunkBased} {
		name := wow.selectorFileName()
		data, mode, err := readRegularFile(layout.root, name)
		if err != nil {
			return nil, fmt.Errorf("required workflow config %s: %w", filepath.Join(cloudOpsWorksDir, wow.selectorFileName()), err)
		}
		paths[wow] = selectorFile{data: data, mode: mode}
	}
	if err := layout.ensure(); err != nil {
		return nil, err
	}
	return paths, nil
}

func (r *Runner) chooseWayOfWork(requested WayOfWork) (WayOfWork, error) {
	layout, err := r.validateCloudOpsWorksDir()
	if err != nil {
		return "", err
	}
	defer layout.root.Close()
	return r.chooseWayOfWorkInLayout(requested, layout)
}

func (r *Runner) chooseWayOfWorkInLayout(requested WayOfWork, layout cloudOpsWorksLayout) (WayOfWork, error) {
	if requested != "" {
		wow, err := ParseWayOfWork(string(requested))
		return wow, err
	}
	enabled, supported, err := r.gitFlowConfigInLayout(layout)
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
	if err != nil {
		return "", err
	}
	if err := layout.ensure(); err != nil {
		return "", err
	}
	return wow, nil
}

func (r *Runner) gitFlowConfig() (enabled, supported bool, err error) {
	layout, err := r.validateCloudOpsWorksDir()
	if err != nil {
		return false, false, err
	}
	defer layout.root.Close()
	return r.gitFlowConfigInLayout(layout)
}

func (r *Runner) gitFlowConfigInLayout(layout cloudOpsWorksLayout) (enabled, supported bool, err error) {
	path := "cloudopsworks-ci.yaml"
	if err := layout.ensure(); err != nil {
		return false, false, err
	}
	data, readErr := layout.root.ReadFile(path)
	if ensureErr := layout.ensure(); ensureErr != nil {
		return false, false, ensureErr
	}
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
	layout, err := r.validateCloudOpsWorksDir()
	if err != nil {
		return false, err
	}
	defer layout.root.Close()
	return r.setGitFlowEnabledInLayout(layout, enabled)
}

func (r *Runner) setGitFlowEnabledInLayout(layout cloudOpsWorksLayout, enabled bool) (bool, error) {
	path := "cloudopsworks-ci.yaml"
	if err := layout.ensure(); err != nil {
		return false, err
	}
	data, err := layout.root.ReadFile(path)
	if ensureErr := layout.ensure(); ensureErr != nil {
		return false, ensureErr
	}
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
	mode := os.FileMode(0o644)
	if info, statErr := layout.root.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := writeAtomicallyInLayout(layout, path, updated, mode); err != nil {
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

func copyAtomically(layout cloudOpsWorksLayout, source selectorFile, target string) (bool, error) {
	if err := layout.ensure(); err != nil {
		return false, err
	}
	data := source.data
	if err := layout.ensure(); err != nil {
		return false, err
	}
	if current, readErr := layout.root.ReadFile(target); readErr == nil && string(current) == string(data) {
		if err := layout.ensure(); err != nil {
			return false, err
		}
		return false, nil
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return false, fmt.Errorf("read current workflow config: %w", readErr)
	} else if err := layout.ensure(); err != nil {
		return false, err
	}
	if err := writeAtomicallyInLayout(layout, target, data, source.mode); err != nil {
		return false, err
	}
	return true, nil
}

type atomicTempFile interface {
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Close() error
}

var createRootAtomicTempFile = newRootAtomicTempFile
var replaceAtomicFileInRoot = replacement.ReplaceInRoot

func writeAtomicallyInLayout(layout cloudOpsWorksLayout, path string, data []byte, mode os.FileMode) error {
	if err := layout.ensure(); err != nil {
		return err
	}
	temporary, file, err := createRootAtomicTempFile(layout.root)
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	defer layout.root.Remove(temporary)
	if err := layout.ensure(); err != nil {
		_ = file.Close()
		return err
	}
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
	if err := layout.ensure(); err != nil {
		return err
	}
	if err := replaceAtomicFileInRoot(layout.root, temporary, path); err != nil {
		return fmt.Errorf("replace config atomically: %w", err)
	}
	return nil
}

var beforeRootAtomicTempCreate = func() {}

func newRootAtomicTempFile(root *os.Root) (string, atomicTempFile, error) {
	beforeRootAtomicTempCreate()
	var random [12]byte
	for range 100 {
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, err
		}
		name := fmt.Sprintf(".tronador-%x", random)
		file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return name, file, err
	}
	return "", nil, errors.New("create unique temporary config")
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
	if err := r.validateGitFlowPrimaryDistinct(ctx); err != nil {
		return false, err
	}
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
		primary, remote, err = r.resolveDefaultPrimary(ctx)
		if err != nil {
			return false, err
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
		if _, err := r.git(ctx, "push", "--set-upstream", "origin", "refs/heads/develop:refs/heads/develop"); err != nil {
			return false, err
		}
		return true, nil
	} else if !isExitStatus(err, 1) {
		return false, err
	}
	if _, err := r.git(ctx, "checkout", "-b", "develop", "refs/heads/"+primary); err != nil {
		return false, err
	}
	if _, err := r.git(ctx, "push", "--set-upstream", "origin", "refs/heads/develop:refs/heads/develop"); err != nil {
		return false, err
	}
	return true, nil
}

// validateGitFlowPrimaryDistinct rejects the invalid topology where develop is
// both GitFlow's integration branch and its primary release branch. A supplied
// primary is checked without Git I/O; otherwise origin/HEAD is the discovered
// primary when available. This runs before the existing-develop idempotency
// return so an already-created develop cannot mask the invalid topology.
func (r *Runner) validateGitFlowPrimaryDistinct(ctx context.Context) error {
	if r.mainBranch != "" {
		if r.mainBranch == "develop" {
			return errors.New("gitflow primary branch must not be develop")
		}
		return nil
	}
	originHead, err := r.git(ctx, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	if err != nil {
		if isExitStatus(err, 1) {
			return nil
		}
		return fmt.Errorf("resolve origin/HEAD for GitFlow topology: %w", err)
	}
	const originPrefix = "refs/remotes/origin/"
	if !strings.HasPrefix(originHead, originPrefix) {
		return fmt.Errorf("invalid origin/HEAD target %q", originHead)
	}
	primary := strings.TrimPrefix(originHead, originPrefix)
	if !safeRef(primary) {
		return fmt.Errorf("invalid origin/HEAD target %q", originHead)
	}
	if primary == "develop" {
		return errors.New("gitflow primary branch must not be develop")
	}
	return nil
}

// resolveDefaultPrimary first uses origin/HEAD when it identifies a safe,
// checked-out branch with a matching origin ref. Only an absent origin/HEAD
// retains the legacy main/master fallback; malformed or unsafe targets fail
// closed instead of selecting another branch.
func (r *Runner) resolveDefaultPrimary(ctx context.Context) (string, string, error) {
	originHead, originHeadErr := r.git(ctx, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	if originHeadErr != nil {
		if !isExitStatus(originHeadErr, 1) {
			return "", "", fmt.Errorf("resolve origin/HEAD: %w", originHeadErr)
		}
	} else {
		const originPrefix = "refs/remotes/origin/"
		if !strings.HasPrefix(originHead, originPrefix) {
			return "", "", fmt.Errorf("invalid origin/HEAD target %q", originHead)
		}
		primary := strings.TrimPrefix(originHead, originPrefix)
		if !safeRef(primary) {
			return "", "", fmt.Errorf("invalid origin/HEAD target %q", originHead)
		}
		remote, remoteErr := r.git(ctx, "rev-parse", "refs/remotes/origin/"+primary)
		if remoteErr != nil {
			return "", "", fmt.Errorf("resolve origin/HEAD branch origin/%s: %w", primary, remoteErr)
		}
		branch, branchErr := r.git(ctx, "branch", "--show-current")
		if branchErr != nil {
			return "", "", branchErr
		}
		if branch != primary {
			return "", "", fmt.Errorf("refusing to initialize GitFlow from %q; check out origin/HEAD branch %q first", branch, primary)
		}
		local, localErr := r.git(ctx, "rev-parse", "HEAD")
		if localErr != nil {
			return "", "", localErr
		}
		if local != remote {
			return "", "", fmt.Errorf("refusing to initialize GitFlow: %s is not equal to origin/%s", primary, primary)
		}
		return primary, remote, nil
	}

	branch, branchErr := r.git(ctx, "branch", "--show-current")
	if branchErr != nil {
		return "", "", branchErr
	}
	if branch != "main" && branch != "master" {
		return "", "", fmt.Errorf("refusing to initialize GitFlow from %q; check out main or master first", branch)
	}
	local, localErr := r.git(ctx, "rev-parse", "HEAD")
	if localErr != nil {
		return "", "", localErr
	}
	remote, remoteErr := r.git(ctx, "rev-parse", "refs/remotes/origin/"+branch)
	if remoteErr != nil {
		return "", "", fmt.Errorf("refusing to initialize GitFlow without origin/%s: %w", branch, remoteErr)
	}
	if local != remote {
		return "", "", fmt.Errorf("refusing to initialize GitFlow: %s is not equal to origin/%s", branch, branch)
	}
	return branch, remote, nil
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
