package pdf

import (
	"bytes"
	"context"
	"encoding/json"
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

// TestPageReportsDeclaredBoundaries pins that [Reader.Page] reports each page
// boundary as the file writes it beside the effective one: a box extending
// beyond the media box keeps its declared size, a crop box inherited from the
// page tree (ISO 32000-1, 7.7.3.3, Table 30) is the ancestor's entry, and an
// absent box, or one without the four elements of a rectangle (ISO 32000-1,
// 7.9.5), is nil rather than its default.
func TestPageReportsDeclaredBoundaries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := buildTestPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 /MediaBox [0 0 612 792] /CropBox [-20 -20 700 900] >>",
		"<< /Type /Page /Parent 2 0 R /BleedBox [-9 -9 621 801] /ArtBox [1 1 600 700 9] >>",
		"<< /Type /Page /Parent 2 0 R /CropBox [10 10 602 782] /TrimBox [36 36 576 756] >>",
	}, "")
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	first, err := r.Page(ctx, 1)
	if err != nil {
		t.Fatalf("Page(1): %v", err)
	}
	second, err := r.Page(ctx, 2)
	if err != nil {
		t.Fatalf("Page(2): %v", err)
	}

	for _, c := range []struct {
		name string
		got  *[4]float64
		want *[4]float64
	}{
		{"page 1 DeclaredCropBox, inherited as written", first.DeclaredCropBox, &[4]float64{-20, -20, 700, 900}},
		{"page 1 DeclaredBleedBox, beyond the media box", first.DeclaredBleedBox, &[4]float64{-9, -9, 621, 801}},
		{"page 1 DeclaredTrimBox, absent", first.DeclaredTrimBox, nil},
		{"page 1 DeclaredArtBox, five elements", first.DeclaredArtBox, nil},
		{"page 2 DeclaredCropBox, its own entry", second.DeclaredCropBox, &[4]float64{10, 10, 602, 782}},
		{"page 2 DeclaredBleedBox, absent", second.DeclaredBleedBox, nil},
		{"page 2 DeclaredTrimBox", second.DeclaredTrimBox, &[4]float64{36, 36, 576, 756}},
		{"page 2 DeclaredArtBox, absent", second.DeclaredArtBox, nil},
	} {
		switch {
		case c.want == nil && c.got != nil:
			t.Errorf("%s = %v, want nil", c.name, *c.got)
		case c.want != nil && c.got == nil:
			t.Errorf("%s = nil, want %v", c.name, *c.want)
		case c.want != nil && *c.got != *c.want:
			t.Errorf("%s = %v, want %v", c.name, *c.got, *c.want)
		}
	}

	// The effective boxes of the same pages are clamped and defaulted, so the
	// declared ones are the only place the written values survive.
	for _, c := range []struct {
		name      string
		got, want [4]float64
	}{
		{"page 1 CropBox", first.CropBox, [4]float64{0, 0, 612, 792}},
		{"page 1 BleedBox", first.BleedBox, [4]float64{0, 0, 612, 792}},
		{"page 1 TrimBox", first.TrimBox, [4]float64{0, 0, 612, 792}},
		{"page 1 ArtBox", first.ArtBox, [4]float64{0, 0, 612, 792}},
		{"page 2 BleedBox", second.BleedBox, [4]float64{10, 10, 602, 782}},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestPageDeclaredBoundariesJSON pins the JSON a [models.Page] serializes to: a
// declared box is written under its own key when the file declares it and
// left out when it does not, beside the effective boxes, which are always
// written.
func TestPageDeclaredBoundariesJSON(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := pageBoxPDF("/MediaBox [0 0 612 792] /TrimBox [0 0 700 700]")
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p, err := r.Page(ctx, 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}

	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	for key, want := range map[string]string{
		"trimBox":         "[0,0,612,700]",
		"declaredTrimBox": "[0,0,700,700]",
		"cropBox":         "[0,0,612,792]",
	} {
		if string(got[key]) != want {
			t.Errorf("%s = %s, want %s. JSON: %s", key, got[key], want, raw)
		}
	}
	for _, key := range []string{"declaredCropBox", "declaredBleedBox", "declaredArtBox"} {
		if v, ok := got[key]; ok {
			t.Errorf("%s = %s, want it left out for an absent box. JSON: %s", key, v, raw)
		}
	}
}
