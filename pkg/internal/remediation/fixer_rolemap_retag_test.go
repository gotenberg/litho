package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestResolvedStructTypeFollowsRoleMap pins that a fixer judging a child by role
// resolves it the way the validator does.
//
// A fixer reading the raw /S disagrees with the rule it repairs. govdocs
// thread_100_100631.pdf maps /LI_Title to /LBody: the list fixer saw an
// unrecognised child, treated an existing list body as an orphan, and wrapped it
// in a second LBody, leaving an LBody parented by an LBody and drawing
// ISO 14289-1 §7.2 test 18, which the input never had.
func TestResolvedStructTypeFollowsRoleMap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := &document.Document{Logger: document.NopLogger()}

	roleMap := map[ast.PdfName]ast.PdfName{
		"LI_Title": "LBody",
		"LI_Label": "Lbl",
		"Chain":    "Middle",
		"Middle":   "P",
		"Loop":     "Loop",
	}

	cases := []struct {
		name string
		s    ast.PdfName
		want ast.PdfName
	}{
		{"a mapped name resolves to its target", "LI_Title", "LBody"},
		{"a second mapped name resolves too", "LI_Label", "Lbl"},
		{"an unmapped standard name is itself", "LBody", "LBody"},
		{"an unmapped custom name is itself", "Whatever", "Whatever"},
		{"a chain resolves to its end", "Chain", "P"},
		{"a self-mapping terminates", "Loop", "Loop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			elem := ast.PdfDict{"S": tc.s}
			if got := resolvedStructType(ctx, doc, elem, roleMap); got != tc.want {
				t.Errorf("resolvedStructType(%q) = %q, want %q", tc.s, got, tc.want)
			}
		})
	}
}

// TestResolvedStructTypeNilRoleMap pins that a document with no /RoleMap is
// judged by its raw /S, unchanged.
func TestResolvedStructTypeNilRoleMap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := &document.Document{Logger: document.NopLogger()}

	if got := resolvedStructType(ctx, doc, ast.PdfDict{"S": ast.PdfName("LBody")}, nil); got != "LBody" {
		t.Errorf("with no role map the raw /S stands, got %q", got)
	}
}

// TestResolvedStructTypeCycleTerminates pins that a multi-hop /RoleMap cycle
// does not hang. The rolemap fixer breaks such cycles, but this must not depend
// on having run: fixer order is registry order, and a caller may reach here on a
// document whose map was never cleaned.
func TestResolvedStructTypeCycleTerminates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := &document.Document{Logger: document.NopLogger()}

	roleMap := map[ast.PdfName]ast.PdfName{"A": "B", "B": "C", "C": "A"}
	got := resolvedStructType(ctx, doc, ast.PdfDict{"S": ast.PdfName("A")}, roleMap)
	if got != "A" && got != "B" && got != "C" {
		t.Errorf("a cycle must terminate on one of its members, got %q", got)
	}
}

// retagPDF builds a non-namespaced document whose structure tree holds one
// element of the given role, so the retag pass has something to rewrite.
func retagPDF(roleMap, elemRole string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /RoleMap " + roleMap + " /K [5 0 R] >>",
		"<< /Type /StructElem /S /" + elemRole + " /P 4 0 R >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// roleOfElement returns /S on the structure element (object 5) as the fixer left
// it.
func roleOfElement(t *testing.T, alloc *writer.ObjAllocator) ast.PdfName {
	t.Helper()
	obj, ok := alloc.Pending()[5]
	if !ok {
		return "" // untouched: the fixer rewrote nothing for this element
	}
	d, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("object 5 is %T, want a dict", obj)
	}
	name, _ := d["S"].(ast.PdfName)
	return name
}

// TestFixRoleMap_RetagsOnlyTowardsStandardTargets pins both halves of the retag.
//
// Dropping a standard-type mapping un-maps every element that used it, which
// silently changes what the document says. Retagging to the declared target
// keeps that meaning, but ONLY when the target is itself standard: veraPDF's
// PDF_UA-1__7.1-t07-fail-a.pdf maps /Document to the CUSTOM /Book, and
// rewriting the element to /Book left it an unmapped type, turning a compliant
// repair into an ISO 14289-1 §7.1 test 5 failure. The corpus gate still passed;
// only the count against the previous run showed it.
func TestFixRoleMap_RetagsOnlyTowardsStandardTargets(t *testing.T) {
	t.Parallel()

	t.Run("a standard target is retagged", func(t *testing.T) {
		t.Parallel()
		doc, alloc := newTestDoc(t, retagPDF("<< /H4 /P >>", "H4"))
		if _, err := fixRoleMap(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
			t.Fatalf("fixRoleMap: %v", err)
		}
		if got := roleOfElement(t, alloc); got != "P" {
			t.Errorf("element should be retagged to the declared target P, got %q", got)
		}
	})

	t.Run("a custom target is left alone", func(t *testing.T) {
		t.Parallel()
		doc, alloc := newTestDoc(t, retagPDF("<< /Document /Book >>", "Document"))
		if _, err := fixRoleMap(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
			t.Fatalf("fixRoleMap: %v", err)
		}
		// Retagging to /Book would leave an element of an unmapped custom type.
		if got := roleOfElement(t, alloc); got != "" && got != "Document" {
			t.Errorf("element must stay /Document, got %q", got)
		}
		rm := resolvedRoleMap(t, alloc)
		if _, ok := rm["Document"]; ok {
			t.Errorf("the standard-type mapping should still be dropped: %v", rm)
		}
	})
}

// tableSectionPDF builds a Table carrying a header section named by headRole and
// two body rows directly under the Table, with no TBody: ISO 14289-1 §7.2 t14.
func tableSectionPDF(headRole, roleMap string) []byte {
	st := "<< /Type /StructTreeRoot /K [5 0 R]"
	if roleMap != "" {
		st += " /RoleMap " + roleMap
	}
	st += " >>"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		st,
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R 7 0 R 8 0 R] >>",
		"<< /Type /StructElem /S /" + headRole + " /P 5 0 R >>",
		"<< /Type /StructElem /S /TR /P 5 0 R >>",
		"<< /Type /StructElem /S /TR /P 5 0 R >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// TestFixTableSections_ResolvesRoleMappedSections pins that the table fixer sees
// a section named through the /RoleMap.
//
// The validator resolves roles, so both documents below draw ISO 14289-1 §7.2
// t14; reading the raw /S made the fixer repair only the literal one and decline
// the mapped one, leaving a violation it was supposed to answer.
func TestFixTableSections_ResolvesRoleMappedSections(t *testing.T) {
	t.Parallel()

	wrapped := func(t *testing.T, data []byte) int {
		t.Helper()
		doc, alloc := newTestDoc(t, data)
		fixes, err := fixTableSections(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
		if err != nil {
			t.Fatalf("fixTableSections: %v", err)
		}
		return len(fixes)
	}

	t.Run("a literal THead is repaired", func(t *testing.T) {
		t.Parallel()
		if n := wrapped(t, tableSectionPDF("THead", "")); n != 1 {
			t.Errorf("a literal THead must draw the TBody repair, got %d fixes", n)
		}
	})

	t.Run("a role-mapped THead is repaired too", func(t *testing.T) {
		t.Parallel()
		if n := wrapped(t, tableSectionPDF("TableHead", "<< /TableHead /THead >>")); n != 1 {
			t.Errorf("a role-mapped THead must draw the same repair, got %d fixes", n)
		}
	})

	t.Run("no section means no repair", func(t *testing.T) {
		t.Parallel()
		// Without a THead or TFoot the rows are not misplaced, so the fixer
		// must stay out: this is the control that keeps the check honest.
		if n := wrapped(t, tableSectionPDF("TR", "")); n != 0 {
			t.Errorf("a table with no header section must not be repaired, got %d fixes", n)
		}
	})
}
