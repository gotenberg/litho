package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestMatchSingleDocRoot(t *testing.T) {
	t.Parallel()
	if !matchSingleDocRoot(compliance.Violation{Rule: compliance.Rule_14289_2_8_2_5_2_t01}) {
		t.Error("must match the single-Document-root rule (§8.2.5.2 t1)")
	}
	// t2 (namespace) is handled implicitly by the /NS we set, not claimed directly.
	if matchSingleDocRoot(compliance.Violation{Rule: compliance.Rule_14289_2_8_2_5_2_t02}) {
		t.Error("must not claim the namespace rule (§8.2.5.2 t2)")
	}
}

func TestFixSingleDocRoot_WrapsMultipleChildren(t *testing.T) {
	// StructTreeRoot(4) -> [H1(5), P(6)] with a pdf2 namespace (7). The fixer wraps
	// both in a new Document and repoints their /P.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R 6 0 R] /Namespaces [7 0 R] >>",
		"<< /Type /StructElem /S /H1 /P 4 0 R >>",
		"<< /Type /StructElem /S /P /P 4 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixSingleDocRoot(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixSingleDocRoot: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 fix description, got %v", descs)
	}

	// The root /K now points at one new Document element.
	root := pendingDict(t, alloc, 4)
	docRef, ok := root["K"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("root /K is not a single indirect reference: %T", root["K"])
	}
	docElem, ok := alloc.Pending()[docRef.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("new Document element not allocated at obj %d", docRef.ObjectNum)
	}
	if docElem["S"] != ast.PdfName("Document") {
		t.Errorf("wrapper /S = %v, want Document", docElem["S"])
	}
	if p, _ := docElem["P"].(ast.PdfIndirectRef); p.ObjectNum != 4 {
		t.Errorf("wrapper /P = obj %d, want StructTreeRoot (4)", p.ObjectNum)
	}
	// Its /NS must be the existing pdf2 namespace (obj 7), not a new one.
	if ns, _ := docElem["NS"].(ast.PdfIndirectRef); ns.ObjectNum != 7 {
		t.Errorf("wrapper /NS = obj %d, want the existing pdf2 namespace (7)", ns.ObjectNum)
	}
	// Each original child now points at the Document.
	for _, objNum := range []int{5, 6} {
		child := pendingDict(t, alloc, objNum)
		if p, _ := child["P"].(ast.PdfIndirectRef); p.ObjectNum != docRef.ObjectNum {
			t.Errorf("child %d /P = obj %d, want the Document (%d)", objNum, p.ObjectNum, docRef.ObjectNum)
		}
	}
}

func TestFixSingleDocRoot_CreatesNamespaceWhenAbsent(t *testing.T) {
	// No /Namespaces on the root: the fixer creates a pdf2 namespace and references
	// it from the wrapper Document and the root /Namespaces.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Sect /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixSingleDocRoot(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixSingleDocRoot: %v", err)
	}
	root := pendingDict(t, alloc, 4)
	nsArr, ok := root["Namespaces"].(ast.PdfArray)
	if !ok || len(nsArr) == 0 {
		t.Fatalf("root /Namespaces not created: %v", root["Namespaces"])
	}
	docRef := root["K"].(ast.PdfIndirectRef)
	docElem := alloc.Pending()[docRef.ObjectNum].(ast.PdfDict)
	nsRef, ok := docElem["NS"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("wrapper /NS missing")
	}
	ns := alloc.Pending()[nsRef.ObjectNum].(ast.PdfDict)
	if ns["NS"] != ast.PdfString("http://iso.org/pdf2/ssn") {
		t.Errorf("created namespace /NS = %v, want the pdf2 URI", ns["NS"])
	}
}

func TestFixSingleDocRoot_ExistingDocumentUntouched(t *testing.T) {
	// Root already has a single Document child: nothing to do.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixSingleDocRoot(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixSingleDocRoot: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("existing Document root: got %v, want no fixes", descs)
	}
	if _, queued := alloc.Pending()[4]; queued {
		t.Error("a conformant single-Document root must not be modified")
	}
}
