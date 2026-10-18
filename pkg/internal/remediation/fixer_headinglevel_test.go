package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
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
			wantBefore: 2, wantAfter: 1, wantKid6: "H1", wantKid9Struct: "Sect",
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
