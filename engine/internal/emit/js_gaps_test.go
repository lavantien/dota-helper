package emit

import (
	"math"
	"strings"
	"testing"
)

// quoteJS must escape both control characters the authored prose can carry
// through copy-paste; newline and carriage return each become their two-byte
// javascript escape.
func TestQuoteJSEscapesNewlineAndCarriageReturn(t *testing.T) {
	if got := quoteJS("line1\nline2"); got != `'line1\nline2'` {
		t.Errorf("quoteJS newline = %s", got)
	}
	if got := quoteJS("line1\rline2"); got != `'line1\rline2'` {
		t.Errorf("quoteJS carriage return = %s", got)
	}
}

// TestJSValueKindArms pins the renderJS arms the artifacts' own shapes never
// hit: nulls, non-finite floats, empty composites, unsigned kinds, and the
// panic on kinds that cannot render.
func TestJSValueKindArms(t *testing.T) {
	if got := jsValue(nil); got != "null" {
		t.Errorf("jsValue(nil) = %s", got)
	}
	var p *int
	if got := jsValue(p); got != "null" {
		t.Errorf("jsValue(nil pointer) = %s", got)
	}
	n := 3
	if got := jsValue(&n); got != "3" {
		t.Errorf("jsValue(pointer) = %s", got)
	}
	if got := jsValue([]any{nil}); got != "[null]" {
		t.Errorf("jsValue([nil]) = %s", got)
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got := jsValue(v); got != "null" {
			t.Errorf("jsValue(%v) = %s, want null", v, got)
		}
	}
	if got := jsValue([]int(nil)); got != "[]" {
		t.Errorf("jsValue(nil slice) = %s", got)
	}
	if got := jsValue(map[string]int{}); got != "{}" {
		t.Errorf("jsValue(empty map) = %s", got)
	}
	if got := jsValue(struct{}{}); got != "{}" {
		t.Errorf("jsValue(empty struct) = %s", got)
	}
	if got := jsValue(uint16(7)); got != "7" {
		t.Errorf("jsValue(uint16) = %s", got)
	}
	if got := jsValue([2]string{"a", "b"}); got != `['a', 'b']` {
		t.Errorf("jsValue(array) = %s", got)
	}
	if got := jsValue(map[string]int{"b": 1, "a": 2}); got != `{ 'a': 2, 'b': 1 }` {
		t.Errorf("jsValue(map) = %s, want keys sorted ascending", got)
	}
}

type taggedFields struct {
	A     string  `json:"a"`
	B     int     `json:"b,omitempty"`
	C     bool    `json:",omitempty"`
	Hides float64 `json:"-"`
}

// struct rendering honors json names, omitempty, and keeps an explicitly
// dashed field under its Go name rather than dropping it.
func TestJSValueStructTags(t *testing.T) {
	if got := jsValue(taggedFields{A: "x"}); got != `{ 'a': 'x', 'Hides': 0 }` {
		t.Errorf("jsValue(zero omitempty fields) = %s", got)
	}
	got := jsValue(taggedFields{A: "x", B: 7, C: true, Hides: 1.5})
	if got != `{ 'a': 'x', 'b': 7, 'C': true, 'Hides': 1.5 }` {
		t.Errorf("jsValue(populated fields) = %s", got)
	}
}

func TestJSValuePanicsOnUnsupportedKind(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("jsValue must panic on an unrenderable kind")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "unsupported js value kind") {
			t.Fatalf("panic = %v, want the unsupported-kind message", r)
		}
	}()
	jsValue(make(chan int))
}
