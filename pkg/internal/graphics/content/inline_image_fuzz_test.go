package content

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// FuzzInlineImages feeds arbitrary bytes to the reader of inline image data
// that a parse reading a stream as veraPDF does uses ([inlineImages]), and
// asserts crash-freedom and that reading never resumes before the data it
// was asked to read.
//
// The bytes are a content stream's, entirely file-controlled, and the reader
// indexes them itself: it scans image data for EI, reads the token after
// each, and parses the dictionaries of the images that follow.
func FuzzInlineImages(f *testing.F) {
	f.Add([]byte("ID xx EI\nBI /W 1 /H 1 /L 2 ID yy EI\n% c\nBI ID zz EI Q"))
	f.Add([]byte("ID x EI %EI %EI\nzz EI n0 EI B0 EI )x EI ID yyy EI d0"))
	// Truncated: an EI with no white space after it, and a dictionary with
	// no ID.
	f.Add([]byte("ID xx EI BI /W [1 2"))
	f.Add([]byte("ID xx E"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			return
		}
		ctx := context.Background()
		for _, idEnd := range []int{0, len(data) / 2, len(data)} {
			r := newInlineImages(data)
			for _, dict := range []ast.PdfDict{nil, {"L": ast.PdfInt(3)}, {"Length": ast.PdfReal(1e18)}} {
				if got := r.resume(ctx, idEnd, dict); got < idEnd {
					t.Fatalf("resume(%d) = %d, before the data", idEnd, got)
				}
			}
		}
	})
}
