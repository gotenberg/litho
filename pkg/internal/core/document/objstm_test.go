package document

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// buildMultiObjectObjStmPDF constructs an xref-stream PDF whose object stream 8
// holds three objects (3, 6, 7) at indices 0, 1, 2. It exercises the object
// stream offset table: extracting index k must return object k, not a neighbour.
func buildMultiObjectObjStmPDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")

	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	// Object stream 8: header "objNum offset" pairs, then the object data.
	// data offsets are relative to /First.
	data := "<< /A 1 >> << /B 2 >> << /C 3 >>"
	header := "3 0 6 11 7 22 " // obj 3 @0, obj 6 @11, obj 7 @22
	first := len(header)
	objStm := header + data

	var compObjStm bytes.Buffer
	zw := zlib.NewWriter(&compObjStm)
	_, _ = zw.Write([]byte(objStm))
	_ = zw.Close()

	off8 := buf.Len()
	buf.WriteString(fmt.Sprintf("8 0 obj\n<< /Type /ObjStm /N 3 /First %d /Filter /FlateDecode /Length %d >>\nstream\n", first, compObjStm.Len()))
	buf.Write(compObjStm.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	// XRef stream (object 9), W = [1 3 1].
	const entrySize = 5
	const numObjs = 10
	stream := make([]byte, numObjs*entrySize)
	writeEntry := func(idx, typ int, f2 int64, f3 int) {
		base := idx * entrySize
		stream[base] = byte(typ)
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(f2)) //nolint:gosec // test offsets fit
		copy(stream[base+1:base+4], b[1:4])
		stream[base+4] = byte(f3)
	}
	off9 := buf.Len() // the XRef stream starts right after object 8
	writeEntry(1, 1, int64(off1), 0)
	writeEntry(2, 1, int64(off2), 0)
	writeEntry(3, 2, 8, 0) // in ObjStm 8, index 0
	writeEntry(6, 2, 8, 1) // in ObjStm 8, index 1
	writeEntry(7, 2, 8, 2) // in ObjStm 8, index 2
	writeEntry(8, 1, int64(off8), 0)
	writeEntry(9, 1, int64(off9), 0)

	var compXRef bytes.Buffer
	zw2 := zlib.NewWriter(&compXRef)
	_, _ = zw2.Write(stream)
	_ = zw2.Close()

	buf.WriteString(fmt.Sprintf("9 0 obj\n<< /Type /XRef /Size %d /Root 1 0 R /W [1 3 1] /Filter /FlateDecode /Length %d >>\nstream\n", numObjs, compXRef.Len()))
	buf.Write(compXRef.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", off9))
	return buf.Bytes()
}

// TestObjectStream_MultiObjectExtraction verifies the object-stream offset
// table returns the correct object for each index, including on a repeated
// resolve that hits the decoded-stream cache. A wrong offset table would map an
// index to a neighbour's bytes.
func TestObjectStream_MultiObjectExtraction(t *testing.T) {
	data := buildMultiObjectObjStmPDF()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	want := map[int]struct {
		key ast.PdfName
		val ast.PdfInt
	}{
		3: {"A", 1},
		6: {"B", 2},
		7: {"C", 3},
	}

	// Resolve twice: the first pass builds the offset table, the second hits
	// the cache. Both must return the correct object per index.
	for pass := 0; pass < 2; pass++ {
		for objNum, exp := range want {
			obj, err := doc.Resolve(context.Background(), ast.PdfIndirectRef{ObjectNum: objNum})
			if err != nil {
				t.Fatalf("pass %d: resolve object %d: %v", pass, objNum, err)
			}
			dict, ok := obj.(ast.PdfDict)
			if !ok {
				t.Fatalf("pass %d: object %d: got %T, want PdfDict", pass, objNum, obj)
			}
			got, ok := dict[exp.key].(ast.PdfInt)
			if !ok || got != exp.val {
				t.Errorf("pass %d: object %d: got %v, want /%s %d", pass, objNum, dict, exp.key, exp.val)
			}
		}
	}
}

// TestObjStmData_SlotFor pins the recovery when a damaged xref points a
// compressed object at the wrong index: the object number is located in the
// header instead (qpdf override-compressed-object.pdf). The well-formed fast
// path trusts the index; an absent object number falls back to the index.
func TestObjStmData_SlotFor(t *testing.T) {
	o := &objStmData{objNums: []int{5, 8, 12}, offsets: []int{0, 10, 20}}
	cases := []struct {
		idx, num, want int
		note           string
	}{
		{1, 8, 1, "fast path: index matches object number"},
		{2, 8, 1, "recover: xref index wrong, find object 8 by number"},
		{99, 5, 0, "recover: out-of-range index, find object 5 by number"},
		{2, 404, 2, "fallback: unknown object number, trust in-range index"},
		{99, 404, -1, "not found: unknown object number and out-of-range index"},
	}
	for _, c := range cases {
		if got := o.slotFor(c.idx, c.num); got != c.want {
			t.Errorf("slotFor(%d, %d) = %d, want %d (%s)", c.idx, c.num, got, c.want, c.note)
		}
	}
}
