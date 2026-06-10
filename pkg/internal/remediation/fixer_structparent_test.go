package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// structTreePDF builds a tagged single-page PDF whose catalog points at a
// StructTreeRoot (object 4). The caller supplies the StructTreeRoot body and any
// structure-element bodies (objects 5+).
func structTreePDF(structTreeRoot string, elems ...string) []byte {
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		structTreeRoot,
	}
	bodies = append(bodies, elems...)
	return buildStructPDF(bodies...)
}

func TestMatchStructParent(t *testing.T) {
	t.Parallel()
	for _, rule := range []compliance.Rule{
		compliance.Rule_14289_1_7_1_t12,
		compliance.Rule_14289_2_8_2_1_t02,
	} {
		if !matchStructParent(compliance.Violation{Rule: rule}) {
			t.Errorf("must match /P rule %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
	// Must not claim an unrelated rule.
	if matchStructParent(compliance.Violation{Rule: compliance.Rule_19005_1_6_3_2_t01}) {
		t.Error("must not claim the PDF/A-1 font rule")
	}
}

func TestFixStructParent_AddsParentToTopLevel(t *testing.T) {
	// StructTreeRoot(4) -> Document element(5) that lacks /P.
	pdf := structTreePDF(
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixStructParent(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixStructParent: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 fix description, got %v", descs)
	}
	p, ok := pendingDict(t, alloc, 5)["P"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("/P not added or not an indirect reference")
	}
	if p.ObjectNum != 4 {
		t.Errorf("/P = obj %d, want the StructTreeRoot (obj 4)", p.ObjectNum)
	}
}

func TestFixStructParent_AddsParentToNested(t *testing.T) {
	// Document(5) already has /P; its child Span(6) does not.
	pdf := structTreePDF(
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K 6 0 R >>",
		"<< /Type /StructElem /S /Span >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixStructParent(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixStructParent: %v", err)
	}
	if _, queued := alloc.Pending()[5]; queued {
		t.Error("Document element already has /P and must not be rewritten")
	}
	p, ok := pendingDict(t, alloc, 6)["P"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("nested /P not added or not an indirect reference")
	}
	if p.ObjectNum != 5 {
		t.Errorf("nested /P = obj %d, want the Document element (obj 5)", p.ObjectNum)
	}
}

func TestFixStructParent_ConformantUntouched(t *testing.T) {
	// Every element already carries /P: nothing is queued.
	pdf := structTreePDF(
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixStructParent(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixStructParent: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("conformant tree: got %v, want no fixes", descs)
	}
	if _, queued := alloc.Pending()[5]; queued {
		t.Error("conformant element must not be queued for replacement")
	}
}

func TestFixStructParent_SkipsNonStructChild(t *testing.T) {
	// /K holds a real element (5) and an object reference (6, no /S): only the
	// element gets /P; the OBJR is never touched.
	pdf := structTreePDF(
		"<< /Type /StructTreeRoot /K [5 0 R 6 0 R] >>",
		"<< /Type /StructElem /S /Document >>",
		"<< /Type /OBJR /Obj 3 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixStructParent(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixStructParent: %v", err)
	}
	if _, queued := alloc.Pending()[5]; !queued {
		t.Error("Document element should have received /P")
	}
	if _, queued := alloc.Pending()[6]; queued {
		t.Error("OBJR (no /S) must not be given /P")
	}
}
