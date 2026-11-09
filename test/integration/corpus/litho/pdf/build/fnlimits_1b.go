package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("fnlimits_1b", fnlimits1b) }

// fnlimits1b builds fnlimits_1b.pdf: PostScript calculator functions past the
// implementation limits.
//
// ISO 19005-1, 6.1.12 and ISO 19005-2, 6.1.13 limit every number a conforming
// file contains, and a type 4 function writes its program as a stream of numbers
// (ISO 32000-1, 7.10.5). veraPDF parses the program of every type 4 function its
// model reaches and judges each number as a CosInteger or a CosReal: the integer
// limit (t1) and, at PDF/A-1, the real limit of 32767 (t2). Its model reaches a
// function as the tint transform of a Separation or DeviceN space a link reaches,
// through a graphics state's /TR or /TR2 and a halftone's /TransferFunction, and
// through the /Functions of a stitching function, but never as a shading's
// /Function. One page draws:
//
// | Function | Reached as                                                 | Program numbers         | Verdict at PDF/A-1b |
// | -------- | ---------------------------------------------------------- | ----------------------- | ------------------- |
// | A        | tint transform of the Separation /Spot cs selects          | 2147483648 and 40000.5  | t1 and t2           |
// | B        | /TransferFunction of the /Spot component of a type 5 /HT   | 99999.5                 | t2                  |
// | E        | inside the stitching tint transform of the Separation /Two | 4294967296              | t1                  |
// | C        | /Function of the shading sh paints                         | 2147483648 and 40000.5  | silent              |
// | F        | tint transform of a Separation the resources list, unused  | 2147483648 and 40000.5  | silent              |
//
// The stitching tint transform of /Two is a direct dictionary naming E, so every
// link to /Two compares it with itself, which ISO 19005-2, 6.2.4.4 t2 must find
// consistent. No corpus file holds a type 4 function past a limit, nor a direct
// tint transform naming another object.
//
// Every count was confirmed with live veraPDF 1.30.2. See ../FIXTURES.md for the
// authoritative expected firing set.
func fnlimits1b() ([]byte, error) {
	pdf := pdfgen.New()

	// type4 is an indirect type 4 function of one input and one output.
	type4 := func(program string) pdfgen.Object {
		return pdf.MakeIndirect(pdf.Stream([]byte(program), "FunctionType", 4, "Domain", a(0, 1), "Range", a(0, 1)))
	}

	fnA := type4("{ 2147483648 pop 40000.5 pop }")
	fnB := type4("{ pop 99999.5 pop 0.5 }")
	fnC := type4("{ 2147483648 pop 40000.5 pop }")
	fnE := type4("{ pop 4294967296 pop 0.5 }")
	fnF := type4("{ 2147483648 pop 40000.5 pop }")

	stitching := d("FunctionType", 3, "Domain", a(0, 1), "Functions", a(fnE), "Bounds", a(),
		"Encode", a(0, 1))
	screen := d("Type", n("Halftone"), "HalftoneType", 1, "Frequency", 60, "Angle", 45, "SpotFunction", n("Round"))
	halftone := d(
		"Type", n("Halftone"),
		"HalftoneType", 5,
		"Default", screen,
		"Spot", d("Type", n("Halftone"), "HalftoneType", 1, "Frequency", 60, "Angle", 45, "SpotFunction", n("Round"),
			"TransferFunction", fnB),
	)
	resources := d(
		"ColorSpace", d(
			"CS1", a(n("Separation"), n("/Spot"), n("DeviceGray"), fnA),
			"CS2", a(n("Separation"), n("/Two"), n("DeviceGray"), stitching),
			"CS3", a(n("Separation"), n("/Unused"), n("DeviceGray"), fnF),
		),
		"ExtGState", d("GS1", d("Type", n("ExtGState"), "HT", halftone)),
		"Shading", d("Sh1", d("ShadingType", 2, "ColorSpace", n("DeviceGray"), "Coords", a(0, 0, 100, 0),
			"Function", fnC)),
	)
	content := "/GS1 gs\n/CS1 cs 1 scn 10 10 50 50 re f\n/CS2 cs 1 scn 70 10 50 50 re f\n" +
		"q 130 10 50 50 re W n /Sh1 sh Q\n"
	page := pdf.Indirect(d("Type", n("Page"), "MediaBox", a(0, 0, 200, 100), "Resources", resources,
		"Contents", pdf.Stream([]byte(content))))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 1, Conformance: "B"}))
	return save(pdf, "1.4")
}
