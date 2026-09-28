package remediation

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// FontRequest describes a font a document shows without embedding its
// program, for a [FontProvider] to answer.
type FontRequest struct {
	// BaseFont is the font's /BaseFont as the file writes it, a subset tag
	// included; a composite font's is its descendant CIDFont's.
	BaseFont string
	// Subtype is the font's /Subtype: Type1, MMType1 or TrueType for a
	// simple font, the descendant's CIDFontType0 or CIDFontType2 for a
	// composite one.
	Subtype string
	// Flags is the font descriptor's /Flags (ISO 32000-1, 9.8.2), 0 when the
	// font has no descriptor.
	Flags int
	// Encoding names the font's encoding: the predefined simple encoding its
	// /Encoding or /BaseEncoding names, or the CMap a composite font's
	// /Encoding names. It is empty when the font names none.
	Encoding string
	// Ordering is a composite font's character collection, Registry and
	// Ordering joined by a hyphen (Adobe-Japan1), and empty for a simple
	// font.
	Ordering string
	// Codes lists what the document shows with the font, in code order.
	Codes []FontCode
}

// FontCode is one code a document shows with a font.
type FontCode struct {
	// Code is a simple font's character code, or the CID a composite font's
	// code selects.
	Code uint32
	// Glyph is the glyph name a simple font's encoding gives the code, empty
	// when it gives none or the font is composite.
	Glyph string
	// Unicode is the character the glyph stands for, 0 when unknown.
	Unicode rune
	// Width is the width the font declares for the code, in glyph space
	// units (thousandths of an em).
	Width float64
}

// FontProvider returns the program to embed for a font, a TrueType or
// OpenType program or a bare CFF one, or nil to supply none. An error aborts
// the remediation.
type FontProvider func(ctx context.Context, req FontRequest) ([]byte, error)

// FontRefusal records a font a remediation left unembedded although a
// [FontProvider] was configured.
type FontRefusal struct {
	// BaseFont is the font's /BaseFont.
	BaseFont string
	// Object is the object number of the font dictionary.
	Object int
	// Err wraps one of the refusal sentinels, [ErrFontNotSupplied] to
	// [ErrFontRejected].
	Err error
}

// outlineFormat names the program a font dictionary receives.
type outlineFormat int

const (
	// outlinesTrueType is a TrueType program with glyf outlines, embedded
	// as /FontFile2 (ISO 32000-1, Table 126).
	outlinesTrueType outlineFormat = iota + 1
	// outlinesCFF is a bare name-keyed CFF program, embedded as /FontFile3
	// with /Subtype /Type1C (ISO 32000-1, Table 126). An OpenType program
	// with CFF outlines gives its CFF table.
	outlinesCFF
)

// Font descriptor flags (ISO 32000-1, 9.8.2, Table 123).
const (
	fontFlagFixedPitch  = 1 << 0
	fontFlagSymbolic    = 1 << 2
	fontFlagNonsymbolic = 1 << 5
	fontFlagItalic      = 1 << 6
)

// suppliedProgram is a caller's font program, read.
type suppliedProgram struct {
	// data is what the embedding writes: the program as supplied, or the CFF
	// table of an OpenType program with CFF outlines.
	data   []byte
	format outlineFormat
	// psName is the program's PostScript name.
	psName string
	// cid reports a CID-keyed CFF program.
	cid bool
	// unitsPerEm scales advances to glyph space.
	unitsPerEm float64
	// advances holds each glyph's advance, in font units.
	advances map[uint16]float64
	// unicode maps a code point to the glyph a Unicode cmap subtable gives
	// it, and symbol a code to the glyph a (3,0) or (1,0) subtable gives it
	// (TrueType only).
	unicode, symbol map[uint16]uint16
	// names maps a glyph name to its glyph: the post table's names for a
	// TrueType program, the charset for a CFF one.
	names map[string]uint16
	// builtin is a CFF program's built-in encoding.
	builtin map[byte]string
	// Descriptor metrics, in glyph space units.
	bbox                       [4]float64
	ascent, descent, capHeight float64
	italicAngle                float64
	fixedPitch, bold           bool
}

// minUnitsPerEm and maxUnitsPerEm bound the head table's unitsPerEm: the
// OpenType specification (head table) gives the range 16 to 16384.
const (
	minUnitsPerEm = 16
	maxUnitsPerEm = 16384
)

// boldWeightClass is the OS/2 usWeightClass from which a program is bold
// (OpenType specification, OS/2 table: 600 is Semi-bold).
const boldWeightClass = 600

// readSuppliedProgram reads a caller's program, refusing one past limit bytes,
// the document's decoded-stream budget: a reader of the output could not
// decode a /FontFile larger than that.
func readSuppliedProgram(ctx context.Context, data []byte, limit int64) (*suppliedProgram, error) {
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %d bytes, past the %d-byte stream budget", ErrFontProgramInvalid, len(data), limit)
	}
	if truetype.IsSfnt(data) {
		return readSfntProgram(ctx, data)
	}
	if len(data) >= 4 && data[0] == 1 {
		p, err := readCFFProgram(ctx, data)
		if err != nil {
			return nil, err
		}
		return p, nil
	}
	return nil, fmt.Errorf("%w: not a TrueType, OpenType or CFF program", ErrFontProgramInvalid)
}

// readSfntProgram reads a TrueType or OpenType program.
func readSfntProgram(ctx context.Context, data []byte) (*suppliedProgram, error) {
	info, err := truetype.ReadInfo(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFontProgramInvalid, err)
	}
	// The OpenType specification (OS/2 table, fsType) makes these the bits
	// that forbid embedding a font's outlines in a document.
	if info.EmbeddingRestricted() {
		return nil, fmt.Errorf("%w: OS/2 fsType %#04x", ErrFontEmbeddingRestricted, info.FSType)
	}
	var p *suppliedProgram
	switch info.Outline {
	case truetype.OutlineTrueType:
		if info.UnitsPerEm < minUnitsPerEm || info.UnitsPerEm > maxUnitsPerEm {
			return nil, fmt.Errorf("%w: unitsPerEm %d", ErrFontProgramInvalid, info.UnitsPerEm)
		}
		advances, err := truetype.GlyphWidths(ctx, data)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFontProgramInvalid, err)
		}
		upem := float64(info.UnitsPerEm)
		p = &suppliedProgram{
			data:       data,
			format:     outlinesTrueType,
			unitsPerEm: upem,
			advances:   advances,
			unicode:    typography.ExtractCharCodeToGID(ctx, data),
			symbol:     typography.ExtractSymbolCharCodeToGID(ctx, data),
			names:      invertGlyphNames(typography.ExtractGlyphNames(ctx, data)),
		}
		for k, v := range info.BBox {
			p.bbox[k] = float64(v) * 1000 / upem
		}
		p.italicAngle = info.ItalicAngle
		p.fixedPitch = info.FixedPitch
		p.ascent = float64(info.Ascender) * 1000 / upem
		p.descent = float64(info.Descender) * 1000 / upem
		p.capHeight = float64(info.CapHeight) * 1000 / upem
	case truetype.OutlineCFF:
		records, err := truetype.ParseTableDirectory(data)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFontProgramInvalid, err)
		}
		table, err := truetype.TableData(data, records, "CFF ")
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFontProgramInvalid, err)
		}
		if p, err = readCFFProgram(ctx, table); err != nil {
			return nil, err
		}
		// The OS/2 metrics are in the OpenType font's units, the head
		// table's; the CFF table's FontMatrix states the same scale.
		if info.UnitsPerEm >= minUnitsPerEm && info.UnitsPerEm <= maxUnitsPerEm {
			upem := float64(info.UnitsPerEm)
			p.ascent = float64(info.Ascender) * 1000 / upem
			p.descent = float64(info.Descender) * 1000 / upem
			p.capHeight = float64(info.CapHeight) * 1000 / upem
		}
	case truetype.OutlineNone:
		return nil, fmt.Errorf("%w: neither a glyf nor a CFF table", ErrFontProgramInvalid)
	}
	p.bold = info.WeightClass >= boldWeightClass || info.MacStyle&1 != 0
	if p.psName = info.PostScriptName; p.psName == "" {
		p.psName = info.FullName
	}
	return p, nil
}

// readCFFProgram reads a bare CFF program.
func readCFFProgram(ctx context.Context, data []byte) (*suppliedProgram, error) {
	info, err := cff.ReadInfo(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFontProgramInvalid, err)
	}
	advances, err := cff.GlyphWidths(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFontProgramInvalid, err)
	}
	names, err := cff.GlyphNames(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFontProgramInvalid, err)
	}
	upem := float64(cff.ReadUnitsPerEm(data))
	if upem < minUnitsPerEm || upem > maxUnitsPerEm {
		return nil, fmt.Errorf("%w: a FontMatrix of %g units per em", ErrFontProgramInvalid, upem)
	}
	p := &suppliedProgram{
		data:        data,
		format:      outlinesCFF,
		psName:      info.FontName,
		cid:         info.CID,
		unitsPerEm:  upem,
		advances:    advances,
		names:       invertGlyphNames(names),
		builtin:     cff.ExtractEncoding(data),
		italicAngle: info.ItalicAngle,
		fixedPitch:  info.FixedPitch,
	}
	for k, v := range info.BBox {
		p.bbox[k] = v * 1000 / upem
	}
	p.ascent, p.descent, p.capHeight = p.bbox[3], p.bbox[1], p.bbox[3]
	return p, nil
}

// invertGlyphNames returns the glyph each name labels, the lowest one when
// several share a name.
func invertGlyphNames(names map[uint16]string) map[string]uint16 {
	out := make(map[string]uint16, len(names))
	for gid, name := range names {
		if prev, ok := out[name]; !ok || gid < prev {
			out[name] = gid
		}
	}
	return out
}

// advance returns glyph gid's advance in glyph space units.
func (p *suppliedProgram) advance(gid uint16) (float64, bool) {
	w, ok := p.advances[gid]
	if !ok {
		return 0, false
	}
	return w * 1000 / p.unitsPerEm, true
}
