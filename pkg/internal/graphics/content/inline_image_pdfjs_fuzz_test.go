package content

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// FuzzPDFJSInlineImageEnd feeds arbitrary bytes to the reading of inline image
// data PDF.js does ([pdfjsImages.end]), under every filter it reads a
// marker for, and asserts crash-freedom, termination, and that reading resumes
// inside the stream and no earlier than the data starts.
//
// The bytes are a content stream's, entirely file-controlled: the scan steps
// over DCT segment lengths the data states, and lexes the bytes after every
// EI it meets.
func FuzzPDFJSInlineImageEnd(f *testing.F) {
	f.Add([]byte("ID xyEI Q\nBT (A) Tj ET"))
	f.Add([]byte("ID \xff\xd8\xff\xe0\x00\x10 EI Q \xff\xd9 EI\n~> EI >EI x EI 1 2 Q"))
	// Truncated: an EI with a string, a sign and a DCT marker cut short
	// after it.
	f.Add([]byte("ID x EI (a\\1 EI -"))
	f.Add([]byte("ID \xff\xc0"))

	filters := []ast.PdfDict{{}, {"F": ast.PdfName("DCT")}, {"F": ast.PdfName("A85")}, {"Filter": ast.PdfArray{ast.PdfName("AHx")}}}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			return
		}
		ctx := context.Background()
		// One reader serves every image, as it serves the images of a stream.
		r := newPDFJSImages(data)
		for _, idEnd := range []int{0, len(data) / 2, len(data)} {
			start := min(idEnd+1, len(data))
			for _, dict := range filters {
				if got, _ := r.end(ctx, idEnd, dict); got < start || got > len(data) {
					t.Fatalf("end(%d, %v) = %d, outside [%d, %d]", idEnd, dict, got, start, len(data))
				}
			}
		}
	})
}
