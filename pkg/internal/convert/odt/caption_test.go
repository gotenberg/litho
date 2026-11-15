package odt

import (
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
)

// TestConvert_CaptionedPictures holds the rule that ties an OpenDocument
// caption to a picture ([captionOf]) under office.Options.DerivedAltText:
// the caption frame's one paragraph holding the picture's frame and a
// figure sequence. A table caption, a caption frame of two pictures, a
// described picture and the option off give the picture nothing more.
func TestConvert_CaptionedPictures(t *testing.T) {
	pic := func(title string) string {
		return `<draw:frame draw:name="Image1" text:anchor-type="as-char" svg:width="2cm" svg:height="1cm"><draw:image xlink:href="Pictures/a.png"/>` + title + `</draw:frame>`
	}
	captioned := func(seq, inner string) string {
		return `<text:p><draw:frame draw:name="Frame1" text:anchor-type="paragraph" svg:width="4cm"><draw:text-box fo:min-height="1cm">` +
			`<text:p text:style-name="Figure">` + inner + seq + ` <text:sequence text:ref-name="ref0" text:name="` + seq + `" text:formula="ooow:` + seq + `+1" style:num-format="1">1</text:sequence>: Sales  by region</text:p>` +
			`</draw:text-box></draw:frame></text:p>`
	}
	cases := []struct {
		name, body string
		want       []string
	}{
		{"figure caption", captioned("Figure", pic("")), []string{"Figure 1: Sales by region"}},
		{"illustration caption", captioned("Illustration", pic("")), []string{"Illustration 1: Sales by region"}},
		{"table caption", captioned("Table", pic("")), []string{""}},
		{"two pictures", captioned("Figure", pic("")+strings.Replace(pic(""), "Image1", "Image2", 1)), []string{"", ""}},
		{"described", captioned("Figure", pic("<svg:title>Own</svg:title>")), []string{"Own"}},
	}
	for _, tc := range cases {
		for _, on := range []bool{true, false} {
			b := newTestBuilder(t, doc{body: tc.body, pictures: map[string][]byte{"Pictures/a.png": tinyPNG(t)}})
			b.derivedAlt = on
			root, _ := b.document(odfBody(b))
			var got []string
			walkNodes(root, func(n *layout.Node) {
				if n.Kind == layout.Replaced && n.Role == "Figure" {
					got = append(got, n.Alt)
				}
			})
			want := tc.want
			if !on {
				want = make([]string, len(tc.want))
				for i, w := range tc.want {
					if w == "Own" {
						want[i] = w
					}
				}
			}
			if !slices.Equal(got, want) {
				t.Errorf("%s (option %v): pictures %q, want %q", tc.name, on, got, want)
			}
		}
	}
}
