package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("gstateform_1b", gstateform1b) }

// gstateform1b builds gstateform_1b.pdf: graphics states, form XObjects and an ICC mismatch.
//
// Four rules the coverage scan showed unreached, on three unrelated parts of the
// document, plus the pair that separates the two form XObject prohibitions:
//
// | Construct                                          | Rule                    |
// | -------------------------------------------------- | ----------------------- |
// | an `ExtGState` `/SMask` that is not `/None`        | `6.4/1` (A-1b)          |
// | an `ExtGState` carrying `/HTO`                     | `6.2.5/3` (A-4)         |
// | a form XObject with `/Subtype2 /PS`                | `6.2.5/1` (A-1b)        |
// | an ICC profile whose `/N` contradicts its header   | `6.2.3.2/2` (A-1b)      |
// | a form XObject carrying `/OPI`                     | `6.2.4/2` (A-1b), `6.2.8.1/1` (A-4) |
//
// The two offending form XObjects sit side by side because the prohibitions they
// break are one rule in some parts and two in others. ISO 19005-1 splits them:
// 6.2.4 test 2 answers for `/OPI` on any XObject, 6.2.5 test 1 for a PostScript
// one. ISO 19005-2 and -3 fold both into 6.2.9 test 1. ISO 19005-4, 6.2.8.1 covers
// `/OPI` alone, because ISO 32000-2 removed PostScript XObjects from the format
// and left part 4 nothing to prohibit. A file carrying only one of the two would
// let a checker that reported the wrong one of those rules still look correct.
//
// Every graphics state is invoked with `gs` and every form is drawn with `Do`.
// An `ExtGState` that no content stream selects is not part of the page's state,
// and a validator that judged it anyway would be answering for a resource the
// page never uses. That distinction was measured, not assumed: the same
// dictionaries left unused fire nothing in veraPDF.
//
// Each defect is paired with a conforming twin that differs only in the offending
// entry: `/SMask /None` against the soft mask, a plain `ExtGState` against `/HTO`,
// a plain form against the PostScript and OPI ones, and an ICC profile whose `/N`
// agrees with its header against the one where it does not. The twins share the
// page, so over-suppression and over-reporting both show up as a count.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func gstateform1b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	const iccHeaderSize = 128

	// iccProfile is a structurally minimal ICC profile declaring the given
	// header space.
	iccProfile := func(colourSpace string) []byte {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize)
		copy(header[0:4], iccU32(nil, iccHeaderSize+len(tagTable)))
		copy(header[4:8], "lith")
		binary.BigEndian.PutUint32(header[8:12], 0x02100000)
		copy(header[12:16], "prtr")
		copy(header[16:20], colourSpace)
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		binary.BigEndian.PutUint32(header[68:72], 0x0000F6D6)
		binary.BigEndian.PutUint32(header[72:76], 0x00010000)
		binary.BigEndian.PutUint32(header[76:80], 0x0000D32D)
		return append(header, tagTable...)
	}

	// iccBased is an ICCBased colour space whose /N may or may not match the
	// header.
	iccBased := func(colourSpace string, components int) obj {
		stream := pdf.Stream(iccProfile(colourSpace))
		stream.Set("N", components)
		return pdf.MakeIndirect(a(n("ICCBased"), pdf.MakeIndirect(stream)))
	}

	// form is one form XObject filling its 40 x 40 box, optionally carrying
	// a forbidden entry.
	form := func(extra ...any) *pdfgen.Stream {
		stream := pdf.Stream([]byte("0 0 40 40 re f\n"))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 40, 40))
		stream.Set("Resources", d())
		for i := 0; i < len(extra); i += 2 {
			stream.Set(extra[i].(string), extra[i+1])
		}
		pdf.MakeIndirect(stream)
		return stream
	}

	// The soft mask's own group. It is a transparency group, which is itself
	// forbidden at part 1, so the mask stays visibly separate from the state that
	// selects it.
	mask := form("Group", d("S", n("Transparency"), "CS", n("DeviceGray")))

	extGStates := d(
		// 6.4 t1: a soft mask that is not /None.
		"GSmask", pdf.Indirect(
			d("Type", n("ExtGState"), "SMask", d("S", n("Luminosity"), "G", mask)),
		),
		// The control: the one soft mask value the clause allows.
		"GSnone", pdf.Indirect(d("Type", n("ExtGState"), "SMask", n("/None"))),
		// 6.2.5 t3 at part 4: a halftone origin.
		"GShto", pdf.Indirect(d("Type", n("ExtGState"), "HTO", n("Default"))),
		// The control: a graphics state whose entries no part objects to.
		"GSplain", pdf.Indirect(d("Type", n("ExtGState"), "LW", 1)),
	)

	xobjects := d(
		// 6.2.5 t1 at part 1: a PostScript form XObject. Part 4 has no rule for it.
		"FmPS", form("Subtype2", n("PS")),
		// 6.2.4 t2 at part 1, 6.2.8.1 t1 at part 4: the OPI key.
		"FmOPI", form("OPI", d("Version", d())),
		// The control: a form that carries neither.
		"FmOK", form(),
	)

	colourSpaces := d(
		// 6.2.3.2 t2: three components declared over a four-component header.
		"CSbad", iccBased("CMYK", 3),
		// The control: the same header with the component count it implies.
		"CSok", iccBased("CMYK", 4),
	)

	content := `BT /F1 12 Tf 50 750 Td (graphics states, forms and ICC profiles) Tj ET
q /GSmask gs 50 700 40 40 re f Q
q /GSnone gs 100 700 40 40 re f Q
q /GShto gs 150 700 40 40 re f Q
q /GSplain gs 200 700 40 40 re f Q
q 1 0 0 1 50 640 cm /FmPS Do Q
q 1 0 0 1 100 640 cm /FmOPI Do Q
q 1 0 0 1 150 640 cm /FmOK Do Q
q /CSbad cs 0 0 0 sc 50 580 40 40 re f Q
q /CSok cs 0 0 0 0 sc 100 580 40 40 re f Q
`

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("F1", font),
			"ExtGState", extGStates,
			"XObject", xobjects,
			"ColorSpace", colourSpaces,
		),
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 1, Conformance: "B"}))

	// Deliberately no /OutputIntents, an independent defect that keeps the ICC
	// component count answering for itself rather than for a missing destination.
	return save(pdf, "1.7")
}
