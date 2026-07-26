package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// nsTestDoc builds a document whose structure tree root declares the PDF 2.0,
// a custom (role-mapping), and the PDF 1.7 namespaces, plus a legacy /RoleMap.
// Object numbers: 5 = pdf2 ns, 6 = custom ns (MyForm -> [Form, pdf2]), 7 = pdf1
// ns, 8 = the single Document child (in pdf2). Returns the doc and the resolved
// structure tree root.
func nsTestDoc(t *testing.T) (*document.Document, ast.PdfDict) {
	t.Helper()
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /Namespaces [5 0 R 6 0 R 7 0 R] /RoleMap << /MyDiv /Div >> /K 8 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		"<< /Type /Namespace /NS (http://example.com/ns) /RoleMapNS << /MyForm [/Form 5 0 R] >> >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) >>",
		"<< /Type /StructElem /S /Document /NS 5 0 R /P 4 0 R >>",
	)
	root, err := document.ResolveDict(context.Background(), doc, cat["StructTreeRoot"])
	if err != nil || root == nil {
		t.Fatalf("resolve StructTreeRoot: %v", err)
	}
	return doc, root
}

func ref(n int) ast.PdfIndirectRef { return ast.PdfIndirectRef{ObjectNum: n} }

func TestNSRoleResolver(t *testing.T) {
	ctx := context.Background()
	doc, root := nsTestDoc(t)
	r := newNSRoleResolver(ctx, doc, root)

	const pdf2 = "http://iso.org/pdf2/ssn"
	const pdf1 = "http://iso.org/pdf/ssn"

	// Core chain resolution by (type, namespace URI).
	chain := []struct {
		name           string
		typeName, ns   string
		wantType, wfNS string
		wantOK         bool
	}{
		{"standard in pdf2", "Form", pdf2, "Form", pdf2, true},
		{"RoleMapNS custom->Form", "MyForm", "http://example.com/ns", "Form", pdf2, true},
		{"standard in pdf1", "Document", pdf1, "Document", pdf1, true},
		{"legacy RoleMap (no ns)", "MyDiv", "", "Div", "", true},
		{"unmapped in non-standard ns", "Bogus", "http://example.com/ns", "", "", false},
	}
	for _, c := range chain {
		gotType, gotNS, ok := r.resolveStandardType(ctx, doc, c.typeName, c.ns)
		if gotType != c.wantType || gotNS != c.wfNS || ok != c.wantOK {
			t.Errorf("%s: got (%q,%q,%v), want (%q,%q,%v)", c.name, gotType, gotNS, ok, c.wantType, c.wfNS, c.wantOK)
		}
	}

	// Element resolution: /NS reference and default-namespace inheritance.
	if gt, gn, ok := r.elementStandardType(ctx, doc, ast.PdfDict{"S": ast.PdfName("MyForm"), "NS": ref(6)}); !ok || gt != "Form" || gn != pdf2 {
		t.Errorf("element MyForm in custom ns: got (%q,%q,%v)", gt, gn, ok)
	}
	// No /NS: the element belongs to NO namespace and the document default is
	// not substituted. veraPDF reports null here, so reporting the default let a
	// Document without /NS pass ISO 14289-2, 8.2.5.2 t2 whenever that default was
	// the PDF 2.0 namespace, and hid a standard Note from 8.2.5.14 t1. The type
	// still resolves: ISO 32000-1:2008, 14.8.4 judges an element in no namespace
	// against the PDF 1.7 vocabulary, which contains Form.
	if gt, gn, ok := r.elementStandardType(ctx, doc, ast.PdfDict{"S": ast.PdfName("Form")}); !ok || gt != "Form" || gn != "" {
		t.Errorf("element Form without /NS: got (%q,%q,%v), want (%q,%q,%v)", gt, gn, ok, "Form", "", true)
	}
}

func TestDocumentNamespaceRule(t *testing.T) {
	ctx := context.Background()

	// Document declared in the PDF 1.7 namespace must fail §8.2.5.2 t2.
	doc1, cat1 := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /Namespaces [5 0 R] /K 6 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) >>",
		"<< /Type /StructElem /S /Document /NS 5 0 R /P 4 0 R >>",
	)
	root1, err := document.ResolveDict(ctx, doc1, cat1["StructTreeRoot"])
	if err != nil || root1 == nil {
		t.Fatalf("resolve root1: %v", err)
	}
	var got []Violation
	checkSingleDocumentRoot(ctx, doc1, root1, &got)
	if !hasRule(got, Rule_14289_2_8_2_5_2_t02) {
		t.Errorf("Document in PDF 1.7 namespace: got %v, want §8.2.5.2 t2", got)
	}

	// Document in the PDF 2.0 namespace is conformant.
	doc2, root2 := nsTestDoc(t)
	var ok []Violation
	checkSingleDocumentRoot(ctx, doc2, root2, &ok)
	if hasRule(ok, Rule_14289_2_8_2_5_2_t02) {
		t.Errorf("Document in PDF 2.0 namespace: got %v, want no §8.2.5.2 t2", ok)
	}
}

// TestSingleDocumentRootTestNumber pins which of the two ISO 14289-2 §8.2.5.2
// tests fires per failure shape. t1 constrains the root children's resolved
// standard types; t2 constrains only the namespace of an otherwise conforming
// single Document. Deciding either on the raw /S name swaps the two verdicts,
// which reads as a clean result under a clause-only comparison because the
// false positive on one test cancels the false negative on the other.
func TestSingleDocumentRootTestNumber(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name           string
		objs           []string
		wantT1, wantT2 bool
	}{
		{
			// /MyDoc role-maps to Document, so the resolved kid type is
			// Document: t1 holds and only the namespace is at fault.
			name: "legacy RoleMap to Document",
			objs: []string{
				"<< /Type /StructTreeRoot /RoleMap << /MyDoc /Document >> /K [5 0 R] >>",
				"<< /Type /StructElem /S /MyDoc /P 4 0 R >>",
			},
			wantT2: true,
		},
		{
			// Named Document but in a namespace that maps nowhere, so it
			// resolves to no standard type at all: the root has no Document.
			name: "Document in unmapped namespace",
			objs: []string{
				"<< /Type /StructTreeRoot /Namespaces [5 0 R] /K [6 0 R] >>",
				"<< /Type /Namespace /NS (http://example.com/ns) >>",
				"<< /Type /StructElem /S /Document /NS 5 0 R /P 4 0 R >>",
			},
			wantT1: true,
		},
		{
			// Part is a pass-through wrapper, so the Document it groups is
			// the root's effective only child and the file conforms.
			name: "Document behind a pass-through Part",
			objs: []string{
				"<< /Type /StructTreeRoot /Namespaces [5 0 R] /K [6 0 R] >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
				"<< /Type /StructElem /S /Part /NS 5 0 R /P 4 0 R /K [7 0 R] >>",
				"<< /Type /StructElem /S /Document /NS 5 0 R /P 6 0 R >>",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objs := append([]string{
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			}, c.objs...)
			doc, cat := objsDocPDF(t, objs...)
			root, err := document.ResolveDict(ctx, doc, cat["StructTreeRoot"])
			if err != nil || root == nil {
				t.Fatalf("resolve StructTreeRoot: %v", err)
			}

			var got []Violation
			checkSingleDocumentRoot(ctx, doc, root, &got)
			if hasRule(got, Rule_14289_2_8_2_5_2_t01) != c.wantT1 {
				t.Errorf("§8.2.5.2 t1: got fired=%v, want %v (violations %v)",
					!c.wantT1, c.wantT1, got)
			}
			if hasRule(got, Rule_14289_2_8_2_5_2_t02) != c.wantT2 {
				t.Errorf("§8.2.5.2 t2: got fired=%v, want %v (violations %v)",
					!c.wantT2, c.wantT2, got)
			}
		})
	}
}

// TestElementStandardTypeNoNamespaceIsNotTheDefault pins that an element which
// declares no /NS belongs to no namespace, rather than inheriting the document
// default from /Namespaces[0]. veraPDF 1.30.2 reports null for such an element,
// so substituting the default let a Document without /NS satisfy
// firstChildStandardTypeNamespaceURL and pass ISO 14289-2, 8.2.5.2 t2 whenever
// the default happened to be the PDF 2.0 namespace, and hid a standard Note
// from 8.2.5.14 t1.
func TestElementStandardTypeNoNamespaceIsNotTheDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// /Namespaces[0] is the PDF 2.0 namespace, so the old default-inheritance
	// would have reported it for every element lacking /NS.
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /Namespaces [6 0 R 7 0 R] >>",
		"<< /Type /StructElem /S /Document >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) >>",
	)

	root, err := document.ResolveDict(ctx, doc, cat["StructTreeRoot"])
	if err != nil || root == nil {
		t.Fatalf("resolve StructTreeRoot: %v", err)
	}
	r := newNSRoleResolver(ctx, doc, root)

	_, gotNS, ok := r.elementStandardType(ctx, doc, ast.PdfDict{"S": ast.PdfName("Document")})
	if !ok {
		t.Fatal("Document without /NS must still resolve to a standard type")
	}
	if gotNS != "" {
		t.Errorf("namespace of an element without /NS: got %q, want empty", gotNS)
	}

	// The whole point: 8.2.5.2 t2 must fire for that Document.
	var results []Violation
	checkSingleDocumentRoot(ctx, doc, root, &results)
	if !hasRule(results, Rule_14289_2_8_2_5_2_t02) {
		t.Errorf("Document without /NS must fire §8.2.5.2 t2: got %v", results)
	}
}
