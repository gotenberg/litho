package css

import (
	"math"
	"testing"
)

// TestCalc_FunctionsAsChromiumPrints locks the math functions of CSS
// Values 4, 10.2 to 10.7 to the widths Chromium prints for them, in CSS
// pixels, and the forms it refuses.
//
// # Corpus evidence
//
//   - probe, html: each value set as the width of a box in a 400px wide
//     parent and printed by Chromium 156 in the pinned image; the box
//     widths read from the PDF and from getComputedStyle.
func TestCalc_FunctionsAsChromiumPrints(t *testing.T) {
	t.Parallel()
	lc := lenCtx{fontSize: 12, rootFontSize: 12, width: 600, height: 800}
	tests := []struct {
		src  string
		want float64
		ok   bool
	}{
		{"round(117px, 10px)", 120, true},
		{"round(up, 111px, 10px)", 120, true},
		{"round(down, 119px, 10px)", 110, true},
		{"round(to-zero, -119px, 10px)", -110, true},
		{"round(nearest, 15px, 10px)", 20, true},
		{"round(-15px, 10px)", -10, true},
		{"round(25px)", 0, false},
		{"calc(1px * round(2.5))", 3, true},
		{"mod(130px, 50px)", 30, true},
		{"rem(130px, 50px)", 30, true},
		{"mod(-130px, 50px)", 20, true},
		{"rem(-130px, 50px)", -30, true},
		{"calc(100px * sin(30deg))", 50, true},
		{"calc(100px * cos(60deg))", 50, true},
		{"calc(100px * tan(45deg))", 100, true},
		{"calc(1px * asin(0.5) / 1deg)", 30, true},
		{"calc(100px * pow(2, 3) / 8)", 100, true},
		{"calc(10px * sqrt(16))", 40, true},
		{"hypot(30px, 40px)", 50, true},
		{"calc(10px * log(100, 10))", 20, true},
		{"calc(10px * exp(1))", 10 * math.E, true},
		{"abs(-50px)", 50, true},
		{"calc(80px * sign(-5px) + 100px)", 20, true},
		{"calc(10px / 1px * 10px)", 100, true},
		{"calc(100px * atan2(1px, 1px) / 45deg)", 100, true},
		{"calc(e * 10px)", 10 * math.E, true},
		{"calc(pi * 10px)", 10 * math.Pi, true},
		{"min(infinity * 1px, 88px)", 88, true},
		{"calc(2 * 50px)", 100, true},
		{"calc(1em * cos(pi * 1rad))", -16, true},
		{"max(1em, 1px, round(up, 3rem, 7px))", 49, true},
		{"sin(1px)", 0, false},
		{"pow(2px, 2)", 0, false},
		{"mod(1px, 2)", 0, false},
		{"round(sideways, 1px, 2px)", 0, false},
		{"calc(10px * NaN)", 0, true},
	}
	for _, tt := range tests {
		v := oneValue(tt.src)
		l, ok := parseLength(v[0], 0)
		if ok != tt.ok {
			t.Errorf("%s: ok = %v, want %v", tt.src, ok, tt.ok)
			continue
		}
		if ok {
			if got := l.dim(lc, false); !dimEq(got, pts(tt.want*Pt)) {
				t.Errorf("%s = %+v, want %vpx", tt.src, got, tt.want)
			}
		}
	}
}

// TestCalc_NumbersAndAngles locks math functions where a <number>, an
// <integer> or an <angle> is expected.
func TestCalc_NumbersAndAngles(t *testing.T) {
	t.Parallel()
	if v, ok := parseNumber(oneValue("round(0.55, 0.1)")[0]); !ok || !near(v, 0.6) {
		t.Errorf("round(0.55, 0.1) = %v, %v", v, ok)
	}
	if v, ok := parseInteger(oneValue("calc(7 / 2)")[0]); !ok || v != 4 {
		t.Errorf("an <integer> of calc(7 / 2) = %v, %v; want 4, rounded", v, ok)
	}
	if v, ok := parseAngle(oneValue("calc(60deg * 2 + 0.25turn)")[0]); !ok || !near(v, 210) {
		t.Errorf("calc(60deg * 2 + 0.25turn) = %v, %v", v, ok)
	}
	if v, ok := parseAngle(oneValue("atan2(1, -1)")[0]); !ok || !near(v, 135) {
		t.Errorf("atan2(1, -1) = %v, %v", v, ok)
	}
	if _, ok := parseAngle(oneValue("calc(1px + 1deg)")[0]); ok {
		t.Errorf("calc(1px + 1deg) accepted")
	}
}

// TestCalc_Units locks the font-relative, root and viewport units of CSS
// Values 4, 6.1. The small, large and dynamic viewport units are the page
// area, as vi and vb are on a horizontal root; cap falls back to the
// ascent, then 0.7em, and ic to 1em.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints 10vw, 10svw, 10lvw, 10dvw and 10vi
//     at one width, 10vh, 10svh and 10vb at another, on an A4 page with
//     10mm margins; 10ic and 10ric at 10em with a primary font that maps
//     no "水".
func TestCalc_Units(t *testing.T) {
	t.Parallel()
	lc := lenCtx{
		fontSize: 10, rootFontSize: 20, width: 600, height: 800, lineHeight: 15, rootLineHeight: 22,
		font:     FontUnits{XHeight: 0.5, Zero: 0.6, CapHeight: 0.7, Ascent: 0.9, Ideograph: 0.95},
		rootFont: FontUnits{XHeight: 0.4, Zero: 0.5, Ascent: 0.8},
	}
	tests := []struct {
		src  string
		want float64
	}{
		{"10vw", 60}, {"10svw", 60}, {"10lvw", 60}, {"10dvw", 60}, {"10vi", 60},
		{"10vh", 80}, {"10svh", 80}, {"10lvh", 80}, {"10dvh", 80}, {"10vb", 80},
		{"10vmin", 60}, {"10svmin", 60}, {"10dvmax", 80}, {"10lvmax", 80},
		{"10cap", 70}, {"10rcap", 160}, {"10ex", 50}, {"10rex", 80},
		{"10ch", 60}, {"10rch", 100}, {"10ic", 95}, {"10ric", 200},
		{"10lh", 150}, {"10rlh", 220}, {"40Q", 40 * 72 / 101.6},
		{"calc(1cap + 1rcap + 1vi)", 7 + 16 + 6},
		{"max(1cap, 1rex)", 8},
	}
	for _, tt := range tests {
		l, ok := parseLength(oneValue(tt.src)[0], 0)
		if !ok {
			t.Errorf("%s refused", tt.src)
			continue
		}
		if got := l.dim(lc, false); !dimEq(got, pts(tt.want)) {
			t.Errorf("%s = %+v, want %v", tt.src, got, tt.want)
		}
	}
	vertical := lc
	vertical.verticalRoot = true
	if l, _ := parseLength(oneValue("10vi")[0], 0); !dimEq(l.dim(vertical, false), pts(80)) {
		t.Errorf("10vi on a vertical root = %+v, want the page height", l.dim(vertical, false))
	}
	var bare lenCtx
	bare.fontSize = 10
	if l, _ := parseLength(oneValue("10cap")[0], 0); !dimEq(l.dim(bare, false), pts(70)) {
		t.Errorf("10cap without font measures = %+v, want 0.7em", l.dim(bare, false))
	}
}
