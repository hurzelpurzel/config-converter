package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunStdin(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"to-yaml"}, strings.NewReader("server.port=8080\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	want := "server:\n  port: \"8080\"\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileAndOutputFlagAfterInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "application.properties")
	output := filepath.Join(dir, "application.yml")
	if err := os.WriteFile(input, []byte("a.b=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"to-yaml", input, "-o", output}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a:\n  b: \"1\"\n" {
		t.Fatalf("output file = %q", data)
	}
}

func TestRunErrors(t *testing.T) {
	tests := []struct {
		args  []string
		input string
		code  int
	}{
		{nil, "", 2},
		{[]string{"unknown"}, "", 2},
		{[]string{"to-yaml", "a", "b"}, "", 2},
		{[]string{"to-yaml", "--nope"}, "", 2},
		{[]string{"to-yaml", "-o"}, "", 2},
		{[]string{"to-properties"}, "a=1\na.b=2\n", 1},
		{[]string{"generate-schema", "a", "b"}, "", 2},
		{[]string{"to-yaml", "--schema", "schema.json"}, "", 2},
		{[]string{"validate"}, "", 2},
	}

	for _, test := range tests {
		var stdout, stderr bytes.Buffer
		code := run(test.args, strings.NewReader(test.input), &stdout, &stderr)
		if code != test.code {
			t.Errorf("run(%q) exit code = %d, want %d (stderr: %s)", test.args, code, test.code, stderr.String())
		}
	}
}

func TestRunHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}, {"to-yaml", "--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Errorf("run(%q) exit code = %d", args, code)
		}
		if !strings.Contains(stdout.String(), "Usage:") {
			t.Errorf("run(%q) did not print usage", args)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("version exit code = %d", code)
	}
	if !strings.HasPrefix(stdout.String(), "config-converter ") {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func TestRunGenerateSchema(t *testing.T) {
	input := "server:\n  port: 8080\n  enabled: true\n"
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate-schema"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		`"$schema": "https://json-schema.org/draft/2020-12/schema"`,
		`"port"`,
		`"type": "integer"`,
		`"enabled"`,
		`"type": "boolean"`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("schema output does not contain %q:\n%s", want, output)
		}
	}
}

func TestRunValidate(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "application.schema.json")
	yamlPath := filepath.Join(dir, "application.yml")
	if err := os.WriteFile(schemaPath, []byte(`{
		"type": "object",
		"properties": {"port": {"type": "integer"}},
		"required": ["port"]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(yamlPath, []byte("port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"validate", yamlPath, "--schema", schemaPath}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("valid file: exit code = %d, stderr: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("valid file wrote stdout: %q", stdout.String())
	}

	if err := os.WriteFile(yamlPath, []byte("port: many\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"validate", yamlPath, schemaPath}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("invalid file: exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "does not match schema") {
		t.Errorf("invalid file stderr = %q", stderr.String())
	}
}

func TestRunValidateMissingSchema(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"validate"}, strings.NewReader("port: 8080\n"), &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "requires a schema file") {
		t.Errorf("stderr = %q", stderr.String())
	}
}
