package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// minimalPDFNoStructTree builds a valid single-page PDF with a Catalog that
// does not carry a /StructTreeRoot entry. Used as the input fixture for the
// [fixStructTree] fixer.
func minimalPDFNoStructTree() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000115 00000 n \r\n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\n" +
			"startxref\n198\n%%EOF\n")
}

func TestFixStructTree_AddsMinimalStructureTree(t *testing.T) {
	ctx := context.Background()
	data := minimalPDFNoStructTree()

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	alloc := writer.NewObjAllocator(3) // xref has objects 1..3

	descs, err := fixStructTree(ctx, alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %d", len(descs))
	}

	// The fixer must have written a new catalog referencing a new
	// StructTreeRoot object.
	pending := alloc.Pending()
	newCatalogObj, ok := pending[1]
	if !ok {
		t.Fatal("expected pending entry for object 1 (catalog)")
	}
	newCatalog, ok := newCatalogObj.(ast.PdfDict)
	if !ok {
		t.Fatalf("catalog: got %T, want ast.PdfDict", newCatalogObj)
	}
	stRef, ok := newCatalog["StructTreeRoot"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("catalog /StructTreeRoot: got %T, want ast.PdfIndirectRef", newCatalog["StructTreeRoot"])
	}

	// Follow the /StructTreeRoot reference.
	stObj, ok := pending[stRef.ObjectNum]
	if !ok {
		t.Fatalf("expected pending entry for StructTreeRoot at obj %d", stRef.ObjectNum)
	}
	stDict, ok := stObj.(ast.PdfDict)
	if !ok {
		t.Fatalf("StructTreeRoot: got %T, want ast.PdfDict", stObj)
	}
	if stDict["Type"] != ast.PdfName("StructTreeRoot") {
		t.Errorf("StructTreeRoot /Type: got %v, want StructTreeRoot", stDict["Type"])
	}

	// The tree root's /K points at the document element.
	docElemRef, ok := stDict["K"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("StructTreeRoot /K: got %T, want ast.PdfIndirectRef", stDict["K"])
	}
	docElemObj, ok := pending[docElemRef.ObjectNum]
	if !ok {
		t.Fatalf("expected pending entry for document element at obj %d", docElemRef.ObjectNum)
	}
	docElem, ok := docElemObj.(ast.PdfDict)
	if !ok {
		t.Fatalf("document element: got %T, want ast.PdfDict", docElemObj)
	}
	if docElem["Type"] != ast.PdfName("StructElem") {
		t.Errorf("document element /Type: got %v, want StructElem", docElem["Type"])
	}
	if docElem["S"] != ast.PdfName("Document") {
		t.Errorf("document element /S: got %v, want Document", docElem["S"])
	}

	// Regression guard for the structure-element parent key.
	//
	// Per ISO 32000-2, Table 355 the parent entry of a structure element
	// is /P. Earlier fixer versions wrote /ParentTreeRoot, then /Parent;
	// neither is a structure-element entry, so the Document element was
	// parentless for conforming readers.
	parentRef, ok := docElem["P"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("document element /P: got %T (%v), want ast.PdfIndirectRef",
			docElem["P"], docElem["P"])
	}
	if parentRef != stRef {
		t.Errorf("document element /P: got obj %d, want tree root obj %d",
			parentRef.ObjectNum, stRef.ObjectNum)
	}
	for _, bad := range []ast.PdfName{"Parent", "ParentTreeRoot"} {
		if _, hasBug := docElem[bad]; hasBug {
			t.Errorf("document element should not have /%s key", bad)
		}
	}
}

// resolveNSString follows a /NS reference into the allocator's pending set
// and returns the namespace dict's /NS string.
func resolveNSString(t *testing.T, alloc *writer.ObjAllocator, nsObj ast.PdfObject) string {
	t.Helper()
	nsRef, ok := nsObj.(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("/NS: got %T (%v), want ast.PdfIndirectRef", nsObj, nsObj)
	}
	nsDict := pendingDict(t, alloc, nsRef.ObjectNum)
	uri, ok := nsDict["NS"].(ast.PdfString)
	if !ok {
		t.Fatalf("namespace dict /NS: got %T, want ast.PdfString", nsDict["NS"])
	}
	return string(uri)
}

func TestFixStructTree_UA2NewTreeUsesPDF2NamespaceOnDocument(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalPDFNoStructTree())

	descs, err := fixStructTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %d: %v", len(descs), descs)
	}

	newCatalog := pendingDict(t, alloc, 1)
	stRef, ok := newCatalog["StructTreeRoot"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("catalog /StructTreeRoot: got %T", newCatalog["StructTreeRoot"])
	}
	stDict := pendingDict(t, alloc, stRef.ObjectNum)

	nsArr, ok := stDict["Namespaces"].(ast.PdfArray)
	if !ok || len(nsArr) != 2 {
		t.Fatalf("StructTreeRoot /Namespaces: got %T (%v), want 2-element array",
			stDict["Namespaces"], stDict["Namespaces"])
	}

	docElemRef, ok := stDict["K"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("StructTreeRoot /K: got %T", stDict["K"])
	}
	docElem := pendingDict(t, alloc, docElemRef.ObjectNum)

	// ISO 14289-2 §8.2.5.2 t2: the Document element must be in the PDF 2.0
	// namespace. The fixer used to point it at PDF 1.7.
	if uri := resolveNSString(t, alloc, docElem["NS"]); uri != pdf2NamespaceURI {
		t.Errorf("document element namespace: got %q, want %q", uri, pdf2NamespaceURI)
	}
	if pRef, ok := docElem["P"].(ast.PdfIndirectRef); !ok || pRef != stRef {
		t.Errorf("document element /P: got %v, want ref to obj %d", docElem["P"], stRef.ObjectNum)
	}
}

func TestFixStructTree_UA2ExistingTreeSetsMissingDocumentNS(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixStructTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %d: %v", len(descs), descs)
	}

	stDict := pendingDict(t, alloc, 4)
	if _, ok := stDict["Namespaces"].(ast.PdfArray); !ok {
		t.Fatalf("StructTreeRoot /Namespaces missing after fix: %v", stDict["Namespaces"])
	}

	docElem := pendingDict(t, alloc, 5)
	if uri := resolveNSString(t, alloc, docElem["NS"]); uri != pdf2NamespaceURI {
		t.Errorf("document element namespace: got %q, want %q", uri, pdf2NamespaceURI)
	}
}

func TestFixStructTree_UA2NamespacesPresentRepointsDocNS(t *testing.T) {
	// /Namespaces already lists both standard namespaces; the Document
	// element still points at PDF 1.7. The fixer must repoint it at the
	// listed 2.0 namespace without allocating a duplicate.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /Namespaces [6 0 R 7 0 R] >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /NS 6 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixStructTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %d: %v", len(descs), descs)
	}

	docElem := pendingDict(t, alloc, 5)
	nsRef, ok := docElem["NS"].(ast.PdfIndirectRef)
	if !ok || nsRef.ObjectNum != 7 {
		t.Errorf("document element /NS: got %v, want reuse of listed 2.0 namespace obj 7", docElem["NS"])
	}
	// No new namespace object and no StructTreeRoot rewrite needed.
	if _, rewrote := alloc.Pending()[4]; rewrote {
		t.Error("StructTreeRoot should not be rewritten when a 2.0 namespace is already listed")
	}
}

// roleMappedDocumentPDF builds the shape the veraPDF corpus actually carries:
// a structure tree whose root child spells a PRIVATE type that the /RoleMap
// sends to Document.
//
// Measured on PDF_A-1a__veraPDF_test_suite_6-7-11-t01-fail-a and four siblings:
// StructTreeRoot /K [10 0 R] /RoleMap 12 0 R, no /Namespaces, obj 10 is
// /S /PDFDocument, and obj 12 maps /PDFDocument -> /Document (and
// /Rectangle -> /Figure). Every one of the five is identical in shape.
func roleMappedDocumentPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /RoleMap 6 0 R >>",
		"<< /Type /StructElem /S /PDFDocument /P 4 0 R >>",
		"<< /PDFDocument /Document /Rectangle /Figure >>",
	)
}

// TestFixStructTree_UA2RoleMappedDocumentGetsPDF2NS pins that the Document
// element is found through the /RoleMap, not by the name its /S spells.
//
// ISO 14289-2, 8.2.5.2 t2 obliges the root Document element to declare the
// PDF 2.0 namespace. The validator decides what "is a Document" means by
// resolving /S through /RoleMap (ISO 32000-1, 14.8.4), so it reports the
// violation here. [documentElementNeedingNS] compared the RAW /S against
// "Document", so it answered no, and the repair wrote /Namespaces onto the
// root while silently leaving the element untagged: claimed, fired, and not
// repaired.
//
// The asymmetry is measurable in the gate rather than inferred. All five
// carriers are clean under every PDF/A profile and come back post=1 with
// residual exactly ['8.2.5.2-t02'] under PDF_UA-2 and both WTPDF levels,
// which is 15 cells that litho and veraPDF already agree on.
//
// [resolvedStructType] is the shared resolver whose own godoc records this
// same mistake costing a spurious 7.2 t18 in the list fixer.
func TestFixStructTree_UA2RoleMappedDocumentGetsPDF2NS(t *testing.T) {
	doc, alloc := newTestDoc(t, roleMappedDocumentPDF())

	if _, err := fixStructTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}

	// The root gains /Namespaces on this branch whether or not the element is
	// recognised, so asserting it first keeps the real failure below specific:
	// the repair ran, and only the element was skipped.
	stDict := pendingDict(t, alloc, 4)
	if _, ok := stDict["Namespaces"].(ast.PdfArray); !ok {
		t.Fatalf("StructTreeRoot /Namespaces missing after fix: %v", stDict["Namespaces"])
	}

	docElem := pendingDict(t, alloc, 5)
	if uri := resolveNSString(t, alloc, docElem["NS"]); uri != pdf2NamespaceURI {
		t.Errorf("role-mapped Document namespace: got %q, want %q", uri, pdf2NamespaceURI)
	}
}

// TestFixStructTree_UA2LiteralDocumentMappedAwayStillGetsNS pins the arm that
// resolving through the /RoleMap does NOT cover.
//
// Shape measured on veraPDF's PDF_UA-1__7.1-t07-fail-a.pdf, which
// fixer_rolemap.go already names: StructTreeRoot /K [18 0 R] with
// /RoleMap << /Document /Book >> and no /Namespaces, where object 18 is
// literally /S /Document.
//
// Following the map sends a literally-named Document AWAY from Document (to
// Book, which is standard nowhere), so a guard that only resolves rejects the
// very element the rule is about. Writing /NS is what repairs this file:
// [equivalentType] consults the legacy /RoleMap only for the empty and PDF 1.7
// namespaces (accessibility_structure.go:547), so declaring the PDF 2.0
// namespace escapes the harmful Document->Book mapping and the element reads as
// a standard Document again.
//
// Measured: replacing the raw-name check with resolution alone dropped this
// file from fully-fixed to three violations (8.2.5.2-t01, 8.2.4-t04 and
// Table 5. StructTreeRoot-Document-t01) across PDF_UA-2 and both WTPDF levels.
// The two checks identify overlapping but different sets, so the guard needs
// BOTH arms.
func TestFixStructTree_UA2LiteralDocumentMappedAwayStillGetsNS(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /RoleMap 6 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
		// The producer maps the standard name to a custom one.
		"<< /Document /Book >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixStructTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}

	docElem := pendingDict(t, alloc, 5)
	if uri := resolveNSString(t, alloc, docElem["NS"]); uri != pdf2NamespaceURI {
		t.Errorf("literal Document mapped to Book: got namespace %q, want %q", uri, pdf2NamespaceURI)
	}
}

// TestFixStructTree_UA2LeavesARoleMappedNonDocumentAlone is the control that
// keeps the fix narrow.
//
// Only a Document carries the 8.2.5.2 t2 obligation. Other root children may
// be valid ONLY in the PDF 1.7 namespace, so retagging one to 2.0 replaces a
// reported violation with a new one. A repair that simply tagged whatever the
// root's /K names would pass the test above and fail here, which is what makes
// this a control rather than a restatement.
func TestFixStructTree_UA2LeavesARoleMappedNonDocumentAlone(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /RoleMap 6 0 R >>",
		// Resolves to Figure, not Document.
		"<< /Type /StructElem /S /Rectangle /P 4 0 R >>",
		"<< /PDFDocument /Document /Rectangle /Figure >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixStructTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}

	if obj, written := alloc.Pending()[5]; written {
		t.Errorf("a root child resolving to Figure was rewritten: %v", obj)
	}
}

func TestFixStructTree_UA2NonDocumentChildUntouched(t *testing.T) {
	// A single Note child in the PDF 1.7 namespace must not be retagged:
	// Note does not exist in the PDF 2.0 namespace.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /Namespaces [6 0 R] >>",
		"<< /Type /StructElem /S /Note /P 4 0 R /NS 6 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixStructTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("expected no fixes on non-Document child, got %v and %d pending", descs, len(alloc.Pending()))
	}
}

func TestFixStructTree_UA2ForeignNSUntouched(t *testing.T) {
	// A Document element in a non-standard namespace is left alone.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /Namespaces [6 0 R] >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /NS 6 0 R >>",
		"<< /Type /Namespace /NS (http://example.com/custom/ns) >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixStructTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("expected no fixes on foreign namespace, got %v and %d pending", descs, len(alloc.Pending()))
	}
}

func TestFixStructTree_UA2ExistingTreeRepointsPDF1NS(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /NS 6 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixStructTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}

	docElem := pendingDict(t, alloc, 5)
	if uri := resolveNSString(t, alloc, docElem["NS"]); uri != pdf2NamespaceURI {
		t.Errorf("document element namespace: got %q, want %q (PDF 1.7 must be repointed)", uri, pdf2NamespaceURI)
	}
	if pRef, ok := docElem["P"].(ast.PdfIndirectRef); !ok || pRef.ObjectNum != 4 {
		t.Errorf("document element /P: got %v, want ref to obj 4", docElem["P"])
	}
}

func TestFixStructTree_SkipsIfAlreadyPresent(t *testing.T) {
	ctx := context.Background()
	// A catalog that already has /StructTreeRoot should be left alone.
	data := []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"4 0 obj\n<< /Type /StructTreeRoot >>\nendobj\n" +
			"xref\n0 5\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000081 00000 n \r\n" +
			"0000000138 00000 n \r\n" +
			"0000000221 00000 n \r\n" +
			"trailer\n<< /Size 5 /Root 1 0 R >>\n" +
			"startxref\n264\n%%EOF\n")

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(4)

	descs, err := fixStructTree(ctx, alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("expected no descriptions on skip, got %v", descs)
	}
	if len(alloc.Pending()) != 0 {
		t.Errorf("expected no pending allocations on skip, got %d", len(alloc.Pending()))
	}
}
