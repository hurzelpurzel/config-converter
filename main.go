// Command config-converter converts Spring application.properties files to
// YAML and vice versa, generates JSON Schemas from YAML and validates YAML
// against JSON Schemas.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/hurzelpurzel/config-converter/internal/convert"
	"github.com/hurzelpurzel/config-converter/internal/schema"
)

var version = ""

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	switch args[0] {
	case "-h", "--help", "help":
		usage(stdout)
		return 0
	case "-v", "--version", "version":
		fmt.Fprintf(stdout, "config-converter %s\n", buildVersion())
		return 0
	}

	command := args[0]
	var convertFunc func([]byte) ([]byte, error)
	generateSchema := false
	validate := false
	switch command {
	case "to-yaml", "properties-to-yaml", "p2y":
		convertFunc = convert.PropertiesToYAML
	case "to-properties", "yaml-to-properties", "y2p":
		convertFunc = convert.YAMLToProperties
	case "generate-schema", "to-schema", "yaml-to-schema", "y2s":
		generateSchema = true
	case "validate":
		validate = true
	default:
		fmt.Fprintf(stderr, "config-converter: unknown command %q\n\n", command)
		usage(stderr)
		return 2
	}

	var output, schemaPath string
	var positional []string
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			usage(stdout)
			return 0
		case arg == "-v" || arg == "--version":
			fmt.Fprintf(stdout, "config-converter %s\n", buildVersion())
			return 0
		case arg == "-o" || arg == "--output":
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "config-converter: %s requires a file name\n", arg)
				return 2
			}
			i++
			output = args[i]
		case strings.HasPrefix(arg, "-o="):
			output = strings.TrimPrefix(arg, "-o=")
		case strings.HasPrefix(arg, "--output="):
			output = strings.TrimPrefix(arg, "--output=")
		case arg == "-s" || arg == "--schema":
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "config-converter: %s requires a file name\n", arg)
				return 2
			}
			i++
			schemaPath = args[i]
		case strings.HasPrefix(arg, "-s="):
			schemaPath = strings.TrimPrefix(arg, "-s=")
		case strings.HasPrefix(arg, "--schema="):
			schemaPath = strings.TrimPrefix(arg, "--schema=")
		case arg == "-" || !strings.HasPrefix(arg, "-"):
			positional = append(positional, arg)
		default:
			fmt.Fprintf(stderr, "config-converter: unknown flag %q\n", arg)
			return 2
		}
	}

	if validate {
		if len(positional) > 2 {
			fmt.Fprintln(stderr, "config-converter: expected at most one input file and one schema file")
			return 2
		}
		input := "-"
		if len(positional) >= 1 {
			input = positional[0]
		}
		if len(positional) == 2 {
			if schemaPath != "" {
				fmt.Fprintln(stderr, "config-converter: schema given both as argument and with --schema")
				return 2
			}
			schemaPath = positional[1]
		}
		if schemaPath == "" {
			fmt.Fprintln(stderr, "config-converter: validate requires a schema file (--schema FILE)")
			return 2
		}
		if output != "" {
			fmt.Fprintln(stderr, "config-converter: validate does not write output")
			return 2
		}

		data, err := readInput(input, stdin)
		if err != nil {
			fmt.Fprintf(stderr, "config-converter: %v\n", err)
			return 1
		}
		schemaData, err := readInput(schemaPath, stdin)
		if err != nil {
			fmt.Fprintf(stderr, "config-converter: %v\n", err)
			return 1
		}
		if err := schema.ValidateYAML(data, schemaData); err != nil {
			fmt.Fprintf(stderr, "config-converter: %v\n", err)
			return 1
		}
		return 0
	}

	if schemaPath != "" {
		fmt.Fprintln(stderr, "config-converter: --schema is only supported by validate")
		return 2
	}
	if len(positional) > 1 {
		fmt.Fprintln(stderr, "config-converter: expected at most one input file")
		return 2
	}

	input := "-"
	if len(positional) == 1 {
		input = positional[0]
	}

	data, err := readInput(input, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "config-converter: %v\n", err)
		return 1
	}

	var result []byte
	if generateSchema {
		result, err = schema.GenerateYAML(data)
	} else {
		result, err = convertFunc(data)
	}
	if err != nil {
		fmt.Fprintf(stderr, "config-converter: %v\n", err)
		return 1
	}
	if err := writeOutput(output, result, stdout); err != nil {
		fmt.Fprintf(stderr, "config-converter: %v\n", err)
		return 1
	}
	return 0
}

func readInput(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func writeOutput(path string, data []byte, stdout io.Writer) error {
	if path == "" || path == "-" {
		if _, err := stdout.Write(data); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func usage(w io.Writer) {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(w, `Usage: %s <command> [flags] [input]

Convert Spring application.properties files to YAML and vice versa, generate
JSON Schemas from YAML and validate YAML against JSON Schemas.

Commands:
  to-yaml, properties-to-yaml, p2y  convert properties to YAML
  to-properties, yaml-to-properties, y2p
                                    convert YAML to properties
  generate-schema, to-schema, yaml-to-schema, y2s
                                    generate a JSON Schema from YAML
  validate                          validate YAML against a JSON Schema

Arguments:
  input   file to read; omit or use "-" to read stdin

Flags:
  -o, --output FILE   write to FILE instead of stdout
  -s, --schema FILE   JSON Schema (JSON or YAML) for validate; may also be
                      passed as the second positional argument
  -h, --help          show this help
  -v, --version       show the version

Examples:
  %s to-yaml application.properties
  %s to-yaml application.properties -o application.yml
  %s to-properties application.yml -o application.properties
  %s generate-schema application.yml -o application.schema.json
  %s validate application.yml --schema application.schema.json
  cat application.yml | %s to-properties
`, name, name, name, name, name, name, name)
}
