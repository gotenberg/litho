package css

import (
	"math"
	"testing"
)

// TestCalcSize_Grammar holds calc-size() to CSS Sizing 4, 7 as Chromium
// parses it: two arguments, a basis that is any, a length, a percentage, a
// nested calc-size() or a keyword the property takes but none, the size
// keyword only beside a basis that is not any, and nowhere but the size
// properties and flex-basis. An invalid value leaves the initial.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) CSS.supports
//     answers as the want column for each declaration.
func TestCalcSize_Grammar(t *testing.T) {
	t.Parallel()
	width := func(s *Style) Dimension { return s.Width }
	tests := []struct {
		decl     string
		get      func(*Style) Dimension
		kind     DimKind
		sizeCalc bool
	}{
		{"width: calc-size(auto, size / 2)", width, Auto, true},
		{"width: calc-size(fit-content, size + 10px)", width, FitContent, true},
		{"width: calc-size(min-content, size)", width, MinContent, true},
		{"width: calc-size(max-content, round(up, size, 7px))", width, MaxContent, true},
		{"width: calc-size(stretch, size - 10%)", width, Stretch, true},
		{"width: calc-size(calc-size(min-content, size / 2), size + 10%)", width, MinContent, true},
		{"width: calc-size(any, 30px)", width, Points, false},
		{"width: calc-size(20px, size * 2)", width, Points, false},
		{"width: calc-size(50%, size + 10px)", width, Calc, false},
		{"width: calc-size(calc-size(any, 10px), size * 3)", width, Points, false},
		{"width: calc-size(auto)", width, Auto, false},
		{"width: calc-size(none, size)", width, Auto, false},
		{"width: calc-size(any, size)", width, Auto, false},
		{"width: calc-size(auto, 2)", width, Auto, false},
		{"width: calc-size(content, size)", width, Auto, false},
		{"height: calc-size(auto, min(size, 100px))", func(s *Style) Dimension { return s.Height }, Auto, true},
		{"min-height: calc-size(auto, size - 50px)", func(s *Style) Dimension { return s.MinHeight }, Points, true},
		{"min-width: calc-size(max-content, size)", func(s *Style) Dimension { return s.MinWidth }, MaxContent, true},
		{"max-width: calc-size(min-content, size * 1000)", func(s *Style) Dimension { return s.MaxWidth }, MinContent, true},
		{"max-width: calc-size(auto, size)", func(s *Style) Dimension { return s.MaxWidth }, None, false},
		{"max-height: calc-size(auto, size)", func(s *Style) Dimension { return s.MaxHeight }, None, false},
		{"flex-basis: calc-size(auto, size * 3)", func(s *Style) Dimension { return s.FlexBasis }, Auto, true},
		{"inline-size: calc-size(fit-content, size)", width, FitContent, true},
		{"margin-left: calc-size(any, 10px)", func(s *Style) Dimension { return s.Margin[Left] }, Points, false},
		{"width: calc-size(-webkit-fill-available, size)", width, Stretch, true},
		{"width: calc-size(-moz-available, size)", width, Auto, false},
		{"width: calc-size(fit-content(10px), size)", width, Auto, false},
		{"width: calc-size(auto, size + 1)", width, Auto, false},
		{"width: CALC-SIZE(auto, SIZE)", width, Auto, true},
		{"height: calc-size(content, size)", func(s *Style) Dimension { return s.Height }, Auto, false},
		{"flex-basis: calc-size(content, size)", func(s *Style) Dimension { return s.FlexBasis }, FlexContent, true},
		{"flex-basis: calc-size(-webkit-fit-content, size)", func(s *Style) Dimension { return s.FlexBasis }, Auto, false},
		{"max-inline-size: calc-size(fit-content, size)", func(s *Style) Dimension { return s.MaxWidth }, FitContent, true},
	}
	for _, tt := range tests {
		got := tt.get(styleOf(t, tt.decl))
		if got.Kind != tt.kind || got.SizeCalc() != tt.sizeCalc {
			t.Errorf("%s = kind %v size calc %v, want %v %v", tt.decl, got.Kind, got.SizeCalc(), tt.kind, tt.sizeCalc)
		}
	}
	if s := styleOf(t, "min-height: calc-size(auto, size - 50px)"); !s.MinHeightAuto {
		t.Error("min-height: calc-size(auto, ...) does not keep the automatic minimum")
	}
	if s := styleOf(t, "margin-left: calc-size(any, 10px)"); s.Margin[Left].Value != 0 {
		t.Errorf("margin-left took calc-size(): %+v", s.Margin[Left])
	}
}

// TestCalcSize_OfSize locks the resolution of CSS Sizing 4, 7.1: the size
// keyword stands for the basis's used size, percentages resolve against
// the reference, a length or any basis folds into a calculation, a nested
// calc-size() is its basis with its calculation as the size, and an
// automatic minimum through the calculation resolves from 0.
func TestCalcSize_OfSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl       string
		get        func(*Style) Dimension
		size, ref  float64
		want       float64
		resolveRef float64
	}{
		{"width: calc-size(auto, size / 2)", func(s *Style) Dimension { return s.Width }, 300, 0, 150, math.NaN()},
		{"width: calc-size(fit-content, size + 10%)", func(s *Style) Dimension { return s.Width }, 100, 200, 120, math.NaN()},
		{"width: calc-size(calc-size(min-content, size + 15px), size * 2)", func(s *Style) Dimension { return s.Width }, 30, 0, 82.5, math.NaN()},
		{"min-height: calc-size(auto, size + 30px)", func(s *Style) Dimension { return s.MinHeight }, 15, 0, 37.5, 22.5},
		{"width: calc-size(20%, size + 15px)", func(s *Style) Dimension { return s.Width }, 0, 0, 0, 51.25},
		{"width: calc-size(any, 30px)", func(s *Style) Dimension { return s.Width }, 0, 0, 0, 22.5},
	}
	for _, tt := range tests {
		d := tt.get(styleOf(t, tt.decl))
		if d.SizeCalc() {
			if got := d.OfSize(tt.size, tt.ref); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("%s of %v = %v, want %v", tt.decl, tt.size, got, tt.want)
			}
		}
		if !math.IsNaN(tt.resolveRef) {
			if got := d.Resolve(200); math.Abs(got-tt.resolveRef) > 1e-9 {
				t.Errorf("%s resolves to %v against 200, want %v", tt.decl, got, tt.resolveRef)
			}
		}
	}
}

// TestCalcSize_NestingBounded refuses a calc-size() whose size keywords,
// replaced by nested bases, grow past maxCalcSizeNodes.
func TestCalcSize_NestingBounded(t *testing.T) {
	t.Parallel()
	v := "min-content"
	for range 12 {
		v = "calc-size(" + v + ", size + size)"
	}
	if s := styleOf(t, "width: "+v); s.Width.Kind != Auto || s.Width.SizeCalc() {
		t.Errorf("a calc-size() of 2^12 size keywords parsed: %+v", s.Width)
	}
}
