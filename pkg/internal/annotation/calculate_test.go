package annotation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// calculationPDF builds a one-page document whose form has a text field "a"
// storing "stored" and a text field "b" storing "kept", whose calculate action
// runs script.
func calculationPDF(t *testing.T, script string) *document.Document {
	t.Helper()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (a) /V (stored) /Rect [0 0 10 10] >>",
		fmt.Sprintf("<< /Type /Annot /Subtype /Widget /FT /Tx /T (b) /V (kept) /Rect [0 20 10 30]"+
			" /AA << /C << /S /JavaScript /JS (%s) >> >> >>", script),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// fieldValue returns the value ExtractFormFields reports for name.
func fieldValue(t *testing.T, ctx context.Context, doc *document.Document, name string) (string, error) {
	t.Helper()
	fields, err := ExtractFormFields(ctx, doc)
	if err != nil {
		return "", err
	}
	for _, f := range fields {
		if f.Name == name {
			return f.Value, nil
		}
	}
	t.Fatalf("no field %q among %v", name, fields)
	return "", nil
}

// endlessScripts are calculate actions that never end, in the script itself
// and in the read of the value it leaves, a toString litho calls.
var endlessScripts = map[string]string{
	"a loop":                `while\(true\){}`,
	"a looping event value": `event.value = {toString: function\(\){ while\(true\){} }}`,
}

// TestCalculationStopsWithTheContext pins that a calculation script that never
// ends returns when the caller's context is done, with its error. It ran until
// the process ended: a deadline three seconds out was ten seconds past and
// counting.
func TestCalculationStopsWithTheContext(t *testing.T) {
	t.Parallel()

	for name, script := range endlessScripts {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc := calculationPDF(t, script)
			const deadline = 100 * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), deadline)
			defer cancel()

			start := time.Now()
			_, err := fieldValue(t, ctx, doc, "b")
			if elapsed := time.Since(start); elapsed > deadline+maxCalculationTime/2 {
				t.Errorf("returned %v after start, past the %v deadline", elapsed, deadline)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("error = %v, want context.DeadlineExceeded", err)
			}
		})
	}
}

// TestCalculationStopsWithinItsBudget pins that a calculation script that never
// ends, under a context that never ends, stops at maxCalculationTime, and that
// the field keeps the value the document stores.
func TestCalculationStopsWithinItsBudget(t *testing.T) {
	t.Parallel()

	for name, script := range endlessScripts {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc := calculationPDF(t, script)
			start := time.Now()
			got, err := fieldValue(t, context.Background(), doc, "b")
			if elapsed := time.Since(start); elapsed > 3*maxCalculationTime {
				t.Errorf("returned %v after start, past the %v budget", elapsed, maxCalculationTime)
			}
			if err != nil {
				t.Fatalf("ExtractFormFields: %v", err)
			}
			if got != "kept" {
				t.Errorf("b = %q, want the stored %q", got, "kept")
			}
		})
	}
}

// TestCalculationStillComputes is the control: a calculation that ends sets
// its field from the field it reads.
func TestCalculationStillComputes(t *testing.T) {
	t.Parallel()

	doc := calculationPDF(t, `event.value = this.getField\("a"\).value + "!";`)
	got, err := fieldValue(t, context.Background(), doc, "b")
	if err != nil {
		t.Fatalf("ExtractFormFields: %v", err)
	}
	if got != "stored!" {
		t.Errorf("b = %q, want %q", got, "stored!")
	}
}

// TestCalculationOrderIsTheDocuments pins the order the scripts run in, which
// a calculation reading another's result depends on: the document's, then the
// names it does not list, sorted.
func TestCalculationOrderIsTheDocuments(t *testing.T) {
	t.Parallel()

	scripts := map[string]string{"z": "", "a": "", "m": "", "orphan2": "", "orphan1": ""}
	got := calculationOrder(scripts, []string{"z", "x", "m", "a", "m"})
	want := []string{"z", "m", "a", "orphan1", "orphan2"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("calculationOrder = %v, want %v", got, want)
	}
}
