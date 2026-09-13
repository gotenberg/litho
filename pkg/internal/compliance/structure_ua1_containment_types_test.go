package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestUA1Containment_ByStandardType pins that the ISO 14289-1 §7.2 containment
// rules read the element, its parent and its kids by standard type, as
// veraPDF's SE objects, parentStandardType and kidsStandardTypes do.
//
// govdocs thread_000_000150.pdf maps LI_Label to Lbl and LI_Title to LBody;
// comparing the names as written reported §7.2 t20 on every list item. A Div,
// Part, or NonStruct is looked through on both sides: its kids count as kids of
// the element around it.
func TestUA1Containment_ByStandardType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		roleMap string
		k       string
		rule    Rule
		fires   bool
	}{
		{"role-mapped list item kids", "/RoleMap << /LI_Label /Lbl /LI_Title /LBody >>",
			"[<< /S /L /K [<< /S /LI /K [<< /S /LI_Label >> << /S /LI_Title >>] >>] >>]", Rule_14289_1_7_2_t20, false},
		{"a Span in a list item", "",
			"[<< /S /L /K [<< /S /LI /K [<< /S /Span >>] >>] >>]", Rule_14289_1_7_2_t20, true},
		{"list item kids wrapped in a Div", "",
			"[<< /S /L /K [<< /S /LI /K [<< /S /Div /K [<< /S /Lbl >> << /S /LBody >>] >>] >>] >>]", Rule_14289_1_7_2_t20, false},
		{"a list item wrapped in a Div", "",
			"[<< /S /L /K [<< /S /Div /K [<< /S /LI /K [<< /S /Lbl >>] >>] >>] >>]", Rule_14289_1_7_2_t17, false},
		{"two role-mapped table heads", "/RoleMap << /MyHead /THead >>",
			"[<< /S /Table /K [<< /S /MyHead >> << /S /MyHead >> << /S /TBody >>] >>]", Rule_14289_1_7_2_t11, true},
		{"a role-mapped cell in a row", "/RoleMap << /MyCell /TD >>",
			"[<< /S /Table /K [<< /S /TR /K [<< /S /MyCell >>] >>] >>]", Rule_14289_1_7_2_t10, false},
		// veraPDF gives a child of the structure tree root no parent type, and
		// stops its climb through pass-through parents at the root, where it
		// reports the last one's type: on one-element fixtures it fails §7.2 t17
		// for a list item at the root and for one under a Part at the root.
		{"a list item at the root", "",
			"[<< /S /LI /K [<< /S /Lbl >>] >>]", Rule_14289_1_7_2_t17, true},
		{"a list item under a Part at the root", "",
			"[<< /S /Part /K [<< /S /LI /K [<< /S /Lbl >>] >>] >>]", Rule_14289_1_7_2_t17, true},
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
			if fired := hasRule(results, tt.rule); fired != tt.fires {
				t.Errorf("%s fired = %v, want %v (%v)", tt.rule.Clause, fired, tt.fires, results)
			}
		})
	}
}
