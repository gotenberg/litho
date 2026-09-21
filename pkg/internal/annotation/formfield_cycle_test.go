package annotation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// formDataOf flattens the reported fields to a name/value map, the shape
// pkg/pdf's Reader.FormData builds from the same slice.
func formDataOf(t *testing.T, ctx context.Context, doc *document.Document) map[string]string {
	t.Helper()

	fields, err := ExtractFormData(ctx, doc)
	if err != nil {
		t.Fatalf("ExtractFormData: %v", err)
	}
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		out[f.Name] = f.Value
	}
	return out
}

// TestExtractFormData_SelfReferencingKidTerminates pins that a form field whose
// /Kids names itself is descended once rather than forever.
//
// Measured before the guard, through the public ExtractFormData path:
// "fatal error: stack overflow", frames traverseFields. That is a fatal runtime
// error, not a panic, so the recover() at Reader.FormData cannot convert it.
//
// The field tree's own seen map did not bound this: it de-duplicates the
// reported order, is keyed on the qualified name, and a cyclic /Kids grows that
// name at every level, so it can never match.
func TestExtractFormData_SelfReferencingKidTerminates(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /T (a) /Kids [4 0 R] >>",
	})

	if _, err := ExtractFormData(ctx, doc); err != nil {
		t.Fatalf("extracting from a cyclic field tree: %v", err)
	}
}

// TestExtractFormData_OrdinaryFormStillRead is the control. A guard that
// refused to descend would satisfy the test above while reporting no value for
// any nested field in a real document.
func TestExtractFormData_OrdinaryFormStillRead(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /T (outer) /Kids [5 0 R] >>",
		"<< /FT /Tx /T (inner) /V (alice) >>",
	})

	if got := formDataOf(t, ctx, doc)["outer.inner"]; got != "alice" {
		t.Errorf("outer.inner = %q, want %q: the descent no longer reaches the kid", got, "alice")
	}
}

// TestExtractFormData_SharedSubtreeReportedUnderBothNames pins why the set must
// be STACK-SCOPED rather than persistent.
//
// Object 6 is reached under field 4 as "a.mid" and under field 5 as "b.mid",
// and both subtrees are real entries because this walk collects per fully
// qualified name. A mark that was never cleared would skip object 6 on the
// second path and silently drop "b.mid.leaf", turning a DoS guard into lost
// form data.
//
// The SHARED NODE MUST ITSELF HAVE /Kids. The mark is taken inside the /Kids
// block, for a node about to be descended into, so a shared LEAF is never
// marked and a two-level fixture cannot tell the two designs apart: it passes
// under both. Object 6 therefore carries object 7 rather than being the leaf.
func TestExtractFormData_SharedSubtreeReportedUnderBothNames(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /T (a) /Kids [6 0 R] >>",
		"<< /T (b) /Kids [6 0 R] >>",
		"<< /T (mid) /Kids [7 0 R] >>",
		"<< /FT /Tx /T (leaf) /V (v) >>",
	})

	data := formDataOf(t, ctx, doc)
	if got := data["a.mid.leaf"]; got != "v" {
		t.Errorf("a.mid.leaf = %q, want %q", got, "v")
	}
	if got := data["b.mid.leaf"]; got != "v" {
		t.Errorf("b.mid.leaf = %q, want %q: the shared subtree was skipped on the second path", got, "v")
	}
}
