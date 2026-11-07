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
