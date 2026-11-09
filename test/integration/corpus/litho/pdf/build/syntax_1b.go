package build

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("syntax_1b", syntax1b) }

// syntax1b builds syntax_1b.pdf: stream syntax, actions and the XMP header at PDF/A-1b.
//
// The coverage scan showed ISO 19005-1 6.1.7 and 6.6.1 unreached between them. The
// two are independent axes, which is what makes them worth putting in one file: a
// check that abandons a document after the first syntax complaint reports one rule
// where four are due.
//
// 6.7.5, which forbids `bytes` and `encoding` in the XMP packet header, is not
// here. pikepdf reserialises any packet assigned to the catalog, reordering
// attributes and rewriting the header, so those two attributes survive neither
// authoring nor a length-preserving patch. Reaching that clause needs a
// hand-assembled file.
//
// | Construct                                           | Rule      |
// | --------------------------------------------------- | --------- |
// | a stream whose `/Length` is not its byte count      | `6.1.7/1` |
// | a stream keyword followed by a lone CARRIAGE RETURN | `6.1.7/2` |
// | a named action other than the four permitted        | `6.6.1/2` |
// | a form field carrying an `/A` action                | `6.6.1/3` |
//
// The two 6.1.7 defects sit on separate streams deliberately. A single stream that
// both mis-declares its length and breaks its end-of-line would leave the second
// condition unproven, because a parser that gives up on the first problem never
// reaches it. Each defective stream sits beside the page content stream, which is
// well formed and is the control for both.
//
// `6.6.1` splits by object: test 2 judges a named action, so it rides on an
// outline item, while test 3 judges a form field, so it needs an AcroForm field
// carrying `/A`. Each has a conforming neighbour: a `/NextPage` named action, which
// is one of the four the clause permits, and a second field with no action at all.
//
// Both 6.1.7 defects are beyond pikepdf, which computes `/Length` from the data and
// emits a conforming end-of-line. They are patched into the saved bytes,
// each replacement the same length as what it replaces, so no cross-reference
// offset moves. The builder refuses any patch that would change a length.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func syntax1b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// The two streams that carry the 6.1.7 defects. Each is a Form XObject so the
	// page can invoke it, and each is marked with a distinctive payload so the
	// byte patches below can find exactly one occurrence.
	badLengthMark := []byte("% litho bad length marker\n")
	badEOLMark := []byte("% litho bad eol marker\n")

	markedForm := func(mark []byte) *pdfgen.Stream {
		s := pdf.Stream(append(append([]byte{}, mark...), "0.5 0.5 0.5 rg 0 0 20 20 re f\n"...),
			"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 20, 20), "Resources", d())
		pdf.MakeIndirect(s)
		return s
	}

	badLength := markedForm(badLengthMark)
	badEOL := markedForm(badEOLMark)

	appearance := func() *pdfgen.Stream {
		ap := pdf.Stream([]byte("0 0 0 rg 0 0 60 20 re f\n"),
			"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 60, 20))
		pdf.MakeIndirect(ap)
		return ap
	}

	// 6.6.1 t3: a form field performing an action, beside one that performs none.
	fieldWithAction := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", a(50, 600, 110, 620),
		"F", 4,
		"FT", n("Btn"),
		"T", text("acts"),
		"AP", d("N", appearance()),
		"A", d("S", n("Named"), "N", n("NextPage")),
	))
	fieldWithoutAction := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", a(50, 560, 110, 580),
		"F", 4,
		"FT", n("Btn"),
		"T", text("inert"),
		"AP", d("N", appearance()),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("F1", font),
			"XObject", d("FmL", badLength, "FmE", badEOL),
		),
		"Contents", pdf.Stream([]byte(
			"BT /F1 12 Tf 50 700 Td (stream syntax and actions) Tj ET\n"+
				"q 1 0 0 1 60 400 cm /FmL Do Q\n"+
				"q 1 0 0 1 120 400 cm /FmE Do Q\n")),
		"Annots", a(fieldWithAction, fieldWithoutAction),
	))
	pdf.AddPage(page)

	pdf.Root.Set("AcroForm", d("Fields", a(fieldWithAction, fieldWithoutAction)))

	// 6.6.1 t2: a named action the clause does not permit, beside a permitted one.
	outlineBad := pdf.Indirect(d(
		"Title", text("forbidden named action"),
		"A", d("S", n("Named"), "N", n("/GoForward")),
	))
	outlineOK := pdf.Indirect(d(
		"Title", text("permitted named action"),
		"A", d("S", n("Named"), "N", n("NextPage")),
	))
	outlineBad.Set("Next", outlineOK)
	outlineOK.Set("Prev", outlineBad)
	outlines := pdf.Indirect(d("Type", n("Outlines"), "First", outlineBad, "Last", outlineOK, "Count", 2))
	outlineBad.Set("Parent", outlines)
	outlineOK.Set("Parent", outlines)
	pdf.Root.Set("Outlines", outlines)

	// A conforming XMP packet. It carries a dc:title so the file is not also short
	// of one, keeping the verdict about stream syntax and actions.
	xmp := "<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n" +
		"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n" +
		" <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n" +
		"  <rdf:Description rdf:about=\"\" xmlns:dc=\"http://purl.org/dc/elements/1.1/\">\n" +
		"   <dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">syntax</rdf:li></rdf:Alt></dc:title>\n" +
		"  </rdf:Description>\n" +
		" </rdf:RDF>\n" +
		"</x:xmpmeta>\n" +
		"<?xpacket end=\"w\"?>"
	metadata := pdf.Stream([]byte(xmp), "Type", n("Metadata"), "Subtype", n("XML"))
	pdf.MakeIndirect(metadata)
	pdf.Root.Set("Metadata", metadata)

	// Deliberately no /OutputIntents.
	data, err := save(pdf, "1.4")
	if err != nil {
		return nil, err
	}

	// 6.1.7 t1: /Length must equal the byte count that follows. The declared value
	// sits immediately before the stream keyword that introduces the marked payload;
	// rewriting its digits in place keeps every offset intact. Search from the
	// payload rather than for the nearest "<<", which finds the empty /Resources
	// dictionary nested inside the same object.
	payload := bytes.Index(data, badLengthMark)
	all := regexp.MustCompile(`/Length (\d+)([^>]*)>>\s*stream\r?\n`).FindAllSubmatchIndex(data[:payload], -1)
	if len(all) == 0 {
		return nil, fmt.Errorf("6.1.7 t1: /Length before the marked stream not found")
	}
	m := all[len(all)-1]
	digits := string(data[m[2]:m[3]])
	var length int
	if _, err := fmt.Sscanf(digits, "%d", &length); err != nil {
		return nil, fmt.Errorf("6.1.7 t1: %w", err)
	}
	wrong := fmt.Sprint(length + 1)
	if len(wrong) != len(digits) {
		return nil, fmt.Errorf("6.1.7 t1: differing length would change the digit count")
	}
	data = append(append(append([]byte{}, data[:m[2]]...), wrong...), data[m[3]:]...)

	// 6.1.7 t2: the stream keyword must be followed by CRLF or a single LINE FEED.
	// A lone CARRIAGE RETURN satisfies neither and occupies the same one byte.
	payload = bytes.Index(data, badEOLMark)
	kw := bytes.LastIndex(data[:payload], []byte("stream\n"))
	data[kw+len("stream")] = '\r'
	return data, nil
}
