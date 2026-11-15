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

// TestFrameAtParagraphTop locks where Writer sets a frame anchored to a
// paragraph that no text wraps, at the top of the paragraph's area (see
// [builder.frameFirst], which holds the measurements): the frame's top
// margin below the paragraph's top, the paragraph's own top margin lost,
// and the paragraph's text, before the frame or after it, below the
// frame's bottom margin.
func TestFrameAtParagraphTop(t *testing.T) {
	auto := `<style:style style:name="fr" style:family="graphic"><style:graphic-properties style:wrap="none" style:vertical-pos="top" style:vertical-rel="paragraph" style:horizontal-pos="center" style:horizontal-rel="paragraph" fo:margin-top="0.2cm" fo:margin-bottom="0.5cm"/></style:style>` +
		`<style:style style:name="P1" style:family="paragraph"><style:paragraph-properties fo:margin-top="1cm"/></style:style>`
	frame := `<draw:frame draw:style-name="fr" draw:name="F" text:anchor-type="paragraph" svg:width="6cm" svg:height="3cm"><draw:text-box><text:p>Inside</text:p></draw:text-box></draw:frame>`
	for _, body := range []string{
		`<text:p>Before</text:p><text:p text:style-name="P1">` + frame + `Anchor</text:p><text:p>After</text:p>`,
		`<text:p>Before</text:p><text:p text:style-name="P1">Anchor` + frame + `</text:p><text:p>After</text:p>`,
	} {
		pg := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0]
		y := map[string]float64{}
		for _, r := range runs(pg) {
			y[strings.TrimSpace(r.text)] = r.y
		}
		line := y["After"] - y["Anchor"]
		// 0.2 cm, 3 cm and 0.5 cm.
		want := line + 5.67 + 85.04 + 14.17
		if got := y["Anchor"] - y["Before"]; !near(got, want, 0.05) {
			t.Errorf("%s: Anchor %.2f below Before, want %.2f", body, got, want)
		}
		if got := y["Inside"] - y["Before"]; !near(got, line+5.67, 0.05) {
			t.Errorf("%s: the frame's text %.2f below Before, want %.2f", body, got, line+5.67)
		}
	}
}
