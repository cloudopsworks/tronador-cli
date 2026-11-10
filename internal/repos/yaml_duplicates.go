package repos

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// deduplicateYAMLTemplateEntries removes redundant inactive entries by full
// mapping path, never by leaf name. The renderer owns placement: active values
// occupy their template slots. This final pass prevents old commented copies
// from surviving elsewhere, including copies carried by the fallback encoder.
// Different template-authored value kinds remain documented alternatives.
// Conflicting active keys are rejected by validation, never silently selected.
func deduplicateYAMLTemplateEntries(template, content []byte) ([]byte, error) {
	var baseline, original yaml.Node
	if !validateRenderedYAML(template) || !validateRenderedYAML(content) || yaml.Unmarshal(template, &baseline) != nil || yaml.Unmarshal(content, &original) != nil {
		return nil, fmt.Errorf("invalid YAML before duplicate reconciliation")
	}
	if root := documentRoot(&original); root == nil || root.Kind != yaml.MappingNode {
		return content, nil
	}
	active := yamlMappingValues(documentRoot(&original))
	targetValues := yamlMappingValues(documentRoot(&baseline))
	withinSequence := func(path string) bool {
		for _, values := range []map[string]*yaml.Node{active, targetValues} {
			for parent, value := range values {
				if value.Kind == yaml.SequenceNode && strings.HasPrefix(path, parent+rawYAMLPathSeparator) {
					return true
				}
			}
		}
		return false
	}
	targetKinds := make(map[string]map[yaml.Kind]bool)
	targetDefaults := make(map[string][]*yaml.Node)
	addDefault := func(path string, value *yaml.Node) {
		if targetKinds[path] == nil {
			targetKinds[path] = make(map[yaml.Kind]bool)
		}
		targetKinds[path][value.Kind] = true
		targetDefaults[path] = append(targetDefaults[path], value)
	}
	for path, value := range targetValues {
		addDefault(path, value)
	}
	targetLines := splitYAMLLines(template)
	targetPayload := yamlScalarPayloadLines(&baseline, targetLines)
	for _, entry := range yamlRawSpanEntries(targetLines) {
		if !entry.span.commented || targetPayload[entry.span.start] || withinSequence(entry.path) {
			continue
		}
		if value := commentedEntryValue(targetLines[entry.span.start:entry.span.end], pathLeaf(entry.path)); value != nil {
			addDefault(entry.path, value)
		}
	}
	lines := splitYAMLLines(content)
	payload := yamlScalarPayloadLines(&original, lines)
	remove := make(map[int]bool)
	seen := make(map[string]bool)
	protectedUntil := -1
	for _, entry := range yamlRawSpanEntries(lines) {
		span := entry.span
		if span.start < protectedUntil || !span.commented || payload[span.start] || withinSequence(entry.path) {
			continue
		}
		value := commentedEntryValue(lines[span.start:span.end], pathLeaf(entry.path))
		if value == nil {
			continue // Prose and ambiguous examples are not configuration entries.
		}
		kinds := targetKinds[entry.path]
		configured := active[entry.path]
		knownDefault := false
		for _, candidate := range targetDefaults[entry.path] {
			knownDefault = knownDefault || semanticYAMLNodeEqual(candidate, value)
		}
		if configured != nil && value.Kind == yaml.MappingNode && (!knownDefault && !semanticYAMLNodeEqual(configured, value) || !yamlMappingShapeContains(configured, value)) {
			// Protect the whole saved subtree, not just its parent header; a
			// later child span must not chip defaults out of an operator copy.
			protectedUntil = span.end
			continue
		}
		// A saved operator value (especially an unconfigured descendant of an
		// active parent) must not disappear just because the parent is active.
		redundant := configured != nil && (knownDefault || semanticYAMLNodeEqual(configured, value)) && !(len(kinds) > 1 && value.Kind != configured.Kind)
		// Only collapse identical inactive copies. Differing commented operator
		// values are not duplicates we have authority to discard.
		identity := entry.path + "\x00" + strings.TrimSpace(strings.Join(reindentRawLines(lines[span.start:span.end], 0), "\n"))
		if redundant || seen[identity] {
			for at := span.start; at < span.end; at++ {
				remove[at] = true
			}
		}
		seen[identity] = true
	}
	if len(remove) == 0 {
		return content, nil
	}
	kept := make([]string, 0, len(lines))
	for at, line := range lines {
		if !remove[at] {
			kept = append(kept, line)
		}
	}
	result := restoreTrailingLineFeeds([]byte(strings.Join(kept, "\n")), trailingLineFeedCount(content))
	var after yaml.Node
	if !validateRenderedYAML(result) || yaml.Unmarshal(result, &after) != nil || !semanticYAMLNodeEqual(documentRoot(&original), documentRoot(&after)) {
		return nil, fmt.Errorf("duplicate reconciliation changed YAML semantics")
	}
	return result, nil
}

// Defaults for still-unconfigured descendants are useful template documentation,
// not duplicates of the active parent. Keep them rather than deleting the whole
// commented subtree during an atomic/fallback merge.
func yamlMappingShapeContains(configured, candidate *yaml.Node) bool {
	if candidate.Kind != yaml.MappingNode {
		return true
	}
	if configured.Kind != yaml.MappingNode {
		return false
	}
	for index := 0; index+1 < len(candidate.Content); index += 2 {
		at := mappingIndex(configured, candidate.Content[index].Value)
		if at < 0 || !yamlMappingShapeContains(configured.Content[at+1], candidate.Content[index+1]) {
			return false
		}
	}
	return true
}

func yamlMappingValues(root *yaml.Node) map[string]*yaml.Node {
	values := make(map[string]*yaml.Node)
	var walk func(*yaml.Node, string)
	walk = func(node *yaml.Node, path string) {
		if node == nil || node.Kind != yaml.MappingNode {
			return // Sequence members have distinct identities, not global paths.
		}
		for index := 0; index+1 < len(node.Content); index += 2 {
			child := joinRawYAMLPath(path, node.Content[index].Value)
			values[child] = node.Content[index+1]
			walk(node.Content[index+1], child)
		}
	}
	walk(root, "")
	return values
}

func commentedEntryValue(lines []string, key string) *yaml.Node {
	if !safeCommentedCandidate(lines, key) {
		return nil
	}
	plain := make([]string, len(lines))
	for index, line := range lines {
		plain[index] = uncommentStructuralLine(line)
	}
	var node yaml.Node
	if yaml.Unmarshal([]byte(strings.Join(plain, "\n")), &node) != nil {
		return nil
	}
	root := documentRoot(&node)
	if root == nil || root.Kind != yaml.MappingNode || len(root.Content) != 2 {
		return nil
	}
	return root.Content[1]
}
