package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResolvePageContentBytesKeepsTheReadableParts pins that one unreadable
// part of a /Contents array does not cost the page every other part.
//
// ISO 32000-1, 7.7.3.3 lets a page split its content across an array of
// streams, and a truncated part says nothing about the rest. Returning an error
// for the whole page threw away every operator the readable parts hold, so each
// content-driven check saw an empty page. Viewers render what they can, and
// [content.ParsePageContent] already skips the part and keeps going.
//
// # Corpus evidence
//
//   - pdfjs issue17190_1.pdf: carries 77 parts with one unreadable, and litho
//     reported none of the colour or font defects veraPDF 1.30.2 raises from
//     the other 76.
func TestResolvePageContentBytesKeepsTheReadableParts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Type /Page /Contents [3 0 R 4 0 R 5 0 R] >>",
		"<< /Length 8 >>\nstream\n0 0 0 rg\nendstream",
		// Declared FlateDecode over bytes that are not a Flate stream.
		"<< /Filter /FlateDecode /Length 9 >>\nstream\nnotflate!\nendstream",
		"<< /Length 5 >>\nstream\nBT ET\nendstream",
	)
	page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
	if err != nil {
		t.Fatalf("resolve page: %v", err)
	}

	data, err := resolvePageContentBytes(ctx, doc, page)
	if err != nil {
		t.Fatalf("one bad part cost the whole page: %v", err)
	}
	for _, want := range []string{"0 0 0 rg", "BT ET"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("readable part %q is missing from %q", want, data)
		}
	}
}

// TestResolvePageContentBytesReportsATotalLoss pins the other side: when no
// part decodes there is nothing to check, and the caller has to hear about it
// so the conservative fallbacks engage rather than reading the page as empty.
func TestResolvePageContentBytesReportsATotalLoss(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Type /Page /Contents [3 0 R 4 0 R] >>",
		"<< /Filter /FlateDecode /Length 9 >>\nstream\nnotflate!\nendstream",
		"<< /Filter /FlateDecode /Length 9 >>\nstream\nalsonotfl\nendstream",
	)
	page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
	if err != nil {
		t.Fatalf("resolve page: %v", err)
	}

	data, err := resolvePageContentBytes(ctx, doc, page)
	if err == nil {
		t.Errorf("a page whose every part failed returned %d bytes and no error", len(data))
	}
}
