package build

import (
	"bytes"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("mclang_2a", mclang2a) }

// mclang2a builds mclang_2a.pdf: marked-content /Lang entries PDF/A level A does not judge.
//
// ISO 19005-2, 6.7.4 holds every /Lang entry, "in the document catalog
// dictionary or in a structure element dictionary or property list", to the
// language tag grammar. veraPDF 1.30.2 judges the property list of every BDC
// operator its content walk reaches, inline or named through /Properties, and
// nothing else; each shape below was measured with a crafted file at PDF/A-1a and
// PDF/A-2a. litho searched the raw bytes of each page's content for "/Lang" and
// read every property list of the page /Properties, and reported the invalid
// tags here:
//
// | Where                                              | /Lang   | Rule   |
// | -------------------------------------------------- | ------- | ------ |
// | inline DP                                          | `3z`    | silent |
// | /P1 in /Properties, which no operator names        | `4w`    | silent |
// | "/Lang (7t)" inside an /ActualText string          | `7t`    | silent |
// | a DP naming /P2, a property list holding `8s`      | `8s`    | silent |
// | inline BDC                                         | `en-US` | silent |
// | BDC naming /P0, a property list holding `fr`       | `fr`    | silent |
//
// The file is tagged, with a structure tree of one /Document element that owns
// no content, so it fails no test of 6.7.4 at all. Its sibling mcform_2a.pdf
// carries the shapes veraPDF does judge and litho missed.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func mclang2a() ([]byte, error) {
	pdf := pdfgen.New()

	content := bytes.Join(
		[][]byte{
			[]byte("/Span <</Lang (3z)>> DP"),
			[]byte("/Span <</ActualText (/Lang (7t))>> BDC EMC"),
			[]byte("/Span /P2 DP"),
			[]byte("/Span <</Lang (en-US)>> BDC EMC"),
			[]byte("/Span /P0 BDC EMC"),
		},
		[]byte("\n"),
	)

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Properties", d(
				"P0", pdf.Indirect(d("Lang", text("fr"))),
				"P1", pdf.Indirect(d("Lang", text("4w"))),
				"P2", pdf.Indirect(d("Lang", text("8s"))),
			),
		),
		"Contents", pdf.Stream(append(content, '\n')),
	))
	pdf.AddPage(page)

	root := pdf.Indirect(d("Type", n("StructTreeRoot")))
	root.Set("K", pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", root)))
	pdf.Root.Set("StructTreeRoot", root)
	pdf.Root.Set("MarkInfo", d("Marked", true))

	metadata := pdf.Stream(xmpPacket(xmpOpts{Part: 2, Conformance: "A"}))
	metadata.Set("Type", n("Metadata"))
	metadata.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(metadata))

	return save(pdf, "1.7", noPreservePDFA, noFixMetadataVersion)
}
