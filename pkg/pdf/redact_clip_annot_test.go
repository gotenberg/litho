package pdf

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// clippedLinkDoc is a page drawing "SECRET LINK" with a borderless link over
// the whole run, and, when shared, a second page listing the same link.
func clippedLinkDoc(shared bool) []byte {
	const link = "8 0 obj\n<< /Type /Annot /Subtype /Link /Rect [100 695 400 720] /Border [0 0 0]" +
		" /A << /S /URI /URI (http://example.test/) >> >>\nendobj\n" +
		"9 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n"
	contents := []string{"BT /F1 12 Tf 100 700 Td (SECRET LINK) Tj ET"}
	if shared {
		contents = append(contents, "")
	}
	src := imageDoc(contents, "", "/Annots [8 0 R]", link)
	src = bytes.Replace(src, []byte("5 0 obj\n<< /XObject << /Im1 7 0 R >> >>"), []byte("5 0 obj\n<< /XObject << /Im1 7 0 R >> /Font << /F1 9 0 R >> >>"), 1)
	if shared {
		src = bytes.Replace(src, []byte("/Contents 13 0 R "), []byte("/Contents 13 0 R /Annots [8 0 R] "), 1)
	}
	return src
}

// TestWriter_Redact_FallbackDrawsAStraddlingAnnotationIntoThePage pins that
// [WithRedactionRasterFallback] draws an annotation reaching outside the
// region into the page and removes it, with its pop-up, through the pixel
// check: the part of its look outside stays, and nothing of it is inside.
func TestWriter_Redact_FallbackDrawsAStraddlingAnnotationIntoThePage(t *testing.T) {
	t.Parallel()
	const square = "8 0 obj\n<< /Type /Annot /Subtype /Square /Rect [100 600 300 700] /AP << /N 9 0 R >>" +
		" /Contents (a comment) /Popup 20 0 R >>\nendobj\n" +
		"9 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] /Length 25 >>\nstream\n1 0 0 rg 0 0 200 100 re f\nendstream\nendobj\n" +
		"20 0 obj\n<< /Type /Annot /Subtype /Popup /Rect [400 400 500 500] /Parent 8 0 R >>\nendobj\n"
	src := imageDoc([]string{""}, "", "/Annots [8 0 R 20 0 R]", square)
	ctx := context.Background()
	region := []RedactRegion{{Page: 1, Rect: [4]float64{90, 590, 200, 710}}}

	open := func() *Writer {
		r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return NewWriter(r)
	}
	if _, err := open().Redact(ctx, region); !errors.Is(err, mutations.ErrAnnotationStraddlesRegion) {
		t.Fatalf("default: err = %v, want ErrAnnotationStraddlesRegion", err)
	}

	w := open()
	report, err := w.Redact(ctx, region, WithRedactionRasterFallback())
	if err != nil {
		t.Fatalf("with the fallback: %v", err)
	}
	if report.BakedAnnotations != 1 || report.RasterisedUnits == 0 {
		t.Errorf("BakedAnnotations = %d, RasterisedUnits = %d, want 1 and some", report.BakedAnnotations, report.RasterisedUnits)
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	out, err := Open(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	pages, err := out.resolvePages(ctx)
	if err != nil {
		t.Fatalf("pages: %v", err)
	}
	if annots, _ := document.ResolveArray(ctx, out.doc, pages[0].Dict["Annots"]); len(annots) != 0 {
		t.Errorf("/Annots = %v, want the square and its pop-up gone", annots)
	}
	for n := 1; n <= out.doc.XRefEntryCount(); n++ {
		d, err := document.ResolveDict(ctx, out.doc, ast.PdfIndirectRef{ObjectNum: n})
		if err != nil || d == nil {
			continue
		}
		if st, _ := d["Subtype"].(ast.PdfName); st == "Square" || st == "Popup" {
			t.Errorf("object %d keeps a removed annotation: %v", n, d)
		}
	}
}

// TestWriter_Redact_FallbackRefusesAnAnnotationAnotherPageShows pins that an
// annotation two pages list is not drawn into one of them: emptied or removed,
// it would vanish from the other.
func TestWriter_Redact_FallbackRefusesAnAnnotationAnotherPageShows(t *testing.T) {
	t.Parallel()
	const square = "8 0 obj\n<< /Type /Annot /Subtype /Square /Rect [100 600 300 700] /AP << /N 9 0 R >> >>\nendobj\n" +
		"9 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] /Length 25 >>\nstream\n1 0 0 rg 0 0 200 100 re f\nendstream\nendobj\n"
	src := imageDoc([]string{"", ""}, "", "/Annots [8 0 R]", square)
	src = bytes.Replace(src, []byte("/Contents 13 0 R "), []byte("/Contents 13 0 R /Annots [8 0 R] "), 1)
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = NewWriter(r).Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{90, 590, 200, 710}}}, WithRedactionRasterFallback())
	if !errors.Is(err, ErrRedactionRefused) || !errors.Is(err, mutations.ErrAnnotationStraddlesRegion) {
		t.Errorf("err = %v, want ErrRedactionRefused wrapping ErrAnnotationStraddlesRegion", err)
	}
}

// TestWriter_Redact_CutsALinkWithTheOption pins [WithRedactionAnnotationClipping]
// through the pixel check: the link that drew nothing keeps the part of its
// area outside the region, and a link another page also lists still refuses.
func TestWriter_Redact_CutsALinkWithTheOption(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	region := []RedactRegion{{Page: 1, Rect: [4]float64{90, 690, 150, 730}}}

	redact := func(src []byte, opts ...RedactOption) ([]byte, *RedactReport, error) {
		r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		w := NewWriter(r)
		report, err := w.Redact(ctx, region, opts...)
		if err != nil {
			return nil, nil, err
		}
		var buf bytes.Buffer
		if err := w.WriteTo(ctx, &buf); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return buf.Bytes(), report, nil
	}

	if _, _, err := redact(clippedLinkDoc(false)); !errors.Is(err, ErrRedactionRefused) || !errors.Is(err, mutations.ErrAnnotationStraddlesRegion) {
		t.Fatalf("default: err = %v, want ErrRedactionRefused wrapping ErrAnnotationStraddlesRegion", err)
	}

	out, report, err := redact(clippedLinkDoc(false), WithRedactionAnnotationClipping())
	if err != nil {
		t.Fatalf("with the option: %v", err)
	}
	if !slices.Equal(report.ClippedAnnotations, []int{8}) {
		t.Errorf("ClippedAnnotations = %v, want [8]", report.ClippedAnnotations)
	}
	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	pages, err := r.resolvePages(ctx)
	if err != nil {
		t.Fatalf("pages: %v", err)
	}
	annots, err := document.ResolveArray(ctx, r.doc, pages[0].Dict["Annots"])
	if err != nil || len(annots) != 1 {
		t.Fatalf("/Annots = %v (%v), want the link", annots, err)
	}
	link, err := document.ResolveDict(ctx, r.doc, annots[0])
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	rect, _ := link["Rect"].(ast.PdfArray)
	if len(rect) != 4 || document.Float(rect[0]) != 150 || document.Float(rect[2]) != 400 {
		t.Errorf("/Rect = %v, want [150 695 400 720]", rect)
	}

	if _, _, err := redact(clippedLinkDoc(true), WithRedactionAnnotationClipping()); !errors.Is(err, mutations.ErrAnnotationStraddlesRegion) {
		t.Errorf("shared link: err = %v, want ErrAnnotationStraddlesRegion", err)
	}
}
