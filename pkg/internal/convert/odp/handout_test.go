package odp

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// handoutDoc returns a two-page presentation whose handout master, on an
// A4 page layout with 1 cm and 2 cm margins and a red background, holds
// six thumbnails, a decoration, and header, date-time, footer and
// page-number frames; flags are its drawing-page style's properties.
func handoutDoc(flags string) []byte {
	frame := func(class, x, y, body string) string {
		return `<draw:frame presentation:class="` + class + `" svg:x="` + x + `" svg:y="` + y + `" svg:width="9cm" svg:height="1.5cm"><draw:text-box><text:p>` + body + `</text:p></draw:text-box></draw:frame>`
	}
	var thumbs strings.Builder
	for range 6 {
		thumbs.WriteString(`<draw:page-thumbnail svg:x="1cm" svg:y="1cm" svg:width="9cm" svg:height="5cm"/>`)
	}
	return flat{
		auto: `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="28cm" fo:page-height="15.75cm"/></style:page-layout>` +
			`<style:page-layout style:name="pm2"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" fo:margin-top="1cm" fo:margin-bottom="1cm" fo:margin-left="2cm" fo:margin-right="2cm"/></style:page-layout>` +
			`<style:style style:name="dp2" style:family="drawing-page"><style:drawing-page-properties draw:fill="solid" draw:fill-color="#ff0000" ` + flags + `/></style:style>`,
		master: `<style:handout-master style:page-layout-name="pm2" draw:style-name="dp2" presentation:use-header-name="hdr1" presentation:use-footer-name="ftr1" presentation:use-date-time-name="dtd1">` + thumbs.String() +
			frame("header", "0cm", "0cm", `<presentation:header/>`) + frame("date-time", "12cm", "0cm", `<presentation:date-time/>`) +
			frame("footer", "0cm", "28cm", `<presentation:footer/>`) + frame("page-number", "12cm", "28cm", `<text:page-number>1</text:page-number>`) +
			`<draw:frame svg:x="5cm" svg:y="14cm" svg:width="9cm" svg:height="1.5cm"><draw:text-box><text:p>LOGO</text:p></draw:text-box></draw:frame>` +
			`</style:handout-master><style:master-page style:name="Default" style:page-layout-name="pm1"/>`,
		body: `<presentation:header-decl presentation:name="hdr1">HEADER</presentation:header-decl><presentation:footer-decl presentation:name="ftr1">FOOTER</presentation:footer-decl>` +
			`<presentation:date-time-decl presentation:name="dtd1" presentation:source="fixed">DATE</presentation:date-time-decl>` + page("") + page(""),
	}.bytes()
}

// handoutTexts returns the text of each of the shapes, in order.
func handoutTexts(shapes []*slides.Shape) []string {
	var out []string
	for _, s := range shapes {
		if s.Text != nil {
			out = append(out, strings.Join(texts(s.Text.Paragraphs), "/"))
		}
	}
	return out
}

// TestHandoutMaster checks that a conversion with handouts reads the
// handout master: its page layout's size and margins, its background, its
// decoration and the frames its flags show with the declarations it
// names, the page number a field, its thumbnails left out; and that one
// without handouts leaves it out.
func TestHandoutMaster(t *testing.T) {
	for _, tc := range []struct {
		name     string
		flags    string
		handouts bool
		want     []string
	}{
		{"default flags", "", true, []string{"HEADER", "DATE", "FOOTER", "LOGO"}},
		{"every flag", `presentation:display-page-number="true"`, true, []string{"HEADER", "DATE", "FOOTER", "1", "LOGO"}},
		{"header and footer off", `presentation:display-header="false" presentation:display-footer="false"`, true, []string{"DATE", "LOGO"}},
		{"no handouts", "", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := readDocument(context.Background(), handoutDoc(tc.flags), nil, "", tc.handouts)
			if err != nil {
				t.Fatal(err)
			}
			if got := handoutTexts(doc.HandoutShapes); strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("handout shapes %q, want %q", got, tc.want)
			}
			if !tc.handouts {
				return
			}
			if !near(doc.HandoutWidth, cm(21)) || !near(doc.HandoutHeight, cm(29.7)) {
				t.Errorf("handout page %v by %v", doc.HandoutWidth, doc.HandoutHeight)
			}
			if m := doc.HandoutMargins; !near(m[css.Top], cm(1)) || !near(m[css.Bottom], cm(1)) || !near(m[css.Left], cm(2)) || !near(m[css.Right], cm(2)) {
				t.Errorf("margins %v", m)
			}
			if doc.HandoutBackground.Kind != slides.FillSolid {
				t.Errorf("background %+v", doc.HandoutBackground)
			}
			for _, s := range doc.HandoutShapes {
				if s.Text != nil && texts(s.Text.Paragraphs)[0] == "1" && s.Text.Paragraphs[0].Runs[0].Field != slides.FieldSlideNumber {
					t.Errorf("page number %+v", s.Text.Paragraphs[0].Runs[0])
				}
			}
		})
	}
}
