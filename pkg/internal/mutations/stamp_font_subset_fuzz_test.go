package mutations

import (
	"encoding/binary"
	"testing"
)

// FuzzSubsetTTF feeds arbitrary font bytes to the stamp subsetter and asserts
// crash-freedom.
//
// The bytes are a font file the caller hands to the stamp API, so this walks
// input litho did not produce: a table directory, the loca offsets, and the
// glyf table, following composite-glyph references to a fixed point. Unlike the
// readers beside it this one BUILDS a font from what it reads, so a length it
// trusts too far corrupts the written document rather than only the report.
//
// [FuzzValidateTTFForStamp] covers the validator that runs before it; measured
// against the fuzz corpora, every function in stamp_font_subset.go sat at zero.
func FuzzSubsetTTF(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("\x00\x01\x00\x00"))
	f.Add(stampableTTF()) // valid, but carries no glyf/loca to subset
	f.Add(subsettableTTF())
	f.Add(subsettableTTF()[:len(subsettableTTF())/2])

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		// Derive a keep set from the input so the subsetter is asked to drop
		// some glyphs rather than all or none.
		keep := map[uint16]bool{}
		for i := range 4 {
			if len(data) > i {
				keep[uint16(data[i])] = true
			}
		}
		_, _ = subsetTTF(data, keep)

		_ = findTableOffset(data, "glyf")
		_ = tableChecksum(data)
		for _, format := range []int16{0, 1} {
			_, _ = parseLocaOffsets(data, len(data)/2, format)
		}
	})
}

// subsettableTTF extends the stampable font with the loca and glyf tables the
// subsetter needs, so the fuzzer starts from a font it can actually subset
// rather than one rejected for a missing table.
//
// Two glyphs: .notdef is empty and glyph 1 is a simple glyph with one contour.
// Table records stay sorted by tag.
func subsettableTTF() []byte {
	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:], 1000) // unitsPerEm
	binary.BigEndian.PutUint16(head[50:], 0)    // indexToLocFormat: short

	maxp := make([]byte, 6)
	binary.BigEndian.PutUint32(maxp[0:], 0x00005000)
	binary.BigEndian.PutUint16(maxp[4:], 2) // numGlyphs

	// A simple glyph: one contour, no instructions, two points.
	glyph := make([]byte, 0, 32)
	hdr := make([]byte, 10)
	binary.BigEndian.PutUint16(hdr[0:], 1)   // numberOfContours
	binary.BigEndian.PutUint16(hdr[2:], 0)   // xMin
	binary.BigEndian.PutUint16(hdr[4:], 0)   // yMin
	binary.BigEndian.PutUint16(hdr[6:], 100) // xMax
	binary.BigEndian.PutUint16(hdr[8:], 100) // yMax
	glyph = append(glyph, hdr...)
	end := make([]byte, 2)
	binary.BigEndian.PutUint16(end, 1) // endPtsOfContours[0]: 2 points
	glyph = append(glyph, end...)
	glyph = append(glyph, 0x00, 0x00)             // instructionLength
	glyph = append(glyph, 0x01, 0x01)             // flags: on-curve, on-curve
	glyph = append(glyph, 0x00, 0x00, 0x00, 0x00) // x coords (int16 deltas)
	glyph = append(glyph, 0x00, 0x00, 0x00, 0x00) // y coords

	// loca (short format stores offset/2): glyph 0 empty, glyph 1 the above.
	loca := make([]byte, 6)
	binary.BigEndian.PutUint16(loca[0:], 0)
	binary.BigEndian.PutUint16(loca[2:], 0)
	binary.BigEndian.PutUint16(loca[4:], uint16(len(glyph)/2))

	tables := []struct {
		tag  string
		data []byte
	}{
		{"glyf", glyph}, {"head", head}, {"loca", loca}, {"maxp", maxp},
	}

	const dirEntry = 16
	out := make([]byte, 12+dirEntry*len(tables))
	binary.BigEndian.PutUint32(out[0:], 0x00010000)
	binary.BigEndian.PutUint16(out[4:], uint16(len(tables)))
	for i, t := range tables {
		rec := 12 + i*dirEntry
		copy(out[rec:], t.tag)
		binary.BigEndian.PutUint32(out[rec+8:], uint32(len(out)))
		binary.BigEndian.PutUint32(out[rec+12:], uint32(len(t.data)))
		out = append(out, t.data...)
	}
	return out
}

// TestSubsettableTTFIsSubset pins that the seed reaches the subsetter's real
// work: it must parse and come back changed, not be returned untouched with an
// error the way a font missing a table would be.
func TestSubsettableTTFIsSubset(t *testing.T) {
	t.Parallel()

	raw := subsettableTTF()
	out, err := subsetTTF(raw, map[uint16]bool{1: true})
	if err != nil {
		t.Fatalf("subsetTTF rejected the seed: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("subsetTTF produced nothing")
	}
	if findTableOffset(out, "glyf") < 0 {
		t.Error("the subset font lost its glyf table")
	}
	if findTableOffset(out, "loca") < 0 {
		t.Error("the subset font lost its loca table")
	}
}
