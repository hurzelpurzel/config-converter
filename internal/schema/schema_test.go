package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateYAML(t *testing.T) {
	input := `# application configuration
server:
  port: 8080
  enabled: true
  tags:
    - web
    - api
database:
  url: jdbc:postgresql://localhost/app
  pool-size: 1.5
`

	generated, err := GenerateYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal(generated, &doc); err != nil {
		t.Fatalf("generated schema is not valid JSON: %v\n%s", err, generated)
	}
	if doc["$schema"] != Draft {
		t.Errorf("$schema = %v, want %q", doc["$schema"], Draft)
	}
	if doc["type"] != "object" || doc["additionalProperties"] != false {
		t.Errorf("unexpected root schema: %v", doc)
	}
	assertRequired(t, doc, "database", "server")

	server := property(t, doc, "server")
	if server["type"] != "object" {
		t.Errorf("server type = %v", server["type"])
	}
	if server["description"] != "application configuration" {
		t.Errorf("server description = %q", server["description"])
	}
	assertRequired(t, server, "enabled", "port", "tags")
	if got := property(t, server, "port")["type"]; got != "integer" {
		t.Errorf("server.port type = %v, want integer", got)
	}
	if got := property(t, server, "enabled")["type"]; got != "boolean" {
		t.Errorf("server.enabled type = %v, want boolean", got)
	}
	tags := property(t, server, "tags")
	if tags["type"] != "array" {
		t.Errorf("server.tags type = %v, want array", tags["type"])
	}
	if got := schemaMap(t, tags["items"])["type"]; got != "string" {
		t.Errorf("server.tags.items type = %v, want string", got)
	}
	if got := property(t, property(t, doc, "database"), "pool-size")["type"]; got != "number" {
		t.Errorf("database.pool-size type = %v, want number", got)
	}

	if err := ValidateYAML([]byte(input), generated); err != nil {
		t.Fatalf("generated schema does not validate its source: %v", err)
	}
}

func TestGenerateDescriptionsFromComments(t *testing.T) {
	input := `# Application server configuration.
server:
  # Port to listen on.
  # Defaults to 8080.
  port: 8080
  # Public host name.
  host: localhost
items:
  - name: one
    # Port for this item.
    port: 1
  - name: two
`
	generated, err := GenerateYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(generated, &doc); err != nil {
		t.Fatal(err)
	}

	server := property(t, doc, "server")
	if got := server["description"]; got != "Application server configuration." {
		t.Errorf("server description = %q", got)
	}
	if got := property(t, server, "port")["description"]; got != "Port to listen on.\nDefaults to 8080." {
		t.Errorf("server.port description = %q", got)
	}
	if got := property(t, server, "host")["description"]; got != "Public host name." {
		t.Errorf("server.host description = %q", got)
	}

	itemSchema := schemaMap(t, property(t, doc, "items")["items"])
	if got := property(t, itemSchema, "port")["description"]; got != "Port for this item." {
		t.Errorf("items.port description = %q", got)
	}
}

func TestGenerateMergesArrayObjectSchemas(t *testing.T) {
	input := `items:
  - name: one
    port: 1
  - name: two
    extra: true
`
	generated, err := GenerateYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal(generated, &doc); err != nil {
		t.Fatal(err)
	}
	items := property(t, doc, "items")
	itemSchema := schemaMap(t, items["items"])
	if itemSchema["type"] != "object" {
		t.Fatalf("items.items type = %v, want object", itemSchema["type"])
	}
	assertRequired(t, itemSchema, "name")
	for _, key := range []string{"name", "port", "extra"} {
		if _, ok := itemSchema["properties"].(map[string]any)[key]; !ok {
			t.Errorf("merged schema is missing property %q", key)
		}
	}
}

func TestGenerateMixedArrayUsesAnyOf(t *testing.T) {
	generated, err := GenerateYAML([]byte("values:\n  - 1\n  - text\n  - true\n"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(generated, &doc); err != nil {
		t.Fatal(err)
	}
	items := schemaMap(t, property(t, doc, "values")["items"])
	anyOf, ok := items["anyOf"].([]any)
	if !ok {
		t.Fatalf("items schema = %v, want anyOf", items)
	}
	if len(anyOf) != 3 {
		t.Fatalf("anyOf has %d entries, want 3: %v", len(anyOf), anyOf)
	}
}

func TestGenerateMergesIntegerAndNumber(t *testing.T) {
	generated, err := GenerateYAML([]byte("values:\n  - 1\n  - 2.5\n"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(generated, &doc); err != nil {
		t.Fatal(err)
	}
	items := schemaMap(t, property(t, doc, "values")["items"])
	if items["type"] != "number" {
		t.Errorf("items type = %v, want number", items["type"])
	}
}

func TestValidateYAML(t *testing.T) {
	schemaJSON := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"port": {"type": "integer", "minimum": 1}
		},
		"required": ["name"],
		"additionalProperties": false
	}`

	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{"valid", "name: app\nport: 8080\n", false},
		{"optional property missing", "name: app\n", false},
		{"wrong type", "name: app\nport: many\n", true},
		{"minimum", "name: app\nport: 0\n", true},
		{"missing required", "port: 8080\n", true},
		{"unknown property", "name: app\nextra: true\n", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateYAML([]byte(test.yaml), []byte(schemaJSON))
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateYAML() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateYAMLWithYAMLSchema(t *testing.T) {
	schemaYAML := `type: object
properties:
  enabled:
    type: boolean
required:
  - enabled
`
	if err := ValidateYAML([]byte("enabled: true\n"), []byte(schemaYAML)); err != nil {
		t.Fatalf("valid YAML rejected: %v", err)
	}
	if err := ValidateYAML([]byte("enabled: yes please\n"), []byte(schemaYAML)); err == nil {
		t.Fatal("invalid YAML accepted")
	}
}

func TestValidateYAMLErrors(t *testing.T) {
	validSchema := []byte(`{"type": "object"}`)
	tests := []struct {
		name   string
		yaml   string
		schema string
		want   string
	}{
		{"invalid yaml", "name: [unclosed\n", string(validSchema), "invalid YAML"},
		{"multiple documents", "a: 1\n---\nb: 2\n", string(validSchema), "multiple YAML documents"},
		{"invalid schema", "a: 1\n", `{"type": "nonsense"}`, "invalid JSON Schema"},
		{"invalid schema syntax", "a: 1\n", `type: [`, "invalid JSON Schema"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateYAML([]byte(test.yaml), []byte(test.schema))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateYAML() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateEmptyYAML(t *testing.T) {
	schema := []byte(`{"type": "null"}`)
	if err := ValidateYAML(nil, schema); err != nil {
		t.Fatalf("empty YAML should be null: %v", err)
	}
	if err := ValidateYAML([]byte("# only a comment\n"), schema); err != nil {
		t.Fatalf("comment-only YAML should be null: %v", err)
	}
}

func TestNormalizeNonStringKeys(t *testing.T) {
	generated, err := GenerateYAML([]byte("1: one\n"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(generated, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["properties"].(map[string]any)["1"]; !ok {
		t.Fatalf("numeric mapping key was not converted to a string: %s", generated)
	}
}

func property(t *testing.T, object map[string]any, name string) map[string]any {
	t.Helper()
	properties, ok := object["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties: %v", object)
	}
	value, ok := properties[name]
	if !ok {
		t.Fatalf("schema has no property %q: %v", name, properties)
	}
	return schemaMap(t, value)
}

func schemaMap(t *testing.T, value any) map[string]any {
	t.Helper()
	schema, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is not a schema object: %v", value)
	}
	return schema
}

func assertRequired(t *testing.T, object map[string]any, want ...string) {
	t.Helper()
	values, ok := object["required"].([]any)
	if !ok {
		t.Fatalf("schema has no required list: %v", object)
	}
	got := make([]string, len(values))
	for i, value := range values {
		got[i] = value.(string)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("required = %v, want %v", got, want)
	}
}
