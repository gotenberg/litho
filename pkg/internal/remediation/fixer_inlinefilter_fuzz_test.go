package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// FuzzReencodeInlineImages asserts the re-encoding of inline images never
// panics on crafted bytes, leaves a stream it does not rewrite as written,
// keeps every operation, and leaves nothing to rewrite a second time. The
// stream's names resolve in a resource dictionary of its own, so an image that
// applies JPXDecode or JBIG2Decode moves to an image XObject
// ([moveInlineImage]).
func FuzzReencodeInlineImages(f *testing.F) {
	lzw := "\x80\x00\x08\x08\x07\xfb\xfd\x00\x40\x00\x40\x40"
	f.Add([]byte("q BI /W 4 /H 2 /BPC 8 /CS /G /F /LZW ID "+lzw+" EI Q"), 2)
	f.Add([]byte("q BI /W 4 /H 2 /BPC 8 /CS /G /F /LZW ID "+lzw[:5]), 1) // Truncated inside the image data.
	jbig2 := "\x00\x00\x00\x00\x30\x00\x01\x00\x00\x00\x13\x00\x00\x00\x08\x00\x00\x00\x08\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00" +
		"\x00\x00\x00\x01\x27\x00\x01\x00\x00\x00\x1f\x00\x00\x00\x08\x00\x00\x00\x08\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01" +
		"\x26\xbd\xbb\x76\xed\xdb\xb6\x4d\x53\x80\x08\x00\x80" +
		"\x00\x00\x00\x02\x31\x00\x01\x00\x00\x00\x00"
	f.Add([]byte("q BI /W 8 /H 8 /CS /G /BPC 1 /F /JBIG2Decode ID "+jbig2+"\nEI Q"), 2)
	f.Add([]byte("q BI /W 8 /H 8 /CS /G /BPC 1 /F /JBIG2Decode ID "+jbig2[:20]), 4) // Truncated inside the image data.
	src := buildStructPDF("<< /Type /Catalog >>")
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(src), int64(len(src)), nil)
	if err != nil {
		f.Fatalf("NewDocument: %v", err)
	}
	f.Fuzz(func(t *testing.T, data []byte, part int) {
		ctx := context.Background()
		part = 1 + int(uint(part)%4)
		dec := filter.NewDecoder(nil, limits.Default())
		edit := func() *resourceEdit {
			res := ast.PdfDict{}
			return newResourceEdit(writer.NewObjAllocator(1), doc, resourceOwner{
				load:  func() ast.PdfObject { return res },
				store: func(d ast.PdfDict) bool { res = d; return true },
			})
		}
		out, n, moved := reencodeInlineImages(ctx, data, part, dec, edit())
		if n+moved == 0 {
			if !bytes.Equal(out, data) {
				t.Fatalf("rewrote nothing in %q, but wrote %q", data, out)
			}
			return
		}
		before, err := content.ParseStreamData(ctx, data, content.ForRepair())
		if err != nil {
			t.Fatalf("rewrote %q, which does not parse: %v", data, err)
		}
		after, err := content.ParseStreamData(ctx, out, content.ForRepair())
		if err != nil || len(after) != len(before) {
			t.Fatalf("the rewrite of %q read %d operations as %d (%v): %q", data, len(before), len(after), err, out)
		}
		if _, again, movedAgain := reencodeInlineImages(ctx, out, part, dec, edit()); again+movedAgain != 0 {
			t.Fatalf("the rewrite of %q left %d images to rewrite and %d to move: %q", data, again, movedAgain, out)
		}
	})
}
