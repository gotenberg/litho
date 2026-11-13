package css

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// oneValue tokenizes src into its significant component values.
func oneValue(src string) []token {
	l, _ := parseComponentValues(src)
	return significant(l)
}

func TestParseLength_Units(t *testing.T) {
	t.Parallel()
	lc := lenCtx{fontSize: 10, rootFontSize: 12, width: 600, height: 800, lineHeight: 15, rootLineHeight: 20}
	tests := []struct {
		src  string
		f    lengthFlags
		want Dimension
		ok   bool
	}{
		{"0", 0, pts(0), true},
		{"12px", 0, pts(9), true},
		{"12pt", 0, pts(12), true},
		{"1pc", 0, pts(12), true},
		{"1in", 0, pts(72), true},
		{"2.54cm", 0, pts(72), true},
		{"25.4mm", 0, pts(72), true},
		{"101.6Q", 0, pts(72), true},
		{"2em", 0, pts(20), true},
		{"2ex", 0, pts(10), true},
		{"2ch", 0, pts(10), true},
		{"2rem", 0, pts(24), true},
		{"10vw", 0, pts(60), true},
		{"10vh", 0, pts(80), true},
		{"10vmin", 0, pts(60), true},
		{"10vmax", 0, pts(80), true},
		{"50%", allowPct, pct(0.5), true},
		{"50%", 0, Dimension{}, false},
		{"-1px", nonNeg, Dimension{}, false},
		{"5", 0, Dimension{}, false},
		{"5furlong", 0, Dimension{}, false},
		{"calc(1em + 4px)", 0, pts(13), true},
		{"calc(2 * 3px)", 0, pts(4.5), true},
		{"calc(12px / 2)", 0, pts(4.5), true},
		{"calc((1px + 1px) * 2)", 0, pts(3), true},
		{"calc(50% + 10%)", allowPct, pct(0.6), true},
		{"calc(100% - 20px)", allowPct, Dimension{}, false},
		{"calc(1px+2px)", 0, Dimension{}, false},
		{"calc(1px * 2px)", 0, Dimension{}, false},
		{"calc(1px / 0)", 0, pts(maxLength), true},
		{"calc(3)", 0, Dimension{}, false},
		{"calc(1px + 2)", 0, Dimension{}, false},
		{"-webkit-calc(2px)", 0, pts(1.5), true},
		{"min(1px, 2px)", 0, pts(0.75), true},
		{"2lh", 0, pts(30), true},
		{"1rlh", 0, pts(20), true},
		{"calc(4lh + 2px)", 0, pts(61.5), true},
		{"max(1rem, 1px)", 0, pts(12), true},
		{"max(1px, 1em)", 0, pts(10), true},
		{"min(2em, 30px)", 0, pts(20), true},
		{"clamp(1px, 1em, 4px)", 0, pts(3), true},
		{"calc(2 * max(1px, 1em) + 1px)", 0, pts(20.75), true},
		{"calc(max(1em, 1px) - min(1em, 1px))", 0, pts(9.25), true},
		{"max(1px, 50%)", allowPct, Dimension{}, false},
		{"min(1px)", 0, pts(0.75), true},
		{"clamp(1px, 2px)", 0, Dimension{}, false},
		{"max(1px, 2)", 0, Dimension{}, false},
		{"1e999px", 0, Dimension{}, false},
	}
	for _, tt := range tests {
		v := oneValue(tt.src)
		l, ok := parseLength(v[0], tt.f)
		if ok != tt.ok {
			t.Errorf("%s: ok = %v, want %v", tt.src, ok, tt.ok)
			continue
		}
		if ok {
			if got := l.dim(lc, false); !dimEq(got, tt.want) {
				t.Errorf("%s = %+v, want %+v", tt.src, got, tt.want)
			}
		}
	}
}

func TestParseCalc_MixedIsWarned(t *testing.T) {
	t.Parallel()
	s := mustParse(t, `div { vertical-align: calc(100% - 2em); vertical-align: 80% }`)
	if !hasWarning(s.Warnings(), warn.CSSInvalid, `"vertical-align": a calc() mixing lengths and percentages`) {
		t.Errorf("warnings = %v", s.Warnings())
	}
}

func TestParseCalc_MixedSizeIsCalc(t *testing.T) {
	t.Parallel()
	s := mustParse(t, `div { width: calc(100% - 2em) }`)
	if w := s.Warnings(); len(w) != 0 {
		t.Errorf("warnings = %v", w)
	}
	c := compute(t, `<div id=d></div>`, `div { width: 80%; width: calc(100% - 2em) }`)
	if got := c.of("d").Width; got != (Dimension{Kind: Calc, Value: -24, Fraction: 1}) {
		t.Errorf("width = %+v, want calc(100%% - 24pt)", got)
	}
}

func TestParseColor_Syntaxes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src  string
		want Color
		ok   bool
	}{
		{"red", rgb(255, 0, 0), true},
		{"RebeccaPurple", rgb(102, 51, 153), true},
		{"transparent", Color{}, true},
		{"#fff", rgb(255, 255, 255), true},
		{"#ff000080", Color{R: 1, A: 128.0 / 255}, true},
		{"#f008", Color{R: 1, A: 136.0 / 255}, true},
		{"#12345", Color{}, false},
		{"rgb(255, 0, 0)", rgb(255, 0, 0), true},
		{"rgba(255, 0, 0, 0.5)", Color{R: 1, A: 0.5}, true},
		{"rgb(100%, 0%, 0%)", rgb(255, 0, 0), true},
		{"rgb(100%, 0, 0)", Color{}, false},
		{"rgb(255 0 0 / 50%)", Color{R: 1, A: 0.5}, true},
		{"rgb(255 0% none)", Color{R: 1, A: 1}, true},
		{"rgb(255, 0, 0, 0.5)", Color{R: 1, A: 0.5}, true},
		{"rgba(300, -1, 0)", Color{R: 1, A: 1}, true},
		{"hsl(120, 100%, 50%)", rgb(0, 255, 0), true},
		{"hsl(120deg 100% 50% / 0.25)", Color{G: 1, A: 0.25}, true},
		{"hsl(0.5turn, 100%, 50%)", rgb(0, 255, 255), true},
		{"hsla(240, 100%, 50%, 1)", rgb(0, 0, 255), true},
		{"hsl(120, 100, 50)", Color{}, false},
		{"hsl(120 100 50)", rgb(0, 255, 0), true},
		{"rgb(calc(255) 0 0)", rgb(255, 0, 0), true},
	}
	for _, tt := range tests {
		v := oneValue(tt.src)
		c, ok := parseColor(v[0])
		if ok != tt.ok {
			t.Errorf("%s: ok = %v, want %v", tt.src, ok, tt.ok)
			continue
		}
		if ok && !colorEq(c.c, tt.want) {
			t.Errorf("%s = %+v, want %+v", tt.src, c.c, tt.want)
		}
	}
	if c, ok := parseColor(oneValue("currentColor")[0]); !ok || !c.current {
		t.Errorf("currentColor = %+v, %v", c, ok)
	}
}

func TestResolveURL_AgainstBase(t *testing.T) {
	t.Parallel()
	tests := []struct{ base, ref, want string }{
		{"https://example.com/css/site.css", "../img/a.png", "https://example.com/img/a.png"},
		{"https://example.com/css/site.css", "data:image/png;base64,AAAA", "data:image/png;base64,AAAA"},
		{"", "a.png", "a.png"},
		{"https://example.com/", "#frag", "#frag"},
	}
	for _, tt := range tests {
		if got := resolveURL(tt.base, tt.ref); got != tt.want {
			t.Errorf("resolveURL(%q, %q) = %q, want %q", tt.base, tt.ref, got, tt.want)
		}
	}
	s, err := Parse(t.Context(), []byte(`p { background-image: url(bg.png) }`), ParseOptions{BaseURL: "https://example.com/css/a.css"})
	if err != nil {
		t.Fatal(err)
	}
	doc := parseHTML(t, `<p id=p>`)
	got, err := NewResolver(a4, []Sheet{{Sheet: s, Origin: Author}}).Compute(t.Context(), doc, ComputeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if u := got.Style(byID(doc, "p")).BackgroundImage; u != "https://example.com/css/bg.png" {
		t.Errorf("background-image = %q", u)
	}
}

// TestCalc_ComparesPercentagesAtUsedValueTime locks min(), max() and
// clamp() of a percentage and a length: the value keeps its calculation
// and resolves once the percentage's basis is known (CSS Values 4, 10.2
// and 10.11). A comparison of percentages alone is a percentage.
func TestCalc_ComparesPercentagesAtUsedValueTime(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=d></div><p id=p></p>`, `div { width: 80%; width: min(100%, 600px) } p { width: max(10%, 20%) }`)
	if got := c.of("p").Width; !dimEq(got, pct(0.2)) {
		t.Errorf("max(10%%, 20%%) = %+v, want 20%%", got)
	}
	w := c.of("d").Width
	if w.Kind != Calc || !near(w.Resolve(1000), 450) || !near(w.Resolve(300), 300) {
		t.Errorf("min(100%%, 600px) = %+v, resolves to %v and %v; want 450 and 300", w, w.Resolve(1000), w.Resolve(300))
	}
	if got := styleOf(t, `margin-top: calc(max(10%, 20%) + 2px)`).Margin[Top]; got != (Dimension{Kind: Calc, Value: 1.5, Fraction: 0.2}) {
		t.Errorf("calc(max(10%%, 20%%) + 2px) = %+v, want calc(20%% + 1.5pt)", got)
	}
	tests := []struct {
		decl      string
		ref, want float64
	}{
		{"calc(min(10%, 5px) + 2px)", 100, 5.25},
		{"calc(min(10%, 5px) + 2px)", 20, 3.5},
		{"clamp(10px, 50%, 20px)", 100, 15},
		{"clamp(10px, 50%, 20px)", 10, 7.5},
		{"clamp(50%, 10px, 20px)", 100, 50},
		{"round(50%, 37px)", 300, 138.75},
		{"abs(20% - 300px)", 300, 165},
		{"calc(min(50%, 100px) + 10px)", 300, 82.5},
	}
	for _, tt := range tests {
		got := styleOf(t, "width: "+tt.decl).Width
		if got.Kind != Calc || !near(got.Resolve(tt.ref), tt.want) {
			t.Errorf("width: %s = %+v, resolves against %v to %v, want %v", tt.decl, got, tt.ref, got.Resolve(tt.ref), tt.want)
		}
	}
}

// TestParseInteger_ClampsToTheInt32Range locks the range of an <integer>:
// a literal or a math function past the range of a 32-bit integer clamps
// to its nearest bound, as CSS Values 4, 5 has a value past the supported
// range converted to the closest supported one, and as Chromium holds an
// integer.
func TestParseInteger_ClampsToTheInt32Range(t *testing.T) {
	t.Parallel()
	c := compute(t, `<p id=a><p id=b><p id=c><p id=d><p id=e>`, `
		p { position: relative }
		#a { z-index: 2147483647; order: -2147483648 }
		#b { z-index: 2147483648; order: -2147483649 }
		#c { z-index: 99999999999999999999; order: -99999999999999999999 }
		#d { z-index: calc(2147483647 + 1); order: calc(-2147483647 * 2) }
		#e { z-index: 1e10; order: 2147483646.5 }`)
	for id, want := range map[string][2]int{
		"a": {MaxInteger, MinInteger},
		"b": {MaxInteger, MinInteger},
		"c": {MaxInteger, MinInteger},
		"d": {MaxInteger, MinInteger},
		"e": {0, 0},
	} {
		st := c.of(id)
		if st.ZIndex.Level != want[0] || st.Order != want[1] {
			t.Errorf("#%s: z-index %d, order %d, want %d and %d", id, st.ZIndex.Level, st.Order, want[0], want[1])
		}
	}
	if AddIntegers(MaxInteger, MaxInteger) != MaxInteger || AddIntegers(MinInteger, -1) != MinInteger || AddIntegers(MaxInteger, MinInteger) != -1 {
		t.Error("AddIntegers does not saturate at the integer range")
	}
}
