package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// emptyNamePDF builds a one-page document whose content stream selects a font
// by the name selector verbatim, so a caller can pass the empty name ("/") or a
// name no resource dictionary defines.
func emptyNamePDF(t *testing.T, selector string) []byte {
	t.Helper()

	page := fmt.Sprintf("BT %s 12 Tf 20 700 Td (x) Tj ET\n", selector)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestEmptyResourceNameIsNotAReference pins that the empty name object is not
// reported as an undefined named resource.
//
// ISO 32000-1, 7.3.5 allows a name to be empty, and no resource dictionary can
// usefully key an entry on it, so veraPDF 1.30.2 models no reference for it.
// Reporting it invented one violation per occurrence.
//
// # Corpus evidence
//
//   - pdfjs bug1669099.pdf: writes "/ 12.00000 Tf" in 15 widget appearance
//     streams, and veraPDF reports no ISO 19005-4, 6.2.2 t3 anywhere in that
//     file.
func TestEmptyResourceNameIsNotAReference(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		selector string
		want     bool
	}{
		{name: "the empty name is not a reference", selector: "/", want: false},
		{
			// The control: the same fixture with a name that IS a reference
			// reports it, so a silent empty-name case cannot be the fixture
			// failing to reach the rule.
			name:     "a name no dictionary defines is still reported",
			selector: "/F9",
			want:     true,
		},
		{name: "a defined name stays silent", selector: "/F1", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := emptyNamePDF(t, tc.selector)
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
			stream, err := document.ResolveStream(ctx, doc, pages[0].Dict["Contents"])
			if err != nil {
				t.Fatalf("ResolveStream: %v", err)
			}
			raw, err := doc.DecodeStream(ctx, stream)
			if err != nil {
				t.Fatalf("DecodeStream: %v", err)
			}
			ops, err := content.ParseStreamData(ctx, raw)
			if err != nil {
				t.Fatalf("ParseStreamData: %v", err)
			}
			res := resolveResources(ctx, doc, pages[0].Dict)

			got := false
			for _, v := range checkContentStreamResourcesOps(ctx, doc, ops, nil, false, res, nil, 4, 1, false, "") {
				if v.Rule.Clause == "6.2.2" && v.Rule.TestNumber == 3 {
					got = true
				}
			}
			if got != tc.want {
				t.Errorf("§6.2.2 t3 fired = %v, want %v for selector %q", got, tc.want, tc.selector)
			}
		})
	}
}
