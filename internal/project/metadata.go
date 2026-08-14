package project

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	tomlunstable "github.com/pelletier/go-toml/v2/unstable"
	"github.com/tidwall/sjson"
	yamlv3 "go.yaml.in/yaml/v3"
)

// metadataUpdate describes one precise mutation in a structured project file.
// Selectors are format-specific paths: JSON/YAML use dotted keys, XML uses
// slash-separated element names with optional zero-based sibling indexes, and
// TOML uses a table name plus a field name.
type metadataUpdate struct {
	Format        string
	Path          string
	Selector      string
	Attribute     string
	Value         string
	IgnoreMissing bool
}

func updateMetadataFile(workdir string, update metadataUpdate) error {
	path, err := safeProjectPath(workdir, update.Path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) && update.IgnoreMissing {
		return nil
	}
	if err != nil {
		return err
	}

	updated, err := transformMetadata(data, update)
	if err != nil {
		return err
	}
	if bytes.Equal(updated, data) {
		return nil
	}
	return os.WriteFile(path, updated, fileMode(path))
}

// transformMetadata applies one scoped metadata edit without touching disk.
// Version previews use this same transformer as the live operation.
func transformMetadata(data []byte, update metadataUpdate) ([]byte, error) {
	switch strings.ToLower(update.Format) {
	case "json":
		return updateJSON(data, update.Selector, update.Value)
	case "yaml", "yml":
		return updateYAML(data, update.Selector, update.Value)
	case "xml":
		return updateXML(data, update.Selector, update.Attribute, update.Value)
	case "toml":
		return updateTOML(data, update.Selector, []tomlField{{name: update.Attribute, value: update.Value}})
	default:
		return nil, fmt.Errorf("unsupported metadata format %q", update.Format)
	}
}

func updateJSON(data []byte, selector, value string) ([]byte, error) {
	if strings.TrimSpace(selector) == "" {
		return nil, fmt.Errorf("JSON metadata selector is empty")
	}
	return sjson.SetBytes(data, selector, value)
}

func updateYAML(data []byte, selector, value string) ([]byte, error) {
	parts := splitMetadataPath(selector, ".")
	if len(parts) == 0 {
		return nil, fmt.Errorf("YAML metadata selector is empty")
	}

	var document yamlv3.Node
	decoder := yamlv3.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	target, err := ensureYAMLPath(&document, parts)
	if err != nil {
		return nil, err
	}
	target.Value = value
	target.Tag = "!!str"

	var output bytes.Buffer
	encoder := yamlv3.NewEncoder(&output)
	if err := encoder.Encode(&document); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func splitMetadataPath(path, separator string) []string {
	parts := strings.Split(strings.Trim(path, separator), separator)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func ensureYAMLPath(document *yamlv3.Node, parts []string) (*yamlv3.Node, error) {
	if document.Kind != yamlv3.DocumentNode || len(document.Content) == 0 {
		return nil, fmt.Errorf("YAML document has no root node")
	}
	node := document.Content[0]
	for i, part := range parts {
		if node.Kind != yamlv3.MappingNode {
			return nil, fmt.Errorf("YAML path %q crosses a non-mapping node", strings.Join(parts[:i], "."))
		}
		var value *yamlv3.Node
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value == part {
				value = node.Content[index+1]
				break
			}
		}
		if value == nil {
			key := &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: "!!str", Value: part}
			value = &yamlv3.Node{Kind: yamlv3.MappingNode, Tag: "!!map"}
			node.Content = append(node.Content, key, value)
		}
		if i == len(parts)-1 {
			if value.Kind == yamlv3.AliasNode {
				return nil, fmt.Errorf("YAML path %q points to an alias", strings.Join(parts, "."))
			}
			return value, nil
		}
		node = value
	}
	return nil, fmt.Errorf("YAML path %q is empty", strings.Join(parts, "."))
}

type xmlPathPart struct {
	name     string
	index    int
	hasIndex bool
}

type xmlFrame struct {
	name            string
	index           int
	textValue       string
	textTarget      bool
	textChanged     bool
	hasElementChild bool
}

func updateXML(data []byte, selector, attribute, value string) ([]byte, error) {
	path, err := parseXMLPath(selector)
	if err != nil {
		return nil, err
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
	var output bytes.Buffer
	encoder := xml.NewEncoder(&output)
	stack := make([]xmlFrame, 0, len(path))
	childIndexes := make([]map[string]int, 0, len(path))
	found := false
	for {
		// RawToken keeps default namespace declarations as written. Token would
		// expand namespace URLs onto every element when the stream is encoded
		// again, producing a noisy and potentially invalid Maven POM.
		token, tokenErr := decoder.RawToken()
		if tokenErr != nil {
			if tokenErr == io.EOF {
				break
			}
			return nil, tokenErr
		}
		switch current := token.(type) {
		case xml.StartElement:
			if len(stack) > 0 {
				stack[len(stack)-1].hasElementChild = true
			}
			if len(childIndexes) == 0 {
				childIndexes = append(childIndexes, map[string]int{})
			}
			parent := childIndexes[len(childIndexes)-1]
			index := parent[current.Name.Local]
			parent[current.Name.Local] = index + 1
			frame := xmlFrame{name: current.Name.Local, index: index}
			candidate := append(append([]xmlFrame(nil), stack...), frame)
			if xmlPathMatches(candidate, path) {
				found = true
				if attribute != "" {
					setXMLAttribute(&current, attribute, value)
				}
				if attribute == "" {
					frame.textTarget = true
					frame.textValue = value
				}
			}
			if err := encoder.EncodeToken(current); err != nil {
				return nil, err
			}
			stack = append(stack, frame)
			childIndexes = append(childIndexes, map[string]int{})
		case xml.CharData:
			if len(stack) > 0 {
				frame := &stack[len(stack)-1]
				text := string(current)
				if frame.textTarget && !frame.hasElementChild {
					if frame.textChanged && strings.TrimSpace(text) != "" {
						continue
					}
					if !frame.textChanged && strings.TrimSpace(text) != "" {
						current = xml.CharData([]byte(preserveXMLWhitespace(text, frame.textValue)))
						frame.textChanged = true
					}
				}
			}
			if err := encoder.EncodeToken(current); err != nil {
				return nil, err
			}
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("XML end element %q has no start element", current.Name.Local)
			}
			frame := &stack[len(stack)-1]
			if frame.name != current.Name.Local {
				return nil, fmt.Errorf("XML end element %q does not match %q", current.Name.Local, frame.name)
			}
			if frame.textTarget && !frame.textChanged && !frame.hasElementChild {
				if err := encoder.EncodeToken(xml.CharData([]byte(frame.textValue))); err != nil {
					return nil, err
				}
			}
			if err := encoder.EncodeToken(current); err != nil {
				return nil, err
			}
			stack = stack[:len(stack)-1]
			childIndexes = childIndexes[:len(childIndexes)-1]
		default:
			if err := encoder.EncodeToken(token); err != nil {
				return nil, err
			}
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	if !found {
		return data, nil
	}
	return output.Bytes(), nil
}

func parseXMLPath(selector string) ([]xmlPathPart, error) {
	parts := splitMetadataPath(selector, "/")
	if len(parts) == 0 {
		return nil, fmt.Errorf("XML metadata selector is empty")
	}
	result := make([]xmlPathPart, 0, len(parts))
	for _, part := range parts {
		pathPart := xmlPathPart{name: part}
		if open := strings.IndexByte(part, '['); open >= 0 {
			if !strings.HasSuffix(part, "]") || open == 0 {
				return nil, fmt.Errorf("invalid XML metadata selector %q", selector)
			}
			index, err := strconv.Atoi(part[open+1 : len(part)-1])
			if err != nil || index < 0 {
				return nil, fmt.Errorf("invalid XML metadata selector %q", selector)
			}
			pathPart.name = part[:open]
			pathPart.index = index
			pathPart.hasIndex = true
		}
		result = append(result, pathPart)
	}
	return result, nil
}

func xmlPathMatches(stack []xmlFrame, path []xmlPathPart) bool {
	if len(stack) != len(path) {
		return false
	}
	for index, part := range path {
		if stack[index].name != part.name || (part.hasIndex && stack[index].index != part.index) {
			return false
		}
	}
	return true
}

func setXMLAttribute(element *xml.StartElement, name, value string) {
	for index := range element.Attr {
		if element.Attr[index].Name.Local == name {
			element.Attr[index].Value = value
			return
		}
	}
	element.Attr = append(element.Attr, xml.Attr{Name: xml.Name{Local: name}, Value: value})
}

func preserveXMLWhitespace(original, replacement string) string {
	start := 0
	for start < len(original) && (original[start] == ' ' || original[start] == '\t' || original[start] == '\r' || original[start] == '\n') {
		start++
	}
	end := len(original)
	for end > start && (original[end-1] == ' ' || original[end-1] == '\t' || original[end-1] == '\r' || original[end-1] == '\n') {
		end--
	}
	return original[:start] + replacement + original[end:]
}

type tomlField struct {
	name  string
	value string
}

type tomlEdit struct {
	offset int
	length int
	value  []byte
}

// updateTOMLFields uses go-toml's parser AST to replace only values in the
// requested table. It preserves the surrounding document, comments, ordering,
// and formatting instead of reconstructing the entire TOML file.
func updateTOMLFields(workdir, relative, section string, fields []tomlField) error {
	path, err := safeProjectPath(workdir, relative)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, err := updateTOML(data, section, fields)
	if err != nil {
		return err
	}
	if bytes.Equal(updated, data) {
		return nil
	}
	return os.WriteFile(path, updated, fileMode(path))
}

func updateTOML(data []byte, section string, fields []tomlField) ([]byte, error) {
	wantedSection := strings.Trim(strings.TrimSpace(section), "[]")
	if wantedSection == "" {
		return nil, fmt.Errorf("TOML section is empty")
	}

	parser := tomlunstable.Parser{KeepComments: true}
	parser.Reset(data)
	currentSection := ""
	selected := false
	active := false
	sectionEnd := len(data)
	var edits []tomlEdit
	found := make(map[string]bool, len(fields))
	for parser.NextExpression() {
		expression := parser.Expression()
		switch expression.Kind {
		case tomlunstable.Table, tomlunstable.ArrayTable:
			key := tomlKey(expression)
			currentSection = key
			if active {
				keyIterator := expression.Key()
				keyIterator.Next()
				sectionEnd = trimTOMLBlankLines(data, lineStart(data, int(keyIterator.Node().Raw.Offset)))
				active = false
			}
			if !selected && key == wantedSection {
				selected = true
				active = true
			}
		case tomlunstable.KeyValue:
			if !active || currentSection != wantedSection {
				continue
			}
			key := tomlKey(expression)
			if len(key) == 0 {
				continue
			}
			for _, field := range fields {
				if key != field.name || found[field.name] {
					continue
				}
				value := expression.Value()
				if value == nil {
					return nil, fmt.Errorf("TOML field %s.%s has no value", wantedSection, field.name)
				}
				edits = append(edits, tomlEdit{offset: int(value.Raw.Offset), length: int(value.Raw.Length), value: []byte(strconv.Quote(field.value))})
				found[field.name] = true
			}
		}
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	if !selected {
		return nil, fmt.Errorf("TOML section %s not found", section)
	}

	missingFields := make([]tomlField, 0, len(fields))
	for _, field := range fields {
		if !found[field.name] {
			missingFields = append(missingFields, field)
		}
	}
	missing := make([]byte, 0)
	if len(missingFields) > 0 {
		if sectionEnd > 0 && data[sectionEnd-1] != '\n' {
			missing = append(missing, '\n')
		}
	}
	for _, field := range missingFields {
		missing = append(missing, field.name...)
		missing = append(missing, " = "...)
		missing = append(missing, strconv.Quote(field.value)...)
		missing = append(missing, '\n')
	}
	if len(missing) > 0 {
		edits = append(edits, tomlEdit{offset: sectionEnd, value: missing})
	}
	return applyTOMLEdits(data, edits), nil
}

func tomlKey(expression *tomlunstable.Node) string {
	parts := []string{}
	iterator := expression.Key()
	for iterator.Next() {
		parts = append(parts, string(iterator.Node().Data))
	}
	return strings.Join(parts, ".")
}

func lineStart(data []byte, offset int) int {
	if offset < 0 {
		return 0
	}
	if offset > len(data) {
		offset = len(data)
	}
	if index := bytes.LastIndexByte(data[:offset], '\n'); index >= 0 {
		return index + 1
	}
	return 0
}

func trimTOMLBlankLines(data []byte, offset int) int {
	for offset > 0 {
		previousLineStart := lineStart(data, offset-1)
		if strings.TrimSpace(string(data[previousLineStart:offset])) != "" {
			break
		}
		offset = previousLineStart
	}
	return offset
}

func applyTOMLEdits(data []byte, edits []tomlEdit) []byte {
	for left := 0; left < len(edits); left++ {
		for right := left + 1; right < len(edits); right++ {
			if edits[right].offset > edits[left].offset {
				edits[left], edits[right] = edits[right], edits[left]
			}
		}
	}
	updated := append([]byte(nil), data...)
	for _, edit := range edits {
		end := edit.offset + edit.length
		if edit.offset < 0 || end > len(updated) {
			continue
		}
		updated = append(updated[:edit.offset:edit.offset], append(append([]byte(nil), edit.value...), updated[end:]...)...)
	}
	return updated
}
