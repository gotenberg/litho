package document

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// FuzzDecodeObjectStream feeds arbitrary decoded bytes, /N and /First to the
// object-stream reader. All three come from the file, so it must never panic,
// and every object it returns must carry a number the header could hold.
func FuzzDecodeObjectStream(f *testing.F) {
	f.Add([]byte("3 0 6 11 7 22 << /A 1 >> << /B 2 >> [/C 3]"), 3, 14)
	f.Add([]byte("3 0 6 11 7 22 << /A 1 >> << /B"), 3, 14)
	f.Add([]byte("1 0 2 4 [1 [2] 3]"), 2, 8)
	f.Add([]byte(""), 1, 0)

	doc := &Document{Logger: NopLogger()}
	f.Fuzz(func(t *testing.T, data []byte, n, first int) {
		stream := ast.NewStream(ast.PdfDict{
			"Type":  ast.PdfName("ObjStm"),
			"N":     ast.PdfInt(n),
			"First": ast.PdfInt(first),
		}, data)
		objs, err := doc.DecodeObjectStream(context.Background(), 8, stream)
		if err != nil {
			return
		}
		for _, o := range objs {
			if o.Num < 0 || o.Object == nil {
				t.Fatalf("returned object %d = %v", o.Num, o.Object)
			}
		}
	})
}
