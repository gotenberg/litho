package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixFormTooltip_KeepsTheNestingFixersPendingWidgetWrite pins that the
// tooltip repair rewrites the merged field and widget dictionary annot-nesting
// queued, not the document's copy.
//
// Under PDF/UA-1 an untagged form trips both repairs on the same object:
// ISO 14289-1, 7.18.4 test 1 wants the widget inside a Form element, which
// annot-nesting supplies by giving it a /StructParent, and 7.18.1 test 3 wants
// a /TU on its field, which form-tooltip adds. walkFormTooltipFields read the
// field with document.ResolveDict and wrote it back whole, so the widget went
// out without the /StructParent that links it to its new Form element, and the
// element's object reference pointed at an annotation that no longer pointed
// back.
func TestFixFormTooltip_KeepsTheNestingFixersPendingWidgetWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /AcroForm << /Fields [6 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [] >> >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (field name) /Rect [0 0 10 10] /P 3 0 R >>",
	))

	const widgetObj = 6

	if _, err := fixAnnotNesting(ctx, alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixAnnotNesting: %v", err)
	}
	if _, has := pendingDict(t, alloc, widgetObj)["StructParent"]; !has {
		t.Fatalf("control: annot-nesting did not queue a /StructParent at object %d, so there is "+
			"no earlier edit to lose", widgetObj)
	}

	if _, err := fixFormTooltip(ctx, alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixFormTooltip: %v", err)
	}

	got := pendingDict(t, alloc, widgetObj)
	if tu := got["TU"]; tu != ast.PdfString("field name") {
		t.Fatalf("/TU is %v, want the field name: the tooltip repair did not run, so the check "+
			"below proves nothing", tu)
	}
	if _, has := got["StructParent"]; !has {
		t.Errorf("/StructParent is gone: walkFormTooltipFields rebuilt the widget from the "+
			"document and discarded annot-nesting's write; widget = %v", got)
	}
}
