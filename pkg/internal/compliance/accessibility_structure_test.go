package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestRoleMapChainReachesStandard(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		roleMap map[ast.PdfName]ast.PdfName
		typ     ast.PdfName
		want    bool
	}{
		{"direct standard", nil, "P", true},
		{"single hop to standard", map[ast.PdfName]ast.PdfName{"Standard": "P"}, "Standard", true},
		{"two hops to standard", map[ast.PdfName]ast.PdfName{"A": "B", "B": "Div"}, "A", true},
		{"unmapped non-standard", nil, "PDFDocument", false},
		{"maps to non-standard dead end", map[ast.PdfName]ast.PdfName{"X": "Y"}, "X", false},
		{"circular never reaches standard", map[ast.PdfName]ast.PdfName{"Q": "Q"}, "Q", false},
		{"circular pair", map[ast.PdfName]ast.PdfName{"A": "B", "B": "A"}, "A", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := roleMapChainReachesStandard(tc.roleMap, tc.typ, "PDF/UA-2"); got != tc.want {
				t.Errorf("roleMapChainReachesStandard(%v, %q) = %v, want %v", tc.roleMap, tc.typ, got, tc.want)
			}
		})
	}
}
