package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// inlineFileSpecsEverywherePDF builds a page whose two annotations each carry
// a file specification written inline in an /AF array: a Link that is an
// object of its own, object 4, and one written inline in the page's /Annots.
// Each specification has /F and no /UF, no /AFRelationship, and an embedded
// file that is no PDF, object 5 with no /Subtype and object 6 with the invalid
// /Subtype /plain. An image XObject, object 7, lists a third one inline in its
// own /AF, a stream holding it, and the catalog a fourth, neither embedding a
// file.
func inlineFileSpecsEverywherePDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /AF [<< /Type /Filespec /F (cat.txt) /UF (cat.txt) >>] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im 7 0 R >> >>"+
			" /Annots [4 0 R << /Type /Annot /Subtype /Link /Rect [20 0 30 10] /F 4 /Border [0 0 0]"+
			" /AF [<< /Type /Filespec /F (b.txt) /EF << /F 6 0 R >> >>] >>] >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /F 4 /Border [0 0 0]"+
			" /AF [<< /Type /Filespec /F (a.txt) /EF << /F 5 0 R >> >>] >>",
		"<< /Type /EmbeddedFile /Length 3 >>\nstream\nabc\nendstream",
		"<< /Type /EmbeddedFile /Subtype /plain /Length 3 >>\nstream\nabc\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8"+
			" /AF [<< /Type /Filespec /F (im.txt) /UF (im.txt) >>] /Length 1 >>\nstream\n\x00\nendstream",
	)
}

// embeddedFileFindings returns how often doc fails each test of the embedded
// file clause of profile: 6.1.11 in ISO 19005-1, 6.8 in ISO 19005-2 and -3,
// 6.9 in ISO 19005-4.
func embeddedFileFindings(t *testing.T, doc *document.Document, profile models.PDFAProfile) map[int]int {
	t.Helper()

	errs, err := compliance.ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	clause := map[models.PDFAProfile]string{
		models.PDFA1b: "6.1.11", models.PDFA3b: "6.8", models.PDFA4: "6.9",
	}[profile]
	counts := map[int]int{}
	for _, v := range toViolations(errs) {
		if v.Rule.Clause == clause {
			counts[v.Rule.TestNumber]++
		}
	}
	return counts
}

// remediateAndReopen remediates data at profile under opts, writes the result
// in full and parses it again.
func remediateAndReopen(t *testing.T, data []byte, profile models.PDFAProfile, opts Options) *document.Document {
	t.Helper()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, data)
	if _, err := RemediatePDFA(ctx, doc, alloc, profile, opts); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	var out bytes.Buffer
	if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	return reread
}

// TestRemediatePDFA4_RepairsInlineFileSpecs pins that a PDF/A-4 remediation
// repairs a file specification written inline wherever compliance finds it,
// here in an /AF array of an annotation and of an annotation written inline:
// the embedded file's media type (ISO 19005-4, 6.9 t1), the missing /UF (t2)
// and /AFRelationship (t4). The repairs reached an annotation's /FS alone, or
// no inline specification at all, and each finding survived. The embedded
// files are no PDF (t3), which only the aggressive mode repairs, by dropping
// them.
func TestRemediatePDFA4_RepairsInlineFileSpecs(t *testing.T) {
	t.Parallel()

	doc, _ := newTestDoc(t, inlineFileSpecsEverywherePDF())
	if got := embeddedFileFindings(t, doc, models.PDFA4); got[1] != 2 || got[2] != 2 || got[3] != 2 || got[4] != 2 {
		t.Fatalf("precondition: the source fails 6.9 %v, want tests 1 to 4 twice each", got)
	}

	safe := embeddedFileFindings(t, remediateAndReopen(t, inlineFileSpecsEverywherePDF(), models.PDFA4, Options{PreserveContent: true}), models.PDFA4)
	for _, test := range []int{1, 2, 4} {
		if safe[test] != 0 {
			t.Errorf("default mode: the output still fails 6.9 t%d %d times", test, safe[test])
		}
	}
	if safe[3] != 2 {
		t.Errorf("default mode: 6.9 t3 = %d, want 2: dropping an attachment destroys content", safe[3])
	}

	if aggressive := embeddedFileFindings(t, remediateAndReopen(t, inlineFileSpecsEverywherePDF(), models.PDFA4, Options{}), models.PDFA4); len(aggressive) != 0 {
		t.Errorf("aggressive mode: the output still fails 6.9 %v", aggressive)
	}
}

// TestRemediatePDFA3b_RepairsInlineFileSpecs pins that a PDF/A-3b remediation
// clears every clause 6.8 finding on file specifications written inline in any
// object: an annotation, an annotation written inline, an image XObject's
// stream dictionary and the catalog. Each moves to an object of its own, which
// an /AF array can list (6.8 t4); promotion walked annotations alone.
func TestRemediatePDFA3b_RepairsInlineFileSpecs(t *testing.T) {
	t.Parallel()

	doc, _ := newTestDoc(t, inlineFileSpecsEverywherePDF())
	if got := embeddedFileFindings(t, doc, models.PDFA3b); got[4] != 4 {
		t.Fatalf("precondition: the source fails 6.8 t4 %d times, want 4", got[4])
	}
	if got := embeddedFileFindings(t, remediateAndReopen(t, inlineFileSpecsEverywherePDF(), models.PDFA3b, Options{PreserveContent: true}), models.PDFA3b); len(got) != 0 {
		t.Errorf("the output still fails 6.8 %v", got)
	}
}

// TestRemediatePDFA1b_StripsEFFromInlineFileSpecs pins that an aggressive
// PDF/A-1b remediation removes /EF from a file specification written inline
// anywhere, as ISO 19005-1, 6.1.11 t1 forbids the key wherever the
// specification sits. It stripped an annotation's inline /FS alone.
func TestRemediatePDFA1b_StripsEFFromInlineFileSpecs(t *testing.T) {
	t.Parallel()

	doc, _ := newTestDoc(t, inlineFileSpecsEverywherePDF())
	if got := embeddedFileFindings(t, doc, models.PDFA1b); got[1] != 2 {
		t.Fatalf("precondition: the source fails 6.1.11 t1 %d times, want 2", got[1])
	}
	if got := embeddedFileFindings(t, remediateAndReopen(t, inlineFileSpecsEverywherePDF(), models.PDFA1b, Options{}), models.PDFA1b); len(got) != 0 {
		t.Errorf("the output still fails 6.1.11 %v", got)
	}
}
