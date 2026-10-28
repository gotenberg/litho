package cmap

import "testing"

// TestCIDMap_Codes splits a mixed-width string the way Decode does: in
// 90ms-RKSJ-H, 0x41 is a one-byte code and 0x81 0x40 a two-byte one.
func TestCIDMap_Codes(t *testing.T) {
	m := LoadPredefinedCIDMap("90ms-RKSJ-H")
	if m == nil {
		t.Fatal("expected a CID map for 90ms-RKSJ-H")
	}
	got := m.Codes([]byte{0x41, 0x81, 0x40})
	want := []uint32{0x41, 0x8140}
	if len(got) != len(want) {
		t.Fatalf("Codes = %#x, want %#x", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Codes[%d] = %#x, want %#x", i, got[i], want[i])
		}
	}
	if decoded := m.Decode([]byte{0x41, 0x81, 0x40}); len(decoded) != len(got) {
		t.Errorf("Codes split %d codes, Decode %d", len(got), len(decoded))
	}
}

// TestLoadPredefined_FollowsTheUseCMapChain pins that a -V CMap reads through
// its -H base, as veraPDF's PDCMap and PDF.js's extendCMap read it. 90ms-RKSJ-V
// holds only the vertical forms and no codespace of its own, so a reader
// skipping the chain split no code of it at all.
func TestLoadPredefined_FollowsTheUseCMapChain(t *testing.T) {
	t.Parallel()

	v := LoadPredefinedCIDMap("90ms-RKSJ-V")
	if v == nil {
		t.Fatal("90ms-RKSJ-V does not resolve")
	}
	got := v.Decode([]byte{0x41, 0x81, 0x41})
	if len(got) != 2 || !got[0].Mapped || got[0].CID != 264 || !got[1].Mapped || got[1].CID != 7887 {
		t.Errorf("want <41> from the base as CID 264 and the vertical <8141> as CID 7887, got %+v", got)
	}
	if v.WMode() != 1 {
		t.Errorf("90ms-RKSJ-V declares /WMode %d, want 1", v.WMode())
	}

	uni := LoadPredefinedCMap("UniJIS-UCS2-V")
	if uni == nil {
		t.Fatal("UniJIS-UCS2-V has no ToUnicode composition")
	}
	if n, s, ok := uni.Map([]byte{0x00, 0x41}); !ok || n != 2 || s != "A" {
		t.Errorf("<0041> of UniJIS-UCS2-V, which UniJIS-UCS2-H maps, reads as (%d, %q, %v), want (2, \"A\", true)", n, s, ok)
	}
}

// TestLoadPredefinedCMap_LeavesNotdefRangesOut pins the PDF.js reading: a
// notdefrange code names no character. 90ms-RKSJ-H maps <01> to CID 231, the
// CID of a real character, which composing would emit for a control byte.
func TestLoadPredefinedCMap_LeavesNotdefRangesOut(t *testing.T) {
	t.Parallel()

	uni := LoadPredefinedCMap("90ms-RKSJ-H")
	if uni == nil {
		t.Fatal("90ms-RKSJ-H has no ToUnicode composition")
	}
	if _, s, ok := uni.Map([]byte{0x01}); ok && s != "" {
		t.Errorf("notdefrange code <01> reads as %q, want no character", s)
	}
}

func TestLoadPredefinedCIDMap_Unknown(t *testing.T) {
	if m := LoadPredefinedCIDMap("Not-A-CMap"); m != nil {
		t.Errorf("unknown encoding name must return nil, got %+v", m)
	}
}

// TestCIDMap_Decode_RoundTrip confirms a mapped code decodes back to its CID,
// for both 1- and 2-byte codes. The code's byte width is the narrowest one that
// holds the value without truncation and lands inside a codespace (the rule
// [byteLength] broke for mixed-width CMaps before it stopped truncating codes).
func TestCIDMap_Decode_RoundTrip(t *testing.T) {
	m := LoadPredefinedCIDMap("90ms-RKSJ-H")
	if m == nil {
		t.Fatal("expected a CID map for 90ms-RKSJ-H")
	}

	width := func(code int64) int {
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

	oneByte, twoByte := int64(-1), int64(-1)
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

	for _, code := range []int64{oneByte, twoByte} {
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
	for v := int64(0x8140); v <= 0x9FFC && unmapped == nil; v++ {
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
