package annotation

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestExtractFormData_SharedKidsArrayFanOut pins that the form field walk
// reads a /Kids array written as an object of its own once per path: its
// direct fields each name it again as their /Kids, and read anew wherever it
// is reached the walk recursed until the goroutine stack was exhausted. The
// terminal field beside the repeat is still reported.
func TestExtractFormData_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()

	_, doc := widgetDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"[<< /T (a) /FT /Tx /V (x) /Kids 4 0 R >> << /T (b) /FT /Tx /V (y) >>]",
		"<< /T (p) /Kids 4 0 R >>",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fields, err := ExtractFormData(ctx, doc)
	if err != nil {
		t.Fatalf("ExtractFormData: %v", err)
	}
	got := make(map[string]string, len(fields))
	for _, f := range fields {
		got[f.Name] = f.Value
	}
	if got["p.b"] != "y" {
		t.Errorf("p.b = %q, want %q: %v", got["p.b"], "y", got)
	}
}

// TestExtractFormData_DistinctNamesFanOut pins that fields giving a shared
// child two distinct names cannot fan out 2^depth. Each level lists two named
// fields whose /Kids both name the next level, so the walk, which reports a
// field once per fully qualified name, meets 2^depth names in a file that
// grows linearly with the depth. The walk stops at maxFieldTreeVisits.
func TestExtractFormData_DistinctNamesFanOut(t *testing.T) {
	t.Parallel()

	const levels = 40
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	// Level i is object 4+3i, a nameless node whose /Kids lists the two named
	// fields 5+3i and 6+3i, and both of those list level i+1.
	for i := range levels {
		node := 4 + 3*i
		next := node + 3
		objs = append(objs,
			fmt.Sprintf("<< /Kids [%d 0 R %d 0 R] >>", node+1, node+2),
			fmt.Sprintf("<< /T (a) /Kids [%d 0 R] >>", next),
			fmt.Sprintf("<< /T (b) /Kids [%d 0 R] >>", next),
		)
	}
	objs = append(objs, "<< /T (leaf) /FT /Tx /V (v) >>")
	_, doc := widgetDoc(t, objs)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := ExtractFormFields(ctx, doc); err != nil {
		t.Fatalf("ExtractFormFields: %v", err)
	}
}
