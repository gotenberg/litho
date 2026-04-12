package writer

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestWriteXRefTable_Basic(t *testing.T) {
	records := []XRefRecord{
		{ObjNum: 0, Offset: 0, Generation: 65535, Free: true},
		{ObjNum: 1, Offset: 9, Generation: 0},
		{ObjNum: 2, Offset: 58, Generation: 0},
	}
	trailer := ast.PdfDict{
		"Size": ast.PdfInt(3),
		"Root": ast.PdfIndirectRef{ObjectNum: 1, GenNum: 0},
	}

	var buf bytes.Buffer
	_, _, err := WriteXRefTable(context.Background(), &buf, records, trailer)
	if err != nil {
		t.Fatalf("WriteXRefTable: %v", err)
	}

	out := buf.String()

	// Check structure.
	if !strings.HasPrefix(out, "xref\n") {
		t.Error("missing 'xref' header")
	}
	if !strings.Contains(out, "0 3\n") {
		t.Error("missing subsection header '0 3'")
	}
	if !strings.Contains(out, "0000000000 65535 f \r\n") {
		t.Error("missing free entry for object 0")
	}
	if !strings.Contains(out, "0000000009 00000 n \r\n") {
		t.Error("missing entry for object 1")
	}
	if !strings.Contains(out, "trailer\n") {
		t.Error("missing 'trailer' keyword")
	}
	if !strings.Contains(out, "/Root") {
		t.Error("missing /Root in trailer")
	}
}

func TestWriteXRefTable_NonConsecutive(t *testing.T) {
	// Objects 1 and 5, should produce two subsections.
	records := []XRefRecord{
		{ObjNum: 1, Offset: 10, Generation: 0},
		{ObjNum: 5, Offset: 200, Generation: 0},
	}
	trailer := ast.PdfDict{"Size": ast.PdfInt(6)}

	var buf bytes.Buffer
	_, _, err := WriteXRefTable(context.Background(), &buf, records, trailer)
	if err != nil {
		t.Fatalf("WriteXRefTable: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "1 1\n") {
		t.Error("missing subsection '1 1'")
	}
	if !strings.Contains(out, "5 1\n") {
		t.Error("missing subsection '5 1'")
	}
}

func TestWriteXRefStream_Basic(t *testing.T) {
	records := []XRefRecord{
		{ObjNum: 0, Offset: 0, Generation: 65535, Free: true},
		{ObjNum: 1, Offset: 9, Generation: 0},
		{ObjNum: 2, Offset: 58, Generation: 0},
	}
	trailer := ast.PdfDict{
		"Root": ast.PdfIndirectRef{ObjectNum: 1, GenNum: 0},
	}
	allocator := NewObjAllocator(2)

	var buf bytes.Buffer
	_, err := WriteXRefStream(context.Background(), &buf, records, trailer, allocator)
	if err != nil {
		t.Fatalf("WriteXRefStream: %v", err)
	}

	out := buf.String()
	// The xref stream is written as an indirect object.
	if !strings.Contains(out, "obj\n") {
		t.Error("missing indirect object header")
	}
	if !strings.Contains(out, "/Type /XRef") {
		t.Error("missing /Type /XRef")
	}
	if !strings.Contains(out, "/W") {
		t.Error("missing /W array")
	}
	if !strings.Contains(out, "/Filter /FlateDecode") {
		t.Error("missing /Filter")
	}
	if !strings.Contains(out, "stream\r\n") {
		t.Error("missing stream keyword")
	}
	if !strings.Contains(out, "/Root") {
		t.Error("missing /Root in xref stream dict")
	}
}

func TestBytesNeeded(t *testing.T) {
	tests := []struct {
		val  int64
		want int
	}{
		{0, 1},
		{1, 1},
		{255, 1},
		{256, 2},
		{65535, 2},
		{65536, 3},
		{16777215, 3},
		{16777216, 4},
	}
	for _, tc := range tests {
		got := bytesNeeded(tc.val)
		if got != tc.want {
			t.Errorf("bytesNeeded(%d): got %d, want %d", tc.val, got, tc.want)
		}
	}
}

func TestAppendBigEndian(t *testing.T) {
	// 258 = 0x0102 in 2 bytes big-endian.
	got := appendBigEndian(nil, 258, 2)
	if len(got) != 2 || got[0] != 0x01 || got[1] != 0x02 {
		t.Errorf("appendBigEndian(258, 2): got %v", got)
	}

	// 0 in 1 byte.
	got = appendBigEndian(nil, 0, 1)
	if len(got) != 1 || got[0] != 0x00 {
		t.Errorf("appendBigEndian(0, 1): got %v", got)
	}
}
