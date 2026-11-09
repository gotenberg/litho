package build

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("xrefstm_2b", xrefstm2b) }

// xrefstm2b builds xrefstm_2b.pdf: the trailer a cross-reference stream carries.
//
// ISO 32000-1, 7.5.8.2 has a cross-reference stream's dictionary hold the file's
// trailer beside the entries that describe the stream, and veraPDF builds the
// trailer from the entries ISO 32000-1, Table 15 defines: /Size, /Prev, /Root,
// /Encrypt, /Info and /ID. The dictionary is an indirect object, which the object
// sweep judges whole, so a value in a trailer entry is judged twice, and a
// document information dictionary written directly there three times, while an
// entry Table 15 does not define is judged once. An older section's stream is an
// indirect object like any other, judged once. 922 corpus files end in a
// cross-reference stream, and not one holds a defect in its trailer entries, so no
// count reached the reading.
//
// qpdf writes the first section, a cross-reference stream whose trailer holds a
// direct /Info. The second section is appended by hand, as an incremental update
// rewriting the page's content stream, indexed by a stream of its own:
//
// | Where                                      | Value                         | Rule at PDF/A-2b | Count |
// | ------------------------------------------ | ----------------------------- | ---------------- | ----- |
// | newest `/ID`, first element                | 31 hex digits                 | `6.1.6/1`        | 2     |
// | newest direct `/Info`, `/Title`            | `<414>`                       | `6.1.6/1`        | 3     |
// | newest direct `/Info`, `/Producer`         | `<41G1>`                      | `6.1.6/2`        | 3     |
// | newest `/Foo`, no Table 15 entry           | `<414>`                       | `6.1.6/1`        | 1     |
// | older direct `/Info`, `/Producer`          | `<414>`                       | `6.1.6/1`        | 1     |
// | newest direct `/Info`, `/Count`            | 2147483648                    | `6.1.13/1`       | 3     |
// | newest `/Big`, no Table 15 entry           | 2147483648                    | `6.1.13/1`       | 1     |
// | older direct `/Info`, `/Count`             | 2147483648                    | `6.1.13/1`       | 1     |
// | newest direct `/Info`, `/Subject`          | 32768 bytes                   | `6.1.13/3`       | 3     |
// | newest direct `/Info`, a key               | 200 bytes                     | `6.1.13/4`       | 3     |
// | newest, a key of the stream dictionary     | 200 bytes                     | `6.1.13/4`       | 1     |
// | newest direct `/Info`, `/Width`            | 40000.5                       | PDF/A-1 `6.1.12/2` | 3   |
//
// Each rule's count is the sum of its rows. The real past 32767 is a limit of
// PDF/A-1 alone, which is where it answers. The newest /Size stays in range:
// veraPDF judges an out-of-range /Size twice like any trailer entry, but qpdf
// then refuses the file outright, and a fixture viewers cannot open tests
// nothing a user sees.
//
// The first section's direct /Info holds `(QQQQQ)`, patched as bytes into
// `<414>  ` after the save, since qpdf writes every string it knows as a literal
// of its own choosing; the width is kept, so no offset moves.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func xrefstm2b() ([]byte, error) {
	longKey := strings.Repeat("K", 200)
	const bigInt = 2147483648

	pdf := pdfgen.New()
	content := pdf.Stream([]byte("0 0 m 10 10 l n\n"))
	page := pdf.Indirect(d("Type", n("Page"), "MediaBox", a(0, 0, 200, 100), "Resources", d(), "Contents", content))
	pdf.AddPage(page)
	pdf.Trailer.Set("Info", d("Producer", text("QQQQQ"), "Count", bigInt))

	// Deliberately no /Metadata and no output intent.
	out, err := saveOutput(pdf, "1.7", generateObjectStreams)
	if err != nil {
		return nil, err
	}

	base := out.Bytes
	if bytes.Count(base, []byte("(QQQQQ)")) != 1 || !bytes.Contains(base, []byte("/Type /XRef")) {
		return nil, fmt.Errorf("xrefstm_2b: qpdf did not write one cross-reference stream holding the direct /Info")
	}
	base = bytes.Replace(base, []byte("(QQQQQ)"), []byte("<414>  "), 1)

	startxref := bytes.LastIndex(base, []byte("startxref"))
	prev, err := strconv.Atoi(strings.Fields(string(base[startxref:]))[1])
	if err != nil {
		return nil, fmt.Errorf("xrefstm_2b: startxref: %w", err)
	}
	rootNum := out.Num(pdf.Root)
	contentNum := out.Num(content)
	size := out.Size
	xrefNum := size

	// row is one cross-reference stream entry under /W [1 4 2]: an in-use object.
	row := func(offset int) []byte {
		b := []byte{1, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(b[1:5], uint32(offset)) //nolint:gosec // offsets stay far below 4 GiB.
		return b
	}

	o := bytes.Clone(base)
	contentOffset := len(o)
	body := "0 0 m 20 20 l n\n"
	o = fmt.Appendf(o, "%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", contentNum, len(body), body)
	xrefOffset := len(o)
	rows := append(row(contentOffset), row(xrefOffset)...)
	info := fmt.Sprintf("<< /Title <414> /Producer <41G1> /Count %d /Subject (%s) /%s (x) /Width 40000.5 >>",
		bigInt, strings.Repeat("x", 32768), longKey)
	dictionary := fmt.Sprintf(
		"<< /Type /XRef /Size %d /Index [%d 1 %d 1] /W [1 4 2] /Root %d 0 R /Prev %d "+
			"/ID [<a11ceed0a11ceed0a11ceed0a11ceed> <b0bb1e5b0bb1e5b0bb1e5b0bb1e5b0bb>] "+
			"/Info %s /Foo <414> /Big %d /%s 1 /Length %d >>",
		xrefNum+1, contentNum, xrefNum, rootNum, prev, info, bigInt, longKey, len(rows))
	o = fmt.Appendf(o, "%d 0 obj\n%s\nstream\n", xrefNum, dictionary)
	o = append(o, rows...)
	o = append(o, "\nendstream\nendobj\n"...)
	o = fmt.Appendf(o, "startxref\n%d\n%%%%EOF\n", xrefOffset)
	return o, nil
}
