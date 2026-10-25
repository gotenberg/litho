package graphics

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestNoteUnparsedForm(t *testing.T) {
	t.Parallel()

	box := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(100), ast.PdfInt(50)}
	res := ast.PdfDict{"XObject": ast.PdfDict{}}

	t.Run("not recorded without image boxes", func(t *testing.T) {
		t.Parallel()
		m := NewMachine([4]float64{0, 0, 612, 792})
		m.NoteUnparsedForm(9, res, box, nil)
		if got := m.UnparsedForms(); len(got) != 0 {
			t.Errorf("UnparsedForms = %v, want none", got)
		}
	})

	t.Run("bbox through matrix and CTM", func(t *testing.T) {
		t.Parallel()
		m := NewMachine([4]float64{0, 0, 612, 792})
		m.RecordImageBoxes = true
		m.Current().CTM = [6]float64{1, 0, 0, 1, 300, 500}
		// A quarter turn: the form's x runs up the page.
		matrix := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(-1), ast.PdfInt(0), ast.PdfInt(10), ast.PdfInt(0)}
		m.NoteUnparsedForm(9, res, box, matrix)
		got := m.UnparsedForms()
		if len(got) != 1 {
			t.Fatalf("UnparsedForms = %v, want one", got)
		}
		f := got[0]
		if f.ObjNum != 9 || f.Unbounded || f.MinX != 260 || f.MaxX != 310 || f.MinY != 500 || f.MaxY != 600 {
			t.Errorf("UnparsedForms[0] = %+v, want object 9 bounded by [260 500 310 600]", f)
		}
	})

	t.Run("no usable bbox is unbounded", func(t *testing.T) {
		t.Parallel()
		m := NewMachine([4]float64{0, 0, 612, 792})
		m.RecordImageBoxes = true
		m.NoteUnparsedForm(9, res, ast.PdfArray{ast.PdfInt(0), ast.PdfName("X")}, nil)
		if got := m.UnparsedForms(); len(got) != 1 || !got[0].Unbounded {
			t.Errorf("UnparsedForms = %v, want one unbounded form", got)
		}
	})
}
