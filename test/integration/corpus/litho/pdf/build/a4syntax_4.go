package build

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("a4syntax_4", a4syntax4) }

// a4syntax4 builds a4syntax_4.pdf: file and document level rules under PDF/A-4.
//
// The coverage scan showed twenty-one ISO 19005-4 rules unreached outside the
// JPEG2000 family. Eleven of them judge the file or the catalog rather than any
// page, and they are independent of one another, which is what makes them worth
// putting in one file: a check that abandons a document after the first syntax
// complaint reports one rule where eight axes are due.
//
// | Construct                                          | Rule        |
// | -------------------------------------------------- | ----------- |
// | a stream whose `/Length` is not its byte count     | `6.1.6.1/1` |
// | a name object that is not valid UTF-8              | `6.1.7/1`   |
// | an indirect object header with non-conforming spacing | `6.1.8/1` |
// | an `LZWDecode` filter                              | `6.1.9/1`   |
// | a `/Perms` entry other than `UR3` or `DocMDP`      | `6.1.11/1`  |
// | a catalog `/Version` that is not 2.x               | `6.1.12/1`  |
// | a widget annotation carrying `/A`                  | `6.4.1/1`   |
// | a named action other than the four permitted       | `6.6.1/2`   |
// | `pdfaid:rev` bound to a prefix other than `pdfaid` | `6.7.3/6`   |
// | two optional content groups sharing a `/Name`      | `6.10/2`    |
// | `/AlternatePresentations` in the name dictionary   | `6.11/1`    |
//
// Controls sit beside the defects wherever the test turns on a value rather than
// on presence: a widget with no `/A`, a `/NextPage` named action which is one of
// the four the clause permits, a third optional content group with a distinct
// name, and the page content stream, whose `/Length` and spacing are both correct.
//
// Four of the eleven were beyond pikepdf, which first wrote the committed file:
// it computes `/Length` from the data, normalises object headers, and refuses to
// emit a name that is not valid UTF-8. pdfgen reproduces its output, and the
// four are patched into the saved bytes, each replacement exactly the length of
// what it replaces, so no cross-reference offset moves. The builder refuses any
// patch that would change a length rather than silently corrupting the file. See
// `filestruct_1b.go` and `xref_1b.go` for the same technique.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func a4syntax4() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// 6.1.6.1 t1 rides on its own Form XObject, marked so the byte patch below can
	// find exactly one occurrence. The page content stream beside it stays correct
	// and is the control.
	badLengthMark := []byte("% litho bad length marker\n")

	badLength := pdf.Stream(append(append([]byte{}, badLengthMark...), "0.5 0.5 0.5 rg 0 0 20 20 re f\n"...))
	badLength.Set("Type", n("XObject"))
	badLength.Set("Subtype", n("Form"))
	badLength.Set("BBox", a(0, 0, 20, 20))
	badLength.Set("Resources", d())
	pdf.MakeIndirect(badLength)

	// An LZWDecode stream, forbidden as a stream filter. It sits beside the inline
	// image below because the two are judged by different veraPDF objects, a stream
	// filter against an inline image filter, and only the inline one reaches 6.1.9.
	// The data is genuinely LZW-compressed so a validator has to reject the filter
	// by name rather than by failing to decode it.
	lzwPayload := []byte("0 0 1 rg 0 0 20 20 re f\n")
	lzwForm := pdf.Stream(lzwEncode(lzwPayload, 1))
	lzwForm.Set("Type", n("XObject"))
	lzwForm.Set("Subtype", n("Form"))
	lzwForm.Set("BBox", a(0, 0, 20, 20))
	lzwForm.Set("Resources", d())
	lzwForm.Set("Filter", n("LZWDecode"))
	pdf.MakeIndirect(lzwForm)

	// 6.1.7 t1: a name object whose bytes are not valid UTF-8. veraPDF checks names
	// in a few modelled positions only, and a Separation colorant name is one of
	// them; an unknown catalog key, a resource key, a /Differences glyph name and an
	// annotation /Name were all measured silent. pikepdf, which first wrote the
	// file, would not emit an invalid name, so a placeholder of the exact width goes in and the patch below rewrites
	// it: seven bytes either way, /AAAAAA against /#C0#AF, where C0 AF is an
	// overlong encoding and so not valid UTF-8.
	const utf8Placeholder = "AAAAAA"

	tint := pdf.Stream([]byte("{ }"))
	tint.Set("FunctionType", 4)
	tint.Set("Domain", a(0, 1))
	tint.Set("Range", a(0, 1))
	pdf.MakeIndirect(tint)

	badColorant := pdf.MakeIndirect(a(n("Separation"), n("/"+utf8Placeholder), n("DeviceGray"), tint))
	// The control: an otherwise identical Separation whose colorant name is ASCII.
	goodColorant := pdf.MakeIndirect(a(n("Separation"), n("/Spot"), n("DeviceGray"), tint))

	appearance := func() *pdfgen.Stream {
		ap := pdf.Stream([]byte("0 0 0 rg 0 0 60 20 re f\n"))
		ap.Set("Type", n("XObject"))
		ap.Set("Subtype", n("Form"))
		ap.Set("BBox", a(0, 0, 60, 20))
		pdf.MakeIndirect(ap)
		return ap
	}

	// 6.4.1 t1: a widget carrying /A, beside one that carries none.
	widgetWithAction := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", a(50, 600, 110, 620),
		"F", 4,
		"FT", n("Btn"),
		"T", text("acts"),
		"AP", d("N", appearance()),
		"A", d("S", n("Named"), "N", n("NextPage")),
	))
	widgetWithoutAction := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", a(50, 560, 110, 580),
		"F", 4,
		"FT", n("Btn"),
		"T", text("inert"),
		"AP", d("N", appearance()),
	))

	// 6.10 t2 judges optional content CONFIGURATIONS, not the groups themselves:
	// duplicate group names were measured silent. The groups here carry distinct
	// names on purpose, so the file proves the rule is about configurations.
	ocgA := pdf.Indirect(d("Type", n("OCG"), "Name", text("layer one")))
	ocgB := pdf.Indirect(d("Type", n("OCG"), "Name", text("layer two")))

	// 6.1.9 t1 judges an INLINE image filter, which is a different veraPDF object
	// from the stream filter above. /LZW is the inline abbreviation of LZWDecode;
	// the second inline image uses /AHx, one the clause permits, and is the control.
	inlineImages := "q 20 0 0 20 60 360 cm\n" +
		"BI /W 2 /H 2 /CS /G /BPC 8 /F /LZW ID \x80\x0b\x60\x50\x22\x0c\x0c\x85 EI Q\n" +
		"q 20 0 0 20 100 360 cm\n" +
		"BI /W 2 /H 2 /CS /G /BPC 8 /F /AHx ID 00FF00FF> EI Q\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("F1", font),
			"XObject", d("FmL", badLength, "FmZ", lzwForm),
			"ColorSpace", d("CsBad", badColorant, "CsOk", goodColorant),
			"Properties", d("OcA", ocgA, "OcB", ocgB),
		),
		"Contents", pdf.Stream([]byte(
			"BT /F1 12 Tf 50 700 Td (file and document rules) Tj ET\n"+
				"q 1 0 0 1 60 400 cm /FmL Do Q\n"+
				"q 1 0 0 1 120 400 cm /FmZ Do Q\n"+
				"q /CsBad cs 0.5 scn 60 320 20 20 re f Q\n"+
				"q /CsOk cs 0.5 scn 100 320 20 20 re f Q\n"+inlineImages,
		)),
		"Annots", a(widgetWithAction, widgetWithoutAction),
	))
	pdf.AddPage(page)

	pdf.Root.Set("AcroForm", d("Fields", a(widgetWithAction, widgetWithoutAction)))
	// The default configuration and one alternate share a /Name, which is the
	// defect; the second alternate carries a distinct name and is the control.
	pdf.Root.Set("OCProperties", d(
		"OCGs", a(ocgA, ocgB),
		"D", d("Name", text("shared"), "Order", a(ocgA, ocgB), "ON", a(ocgA, ocgB)),
		"Configs", a(
			d("Name", text("shared"), "Order", a(ocgA, ocgB), "ON", a(ocgA, ocgB)),
			d("Name", text("distinct"), "Order", a(ocgA, ocgB), "ON", a(ocgA, ocgB)),
		),
	))

	// 6.6.1 t2: a named action the clause does not permit, beside a permitted one.
	outlineBad := pdf.Indirect(d(
		"Title", text("forbidden named action"),
		"A", d("S", n("Named"), "N", n("/GoForward")),
	))
	outlineOk := pdf.Indirect(d(
		"Title", text("permitted named action"),
		"A", d("S", n("Named"), "N", n("NextPage")),
	))
	outlineBad.Set("Next", outlineOk)
	outlineOk.Set("Prev", outlineBad)
	outlines := pdf.Indirect(d("Type", n("Outlines"), "First", outlineBad, "Last", outlineOk, "Count", 2))
	outlineBad.Set("Parent", outlines)
	outlineOk.Set("Parent", outlines)
	pdf.Root.Set("Outlines", outlines)

	// 6.1.11 t1: /Perms may name only UR3 and DocMDP.
	pdf.Root.Set("Perms", d("Foo", d("Type", n("SigRef"))))

	// 6.1.12 t1: the catalog /Version must be a PDF 2.x version.
	pdf.Root.Set("Version", n("/1.7"))

	// 6.11 t1: alternate presentations are forbidden outright.
	pdf.Root.Set("Names", d("AlternatePresentations", d("Names", a())))

	// 6.7.3 t6: pdfaid:rev must be bound to the pdfaid prefix. Declaring a second
	// prefix on the same namespace and putting rev under it leaves the claim
	// findable but the prefix wrong. pdfaid:part stays on the expected prefix so
	// the file is still identified as PDF/A-4.
	attachXMP(pdf, bytes.ReplaceAll(
		xmpPacket(xmpOpts{Part: 4, Extra: "    <pdfaid2:rev>2020</pdfaid2:rev>"}),
		[]byte(`xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"`),
		[]byte(`xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"`+"\n"+
			`    xmlns:pdfaid2="http://www.aiim.org/pdfa/ns/id/"`),
	))

	data, err := save(pdf, "2.0")
	if err != nil {
		return nil, err
	}

	// 6.1.6.1 t1: /Length must equal the byte count that follows. The declared value
	// sits immediately before the stream keyword introducing the marked payload.
	// Scanning forward avoids the empty /Resources dictionary nested in the same
	// object, which a backwards search for "<<" would find instead.
	payload := bytes.Index(data, badLengthMark)
	if payload < 0 {
		return nil, fmt.Errorf("6.1.6.1 t1: marked stream not found")
	}
	matches := regexp.MustCompile(`/Length (\d+)([^>]*)>>\s*stream\r?\n`).FindAllSubmatchIndex(data[:payload], -1)
	if matches == nil {
		return nil, fmt.Errorf("6.1.6.1 t1: /Length before the marked stream not found")
	}
	match := matches[len(matches)-1]
	digits := data[match[2]:match[3]]
	v, err := strconv.Atoi(string(digits))
	if err != nil {
		return nil, err
	}
	wrong := []byte(strconv.Itoa(v + 1))
	if len(wrong) != len(digits) {
		return nil, fmt.Errorf("6.1.6.1 t1: differing length would change the digit count")
	}
	data = append(append(append([]byte{}, data[:match[2]]...), wrong...), data[match[3]:]...)

	// 6.1.7 t1: C0 AF is an overlong encoding, which UTF-8 forbids. The colorant
	// name appears once, inside the Separation array; the /CsBad key that names the
	// colour space in /Resources is a different name and is left alone.
	data, err = patchOnce(data, []byte("/"+utf8Placeholder), []byte("/#C0#AF"), "6.1.7 t1")
	if err != nil {
		return nil, err
	}

	// 6.1.8 t1: the object header takes exactly one space between each token.
	// Moving one of the two spaces keeps the header the same width.
	header := regexp.MustCompile(`\n(\d+) 0 obj\n`).FindSubmatch(data)
	if header == nil {
		return nil, fmt.Errorf("6.1.8 t1: no object header found")
	}
	num := string(header[1])
	return patchOnce(data,
		[]byte("\n"+num+" 0 obj\n"),
		[]byte("\n"+num+"  0 obj"),
		"6.1.8 t1",
	)
}
