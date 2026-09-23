package jpx

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Box types, ITU-T T.800, Table I.2.
const (
	boxSignature         = 0x6A502020 // 'jP\040\040'
	boxHeader            = 0x6A703268 // 'jp2h'
	boxColour            = 0x636F6C72 // 'colr'
	boxPalette           = 0x70636C72 // 'pclr'
	boxComponentMapping  = 0x636D6170 // 'cmap'
	boxChannelDefinition = 0x63646566 // 'cdef'
	boxCodestream        = 0x6A703263 // 'jp2c'
)

// signature is the content of the JPEG 2000 Signature box (T.800, I.5.1).
var signature = []byte{0x0D, 0x0A, 0x87, 0x0A}

// codestreamStart is the SOC marker followed by the SIZ marker, which T.800,
// A.4.1 and A.5.1 require at the start of every codestream.
var codestreamStart = []byte{0xFF, 0x4F, 0xFF, 0x51}

// box is one box of a JP2 file (T.800, I.4): its type and its contents.
type box struct {
	kind uint32
	body []byte
}

// readBoxes splits b into consecutive boxes (T.800, I.4). A box whose LBox is
// 0 extends to the end of b.
//
// The loop is bounded by len(b): every box header is at least 8 bytes and
// each iteration consumes one.
func readBoxes(b []byte) ([]box, error) {
	var boxes []box
	for off := 0; off < len(b); {
		rest := b[off:]
		if len(rest) < 8 {
			return nil, fmt.Errorf("%w: %d stray bytes after the last box", ErrMalformed, len(rest))
		}
		length := uint64(binary.BigEndian.Uint32(rest))
		kind := binary.BigEndian.Uint32(rest[4:])
		head := uint64(8)
		switch length {
		case 0:
			length = uint64(len(rest))
		case 1:
			if len(rest) < 16 {
				return nil, fmt.Errorf("%w: box at offset %d has a truncated XLBox", ErrMalformed, off)
			}
			length = binary.BigEndian.Uint64(rest[8:])
			head = 16
		}
		// Per T.800, Table I.1, LBox 2 to 7 are reserved and any length counts
		// the header it sits in.
		if length < head || length > uint64(len(rest)) {
			return nil, fmt.Errorf("%w: box %q at offset %d declares %d bytes, %d remain", ErrMalformed, boxName(kind), off, length, len(rest))
		}
		whole := rest[:length]
		boxes = append(boxes, box{kind: kind, body: whole[head:]})
		off += len(whole)
	}
	return boxes, nil
}

// boxName renders a box type as its four characters.
func boxName(kind uint32) string {
	return string(binary.BigEndian.AppendUint32(nil, kind))
}

// fileHeader holds what the JP2 Header box says about turning codestream
// components into image channels (T.800, I.5.3).
type fileHeader struct {
	// colour is the first Colour Specification box, the only one a JP2
	// reader uses (T.800, I.3.3).
	colour colourSpec
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

// colourSpec is one Colour Specification box (T.800, I.5.3.3).
type colourSpec struct {
	method int
	// enumerated is the EnumCS value when method is methodEnumerated.
	enumerated uint32
	// profile is the ICC profile when method is methodICC or methodAnyICC.
	profile []byte
}

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
// A bare codestream is accepted anyway: the corpora hold 3 such streams that
// OpenJPEG decodes, and a codestream carries every parameter decoding needs,
// so reading one cannot misinterpret a conforming file.
func readFile(data []byte) ([]byte, *fileHeader, error) {
	if bytes.HasPrefix(data, codestreamStart) {
		return data, nil, nil
	}
	boxes, err := readBoxes(data)
	if err != nil {
		return nil, nil, err
	}
	// T.800, I.5.1: the signature box is first, 12 bytes long.
	if len(boxes) == 0 || boxes[0].kind != boxSignature || !bytes.Equal(boxes[0].body, signature) {
		return nil, nil, fmt.Errorf("%w: data is neither a JP2 file nor a codestream", ErrMalformed)
	}
	var (
		hdr        *fileHeader
		codestream []byte
	)
	for _, b := range boxes[1:] {
		switch b.kind {
		case boxHeader:
			// T.800, I.5.3: one JP2 Header box, before the codestream.
			if hdr != nil {
				return nil, nil, fmt.Errorf("%w: more than one JP2 Header box", ErrMalformed)
			}
			if hdr, err = readHeader(b.body); err != nil {
				return nil, nil, err
			}
		case boxCodestream:
			// T.800, I.2.2 allows several codestreams and a JP2 reader decodes
			// the first.
			if codestream == nil {
				codestream = b.body
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
func readHeader(body []byte) (*fileHeader, error) {
	boxes, err := readBoxes(body)
	if err != nil {
		return nil, fmt.Errorf("JP2 Header box: %w", err)
	}
	hdr := &fileHeader{}
	haveColour := false
	for _, b := range boxes {
		switch b.kind {
		case boxColour:
			if haveColour {
				continue
			}
			c, ok, err := readColour(b.body)
			if err != nil {
				return nil, err
			}
			// T.800, Table I.9: a reader ignores a box whose method it does
			// not know, so the first box it does know is the one used.
			if ok {
				hdr.colour, haveColour = c, true
			}
		case boxPalette:
			if hdr.palette != nil {
				return nil, fmt.Errorf("%w: more than one Palette box", ErrMalformed)
			}
			if hdr.palette, err = readPalette(b.body); err != nil {
				return nil, err
			}
		case boxComponentMapping:
			if hdr.mapping != nil {
				return nil, fmt.Errorf("%w: more than one Component Mapping box", ErrMalformed)
			}
			if hdr.mapping, err = readMapping(b.body); err != nil {
				return nil, err
			}
		case boxChannelDefinition:
			if hdr.channels != nil {
				return nil, fmt.Errorf("%w: more than one Channel Definition box", ErrMalformed)
			}
			if hdr.channels, err = readChannels(b.body); err != nil {
				return nil, err
			}
		}
	}
	// T.800, I.5.3.4 and I.5.3.5: a Palette box and a Component Mapping box
	// come together or not at all.
	if (hdr.palette == nil) != (hdr.mapping == nil) {
		return nil, fmt.Errorf("%w: a Palette box requires a Component Mapping box and vice versa", ErrMalformed)
	}
	return hdr, nil
}

// readColour reads a Colour Specification box (T.800, I.5.3.3) and reports
// whether its method is one this package knows.
func readColour(body []byte) (colourSpec, bool, error) {
	if len(body) < 3 {
		return colourSpec{}, false, fmt.Errorf("%w: Colour Specification box of %d bytes", ErrMalformed, len(body))
	}
	c := colourSpec{method: int(body[0])}
	switch c.method {
	case methodEnumerated:
		if len(body) < 7 {
			return colourSpec{}, false, fmt.Errorf("%w: enumerated Colour Specification box of %d bytes", ErrMalformed, len(body))
		}
		c.enumerated = binary.BigEndian.Uint32(body[3:])
	case methodICC, methodAnyICC:
		c.profile = body[3:]
	default:
		return colourSpec{}, false, nil
	}
	return c, true, nil
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
