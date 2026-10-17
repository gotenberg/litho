package compliance

import (
	"encoding/binary"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
)

// TestSelectColr pins which colour specification box [selectColr] chooses
// when a JP2 file carries several.
func TestSelectColr(t *testing.T) {
	t.Parallel()

	box := func(meth, approx int, enumCS uint32) jpx.ColourSpec {
		return jpx.ColourSpec{Method: meth, Approximation: approx, EnumCS: enumCS}
	}

	tests := []struct {
		name          string
		boxes         []jpx.ColourSpec
		wantMethSet   bool
		wantMeth      int
		wantEnumCSSet bool
		wantEnumCS    uint32
	}{
		{"empty", nil, false, 0, false, 0},
		{"single meth1", []jpx.ColourSpec{box(0x01, 0x00, 16)}, true, 0x01, true, 16},
		{"single meth2 no enumCS", []jpx.ColourSpec{box(0x02, 0x00, 0)}, true, 0x02, false, 0},
		{"single meth4", []jpx.ColourSpec{box(0x04, 0x00, 0)}, true, 0x04, false, 0},
		// Two specs, neither APPROX=0x01: no effective colour space (t02-fail-a shape).
		{"two no-approx", []jpx.ColourSpec{box(0x01, 0x00, 16), box(0x01, 0x00, 16)}, false, 0, false, 0},
		// APPROX=0x01 spec wins regardless of order.
		{"approx wins late", []jpx.ColourSpec{box(0x04, 0x00, 0), box(0x01, 0x01, 16)}, true, 0x01, true, 16},
		// METH comes from first APPROX spec even if it lacks an enumCS.
		{"approx meth2", []jpx.ColourSpec{box(0x02, 0x01, 0), box(0x01, 0x00, 16)}, true, 0x02, false, 0},
		{"approx enumCS 19", []jpx.ColourSpec{box(0x01, 0x01, 19), box(0x01, 0x00, 16)}, true, 0x01, true, 19},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := selectColr(tc.boxes)
			if got.methSet != tc.wantMethSet || got.meth != tc.wantMeth ||
				got.enumCSSet != tc.wantEnumCSSet || got.enumCS != tc.wantEnumCS {
				t.Errorf("selectColr(%+v) = %+v, want methSet=%v meth=%#x enumCSSet=%v enumCS=%d",
					tc.boxes, got, tc.wantMethSet, tc.wantMeth, tc.wantEnumCSSet, tc.wantEnumCS)
			}
		})
	}
}

// TestCheckJP2ContainerColrSpec pins which colour specification tests
// [checkJP2Container] fires for each box combination, and that an image
// /ColorSpace waives them.
func TestCheckJP2ContainerColrSpec(t *testing.T) {
	t.Parallel()

	mkBox := func(btype uint32, payload []byte) []byte {
		b := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(b[0:4], uint32(8+len(payload)))
		binary.BigEndian.PutUint32(b[4:8], btype)
		copy(b[8:], payload)
		return b
	}
	colrBox := func(meth, approx byte, enumCS uint32) []byte {
		if meth == 0x01 {
			pl := []byte{meth, 0x00, approx, 0, 0, 0, 0}
			binary.BigEndian.PutUint32(pl[3:7], enumCS)
			return mkBox(jpx.BoxColour, pl)
		}
		return mkBox(jpx.BoxColour, []byte{meth, 0x00, approx})
	}
	ihdrBox := func() []byte {
		pl := make([]byte, 14)
		binary.BigEndian.PutUint16(pl[8:10], 3) // 3 colour channels (valid)
		pl[10] = 0x07                           // bit depth 8 (valid)
		return mkBox(jpx.BoxImageHeader, pl)
	}
	jp2 := func(colrs ...[]byte) []byte {
		inner := ihdrBox()
		for _, c := range colrs {
			inner = append(inner, c...)
		}
		return mkBox(jpx.BoxHeader, inner)
	}
	fired := func(vs []Violation) []int {
		var out []int
		seen := map[int]bool{}
		for _, v := range vs {
			if !seen[v.Rule.TestNumber] {
				seen[v.Rule.TestNumber] = true
				out = append(out, v.Rule.TestNumber)
			}
		}
		return out
	}
	eq := func(a, b []int) bool {
		if len(a) != len(b) {
			return false
		}
		m := map[int]int{}
		for _, x := range a {
			m[x]++
		}
		for _, x := range b {
			m[x]--
		}
		for _, c := range m {
			if c != 0 {
				return false
			}
		}
		return true
	}
	rules := jpxRulesForPart(2)

	tests := []struct {
		name          string
		data          []byte
		hasColorSpace bool
		want          []int
	}{
		// Two specs, neither APPROX=0x01: no exactly-one-spec, no effective method.
		{"two-no-approx", jp2(colrBox(0x01, 0x00, 16), colrBox(0x01, 0x00, 16)), false, []int{2, 3}},
		// Same, but the image dict supplies /ColorSpace: all colour rules waived.
		{"two-no-approx-hasCS", jp2(colrBox(0x01, 0x00, 16), colrBox(0x01, 0x00, 16)), true, nil},
		// Single spec, invalid METH.
		{"single-bad-meth", jp2(colrBox(0x04, 0x00, 0)), false, []int{3}},
		// Single spec, CIEJab enumerated colour space forbidden.
		{"single-ciejab", jp2(colrBox(0x01, 0x00, 19)), false, []int{4}},
		// Single valid spec: clean.
		{"single-valid", jp2(colrBox(0x01, 0x00, 16)), false, nil},
		// Bad-METH on a non-selected spec must NOT fire t3 (APPROX spec is valid).
		{"approx-valid-other-bad", jp2(colrBox(0x01, 0x01, 16), colrBox(0x04, 0x00, 0)), false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := fired(checkJP2Container(tc.data, "Im0", 1, rules, tc.hasColorSpace, document.NopLogger()))
			if !eq(got, tc.want) {
				t.Errorf("checkJP2Container fired tests %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCheckBitDepths pins that [checkBitDepths] fires on inconsistent or
// out-of-range component bit depths and passes uniform valid ones.
func TestCheckBitDepths(t *testing.T) {
	t.Parallel()

	rule := Rule{Specification: "test", Clause: "6.2", TestNumber: 5, Description: "bit depth"}

	t.Run("all same valid depth", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		// Ssiz 0x07 -> bit depth (0x07 & 0x7F)+1 = 8
		checkBitDepths(&results, []byte{0x07, 0x07, 0x07}, "Im0", 1, rule)
		if len(results) != 0 {
			t.Errorf("expected no violations, got %d", len(results))
		}
	})

	t.Run("inconsistent depths", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		checkBitDepths(&results, []byte{0x07, 0x0F}, "Im0", 1, rule)
		if len(results) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(results))
		}
	})

	t.Run("depth out of range", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		// 0x7F & 0x7F + 1 = 128, exceeds 38
		checkBitDepths(&results, []byte{0x7F}, "Im0", 1, rule)
		if len(results) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(results))
		}
	})

	t.Run("empty ssiz", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		checkBitDepths(&results, nil, "Im0", 1, rule)
		if len(results) != 0 {
			t.Errorf("expected no violations for empty ssiz, got %d", len(results))
		}
	})
}
