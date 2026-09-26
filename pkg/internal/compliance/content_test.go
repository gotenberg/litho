package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResolvePageContentBytes_UndecodableFilterSkipped covers the §6.2.2
// false-positive guard: a content stream whose /Filter chain ends with a filter
// litho cannot decode (here a misspelled "Flatedecode") stays filter-encoded,
// so [resolvePageContentBytes] reads it as no content instead of returning the
// still-encoded bytes for operator parsing, as veraPDF reads no operator from
// it. A stream with no filter decodes normally.
func TestResolvePageContentBytes_UndecodableFilterSkipped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	badDoc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Type /Page /Contents 3 0 R >>",
		"<< /Filter /Flatedecode /Length 5 >>\nstream\nhello\nendstream",
	)
	badPage, err := document.ResolveDict(ctx, badDoc, ast.PdfIndirectRef{ObjectNum: 2})
	if err != nil {
		t.Fatalf("resolve page: %v", err)
	}
	if data, err := resolvePageContentBytes(ctx, badDoc, badPage); err != nil || len(data) != 0 {
		t.Errorf("undecodable /Filter: want no content and no error, got %q, %v", data, err)
	}

	okDoc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Type /Page /Contents 3 0 R >>",
		"<< /Length 5 >>\nstream\nBT ET\nendstream",
	)
	okPage, err := document.ResolveDict(ctx, okDoc, ast.PdfIndirectRef{ObjectNum: 2})
	if err != nil {
		t.Fatalf("resolve page: %v", err)
	}
	data, err := resolvePageContentBytes(ctx, okDoc, okPage)
	if err != nil {
		t.Fatalf("unfiltered content: unexpected error %v", err)
	}
	if len(data) == 0 {
		t.Error("unfiltered content: want decoded bytes, got none")
	}
}

// TestResolvePageContentBytes_UndecodablePartSkipped pins that an undecodable
// part of a /Contents array costs only that part. ISO 32000-1, 7.7.3.3 makes the
// array one content stream split into parts, and veraPDF judges the parts it can
// read.
//
// # Corpus evidence
//
//   - pdfjs bomb_giant.pdf: follows its text with /JBIG2Decode parts, and
//     veraPDF reports the text's defects whichever order the parts take.
func TestResolvePageContentBytes_UndecodablePartSkipped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, contents := range []string{"[3 0 R 4 0 R]", "[4 0 R 3 0 R]"} {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Page /Contents "+contents+" >>",
			"<< /Filter /Flatedecode /Length 5 >>\nstream\nhello\nendstream",
			"<< /Length 5 >>\nstream\nBT ET\nendstream",
		)
		page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve page: %v", err)
		}
		data, err := resolvePageContentBytes(ctx, doc, page)
		if err != nil {
			t.Fatalf("contents %s: unexpected error %v", contents, err)
		}
		if !bytes.Contains(data, []byte("BT ET")) || bytes.Contains(data, []byte("hello")) {
			t.Errorf("contents %s: want the readable part alone, got %q", contents, data)
		}
	}
}
