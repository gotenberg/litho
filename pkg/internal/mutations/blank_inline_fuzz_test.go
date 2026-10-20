package mutations

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func FuzzInlineImageKeyword(f *testing.F) {
	f.Add([]byte(" BI /W 1 /H 1 /CS /G /BPC 8 ID \x07\nEI"))
	f.Add([]byte(" [ BI ] BI /W 1"))
	f.Fuzz(func(_ *testing.T, span []byte) {
		if start, ok := inlineImageKeyword(span); ok && (start < 0 || start+len("BI") > len(span)) {
			panic("keyword offset outside the span")
		}
	})
}

func FuzzLocateInlineImage(f *testing.F) {
	f.Add([]byte("q BI /W 1 /H 1 /CS /G /BPC 8 ID \x07\nEI Q"))
	f.Add([]byte("q BI /W 1 /H 1 /CS /G /BPC 8 ID \x07"))
	f.Fuzz(func(_ *testing.T, data []byte) {
		ctx := context.Background()
		ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.SplitGluedOperators())
		if err != nil {
			return
		}
		for i, op := range ops {
			if op.Operator != "BI" || len(op.Operands) != 1 {
				continue
			}
			dict, _ := op.Operands[0].(ast.PdfDict)
			loc, err := locateInlineImage(ctx, data, ranges[i], dict, content.NewInlineImageEnds(data))
			if err == nil && (loc.start < 0 || loc.start > loc.end || loc.end > len(data)) {
				panic("located bytes outside the content")
			}
		}
	})
}
