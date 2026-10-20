package remediation

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

func TestMatchHeadingLevel(t *testing.T) {
	t.Parallel()
	if !matchHeadingLevel(compliance.Violation{Rule: compliance.Rule_14289_2_8_2_5_12_t01}) {
		t.Error("must match ISO 14289-2 8.2.5.12 t1")
	}
	for _, r := range []compliance.Rule{compliance.Rule_14289_2_8_2_5_14_t01, compliance.Rule_14289_1_7_4_4_t03} {
		if matchHeadingLevel(compliance.Violation{Rule: r}) {
			t.Errorf("must not claim %s %s t%d", r.Specification, r.Clause, r.TestNumber)
		}
	}
}

// headingPDF builds a tagged PDF/UA-2 candidate: object 5 is the root Document
// in the PDF 2.0 namespace (object 7), objects 6 and 9 are its two kids, and
// object 8 is the PDF 1.7 namespace. Object 10 is a spare element kid 6 or 9
// may hold. root extends the structure tree root and ns17 the PDF 1.7
// namespace.
func headingPDF(kid6, kid9, kid10, root, ns17 string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /Namespaces [7 0 R 8 0 R] "+root+" >>",
		"<< /Type /StructElem /S /Document /NS 7 0 R /P 4 0 R /K [6 0 R 9 0 R] >>",
		"<< /Type /StructElem /P 5 0 R "+kid6+" >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) "+ns17+" >>",
		"<< /Type /StructElem /P 5 0 R "+kid9+" >>",
		"<< /Type /StructElem "+kid10+" >>",
	)
}

// genericHeadings returns the number of ISO 14289-2 8.2.5.12 t1 findings of
// doc at PDF/UA-2.
func genericHeadings(t *testing.T, doc *document.Document) int {
	t.Helper()
	errs, err := compliance.ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	n := 0
	for _, v := range toViolations(errs) {
		if v.Rule == compliance.Rule_14289_2_8_2_5_12_t01 {
			n++
		}
	}
	return n
}

// TestRemediatePDFUA2NumbersTheTopLevelGenericHeading pins the repair of ISO
// 14289-2, 8.2.5.12 t1: an H nested in no division takes the level its nesting
// gives it, H1. The controls are the H elements whose level needs an outline
// judgement or whose type a role map governs: those stay H and keep failing.
func TestRemediatePDFUA2NumbersTheTopLevelGenericHeading(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name                     string
		kid6, kid9, kid10        string
		root, ns17               string
		wantBefore, wantAfter    int
		wantKid6, wantKid9Struct ast.PdfName
	}{
		{
			name: "two headings under the document",
			kid6: "/S /H", kid9: "/S /H /NS 8 0 R",
			wantBefore: 2, wantKid6: "H1", wantKid9Struct: "H1",
		},
		{
			name: "heading inside a section",
			kid6: "/S /H", kid9: "/S /Sect /K [10 0 R]", kid10: "/S /H /P 9 0 R",
			wantBefore: 2, wantKid6: "H1", wantKid9Struct: "Sect",
		},
		{
			name: "heading inside a div",
			kid6: "/S /P", kid9: "/S /Div /K [10 0 R]", kid10: "/S /H /P 9 0 R",
			wantBefore: 1, wantAfter: 1, wantKid6: "P", wantKid9Struct: "Div",
		},
		{
			name: "legacy role map governing H1",
			kid6: "/S /H", kid9: "/S /P", root: "/RoleMap << /H1 /P >>",
			wantBefore: 1, wantAfter: 1, wantKid6: "H", wantKid9Struct: "P",
		},
		{
			name: "pdf 1.7 namespace role map governing H1",
			kid6: "/S /H /NS 8 0 R", kid9: "/S /P", ns17: "/RoleMapNS << /H1 [/P 7 0 R] >>",
			wantBefore: 1, wantAfter: 1, wantKid6: "H", wantKid9Struct: "P",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := headingPDF(tc.kid6, tc.kid9, tc.kid10, tc.root, tc.ns17)
			if got := genericHeadings(t, inputDoc(t, in)); got != tc.wantBefore {
				t.Fatalf("the input fails 8.2.5.12 t1 %d times, want %d", got, tc.wantBefore)
			}
			out := remediatePDFUA2Output(t, in)
			if got := genericHeadings(t, out); got != tc.wantAfter {
				t.Errorf("the output fails 8.2.5.12 t1 %d times, want %d", got, tc.wantAfter)
			}
			catalog, err := document.ResolveCatalog(ctx, out)
			if err != nil {
				t.Fatalf("resolving the catalog: %v", err)
			}
			root, err := document.ResolveDict(ctx, out, catalog.Dict["StructTreeRoot"])
			if err != nil {
				t.Fatalf("resolving the structure tree root: %v", err)
			}
			docElem := firstKid(t, out, root)
			kids, err := document.ResolveArray(ctx, out, docElem["K"])
			if err != nil || len(kids) != 2 {
				t.Fatalf("the Document holds %v, want two kids", docElem["K"])
			}
			for i, want := range []ast.PdfName{tc.wantKid6, tc.wantKid9Struct} {
				kid, err := document.ResolveDict(ctx, out, kids[i])
				if err != nil {
					t.Fatalf("resolving kid %d: %v", i, err)
				}
				if s, _ := document.ResolveName(ctx, out, kid["S"]); s != want {
					t.Errorf("kid %d is %s, want %s", i, s, want)
				}
			}
		})
	}
}

// nestedHeadingPDF builds a tagged PDF whose structure tree root (object 4)
// lists the Document element, object 5, the first of elems, which are
// numbered from 5 on. root extends the structure tree root.
func nestedHeadingPDF(root string, elems ...string) []byte {
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] " + root + " >>",
	}
	return buildStructPDF(append(bodies, elems...)...)
}

// structTypesInOrder returns the /S of every structure element of doc, in
// depth-first document order, joined by spaces.
func structTypesInOrder(t *testing.T, doc *document.Document) string {
	t.Helper()
	ctx := context.Background()
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("resolving the catalog: %v", err)
	}
	root, err := document.ResolveDict(ctx, doc, catalog.Dict["StructTreeRoot"])
	if err != nil {
		t.Fatalf("resolving the structure tree root: %v", err)
	}
	var out []string
	var walk func(k ast.PdfObject, depth int)
	walk = func(k ast.PdfObject, depth int) {
		if depth > 32 {
			t.Fatal("structure tree deeper than the fixtures build")
		}
		for _, item := range resolveStructKids(ctx, doc, k) {
			elem, err := document.ResolveDict(ctx, doc, item)
			if err != nil || elem == nil {
				continue
			}
			out = append(out, string(structType(ctx, doc, elem)))
			walk(elem["K"], depth+1)
		}
	}
	walk(root["K"], 0)
	return strings.Join(out, " ")
}

// TestRemediatePDFUA2NumbersNestedGenericHeadings pins the repair of ISO
// 14289-2, 8.2.5.12 t1 for an H inside a section: in a tree of Sect elements,
// each H the first and only H of its Sect, every H takes the level its section
// nesting gives it, the outermost H1. An Art of the PDF 1.7 namespace adds a
// level as a Sect does. The controls are the trees whose nesting gives no
// level: a Sect holding two H, an H inside a Part or a Div or after other
// content, an Art in the PDF 2.0 namespace, which defines no Art, a document
// that also numbers its headings, and a nesting deeper than H6.
func TestRemediatePDFUA2NumbersNestedGenericHeadings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		root       string
		elems      []string
		wantBefore int
		wantAfter  int
		wantTypes  string
	}{
		{
			name: "sections nested two deep",
			elems: []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R 8 0 R] >>",
				"<< /Type /StructElem /S /Sect /P 5 0 R /K [7 0 R 10 0 R] >>",
				"<< /Type /StructElem /S /H /P 6 0 R >>",
				"<< /Type /StructElem /S /Sect /P 5 0 R /K [9 0 R 11 0 R] >>",
				"<< /Type /StructElem /S /H /P 8 0 R >>",
				"<< /Type /StructElem /S /P /P 6 0 R >>",
				"<< /Type /StructElem /S /Sect /P 8 0 R /K [12 0 R] >>",
				"<< /Type /StructElem /S /H /P 11 0 R >>",
			},
			wantBefore: 3,
			wantTypes:  "Document Sect H1 P Sect H1 Sect H2",
		},
		{
			name: "a part holding a section",
			elems: []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
				"<< /Type /StructElem /S /Part /P 5 0 R /K [7 0 R 8 0 R] >>",
				"<< /Type /StructElem /S /H /P 6 0 R >>",
				"<< /Type /StructElem /S /Sect /P 6 0 R /K [9 0 R] >>",
				"<< /Type /StructElem /S /H /P 8 0 R >>",
			},
			wantBefore: 2, wantAfter: 2,
			wantTypes: "Document Part H Sect H",
		},
		{
			name: "an article holding a section",
			elems: []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
				"<< /Type /StructElem /S /Art /P 5 0 R /K [7 0 R 8 0 R] >>",
				"<< /Type /StructElem /S /H /P 6 0 R >>",
				"<< /Type /StructElem /S /Sect /P 6 0 R /K [9 0 R] >>",
				"<< /Type /StructElem /S /H /P 8 0 R >>",
			},
			wantBefore: 2,
			wantTypes:  "Document Art H1 Sect H2",
		},
		{
			name: "a section holding an article",
			elems: []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
				"<< /Type /StructElem /S /Sect /P 5 0 R /K [7 0 R 8 0 R] >>",
				"<< /Type /StructElem /S /H /P 6 0 R >>",
				"<< /Type /StructElem /S /Art /P 6 0 R /K [9 0 R] >>",
				"<< /Type /StructElem /S /H /P 8 0 R >>",
			},
			wantBefore: 2,
			wantTypes:  "Document Sect H1 Art H2",
		},
		{
			name: "an article in the pdf 2.0 namespace",
			root: "/Namespaces [9 0 R]",
			elems: []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
				"<< /Type /StructElem /S /Art /NS 9 0 R /P 5 0 R /K [7 0 R 8 0 R] >>",
				"<< /Type /StructElem /S /H /P 6 0 R >>",
				"<< /Type /StructElem /S /Sect /P 6 0 R /K [10 0 R] >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
				"<< /Type /StructElem /S /H /P 8 0 R >>",
			},
			wantBefore: 2, wantAfter: 2,
			wantTypes: "Document Art H Sect H",
		},
		{
			name: "a document heading over a section heading",
			elems: []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R 7 0 R] >>",
				"<< /Type /StructElem /S /H /P 5 0 R >>",
				"<< /Type /StructElem /S /Sect /P 5 0 R /K [8 0 R] >>",
				"<< /Type /StructElem /S /H /P 7 0 R >>",
			},
			wantBefore: 2,
			wantTypes:  "Document H1 Sect H2",
		},
		{
			name: "two headings in one section",
			elems: []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R 9 0 R] >>",
				"<< /Type /StructElem /S /Sect /P 5 0 R /K [7 0 R 8 0 R] >>",
				"<< /Type /StructElem /S /H /P 6 0 R >>",
				"<< /Type /StructElem /S /H /P 6 0 R >>",
				"<< /Type /StructElem /S /Sect /P 5 0 R /K [10 0 R] >>",
				"<< /Type /StructElem /S /H /P 9 0 R >>",
			},
			wantBefore: 3, wantAfter: 3,
			wantTypes: "Document Sect H H Sect H",
		},
		{
			name: "a heading inside a div",
			elems: []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
				"<< /Type /StructElem /S /Sect /P 5 0 R /K [7 0 R] >>",
				"<< /Type /StructElem /S /Div /P 6 0 R /K [8 0 R] >>",
				"<< /Type /StructElem /S /H /P 7 0 R >>",
			},
			wantBefore: 1, wantAfter: 1,
			wantTypes: "Document Sect Div H",
		},
		{
			name: "a heading after other content",
			elems: []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
				"<< /Type /StructElem /S /Sect /P 5 0 R /K [7 0 R 8 0 R] >>",
				"<< /Type /StructElem /S /P /P 6 0 R >>",
				"<< /Type /StructElem /S /H /P 6 0 R >>",
			},
			wantBefore: 1, wantAfter: 1,
			wantTypes: "Document Sect P H",
		},
		{
			name: "a document that also numbers its headings",
			elems: []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R 7 0 R] >>",
				"<< /Type /StructElem /S /H1 /P 5 0 R >>",
				"<< /Type /StructElem /S /Sect /P 5 0 R /K [8 0 R] >>",
				"<< /Type /StructElem /S /H /P 7 0 R >>",
			},
			wantBefore: 1, wantAfter: 1,
			wantTypes: "Document H1 Sect H",
		},
		{
			name: "a role map that numbers a heading",
			root: "/RoleMap << /Title /H2 >>",
			elems: []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
				"<< /Type /StructElem /S /Sect /P 5 0 R /K [7 0 R] >>",
				"<< /Type /StructElem /S /H /P 6 0 R >>",
			},
			wantBefore: 1, wantAfter: 1,
			wantTypes: "Document Sect H",
		},
		{
			name: "a nesting deeper than six levels",
			elems: []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R 7 0 R] >>",
				"<< /Type /StructElem /S /H /P 5 0 R >>",
				"<< /Type /StructElem /S /Sect /P 5 0 R /K [8 0 R] >>",
				"<< /Type /StructElem /S /Sect /P 7 0 R /K [9 0 R] >>",
				"<< /Type /StructElem /S /Sect /P 8 0 R /K [10 0 R] >>",
				"<< /Type /StructElem /S /Sect /P 9 0 R /K [11 0 R] >>",
				"<< /Type /StructElem /S /Sect /P 10 0 R /K [12 0 R] >>",
				"<< /Type /StructElem /S /Sect /P 11 0 R /K [13 0 R] >>",
				"<< /Type /StructElem /S /Sect /P 12 0 R /K [14 0 R] >>",
				"<< /Type /StructElem /S /H /P 13 0 R >>",
			},
			wantBefore: 2, wantAfter: 1,
			wantTypes: "Document H1 Sect Sect Sect Sect Sect Sect Sect H",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := nestedHeadingPDF(tc.root, tc.elems...)
			if got := genericHeadings(t, inputDoc(t, in)); got != tc.wantBefore {
				t.Fatalf("the input fails 8.2.5.12 t1 %d times, want %d", got, tc.wantBefore)
			}
			out := remediatePDFUA2Output(t, in)
			if got := genericHeadings(t, out); got != tc.wantAfter {
				t.Errorf("the output fails 8.2.5.12 t1 %d times, want %d", got, tc.wantAfter)
			}
			if got := structTypesInOrder(t, out); got != tc.wantTypes {
				t.Errorf("the output tree is %q, want %q", got, tc.wantTypes)
			}
		})
	}
}

// TestRemediateWTPDFNumbersGenericHeadings pins that both WTPDF levels, which
// state ISO 14289-2, 8.2.5.12 with the rest of PDF/UA-2, get the heading
// repair a PDF/UA-2 run gets.
func TestRemediateWTPDFNumbersGenericHeadings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := nestedHeadingPDF("",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R 7 0 R] >>",
		"<< /Type /StructElem /S /H /P 5 0 R >>",
		"<< /Type /StructElem /S /Sect /P 5 0 R /K [8 0 R] >>",
		"<< /Type /StructElem /S /H /P 7 0 R >>",
	)
	for _, profile := range []models.WTPDFProfile{models.WTPDF1Accessibility, models.WTPDF1Reuse} {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, in)
			if _, err := RemediateWTPDF(ctx, doc, alloc, profile, Options{}); err != nil {
				t.Fatalf("RemediateWTPDF: %v", err)
			}
			var buf bytes.Buffer
			if err := mutations.FullRewrite(ctx, &buf, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
				t.Fatalf("FullRewrite: %v", err)
			}
			out, err := document.NewDocument(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			errs, err := compliance.ValidateWTPDF(ctx, out, profile)
			if err != nil {
				t.Fatalf("ValidateWTPDF: %v", err)
			}
			for _, v := range toViolations(errs) {
				if v.Rule == compliance.Rule_14289_2_8_2_5_12_t01 {
					t.Errorf("the output still fails 8.2.5.12 t1: %s", v.Context)
				}
			}
			if got, want := structTypesInOrder(t, out), "Document H1 Sect H2"; got != want {
				t.Errorf("the output tree is %q, want %q", got, want)
			}
		})
	}
}
