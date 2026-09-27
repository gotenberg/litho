package writer

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// FuzzHoistDirectStreams writes whatever object the parser reads from the
// input, once as an indirect object and once as a trailer value, and asserts
// the write never panics, never meets a stream it left inline, and produces a
// file holding none.
func FuzzHoistDirectStreams(f *testing.F) {
	f.Add([]byte("<< /A << /Length 3 >>\nstream\nabc\nendstream /B [1 << /Length 1 >>\nstream\nx\nendstream] >>"))
	f.Add([]byte("<< /A << /Length 3 >>\nstream\nab"))
	f.Add([]byte("[<< /Length 2 /DP << /Length 1 >>\nstream\nq\nendstream >>\nstream\nQ \nendstream]"))

	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := context.Background()
		p := parser.NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0)))
		obj, err := p.ParseObject(ctx)
		if err != nil {
			return
		}

		alloc := NewObjAllocator(0)
		held := alloc.Alloc(obj)
		catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog"), "Held": held})
		trailer := ast.PdfDict{"Root": catalog, "Held": obj}

		var out bytes.Buffer
		err = WriteRewrite(ctx, &out, RewriteConfig{Allocator: alloc, Trailer: trailer, PackObjectStreams: true})
		if errors.Is(err, ErrDirectStream) {
			t.Fatalf("the write met a stream the hoist left inline: %v", err)
		}
		if err != nil {
			// A stream whose data cannot be read, or a name the serializer
			// refuses, fails the write; neither is the hoist's to fix.
			return
		}
		if direct, _ := readBack(t, out.Bytes()); direct != 0 {
			t.Fatalf("the output holds %d streams inside another object", direct)
		}
	})
}
