package pdf

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// gluedOperatorsPDF is a one-page file whose content stream writes ET and BT
// with no white space between them, as pdfjs sci-notation.pdf does.
func gluedOperatorsPDF() []byte {
	const stream = "BT /F1 24 Tf 50 500 Td (First line) Tj ETBT /F1 12 Tf 50 600 Td (Second line) Tj ET"
	return buildTestPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, "")
}

// TestGluedOperatorsTextFollowsPDFJS pins that text extraction reads "ETBT" as
// ET then BT, as PDF.js does: the Td after it places the second line from the
// origin of a new text object, at y 600, where PDF.js places it. Read as one
// undefined operator, the Td moved the line from the first line's origin,
// inside the first text object, to y 1100.
func TestGluedOperatorsTextFollowsPDFJS(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := gluedOperatorsPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})

	text, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "First line") || !strings.Contains(text, "Second line") {
		t.Errorf("ExtractText = %q, want both lines", text)
	}

	elements, err := r.ExtractTextElements(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}
	var second *models.TextElement
	for i := range elements {
		if strings.Contains(elements[i].Text, "Second") {
			second = &elements[i]
			break
		}
	}
	if second == nil {
		t.Fatalf("ExtractTextElements = %+v, want an element for the second line", elements)
	}
	if math.Abs(second.X-50) > 1 || math.Abs(second.Y-600) > 1 {
		t.Errorf("the second line starts at (%g, %g), want (50, 600)", second.X, second.Y)
	}
}

// TestGluedOperatorsStayUndefinedForCompliance pins that validation keeps
// reading "ETBT" as the one operator ISO 32000-1, Annex A does not define, which
// veraPDF reports under ISO 19005-2, 6.2.2 t1: its content stream parser
// (PDFStreamParser.nextOperator) reads an operator up to white space or a
// delimiter.
func TestGluedOperatorsStayUndefinedForCompliance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := gluedOperatorsPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})

	violations, err := r.ValidatePDFAReport(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFAReport: %v", err)
	}
	var undefined []string
	for _, v := range violations {
		if v.Clause == "6.2.2" && v.TestNumber == 1 {
			undefined = append(undefined, v.Context)
		}
	}
	if len(undefined) != 1 || !strings.Contains(undefined[0], `"ETBT"`) {
		t.Errorf("ISO 19005-2, 6.2.2 t1 violations = %q, want one for ETBT", undefined)
	}
}
