# config-converter

![build](https://github.com/hurzelpurzel/config-converter/actions/workflows/build.yml/badge.svg)

A small command-line tool written in Go that converts Spring
`application.properties` files to YAML and YAML back to `.properties` files. It
can also infer a JSON Schema from a YAML document and validate YAML against a
schema.

## Build

```sh
go build -o config-converter .
```

Or install it into `$GOBIN`:

```sh
go install .
```

GitHub Actions tests the project and builds binaries for Linux, macOS and
Windows (amd64 and arm64) on every push. Tags matching `v*` are published as
GitHub releases with the binaries and SHA-256 checksums attached.

## Usage

```text
config-converter <command> [flags] [input]

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
  -h, --help          show help
  -v, --version       show the version
```

### Examples

Convert to YAML:

```sh
config-converter to-yaml application.properties
config-converter to-yaml application.properties -o application.yml
cat application.properties | config-converter p2y
```

Convert to properties:

```sh
config-converter to-properties application.yml
config-converter y2p application.yml -o application.properties
cat application.yml | config-converter to-properties
```

For example, this input:

```properties
# Server
server.port=8080
spring.application.name=my-app
management.endpoints.web.exposure.include=health,info
app.servers[0].host=one
app.servers[1].host=two
```

becomes:

```yaml
# Server
server:
  port: "8080"
spring:
  application:
    name: my-app
management:
  endpoints:
    web:
      exposure:
        include: health,info
app:
  servers:
    - host: one
    - host: two
```

And converting back produces:

```properties
server.port=8080
spring.application.name=my-app
management.endpoints.web.exposure.include=health,info
app.servers[0].host=one
app.servers[1].host=two
```

### JSON Schema generation

Generate a JSON Schema from an existing YAML file:

```sh
config-converter generate-schema application.yml
config-converter generate-schema application.yml -o application.schema.json
cat application.yml | config-converter to-schema
```

The generator infers object properties, required keys, scalar types and array
item types. For arrays of objects, the properties of all elements are merged;
keys that are not present on every element are optional. Unknown properties are
rejected by the generated schema. Comments immediately above a key are copied
to that property's `description`; multi-line comments keep their line breaks.

### Validation

Validate a YAML file against a JSON Schema:

```sh
config-converter validate application.yml --schema application.schema.json
config-converter validate application.yml application.schema.json
cat application.yml | config-converter validate - -s application.schema.json
```

The command exits with status `0` when the document is valid and status `1`
when it is invalid. Validation errors are written to stderr. Schemas can be
written as JSON or YAML and may use any JSON Schema draft supported by the
validator (draft 2020-12 is the default when `$schema` is omitted).

### Go API

The schema functionality is also available as a module-internal package:

```go
import "github.com/hurzelpurzel/config-converter/internal/schema"

// Infer a JSON Schema from YAML.
schemaJSON, err := schema.GenerateYAML(yamlData)

// Validate YAML against a JSON Schema (JSON or YAML schema).
err = schema.ValidateYAML(yamlData, schemaJSON)
```

## What is supported

- Java `.properties` syntax:
  - `key=value`, `key: value` and whitespace separators
  - `#` and `!` comments
  - line continuations with a trailing `\`
  - escapes such as `\t`, `\n`, `\r`, `\f`, `\\`, `\uXXXX` and escaped spaces
- Spring-style nested keys:
  - `a.b.c` becomes nested YAML mappings
  - `list[0].name` becomes YAML sequences
  - `settings[some.key]` becomes a map key (`some.key`)
- YAML mappings, sequences, scalars, null values, anchors, aliases and merge keys
  (`<<`)
- Comments are preserved on a best-effort basis. Trailing inline comments
  (`key=value # comment`) are emitted as YAML comments above the corresponding
  key.

All property values are emitted as YAML strings, so values such as `8080` or
`true` keep their exact textual representation.

## Limitations

- Multiple YAML documents (`---` separators) are rejected. Spring profile
  documents must be converted separately or merged first.
- Empty YAML mappings (`{}`) and sequences (`[]`) have no `.properties`
  equivalent and are skipped.
- A YAML map key containing `.` is flattened to a dotted property
  (`a: {b.c: 1}` becomes `a.b.c=1`); the original nesting cannot be restored.
- Comments may move to a neighbouring property because `.properties` has no
  syntax for attaching comments to a value.
- Duplicate properties are allowed (last value wins). A key that is also the
  prefix of a longer key (`shell.auth` next to `shell.auth.jaas.domain`) is kept
  as a literal root key so both values survive the conversion. A single-segment
  key cannot be both a scalar and a parent at the same time (`a=1` plus `a.b=2`
  is an error, because YAML cannot represent it).

## Development

```sh
go test ./...
go vet ./...
```
