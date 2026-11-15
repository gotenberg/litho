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

// TestConvert_ChartObjectDescribed holds the description the replacement
// picture of a chart object takes under office.Options.DerivedAltText: the
// sentence chart.AltText derives from the embedded chart, in an English
// document stating no language of its own. Without the option, and for
// an object that is no chart, it takes none.
func TestConvert_ChartObjectDescribed(t *testing.T) {
	object := func(mime string) string {
		return `<text:p><draw:frame draw:name="Object1" text:anchor-type="as-char" svg:width="8cm" svg:height="5cm">` +
			`<draw:object><office:document xmlns:chart="urn:oasis:names:tc:opendocument:xmlns:chart:1.0" office:mimetype="` + mime + `"><office:body><office:chart>` +
			`<chart:chart chart:class="chart:bar"><chart:title><text:p>Sales</text:p></chart:title><chart:plot-area>` +
			`<chart:axis chart:dimension="x" chart:name="primary-x"><chart:categories table:cell-range-address="local-table.A2:local-table.A3"/></chart:axis><chart:axis chart:dimension="y" chart:name="primary-y"/>` +
			`<chart:series chart:values-cell-range-address="local-table.B2:local-table.B3" chart:label-cell-address="local-table.B1:local-table.B1"/></chart:plot-area>` +
			`<table:table table:name="local-table"><table:table-header-rows><table:table-row><table:table-cell/><table:table-cell office:value-type="string"><text:p>North</text:p></table:table-cell></table:table-row></table:table-header-rows>` +
			`<table:table-rows><table:table-row><table:table-cell office:value-type="string"><text:p>Q1</text:p></table:table-cell><table:table-cell office:value-type="float" office:value="1"/></table:table-row>` +
			`<table:table-row><table:table-cell office:value-type="string"><text:p>Q2</text:p></table:table-cell><table:table-cell office:value-type="float" office:value="2"/></table:table-row></table:table-rows></table:table>` +
			`</chart:chart></office:chart></office:body></office:document></draw:object>` +
			`<draw:image xlink:href="Pictures/a.png"/></draw:frame></text:p>`
	}
	for _, tc := range []struct {
		name, mime string
		on         bool
		alt, lang  string
	}{
		{"chart", "application/vnd.oasis.opendocument.chart", true, "Sales. Clustered column chart, 1 series, 2 categories", ""},
		{"option off", "application/vnd.oasis.opendocument.chart", false, "", ""},
		{"no chart", "application/vnd.oasis.opendocument.text", true, "", ""},
	} {
		b := newTestBuilder(t, doc{body: object(tc.mime), pictures: map[string][]byte{"Pictures/a.png": tinyPNG(t)}})
		b.derivedAlt = tc.on
		root, _ := b.document(odfBody(b))
		var got []string
		walkNodes(root, func(n *layout.Node) {
			if n.Kind == layout.Replaced && n.Role == "Figure" {
				got = append(got, n.Alt+"|"+n.Lang)
			}
		})
		if want := []string{tc.alt + "|" + tc.lang}; !slices.Equal(got, want) {
			t.Errorf("%s: pictures %q, want %q", tc.name, got, want)
		}
	}
}
