package mutations

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// FuzzMatchContentBytes scans arbitrary decoded content stream bytes for a
// token as the output scan does ([scanWalk.matchContentBytes] and
// [scanWalk.scanContentStrings]), skipping the samples of the inline images
// the bytes hold. It asserts the scan never panics or fails, and that a token
// in bytes holding no inline image is found.
func FuzzMatchContentBytes(f *testing.F) {
	body := "BT /F1 12 Tf (SECRET) Tj ET q BI /W 6 /H 1 /CS /G /BPC 8 ID SECRET EI Q SECRET"
	f.Add([]byte(body))
	f.Add([]byte(body[:len(body)/2+20]))
	f.Fuzz(func(t *testing.T, data []byte) {
		doc := copiesDoc(t, copiesObjects(""), nil, "")
		w := newScanWalk(doc, nil, []string{nestedWord})
		ctx := context.Background()
		w.matchContentBytes(ctx, 4, ast.PdfStream{Dict: ast.PdfDict{}}, data)
		w.scanContentStrings(ctx, 4, data)
		if w.err != nil {
			t.Fatalf("scan error: %v", w.err)
		}
		if !bytes.Contains(data, []byte("BI")) && bytes.Contains(data, []byte(nestedWord)) && len(w.hits) == 0 {
			t.Fatal("a token in bytes holding no inline image was not found")
		}
	})
}
