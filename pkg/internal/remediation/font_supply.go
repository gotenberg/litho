package remediation

import (
	"context"
	"fmt"
	"slices"

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

// FontProvider returns the programs that may serve a font, best first, or
// none. The engine embeds the first one it accepts for the font, and reads the
// others only when it refuses the ones before. An error aborts the
// remediation.
type FontProvider func(ctx context.Context, req FontRequest) ([]FontCandidate, error)

// FontCandidate reads one program a [FontProvider] offers: a TrueType or
// OpenType program, or a bare CFF one. It returns nil for a program that turns
// out to be no candidate after all, which the engine passes over. An error
// aborts the remediation.
type FontCandidate func(ctx context.Context) ([]byte, error)

// FontRefusal records a font a remediation left unembedded although a
// [FontProvider] was configured.
type FontRefusal struct {
	// BaseFont is the font's /BaseFont.
	BaseFont string
	// Object is the object number of the font dictionary.
	Object int
	// Err wraps one of the refusal sentinels, [ErrFontNotSupplied] to
	// [ErrFontUnwritable], or [ErrFontRolledBack].
	Err error
}

// FontEmbed records a font a run embedded a supplied program for.
type FontEmbed struct {
	// BaseFont is the font's /BaseFont.
	BaseFont string
	// Object is the object number the font dictionary had in the document
	// the run read, 0 for one written inline in a resource dictionary.
	Object int
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
	// glyphNames maps a TrueType program's glyph to its post table name.
	glyphNames map[uint16]string
	// builtin is a CFF program's built-in encoding.
	builtin map[byte]string
	// Descriptor metrics, in glyph space units.
	bbox                       [4]float64
	ascent, descent, capHeight float64
	italicAngle                float64
	fixedPitch, bold           bool
	// noSubset reports a program whose OS/2 fsType forbids subsetting it.
	noSubset bool
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
		}
		p.glyphNames = typography.ExtractGlyphNames(ctx, data)
		p.names = invertGlyphNames(p.glyphNames)
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
	p.noSubset = info.SubsettingRestricted()
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

// glyphNamed returns the glyph of a TrueType program a glyph name names: the
// one its post table names so, else the one its Unicode cmap gives the name's
// Unicode value ([suppliedProgram.nameToGID]).
func (p *suppliedProgram) glyphNamed(name string) (uint16, bool) {
	if name == "" || name == ".notdef" {
		return 0, false
	}
	if gid, ok := p.names[name]; ok && gid != 0 {
		return gid, true
	}
	return p.nameToGID(name)
}

// symbolCodeBase is the high byte ISO 32000-1, 9.6.6.4 prefixes a symbolic
// TrueType font's single-byte codes with to look them up in a (3,0) cmap
// subtable, the 0xF000 range Microsoft's symbol fonts use.
const symbolCodeBase = 0xF000

// symbolCmap returns the TrueType program prog with a single (3,0) cmap
// subtable, taking each code 0xF000 plus c to the glyph of names[c]
// ([suppliedProgram.glyphNamed]), for a symbolic Type 1 font that becomes a
// symbolic TrueType font: ISO 32000-1, 9.6.6.4 has a reader map a symbolic
// TrueType font's codes through the (3,0) subtable, where the Type 1 font
// named its glyphs through its encoding, so each code keeps the glyph its
// name gave it. A single subtable is what ISO 19005-1, 6.3.7 t3 asks, and a
// (3,0) one what ISO 19005-2, 6.2.11.6 t4 asks. A code whose name no glyph
// bears maps to none, which the coverage check refuses when it is shown. The
// rebuilt program is read anew, so every check that follows judges the
// program the file receives.
//
// # Corpus evidence
//
//   - govdocs: remediated at PDF/A-2b with WithSystemFonts on macOS, 95
//     Symbol and ZapfDingbats fonts over Apple's TrueType Symbol and
//     ZapfDingbats programs were refused as a symbolic Type 1 font over a
//     TrueType program. Read through the program's own (1,0) subtable, 44 of
//     them would still be refused, their encodings placing glyphs at codes
//     that subtable maps elsewhere or not at all (a Symbol /Differences
//     naming second, degree or mu at codes 1 to 6, lozenge at 215); through
//     the (3,0) subtable built from their names, 2 are, for a name the
//     program lacks.
func (e *fontEmbedder) symbolCmap(ctx context.Context, prog *suppliedProgram, names map[byte]string) (*suppliedProgram, error) {
	codes := make(map[uint32]uint16, len(names))
	for code, name := range names {
		if gid, ok := prog.glyphNamed(name); ok && gid != 0 {
			codes[symbolCodeBase+uint32(code)] = gid
		}
	}
	data, err := truetype.RewriteCmap(ctx, prog.data, []truetype.CmapAlias{{Target: cmapWindowsSymbol, Codes: codes}})
	if err != nil {
		return nil, fmt.Errorf("%w: the cmap of %s cannot be rebuilt: %w", ErrFontProgramInvalid, prog.psName, err)
	}
	return readSuppliedProgram(ctx, data, e.doc.Limits().MaxStreamBytes)
}

// The cmap encoding records a TrueType font dictionary reads its program
// through (ISO 32000-1, 9.6.6.4; OpenType specification, cmap table), and the
// Unicode platform's ID.
var (
	cmapWindowsUnicode = truetype.CmapSubtable{PlatformID: 3, EncodingID: 1}
	cmapWindowsSymbol  = truetype.CmapSubtable{PlatformID: 3, EncodingID: 0}
	cmapWindowsUCS4    = truetype.CmapSubtable{PlatformID: 3, EncodingID: 10}
	cmapMacintoshRoman = truetype.CmapSubtable{PlatformID: 1, EncodingID: 0}
)

// cmapUnicodePlatform is the platform ID of the Unicode platform's subtables.
const cmapUnicodePlatform = 0

// fitCmap returns the TrueType program prog with the cmap a TrueType font
// dictionary reads it through, or prog itself when its cmap serves already or
// cannot be rebuilt, which the checks that follow then refuse. A program made
// for another platform than PDF maps its glyphs through a Unicode-platform
// (0,x) subtable, which no PDF reader consults:
//
//   - A non-symbolic font reads a code's glyph name through the (3,1)
//     subtable, and failing it through the (1,0) one (ISO 32000-1, 9.6.6.4),
//     and ISO 19005-2, 6.2.11.6 t2 admits /Differences only over a program
//     holding (3,1). A program with a Unicode subtable and no (3,1) one gains
//     a (3,1) subtable mapping what the Unicode one maps, beside it and the
//     (1,0) one.
//   - A symbolic font reads a code through the (3,0) subtable, and failing it
//     through the (1,0) one. ISO 19005-1, 6.3.7 t3 asks for a single
//     subtable, and ISO 19005-2, 6.2.11.6 t4 for a single one or a (3,0)
//     one. A program holding several keeps the one a reader maps its codes
//     through alone: (3,0), else (1,0).
//
// The rebuilt program is read anew, so every check that follows judges the
// program the file receives.
//
// # Corpus evidence
//
//   - govdocs: remediated at PDF/A-2b with WithSystemFonts on macOS, 160
//     Times, Helvetica and Courier fonts given the Apple programs, which
//     carry (0,3) and (1,0) subtables alone, were refused under 6.2.11.6 t2
//     for their /Differences, and 30 symbolic TrueType Symbol fonts under
//     6.2.11.6 t4 for the two subtables of Apple's Symbol.
func (e *fontEmbedder) fitCmap(ctx context.Context, prog *suppliedProgram, symbolic bool) *suppliedProgram {
	subs, err := truetype.CmapSubtables(prog.data)
	if err != nil {
		return prog
	}
	var aliases []truetype.CmapAlias
	keep := func(s truetype.CmapSubtable) { aliases = append(aliases, truetype.CmapAlias{Target: s, From: s}) }
	has := func(s truetype.CmapSubtable) bool { return slices.Contains(subs, s) }
	if symbolic {
		switch {
		case len(subs) <= 1:
			return prog
		case has(cmapWindowsSymbol):
			keep(cmapWindowsSymbol)
		case has(cmapMacintoshRoman):
			keep(cmapMacintoshRoman)
		default:
			return prog
		}
	} else {
		if has(cmapWindowsUnicode) {
			return prog
		}
		// The source is the subtable [truetype.GlyphToUnicode] reads the
		// program's Unicode values from: (3,10), else the first
		// Unicode-platform one.
		source, found := cmapWindowsUCS4, has(cmapWindowsUCS4)
		for _, s := range subs {
			if !found && s.PlatformID == cmapUnicodePlatform {
				source, found = s, true
			}
		}
		if !found {
			return prog
		}
		keep(source)
		if has(cmapMacintoshRoman) {
			keep(cmapMacintoshRoman)
		}
		aliases = append(aliases, truetype.CmapAlias{Target: cmapWindowsUnicode, From: source})
	}
	data, err := truetype.RewriteCmap(ctx, prog.data, aliases)
	if err != nil {
		e.doc.Logger.Debug("remediation: font-embed: cmap left as supplied", "font", prog.psName, "error", err)
		return prog
	}
	fitted, err := readSuppliedProgram(ctx, data, e.doc.Limits().MaxStreamBytes)
	if err != nil {
		e.doc.Logger.Debug("remediation: font-embed: cmap left as supplied", "font", prog.psName, "error", err)
		return prog
	}
	return fitted
}
