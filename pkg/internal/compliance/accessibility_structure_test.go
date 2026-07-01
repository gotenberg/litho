package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCheckNamespaceRoleMapping_LegacyRoleMapT04 covers ISO 14289-2 §8.2.4 t4 on
// a legacy /RoleMap that remaps the standard type /Document to the non-standard
// /Book. veraPDF keys the rule on the SENonStandard element: it fires only when a
// used element of the standard type resolves through the legacy /RoleMap, which
// happens only when that element has no explicit /NS. An element of the same type
// carrying an explicit /NS to a standard namespace resolves there instead,
// leaving the /RoleMap entry inert.
func TestCheckNamespaceRoleMapping_LegacyRoleMapT04(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(t *testing.T, docElem string) []Violation {
		t.Helper()
		doc, cat := tableDoc(t, 1,
			"<< /Type /Catalog /StructTreeRoot 2 0 R >>",
			"<< /Type /StructTreeRoot /RoleMap << /Document /Book >> /Namespaces [4 0 R] /K [3 0 R] >>",
			docElem,
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		)
		return checkNamespaceRoleMapping(ctx, doc, cat, "PDF/UA-2")
	}

	noNS := build(t, "<< /Type /StructElem /S /Document /P 2 0 R >>")
	if !hasRule(noNS, Rule_14289_2_8_2_4_t04) {
		t.Error("Document element without /NS, remapped to /Book: want §8.2.4 t4")
	}

	withNS := build(t, "<< /Type /StructElem /S /Document /NS 4 0 R /P 2 0 R >>")
	if hasRule(withNS, Rule_14289_2_8_2_4_t04) {
		t.Error("Document element with explicit standard /NS: want no §8.2.4 t4 (legacy /RoleMap inert)")
	}
}

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
