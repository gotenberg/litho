package pdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestRemediate_TaggedContentIsCompressed pins that the content stream the
// auto-tagging marks again is written FlateDecode-compressed. Written plain,
// it weighed several times the compressed stream it replaced.
func TestRemediate_TaggedContentIsCompressed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := untaggedHeadingPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if _, err := w.RemediatePDFUA(ctx, models.PDFUA1); err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r2.Close()
	catalog, err := document.ResolveCatalog(ctx, r2.doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, r2.doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v", err)
	}
	ref, ok := pages[0].Dict["Contents"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("/Contents = %T, want one stream", pages[0].Dict["Contents"])
	}
	obj, err := r2.doc.Resolve(ctx, ref)
	if err != nil {
		t.Fatalf("resolving /Contents: %v", err)
	}
	stream, ok := obj.(ast.PdfStream)
	if !ok {
		t.Fatalf("/Contents resolves to %T, want a stream", obj)
	}
	if f, _ := stream.Dict["Filter"].(ast.PdfName); f != "FlateDecode" {
		t.Errorf("the marked content's /Filter is %v, want FlateDecode", stream.Dict["Filter"])
	}
	if got := pageContent(ctx, t, out.Bytes(), 1); !bytes.Contains([]byte(got), []byte("/H1 <</MCID")) {
		t.Error("the compressed content lost its marks")
	}
}
