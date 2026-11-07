package convert

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/hurzelpurzel/config-converter/internal/properties"
)

// YAMLToProperties converts a single-document YAML file to .properties data.
func YAMLToProperties(data []byte) ([]byte, error) {
	root, err := decodeYAML(data)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, nil
	}

	root, err = deref(root)
	if err != nil {
		return nil, err
	}
	if root.Kind == yaml.ScalarNode && root.Tag == "!!null" {
		return nil, nil
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: YAML root must be a mapping, got %s", root.Line, yamlKindName(root.Kind))
	}

	var entries []properties.Entry
	if err := flattenNode(root, "", nil, nil, 0, &entries); err != nil {
		return nil, err
	}
	return properties.Format(&properties.Document{Entries: entries}), nil
}

func decodeYAML(data []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root *yaml.Node
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid YAML: %w", err)
		}
		if len(doc.Content) == 0 {
			continue // empty document or comments only
		}
		if root != nil {
			return nil, fmt.Errorf("multiple YAML documents are not supported")
		}
		root = doc.Content[0]
	}
	return root, nil
}

type yamlPair struct {
	key   *yaml.Node
	value *yaml.Node
}

func flattenNode(n *yaml.Node, path string, lead, foot []string, depth int, out *[]properties.Entry) error {
	if depth > 500 {
		return fmt.Errorf("YAML nesting is too deep")
	}
	if n == nil {
		return fmt.Errorf("invalid YAML node")
	}

	if n.Kind == yaml.AliasNode {
		target, err := deref(n)
		if err != nil {
			return err
		}
		lead = combineComments(lead, splitComments(n.HeadComment)...)
		aliasFoot := combineComments(splitComments(n.LineComment), splitComments(n.FootComment)...)
		foot = combineComments(aliasFoot, foot...)
		return flattenNode(target, path, lead, foot, depth+1, out)
	}

	switch n.Kind {
	case yaml.ScalarNode:
		lead = combineComments(lead, splitComments(n.HeadComment)...)
		ownFoot := combineComments(splitComments(n.LineComment), splitComments(n.FootComment)...)
		foot = combineComments(ownFoot, foot...)
		*out = append(*out, properties.Entry{
			Key:         path,
			Value:       scalarValue(n),
			Comment:     strings.Join(lead, "\n"),
			FootComment: strings.Join(foot, "\n"),
		})
		return nil

	case yaml.MappingNode:
		lead = combineComments(lead, splitComments(n.HeadComment)...)
		start := len(*out)
		pairs, err := effectivePairs(n, depth+1, make(map[*yaml.Node]bool))
		if err != nil {
			return err
		}
		for i, pair := range pairs {
			key, err := yamlKeyString(pair.key)
			if err != nil {
				return err
			}
			if key == "" {
				return fmt.Errorf("line %d: empty YAML mapping key", pair.key.Line)
			}

			keyLead := splitComments(pair.key.HeadComment)
			keyLead = combineComments(keyLead, splitComments(pair.key.LineComment)...)
			if i == 0 {
				keyLead = combineComments(lead, keyLead...)
			}
			keyFoot := splitComments(pair.key.FootComment)
			if err := flattenNode(pair.value, joinPath(path, key), keyLead, keyFoot, depth+1, out); err != nil {
				return err
			}
		}
		ownFoot := combineComments(splitComments(n.LineComment), splitComments(n.FootComment)...)
		appendFoot(out, start, combineComments(ownFoot, foot...))
		return nil

	case yaml.SequenceNode:
		lead = combineComments(lead, splitComments(n.HeadComment)...)
		start := len(*out)
		for i, item := range n.Content {
			var itemLead []string
			if i == 0 {
				itemLead = lead
			}
			if err := flattenNode(item, indexPath(path, i), itemLead, nil, depth+1, out); err != nil {
				return err
			}
		}
		ownFoot := combineComments(splitComments(n.LineComment), splitComments(n.FootComment)...)
		appendFoot(out, start, combineComments(ownFoot, foot...))
		return nil

	default:
		return fmt.Errorf("line %d: unsupported YAML node type %s", n.Line, yamlKindName(n.Kind))
	}
}

// effectivePairs expands YAML merge keys. Explicit keys in a mapping take
// precedence over merged keys; for a sequence of merged mappings, earlier
// mappings take precedence over later ones.
func effectivePairs(m *yaml.Node, depth int, stack map[*yaml.Node]bool) ([]yamlPair, error) {
	if depth > 500 {
		return nil, fmt.Errorf("YAML nesting is too deep")
	}
	if stack[m] {
		return nil, fmt.Errorf("line %d: recursive YAML merge", m.Line)
	}
	stack[m] = true
	defer delete(stack, m)

	var explicit []yamlPair
	explicitKeys := make(map[string]bool)
	for i := 0; i+1 < len(m.Content); i += 2 {
		key := m.Content[i]
		if isMergeKey(key) {
			continue
		}
		keyStr, err := yamlKeyString(key)
		if err != nil {
			return nil, err
		}
		explicitKeys[keyStr] = true
		explicit = append(explicit, yamlPair{key: key, value: m.Content[i+1]})
	}

	var merged []yamlPair
	mergedKeys := make(map[string]bool)
	for i := 0; i+1 < len(m.Content); i += 2 {
		key := m.Content[i]
		if !isMergeKey(key) {
			continue
		}
		mappings, err := mergeMappings(m.Content[i+1])
		if err != nil {
			return nil, err
		}
		for _, mapping := range mappings {
			pairs, err := effectivePairs(mapping, depth+1, stack)
			if err != nil {
				return nil, err
			}
			for _, pair := range pairs {
				keyStr, err := yamlKeyString(pair.key)
				if err != nil {
					return nil, err
				}
				if explicitKeys[keyStr] || mergedKeys[keyStr] {
					continue
				}
				mergedKeys[keyStr] = true
				merged = append(merged, pair)
			}
		}
	}

	return append(merged, explicit...), nil
}

func mergeMappings(n *yaml.Node) ([]*yaml.Node, error) {
	n, err := deref(n)
	if err != nil {
		return nil, err
	}
	switch n.Kind {
	case yaml.MappingNode:
		return []*yaml.Node{n}, nil
	case yaml.SequenceNode:
		var out []*yaml.Node
		for _, item := range n.Content {
			item, err := deref(item)
			if err != nil {
				return nil, err
			}
			if item.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("line %d: YAML merge value must be a mapping or a sequence of mappings", item.Line)
			}
			out = append(out, item)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("line %d: YAML merge value must be a mapping or a sequence of mappings", n.Line)
	}
}

func isMergeKey(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	return n.Tag == "!!merge" || (n.Kind == yaml.ScalarNode && n.Value == "<<")
}

func deref(n *yaml.Node) (*yaml.Node, error) {
	seen := make(map[*yaml.Node]bool)
	for n != nil && n.Kind == yaml.AliasNode {
		if seen[n] {
			return nil, fmt.Errorf("line %d: recursive YAML alias", n.Line)
		}
		seen[n] = true
		n = n.Alias
	}
	if n == nil {
		return nil, fmt.Errorf("invalid YAML alias")
	}
	return n, nil
}

func yamlKeyString(n *yaml.Node) (string, error) {
	n, err := deref(n)
	if err != nil {
		return "", err
	}
	if n.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("line %d: YAML mapping key must be a scalar", n.Line)
	}
	if n.Tag == "!!null" {
		return "", fmt.Errorf("line %d: YAML mapping key must not be null", n.Line)
	}
	return n.Value, nil
}

func scalarValue(n *yaml.Node) string {
	if n.Tag == "!!null" {
		return ""
	}
	return n.Value
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func indexPath(prefix string, index int) string {
	return fmt.Sprintf("%s[%d]", prefix, index)
}

func splitComments(comment string) []string {
	if comment == "" {
		return nil
	}
	lines := strings.Split(comment, "\n")
	for i, line := range lines {
		line = strings.TrimLeft(line, " \t")
		if strings.HasPrefix(line, "#") {
			line = line[1:]
			if strings.HasPrefix(line, " ") {
				line = line[1:]
			}
		}
		lines[i] = line
	}
	return lines
}

func combineComments(base []string, more ...string) []string {
	if len(more) == 0 {
		return base
	}
	out := make([]string, 0, len(base)+len(more))
	out = append(out, base...)
	out = append(out, more...)
	return out
}

func appendFoot(out *[]properties.Entry, start int, comments []string) {
	if len(comments) == 0 || len(*out) <= start {
		return
	}
	last := &(*out)[len(*out)-1]
	extra := strings.Join(comments, "\n")
	if last.FootComment == "" {
		last.FootComment = extra
	} else if extra != "" {
		last.FootComment += "\n" + extra
	}
}

func yamlKindName(kind yaml.Kind) string {
	switch kind {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "array"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	default:
		return fmt.Sprintf("node kind %d", kind)
	}
}
