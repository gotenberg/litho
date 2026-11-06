package embed

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxToUnicodeUnits bounds the UTF-16 code units one ToUnicode entry names.
// Anti-DoS guard, not spec-mandated: an entry names the characters one glyph
// stands for, a ligature or a cluster of a few characters, and 256 units
// (512 bytes) leaves room for any real cluster while keeping a hostile
// caller from writing one glyph's text without bound.
const maxToUnicodeUnits = 256

// clampText cuts text to the characters whose UTF-16 form fits in
// [maxToUnicodeUnits], at a character boundary.
func clampText(text string) string {
	units := 0
	for i, r := range text {
		n := utf16.RuneLen(r)
		if n < 0 {
			// An invalid rune is written as U+FFFD, one unit.
			n = 1
		}
		if units+n > maxToUnicodeUnits {
			return text[:i]
		}
		units += n
	}
	return text
}

// scaler returns the font's units per em as a float, 1000 when the program
// leaves it unset, and a function converting a font-unit metric to the
// 1000-unit glyph space of a PDF font descriptor.
func (f *Font) scaler() (float64, func(int16) ast.PdfInt) {
	upe := float64(f.unitsPerEm)
	if upe == 0 {
		upe = 1000
	}
	return upe, func(v int16) ast.PdfInt {
		return ast.PdfInt(int(float64(v) * 1000 / upe))
	}
}

// writeDescriptor allocates the FontDescriptor of a font named name whose
// program is the stream fileRef, reached by fileKey: FontFile2 for TrueType
// outlines, FontFile3 for CFF (ISO 32000-1, 9.8.2, Table 126).
func (f *Font) writeDescriptor(alloc *writer.ObjAllocator, name, fileKey string, fileRef ast.PdfIndirectRef) ast.PdfIndirectRef {
	_, scale := f.scaler()
	return alloc.Alloc(ast.PdfDict{
		"Type":               ast.PdfName("FontDescriptor"),
		"FontName":           ast.PdfName(name),
		"Flags":              ast.PdfInt(f.flags),
		"FontBBox":           ast.PdfArray{scale(f.xMin), scale(f.yMin), scale(f.xMax), scale(f.yMax)},
		"ItalicAngle":        ast.PdfReal(f.italicAngle),
		"Ascent":             scale(f.ascent),
		"Descent":            scale(f.descent),
		"CapHeight":          scale(f.ascent),
		"StemV":              ast.PdfInt(80),
		ast.PdfName(fileKey): fileRef,
	})
}

// identitySystemInfo is the CIDSystemInfo of a descendant font Identity-H
// selects glyphs of: Identity-H names the Adobe-Identity-0 collection (ISO
// 32000-1, 9.7.5.2), which the descendant has to name too (9.7.4.2).
func identitySystemInfo() ast.PdfDict {
	return ast.PdfDict{
		"Registry":   ast.PdfString("Adobe"),
		"Ordering":   ast.PdfString("Identity"),
		"Supplement": ast.PdfInt(0),
	}
}

// writeType0 writes, at ref, the Type0 font named name over the descendant
// cidFontRef, with the Identity-H encoding and the ToUnicode CMap cmapRef.
func writeType0(alloc *writer.ObjAllocator, ref ast.PdfIndirectRef, name string, cidFontRef, cmapRef ast.PdfIndirectRef) {
	alloc.AllocAt(ref.ObjectNum, ast.PdfDict{
		"Type":            ast.PdfName("Font"),
		"Subtype":         ast.PdfName("Type0"),
		"BaseFont":        ast.PdfName(name),
		"Encoding":        ast.PdfName("Identity-H"),
		"DescendantFonts": ast.PdfArray{cidFontRef},
		"ToUnicode":       cmapRef,
	})
}

// flateStream returns data as a FlateDecode stream carrying dict (ISO
// 32000-1, 7.4.4), which the embedded programs and the CIDToGIDMap take: a
// font program the subset left whole, as a CID-keyed CFF program is, weighs
// what its subroutines weigh, and Flate takes a fifth to a half off it.
func flateStream(dict ast.PdfDict, data []byte) ast.PdfStream {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	// A bytes.Buffer grows without failing, so neither call can.
	_, _ = w.Write(data)
	_ = w.Close()
	dict["Filter"] = ast.PdfName("FlateDecode")
	dict["Length"] = ast.PdfInt(buf.Len())
	return ast.NewStream(dict, buf.Bytes())
}

// cidWidthArray emits the /W array of a CIDFont (ISO 32000-1, 9.7.4.3),
// keyed by code, scaled to the 1000-unit glyph space. The format used here
// is the sparse "c [w]" variant: one run per code, which is slightly verbose
// but trivial to generate.
func cidWidthArray(widths map[uint16]uint16, unitsPerEm float64) ast.PdfArray {
	if unitsPerEm == 0 {
		unitsPerEm = 1000
	}
	codes := make([]int, 0, len(widths))
	for code := range widths {
		codes = append(codes, int(code))
	}
	sort.Ints(codes)

	result := make(ast.PdfArray, 0, len(codes)*3)
	for _, code := range codes {
		w := float64(widths[uint16(code)]) * 1000 / unitsPerEm //nolint:gosec // code came from a uint16 map
		result = append(result,
			ast.PdfInt(code),
			ast.PdfArray{ast.PdfInt(int(w))},
		)
	}
	return result
}

// toUnicodeCMap generates the ToUnicode CMap stream mapping each 2-byte code
// to the text it stands for (ISO 32000-1, 9.10.3), as PDF/A requires and
// text extraction needs. Each entry is a bfchar whose destination is the
// text in UTF-16BE, so a ligature's code maps to every character it draws
// (Adobe Technical Note #5411). Code 0, .notdef, and empty text get no entry.
func toUnicodeCMap(toUnicode map[uint16]string) ast.PdfStream {
	type entry struct {
		code uint16
		text string
	}
	entries := make([]entry, 0, len(toUnicode))
	for code, text := range toUnicode {
		if code == 0 || text == "" || text == "\x00" {
			continue
		}
		entries = append(entries, entry{code, text})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].code < entries[j].code
	})

	var b strings.Builder
	b.WriteString("/CIDInit /ProcSet findresource begin\n")
	b.WriteString("12 dict begin\n")
	b.WriteString("begincmap\n")
	b.WriteString("/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n")
	b.WriteString("/CMapName /Adobe-Identity-UCS def\n")
	b.WriteString("/CMapType 2 def\n")
	b.WriteString("1 begincodespacerange\n")
	b.WriteString("<0000> <FFFF>\n")
	b.WriteString("endcodespacerange\n")

	// A bfchar block holds at most 100 entries (Adobe Technical Note #5014).
	const chunkSize = 100
	for i := 0; i < len(entries); i += chunkSize {
		end := min(i+chunkSize, len(entries))
		fmt.Fprintf(&b, "%d beginbfchar\n", end-i)
		for _, e := range entries[i:end] {
			fmt.Fprintf(&b, "<%04X> <", e.code)
			for _, u := range utf16.Encode([]rune(clampText(e.text))) {
				fmt.Fprintf(&b, "%04X", u)
			}
			b.WriteString(">\n")
		}
		b.WriteString("endbfchar\n")
	}

	b.WriteString("endcmap\n")
	b.WriteString("CMapName currentdict /CMap defineresource pop\n")
	b.WriteString("end\n")
	b.WriteString("end\n")

	return ast.NewStream(ast.PdfDict{}, []byte(b.String()))
}
