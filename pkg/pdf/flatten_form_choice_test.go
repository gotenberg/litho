package pdf

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// TestFlattenFormLeavesNoField pins that flattening a form whose list box
// holds a multiple selection and no appearance stream leaves no form field,
// and draws the selection into the page. The list box used to survive the
// flatten, and the document still reported a form.
func TestFlattenFormLeavesNoField(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data, err := os.ReadFile("testdata/forms/multi.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	if has, err := r.HasFormFields(ctx); err != nil || !has {
		t.Fatalf("HasFormFields on the fixture = %v, %v, want a form", has, err)
	}

	w := NewWriter(r)
	defer w.Close()
	w.FlattenForm()
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer got.Close()
	if has, err := got.HasFormFields(ctx); err != nil || has {
		t.Errorf("HasFormFields after the flatten = %v, %v, want no form", has, err)
	}
	fields, err := got.FormFields(ctx)
	if err != nil {
		t.Fatalf("FormFields: %v", err)
	}
	if len(fields) != 0 {
		t.Errorf("the flatten left fields %+v", fields)
	}
	text, err := got.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "a, c") {
		t.Errorf("page 1 reads %q, want the list box's selection a, c drawn on it", text)
	}
}
