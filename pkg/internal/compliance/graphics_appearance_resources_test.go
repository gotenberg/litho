package compliance

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// appearanceResourcePDF builds a one-page document whose annotation appearance
// stream selects the marked-content property /MC0. apRes and pageRes supply the
// appearance stream's own /Resources and the page's, verbatim, so a caller can
// place /MC0 on either, on neither, or leave the stream a /Properties that
// holds some other name.
func appearanceResourcePDF(t *testing.T, apRes, pageRes string) []byte {
	t.Helper()

	page := "BT /F1 12 Tf 20 700 Td (body) Tj ET\n"
	ap := "/OC /MC0 BDC\nq 0 0 100 50 re f Q\nEMC\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources " + pageRes + " /Annots [6 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Annot /Subtype /Square /Rect [100 100 200 150] /F 4 /AP << /N 7 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 50] /Resources %s /Length %d >>\nstream\n%s\nendstream",
			apRes, len(ap), ap),
		"<< /Type /OCG /Name (layer) >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// appearanceResourceFires runs the appearance scan over the fixture and reports
// whether the undefined-name rule fired.
func appearanceResourceFires(t *testing.T, apRes, pageRes string, part int) bool {
	t.Helper()

	ctx := context.Background()
	data := appearanceResourcePDF(t, apRes, pageRes)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}

	for _, v := range appearanceScan(ctx, doc, catalog.Dict, part) {
		if v.Rule.Clause == "6.2.2" && v.Rule.TestNumber == 3 {
			return true
		}
	}
	return false
}

// TestAppearanceStreamUndefinedResourceName pins ISO 19005-4, 6.2.2 t3 for a
// name an annotation appearance stream references and no resource dictionary
// defines.
//
// An appearance stream is a content stream (ISO 32000-1, 12.5.5), so the rule
// reaches it, and veraPDF 1.30.2 reports it there.
//
// The page's resources fill in per name, which was measured by adding /MC0 to
// the page alone (t3 fell silent) and then by giving the appearance stream a
// /Properties holding a different name while the page kept /MC0 (still
// silent). A page-defined name fails the inherited-resource test, t2, instead:
// measured with veraPDF 1.30 at PDF/A-2b and PDF/A-4 on both shapes.
//
// # Corpus evidence
//
//   - pdfjs issue14438.pdf: runs /OC /MC0 BDC in 2 of its 19 appearance
//     streams whose own /Resources define no /MC0, and veraPDF fails exactly
//     those 2 while litho reported none. Both per-name measurements were made
//     on this file.
func TestAppearanceStreamUndefinedResourceName(t *testing.T) {
	t.Parallel()

	const noProps = "<< /ProcSet [/PDF] >>"
	const otherProps = "<< /ProcSet [/PDF] /Properties << /MC9 8 0 R >> >>"
	const pagePlain = "<< /Font << /F1 5 0 R >> >>"
	const pageDefines = "<< /Font << /F1 5 0 R >> /Properties << /MC0 8 0 R >> >>"

	for _, tc := range []struct {
		name    string
		apRes   string
		pageRes string
		part    int
		want    bool
	}{
		{
			name:    "no dictionary defines the name",
			apRes:   noProps,
			pageRes: pagePlain,
			part:    4,
			want:    true,
		},
		{
			name:    "the appearance stream defines it",
			apRes:   "<< /ProcSet [/PDF] /Properties << /MC0 8 0 R >> >>",
			pageRes: pagePlain,
			part:    4,
			want:    false,
		},
		{
			name:    "the page defines it and the stream has no /Properties",
			apRes:   noProps,
			pageRes: pageDefines,
			part:    4,
			want:    false,
		},
		{
			name:    "the page defines it and the stream names something else",
			apRes:   otherProps,
			pageRes: pageDefines,
			part:    4,
			want:    false,
		},
		{
			// ISO 19005-2 and -3 state §6.2.2 with t1 and t2 alone, so an
			// undefined name is not a violation of those parts.
			name:    "an undefined name is silent outside part 4",
			apRes:   noProps,
			pageRes: pagePlain,
			part:    2,
			want:    false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := appearanceResourceFires(t, tc.apRes, tc.pageRes, tc.part); got != tc.want {
				t.Errorf("§6.2.2 t3 fired = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAppearanceStreamUndefinedResourceReachesValidation pins that the scan is
// registered in the check groups rather than merely present: a detector nothing
// invokes is a dead rule.
func TestAppearanceStreamUndefinedResourceReachesValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := appearanceResourcePDF(t, "<< /ProcSet [/PDF] >>", "<< /Font << /F1 5 0 R >> >>")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	violations, err := ValidatePDFA(ctx, doc, models.PDFA4)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	for _, v := range violations {
		if strings.Contains(v.Error(), "MC0") && strings.Contains(v.Error(), "annotation appearance") {
			return
		}
	}
	t.Error("a full PDF/A-4 run did not report the appearance stream's undefined name: the check is not wired in")
}
