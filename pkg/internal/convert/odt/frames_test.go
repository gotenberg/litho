package odt

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestDuplicateFrames checks that a frame repeating the name and place of
// the frame read just before it is dropped, as Writer drops it on import
// (see [builder.duplicateFrame], which holds the measurements), and that
// a frame without a fill of its own is filled with the page's colour
// ([builder.pageFill]).
func TestDuplicateFrames(t *testing.T) {
	auto := `<style:style style:name="fr" style:family="graphic"><style:graphic-properties style:vertical-pos="from-top" style:vertical-rel="page" style:horizontal-pos="from-left" style:horizontal-rel="page"/></style:style>`
	frame := func(name, x, text string) string {
		return `<draw:frame draw:style-name="fr" draw:name="` + name + `" text:anchor-type="page" text:anchor-page-number="1" svg:x="` + x +
			`" svg:y="5cm" svg:width="5cm" svg:height="2cm"><draw:text-box><text:p>` + text + `</text:p></draw:text-box></draw:frame>`
	}
	for _, tc := range []struct {
		name, body string
		want       string
	}{
		{"same name and place", frame("F", "3cm", "AAA") + frame("F", "3cm", "BBB") + frame("F", "3cm", "CCC"), "AAA"},
		{"three places", frame("F", "1cm", "AAA") + frame("F", "7cm", "BBB") + frame("F", "13cm", "CCC"), "AAABBBCCC"},
		{"three names", frame("F1", "3cm", "AAA") + frame("F2", "3cm", "BBB") + frame("F3", "3cm", "CCC"), "AAABBBCCC"},
		{"another frame between", frame("F", "3cm", "AAA") + frame("G", "3cm", "BBB") + frame("F", "3cm", "CCC"), "AAABBBCCC"},
		{"a paragraph between", frame("F", "3cm", "AAA") + `<text:p>x</text:p>` + frame("F", "3cm", "CCC"), "xAAACCC"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := convertDoc(t, doc{contentAuto: auto, body: tc.body}.pack(t), office.Options{})
			var got strings.Builder
			for _, r := range runs(out.Pages[0]) {
				got.WriteString(r.text)
			}
			if got.String() != tc.want {
				t.Errorf("text %q, want %q", got.String(), tc.want)
			}
		})
	}
	out := convertDoc(t, doc{contentAuto: auto, body: frame("F", "3cm", "AAA")}.pack(t), office.Options{})
	white := false
	var walk func([]typeset.Fragment)
	walk = func(frags []typeset.Fragment) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Path:
				if v.Fill != nil && v.Fill.Color == (typeset.Color{R: 1, G: 1, B: 1, A: 1}) {
					white = true
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(out.Pages[0].Content)
	if !white {
		t.Error("the frame is not filled with the page's white")
	}
}
