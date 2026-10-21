package flatten

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// FuzzDocument flattens a page drawing arbitrary content against the
// fixture's resources, a transparent state and a font among them, and
// requires it never to panic.
func FuzzDocument(f *testing.F) {
	f.Add([]byte("0 0 1 rg 0 0 100 100 re f q /GS1 gs 1 0 0 rg 10 10 30 30 re f Q BT /F1 12 Tf 10 20 Td (Hi) Tj ET"))
	f.Add([]byte("q /GS1 gs 1 0 0 rg 10 10 30 30 re f Q BT /F1 12 Tf 10 2"))
	f.Fuzz(func(t *testing.T, content []byte) {
		data := fixture{content: string(content)}.bytes()
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
		if err != nil {
			return
		}
		alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
		// An error is an answer; a panic is the defect this looks for.
		_, _ = Document(ctx, alloc, doc, pages, Options{DPI: 36}) //nolint:errcheck // Deliberate: the fuzz target checks for panics only.
	})
}
