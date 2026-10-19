package remediation

import (
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestFixContentTag_EndsBeforeADanglingString pins that the EMC closing the
// wrapper is read as an operator on a page whose content ends inside a string
// it never terminates. The string runs to the end of the stream (ISO 32000-1,
// 7.3.4.2 and 7.3.4.3), so an EMC appended after the last byte was string
// bytes and left the /Document sequence open, which 14.6 makes no
// marked-content sequence (ISO 14289-1, 7.1 t3).
func TestFixContentTag_EndsBeforeADanglingString(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for name, tail := range map[string]string{
		"literal string": "(dangling ( string",
		"hex string":     "<0A1B",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			page := "0 0 m 10 10 l S\n" + tail
			doc, alloc := newTestDoc(t, buildStructPDF(
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R >>",
				"<< /Type /StructTreeRoot /K 5 0 R >>",
				"<< /Type /StructElem /S /Document /P 4 0 R >>",
				"<< /Length "+strconv.Itoa(len(page))+" >>\nstream\n"+page+"\nendstream",
			))
			if _, err := fixContentTag(ctx, alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
				t.Fatalf("fixContentTag: %v", err)
			}
			contentsRef, ok := pendingDict(t, alloc, 3)["Contents"].(ast.PdfIndirectRef)
			if !ok {
				t.Fatal("page /Contents is not a reference to the wrapped stream")
			}
			ops, err := content.ParseStreamData(ctx, pendingStreamBytes(t, alloc, contentsRef.ObjectNum))
			if err != nil {
				t.Fatalf("parse wrapped content: %v", err)
			}
			var got []string
			for _, op := range ops {
				got = append(got, op.Operator)
			}
			want := []string{"BDC", "m", "l", "S", "EMC"}
			if len(got) != len(want) {
				t.Fatalf("wrapped content reads %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("wrapped content reads %v, want %v", got, want)
				}
			}
		})
	}
}
