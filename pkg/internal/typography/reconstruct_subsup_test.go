package typography

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// subsupPDF builds a one-page document whose page shows body, with /F1
// naming Helvetica and fonts adding further font resources, each a name
// and the dictionary it names. objs are further indirect objects, from
// object 10 on.
func subsupPDF(body, fonts string, objs ...string) []byte {
	var b strings.Builder
	b.WriteString("%PDF-1.7\n")
	b.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	b.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	b.WriteString("4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")
	b.WriteString("5 0 obj\n<< /Font << /F1 4 0 R " + fonts + " >> >>\nendobj\n")
	b.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n")
	b.WriteString("6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n")
	for i, o := range objs {
		b.WriteString(strconv.Itoa(10+i) + " 0 obj\n" + o + "\nendobj\n")
	}
	b.WriteString("trailer\n<< /Size " + strconv.Itoa(10+len(objs)) + " /Root 1 0 R >>\n%%EOF\n")
	return []byte(b.String())
}

// subsupExtract returns the text litho extracts from the page of data.
func subsupExtract(t *testing.T, data []byte) string {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	got, err := ExtractText(ctx, doc, machine, pages[0], ops, ExtractionConfig{})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	return got
}

// TestExtractText_MarkedContentEndsRun pins that a marked-content operator
// ends the run of the glyph before it, whether or not the sequence carries
// an MCID, as it ends PDF.js's text item. The subscript "X" is shown by the
// Tf of "NO" through a smaller text matrix, 2.4 units below the baseline:
// less than a quarter of the size of "NO", so only the BDC before it
// starts a run, and the run breaks the line past the 2-unit tolerance.
// Without the BDC the subscript continues the run of "NO".
//
// # Corpus evidence
//
//   - govdocs thread_000_000282.pdf: "NOX" and "SO2" in Touch-Up_Line
//     sequences, read "NOX" where PDF.js reads "NO X".
func TestExtractText_MarkedContentEndsRun(t *testing.T) {
	t.Parallel()
	// Helvetica sets "NO" 722 + 778 units wide, 18 units at 12 points.
	const noRun = "BT /F1 1 Tf 12 0 0 12 72 700 Tm (NO) Tj ET "
	for _, tc := range []struct {
		name, body, want string
	}{
		{"marked content", noRun + "/Span << >> BDC BT /F1 1 Tf 5 0 0 5.76 90 697.6 Tm (X) Tj ET EMC", "NO\nX"},
		{"no marked content", noRun + "BT /F1 1 Tf 5 0 0 5.76 90 697.6 Tm (X) Tj ET", "NOX"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := subsupExtract(t, subsupPDF(tc.body, "")); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
