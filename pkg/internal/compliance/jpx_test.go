package compliance

import (
	"encoding/binary"
	"testing"
)

// TestIsJP2Container pins that [isJP2Container] accepts the 12-byte JP2
// signature box and rejects anything shorter or different.
func TestIsJP2Container(t *testing.T) {
	t.Parallel()

	sig := []byte{0x00, 0x00, 0x00, 0x0C, 0x6A, 0x50, 0x20, 0x20, 0x0D, 0x0A, 0x87, 0x0A}

	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"valid signature", sig, true},
		{"valid with trailing data", append(sig, 0x00, 0x00), true},
		{"too short", sig[:11], false},
		{"empty", nil, false},
		{"wrong byte 4", []byte{0x00, 0x00, 0x00, 0x0C, 0xFF, 0x50, 0x20, 0x20, 0x0D, 0x0A, 0x87, 0x0A}, false},
		{"all zeros", make([]byte, 12), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isJP2Container(tc.data); got != tc.want {
				t.Errorf("isJP2Container(%v) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

// TestParseJP2Boxes pins how [parseJP2Boxes] splits a box sequence, a zero
// length reaching to the end and a length past the data stopping the walk.
func TestParseJP2Boxes(t *testing.T) {
	t.Parallel()

	t.Run("single box", func(t *testing.T) {
		t.Parallel()
		// Length=12, type='test', 4 bytes payload
		data := make([]byte, 12)
		binary.BigEndian.PutUint32(data[0:4], 12)
		binary.BigEndian.PutUint32(data[4:8], 0x74657374) // 'test'
		data[8], data[9], data[10], data[11] = 0xAA, 0xBB, 0xCC, 0xDD
		boxes := parseJP2Boxes(data)
		if len(boxes) != 1 {
			t.Fatalf("expected 1 box, got %d", len(boxes))
		}
		if boxes[0].boxType != 0x74657374 {
			t.Errorf("unexpected box type: %x", boxes[0].boxType)
		}
		if len(boxes[0].payload) != 4 {
			t.Errorf("expected 4 bytes payload, got %d", len(boxes[0].payload))
		}
	})

	t.Run("two boxes", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, 24)
		// Box 1: length=12, type=0x11111111
		binary.BigEndian.PutUint32(data[0:4], 12)
		binary.BigEndian.PutUint32(data[4:8], 0x11111111)
		// Box 2: length=12, type=0x22222222
		binary.BigEndian.PutUint32(data[12:16], 12)
		binary.BigEndian.PutUint32(data[16:20], 0x22222222)
		boxes := parseJP2Boxes(data)
		if len(boxes) != 2 {
			t.Fatalf("expected 2 boxes, got %d", len(boxes))
		}
	})

	t.Run("zero length box extends to end", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, 16)
		binary.BigEndian.PutUint32(data[0:4], 0) // length 0 = rest of data
		binary.BigEndian.PutUint32(data[4:8], 0x74657374)
		boxes := parseJP2Boxes(data)
		if len(boxes) != 1 {
			t.Fatalf("expected 1 box, got %d", len(boxes))
		}
		if len(boxes[0].payload) != 8 {
			t.Errorf("expected 8 bytes payload, got %d", len(boxes[0].payload))
		}
	})

	t.Run("truncated data", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, 8)
		binary.BigEndian.PutUint32(data[0:4], 100) // claims 100 bytes but only 8 available
		binary.BigEndian.PutUint32(data[4:8], 0x74657374)
		boxes := parseJP2Boxes(data)
		if len(boxes) != 0 {
			t.Errorf("expected 0 boxes for truncated data, got %d", len(boxes))
		}
	})

	t.Run("empty data", func(t *testing.T) {
		t.Parallel()
		boxes := parseJP2Boxes(nil)
		if len(boxes) != 0 {
			t.Errorf("expected 0 boxes, got %d", len(boxes))
		}
	})
}

// TestParseCOLR pins how [parseCOLR] reads a colour specification box payload
// for methods 1 and 2, and rejects a short one.
func TestParseCOLR(t *testing.T) {
	t.Parallel()

	t.Run("method 1 with enumCS", func(t *testing.T) {
		t.Parallel()
		// meth=0x01, prec=0, approx=0x01, enumCS=16 (sRGB)
		payload := []byte{0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x10}
		ci, ok := parseCOLR(payload)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if ci.meth != 0x01 || ci.approx != 0x01 || ci.enumCS != 16 {
			t.Errorf("unexpected colrInfo: %+v", ci)
		}
	})

	t.Run("method 2 no enumCS", func(t *testing.T) {
		t.Parallel()
		payload := []byte{0x02, 0x00, 0x00}
		ci, ok := parseCOLR(payload)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if ci.meth != 0x02 || ci.enumCS != 0 {
			t.Errorf("unexpected colrInfo: %+v", ci)
		}
	})

	t.Run("too short", func(t *testing.T) {
		t.Parallel()
		_, ok := parseCOLR([]byte{0x01, 0x00})
		if ok {
			t.Error("expected ok=false for too-short payload")
		}
	})
}

// TestSelectColr pins which colour specification box [selectColr] chooses
// when a JP2 file carries several.
func TestSelectColr(t *testing.T) {
	t.Parallel()

	box := func(meth, approx byte, enumCS uint32) colrInfo {
		return colrInfo{meth: meth, approx: approx, enumCS: enumCS}
	}

	tests := []struct {
		name          string
		boxes         []colrInfo
		wantMethSet   bool
		wantMeth      byte
		wantEnumCSSet bool
		wantEnumCS    uint32
	}{
		{"empty", nil, false, 0, false, 0},
		{"single meth1", []colrInfo{box(0x01, 0x00, 16)}, true, 0x01, true, 16},
		{"single meth2 no enumCS", []colrInfo{box(0x02, 0x00, 0)}, true, 0x02, false, 0},
		{"single meth4", []colrInfo{box(0x04, 0x00, 0)}, true, 0x04, false, 0},
		// Two specs, neither APPROX=0x01: no effective colour space (t02-fail-a shape).
		{"two no-approx", []colrInfo{box(0x01, 0x00, 16), box(0x01, 0x00, 16)}, false, 0, false, 0},
		// APPROX=0x01 spec wins regardless of order.
		{"approx wins late", []colrInfo{box(0x04, 0x00, 0), box(0x01, 0x01, 16)}, true, 0x01, true, 16},
		// METH comes from first APPROX spec even if it lacks an enumCS.
		{"approx meth2", []colrInfo{box(0x02, 0x01, 0), box(0x01, 0x00, 16)}, true, 0x02, false, 0},
		{"approx enumCS 19", []colrInfo{box(0x01, 0x01, 19), box(0x01, 0x00, 16)}, true, 0x01, true, 19},
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
			return mkBox(boxTypeCOLR, pl)
		}
		return mkBox(boxTypeCOLR, []byte{meth, 0x00, approx})
	}
	ihdrBox := func() []byte {
		pl := make([]byte, 14)
		binary.BigEndian.PutUint16(pl[8:10], 3) // 3 colour channels (valid)
		pl[10] = 0x07                           // bit depth 8 (valid)
		return mkBox(boxTypeIHDR, pl)
	}
	jp2 := func(colrs ...[]byte) []byte {
		inner := ihdrBox()
		for _, c := range colrs {
			inner = append(inner, c...)
		}
		return mkBox(boxTypeJP2H, inner)
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
			got := fired(checkJP2Container(tc.data, "Im0", 1, rules, tc.hasColorSpace))
			if !eq(got, tc.want) {
				t.Errorf("checkJP2Container fired tests %v, want %v", got, tc.want)
			}
		})
	}
}

// TestParseJ2KSIZMarker pins that [parseJ2KSIZMarker] reads the component
// count and depths from a SIZ marker and rejects truncated data.
func TestParseJ2KSIZMarker(t *testing.T) {
	t.Parallel()

	t.Run("valid 3-component image", func(t *testing.T) {
		t.Parallel()
		// Build minimal valid J2K with SOC + SIZ marker.
		// Need at least compStart + csiz*3 = 42 + 3*3 = 51 bytes.
		data := make([]byte, 51)
		data[0] = 0xFF
		data[1] = 0x4F // SOC
		data[2] = 0xFF
		data[3] = 0x51 // SIZ
		// Csiz at offset 40 = 3 components
		binary.BigEndian.PutUint16(data[40:42], 3)
		// Component entries at offset 42: Ssiz(1) XRsiz(1) YRsiz(1) per component
		data[42] = 0x07 // Ssiz=7 -> bit depth 8
		data[43] = 1    // XRsiz
		data[44] = 1    // YRsiz
		data[45] = 0x07
		data[46] = 1
		data[47] = 1
		data[48] = 0x07
		data[49] = 1
		data[50] = 1

		numComp, ssiz, err := parseJ2KSIZMarker(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if numComp != 3 {
			t.Errorf("expected 3 components, got %d", numComp)
		}
		if len(ssiz) != 3 {
			t.Errorf("expected 3 ssiz entries, got %d", len(ssiz))
		}
	})

	t.Run("truncated data", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, 30) // too short for Csiz
		_, _, err := parseJ2KSIZMarker(data)
		if err == nil {
			t.Error("expected error for truncated data")
		}
	})
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
