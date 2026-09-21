package mutations

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// kidsCyclePDF gives the interactive form a field whose /Kids names the field
// itself. findAndSetField descends /Kids looking for a fully qualified name, so
// a target that never matches walks the whole tree.
//
// The cycle costs more than stack: every level appends the partial name, so the
// qualified name grows "outer.outer.outer..." one segment per frame.
func kidsCycleObjs() []string {
	return []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /FT /Tx /T (outer) /Kids [4 0 R] >>",
	}
}

// TestSetFormField_KidsCycleTerminates pins that a self-naming /Kids is walked
// once rather than forever.
//
// The existing forms_cycle_test.go covers the /Parent ASCENT only; its fixture
// carries no /Kids cycle, so this descent was never exercised.
func TestSetFormField_KidsCycleTerminates(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := setFormDoc(t, kidsCycleObjs())

	// A name the document does not carry, so the walk cannot exit early.
	err := SetFormField(ctx, alloc, doc, "no-such-field", "bob")
	if err == nil {
		t.Fatal("setting an absent field reported success")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want it to say the field was not found", err)
	}
}

// TestSetFormField_NestedKidStillSettable is the control. A guard that refused
// to descend would satisfy the test above while breaking every real form whose
// field tree has more than one level.
func TestSetFormField_NestedKidStillSettable(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := setFormDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /T (outer) /Kids [5 0 R] >>",
		"<< /FT /Tx /T (inner) >>",
	})

	if err := SetFormField(ctx, alloc, doc, "outer.inner", "bob"); err != nil {
		t.Fatalf("SetFormField on a nested field: %v", err)
	}
	field, ok := alloc.Pending()[5].(ast.PdfDict)
	if !ok {
		t.Fatal("the nested field was not rewritten")
	}
	if got, _ := field["V"].(ast.PdfString); string(got) != "bob" {
		t.Errorf("/V = %q, want %q: the descent no longer reaches the kid", got, "bob")
	}
}

// TestSetFormField_SharedSubtreeFoundByTheSecondPath pins why the visited set
// must be STACK-SCOPED rather than persistent.
//
// findAndSetField matches on the FULLY QUALIFIED name, which depends on the
// path taken: object 6 is "a.mid" under field 4 and "b.mid" under field 5. A
// mark that is never cleared would skip object 6 on the second path and make
// "b.mid.leaf" unfindable, turning a DoS guard into a silent functional
// regression in form filling.
//
// The shape matters. An earlier draft listed one kid twice under a single
// parent and did NOT discriminate: the target matched the first occurrence and
// the function returned before reaching the duplicate, so a persistent set
// passed too. The mark lands on the node whose /Kids is descended, so the case
// has to be a node WITH kids reached by two paths, with the target matching
// only via the second.
func TestSetFormField_SharedSubtreeFoundByTheSecondPath(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := setFormDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /T (a) /Kids [6 0 R] >>",
		"<< /T (b) /Kids [6 0 R] >>",
		"<< /T (mid) /Kids [7 0 R] >>",
		"<< /FT /Tx /T (leaf) >>",
	})

	if err := SetFormField(ctx, alloc, doc, "b.mid.leaf", "bob"); err != nil {
		t.Fatalf("SetFormField on the second path: %v", err)
	}
	field, ok := alloc.Pending()[7].(ast.PdfDict)
	if !ok {
		t.Fatal("the leaf was not rewritten: the shared subtree was skipped on the second path")
	}
	if got, _ := field["V"].(ast.PdfString); string(got) != "bob" {
		t.Errorf("/V = %q, want %q", got, "bob")
	}
}
