package pdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestPageReportsEffectiveBoundaries pins that [Reader.Page] reports the bleed,
// trim and art boxes as ISO 32000-1, 14.11.2.1 makes them effective: reduced to
// their intersection with the media box, and defaulting to the crop box when
// absent (ISO 32000-1, 7.7.3.3, Table 30). MediaBox stays as the file writes it.
func TestPageReportsEffectiveBoundaries(t *testing.T) {
	t.Parallel()

	data := pageBoxPDF("/MediaBox [0 0 612 792] /CropBox [10 10 602 782]" +
		" /BleedBox [-9 -9 621 801] /TrimBox [36 36 648 828]")
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}

	for _, c := range []struct {
		name      string
		got, want [4]float64
	}{
		{"MediaBox", p.MediaBox, [4]float64{0, 0, 612, 792}},
		{"CropBox", p.CropBox, [4]float64{10, 10, 602, 782}},
		{"BleedBox", p.BleedBox, [4]float64{0, 0, 612, 792}},
		{"TrimBox", p.TrimBox, [4]float64{36, 36, 612, 792}},
		{"ArtBox", p.ArtBox, [4]float64{10, 10, 602, 782}},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestPageBoundarySizeCheckJudgesTheDeclaredBox pins that the ISO 19005-2,
// 6.1.13 page boundary size limit judges the box the file writes, not the
// effective box [Reader.Page] reports. A 14405-unit trim box on a 500-unit page
// is reported as 500 units wide, and the file still breaks the 14400-unit limit
// it declares.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-1-13-t09-fail-b.pdf: declares
//     14405-unit bleed, trim and art boxes on a 500-unit media box, the shape
//     this test builds, and veraPDF fails it.
func TestPageBoundarySizeCheckJudgesTheDeclaredBox(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, c := range []struct {
		name    string
		trimBox string
		want    bool
	}{
		{"a declared box beyond the limit fails", "[0 0 14405 14405]", true},
		{"a declared box within the limit passes", "[0 0 14400 14400]", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			data := pageBoxPDF("/MediaBox [0 0 500 500] /TrimBox " + c.trimBox)
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}

			p, err := r.Page(ctx, 1)
			if err != nil {
				t.Fatalf("Page: %v", err)
			}
			if want := [4]float64{0, 0, 500, 500}; p.TrimBox != want {
				t.Errorf("TrimBox = %v, want the effective %v", p.TrimBox, want)
			}

			report, err := r.ValidatePDFAReport(ctx, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFAReport: %v", err)
			}
			fired := false
			for _, v := range report {
				if v.Clause == "6.1.13" && v.TestNumber == 11 {
					fired = true
				}
			}
			if fired != c.want {
				t.Errorf("6.1.13 t11 fired = %v, want %v for /TrimBox %s", fired, c.want, c.trimBox)
			}
		})
	}
}
