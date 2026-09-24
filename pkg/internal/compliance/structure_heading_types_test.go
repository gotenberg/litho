package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestHeadingNesting_ByStandardType pins that ISO 14289-1 §7.4.2 t1 reads a
// heading's level from its standard type, as veraPDF's SEHn object does.
//
// # Corpus evidence
//
//   - govdocs thread_000_000838.pdf: maps "Heading 1" to "Heading 5" onto H1
//     to H5 through its role map and skips levels; veraPDF reports each skip,
//     while litho read the names as written and found no heading at all.
func TestHeadingNesting_ByStandardType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		roleMap string
		k       string
		fires   bool
	}{
		{
			"a role-mapped H2 opening the document", "/RoleMap << /Heading#202 /H2 >>",
			"[<< /S /Heading#202 >>]", true,
		},
		{
			"role-mapped headings in order", "/RoleMap << /Heading#201 /H1 /Heading#202 /H2 >>",
			"[<< /S /Heading#201 >> << /S /Heading#202 >>]", false,
		},
		{
			"an H3 opening the document", "",
			"[<< /S /H3 >>]", true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /StructTreeRoot "+tt.roleMap+" /K "+tt.k+" >>",
			)
			root, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve root: %v", err)
			}
			var results []Violation
			walkStructureTree(ctx, doc, root, "PDF/UA-1", &results)
			if fired := hasRule(results, Rule_14289_1_7_4_2_t01); fired != tt.fires {
				t.Errorf("7.4.2 t1 fired = %v, want %v (%v)", fired, tt.fires, results)
			}
		})
	}
}
