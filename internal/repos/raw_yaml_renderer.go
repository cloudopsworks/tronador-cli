package repos

import (
	"bytes"
	"io"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// renderYAMLTargetBaseline changes only provable target spans. This keeps the
// template's whitespace and inactive examples intact, unlike yaml.Marshal
// which attaches trailing comments to whichever node it happens to emit next.
// It returns false when a local value cannot be safely placed in the template.
func renderYAMLTargetBaseline(target, local []byte, targetNode, localNode *yaml.Node) ([]byte, bool) {
	targetLines := splitYAMLLines(target)
	localLines := splitYAMLLines(local)
	targetSpans := yamlRawSpans(targetLines)
	localSpans := yamlRawSpans(localLines)
	replacements := make(map[int]rawReplacement)
	if !overlayRawYAML(documentRoot(targetNode), documentRoot(localNode), "", targetLines, localLines, targetSpans, localSpans, replacements) {
		return nil, false
	}
	if len(replacements) == 0 {
		return target, true
	}
	starts := make([]int, 0, len(replacements))
	for start := range replacements {
		starts = append(starts, start)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(starts)))
	for _, start := range starts {
		replacement := replacements[start]
		targetLines = append(targetLines[:replacement.start], append(replacement.lines, targetLines[replacement.end:]...)...)
	}
	content := []byte(strings.Join(targetLines, "\n"))
	// Replacement spans may extend to EOF and therefore consume the empty
	// line entries that represent the target's terminal newlines. Terminal
	// newlines are file-level formatting, not part of a YAML value span: keep
	// the target count exactly (including zero) while leaving interior blank
	// lines untouched.
	content = restoreTrailingLineFeeds(content, trailingLineFeedCount(target))
	if !validateRenderedYAML(content) || !renderedYAMLSemanticallyMatches(content, targetNode, localNode) {
		return nil, false
	}
	return content, true
}

type rawYAMLSpan struct {
	start, end   int
	indent       int
	renderIndent int
	commented    bool
}

type rawReplacement struct {
	start, end int
	lines      []string
}

func trailingLineFeedCount(data []byte) int {
	count := 0
	for index := len(data) - 1; index >= 0 && data[index] == '\n'; index-- {
		count++
	}
	return count
}

func restoreTrailingLineFeeds(data []byte, count int) []byte {
	data = bytes.TrimRight(data, "\n")
	return append(data, bytes.Repeat([]byte{'\n'}, count)...)
}

func splitYAMLLines(data []byte) []string {
	// Raw rendering joins with LF because the target baseline owns presentation.
	// Normalize CRLF inputs before span and inline-comment processing so a
	// trailing carriage return cannot turn an otherwise equal inline comment
	// into a distinct one on every subsequent upgrade.
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

const rawYAMLPathSeparator = "\x1f"

// Raw span paths are structural, not dot-joined: YAML keys may themselves contain dots.
func joinRawYAMLPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + rawYAMLPathSeparator + key
}

func yamlRawSpans(lines []string) map[string]rawYAMLSpan {
	// An earlier upgrade can leave an active entry alongside its old commented
	// placeholder. The active entry is the only span represented by the parsed
	// YAML document, so it must win over the comment. Multiple spans with the
	// same activity are deliberately omitted: callers then fail closed instead
	// of choosing an arbitrary duplicate.
	byPath := make(map[string][]rawYAMLSpan)
	for _, entry := range yamlRawSpanEntries(lines) {
		byPath[entry.path] = append(byPath[entry.path], entry.span)
	}
	spans := make(map[string]rawYAMLSpan, len(byPath))
	for path, candidates := range byPath {
		var active, commented []rawYAMLSpan
		for _, candidate := range candidates {
			if candidate.commented {
				commented = append(commented, candidate)
			} else {
				active = append(active, candidate)
			}
		}
		switch {
		case len(active) == 1:
			spans[path] = active[0]
		case len(active) == 0 && len(commented) > 0:
			// Keep a representative commented span so the activation path can
			// count duplicates and reject that ambiguity explicitly.
			spans[path] = commented[len(commented)-1]
		}
	}
	return spans
}

type rawYAMLSpanEntry struct {
	path string
	span rawYAMLSpan
}

func yamlRawSpanEntries(lines []string) []rawYAMLSpanEntry {
	type stackItem struct {
		indent    int
		path      string
		commented bool
	}
	var entries []rawYAMLSpanEntry
	var stack []stackItem
	for index, line := range lines {
		indent, key, commented, ok := rawYAMLKey(line)
		if !ok {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		// Commented-out keys describe inactive defaults, not parents of a
		// subsequent active sibling. Drop them before resolving an active path;
		// otherwise a commented `extra:` makes a later active sibling appear as
		// `extra.sibling` instead of its YAML parent's child.
		if !commented {
			for len(stack) > 0 && stack[len(stack)-1].commented {
				stack = stack[:len(stack)-1]
			}
		}
		path := key
		if len(stack) > 0 {
			path = joinRawYAMLPath(stack[len(stack)-1].path, key)
		}
		entries = append(entries, rawYAMLSpanEntry{path: path, span: rawYAMLSpan{start: index, indent: indent, commented: commented}})
		stack = append(stack, stackItem{indent: indent, path: path, commented: commented})
	}
	// A leading space after '#' is a comment pad when the root default is
	// written as `# key:`; descendants use that same pad. With `#key:` the
	// pad is zero. Preserve the resulting YAML indentation independently from
	// the parser indentation used only for raw path lookup.
	rootPads := make(map[string]int)
	for _, entry := range entries {
		if entry.span.commented && !strings.Contains(entry.path, rawYAMLPathSeparator) {
			rootPads[entry.path] = commentedKeySpaces(lines[entry.span.start])
		}
	}
	for entryIndex := range entries {
		span := entries[entryIndex].span
		if span.commented {
			root := entryPathRoot(entries[entryIndex].path)
			pad := rootPads[root]
			physical := len(lines[span.start]) - len(strings.TrimLeft(lines[span.start], " "))
			span.renderIndent = physical + commentedKeySpaces(lines[span.start]) - pad
		} else {
			span.renderIndent = span.indent
		}
		end := len(lines)
		for index := span.start + 1; index < len(lines); index++ {
			indent, _, _, ok := rawYAMLKey(lines[index])
			if ok && indent <= span.indent {
				end = index
				// A separated prose/blank block before the next key belongs to
				// that next key, not to the preceding value being replaced. Do
				// not steal an adjacent prose line from a commented candidate:
				// it is evidence that the candidate is documentation, not YAML.
				for previous := end - 1; previous > span.start; previous-- {
					trimmed := strings.TrimSpace(lines[previous])
					if trimmed == "" {
						end = previous
						continue
					}
					if strings.HasPrefix(trimmed, "#") {
						// A commented sequence item may be the continuation of a
						// mapping-valued item. It is structural, not prose, even
						// though it does not look like a mapping key by itself.
						if strings.HasPrefix(strings.TrimSpace(uncommentStructuralLine(lines[previous])), "-") {
							break
						}
						if _, _, _, isKey := rawYAMLKey(lines[previous]); !isKey {
							end = previous
							continue
						}
					}
					break
				}
				break
			}
		}
		span.end = end
		entries[entryIndex].span = span
	}
	return entries
}

func rawYAMLKey(line string) (indent int, key string, commented bool, ok bool) {
	indent = len(line) - len(strings.TrimLeft(line, " "))
	rest := line[indent:]
	if strings.HasPrefix(rest, "#") {
		commented = true
		rest = strings.TrimPrefix(rest, "#")
		if strings.HasPrefix(rest, " ") {
			rest = rest[1:]
		}
		extraIndent := len(rest) - len(strings.TrimLeft(rest, " "))
		indent += extraIndent
		rest = rest[extraIndent:]
	}
	colon := strings.IndexByte(rest, ':')
	if colon <= 0 || strings.HasPrefix(rest, "-") {
		return 0, "", false, false
	}
	key = strings.TrimSpace(rest[:colon])
	if len(key) >= 2 && ((key[0] == '\'' && key[len(key)-1] == '\'') || (key[0] == '"' && key[len(key)-1] == '"')) {
		key = key[1 : len(key)-1]
	}
	if key == "" || strings.ContainsAny(key, " \t") {
		return 0, "", false, false
	}
	return indent, key, commented, true
}

func overlayRawYAML(target, local *yaml.Node, path string, targetLines, localLines []string, targetSpans, localSpans map[string]rawYAMLSpan, replacements map[int]rawReplacement) bool {
	if local == nil || local.Kind != yaml.MappingNode || (path != "" && (target == nil || target.Kind != yaml.MappingNode)) {
		return false
	}
	for index := 0; index+1 < len(local.Content); index += 2 {
		key, localValue := local.Content[index], local.Content[index+1]
		childPath := key.Value
		if path != "" {
			childPath = joinRawYAMLPath(path, key.Value)
		}
		targetIndex := -1
		if target != nil && target.Kind == yaml.MappingNode {
			targetIndex = mappingIndex(target, key.Value)
		}
		targetSpan, targetHasSpan := targetSpans[childPath]
		localSpan, localHasSpan := localSpans[childPath]
		if !localHasSpan {
			return false
		}
		if !targetHasSpan {
			if path != "" {
				if !insertRawLocalChild(path, childPath, key, localValue, localSpan, targetLines, localLines, targetSpans, localSpans, replacements) {
					return false
				}
				continue
			}
			lines := reindentRawLines(localLines[localSpan.start:localSpan.end], 0)
			if localValue.Kind == yaml.MappingNode || localValue.Kind == yaml.SequenceNode {
				var ok bool
				lines, ok = normalizedYAMLMappingEntry(key, localValue, 0, 2)
				if !ok {
					return false
				}
			}
			if !addRawReplacement(replacements, rawYAMLSpan{start: len(targetLines), end: len(targetLines)}, append([]string{""}, lines...)) {
				return false
			}
			continue
		}
		if targetIndex < 0 && path != "" && (!targetSpan.commented || localValue.Kind != yaml.SequenceNode && !safeCommentedCandidate(targetLines[targetSpan.start:targetSpan.end], key.Value)) {
			// A raw commented line can look like a child key while the parsed target
			// has no such member (documentation/prose). Insert beside it rather than
			// consuming the prose as a structural default.
			if !insertRawLocalChild(path, childPath, key, localValue, localSpan, targetLines, localLines, targetSpans, localSpans, replacements) {
				return false
			}
			continue
		}
		if targetIndex < 0 || targetSpan.commented {
			if !targetSpan.commented || countCommentedPath(targetLines, childPath, targetSpan.indent) != 1 {
				return false
			}
			candidateReplacements, ok := []rawReplacement(nil), false
			// A directly-commented sequence header is a structural template
			// placeholder even when explanatory prose follows it. Activate only
			// its structural lines at that exact position; the prose remains in
			// place rather than forcing an EOF append.
			if (localValue.Kind == yaml.SequenceNode && safeCommentedCandidate([]string{targetLines[targetSpan.start]}, key.Value)) || (safeCommentedCandidate(targetLines[targetSpan.start:targetSpan.end], key.Value) && !hasFollowingCommentProse(targetLines, targetSpan.end)) {
				candidateReplacements, ok = activateCommentedValue(target, localValue, childPath, targetLines, localLines, targetSpans, localSpans)
			} else if localValue.Kind == yaml.ScalarNode && safeCommentedScalarLine(targetLines[targetSpan.start], key.Value) {
				// Documentation around a scalar default belongs to neighboring
				// sections, but the exact no-gap commented key is still a provable
				// template placeholder. Replace only that line.
				candidateReplacements, ok = []rawReplacement{{start: targetSpan.start, end: targetSpan.start + 1, lines: reindentRawLines(localLines[localSpan.start:localSpan.start+1], renderedTargetIndent(childPath, targetSpans))}}, true
			} else if localValue.Kind != yaml.MappingNode && path == "" {
				// A prose line may contain a colon and resemble a key. Do not
				// consume it; append the active local scalar after the template.
				candidateReplacements, ok = []rawReplacement{{start: len(targetLines), end: len(targetLines), lines: append([]string{""}, reindentRawLines(localLines[localSpan.start:localSpan.end], 0)...)}}, true
			} else {
				// This is prose/documentation, not a default to reconstruct. Keep
				// it as comments below an activated header and place local values
				// after the documented block, avoiding a duplicate #key placeholder.
				candidateReplacements, ok = renderDocumentedCommentedValue(localValue, childPath, targetLines, localLines, targetSpans, localSpans)
			}
			if !ok {
				return false
			}
			for _, replacement := range candidateReplacements {
				if !addRawReplacement(replacements, rawYAMLSpan{start: replacement.start, end: replacement.end}, replacement.lines) {
					return false
				}
			}
			if len(candidateReplacements) == 0 {
				return false
			}
			continue
		}
		targetValue := target.Content[targetIndex+1]
		if targetValue.Kind == yaml.MappingNode && localValue.Kind == yaml.MappingNode {
			// A flow-style mapping has no raw child spans to recurse through.  It
			// is still structurally safe to render as one collection because the
			// active target entry proves both its position and indentation.  The
			// same applies to an empty mapping or a block mapping whose children
			// cannot all be located in the source text.
			expandTargetEmptyFlow := targetEmptyFlowMapping(targetValue, targetLines[targetSpan.start])
			if mappingNeedsAtomicRender(localValue, childPath, localSpans) || expandTargetEmptyFlow {
				replacement, ok := normalizedYAMLMappingEntry(key, localValue, renderedTargetIndent(childPath, targetSpans), yamlParentIndentWidth(childPath, targetSpans))
				if !ok {
					return false
				}
				replacement = preserveTargetKeyComment(targetLines[targetSpan.start:targetSpan.end], replacement)
				span := targetSpan
				// An empty inline mapping may be followed by commented template
				// examples. Only replace its proven header: those examples are
				// documentation, not members of the parsed empty map, and must
				// remain in their target location.
				if expandTargetEmptyFlow {
					span.end = span.start + 1
				}
				if !addRawReplacement(replacements, span, replacement) {
					return false
				}
				continue
			}
			if !overlayRawYAML(targetValue, localValue, childPath, targetLines, localLines, targetSpans, localSpans, replacements) {
				return false
			}
			continue
		}
		if targetValue.Kind == yaml.ScalarNode && localValue.Kind == yaml.ScalarNode && localValue.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0 {
			// Raw spans can include a following physical comment block when its
			// virtual indentation resembles this scalar's descendants. For an
			// ordinary scalar the parsed node proves that only its key/value line
			// is configuration; target baseline documentation owns the remainder.
			line := replaceRawScalar(targetLines[targetSpan.start], reindentRawLines(localLines[localSpan.start:localSpan.start+1], renderedTargetIndent(childPath, targetSpans))[0])
			if !addRawReplacement(replacements, rawYAMLSpan{start: targetSpan.start, end: targetSpan.start + 1}, []string{line}) {
				return false
			}
			continue
		}
		// Sequences, multiline scalars and type changes are atomic. A local
		// sequence can carry stale commented defaults from a prior upgrade after
		// its final active entry; they are not sequence content and must not be
		// copied over the target's own documented examples.
		localSource := localLines[localSpan.start:localSpan.end]
		if localValue.Kind == yaml.SequenceNode {
			localSource = trimTrailingSequenceCommentMaterial(localSource)
		}
		replacement := reindentRawLines(localSource, renderedTargetIndent(childPath, targetSpans))
		// Collections are atomic. Always encode them using the owning target
		// mapping's child width, including same-kind sequences: sequence item
		// descendants are not evidence of the width of this mapping level.
		if localValue.Kind == yaml.MappingNode || localValue.Kind == yaml.SequenceNode {
			if normalized, ok := normalizedYAMLMappingEntry(key, localValue, renderedTargetIndent(childPath, targetSpans), yamlParentIndentWidth(childPath, targetSpans)); ok {
				replacement = normalized
			} else {
				return false
			}
		}
		replacement = preserveTargetKeyComment(targetLines[targetSpan.start:targetSpan.end], preserveSequenceComments(targetLines[targetSpan.start:targetSpan.end], replacement))
		if !addRawReplacement(replacements, targetSpan, replacement) {
			return false
		}
	}
	return true
}

func targetEmptyFlowMapping(value *yaml.Node, line string) bool {
	if value == nil || value.Kind != yaml.MappingNode || len(value.Content) != 0 {
		return false
	}
	plain, _ := splitRawInlineComment(line)
	colon := strings.IndexByte(plain, ':')
	return colon >= 0 && strings.TrimSpace(plain[colon+1:]) == "{}"
}

// mappingNeedsAtomicRender reports whether a mapping cannot be safely updated
// entry-by-entry from raw source lines. Flow mappings and empty mappings do not
// expose child-line spans; rendering their already-parsed value at the active
// target slot preserves semantics while taking indentation from that target.
func mappingNeedsAtomicRender(value *yaml.Node, path string, spans map[string]rawYAMLSpan) bool {
	if value == nil || value.Kind != yaml.MappingNode {
		return false
	}
	if value.Style&yaml.FlowStyle != 0 || len(value.Content) == 0 {
		return true
	}
	for index := 0; index+1 < len(value.Content); index += 2 {
		if _, ok := spans[joinRawYAMLPath(path, value.Content[index].Value)]; !ok {
			return true
		}
	}
	return false
}

// yamlIndentWidth returns the direct-child indentation of a target mapping.
// It intentionally measures the containing mapping, rather than descendants of
// a collection being replaced: a sequence's first mapping item is not evidence
// for the mapping's child width.
func yamlIndentWidth(path string, spans map[string]rawYAMLSpan) int {
	parent, ok := spans[path]
	if !ok {
		return 2
	}
	width := 0
	for candidatePath, candidate := range spans {
		if !isDirectYAMLChild(path, candidatePath) {
			continue
		}
		delta := candidate.renderIndent - parent.renderIndent
		if delta > 0 && (width == 0 || delta < width) {
			width = delta
		}
	}
	if width == 0 {
		return 2
	}
	return width
}

// yamlParentIndentWidth returns the indentation width for children of the
// mapping that owns path.  Atomic replacements use this instead of looking at
// their own descendants (which may be sequence-item mappings).
func yamlParentIndentWidth(path string, spans map[string]rawYAMLSpan) int {
	separator := strings.LastIndex(path, rawYAMLPathSeparator)
	if separator < 0 {
		return 2
	}
	return yamlIndentWidth(path[:separator], spans)
}

// normalizedYAMLMappingEntry emits one semantic mapping entry with a controlled
// collection indentation. It deliberately applies only to inserted/type-changed
// mappings and sequences; scalars and block styles retain their raw local form.
func normalizedYAMLMappingEntry(key, value *yaml.Node, targetIndent, width int) ([]string, bool) {
	if key == nil || value == nil || (value.Kind != yaml.MappingNode && value.Kind != yaml.SequenceNode && (value.Kind != yaml.ScalarNode || value.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0)) {
		return nil, false
	}
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			cloneYAMLNodeForNormalizedOutput(key),
			cloneYAMLNodeForNormalizedOutput(value),
		},
	}}}
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(width)
	if err := encoder.Encode(root); err != nil {
		return nil, false
	}
	if err := encoder.Close(); err != nil {
		return nil, false
	}
	return reindentRawLines(splitYAMLLines(encoded.Bytes()), targetIndent), true
}

func cloneYAMLNodeForNormalizedOutput(node *yaml.Node) *yaml.Node {
	clone := cloneYAMLNode(node)
	if clone == nil {
		return nil
	}
	clone.HeadComment = ""
	clone.FootComment = ""
	for _, child := range clone.Content {
		clearYAMLHeadAndFootComments(child)
	}
	return clone
}

func clearYAMLHeadAndFootComments(node *yaml.Node) {
	if node == nil {
		return
	}
	node.HeadComment = ""
	// Line comments are part of an active scalar/sequence item (for example
	// `- runtime # retained`) and remain meaningful local configuration.
	node.FootComment = ""
	for _, child := range node.Content {
		clearYAMLHeadAndFootComments(child)
	}
}

// insertRawLocalChild adds a local-only mapping member inside an active target
// parent. The target parent is already proven by the semantic merge; choosing
// the first inactive direct child keeps the target's documented defaults below
// the newly active value rather than moving them into a following section.
func insertRawLocalChild(parentPath, childPath string, key, value *yaml.Node, localSpan rawYAMLSpan, targetLines, localLines []string, targetSpans, localSpans map[string]rawYAMLSpan, replacements map[int]rawReplacement) bool {
	parentSpan, targetOK := targetSpans[parentPath]
	_, localParentOK := localSpans[parentPath]
	if !targetOK || !localParentOK || parentSpan.commented || localSpan.start >= localSpan.end {
		return false
	}
	if key == nil || value == nil {
		return false
	}
	insertAt := parentSpan.end
	for _, entry := range yamlRawSpanEntries(targetLines) {
		if entry.span.start <= parentSpan.start || entry.span.start >= parentSpan.end || !entry.span.commented || !isDirectYAMLChild(parentPath, entry.path) {
			continue
		}
		if entry.span.start < insertAt {
			insertAt = entry.span.start
		}
	}
	// Keep an adjacent prose/comment block with the inactive default rather than
	// splitting it from the documented option that follows the inserted value.
	for insertAt > parentSpan.start+1 {
		previous := strings.TrimSpace(targetLines[insertAt-1])
		if previous == "" || strings.HasPrefix(previous, "#") {
			insertAt--
			continue
		}
		break
	}
	childIndent := renderedTargetIndent(parentPath, targetSpans) + yamlIndentWidth(parentPath, targetSpans)
	lines := reindentRawLines(localLines[localSpan.start:localSpan.end], childIndent)
	if value.Kind == yaml.MappingNode || value.Kind == yaml.SequenceNode {
		var ok bool
		lines, ok = normalizedYAMLMappingEntry(key, value, childIndent, yamlIndentWidth(parentPath, targetSpans))
		if !ok {
			return false
		}
	}
	return addRawReplacement(replacements, rawYAMLSpan{start: insertAt, end: insertAt}, lines)
}

func isDirectYAMLChild(parentPath, candidatePath string) bool {
	prefix := parentPath + rawYAMLPathSeparator
	if !strings.HasPrefix(candidatePath, prefix) {
		return false
	}
	return !strings.Contains(strings.TrimPrefix(candidatePath, prefix), rawYAMLPathSeparator)
}

func countCommentedPath(lines []string, path string, indent int) int {
	count := 0
	for _, entry := range yamlRawSpanEntries(lines) {
		if entry.path == path && entry.span.commented && entry.span.indent == indent {
			count++
		}
	}
	return count
}

// safeCommentedCandidate only recognizes a block when it can be reconstructed
// as exactly one YAML mapping entry. This rejects narrative comments that look
// like YAML by accident, mixed active/commented blocks, and ambiguous defaults.
func hasFollowingCommentProse(lines []string, end int) bool {
	if end >= len(lines) {
		return false
	}
	trimmed := strings.TrimSpace(lines[end])
	if !strings.HasPrefix(trimmed, "#") {
		return false
	}
	_, _, _, isKey := rawYAMLKey(lines[end])
	return !isKey
}

func safeCommentedCandidate(lines []string, wantedKey string) bool {
	if len(lines) == 0 {
		return false
	}
	// A single space after the marker with no additional virtual indentation
	// (for example "# key: explanation") is usually narrative prose. The
	// decisive evidence is indentation encoded after the marker, not the key
	// name: "#      targets:" is a nested structural default even though it
	// begins with "# ".
	firstPhysicalIndent := leadingRawSpaces(lines[0])
	firstVirtualIndent, _, firstCommented, firstIsKey := rawYAMLKey(lines[0])
	first := strings.TrimLeft(lines[0], " ")
	if !firstCommented || !firstIsKey || (strings.HasPrefix(first, "# ") && firstVirtualIndent <= firstPhysicalIndent) {
		return false
	}
	plain := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			plain = append(plain, "")
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		rest := line[indent:]
		if !strings.HasPrefix(rest, "#") {
			return false
		}
		uncommented := uncommentStructuralLine(line)
		trimmed := strings.TrimSpace(uncommented)
		if _, _, _, ok := rawYAMLKey(line); !ok && !strings.HasPrefix(trimmed, "-") {
			return false
		}
		plain = append(plain, uncommented)
	}
	data := []byte(strings.Join(plain, "\n") + "\n")
	if !validateRenderedYAML(data) {
		return false
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return false
	}
	root := documentRoot(&node)
	return root != nil && root.Kind == yaml.MappingNode && len(root.Content) == 2 && root.Content[0].Value == wantedKey
}

func safeCommentedScalarLine(line, wantedKey string) bool {
	trimmed := strings.TrimLeft(line, " ")
	// A space directly after '#' marks prose rather than the template's
	// commented-out scalar convention.
	if !strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "# ") {
		return false
	}
	return safeCommentedCandidate([]string{line}, wantedKey)
}

func uncommentStructuralLine(line string) string {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	rest := line[indent:]
	if !strings.HasPrefix(rest, "#") {
		return line
	}
	rest = rest[1:]
	if strings.HasPrefix(rest, " ") {
		rest = rest[1:]
	}
	return strings.Repeat(" ", indent) + rest
}

func activateCommentedValue(target, local *yaml.Node, path string, targetLines, localLines []string, targetSpans, localSpans map[string]rawYAMLSpan) ([]rawReplacement, bool) {
	targetSpan, targetOK := targetSpans[path]
	localSpan, localOK := localSpans[path]
	if !targetOK || !localOK || !targetSpan.commented {
		return nil, false
	}
	if local.Kind == yaml.MappingNode {
		header := []string{activatedMappingHeader(targetLines[targetSpan.start], renderedTargetIndent(path, targetSpans))}
		replacements := []rawReplacement{{start: targetSpan.start, end: targetSpan.start + 1, lines: header}}
		for index := 0; index+1 < len(local.Content); index += 2 {
			key, localValue := local.Content[index], local.Content[index+1]
			childPath := joinRawYAMLPath(path, key.Value)
			childSpan, hasChild := targetSpans[childPath]
			if hasChild && childSpan.commented && countCommentedPath(targetLines, childPath, childSpan.indent) == 1 && (safeCommentedCandidate(targetLines[childSpan.start:childSpan.end], key.Value) || commentedFlowCollectionHeader(targetLines[childSpan.start], key.Value)) {
				child, ok := activateCommentedValue(nil, localValue, childPath, targetLines, localLines, targetSpans, localSpans)
				if !ok {
					return nil, false
				}
				replacements = append(replacements, child...)
				continue
			}
			// A missing child (or prose-shaped comment that must not be consumed)
			// is inserted at this mapping's proven child indentation. Crucially, do
			// not replace the parent span: target-only commented defaults and their
			// documentation remain exactly where the template put them.
			insert, ok := normalizedCommentedMappingChild(path, key, localValue, targetLines, targetSpans)
			if !ok {
				return nil, false
			}
			replacements = append(replacements, insert)
		}
		return replacements, true
	}
	if local.Kind == yaml.ScalarNode {
		return []rawReplacement{{start: targetSpan.start, end: targetSpan.start + 1, lines: reindentRawLines(localLines[localSpan.start:localSpan.start+1], renderedTargetIndent(path, targetSpans))}}, true
	}
	// Sequences are intentionally atomic. Limit the replaced target span to
	// contiguous structural commented sequence lines so following prose remains
	// attached to its original template section.
	sequenceEnd := commentedSequenceSpanEnd(targetLines, targetSpan)
	targetSequence := targetLines[targetSpan.start:sequenceEnd]
	localSource := trimTrailingSequenceCommentMaterial(localLines[localSpan.start:localSpan.end])
	lines := reindentRawLines(localSource, renderedTargetIndent(path, targetSpans))
	if normalized, ok := normalizedYAMLMappingEntry(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: pathLeaf(path)}, local, renderedTargetIndent(path, targetSpans), yamlParentIndentWidth(path, targetSpans)); ok {
		lines = normalized
	}
	lines = preserveTargetKeyComment(targetSequence, preserveSequenceComments(targetSequence, lines))
	return []rawReplacement{{start: targetSpan.start, end: sequenceEnd, lines: lines}}, true
}

func commentedFlowCollectionHeader(line, wantedKey string) bool {
	_, key, commented, ok := rawYAMLKey(line)
	if !ok || !commented || key != wantedKey {
		return false
	}
	plain, _ := splitRawInlineComment(uncommentStructuralLine(line))
	colon := strings.IndexByte(plain, ':')
	if colon < 0 {
		return false
	}
	value := strings.TrimSpace(plain[colon+1:])
	return value == "{}" || value == "[]"
}

func activatedMappingHeader(line string, targetIndent int) string {
	plain := uncommentStructuralLine(line)
	value, comment := splitRawInlineComment(plain)
	colon := strings.IndexByte(value, ':')
	if colon < 0 {
		return strings.Repeat(" ", targetIndent) + strings.TrimSpace(value)
	}
	return strings.Repeat(" ", targetIndent) + strings.TrimSpace(value[:colon]) + ":" + func() string {
		if comment == "" {
			return ""
		}
		return " " + comment
	}()
}

func pathLeaf(path string) string {
	if separator := strings.LastIndex(path, rawYAMLPathSeparator); separator >= 0 {
		return path[separator+len(rawYAMLPathSeparator):]
	}
	return path
}

func normalizedCommentedMappingChild(parentPath string, key, value *yaml.Node, targetLines []string, targetSpans map[string]rawYAMLSpan) (rawReplacement, bool) {
	parent, ok := targetSpans[parentPath]
	if !ok || !parent.commented {
		return rawReplacement{}, false
	}
	insertAt := parent.end
	for _, entry := range yamlRawSpanEntries(targetLines) {
		if entry.span.start <= parent.start || !entry.span.commented || !isDirectYAMLChild(parentPath, entry.path) {
			continue
		}
		if entry.span.start < insertAt {
			insertAt = entry.span.start
		}
	}
	lines, ok := normalizedYAMLMappingEntry(key, value, renderedTargetIndent(parentPath, targetSpans)+yamlIndentWidth(parentPath, targetSpans), yamlIndentWidth(parentPath, targetSpans))
	if !ok {
		return rawReplacement{}, false
	}
	return rawReplacement{start: insertAt, end: insertAt, lines: lines}, true
}

func commentedSequenceSpanEnd(lines []string, span rawYAMLSpan) int {
	// A commented sequence can contain mapping-valued items.  Treat the header,
	// every commented item, and its commented descendants as one structural
	// candidate; otherwise replacing only `#  - name:` leaves orphaned
	// `#    key:` continuations behind.  Stop at prose/non-comment material and
	// retain the longest slice that reconstructs exactly this mapping entry.
	best := span.start + 1
	for end := span.start + 2; end <= span.end; end++ {
		line := lines[end-1]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !strings.HasPrefix(trimmed, "#") {
			break
		}
		uncommented := strings.TrimSpace(uncommentStructuralLine(line))
		if !strings.HasPrefix(uncommented, "-") {
			if _, _, _, isKey := rawYAMLKey(line); !isKey {
				break
			}
		}
		if safeCommentedCandidate(lines[span.start:end], pathLeafForSpan(lines[span.start])) {
			best = end
		}
	}
	return best
}

func pathLeafForSpan(line string) string {
	_, key, _, ok := rawYAMLKey(line)
	if !ok {
		return ""
	}
	return key
}

// trimTrailingSequenceCommentMaterial removes only a trailing suffix of blank
// or comment-only lines from an active local sequence source. Such lines can be
// obsolete inactive configuration left by a prior upgrade. Block-scalar content
// is intentionally never trimmed: a line beginning with '#' is data inside a
// literal or folded scalar, not a YAML comment.
func trimTrailingSequenceCommentMaterial(lines []string) []string {
	end := len(lines)
	for end > 0 {
		trimmed := strings.TrimSpace(lines[end-1])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		if rawLineIsBlockScalarContent(lines, end-1) {
			return lines
		}
		end--
	}
	return lines[:end]
}

func rawLineIsBlockScalarContent(lines []string, wanted int) bool {
	blockIndent := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := leadingRawSpaces(line)
		if blockIndent >= 0 && trimmed != "" && indent <= blockIndent {
			blockIndent = -1
		}
		if index == wanted {
			return blockIndent >= 0
		}
		if blockIndent < 0 && rawLineStartsBlockScalar(line) {
			blockIndent = indent
		}
	}
	return false
}

func rawLineStartsBlockScalar(line string) bool {
	value, _ := splitRawInlineComment(line)
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "- ") {
		value = strings.TrimSpace(strings.TrimPrefix(value, "-"))
	}
	return value == "|" || value == ">" || value == "|-" || value == ">-" || value == "|+" || value == ">+" || strings.HasSuffix(value, ": |") || strings.HasSuffix(value, ": >") || strings.HasSuffix(value, ": |-") || strings.HasSuffix(value, ": >-") || strings.HasSuffix(value, ": |+") || strings.HasSuffix(value, ": >+")
}

func renderDocumentedCommentedValue(local *yaml.Node, path string, targetLines, localLines []string, targetSpans, localSpans map[string]rawYAMLSpan) ([]rawReplacement, bool) {
	targetSpan, targetOK := targetSpans[path]
	localSpan, localOK := localSpans[path]
	if !targetOK || !localOK || !targetSpan.commented || local.Kind != yaml.MappingNode || localSpan.end <= localSpan.start+1 {
		return nil, false
	}
	// Keep prose as comments within an activated header; it is not parsed as a
	// default, while the active local entry remains the only structural key.
	header := rawReplacement{start: targetSpan.start, end: targetSpan.start + 1, lines: reindentRawLines([]string{uncommentStructuralLine(targetLines[targetSpan.start])}, renderedTargetIndent(path, targetSpans))}
	replacements := []rawReplacement{header}
	var missingLines []string
	for index := 0; index+1 < len(local.Content); index += 2 {
		key, localValue := local.Content[index], local.Content[index+1]
		childPath := joinRawYAMLPath(path, key.Value)
		childSpan, exists := targetSpans[childPath]
		if exists && childSpan.commented && countCommentedPath(targetLines, childPath, childSpan.indent) == 1 {
			child, ok := activateCommentedValue(nil, localValue, childPath, targetLines, localLines, targetSpans, localSpans)
			if ok {
				replacements = append(replacements, child...)
				continue
			}
		}
		localChildSpan, ok := localSpans[childPath]
		if !ok {
			return nil, false
		}
		childIndent := renderedTargetIndent(path, targetSpans) + yamlIndentWidth(path, targetSpans)
		lines := reindentRawLines(localLines[localChildSpan.start:localChildSpan.end], childIndent)
		if localValue.Kind == yaml.MappingNode || localValue.Kind == yaml.SequenceNode {
			var normalized bool
			lines, normalized = normalizedYAMLMappingEntry(key, localValue, childIndent, yamlIndentWidth(path, targetSpans))
			if !normalized {
				return nil, false
			}
		}
		if len(missingLines) == 0 {
			missingLines = append(missingLines, "")
		}
		missingLines = append(missingLines, lines...)
	}
	if len(missingLines) > 0 {
		replacements = append(replacements, rawReplacement{start: targetSpan.end, end: targetSpan.end, lines: missingLines})
	}
	return replacements, true
}

func preserveTargetKeyComment(target, replacement []string) []string {
	if len(target) == 0 || len(replacement) == 0 {
		return replacement
	}
	commentSource := target[0]
	_, _, commented, isKey := rawYAMLKey(target[0])
	if isKey && commented {
		// The leading # is structural, not an inline comment. Any comment left
		// after uncommenting the key line is a real target annotation to retain.
		commentSource = uncommentStructuralLine(target[0])
	}
	_, comment := splitRawInlineComment(commentSource)
	if comment == "" {
		return replacement
	}
	out := append([]string(nil), replacement...)
	out[0] = replaceRawScalar(commentSource, out[0])
	return out
}

func preserveSequenceComments(target, replacement []string) []string {
	out := append([]string(nil), replacement...)
	targetItems := structuralSequenceItemLines(target)
	replacementItems := structuralSequenceItemLines(out)
	for index := 0; index < len(targetItems) && index < len(replacementItems); index++ {
		targetLine, replacementLine := targetItems[index], replacementItems[index]
		_, comment := splitRawInlineComment(structuralSequenceLine(target[targetLine]))
		if comment != "" {
			out[replacementLine] = replaceRawScalar(structuralSequenceLine(target[targetLine]), out[replacementLine])
		}
	}
	return out
}

// structuralSequenceItemLines pairs top-level sequence items by structure,
// rather than raw line number. This keeps target annotations when a commented
// template item expands to a mapping or local items have different line counts.
func structuralSequenceItemLines(lines []string) []int {
	itemIndent := -1
	var indexes []int
	for index, line := range lines {
		plain := structuralSequenceLine(line)
		indent := leadingRawSpaces(plain)
		if !strings.HasPrefix(strings.TrimSpace(plain), "-") {
			continue
		}
		if itemIndent < 0 {
			itemIndent = indent
		}
		if indent == itemIndent {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func structuralSequenceLine(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	if strings.HasPrefix(trimmed, "#") {
		return uncommentStructuralLine(line)
	}
	return line
}

func addRawReplacement(replacements map[int]rawReplacement, span rawYAMLSpan, lines []string) bool {
	if existing, ok := replacements[span.start]; ok {
		if span.start == span.end {
			// An insertion at a replaced sibling boundary belongs before that
			// sibling. This is what keeps an inserted child inside its newly
			// activated mapping instead of after the next template child when the
			// local mapping orders that sibling first.
			if existing.start != existing.end {
				existing.lines = append(append([]string(nil), lines...), existing.lines...)
			} else {
				existing.lines = append(existing.lines, lines...)
			}
			replacements[span.start] = existing
			return true
		}
		if existing.start == existing.end {
			lines = append(append([]string(nil), existing.lines...), lines...)
			delete(replacements, span.start)
		} else {
			return false
		}
	}
	for _, existing := range replacements {
		if span.start < existing.end && existing.start < span.end {
			return false
		}
	}
	replacements[span.start] = rawReplacement{start: span.start, end: span.end, lines: lines}
	return true
}

func renderedTargetIndent(path string, spans map[string]rawYAMLSpan) int {
	return spans[path].renderIndent
}

func entryPathRoot(path string) string {
	if separator := strings.Index(path, rawYAMLPathSeparator); separator >= 0 {
		return path[:separator]
	}
	return path
}

func commentedKeySpaces(line string) int {
	trimmed := strings.TrimLeft(line, " ")
	if !strings.HasPrefix(trimmed, "#") {
		return 0
	}
	rest := trimmed[1:]
	return len(rest) - len(strings.TrimLeft(rest, " "))
}

func reindentRawLines(lines []string, targetIndent int) []string {
	if len(lines) == 0 {
		return nil
	}
	sourcePhysicalIndent := leadingRawSpaces(lines[0])
	sourceVirtualIndent := rawLineVirtualIndent(lines[0])
	out := make([]string, len(lines))
	for index, line := range lines {
		if strings.TrimSpace(line) == "" {
			out[index] = line
			continue
		}
		physicalIndent := leadingRawSpaces(line)
		rest := line[physicalIndent:]
		if strings.HasPrefix(rest, "#") {
			// Structural commented YAML may encode its indentation after '#'
			// (for example '#        name:'). Rebase that virtual indentation
			// independently from the marker's physical column; never slice the
			// marker merely because the source header was indented.
			markerIndent := max(0, targetIndent+physicalIndent-sourcePhysicalIndent)
			if _, _, _, isKey := rawYAMLKey(line); isKey {
				virtualIndent := targetIndent + rawLineVirtualIndent(line) - sourceVirtualIndent
				keyText := strings.TrimLeft(rest[1:], " ")
				out[index] = strings.Repeat(" ", markerIndent) + "#" + strings.Repeat(" ", max(0, virtualIndent-markerIndent)) + keyText
				continue
			}
			out[index] = strings.Repeat(" ", markerIndent) + rest
			continue
		}
		indent := targetIndent + physicalIndent - sourcePhysicalIndent
		out[index] = strings.Repeat(" ", max(0, indent)) + rest
	}
	return out
}

func leadingRawSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func rawLineVirtualIndent(line string) int {
	if indent, _, commented, ok := rawYAMLKey(line); ok && commented {
		return indent
	}
	return leadingRawSpaces(line)
}

func replaceRawScalar(target, local string) string {
	_, targetComment := splitRawInlineComment(target)
	localValue, localComment := splitRawInlineComment(local)
	comment := mergeRawInlineComments(targetComment, localComment)
	if comment == "" {
		return strings.TrimRight(localValue, " ")
	}
	return strings.TrimRight(localValue, " ") + " " + comment
}

// mergeRawInlineComments keeps target annotations first, then adds distinct
// local annotations once. It only receives text after splitRawInlineComment
// has found the YAML comment marker, so semicolons in quoted scalar values are
// never considered separators here.
func mergeRawInlineComments(target, local string) string {
	seen := make(map[string]struct{})
	fragments := make([]rawInlineCommentFragment, 0)
	for _, comment := range []string{target, local} {
		for _, fragment := range rawInlineCommentFragments(comment) {
			if _, exists := seen[fragment.normalized]; exists {
				continue
			}
			seen[fragment.normalized] = struct{}{}
			fragments = append(fragments, fragment)
		}
	}
	if len(fragments) == 0 {
		return ""
	}
	// The first occurrence is target-owned when present, so retain its exact
	// marker spelling (for example `#sample` versus `# sample`). Later unique
	// fragments use the established delimiter while normalized bodies prevent
	// that delimiter from growing on subsequent renders.
	comment := fragments[0].lexical
	for _, fragment := range fragments[1:] {
		comment += "; # " + fragment.normalized
	}
	return comment
}

type rawInlineCommentFragment struct {
	normalized string
	lexical    string
}

func rawInlineCommentFragments(comment string) []rawInlineCommentFragment {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return nil
	}
	fragments := make([]rawInlineCommentFragment, 0, 1)
	for _, fragment := range strings.Split(comment, ";") {
		lexical := strings.TrimSpace(fragment)
		normalized := strings.Join(strings.Fields(strings.TrimSpace(strings.TrimPrefix(lexical, "#"))), " ")
		if normalized != "" {
			fragments = append(fragments, rawInlineCommentFragment{normalized: normalized, lexical: lexical})
		}
	}
	return fragments
}

func splitRawInlineComment(line string) (string, string) {
	quote := rune(0)
	for index, r := range line {
		switch r {
		case '\'', '"':
			if quote == 0 {
				quote = r
			} else if quote == r {
				quote = 0
			}
		case '#':
			if quote == 0 && (index == 0 || line[index-1] == ' ' || line[index-1] == '\t') {
				return line[:index], line[index:]
			}
		}
	}
	return line, ""
}

// renderedYAMLSemanticallyMatches is the final fail-closed guard for raw
// reconstruction. Presentation (comments, whitespace and scalar style) is deliberately
// ignored; YAML kinds, tags, keys and values must match the normal merge result.
func renderedYAMLSemanticallyMatches(rendered []byte, target, local *yaml.Node) bool {
	var actual yaml.Node
	if err := yaml.Unmarshal(rendered, &actual); err != nil {
		return false
	}
	expected := cloneYAMLNode(target)
	if expected == nil {
		return false
	}
	var unmapped []string
	mergeYAMLNodes(expected, local, "", &unmapped)
	return semanticYAMLNodeEqual(documentRoot(&actual), documentRoot(expected))
}

func semanticYAMLNodeEqual(left, right *yaml.Node) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.Kind != right.Kind || left.Tag != right.Tag || left.Value != right.Value || len(left.Content) != len(right.Content) {
		return false
	}
	if left.Kind == yaml.MappingNode {
		if len(left.Content)%2 != 0 || len(right.Content)%2 != 0 {
			return false
		}
		rightValues := make(map[string]*yaml.Node, len(right.Content)/2)
		for index := 0; index < len(right.Content); index += 2 {
			key := right.Content[index]
			// Duplicate keys are ambiguous for merge semantics, so raw output
			// must not be accepted on their behalf.
			identity := key.Tag + "\\x00" + key.Value
			if _, exists := rightValues[identity]; exists {
				return false
			}
			rightValues[identity] = right.Content[index+1]
		}
		for index := 0; index < len(left.Content); index += 2 {
			key := left.Content[index]
			identity := key.Tag + "\\x00" + key.Value
			value, exists := rightValues[identity]
			if !exists || !semanticYAMLNodeEqual(left.Content[index+1], value) {
				return false
			}
		}
		return true
	}
	for index := range left.Content {
		if !semanticYAMLNodeEqual(left.Content[index], right.Content[index]) {
			return false
		}
	}
	return true
}

func validateRenderedYAML(data []byte) bool {
	var node yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&node); err != nil && err != io.EOF {
		return false
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return false
	}
	return validateYAMLNode(&node, "rendered output") == nil
}
