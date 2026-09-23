package jpx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"testing"
)

// boxOf builds a box with a 4-byte LBox (T.800, I.4).
func boxOf(kind uint32, body ...[]byte) []byte {
	content := slices.Concat(body...)
	b := binary.BigEndian.AppendUint32(nil, uint32(8+len(content)))
	b = binary.BigEndian.AppendUint32(b, kind)
	return append(b, content...)
}

var (
	signatureBox = boxOf(boxSignature, signature)
	fileTypeBox  = boxOf(0x66747970, []byte("jp2 \x00\x00\x00\x00jp2 "))
	// imageHeaderBox describes the J.10 image: 9 high, 1 wide, one 8-bit
	// component.
	imageHeaderBox = boxOf(0x69686472, []byte{0, 0, 0, 9, 0, 0, 0, 1, 0, 1, 7, 7, 0, 0})
	greyBox        = boxOf(boxColour, []byte{methodEnumerated, 0, 0, 0, 0, 0, 17})
)

// TestReadFile_JP2 reads the header boxes of a JP2 file that wraps the J.10
// codestream: a vendor colour box the reader must skip (T.800, Table I.9),
// then the enumerated greyscale box it uses, an ICC box it must ignore as not
// the first known one (I.3.3), a palette with an 8-bit and a signed 10-bit
// column (I.5.3.4), its component mapping and a channel definition.
func TestReadFile_JP2(t *testing.T) {
	t.Parallel()

	vendor := boxOf(boxColour, []byte{4, 0, 0, 1, 2, 3, 4})
	icc := boxOf(boxColour, []byte{methodICC, 0, 0, 'I', 'C', 'C'})
	// Two entries: (0x12, -1) and (0xFE, 0x1FF).
	pclr := boxOf(boxPalette, []byte{0, 2, 2, 0x07, 0x89, 0x12, 0x03, 0xFF, 0xFE, 0x01, 0xFF})
	cmap := boxOf(boxComponentMapping, []byte{0, 0, 1, 0, 0, 0, 1, 1})
	cdef := boxOf(boxChannelDefinition, []byte{0, 2, 0, 0, 0, 0, 0, 1, 0, 1, 0, 1, 0, 0})
	header := boxOf(boxHeader, imageHeaderBox, vendor, greyBox, icc, pclr, cmap, cdef)
	file := slices.Concat(signatureBox, fileTypeBox, header, boxOf(boxCodestream, annexJ10))

	stream, hdr, err := readFile(file)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	if !bytes.Equal(stream, annexJ10) {
		t.Errorf("codestream differs from the one wrapped")
	}
	if hdr.colour.method != methodEnumerated || hdr.colour.enumerated != 17 {
		t.Errorf("colour = %+v, want enumerated greyscale", hdr.colour)
	}
	p := hdr.palette
	if p == nil || p.entries != 2 || len(p.columns) != 2 {
		t.Fatalf("palette = %+v", p)
	}
	if c := p.columns[0]; c.precision != 8 || c.signed || !slices.Equal(c.values, []int64{0x12, 0xFE}) {
		t.Errorf("palette column 0 = %+v", c)
	}
	if c := p.columns[1]; c.precision != 10 || !c.signed || !slices.Equal(c.values, []int64{-1, 0x1FF}) {
		t.Errorf("palette column 1 = %+v", c)
	}
	if want := []componentMapping{{component: 0, viaPalette: true}, {component: 0, viaPalette: true, column: 1}}; !slices.Equal(hdr.mapping, want) {
		t.Errorf("mapping = %+v, want %+v", hdr.mapping, want)
	}
	if want := []channelDefinition{{channel: 0, kind: 0, association: 1}, {channel: 1, kind: 1, association: 0}}; !slices.Equal(hdr.channels, want) {
		t.Errorf("channels = %+v, want %+v", hdr.channels, want)
	}
}

// TestReadFile_BoxLengths reads a JP2 file whose header box uses the 8-byte
// XLBox and whose codestream box has LBox 0, running to the end of the file
// (T.800, I.4).
func TestReadFile_BoxLengths(t *testing.T) {
	t.Parallel()

	content := slices.Concat(imageHeaderBox, greyBox)
	header := binary.BigEndian.AppendUint32(nil, 1)
	header = binary.BigEndian.AppendUint32(header, boxHeader)
	header = binary.BigEndian.AppendUint64(header, uint64(16+len(content)))
	header = append(header, content...)
	last := binary.BigEndian.AppendUint32(nil, 0)
	last = binary.BigEndian.AppendUint32(last, boxCodestream)
	file := slices.Concat(signatureBox, fileTypeBox, header, last, annexJ10)

	stream, hdr, err := readFile(file)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	if !bytes.Equal(stream, annexJ10) || hdr.colour.enumerated != 17 {
		t.Errorf("stream of %d bytes, colour %+v", len(stream), hdr.colour)
	}
}

// TestReadFile_Codestream accepts a bare codestream, which has no JP2 header.
func TestReadFile_Codestream(t *testing.T) {
	t.Parallel()

	stream, hdr, err := readFile(annexJ10)
	if err != nil || hdr != nil || !bytes.Equal(stream, annexJ10) {
		t.Errorf("readFile = %d bytes, %+v, %v", len(stream), hdr, err)
	}
}

// TestReadFile_Rejects checks that files breaking a rule of T.800, Annex I
// are refused as malformed.
func TestReadFile_Rejects(t *testing.T) {
	t.Parallel()

	header := boxOf(boxHeader, imageHeaderBox, greyBox)
	codestream := boxOf(boxCodestream, annexJ10)
	reserved := slices.Clone(codestream)
	binary.BigEndian.PutUint32(reserved, 2)
	overlong := slices.Clone(codestream)
	binary.BigEndian.PutUint32(overlong, uint32(len(codestream)+1))
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"no signature", slices.Concat(fileTypeBox, header, codestream)},
		{"wrong signature", slices.Concat(boxOf(boxSignature, []byte{0x0D, 0x0A, 0x87, 0x0B}), fileTypeBox, header, codestream)},
		{"codestream before the header", slices.Concat(signatureBox, fileTypeBox, codestream, header)},
		{"no codestream", slices.Concat(signatureBox, fileTypeBox, header)},
		{"two headers", slices.Concat(signatureBox, fileTypeBox, header, header, codestream)},
		{"reserved LBox", slices.Concat(signatureBox, fileTypeBox, header, reserved)},
		{"box past the end", slices.Concat(signatureBox, fileTypeBox, header, overlong)},
		{"stray bytes", slices.Concat(signatureBox, fileTypeBox, header, codestream, []byte{0, 0, 0})},
		{"palette of 1025 entries", slices.Concat(signatureBox, fileTypeBox, boxOf(boxHeader, imageHeaderBox, greyBox, boxOf(boxPalette, []byte{0x04, 0x01, 1, 7}), boxOf(boxComponentMapping, []byte{0, 0, 1, 0})), codestream)},
		{"palette shorter than its entries", slices.Concat(signatureBox, fileTypeBox, boxOf(boxHeader, imageHeaderBox, greyBox, boxOf(boxPalette, []byte{0, 2, 1, 7, 0}), boxOf(boxComponentMapping, []byte{0, 0, 1, 0})), codestream)},
		{"mapping type 2", slices.Concat(signatureBox, fileTypeBox, boxOf(boxHeader, imageHeaderBox, greyBox, boxOf(boxPalette, []byte{0, 1, 1, 7, 0}), boxOf(boxComponentMapping, []byte{0, 0, 2, 0})), codestream)},
		{"channel definition shorter than its count", slices.Concat(signatureBox, fileTypeBox, boxOf(boxHeader, imageHeaderBox, greyBox, boxOf(boxChannelDefinition, []byte{0, 2, 0, 0, 0, 0, 0, 1})), codestream)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, _, err := readFile(tc.data); !errors.Is(err, ErrMalformed) {
				t.Errorf("readFile = %v, want %v", err, ErrMalformed)
			}
		})
	}
}
