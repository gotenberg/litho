package typeset_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// formDoc is a tagged document with a field of every kind, a radio button
// group of two buttons, two text fields of one name and a field on a
// second page, each widget under a Form element. Each field shows its
// value in its appearance.
func formDoc(t *testing.T) *typeset.Document {
	t.Helper()
	root := &typeset.StructElem{Role: "Document"}
	p := &typeset.StructElem{Role: "P"}
	root.Kids = append(root.Kids, p)
	form := func() *typeset.StructElem {
		e := &typeset.StructElem{Role: "Form"}
		p.Kids = append(p.Kids, e)
		return e
	}
	label := run(t, sans, 12, typeset.Point{X: 40, Y: 60}, "Name")
	label.Parent = p
	box := &typeset.Path{Ops: rect(100, 48, 150, 16), Stroke: &typeset.Stroke{Color: typeset.Black, Width: 0.5}}
	value := func(x, y float64, s string) []typeset.Fragment {
		return []typeset.Fragment{run(t, sans, 10, typeset.Point{X: x + 2, Y: y + 12}, s)}
	}
	mark := []typeset.Fragment{&typeset.Path{Ops: rect(42, 92, 8, 8), Fill: &typeset.Fill{Color: typeset.Black}}}
	page1 := letter(label, box)
	page1.Fields = []typeset.Field{
		{Kind: typeset.FieldText, Rect: typeset.Rect{X: 100, Y: 48, Width: 150, Height: 16}, Name: "name", Description: "Your name", Value: "Ada", FontSize: 10, MaxLen: 40, Appearance: value(100, 48, "Ada"), Parent: form()},
		{Kind: typeset.FieldCheckBox, Rect: typeset.Rect{X: 40, Y: 90, Width: 12, Height: 12}, Name: "agree", Checked: true, Appearance: mark, Parent: form()},
		{Kind: typeset.FieldRadio, Rect: typeset.Rect{X: 40, Y: 110, Width: 12, Height: 12}, Name: "color", OnState: "red", Appearance: mark, Parent: form()},
		{Kind: typeset.FieldRadio, Rect: typeset.Rect{X: 60, Y: 110, Width: 12, Height: 12}, Name: "color", OnState: "blue", Checked: true, Appearance: mark, Parent: form()},
		{Kind: typeset.FieldList, Rect: typeset.Rect{X: 100, Y: 130, Width: 100, Height: 40}, Name: "fruit", Options: []string{"Apple", "Banana"}, Value: "Banana", Appearance: value(100, 130, "Banana"), Parent: form()},
		{Kind: typeset.FieldList, Rect: typeset.Rect{X: 100, Y: 180, Width: 100, Height: 40}, Name: "multi", Options: []string{"One", "Two", "Three"}, MultiSelect: true, Selected: []int{0, 2}, Parent: form()},
		{Kind: typeset.FieldCombo, Rect: typeset.Rect{X: 100, Y: 230, Width: 100, Height: 16}, Name: "city", Options: []string{"Paris", "Berlin"}, Value: "Rome", Editable: true, ReadOnly: true, Appearance: value(100, 230, "Rome"), Parent: form()},
		{Kind: typeset.FieldText, Rect: typeset.Rect{X: 100, Y: 260, Width: 100, Height: 16}, Name: "dup", Value: "First", Parent: form()},
		{Kind: typeset.FieldText, Rect: typeset.Rect{X: 100, Y: 280, Width: 100, Height: 16}, Name: "dup", Value: "Second", Parent: form()},
	}
	page2 := letter()
	page2.Fields = []typeset.Field{{Kind: typeset.FieldText, Rect: typeset.Rect{X: 40, Y: 40, Width: 100, Height: 30}, Name: "a.b", Value: "Line one\nLine two", Multiline: true, Parent: form()}}
	return &typeset.Document{
		Metadata:  typeset.Metadata{Title: "Form", Lang: "en"},
		Pages:     []*typeset.Page{page1, page2},
		Structure: root,
	}
}

func TestWriteFieldsReadBack(t *testing.T) {
	t.Parallel()
	out := write(t, formDoc(t))
	r := open(t, out)
	got, err := r.FormFields(context.Background())
	if err != nil {
		t.Fatalf("FormFields: %v", err)
	}
	type field struct {
		name  string
		kind  models.FormFieldType
		value string
	}
	var have []field
	for _, f := range got {
		have = append(have, field{f.Name, f.Type, f.Value})
	}
	want := []field{
		{"name", models.FormFieldText, "Ada"},
		{"agree", models.FormFieldCheckbox, "Yes"},
		{"color", models.FormFieldRadio, "blue"},
		{"fruit", models.FormFieldList, "Banana"},
		{"multi", models.FormFieldList, "One, Three"},
		{"city", models.FormFieldCombo, "Rome"},
		{"dup", models.FormFieldText, "First"},
		{"dup_2", models.FormFieldText, "Second"},
		{"a_b", models.FormFieldText, "Line one\nLine two"},
	}
	slices.SortFunc(have, func(a, b field) int { return cmpString(a.name, b.name) })
	slices.SortFunc(want, func(a, b field) int { return cmpString(a.name, b.name) })
	if !slices.Equal(have, want) {
		t.Errorf("fields = %+v\nwant %+v", have, want)
	}
	for _, f := range got {
		switch f.Name {
		case "name":
			if f.MaxLen != 40 {
				t.Errorf("name MaxLen = %d", f.MaxLen)
			}
		case "city":
			if !f.ReadOnly || !f.Editable || len(f.Options) != 2 {
				t.Errorf("city = %+v", f)
			}
		case "color":
			if !slices.Equal(f.States, []string{"red", "blue"}) {
				t.Errorf("color states = %v", f.States)
			}
		}
	}
	annots, err := r.ExtractAnnotations(context.Background(), 1)
	if err != nil {
		t.Fatalf("ExtractAnnotations: %v", err)
	}
	if len(annots) != 9 {
		t.Fatalf("%d annotations on page 1, want 9", len(annots))
	}
	if a := annots[0]; a.Subtype != "Widget" || a.Rect != [4]float64{100, 728, 250, 744} {
		t.Errorf("first widget = %+v", a)
	}
	if bytes.Contains(out, []byte("NeedAppearances")) {
		t.Error("the form asks the reader for appearances")
	}
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// TestWriteFieldsDrawTheirValue renders the page: the widget's appearance
// draws the value taken off the page.
func TestWriteFieldsDrawTheirValue(t *testing.T) {
	t.Parallel()
	img := render(t, open(t, write(t, formDoc(t))), 1)
	if inkIn(img, 102, 51, 130, 62) == 0 {
		t.Error("the text field's appearance draws nothing")
	}
	if inkIn(img, 42, 92, 50, 100) == 0 {
		t.Error("the check box's on appearance draws nothing")
	}
	if inkIn(img, 42, 112, 50, 120) != 0 {
		t.Error("the radio button that is off draws its on appearance")
	}
}

func TestWriteFieldsAtEveryVersion(t *testing.T) {
	t.Parallel()
	for _, v := range []typeset.Version{typeset.Version14, typeset.Version17, typeset.Version20} {
		var buf bytes.Buffer
		if err := typeset.Write(context.Background(), formDoc(t), &buf, typeset.Options{Version: v}); err != nil {
			t.Fatalf("Write %s: %v", v, err)
		}
		fields, err := open(t, buf.Bytes()).FormFields(context.Background())
		if err != nil || len(fields) != 9 {
			t.Errorf("%s: %d fields, %v", v, len(fields), err)
		}
		qpdfCheck(t, buf.Bytes())
	}
}

// qpdfCheck runs qpdf --check over out, when qpdf is installed.
func qpdfCheck(t *testing.T, out []byte) {
	t.Helper()
	bin, err := exec.LookPath("qpdf")
	if err != nil {
		t.Log("qpdf not installed: skipping its check")
		return
	}
	path := filepath.Join(t.TempDir(), "form.pdf")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	msg, err := exec.CommandContext(context.Background(), bin, "--check", path).CombinedOutput()
	if err != nil || bytes.Contains(msg, []byte("WARNING")) {
		t.Errorf("qpdf --check: %v\n%s", err, msg)
	}
}

func TestWriteFieldsDeterministic(t *testing.T) {
	t.Parallel()
	if a, b := write(t, formDoc(t)), write(t, formDoc(t)); !bytes.Equal(a, b) {
		t.Error("two writes of one form differ")
	}
}

func TestWriteFieldsRemediate(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		fix   func(context.Context, *pdf.Writer) (*pdf.RemediationReport, error)
		check func(context.Context, *pdf.Reader) ([]error, error)
	}{
		{"PDF/A-2b", func(ctx context.Context, w *pdf.Writer) (*pdf.RemediationReport, error) {
			return w.RemediatePDFA(ctx, models.PDFA2b)
		}, func(ctx context.Context, r *pdf.Reader) ([]error, error) { return r.ValidatePDFA(ctx, models.PDFA2b) }},
		{"PDF/UA-1", func(ctx context.Context, w *pdf.Writer) (*pdf.RemediationReport, error) {
			return w.RemediatePDFUA(ctx, models.PDFUA1)
		}, func(ctx context.Context, r *pdf.Reader) ([]error, error) { return r.ValidatePDFUA(ctx, models.PDFUA1) }},
		{"PDF/UA-2", func(ctx context.Context, w *pdf.Writer) (*pdf.RemediationReport, error) {
			return w.RemediatePDFUA(ctx, models.PDFUA2)
		}, func(ctx context.Context, r *pdf.Reader) ([]error, error) { return r.ValidatePDFUA(ctx, models.PDFUA2) }},
	} {
		version := typeset.Version17
		if c.name == "PDF/UA-2" {
			// PDF/UA-2 builds on PDF 2.0 (ISO 14289-2, 5).
			version = typeset.Version20
		}
		var buf bytes.Buffer
		if err := typeset.Write(context.Background(), formDoc(t), &buf, typeset.Options{Version: version}); err != nil {
			t.Fatalf("Write: %v", err)
		}
		report, fixed := remediate(t, buf.Bytes(), c.fix)
		for _, f := range report.Fixed {
			t.Logf("%s fixed: %s", c.name, f)
		}
		for _, v := range report.Residual {
			t.Errorf("%s residual: %v", c.name, v)
		}
		violations, err := c.check(context.Background(), open(t, fixed))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, v := range violations {
			t.Errorf("%s remediated output: %v", c.name, v)
		}
	}
}

func TestValidateRefusesBadFields(t *testing.T) {
	t.Parallel()
	ok := typeset.Field{Kind: typeset.FieldText, Rect: typeset.Rect{Width: 10, Height: 10}}
	for name, mutate := range map[string]func(*typeset.Field){
		"kind":      func(f *typeset.Field) { f.Kind = 99 },
		"empty":     func(f *typeset.Field) { f.Rect.Width = 0 },
		"utf8":      func(f *typeset.Field) { f.Value = "\xff" },
		"off state": func(f *typeset.Field) { f.OnState = "Off" },
		"selection": func(f *typeset.Field) { f.Options, f.Selected = []string{"a"}, []int{1} },
		"order":     func(f *typeset.Field) { f.Options, f.Selected = []string{"a", "b"}, []int{1, 0} },
		"max len":   func(f *typeset.Field) { f.MaxLen = -1 },
	} {
		f := ok
		mutate(&f)
		page := letter()
		page.Fields = []typeset.Field{f}
		err := (&typeset.Document{Pages: []*typeset.Page{page}}).Validate(t.Context())
		if !errors.Is(err, typeset.ErrInvalidField) {
			t.Errorf("%s: Validate = %v, want ErrInvalidField", name, err)
		}
	}
}

func TestTakeContentAndBounds(t *testing.T) {
	t.Parallel()
	form := &typeset.StructElem{Role: "Form"}
	span := &typeset.StructElem{Role: "Span"}
	form.Kids = []*typeset.StructElem{span}
	value := run(t, sans, 10, typeset.Point{X: 20, Y: 30}, "Ada")
	value.Parent = span
	other := run(t, sans, 10, typeset.Point{X: 20, Y: 60}, "Other")
	box := &typeset.Path{Ops: rect(10, 10, 5, 5), Stroke: &typeset.Stroke{Color: typeset.Black, Width: 2}, Parent: form}
	group := &typeset.Group{Transform: typeset.Matrix{A: 1, D: 1, E: 100}, Content: []typeset.Fragment{box, other}}
	page := letter(value, group)
	taken := typeset.TakeContent(page, form)
	if v, ok := taken[0].(*typeset.Text); len(taken) != 2 || !ok || v.Parent != nil || v.Origin != value.Origin {
		t.Fatalf("taken = %+v", taken)
	}
	if g, ok := taken[1].(*typeset.Group); !ok || len(g.Content) != 1 || g.Content[0].(*typeset.Path).Parent != nil || g.Transform.E != 100 {
		t.Errorf("the path came back as %+v", taken[1])
	}
	if len(page.Content) != 1 || len(group.Content) != 2 {
		t.Errorf("page = %+v, shared group changed: %d", page.Content, len(group.Content))
	}
	m := typeset.NewMeasurer(t.Context())
	r, ok := m.Bounds(taken[1:])
	if !ok || r != (typeset.Rect{X: 109, Y: 9, Width: 7, Height: 7}) {
		t.Errorf("Bounds of the path = %v, %v", r, ok)
	}
	r, ok = m.Bounds(taken[:1])
	if !ok || r.X != 20 || r.Y >= 30 || r.Width <= 0 || r.Y+r.Height <= 30 {
		t.Errorf("Bounds of the run = %v, %v", r, ok)
	}
	if _, ok := m.Bounds(nil); ok {
		t.Error("Bounds of nothing reports a rectangle")
	}
	if gid, ok := m.Glyph(sans, 'A'); !ok || gid == 0 {
		t.Errorf("Glyph('A') = %d, %v", gid, ok)
	}
	if err := m.Err(); err != nil {
		t.Errorf("Err() = %v after faces that load", err)
	}
	broken := &typeset.Face{Key: "broken", Program: []byte("not a font")}
	if _, ok := m.Glyph(broken, 'A'); ok {
		t.Error("a face that does not load gives a glyph")
	}
	if err := m.Err(); !errors.Is(err, typeset.ErrInvalidFont) {
		t.Errorf("Err() = %v after a face that does not load, want ErrInvalidFont", err)
	}
}
