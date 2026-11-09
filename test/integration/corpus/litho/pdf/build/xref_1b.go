package build

import (
	"fmt"
	"regexp"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("xref_1b", xref1b) }

// xref1b builds xref_1b.pdf: cross reference table formatting under PDF/A-1b.
//
// The coverage scan showed ISO 19005-1, 6.1.4 unreached. Two of its three tests
// constrain the bytes of the cross reference table itself: the `xref` keyword and
// the subsection header are separated by exactly one EOL marker, and the header's
// starting object number and count by exactly one SPACE.
//
// No writer emits either defect, so both are planted as bytes after the save. The
// patch stays inside the cross reference section, which is the one region whose
// own offset nothing points into: `startxref` records where the table begins, and
// that does not move. Object offsets are all earlier in the file and unaffected.
//
// Both defects sit on the same header line on purpose, and that is the point of
// the fixture. They are separate tests of one clause, so a scanner that stops at
// the first one it finds reports one rule where two are due. litho did exactly
// that: the surplus EOL left its parser positioned on the extra byte, the
// subsection header scan then read no digits and stopped, and the spacing defect
// went unreported. Only the pair reveals it; either defect alone agreed.
//
// Test 3 forbids a cross reference stream, which cannot coexist with a table, so
// it belongs to a file of its own and is not planted here.
//
// Beyond the clause the file carries the usual independent defects, no XMP packet
// and no output intent, so a table verdict is visibly separate from them.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func xref1b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 50 700 Td (cross reference table) Tj ET\n")),
	))
	pdf.AddPage(page)

	// Deliberately no /Metadata and no /OutputIntents.
	raw, err := save(pdf, "1.4")
	if err != nil {
		return nil, err
	}

	// `xref\n0 6\n` becomes `xref\n\n0  6\n`: a second EOL marker after the keyword
	// (test 2) and a second space inside the header (test 1). Both bytes are added
	// inside the table, after the offset `startxref` records.
	re := regexp.MustCompile(`(\nxref)\n(\d+) (\d+)`)
	loc := re.FindSubmatchIndex(raw)
	if loc == nil {
		return nil, fmt.Errorf("xref_1b: expected exactly one cross reference subsection header")
	}
	var out []byte
	out = append(out, raw[:loc[0]]...)
	out = append(out, raw[loc[2]:loc[3]]...)
	out = append(out, "\n\n"...)
	out = append(out, raw[loc[4]:loc[5]]...)
	out = append(out, "  "...)
	out = append(out, raw[loc[6]:loc[7]]...)
	out = append(out, raw[loc[1]:]...)
	return out, nil
}
