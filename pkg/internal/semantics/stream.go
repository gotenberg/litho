package semantics

import (
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// openStreamFunc returns a lazy Open function that decodes a PDF stream
// through its filter pipeline on demand. The returned io.ReadCloser streams
// decoded data without buffering the entire resource at parse time.
func openStreamFunc(stream ast.PdfStream) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		return filter.NewStreamReader(stream)
	}
}
