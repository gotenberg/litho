package jpx

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Box types, ITU-T T.800, Table I.2. The exported ones are those a reader of
// the file structure needs beside the decoder, such as the PDF/A checks of
// ISO 19005-2, 6.2.8.3.
const (
	boxSignature         = 0x6A502020 // 'jP\040\040'
	BoxHeader            = 0x6A703268 // 'jp2h'
	BoxImageHeader       = 0x69686472 // 'ihdr'
	BoxBitsPerComponent  = 0x62706363 // 'bpcc'
	BoxColour            = 0x636F6C72 // 'colr'
	boxPalette           = 0x70636C72 // 'pclr'
	boxComponentMapping  = 0x636D6170 // 'cmap'
	boxChannelDefinition = 0x63646566 // 'cdef'
	boxCodestream        = 0x6A703263 // 'jp2c'
)

// signature is the content of the JPEG 2000 Signature box (T.800, I.5.1).
var signature = []byte{0x0D, 0x0A, 0x87, 0x0A}

// wholeSignatureBox is the whole Signature box, LBox 12 included, as T.800, I.5.1
// fixes it.
var wholeSignatureBox = []byte{0x00, 0x00, 0x00, 0x0C, 0x6A, 0x50, 0x20, 0x20, 0x0D, 0x0A, 0x87, 0x0A}

// codestreamStart is the SOC marker followed by the SIZ marker, which T.800,
// A.4.1 and A.5.1 require at the start of every codestream.
var codestreamStart = []byte{0xFF, 0x4F, 0xFF, 0x51}

// IsJP2 reports whether data starts with the 12-byte Signature box of a JP2
// file (T.800, I.5.1).
func IsJP2(data []byte) bool {
	return bytes.HasPrefix(data, wholeSignatureBox)
}

// Box is one box of a JP2 file (T.800, I.4).
type Box struct {
	// Type is TBox, the box type.
	Type uint32
	// Offset is where the box header starts in the bytes it was read from.
	Offset int
	// Body is the box contents, after LBox, TBox and XLBox.
	Body []byte
}

// ReadBoxes splits b into consecutive boxes (T.800, I.4). A box whose LBox is
// 0 extends to the end of b.
//
// When a box header is malformed, ReadBoxes returns the boxes before it with
// an error wrapping [ErrMalformed], so a reader that judges the file structure
// rather than decoding it can still look at what precedes the damage.
//
// The loop is bounded by len(b): every box header is at least 8 bytes and
// each iteration consumes one.
func ReadBoxes(b []byte) ([]Box, error) {
	var boxes []Box
	for off := 0; off < len(b); {
		rest := b[off:]
		if len(rest) < 8 {
			return boxes, fmt.Errorf("%w: %d stray bytes after the last box", ErrMalformed, len(rest))
		}
		length := uint64(binary.BigEndian.Uint32(rest))
		kind := binary.BigEndian.Uint32(rest[4:])
		head := uint64(8)
		switch length {
		case 0:
			length = uint64(len(rest))
		case 1:
			if len(rest) < 16 {
				return boxes, fmt.Errorf("%w: box at offset %d has a truncated XLBox", ErrMalformed, off)
			}
			length = binary.BigEndian.Uint64(rest[8:])
			head = 16
		}
		// Per T.800, Table I.1, LBox 2 to 7 are reserved and any length counts
		// the header it sits in.
		if length < head || length > uint64(len(rest)) {
			return boxes, fmt.Errorf("%w: box %q at offset %d declares %d bytes, %d remain", ErrMalformed, boxName(kind), off, length, len(rest))
		}
		whole := rest[:length]
		boxes = append(boxes, Box{Type: kind, Offset: off, Body: whole[head:]})
		off += len(whole)
	}
	return boxes, nil
}

// boxName renders a box type as its four characters.
func boxName(kind uint32) string {
	return string(binary.BigEndian.AppendUint32(nil, kind))
}

// ImageHeader holds the fields of an Image Header box (T.800, I.5.3.1).
type ImageHeader struct {
	Height, Width uint32
	// Components is NC, the number of components.
	Components int
	// BPC is the bits-per-component byte: the depth minus one in the low
	// seven bits and the sign in the high bit, or 0xFF when the depths
	// differ and a Bits Per Component box states them (T.800, Table I.5).
	BPC byte
	// Compression, UnknownColourspace and IPR are C, UnkC and IPR.
	Compression, UnknownColourspace, IPR byte
}

// imageHeaderBytes is the size of an Image Header box's contents (T.800,
// I.5.3.1).
const imageHeaderBytes = 14

// ReadImageHeader reads the contents of an Image Header box (T.800, I.5.3.1).
// Bytes past the 14 the box holds are ignored.
func ReadImageHeader(body []byte) (ImageHeader, error) {
	if len(body) < imageHeaderBytes {
		return ImageHeader{}, fmt.Errorf("%w: Image Header box of %d bytes", ErrMalformed, len(body))
	}
	return ImageHeader{
		Height:             binary.BigEndian.Uint32(body),
		Width:              binary.BigEndian.Uint32(body[4:]),
		Components:         int(binary.BigEndian.Uint16(body[8:])),
		BPC:                body[10],
		Compression:        body[11],
		UnknownColourspace: body[12],
		IPR:                body[13],
	}, nil
}

// ColourSpec holds the fields of one Colour Specification box, whatever its
// method (T.800, I.5.3.3; ISO/IEC 15444-2, M.11.7).
type ColourSpec struct {
	// Method is METH.
	Method int
	// Precedence is PREC, a signed byte (ISO/IEC 15444-2, Table M.24).
	Precedence int
	// Approximation is APPROX.
	Approximation int
	// EnumCS is the enumerated colour space of a method 1 box that holds
	// one, and 0 otherwise.
	EnumCS uint32
	// Params is what follows APPROX: EnumCS and the EP fields for method 1,
	// the ICC profile for methods 2 and 3.
	Params []byte
}

// ReadColourSpec reads the fields of a Colour Specification box (T.800,
// I.5.3.3). It fails only on a box too short for METH, PREC and APPROX: what
// a method requires of the rest is for the reader of that method to check.
func ReadColourSpec(body []byte) (ColourSpec, error) {
	if len(body) < 3 {
		return ColourSpec{}, fmt.Errorf("%w: Colour Specification box of %d bytes", ErrMalformed, len(body))
	}
	c := ColourSpec{Method: int(body[0]), Precedence: int(body[1]), Approximation: int(body[2]), Params: body[3:]}
	if c.Precedence >= 128 {
		c.Precedence -= 256
	}
	if c.Method == methodEnumerated && len(c.Params) >= 4 {
		c.EnumCS = binary.BigEndian.Uint32(c.Params)
	}
	return c, nil
}

// fileHeader holds what the JP2 Header box says about turning codestream
// components into image channels (T.800, I.5.3).
type fileHeader struct {
	// colours holds the Colour Specification boxes whose method this
	// package knows, in file order.
	colours []colourSpec
	// palette is the Palette box, or nil.
	palette *palette
	// mapping is the Component Mapping box, or nil, which maps component i
	// to channel i (T.800, I.5.3.5).
	mapping []componentMapping
	// channels is the Channel Definition box, or nil (T.800, I.5.3.6).
	channels []channelDefinition
}

// Colour specification methods, T.800 Table I.9. ISO/IEC 15444-2 adds 3 (any
// ICC profile) and 4 (vendor colour), which ISO 32000-1, 7.4.9 admits.
const (
	methodEnumerated = 1
	methodICC        = 2
	methodAnyICC     = 3
)

// colourSpec is one Colour Specification box (T.800, I.5.3.3) of a method
// this package knows. PREC and APPROX, which a JP2 file sets to 0, rank a JPX
// file's boxes (ISO/IEC 15444-2, M.11.7.2).
type colourSpec struct {
	ColourSpec
	// profile is the ICC profile when Method is methodICC or methodAnyICC.
	profile []byte
	// lab holds the EP fields of an enumerated CIELab specification.
	lab *LabParams
}

// labEPBytes is the size of the EP fields of an enumerated CIELab
// specification: RL, OL, RA, OA, RB, OB and IL, four bytes each (ISO/IEC
// 15444-2, M.11.7.4.1).
const labEPBytes = 7 * 4

// palette is a Palette box (T.800, I.5.3.4): entries rows of one value per
// column.
type palette struct {
	entries int
	columns []paletteColumn
}

// paletteColumn is one column of a palette.
type paletteColumn struct {
	precision int
	signed    bool
	// values holds one value per palette entry.
	values []int64
}

// componentMapping is one channel of a Component Mapping box (T.800,
// I.5.3.5): the component it comes from and, for a palette mapping, the
// palette column it is looked up in.
type componentMapping struct {
	component  int
	viaPalette bool
	column     int
}

// channelDefinition is one description of a Channel Definition box (T.800,
// I.5.3.6).
type channelDefinition struct {
	channel     int
	kind        int
	association int
}

// readFile finds the codestream in data and the JP2 header describing it.
// data is either a JP2 file (T.800, Annex I) or a bare codestream (Annex A),
// in which case the header is nil.
//
// ISO 32000-1, 7.4.9 says the filter shall expect a full JPX file structure.
// A bare codestream is accepted anyway: real documents embed such streams
// and OpenJPEG decodes them, and a codestream carries every parameter
// decoding needs, so reading one cannot misinterpret a conforming file.
//
// # Corpus evidence
//
//   - All corpora: 3 streams are bare codestreams, and OpenJPEG decodes
//     them.
func readFile(data []byte) ([]byte, *fileHeader, error) {
	if bytes.HasPrefix(data, codestreamStart) {
		return data, nil, nil
	}
	boxes, err := ReadBoxes(data)
	if err != nil {
		return nil, nil, err
	}
	// T.800, I.5.1: the signature box is first, 12 bytes long.
	if len(boxes) == 0 || boxes[0].Type != boxSignature || !bytes.Equal(boxes[0].Body, signature) {
		return nil, nil, fmt.Errorf("%w: data is neither a JP2 file nor a codestream", ErrMalformed)
	}
	var (
		hdr        *fileHeader
		codestream []byte
	)
	for _, b := range boxes[1:] {
		switch b.Type {
		case BoxHeader:
			// T.800, I.5.3: one JP2 Header box, before the codestream.
			if hdr != nil {
				return nil, nil, fmt.Errorf("%w: more than one JP2 Header box", ErrMalformed)
			}
			if hdr, err = readHeader(b.Body); err != nil {
				return nil, nil, err
			}
		case boxCodestream:
			// T.800, I.2.2 allows several codestreams and a JP2 reader decodes
			// the first.
			if codestream == nil {
				codestream = b.Body
			}
		}
		if codestream != nil {
			break
		}
	}
	if hdr == nil {
		return nil, nil, fmt.Errorf("%w: no JP2 Header box before the codestream", ErrMalformed)
	}
	if codestream == nil {
		return nil, nil, fmt.Errorf("%w: no Contiguous Codestream box", ErrMalformed)
	}
	return codestream, hdr, nil
}

// readHeader reads the boxes of a JP2 Header box (T.800, I.5.3).
//
// # Corpus evidence
//
//   - govdocs: six streams put the Component Mapping box outside the JP2
//     Header box. Each is an /Indexed image, and pdf.js decodes them.
func readHeader(body []byte) (*fileHeader, error) {
	boxes, err := ReadBoxes(body)
	if err != nil {
		return nil, fmt.Errorf("JP2 Header box: %w", err)
	}
	hdr := &fileHeader{}
	for _, b := range boxes {
		switch b.Type {
		case BoxColour:
			c, ok, err := readColour(b.Body)
			if err != nil {
				return nil, err
			}
			// T.800, Table I.9: a reader ignores a box whose method it does
			// not know.
			if ok {
				hdr.colours = append(hdr.colours, c)
			}
		case boxPalette:
			if hdr.palette != nil {
				return nil, fmt.Errorf("%w: more than one Palette box", ErrMalformed)
			}
			if hdr.palette, err = readPalette(b.Body); err != nil {
				return nil, err
			}
		case boxComponentMapping:
			if hdr.mapping != nil {
				return nil, fmt.Errorf("%w: more than one Component Mapping box", ErrMalformed)
			}
			if hdr.mapping, err = readMapping(b.Body); err != nil {
				return nil, err
			}
		case boxChannelDefinition:
			if hdr.channels != nil {
				return nil, fmt.Errorf("%w: more than one Channel Definition box", ErrMalformed)
			}
			if hdr.channels, err = readChannels(b.Body); err != nil {
				return nil, err
			}
		}
	}
	// T.800, I.5.3.4 and I.5.3.5 pair a Palette box with a Component Mapping
	// box. A header holding one without the other is read anyway: without a
	// mapping, I.5.3.5 maps each component directly to a channel, and a
	// mapping without a palette fails only for a channel that looks one up
	// (see Image.Channels). Producers put the Component Mapping box of an
	// /Indexed image outside the JP2 Header box, and ISO 32000-1, 7.4.9 reads
	// its index samples with the JPEG 2000 colour specification ignored,
	// palette included (T.800, I.3.4); pdf.js decodes such images.
	return hdr, nil
}

// readColour reads a Colour Specification box (T.800, I.5.3.3) and reports
// whether its method is one this package knows.
func readColour(body []byte) (colourSpec, bool, error) {
	spec, err := ReadColourSpec(body)
	if err != nil {
		return colourSpec{}, false, err
	}
	c := colourSpec{ColourSpec: spec}
	switch c.Method {
	case methodEnumerated:
		if len(body) < 7 {
			return colourSpec{}, false, fmt.Errorf("%w: enumerated Colour Specification box of %d bytes", ErrMalformed, len(body))
		}
		if c.EnumCS == ColourCIELab {
			c.lab = readLabEP(body[7:])
		}
	case methodICC, methodAnyICC:
		c.profile = c.Params
	default:
		return colourSpec{}, false, nil
	}
	return c, true, nil
}

// readLabEP reads the EP fields of an enumerated CIELab specification
// (ISO/IEC 15444-2, M.11.7.4.1). A box without them takes the defaults, whose
// offsets depend on the channels' depths and are filled in by the reader of
// the samples (see [DefaultLab]). A box holding some but not all of them
// states no parameters a reader could apply and is read as holding none.
func readLabEP(ep []byte) *LabParams {
	if len(ep) < labEPBytes {
		return &LabParams{Default: true, Illuminant: IlluminantD50}
	}
	u := func(i int) float64 { return float64(binary.BigEndian.Uint32(ep[4*i:])) }
	return &LabParams{
		Range:      [3]float64{u(0), u(2), u(4)},
		Offset:     [3]float64{u(1), u(3), u(5)},
		Illuminant: binary.BigEndian.Uint32(ep[24:]),
	}
}

// maxPaletteEntries is the largest NE a Palette box may state (T.800, Table
// I.12).
const maxPaletteEntries = 1024

// maxPalettePrecision is the deepest palette column T.800, Table I.13 allows,
// 38 bits.
const maxPalettePrecision = 38

// readPalette reads a Palette box (T.800, I.5.3.4).
func readPalette(body []byte) (*palette, error) {
	if len(body) < 3 {
		return nil, fmt.Errorf("%w: Palette box of %d bytes", ErrMalformed, len(body))
	}
	entries := int(binary.BigEndian.Uint16(body))
	columns := int(body[2])
	if entries < 1 || entries > maxPaletteEntries || columns < 1 {
		return nil, fmt.Errorf("%w: palette of %d entries and %d columns", ErrMalformed, entries, columns)
	}
	if len(body) < 3+columns {
		return nil, fmt.Errorf("%w: Palette box of %d bytes holds no depth for each of %d columns", ErrMalformed, len(body), columns)
	}
	p := &palette{entries: entries, columns: make([]paletteColumn, columns)}
	rowBytes := 0
	for i := range p.columns {
		b := body[3+i]
		p.columns[i].precision = int(b&0x7F) + 1
		p.columns[i].signed = b&0x80 != 0
		if p.columns[i].precision > maxPalettePrecision {
			return nil, fmt.Errorf("%w: palette column %d is %d bits deep", ErrMalformed, i, p.columns[i].precision)
		}
		p.columns[i].values = make([]int64, entries)
		rowBytes += (p.columns[i].precision + 7) / 8
	}
	data := body[3+columns:]
	if len(data) < entries*rowBytes {
		return nil, fmt.Errorf("%w: Palette box holds %d bytes of entries, %d wanted", ErrMalformed, len(data), entries*rowBytes)
	}
	// Values are stored entry by entry, each padded to whole bytes with the
	// value in the low-order bits (T.800, I.5.3.4).
	off := 0
	for j := range entries {
		for i := range p.columns {
			col := &p.columns[i]
			// At most five bytes for 38 bits, so v cannot overflow.
			var v int64
			for range (col.precision + 7) / 8 {
				v = v<<8 | int64(data[off])
				off++
			}
			v &= 1<<col.precision - 1
			if col.signed && v>>(col.precision-1) != 0 {
				v -= 1 << col.precision
			}
			col.values[j] = v
		}
	}
	return p, nil
}

// Component mapping types, T.800 Table I.14.
const (
	mappingDirect  = 0
	mappingPalette = 1
)

// readMapping reads a Component Mapping box (T.800, I.5.3.5).
func readMapping(body []byte) ([]componentMapping, error) {
	if len(body) == 0 || len(body)%4 != 0 {
		return nil, fmt.Errorf("%w: Component Mapping box of %d bytes", ErrMalformed, len(body))
	}
	m := make([]componentMapping, len(body)/4)
	for i := range m {
		e := body[4*i:]
		m[i].component = int(binary.BigEndian.Uint16(e))
		switch e[2] {
		case mappingDirect:
		case mappingPalette:
			m[i].viaPalette = true
			m[i].column = int(e[3])
		default:
			return nil, fmt.Errorf("%w: channel %d has mapping type %d", ErrMalformed, i, e[2])
		}
	}
	return m, nil
}

// readChannels reads a Channel Definition box (T.800, I.5.3.6).
func readChannels(body []byte) ([]channelDefinition, error) {
	if len(body) < 2 {
		return nil, fmt.Errorf("%w: Channel Definition box of %d bytes", ErrMalformed, len(body))
	}
	n := int(binary.BigEndian.Uint16(body))
	if n == 0 || len(body) < 2+6*n {
		return nil, fmt.Errorf("%w: Channel Definition box of %d bytes for %d descriptions", ErrMalformed, len(body), n)
	}
	defs := make([]channelDefinition, n)
	for i := range defs {
		e := body[2+6*i:]
		defs[i] = channelDefinition{
			channel:     int(binary.BigEndian.Uint16(e)),
			kind:        int(binary.BigEndian.Uint16(e[2:])),
			association: int(binary.BigEndian.Uint16(e[4:])),
		}
	}
	return defs, nil
}
