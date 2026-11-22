package css

import "testing"

// TestAppearance_Computes reads appearance and its prefixed alias (CSS
// Basic User Interface 4, 7.2): the compat-auto keywords compute to auto,
// textfield and menulist-button keep their own value, and the property
// is not inherited.
func TestAppearance_Computes(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=n><span id=k></span></div><div id=a></div><div id=c></div><div id=t></div><div id=m></div><div id=w></div>`,
		`#n { appearance: none } #a { appearance: auto } #c { appearance: checkbox } #t { appearance: textfield }
		 #m { appearance: menulist-button } #w { -webkit-appearance: auto } #k { color: red }`)
	for id, want := range map[string]Appearance{
		"n": AppearanceNone, "k": AppearanceNone, "a": AppearanceAuto, "c": AppearanceAuto,
		"t": AppearanceTextField, "m": AppearanceMenuListButton, "w": AppearanceAuto,
	} {
		if got := c.of(id).Appearance; got != want {
			t.Errorf("%s: appearance %v, want %v", id, got, want)
		}
	}
}

// TestAccentColor_Computes reads accent-color (CSS Basic User Interface
// 4, 6.2): auto is nil, a colour is held and inherited, currentColor is
// the element's own colour.
func TestAccentColor_Computes(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=a><p id=in></p><p id=auto></p></div><p id=cur></p><p id=none></p>`,
		`#a { accent-color: #c00 } #auto { accent-color: auto } #cur { color: lime; accent-color: currentColor }`)
	if got := c.of("a").AccentColor; got == nil || !colorEq(*got, rgb(0xcc, 0, 0)) {
		t.Errorf("accent-color %v, want #c00", got)
	}
	if got := c.of("in").AccentColor; got == nil || !colorEq(*got, rgb(0xcc, 0, 0)) {
		t.Errorf("inherited accent-color %v, want #c00", got)
	}
	if got := c.of("auto").AccentColor; got != nil {
		t.Errorf("auto: accent-color %v, want nil", got)
	}
	if got := c.of("cur").AccentColor; got == nil || !colorEq(*got, rgb(0, 255, 0)) {
		t.Errorf("currentColor: accent-color %v, want lime", got)
	}
	if got := c.of("none").AccentColor; got != nil {
		t.Errorf("initial accent-color %v, want nil", got)
	}
}

// TestAuthorStyled_MarksBorderAndBackground marks an element whose border
// or background an author's declaration sets, even to the value the user
// agent gives it, and not one the user agent alone styles or whose
// padding or outline the author sets: a form control is drawn as its CSS
// box only under the first.
func TestAuthorStyled_MarksBorderAndBackground(t *testing.T) {
	t.Parallel()
	c := compute(t, `<input id=ua><input id=bw><input id=br><input id=bc><input id=bg><input id=bgi><input id=pad><input id=all>`,
		`#bw { border-width: 2px } #br { border-radius: 4px } #bc { border-color: red } #bg { background-color: white }
		 #bgi { background-image: none } #pad { padding: 4px; outline: 1px solid } #all { border: 0; background: none }`)
	for id, want := range map[string][2]bool{
		"ua": {false, false}, "bw": {true, false}, "br": {true, false}, "bc": {true, false},
		"bg": {false, true}, "bgi": {false, true}, "pad": {false, false}, "all": {true, true},
	} {
		st := c.of(id)
		if got := [2]bool{st.AuthorBorder, st.AuthorBackground}; got != want {
			t.Errorf("%s: author border and background %v, want %v", id, got, want)
		}
	}
}

// TestResize_Computes reads resize (CSS Basic User Interface 4, 5.1): a
// textarea is resizable by the user agent sheet, as Chromium's is, and an
// author's none stops it.
func TestResize_Computes(t *testing.T) {
	t.Parallel()
	c := compute(t, `<textarea id=ta></textarea><textarea id=no></textarea><div id=d></div><div id=v></div>`,
		`#no { resize: none } #v { resize: vertical }`)
	for id, want := range map[string]Resize{"ta": ResizeAuto, "no": ResizeNone, "d": ResizeNone, "v": ResizeVertical} {
		if got := c.of(id).Resize; got != want {
			t.Errorf("%s: resize %v, want %v", id, got, want)
		}
	}
}
