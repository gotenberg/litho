package pdf

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// kOnlyPDF paints DeviceGray black text and a DeviceRGB black rectangle.
func kOnlyPDF() []byte {
	return colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		colorStream("", "0 g BT /F1 12 Tf 10 10 Td (Ink) Tj ET 0 0 0 rg 50 50 20 20 re f"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// kOnlyConverted converts kOnlyPDF to CMYK with opts and returns the k
// operators of the output's page, one per line, and the report.
func kOnlyConverted(t *testing.T, opts ...ColorOption) (string, *ColorReport) {
	t.Helper()
	ctx := context.Background()
	w := NewWriter(openColorPDF(t, kOnlyPDF()))
	report, err := w.ConvertColors(ctx, CMYKColorTarget(), opts...)
	if err != nil {
		t.Fatalf("ConvertColors: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	doc, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) != 1 {
		t.Fatalf("ResolvePages: %d pages, %v", len(pages), err)
	}
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict, content.WholeStream())
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}
	var lines []string
	for _, op := range ops {
		if op.Operator != "k" {
			continue
		}
		parts := make([]string, 0, len(op.Operands)+1)
		for _, o := range op.Operands {
			v, _ := document.Number(o)
			parts = append(parts, strconv.FormatFloat(v, 'f', -1, 64))
		}
		lines = append(lines, strings.Join(append(parts, "k"), " "))
	}
	return strings.Join(lines, "\n"), report
}

func TestConvertColors_KOnlyBlack(t *testing.T) {
	got, report := kOnlyConverted(t, WithKOnlyBlack())
	if got != "0 0 0 1 k\n0 0 0 1 k" {
		t.Errorf("colours %q, want two K-only blacks", got)
	}
	if report.KOnlyBlackColors != 2 {
		t.Errorf("KOnlyBlackColors %d, want 2", report.KOnlyBlackColors)
	}

	got, report = kOnlyConverted(t)
	if strings.Contains(got, "0 0 0 1 k") {
		t.Errorf("default colours %q, want four-colour blacks", got)
	}
	if report.KOnlyBlackColors != 0 {
		t.Errorf("default KOnlyBlackColors %d", report.KOnlyBlackColors)
	}
}
