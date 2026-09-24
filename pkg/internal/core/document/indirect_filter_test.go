package document

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestDecodeStream_IndirectFilter pins that a stream names its filters
// indirectly without becoming unreadable.
//
// Per ISO 32000-1, 7.3.10, any value may be written as an indirect reference
// unless its table says otherwise, and Table 5 places no such restriction on
// /Filter or /DecodeParms. The filter pipeline receives only the dictionary and
// cannot resolve anything, so litho returned "invalid type for /Filter" and
// decoded nothing.
//
// # Corpus evidence
//
//   - pdfjs bug1727053.pdf: writes /Filter 3 0 R for a 751 KB embedded
//     TrueType program, which left the font unreadable and made litho report
//     the font as not embedded.
func TestDecodeStream_IndirectFilter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// "Hi" run through ASCIIHexDecode.
	const hex = "4869>"

	decode := func(t *testing.T, objs ...string) ([]byte, error) {
		t.Helper()
		data := buildDestPDF(objs...)
		doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		stream, err := ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve stream: %v", err)
		}
		return doc.DecodeStream(ctx, stream)
	}

	t.Run("filter named by an indirect reference", func(t *testing.T) {
		t.Parallel()
		got, err := decode(t,
			"<< /Type /Catalog /Pages 4 0 R >>",
			"<< /Length 5 /Filter 3 0 R >>\nstream\n"+hex+"\nendstream",
			"/ASCIIHexDecode",
			"<< /Type /Pages /Kids [] /Count 0 >>",
		)
		if err != nil {
			t.Fatalf("decode with indirect /Filter: %v", err)
		}
		if string(got) != "Hi" {
			t.Errorf("decoded %q, want %q", got, "Hi")
		}
	})

	t.Run("filter array element named by an indirect reference", func(t *testing.T) {
		t.Parallel()
		got, err := decode(t,
			"<< /Type /Catalog /Pages 4 0 R >>",
			"<< /Length 5 /Filter [3 0 R] >>\nstream\n"+hex+"\nendstream",
			"/ASCIIHexDecode",
			"<< /Type /Pages /Kids [] /Count 0 >>",
		)
		if err != nil {
			t.Fatalf("decode with indirect filter array element: %v", err)
		}
		if string(got) != "Hi" {
			t.Errorf("decoded %q, want %q", got, "Hi")
		}
	})

	t.Run("direct filter still decodes", func(t *testing.T) {
		t.Parallel()
		got, err := decode(t,
			"<< /Type /Catalog /Pages 4 0 R >>",
			"<< /Length 5 /Filter /ASCIIHexDecode >>\nstream\n"+hex+"\nendstream",
			"null",
			"<< /Type /Pages /Kids [] /Count 0 >>",
		)
		if err != nil {
			t.Fatalf("decode with direct /Filter: %v", err)
		}
		if string(got) != "Hi" {
			t.Errorf("decoded %q, want %q", got, "Hi")
		}
	})

	t.Run("unfiltered stream is untouched", func(t *testing.T) {
		t.Parallel()
		got, err := decode(t,
			"<< /Type /Catalog /Pages 4 0 R >>",
			"<< /Length 2 >>\nstream\nHi\nendstream",
			"null",
			"<< /Type /Pages /Kids [] /Count 0 >>",
		)
		if err != nil {
			t.Fatalf("decode without /Filter: %v", err)
		}
		if string(got) != "Hi" {
			t.Errorf("decoded %q, want %q", got, "Hi")
		}
	})
}
