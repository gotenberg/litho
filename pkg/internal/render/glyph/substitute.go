package glyph

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

// Options configure how fonts load.
type Options struct {
	// Substitutes supplies programs for the fonts a document does not
	// embed. Nil supplies none, and those fonts draw placeholders.
	Substitutes Provider
}

// Request describes a font a document shows without embedding its program,
// for a [Provider] to answer. Its fields mean what the remediation font
// request's do, so a caller adapts one provider to both.
type Request struct {
	// BaseFont is the font name asked for: the font's /BaseFont as the file
	// writes it, a subset tag included, or the standard 14 font that stands
	// in for it ([Font] asks for both, in that order).
	BaseFont string
	// Subtype is the font's /Subtype, the descendant CIDFont's for a Type 0
	// font.
	Subtype string
	// Flags is the font descriptor's /Flags (ISO 32000-1, 9.8.2), 0 when the
	// font has no descriptor.
	Flags int
	// Encoding names the predefined encoding or CMap the font names, "" for
	// none.
	Encoding string
	// Ordering is a Type 0 font's character collection, Registry and
	// Ordering joined by a hyphen, "" for a simple font.
	Ordering string
}

// Provider returns the programs that may serve a font, best first, or none.
// A [Font] draws with the first one that parses. An error aborts loading the
// font. A provider shared by concurrent loads must be safe for concurrent use.
type Provider func(ctx context.Context, req Request) ([]Candidate, error)

// Candidate reads one program a [Provider] offers: a TrueType or OpenType
// program, a bare CFF program or a Type 1 program. It returns nil for a
// program that turns out to be no candidate.
type Candidate func(ctx context.Context) ([]byte, error)

// substitute asks opts.Substitutes for a program to draw a font the document
// does not embed, trying the requests [substituteRequests] lists in turn. A
// font no candidate serves draws placeholders.
func (f *Font) substitute(ctx context.Context, req Request, opts Options) error {
	f.source = SourcePlaceholder
	if opts.Substitutes == nil {
		return nil
	}
	for _, r := range substituteRequests(req) {
		candidates, err := opts.Substitutes(ctx, r)
		if err != nil {
			return fmt.Errorf("asking for a substitute for %q: %w", r.BaseFont, err)
		}
		for _, candidate := range candidates {
			data, err := candidate(ctx)
			if err != nil {
				return fmt.Errorf("reading a substitute for %q: %w", r.BaseFont, err)
			}
			if len(data) == 0 {
				continue
			}
			prog, err := parseProgram(ctx, data)
			if err != nil {
				f.logger.Debug("glyph: substitute program does not parse", slog.String("font", r.BaseFont), slog.Any("err", err))
				continue
			}
			f.prog, f.source = prog, SourceSubstitute
			return nil
		}
	}
	return nil
}

// substituteRequests lists what a font the document does not embed is asked
// for, in order: its own name, which a provider matches against the faces it
// holds; for a CIDFont of an Adobe CJK character collection, the faces of
// [cjkStandIns] that cover the collection's script; then the standard 14
// font (ISO 32000-1, 9.6.2.2) whose metrics stand in for it, the family a
// viewer draws it with, chosen by name where text
// extraction's metric table names one ([typography.Standard14StandIn]) and
// otherwise by the descriptor's /Flags: FixedPitch for Courier, Serif for
// Times, any other for Helvetica, with the weight and slant the flags and the
// name's style words give. A narrow face (Narrow, Condensed, Compressed)
// asks for Helvetica-Narrow first, the width class the provider's
// metric-compatible families answer. A symbolic font (9.8.2, Table 123)
// asks for the stand-in its flags give like any other: its codes select the
// stand-in's glyphs by the names its encoding gives them, as poppler and
// PDFium draw it, where a placeholder would hide text both show.
//
// # Corpus evidence
//
//   - litho ttencoding_2b: two symbolic TrueType fonts no standard font
//     answers to, one under WinAnsiEncoding and one with no /Encoding, show
//     41 42; poppler and PDFium draw A and B with the face they draw the
//     nonsymbolic ones with.
func substituteRequests(req Request) []Request {
	out := []Request{req}
	for _, face := range cjkStandIns[req.Ordering] {
		r := req
		r.BaseFont = face
		out = append(out, r)
	}
	name := req.BaseFont
	if hasSubsetTag(name) {
		name = name[7:]
	}
	std, ok := typography.Standard14StandIn(name)
	if !ok {
		std = flagsStandIn(name, req.Flags)
	}
	if narrow(name) && strings.HasPrefix(std, "Helvetica") {
		r := req
		r.BaseFont = "Helvetica-Narrow" + strings.TrimPrefix(std, "Helvetica")
		out = append(out, r)
	}
	if std != name {
		r := req
		r.BaseFont = std
		out = append(out, r)
	}
	return out
}

// cjkStandIns names, by character collection (ISO 32000-1, 9.7.3, Registry
// and Ordering joined by a hyphen), the PostScript names of the Regular faces
// that cover the collection's script, best first: the region's subset Noto
// Sans CJK font, its pan-CJK Noto Sans CJK face and the Source Han Sans faces
// of the same design. A latin standard 14 face draws none of the characters
// the collection exists for. Poppler draws such a font with the first face
// fontconfig offers that supports the collection's language (GlobalParams.cc,
// getFontLang: Adobe-GB1 zh-cn, Adobe-CNS1 zh-tw, Adobe-Japan1 ja,
// Adobe-Korea1 ko), and the corpus image ships the subset faces, so both
// renderers draw a CJK font the file does not embed with the same face. A
// drawing choice, not spec-mandated: the /CIDSystemInfo of ISO 32000-1,
// 9.7.3 names the character collection, not a font to draw it with.
//
//nolint:gochecknoglobals // static lookup table
var cjkStandIns = map[string][]string{
	"Adobe-Japan1": {"NotoSansJP-Regular", "NotoSansCJKjp-Regular", "SourceHanSansJP-Regular", "SourceHanSans-Regular"},
	"Adobe-Japan2": {"NotoSansJP-Regular", "NotoSansCJKjp-Regular", "SourceHanSansJP-Regular", "SourceHanSans-Regular"},
	"Adobe-Korea1": {"NotoSansKR-Regular", "NotoSansCJKkr-Regular", "SourceHanSansKR-Regular", "SourceHanSansK-Regular"},
	"Adobe-GB1":    {"NotoSansSC-Regular", "NotoSansCJKsc-Regular", "SourceHanSansCN-Regular", "SourceHanSansSC-Regular"},
	"Adobe-CNS1":   {"NotoSansTC-Regular", "NotoSansCJKtc-Regular", "SourceHanSansTW-Regular", "SourceHanSansTC-Regular"},
}

// flagsStandIn names the standard 14 font that stands in for a font no
// metric table names, by its descriptor's /Flags (ISO 32000-1, 9.8.2, Table
// 123) and the style words of its name.
func flagsStandIn(name string, flags int) string {
	lower := strings.ToLower(name)
	bold := flags&flagForceBold != 0 || containsAny(lower, "bold", "black", "heavy", "semibold", "demi")
	italic := flags&flagItalic != 0 || containsAny(lower, "italic", "oblique")
	switch {
	case flags&flagFixedPitch != 0:
		return styled("Courier", "Courier", bold, italic, "Oblique")
	case flags&flagSerif != 0:
		return styled("Times-Roman", "Times", bold, italic, "Italic")
	default:
		return styled("Helvetica", "Helvetica", bold, italic, "Oblique")
	}
}

// styled composes a standard 14 font name: regular, then the -Bold, -Italic
// or -Oblique and -BoldItalic or -BoldOblique faces (ISO 32000-1, 9.6.2.2).
func styled(regular, family string, bold, italic bool, slant string) string {
	switch {
	case bold && italic:
		return family + "-Bold" + slant
	case bold:
		return family + "-Bold"
	case italic:
		return family + "-" + slant
	}
	return regular
}

// narrow reports a font name whose width word asks for a condensed face.
func narrow(name string) bool {
	return containsAny(strings.ToLower(name), "narrow", "condensed", "compressed")
}

func containsAny(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// substituteGlyph draws code from the substitute program: by the glyph name
// a simple font's encoding gives it, then by the character the name, the
// font's /ToUnicode or its collection's table stands for. A glyph whose
// advance differs from the declared width is scaled horizontally to it, so
// it fills the space the text position moves by (ISO 32000-1, 9.2.4). PDF.js
// does the same for a glyph wider than its width (canvas.js, showText, the
// remeasure branch); scaling a narrower one too keeps a substitute line as
// long as the font's own would be. A code the substitute holds no glyph for
// draws a placeholder, unless it selects .notdef ([Font.selectsNotdef]): it
// then draws nothing.
func (f *Font) substituteGlyph(ctx context.Context, code Code, g *Glyph) {
	ref, ok := f.selectSubstitute(code)
	if !ok {
		if f.selectsNotdef(code) {
			g.Source, g.Notdef = SourceSubstitute, true
			g.Matrix = geom.Matrix{0.001, 0, 0, 0.001, 0, 0}
			return
		}
		f.placeholderGlyph(code, g)
		return
	}
	g.Source = SourceSubstitute
	path, m, advance, err := f.prog.draw(ctx, ref)
	g.Matrix = m
	if err != nil {
		f.logger.Debug("glyph: drawing substitute glyph", slog.Any("err", err))
		g.Unreadable = true
		return
	}
	g.Outline = path
	if advance > 0 && g.Advance > 0 && math.Abs(g.Advance/advance-1) > widthTolerance {
		g.Matrix = m.Mul(geom.Matrix{g.Advance / advance, 0, 0, 1, 0, 0})
	}
}

// selectsNotdef reports a simple font's code that stands for no glyph: its
// encoding gives it no glyph name, or the name .notdef, and no /ToUnicode or
// encoding table gives it a character. Such a code selects the .notdef glyph
// of whatever program draws the font (ISO 32000-1, 9.6.6.3), and the .notdef
// of a program the document does not embed is unknown: a substitute's own
// .notdef (DejaVu's box, the URW faces' empty glyph) is another program's
// design. It draws nothing, as poppler and PDFium draw it. A code that stands
// for a character the substitute lacks still draws a placeholder, which shows
// text the page holds and litho cannot draw.
//
// # Corpus evidence
//
//   - litho unembednames_2u: an unembedded Helvetica shows codes 01 and 80,
//     which StandardEncoding leaves unnamed, and a symbolic Arial TrueType
//     codes 00 and 01, which WinAnsiEncoding leaves unnamed; poppler and
//     PDFium draw none of the four.
func (f *Font) selectsNotdef(code Code) bool {
	if f.kind != kindSimple {
		return false
	}
	name := f.enc.names[code.Bytes[0]]
	if name != "" && name != notdefName {
		return false
	}
	_, ok := f.codeRune(code, "")
	return !ok
}

// widthTolerance is the relative difference between a substitute glyph's
// advance and the declared width below which the glyph is drawn unscaled: a
// thousandth, half a unit of the 1000-unit glyph space widths are declared
// in, so a metric-compatible face draws exactly as designed. A drawing
// threshold, not spec-mandated.
const widthTolerance = 0.001

// selectSubstitute returns the substitute's glyph for code. A simple font's
// code selects it by the glyph name its encoding gives the code, then by the
// character [Font.codeRune] reads; a Type 0 font's code by the character
// [Font.cidRune] reads. Either character goes through the substitute's
// Unicode cmap, then the Adobe Glyph List name of the character.
func (f *Font) selectSubstitute(code Code) (glyphRef, bool) {
	name := ""
	var (
		r  rune
		ok bool
	)
	if f.kind == kindSimple {
		name = f.enc.names[code.Bytes[0]]
		if ref, ok := f.prog.byName(name); ok && name != "" && name != notdefName {
			return ref, true
		}
		r, ok = f.codeRune(code, name)
	} else {
		r, ok = f.cidRune(code)
	}
	if !ok {
		return glyphRef{}, false
	}
	if t := f.prog.sfnt(); t != nil && t.unicode != nil {
		if gid, ok := t.unicode[uint32(r)]; ok && gid != 0 { //nolint:gosec // r is a Unicode scalar value.
			return f.prog.byGID(int(gid))
		}
	}
	if gName := encodings.RuneToGlyph(r); gName != name {
		return f.prog.byName(gName)
	}
	return glyphRef{}, false
}

// codeRune returns the character code stands for: the Adobe Glyph List
// reading of the glyph name a simple font's encoding gives it, else the one
// character text extraction's /ToUnicode, encoding or collection table give
// it.
func (f *Font) codeRune(code Code, name string) (rune, bool) {
	if name != "" {
		if r, ok := glyphRune(name); ok {
			return r, true
		}
	}
	m := f.metrics
	if m.ToUnicode != nil {
		if s, ok := m.ToUnicode.Mapping[string(code.Bytes)]; ok {
			if r, size := utf8.DecodeRuneInString(s); r != utf8.RuneError && size == len(s) {
				return r, true
			}
		}
	}
	if f.kind == kindSimple {
		r, ok := m.Encoding[code.Bytes[0]]
		return r, ok && r != 0
	}
	if cid, mapped := f.cid(code); mapped && m.GIDToUnicode != nil && cid >= 0 && cid <= math.MaxUint16 {
		r, ok := m.GIDToUnicode[uint16(cid)] //nolint:gosec // cid is bounded to a uint16 above.
		return r, ok
	}
	return 0, false
}

// cidRune returns the character a Type 0 font's code stands for when the
// font draws with a substitute, from the first of three sources that gives
// one character:
//
//   - the font's own /ToUnicode CMap at the code (ISO 32000-1, 9.10.3);
//   - its Adobe character collection's UCS2 CMap at the CID the encoding
//     selects (9.10.2, steps a to e);
//   - the glyph order of the host face a non-embedded Identity CIDFontType2
//     names, at the CID ([typography.StandardFontGlyphMap]). Under an
//     Identity CMap the code is the CID (9.7.5.2), and with no /CIDToGIDMap
//     stream the CID is the glyph index of the program the file was laid out
//     with (9.7.4.2, Table 117), the face installed where it was produced.
//     9.10.2 leaves such a font's characters to the reader; PDF.js reads the
//     glyph index through the glyph order of the face its name stands for
//     (src/core/fonts.js, fallbackToSystemFont, with the tables of
//     src/core/standard_fonts.js, getGlyphMapForStandardFonts and its Arial
//     Black and Calibri supplements, at the PDFJS_REVISION versions.env
//     pins), and so does litho.
//
// A code no source gives one character draws a placeholder. Neither the CID
// read as a character nor the CID read as a glyph index of the substitute
// stands in: a CID indexes its collection (9.7.3), and /CIDToGIDMap maps CIDs
// to the glyphs of the embedded program only (9.7.4.2), never to a
// substitute's own glyph order. Poppler reads the CID of an Adobe-Identity
// font without /ToUnicode as a character.
//
// # Corpus evidence
//
//   - pdfjs issue15594_reduced: an unembedded Calibri, Identity-H,
//     Adobe-Identity, no /ToUnicode. The Calibri glyph order reads CIDs 9,
//     80, 109, 263, 386 and 442 as ÄÖÜäöü, which litho draws, as PDF.js
//     does; poppler draws U+0009, P, m, ć, Ƃ and ƺ.
func (f *Font) cidRune(code Code) (rune, bool) {
	if f.toUnicode != nil {
		if s, ok := f.toUnicode.Mapping[string(code.Bytes)]; ok {
			if r, size := utf8.DecodeRuneInString(s); r != utf8.RuneError && size == len(s) {
				return r, true
			}
		}
	}
	cid, mapped := f.cid(code)
	if !mapped {
		return 0, false
	}
	if r, ok := f.collection[cid]; ok && r != 0 {
		return r, true
	}
	if f.glyphOrder != nil && cid >= 0 && cid <= math.MaxUint16 {
		r, ok := f.glyphOrder[uint16(cid)] //nolint:gosec // cid is bounded to a uint16 above.
		return r, ok && r != 0
	}
	return 0, false
}

// glyphRune reads a glyph name as the character the Adobe Glyph List, its
// uniXXXX and uXXXX forms included, gives it.
func glyphRune(name string) (rune, bool) {
	r, ok := encodings.GlyphToRuneFull(name)
	return r, ok && r != 0
}

// Placeholder geometry, in thousandths of text space (the glyph space of
// ISO 32000-1, 9.2.4). Drawing choices, not spec-mandated: the default
// ascent and descent of a font with no usable /FontBBox are those of the
// standard Latin faces, and the frame is a twentieth of an em thick, thin
// enough that a narrow glyph still shows its hollow.
const (
	placeholderAscent  = 750
	placeholderDescent = -250
	placeholderFrame   = 50
)

// placeholderGlyph draws code as a hollow box as wide as the declared width
// and as tall as the font's bounding box: what a viewer without the font's
// program shows instead of guessing a glyph. A code that stands for white
// space, and one with no width, draws nothing.
func (f *Font) placeholderGlyph(code Code, g *Glyph) {
	g.Source = SourcePlaceholder
	g.Matrix = geom.Matrix{0.001, 0, 0, 0.001, 0, 0}
	name := ""
	if f.kind == kindSimple {
		name = f.enc.names[code.Bytes[0]]
	}
	if r, ok := f.codeRune(code, name); ok && unicode.IsSpace(r) {
		return
	}
	if f.kind == kindSimple && name == "" && code.WordSpace() {
		return
	}
	width := g.Advance * 1000
	if width <= 0 {
		return
	}
	lo, hi := f.box[1], f.box[3]
	if hi <= lo {
		lo, hi = placeholderDescent, placeholderAscent
	}
	frame := min(placeholderFrame, width/4, (hi-lo)/4)
	var p geom.Path
	rect(&p, 0, lo, width, hi, false)
	rect(&p, frame, lo+frame, width-frame, hi-frame, true)
	g.Outline = p
}

// rect appends a closed rectangle, counterclockwise or, reversed, clockwise,
// so that an inner rectangle drawn reversed cuts a hole under the nonzero
// winding rule (ISO 32000-1, 8.5.3.3.2).
func rect(p *geom.Path, x0, y0, x1, y1 float64, reversed bool) {
	pts := [4]geom.Point{{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: y1}, {X: x0, Y: y1}}
	if reversed {
		pts[1], pts[3] = pts[3], pts[1]
	}
	p.MoveTo(pts[0])
	for _, pt := range pts[1:] {
		p.LineTo(pt)
	}
	p.Close()
}
