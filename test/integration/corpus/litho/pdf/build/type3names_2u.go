package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("type3names_2u", type3names2u) }

// type3names2u builds type3names_2u.pdf: Type 3 fonts with no /ToUnicode whose glyph names
// are the only statement of what their glyphs mean.
//
// ISO 19005-1, 6.3.8 asks a PDF/A-1 level A font for a /ToUnicode CMap, and
// veraPDF's PDF/A-1 flavour reads a Type 3 glyph's Unicode value from the CMap
// alone. ISO 19005-2, 6.2.11.7.2 and ISO 14289, 7.21.7 and 8.4.5.8 accept a glyph
// name veraPDF finds in the Adobe Glyph List, the Symbol font or ZapfDingbats,
// and refuse a uniXXXX spelling. Each font rides on its own resource name:
//
//	TN  a Type 3 font whose /Differences names 41 A, 42 alpha, 43 a12 and 44
//	    uni00E9, all shown. Every name states a value, so remediation writes a
//	    /ToUnicode from them: 6.3.8 at PDF/A-1a, and 44 at PDF/A-2u and PDF/UA,
//	    clear.
//	TP  a Type 3 font whose /Differences names 41 A and 42 g12, both shown. g12
//	    is the producer's own name and states nothing, so remediation writes no
//	    /ToUnicode: a map leaving 42 out would claim a repair veraPDF does not
//	    see.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func type3names2u() ([]byte, error) {
	pdf := pdfgen.New()

	type3 := func(names []string) *pdfgen.Dict {
		proc := pdf.MakeIndirect(pdf.Stream([]byte("1000 0 0 0 1000 1000 d1 0 0 1000 1000 re f")))
		charprocs := d()
		diffs := a(65)
		widths := a()
		for _, nm := range names {
			charprocs.Set(nm, proc)
			diffs.Append(n(nm))
			widths.Append(1000)
		}
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"FontBBox", a(0, 0, 1000, 1000),
			"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
			"CharProcs", charprocs,
			"Encoding", d("Type", n("Encoding"), "Differences", diffs),
			"FirstChar", 65,
			"LastChar", 64+len(names),
			"Widths", widths,
			"Resources", d(),
		))
	}

	fonts := d("TN", type3([]string{"A", "alpha", "a12", "uni00E9"}), "TP", type3([]string{"A", "g12"}))
	body := []byte("BT 10 700 Td\n/TN 12 Tf (ABCD) Tj\n0 -20 Td /TP 12 Tf (AB) Tj\nET\n")
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", fonts),
		"Contents", pdf.MakeIndirect(pdf.Stream(body)),
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.4")
}
