package colorfont

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"slices"
)

// maxStrikes caps the strikes, the bitmap sizes, read from CBLC or sbix.
// Anti-DoS guard, not spec-mandated: each strike is a pass over its index
// when a glyph is looked up, and a colour font holds a handful of strikes.
const maxStrikes = 64

// maxIndexSubTables caps the index subtables of one CBLC strike, which a
// glyph lookup walks. Anti-DoS guard, not spec-mandated: a font groups
// its glyphs in a few ranges, and Noto Color Emoji holds three.
const maxIndexSubTables = 4096

// maxDupeDepth caps the chain of sbix 'dupe' glyphs, each naming another
// glyph's data. Anti-DoS guard, not spec-mandated: the OpenType sbix table
// makes a dupe name a glyph that holds an image, one step.
const maxDupeDepth = 4

// Format is how the image of a [Bitmap] is stored.
type Format uint8

// The image formats a [Bitmap] holds.
const (
	// PNG is a PNG file (CBDT formats 17, 18 and 19; sbix 'png ').
	PNG Format = iota + 1
	// JPEG is a JPEG file (sbix 'jpg ').
	JPEG
)

// Bitmap is the colour bitmap of one glyph at one strike.
type Bitmap struct {
	// Data is the image file, a slice of the program the [Font] was parsed
	// from: the caller must not change it.
	Data   []byte
	Format Format
	// PPEM is the strike's pixels per em: a pixel of the image is
	// UnitsPerEm/PPEM font units.
	PPEM int
	// Left and Top place the image's top-left corner from the glyph
	// origin, in pixels, x to the right and y up.
	Left, Top int
	// Width and Height are the image's size in pixels.
	Width, Height int
}

// cbStrike is one bitmap size of the CBLC table.
type cbStrike struct {
	ppem int
	// array is the offset of its index subtable array in CBLC, count its
	// index subtables.
	array uint64
	count int
	cblc  []byte
}

// readCBLC reads the bitmap sizes of a CBLC table (OpenType, CBLC:
// header, BitmapSize records). A glyph lookup checks each index subtable
// against the table.
func readCBLC(cblc []byte) ([]cbStrike, error) {
	n, ok := u32(cblc, 4)
	if !ok {
		return nil, fmt.Errorf("%w: CBLC header", ErrInvalidFont)
	}
	if n > maxStrikes {
		return nil, fmt.Errorf("%w: %d CBLC strikes past the %d bound", ErrInvalidFont, n, maxStrikes)
	}
	var strikes []cbStrike
	for i := range uint64(n) {
		rec, ok := span(cblc, 8+i*48, 48)
		if !ok {
			return nil, fmt.Errorf("%w: CBLC BitmapSize %d", ErrInvalidFont, i)
		}
		array, _ := u32(rec, 0)
		count, _ := u32(rec, 8)
		if count > maxIndexSubTables {
			return nil, fmt.Errorf("%w: %d CBLC index subtables past the %d bound", ErrInvalidFont, count, maxIndexSubTables)
		}
		if _, ok := span(cblc, uint64(array), uint64(count)*8); !ok {
			return nil, fmt.Errorf("%w: CBLC index subtable array %d", ErrInvalidFont, i)
		}
		ppem := int(rec[44])
		if ppem == 0 || count == 0 {
			continue
		}
		strikes = append(strikes, cbStrike{ppem: ppem, array: uint64(array), count: int(count), cblc: cblc}) // fits int: count <= maxIndexSubTables
	}
	return strikes, nil
}

// sbixStrike is one strike of the sbix table: its pixels per em and the
// offset of its glyph data offsets in the table.
type sbixStrike struct {
	ppem   int
	offset uint64
}

// readSbix reads the strikes of an sbix table (OpenType, sbix: header and
// Strike records), each holding numGlyphs+1 glyph data offsets.
func readSbix(sbix []byte, numGlyphs int) ([]sbixStrike, error) {
	n, ok := u32(sbix, 4)
	if !ok {
		return nil, fmt.Errorf("%w: sbix header", ErrInvalidFont)
	}
	if n > maxStrikes {
		return nil, fmt.Errorf("%w: %d sbix strikes past the %d bound", ErrInvalidFont, n, maxStrikes)
	}
	var strikes []sbixStrike
	for i := range uint64(n) {
		off, ok := u32(sbix, 8+4*i)
		if !ok {
			return nil, fmt.Errorf("%w: sbix strike offset %d", ErrInvalidFont, i)
		}
		ppem, ok := u16(sbix, uint64(off))
		if !ok {
			return nil, fmt.Errorf("%w: sbix strike %d", ErrInvalidFont, i)
		}
		if _, ok := span(sbix, uint64(off)+4, uint64(numGlyphs+1)*4); !ok {
			return nil, fmt.Errorf("%w: sbix strike %d glyph offsets", ErrInvalidFont, i)
		}
		if ppem == 0 {
			continue
		}
		strikes = append(strikes, sbixStrike{ppem: int(ppem), offset: uint64(off)})
	}
	return strikes, nil
}

// preference orders strike indices for a glyph wanted at ppem pixels per
// em: the strikes at least that large, smallest first, then the smaller
// ones, largest first. The nearest strike that does not scale up comes
// first, so a bitmap is drawn at or above its stored resolution where the
// font allows it.
func preference(ppems []int, ppem float64) []int {
	order := make([]int, len(ppems))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		pa, pb := ppems[a], ppems[b]
		ua, ub := float64(pa) >= ppem, float64(pb) >= ppem
		switch {
		case ua && !ub:
			return -1
		case ub && !ua:
			return 1
		case ua:
			return pa - pb
		default:
			return pb - pa
		}
	})
	return order
}

// Bitmap returns the colour bitmap of glyph gid for drawing at ppem pixels
// per em, from the strike [preference] puts first among those that hold
// the glyph: CBDT strikes before sbix ones. It reports false when no
// strike holds a bitmap of a supported format for the glyph, and returns
// an error wrapping [ErrInvalidFont] when the index or the glyph data runs
// past its table.
func (f *Font) Bitmap(gid uint16, ppem float64) (Bitmap, bool, error) {
	if int(gid) >= f.numGlyphs {
		return Bitmap{}, false, nil
	}
	ppems := make([]int, len(f.strikes))
	for i, s := range f.strikes {
		ppems[i] = s.ppem
	}
	for _, i := range preference(ppems, ppem) {
		bm, ok, err := f.strikes[i].glyph(f.cbdt, gid)
		if err != nil || ok {
			return bm, ok, err
		}
	}
	ppems = ppems[:0]
	for _, s := range f.sbixStrikes {
		ppems = append(ppems, s.ppem)
	}
	for _, i := range preference(ppems, ppem) {
		bm, ok, err := f.sbixGlyph(f.sbixStrikes[i], gid, 0)
		if err != nil || ok {
			return bm, ok, err
		}
	}
	return Bitmap{}, false, nil
}

// glyph returns the bitmap of gid in strike s, looked up through its index
// subtables (OpenType, CBLC, IndexSubTableArray and the index subtable
// formats 1 to 5) in cbdt (OpenType, CBDT, glyph image formats 17, 18 and
// 19).
func (s cbStrike) glyph(cbdt []byte, gid uint16) (Bitmap, bool, error) {
	bad := func(what string) (Bitmap, bool, error) {
		return Bitmap{}, false, fmt.Errorf("%w: CBLC strike %d ppem: %s", ErrInvalidFont, s.ppem, what)
	}
	for k := range uint64(s.count) {
		rec := s.array + 8*k
		first, _ := u16(s.cblc, rec)
		last, _ := u16(s.cblc, rec+2)
		if gid < first || gid > last {
			continue
		}
		add, _ := u32(s.cblc, rec+4)
		sub := s.array + uint64(add)
		header, ok := span(s.cblc, sub, 8)
		if !ok {
			return bad("index subtable header")
		}
		indexFormat := uint16(header[0])<<8 | uint16(header[1])
		imageFormat := uint16(header[2])<<8 | uint16(header[3])
		dataOff := uint64(header[4])<<24 | uint64(header[5])<<16 | uint64(header[6])<<8 | uint64(header[7])
		i := uint64(gid - first)
		var start, end uint64
		var big []byte
		switch indexFormat {
		case 1, 3:
			w := uint64(4)
			if indexFormat == 3 {
				w = 2
			}
			offs, ok := span(s.cblc, sub+8+w*i, 2*w)
			if !ok {
				return bad("index format offsets")
			}
			if w == 4 {
				start = uint64(offs[0])<<24 | uint64(offs[1])<<16 | uint64(offs[2])<<8 | uint64(offs[3])
				end = uint64(offs[4])<<24 | uint64(offs[5])<<16 | uint64(offs[6])<<8 | uint64(offs[7])
			} else {
				start = uint64(offs[0])<<8 | uint64(offs[1])
				end = uint64(offs[2])<<8 | uint64(offs[3])
			}
		case 2:
			size, ok := u32(s.cblc, sub+8)
			if big, ok = span(s.cblc, sub+12, 8); !ok || size == 0 {
				return bad("index format 2 metrics")
			}
			start = uint64(size) * i
			end = start + uint64(size)
		case 4:
			n, ok := u32(s.cblc, sub+8)
			if !ok {
				return bad("index format 4 count")
			}
			pairs, ok := span(s.cblc, sub+12, (uint64(n)+1)*4)
			if !ok {
				return bad("index format 4 pairs")
			}
			found := false
			for j := uint64(0); j < uint64(n); j++ {
				if uint16(pairs[4*j])<<8|uint16(pairs[4*j+1]) == gid {
					start = uint64(pairs[4*j+2])<<8 | uint64(pairs[4*j+3])
					end = uint64(pairs[4*j+6])<<8 | uint64(pairs[4*j+7])
					found = true
					break
				}
			}
			if !found {
				return Bitmap{}, false, nil
			}
		case 5:
			size, ok := u32(s.cblc, sub+8)
			if big, ok = span(s.cblc, sub+12, 8); !ok || size == 0 {
				return bad("index format 5 metrics")
			}
			n, ok := u32(s.cblc, sub+20)
			if !ok {
				return bad("index format 5 count")
			}
			ids, ok := span(s.cblc, sub+24, uint64(n)*2)
			if !ok {
				return bad("index format 5 glyph IDs")
			}
			found := false
			for j := uint64(0); j < uint64(n); j++ {
				if uint16(ids[2*j])<<8|uint16(ids[2*j+1]) == gid {
					start = uint64(size) * j
					end = start + uint64(size)
					found = true
					break
				}
			}
			if !found {
				return Bitmap{}, false, nil
			}
		default:
			return Bitmap{}, false, nil
		}
		if end <= start {
			return Bitmap{}, false, nil
		}
		data, ok := span(cbdt, dataOff+start, end-start)
		if !ok {
			return bad("glyph data past CBDT")
		}
		return cbdtImage(data, imageFormat, big, s.ppem)
	}
	return Bitmap{}, false, nil
}

// cbdtImage reads a glyph image of CBDT format 17 (small metrics, PNG), 18
// (big metrics, PNG) or 19 (PNG, metrics big from the index subtable,
// given in big). Any other format reports false.
func cbdtImage(data []byte, format uint16, big []byte, ppem int) (Bitmap, bool, error) {
	var metrics []byte
	switch format {
	case 17:
		metrics = data[:min(5, len(data))]
		data = data[min(5, len(data)):]
	case 18:
		metrics = data[:min(8, len(data))]
		data = data[min(8, len(data)):]
	case 19:
		metrics = big
	default:
		return Bitmap{}, false, nil
	}
	if len(metrics) < 5 {
		return Bitmap{}, false, fmt.Errorf("%w: CBDT glyph metrics", ErrInvalidFont)
	}
	n, ok := u32(data, 0)
	if !ok {
		return Bitmap{}, false, fmt.Errorf("%w: CBDT glyph data length", ErrInvalidFont)
	}
	png, ok := span(data, 4, uint64(n))
	if !ok {
		return Bitmap{}, false, fmt.Errorf("%w: CBDT glyph data past its range", ErrInvalidFont)
	}
	// SmallGlyphMetrics and BigGlyphMetrics both open with height, width,
	// bearing x and bearing y, the bearings signed (OpenType, EBDT).
	return Bitmap{
		Data:   png,
		Format: PNG,
		PPEM:   ppem,
		Height: int(metrics[0]),
		Width:  int(metrics[1]),
		Left:   int(int8(metrics[2])), //nolint:gosec // reinterprets the byte of a signed int8 bearing
		Top:    int(int8(metrics[3])), //nolint:gosec // reinterprets the byte of a signed int8 bearing
	}, true, nil
}

// sbixGlyph returns the bitmap of gid in strike s (OpenType, sbix, Glyph
// data): a PNG or JPEG image whose bottom-left corner sits at its origin
// offsets, in pixels, from the glyph origin; a 'dupe' names the glyph whose
// data it shares.
func (f *Font) sbixGlyph(s sbixStrike, gid uint16, depth int) (Bitmap, bool, error) {
	if int(gid) >= f.numGlyphs || depth > maxDupeDepth {
		return Bitmap{}, false, nil
	}
	start, _ := u32(f.sbix, s.offset+4+4*uint64(gid))
	end, _ := u32(f.sbix, s.offset+8+4*uint64(gid))
	if end <= start {
		return Bitmap{}, false, nil
	}
	data, ok := span(f.sbix, s.offset+uint64(start), uint64(end-start))
	if !ok || len(data) < 8 {
		return Bitmap{}, false, fmt.Errorf("%w: sbix glyph %d data", ErrInvalidFont, gid)
	}
	x := int(int16(uint16(data[0])<<8 | uint16(data[1]))) //nolint:gosec // reinterprets the bits of a signed int16 offset
	y := int(int16(uint16(data[2])<<8 | uint16(data[3]))) //nolint:gosec // reinterprets the bits of a signed int16 offset
	img := data[8:]
	var format Format
	switch string(data[4:8]) {
	case "png ":
		format = PNG
	case "jpg ":
		format = JPEG
	case "dupe":
		other, ok := u16(img, 0)
		if !ok {
			return Bitmap{}, false, fmt.Errorf("%w: sbix dupe of glyph %d", ErrInvalidFont, gid)
		}
		return f.sbixGlyph(s, other, depth+1)
	default:
		return Bitmap{}, false, nil
	}
	cfg, err := decodeConfig(img, format)
	if err != nil {
		return Bitmap{}, false, fmt.Errorf("%w: sbix glyph %d image: %w", ErrInvalidFont, gid, err)
	}
	return Bitmap{
		Data: img, Format: format, PPEM: s.ppem,
		Left: x, Top: y + cfg.Height,
		Width: cfg.Width, Height: cfg.Height,
	}, true, nil
}

// decodeConfig reads the size of an image file without decoding it.
func decodeConfig(data []byte, format Format) (image.Config, error) {
	if format == JPEG {
		return jpeg.DecodeConfig(bytes.NewReader(data))
	}
	return png.DecodeConfig(bytes.NewReader(data))
}
