package mutations

import (
	"encoding/binary"
	"testing"
)

// FuzzValidateTTFForStamp feeds arbitrary bytes to the stamp font validator and
// asserts crash-freedom.
//
// The bytes come from a caller-supplied font file rather than from the PDF, but
// the rule is the same: this function's leading argument is a []byte it did not
// produce, and it walks an sfnt table directory to decide whether the font can
// be embedded. A truncated directory or a table offset past the end of the file
// is exactly what it exists to reject, so it must reject rather than panic.
func FuzzValidateTTFForStamp(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"))
	f.Add([]byte("OTTO\x00\x01\x00\x00"))
	f.Add([]byte("ttcf\x00\x01\x00\x00\x00\x00\x00\x02"))
	f.Add(append([]byte("\x00\x01\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00"),
		[]byte("glyf\x00\x00\x00\x00\xff\xff\xff\xff\xff\xff\xff\xff")...)) // offset past EOF
	// A font the validator ACCEPTS. Every seed above is rejected in the first
	// few lines, so without this the table reading that follows (head, hhea,
	// hmtx widths, the cmap walk) is never reached: mutation cannot invent five
	// consistent tables by chance.
	f.Add(stampableTTF())

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_ = ValidateTTFForStamp(data)
	})
}

// stampableTTF builds the smallest font [ValidateTTFForStamp] accepts: the five
// tables it reads, each just long enough to satisfy the offsets it indexes.
// Table records must be sorted by tag, which cmap/head/hhea/hmtx/maxp already
// are.
func stampableTTF() []byte {
	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:], 1000) // unitsPerEm
	binary.BigEndian.PutUint16(head[36:], 0)    // xMin
	binary.BigEndian.PutUint16(head[38:], 0)    // yMin
	binary.BigEndian.PutUint16(head[40:], 1000) // xMax
	binary.BigEndian.PutUint16(head[42:], 1000) // yMax
	binary.BigEndian.PutUint16(head[50:], 0)    // indexToLocFormat: short

	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[4:], 800)    // ascender
	binary.BigEndian.PutUint16(hhea[6:], 0xFF38) // descender: -200
	binary.BigEndian.PutUint16(hhea[34:], 1)     // numberOfHMetrics

	hmtx := make([]byte, 4)
	binary.BigEndian.PutUint16(hmtx[0:], 500) // advanceWidth
	binary.BigEndian.PutUint16(hmtx[2:], 0)   // leftSideBearing

	maxp := make([]byte, 6)
	binary.BigEndian.PutUint32(maxp[0:], 0x00005000) // version 0.5
	binary.BigEndian.PutUint16(maxp[4:], 1)          // numGlyphs

	const subtableOff = 4 + 8
	cmap := make([]byte, subtableOff+262)
	binary.BigEndian.PutUint16(cmap[2:], 1) // numTables
	binary.BigEndian.PutUint16(cmap[4:], 3) // platformID: Windows
	binary.BigEndian.PutUint16(cmap[6:], 1) // encodingID: Unicode BMP
	binary.BigEndian.PutUint32(cmap[8:], subtableOff)
	binary.BigEndian.PutUint16(cmap[subtableOff+0:], 0)   // format 0
	binary.BigEndian.PutUint16(cmap[subtableOff+2:], 262) // length

	tables := []struct {
		tag  string
		data []byte
	}{
		{"cmap", cmap}, {"head", head}, {"hhea", hhea}, {"hmtx", hmtx}, {"maxp", maxp},
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

// TestStampableTTFIsAccepted pins that the seed above really is accepted. A
// seed rejected at the first table would leave the target looking well seeded
// while covering nothing past the directory walk.
func TestStampableTTFIsAccepted(t *testing.T) {
	t.Parallel()

	if err := ValidateTTFForStamp(stampableTTF()); err != nil {
		t.Fatalf("the seed font was rejected: %v", err)
	}
}
