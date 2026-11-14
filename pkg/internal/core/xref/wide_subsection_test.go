package xref

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// TestParseXRefTable_SubsectionPastThirtyTwoBitsIsMalformed pins that a
// subsection whose start, count or last object number passes 2^31 - 1 is
// malformed where int is 32 bits and where it is 64: ISO 32000-1, Annex C,
// Table C.1 holds an object number to 32 bits, and an index that wraps where
// int is 32 bits would file the entries under negative numbers.
func TestParseXRefTable_SubsectionPastThirtyTwoBitsIsMalformed(t *testing.T) {
	t.Parallel()

	const entry = "0000000009 00000 n \n"
	for name, table := range map[string]string{
		"start":       "xref\n3000000000 1\n" + entry,
		"count":       "xref\n0 3000000000\n" + entry,
		"last object": "xref\n2147483647 2\n" + entry + entry,
		"generation":  "xref\n0 1\n0000000009 3000000000 n \n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := []byte(table + "trailer\n<< /Size 1 >>\n")
			lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
			entries := make(map[int]XRefEntry)
			_, _, _, err := ParseXRefTable(context.Background(), lex, entries)
			if !errors.Is(err, ErrMalformedXRef) {
				t.Fatalf("ParseXRefTable = %v, want ErrMalformedXRef", err)
			}
			for objNum := range entries {
				if objNum < 0 {
					t.Errorf("entry filed under object %d", objNum)
				}
			}
		})
	}

	// The last object number a subsection may reach is 2^31 - 1.
	data := []byte("xref\n2147483646 2\n" + entry + entry + "trailer\n<< /Size 1 >>\n")
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	entries := make(map[int]XRefEntry)
	if _, _, _, err := ParseXRefTable(context.Background(), lex, entries); err != nil {
		t.Fatalf("ParseXRefTable: %v", err)
	}
	if _, ok := entries[2147483647]; !ok || len(entries) != 2 {
		t.Errorf("entries = %v, want objects 2147483646 and 2147483647", entries)
	}
}
