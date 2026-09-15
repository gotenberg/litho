package document

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// buildIntegerOverflowPDF constructs an xref-stream PDF whose catalog (object 1)
// holds an integer literal past the 64-bit range and whose object stream 4
// holds object 3, an array with another one.
func buildIntegerOverflowPDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")

	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Big 99999999999999999999 >>\nendobj\n")
	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	const header = "3 0 "
	objStm := header + "[-99999999999999999999 1]"
	off4 := buf.Len()
	fmt.Fprintf(&buf, "4 0 obj\n<< /Type /ObjStm /N 1 /First %d /Length %d >>\nstream\n%s\nendstream\nendobj\n",
		len(header), len(objStm), objStm)

	// XRef stream (object 5), W = [1 4 2], uncompressed.
	off5 := buf.Len()
	var xref bytes.Buffer
	for _, e := range [][3]int{{0, 0, 65535}, {1, off1, 0}, {1, off2, 0}, {2, 4, 0}, {1, off4, 0}, {1, off5, 0}} {
		xref.WriteByte(byte(e[0]))
		_ = binary.Write(&xref, binary.BigEndian, uint32(e[1])) //nolint:gosec // test offsets fit in 32 bits
		_ = binary.Write(&xref, binary.BigEndian, uint16(e[2])) //nolint:gosec // generations and indices fit in 16 bits
	}
	fmt.Fprintf(&buf, "5 0 obj\n<< /Type /XRef /Size 6 /Root 1 0 R /W [1 4 2] /Length %d >>\nstream\n", xref.Len())
	buf.Write(xref.Bytes())
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", off5)
	return buf.Bytes()
}

// TestIntegerOverflowsRecordedPerObject pins that resolving an object records
// the integer literals past the 64-bit range its parse read as reals, whether
// the object sits at a file offset or inside an object stream, so a PDF/A check
// can judge them as the integers ISO 32000-1, 7.3.3 says they are.
func TestIntegerOverflowsRecordedPerObject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := buildIntegerOverflowPDF()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	for _, tt := range []struct {
		objNum int
		want   []float64
	}{
		{1, []float64{1e20}},
		{2, nil},
		{3, []float64{-1e20}},
	} {
		if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: tt.objNum}); err != nil {
			t.Fatalf("resolve object %d: %v", tt.objNum, err)
		}
		if got := doc.IntegerOverflows(tt.objNum); !slices.Equal(got, tt.want) {
			t.Errorf("IntegerOverflows(%d) = %v, want %v", tt.objNum, got, tt.want)
		}
	}
}
