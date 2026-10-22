package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// formTextPDF builds a page whose content is one form showing two words,
// LEFT at x 100 and RIGHT at x 400, at y 700.
func formTextPDF() []byte {
	const form = "BT /F1 12 Tf 100 700 Td (LEFT) Tj 300 0 Td (RIGHT) Tj ET"
	const page = "/Fm0 Do"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Fm0 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /Font << /F1 6 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(form), form),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// TestWriterRedactRasterFallback pins [WithRedactionRasterFallback]: a region
// over a word a form draws is refused without it, and with it the word goes,
// the other stays, and the report counts the form, on a Writer holding an
// edit queued before the redaction, which the fallback writes out first.
func TestWriterRedactRasterFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := formTextPDF()
	region := []RedactRegion{{Page: 1, Rect: [4]float64{390, 690, 500, 720}}}

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if _, err := w.Redact(ctx, region); !errors.Is(err, ErrRedactionRefused) {
		t.Fatalf("without the fallback: err = %v, want ErrRedactionRefused", err)
	}

	w.SetMetadata(Metadata{Title: "Kept title"})
	report, err := w.Redact(ctx, region, WithRedactionRasterFallback())
	if err != nil {
		t.Fatalf("with the fallback: %v", err)
	}
	if report.RasterisedUnits != 1 || strings.Join(report.RemovedText, "|") != "RIGHT" {
		t.Errorf("report: %d units, removed %q; want 1 and RIGHT", report.RasterisedUnits, report.RemovedText)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if bytes.Contains(out.Bytes(), []byte("RIGHT")) {
		t.Error("the redacted word is still in the file")
	}
	got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	text, err := got.ExtractText(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "LEFT") {
		t.Errorf("text = %q, want LEFT kept", text)
	}
}
