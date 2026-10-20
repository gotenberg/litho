package pdf

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// TestWriter_Redact_RefusesAnAnnotationAnotherPageShows pins that removing an
// annotation two pages list is exact only when both pages' regions cover it.
// Removing it from page 1 alone left page 2 showing it, and a widget so
// removed took its field out of /AcroForm while page 2 kept drawing it.
//
// # Corpus evidence
//
//   - litho annotshare_ua1.pdf: a widget and a Text annotation listed by
//     pages 1 and 2. A whole-page redaction of page 1 dropped the widget's
//     field, and qpdf --check warned that page 2's widget was not reachable
//     from /AcroForm.
func TestWriter_Redact_RefusesAnAnnotationAnotherPageShows(t *testing.T) {
	t.Parallel()

	const freeText = "8 0 obj\n<< /Type /Annot /Subtype /FreeText /Rect [100 695 200 720] /Contents (SECRET) >>\nendobj\n"
	src := imageDoc([]string{"", ""}, "", "/Annots [8 0 R]", freeText)
	// imageDoc writes page 2 without /Annots; list the same object there too.
	src = bytes.Replace(src, []byte("12 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 13 0 R "),
		[]byte("12 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 13 0 R /Annots [8 0 R] "), 1)
	if !bytes.Contains(src, []byte("13 0 R /Annots [8 0 R]")) {
		t.Fatal("the fixture's second page does not list the annotation")
	}
	region := [4]float64{90, 690, 300, 730}
	ctx := context.Background()

	redact := func(regions []RedactRegion) (*RedactReport, error) {
		r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return NewWriter(r).Redact(ctx, regions)
	}

	_, err := redact([]RedactRegion{{Page: 1, Rect: region}})
	if !errors.Is(err, ErrRedactionRefused) || !errors.Is(err, mutations.ErrAnnotationStraddlesRegion) {
		t.Fatalf("one page: err = %v, want ErrRedactionRefused wrapping ErrAnnotationStraddlesRegion", err)
	}

	report, err := redact([]RedactRegion{{Page: 1, Rect: region}, {Page: 2, Rect: region}})
	if err != nil {
		t.Fatalf("both pages: %v", err)
	}
	if !slices.Contains(report.RemovedAnnotations, 8) {
		t.Errorf("both pages: RemovedAnnotations = %v, want object 8", report.RemovedAnnotations)
	}
}
