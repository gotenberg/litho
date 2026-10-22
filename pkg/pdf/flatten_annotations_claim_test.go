package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// untaggedAnnotationPDF builds an untagged page with a printed FreeText
// annotation whose appearance shows "Reviewed", claiming PDF/UA-1 in its
// metadata when claim is set.
func untaggedAnnotationPDF(claim bool) []byte {
	const ap = "BT /Helv 10 Tf 5 5 Td (Reviewed) Tj ET"
	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	if claim {
		catalog = "<< /Type /Catalog /Pages 2 0 R /Metadata 7 0 R >>"
	}
	objs := []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /FreeText /Rect [50 700 250 720] /F 4 /P 3 0 R /Contents (Reviewed) /DA (/Helv 10 Tf 0 g) /AP << /N 5 0 R >> >>",
		colorStream("/Type /XObject /Subtype /Form /BBox [0 0 200 20] /Resources << /Font << /Helv 6 0 R >> >>", ap),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	if claim {
		objs = append(objs, colorStream("/Type /Metadata /Subtype /XML", flattenClaimXMP))
	}
	return colorFixturePDF(objs)
}

// TestFlattenTransparencyUntaggedAnnotationClaim pins how
// [Writer.FlattenTransparency] marks an annotation it draws into an untagged
// document, as [Writer.FlattenForm] marks a baked widget. With no PDF/UA
// claim the drawing is plain page content, so default text extraction
// returns its text and ISO 14289-1, 7.1 test 3 flags it. With a PDF/UA-1
// claim it is an artifact (ISO 14289-1, 7.1), which the default extraction
// leaves out, [WithArtifacts] returns, and 7.1 test 3 passes.
func TestFlattenTransparencyUntaggedAnnotationClaim(t *testing.T) {
	t.Parallel()
	for _, claim := range []bool{false, true} {
		t.Run(fmt.Sprintf("claim=%v", claim), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			report, out := flattenPDF(t, untaggedAnnotationPDF(claim), WithFlattenPrintedAnnotations())
			if len(report.Pages) != 1 || report.Pages[0].Annotations != 1 {
				t.Fatalf("report %+v, want the annotation drawn", report)
			}
			flat, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
			if err != nil {
				t.Fatalf("Open output: %v", err)
			}
			defer flat.Close()

			text, err := flat.ExtractText(ctx, 1)
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if got := strings.Contains(text, "Reviewed"); got == claim {
				t.Errorf("default text = %q, want the drawn text present %v", text, !claim)
			}
			all, err := flat.ExtractText(ctx, 1, WithArtifacts())
			if err != nil {
				t.Fatalf("ExtractText(WithArtifacts): %v", err)
			}
			if !strings.Contains(all, "Reviewed") {
				t.Errorf("text with artifacts = %q, want the drawn text", all)
			}
			violations, err := flat.ValidatePDFUAReport(ctx, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUAReport: %v", err)
			}
			unmarked := 0
			for _, v := range violations {
				if v.Clause == "7.1" && v.TestNumber == 3 {
					unmarked++
				}
			}
			if wantUnmarked := !claim; (unmarked > 0) != wantUnmarked {
				t.Errorf("ISO 14289-1, 7.1 test 3 fires %d times, want firing %v", unmarked, wantUnmarked)
			}
		})
	}
}
