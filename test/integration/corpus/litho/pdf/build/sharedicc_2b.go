package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("sharedicc_2b", sharedicc2b) }

// sharedicc2b builds sharedicc_2b.pdf: one content stream four pages share, each page
// mapping its colour space name to an ICCBased space of its own.
//
// ISO 32000-1, 7.7.3.3 lets pages share a content stream, and the names its
// operators use resolve in each page's own /Resources (7.8.3), so one stream
// paints a different colour space on each page. Merging duplicate streams on a
// write, litho's --compact among them, makes such files: csnames_2b.pdf, so
// rewritten, shares one stream between five pages and another between six.
// veraPDF judges a shared stream once, through the first page's resources, and
// judges none of the spaces a later page maps; litho judges each page whose
// resources differ.
//
// Every page fills with /CS0 through the one stream:
//
// | Page | /CS0                                                      | Answers         |
// | ---- | --------------------------------------------------------- | --------------- |
// | 1    | ICCBased over a version 2 display profile                 | nothing         |
// | 2    | ICCBased over a version 2 abstract profile                | 6.2.4.2 t1      |
// | 3    | ICCBased over a version 4 display profile                 | nothing at 2b   |
// | 4    | page 1's space, in resources equal to page 1's            | nothing         |
//
// ISO 19005-2, 6.2.4.2 t1 takes an ICC profile of the input, output, display
// or colour space class, so the abstract one fails at every PDF/A level. PDF/A-2
// and later permit ICC version 4, so page 3 fails only at PDF/A-1, whose ISO
// 19005-1, 6.2.3.2 reads the PDF 1.4 profile, version 2. Page 4 is the control:
// it reads the stream under the same resources as page 1, and is judged once
// with it.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func sharedicc2b() ([]byte, error) {
	pdf := pdfgen.New()

	const (
		iccHeaderSize = 128
		iccVersion21  = 0x02100000
		iccVersion42  = 0x04200000
	)

	// profile is a header-only RGB profile of the given class and version.
	profile := func(deviceClass string, version uint32) pdfgen.Object {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize)
		binary.BigEndian.PutUint32(header[0:4], uint32(iccHeaderSize+len(tagTable))) //nolint:gosec // a constant 132.
		binary.BigEndian.PutUint32(header[8:12], version)
		copy(header[12:16], deviceClass)
		copy(header[16:20], "RGB ")
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		stream := pdf.Stream(append(header, tagTable...))
		stream.Set("N", 3)
		return pdf.MakeIndirect(stream)
	}

	space := func(deviceClass string, version uint32) pdfgen.Object {
		return pdf.MakeIndirect(a(n("ICCBased"), profile(deviceClass, version)))
	}

	content := pdf.MakeIndirect(pdf.Stream([]byte("/CS0 cs 0.5 0.5 0.5 scn 0 0 10 10 re f\n")))

	page := func(cs pdfgen.Object) {
		dd := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", d("ColorSpace", d("CS0", cs)),
			"Contents", content,
		)
		pdf.AddPage(pdf.Indirect(dd))
	}

	display := space("mntr", iccVersion21)
	page(display)
	page(space("abst", iccVersion21))
	page(space("mntr", iccVersion42))
	page(display)

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "1.7")
}
