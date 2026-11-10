package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("laterreads_2u", laterreads2u) }

// laterreads2u builds laterreads_2u.pdf: what a later remediation fixer reads of an earlier one.
//
// Each page carries a construct a remediation fixer read past what an earlier
// fixer, or an earlier page, had already settled. No gated corpus file reaches
// them:
//
// Page 1 states no /Resources, and no node of its page tree does, and it has no
// /Contents. Its one annotation, a Square, draws an appearance stream that
// carries an /OPI key (ISO 19005-2, 6.2.9 t1) and names, in its own /Resources,
// an image with /Interpolate true (6.2.8 t3). The graphics repair skipped the
// page, its appearances with it.
//
// Page 2's /Annots is an indirect array: a Screen annotation, which no PDF/A part
// permits, with /CA 0.5 and no appearance, and a Square with /CA 0.5 and no
// appearance. The annotations fixer removes the Screen from the array and nulls
// it; annot-appearance (from part 2) and annot-opacity (part 1) then read the
// array as the file wrote it and wrote the Screen back, repaired, as an orphan.
//
// Page 3's /Annots is an indirect array too: a Hidden FileAttachment written
// inline, whose inline file specification has /F, no /UF and an embedded text
// file, and a Square. annot-flags removes the FileAttachment, which shows
// nowhere, and wrote the filtered array into the page, leaving the array object
// as the file wrote it, for an incremental update to keep and for filespec-fuf
// to repair and write back.
//
// Pages 4 and 5 show text with unembedded ZapfDingbats fonts, which carry no
// /ToUnicode (ISO 19005-2, 6.2.11.7.2):
//
//	Zp  named by the /Resources of pages 4 and 5, shown on page 5 alone. The
//	    fonts repair decided on page 4, which shows no code with it.
//	Zf  named by the form page 5 draws, and by no page.
//	Za  named by the appearance of page 5's annotation, and by no page.
//
// Every annotation but the FileAttachment has /F 4. The image on page 1 is in an
// ICCBased space, and the file carries a CMYK PDF/A output intent: veraPDF looks
// up a /Default space for a device space in the /Resources page 1 lacks, and a
// repair of device colour would give the page /Resources ahead of the graphics
// repair. The XMP packet claims PDF/A-2u.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func laterreads2u() ([]byte, error) {
	const printFlag = 4
	const hidden = 2
	const iccHeaderSize = 128
	const iccVersion21 = 0x02100000

	pdf := pdfgen.New()

	// iccProfile is a structurally minimal ICC profile veraPDF will parse, as
	// remreach_2b.go builds it.
	iccProfile := func(deviceClass, colourSpace string, components int) *pdfgen.Stream {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize)
		copy(header[0:4], iccU32(nil, iccHeaderSize+len(tagTable)))
		copy(header[4:8], "LTHO")
		binary.BigEndian.PutUint32(header[8:12], iccVersion21)
		copy(header[12:16], deviceClass)
		copy(header[16:20], colourSpace)
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		binary.BigEndian.PutUint32(header[68:72], 0x0000F6D6)
		binary.BigEndian.PutUint32(header[72:76], 0x00010000)
		binary.BigEndian.PutUint32(header[76:80], 0x0000D32D)
		stream := pdf.Stream(append(header, tagTable...))
		stream.Set("N", components)
		pdf.MakeIndirect(stream)
		return stream
	}

	// form is a form XObject drawing body against resources.
	form := func(body string, resources *pdfgen.Dict, extra ...any) *pdfgen.Stream {
		s := pdf.Stream([]byte(body))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(0, 0, 20, 20))
		s.Set("Resources", resources)
		for i := 0; i < len(extra); i += 2 {
			s.Set(extra[i].(string), extra[i+1])
		}
		pdf.MakeIndirect(s)
		return s
	}

	// annot is an annotation at height y.
	annot := func(subtype pdfgen.Name, y, flags int, extra ...any) *pdfgen.Dict {
		dd := d(
			"Type", n("Annot"),
			"Subtype", subtype,
			"Rect", a(50, y, 70, y+20),
			"F", flags,
		)
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1]) //nolint:gosec // extra holds key, value pairs.
		}
		return dd
	}

	// page is a page drawing content; nil leaves /Resources or /Contents out.
	page := func(resources *pdfgen.Dict, content *string, annots *pdfgen.Array) {
		dd := d("Type", n("Page"), "MediaBox", a(0, 0, 612, 792))
		if content != nil {
			dd.Set("Contents", pdf.MakeIndirect(pdf.Stream([]byte(*content))))
		}
		if resources != nil {
			dd.Set("Resources", resources)
		}
		if annots != nil {
			dd.Set("Annots", annots)
		}
		pdf.AddPage(pdf.Indirect(dd))
	}
	content := func(s string) *string { return &s }

	// blankAP is an empty /AP /N form, which 6.3.3 asks for and which paints
	// nothing.
	blankAP := func() *pdfgen.Dict {
		return d("N", form("", d()))
	}

	// zapf is an unembedded ZapfDingbats, with no /Encoding and no
	// /ToUnicode.
	zapf := func() *pdfgen.Dict {
		return pdf.Indirect(
			d("Type", n("Font"), "Subtype", n("Type1"), "BaseFont", n("ZapfDingbats")),
		)
	}

	// Page 1: no /Resources anywhere above the page.
	interp := pdf.Stream([]byte("\x80\x80\x80"))
	interp.Set("Type", n("XObject"))
	interp.Set("Subtype", n("Image"))
	interp.Set("Width", 1)
	interp.Set("Height", 1)
	interp.Set("ColorSpace", a(n("ICCBased"), iccProfile("mntr", "RGB ", 3)))
	interp.Set("BitsPerComponent", 8)
	interp.Set("Interpolate", true)
	apOPI := form(
		"q 20 0 0 20 0 0 cm /Im Do Q\n",
		d("XObject", d("Im", pdf.MakeIndirect(interp))),
		"OPI", d("Version", 1.3),
	)
	page(nil, nil, a(pdf.Indirect(annot(n("Square"), 700, printFlag, "AP", d("N", apOPI)))))

	// Page 2: a forbidden Screen beside a Square, in an indirect /Annots.
	screen := pdf.Indirect(annot(n("Screen"), 700, printFlag, "CA", 0.5))
	square := pdf.Indirect(annot(n("Square"), 660, printFlag, "CA", 0.5))
	page(d(), content(""), pdf.MakeIndirect(a(screen, square)).(*pdfgen.Array))

	// Page 3: a Hidden FileAttachment written inline, in an indirect /Annots.
	payload := pdf.Stream([]byte("litho corpus attachment\n"))
	payload.Set("Type", n("EmbeddedFile"))
	attachment := annot(
		n("FileAttachment"),
		700,
		hidden,
		"AP", blankAP(),
		"FS", d("Type", n("Filespec"), "F", text("a.txt"), "EF", d("F", pdf.MakeIndirect(payload))),
	)
	kept := pdf.Indirect(annot(n("Square"), 660, printFlag, "AP", blankAP()))
	page(d(), content(""), pdf.MakeIndirect(a(attachment, kept)).(*pdfgen.Array))

	// Pages 4 and 5: one font shown past the first page naming it, one a form
	// alone names, one an appearance alone names.
	zp, zf, za := zapf(), zapf(), zapf()
	page(d("Font", d("Z", zp)), content("q Q\n"), nil)
	fm := form("BT /D 12 Tf 2 2 Td (#$) Tj ET\n", d("Font", d("D", zf)))
	apFont := form("BT /D 12 Tf 2 2 Td (%&) Tj ET\n", d("Font", d("D", za)))
	page(
		d("Font", d("Z", zp), "XObject", d("Fm", fm)),
		content("BT /Z 12 Tf 50 700 Td (!\") Tj ET\nq /Fm Do Q\n"),
		a(pdf.Indirect(annot(n("Square"), 600, printFlag, "AP", d("N", apFont), "Contents", text("mark")))),
	)

	// The PDF/A output intent makes the initial DeviceGray the text paints in
	// conforming, so the output-intent repair, which runs before graphics, gives
	// page 1 no /Resources of its own.
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho destination"),
			"DestOutputProfile", iccProfile("prtr", "CMYK", 4),
		)),
	))
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "U"}))
	return save(pdf, "1.7")
}
