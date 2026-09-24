package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestUA1RootParentIsNoTypeName pins that the structure tree root the PDF/UA-1
// containment rules see is no structure type name. The walk once marked the
// root with the type string "\x00root", on the premise that no name holds a
// NUL, but litho's lexer decodes /#00root to exactly that string, as veraPDF and
// PDF.js do, although ISO 32000-1, 7.3.5 forbids the #00 escape.
//
// veraPDF reads /#00root as any other non-standard type, measured on a
// one-page fixture: an LI under a Div under a /#00root element fails ISO
// 14289-1, 7.2 t17 exactly as it does under /Foo, and veraPDF's climb through
// the Div stops at the /#00root element, which has no standard type, not at the
// root.
func TestUA1RootParentIsNoTypeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		top        string
		wantParent string
	}{
		{"a Div under a /#00root element", "/#00root", "LI inside /(none) instead of L"},
		{"a Div under a non-standard element", "/Foo", "LI inside /(none) instead of L"},
		{"a Div under a Div at the root", "/Div", "LI inside /Div instead of L"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /StructTreeRoot /K [3 0 R] >>",
				"<< /Type /StructElem /S "+tt.top+" /K [<< /S /Div /K [<< /S /LI /K [<< /S /Lbl >>] >>] >>] >>",
			)
			top, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
			if err != nil {
				t.Fatalf("resolve top element: %v", err)
			}
			if tt.top == "/#00root" && top["S"] != ast.PdfName("\x00root") {
				t.Fatalf("/S = %q, want the NUL-led name the lexer decodes /#00root to", top["S"])
			}
			root, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve root: %v", err)
			}
			var results []Violation
			walkStructureTree(ctx, doc, root, "PDF/UA-1", &results)
			var got []string
			for _, v := range results {
				if v.Rule == Rule_14289_1_7_2_t17 {
					got = append(got, v.Context)
				}
			}
			if len(got) != 1 || got[0] != tt.wantParent {
				t.Errorf("§7.2 t17 findings = %q, want [%q]", got, tt.wantParent)
			}
		})
	}

	// A parent whose type key is the former marker string is that type, not
	// the root.
	if got := ua1ParentType(ua1ParentRef{key: "\x00root"}); got != "\x00root" {
		t.Errorf("ua1ParentType(key %q) = %q, want the key itself", "\x00root", got)
	}
	if got := ua1ParentType(ua1ParentRef{root: true}); got != "(none)" {
		t.Errorf("ua1ParentType(root) = %q, want (none)", got)
	}
}
