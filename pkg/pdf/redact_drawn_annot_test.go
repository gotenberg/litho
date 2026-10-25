package pdf

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// TestWriter_Redact_PlacesAnAppearanceLessAnnotationWhereItDraws pins that an
// annotation with no appearance stream is judged where a viewer draws it, not
// by its /Rect alone. A Line is drawn from /L (ISO 32000-1, 12.5.6.7) whatever
// /Rect says, so one with /Rect [0 0 0 0] and /L across a region was kept and
// went on showing inside the region after Redact reported success.
//
// # Corpus evidence
//
//   - pdfjs annotation-line-without-appearance-empty-Rect: the centred
//     quarter of page 1 kept the red line poppler 26.09.0 and litho's
//     renderer both draw through it.
func TestWriter_Redact_PlacesAnAppearanceLessAnnotationWhereItDraws(t *testing.T) {
	t.Parallel()

	const line = "8 0 obj\n<< /Type /Annot /Subtype /Line /Rect [0 0 0 0] /L [24 572 594 572] /C [1 0 0] /Contents (SECRET) >>\nendobj\n"
	src := imageDoc([]string{""}, "", "/Annots [8 0 R]", line)
	ctx := context.Background()

	redact := func(region [4]float64) (*RedactReport, error) {
		r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return NewWriter(r).Redact(ctx, []RedactRegion{{Page: 1, Rect: region}})
	}

	_, err := redact([4]float64{153, 198, 459, 594})
	if !errors.Is(err, ErrRedactionRefused) || !errors.Is(err, mutations.ErrAnnotationStraddlesRegion) {
		t.Fatalf("quarter: err = %v, want ErrRedactionRefused wrapping ErrAnnotationStraddlesRegion", err)
	}

	report, err := redact([4]float64{0, 0, 612, 792})
	if err != nil {
		t.Fatalf("whole page: %v", err)
	}
	if !slices.Contains(report.RemovedAnnotations, 8) {
		t.Errorf("whole page: RemovedAnnotations = %v, want object 8", report.RemovedAnnotations)
	}

	if _, err := redact([4]float64{100, 100, 200, 200}); err != nil {
		t.Errorf("region clear of the line: %v", err)
	}
}

// TestWriter_Redact_PlacesATextIconWhereItDraws pins that a Text annotation
// with no appearance stream is judged by the icon a viewer draws for it, 24
// units square from the upper-left corner of /Rect and upright on a turned
// page (ISO 32000-1, 12.5.6.4), with the pixel check on.
func TestWriter_Redact_PlacesATextIconWhereItDraws(t *testing.T) {
	t.Parallel()

	const note = "8 0 obj\n<< /Type /Annot /Subtype /Text /Rect [100 600 104 604] /C [1 1 0] /Contents (SECRET) >>\nendobj\n"
	ctx := context.Background()
	redact := func(page string, region [4]float64) (*RedactReport, error) {
		src := imageDoc([]string{""}, "", page, note)
		r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return NewWriter(r).Redact(ctx, []RedactRegion{{Page: 1, Rect: region}})
	}

	// The icon spans x 100 to 124, y 580 to 604, past the small /Rect.
	report, err := redact("/Annots [8 0 R]", [4]float64{90, 570, 130, 610})
	if err != nil || !slices.Contains(report.RemovedAnnotations, 8) {
		t.Errorf("region holding the icon: report %v, err %v, want object 8 removed", report, err)
	}
	for _, region := range [][4]float64{{105, 581, 130, 599}, {0, 590, 612, 700}} {
		if _, err := redact("/Annots [8 0 R]", region); !errors.Is(err, mutations.ErrAnnotationStraddlesRegion) {
			t.Errorf("region %v across the icon: err = %v, want ErrAnnotationStraddlesRegion", region, err)
		}
	}

	// On a page turned a quarter the icon stays upright: in default user
	// space it spans y 604 to 628, above the /Rect.
	report, err = redact("/Rotate 90 /Annots [8 0 R]", [4]float64{90, 598, 130, 640})
	if err != nil || !slices.Contains(report.RemovedAnnotations, 8) {
		t.Errorf("turned page, region holding the upright icon: report %v, err %v, want object 8 removed", report, err)
	}
}

// TestWriter_Redact_KeepsAnAppearanceLessSquareARegionPasses pins that the box
// a synthesised appearance inks is bounded as viewers draw it: a Square is
// drawn inside /Rect, so a region just past /Rect neither selects it nor
// refuses, however wide its border.
//
// # Corpus evidence
//
//   - pdfjs annotation-square-circle-without-appearance: a band 4.7 points
//     above a Circle with a border 3 wide refused, its ink bounded at five
//     widths past the circle.
func TestWriter_Redact_KeepsAnAppearanceLessSquareARegionPasses(t *testing.T) {
	t.Parallel()

	const square = "8 0 obj\n<< /Type /Annot /Subtype /Square /Rect [100 100 200 200] /BS << /W 5 >> /C [1 0 0] >>\nendobj\n"
	src := imageDoc([]string{""}, "", "/Annots [8 0 R]", square)
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	report, err := NewWriter(r).Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{0, 205, 612, 215}}})
	if err != nil {
		t.Fatalf("band past /Rect: %v", err)
	}
	if len(report.RemovedAnnotations) != 0 {
		t.Errorf("RemovedAnnotations = %v, want none", report.RemovedAnnotations)
	}
}
