package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// FuzzNameSplices asserts the name walk of the content-stream rename never
// panics on crafted bytes and returns splices in stream order that do not
// overlap and lie inside the stream, so [applyTokenSplices] can write them,
// each covering a span that begins with the solidus of a name.
func FuzzNameSplices(f *testing.F) {
	f.Add([]byte("BT /F#20one 12 Tf (x) Tj ET /Tag <</MCID 0>> BDC EMC q BI /W 1 /H 1 /BPC 8 /CS /Cs1 ID \x00/ EI Q /Fm0 Do"))
	f.Add([]byte("BT /F#2")) // Truncated inside an escape.
	f.Fuzz(func(t *testing.T, data []byte) {
		splices, _, _ := nameSplices(context.Background(), data, func(name string) (ast.PdfName, bool) {
			return ast.PdfName("N" + name), len(name)%2 == 0
		})
		prev := 0
		for _, s := range splices {
			if s.start < prev || s.end < s.start || s.end > len(data) || data[s.start] != '/' {
				t.Fatalf("splice [%d, %d) after %d in a %d-byte stream", s.start, s.end, prev, len(data))
			}
			prev = s.end
		}
		applyTokenSplices(data, splices)
	})
}
