package typography

import (
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// Font descriptor flags (ISO 32000-1, 9.8.2, Table 123).
const (
	descriptorFixedPitch  = 1 << 0
	descriptorSerif       = 1 << 1
	descriptorSymbolic    = 1 << 2
	descriptorNonsymbolic = 1 << 5
	descriptorItalic      = 1 << 6
)

// WriteStandard14Font allocates in alloc a font dictionary for the standard
// font baseFont, one of the fourteen ISO 32000-1, 9.6.2.2 names, written
// without its program, and returns a reference to it.
//
// The dictionary is complete at every PDF version. ISO 32000-1, 9.6.2.2
// deprecates from PDF 1.5 the special treatment that let a standard font
// leave out /FirstChar, /LastChar, /Widths and /FontDescriptor, and ISO
// 32000-2, 9.6.2.2 requires them, so the dictionary states them all
// ([standard14Widths], [standard14FontDescriptor]): one form a strict reader
// of any version accepts, at about a kilobyte a font uncompressed.
//
// Every font but Symbol and ZapfDingbats names WinAnsiEncoding (ISO 32000-1,
// Annex D.2), the encoding litho writes their text in; those two state no
// /Encoding and draw through their built-in encodings (Annex D.5 and D.6).
// The descriptor holds no font program: a profile that requires every font
// used for rendering to be embedded (ISO 19005-2, 6.2.11.4.1) fails the file
// until a remediation embeds one.
func WriteStandard14Font(alloc *writer.ObjAllocator, baseFont string) (ast.PdfIndirectRef, error) {
	d, ok := standard14Descriptors[baseFont]
	if !ok {
		return ast.PdfIndirectRef{}, fmt.Errorf("%w: %q", ErrNotStandard14, baseFont)
	}
	first, last, widths := standard14Widths(baseFont, d)
	font := ast.PdfDict{
		"Type":           ast.PdfName("Font"),
		"Subtype":        ast.PdfName("Type1"),
		"BaseFont":       ast.PdfName(baseFont),
		"FirstChar":      ast.PdfInt(first),
		"LastChar":       ast.PdfInt(last),
		"Widths":         widths,
		"FontDescriptor": alloc.Alloc(standard14FontDescriptor(baseFont, d)),
	}
	if !d.fontSpecific {
		font["Encoding"] = ast.PdfName("WinAnsiEncoding")
	}
	return alloc.Alloc(font), nil
}

// standard14Widths returns the /FirstChar, /LastChar and /Widths of the
// standard font name (ISO 32000-1, 9.6.2.1, Table 111): the codes from the
// first to the last the font's encoding names a glyph for, each given the WX
// width of its AFM file for the glyph the code selects ([Standard14CodeGlyph]).
//
// Under WinAnsiEncoding a code the table leaves unused above octal 40 selects
// the bullet (ISO 32000-1, Annex D.2, note 4), so it takes the bullet's width,
// as litho's reading of the encoding does. A code a built-in encoding leaves
// unused selects no glyph and takes 0.
func standard14Widths(name string, d standard14Descriptor) (first, last int, widths ast.PdfArray) {
	var w [256]int
	first, last = -1, -1
	for code := range 256 {
		glyph := Standard14CodeGlyph(name, byte(code)) //nolint:gosec // G115: code is bounded to 0..255 by the loop.
		if glyph == "" && !d.fontSpecific && code > 0o40 {
			glyph = "bullet"
		}
		if glyph == "" {
			continue
		}
		width, ok := Standard14Width(name, glyph)
		if !ok {
			continue
		}
		w[code] = int(width)
		if first < 0 {
			first = code
		}
		last = code
	}
	widths = make(ast.PdfArray, 0, last-first+1)
	for code := first; code <= last; code++ {
		widths = append(widths, ast.PdfInt(w[code]))
	}
	return first, last, widths
}

// standard14FontDescriptor returns the font descriptor of the standard font
// name (ISO 32000-1, 9.8, Table 122), every entry it requires of a font that
// is not Type 3 read from the font's AFM file d:
//
//   - /FontName is the font's name, /FontBBox the file's FontBBox, /ItalicAngle
//     its ItalicAngle and /StemV its StdVW.
//   - /Ascent, /Descent and /CapHeight are its Ascender, Descender and
//     CapHeight. The files of Symbol and ZapfDingbats state none of the three,
//     which Adobe Technical Note #5004, section 4 makes optional for a font
//     without Latin characters: /Ascent and /Descent are then the top and the
//     bottom of FontBBox, and /CapHeight the ascent, as a remediation builds a
//     descriptor for a program without a cap height. ISO 32000-2, Table 120
//     asks no /CapHeight of a font without Latin characters, ISO 32000-1,
//     Table 122 asks it of every font.
//   - /Flags (Table 123) is FixedPitch for an IsFixedPitch file, Symbolic for
//     an EncodingScheme of FontSpecific and Nonsymbolic otherwise, Italic for
//     an ItalicAngle other than 0, and Serif for the Times and Courier
//     families, whose glyphs have serifs; an AFM file states no serifs.
func standard14FontDescriptor(name string, d standard14Descriptor) ast.PdfDict {
	ascent, descent, capHeight := d.ascender, d.descender, d.capHeight
	if !d.vertical {
		ascent, descent = d.bbox[3], d.bbox[1]
		capHeight = ascent
	}
	flags := descriptorNonsymbolic
	if d.fontSpecific {
		flags = descriptorSymbolic
	}
	if d.fixedPitch {
		flags |= descriptorFixedPitch
	}
	if strings.HasPrefix(name, "Times-") || strings.HasPrefix(name, "Courier") {
		flags |= descriptorSerif
	}
	if d.italicAngle != 0 {
		flags |= descriptorItalic
	}
	var angle ast.PdfObject = ast.PdfReal(d.italicAngle)
	if d.italicAngle == math.Trunc(d.italicAngle) {
		angle = ast.PdfInt(int(d.italicAngle))
	}
	return ast.PdfDict{
		"Type":        ast.PdfName("FontDescriptor"),
		"FontName":    ast.PdfName(name),
		"Flags":       ast.PdfInt(flags),
		"FontBBox":    ast.PdfArray{ast.PdfInt(d.bbox[0]), ast.PdfInt(d.bbox[1]), ast.PdfInt(d.bbox[2]), ast.PdfInt(d.bbox[3])},
		"ItalicAngle": angle,
		"Ascent":      ast.PdfInt(ascent),
		"Descent":     ast.PdfInt(descent),
		"CapHeight":   ast.PdfInt(capHeight),
		"StemV":       ast.PdfInt(d.stdVW),
	}
}
