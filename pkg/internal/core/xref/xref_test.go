package xref

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestReadInt(t *testing.T) {
	tests := []struct {
		data  []byte
		width int
		want  int64
	}{
		{[]byte{0x00}, 1, 0},
		{[]byte{0x01}, 1, 1},
		{[]byte{0xFF}, 1, 255},
		{[]byte{0x01, 0x00}, 2, 256},
		{[]byte{0x00, 0x01, 0x00}, 3, 256},
		{[]byte{}, 0, 0},
	}
	for _, tc := range tests {
		got := readInt(tc.data, tc.width)
		if got != tc.want {
			t.Errorf("readInt(%v, %d) = %d, want %d", tc.data, tc.width, got, tc.want)
		}
	}
}

func TestParseXRefStream_Basic(t *testing.T) {
	// Build a minimal xref stream with 3 entries:
	// obj 0: free (type=0, next=0, gen=65535)
	// obj 1: normal (type=1, offset=100, gen=0)
	// obj 2: compressed (type=2, objstm=1, index=0)

	// W = [1 2 1] -> entrySize=4
	var data []byte
	// Entry 0: type=0, offset=0x0000, gen=0xFF (free)
	data = append(data, 0x00, 0x00, 0x00, 0xFF)
	// Entry 1: type=1, offset=0x0064 (100), gen=0x00
	data = append(data, 0x01, 0x00, 0x64, 0x00)
	// Entry 2: type=2, objstm=0x0001, index=0x00
	data = append(data, 0x02, 0x00, 0x01, 0x00)

	stream := ast.NewStream(ast.PdfDict{
		"W":    ast.PdfArray{ast.PdfInt(1), ast.PdfInt(2), ast.PdfInt(1)},
		"Size": ast.PdfInt(3),
	}, data)

	indexMap := make(map[int]XRefEntry)
	prev, err := ParseXRefStream(context.Background(), stream, indexMap)
	if err != nil {
		t.Fatal(err)
	}
	if prev != 0 {
		t.Errorf("prev = %d, want 0", prev)
	}

	if len(indexMap) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(indexMap))
	}

	// Check entry 0 (free).
	e0 := indexMap[0]
	if !e0.Free || e0.Type != 0 {
		t.Errorf("entry 0: Free=%v, Type=%d; want Free=true, Type=0", e0.Free, e0.Type)
	}

	// Check entry 1 (normal at offset 100).
	e1 := indexMap[1]
	if e1.Free || e1.Type != 1 || e1.Offset != 100 {
		t.Errorf("entry 1: Free=%v, Type=%d, Offset=%d; want Free=false, Type=1, Offset=100", e1.Free, e1.Type, e1.Offset)
	}

	// Check entry 2 (compressed in obj stream 1).
	e2 := indexMap[2]
	if e2.Free || e2.Type != 2 || e2.ObjStmNum != 1 || e2.ObjStmIndex != 0 {
		t.Errorf("entry 2: Free=%v, Type=%d, ObjStm=%d, Idx=%d", e2.Free, e2.Type, e2.ObjStmNum, e2.ObjStmIndex)
	}
}

func TestParseXRefStream_WithPrev(t *testing.T) {
	// Single entry, with /Prev pointing to offset 500.
	data := []byte{0x01, 0x00, 0x0A, 0x00}
	stream := ast.NewStream(ast.PdfDict{
		"W":    ast.PdfArray{ast.PdfInt(1), ast.PdfInt(2), ast.PdfInt(1)},
		"Size": ast.PdfInt(1),
		"Prev": ast.PdfInt(500),
	}, data)

	indexMap := make(map[int]XRefEntry)
	prev, err := ParseXRefStream(context.Background(), stream, indexMap)
	if err != nil {
		t.Fatal(err)
	}
	if prev != 500 {
		t.Errorf("prev = %d, want 500", prev)
	}
}

func TestParseXRefStream_WithIndex(t *testing.T) {
	// Index=[5 2] means objects 5 and 6.
	data := []byte{
		0x01, 0x00, 0x20, 0x00, // obj 5: type=1, offset=32
		0x01, 0x00, 0x40, 0x00, // obj 6: type=1, offset=64
	}
	stream := ast.NewStream(ast.PdfDict{
		"W":     ast.PdfArray{ast.PdfInt(1), ast.PdfInt(2), ast.PdfInt(1)},
		"Size":  ast.PdfInt(7),
		"Index": ast.PdfArray{ast.PdfInt(5), ast.PdfInt(2)},
	}, data)

	indexMap := make(map[int]XRefEntry)
	_, err := ParseXRefStream(context.Background(), stream, indexMap)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := indexMap[5]; !ok {
		t.Error("expected entry for object 5")
	}
	if _, ok := indexMap[6]; !ok {
		t.Error("expected entry for object 6")
	}
	if indexMap[5].Offset != 32 {
		t.Errorf("obj 5 offset = %d, want 32", indexMap[5].Offset)
	}
	if indexMap[6].Offset != 64 {
		t.Errorf("obj 6 offset = %d, want 64", indexMap[6].Offset)
	}
}

func TestParseXRefStream_NoOverwrite(t *testing.T) {
	// If an entry already exists, it should not be overwritten.
	data := []byte{0x01, 0x00, 0xFF, 0x00}
	stream := ast.NewStream(ast.PdfDict{
		"W":    ast.PdfArray{ast.PdfInt(1), ast.PdfInt(2), ast.PdfInt(1)},
		"Size": ast.PdfInt(1),
	}, data)

	indexMap := map[int]XRefEntry{
		0: {Offset: 999, Type: 1},
	}
	_, err := ParseXRefStream(context.Background(), stream, indexMap)
	if err != nil {
		t.Fatal(err)
	}
	if indexMap[0].Offset != 999 {
		t.Errorf("existing entry was overwritten: offset = %d, want 999", indexMap[0].Offset)
	}
}

func TestParseXRefStream_MissingW(t *testing.T) {
	stream := ast.NewStream(ast.PdfDict{
		"Size": ast.PdfInt(1),
	}, []byte{0x01, 0x00, 0x0A, 0x00})
	_, err := ParseXRefStream(context.Background(), stream, make(map[int]XRefEntry))
	if err == nil {
		t.Error("expected error for missing /W")
	}
}

func TestParseXRefStream_MissingSize(t *testing.T) {
	stream := ast.NewStream(ast.PdfDict{
		"W": ast.PdfArray{ast.PdfInt(1), ast.PdfInt(2), ast.PdfInt(1)},
	}, []byte{})
	_, err := ParseXRefStream(context.Background(), stream, make(map[int]XRefEntry))
	if err == nil {
		t.Error("expected error for missing /Size")
	}
}
