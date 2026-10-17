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
	imageHeaderBox = boxOf(BoxImageHeader, []byte{0, 0, 0, 9, 0, 0, 0, 1, 0, 1, 7, 7, 0, 0})
	greyBox        = boxOf(BoxColour, []byte{methodEnumerated, 0, 0, 0, 0, 0, 17})
)

// TestReadFile_JP2 reads the header boxes of a JP2 file that wraps the J.10
// codestream: a vendor colour box the reader must skip (T.800, Table I.9),
// then an enumerated greyscale box and an ICC box it keeps in that order, a
// palette with an 8-bit and a signed 10-bit column (I.5.3.4), its component
// mapping and a channel definition.
func TestReadFile_JP2(t *testing.T) {
	t.Parallel()

	vendor := boxOf(BoxColour, []byte{4, 0, 0, 1, 2, 3, 4})
	icc := boxOf(BoxColour, []byte{methodICC, 0, 0, 'I', 'C', 'C'})
	// Two entries: (0x12, -1) and (0xFE, 0x1FF).
	pclr := boxOf(boxPalette, []byte{0, 2, 2, 0x07, 0x89, 0x12, 0x03, 0xFF, 0xFE, 0x01, 0xFF})
	cmap := boxOf(boxComponentMapping, []byte{0, 0, 1, 0, 0, 0, 1, 1})
	cdef := boxOf(boxChannelDefinition, []byte{0, 2, 0, 0, 0, 0, 0, 1, 0, 1, 0, 1, 0, 0})
	header := boxOf(BoxHeader, imageHeaderBox, vendor, greyBox, icc, pclr, cmap, cdef)
	file := slices.Concat(signatureBox, fileTypeBox, header, boxOf(boxCodestream, annexJ10))

	stream, hdr, err := readFile(file)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	if !bytes.Equal(stream, annexJ10) {
		t.Errorf("codestream differs from the one wrapped")
	}
	if len(hdr.colours) != 2 || hdr.colours[0].Method != methodEnumerated || hdr.colours[0].EnumCS != 17 ||
		hdr.colours[1].Method != methodICC || string(hdr.colours[1].profile) != "ICC" {
		t.Errorf("colours = %+v, want enumerated greyscale then the ICC profile", hdr.colours)
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
	header = binary.BigEndian.AppendUint32(header, BoxHeader)
	header = binary.BigEndian.AppendUint64(header, uint64(16+len(content)))
	header = append(header, content...)
	last := binary.BigEndian.AppendUint32(nil, 0)
	last = binary.BigEndian.AppendUint32(last, boxCodestream)
	file := slices.Concat(signatureBox, fileTypeBox, header, last, annexJ10)

	stream, hdr, err := readFile(file)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	if !bytes.Equal(stream, annexJ10) || len(hdr.colours) != 1 || hdr.colours[0].EnumCS != 17 {
		t.Errorf("stream of %d bytes, colours %+v", len(stream), hdr.colours)
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

	header := boxOf(BoxHeader, imageHeaderBox, greyBox)
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
		{"palette of 1025 entries", slices.Concat(signatureBox, fileTypeBox, boxOf(BoxHeader, imageHeaderBox, greyBox, boxOf(boxPalette, []byte{0x04, 0x01, 1, 7}), boxOf(boxComponentMapping, []byte{0, 0, 1, 0})), codestream)},
		{"palette shorter than its entries", slices.Concat(signatureBox, fileTypeBox, boxOf(BoxHeader, imageHeaderBox, greyBox, boxOf(boxPalette, []byte{0, 2, 1, 7, 0}), boxOf(boxComponentMapping, []byte{0, 0, 1, 0})), codestream)},
		{"mapping type 2", slices.Concat(signatureBox, fileTypeBox, boxOf(BoxHeader, imageHeaderBox, greyBox, boxOf(boxPalette, []byte{0, 1, 1, 7, 0}), boxOf(boxComponentMapping, []byte{0, 0, 2, 0})), codestream)},
		{"channel definition shorter than its count", slices.Concat(signatureBox, fileTypeBox, boxOf(BoxHeader, imageHeaderBox, greyBox, boxOf(boxChannelDefinition, []byte{0, 2, 0, 0, 0, 0, 0, 1})), codestream)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, _, err := readFile(tc.data); !errors.Is(err, ErrMalformed) {
				t.Errorf("readFile = %v, want %v", err, ErrMalformed)
			}
		})
	}
}

// TestIsJP2 pins that [IsJP2] accepts the 12-byte Signature box and rejects
// anything shorter or different.
func TestIsJP2(t *testing.T) {
	t.Parallel()

	sig := []byte{0x00, 0x00, 0x00, 0x0C, 0x6A, 0x50, 0x20, 0x20, 0x0D, 0x0A, 0x87, 0x0A}
	for _, tc := range []struct {
		name string
		data []byte
		want bool
	}{
		{"valid signature", sig, true},
		{"valid with trailing data", append(slices.Clone(sig), 0x00, 0x00), true},
		{"too short", sig[:11], false},
		{"empty", nil, false},
		{"wrong byte 4", []byte{0x00, 0x00, 0x00, 0x0C, 0xFF, 0x50, 0x20, 0x20, 0x0D, 0x0A, 0x87, 0x0A}, false},
		{"all zeros", make([]byte, 12), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsJP2(tc.data); got != tc.want {
				t.Errorf("IsJP2(%v) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

// TestReadBoxes pins how [ReadBoxes] splits a box sequence: a zero length
// reaches to the end, and a malformed header stops the walk, returning the
// boxes before it with an error.
func TestReadBoxes(t *testing.T) {
	t.Parallel()

	first := boxOf(0x11111111, []byte{0xAA, 0xBB, 0xCC, 0xDD})
	second := boxOf(0x22222222)
	toEnd := binary.BigEndian.AppendUint32([]byte{0, 0, 0, 0}, 0x74657374)
	toEnd = append(toEnd, make([]byte, 8)...)
	overlong := binary.BigEndian.AppendUint32([]byte{0, 0, 0, 100}, 0x74657374)
	extended := binary.BigEndian.AppendUint32([]byte{0, 0, 0, 1}, 0x74657374)
	extended = binary.BigEndian.AppendUint64(extended, 20)
	extended = append(extended, 1, 2, 3, 4)

	for _, tc := range []struct {
		name    string
		data    []byte
		types   []uint32
		offsets []int
		bodies  []int
		wantErr bool
	}{
		{"empty", nil, nil, nil, nil, false},
		{"two boxes", slices.Concat(first, second), []uint32{0x11111111, 0x22222222}, []int{0, 12}, []int{4, 0}, false},
		{"zero length reaches to the end", toEnd, []uint32{0x74657374}, []int{0}, []int{8}, false},
		{"extended length", extended, []uint32{0x74657374}, []int{0}, []int{4}, false},
		{"length past the data", overlong, nil, nil, nil, true},
		{"box past the data after a good one", slices.Concat(first, overlong), []uint32{0x11111111}, []int{0}, []int{4}, true},
		{"stray bytes after a good one", slices.Concat(first, []byte{0, 0}), []uint32{0x11111111}, []int{0}, []int{4}, true},
		{"truncated XLBox", slices.Concat(first, []byte{0, 0, 0, 1, 't', 'e', 's', 't', 0}), []uint32{0x11111111}, []int{0}, []int{4}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			boxes, err := ReadBoxes(tc.data)
			if gotErr := err != nil; gotErr != tc.wantErr || (gotErr && !errors.Is(err, ErrMalformed)) {
				t.Fatalf("ReadBoxes error = %v, want an error: %v", err, tc.wantErr)
			}
			if len(boxes) != len(tc.types) {
				t.Fatalf("ReadBoxes found %d boxes, want %d", len(boxes), len(tc.types))
			}
			for i, b := range boxes {
				if b.Type != tc.types[i] || b.Offset != tc.offsets[i] || len(b.Body) != tc.bodies[i] {
					t.Errorf("box %d = %q at %d with %d bytes, want %q at %d with %d bytes",
						i, boxName(b.Type), b.Offset, len(b.Body), boxName(tc.types[i]), tc.offsets[i], tc.bodies[i])
				}
			}
		})
	}
}

// TestReadColourSpec pins how [ReadColourSpec] reads METH, PREC, APPROX and
// EnumCS whatever the method, and rejects a box too short for them.
func TestReadColourSpec(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body []byte
		want ColourSpec
	}{
		{"enumerated sRGB", []byte{0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x10}, ColourSpec{Method: 1, Approximation: 1, EnumCS: ColourSRGB}},
		{"enumerated without EnumCS", []byte{0x01, 0x00, 0x00, 0x00}, ColourSpec{Method: 1}},
		{"ICC", []byte{0x02, 0x00, 0x00, 'I', 'C', 'C'}, ColourSpec{Method: 2}},
		{"vendor method", []byte{0x04, 0x00, 0x00}, ColourSpec{Method: 4}},
		{"negative precedence", []byte{0x01, 0xFF, 0x02, 0x00, 0x00, 0x00, 0x11}, ColourSpec{Method: 1, Precedence: -1, Approximation: 2, EnumCS: ColourGreyscale}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := ReadColourSpec(tc.body)
			if err != nil {
				t.Fatalf("ReadColourSpec: %v", err)
			}
			if c.Method != tc.want.Method || c.Precedence != tc.want.Precedence || c.Approximation != tc.want.Approximation || c.EnumCS != tc.want.EnumCS {
				t.Errorf("ReadColourSpec = %+v, want %+v", c, tc.want)
			}
			if !bytes.Equal(c.Params, tc.body[3:]) {
				t.Errorf("Params = %v, want %v", c.Params, tc.body[3:])
			}
		})
	}
	if _, err := ReadColourSpec([]byte{0x01, 0x00}); !errors.Is(err, ErrMalformed) {
		t.Errorf("ReadColourSpec of 2 bytes = %v, want %v", err, ErrMalformed)
	}
}

// TestReadImageHeader pins the fields [ReadImageHeader] reads and that it
// rejects a box shorter than 14 bytes.
func TestReadImageHeader(t *testing.T) {
	t.Parallel()

	h, err := ReadImageHeader(imageHeaderBox[8:])
	if err != nil {
		t.Fatalf("ReadImageHeader: %v", err)
	}
	if h.Height != 9 || h.Width != 1 || h.Components != 1 || h.BPC != 7 || h.Compression != 7 {
		t.Errorf("ReadImageHeader = %+v, want 9x1, one 8-bit component, C 7", h)
	}
	if _, err := ReadImageHeader(imageHeaderBox[8:21]); !errors.Is(err, ErrMalformed) {
		t.Errorf("ReadImageHeader of 13 bytes = %v, want %v", err, ErrMalformed)
	}
}
