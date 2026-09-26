package document

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// keyOrderPDF builds a document whose object 3 is written uncompressed and
// object 6 inside the object stream 5, each a dictionary with its keys in a
// known order, and object 7 an uncompressed array. The index is a
// cross-reference stream, object 4, written without a filter.
func keyOrderPDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")
	offsets := map[int]int{}
	obj := func(n int, body string) {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	obj(2, "<< /Type /Pages /Kids [] /Count 0 >>")
	obj(3, "<< /Zeta 1 /Alpha null /Mid [1 2 << /X 1 >>] /Alpha 2 /Beta 7 0 R /Zeta 3 >>")
	member := "<< /Yellow 1 /Blue (b) /Red << /Nested /Name >> >>"
	header := "6 0 "
	objStm := header + member
	obj(5, fmt.Sprintf("<< /Type /ObjStm /N 1 /First %d /Length %d >>\nstream\n%s\nendstream", len(header), len(objStm), objStm))
	obj(7, "[1 2 3]")

	// Cross-reference stream, W [1 4 2], one row for each of objects 0 to 7.
	var rows bytes.Buffer
	row := func(typ, f2, f3 int) {
		rows.WriteByte(byte(typ))
		rows.Write([]byte{byte(f2 >> 24), byte(f2 >> 16), byte(f2 >> 8), byte(f2)})
		rows.Write([]byte{byte(f3 >> 8), byte(f3)})
	}
	xrefOffset := buf.Len()
	row(0, 0, 0xFFFF)
	for n := 1; n <= 7; n++ {
		switch n {
		case 4:
			row(1, xrefOffset, 0)
		case 6:
			row(2, 5, 0)
		default:
			row(1, offsets[n], 0)
		}
	}
	fmt.Fprintf(&buf, "4 0 obj\n<< /Type /XRef /Size 8 /W [1 4 2] /Root 1 0 R /Length %d >>\nstream\n", rows.Len())
	buf.Write(rows.Bytes())
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefOffset)
	return buf.Bytes()
}

// TestDictKeyOrder pins that [Document.DictKeyOrder] gives the keys of a
// dictionary in the order the file writes them, each where it first appears
// with a value other than null, for an uncompressed object and for one held in
// an object stream, and nothing for an object that is no dictionary or that
// the index lacks.
func TestDictKeyOrder(t *testing.T) {
	t.Parallel()
	data := keyOrderPDF()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	ctx := context.Background()
	for _, tt := range []struct {
		objNum int
		want   []ast.PdfName
	}{
		{3, []ast.PdfName{"Zeta", "Mid", "Alpha", "Beta"}},
		{6, []ast.PdfName{"Yellow", "Blue", "Red"}},
		{7, nil},
		{99, nil},
	} {
		if got := doc.DictKeyOrder(ctx, tt.objNum); !slices.Equal(got, tt.want) {
			t.Errorf("DictKeyOrder(%d) = %v, want %v", tt.objNum, got, tt.want)
		}
	}
}
