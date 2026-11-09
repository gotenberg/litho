package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("unclosedmc_ua1", unclosedmcUa1) }

// unclosedmcUa1 builds unclosedmc_ua1.pdf: an artifact sequence the content stream never
// closes, at PDF/UA-1.
//
// ISO 32000-1, 14.6 delimits a marked-content sequence by its BMC or BDC and the
// matching EMC, so content after a BMC no EMC closes lies in no sequence at all,
// and ISO 14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1 judge it as content neither
// tagged nor artifacted. veraPDF reads it that way: its semantic content stream
// makes everything after the last balanced EMC one unmarked group. litho counted
// the open /Artifact as marking what followed it, so the page passed. No file of
// the litho or veraPDF corpora reaches it; the pdfjs file that does is not
// remediated by the gate.
//
// The page holds a tagged paragraph, closed, as the control, then an /Artifact
// BMC and a stroked rule with no EMC after it. The document is otherwise a
// conforming PDF/UA-1 file: a structure tree whose /P reaches the paragraph,
// /MarkInfo, /Lang, DisplayDocTitle, an XMP packet carrying dc:title and the
// pdfuaid claim, and the conforming embedded TrueType of baretree_2b.pdf. So at
// PDF/UA-1 and PDF/UA-2 the unclosed sequence is the one content finding.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func unclosedmcUa1() ([]byte, error) {
	pdf := pdfgen.New()

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoBareSans"),
		"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontBBox", a(0, -200, 550, 700),
		"FontFile2", pdf.MakeIndirect(fontFile),
	)
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoBareSans"),
		"FirstChar", 0x41,
		"LastChar", 0x42,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))

	// The paragraph's sequence closes; the artifact's never does, so the rule it
	// strokes is in no sequence.
	content := "/P <</MCID 0>> BDC BT /F1 24 Tf 20 100 Td (AB) Tj ET EMC\n" +
		"/Artifact BMC\n" +
		"20 90 m 180 90 l S\n"
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))
	paragraph := pdf.Indirect(d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page, "K", 0))
	document.Set("K", a(paragraph))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(paragraph)))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	return save(pdf, "1.7", noFixMetadataVersion)
}
