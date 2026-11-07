// Command config-converter converts Spring application.properties files to
// YAML and vice versa.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/hurzelpurzel/config-converter/internal/convert"
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
	switch command {
	case "to-yaml", "properties-to-yaml", "p2y":
		convertFunc = convert.PropertiesToYAML
	case "to-properties", "yaml-to-properties", "y2p":
		convertFunc = convert.YAMLToProperties
	default:
		fmt.Fprintf(stderr, "config-converter: unknown command %q\n\n", command)
		usage(stderr)
		return 2
	}

	var output string
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
		case arg == "-" || !strings.HasPrefix(arg, "-"):
			positional = append(positional, arg)
		default:
			fmt.Fprintf(stderr, "config-converter: unknown flag %q\n", arg)
			return 2
		}
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
	result, err := convertFunc(data)
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

Convert Spring application.properties files to YAML and vice versa.

Commands:
  to-yaml, properties-to-yaml, p2y   convert properties to YAML
  to-properties, yaml-to-properties, y2p
                                     convert YAML to properties

Arguments:
  input   file to read; omit or use "-" to read stdin

Flags:
  -o, --output FILE   write to FILE instead of stdout
  -h, --help          show this help
  -v, --version       show the version

Examples:
  %s to-yaml application.properties
  %s to-yaml application.properties -o application.yml
  %s to-properties application.yml -o application.properties
  cat application.yml | %s to-properties
`, name, name, name, name, name)
}
