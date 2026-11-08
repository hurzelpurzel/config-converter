package convert

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hurzelpurzel/config-converter/internal/properties"
)

func TestPropertiesToYAML(t *testing.T) {
	input := `# Application
server.port=8080
spring.application.name=my-app
management.endpoints.web.exposure.include=health,info
app.servers[0].host=one
app.servers[1].host=two
app.settings[foo.bar]=baz
`
	want := `# Application
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
  settings:
    foo.bar: baz
`
	got, err := PropertiesToYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected YAML\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPropertiesToYAMLArrayGaps(t *testing.T) {
	input := "list[0]=x\nlist[2]=z\n"
	want := `list:
  - x
  - null
  - z
`
	got, err := PropertiesToYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected YAML\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPropertiesToYAMLComments(t *testing.T) {
	input := "# header\nserver.port=8080\n# database\nspring.datasource.url=jdbc:postgresql://localhost/db\n"
	want := `# header
server:
  port: "8080"
spring:
  datasource:
    # database
    url: jdbc:postgresql://localhost/db
`
	got, err := PropertiesToYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected YAML\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPropertiesToYAMLInlineComments(t *testing.T) {
	input := "# header\n# BANNER\nbanner.charset=UTF-8 # Banner file encoding.\nbanner.location=classpath:banner.txt # Banner file location.\nlogging.config= # Logging config.\n"
	want := "# header\n# BANNER\nbanner:\n  # Banner file encoding.\n  charset: UTF-8\n  # Banner file location.\n  location: classpath:banner.txt\nlogging:\n  # Logging config.\n  config: \"\"\n"
	got, err := PropertiesToYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected YAML\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPropertiesToYAMLKeepsHashInValue(t *testing.T) {
	input := "url=https://example.com/#anchor\ntext=foo #comment\n"
	want := "url: https://example.com/#anchor\ntext: 'foo #comment'\n"
	got, err := PropertiesToYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected YAML\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPropertiesToYAMLKeyPrefixConflict(t *testing.T) {
	input := "shell.auth=simple\nshell.auth.jaas.domain=my-domain\n"
	want := "shell.auth: simple\nshell:\n  auth:\n    jaas:\n      domain: my-domain\n"
	got, err := PropertiesToYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected YAML\n got:\n%s\nwant:\n%s", got, want)
	}

	propsData, err := YAMLToProperties(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(propsData) != input {
		t.Fatalf("round trip = %q, want %q", propsData, input)
	}
}

func TestPropertiesToYAMLFooterAndCommentsOnly(t *testing.T) {
	got, err := PropertiesToYAML([]byte("a=1\n# footer\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "a: \"1\"\n# footer\n"; string(got) != want {
		t.Fatalf("unexpected YAML\n got: %q\nwant: %q", got, want)
	}

	got, err = PropertiesToYAML([]byte("# one\n# two\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "# one\n# two\n"; string(got) != want {
		t.Fatalf("unexpected YAML\n got: %q\nwant: %q", got, want)
	}
}

func TestPropertiesToYAMLErrors(t *testing.T) {
	tests := []string{
		"a=1\na.b=2\n",
		"a.b=2\na=1\n",
		"a[0]=1\na.b=2\n",
		"a=1\na[0]=2\n",
		"a..b=1\n",
		"a[0=1\n",
		"[0]=x\n",
	}
	for _, input := range tests {
		if _, err := PropertiesToYAML([]byte(input)); err == nil {
			t.Errorf("PropertiesToYAML(%q) succeeded, want error", input)
		}
	}
}

func TestYAMLToProperties(t *testing.T) {
	input := `server:
  port: 8080
  servlet:
    context-path: /app
spring:
  profiles:
    active: dev
  list:
    - a
    - b
`
	want := "server.port=8080\n" +
		"server.servlet.context-path=/app\n" +
		"spring.profiles.active=dev\n" +
		"spring.list[0]=a\n" +
		"spring.list[1]=b\n"
	got, err := YAMLToProperties([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected properties\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestYAMLToPropertiesScalarTypes(t *testing.T) {
	input := `a: true
b: null
c: 1.5
d: "true"
e: 2024-01-02
f: ~
g: ""
`
	want := "a=true\nb=\nc=1.5\nd=true\ne=2024-01-02\nf=\ng=\n"
	got, err := YAMLToProperties([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected properties\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestYAMLToPropertiesComments(t *testing.T) {
	input := `# header
server:
  # the port
  port: 8080 # inline
  list:
    - a
    # b doc
    - b
`
	want := "# header\n" +
		"# the port\n" +
		"server.port=8080\n" +
		"# inline\n" +
		"server.list[0]=a\n" +
		"# b doc\n" +
		"server.list[1]=b\n"
	got, err := YAMLToProperties([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected properties\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestYAMLToPropertiesAnchorsAndMerge(t *testing.T) {
	input := `defaults: &defaults
  timeout: 30
service:
  <<: *defaults
  name: api
copy: *defaults
`
	want := "defaults.timeout=30\n" +
		"service.timeout=30\n" +
		"service.name=api\n" +
		"copy.timeout=30\n"
	got, err := YAMLToProperties([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected properties\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestYAMLToPropertiesMergePrecedence(t *testing.T) {
	input := `a: &a
  x: 1
  y: 2
b: &b
  x: 10
  z: 20
merged:
  <<: [*a, *b]
`
	want := "a.x=1\na.y=2\nb.x=10\nb.z=20\nmerged.x=1\nmerged.y=2\nmerged.z=20\n"
	got, err := YAMLToProperties([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected properties\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestYAMLToPropertiesErrors(t *testing.T) {
	tests := []string{
		"---\na: 1\n---\nb: 2\n",
		"- a\n- b\n",
		"just a string\n",
		"<<: 1\n",
	}
	for _, input := range tests {
		if _, err := YAMLToProperties([]byte(input)); err == nil {
			t.Errorf("YAMLToProperties(%q) succeeded, want error", input)
		}
	}
}

func TestYAMLEmpty(t *testing.T) {
	for _, input := range []string{"", "# only comments\n", "{}\n", "null\n"} {
		got, err := YAMLToProperties([]byte(input))
		if err != nil {
			t.Errorf("YAMLToProperties(%q): %v", input, err)
			continue
		}
		if len(got) != 0 {
			t.Errorf("YAMLToProperties(%q) = %q, want empty", input, got)
		}
	}
}

func TestPropertiesToYAMLEmpty(t *testing.T) {
	got, err := PropertiesToYAML(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestRoundTripPropertiesYAMLProperties(t *testing.T) {
	input := `server.port=8080
server.servlet.context-path=/app
spring.datasource.url=jdbc:postgresql://localhost/db
feature.enabled=true
empty=
special.value= leading and trailing 
multi.line=one\nTwo
app.list[0]=a
app.list[2]=c
`
	yamlData, err := PropertiesToYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	propsData, err := YAMLToProperties(yamlData)
	if err != nil {
		t.Fatal(err)
	}

	got := parseValues(t, propsData)
	want := parseValues(t, []byte(replaceSparse(t, input)))

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch\n got: %#v\nwant: %#v\nYAML:\n%s\nproperties:\n%s", got, want, yamlData, propsData)
	}
}

// parseValues parses properties and returns key/value pairs, ignoring order.
func parseValues(t *testing.T, data []byte) map[string]string {
	t.Helper()
	doc, err := properties.Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("parse properties: %v\n%s", err, data)
	}
	values := make(map[string]string)
	for _, entry := range doc.Entries {
		values[entry.Key] = entry.Value
	}
	return values
}

// replaceSparse inserts the explicit null slot that the round trip produces.
func replaceSparse(t *testing.T, input string) string {
	t.Helper()
	return strings.Replace(input, "app.list[2]=c", "app.list[1]=\napp.list[2]=c", 1)
}
