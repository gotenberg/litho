package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixRoleMapDropsAStandardTypeFromACycle pins that a standard structure
// type caught in a role-map cycle is dropped, never redirected.
//
// Breaking a self-mapping such as /LI -> /LI by pointing it at /P remaps a
// standard type, which ISO 14289-1, 7.1 test 7 forbids outright and litho
// itself reports, and it turns every list item into a paragraph.
//
// Nothing is left dangling by the drop: a chain aimed at /LI now ends on the
// standard name itself, which is where it should end.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-2__8.2.4-t02-fail-a: maps /LI to itself, and pointing it
//     at /P left the remediated file with <L> containing <P> and <P>
//     containing <LBody>, three findings veraPDF confirmed on litho's own
//     output.
func TestFixRoleMapDropsAStandardTypeFromACycle(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"PDF/UA-1", "PDF/UA-2"} {
		// Namespaced, so the later standard-source sweep does not run and the
		// cycle branch is the only thing that can act.
		data := roleMapPDF("<< /LI /LI >>", true)
		doc, alloc := newTestDoc(t, data)

		if _, err := fixRoleMap(context.Background(), alloc, doc, Options{}, nil, profile); err != nil {
			t.Fatalf("%s: fixRoleMap: %v", profile, err)
		}
		rm := resolvedRoleMap(t, alloc)
		if got, still := rm["LI"]; still {
			t.Errorf("%s: /LI is still mapped, to %v", profile, got)
		}
	}
}

// TestFixRoleMapStillRedirectsACustomCycle is the control: a custom type in a
// cycle keeps the redirect, because deleting it would leave any entry aimed at
// it pointing at a name that maps to nothing.
func TestFixRoleMapStillRedirectsACustomCycle(t *testing.T) {
	t.Parallel()

	data := roleMapPDF("<< /Foo /Bar /Bar /Foo /MyType /Bar >>", true)
	doc, alloc := newTestDoc(t, data)

	if _, err := fixRoleMap(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixRoleMap: %v", err)
	}
	rm := resolvedRoleMap(t, alloc)

	if rm["Foo"] == ast.PdfName("Bar") && rm["Bar"] == ast.PdfName("Foo") {
		t.Errorf("the custom cycle was not broken: %v", rm)
	}
	// Whichever edge was cut, both custom sources must still map somewhere so
	// /MyType -> /Bar does not dangle.
	for _, src := range []ast.PdfName{"Foo", "Bar"} {
		if _, ok := rm[src]; !ok {
			t.Errorf("/%s was dropped, leaving entries aimed at it unmapped: %v", src, rm)
		}
	}
}
