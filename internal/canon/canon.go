// Package canon produces RFC 8785 (JCS) canonical JSON for the subset of JSON
// Mandate signs and hashes: objects, arrays, strings, booleans, null and
// integers. Non-integer numbers are rejected, since money is always int64
// minor units and floats have no portable canonical form.
package canon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf16"
)

var ErrNonInteger = errors.New("canon: non-integer number")

// Marshal encodes v with encoding/json, then re-emits it canonically.
func Marshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := write(&buf, tree); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func write(b *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case json.Number:
		s := t.String()
		if strings.ContainsAny(s, ".eE") {
			return fmt.Errorf("%w: %s", ErrNonInteger, s)
		}
		if s == "-0" {
			s = "0"
		}
		b.WriteString(s)
	case string:
		writeString(b, t)
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := write(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		// JCS orders keys by UTF-16 code units, not bytes.
		slices.SortFunc(keys, func(a, b string) int {
			return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
		})
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, k)
			b.WriteByte(':')
			if err := write(b, t[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("canon: unsupported type %T", v)
	}
	return nil
}

// writeString follows ECMAScript JSON.stringify escaping, which JCS mandates.
func writeString(b *bytes.Buffer, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hex[r>>4])
				b.WriteByte(hex[r&0xf])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
