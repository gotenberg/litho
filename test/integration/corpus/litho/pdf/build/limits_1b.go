package build

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("limits_1b", limits1b) }

// limits1b builds limits_1b.pdf: implementation limits from ISO 19005-1, 6.1.12.
//
// Part 1 and part 2 cap different things, so `limits_2b.pdf` never reaches this
// clause. Its 40000-byte string breaks the part-2 cap of 32767 and sits well
// inside the part-1 cap of 65535, and the array and dictionary caps exist in
// part 1 only. A coverage scan therefore still showed `t1`, `t2`, `t3`, `t5`,
// `t6`, `t7` and `t10` unreached.
//
// Every breach lives in a different indirect object, and every one of them is
// reachable from the page: four sibling entries of the page-piece dictionary,
// two sibling annotations, two fonts. The page-piece dictionary is the masking
// probe. A check that reports the first breach in that bag and stops reports one
// rule where four are due.
//
// Each breach is paired with a control at exactly the limit, in its own object,
// so an off-by-one in either direction shows as a wrong firing count rather than
// as silence:
//
//	2147483648, -2147483649   against   2147483647, -2147483648
//	32767.5, -32767.5         against   32767.0, -32767.0
//	a 65536-byte string       against   a 65535-byte string
//	an 8192-element array     against   an 8191-element array
//	a 4096-entry dictionary   against   a 4095-entry dictionary
//	a maximal CID of 65536    against   a maximal CID of 65535
//
// PDF defines no standard dictionary that carries an arbitrary number, so the
// numeric and container probes sit in the page-piece dictionary, which
// ISO 32000-1, 14.5 reserves for private application data. ISO 32000-1,
// Table 30 then requires the page `/LastModified` that accompanies it.
//
// `t7` stays unreached on purpose. It needs more than 8388607 indirect objects,
// whose cross-reference table alone runs to 168 MB at 20 bytes an entry.
//
// The overflowing CMap range is one the content never renders. A rendered CID
// above 65535 additionally trips litho's part-1 glyph-presence check on a font
// that embeds no program, which veraPDF exempts, and this fixture is about
// implementation limits rather than that divergence.
//
// Deliberately no /Metadata and no /OutputIntents, as in `limits_2b.pdf`.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func limits1b() ([]byte, error) {
	pdf := pdfgen.New()

	// t1: the first integer past each end of the 32-bit range, against the two
	// values that sit on it.
	intsOver := pdf.Indirect(d("Hi", 2147483648, "Lo", -2147483649))
	intsEdge := pdf.Indirect(d("Hi", 2147483647, "Lo", -2147483648))

	// t2: the same, half a unit past each end. Decimal keeps the token verbatim.
	// pikepdf writes a plain Python float of 32767.0 as the integer token `32767`,
	// which is a CosInteger and tests nothing.
	realsOver := pdf.Indirect(d("Hi", pdfgen.Real("32767.5"), "Lo", pdfgen.Real("-32767.5")))
	realsEdge := pdf.Indirect(d("Hi", pdfgen.Real("32767.0"), "Lo", pdfgen.Real("-32767.0")))

	// t5 and t6: one element and one entry past the container limits.
	zeros := func(count int) *pdfgen.Array {
		arr := a()
		for range count {
			arr.Append(0)
		}
		return arr
	}
	keys := func(count int) *pdfgen.Dict {
		dd := d()
		for i := range count {
			dd.Set(fmt.Sprintf("K%04d", i), 0)
		}
		return dd
	}
	arrayOver := pdf.MakeIndirect(zeros(8192))
	arrayEdge := pdf.MakeIndirect(zeros(8191))
	dictOver := pdf.Indirect(keys(4096))
	dictEdge := pdf.Indirect(keys(4095))

	modified := text("D:20240101000000+00'00'")

	// probe pairs one breach with its control in a page-piece data
	// dictionary.
	probe := func(over, edge pdfgen.Object) *pdfgen.Dict {
		return d("LastModified", modified, "Private", a(over, edge))
	}

	// annotation builds a conforming Square annotation carrying text in
	// /Contents.
	annotation := func(y int, contents string) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Square"),
			"Rect", a(400, y, 500, y+50),
			"F", 4,
			"AP", d("N", pdf.Stream([]byte(""))),
			"Contents", text(contents),
		))
	}

	// t3: the limit is a length below 65536, so 65536 bytes is the first failing
	// one. Both annotations conform in every other respect, which keeps 6.5.3 out
	// of the way.
	stringOver := annotation(700, strings.Repeat("o", 65536))
	stringEdge := annotation(620, strings.Repeat("e", 65535))

	// cmap embeds a CMap whose second range is never rendered and ends at
	// base+255.
	//
	// Codes <0000> to <00FF> carry the rendered text and stay on low CIDs. The
	// maximal CID the file declares is therefore base+255, which is what
	// ISO 19005-1, 6.1.12 caps, without the content ever selecting a glyph the
	// descendant font cannot supply.
	cmap := func(name string, base int) *pdfgen.Stream {
		body := strings.Join([]string{
			"/CIDInit /ProcSet findresource begin",
			"12 dict begin",
			"begincmap",
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) " +
				"/Supplement 0 >> def",
			fmt.Sprintf("/CMapName /%s def", name),
			"/CMapType 1 def",
			"/WMode 0 def",
			"1 begincodespacerange",
			"<0000> <FFFF>",
			"endcodespacerange",
			"2 begincidrange",
			"<0000> <00FF> 1",
			fmt.Sprintf("<F000> <F0FF> %d", base),
			"endcidrange",
			"endcmap",
			"CMapName currentdict /CMap defineresource pop",
			"end",
			"end",
			"",
		}, "\n")
		stream := pdf.Stream([]byte(body))
		stream.Set("Type", n("CMap"))
		stream.Set("CMapName", n(name))
		stream.Set("CIDSystemInfo", d(
			"Registry", text("Adobe"), "Ordering", text("Identity"), "Supplement", 0,
		))
		stream.Set("WMode", 0)
		pdf.MakeIndirect(stream)
		return stream
	}

	// compositeFont builds a Type0 font over an embedded CMap and one
	// descendant CIDFont.
	compositeFont := func(name string, base int) *pdfgen.Dict {
		descendant := pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("CIDFontType2"),
			"BaseFont", n(name),
			"CIDSystemInfo", d(
				"Registry", text("Adobe"), "Ordering", text("Identity"), "Supplement", 0,
			),
			"DW", 1000,
			"CIDToGIDMap", n("Identity"),
			"FontDescriptor", d(
				"Type", n("FontDescriptor"),
				"FontName", n(name),
				"Flags", 4,
				"FontBBox", a(0, 0, 1000, 1000),
				"ItalicAngle", 0,
				"Ascent", 800,
				"Descent", -200,
				"CapHeight", 700,
				"StemV", 80,
			),
		))
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type0"),
			"BaseFont", n(name),
			"Encoding", cmap(name+"-H", base),
			"DescendantFonts", a(descendant),
		))
	}

	// t10: 65281 puts the maximal CID at 65536, one past the cap; 65280 puts it at
	// 65535, exactly on it.
	fontOver := compositeFont("LithoOver", 65281)
	fontEdge := compositeFont("LithoEdge", 65280)

	content := "BT /F0 12 Tf 50 560 Td <0041> Tj ET\n" +
		"BT /F1 12 Tf 50 540 Td <0041> Tj ET\n"

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F0", fontOver, "F1", fontEdge)),
		"Contents", pdf.Stream([]byte(content)),
		"Annots", a(stringOver, stringEdge),
		"LastModified", modified,
		"PieceInfo", d(
			"LithoInteger", probe(intsOver, intsEdge),
			"LithoReal", probe(realsOver, realsEdge),
			"LithoArray", probe(arrayOver, arrayEdge),
			"LithoDict", probe(dictOver, dictEdge),
		),
	)
	pdf.AddPage(pdf.Indirect(page))

	return save(pdf, "1.4")
}
