package compliance

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// twoSectionPDF builds a file laid out the way a linearizer does: the section
// the last startxref designates sits at a LOW offset and carries the /ID, while
// the section it links through /Prev sits further into the file and carries
// none. linLength is written as the linearization dictionary's /L.
//
// Passing the real file length makes veraPDF judge the file linearized, which
// resolves the identifier from the first-page section; any other value defeats
// that test, as trailing bytes after %%EOF do in the wild.
func twoSectionPDF(linLength func(size int) int) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	// Object 1 is the linearization dictionary, which shall be first.
	linPos := buf.Len()
	linPlaceholder := "1 0 obj\n<< /Linearized 1 /L %010d /O 3 /E 0 /N 1 /T 0 >>\nendobj\n"
	fmt.Fprintf(&buf, linPlaceholder, 0)

	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Catalog /Pages 3 0 R >>\nendobj\n")
	off3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	// The low-offset section: it carries the /ID and links to the one below.
	lowXRef := buf.Len()
	buf.WriteString("xref\n0 4\n0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", linPos)
	fmt.Fprintf(&buf, "%010d 00000 n \n", off2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", off3)
	fmt.Fprintf(&buf, "trailer\n<< /Size 4 /Root 2 0 R /Prev %010d"+
		" /ID [<0123456789ABCDEF0123456789ABCDEF> <FEDCBA9876543210FEDCBA9876543210>] >>\n"+
		"startxref\n0\n%%%%EOF\n", 0)

	// The high-offset section: same entries, no /ID, no /Prev.
	highXRef := buf.Len()
	buf.WriteString("xref\n0 4\n0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", linPos)
	fmt.Fprintf(&buf, "%010d 00000 n \n", off2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", off3)
	fmt.Fprintf(&buf, "trailer\n<< /Size 4 /Root 2 0 R >>\nstartxref\n%d\n%%%%EOF\n", lowXRef)

	data := buf.Bytes()
	// Patch the /Prev of the low section to point at the high one, keeping the
	// byte count identical so no offset moves.
	old := fmt.Sprintf("/Prev %010d", 0)
	newPrev := fmt.Sprintf("/Prev %010d", highXRef)
	data = bytes.Replace(data, []byte(old), []byte(newPrev), 1)
	// Patch /L now that the size is known, again without changing the length.
	oldL := fmt.Sprintf("/L %010d", 0)
	newL := fmt.Sprintf("/L %010d", linLength(len(data)))
	data = bytes.Replace(data, []byte(oldL), []byte(newL), 1)
	return data
}

// TestTrailerID_ReadsTheSectionFurthestIntoTheFile pins which trailer answers
// ISO 19005-2, 6.1.3 t1 when a file carries two cross-reference sections and the
// last startxref points at the one nearer the top.
//
// veraPDF reads the section with the greatest start offset, so a first-page
// section carrying /ID does not satisfy the rule for a file whose later section
// carries none. It makes an exception for a file it judges linearized, and that
// judgement requires /L to equal the real file length: see
// [trailerAnsweringID], where both directions were measured on real files.
func TestTrailerID_ReadsTheSectionFurthestIntoTheFile(t *testing.T) {
	t.Parallel()

	exact := func(size int) int { return size }
	short := func(size int) int { return size - 2 }

	for _, tc := range []struct {
		name  string
		data  []byte
		fires bool
	}{
		// /L misses the real length, as trailing bytes make it: veraPDF does not
		// judge the file linearized, so the section furthest into the file
		// answers, and it carries no /ID.
		{"a stale /L leaves the furthest section answering", twoSectionPDF(short), true},
		// /L matches, so the file is linearized and the first-page section's
		// identifier stands.
		{"a linearized file keeps its first-page identifier", twoSectionPDF(exact), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b} {
				if got := firesRuleProfile(t, tc.data, profile, "6.1.3", 1); got != tc.fires {
					t.Errorf("%s: 6.1.3 t1 fired = %v, want %v", profile, got, tc.fires)
				}
			}
		})
	}
}
