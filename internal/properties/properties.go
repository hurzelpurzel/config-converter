// Package properties parses and formats Java .properties files.
//
// The parser follows the rules of java.util.Properties: both '=' and ':'
// separate keys from values, whitespace can act as a separator, comments start
// with '#' or '!', logical lines can be continued with a trailing backslash and
// the usual escape sequences (\t, \n, \r, \f, \\ and \uXXXX) are supported.
package properties

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Entry is a single logical property assignment.
type Entry struct {
	// Key and Value are fully unescaped.
	Key   string
	Value string

	// Comment contains the comment lines immediately preceding the entry.
	// The leading '#' or '!' markers have been removed.
	Comment string
	// FootComment contains comment lines that belong after the entry. It is
	// mainly populated when converting YAML to properties.
	FootComment string

	// Line is the 1-based physical line on which the entry starts.
	Line int
}

// Document is a parsed properties file.
type Document struct {
	Header  string
	Entries []Entry
	Footer  string
}

// Parse reads a Java .properties document.
func Parse(r io.Reader) (*Document, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)

	doc := &Document{}
	var pending []string
	var logical strings.Builder
	logicalLine := 0
	continuing := false
	lineNo := 0

	// A one-line lookahead is needed to recognise continuation lines that end
	// in an escaped backslash (see continuation).
	var lookahead string
	haveLookahead := false
	nextLine := func() (string, bool) {
		if haveLookahead {
			haveLookahead = false
			return lookahead, true
		}
		if scanner.Scan() {
			return scanner.Text(), true
		}
		return "", false
	}
	peekLine := func() (string, bool) {
		if !haveLookahead {
			if !scanner.Scan() {
				return "", false
			}
			lookahead = scanner.Text()
			haveLookahead = true
		}
		return lookahead, true
	}

	finishLogical := func() error {
		entry, err := parseLogical(logical.String(), logicalLine)
		if err != nil {
			return err
		}
		if entry.Key == "" {
			return fmt.Errorf("line %d: empty property key", logicalLine)
		}
		if len(doc.Entries) == 0 && doc.Header == "" {
			doc.Header = strings.Join(pending, "\n")
		} else {
			entry.Comment = strings.Join(pending, "\n")
		}
		pending = nil
		doc.Entries = append(doc.Entries, entry)
		return nil
	}

	for {
		line, ok := nextLine()
		if !ok {
			break
		}
		lineNo++

		if continuing {
			line = strings.TrimLeft(line, " \t\f")
			next, hasNext := peekLine()
			if more, strip := continuation(line, next, hasNext); more {
				logical.WriteString(line[:len(line)-strip])
				continue
			}
			logical.WriteString(line)
			if err := finishLogical(); err != nil {
				return nil, err
			}
			logical.Reset()
			continuing = false
			continue
		}

		line = strings.TrimLeft(line, " \t\f")
		if line == "" {
			continue
		}
		if line[0] == '#' || line[0] == '!' {
			pending = append(pending, commentText(line))
			continue
		}

		logicalLine = lineNo
		logical.Reset()
		next, hasNext := peekLine()
		if more, strip := continuation(line, next, hasNext); more {
			logical.WriteString(line[:len(line)-strip])
			continuing = true
			continue
		}
		logical.WriteString(line)
		if err := finishLogical(); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// A continuation at EOF simply ends the logical line.
	if continuing {
		if err := finishLogical(); err != nil {
			return nil, err
		}
	}
	doc.Footer = strings.Join(pending, "\n")
	return doc, nil
}

func trailingBackslashes(line string) int {
	n := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		n++
	}
	return n
}

// continuation reports whether line continues on the next physical line and
// how many trailing backslashes to drop.
//
// Java continues a logical line after an odd number of backslashes. As a
// tolerance for hand-wrapped files (for example the sample in Spring Boot's
// appendix) a trailing escaped backslash ("\\") followed by an indented line
// is also treated as a continuation.
func continuation(line, next string, hasNext bool) (bool, int) {
	n := trailingBackslashes(line)
	if n == 0 {
		return false, 0
	}
	if n%2 == 1 {
		return true, 1
	}
	if n == 2 && hasNext {
		trimmed := strings.TrimLeft(next, " \t\f")
		if trimmed == "" || len(trimmed) == len(next) || trimmed[0] == '#' || trimmed[0] == '!' {
			return false, 0
		}
		// Do not swallow an indented line that is itself a new property.
		if hasSeparator(trimmed) {
			return false, 0
		}
		return true, 2
	}
	return false, 0
}

func hasSeparator(line string) bool {
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '=', ':':
			return true
		}
	}
	return false
}

func commentText(line string) string {
	line = line[1:]
	if strings.HasPrefix(line, " ") {
		line = line[1:]
	}
	return line
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\f'
}

func parseLogical(line string, lineNo int) (Entry, error) {
	keyEnd := len(line)
	sep := -1
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' {
			i++ // the escaped character cannot be a separator
			continue
		}
		if c == '=' || c == ':' || isSpace(c) {
			keyEnd = i
			sep = i
			break
		}
	}

	keyRaw := line[:keyEnd]
	valueRaw := ""
	if sep >= 0 {
		i := sep
		if line[i] == '=' || line[i] == ':' {
			i++
		} else {
			for i < len(line) && isSpace(line[i]) {
				i++
			}
			if i < len(line) && (line[i] == '=' || line[i] == ':') {
				i++
			}
		}
		for i < len(line) && isSpace(line[i]) {
			i++
		}
		valueRaw = line[i:]
	}

	key, err := unescape(keyRaw)
	if err != nil {
		return Entry{}, fmt.Errorf("line %d: invalid key: %w", lineNo, err)
	}
	value, err := unescape(valueRaw)
	if err != nil {
		return Entry{}, fmt.Errorf("line %d: invalid value: %w", lineNo, err)
	}
	return Entry{Key: key, Value: value, Line: lineNo}, nil
}

func unescape(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}

		i++
		if i >= len(s) {
			// A dangling backslash at the end of a logical line is ignored.
			break
		}
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			r, last, err := decodeUnicodeEscape(s, i)
			if err != nil {
				return "", err
			}
			i = last
			b.WriteRune(r)
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String(), nil
}

// decodeUnicodeEscape decodes the four hex digits following the 'u' at index u.
// It also combines UTF-16 surrogate pairs. The returned index is the last
// consumed byte.
func decodeUnicodeEscape(s string, u int) (rune, int, error) {
	if u+4 >= len(s) {
		return 0, 0, fmt.Errorf("incomplete \\u escape")
	}
	v, err := strconv.ParseUint(s[u+1:u+5], 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid \\u escape %q", s[u-1:u+5])
	}
	last := u + 4
	r := rune(v)

	if r >= 0xD800 && r <= 0xDBFF && last+2 < len(s) && s[last+1] == '\\' && s[last+2] == 'u' {
		low, lowLast, err := decodeUnicodeEscape(s, last+2)
		if err == nil && low >= 0xDC00 && low <= 0xDFFF {
			return utf16.DecodeRune(r, low), lowLast, nil
		}
	}
	return r, last, nil
}

// Format renders a document using '=' as separator.
func Format(doc *Document) []byte {
	var b strings.Builder
	writeCommentBlock(&b, doc.Header)
	for _, entry := range doc.Entries {
		writeCommentBlock(&b, entry.Comment)
		b.WriteString(escapeKey(entry.Key))
		b.WriteByte('=')
		b.WriteString(escapeValue(entry.Value))
		b.WriteByte('\n')
		writeCommentBlock(&b, entry.FootComment)
	}
	writeCommentBlock(&b, doc.Footer)
	return []byte(b.String())
}

func writeCommentBlock(b *strings.Builder, comment string) {
	if comment == "" {
		return
	}
	for _, line := range strings.Split(comment, "\n") {
		if line == "" {
			b.WriteString("#\n")
			continue
		}
		b.WriteString("# ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

func escapeKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case ' ':
			b.WriteString(`\ `)
		case '=', ':', '#', '!':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}

func escapeValue(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	leading := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != ' ' {
			leading = false
		}
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case ' ':
			if leading {
				b.WriteString(`\ `)
			} else {
				b.WriteByte(' ')
			}
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}
