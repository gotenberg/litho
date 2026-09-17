package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

const (
	roleMapTestPDF1 = "http://iso.org/pdf/ssn"
	roleMapTestPDF2 = "http://iso.org/pdf2/ssn"
)

// roleMapDoc builds a structure tree root carrying the given legacy /RoleMap and
// a single Document child, declaring both standard namespaces. roleMapNS, when
// not empty, is spliced into the PDF 2.0 namespace dictionary so a namespaced
// mapping can be exercised. Object numbers: 5 = pdf2 ns, 6 = pdf1 ns, 7 = the
// Document child.
func roleMapDoc(t *testing.T, roleMap, roleMapNS string) (*document.Document, ast.PdfDict) {
	t.Helper()
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /Namespaces [5 0 R 6 0 R] /RoleMap "+roleMap+" /K [7 0 R] >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) "+roleMapNS+" >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
	)
	root, err := document.ResolveDict(context.Background(), doc, cat["StructTreeRoot"])
	if err != nil || root == nil {
		t.Fatalf("resolve StructTreeRoot: %v", err)
	}
	return doc, root
}

// TestRoleMapAppliesBeforeStandardName pins that a structure type is resolved
// through the mapping that governs it BEFORE it is accepted as standard, per
// ISO 32000-2:2020, 14.8.6: a standard type that is remapped no longer denotes
// that standard type.
//
// Accepting the standard name first reported ISO 14289-2 8.2.5.2 t2 (a Document
// in the wrong namespace) where veraPDF reports t1 (the root holds no Document
// at all), and disagreed with [iso32005Resolve], which already applies the
// mapping first. Every expectation below was measured against veraPDF 1.30.2 on
// an equivalent fixture.
func TestRoleMapAppliesBeforeStandardName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// A /RoleMapNS entry on the PDF 2.0 namespace, mapping Document into the
	// PDF 1.7 namespace as a Sect.
	const nsMapsDocumentToSect = "/RoleMapNS << /Document [/Sect 6 0 R] >>"

	cases := []struct {
		name               string
		roleMap, roleMapNS string
		typeName, ns       string
		wantType, wantNS   string
		wantOK             bool
	}{
		{
			// veraPDF: 8.2.5.2 t1 fails, t2 passes vacuously.
			name:    "standard type remapped to a non-standard one stands for nothing",
			roleMap: "<< /Document /Book >>", typeName: "Document",
			wantOK: false,
		},
		{
			// A type mapped to itself has no applicable mapping; 8.2.4 t2 reports
			// the circular entry separately. veraPDF: t1 passes, t2 fails.
			name:    "standard type mapped to itself stands for itself",
			roleMap: "<< /Document /Document >>", typeName: "Document",
			wantType: "Document", wantOK: true,
		},
		{
			// veraPDF resolves the kid to Sect, so the root holds no Document.
			name:    "standard type remapped to another standard type",
			roleMap: "<< /Document /Sect >>", typeName: "Document",
			wantType: "Sect", wantOK: true,
		},
		{
			// A chain that returns to its start reaches no standard type.
			name:    "cycle through the legacy map resolves to nothing",
			roleMap: "<< /Document /Sect /Sect /Document >>", typeName: "Document",
			wantOK: false,
		},
		{
			// The legacy map governs the PDF 1.7 namespace too.
			name:    "PDF 1.7 namespace honours the legacy map",
			roleMap: "<< /Document /Book >>", typeName: "Document", ns: roleMapTestPDF1,
			wantOK: false,
		},
		{
			// It never governs the PDF 2.0 namespace: the entry stays inert, and
			// veraPDF reports no 8.2.5.2 violation at all on that shape.
			name:    "PDF 2.0 namespace leaves the legacy map inert",
			roleMap: "<< /Document /Book >>", typeName: "Document", ns: roleMapTestPDF2,
			wantType: "Document", wantNS: roleMapTestPDF2, wantOK: true,
		},
		{
			// A namespace's own /RoleMapNS governs a standard type in it, which
			// the legacy map does not. veraPDF reports 8.2.5.2 t1 on this shape.
			name:      "namespaced role map remaps a standard type",
			roleMapNS: nsMapsDocumentToSect,
			roleMap:   "<< >>", typeName: "Document", ns: roleMapTestPDF2,
			wantType: "Sect", wantNS: roleMapTestPDF1, wantOK: true,
		},
		{
			// An unmapped standard type still stands for itself.
			name:    "unmapped standard type is unaffected",
			roleMap: "<< /MyDiv /Div >>", typeName: "Document",
			wantType: "Document", wantOK: true,
		},
		{
			// The ordinary use of the legacy map keeps working.
			name:    "non-standard type mapped to a standard one",
			roleMap: "<< /MyDiv /Div >>", typeName: "MyDiv",
			wantType: "Div", wantOK: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			doc, root := roleMapDoc(t, c.roleMap, c.roleMapNS)
			r := newNSRoleResolver(ctx, doc, root)
			gotType, gotNS, ok := r.resolveStandardType(ctx, doc, c.typeName, c.ns)
			if gotType != c.wantType || gotNS != c.wantNS || ok != c.wantOK {
				t.Errorf("resolveStandardType(%q, %q) = (%q, %q, %v), want (%q, %q, %v)",
					c.typeName, c.ns, gotType, gotNS, ok, c.wantType, c.wantNS, c.wantOK)
			}
		})
	}
}

// TestSingleDocumentRootRemappedStandardType pins the verdict the resolution
// order decides: a root whose only child is a Document that the legacy /RoleMap
// remaps holds no Document, so ISO 14289-2 8.2.5.2 t1 fires and t2 does not.
// veraPDF 1.30.2 reports exactly that, and reported the opposite test to litho
// on verapdf/PDF_UA-1__7.1-t07-fail-a.pdf.
func TestSingleDocumentRootRemappedStandardType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, root := roleMapDoc(t, "<< /Document /Book >>", "")
	var got []Violation
	checkSingleDocumentRoot(ctx, doc, root, &got)

	if !hasRule(got, Rule_14289_2_8_2_5_2_t01) {
		t.Errorf("Document remapped to Book: want 8.2.5.2 t1, got %v", got)
	}
	if hasRule(got, Rule_14289_2_8_2_5_2_t02) {
		t.Errorf("Document remapped to Book: want no 8.2.5.2 t2, got %v", got)
	}
}
