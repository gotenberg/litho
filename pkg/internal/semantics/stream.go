package semantics

import (
	"context"
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// openStreamFunc returns a lazy Open function that decodes a PDF stream
// through its filter pipeline on demand. The returned io.ReadCloser streams
// decoded data without buffering the entire resource at parse time.
func openStreamFunc(doc *document.Document, stream ast.PdfStream) func(ctx context.Context) (io.ReadCloser, error) {
	return func(ctx context.Context) (io.ReadCloser, error) {
		return doc.NewStreamReader(ctx, stream)
	}
}
