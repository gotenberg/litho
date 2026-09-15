package compliance

import (
	"encoding/binary"
	"testing"
)

// FuzzJPXContainer feeds arbitrary bytes to the JPEG 2000 container checks and
// asserts crash-freedom.
//
// The bytes are an image XObject's stream, so they are file-controlled, and
// these walk the JP2 box tree and the raw J2K codestream markers to decide
// whether the colour specification conforms. [FuzzParseJP2Boxes] beside this
// covers the box walker alone; measured against the fuzz corpora, the three
// container-level checks were reached by nothing.
//
// All applicable parts run because the rule set is selected by part, and the
// hasColorSpace flag is toggled because it gates a different branch of the
// colour check rather than only the message.
func FuzzJPXContainer(f *testing.F) {
	f.Add([]byte{})
	f.Add(jp2Signature())
	f.Add(minimalJP2())
	f.Add(minimalJP2()[:len(minimalJP2())/2])
	f.Add(minimalJ2K())
	// A box declaring length 0, which per ISO 15444-1 means "to end of file":
	// the walker must not read it as an empty box and spin.
	f.Add(append(jp2Signature(), []byte{0x00, 0x00, 0x00, 0x00, 'j', 'p', '2', 'c'}...))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_ = isJP2Container(data)
		for _, part := range []int{2, 3, 4} {
			rules := jpxRulesForPart(part)
			for _, hasCS := range []bool{false, true} {
				_ = checkJP2Container(data, "Im0", 1, rules, hasCS)
			}
			_ = checkRawJ2K(data, "Im0", 1, rules)
		}
	})
}

// jp2Signature is the 12-byte JP2 signature box every container starts with.
func jp2Signature() []byte {
	return []byte{0x00, 0x00, 0x00, 0x0C, 'j', 'P', ' ', ' ', 0x0D, 0x0A, 0x87, 0x0A}
}

// minimalJP2 builds a container the box walker can traverse: the signature, a
// file-type box, and a header box holding an image header and a colour
// specification, so the colour checks have something to judge.
func minimalJP2() []byte {
	box := func(tag string, payload []byte) []byte {
		out := make([]byte, 8, 8+len(payload))
		binary.BigEndian.PutUint32(out[0:], uint32(8+len(payload)))
		copy(out[4:], tag)
		return append(out, payload...)
	}

	ftyp := box("ftyp", append([]byte("jp2 "), 0x00, 0x00, 0x00, 0x00, 'j', 'p', '2', ' '))

	ihdr := make([]byte, 14)
	binary.BigEndian.PutUint32(ihdr[0:], 1) // height
	binary.BigEndian.PutUint32(ihdr[4:], 1) // width
	binary.BigEndian.PutUint16(ihdr[8:], 1) // component count
	ihdr[10] = 7                            // bits per component - 1

	// colr: METH 1 (enumerated), PREC 0, APPROX 0, enumCS 17 (greyscale).
	colr := make([]byte, 7)
	colr[0] = 1
	binary.BigEndian.PutUint32(colr[3:], 17)

	jp2h := box("jp2h", append(box("ihdr", ihdr), box("colr", colr)...))

	out := jp2Signature()
	out = append(out, ftyp...)
	out = append(out, jp2h...)
	return append(out, box("jp2c", minimalJ2K())...)
}

// minimalJ2K is a raw codestream: SOC, then a SIZ marker declaring a 1x1 image
// with one component, which is what the raw-codestream check reads.
func minimalJ2K() []byte {
	siz := make([]byte, 41)
	binary.BigEndian.PutUint16(siz[0:], 0xFF51) // SIZ marker
	binary.BigEndian.PutUint16(siz[2:], uint16(len(siz)-2))
	binary.BigEndian.PutUint32(siz[6:], 1)  // Xsiz
	binary.BigEndian.PutUint32(siz[10:], 1) // Ysiz
	binary.BigEndian.PutUint16(siz[38:], 1) // Csiz: one component
	siz[40] = 7                             // Ssiz: 8-bit unsigned

	out := []byte{0xFF, 0x4F} // SOC
	return append(out, siz...)
}

// TestMinimalJP2IsWalked pins that the container seed is recognised and
// traversed, rather than rejected at the signature and leaving the box walk
// unexercised.
func TestMinimalJP2IsWalked(t *testing.T) {
	t.Parallel()

	data := minimalJP2()
	if !isJP2Container(data) {
		t.Fatal("the seed was not recognised as a JP2 container")
	}
	boxes := parseJP2Boxes(data)
	if len(boxes) == 0 {
		t.Fatal("the box walker found no boxes in the seed")
	}
	var sawHeader bool
	for _, b := range boxes {
		if b.boxType == boxTypeJP2H {
			sawHeader = true
		}
	}
	if !sawHeader {
		t.Errorf("the header box was not found, got %d boxes", len(boxes))
	}
}
