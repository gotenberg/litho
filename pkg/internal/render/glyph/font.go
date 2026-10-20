package glyph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// Source names where a glyph's outline comes from, for the render report.
type Source int

const (
	// SourceEmbedded is a glyph of the program the document embeds.
	SourceEmbedded Source = iota + 1
	// SourceSubstitute is a glyph of a program [Options.Substitutes]
	// supplied for a font the document does not embed.
	SourceSubstitute
	// SourcePlaceholder is the hollow box drawn for a glyph no program
	// supplies.
	SourcePlaceholder
	// SourceType3 is a Type 3 glyph description (ISO 32000-1, 9.6.5).
	SourceType3
)

// String names the source.
func (s Source) String() string {
	switch s {
	case SourceEmbedded:
		return "embedded"
	case SourceSubstitute:
		return "substitute"
	case SourcePlaceholder:
		return "placeholder"
	case SourceType3:
		return "type3"
	}
	return "unknown"
}

// Code is one character code of a shown string (ISO 32000-1, 9.4.3).
type Code struct {
	// Bytes are the code's bytes, a subslice of the shown string.
	Bytes []byte
	// Value is the code's big-endian value.
	Value uint32
}

// WordSpace reports the single-byte code 32, the only code word spacing (Tw)
// applies to (ISO 32000-1, 9.3.3).
func (c Code) WordSpace() bool {
	return len(c.Bytes) == 1 && c.Bytes[0] == ' '
}

// Glyph is what one character code draws.
type Glyph struct {
	// Outline is the glyph's outline in glyph space, filled with the
	// nonzero winding rule. It is empty for a glyph that paints nothing and
	// for a Type 3 glyph, whose description draws it.
	Outline geom.Path
	// Matrix maps glyph space to text space at a font size of 1: the
	// program's FontMatrix (ISO 32000-1, 9.2.4), a Type 3 font's /FontMatrix
	// (9.6.5), and for a substitute the horizontal scale that fits its
	// advance to the declared width.
	Matrix geom.Matrix
	// Advance is the horizontal displacement w0 in text space units at a
	// font size of 1: the declared width divided by 1000 (9.2.4), or a Type
	// 3 width mapped through /FontMatrix.
	Advance float64
	// W1 is the vertical displacement and V the position vector of a glyph
	// of a font in vertical writing mode, in text space units at a font
	// size of 1 (9.7.4.3): the glyph is drawn with its origin moved by -V,
	// and the text position moves by W1. Both are zero in horizontal mode.
	W1 float64
	V  geom.Point
	// Source says where the outline comes from.
	Source Source
	// Notdef reports a code that selects no glyph of the font: Outline is
	// then the program's .notdef glyph (9.6.6.3, 9.7.6.3), or empty when it
	// holds none, and a Type 3 code names no glyph description.
	Notdef bool
	// Unreadable reports a glyph whose outline the program could not
	// draw; Outline is then empty.
	Unreadable bool
	// CharProc is the glyph description of a Type 3 glyph, nil for any
	// other font or when the code names none.
	CharProc *ast.PdfStream
	// Resources are the resources a Type 3 glyph description runs with,
	// the font's /Resources; nil when the font states none, in which case
	// the description uses the resources of the page or form showing it
	// (9.6.5, Table 112).
	Resources ast.PdfDict
}

// fontKind is the glyph selection a font uses.
type fontKind int

const (
	kindSimple fontKind = iota + 1
	kindType0
	kindType3
)

// Font is a font loaded for drawing glyphs. It is immutable once [Load]
// returns it and safe for concurrent use.
type Font struct {
	kind fontKind
	// subtype is the font's /Subtype, its descendant CIDFont's for a Type 0
	// font.
	subtype string
	source  Source
	metrics *typography.Font
	logger  *slog.Logger

	// prog is the embedded program when source is SourceEmbedded, the
	// supplied one when SourceSubstitute, nil otherwise.
	prog program

	// Simple fonts.
	enc      simpleEncoding
	symbolic bool
	// nameRoute reports a TrueType font whose codes 9.6.6.4 maps through
	// glyph names; see [Font.trueTypeGlyph].
	nameRoute bool

	// Type 0 fonts. cids is nil for Identity-H and Identity-V, whose
	// two-byte code is the CID (9.7.5.2, Table 118), and for an encoding
	// litho cannot read; cidToGID is nil for an Identity /CIDToGIDMap.
	cids     *cmap.CIDMap
	cidToGID []uint16

	// Type 3 fonts.
	type3 *type3Font

	// box is the font's bounding box in thousandths of text space, which
	// sizes a placeholder.
	box [4]float64
}

// Source returns where the font's glyphs come from: its embedded program, a
// substitute, placeholders, or Type 3 glyph descriptions. A font whose
// substitute lacks some glyph still draws those as placeholders; each
// [Glyph] carries its own source.
func (f *Font) Source() Source {
	return f.source
}

// WMode returns the font's writing mode: 0 horizontal, 1 vertical (ISO
// 32000-1, 9.7.4.3).
func (f *Font) WMode() int {
	return f.metrics.WMode
}

// size is the byte length of the program the font holds, which the cache
// budget counts.
func (f *Font) size() int {
	if f.prog == nil {
		return 0
	}
	return f.prog.size()
}

// Load reads the font dictionary fontObj, an indirect reference or a direct
// dictionary, for drawing its glyphs. The font's metrics come from
// [typography.ResolveFontObject], shared with text extraction through the
// document's font cache; its embedded program is decoded through the
// document's font program cache.
func Load(ctx context.Context, doc *document.Document, fontObj ast.PdfObject, opts Options) (*Font, error) {
	fontDict, err := document.ResolveDict(ctx, doc, fontObj)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotAFont, err)
	}
	if fontDict == nil {
		return nil, ErrNotAFont
	}
	metrics, err := typography.ResolveFontObject(ctx, doc, fontObj)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotAFont, err)
	}
	f := &Font{metrics: metrics, subtype: metrics.Subtype, logger: doc.Logger}
	f.box = metrics.FontBBox

	switch metrics.Subtype {
	case "Type3":
		f.kind, f.source = kindType3, SourceType3
		f.type3 = loadType3(ctx, doc, fontDict)
		f.enc = resolveSimpleEncoding(ctx, doc, fontDict, nil, "")
		return f, nil
	case "Type0":
		f.kind = kindType0
		return f, f.loadType0(ctx, doc, fontDict, opts)
	default:
		f.kind = kindSimple
		return f, f.loadSimple(ctx, doc, fontDict, opts)
	}
}

// loadSimple reads a Type 1, MMType1 or TrueType font.
func (f *Font) loadSimple(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, opts Options) error {
	desc := document.ResolveFontDescriptor(ctx, doc, fontDict)
	flags := int(document.Float(desc["Flags"]))
	f.symbolic = flags&flagSymbolic != 0
	baseFont := nameOf(ctx, doc, fontDict["BaseFont"])

	data, prog := embeddedProgram(ctx, doc, desc)
	var builtin *nameTable
	switch p := prog.(type) {
	case *type1Program:
		builtin = type1Builtin(data)
	case *cffProgram:
		if p.tables == nil {
			builtin = cffBuiltin(data)
		} else if table, err := truetype.CFFTable(data); err == nil {
			builtin = cffBuiltin(table)
		}
	}
	if builtin == nil && prog == nil {
		// A font the document does not embed has the built-in encoding of
		// the standard font it is (ISO 32000-1, 9.6.6.1 and Annex D.5,
		// D.6): Symbol and ZapfDingbats have their own.
		if std, ok := typography.Standard14StandIn(baseFont); ok && (std == "Symbol" || std == "ZapfDingbats") {
			builtin = predefinedNames()[std]
		}
	}
	f.enc = resolveSimpleEncoding(ctx, doc, fontDict, builtin, implicitEncoding(f.subtype, f.symbolic, prog != nil))
	// ISO 32000-1, 9.6.6.4: a TrueType font whose Symbolic flag is clear and
	// that names an encoding or sets the Nonsymbolic flag maps its codes
	// through glyph names, the table's undefined entries filled from
	// StandardEncoding. A set Symbolic flag makes it ignore the encoding.
	f.nameRoute = f.subtype != "TrueType" || !f.symbolic && (f.enc.stated || flags&flagNonsymbolic != 0)
	if f.subtype == "TrueType" && f.nameRoute {
		for code, name := range encodings.StandardEncodingNames {
			if f.enc.names[code] == "" {
				f.enc.names[code] = name
			}
		}
	}

	if prog != nil {
		f.prog, f.source = prog, SourceEmbedded
		return nil
	}
	req := Request{
		BaseFont: baseFont,
		Subtype:  f.subtype,
		Flags:    flags,
		Encoding: simpleEncodingName(ctx, doc, fontDict),
	}
	return f.substitute(ctx, req, opts)
}

// implicitEncoding names the base encoding of a simple font whose program
// has no built-in encoding litho reads. ISO 32000-1, 9.6.6.1, Table 114
// gives a nonsymbolic font StandardEncoding. A nonsymbolic TrueType font
// takes WinAnsiEncoding instead: its program has no glyph-name encoding to
// fall back on, and PDF.js gives it WinAnsi (evaluator.js,
// extractDataStructures), as litho's text extraction does. A symbolic
// font's built-in encoding is its program's, so it gets none here.
func implicitEncoding(subtype string, symbolic, embedded bool) string {
	switch {
	case subtype == "TrueType" && !symbolic:
		return encWinAnsi
	case symbolic && embedded:
		return ""
	default:
		return encStandard
	}
}

// loadType0 reads a Type 0 font and its descendant CIDFont (ISO 32000-1,
// 9.7.6.1).
func (f *Font) loadType0(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, opts Options) error {
	f.cids = encodingCIDMap(ctx, doc, fontDict)
	descendants, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil {
		doc.Logger.Debug("glyph: resolving /DescendantFonts", slog.Any("err", err))
	}
	var cidFont ast.PdfDict
	if len(descendants) > 0 {
		cidFont, err = document.ResolveDict(ctx, doc, descendants[0])
		if err != nil {
			doc.Logger.Debug("glyph: resolving descendant CIDFont", slog.Any("err", err))
		}
	}
	f.subtype = nameOf(ctx, doc, cidFont["Subtype"])
	if f.subtype == "CIDFontType2" {
		f.cidToGID = resolveCIDToGIDMap(ctx, doc, cidFont["CIDToGIDMap"])
	}
	desc := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if _, prog := embeddedProgram(ctx, doc, desc); prog != nil {
		f.prog, f.source = prog, SourceEmbedded
		return nil
	}
	req := Request{
		BaseFont: nameOf(ctx, doc, cidFont["BaseFont"]),
		Subtype:  f.subtype,
		Flags:    int(document.Float(desc["Flags"])),
		Encoding: nameOf(ctx, doc, fontDict["Encoding"]),
		Ordering: cidOrdering(ctx, doc, cidFont),
	}
	return f.substitute(ctx, req, opts)
}

// encodingCIDMap returns the code-to-CID reading of a Type 0 font's
// /Encoding: nil for Identity-H and Identity-V, whose two-byte code is the
// CID (ISO 32000-1, 9.7.5.2, Table 118), a predefined CMap of Table 118 or a
// character collection's own CMap, or an embedded CMap stream (9.7.5.3). An
// encoding litho cannot read also gives nil, and its codes are then read as
// two-byte CIDs, the Identity reading PDF.js falls back on.
func encodingCIDMap(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) *cmap.CIDMap {
	encObj := fontDict["Encoding"]
	resolved, err := document.ResolveObject(ctx, doc, encObj)
	if err != nil {
		doc.Logger.Debug("glyph: resolving Type 0 /Encoding", slog.Any("err", err))
		return nil
	}
	switch v := resolved.(type) {
	case ast.PdfName:
		if v == "Identity-H" || v == "Identity-V" {
			return nil
		}
		if m := cmap.LoadPredefinedCIDMap(string(v)); m != nil {
			return m
		}
		return cmap.PredefinedCIDBase(string(v))
	case ast.PdfStream:
		return typography.EmbeddedEncodingCIDMap(ctx, doc, encObj)
	}
	return nil
}

// cidOrdering returns a CIDFont's character collection, its /CIDSystemInfo
// Registry and Ordering joined by a hyphen (ISO 32000-1, 9.7.3).
func cidOrdering(ctx context.Context, doc *document.Document, cidFont ast.PdfDict) string {
	info, err := document.ResolveDict(ctx, doc, cidFont["CIDSystemInfo"])
	if err != nil || info == nil {
		return ""
	}
	registry := stringOf(ctx, doc, info["Registry"])
	ordering := stringOf(ctx, doc, info["Ordering"])
	if registry == "" || ordering == "" {
		return ""
	}
	return registry + "-" + ordering
}

// Codes splits a shown string into its character codes: one byte each for a
// simple or Type 3 font (ISO 32000-1, 9.6.6), and by the encoding CMap's
// codespace ranges for a Type 0 font (9.7.6.2), two bytes each under
// Identity-H and Identity-V (9.7.5.2, Table 118). A trailing byte an Identity
// encoding leaves alone is a code of its own, which selects .notdef.
func (f *Font) Codes(data []byte) []Code {
	out := make([]Code, 0, len(data))
	for len(data) > 0 {
		n := 1
		if f.kind == kindType0 {
			if f.cids != nil {
				n, _ = f.cids.CodeLength(data)
			} else {
				n = 2
			}
			n = min(max(n, 1), len(data))
		}
		code := Code{Bytes: data[:n:n]}
		for _, b := range code.Bytes {
			code.Value = code.Value<<8 | uint32(b)
		}
		out = append(out, code)
		data = data[n:]
	}
	return out
}

// Glyph resolves code to the glyph it draws. It fails only when ctx is done:
// a glyph the program cannot draw comes back marked [Glyph.Unreadable] with
// an empty outline.
func (f *Font) Glyph(ctx context.Context, code Code) (Glyph, error) {
	if err := ctx.Err(); err != nil {
		return Glyph{}, fmt.Errorf("resolving glyph: %w", err)
	}
	g := Glyph{Advance: f.metrics.CodeWidth(code.Bytes) / 1000}
	if f.metrics.WMode == 1 {
		w1, vx, vy := f.metrics.CodeVerticalMetrics(code.Bytes)
		g.W1, g.V = w1/1000, geom.Point{X: vx / 1000, Y: vy / 1000}
	}
	switch {
	case f.kind == kindType3:
		f.type3Glyph(ctx, code, &g)
		return g, nil
	case f.source == SourceEmbedded:
		f.embeddedGlyph(ctx, code, &g)
	case f.source == SourceSubstitute:
		f.substituteGlyph(ctx, code, &g)
	default:
		f.placeholderGlyph(code, &g)
	}
	return g, nil
}

// embeddedGlyph draws code from the embedded program, or its .notdef glyph
// when the program holds no glyph for it.
func (f *Font) embeddedGlyph(ctx context.Context, code Code, g *Glyph) {
	g.Source = SourceEmbedded
	ref, ok := f.selectEmbedded(code)
	if !ok {
		g.Notdef = true
		if ref, ok = f.prog.notdef(); !ok {
			g.Matrix = geom.Matrix{0.001, 0, 0, 0.001, 0, 0}
			return
		}
	}
	path, m, _, err := f.prog.draw(ctx, ref)
	g.Matrix = m
	if err != nil {
		f.logger.Debug("glyph: drawing embedded glyph", slog.Any("err", err))
		g.Unreadable = true
		return
	}
	g.Outline = path
}

// selectEmbedded returns the glyph of the embedded program code selects.
func (f *Font) selectEmbedded(code Code) (glyphRef, bool) {
	if f.kind == kindType0 {
		cid, mapped := f.cid(code)
		if !mapped {
			return glyphRef{}, false
		}
		return f.cidGlyph(cid)
	}
	b := code.Bytes[0]
	if t := f.prog.sfnt(); t != nil {
		return f.trueTypeGlyph(t, b)
	}
	return f.prog.byName(f.enc.names[b])
}

// cid returns the CID a Type 0 font's code selects and whether a mapping of
// its encoding gave it; an unmapped code selects .notdef (ISO 32000-1,
// 9.7.6.3).
func (f *Font) cid(code Code) (int, bool) {
	if f.cids == nil {
		return int(code.Value), len(code.Bytes) == 2
	}
	decoded := f.cids.Decode(code.Bytes)
	if len(decoded) == 0 {
		return 0, false
	}
	return decoded[0].CID, decoded[0].Mapped
}

// cidGlyph returns the glyph a CID selects: through /CIDToGIDMap for a
// CIDFontType2 font, through the charset of a CID-keyed CFF program for a
// CIDFontType0 font (ISO 32000-1, 9.7.4.2). A CID the map does not cover
// selects glyph 0, .notdef.
func (f *Font) cidGlyph(cid int) (glyphRef, bool) {
	if f.subtype == "CIDFontType2" {
		gid := cid
		if f.cidToGID != nil {
			if cid < 0 || cid >= len(f.cidToGID) {
				return glyphRef{}, false
			}
			gid = int(f.cidToGID[cid])
		}
		if gid == 0 {
			return glyphRef{}, false
		}
		return f.prog.byGID(gid)
	}
	return f.prog.byCID(cid)
}

// trueTypeGlyph selects the glyph of a simple font's TrueType or OpenType
// program for code b by the routes of ISO 32000-1, 9.6.6.4. A font whose
// Symbolic flag is clear and that names an encoding or sets the Nonsymbolic
// flag takes the glyph name's route: the name's character in the (3,1) cmap,
// or its Mac OS Roman code in the (1,0) cmap. Any other takes the code's
// route: the
// (3,0) cmap at the code in each of the ranges 0x0000, 0xF000, 0xF100 and
// 0xF200, then the (1,0) cmap. Either route that finds nothing falls to the
// other, then to the post table's name for the code, and a program without a
// cmap takes the code as its glyph ID; this lenience is PDF.js's (fonts.js,
// getCharCodeToGlyphIdMap), which draws the producers whose fonts break
// 9.6.6.4's preconditions.
func (f *Font) trueTypeGlyph(t *sfntTables, b byte) (glyphRef, bool) {
	name := f.enc.names[b]
	byName := func() (int, bool) {
		if name == "" {
			return 0, false
		}
		if r, ok := glyphRune(name); ok && t.unicode != nil {
			if gid, ok := t.unicode[uint32(r)]; ok && gid != 0 { //nolint:gosec // a glyph name's rune is a Unicode scalar value.
				return int(gid), true
			}
		}
		if t.mac != nil {
			if mc, ok := macRomanCodes()[name]; ok {
				if gid, ok := t.mac[uint32(mc)]; ok && gid != 0 {
					return int(gid), true
				}
			}
		}
		return 0, false
	}
	byCode := func() (int, bool) {
		if t.symbol != nil {
			for _, hi := range [...]uint32{0x0000, 0xF000, 0xF100, 0xF200} {
				if gid, ok := t.symbol[hi|uint32(b)]; ok && gid != 0 {
					return int(gid), true
				}
			}
		}
		if t.mac != nil {
			if gid, ok := t.mac[uint32(b)]; ok && gid != 0 {
				return int(gid), true
			}
		}
		return 0, false
	}
	routes := [2]func() (int, bool){byCode, byName}
	if f.nameRoute {
		routes = [2]func() (int, bool){byName, byCode}
	}
	for _, route := range routes {
		if gid, ok := route(); ok {
			return f.prog.byGID(gid)
		}
	}
	if name != "" {
		if ref, ok := f.prog.byName(name); ok && ref.gid != 0 {
			return ref, true
		}
	}
	if t.unicode != nil {
		// A symbolic font with a Unicode cmap alone: PDF.js reads the code
		// as the character (fonts.js, getCharCodeToGlyphIdMap).
		if gid, ok := t.unicode[uint32(b)]; ok && gid != 0 {
			return f.prog.byGID(int(gid))
		}
	}
	if !t.hasCmap() && b != 0 {
		return f.prog.byGID(int(b))
	}
	return glyphRef{}, false
}

// nameOf resolves obj to a name, "" when it is none.
func nameOf(ctx context.Context, doc *document.Document, obj ast.PdfObject) string {
	if obj == nil {
		return ""
	}
	n, err := document.ResolveName(ctx, doc, obj)
	if err != nil {
		return ""
	}
	return string(n)
}

// stringOf resolves obj to the text of a string or a name, "" otherwise.
func stringOf(ctx context.Context, doc *document.Document, obj ast.PdfObject) string {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return ""
	}
	switch v := resolved.(type) {
	case ast.PdfString:
		return string(v)
	case ast.PdfName:
		return string(v)
	}
	return ""
}

// simpleEncodingName names a simple font's predefined encoding, its
// /Encoding name or its dictionary's /BaseEncoding, "" when it names none.
func simpleEncodingName(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) string {
	resolved, err := document.ResolveObject(ctx, doc, fontDict["Encoding"])
	if err != nil {
		return ""
	}
	switch v := resolved.(type) {
	case ast.PdfName:
		return string(v)
	case ast.PdfDict:
		return nameOf(ctx, doc, v["BaseEncoding"])
	}
	return ""
}

// Font descriptor flags (ISO 32000-1, 9.8.2, Table 123).
const (
	flagFixedPitch  = 1 << 0
	flagSerif       = 1 << 1
	flagSymbolic    = 1 << 2
	flagNonsymbolic = 1 << 5
	flagItalic      = 1 << 6
	flagForceBold   = 1 << 18
)

// hasSubsetTag reports a /BaseFont that opens with a subset tag, six
// uppercase letters and a plus sign (ISO 32000-1, 9.6.4).
func hasSubsetTag(name string) bool {
	if len(name) < 7 || name[6] != '+' {
		return false
	}
	return strings.IndexFunc(name[:6], func(r rune) bool { return r < 'A' || r > 'Z' }) < 0
}
