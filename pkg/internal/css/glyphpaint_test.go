package css

import (
	"math"
	"testing"
)

// TestGlyphPaint_Computes holds the text stroke longhands to the
// Compatibility Standard, 3.4.6: -webkit-text-stroke-width is a <line-width>
// computed to an absolute length, the colours default to currentcolor,
// and -webkit-text-stroke sets both, either left out reset.
func TestGlyphPaint_Computes(t *testing.T) {
	t.Parallel()
	red, green := Color{R: 1, A: 1}, Color{G: 128.0 / 255, A: 1}
	blue := Color{B: 1, A: 1}
	tests := []struct {
		decls string
		width float64
		color Color
		fill  Color
		first bool
	}{
		{"", 0, blue, blue, false},
		{"-webkit-text-stroke-width: 4px", 3, blue, blue, false},
		{"-webkit-text-stroke-width: thin", 0.75, blue, blue, false},
		{"-webkit-text-stroke-width: medium", 2.25, blue, blue, false},
		{"-webkit-text-stroke-width: thick", 3.75, blue, blue, false},
		{"-webkit-text-stroke-width: -1px", 0, blue, blue, false},
		{"-webkit-text-stroke-width: 10%", 0, blue, blue, false},
		{"-webkit-text-stroke: 2px red", 1.5, red, blue, false},
		{"-webkit-text-stroke: red 2px", 1.5, red, blue, false},
		{"-webkit-text-stroke: red", 0, red, blue, false},
		{"-webkit-text-stroke: 8px", 6, blue, blue, false},
		{"-webkit-text-stroke-color: red; -webkit-text-stroke: 1px", 0.75, blue, blue, false},
		{"-webkit-text-stroke: 1px red green", 0, blue, blue, false},
		{"-webkit-text-fill-color: green", 0, blue, green, false},
		{"-webkit-text-fill-color: transparent", 0, blue, Color{}, false},
		{"color: red; -webkit-text-stroke-width: 1px", 0.75, red, red, false},
		{"paint-order: stroke", 0, blue, blue, true},
		{"paint-order: stroke fill", 0, blue, blue, true},
		{"paint-order: markers stroke", 0, blue, blue, true},
		{"paint-order: fill stroke", 0, blue, blue, false},
		{"paint-order: markers", 0, blue, blue, false},
		{"paint-order: stroke stroke", 0, blue, blue, false},
		{"paint-order: normal", 0, blue, blue, false},
	}
	for _, tt := range tests {
		st := styleOf(t, tt.decls)
		w, c, first := st.TextStroke()
		if math.Abs(w-tt.width) > 1e-9 || c != tt.color || first != tt.first || st.TextFill() != tt.fill {
			t.Errorf("%q: stroke %v %+v first %v fill %+v, want %v %+v %v %+v", tt.decls, w, c, first, st.TextFill(), tt.width, tt.color, tt.first, tt.fill)
		}
	}
}

// TestGlyphPaint_Inherits holds the four longhands inherited, and a
// currentcolor stroke kept as the keyword, which a child resolves against
// its own color (CSS Color 4, 4.4), where an explicit colour inherits as
// the colour.
func TestGlyphPaint_Inherits(t *testing.T) {
	t.Parallel()
	s := compute(t, `<div id=p><span id=c>x</span></div>`,
		`#p { -webkit-text-stroke: 2px; -webkit-text-fill-color: lime; paint-order: stroke; color: red } #c { color: blue }`)
	w, c, first := s.of("c").TextStroke()
	if w != 1.5 || c != (Color{B: 1, A: 1}) || !first {
		t.Errorf("child stroke %v %+v first %v, want 1.5 blue true", w, c, first)
	}
	if got := s.of("c").TextFill(); got != (Color{G: 1, A: 1}) {
		t.Errorf("child fill %+v, want lime", got)
	}
}

// TestGlyphPaint_InitialSharesNothing holds a style that sets none of the
// longhands, or sets them to their initial values, to a nil GlyphPaint,
// which costs nothing more.
func TestGlyphPaint_InitialSharesNothing(t *testing.T) {
	t.Parallel()
	for _, decls := range []string{"", "-webkit-text-stroke: 0 currentcolor; -webkit-text-fill-color: currentcolor; paint-order: normal"} {
		if st := styleOf(t, decls); st.GlyphPaint != nil {
			t.Errorf("%q: GlyphPaint %+v, want nil", decls, st.GlyphPaint)
		}
	}
}

// TestGlyphPaint_Supports holds @supports to what litho parses (CSS
// Conditional 3, 6.1).
func TestGlyphPaint_Supports(t *testing.T) {
	t.Parallel()
	s := compute(t, `<div id=a></div>`, `
		@supports (-webkit-text-stroke: 1px red) and (-webkit-text-fill-color: red) and (paint-order: stroke fill) { #a { order: 1 } }
		@supports (paint-order: stroke stroke) { #a { order: 2 } }`)
	if got := s.of("a").Order; got != 1 {
		t.Errorf("order %d, want 1", got)
	}
}

// TestFieldSizing_Computes holds field-sizing to its two keywords (CSS
// Form Control Styling 1, 7.1), not inherited.
func TestFieldSizing_Computes(t *testing.T) {
	t.Parallel()
	s := compute(t, `<div id=p><input id=c></div>`, `#p { field-sizing: content }`)
	if got := s.of("p").FieldSizing; got != FieldContent {
		t.Errorf("field-sizing %v, want content", got)
	}
	if got := s.of("c").FieldSizing; got != FieldFixed {
		t.Errorf("child field-sizing %v, want fixed: the property is not inherited", got)
	}
	if got := styleOf(t, "field-sizing: auto").FieldSizing; got != FieldFixed {
		t.Errorf("field-sizing: auto %v, want the declaration dropped", got)
	}
}
