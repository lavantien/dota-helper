// Package emit renders the derived tables into the generated js artifacts.
// Formatters are pure bytes-out functions over plain input structs; a thin
// loader reads the duckdb tables and a thin writer persists the bytes.
package emit

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// quoteJS renders s as a single-quoted JavaScript string literal.
func quoteJS(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// fmtShort renders v with trailing zeros trimmed: 45, 51.86, 0.5.
func fmtShort(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// fmtFixed renders v with exactly dec decimals: 0.5 -> 0.5000.
func fmtFixed(v float64, dec int) string {
	return strconv.FormatFloat(v, 'f', dec, 64)
}

// RoundFixed rounds v to dec decimals by parsing back exactly the string
// fmtFixed prints, so in-engine consumers (the eval replay) score against
// the digits the artifact ships instead of unrounded floats.
func RoundFixed(v float64, dec int) float64 {
	n, err := strconv.ParseFloat(fmtFixed(v, dec), 64)
	if err != nil {
		return v
	}
	return n
}

// jsValue renders any json-shaped value as a compact javascript literal:
// single-quoted strings, struct fields in declaration order honoring json
// names and omitempty, map keys sorted ascending so output is deterministic.
func jsValue(v any) string {
	return renderJS(reflect.ValueOf(v))
}

func renderJS(v reflect.Value) string {
	if !v.IsValid() {
		return "null"
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return "null"
		}
		return renderJS(v.Elem())
	case reflect.String:
		return quoteJS(v.String())
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if f != f || f > 1e308 || f < -1e308 {
			return "null"
		}
		return fmtShort(f)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return "[]"
		}
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = renderJS(v.Index(i))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case reflect.Map:
		keys := v.MapKeys()
		sort.Slice(keys, func(a, b int) bool {
			return keys[a].String() < keys[b].String()
		})
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = quoteJS(k.String()) + ": " + renderJS(v.MapIndex(k))
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	case reflect.Struct:
		t := v.Type()
		var parts []string
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := f.Name
			omitEmpty := false
			if tag, ok := f.Tag.Lookup("json"); ok {
				fields := strings.Split(tag, ",")
				if fields[0] != "" && fields[0] != "-" {
					name = fields[0]
				}
				for _, opt := range fields[1:] {
					if opt == "omitempty" {
						omitEmpty = true
					}
				}
			}
			fv := v.Field(i)
			if omitEmpty && fv.IsZero() {
				continue
			}
			parts = append(parts, quoteJS(name)+": "+renderJS(fv))
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	panic(fmt.Sprintf("emit: unsupported js value kind %s", v.Kind()))
}
