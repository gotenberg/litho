package document

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// objStmPDF builds an xref-stream PDF whose object stream 8 holds the objects
// members, in header order, with /First at the end of header. The catalog and
// page tree are objects 1 and 2, uncompressed, and the cross-reference stream
// is object 9, so members must avoid 1, 2, 8 and 9 and stay below 16.
func objStmPDF(t *testing.T, header, body string, members ...int) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")
	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	var packed bytes.Buffer
	zw := zlib.NewWriter(&packed)
	if _, err := zw.Write([]byte(header + body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	off8 := buf.Len()
	fmt.Fprintf(&buf, "8 0 obj\n<< /Type /ObjStm /N %d /First %d /Filter /FlateDecode /Length %d >>\nstream\n",
		len(members), len(header), packed.Len())
	buf.Write(packed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	const size, entry = 16, 4 // /W [1 2 1]
	table := make([]byte, size*entry)
	put := func(num, typ, f2, f3 int) {
		table[num*entry] = byte(typ)
		binary.BigEndian.PutUint16(table[num*entry+1:], uint16(f2)) //nolint:gosec // test offsets fit in 16 bits.
		table[num*entry+3] = byte(f3)
	}
	off9 := buf.Len()
	put(1, 1, off1, 0)
	put(2, 1, off2, 0)
	put(8, 1, off8, 0)
	put(9, 1, off9, 0)
	for i, num := range members {
		put(num, 2, 8, i)
	}
	fmt.Fprintf(&buf, "9 0 obj\n<< /Type /XRef /Size %d /Root 1 0 R /W [1 2 1] /Length %d >>\nstream\n", size, len(table))
	buf.Write(table)
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", off9)
	return buf.Bytes()
}

// resolveCompressed opens data and resolves each of nums.
func resolveCompressed(t *testing.T, data []byte, nums ...int) map[int]ast.PdfObject {
	t.Helper()
	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.Rebuilt {
		t.Fatal("the test file's cross-reference stream did not parse")
	}
	got := make(map[int]ast.PdfObject, len(nums))
	for _, num := range nums {
		obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: num})
		if err != nil {
			t.Fatalf("Resolve(%d): %v", num, err)
		}
		got[num] = obj
	}
	return got
}

// TestResolveCompressed_StopsObjectAtNextOffset pins that a compressed object
// whose literal string never closes before the next object stops there, as an
// uncompressed one stops at the next object the table places: the dictionary
// keeps the pairs before the string, and object 4's /B is not read as a value
// of object 3.
func TestResolveCompressed_StopsObjectAtNextOffset(t *testing.T) {
	t.Parallel()

	obj3 := "<< /Width 4 /Junk (\x80binary "
	body := obj3 + "<< /A (x)) /B 2 >>"
	header := fmt.Sprintf("3 0 4 %d ", len(obj3))
	got := resolveCompressed(t, objStmPDF(t, header, body, 3, 4), 3, 4)

	dict, ok := got[3].(ast.PdfDict)
	if !ok {
		t.Fatalf("object 3 = %T, want ast.PdfDict", got[3])
	}
	if dict["Width"] != ast.PdfInt(4) {
		t.Errorf("object 3 lost the pair before its string: %v", dict)
	}
	if b, found := dict["B"]; found {
		t.Errorf("object 3 took object 4's /B = %v", b)
	}
	if _, ok := got[4].(ast.PdfDict); !ok {
		t.Errorf("object 4 = %T, want ast.PdfDict", got[4])
	}
}

// TestResolveCompressed_AgreesWithDecodeObjectStream pins that resolving a
// compressed object reads it as [Document.DecodeObjectStream] does, within its
// own bytes. Object 1 of the stream is declared to span "[1 [", so the array
// the unbounded bytes would give it is not what it holds.
func TestResolveCompressed_AgreesWithDecodeObjectStream(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, header, body string
		members            []int
	}{
		{"ends at the next offset", "3 0 4 4 ", "[1 [2] 3]", []int{3, 4}},
		{"header out of order", "5 22 3 0 4 11 ", "<< /A 1 >> << /B 2 >> [/C 3]", []int{5, 3, 4}},
		{"last object runs to the end", "3 0 4 11 ", "<< /A 1 >> << /B 2 ", []int{3, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			members := tc.members
			got := resolveCompressed(t, objStmPDF(t, tc.header, tc.body, members...), members...)

			stream := plainObjStm(len(members), tc.header, tc.body)
			want := decodeNums(t, stream)
			for _, num := range members {
				if w, decoded := want[num]; decoded && !reflect.DeepEqual(got[num], w) {
					t.Errorf("object %d resolves to %v, DecodeObjectStream reads %v", num, got[num], w)
				}
			}
		})
	}

	got := resolveCompressed(t, objStmPDF(t, "3 0 4 4 ", "[1 [2] 3]", 3, 4), 3, 4)
	if reflect.DeepEqual(got[3], ast.PdfArray{ast.PdfInt(1), ast.PdfArray{ast.PdfInt(2)}, ast.PdfInt(3)}) {
		t.Errorf("object 3 = %v: it was read past the offset where object 4 begins", got[3])
	}
	if got[4] != ast.PdfInt(2) {
		t.Errorf("object 4 = %v, want 2", got[4])
	}
}

// TestResolveCompressed_RefusesFirstOutsideTheStream pins that an object
// stream whose /First lies outside its decoded bytes yields no object: ISO
// 32000-1, 7.5.7, Table 16 places the first object there. The decoded bytes
// are "3 5 7", and /First -3 would put object 3 at the header's own 5.
func TestResolveCompressed_RefusesFirstOutsideTheStream(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := objStmPDF(t, "3 5 ", "7", 3)
	data = bytes.Replace(data, []byte("/First 4 "), []byte("/First -3"), 1)
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 3}); err == nil {
		t.Errorf("Resolve(3) = %v, want an error for /First -3", obj)
	}
}
