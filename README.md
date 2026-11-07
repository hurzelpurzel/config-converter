# config-converter

![build](https://github.com/hurzelpurzel/config-converter/actions/workflows/build.yml/badge.svg)

A small, dependency-light command-line tool written in Go that converts Spring
`application.properties` files to YAML and YAML back to `.properties` files.

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
  to-yaml, properties-to-yaml, p2y   convert properties to YAML
  to-properties, yaml-to-properties, y2p
                                     convert YAML to properties

Arguments:
  input   file to read; omit or use "-" to read stdin

Flags:
  -o, --output FILE   write to FILE instead of stdout
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
- Comments are preserved on a best-effort basis.

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
- Duplicate properties are allowed (last value wins), but a property cannot be
  both a scalar and a parent at the same time (`a=1` plus `a.b=2` is an error,
  because YAML cannot represent it).

## Development

```sh
go test ./...
go vet ./...
```
