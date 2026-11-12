package compose_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// formDoc is a tagged document with a field of every kind, a radio button
// group of two buttons, two text fields of one name and a field on a
// second page, each widget under a Form element. Each field shows its
// value in its appearance.
func formDoc(t *testing.T) *compose.Document {
	t.Helper()
	root := &compose.StructElem{Role: "Document"}
	p := &compose.StructElem{Role: "P"}
	root.Kids = append(root.Kids, p)
	form := func() *compose.StructElem {
		e := &compose.StructElem{Role: "Form"}
		p.Kids = append(p.Kids, e)
		return e
	}
	label := run(t, sans, 12, compose.Point{X: 40, Y: 60}, "Name")
	label.Parent = p
	box := &compose.Path{Ops: rect(100, 48, 150, 16), Stroke: &compose.Stroke{Color: compose.Black, Width: 0.5}}
	value := func(x, y float64, s string) []compose.Fragment {
		return []compose.Fragment{run(t, sans, 10, compose.Point{X: x + 2, Y: y + 12}, s)}
	}
	mark := []compose.Fragment{&compose.Path{Ops: rect(42, 92, 8, 8), Fill: &compose.Fill{Color: compose.Black}}}
	page1 := letter(label, box)
	page1.Fields = []compose.Field{
		{Kind: compose.FieldText, Rect: compose.Rect{X: 100, Y: 48, Width: 150, Height: 16}, Name: "name", Description: "Your name", Value: "Ada", FontSize: 10, MaxLen: 40, Appearance: value(100, 48, "Ada"), Parent: form()},
		{Kind: compose.FieldCheckBox, Rect: compose.Rect{X: 40, Y: 90, Width: 12, Height: 12}, Name: "agree", Checked: true, Appearance: mark, Parent: form()},
		{Kind: compose.FieldRadio, Rect: compose.Rect{X: 40, Y: 110, Width: 12, Height: 12}, Name: "color", OnState: "red", Appearance: mark, Parent: form()},
		{Kind: compose.FieldRadio, Rect: compose.Rect{X: 60, Y: 110, Width: 12, Height: 12}, Name: "color", OnState: "blue", Checked: true, Appearance: mark, Parent: form()},
		{Kind: compose.FieldList, Rect: compose.Rect{X: 100, Y: 130, Width: 100, Height: 40}, Name: "fruit", Options: []string{"Apple", "Banana"}, Value: "Banana", Appearance: value(100, 130, "Banana"), Parent: form()},
		{Kind: compose.FieldList, Rect: compose.Rect{X: 100, Y: 180, Width: 100, Height: 40}, Name: "multi", Options: []string{"One", "Two", "Three"}, MultiSelect: true, Selected: []int{0, 2}, Parent: form()},
		{Kind: compose.FieldCombo, Rect: compose.Rect{X: 100, Y: 230, Width: 100, Height: 16}, Name: "city", Options: []string{"Paris", "Berlin"}, Value: "Rome", Editable: true, ReadOnly: true, Appearance: value(100, 230, "Rome"), Parent: form()},
		{Kind: compose.FieldText, Rect: compose.Rect{X: 100, Y: 260, Width: 100, Height: 16}, Name: "dup", Value: "First", Parent: form()},
		{Kind: compose.FieldText, Rect: compose.Rect{X: 100, Y: 280, Width: 100, Height: 16}, Name: "dup", Value: "Second", Parent: form()},
	}
	page2 := letter()
	page2.Fields = []compose.Field{{Kind: compose.FieldText, Rect: compose.Rect{X: 40, Y: 40, Width: 100, Height: 30}, Name: "a.b", Value: "Line one\nLine two", Multiline: true, Parent: form()}}
	return &compose.Document{
		Metadata:  compose.Metadata{Title: "Form", Lang: "en"},
		Pages:     []*compose.Page{page1, page2},
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
// draws the value the page no longer holds.
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
	for _, v := range []compose.Version{compose.Version14, compose.Version17, compose.Version20} {
		var buf bytes.Buffer
		if err := compose.Write(context.Background(), formDoc(t), &buf, compose.Options{Version: v}); err != nil {
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
	out := write(t, formDoc(t))
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
	} {
		report, fixed := remediate(t, out, c.fix)
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
	ok := compose.Field{Kind: compose.FieldText, Rect: compose.Rect{Width: 10, Height: 10}}
	for name, mutate := range map[string]func(*compose.Field){
		"kind":      func(f *compose.Field) { f.Kind = 99 },
		"empty":     func(f *compose.Field) { f.Rect.Width = 0 },
		"utf8":      func(f *compose.Field) { f.Value = "\xff" },
		"off state": func(f *compose.Field) { f.OnState = "Off" },
		"selection": func(f *compose.Field) { f.Options, f.Selected = []string{"a"}, []int{1} },
		"order":     func(f *compose.Field) { f.Options, f.Selected = []string{"a", "b"}, []int{1, 0} },
		"max len":   func(f *compose.Field) { f.MaxLen = -1 },
	} {
		f := ok
		mutate(&f)
		page := letter()
		page.Fields = []compose.Field{f}
		err := (&compose.Document{Pages: []*compose.Page{page}}).Validate()
		if !errors.Is(err, compose.ErrInvalidField) {
			t.Errorf("%s: Validate = %v, want ErrInvalidField", name, err)
		}
	}
}

func TestTakeContentAndBounds(t *testing.T) {
	t.Parallel()
	form := &compose.StructElem{Role: "Form"}
	span := &compose.StructElem{Role: "Span"}
	form.Kids = []*compose.StructElem{span}
	value := run(t, sans, 10, compose.Point{X: 20, Y: 30}, "Ada")
	value.Parent = span
	other := run(t, sans, 10, compose.Point{X: 20, Y: 60}, "Other")
	box := &compose.Path{Ops: rect(10, 10, 5, 5), Stroke: &compose.Stroke{Color: compose.Black, Width: 2}, Parent: form}
	group := &compose.Group{Transform: compose.Matrix{A: 1, D: 1, E: 100}, Content: []compose.Fragment{box, other}}
	page := letter(value, group)
	taken := compose.TakeContent(page, form)
	if len(taken) != 2 || taken[0] != value {
		t.Fatalf("taken = %+v", taken)
	}
	if g, ok := taken[1].(*compose.Group); !ok || len(g.Content) != 1 || g.Content[0] != box || g.Transform.E != 100 {
		t.Errorf("the path came back as %+v", taken[1])
	}
	if len(page.Content) != 1 || len(group.Content) != 2 {
		t.Errorf("page = %+v, shared group changed: %d", page.Content, len(group.Content))
	}
	m := compose.NewMeasurer()
	r, ok := m.Bounds(taken[1:])
	if !ok || r != (compose.Rect{X: 109, Y: 9, Width: 7, Height: 7}) {
		t.Errorf("Bounds of the path = %v, %v", r, ok)
	}
	r, ok = m.Bounds(taken[:1])
	if !ok || r.X != 20 || !(r.Y < 30) || !(r.Width > 0) || !(r.Y+r.Height > 30) {
		t.Errorf("Bounds of the run = %v, %v", r, ok)
	}
	if _, ok := m.Bounds(nil); ok {
		t.Error("Bounds of nothing reports a rectangle")
	}
	if gid, ok := m.Glyph(sans, 'A'); !ok || gid == 0 {
		t.Errorf("Glyph('A') = %d, %v", gid, ok)
	}
}
