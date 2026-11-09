package build

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("linearized_1b", linearized1b) }

// linearized1b builds linearized_1b.pdf: a linearized file whose two trailers disagree on /ID.
//
// ISO 19005-1, 6.1.3 test 4 is the only rule in the corpus that reads more than
// one trailer. It reads `(isLinearized != true) || lastID == null ||
// (firstPageID == lastID)`: a linearized file states its `/ID` twice, once in the
// first-page trailer at the top and once in the last trailer at the end, and the
// two shall be identical. No fixture reached it, and a checker that keeps only
// one trailer cannot answer it at all.
//
// The two `/ID` arrays here are:
//
// | Trailer    | `/ID`                          |
// | ---------- | ------------------------------ |
// | first page | `[<a11c...> <c0ff...>]`        |
// | last       | `[<a11c...> <b0bb...>]`        |
//
// Element 0 matches and element 1 does not, so only a check comparing the whole
// array answers. Both arrays also hold two *different* strings, which is the
// control: ISO 32000-1, 14.4 has element 1 change whenever the file is updated,
// so a pair that differs within one trailer is conformant. A check that reads the
// requirement as "the two entries of the `/ID` array shall be identical" fires on
// the last trailer, which is the conforming one.
//
// Test 1 of the same clause, `/ID` presence, stays silent: both trailers carry the
// key and every element is a non-empty string. The 6.1.3 verdict is therefore
// attributable to the comparison alone.
//
// The three pages carry an unembedded Helvetica and paint in DeviceGray, and the
// file has neither `/Metadata` nor an output intent, so the font, colour and
// metadata clauses answer from three other parts of the specification. A checker
// that stops at its first structural verdict reports one rule where four are due.
//
// qpdf lays the file out, since linearization is a whole-file property no
// builder can plant object by object. The `/ID` values are patched afterwards
// against the bytes qpdf wrote: they are hex strings of a fixed width, so no
// offset moves and the linearized layout survives.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func linearized1b() ([]byte, error) {
	// Three 16-byte identifiers. Element 0 is shared, element 1 is not.
	idShared := []byte("a11ceed0a11ceed0a11ceed0a11ceed0")
	idLast := []byte("b0bb1e5b0bb1e5b0bb1e5b0bb1e5b0bb")
	idFirstPage := []byte("c0ffeec0ffeec0ffeec0ffeec0ffeec0")

	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"), "Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"), "Encoding", n("WinAnsiEncoding")))

	for i := range 3 {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d("Font", d("F1", font)),
			"Contents", pdf.Stream(fmt.Appendf(nil, "BT /F1 12 Tf 50 700 Td (page %d) Tj ET\n", i))))
		pdf.AddPage(page)
	}

	// Deliberately no /Metadata and no output intent.
	out, err := pdf.Save(pdfgen.SaveOptions{MinVersion: "1.7", Linearize: true})
	if err != nil {
		return nil, err
	}
	data := bytes.Clone(out.Bytes)
	if !bytes.Contains(data, []byte("/Linearized")) {
		return nil, fmt.Errorf("qpdf did not linearize the file, so 6.1.3 test 4 is out of scope")
	}

	matches := regexp.MustCompile(`/ID\s*\[\s*<([0-9a-fA-F]+)>\s*<([0-9a-fA-F]+)>\s*\]`).FindAllSubmatchIndex(data, -1)
	if len(matches) != 2 {
		return nil, fmt.Errorf("expected exactly two trailers, found %d", len(matches))
	}

	// The first match is the first-page trailer at the top of the file; the second
	// is the last trailer. Every replacement keeps its width so no xref offset moves.
	for i, second := range [][]byte{idFirstPage, idLast} {
		m := matches[i]
		for g, value := range map[int][]byte{1: idShared, 2: second} {
			start, end := m[2*g], m[2*g+1]
			if end-start != len(value) {
				return nil, fmt.Errorf("patched /ID changes width, which would move an xref offset")
			}
			copy(data[start:end], value)
		}
	}
	return data, nil
}
