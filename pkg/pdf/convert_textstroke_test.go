package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestConvert_StrokedTextReadsOnce holds text painted with
// -webkit-text-stroke to one text run a phrase: in the plain conversion
// and once remediated to PDF/UA-1, every copy of the page's text read,
// hidden ones and artifacts included, holds each phrase once and in
// order, whether the stroke is painted over the fill, under it by
// paint-order, or alone over a transparent fill; and the PDF/UA file
// conforms.
func TestConvert_StrokedTextReadsOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fonts := WithConvertFontDirs(convertFontDir(t))
	src := `<html lang="en"><head><title>Stroke</title></head><body>
<h1 style="-webkit-text-stroke: 3px red">Night Market</h1>
<p style="font-size: 40px; -webkit-text-stroke: 4px navy; paint-order: stroke fill">by the river</p>
<p style="font-size: 30px; -webkit-text-stroke: 1px black; -webkit-text-fill-color: transparent">hollow words</p>
</body></html>`
	var plain, ua bytes.Buffer
	if _, err := FromHTML(strings.NewReader(src), fonts).WriteTo(ctx, &plain); err != nil {
		t.Fatalf("converting: %v", err)
	}
	var report *models.RemediationReport
	_, err := NewPipeline().Convert(FromHTML(strings.NewReader(src), fonts)).Apply(func(ctx context.Context, w *Writer) error {
		var err error
		report, err = w.RemediatePDFUA(ctx, models.PDFUA1)
		return err
	}).WriteTo(ctx, &ua, WithFullRewrite())
	if err != nil {
		t.Fatalf("remediating: %v", err)
	}
	if len(report.Residual) > 0 {
		t.Errorf("PDF/UA-1 residual %v", report.Residual)
	}
	for name, data := range map[string][]byte{"plain": plain.Bytes(), "PDF/UA-1": ua.Bytes()} {
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("%s: Open: %v", name, err)
		}
		text, err := r.ExtractText(ctx, 1, WithHiddenText())
		if err != nil {
			t.Fatalf("%s: ExtractText: %v", name, err)
		}
		flat := strings.Join(strings.Fields(text), " ")
		if flat != "Night Market by the river hollow words" {
			t.Errorf("%s: every copy of the text reads %q, want each phrase once", name, flat)
		}
		if name == "PDF/UA-1" {
			if v, err := r.ValidatePDFUA(ctx, models.PDFUA1); err != nil || len(v) > 0 {
				t.Errorf("PDF/UA-1 violations %v (%v)", v, err)
			}
		}
		r.Close()
	}
}
