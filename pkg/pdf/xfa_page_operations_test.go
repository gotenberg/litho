package pdf

import (
	"bytes"
	"context"
	"testing"
)

// TestReader_HasXFAFormAsksForXFANotForAForm pins what HasXFAForm answers: true
// on a hybrid form carrying /AcroForm /XFA, false on an AcroForm of widgets
// alone and on a page with no form.
func TestReader_HasXFAFormAsksForXFANotForAForm(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		data []byte
		want bool
	}{
		{"an XFA form", hybridXFAFormPDF(), true},
		{"an AcroForm", formPDF(), false},
		{"a plain page", untaggedHeadingPDF(), false},
	} {
		if got := openColorPDF(t, tc.data).HasXFAForm(t.Context()); got != tc.want {
			t.Errorf("%s: HasXFAForm = %t, want %t", tc.name, got, tc.want)
		}
	}
}

// TestPageOperations_DropXFA pins what [Reader.HasXFAForm] warns a caller of:
// Split and Merge copy pages and their widgets, not the catalog's /XFA, so
// their output carries no XFA, whether alone or as a pipeline stage and
// whichever merged document carries it. A stage that edits the document in
// place, a rotation, keeps it.
func TestPageOperations_DropXFA(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		write func(t *testing.T, xfa, plain *Reader) []byte
		want  bool
	}{
		{"Split", func(t *testing.T, xfa, _ *Reader) []byte {
			var out bytes.Buffer
			if err := Split(t.Context(), xfa, []int{1}, &out); err != nil {
				t.Fatalf("Split: %v", err)
			}
			return out.Bytes()
		}, false},
		{"Merge, the XFA form first", func(t *testing.T, xfa, plain *Reader) []byte {
			var out bytes.Buffer
			if err := Merge(t.Context(), []*Reader{xfa, plain}, &out); err != nil {
				t.Fatalf("Merge: %v", err)
			}
			return out.Bytes()
		}, false},
		{"Merge, the XFA form second", func(t *testing.T, xfa, plain *Reader) []byte {
			var out bytes.Buffer
			if err := Merge(t.Context(), []*Reader{plain, xfa}, &out); err != nil {
				t.Fatalf("Merge: %v", err)
			}
			return out.Bytes()
		}, false},
		{"Pipeline.Split", func(t *testing.T, xfa, _ *Reader) []byte {
			return runPipeline(t, NewPipeline().Open(xfa).Split([]int{1}))
		}, false},
		{"Pipeline.Merge, the XFA form opened", func(t *testing.T, xfa, plain *Reader) []byte {
			return runPipeline(t, NewPipeline().Open(xfa).Merge([]*Reader{plain}))
		}, false},
		{"Pipeline.Merge, the XFA form merged", func(t *testing.T, xfa, plain *Reader) []byte {
			return runPipeline(t, NewPipeline().Open(plain).Merge([]*Reader{xfa}))
		}, false},
		{"Pipeline.Apply(RotatePages)", func(t *testing.T, xfa, _ *Reader) []byte {
			return runPipeline(t, NewPipeline().Open(xfa).Apply(func(_ context.Context, w *Writer) error {
				return w.RotatePages(90)
			}))
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out := tc.write(t, openColorPDF(t, hybridXFAFormPDF()), openColorPDF(t, untaggedHeadingPDF()))
			if got := openColorPDF(t, out).HasXFAForm(t.Context()); got != tc.want {
				t.Errorf("the output's HasXFAForm = %t, want %t", got, tc.want)
			}
		})
	}
}
