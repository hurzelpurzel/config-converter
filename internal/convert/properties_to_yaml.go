// Package convert translates between Spring-style .properties files and YAML.
package convert

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/hurzelpurzel/config-converter/internal/properties"
)

type segment struct {
	name    string
	index   int
	isIndex bool
}

type nodeKind int

const (
	pendingNode nodeKind = iota
	scalarNode
	mapNode
	seqNode
)

type treeNode struct {
	kind nodeKind

	value       string
	comment     string
	footComment string
	line        int

	keys     []string
	children map[string]*treeNode
	items    []*treeNode
}

func newMapNode() *treeNode {
	return &treeNode{kind: mapNode, children: make(map[string]*treeNode)}
}

// PropertiesToYAML converts Java .properties data to YAML.
func PropertiesToYAML(data []byte) ([]byte, error) {
	doc, err := properties.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if len(doc.Entries) == 0 {
		return commentOnlyYAML(doc.Header, doc.Footer), nil
	}

	root, err := buildTree(doc)
	if err != nil {
		return nil, err
	}

	yamlRoot := treeToYAML(root)
	yamlRoot.HeadComment = doc.Header
	if doc.Footer != "" {
		if len(yamlRoot.Content) >= 2 {
			// A foot comment on the root mapping would add a blank line before
			// it. Put it on the last top-level key instead.
			yamlRoot.Content[len(yamlRoot.Content)-2].FootComment = doc.Footer
		} else {
			yamlRoot.FootComment = doc.Footer
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlRoot); err != nil {
		return nil, fmt.Errorf("encode YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode YAML: %w", err)
	}
	return buf.Bytes(), nil
}

func buildTree(doc *properties.Document) (*treeNode, error) {
	root := newMapNode()
	for _, entry := range doc.Entries {
		segs, err := splitKey(entry.Key)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid property key %q: %w", entry.Line, entry.Key, err)
		}

		cur := root
		for i, seg := range segs {
			child, err := childFor(cur, seg)
			if err != nil {
				return nil, fmt.Errorf("line %d: property %q: %w", entry.Line, entry.Key, err)
			}

			if i == len(segs)-1 {
				if child.kind != pendingNode && child.kind != scalarNode {
					return nil, fmt.Errorf("line %d: property %q conflicts with a %s defined at line %d",
						entry.Line, entry.Key, kindName(child.kind), child.line)
				}
				child.kind = scalarNode
				child.value = entry.Value
				child.comment = entry.Comment
				child.footComment = entry.FootComment
				child.line = entry.Line
				continue
			}

			want := mapNode
			if segs[i+1].isIndex {
				want = seqNode
			}
			if child.kind == pendingNode {
				child.kind = want
				child.line = entry.Line
				if want == mapNode {
					child.children = make(map[string]*treeNode)
				}
			} else if child.kind != want {
				return nil, fmt.Errorf("line %d: property %q conflicts with a %s defined at line %d",
					entry.Line, entry.Key, kindName(child.kind), child.line)
			}
			cur = child
		}
	}
	return root, nil
}

func childFor(cur *treeNode, seg segment) (*treeNode, error) {
	if seg.isIndex {
		if cur.kind != seqNode {
			return nil, fmt.Errorf("array index [%d] used outside of an array", seg.index)
		}
		for len(cur.items) <= seg.index {
			cur.items = append(cur.items, &treeNode{kind: pendingNode})
		}
		if cur.items[seg.index] == nil {
			cur.items[seg.index] = &treeNode{kind: pendingNode}
		}
		return cur.items[seg.index], nil
	}

	if cur.kind != mapNode {
		return nil, fmt.Errorf("key %q used outside of a mapping", seg.name)
	}
	if cur.children == nil {
		cur.children = make(map[string]*treeNode)
	}
	if child, ok := cur.children[seg.name]; ok {
		return child, nil
	}
	child := &treeNode{kind: pendingNode}
	cur.children[seg.name] = child
	cur.keys = append(cur.keys, seg.name)
	return child, nil
}

func splitKey(key string) ([]segment, error) {
	var segs []segment
	var part strings.Builder

	flush := func() error {
		if part.Len() == 0 {
			return fmt.Errorf("empty path segment")
		}
		segs = append(segs, segment{name: part.String()})
		part.Reset()
		return nil
	}

	for i := 0; i < len(key); {
		switch key[i] {
		case '.':
			if err := flush(); err != nil {
				return nil, err
			}
			i++
			if i == len(key) {
				return nil, fmt.Errorf("trailing '.'")
			}
		case '[':
			if part.Len() > 0 {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			end := strings.IndexByte(key[i+1:], ']')
			if end < 0 {
				return nil, fmt.Errorf("unclosed '['")
			}
			end += i + 1
			content := key[i+1 : end]
			if content == "" {
				return nil, fmt.Errorf("empty '[]'")
			}
			if isDigits(content) {
				idx, err := strconv.Atoi(content)
				if err != nil {
					return nil, fmt.Errorf("invalid array index %q", content)
				}
				segs = append(segs, segment{index: idx, isIndex: true})
			} else {
				segs = append(segs, segment{name: content})
			}
			i = end + 1
			if i < len(key) {
				switch key[i] {
				case '.':
					i++
					if i == len(key) {
						return nil, fmt.Errorf("trailing '.'")
					}
				case '[':
					// another index follows
				default:
					return nil, fmt.Errorf("unexpected %q after ']'", key[i])
				}
			}
		default:
			part.WriteByte(key[i])
			i++
		}
	}
	if part.Len() > 0 {
		if err := flush(); err != nil {
			return nil, err
		}
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("empty key")
	}
	if segs[0].isIndex {
		return nil, fmt.Errorf("root-level array indexes are not supported")
	}
	return segs, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func treeToYAML(n *treeNode) *yaml.Node {
	switch n.kind {
	case mapNode:
		out := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, key := range n.keys {
			child := n.children[key]
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
			valueNode := treeToYAML(child)
			if child.kind == scalarNode {
				// yaml.v3 places a HeadComment on a mapping value after the
				// pair; comments have to sit on the key to stay above it.
				keyNode.HeadComment = valueNode.HeadComment
				keyNode.FootComment = valueNode.FootComment
				valueNode.HeadComment = ""
				valueNode.FootComment = ""
			}
			out.Content = append(out.Content, keyNode, valueNode)
		}
		return out
	case seqNode:
		out := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range n.items {
			if item == nil || item.kind == pendingNode {
				out.Content = append(out.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"})
				continue
			}
			out.Content = append(out.Content, treeToYAML(item))
		}
		return out
	case scalarNode:
		return &yaml.Node{
			Kind:        yaml.ScalarNode,
			Tag:         "!!str",
			Value:       n.value,
			HeadComment: n.comment,
			FootComment: n.footComment,
		}
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	}
}

func kindName(k nodeKind) string {
	switch k {
	case mapNode:
		return "mapping"
	case seqNode:
		return "array"
	case scalarNode:
		return "value"
	case pendingNode:
		return "unset value"
	default:
		return "unknown value"
	}
}

func commentOnlyYAML(parts ...string) []byte {
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		for _, line := range strings.Split(part, "\n") {
			if line == "" {
				b.WriteString("#\n")
				continue
			}
			b.WriteString("# ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return []byte(b.String())
}
