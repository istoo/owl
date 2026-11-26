package redact

import (
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// jsonPlain is s, JSON text of its own (a tool call's arguments), as the
// vendor and the agent read it once unescaped, and where each of its bytes
// begins in s. at has len(plain)+1 entries, the last s's length, so a span
// found in plain maps back to the bytes of s it was written as: a value
// with a newline, a quote or a backslash in it is found where it is
// written escaped. When s has no escape, plain is s and at is nil.
func jsonPlain(s string) (plain string, at []int) {
	if !hasJSONEscape(s) {
		return s, nil
	}
	var b []byte
	var pos []int
	add := func(from int, out ...byte) {
		for range out {
			pos = append(pos, from)
		}
		b = append(b, out...)
	}
	for i := 0; i < len(s); {
		if s[i] != '\\' || i+1 >= len(s) {
			add(i, s[i])
			i++
			continue
		}
		start := i
		var out []byte
		switch s[i+1] {
		case '"', '\\', '/':
			out = []byte{s[i+1]}
			i += 2
		case 'b':
			out = []byte{'\b'}
			i += 2
		case 'f':
			out = []byte{'\f'}
			i += 2
		case 'n':
			out = []byte{'\n'}
			i += 2
		case 'r':
			out = []byte{'\r'}
			i += 2
		case 't':
			out = []byte{'\t'}
			i += 2
		case 'u':
			r, n := jsonRune(s[i:])
			if n == 0 {
				out = []byte{s[i+1]}
				i += 2
				break
			}
			out = utf8.AppendRune(nil, r)
			i += n
		default:
			// not an escape JSON knows: the backslash and its letter are
			// the text's own
			add(start, s[i], s[i+1])
			i += 2
			continue
		}
		add(start, out...)
	}
	pos = append(pos, len(s))
	return string(b), pos
}

// hasJSONEscape says s has an escape JSON reads: a backslash that the
// byte after it makes one.
func hasJSONEscape(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '\\' {
			continue
		}
		switch s[i+1] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
			return true
		}
	}
	return false
}

// jsonRune is the rune the escape at the start of s is (a \u escape, with
// a surrogate pair when it is one), and how long the escape is; 0 when s
// doesn't start with a whole \u escape.
func jsonRune(s string) (rune, int) {
	if len(s) < 6 || s[0] != '\\' || s[1] != 'u' {
		return 0, 0
	}
	n, err := strconv.ParseUint(s[2:6], 16, 32)
	if err != nil {
		return 0, 0
	}
	r := rune(n)
	if utf16.IsSurrogate(r) && len(s) >= 12 && s[6] == '\\' && s[7] == 'u' {
		if n2, err := strconv.ParseUint(s[8:12], 16, 32); err == nil {
			if pair := utf16.DecodeRune(r, rune(n2)); pair != utf8.RuneError {
				return pair, 12
			}
		}
	}
	return r, 6
}

// spansInJSON maps spans found in the plain text of JSON text back to
// where they are written in it.
func spansInJSON(found []span, at []int) []span {
	if at == nil {
		return found
	}
	for i := range found {
		found[i].start = at[found[i].start]
		found[i].end = at[found[i].end]
	}
	return found
}

// knownSpansInJSON is knownSpans for JSON text of its own: a value masked
// before reads here as it is written, escaped, where knownSpans reading
// the unescapedView misses one with a newline or a quote in it (#58).
func knownSpansInJSON(s string, o Options) []span {
	if !o.Secrets && !o.Personal || !hasJSONEscape(s) {
		return nil
	}
	mu.RLock()
	any := len(known) > 0
	mu.RUnlock()
	if !any {
		return nil
	}
	plain, at := jsonPlain(s)
	return spansInJSON(knownSpans(plain, o), at)
}

// unescapeValue is v as the vendor and the agent read it: a JSON string's
// text with its escapes decoded. A value that isn't such text, or has no
// escape, is v itself.
func unescapeValue(v string) string {
	if !hasJSONEscape(v) {
		return v
	}
	plain, at := jsonPlain(v)
	if at == nil {
		return v
	}
	return plain
}
