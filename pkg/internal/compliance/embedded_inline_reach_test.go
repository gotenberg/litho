package compliance

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestCollectFileSpecs_InlineInEveryObject pins that a file specification
// written inline is gathered in whatever object holds it, not only in an
// indirect annotation, and named by that object and its path there. veraPDF
// walks every value of every object and judges each /Type /Filespec it meets:
// measured on an inline /FS in an annotation that is itself written inline in
// the page's /Annots, which it cites under the page object, and on a reference
// XObject's inline /Ref /F, which it cites under the form.
//
// # Corpus evidence
//
//   - litho complex_1b.pdf and complex_4.pdf: veraPDF reports the /Ref /F of
//     a reference XObject under ISO 19005-3, 6.8 t3 and t4.
func TestCollectFileSpecs_InlineInEveryObject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, catalog := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /AF [<< /Type /Filespec /F (cat.txt) >>] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /X 4 0 R >> >>"+
			" /AF [<< /Type /Filespec /F (page.txt) >>]"+
			" /Annots [<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 10 10]"+
			" /FS << /Type /Filespec /F (a.txt) /EF << /F 5 0 R >> >> >> 6 0 R] >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 1 1] /Length 0"+
			" /Ref << /F << /Type /Filespec /F (target.pdf) >> /Page 0 >> >>\nstream\n\nendstream",
		"<< /Type /EmbeddedFile /Length 4 >>\nstream\ndata\nendstream",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /AF [<< /Type /Filespec /F (b.txt) >>] >>",
	)

	var got []string
	for _, fs := range collectFileSpecs(ctx, doc, catalog) {
		got = append(got, fs.label())
	}
	want := []string{
		"object 1 /AF[0]",
		"object 3 /AF[0]",
		"object 3 /Annots[0]/FS",
		"object 4 /Ref/F",
		"object 6 /AF[0]",
	}
	if !slices.Equal(got, want) {
		t.Errorf("gathered %q, want %q", got, want)
	}
}

// TestInlineFileSpecInInlineAnnotation pins the clause 6.8 findings on a file
// specification written inline in an annotation that is itself written inline
// in a page's /Annots, one per test, as veraPDF 1.30.2 reports them on that
// file at PDF/A-3b: the embedded file has no /Subtype (t1), the specification
// has /F and no /UF (t2) and no /AFRelationship (t3), and no /AF array lists it
// (t4). The walk over indirect annotations skipped it, so none fired.
func TestInlineFileSpecInInlineAnnotation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]"+
			" /Annots [<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 10 10] /F 4"+
			" /FS << /Type /Filespec /F (d.txt) /EF << /F 4 0 R >> >> >>] >>",
		"<< /Type /EmbeddedFile /Length 4 >>\nstream\ndata\nendstream",
	)

	errs, err := ValidatePDFA(ctx, doc, models.PDFA3b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	counts := map[int]int{}
	for _, e := range errs {
		if v, ok := e.(Violation); ok && v.Rule.Specification == "ISO_19005_3" && v.Rule.Clause == "6.8" {
			counts[v.Rule.TestNumber]++
			if !strings.HasPrefix(v.Context, "object 3 /Annots[0]/FS: ") {
				t.Errorf("t%d context %q does not name the page and the path", v.Rule.TestNumber, v.Context)
			}
		}
	}
	for _, test := range []int{1, 2, 3, 4} {
		if counts[test] != 1 {
			t.Errorf("clause 6.8 t%d violations = %d, want 1", test, counts[test])
		}
	}
}
