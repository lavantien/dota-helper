package emit

import (
	"strconv"
	"testing"
)

// TestRoundFixedMatchesFmtFixed pins the parity rule: RoundFixed is the
// float readback of exactly the string fmtFixed prints, halfway cases
// included, so in-engine consumers see the digits the artifact ships.
func TestRoundFixedMatchesFmtFixed(t *testing.T) {
	values := []float64{
		0, 0.5, 1.0 / 3.0, 2.0 / 3.0,
		0.12345, 0.123449, 0.123451, 0.00005, 0.50005,
		0.0625, 0.1875, 0.5625, 1.0625, // binary-exact decimal midpoints
		0.9999499999, 0.99995, 1.00005, 0.00625001,
		-0.000049999, -0.00005,
	}
	for _, dec := range []int{2, 3, 4} {
		for _, v := range values {
			want, err := strconv.ParseFloat(fmtFixed(v, dec), 64)
			if err != nil {
				t.Fatalf("fmtFixed(%v, %d) printed a string ParseFloat rejects", v, dec)
			}
			if got := RoundFixed(v, dec); got != want {
				t.Fatalf("RoundFixed(%v, %d) = %v, want the fmtFixed parse %v", v, dec, got, want)
			}
		}
	}
	// round-half-even on exact midpoints is the shared strconv behavior both
	// sides must keep: 0.0625 at 3dp prints 0.062, 0.1875 prints 0.188.
	if s := fmtFixed(0.0625, 3); s != "0.062" {
		t.Fatalf("fmtFixed(0.0625, 3) = %s, want 0.062", s)
	}
	if s := fmtFixed(0.1875, 3); s != "0.188" {
		t.Fatalf("fmtFixed(0.1875, 3) = %s, want 0.188", s)
	}
	if RoundFixed(0.123456, 4) != 0.1235 || RoundFixed(0.123449, 4) != 0.1234 {
		t.Fatalf("RoundFixed 4dp boundaries moved: %v %v", RoundFixed(0.123456, 4), RoundFixed(0.123449, 4))
	}
}
