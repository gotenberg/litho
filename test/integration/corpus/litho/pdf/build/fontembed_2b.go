package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("fontembed_2b", fontembed2b) }

// fontembed2b builds fontembed_2b.pdf: unembedded fonts a caller-supplied program can
// embed, in every place a font dictionary sits, and two a program cannot serve.
//
// Every font shows text and embeds no program, so each fails ISO 19005-2,
// 6.2.11.4.1 t1 once. Remediated without a font provider, all nine stay in the
// residual. pkg/pdf's TestFontEmbedFixture remediates the file with synthetic
// programs built at test time (no font program is committed) and pins what each
// font becomes:
//
//	HV  Helvetica, a standard font with no /Widths, indirect, shows "Hello":
//	    embedded over a TrueType program, with the widths its metrics give.
//	TT  LithoSans, a TrueType font under WinAnsiEncoding, indirect: embedded.
//	CF  LithoSerif, a non-symbolic Type 1 font: embedded over a CFF program as
//	    /FontFile3 /Type1C.
//	IN  Helvetica-Bold written inline in the page's direct /Font dictionary:
//	    embedded at a new object number the page's /Font names.
//	FI  LithoSans again, written inline in the direct /Resources of form X1:
//	    embedded, the form's dictionary rewritten around its bytes.
//	SH  Helvetica-Oblique written inline in a /Resources dictionary pages 2 and
//	    3 share as one object: embedded once, reached from both pages.
//	JP  LithoMincho, a Type0 font over an unembedded Adobe-Japan1 CIDFontType0
//	    under UniJIS-UCS2-H, shows U+3042: its descendant becomes a
//	    CIDFontType2 with a /CIDToGIDMap.
//	NO  LithoMissing, a TrueType font no program is supplied for: refused.
//	WD  LithoWide, a TrueType font declaring 900 where the program advances
//	    667: refused, since embedding it would move its text.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func fontembed2b() ([]byte, error) {
	pdf := pdfgen.New()

	descriptor := func(name string, flags int) *pdfgen.Dict {
		return d(
			"Type", n("FontDescriptor"),
			"FontName", n(name),
			"Flags", flags,
			"FontBBox", a(0, -200, 1000, 800),
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
		)
	}

	truetype := func(name string, widths ...any) *pdfgen.Dict {
		return d(
			"Type", n("Font"),
			"Subtype", n("TrueType"),
			"BaseFont", n(name),
			"Encoding", n("WinAnsiEncoding"),
			"FirstChar", 65,
			"LastChar", 65+len(widths)-1,
			"Widths", a(widths...),
			"FontDescriptor", pdf.Indirect(descriptor(name, 32)),
		)
	}

	standard := func(name string) *pdfgen.Dict {
		return d("Type", n("Font"), "Subtype", n("Type1"), "BaseFont", n(name))
	}

	hv := pdf.Indirect(standard("/Helvetica"))
	tt := pdf.Indirect(truetype("/LithoSans", 667, 667))
	cf := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("LithoSerif"),
		"Encoding", n("WinAnsiEncoding"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(722, 667),
		"FontDescriptor", pdf.Indirect(descriptor("/LithoSerif", 34)),
	))
	jp := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type0"),
		"BaseFont", n("LithoMincho"),
		"Encoding", n("/UniJIS-UCS2-H"),
		"DescendantFonts", a(
			pdf.Indirect(d(
				"Type", n("Font"),
				"Subtype", n("CIDFontType0"),
				"BaseFont", n("LithoMincho"),
				"CIDSystemInfo", d(
					"Registry", text("Adobe"),
					"Ordering", text("Japan1"),
					"Supplement", 2,
				),
				"DW", 1000,
				"FontDescriptor", pdf.Indirect(descriptor("/LithoMincho", 4)),
			)),
		),
	))
	no := pdf.Indirect(truetype("/LithoMissing", 667, 667))
	wd := pdf.Indirect(truetype("/LithoWide", 900, 900))

	formBody := []byte("BT 72 400 Td /FI 12 Tf (AB) Tj ET\n")
	formStream := pdf.Stream(formBody)
	formStream.Set("Type", n("XObject"))
	formStream.Set("Subtype", n("Form"))
	formStream.Set("BBox", a(0, 0, 612, 792))
	formStream.Set("Resources", d("Font", d("FI", truetype("/LithoSans", 667, 667))))
	form := pdf.MakeIndirect(formStream)

	first := "BT 72 700 Td\n" +
		"/HV 12 Tf (Hello) Tj\n" +
		"0 -20 Td /TT 12 Tf (AB) Tj\n" +
		"0 -20 Td /CF 12 Tf (AB) Tj\n" +
		"0 -20 Td /IN 12 Tf (AB) Tj\n" +
		"0 -20 Td /JP 12 Tf <3042> Tj\n" +
		"0 -20 Td /NO 12 Tf (AB) Tj\n" +
		"0 -20 Td /WD 12 Tf (AB) Tj\n" +
		"ET\n" +
		"/X1 Do\n"
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("HV", hv, "TT", tt, "CF", cf, "IN", standard("/Helvetica-Bold"), "JP", jp, "NO", no, "WD", wd),
			"XObject", d("X1", form),
		),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(first))),
	))
	pdf.AddPage(page)

	shared := pdf.Indirect(d("Font", d("SH", standard("/Helvetica-Oblique"))))
	for _, y := range []string{"700", "600"} {
		body := "BT 72 " + y + " Td /SH 12 Tf (AB) Tj ET\n"
		other := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", shared,
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte(body))),
		))
		pdf.AddPage(other)
	}

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.7")
}
