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

// TestHeaderFrameWrapsNothing locks that a frame anchored in a header
// leaves the header's lines full width, whatever its wrap, unless
// settings.xml sets UseFormerTextWrapping (see [builder.place], which
// holds the measurements).
func TestHeaderFrameWrapsNothing(t *testing.T) {
	words := strings.Repeat("word ", 30)
	for _, wrap := range []string{"dynamic", "parallel", "former"} {
		former := wrap == "former"
		if former {
			wrap = "dynamic"
		}
		auto := `<style:style style:name="fr" style:family="graphic"><style:graphic-properties style:wrap="` + wrap + `" style:vertical-pos="from-top" style:vertical-rel="paragraph" style:horizontal-pos="from-left" style:horizontal-rel="paragraph"/></style:style>` +
			`<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" fo:margin-top="2cm" fo:margin-bottom="2cm" fo:margin-left="2cm" fo:margin-right="2cm"/><style:header-style><style:header-footer-properties fo:min-height="0cm"/></style:header-style></style:page-layout>`
		masters := `<style:master-page style:name="Standard" style:page-layout-name="pm1"><style:header><text:p><draw:frame draw:style-name="fr" text:anchor-type="char" svg:x="5cm" svg:y="0.3cm" svg:width="3cm" svg:height="2cm"><draw:text-box><text:p>FRAME</text:p></draw:text-box></draw:frame>` + words + `</text:p></style:header></style:master-page>`
		data := doc{autoStyles: auto, masters: masters, body: `<text:p>Body</text:p>`}.flat()
		if former {
			data = []byte(strings.Replace(string(data), "<office:font-face-decls>", `<office:settings xmlns:config="urn:oasis:names:tc:opendocument:xmlns:config:1.0"><config:config-item-set config:name="ooo:configuration-settings">`+
				`<config:config-item config:name="UseFormerTextWrapping" config:type="boolean">true</config:config-item></config:config-item-set></office:settings><office:font-face-decls>`, 1))
		}
		pg := convertDoc(t, data, office.Options{}).Pages[0]
		wrapped := false
		for _, l := range lines(pg) {
			if strings.HasPrefix(l.text, "word") && !near(l.x, 56.69, 0.1) {
				wrapped = true
			}
		}
		if wrapped != former {
			t.Errorf("%s, former wrapping %v: header lines wrapped %v", wrap, former, wrapped)
		}
	}
}

// TestFrameTouchesLineBefore locks that a frame whose top meets the bottom
// of the paragraph before its own wraps that paragraph's line, and that
// one a bottom margin away from it does not (see
// collectWordWraps in the layout, which holds the measurements).
func TestFrameTouchesLineBefore(t *testing.T) {
	auto := `<style:style style:name="fr" style:family="graphic"><style:graphic-properties fo:margin="0cm" style:wrap="right" style:vertical-pos="from-top" style:vertical-rel="paragraph" style:horizontal-pos="from-left" style:horizontal-rel="paragraph"/></style:style>` +
		`<style:style style:name="PB" style:family="paragraph"><style:paragraph-properties fo:margin-bottom="0.2cm"/></style:style>`
	frame := `<draw:frame draw:style-name="fr" draw:name="F" text:anchor-type="paragraph" svg:x="0cm" svg:y="0cm" svg:width="3cm" svg:height="2cm"><draw:text-box><text:p>FRAME</text:p></draw:text-box></draw:frame>`
	for _, tc := range []struct {
		style string
		x     float64
	}{{"", 56.69 + 85.04}, {"PB", 56.69}} {
		body := `<text:p text:style-name="` + tc.style + `">Before</text:p><text:p>` + frame + `After</text:p>`
		pg := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0]
		for _, r := range runs(pg) {
			if strings.TrimSpace(r.text) == "Before" && !near(r.x, tc.x, 0.1) {
				t.Errorf("style %q: Before at x %.2f, want %.2f", tc.style, r.x, tc.x)
			}
		}
	}
}

// TestPageFrameInParagraph locks that a page-anchored frame a producer
// nests in a paragraph prints at its page offsets, on the page its
// paragraph lands on or on the page its text:anchor-page-number names.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, a 3 by 2 cm frame
//     anchored to the page at svg:x 8 cm and svg:y 10 cm inside a
//     paragraph that starts page 2 prints on page 2 with its text at
//     226.75, 283.96; with text:anchor-page-number 1 it prints on page 1
//     at the same place; a draw:rect so nested prints too.
func TestPageFrameInParagraph(t *testing.T) {
	auto := `<style:style style:name="fr" style:family="graphic"><style:graphic-properties fo:margin="0cm" fo:padding="0cm" fo:border="none" style:wrap="right" style:vertical-pos="from-top" style:vertical-rel="page" style:horizontal-pos="from-left" style:horizontal-rel="page"/></style:style>` +
		`<style:style style:name="PB" style:family="paragraph"><style:paragraph-properties fo:break-before="page"/></style:style>`
	for _, tc := range []struct {
		number string
		page   int
	}{{"", 1}, {` text:anchor-page-number="1"`, 0}} {
		frame := `<draw:frame draw:style-name="fr" draw:name="F" text:anchor-type="page"` + tc.number + ` svg:x="8cm" svg:y="10cm" svg:width="3cm" svg:height="2cm"><draw:text-box><text:p>FRAME</text:p></draw:text-box></draw:frame>`
		body := `<text:p>One</text:p><text:p text:style-name="PB">Two ` + frame + `text</text:p>`
		d := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{})
		if len(d.Pages) != 2 {
			t.Fatalf("number %q: %d pages, want 2", tc.number, len(d.Pages))
		}
		found := false
		for _, r := range runs(d.Pages[tc.page]) {
			if strings.TrimSpace(r.text) == "FRAME" {
				found = true
				if !near(r.x, 226.77, 0.2) {
					t.Errorf("number %q: FRAME at x %.2f, want 226.77", tc.number, r.x)
				}
			}
		}
		if !found {
			t.Errorf("number %q: no FRAME on page %d", tc.number, tc.page+1)
		}
	}
}
