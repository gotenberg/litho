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
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n\r\n", offsets[i])
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
	// cycle is broken by remapping, not by deleting: a deleted entry leaves
	// every other entry aimed at it pointing at an unmapped name. Both the
	// custom and the standard-named entries are kept because a standard name
	// may belong to a custom namespace here.
	data := roleMapPDF("<< /A /B /B /A /Custom /P /Title /P >>", true)
	doc, alloc := newTestDoc(t, data)

	fixes, err := fixRoleMap(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixRoleMap: %v", err)
	}
	rm := resolvedRoleMap(t, alloc)

	if rm["A"] == ast.PdfName("B") && rm["B"] == ast.PdfName("A") {
		t.Errorf("circular mapping A<->B was not broken: %v", rm)
	}
	if rm["A"] != ast.PdfName("P") {
		t.Errorf("the entry closing the cycle should be remapped to a standard type: %v", rm)
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
	// Legacy (no /Namespaces) PDF/A-2 document. Standardness is profile-aware and
	// shared with the compliance checker: Div is standard under PDF/A-2, so its
	// redundant Div->P mapping is dropped; Title was introduced by ISO 32000-2 and
	// is NON-standard under PDF/A-2, so its Title->P mapping is a required repair
	// and is kept. A self-loop X->X is remapped to P (not dropped), so any entry
	// targeting X still terminates at a standard type. A custom mapping is kept.
	data := roleMapPDF("<< /Div /P /X /X /Custom /P /Title /P >>", false)
	doc, alloc := newTestDoc(t, data)

	if _, err := fixRoleMap(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2A"); err != nil {
		t.Fatalf("fixRoleMap: %v", err)
	}
	rm := resolvedRoleMap(t, alloc)

	if _, ok := rm["Div"]; ok {
		t.Errorf("standard source Div should be dropped in a non-namespaced document: %v", rm)
	}
	if rm["X"] != ast.PdfName("P") {
		t.Errorf("self-loop X->X should be remapped to P, not dropped: %v", rm)
	}
	if rm["Title"] != ast.PdfName("P") {
		t.Errorf("Title is non-standard under PDF/A-2; Title->P must be kept: %v", rm)
	}
	if rm["Custom"] != ast.PdfName("P") {
		t.Errorf("custom->standard mapping should be kept: %v", rm)
	}
}

// TestFixRoleMap_CycleRemapAvoidsOrphan pins the repair of a non-namespaced
// 2-cycle A<->B, where B is also targeted by the cycle. Dropping the edge that
// closes the cycle would leave the other entry pointing at an unmapped name
// (ISO 14289-1 §7.1 t5). The fix remaps the closing source to P so every entry
// terminates at a standard type. Title, non-standard under PDF/UA-1, keeps its
// mapping.
//
// # Corpus evidence
//
//   - verapdf 7.1-t05-fail-d: carries this 2-cycle.
func TestFixRoleMap_CycleRemapAvoidsOrphan(t *testing.T) {
	data := roleMapPDF("<< /A /B /B /A /Title /P >>", false)
	doc, alloc := newTestDoc(t, data)

	if _, err := fixRoleMap(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixRoleMap: %v", err)
	}
	rm := resolvedRoleMap(t, alloc)

	if rm["A"] != ast.PdfName("P") {
		t.Errorf("cycle-closing source A should be remapped to P, not dropped: %v", rm)
	}
	if rm["B"] != ast.PdfName("A") {
		t.Errorf("B->A should be preserved and reach a standard type via A->P: %v", rm)
	}
	if rm["Title"] != ast.PdfName("P") {
		t.Errorf("Title is non-standard under PDF/UA-1; Title->P must be kept: %v", rm)
	}
}

// TestFixRoleMap_BreaksCycleAtItsStandardType pins the repair of a cycle
// through a standard and a private type, /Sect -> /Book -> /Sect, which the
// sorted walk enters at the private /Book. The cycle is broken by dropping the
// standard /Sect entry, so a Sect reads as the Sect it names and /Book leads to
// it; sending /Book to /P sent every Sect on to a type it never claimed.
//
// # Corpus evidence
//
//   - litho rolecycle_ua1 and rolecycle_ua2: carry this cycle.
func TestFixRoleMap_BreaksCycleAtItsStandardType(t *testing.T) {
	for _, tc := range []struct {
		profile    string
		namespaced bool
	}{
		{"PDF/UA-1", false},
		{"PDF/A-2a", false},
		{"PDF/UA-2", true},
	} {
		data := roleMapPDF("<< /Sect /Book /Book /Sect /Item /Sect >>", tc.namespaced)
		doc, alloc := newTestDoc(t, data)

		fixes, err := fixRoleMap(context.Background(), alloc, doc, Options{}, nil, tc.profile)
		if err != nil {
			t.Fatalf("%s: fixRoleMap: %v", tc.profile, err)
		}
		rm := resolvedRoleMap(t, alloc)

		if _, ok := rm["Sect"]; ok {
			t.Errorf("%s: the standard /Sect entry closing the cycle should be dropped: %v", tc.profile, rm)
		}
		if rm["Book"] != ast.PdfName("Sect") {
			t.Errorf("%s: /Book -> /Sect should be kept, ending on the standard /Sect: %v", tc.profile, rm)
		}
		if rm["Item"] != ast.PdfName("Sect") {
			t.Errorf("%s: /Item -> /Sect should be kept: %v", tc.profile, rm)
		}
		if !anyFixContains(fixes, "circular") {
			t.Errorf("%s: expected a circular-mapping fix description, got %v", tc.profile, fixes)
		}
	}
}

// TestFixRoleMap_DropsDeadEndStandardRemap_Namespaced pins the PDF/UA-2 repair
// of ISO 14289-2 §8.2.4 t4 in a namespaced document: a standard type whose
// /RoleMap chain reaches no standard type (Sect -> Book, Div -> Foo -> Bar)
// loses its entry, while one whose chain ends on a standard type
// (Note -> Foo2 -> P) and a private type's mapping keep theirs.
//
// # Corpus evidence
//
//   - litho stdremap_ua2: its PDF 1.7 Sect read as Book until the entry went,
//     and veraPDF reported 8.2.4 t4 on every PDF/UA-2 and WTPDF output.
func TestFixRoleMap_DropsDeadEndStandardRemap_Namespaced(t *testing.T) {
	data := roleMapPDF("<< /Sect /Book /Div /Foo /Foo /Bar /Note /Foo2 /Foo2 /P /Custom /P >>", true)
	doc, alloc := newTestDoc(t, data)

	fixes, err := fixRoleMap(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixRoleMap: %v", err)
	}
	rm := resolvedRoleMap(t, alloc)

	for _, gone := range []ast.PdfName{"Sect", "Div"} {
		if _, ok := rm[gone]; ok {
			t.Errorf("%s leads to no standard type and should be dropped: %v", gone, rm)
		}
	}
	for src, want := range map[ast.PdfName]ast.PdfName{"Note": "Foo2", "Foo2": "P", "Custom": "P", "Foo": "Bar"} {
		if rm[src] != want {
			t.Errorf("%s -> %s should be kept: %v", src, want, rm)
		}
	}
	if !anyFixContains(fixes, "no standard type") {
		t.Errorf("expected a dead-end fix description, got %v", fixes)
	}
}
