package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// overprintPDF paints content on one page whose resources name the
// graphics state dictionary object 5, gs.
func overprintPDF(gs, content string) []byte {
	return colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R /Resources << /ExtGState << /GS0 5 0 R >> >> >>",
		colorStream("", content),
		gs,
	})
}

func TestConvertColors_OverprintModePreserved(t *testing.T) {
	ctx := context.Background()
	r := openColorPDF(t, overprintPDF("<< /Type /ExtGState /OP true /OPM 1 >>", "/GS0 gs 1 0 0 rg 0 0 50 50 re f"))
	w := NewWriter(r)
	report, err := w.ConvertColors(ctx, CMYKColorTarget())
	if err != nil {
		t.Fatalf("ConvertColors: %v", err)
	}
	if report.OverprintPreserved != 1 || report.OverprintStates != 1 || report.OverprintChanged != 0 {
		t.Fatalf("report: %+v", report)
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
	states, err := document.ResolveDict(ctx, doc, pages[0].DrawResources["ExtGState"])
	if err != nil {
		t.Fatalf("resolving /ExtGState: %v", err)
	}
	gs, err := document.ResolveDict(ctx, doc, states["GS0"])
	if err != nil {
		t.Fatalf("resolving the graphics state: %v", err)
	}
	if fmt.Sprint(gs["OPM"]) != "0" {
		t.Errorf("/OPM %v, want 0", gs["OPM"])
	}
}

func TestConvertColors_OverprintChangeReported(t *testing.T) {
	r := openColorPDF(t, overprintPDF("<< /Type /ExtGState /OP true /OPM 1 >>", "/GS0 gs 0 1 1 0 k 0 0 50 50 re f"))
	report, err := NewWriter(r).ConvertColors(context.Background(), RGBColorTarget())
	if err != nil {
		t.Fatalf("ConvertColors: %v", err)
	}
	if report.OverprintChanged != 1 || report.OverprintPreserved != 0 {
		t.Errorf("report: %+v", report)
	}
}
