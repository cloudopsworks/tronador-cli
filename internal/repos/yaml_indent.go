package repos

import (
	"bytes"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

const configYAMLIndent = 2

// sanitizeYAMLIndentation uses the encoder only as a structural layout oracle.
// Re-encoding the file itself would move comments and rewrite scalar styles.
// Instead, change leading whitespace at the original AST's line positions and
// retain the original text, including inactive examples and scalar payloads.
// Never accept a whitespace edit that changes the parsed configuration.
func sanitizeYAMLIndentation(data []byte) ([]byte, error) {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if !validateRenderedYAML(data) {
		return nil, fmt.Errorf("invalid YAML before indentation normalization")
	}
	var original yaml.Node
	if err := yaml.Unmarshal(data, &original); err != nil {
		return nil, err
	}
	var layout bytes.Buffer
	encoder := yaml.NewEncoder(&layout)
	encoder.SetIndent(configYAMLIndent)
	if err := encoder.Encode(&original); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	var canonical yaml.Node
	if err := yaml.Unmarshal(layout.Bytes(), &canonical); err != nil {
		return nil, err
	}
	lines := splitYAMLLines(data)
	type lineIndent struct{ column, old, new int }
	anchors := make(map[int]lineIndent)
	payload := yamlScalarPayloadLines(&original, lines)
	var collect func(*yaml.Node, *yaml.Node)
	collect = func(old, normalized *yaml.Node) {
		line := old.Line - 1
		if old.Kind != yaml.DocumentNode && line >= 0 && line < len(lines) {
			indent := leadingRawSpaces(lines[line])
			// The first token on a line owns its indentation. In particular, a
			// sequence item's key starts two columns after the '-' marker.
			if previous, ok := anchors[line]; !ok || old.Column < previous.column {
				anchors[line] = lineIndent{old.Column, indent, normalized.Column - 1 - (old.Column - 1 - indent)}
			}
		}
		// A flow collection is one token for indentation purposes. Its internal
		// layout (and multiline quoted content) is not block YAML structure.
		if old.Style&yaml.FlowStyle != 0 || len(old.Content) != len(normalized.Content) {
			return
		}
		for index, child := range old.Content {
			collect(child, normalized.Content[index])
			if old.Kind == yaml.MappingNode && index%2 == 0 && strings.HasPrefix(strings.TrimSpace(lines[child.Line-1]), "? ") {
				anchor := anchors[child.Line-1]
				anchor.new = normalized.Content[index].Column - 1
				anchors[child.Line-1] = anchor
			}
			if old.Kind == yaml.MappingNode && index%2 == 1 && child.Line > old.Content[index-1].Line && child.Kind == yaml.ScalarNode {
				// The encoder may compact an own-line scalar onto its key line.
				// Its inline value column is not a structural indentation level.
				at := child.Line - 1
				anchor := anchors[at]
				anchor.new = normalized.Content[index-1].Column - 1 + configYAMLIndent
				if strings.HasPrefix(strings.TrimSpace(lines[at]), ": ") {
					anchor.new -= configYAMLIndent
				}
				anchors[at] = anchor
			}
			if old.Kind == yaml.SequenceNode {
				// '-\n    key: value' is valid too. Every item's standalone dash
				// needs an anchor, not only the sequence's first item.
				for at := child.Line - 1; at >= old.Line-1; at-- {
					if leadingRawSpaces(lines[at]) == old.Column-1 && (strings.TrimSpace(lines[at]) == "-" || strings.HasPrefix(strings.TrimSpace(lines[at]), "- ")) {
						anchors[at] = lineIndent{old.Column, old.Column - 1, normalized.Column - 1}
						break
					}
				}
			}
		}
	}
	collect(&original, &canonical)

	delta := 0
	for index, line := range lines {
		if anchor, ok := anchors[index]; ok {
			delta = anchor.new - anchor.old
		} else if !payload[index] && strings.HasPrefix(strings.TrimSpace(line), "#") {
			// Comments are not children of the preceding scalar. Resolve their
			// own physical column against the nearest structural peer instead.
			// This keeps root documentation root-aligned after a deep sequence.
			indent := leadingRawSpaces(line)
			nearest, distance, nearestAt := 0, len(lines)+1, len(lines)
			for at, anchor := range anchors {
				if anchor.old == indent && (absLineDistance(index, at) < distance || absLineDistance(index, at) == distance && at < nearestAt) {
					nearest, distance = anchor.new-indent, absLineDistance(index, at)
					nearestAt = at
				}
			}
			lines[index] = strings.Repeat(" ", max(0, indent+nearest)) + line[indent:]
			continue
		}
		if line != "" {
			indent := leadingRawSpaces(line)
			lines[index] = strings.Repeat(" ", max(0, indent+delta)) + line[indent:]
		}
	}
	content := restoreTrailingLineFeeds([]byte(strings.Join(lines, "\n")), trailingLineFeedCount(data))
	var result yaml.Node
	if !validateRenderedYAML(content) || yaml.Unmarshal(content, &result) != nil || !semanticYAMLNodeEqual(documentRoot(&original), documentRoot(&result)) {
		return nil, fmt.Errorf("indentation normalization changed YAML semantics")
	}
	return content, nil
}

func yamlScalarPayloadLines(root *yaml.Node, lines []string) map[int]bool {
	payload := make(map[int]bool)
	var walk func(*yaml.Node)
	walk = func(node *yaml.Node) {
		line := node.Line - 1
		if node.Kind == yaml.ScalarNode && line >= 0 && line < len(lines) {
			if node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
				for at := line + 1; at < len(lines); at++ {
					if strings.TrimSpace(lines[at]) != "" && leadingRawSpaces(lines[at]) <= leadingRawSpaces(lines[line]) {
						break
					}
					payload[at] = true
				}
			} else if node.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) != 0 {
				markQuotedYAMLPayload(lines, node, payload)
			}
		}
		for _, child := range node.Content {
			walk(child)
		}
	}
	walk(root)
	return payload
}

// Quoted scalars may span physical lines even when their parsed value contains
// no newline. A leading '#' inside them is data, not a YAML comment.
func markQuotedYAMLPayload(lines []string, node *yaml.Node, payload map[int]bool) {
	quote := byte('\'')
	if node.Style&yaml.DoubleQuotedStyle != 0 {
		quote = '"'
	}
	escaped := false
	for at := node.Line - 1; at < len(lines); at++ {
		start := 0
		if at == node.Line-1 {
			// A scalar may start with an explicit YAML tag before its quote.
			opening := strings.IndexByte(lines[at][node.Column-1:], quote)
			if opening < 0 {
				return
			}
			start = node.Column + opening // skip the opening quote
		} else {
			payload[at] = true
		}
		for column := start; column < len(lines[at]); column++ {
			ch := lines[at][column]
			if escaped {
				escaped = false
				continue
			}
			if quote == '"' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				if quote == '\'' && column+1 < len(lines[at]) && lines[at][column+1] == quote {
					column++
					continue
				}
				return
			}
		}
		escaped = false
	}
}

func absLineDistance(a, b int) int {
	if a < b {
		return b - a
	}
	return a - b
}
