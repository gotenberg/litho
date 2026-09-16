package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// TestCheckTrailerID_NotAnArrayFailsEveryPart pins ISO 19005-1/2/4, 6.1.3 t1 on
// a trailer /ID that is not an array: file identifiers are an array of two
// byte strings (ISO 32000-1, 14.4), and veraPDF reads any other value as no
// identifier at all, under every part. pdfjs issue5599.pdf writes a number and
// issue11651.pdf null; both were measured at PDF/A-1b, 2b and 4.
func TestCheckTrailerID_NotAnArrayFailsEveryPart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		id   ast.PdfObject
	}{
		{"a number", ast.PdfInt(77777777777777777)},
		{"null", ast.PdfNull{}},
		{"a string", ast.PdfString("abcd")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, profile := range []string{"PDF/A-1b", "PDF/A-2b", "PDF/A-4"} {
				doc := &document.Document{Index: xref.Index{Trailer: ast.PdfDict{"ID": tc.id}}}
				if len(checkTrailerID(context.Background(), doc, profile)) == 0 {
					t.Errorf("%s: an /ID that is %s must fail 6.1.3 t1", profile, tc.name)
				}
			}
		})
	}
}
