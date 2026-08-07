package xref

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// TestParseXRefTable_MislabelledFirstSubsection pins the recovery for a
// subsection that declares its start as 1 while beginning with the free-list
// head.
//
// ISO 32000-1, 7.5.4 gives offset 0 with generation 65535 to object 0 and only
// object 0, so such a subsection has labelled every entry one too high: each
// object maps onto its predecessor's bytes and the real object 1 reads as free.
// pdfjs issue7229.pdf does this, and it cost page 1 its only image.
func TestParseXRefTable_MislabelledFirstSubsection(t *testing.T) {
	table := "xref\r1 4\r" +
		"0000000000 65535 f \r" +
		"0000000009 00000 n \r" +
		"0000000100 00000 n \r" +
		"0000000200 00000 n \r" +
		"trailer\r<< /Size 4 /Root 3 0 R >>\r"

	entries := make(map[int]XRefEntry)
	data := []byte(table)
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	if _, _, err := ParseXRefTable(context.Background(), lex, entries); err != nil {
		t.Fatalf("ParseXRefTable: %v", err)
	}

	if e, ok := entries[0]; !ok || !e.Free {
		t.Errorf("object 0 = %+v, want the free-list head", e)
	}
	for objNum, wantOffset := range map[int]int64{1: 9, 2: 100, 3: 200} {
		e, ok := entries[objNum]
		if !ok {
			t.Errorf("object %d missing from the index", objNum)
			continue
		}
		if e.Free {
			t.Errorf("object %d reads as free, want a live entry at %d", objNum, wantOffset)
		}
		if e.Offset != wantOffset {
			t.Errorf("object %d offset = %d, want %d", objNum, e.Offset, wantOffset)
		}
	}
}

// TestParseXRefTable_HonestSubsectionStart keeps the recovery narrow: a
// subsection that genuinely starts at object 1 and does not begin with the
// free-list head keeps its declared numbering.
func TestParseXRefTable_HonestSubsectionStart(t *testing.T) {
	table := "xref\r1 2\r" +
		"0000000009 00000 n \r" +
		"0000000100 00000 n \r" +
		"trailer\r<< /Size 3 /Root 1 0 R >>\r"

	entries := make(map[int]XRefEntry)
	data := []byte(table)
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	if _, _, err := ParseXRefTable(context.Background(), lex, entries); err != nil {
		t.Fatalf("ParseXRefTable: %v", err)
	}
	if _, ok := entries[0]; ok {
		t.Error("object 0 was invented for a subsection that does not describe it")
	}
	if e, ok := entries[1]; !ok || e.Offset != 9 {
		t.Errorf("object 1 = %+v, want offset 9", e)
	}
	if e, ok := entries[2]; !ok || e.Offset != 100 {
		t.Errorf("object 2 = %+v, want offset 100", e)
	}
}
