package remediation

import (
	"bytes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestWriteOperand_DictIsOrdered pins that an inline content-stream dictionary
// serializes its keys in a stable order.
//
// CLAUDE.md's contract: identical input and options produce identical output
// bytes. Ranging the map instead emitted the same /Artifact dictionary in a
// different key order from one run to the next.
//
// # Corpus evidence
//
//   - pdfjs issue11678.pdf: ranging the map emitted an /Artifact as "/Attached
//     /Type /Subtype" on one run and "/Type /Subtype /Attached" on the next.
func TestWriteOperand_DictIsOrdered(t *testing.T) {
	dict := ast.PdfDict{
		"Type":     ast.PdfName("Pagination"),
		"Subtype":  ast.PdfName("Header"),
		"Attached": ast.PdfArray{ast.PdfName("Top")},
		"BBox":     ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(1)},
	}

	var first string
	for i := range 50 {
		var buf bytes.Buffer
		writeOperand(&buf, dict)
		got := buf.String()
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("run %d serialized the same dictionary differently:\n  %s\n  %s", i, first, got)
		}
	}

	want := "<< /Attached [/Top] /BBox [0 0 1 1] /Subtype /Header /Type /Pagination>>"
	if first != want {
		t.Errorf("serialized as %q, want key order %q", first, want)
	}
}

// TestSerializeOps_Stable pins the same property through the operation
// serializer the content fixers actually call.
func TestSerializeOps_Stable(t *testing.T) {
	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("Artifact"),
			ast.PdfDict{
				"Type":     ast.PdfName("Pagination"),
				"Subtype":  ast.PdfName("Header"),
				"Attached": ast.PdfArray{ast.PdfName("Top")},
			},
		}},
		{Operator: "EMC"},
	}

	first := serializeOps(ops)
	for range 50 {
		if got := serializeOps(ops); !bytes.Equal(got, first) {
			t.Fatalf("serializeOps is not stable:\n  %s\n  %s", first, got)
		}
	}
}
