package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("cmaps_2b", cmaps2b) }

// cmaps2b builds cmaps_2b.pdf: CMap embedding and consistency under PDF/A-2b.
//
// The coverage scan showed ISO 19005-2, 6.2.11.3.3 unreached, and it was the
// largest remaining cluster shared by all four part-2 and part-3 profiles. It
// governs the CMaps a Type 0 font uses: which may be named rather than embedded,
// whether an embedded CMap agrees with its own dictionary, and what an embedded
// CMap may pull in through `/UseCMap`.
//
// Three defects sit in three sibling fonts, all drawn on the page, which is the
// masking probe: a check that reports the first offending CMap and stops reports
// one rule where three are due. Two conforming fonts sit beside them, one naming
// a predefined CMap and one embedding a CMap that agrees with itself and pulls in
// only a predefined base.
//
// The `/UseCMap` of font C references an embedded CMap stream rather than naming
// one. That is not decoration: a `/UseCMap` naming a CMap veraPDF cannot resolve
// makes it throw, abandoning the run, so the reference has to point at a real
// stream. See ../FIXTURES.md.
//
// The descendant CIDFonts carry no font program, which is a defect of a different
// clause entirely, so a CMap verdict is visibly separate from the embedding one.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func cmaps2b() ([]byte, error) {
	pdf := pdfgen.New()

	identitySystemInfo := d(
		"Registry", text("Adobe"), "Ordering", text("Identity"), "Supplement", 0,
	)

	// cmapProgram is a minimal but well-formed CMap program.
	cmapProgram := func(name string, wmode int) []byte {
		return []byte("%!PS-Adobe-3.0 Resource-CMap\n" +
			"/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
			"/CIDSystemInfo 3 dict dup begin /Registry (Adobe) def " +
			"/Ordering (Identity) def /Supplement 0 def end def\n" +
			"/CMapName /" + name + " def\n/CMapType 1 def\n" +
			"/WMode " + fmt.Sprint(wmode) + " def\n" +
			"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
			"1 begincidrange\n<0000> <FFFF> 0\nendcidrange\n" +
			"endcmap\nCMapName currentdict /CMap defineresource pop\nend\n" + "end\n")
	}

	// embeddedCMap is one embedded CMap stream. useCMap nil is Python's
	// None; the wmodes default to 0 there.
	embeddedCMap := func(name string, programWMode, dictWMode int, useCMap pdfgen.Object) *pdfgen.Stream {
		s := pdf.Stream(cmapProgram(name, programWMode))
		s.Set("Type", n("CMap"))
		s.Set("CMapName", n("/"+name))
		s.Set("CIDSystemInfo", identitySystemInfo)
		s.Set("WMode", dictWMode)
		if useCMap != nil {
			s.Set("UseCMap", useCMap)
		}
		return pdf.MakeIndirect(s).(*pdfgen.Stream)
	}

	// type0 is a Type 0 font whose descendant carries no font program.
	type0 := func(name string, encoding pdfgen.Object) *pdfgen.Dict {
		descriptor := pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/"+name),
			"Flags", 4,
			"ItalicAngle", 0,
			"Ascent", 880,
			"Descent", -120,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -120, 1000, 880),
		))
		descendant := pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("CIDFontType0"),
			"BaseFont", n("/"+name),
			"CIDSystemInfo", identitySystemInfo,
			"FontDescriptor", descriptor,
			"DW", 1000,
		))
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type0"),
			"BaseFont", n("/"+name),
			"Encoding", encoding,
			"DescendantFonts", a(descendant),
		))
	}

	fonts := d(
		// A names a CMap that is neither one of the predefined set nor embedded.
		"T1", type0("LithoNamedCMap", n("/LithoCustomCMap")),
		// B embeds a CMap whose dictionary /WMode contradicts its program.
		"T2", type0("LithoWModeSplit", embeddedCMap("LithoWModeCMap", 0, 1, nil)),
		// C embeds a CMap that pulls in another embedded CMap whose name is not one
		// of the predefined set.
		"T3", type0(
			"LithoChained",
			embeddedCMap(
				"LithoOuterCMap", 0, 0,
				embeddedCMap("LithoInnerCMap", 0, 0, nil),
			),
		),
		// D and E conform. D names a predefined CMap; E embeds one that agrees with
		// its own dictionary and pulls in a predefined base by name.
		"T4", type0("LithoIdentity", n("/Identity-H")),
		"T5", type0(
			"LithoEmbeddedOK",
			embeddedCMap("LithoConformingCMap", 0, 0, n("/Identity-H")),
		),
	)

	content := ""
	for i, key := range fonts.Keys() {
		content += fmt.Sprintf("BT /%s 12 Tf 50 %d Td <0041> Tj ET\n", key, 720-i*20)
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", fonts),
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	// Deliberately no /Metadata and no /OutputIntents.
	return save(pdf, "1.7")
}
