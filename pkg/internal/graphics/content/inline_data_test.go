package content

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestInlineImageData pins the data the BI operation carries under
// [InlineImageData]: the bytes between the white space after ID and the white
// space before EI, a data byte that is white space included, and nothing
// without the option.
func TestInlineImageData(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, stream string
		want         []byte
	}{
		{"plain", "q BI /W 2 /H 1 /CS /G /BPC 8 ID \x00\xff EI Q", []byte{0x00, 0xff}},
		{"data ending in white space", "BI /W 4 /H 1 /CS /G /BPC 8 ID \x00\xff)\r EI Q", []byte("\x00\xff)\r")},
		{"line feeds", "BI /W 1 /H 1 /CS /G /BPC 8 ID\nA\nEI\nQ", []byte("A")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, opts := range [][]Option{{InlineImageData()}, {InlineImageData(), SplitGluedOperators()}, {InlineImageData(), WholeStream()}} {
				ops, err := ParseStreamData(context.Background(), []byte(tc.stream), opts...)
				if err != nil {
					t.Fatal(err)
				}
				var bi *Operation
				for i := range ops {
					if ops[i].Operator == "BI" {
						bi = &ops[i]
					}
				}
				if bi == nil || len(bi.Operands) != 2 {
					t.Fatalf("BI operation %v, want a dictionary and the data", bi)
				}
				if got, _ := bi.Operands[1].(ast.PdfString); !bytes.Equal([]byte(got), tc.want) {
					t.Errorf("data %q, want %q", got, tc.want)
				}
			}
			ops, err := ParseStreamData(context.Background(), []byte(tc.stream), SplitGluedOperators())
			if err != nil {
				t.Fatal(err)
			}
			for _, op := range ops {
				if op.Operator == "BI" && len(op.Operands) != 1 {
					t.Errorf("without the option BI carries %d operands, want the dictionary alone", len(op.Operands))
				}
			}
		})
	}
}

// FuzzInlineImageData checks that the data an inline image carries is always
// a span of the stream and that keeping it changes no operation.
func FuzzInlineImageData(f *testing.F) {
	f.Add([]byte("q BI /W 2 /H 1 /CS /G /BPC 8 ID \x00\xff EI Q"))
	f.Add([]byte("BI /W 2 /H 1 /CS /G /BPC 8 ID \x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			return
		}
		for _, base := range [][]Option{{SplitGluedOperators()}, nil, {WholeStream()}} {
			plain, perr := ParseStreamData(context.Background(), data, base...)
			kept, kerr := ParseStreamData(context.Background(), data, append([]Option{InlineImageData()}, base...)...)
			if (perr == nil) != (kerr == nil) || len(plain) != len(kept) {
				t.Fatalf("keeping inline data changed the parse: %d ops (%v) vs %d (%v)", len(plain), perr, len(kept), kerr)
			}
			for _, op := range kept {
				if op.Operator != "BI" {
					continue
				}
				if len(op.Operands) != 2 {
					t.Fatalf("BI carries %d operands", len(op.Operands))
				}
				d, _ := op.Operands[1].(ast.PdfString)
				if !bytes.Contains(data, []byte(d)) {
					t.Fatalf("inline data %q is no span of the stream", d)
				}
			}
		}
	})
}
