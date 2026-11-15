package css

import "testing"

// TestColorScheme_UsedScheme locks the used colour scheme of CSS Color
// Adjustment 1, 2.1 under a light preference, the one Chromium prints
// with: dark only when the list names dark and not light.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 paints Canvas
//     dark under dark, only dark, dark only and foo dark, light under
//     light dark and dark light.
func TestColorScheme_UsedScheme(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value     string
		dark      bool
		darkPrefs bool
	}{
		{"dark", true, true},
		{"only dark", true, true},
		{"dark only", true, true},
		{"foo dark", true, true},
		{"light dark", false, true},
		{"dark light", false, true},
		{"light", false, false},
		{"normal", false, false},
		{"only", false, false},
		{"dark normal", false, false},
		{"only dark only", false, false},
	}
	for _, tt := range tests {
		st := compute(t, `<div id=d></div>`, "#d { color-scheme: "+tt.value+" }").of("d")
		if st.DarkScheme != tt.dark {
			t.Errorf("color-scheme: %s: dark %v, want %v", tt.value, st.DarkScheme, tt.dark)
		}
		m := a4
		m.Features = map[string]string{"prefers-color-scheme": "dark"}
		if got := computeMedia(t, `<div id=d></div>`, "#d { color-scheme: "+tt.value+" }", m).of("d").DarkScheme; got != tt.darkPrefs {
			t.Errorf("color-scheme: %s preferring dark: dark %v, want %v", tt.value, got, tt.darkPrefs)
		}
	}
}

// TestColorScheme_Colors locks what a dark used scheme resolves (CSS
// Color Adjustment 1, 2.2; CSS Color 5, 6.2): light-dark() takes its
// second colour, the system colours their dark values, the root's initial
// colour is CanvasText, white, and an element that only inherits a colour
// keeps its parent's.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints text white
//     under :root { color-scheme: dark }, black in a color-scheme: dark
//     element of a light page, and CanvasText there white.
func TestColorScheme_Colors(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=d><p id=a></p><p id=b></p><a id=l href=x>x</a></div><p id=e></p>`, `
#d { color-scheme: dark }
#a { color: light-dark(rgb(255, 0, 0), rgb(0, 0, 255)); background-color: Canvas }
#b { color: CanvasText; border-top: 1px solid light-dark(red, lime) }
#e { color: light-dark(rgb(255, 0, 0), rgb(0, 0, 255)); background-color: Canvas }`)
	tests := []struct {
		id   string
		got  Color
		want Color
	}{
		{"#d color", c.of("d").Color, rgb(0, 0, 0)},
		{"#a color", c.of("a").Color, rgb(0, 0, 255)},
		{"#a background", c.of("a").BackgroundColor, rgb(18, 18, 18)},
		{"#b color", c.of("b").Color, rgb(255, 255, 255)},
		{"#b border", c.of("b").Border[Top].Color, rgb(0, 255, 0)},
		{"#l color", c.of("l").Color, rgb(158, 158, 255)},
		{"#e color", c.of("e").Color, rgb(255, 0, 0)},
		{"#e background", c.of("e").BackgroundColor, rgb(255, 255, 255)},
	}
	for _, tt := range tests {
		if !colorEq(tt.got, tt.want) {
			t.Errorf("%s = %+v, want %+v", tt.id, tt.got, tt.want)
		}
	}
	root := compute(t, `<p id=p></p>`, `:root { color-scheme: dark }`)
	if !colorEq(root.of("p").Color, rgb(255, 255, 255)) {
		t.Errorf("text under a dark root = %+v, want white", root.of("p").Color)
	}
	if !colorEq(compute(t, `<p id=p></p>`, ``).of("p").Color, rgb(0, 0, 0)) {
		t.Error("text under a light root is not black")
	}
}
