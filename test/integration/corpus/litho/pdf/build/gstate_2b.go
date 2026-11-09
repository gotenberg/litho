package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("gstate_2b", gstate2b) }

// gstate2b builds gstate_2b.pdf: transfer functions and halftones in graphics states.
//
// The coverage scan showed ISO 19005-2, 6.2.5 unreached. It forbids transfer
// functions and constrains halftones, and every one of its tests lives in an
// ExtGState dictionary, so the whole clause is authorable without any unusual
// content.
//
// Six graphics states are applied on one page and five carry a different
// defect, so a check that reports the first offending state and stops reports
// one rule where five are due. The sixth is conforming, so over-suppression is
// visible too.
//
// veraPDF judges a graphics state only where a gs sets it, once for the whole
// document when it is an indirect object and once for every gs when it is
// written directly, and an indirect halftone once whatever states name it. The
// same page reaches each of those counts:
//
// | State        | Written  | Set by                           | Rule at 2b       |
// | ------------ | -------- | -------------------------------- | ---------------- |
// | GS7          | directly | two gs                           | `6.2.5/1` x2     |
// | GS8          | directly | no gs                            | silent           |
// | GS9          | directly | two gs, `/RI /Foo`               | `6.2.6/1` x2     |
// | H1, H2       | directly | one gs each, one type 6 `/HT`    | `6.2.5/4` once   |
// | GSn          | directly | one gs in a form a form paints   | `6.2.5/3`        |
// | GSi          | directly | one gs in a form no Do paints    | silent           |
//
// See ../FIXTURES.md for the authoritative expected firing set.
func gstate2b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// A PostScript calculator function, used as a transfer function below.
	transferStream := pdf.Stream([]byte("{ 1 exch sub }"))
	transferStream.Set("FunctionType", 4)
	transferStream.Set("Domain", a(0, 1))
	transferStream.Set("Range", a(0, 1))
	transfer := pdf.MakeIndirect(transferStream)

	// GS1: /TR is forbidden outright (6.2.5 t1).
	gs1 := pdf.Indirect(d("Type", n("ExtGState"), "TR", transfer))

	// GS2: /TR2 is allowed only with the value /Default (t2).
	gs2 := pdf.Indirect(d("Type", n("ExtGState"), "TR2", transfer))

	// GS3: /HTP is forbidden outright (t3).
	gs3 := pdf.Indirect(d("Type", n("ExtGState"), "HTP", a(transfer)))

	// GS4: a halftone must be type 1 or 5; type 6 is not permitted (t4).
	gs4 := pdf.Indirect(d(
		"Type", n("ExtGState"),
		"HT", pdf.Indirect(d(
			"Type", n("Halftone"),
			"HalftoneType", 6,
			"Width", 8,
			"Height", 8,
			"TransferFunction", n("Identity"),
		)),
	))

	// GS5: a halftone shall not carry /HalftoneName (t5). Type 1 keeps the type
	// test satisfied, so the name is the only defect this state contributes.
	gs5 := pdf.Indirect(d(
		"Type", n("ExtGState"),
		"HT", pdf.Indirect(d(
			"Type", n("Halftone"),
			"HalftoneType", 1,
			"HalftoneName", text("named halftone"),
			"Frequency", 60,
			"Angle", 45,
			"SpotFunction", n("Round"),
		)),
	))

	// GS6 conforms: /TR2 set to the one permitted value, a type 1 halftone with
	// no name. A firing against it would be a false positive.
	gs6 := pdf.Indirect(d(
		"Type", n("ExtGState"),
		"TR2", n("Default"),
		"HT", pdf.Indirect(d(
			"Type", n("Halftone"),
			"HalftoneType", 1,
			"Frequency", 60,
			"Angle", 45,
			"SpotFunction", n("Round"),
		)),
	))

	// GS7 and GS9 are written directly and set twice, so each fails its rule
	// twice; GS8 is written directly and set by no gs, so it fails nothing.
	gs7 := d("Type", n("ExtGState"), "TR", transfer)
	gs8 := d("Type", n("ExtGState"), "TR", transfer)
	gs9 := d("Type", n("ExtGState"), "RI", n("/Foo"))

	// H1 and H2 are two direct states naming one indirect type 6 halftone, which
	// fails 6.2.5 t4 once however many states reach it.
	sharedHT := pdf.Indirect(d("Type", n("Halftone"), "HalftoneType", 6, "Width", 8, "Height", 8))
	h1 := d("Type", n("ExtGState"), "HT", sharedHT)
	h2 := d("Type", n("ExtGState"), "HT", sharedHT)

	// form is a form XObject drawing content with its own resources.
	form := func(content string, resources *pdfgen.Dict) pdfgen.Object {
		stream := pdf.Stream([]byte(content))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 60, 30))
		stream.Set("Resources", resources)
		return pdf.MakeIndirect(stream)
	}

	// FmOuter paints FmInner, which sets a direct /HTP state of its own; FmIdle
	// sets a direct /TR state and no Do paints it.
	fmInner := form(
		"/GSn gs 0 0 60 30 re f\n",
		d("ExtGState", d("GSn", d("Type", n("ExtGState"), "HTP", a(transfer)))),
	)
	fmOuter := form("/FmInner Do\n", d("XObject", d("FmInner", fmInner)))
	fmIdle := form(
		"/GSi gs 0 0 60 30 re f\n",
		d("ExtGState", d("GSi", d("Type", n("ExtGState"), "TR", transfer))),
	)

	content := `q /GS1 gs 0 0 0 rg 50 700 60 30 re f Q
q /GS2 gs 50 660 60 30 re f Q
q /GS3 gs 50 620 60 30 re f Q
q /GS4 gs 50 580 60 30 re f Q
q /GS5 gs 50 540 60 30 re f Q
q /GS6 gs 50 500 60 30 re f Q
q /GS7 gs 150 700 60 30 re f Q
q /GS7 gs 150 660 60 30 re f Q
q /GS9 gs 150 620 60 30 re f Q
q /GS9 gs 150 580 60 30 re f Q
q /H1 gs 150 540 60 30 re f Q
q /H2 gs 150 500 60 30 re f Q
q 1 0 0 1 250 700 cm /FmOuter Do Q
BT /F1 12 Tf 50 460 Td (graphics states) Tj ET
`

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("F1", font),
			"ExtGState", d(
				"GS1", gs1, "GS2", gs2, "GS3", gs3, "GS4", gs4, "GS5", gs5, "GS6", gs6,
				"GS7", gs7, "GS8", gs8, "GS9", gs9, "H1", h1, "H2", h2,
			),
			"XObject", d("FmOuter", fmOuter, "FmIdle", fmIdle),
		),
		"Contents", pdf.Stream([]byte(content)),
	)
	pdf.AddPage(pdf.Indirect(page))

	// Deliberately no /Metadata and no /OutputIntents.
	return save(pdf, "1.7")
}
