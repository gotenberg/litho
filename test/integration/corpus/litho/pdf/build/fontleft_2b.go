package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("fontleft_2b", fontleft2b) }

// fontleft2b builds fontleft_2b.pdf: glyph widths a font program cannot answer, and the
// order in which a validator reads a font's program.
//
// Each construct rides on a font of its own, so a check that abandons a font
// after its first bad glyph, or condemns a font wholesale, shows as a count:
//
//	CC  a Type0 font over a CIDFontType0 whose CID-keyed CFF charset holds CIDs
//	    1 and 2. It shows CID 1, whose /W width matches its advance, and CID 3,
//	    which the charset lacks and /W declares 500 wide against the /DW of
//	    1000. veraPDF weighs a CID the charset lacks at the default width, so
//	    CID 3 fails the width rule beside the presence and .notdef rules.
//	CK  the same font showing CID 3 with no /W entry for it: the declared
//	    width is the default too, so the width rule passes. The control.
//	CN  a Type1 over a bare CFF whose /Differences name code 65 .notdef, with
//	    a /Widths entry of 500 against the 600 the program's .notdef advances.
//	    The charset holds .notdef, so the glyph is present; its width fails.
//	T3  a Type 3 font whose encoding names code 66 /b, for which it holds no
//	    glyph procedure, 500 wide in /Widths. With no procedure the program's
//	    width is the missing width, 0, so the width rule fails beside presence.
//	TS  a Type 3 font under a /FontMatrix of 0.01, whose procedure declares 50
//	    against a /Widths entry of 50.5. Both widths are compared as written, in
//	    glyph space, so half a unit apart passes. The control.
//	TF  a composite TrueType font the page draws a form with before its own
//	    text uses it. At PDF/A veraPDF reads the program for the form, which
//	    shows CIDs 5, 6 and 8, all past the three-glyph program, and not for the
//	    page text after it, which shows CID 7: the presence rule fails three
//	    times, where judging the page text first fails it once. At PDF/UA the
//	    form claims the font without its glyphs being judged, and neither fails.
//	TA  a composite TrueType font the page text shows CID 5 through, and an
//	    annotation appearance CIDs 6 and 7. At PDF/A-2 veraPDF reads the program
//	    for the page text, so the presence rule fails once; at PDF/A-1 and
//	    PDF/UA it parses the annotation first and fails it twice.
//	TM  a composite TrueType font shown only in a form drawn while rendering
//	    mode 3 is in force. The form inherits the mode, so its missing CID 5 is
//	    exempt from the presence rule and still fails the .notdef rule.
//	TN  a Type0 font with no /BaseFont of its own over a descendant that names
//	    one. veraPDF reads a Type0 font's name from its descendant, so the
//	    /BaseFont rule passes. The control.
//
// Every count was confirmed with live veraPDF 1.30. See ../FIXTURES.md for the
// authoritative expected firing set.
func fontleft2b() ([]byte, error) {
	pdf := pdfgen.New()

	const nonsymbolic = 32
	ttProgram := ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A", 0x42: "B"}}}, nil)
	cffProgram := cffBuild()
	cidCFFProgram := cffBuildCID([]int{1, 2})

	program := func(data []byte, keys ...any) *pdfgen.Stream {
		stream := pdf.Stream(data)
		for i := 0; i < len(keys); i += 2 {
			stream.Set(keys[i].(string), keys[i+1])
		}
		return pdf.MakeIndirect(stream).(*pdfgen.Stream)
	}

	descriptor := func(name string, fontFile ...any) *pdfgen.Dict {
		return pdf.Indirect(d(append([]any{
			"Type", n("FontDescriptor"),
			"FontName", n("/" + name),
			"Flags", nonsymbolic,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
		}, fontFile...)...))
	}

	type0 := func(name string, descendant *pdfgen.Dict, baseFont bool) *pdfgen.Dict {
		font := d(
			"Type", n("Font"),
			"Subtype", n("Type0"),
			"Encoding", n("/Identity-H"),
			"DescendantFonts", a(pdf.Indirect(descendant)),
		)
		if baseFont {
			font.Set("BaseFont", n("/"+name))
		}
		return pdf.Indirect(font)
	}

	cidInfo := func() *pdfgen.Dict {
		return d("Registry", text("Adobe"), "Ordering", text("Identity"), "Supplement", 0)
	}

	// cffCIDFont is a Type0 font over a CIDFontType0 embedding the CID-keyed
	// CFF.
	cffCIDFont := func(name string, widths *pdfgen.Array) *pdfgen.Dict {
		descendant := d(
			"Type", n("Font"),
			"Subtype", n("CIDFontType0"),
			"BaseFont", n("/"+name),
			"CIDSystemInfo", cidInfo(),
			"FontDescriptor", descriptor(name, "FontFile3", program(cidCFFProgram, "Subtype", n("CIDFontType0C"))),
			"DW", 1000,
			"W", widths,
		)
		return type0(name, descendant, true)
	}

	// ttCIDFont is a Type0 font over a CIDFontType2 embedding the three-glyph
	// TrueType.
	ttCIDFont := func(name string, baseFont bool) *pdfgen.Dict {
		descendant := d(
			"Type", n("Font"),
			"Subtype", n("CIDFontType2"),
			"BaseFont", n("/"+name),
			"CIDSystemInfo", cidInfo(),
			"FontDescriptor", descriptor(name, "FontFile2", program(ttProgram, "Length1", len(ttProgram))),
			"DW", ttAdvanceWidth,
			"CIDToGIDMap", n("Identity"),
		)
		return type0(name, descendant, baseFont)
	}

	type3 := func(name string, matrix, differences, widths *pdfgen.Array, proc string) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"Name", n("/"+name),
			"FontBBox", a(0, 0, 1000, 1000),
			"FontMatrix", matrix,
			"CharProcs", d("a", program([]byte(proc))),
			"Encoding", d("Type", n("Encoding"), "Differences", differences),
			"FirstChar", 65,
			"LastChar", 64+widths.Len(),
			"Widths", widths,
			"Resources", d(),
		))
	}

	fonts := d(
		"CC", cffCIDFont("LithoCIDCFF", a(1, a(600), 3, a(500))),
		"CK", cffCIDFont("LithoCIDCFFDefault", a(1, a(600))),
		"CN", pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type1"),
			"BaseFont", n("/LithoCFFNotdef"),
			"FirstChar", 65,
			"LastChar", 66,
			"Widths", a(500, cffAdvanceWidth),
			"Encoding", d(
				"Type", n("Encoding"),
				"BaseEncoding", n("WinAnsiEncoding"),
				"Differences", a(65, n("/.notdef")),
			),
			"FontDescriptor", descriptor("LithoCFFNotdef", "FontFile3", program(cffProgram, "Subtype", n("Type1C"))),
		)),
		"T3", type3("LithoT3", a(0.001, 0, 0, 0.001, 0, 0), a(65, n("a"), n("b")), a(600, 500),
			"600 0 0 0 500 700 d1 0 0 500 700 re f"),
		"TS", type3("LithoT3Scaled", a(0.01, 0, 0, 0.01, 0, 0), a(65, n("a")), a(50.5),
			"50 0 0 0 50 70 d1 0 0 50 70 re f"),
		"TF", ttCIDFont("LithoCIDFormFirst", true),
		"TA", ttCIDFont("LithoCIDAnnotation", true),
		"TM", ttCIDFont("LithoCIDInherited", true),
		"TN", ttCIDFont("LithoCIDUnnamed", false),
	)

	// form is a Form XObject naming the listed page fonts in its own
	// resources.
	form := func(body string, fontNames []string) *pdfgen.Stream {
		stream := pdf.Stream([]byte(body))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 612, 792))
		named := d()
		for _, name := range fontNames {
			named.Set(name, fonts.Get(name))
		}
		stream.Set("Resources", d("Font", named))
		return pdf.MakeIndirect(stream).(*pdfgen.Stream)
	}

	formFirst := form("BT /TF 12 Tf 50 500 Td <000500060008> Tj ET", []string{"TF"})
	inherited := form("BT /TM 12 Tf 50 300 Td <0005> Tj ET", []string{"TM"})

	appearance := pdf.Stream([]byte("BT /TA 12 Tf 10 10 Td <00060007> Tj ET"))
	appearance.Set("Type", n("XObject"))
	appearance.Set("Subtype", n("Form"))
	appearance.Set("BBox", a(0, 0, 100, 40))
	appearance.Set("Resources", d("Font", d("TA", fonts.Get("TA"))))
	annot := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Stamp"),
		"Rect", a(400, 700, 500, 740),
		"F", 4,
		"AP", d("N", pdf.MakeIndirect(appearance)),
	))

	content := "BT /CC 12 Tf 50 740 Td <00010003> Tj ET\n" +
		"BT /CK 12 Tf 50 710 Td <0003> Tj ET\n" +
		"BT /CN 12 Tf 50 680 Td (AB) Tj ET\n" +
		"BT /T3 12 Tf 50 650 Td (AB) Tj ET\n" +
		"BT /TS 12 Tf 50 620 Td (A) Tj ET\n" +
		// The form shows TF first, so its glyphs are the ones judged.
		"q /FmFirst Do Q\n" +
		"BT /TF 12 Tf 50 590 Td <0007> Tj ET\n" +
		// TA is shown on the page here and in the annotation appearance.
		"BT /TA 12 Tf 50 560 Td <0005> Tj ET\n" +
		"BT /TN 12 Tf 50 530 Td <0001> Tj ET\n" +
		// Rendering mode 3 set outside the form, inherited by it, then reset.
		"BT 3 Tr ET q /FmInherited Do Q BT 0 Tr ET\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", fonts,
			"XObject", d("FmFirst", formFirst, "FmInherited", inherited),
		),
		"Contents", pdf.Stream([]byte(content)),
		"Annots", a(annot),
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata, both independent of the font
	// clauses above so a font verdict stays visibly separate from them.
	return save(pdf, "1.7")
}
