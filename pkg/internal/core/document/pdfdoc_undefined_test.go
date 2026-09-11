package document

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestContainsUndefinedPDFDocBytes pins which strings hold a byte ISO 32000-1,
// Table D.2 leaves undefined. Only a string read as PDFDocEncoding can: a
// UTF-16 or UTF-8 string carries its own encoding, and [DecodePdfString] keeps
// every code unit of it.
func TestContainsUndefinedPDFDocBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"plain text", "EN-US", false},
		{"trailing NUL", "EN-US\x00", true},
		{"embedded control byte", "EN\x05US", true},
		{"DEL", "EN-US\x7f", true},
		{"tab and newline are defined", "EN\t-\nUS", false},
		{"high bytes are defined", "caf\xe9", false},
		{"UTF-16BE with a NUL code unit", "\xfe\xff\x00E\x00N\x00\x00", false},
		{"UTF-16LE", "\xff\xfeE\x00N\x00", false},
		{"UTF-8 with a byte order mark", "\xef\xbb\xbfEN\x00", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ContainsUndefinedPDFDocBytes(ast.PdfString(tt.s)); got != tt.want {
				t.Errorf("ContainsUndefinedPDFDocBytes(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}
