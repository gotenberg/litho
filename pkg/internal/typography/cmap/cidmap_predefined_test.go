package cmap

import "testing"

func TestLoadPredefinedCIDMap_Unknown(t *testing.T) {
	if m := LoadPredefinedCIDMap("Not-A-CMap"); m != nil {
		t.Errorf("unknown encoding name must return nil, got %+v", m)
	}
}

// TestCIDMap_Decode_RoundTrip confirms a mapped code decodes back to its CID,
// for both 1- and 2-byte codes. The code's byte width is the narrowest one that
// holds the value without truncation and lands inside a codespace (this is what
// the buggy package-internal byteLength fails to do for mixed-width CMaps).
func TestCIDMap_Decode_RoundTrip(t *testing.T) {
	m := LoadPredefinedCIDMap("90ms-RKSJ-H")
	if m == nil {
		t.Fatal("expected a CID map for 90ms-RKSJ-H")
	}

	width := func(code int) int {
		for _, n := range m.cs.codeLengths() {
			if code >= 1<<(8*n) {
				continue // value would be truncated at this width
			}
			if m.cs.inCodeSpace(intToBytes(code, n)) {
				return n
			}
		}
		return 0
	}

	var oneByte, twoByte = -1, -1
	for code := range m.charToCID {
		switch width(code) {
		case 1:
			if oneByte < 0 {
				oneByte = code
			}
		case 2:
			if twoByte < 0 {
				twoByte = code
			}
		}
		if oneByte >= 0 && twoByte >= 0 {
			break
		}
	}

	for _, code := range []int{oneByte, twoByte} {
		if code < 0 {
			continue
		}
		w := width(code)
		got := m.Decode(intToBytes(code, w))
		if len(got) != 1 || !got[0].Mapped || got[0].CID != m.charToCID[code] {
			t.Errorf("round-trip code %#x (width %d): got %+v, want CID %d mapped", code, w, got, m.charToCID[code])
		}
	}
}

// TestCIDMap_Decode_OutOfCodespace confirms bytes outside every codespace range
// decode as unmapped (a .notdef reference).
func TestCIDMap_Decode_OutOfCodespace(t *testing.T) {
	m := LoadPredefinedCIDMap("90ms-RKSJ-H")
	if m == nil {
		t.Fatal("expected a CID map for 90ms-RKSJ-H")
	}
	// 0xFF is neither a valid Shift-JIS single byte nor a valid lead byte.
	got := m.Decode([]byte{0xFF})
	if len(got) != 1 || got[0].Mapped {
		t.Errorf("0xFF should decode as unmapped (.notdef), got %+v", got)
	}
}

// TestCIDMap_Decode_RealignsAfterUnmappedMultiByte confirms that an unmapped
// two-byte code in a mixed-width CMap does not swallow a following mapped
// single-byte code: the decoder re-aligns and still resolves it.
func TestCIDMap_Decode_RealignsAfterUnmappedMultiByte(t *testing.T) {
	m := LoadPredefinedCIDMap("90ms-RKSJ-H")
	if m == nil {
		t.Fatal("expected a CID map for 90ms-RKSJ-H")
	}

	// Find a 2-byte value that is in the codespace but absent from the map.
	var unmapped []byte
	for v := 0x8140; v <= 0x9FFC && unmapped == nil; v++ {
		b := intToBytes(v, 2)
		if m.cs.inCodeSpace(b) {
			if _, ok := m.charToCID[v]; !ok {
				unmapped = b
			}
		}
	}
	if unmapped == nil {
		t.Skip("no in-codespace-unmapped 2-byte code found")
	}
	// A mapped single-byte code (ASCII 'A').
	if _, ok := m.charToCID[0x41]; !ok {
		t.Skip("0x41 not mapped")
	}

	got := m.Decode(append(append([]byte{}, unmapped...), 0x41))
	last := got[len(got)-1]
	if !last.Mapped || last.CID != m.charToCID[0x41] {
		t.Errorf("trailing mapped code 0x41 must survive the unmapped %#x: got %+v", unmapped, got)
	}
}
