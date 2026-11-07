package properties

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	src := "# header\n" +
		"key=value\n" +
		"spaced = spaced value\n" +
		"colon: colon value\n" +
		"escaped\\=key\\:x = a\\=b\\:c\n" +
		"continued = first \\\n" +
		"    second \\\n" +
		"    third\n" +
		"unicode=\\u00e4\\u00f6\\u00fc\n" +
		"emoji=\\uD83D\\uDE00\n" +
		"hash\\#key=v\n" +
		"key\\ with\\ spaces=value\n" +
		"empty=\n" +
		"novalue\n" +
		"! bang comment\n" +
		"# another\n" +
		"after=1\n"

	doc, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}

	want := &Document{
		Header: "header",
		Entries: []Entry{
			{Key: "key", Value: "value", Line: 2},
			{Key: "spaced", Value: "spaced value", Line: 3},
			{Key: "colon", Value: "colon value", Line: 4},
			{Key: "escaped=key:x", Value: "a=b:c", Line: 5},
			{Key: "continued", Value: "first second third", Line: 6},
			{Key: "unicode", Value: "äöü", Line: 9},
			{Key: "emoji", Value: "😀", Line: 10},
			{Key: "hash#key", Value: "v", Line: 11},
			{Key: "key with spaces", Value: "value", Line: 12},
			{Key: "empty", Value: "", Line: 13},
			{Key: "novalue", Value: "", Line: 14},
			{Key: "after", Value: "1", Comment: "bang comment\nanother", Line: 17},
		},
	}
	if !reflect.DeepEqual(doc, want) {
		t.Fatalf("unexpected document\n got: %#v\nwant: %#v", doc, want)
	}
}

func TestParseCommentsAndContinuationAtEOF(t *testing.T) {
	doc, err := Parse(strings.NewReader("# at start\n\na=1\n# trailing\nb=two\\\n"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Header != "at start" {
		t.Errorf("Header = %q", doc.Header)
	}
	if len(doc.Entries) != 2 {
		t.Fatalf("got %d entries", len(doc.Entries))
	}
	if doc.Entries[0].Key != "a" || doc.Entries[0].Value != "1" {
		t.Errorf("entry 0 = %#v", doc.Entries[0])
	}
	if doc.Entries[1].Comment != "trailing" || doc.Entries[1].Value != "two" {
		t.Errorf("entry 1 = %#v", doc.Entries[1])
	}
	if doc.Footer != "" {
		t.Errorf("Footer = %q", doc.Footer)
	}
}

func TestParseFooter(t *testing.T) {
	doc, err := Parse(strings.NewReader("a=1\n# end\n"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Footer != "end" {
		t.Errorf("Footer = %q", doc.Footer)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []string{
		"=value\n",
		"a=\\u12\n",
		"a=\\uZZZZ\n",
	}
	for _, src := range tests {
		if _, err := Parse(strings.NewReader(src)); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", src)
		}
	}
}

func TestFormatRoundTrip(t *testing.T) {
	doc := &Document{
		Header: "head",
		Entries: []Entry{
			{Key: "a b", Value: "x", Comment: "first\n  indented"},
			{Key: "c:d=e#f!g\\h", Value: "  lead\tx\nline2\r", Comment: ""},
			{Key: "plain", Value: ""},
		},
		Footer: "tail",
	}

	first := string(Format(doc))
	parsed, err := Parse(strings.NewReader(first))
	if err != nil {
		t.Fatalf("Parse(Format(doc)): %v\n%s", err, first)
	}
	second := string(Format(parsed))
	if second != first {
		t.Fatalf("format is not stable\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	got := make(map[string]string)
	for _, entry := range parsed.Entries {
		got[entry.Key] = entry.Value
	}
	want := map[string]string{
		"a b":          "x",
		"c:d=e#f!g\\h": "  lead\tx\nline2\r",
		"plain":        "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("values = %#v, want %#v", got, want)
	}
}

func TestFormatSeparatorHandling(t *testing.T) {
	doc := &Document{Entries: []Entry{{Key: "key", Value: " value"}}}
	out := string(Format(doc))
	if out != "key=\\ value\n" {
		t.Fatalf("Format = %q", out)
	}
	parsed, err := Parse(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Entries[0].Value != " value" {
		t.Fatalf("round trip value = %q", parsed.Entries[0].Value)
	}
}
