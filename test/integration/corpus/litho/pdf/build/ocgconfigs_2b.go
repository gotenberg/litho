package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("ocgconfigs_2b", ocgconfigs2b) }

// ocgconfigs2b builds ocgconfigs_2b.pdf: an /Order left incomplete in /Configs alone.
//
// ISO 19005-2, 6.9 t3 and ISO 19005-4, 6.10 t3 require the /Order array of any
// optional content configuration dictionary to reference every OCG in the file.
// veraPDF tests it on each configuration, the default /D and every /Configs
// entry (ISO 32000-1, Table 100). The one corpus file with an incomplete /Configs
// /Order, complex_ua2.pdf, also fails /D, so a check reading /D alone reported
// the rule there anyway and nothing isolated the alternate configurations.
//
// Two OCGs, "Shown" and "Hidden", and four configurations, each named:
//
// | Configuration      | /Order                         | Verdict           |
// | ------------------ | ------------------------------ | ----------------- |
// | /D "Default"       | both OCGs                      | silent            |
// | /Configs[0] direct | "Shown" alone                  | 6.9 t3            |
// | /Configs[1] direct | both, inside a labelled group  | silent, a control |
// | /Configs[2] object | empty                          | 6.9 t3            |
//
// The page draws nothing and the XMP packet claims PDF/A-2b. Every count was
// confirmed with live veraPDF 1.30.2. See ../FIXTURES.md for the authoritative
// expected firing set.
func ocgconfigs2b() ([]byte, error) {
	pdf := pdfgen.New()
	pdf.AddBlankPage(612, 792)

	shown := pdf.Indirect(d("Type", n("OCG"), "Name", text("Shown")))
	hidden := pdf.Indirect(d("Type", n("OCG"), "Name", text("Hidden")))
	empty := pdf.Indirect(d("Name", text("Empty"), "Order", a()))

	pdf.Root.Set("OCProperties", d(
		"OCGs", a(shown, hidden),
		"D", d("Name", text("Default"), "Order", a(shown, hidden), "OFF", a(hidden)),
		"Configs", a(
			d("Name", text("Partial"), "Order", a(shown)),
			d("Name", text("Grouped"), "Order", a(a(text("Layers"), shown, hidden))),
			empty,
		),
	))
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))

	return save(pdf, "1.7")
}
