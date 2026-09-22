package mutations

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestCollectRootFields_MarkupAnnotationTitleIsNotAField pins that /T on a
// markup annotation does not make it a form field.
//
// /T carries two unrelated meanings: the partial field name on a field
// dictionary (ISO 32000-1, Table 220), and the pop-up window title,
// conventionally the author, on a markup annotation (Table 170). Collecting on
// /T alone conflated them, so split and merge wrote an /AcroForm onto documents
// that have no form. Measured on pdfjs issue20062.pdf, whose only annotation is
// a /Polygon with /T (author) and which carries no /AcroForm, no /Widget and no
// /FT: merging it with itself produced /Fields naming both polygons.
//
// The existing control in acroform_test.go is a /Link, which carries no /T at
// all, so it never reached this branch.
func TestCollectRootFields_MarkupAnnotationTitleIsNotAField(t *testing.T) {
	for _, subtype := range []string{"Polygon", "FreeText", "Text", "Highlight", "Ink", "Square"} {
		t.Run(subtype, func(t *testing.T) {
			alloc := writer.NewObjAllocator(1)
			widget := alloc.Alloc(ast.PdfDict{"Subtype": ast.PdfName("Widget"), "FT": ast.PdfName("Tx")})
			markup := alloc.Alloc(ast.PdfDict{
				"Subtype":  ast.PdfName(subtype),
				"T":        ast.PdfString("author"),
				"Contents": ast.PdfString("a comment"),
			})

			got := collectRootFields(alloc, []ast.PdfObject{widgetPage(alloc, widget, markup)})

			for _, f := range got {
				if f == ast.PdfObject(markup) {
					t.Errorf("/%s annotation with /T was named as a form field", subtype)
				}
			}
			if len(got) != 1 || got[0] != ast.PdfObject(widget) {
				t.Errorf("/Fields = %v, want only the widget %v", got, widget)
			}
		})
	}
}

// TestCollectRootFields_MarkupOnlyPageYieldsNoFields pins the consequence that
// matters: a page holding nothing but such an annotation must produce no
// fields, which is what keeps rebuildAcroForm from inventing a form for a
// document that has none.
func TestCollectRootFields_MarkupOnlyPageYieldsNoFields(t *testing.T) {
	alloc := writer.NewObjAllocator(1)
	only := alloc.Alloc(ast.PdfDict{"Subtype": ast.PdfName("Polygon"), "T": ast.PdfString("author")})

	if got := collectRootFields(alloc, []ast.PdfObject{widgetPage(alloc, only)}); len(got) != 0 {
		t.Errorf("/Fields = %v for a page with only a markup annotation, want none", got)
	}
}

// TestCollectRootFields_NonTerminalFieldStillQualifies is the control against
// over-correcting the tests above.
//
// A non-terminal field node carries /T and /Kids and no /FT (its descendants
// inherit the type), and carries no /Subtype because it is not an annotation.
// Rejecting every /T-bearing dictionary would strand the widgets whose /Parent
// chain ends there, which is the regression the subtype check is scoped to
// avoid.
func TestCollectRootFields_NonTerminalFieldStillQualifies(t *testing.T) {
	alloc := writer.NewObjAllocator(1)

	root := alloc.Alloc(ast.PdfDict{"T": ast.PdfString("group")}) // no /FT, no /Subtype
	kid := alloc.Alloc(ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Parent": root})

	got := collectRootFields(alloc, []ast.PdfObject{widgetPage(alloc, kid)})
	if len(got) != 1 || got[0] != ast.PdfObject(root) {
		t.Fatalf("/Fields = %v, want the non-terminal root %v", got, root)
	}
}

// TestCollectRootFields_MergedWidgetWithoutFTStillQualifies pins the other case
// the subtype check must not break: the merged field-and-annotation of ISO
// 32000-1, 12.5.6.19 written without its required /FT.
func TestCollectRootFields_MergedWidgetWithoutFTStillQualifies(t *testing.T) {
	alloc := writer.NewObjAllocator(1)

	merged := alloc.Alloc(ast.PdfDict{"Subtype": ast.PdfName("Widget"), "T": ast.PdfString("name")})

	got := collectRootFields(alloc, []ast.PdfObject{widgetPage(alloc, merged)})
	if len(got) != 1 || got[0] != ast.PdfObject(merged) {
		t.Fatalf("/Fields = %v, want the merged field-widget %v", got, merged)
	}
}
