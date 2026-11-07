package css

import (
	"context"
	"fmt"
	"testing"
)

// faceOf parses one @font-face block with body and returns its rule and
// the warnings, ok false when the rule was dropped.
func faceOf(t *testing.T, body string) (FontFace, []Warning, bool) {
	t.Helper()
	s, err := Parse(context.Background(), []byte("@font-face { font-family: F; src: url(f.woff2); "+body+" }"), ParseOptions{BaseURL: "https://e.com/"})
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(a4, []Sheet{{Sheet: s, Origin: Author}})
	faces := r.FontFaces()
	if len(faces) != 1 {
		return FontFace{}, s.Warnings(), false
	}
	return faces[0], s.Warnings(), true
}

func TestFontFace_Descriptors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		body string
		want string
	}{
		{"", "w400-400 s0 o0-0 x100-100"},
		{"font-weight: bold", "w700-700 s0 o0-0 x100-100"},
		{"font-weight: 800 200", "w200-800 s0 o0-0 x100-100"},
		{"font-weight: auto", "w400-400 s0 o0-0 x100-100"},
		{"font-weight: 0", "w400-400 s0 o0-0 x100-100"},
		{"font-style: italic", "w400-400 s1 o0-0 x100-100"},
		{"font-style: oblique", "w400-400 s2 o14-14 x100-100"},
		{"font-style: oblique 30deg 10deg", "w400-400 s2 o10-30 x100-100"},
		{"font-style: oblique -0.25turn", "w400-400 s2 o-90--90 x100-100"},
		{"font-style: oblique 91deg", "w400-400 s0 o0-0 x100-100"},
		{"font-style: oblique 10", "w400-400 s0 o0-0 x100-100"},
		{"font-style: auto", "w400-400 s0 o0-0 x100-100"},
		{"font-stretch: condensed", "w400-400 s0 o0-0 x75-75"},
		{"font-stretch: 150% 50%", "w400-400 s0 o0-0 x50-150"},
		{"font-stretch: ultra-condensed ultra-expanded", "w400-400 s0 o0-0 x50-200"},
		{"font-stretch: -5%", "w400-400 s0 o0-0 x100-100"},
		{"font-stretch: auto", "w400-400 s0 o0-0 x100-100"},
	}
	for _, tt := range tests {
		f, _, ok := faceOf(t, tt.body)
		got := fmt.Sprintf("w%d-%d s%d o%v-%v x%v-%v", f.WeightMin, f.WeightMax, f.Style, f.ObliqueMin, f.ObliqueMax, f.StretchMin, f.StretchMax)
		if !ok || got != tt.want {
			t.Errorf("%q: %s, want %s", tt.body, got, tt.want)
		}
	}
}

func TestFontFace_Sources(t *testing.T) {
	t.Parallel()
	s, err := Parse(context.Background(), []byte(`@font-face {
  font-family: F;
  src: local(Foo Bar), local("Baz"), local("A" B), local(1),
       url(a.woff2) format("woff2"),
       url(b.woff) format("woff", "truetype"),
       url(c.ttf) format(truetype) tech(features-opentype, variations),
       url(d.ttf) tech(color-colrv1),
       url(e.ttf) tech(x) format(woff),
       url(f.ttf) format(),
       url(g.ttf) bogus(),
       url(h.ttf) format(woff) format(woff);
}`), ParseOptions{BaseURL: "https://e.com/"})
	if err != nil {
		t.Fatal(err)
	}
	faces := NewResolver(a4, []Sheet{{Sheet: s, Origin: Author}}).FontFaces()
	if len(faces) != 1 {
		t.Fatalf("faces = %+v", faces)
	}
	want := "[{ Foo Bar  [] []} { Baz  [] []} {https://e.com/a.woff2  woff2 [woff2] []} " +
		"{https://e.com/b.woff  woff [woff truetype] []} " +
		"{https://e.com/c.ttf  truetype [truetype] [features-opentype variations]} " +
		"{https://e.com/d.ttf   [] [color-colrv1]}]"
	if got := fmt.Sprint(faces[0].Sources); got != want {
		t.Errorf("sources =\n%s\nwant\n%s", got, want)
	}
}

func TestFontFace_IgnoredDescriptorsWarn(t *testing.T) {
	t.Parallel()
	f, warns, ok := faceOf(t, `font-display: swap; size-adjust: 90%; font-feature-settings: "liga" 0; ascent-override: 90%`)
	if !ok || f.Family != "F" {
		t.Fatalf("the rule was dropped: %+v", f)
	}
	for _, name := range []string{"size-adjust", "font-feature-settings", "ascent-override"} {
		if !hasWarning(warns, "unsupported-css-property", name) {
			t.Errorf("no warning for %s: %v", name, warns)
		}
	}
	if hasWarning(warns, "unsupported-css-property", "font-display") {
		t.Errorf("font-display warned: %v", warns)
	}
}
