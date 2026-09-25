package repos

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// upgradeCloudOpsworksConfig applies the template's CloudOps YAML as a
// comment-preserving baseline and overlays values configured by the repository.
// It deliberately does not touch workflows or _VERSION; callers sequence those
// operations around this method.
func (r *Runner) upgradeCloudOpsworksConfig(state RepositoryState) error {
	plan, err := r.buildCloudOpsworksConfigPlan(state)
	if err != nil {
		return err
	}
	_, err = r.applyCloudOpsworksConfigPlan(plan)
	return err
}

// cloudOpsworksConfigPlan is fully validated before it changes repository files.
type cloudOpsworksConfigPlan struct {
	writes []yamlWrite
}

func (r *Runner) buildCloudOpsworksConfigPlan(state RepositoryState) (cloudOpsworksConfigPlan, error) {
	return r.buildCloudOpsworksConfigPlanExcluding(state, "")
}

func (r *Runner) buildCloudOpsworksConfigPlanExcluding(state RepositoryState, excludedSubtree string) (cloudOpsworksConfigPlan, error) {
	templateBase := r.templatePath()
	templateRoot := filepath.Join(templateBase, ".cloudopsworks")
	if !exists(templateRoot) {
		return cloudOpsworksConfigPlan{}, nil
	}
	destinationRoot := filepath.Join(state.WorkDir, ".cloudopsworks")
	sourceRoot := destinationRoot
	legacyRoot := filepath.Join(state.WorkDir, ".github")
	// Migrate is intentionally a no-op in dry-run mode. Read the legacy source
	// in that case, but always plan the v5.10 destination tree.
	if state.Pre510 && exists(legacyRoot) {
		sourceRoot = legacyRoot
	} else if !exists(sourceRoot) && state.BlueprintPath != "" {
		sourceRoot = filepath.Join(state.WorkDir, state.BlueprintPath)
	}

	templateFiles, err := yamlFiles(templateBase, templateRoot)
	if err != nil {
		return cloudOpsworksConfigPlan{}, fmt.Errorf("list template CloudOps YAML: %w", err)
	}
	templateFiles = excludeYAMLSubtree(templateFiles, excludedSubtree)
	localFiles, err := yamlFiles(state.WorkDir, sourceRoot)
	if err != nil && !os.IsNotExist(err) {
		return cloudOpsworksConfigPlan{}, fmt.Errorf("list repository CloudOps YAML: %w", err)
	}
	localFiles = excludeYAMLSubtree(localFiles, excludedSubtree)
	templates, err := readYAMLDocuments(templateBase, templateRoot, templateFiles)
	if err != nil {
		return cloudOpsworksConfigPlan{}, err
	}
	if sourceRoot == legacyRoot {
		localFiles = migrationOwnedYAMLFiles(r, localFiles, templates)
	}
	locals, err := readYAMLDocuments(state.WorkDir, sourceRoot, localFiles)
	if err != nil {
		return cloudOpsworksConfigPlan{}, err
	}

	cloud, cloudType := configuredCloud(locals["vars/inputs-global.yaml"])
	usedTargets := make(map[string]bool)
	plan := make([]yamlWrite, 0, len(locals)+len(templates))
	localNames := make([]string, 0, len(locals))
	for name := range locals {
		localNames = append(localNames, name)
	}
	sort.Strings(localNames)

	for _, localName := range localNames {
		local := locals[localName]
		targetName, ok := "", false
		if isTopLevelInput(localName) {
			// A local Agents declaration is authoritative. Every declared cloud/type
			// alternative must resolve to the same target baseline; otherwise
			// preserve the local file instead of guessing via exact/mobile/global fallbacks.
			if headerTarget, hasHeader, unique := localAgentsHeaderTarget(templates, local.data); hasHeader {
				if !unique {
					warnConfig(r, localName, "header")
					continue
				}
				targetName, ok = headerTarget, true
			} else if _, exact := templates[localName]; exact {
				targetName, ok = localName, true
			}
			// Mobile is an active-platform fallback, but still outranks generic
			// cloud/cloud_type scaffolds (including library).
			if !ok {
				if mobileTarget, mobileOK, mobileAmbiguous := mobileInputTarget(templates, locals["vars/inputs-global.yaml"].node); mobileAmbiguous {
					warnConfig(r, localName, "mobile")
					continue
				} else if mobileOK {
					targetName, ok = mobileTarget, true
				}
			}
			if !ok {
				targetName, ok = selectTargetYAML(localName, templates, cloud, cloudType)
			}
		} else {
			targetName, ok = selectTargetYAML(localName, templates, cloud, cloudType)
		}
		if !ok {
			warnConfig(r, localName, "file")
			continue
		}
		target := templates[targetName]
		if !hasActiveYAMLNode(local.node) {
			if hasYAMLComment(local.data) {
				warnConfig(r, localName, "comment-only")
			}
			plan = append(plan, yamlWrite{path: filepath.Join(destinationRoot, filepath.FromSlash(localName)), content: target.data, mode: local.mode, name: localName, removeSource: legacyYAMLSource(sourceRoot, legacyRoot, localName)})
			usedTargets[targetName] = true
			continue
		}
		merged := cloneYAMLNode(target.node)
		var unmapped []string
		mergeYAMLNodes(merged, local.node, "", &unmapped)
		for _, path := range unmapped {
			warnConfig(r, localName, path)
		}
		content, rendered := renderYAMLTargetBaseline(target.data, local.data, target.node, local.node)
		if !rendered {
			warnConfig(r, localName, "renderer-fallback: raw spans not provable")
			content, err = yaml.Marshal(merged)
			if err != nil {
				return cloudOpsworksConfigPlan{}, fmt.Errorf("marshal merged YAML %s: %w", localName, err)
			}
			if !validateRenderedYAML(content) {
				return cloudOpsworksConfigPlan{}, fmt.Errorf("validate merged YAML %s", localName)
			}
		}
		plan = append(plan, yamlWrite{path: filepath.Join(destinationRoot, filepath.FromSlash(localName)), content: content, mode: local.mode, name: localName, removeSource: legacyYAMLSource(sourceRoot, legacyRoot, localName)})
		usedTargets[targetName] = true
	}

	// Copy target-owned configuration files which do not represent generic
	// deployment scaffolds. Generic inputs are only baselines for local files;
	// adding them would create unintended environments in consumer repositories.
	targetNames := make([]string, 0, len(templates))
	for name := range templates {
		targetNames = append(targetNames, name)
	}
	sort.Strings(targetNames)
	for _, targetName := range targetNames {
		if usedTargets[targetName] || !copyUnmatchedTarget(targetName, cloud, cloudType, locals) {
			continue
		}
		target := templates[targetName]
		plan = append(plan, yamlWrite{path: filepath.Join(destinationRoot, filepath.FromSlash(targetName)), content: target.data, mode: target.mode, name: targetName})
	}
	sort.Slice(plan, func(i, j int) bool { return plan[i].path < plan[j].path })
	if err := validateYAMLWriteDestinations(state.WorkDir, plan); err != nil {
		return cloudOpsworksConfigPlan{}, err
	}
	return cloudOpsworksConfigPlan{writes: plan}, nil
}

func (r *Runner) applyCloudOpsworksConfigPlan(plan cloudOpsworksConfigPlan) ([]string, error) {
	changed := make([]string, 0, len(plan.writes))
	for _, write := range plan.writes {
		didChange, err := r.writeFileIfChanged(write.path, write.content, write.mode)
		if err != nil {
			return nil, fmt.Errorf("write CloudOps YAML %s: %w", write.name, err)
		}
		if didChange {
			changed = append(changed, filepath.ToSlash(strings.TrimPrefix(write.path, r.Opts.WorkDir+string(filepath.Separator))))
		}
	}
	// A legacy source is removed only after every validated destination write
	// succeeds. This avoids losing source configuration on a later failure.
	for _, write := range plan.writes {
		if write.removeSource == "" || !exists(write.removeSource) {
			continue
		}
		if err := r.removeAll(write.removeSource); err != nil {
			return nil, fmt.Errorf("remove migrated CloudOps YAML %s: %w", write.name, err)
		}
		changed = append(changed, filepath.ToSlash(strings.TrimPrefix(write.removeSource, r.Opts.WorkDir+string(filepath.Separator))))
	}
	return changed, nil
}

type yamlDocument struct {
	node *yaml.Node
	mode os.FileMode
	data []byte
}

type yamlWrite struct {
	path         string
	content      []byte
	mode         os.FileMode
	name         string
	removeSource string
}

func legacyYAMLSource(sourceRoot, legacyRoot, name string) string {
	if filepath.Clean(sourceRoot) != filepath.Clean(legacyRoot) {
		return ""
	}
	return filepath.Join(sourceRoot, filepath.FromSlash(name))
}

func validateYAMLWriteDestinations(workDir string, writes []yamlWrite) error {
	root := filepath.Clean(workDir)
	for _, write := range writes {
		path := filepath.Clean(write.path)
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe CloudOps YAML destination %q", write.path)
		}
		for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
			info, statErr := os.Lstat(parent)
			if statErr == nil {
				if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
					return fmt.Errorf("invalid CloudOps YAML destination parent %q", parent)
				}
			} else if !os.IsNotExist(statErr) {
				return fmt.Errorf("stat CloudOps YAML destination parent %q: %w", parent, statErr)
			}
			if parent == root {
				break
			}
		}
		if info, statErr := os.Lstat(path); statErr == nil && (info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("invalid CloudOps YAML destination %q", write.path)
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return fmt.Errorf("stat CloudOps YAML destination %q: %w", write.path, statErr)
		}
	}
	return nil
}

func yamlFiles(base, root string) ([]string, error) {
	if err := validateYAMLSourceDirectoryChain(base, root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("unsafe YAML source %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe YAML source %q: symlinks are not supported", path)
		}
		if entry.IsDir() {
			if !info.IsDir() {
				return fmt.Errorf("unsafe YAML source %q: directory changed during inventory", path)
			}
			return nil
		}
		if !isYAMLFile(path) {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe YAML source %q: not a regular file", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	sort.Strings(files)
	return files, err
}

func validateYAMLSourceDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("unsafe YAML source %q: expected a non-symlink directory", path)
	}
	return nil
}

// validateYAMLSourceDirectoryChain rejects a source root reached through a
// symlinked ancestor inside the repository before inventory follows it.
func validateYAMLSourceDirectoryChain(base, root string) error {
	base = filepath.Clean(base)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(base, root)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe YAML source root %q", root)
	}
	current := base
	if err := validateYAMLSourceDirectory(current); err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if err := validateYAMLSourceDirectory(current); err != nil {
			return err
		}
	}
	return nil
}

func isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

// migrationOwnedYAMLFiles excludes GitHub operational YAML (workflows,
// Dependabot, and issue templates) from the dry-run v5.9 inventory. Only
// files moved by the v5.10 migration are CloudOps configuration candidates.
func migrationOwnedYAMLFiles(r *Runner, files []string, templates map[string]yamlDocument) []string {
	filtered := make([]string, 0, len(files))
	for _, name := range files {
		dir, base := filepath.ToSlash(filepath.Dir(name)), filepath.Base(name)
		if dir == "vars" || strings.HasPrefix(dir, "vars/") || dir == "values" || strings.HasPrefix(dir, "values/") || (dir == "." && (base == "labeler.yml" || base == "auto-assign.yml" || strings.HasPrefix(base, "cloudopsworks") || strings.HasPrefix(base, "gitversion"))) {
			filtered = append(filtered, name)
			continue
		}
		// Legacy root YAML is configuration only when the v5.10 template has
		// that exact CloudOps counterpart. This keeps GitHub operational YAML
		// out while preserving real root configuration during migration.
		if dir == "." {
			if _, ok := templates[name]; ok {
				filtered = append(filtered, name)
			} else if !legacyOperationalRootYAML(base) {
				warnConfig(r, name, "file")
			}
		}
	}
	return filtered
}

func legacyOperationalRootYAML(base string) bool {
	switch base {
	case "dependabot.yml", "dependabot.yaml", "codeql.yml", "codeql.yaml", "secret_scanning.yml", "secret_scanning.yaml", "pull_request_template.yml", "pull_request_template.yaml":
		return true
	default:
		return false
	}
}

func excludeYAMLSubtree(files []string, subtree string) []string {
	subtree = configRelativeSubtree(subtree)
	if subtree == "" {
		return files
	}
	prefix := subtree + "/"
	out := make([]string, 0, len(files))
	for _, file := range files {
		if file != subtree && !strings.HasPrefix(file, prefix) {
			out = append(out, file)
		}
	}
	return out
}

// configRelativeSubtree converts a configured repository path to the path below
// .cloudopsworks, which is the root used by the YAML inventories.
func configRelativeSubtree(path string) string {
	path = strings.Trim(filepath.ToSlash(path), "/")
	path = strings.TrimPrefix(path, ".cloudopsworks/")
	if path == ".cloudopsworks" || path == "." {
		return ""
	}
	return path
}

func readYAMLDocuments(base, root string, names []string) (map[string]yamlDocument, error) {
	documents := make(map[string]yamlDocument, len(names))
	for _, name := range names {
		path := filepath.Join(root, filepath.FromSlash(name))
		info, err := validateYAMLSourceFile(base, root, path)
		if err != nil {
			return nil, fmt.Errorf("unsafe YAML source %s: %w", name, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read YAML %s: %w", name, err)
		}
		var node yaml.Node
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		if err := decoder.Decode(&node); err != nil && err != io.EOF {
			return nil, fmt.Errorf("parse YAML %s: %w", name, err)
		}
		var extra yaml.Node
		if err := decoder.Decode(&extra); err != io.EOF {
			if err == nil {
				return nil, fmt.Errorf("unsafe YAML %s: multiple documents are not supported", name)
			}
			return nil, fmt.Errorf("parse YAML %s: %w", name, err)
		}
		if err := validateYAMLNode(&node, name); err != nil {
			return nil, err
		}
		documents[name] = yamlDocument{node: &node, mode: info.Mode().Perm(), data: data}
	}
	return documents, nil
}

// validateYAMLSourceFile re-checks every source component immediately before
// ReadFile so an inventory entry cannot switch to a symlink after discovery.
func validateYAMLSourceFile(base, root, path string) (os.FileInfo, error) {
	if err := validateYAMLSourceDirectoryChain(base, root); err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("path escapes inventory root")
	}
	parent := root
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		parent = filepath.Join(parent, part)
		if err := validateYAMLSourceDirectory(parent); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected a non-symlink regular file")
	}
	return info, nil
}

func validateYAMLNode(node *yaml.Node, name string) error {
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return fmt.Errorf("unsafe YAML %s: aliases and anchors are not supported", name)
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]bool, len(node.Content)/2)
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode {
				return fmt.Errorf("unsafe YAML %s: non-scalar mapping key", name)
			}
			if seen[key.Value] {
				return fmt.Errorf("unsafe YAML %s: duplicate mapping key %q", name, key.Value)
			}
			seen[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if err := validateYAMLNode(child, name); err != nil {
			return err
		}
	}
	return nil
}

func configuredCloud(doc yamlDocument) (string, string) {
	root := documentRoot(doc.node)
	if root == nil || root.Kind != yaml.MappingNode {
		return "", ""
	}
	return normalizeAgentValue(mappingScalar(root, "cloud")), normalizeAgentValue(mappingScalar(root, "cloud_type"))
}

func documentRoot(node *yaml.Node) *yaml.Node {
	if node != nil && node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return node
}

func hasActiveYAMLNode(node *yaml.Node) bool {
	root := documentRoot(node)
	return root != nil && root.Kind != 0
}

func hasYAMLComment(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			return true
		}
	}
	return false
}

func mappingScalar(node *yaml.Node, wanted string) string {
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == wanted {
			return node.Content[index+1].Value
		}
	}
	return ""
}

func selectTargetYAML(localName string, templates map[string]yamlDocument, cloud, cloudType string) (string, bool) {
	if _, ok := templates[localName]; ok {
		return localName, true
	}
	dir, base := filepath.ToSlash(filepath.Dir(localName)), filepath.Base(localName)
	if dir == "vars" && strings.HasPrefix(base, "inputs-") && base != "inputs-global.yaml" {
		target, matched, ambiguous := agentsHeaderTarget(templates, cloud, cloudType)
		if ambiguous {
			return "", false
		}
		if matched {
			return target, true
		}
		candidate := rootInputScaffold(cloud, cloudType)
		if _, ok := templates[candidate]; ok {
			return candidate, true
		}
	}
	if dir == "vars/helm" || dir == "vars/apigw" || dir == "vars/preview" {
		if target, ok := environmentTarget(dir, base, templates); ok {
			return target, true
		}
	}
	return "", false
}

func isTopLevelInput(name string) bool {
	return filepath.ToSlash(filepath.Dir(name)) == "vars" && strings.HasPrefix(filepath.Base(name), "inputs-") && filepath.Base(name) != "inputs-global.yaml"
}

// mobileInputTarget is template-aware: Flutter deliberately has Android and
// XCode baselines but no invented Flutter baseline. Both/neither is ambiguous.
func mobileInputTarget(templates map[string]yamlDocument, global *yaml.Node) (string, bool, bool) {
	root := documentRoot(global)
	if root == nil || root.Kind != yaml.MappingNode {
		return "", false, false
	}
	android := mappingMobileActive(root, "android")
	xcode := mappingMobileActive(root, "xcode")
	if flutter := mappingIndex(root, "flutter"); flutter >= 0 && yamlMobileValueActive(root.Content[flutter+1]) {
		platforms := mappingPlatforms(root.Content[flutter+1])
		android = android || platforms["android"]
		xcode = xcode || platforms["ios"] || platforms["xcode"]
	}
	if android == xcode {
		return "", false, android
	}
	name := "vars/inputs-ANDROID-ENV.yaml"
	if xcode {
		name = "vars/inputs-XCODE-ENV.yaml"
	}
	_, ok := templates[name]
	return name, ok, false
}

func mappingMobileActive(root *yaml.Node, key string) bool {
	index := mappingIndex(root, key)
	return index >= 0 && yamlMobileValueActive(root.Content[index+1])
}

func yamlMobileValueActive(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.MappingNode {
		return true
	}
	if node.Kind != yaml.ScalarNode {
		return false
	}
	return strings.EqualFold(node.Value, "true")
}

func mappingPlatforms(node *yaml.Node) map[string]bool {
	out := map[string]bool{}
	if node == nil || node.Kind != yaml.MappingNode {
		return out
	}
	index := mappingIndex(node, "platforms")
	if index < 0 {
		return out
	}
	for _, item := range node.Content[index+1].Content {
		if item.Kind == yaml.ScalarNode {
			out[strings.ToLower(item.Value)] = true
		}
	}
	return out
}

func rootInputScaffold(cloud, cloudType string) string {
	switch {
	case isKubernetesCloudType(cloudType):
		return "vars/inputs-KUBERNETES-ENV.yaml"
	case cloud == "aws" && cloudType == "lambda":
		return "vars/inputs-LAMBDA-ENV.yaml"
	case cloud == "aws" && cloudType == "beanstalk":
		return "vars/inputs-BEANSTALK-ENV.yaml"
	case cloud == "gcp" && cloudType == "appengine":
		return "vars/inputs-APPENGINE.yaml"
	case cloud == "gcp" && cloudType == "cloudrun":
		return "vars/inputs-CLOUDRUN.yaml"
	case cloud == "none" || isLibraryCloudType(cloudType):
		return "vars/inputs-LIB-ENV.yaml"
	default:
		return ""
	}
}

// agentsHeaderTarget reads the template's documented header metadata before
// relying on legacy scaffold names. A template can describe more than one
// cloud/type pair using multiple # Agents lines; exactly one matching file is
// required to avoid inventing a baseline.
func agentsHeaderTarget(templates map[string]yamlDocument, cloud, cloudType string) (string, bool, bool) {
	if cloud == "" && cloudType == "" {
		return "", false, false
	}
	matches := make([]string, 0, 1)
	for name, document := range templates {
		if filepath.ToSlash(filepath.Dir(name)) != "vars" || !strings.HasPrefix(filepath.Base(name), "inputs-") {
			continue
		}
		for _, metadata := range agentsHeaders(document.data) {
			if agentsHeaderMatches(metadata, cloud, cloudType) {
				matches = append(matches, name)
				break
			}
		}
	}
	sort.Strings(matches)
	if len(matches) == 1 {
		return matches[0], true, false
	}
	return "", false, len(matches) > 1
}

type agentsHeader struct {
	clouds     []string
	cloudTypes []string
}

func agentsHeaders(data []byte) []agentsHeader {
	var headers []agentsHeader
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "# agents:") {
			continue
		}
		fields := strings.FieldsFunc(strings.TrimSpace(line[len("# Agents:"):]), func(r rune) bool {
			return r == ';' || r == ','
		})
		header := agentsHeader{}
		for _, field := range fields {
			key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
			if !ok {
				continue
			}
			values := strings.Split(value, "|")
			for index := range values {
				values[index] = normalizeAgentValue(values[index])
			}
			switch normalizeAgentValue(key) {
			case "cloud":
				header.clouds = append(header.clouds, values...)
			case "cloudtype":
				header.cloudTypes = append(header.cloudTypes, values...)
			}
		}
		if len(header.clouds) > 0 || len(header.cloudTypes) > 0 {
			headers = append(headers, header)
		}
	}
	return headers
}

// localAgentsHeaderTarget resolves every local # Agents declaration against
// template metadata. Headers can declare pipe-delimited alternatives or appear
// more than once, but all declared combinations must identify one target.
func localAgentsHeaderTarget(templates map[string]yamlDocument, data []byte) (string, bool, bool) {
	headers := agentsHeaders(data)
	if len(headers) == 0 {
		return "", false, false
	}
	matches := make(map[string]struct{})
	for _, header := range headers {
		clouds, cloudTypes := header.clouds, header.cloudTypes
		if len(clouds) == 0 {
			clouds = []string{""}
		}
		if len(cloudTypes) == 0 {
			cloudTypes = []string{""}
		}
		for _, cloud := range clouds {
			for _, cloudType := range cloudTypes {
				target, matched, ambiguous := agentsHeaderTarget(templates, cloud, cloudType)
				if !matched || ambiguous {
					return "", true, false
				}
				matches[target] = struct{}{}
			}
		}
	}
	if len(matches) != 1 {
		return "", true, false
	}
	for target := range matches {
		return target, true, true
	}
	return "", true, false
}

func normalizeAgentValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == ' ' {
			return -1
		}
		return r
	}, value)
}

func agentsHeaderMatches(header agentsHeader, cloud, cloudType string) bool {
	return headerValueMatches(header.clouds, cloud) && headerValueMatches(header.cloudTypes, cloudType)
}

func headerValueMatches(values []string, actual string) bool {
	if len(values) == 0 {
		return true
	}
	actual = normalizeAgentValue(actual)
	for _, value := range values {
		if value == actual || (value == "kubernetes" && isKubernetesCloudType(actual)) || (actual == "kubernetes" && isKubernetesCloudType(value)) || (isLibraryCloudType(value) && isLibraryCloudType(actual)) {
			return true
		}
	}
	return false
}

func isKubernetesCloudType(value string) bool {
	return value == "kubernetes" || value == "eks" || value == "aks" || value == "gke"
}

func isLibraryCloudType(value string) bool {
	return value == "none" || value == "library" || value == "lib"
}

func environmentTarget(dir, base string, templates map[string]yamlDocument) (string, bool) {
	env, ok := environmentToken(base)
	if !ok {
		return "", false
	}
	prefix := dir + "/"
	matches := make([]string, 0, 1)
	for name := range templates {
		if strings.HasPrefix(name, prefix) && hasEnvironmentToken(filepath.Base(name), env) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return "", false
	}
	family := fileFamily(base)
	preferred := make([]string, 0, len(matches))
	for _, name := range matches {
		if fileFamily(filepath.Base(name)) == family {
			preferred = append(preferred, name)
		}
	}
	if len(preferred) > 0 {
		matches = preferred
	}
	sort.Strings(matches)
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func environmentToken(name string) (string, bool) {
	var found string
	for _, token := range nameTokens(name) {
		if token != "dev" && token != "uat" && token != "prod" {
			continue
		}
		if found != "" && found != token {
			return "", false
		}
		found = token
	}
	return found, found != ""
}

func hasEnvironmentToken(name, wanted string) bool {
	for _, token := range nameTokens(name) {
		if token == wanted {
			return true
		}
	}
	return false
}

func fileFamily(name string) string {
	tokens := nameTokens(name)
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

func nameTokens(name string) []string {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	return strings.FieldsFunc(strings.ToLower(stem), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
}

func copyUnmatchedTarget(name, cloud, cloudType string, locals map[string]yamlDocument) bool {
	if !strings.HasPrefix(name, "vars/") {
		return true
	}
	if name == "vars/inputs-global.yaml" {
		return true
	}
	if filepath.ToSlash(filepath.Dir(name)) == "vars" && strings.HasPrefix(filepath.Base(name), "inputs-") {
		return false
	}
	for _, prefix := range []string{"vars/helm/", "vars/apigw/", "vars/preview/"} {
		if strings.HasPrefix(name, prefix) {
			return subdirApplicable(prefix, cloud, cloudType, locals)
		}
	}
	return true
}

func subdirApplicable(prefix, cloud, cloudType string, locals map[string]yamlDocument) bool {
	for name := range locals {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	if prefix == "vars/helm/" {
		return isKubernetesCloudType(cloudType)
	}
	global := documentRoot(locals["vars/inputs-global.yaml"].node)
	if global == nil {
		return false
	}
	if prefix == "vars/apigw/" {
		return mappingEnabled(global, "apis")
	}
	return mappingEnabled(global, "preview")
}

func mappingEnabled(node *yaml.Node, key string) bool {
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value != key {
			continue
		}
		value := node.Content[index+1]
		if value.Kind == yaml.ScalarNode {
			return strings.EqualFold(value.Value, "true")
		}
		if value.Kind == yaml.MappingNode {
			return strings.EqualFold(mappingScalar(value, "enabled"), "true")
		}
	}
	return false
}

func mergeYAMLNodes(target, local *yaml.Node, path string, unmapped *[]string) {
	targetRoot, localRoot := documentRoot(target), documentRoot(local)
	if targetRoot == nil || localRoot == nil {
		return
	}
	mergeYAMLValue(targetRoot, localRoot, path, unmapped)
}

func mergeYAMLValue(target, local *yaml.Node, path string, unmapped *[]string) {
	if target.Kind != yaml.MappingNode || local.Kind != yaml.MappingNode {
		replaceYAMLNodePreservingComments(target, local)
		return
	}
	for index := 0; index+1 < len(local.Content); index += 2 {
		localKey, localValue := local.Content[index], local.Content[index+1]
		childPath := localKey.Value
		if path != "" {
			childPath = path + "." + localKey.Value
		}
		targetIndex := mappingIndex(target, localKey.Value)
		if targetIndex < 0 {
			target.Content = append(target.Content, cloneYAMLNode(localKey), cloneYAMLNode(localValue))
			*unmapped = append(*unmapped, childPath)
			continue
		}
		mergeYAMLNodeComments(target.Content[targetIndex], localKey)
		mergeYAMLValue(target.Content[targetIndex+1], localValue, childPath, unmapped)
	}
}

func replaceYAMLNodePreservingComments(target, local *yaml.Node) {
	replacement := cloneYAMLNode(local)
	mergeYAMLNodeComments(target, replacement)
	*target = *replacement
}

func mergeYAMLNodeComments(target, replacement *yaml.Node) {
	replacement.HeadComment = mergeYAMLComments(target.HeadComment, replacement.HeadComment)
	replacement.LineComment = mergeYAMLComments(target.LineComment, replacement.LineComment)
	replacement.FootComment = mergeYAMLComments(target.FootComment, replacement.FootComment)
	for index := 0; index < len(target.Content) && index < len(replacement.Content); index++ {
		mergeYAMLNodeComments(target.Content[index], replacement.Content[index])
	}
}

func mergeYAMLComments(target, local string) string {
	if local == "" || local == target {
		return target
	}
	if target == "" {
		return local
	}
	if strings.Contains(target, local) {
		return target
	}
	return target + "\n" + local
}

func mappingIndex(node *yaml.Node, key string) int {
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return index
		}
	}
	return -1
}

func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	clone := *node
	clone.Content = make([]*yaml.Node, len(node.Content))
	for index, child := range node.Content {
		clone.Content[index] = cloneYAMLNode(child)
	}
	return &clone
}

func warnConfig(r *Runner, file, path string) {
	fmt.Fprintf(r.Opts.Stdout, "WARNING preserving unmapped CloudOps configuration %s path %s\n", file, path)
}

// copyDirNonYAMLIfExists copies non-YAML files without changing destination
// files that are absent from the template. It returns affected destination paths
// so callers can stage only template-owned changes.
func (r *Runner) copyDirNonYAMLIfExists(src, dst string) ([]string, error) {
	relativeFiles, err := nonYAMLFiles(src)
	if err != nil {
		return nil, err
	}
	affected := make([]string, 0, len(relativeFiles))
	for _, rel := range relativeFiles {
		destination := filepath.Join(dst, rel)
		if err := r.copyFileIfExists(filepath.Join(src, rel), destination); err != nil {
			return nil, err
		}
		affected = append(affected, destination)
	}
	return affected, nil
}

func nonYAMLFiles(root string) ([]string, error) {
	if !exists(root) {
		return nil, nil
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || isYAMLFile(path) || entry.Name() == "_VERSION" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// replaceDirNonYAMLIfExists refreshes template-owned non-YAML files while
// retaining YAML previously reconciled by upgradeCloudOpsworksConfig.
func (r *Runner) replaceDirNonYAMLIfExists(src, dst string) ([]string, error) {
	if !exists(src) {
		return nil, nil
	}
	removed, err := r.removeNonYAMLContents(dst)
	if err != nil {
		return nil, err
	}
	copied, err := r.copyDirNonYAMLIfExists(src, dst)
	if err != nil {
		return nil, err
	}
	return append(removed, copied...), nil
}

// removeNonYAMLContents clears stale template-owned boilerplate files while
// retaining YAML that was value-merged by upgradeCloudOpsworksConfig.
func (r *Runner) removeNonYAMLContents(path string) ([]string, error) {
	files, err := nonYAMLFiles(path)
	if err != nil {
		return nil, err
	}
	removed := make([]string, 0, len(files))
	for _, rel := range files {
		file := filepath.Join(path, rel)
		if err := r.removeAll(file); err != nil {
			return nil, err
		}
		removed = append(removed, file)
	}
	return removed, nil
}
