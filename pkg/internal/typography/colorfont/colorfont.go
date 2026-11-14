package colorfont

import (
	"encoding/binary"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// Font is the colour tables of one OpenType program. Its methods read the
// program it was parsed from, which the caller keeps unchanged. A Font is
// safe for concurrent use: nothing changes it after [Parse].
type Font struct {
	upem      int
	numGlyphs int

	cbdt    []byte
	strikes []cbStrike

	sbix        []byte
	sbixStrikes []sbixStrike

	colr *colr
	cpal *cpal
}

// Parse reads the colour tables of program, a single sfnt program. It
// returns an error wrapping [ErrNoColor] when the program holds none of
// CBLC with CBDT, sbix, or COLR with CPAL, and one wrapping
// [ErrInvalidFont] when a table it holds is broken. A table that holds no
// glyph counts as absent.
func Parse(program []byte) (*Font, error) {
	records, err := truetype.ParseTableDirectory(program)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFont, err)
	}
	table := func(tag string) []byte {
		data, err := truetype.TableData(program, records, tag)
		if err != nil {
			return nil
		}
		return data
	}
	f := &Font{}
	if err := f.readHead(table("head"), table("maxp")); err != nil {
		return nil, err
	}
	if cblc, cbdt := table("CBLC"), table("CBDT"); cblc != nil && cbdt != nil {
		strikes, err := readCBLC(cblc)
		if err != nil {
			return nil, err
		}
		f.cbdt, f.strikes = cbdt, strikes
	}
	if sbix := table("sbix"); sbix != nil {
		strikes, err := readSbix(sbix, f.numGlyphs)
		if err != nil {
			return nil, err
		}
		f.sbix, f.sbixStrikes = sbix, strikes
	}
	if colrData, cpalData := table("COLR"), table("CPAL"); colrData != nil && cpalData != nil {
		c, err := readCOLR(colrData)
		if err != nil {
			return nil, err
		}
		p, err := readCPAL(cpalData)
		if err != nil {
			return nil, err
		}
		if c.numBase > 0 {
			f.colr, f.cpal = c, p
		}
	}
	if len(f.strikes) == 0 && len(f.sbixStrikes) == 0 && f.colr == nil {
		return nil, ErrNoColor
	}
	return f, nil
}

// readHead reads the units per em of head and the glyph count of maxp
// (OpenType, head and maxp tables).
func (f *Font) readHead(head, maxp []byte) error {
	if len(head) < 54 || len(maxp) < 6 {
		return fmt.Errorf("%w: head or maxp missing or short", ErrInvalidFont)
	}
	f.upem = int(binary.BigEndian.Uint16(head[18:]))
	// OpenType, head: unitsPerEm lies in 16 to 16384.
	if f.upem < 16 || f.upem > 16384 {
		return fmt.Errorf("%w: unitsPerEm %d", ErrInvalidFont, f.upem)
	}
	f.numGlyphs = int(binary.BigEndian.Uint16(maxp[4:]))
	return nil
}

// UnitsPerEm returns the program's units per em.
func (f *Font) UnitsPerEm() int { return f.upem }

// HasBitmaps reports whether the program holds colour bitmaps.
func (f *Font) HasBitmaps() bool { return len(f.strikes) > 0 || len(f.sbixStrikes) > 0 }

// HasLayers reports whether the program holds layered colour glyphs.
func (f *Font) HasLayers() bool { return f.colr != nil }

// u16 reads the big-endian uint16 at off of b, reporting false when it
// runs past b.
func u16(b []byte, off uint64) (uint16, bool) {
	if off+2 > uint64(len(b)) {
		return 0, false
	}
	return binary.BigEndian.Uint16(b[off:]), true
}

// u32 reads the big-endian uint32 at off of b, reporting false when it
// runs past b.
func u32(b []byte, off uint64) (uint32, bool) {
	if off+4 > uint64(len(b)) {
		return 0, false
	}
	return binary.BigEndian.Uint32(b[off:]), true
}

// span returns b[off:off+n], reporting false when it runs past b.
func span(b []byte, off, n uint64) ([]byte, bool) {
	if off > uint64(len(b)) || n > uint64(len(b))-off {
		return nil, false
	}
	return b[off : off+n], true
}
