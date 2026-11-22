package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// widgetPaths returns the paths of page 1 of res, groups opened.
func widgetPaths(res *Result) []*typeset.Path {
	var out []*typeset.Path
	var walk func(fs []typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				out = append(out, v)
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	return out
}

// colorsOf returns the fill and stroke colours of paths, as #rrggbb, fill
// or stroke first.
func colorsOf(paths []*typeset.Path) []string {
	hex := func(c typeset.Color) string {
		v := func(x float64) int { return int(x*255 + 0.5) }
		const digits = "0123456789abcdef"
		b := []byte{'#', 0, 0, 0, 0, 0, 0}
		for i, x := range []int{v(c.R), v(c.G), v(c.B)} {
			b[1+2*i], b[2+2*i] = digits[x>>4&0xf], digits[x&0xf]
		}
		return string(b)
	}
	var out []string
	for _, p := range paths {
		switch {
		case p.Fill != nil && p.Fill.Shading == nil:
			out = append(out, "fill "+hex(p.Fill.Color))
		case p.Stroke != nil:
			out = append(out, "stroke "+hex(p.Stroke.Color))
		}
	}
	return out
}

// widgetStyle returns an inline-block style drawn as a widget.
func widgetStyle(mods ...func(*css.Style)) *css.Style {
	return sty(append([]func(*css.Style){display(css.InlineBlock), func(s *css.Style) { s.Appearance = css.AppearanceAuto }}, mods...)...)
}

// TestWidget_DrawnAsWidget decides when a control paints its widget in
// place of its CSS box (CSS Basic User Interface 4, 7.2): under
// appearance auto unless an author styled its border or background; a
// check box, a radio button and a range whatever their border or
// background, unless their appearance is none; a list box whatever its
// appearance.
func TestWidget_DrawnAsWidget(t *testing.T) {
	t.Parallel()
	with := func(a css.Appearance, border, bg bool) *css.Style {
		return &css.Style{Appearance: a, AuthorBorder: border, AuthorBackground: bg}
	}
	for _, tc := range []struct {
		name string
		kind ControlKind
		st   *css.Style
		want bool
	}{
		{"text field", ControlTextField, with(css.AppearanceAuto, false, false), true},
		{"text field, author border", ControlTextField, with(css.AppearanceAuto, true, false), false},
		{"button, author background", ControlButton, with(css.AppearanceAuto, false, true), false},
		{"text field, none", ControlTextField, with(css.AppearanceNone, false, false), false},
		{"search as textfield", ControlTextField, with(css.AppearanceTextField, false, false), true},
		{"drop-down, menulist-button", ControlDropDown, with(css.AppearanceMenuListButton, false, false), false},
		{"check box, author border", ControlCheckBox, with(css.AppearanceAuto, true, true), true},
		{"check box, none", ControlCheckBox, with(css.AppearanceNone, false, false), false},
		{"range, author background", ControlRange, with(css.AppearanceAuto, false, true), true},
		{"list box, none", ControlListBox, with(css.AppearanceNone, false, false), true},
		{"list box, author border", ControlListBox, with(css.AppearanceAuto, true, false), false},
		{"progress, author background", ControlProgress, with(css.AppearanceAuto, false, true), false},
	} {
		if got := drawnAsWidget(&Control{Kind: tc.kind}, tc.st); got != tc.want {
			t.Errorf("%s: drawn as widget %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestWidget_AccentContrast takes the dark colours around an accent whose
// contrast with white is under 3, as Chromium does, and keeps the light
// ones with a white tick above.
func TestWidget_AccentContrast(t *testing.T) {
	t.Parallel()
	gray := func(v float64) *css.Style {
		return &css.Style{AccentColor: &css.Color{R: v / 255, G: v / 255, B: v / 255, A: 1}}
	}
	if th := themeOf(gray(148)); th.box != lightWidgets.box || th.tick != hexColor(0xffffff) {
		t.Errorf("gray 148 (contrast 3.03): box %v tick %v, want the light box and a white tick", th.box, th.tick)
	}
	if th := themeOf(gray(156)); th.box != darkWidgets.box || th.tick != darkWidgets.tick {
		t.Errorf("gray 156 (contrast 2.75): box %v tick %v, want the dark ones", th.box, th.tick)
	}
	if th := themeOf(&css.Style{}); th.accent != hexColor(0x0075ff) {
		t.Errorf("default accent %v, want #0075ff", th.accent)
	}
	if th := themeOf(&css.Style{DarkScheme: true}); th.field != hexColor(0x3b3b3b) || th.accent != hexColor(0x99c8ff) {
		t.Errorf("dark scheme: field %v accent %v", th.field, th.accent)
	}
}

// TestWidget_CheckBoxBox sizes a check box drawn as its widget 13 by 13
// pixels whatever its border and padding, sets its border box's bottom on
// the baseline, and paints its box filled with the accent and ticked when
// checked.
func TestWidget_CheckBoxBox(t *testing.T) {
	t.Parallel()
	st := widgetStyle(margin(3), func(s *css.Style) {
		s.Padding = [4]css.Dimension{css.Pts(px(4)), css.Pts(px(4)), css.Pts(px(4)), css.Pts(px(4))}
		for i := range s.Border {
			s.Border[i] = css.Border{Width: px(3), Style: css.Solid, Color: css.Color{A: 1}}
		}
	})
	cb := &Node{Kind: Element, Style: st, Control: &Control{Kind: ControlCheckBox, Checked: true}}
	root := doc(el(sty(), txt(sty(), "x"), cb))
	rb := boxes(t, root, page(200, 100))
	b := ofNode(rb, cb)
	if !near(b.borderW()/css.Pt, 13) || !near(b.borderH()/css.Pt, 13) {
		t.Fatalf("check box %.2f by %.2f px, want 13 by 13", b.borderW()/css.Pt, b.borderH()/css.Pt)
	}
	if !b.hasBaseline || !near(b.baseline, b.borderH()) {
		t.Errorf("baseline %.2f, want the border box's bottom %.2f", b.baseline, b.borderH())
	}
	got := colorsOf(widgetPaths(run(t, root, page(200, 100))))
	want := []string{"fill #0075ff", "stroke #ffffff"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("paths %v, want %v", got, want)
	}
}

// TestWidget_FieldFaces paints a text field's face over its border box,
// #ffffff edged with #767676 at 0.75, a disabled one Chromium's composite
// of its translucent layers, and a field an author bordered as CSS draws
// its border.
func TestWidget_FieldFaces(t *testing.T) {
	t.Parallel()
	field := func(c *Control, mods ...func(*css.Style)) []string {
		st := widgetStyle(append([]func(*css.Style){width(50), height(10)}, mods...)...)
		return colorsOf(widgetPaths(run(t, doc(el(sty(), &Node{Kind: Element, Style: st, Control: c})), page(100, 50))))
	}
	if got := field(&Control{Kind: ControlTextField}); len(got) != 2 || got[0] != "fill #ffffff" || got[1] != "stroke #989898" {
		t.Errorf("field %v, want a #ffffff face edged #989898", got)
	}
	if got := field(&Control{Kind: ControlTextField, Disabled: true}); len(got) != 2 || got[0] != "fill #f8f8f8" || got[1] != "stroke #dbdbdb" {
		t.Errorf("disabled field %v, want #f8f8f8 edged #dbdbdb", got)
	}
	if got := field(&Control{Kind: ControlTextField}, func(s *css.Style) { s.AuthorBorder = true }); len(got) != 0 {
		t.Errorf("author bordered field %v, want no widget paint", got)
	}
}

// TestWidget_DropDownArrow paints a drop-down box's arrow in its text
// colour 4 pixels in from its right border, under appearance auto and
// menulist-button, and none under appearance none.
func TestWidget_DropDownArrow(t *testing.T) {
	t.Parallel()
	red := css.Color{R: 0.8, A: 1}
	arrow := func(a css.Appearance) []*typeset.Path {
		st := widgetStyle(width(60), height(15), func(s *css.Style) { s.Appearance, s.Color, s.BoxSizing = a, red, css.BorderBox })
		var out []*typeset.Path
		for _, p := range widgetPaths(run(t, doc(el(sty(), &Node{Kind: Element, Style: st, Control: &Control{Kind: ControlDropDown}})), page(100, 50))) {
			if p.Stroke != nil && p.Stroke.Width == 2*css.Pt {
				out = append(out, p)
			}
		}
		return out
	}
	for _, a := range []css.Appearance{css.AppearanceAuto, css.AppearanceMenuListButton} {
		got := arrow(a)
		if len(got) != 1 || got[0].Stroke.Color.R != 0.8 {
			t.Fatalf("appearance %v: arrows %v, want one in the text colour", a, got)
		}
		if right := got[0].Ops[2].P[0].X; !near(right, px(60-4)) {
			t.Errorf("appearance %v: arrow ends at %.2f, want %.2f", a, right, px(56))
		}
	}
	if got := arrow(css.AppearanceNone); len(got) != 0 {
		t.Errorf("appearance none: %d arrows, want none", len(got))
	}
}

// TestWidget_RangeThumb places a range's thumb from 7.5 pixels in at 0 to
// 7.5 pixels from the right at 1, and draws the thumb alone under
// appearance: none.
func TestWidget_RangeThumb(t *testing.T) {
	t.Parallel()
	thumb := func(v float64, a css.Appearance) (float64, int) {
		st := widgetStyle(func(s *css.Style) { s.Appearance = a })
		ps := widgetPaths(run(t, doc(el(sty(), &Node{Kind: Element, Style: st, Control: &Control{Kind: ControlRange, Value: v}})), page(200, 50)))
		last := ps[len(ps)-1]
		x0, x1 := last.Ops[0].P[0].X, last.Ops[2].P[2].X
		return (x0 + x1) / 2, len(ps)
	}
	if c, _ := thumb(0, css.AppearanceAuto); !near(c, px(7.5)) {
		t.Errorf("value 0: thumb at %.2f, want %.2f", c, px(7.5))
	}
	if c, _ := thumb(1, css.AppearanceAuto); !near(c, px(129-7.5)) {
		t.Errorf("value 1: thumb at %.2f, want %.2f", c, px(121.5))
	}
	if _, n := thumb(0.5, css.AppearanceNone); n != 1 {
		t.Errorf("appearance none: %d paths, want the thumb alone", n)
	}
}

// TestWidget_DatePickerHidden draws a date field's calendar, and none on a
// disabled field.
func TestWidget_DatePickerHidden(t *testing.T) {
	t.Parallel()
	icons := func(disabled bool) int {
		st := widgetStyle(width(100), height(15))
		n := 0
		for _, p := range widgetPaths(run(t, doc(el(sty(), &Node{Kind: Element, Style: st, Control: &Control{Kind: ControlDate, Disabled: disabled}})), page(200, 50))) {
			if p.Fill != nil && p.Fill.Rule == typeset.EvenOdd {
				n++
			}
		}
		return n
	}
	if got := icons(false); got != 1 {
		t.Errorf("date field: %d icons, want 1", got)
	}
	if got := icons(true); got != 0 {
		t.Errorf("disabled date field: %d icons, want none", got)
	}
}

// TestWidget_RangeBaselineUnderNone sets a range's border box's bottom on
// the baseline under appearance: none too: its thumb keeps it a slider.
func TestWidget_RangeBaselineUnderNone(t *testing.T) {
	t.Parallel()
	r := &Node{Kind: Element, Style: widgetStyle(margin(2), func(s *css.Style) { s.Appearance = css.AppearanceNone }), Control: &Control{Kind: ControlRange}}
	b := ofNode(boxes(t, doc(el(sty(), txt(sty(), "x"), r)), page(300, 100)), r)
	if !b.hasBaseline || !near(b.baseline, b.borderH()) {
		t.Errorf("baseline %.2f, want the border box's bottom %.2f", b.baseline, b.borderH())
	}
}

// TestWidget_GripFollowsResize draws a textarea's resize grip unless its
// resize is none (CSS Basic User Interface 4, 5.1).
func TestWidget_GripFollowsResize(t *testing.T) {
	t.Parallel()
	grips := func(r css.Resize) int {
		st := widgetStyle(width(100), height(30), func(s *css.Style) { s.Resize = r })
		n := 0
		for _, p := range widgetPaths(run(t, doc(el(sty(), &Node{Kind: Element, Style: st, Control: &Control{Kind: ControlTextArea}})), page(200, 50))) {
			if p.Stroke != nil && len(p.Ops) == 4 {
				n++
			}
		}
		return n
	}
	if got := grips(css.ResizeAuto); got != 2 {
		t.Errorf("resize auto: %d grip strokes, want 2", got)
	}
	if got := grips(css.ResizeNone); got != 0 {
		t.Errorf("resize none: %d grip strokes, want none", got)
	}
}

// TestStructure_FormHoldsNoElement hangs what a Form element shows, a
// button's spans and paragraphs, from the Form itself, which ISO/TS
// 32005, Table 5 lets hold no P, Span or other Form, and keeps its
// PrintField attributes.
func TestStructure_FormHoldsNoElement(t *testing.T) {
	t.Parallel()
	button := &Node{Kind: Element, Style: widgetStyle(), Role: "Form", PrintField: typeset.PrintField{Role: typeset.PrintPushButton},
		Control: &Control{Kind: ControlButton}}
	button.Children = []*Node{
		{Kind: Element, Style: sty(), Role: "P", Children: []*Node{txt(sty(), "Go")}},
		{Kind: Element, Style: inlineSty(), Role: "Span", Children: []*Node{txt(sty(), "now")}},
	}
	res := run(t, doc(el(sty(), button)), page(200, 100))
	var form *typeset.StructElem
	var walk func(e *typeset.StructElem)
	walk = func(e *typeset.StructElem) {
		if e.Role == "Form" {
			form = e
		}
		for _, k := range e.Kids {
			walk(k)
		}
	}
	walk(res.Structure)
	if form == nil || form.PrintField.Role != typeset.PrintPushButton {
		t.Fatalf("Form %+v, want one with its PrintField", form)
	}
	if len(form.Kids) != 0 {
		t.Errorf("the Form holds %d elements, want none", len(form.Kids))
	}
}
