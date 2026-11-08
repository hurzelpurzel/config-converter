// Package schema generates JSON Schemas from YAML documents and validates YAML
// values against JSON Schemas.
package schema

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// Draft is the JSON Schema dialect used for generated schemas.
const Draft = "https://json-schema.org/draft/2020-12/schema"

// GenerateYAML infers a JSON Schema from a single YAML document and returns it
// as indented JSON.
//
// The generated schema mirrors the shape of the document: every observed
// mapping key is required and unknown properties are rejected. For arrays, the
// item schemas of all elements are merged, so properties that are only present
// on some objects become optional. Comments immediately above a mapping key or
// sequence item are used as its description.
func GenerateYAML(data []byte) ([]byte, error) {
	root, err := decodeYAMLDocument(data)
	if err != nil {
		return nil, err
	}

	generated, err := inferNode(root)
	if err != nil {
		return nil, err
	}
	generated["$schema"] = Draft

	out, err := json.MarshalIndent(generated, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode JSON Schema: %w", err)
	}
	return append(out, '\n'), nil
}

// ValidateYAML validates a single YAML document against a JSON Schema. The
// schema may be written as JSON or YAML.
//
// A nil error means the document is valid. Invalid documents, malformed YAML
// and malformed schemas are reported as errors.
func ValidateYAML(data, schemaData []byte) error {
	schemaValue, err := decodeSchema(schemaData)
	if err != nil {
		return err
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("memory://schema.json", schemaValue); err != nil {
		return fmt.Errorf("invalid JSON Schema: %w", err)
	}
	compiled, err := compiler.Compile("memory://schema.json")
	if err != nil {
		return fmt.Errorf("invalid JSON Schema: %w", err)
	}

	value, err := decodeYAML(data)
	if err != nil {
		return err
	}
	if err := compiled.Validate(value); err != nil {
		return fmt.Errorf("YAML does not match schema: %w", err)
	}
	return nil
}

// decodeYAML decodes exactly one YAML document into JSON-compatible values.
// Empty input is treated as null.
func decodeYAML(data []byte) (any, error) {
	root, err := decodeYAMLDocument(data)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, nil
	}
	var value any
	if err := root.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	return normalize(value), nil
}

// decodeYAMLDocument returns the root node of the single YAML document in data.
// Empty input returns a nil node.
func decodeYAMLDocument(data []byte) (*yaml.Node, error) {
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
			continue
		}
		if root != nil {
			return nil, fmt.Errorf("multiple YAML documents are not supported")
		}
		root = doc.Content[0]
	}
	return root, nil
}

// decodeSchema accepts JSON first and falls back to YAML, which is convenient
// because JSON is a subset of YAML but YAML schemas use less punctuation.
func decodeSchema(data []byte) (any, error) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&value); err == nil {
		var extra any
		if err := dec.Decode(&extra); errors.Is(err, io.EOF) {
			return value, nil
		}
	}

	value, err := decodeYAML(data)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON Schema: %w", err)
	}
	return value, nil
}

// normalize converts YAML-specific values into JSON-compatible values.
func normalize(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = normalize(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[fmt.Sprint(key)] = normalize(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalize(item)
		}
		return out
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case []byte:
		return base64.StdEncoding.EncodeToString(v)
	default:
		return value
	}
}

// inferNode builds a JSON Schema for a YAML node. Comments immediately before
// a mapping key or sequence item become the description of its schema.
func inferNode(node *yaml.Node) (map[string]any, error) {
	if node == nil {
		return map[string]any{"type": "null"}, nil
	}

	resolved, err := derefNode(node)
	if err != nil {
		return nil, err
	}

	var generated map[string]any
	switch resolved.Kind {
	case yaml.ScalarNode:
		var value any
		if err := resolved.Decode(&value); err != nil {
			return nil, fmt.Errorf("invalid YAML: %w", err)
		}
		generated = infer(normalize(value))

	case yaml.MappingNode:
		generated, err = inferMapping(resolved)
		if err != nil {
			return nil, err
		}

	case yaml.SequenceNode:
		generated, err = inferSequence(resolved)
		if err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("line %d: unsupported YAML node type %s", resolved.Line, yamlKindName(resolved.Kind))
	}

	setDescription(generated, commentText(resolved.HeadComment))
	return generated, nil
}

func inferMapping(node *yaml.Node) (map[string]any, error) {
	pairs, err := effectivePairs(node, 0, make(map[*yaml.Node]bool))
	if err != nil {
		return nil, err
	}

	properties := make(map[string]any, len(pairs))
	required := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		key, err := yamlKeyString(pair.key)
		if err != nil {
			return nil, err
		}
		valueSchema, err := inferNode(pair.value)
		if err != nil {
			return nil, err
		}
		if description := commentText(pair.key.HeadComment); description != "" {
			setDescription(valueSchema, description)
		}
		properties[key] = valueSchema
		required = append(required, key)
	}
	sort.Strings(required)

	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}, nil
}

func inferSequence(node *yaml.Node) (map[string]any, error) {
	if len(node.Content) == 0 {
		return map[string]any{"type": "array"}, nil
	}

	var item map[string]any
	for _, child := range node.Content {
		schema, err := inferNode(child)
		if err != nil {
			return nil, err
		}
		if item == nil {
			item = schema
		} else {
			item = mergeSchemas(item, schema)
		}
	}
	return map[string]any{
		"type":  "array",
		"items": item,
	}, nil
}

// commentText strips YAML comment markers and surrounding whitespace.
func commentText(comment string) string {
	if comment == "" {
		return ""
	}
	lines := strings.Split(comment, "\n")
	for i, line := range lines {
		line = strings.TrimLeft(line, " \t")
		if strings.HasPrefix(line, "#") {
			line = strings.TrimPrefix(line[1:], " ")
		}
		lines[i] = line
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func setDescription(schema map[string]any, description string) {
	if description != "" {
		schema["description"] = description
	}
}

// infer builds a JSON Schema for a decoded scalar value.
func infer(value any) map[string]any {
	switch v := value.(type) {
	case nil:
		return map[string]any{"type": "null"}
	case bool:
		return map[string]any{"type": "boolean"}
	case string:
		return map[string]any{"type": "string"}
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return map[string]any{"type": "integer"}
	case float32, float64:
		return map[string]any{"type": "number"}
	case []any:
		return inferArray(v)
	case map[string]any:
		return inferObject(v)
	default:
		return map[string]any{"type": "string"}
	}
}

func inferObject(value map[string]any) map[string]any {
	properties := make(map[string]any, len(value))
	required := make([]string, 0, len(value))
	for key, item := range value {
		properties[key] = infer(item)
		required = append(required, key)
	}
	sort.Strings(required)

	out := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
	return out
}

func inferArray(value []any) map[string]any {
	if len(value) == 0 {
		return map[string]any{"type": "array"}
	}

	item := infer(value[0])
	for _, element := range value[1:] {
		item = mergeSchemas(item, infer(element))
	}
	return map[string]any{
		"type":  "array",
		"items": item,
	}
}

// mergeSchemas combines two inferred schemas so that an array can accept
// elements with slightly different shapes. Descriptions are annotations that
// do not affect validation; the first available description is kept.
func mergeSchemas(a, b map[string]any) map[string]any {
	description := firstDescription(a, b)
	merged := mergeSchemasWithoutDescription(withoutDescription(a), withoutDescription(b))
	setDescription(merged, description)
	return merged
}

func mergeSchemasWithoutDescription(a, b map[string]any) map[string]any {
	if reflect.DeepEqual(a, b) {
		return a
	}

	aType, _ := a["type"].(string)
	bType, _ := b["type"].(string)
	if (aType == "integer" && bType == "number") || (aType == "number" && bType == "integer") {
		return map[string]any{"type": "number"}
	}
	if aType == "object" && bType == "object" {
		return mergeObjects(a, b)
	}
	if aType == "array" && bType == "array" {
		return mergeArrays(a, b)
	}

	return unionSchemas(a, b)
}

func firstDescription(a, b map[string]any) string {
	if description, ok := a["description"].(string); ok && description != "" {
		return description
	}
	if description, ok := b["description"].(string); ok && description != "" {
		return description
	}
	return ""
}

func withoutDescription(schema map[string]any) map[string]any {
	if _, ok := schema["description"]; !ok {
		return schema
	}
	out := make(map[string]any, len(schema))
	for key, value := range schema {
		if key != "description" {
			out[key] = value
		}
	}
	return out
}

func mergeObjects(a, b map[string]any) map[string]any {
	aProps, _ := a["properties"].(map[string]any)
	bProps, _ := b["properties"].(map[string]any)

	properties := make(map[string]any, len(aProps)+len(bProps))
	for key, item := range aProps {
		properties[key] = item
	}
	for key, item := range bProps {
		if existing, ok := properties[key]; ok {
			existingSchema, existingOK := existing.(map[string]any)
			itemSchema, itemOK := item.(map[string]any)
			if existingOK && itemOK {
				properties[key] = mergeSchemas(existingSchema, itemSchema)
				continue
			}
		}
		properties[key] = item
	}

	required := intersect(requiredKeys(a), requiredKeys(b))
	sort.Strings(required)

	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func mergeArrays(a, b map[string]any) map[string]any {
	aItems, aOK := a["items"].(map[string]any)
	bItems, bOK := b["items"].(map[string]any)
	switch {
	case aOK && bOK:
		return map[string]any{"type": "array", "items": mergeSchemas(aItems, bItems)}
	case aOK:
		return a
	case bOK:
		return b
	default:
		return map[string]any{"type": "array"}
	}
}

func requiredKeys(schema map[string]any) []string {
	values, _ := schema["required"].([]string)
	return values
}

func intersect(a, b []string) []string {
	present := make(map[string]bool, len(b))
	for _, value := range b {
		present[value] = true
	}
	var out []string
	for _, value := range a {
		if present[value] {
			out = append(out, value)
		}
	}
	return out
}

func unionSchemas(a, b map[string]any) map[string]any {
	var schemas []any
	for _, schema := range []map[string]any{a, b} {
		if nested, ok := schema["anyOf"].([]any); ok {
			schemas = append(schemas, nested...)
			continue
		}
		schemas = append(schemas, schema)
	}

	var unique []any
	for _, candidate := range schemas {
		duplicate := false
		for _, existing := range unique {
			if reflect.DeepEqual(existing, candidate) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			unique = append(unique, candidate)
		}
	}
	if len(unique) == 1 {
		return unique[0].(map[string]any)
	}
	return map[string]any{"anyOf": unique}
}

type yamlPair struct {
	key   *yaml.Node
	value *yaml.Node
}

// effectivePairs expands YAML merge keys. Explicit keys take precedence over
// merged keys; for a sequence of merged mappings, earlier mappings win.
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
		keyString, err := yamlKeyString(key)
		if err != nil {
			return nil, err
		}
		explicitKeys[keyString] = true
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
				keyString, err := yamlKeyString(pair.key)
				if err != nil {
					return nil, err
				}
				if explicitKeys[keyString] || mergedKeys[keyString] {
					continue
				}
				mergedKeys[keyString] = true
				merged = append(merged, pair)
			}
		}
	}

	return append(merged, explicit...), nil
}

func mergeMappings(n *yaml.Node) ([]*yaml.Node, error) {
	n, err := derefNode(n)
	if err != nil {
		return nil, err
	}
	switch n.Kind {
	case yaml.MappingNode:
		return []*yaml.Node{n}, nil
	case yaml.SequenceNode:
		out := make([]*yaml.Node, 0, len(n.Content))
		for _, item := range n.Content {
			item, err := derefNode(item)
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

func derefNode(n *yaml.Node) (*yaml.Node, error) {
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
	n, err := derefNode(n)
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

func isMergeKey(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	return n.Tag == "!!merge" || (n.Kind == yaml.ScalarNode && n.Value == "<<")
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
