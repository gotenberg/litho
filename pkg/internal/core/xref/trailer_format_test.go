package xref

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// TestParseXRefTable_TrailerFormatViolations pins that the trailer dictionary
// is read under format checking: a hex string with an odd digit count in it
// is returned as a format violation naming the entry it stands in, and a
// clean trailer returns none.
func TestParseXRefTable_TrailerFormatViolations(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		trailer string
		want    int
	}{
		{"clean", "<< /Size 1 /Root 1 0 R /ID [<0011> <0011>] >>", 0},
		{"odd hex string in a direct /Info", "<< /Size 1 /Root 1 0 R /Info << /Producer <414> >> >>", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := []byte("xref\n0 1\n0000000000 65535 f \ntrailer\n" + tc.trailer + "\n")
			lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
			_, _, violations, err := ParseXRefTable(context.Background(), lex, make(map[int]XRefEntry))
			if err != nil {
				t.Fatalf("ParseXRefTable: %v", err)
			}
			if len(violations) != tc.want {
				t.Fatalf("got %d violations %v, want %d", len(violations), violations, tc.want)
			}
			for _, v := range violations {
				if !errors.Is(v, parser.ErrHexStringOddDigits) {
					t.Errorf("violation %v is not an odd hex string", v)
				}
				entry, ok := errors.AsType[*parser.EntryViolationError](v)
				if !ok || len(entry.Path) == 0 || entry.Path[0].Key != "Info" {
					t.Errorf("violation %v does not name the /Info entry", v)
				}
			}
		})
	}
}
