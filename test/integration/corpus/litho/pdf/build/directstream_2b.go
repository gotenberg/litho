package build

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("directstream_2b", directstream2b) }

// directstream2b builds directstream_2b.pdf: streams written inside other objects, at PDF/A-2b.
//
// ISO 32000-1, 7.3.8.1 makes every stream an indirect object. litho's parser
// reads one written inline, as veraPDF's does, and every writer path wrote it
// back where it found it, where qpdf reads the object holding it as damaged; a
// compacted write packed it into an object stream, which ISO 32000-1, 7.5.7
// forbids. No corpus file holds a stream inside another object, so no count
// reached the writers. The page holds one in each place a copy meets one:
//
// | Where the stream sits                                     | Shows |
// | --------------------------------------------------------- | ----- |
// | the page's indirect content stream, first in `/Contents`  | A     |
// | the `/Contents` array, directly, second                   | B     |
// | the page's `/XObject` resources, directly, as `/Fm0`      | AB    |
// | `/Fm0`'s own `/XObject` resources, directly, as `/Fm1`    | BA    |
//
// The direct content stream draws `/Fm0`, which draws `/Fm1`, and each stream
// shows its own text in the conforming embedded TrueType of ttcmap_2b.pdf, so a
// write that drops or garbles one loses text a mutation promises. The text paints
// in the initial DeviceGray, with no output intent.
//
// The page's /MediaBox also runs on past its four coordinates into a reference to
// the indirect content stream, as two pdfjs files write one by accident. A page
// copy resolved that reference into the /CropBox it wrote, which put the content
// stream inline too.
//
// qpdf makes every stream indirect, and pdfgen reproduces its output. The base
// revision is saved by pdfgen, and a second revision is appended by hand, as an
// incremental update rewriting the page, indexed by a cross-reference table of
// its own.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func directstream2b() ([]byte, error) {
	latin := map[int]string{0x41: "A", 0x42: "B"}

	pdf := pdfgen.New()
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program, "Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoDirectSans"),
		"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontBBox", a(0, -200, 550, 700),
		"FontFile2", pdf.MakeIndirect(fontFile),
	)
	const first, last = 0x41, 0x42
	widths := a()
	for range last - first + 1 {
		widths.Append(ttAdvanceWidth)
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoDirectSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", widths,
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))

	textOp := func(shown string, x, y int) string {
		return fmt.Sprintf("BT /F1 24 Tf %d %d Td (%s) Tj ET\n", x, y, shown)
	}

	content := pdf.Stream([]byte(textOp("A", 72, 700)))
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.MakeIndirect(content),
	))
	pdf.AddPage(page)
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))

	out, err := saveOutput(pdf, "1.7")
	if err != nil {
		return nil, err
	}

	base := out.Bytes
	pagesNum := out.Num(pdf.Pages)
	pageNum := out.Num(page)
	contentNum := out.Num(content)
	fontNum := out.Num(font)
	rootNum := out.Num(pdf.Root)
	size := out.Size
	docID := hex.EncodeToString(idOf(base))
	startxref := bytes.LastIndex(base, []byte("startxref"))
	prev, err := strconv.Atoi(strings.Fields(string(base[startxref:]))[1])
	if err != nil {
		return nil, fmt.Errorf("directstream_2b: startxref: %w", err)
	}

	// inline is a stream written as a direct object, where ISO 32000-1, 7.3.8.1
	// forbids one.
	inline := func(dictionary, data string) string {
		entries := ""
		if dictionary != "" {
			entries = dictionary + " "
		}
		return fmt.Sprintf("<< %s/Length %d >>\nstream\n%s\nendstream", entries, len(data), data)
	}

	form := func(bbox, resources, data string) string {
		return inline(fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [%s] /Resources << %s >>", bbox, resources), data)
	}

	fontRes := fmt.Sprintf("/Font << /F1 %d 0 R >>", fontNum)
	inner := form("0 0 100 40", fontRes, textOp("BA", 0, 10))
	outer := form(
		"0 0 300 100",
		fontRes+" /XObject << /Fm1 "+inner+" >>",
		textOp("AB", 0, 60)+"/Fm1 Do\n",
	)
	directContent := inline("", "q 1 0 0 1 72 400 cm /Fm0 Do Q\n"+textOp("B", 72, 600))
	pageBody := fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792 %d 0 R] /Resources << %s /XObject << /Fm0 %s >> >> "+
			"/Contents [%d 0 R %s] >>", pagesNum, contentNum, fontRes, outer, contentNum, directContent)

	o := bytes.Clone(base)
	pageOffset := len(o)
	o = fmt.Appendf(o, "%d 0 obj\n%s\nendobj\n", pageNum, pageBody)
	xrefOffset := len(o)
	o = fmt.Appendf(o, "xref\n0 1\n0000000000 65535 f\r\n%d 1\n%010d 00000 n\r\n", pageNum, pageOffset)
	o = fmt.Appendf(o, "trailer\n<< /Size %d /Root %d 0 R /Prev %d /ID [<%s> <%s>] >>\n", size, rootNum, prev, docID, docID)
	o = fmt.Appendf(o, "startxref\n%d\n%%%%EOF\n", xrefOffset)
	return o, nil
}
