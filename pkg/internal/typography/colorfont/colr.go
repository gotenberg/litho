package colorfont

import (
	"fmt"
	"sort"
)

// maxGlyphLayers caps the layers one COLR glyph draws. Anti-DoS guard, not
// spec-mandated: each layer is an outline filled on every use of the
// glyph, and emoji fonts stack a few dozen at most.
const maxGlyphLayers = 1024

// Foreground is the palette index of a layer drawn in the text's colour
// (OpenType, COLR, version 0: palette index 0xFFFF).
const Foreground = 0xFFFF

// Layer is one layer of a COLR glyph: the outline of glyph GID filled with
// palette entry Palette, or the text's colour when Palette is
// [Foreground].
type Layer struct {
	GID     uint16
	Palette uint16
}

// Color is a CPAL colour, sRGB with straight alpha.
type Color struct {
	R, G, B, A uint8
}

// colr is the version 0 part of a COLR table: its base glyph records,
// sorted by glyph, and its layer records.
type colr struct {
	data      []byte
	numBase   int
	baseOff   uint64
	layerOff  uint64
	numLayers int
}

// readCOLR reads the header of a COLR table (OpenType, COLR, version 0
// header, which version 1 extends). A version 1 table's paint graph is not
// read.
func readCOLR(b []byte) (*colr, error) {
	header, ok := span(b, 0, 14)
	if !ok {
		return nil, fmt.Errorf("%w: COLR header", ErrInvalidFont)
	}
	c := &colr{
		data:      b,
		numBase:   int(header[2])<<8 | int(header[3]),
		baseOff:   uint64(header[4])<<24 | uint64(header[5])<<16 | uint64(header[6])<<8 | uint64(header[7]),
		layerOff:  uint64(header[8])<<24 | uint64(header[9])<<16 | uint64(header[10])<<8 | uint64(header[11]),
		numLayers: int(header[12])<<8 | int(header[13]),
	}
	if c.numBase > 0 {
		if _, ok := span(b, c.baseOff, uint64(c.numBase)*6); !ok {
			return nil, fmt.Errorf("%w: COLR base glyph records", ErrInvalidFont)
		}
	}
	if c.numLayers > 0 {
		if _, ok := span(b, c.layerOff, uint64(c.numLayers)*4); !ok {
			return nil, fmt.Errorf("%w: COLR layer records", ErrInvalidFont)
		}
	}
	return c, nil
}

// Layers returns the layers of glyph gid, bottom first, as its COLR base
// glyph record lists them, and reports false when the glyph has none. It
// returns an error wrapping [ErrInvalidFont] when the record names layers
// past the table's, and one when they number more than a glyph may draw.
func (f *Font) Layers(gid uint16) ([]Layer, bool, error) {
	c := f.colr
	if c == nil {
		return nil, false, nil
	}
	// The base glyph records are sorted by glyph ID (OpenType, COLR).
	glyphAt := func(i int) uint16 {
		off := c.baseOff + uint64(i)*6
		return uint16(c.data[off])<<8 | uint16(c.data[off+1])
	}
	i := sort.Search(c.numBase, func(i int) bool { return glyphAt(i) >= gid })
	if i == c.numBase || glyphAt(i) != gid {
		return nil, false, nil
	}
	rec := c.baseOff + uint64(i)*6
	first := int(c.data[rec+2])<<8 | int(c.data[rec+3])
	n := int(c.data[rec+4])<<8 | int(c.data[rec+5])
	if n == 0 {
		return nil, false, nil
	}
	if first+n > c.numLayers {
		return nil, false, fmt.Errorf("%w: COLR glyph %d layers past the layer records", ErrInvalidFont, gid)
	}
	if n > maxGlyphLayers {
		return nil, false, fmt.Errorf("%w: COLR glyph %d has %d layers, past the %d bound", ErrInvalidFont, gid, n, maxGlyphLayers)
	}
	layers := make([]Layer, n)
	for k := range layers {
		off := c.layerOff + uint64(first+k)*4
		layers[k] = Layer{
			GID:     uint16(c.data[off])<<8 | uint16(c.data[off+1]),
			Palette: uint16(c.data[off+2])<<8 | uint16(c.data[off+3]),
		}
	}
	return layers, true, nil
}

// cpal is a CPAL table: numPalettes palettes of entries colours each, the
// first colour record of each palette, and the palette types of version
// 1.
type cpal struct {
	data    []byte
	entries int
	first   []int
	records uint64
	types   []uint32
}

// readCPAL reads a CPAL table (OpenType, CPAL, versions 0 and 1).
func readCPAL(b []byte) (*cpal, error) {
	header, ok := span(b, 0, 12)
	if !ok {
		return nil, fmt.Errorf("%w: CPAL header", ErrInvalidFont)
	}
	version := int(header[0])<<8 | int(header[1])
	p := &cpal{
		data:    b,
		entries: int(header[2])<<8 | int(header[3]),
		records: uint64(header[8])<<24 | uint64(header[9])<<16 | uint64(header[10])<<8 | uint64(header[11]),
	}
	numPalettes := int(header[4])<<8 | int(header[5])
	numRecords := int(header[6])<<8 | int(header[7])
	// OpenType, CPAL: a table holds at least one palette.
	if numPalettes == 0 {
		return nil, fmt.Errorf("%w: CPAL without a palette", ErrInvalidFont)
	}
	if _, ok := span(b, p.records, uint64(numRecords)*4); !ok {
		return nil, fmt.Errorf("%w: CPAL colour records", ErrInvalidFont)
	}
	indices, ok := span(b, 12, uint64(numPalettes)*2)
	if !ok {
		return nil, fmt.Errorf("%w: CPAL palette indices", ErrInvalidFont)
	}
	p.first = make([]int, numPalettes)
	for i := range p.first {
		p.first[i] = int(indices[2*i])<<8 | int(indices[2*i+1])
		if p.first[i]+p.entries > numRecords {
			return nil, fmt.Errorf("%w: CPAL palette %d past the colour records", ErrInvalidFont, i)
		}
	}
	if version >= 1 {
		typesOff, ok := u32(b, 12+uint64(numPalettes)*2)
		if !ok {
			return nil, fmt.Errorf("%w: CPAL version 1 header", ErrInvalidFont)
		}
		if typesOff != 0 {
			raw, ok := span(b, uint64(typesOff), uint64(numPalettes)*4)
			if !ok {
				return nil, fmt.Errorf("%w: CPAL palette types", ErrInvalidFont)
			}
			p.types = make([]uint32, numPalettes)
			for i := range p.types {
				p.types[i] = uint32(raw[4*i])<<24 | uint32(raw[4*i+1])<<16 | uint32(raw[4*i+2])<<8 | uint32(raw[4*i+3])
			}
		}
	}
	return p, nil
}

// NumPalettes returns the number of CPAL palettes, 0 for a font without
// layered glyphs.
func (f *Font) NumPalettes() int {
	if f.cpal == nil {
		return 0
	}
	return len(f.cpal.first)
}

// Palette returns the colours of palette i, nil when the font has no
// palette i. The first palette, 0, is the font's default (OpenType,
// CPAL).
func (f *Font) Palette(i int) []Color {
	p := f.cpal
	if p == nil || i < 0 || i >= len(p.first) {
		return nil
	}
	out := make([]Color, p.entries)
	for k := range out {
		// A ColorRecord is blue, green, red, alpha (OpenType, CPAL).
		off := p.records + uint64(p.first[i]+k)*4
		out[k] = Color{B: p.data[off], G: p.data[off+1], R: p.data[off+2], A: p.data[off+3]}
	}
	return out
}

// The palette type flags of CPAL version 1.
const (
	paletteLight = 0x1 // USABLE_WITH_LIGHT_BACKGROUND
	paletteDark  = 0x2 // USABLE_WITH_DARK_BACKGROUND
)

// PaletteForLight returns the first palette the font marks as usable on a
// light background, and PaletteForDark the first marked for a dark one;
// each reports false when no palette carries the flag (OpenType, CPAL
// version 1, paletteTypes).
func (f *Font) PaletteForLight() (int, bool) { return f.paletteWith(paletteLight) }

// PaletteForDark returns the first palette the font marks as usable on a
// dark background; see [Font.PaletteForLight].
func (f *Font) PaletteForDark() (int, bool) { return f.paletteWith(paletteDark) }

func (f *Font) paletteWith(flag uint32) (int, bool) {
	if f.cpal == nil {
		return 0, false
	}
	for i, t := range f.cpal.types {
		if t&flag != 0 {
			return i, true
		}
	}
	return 0, false
}
