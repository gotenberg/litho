package remediation

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// roleMapPDF builds a minimal tagged PDF whose StructTreeRoot (object 4) carries
// the given inline /RoleMap and, when namespaced, a /Namespaces array, so
// fixRoleMap can be exercised end to end.
func roleMapPDF(roleMap string, namespaced bool) []byte {
	st := "<< /Type /StructTreeRoot /RoleMap " + roleMap
	if namespaced {
		st += " /Namespaces [5 0 R]"
	}
	st += " >>"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		st,
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
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

// resolvedRoleMap returns the /RoleMap that fixRoleMap wrote back onto the
// StructTreeRoot (object 4), or an empty dict when the fixer removed it.
func resolvedRoleMap(t *testing.T, alloc *writer.ObjAllocator) ast.PdfDict {
	t.Helper()
	st := pendingDict(t, alloc, 4)
	rmObj, ok := st["RoleMap"]
	if !ok {
		return ast.PdfDict{}
	}
	rm, ok := rmObj.(ast.PdfDict)
	if !ok {
		t.Fatalf("RoleMap: got %T, want ast.PdfDict", rmObj)
	}
	return rm
}

func anyFixContains(fixes []string, sub string) bool {
	for _, f := range fixes {
		if strings.Contains(f, sub) {
			return true
		}
	}
	return false
}

func TestFixRoleMap_BreaksMultiHopCycle_KeepsNamespacedStandard(t *testing.T) {
	// Namespaced (PDF 2.0 / PDF/UA-2) document: a 2-cycle A<->B, a valid
	// custom->standard mapping, and a standard-NAMED mapping (Title->P). The
	// cycle is broken; both the custom and the standard-named entries are kept
	// because a standard name may belong to a custom namespace here.
	data := roleMapPDF("<< /A /B /B /A /Custom /P /Title /P >>", true)
	doc, alloc := newTestDoc(t, data)

	fixes, err := fixRoleMap(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixRoleMap: %v", err)
	}
	rm := resolvedRoleMap(t, alloc)

	if _, a := rm["A"]; a {
		if _, b := rm["B"]; b {
			t.Errorf("circular mapping A<->B was not broken: %v", rm)
		}
	}
	if rm["Custom"] != ast.PdfName("P") {
		t.Errorf("custom->standard mapping should be kept: %v", rm)
	}
	if rm["Title"] != ast.PdfName("P") {
		t.Errorf("standard-named mapping should be kept in a namespaced document: %v", rm)
	}
	if !anyFixContains(fixes, "circular") {
		t.Errorf("expected a circular-mapping fix description, got %v", fixes)
	}
}

func TestFixRoleMap_DropsStandardSourceAndSelfLoop_NonNamespaced(t *testing.T) {
	// Legacy (no /Namespaces) document: a standard-typed source (Title) is
	// dropped per PDF/A-2a §7.1, and a self-loop (X->X) is dropped as a
	// 1-cycle. A plain custom mapping is preserved.
	data := roleMapPDF("<< /Title /P /X /X /Custom /P >>", false)
	doc, alloc := newTestDoc(t, data)

	if _, err := fixRoleMap(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2A"); err != nil {
		t.Fatalf("fixRoleMap: %v", err)
	}
	rm := resolvedRoleMap(t, alloc)

	if _, ok := rm["Title"]; ok {
		t.Errorf("standard source Title should be dropped in a non-namespaced document: %v", rm)
	}
	if _, ok := rm["X"]; ok {
		t.Errorf("self-loop X->X should be dropped: %v", rm)
	}
	if rm["Custom"] != ast.PdfName("P") {
		t.Errorf("custom->standard mapping should be kept: %v", rm)
	}
}
