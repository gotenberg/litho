package annot

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// FuzzCollect opens the fuzzed bytes and collects the annotation appearances
// of their first page for view and for print, building those it synthesises.
// Collect never panics.
func FuzzCollect(f *testing.F) {
	seed := onePage("", "[5 0 R 6 0 R 7 0 R 8 0 R]",
		annot("Square", FlagPrint, "<< /N 9 0 R >>", "/C [1 0 0]"),
		"<< /Type /Annot /Subtype /Ink /Rect [10 10 90 90] /F 4 /InkList [[10 10 50 80 90 10]] /C [0 0 1] /BS << /W 3 /S /D /D [3 2] >> >>",
		"<< /Type /Annot /Subtype /Widget /FT /Btn /Rect [0 0 20 20] /AS /On /AP << /N << /On 9 0 R /Off 9 0 R >> >> /MK << /R 90 >> >>",
		"<< /Type /Annot /Subtype /Highlight /Rect [0 0 50 20] /QuadPoints [0 20 50 20 0 0 50 0] /C [1 1 0] /CA 0.5 >>",
		form("/BBox [0 0 60 60] /Matrix [0 1 -1 0 60 0]"),
	)
	f.Add(seed)
	f.Add(seed[:len(seed)/2])
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := context.Background()
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			return
		}
		catalog, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			return
		}
		pages, err := document.ResolvePages(ctx, doc, catalog)
		if err != nil || len(pages) == 0 {
			return
		}
		for _, mode := range []Mode{View, Print} {
			// A damaged annotation is a skip, not an error: Collect fails on
			// ctx alone.
			if _, _, err := Collect(ctx, doc, pages[0], Options{Mode: mode, Synthesize: true}); err != nil {
				t.Fatalf("Collect: %v", err)
			}
		}
	})
}
