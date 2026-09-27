package mutations

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func TestSourceVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		header, catalog string
		want            string
	}{
		// The reader reports 1.7 here, and writing that into the header
		// left no trace of the 2.0 the source declared.
		{"a later header wins over the catalog", "2.0", "1.7", "2.0"},
		{"an earlier header is not lowered to the catalog", "1.7", "1.4", "1.7"},
		{"a later catalog entry raises the header", "1.4", "1.7", "1.7"},
		{"a header alone stands", "2.0", "2.0", "2.0"},
		{"a headerless file takes its catalog's version", "", "1.6", "1.6"},
		{"a file declaring nothing yields nothing", "", "", ""},
		{"versions compare as numbers", "1.10", "1.5", "1.10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Version is what document.initCatalogVersion leaves: the catalog
			// entry when there is one, the header's otherwise.
			read := tc.catalog
			if read == "" {
				read = tc.header
			}
			doc := &document.Document{HeaderVersion: tc.header, Version: read}
			if got := sourceVersion(doc); got != tc.want {
				t.Errorf("sourceVersion(header %q, catalog %q) = %q, want %q", tc.header, tc.catalog, got, tc.want)
			}
		})
	}
}

// TestScanOutputForTokens_WalksADirectInfo pins that the redaction scan reads
// a document information dictionary the trailer holds directly. The rewrite
// copies one (see copyTrailerInfo), so a scan following only a reference
// would pass a file whose /Info still holds the redacted word.
func TestScanOutputForTokens_WalksADirectInfo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	body := "BT /F1 12 Tf 100 700 Td (KEEP) Tj ET"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 5 /Root 1 0 R /Info << /Title (SECRET) >> >>\n%%EOF\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if _, direct := doc.Index.Trailer["Info"].(ast.PdfDict); !direct {
		t.Fatalf("trailer /Info = %T, want a direct dictionary", doc.Index.Trailer["Info"])
	}

	out, err := ScanOutputForTokens(ctx, nil, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScanOutputForTokens: %v", err)
	}
	if !hasHit(out, 0, "string") {
		t.Errorf("the direct /Info copy was not reported: %+v", out.Hits)
	}
}
