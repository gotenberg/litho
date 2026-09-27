package remediation

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestMatchSingleDocRoot(t *testing.T) {
	t.Parallel()
	if !matchSingleDocRoot(compliance.Violation{Rule: compliance.Rule_14289_2_8_2_5_2_t01}) {
		t.Error("must match the single-Document-root rule (§8.2.5.2 t1)")
	}
	// t2 (namespace) is handled implicitly by the /NS the fixer sets, not claimed directly.
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
	// And the Document holds them, in order.
	wantKids(t, docElem, 5, 6)
}

// wantKids fails unless elem's /K lists exactly the objects nums, in order.
func wantKids(t *testing.T, elem ast.PdfDict, nums ...int) {
	t.Helper()
	kids, _ := elem["K"].(ast.PdfArray)
	got := make([]int, 0, len(kids))
	for _, kid := range kids {
		ref, _ := kid.(ast.PdfIndirectRef)
		got = append(got, ref.ObjectNum)
	}
	if !slices.Equal(got, nums) {
		t.Errorf("%v /K lists objects %v, want %v", elem["S"], got, nums)
	}
}

// TestFixSingleDocRoot_KeepsTheTreeReachable pins that the Document names the
// elements it wraps as its kids, and a container it interposes names the
// element it holds (ISO 32000-1, 14.7.2, Table 323: /K lists an element's
// children). Listing the Document itself in place of each kid left every
// element of the tree unreachable from its root, and the walks that read the
// tree from the root, the structure destination one among them, found no
// element at all.
func TestFixSingleDocRoot_KeepsTheTreeReachable(t *testing.T) {
	t.Parallel()
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R 6 0 R] /Namespaces [7 0 R] >>",
		"<< /Type /StructElem /S /H1 /P 4 0 R >>",
		"<< /Type /StructElem /S /LI /P 4 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
	)
	doc, alloc := newTestDoc(t, pdf)
	if _, err := fixSingleDocRoot(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixSingleDocRoot: %v", err)
	}

	docRef, _ := pendingDict(t, alloc, 4)["K"].(ast.PdfIndirectRef)
	docElem := pendingDict(t, alloc, docRef.ObjectNum)
	kids, _ := docElem["K"].(ast.PdfArray)
	if len(kids) != 2 {
		t.Fatalf("the Document holds %v, want the H1 and the LI's container", kids)
	}
	container, _ := kids[1].(ast.PdfIndirectRef)
	wantKids(t, docElem, 5, container.ObjectNum)
	wantKids(t, pendingDict(t, alloc, container.ObjectNum), 6)
	if p, _ := pendingDict(t, alloc, 6)["P"].(ast.PdfIndirectRef); p.ObjectNum != container.ObjectNum {
		t.Errorf("the LI's /P = obj %d, want its container (%d)", p.ObjectNum, container.ObjectNum)
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
