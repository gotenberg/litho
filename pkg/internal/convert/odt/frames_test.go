package odt

import (
	"encoding/base64"
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
	auto := `<style:style style:name="fr" style:family="graphic" style:parent-style-name="Frame"><style:graphic-properties style:vertical-pos="from-top" style:vertical-rel="page" style:horizontal-pos="from-left" style:horizontal-rel="page"/></style:style>`
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

// pathColours returns the fill and stroke colours of the paths of frags,
// groups walked.
func pathColours(frags []typeset.Fragment) map[typeset.Color]bool {
	out := map[typeset.Color]bool{}
	var walk func([]typeset.Fragment)
	walk = func(frags []typeset.Fragment) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Path:
				if v.Fill != nil {
					out[v.Fill.Color] = true
				}
				if v.Stroke != nil {
					out[v.Stroke.Color] = true
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(frags)
	return out
}

// TestBareFrame locks the fill and outline Writer paints a text frame
// whose style is an automatic style of no parent with (see
// [builder.bareFrame], which holds the measurements).
func TestBareFrame(t *testing.T) {
	rgb := func(r, g, b int) typeset.Color {
		return typeset.Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255, A: 1}
	}
	fill, line, red := rgb(0x72, 0x9f, 0xcf), rgb(0x34, 0x65, 0xa4), rgb(255, 0, 0)
	for _, tc := range []struct {
		name, parent, props string
		want, not           []typeset.Color
	}{
		{"bare", "", "", []typeset.Color{fill, line}, nil},
		{"background ignored", "", `fo:background-color="#ff0000"`, []typeset.Color{fill, line}, []typeset.Color{red}},
		{"border ignored", "", `fo:border="1pt solid #ff0000"`, []typeset.Color{fill, line}, []typeset.Color{red}},
		{"no fill", "", `draw:fill="none"`, []typeset.Color{line}, []typeset.Color{fill}},
		{"no stroke", "", `draw:stroke="none"`, []typeset.Color{fill}, []typeset.Color{line}},
		{"own colours", "", `draw:fill-color="#ff0000" svg:stroke-color="#00ff00"`, []typeset.Color{red, rgb(0, 255, 0)}, []typeset.Color{fill, line}},
		{"parent", ` style:parent-style-name="Frame"`, "", nil, []typeset.Color{fill, line}},
	} {
		auto := `<style:style style:name="fr" style:family="graphic"` + tc.parent + `><style:graphic-properties ` + tc.props + ` style:vertical-pos="from-top" style:vertical-rel="page" style:horizontal-pos="from-left" style:horizontal-rel="page"/></style:style>`
		body := `<text:p><draw:frame draw:style-name="fr" draw:name="F" text:anchor-type="page" text:anchor-page-number="1" svg:x="3cm" svg:y="5cm" svg:width="4cm" svg:height="2cm"><draw:text-box><text:p>A</text:p></draw:text-box></draw:frame></text:p>`
		got := pathColours(convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0].Content)
		for _, c := range tc.want {
			if !got[c] {
				t.Errorf("%s: no path of %v in %v", tc.name, c, got)
			}
		}
		for _, c := range tc.not {
			if got[c] {
				t.Errorf("%s: a path of %v", tc.name, c)
			}
		}
	}
}

// TestPictureColourAdjustments locks that a frame's picture takes the
// colour adjustments of its graphic style (see [odf.AdjustPicture]): the
// black stroke of an SVG picture of luminance 0%, contrast 0% and red
// 100% is stroked red, and black without them.
func TestPictureColourAdjustments(t *testing.T) {
	svgData := base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="30" height="10" style="fill:none;stroke-width:3;stroke:rgb(0,0,0)"/></svg>`))
	black, red := typeset.Color{A: 1}, typeset.Color{R: 1, A: 1}
	for _, tc := range []struct {
		props     string
		want, not typeset.Color
	}{
		{`draw:luminance="0%" draw:contrast="0%" draw:red="100%"`, red, black},
		{``, black, red},
	} {
		auto := `<style:style style:name="fr" style:family="graphic"><style:graphic-properties ` + tc.props + `/></style:style>`
		body := `<text:p><draw:frame draw:style-name="fr" draw:name="F" text:anchor-type="as-char" svg:width="4cm" svg:height="2cm"><draw:image><office:binary-data>` + svgData + `</office:binary-data></draw:image></draw:frame></text:p>`
		got := pathColours(convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0].Content)
		if !got[tc.want] || got[tc.not] {
			t.Errorf("props %q: colours %v, want %v and not %v", tc.props, got, tc.want, tc.not)
		}
	}
}

// TestDefaultFillFillsNoFrame locks that the default graphic style's
// draw:fill fills no text frame whose own styles state none, while its
// draw:fill-color serves the frames that fill (see [builder.bareFrame]).
func TestDefaultFillFillsNoFrame(t *testing.T) {
	yellow := typeset.Color{R: 1, G: 1, B: 0, A: 1}
	styles := `<style:default-style style:family="graphic"><style:graphic-properties draw:fill="solid" draw:fill-color="#ffff00"/></style:default-style>` +
		`<style:style style:name="Frame" style:family="graphic"/>`
	for _, tc := range []struct {
		parent string
		want   bool
	}{{` style:parent-style-name="Frame"`, false}, {"", true}} {
		auto := `<style:style style:name="fr" style:family="graphic"` + tc.parent + `><style:graphic-properties style:vertical-pos="from-top" style:vertical-rel="page" style:horizontal-pos="from-left" style:horizontal-rel="page"/></style:style>`
		body := `<text:p><draw:frame draw:style-name="fr" draw:name="F" text:anchor-type="page" text:anchor-page-number="1" svg:x="3cm" svg:y="5cm" svg:width="4cm" svg:height="2cm"><draw:text-box><text:p>A</text:p></draw:text-box></draw:frame></text:p>`
		got := pathColours(convertDoc(t, doc{styles: styles, contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0].Content)
		if got[yellow] != tc.want {
			t.Errorf("parent %q: yellow fill %v, want %v", tc.parent, got[yellow], tc.want)
		}
	}
}

// TestBuiltinFrame locks that a frame whose style's parent is Frame, which
// the document does not define, takes Writer's built-in Frame (see
// [builtinFrame], which holds the measurements): centred, its text inside
// a padding and a border, and that a document's own Frame replaces it.
func TestBuiltinFrame(t *testing.T) {
	auto := `<style:style style:name="fr1" style:family="graphic" style:parent-style-name="Frame"/>`
	words := strings.Repeat("word ", 120)
	body := `<text:p>Before.</text:p><text:p><draw:frame draw:style-name="fr1" draw:name="F" text:anchor-type="paragraph" svg:width="4cm" svg:height="2cm"><draw:text-box><text:p>IN</text:p></draw:text-box></draw:frame>` + words + `</text:p>`
	for _, tc := range []struct {
		styles string
		x      float64
	}{{"", 245.26}, {`<style:style style:name="Frame" style:family="graphic"/>`, 56.69}} {
		pg := convertDoc(t, doc{styles: tc.styles, contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0]
		found := false
		for _, r := range runs(pg) {
			if strings.TrimSpace(r.text) == "IN" {
				found = true
				if !near(r.x, tc.x, 0.3) {
					t.Errorf("styles %q: IN at x %.2f, want %.2f", tc.styles, r.x, tc.x)
				}
			}
		}
		if !found {
			t.Errorf("styles %q: no IN", tc.styles)
		}
	}
}

// TestFrameCentredInCell locks that a run-through frame centred on its
// paragraph is centred in the paragraph of the table cell holding it,
// not across the page (see [alignInParagraph]).
func TestFrameCentredInCell(t *testing.T) {
	auto := `<style:style style:name="T" style:family="table"><style:table-properties style:width="17cm" table:align="left"/></style:style>` +
		`<style:style style:name="T.A" style:family="table-column"><style:table-column-properties style:column-width="8.5cm"/></style:style>` +
		`<style:style style:name="T.A1" style:family="table-cell"><style:table-cell-properties fo:padding="0cm" fo:border="none"/></style:style>` +
		`<style:style style:name="fr" style:family="graphic" style:parent-style-name="Frame"><style:graphic-properties fo:margin="0cm" fo:padding="0cm" fo:border="none" style:wrap="run-through" style:vertical-pos="top" style:vertical-rel="paragraph" style:horizontal-pos="center" style:horizontal-rel="paragraph"/></style:style>`
	frame := `<draw:frame draw:style-name="fr" draw:name="F" text:anchor-type="char" svg:width="4.5cm" svg:height="2cm"><draw:text-box><text:p>IN</text:p></draw:text-box></draw:frame>`
	body := `<table:table table:name="T" table:style-name="T"><table:table-column table:style-name="T.A" table:number-columns-repeated="2"/>` +
		`<table:table-row><table:table-cell table:style-name="T.A1"><text:p>a</text:p></table:table-cell><table:table-cell table:style-name="T.A1"><text:p>` + frame + `</text:p></table:table-cell></table:table-row></table:table>`
	pg := convertDoc(t, doc{styles: `<style:style style:name="Frame" style:family="graphic"/>`, contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0]
	// The second cell starts at 2 + 8.5 cm; the frame 2 cm inside it.
	want := (2 + 8.5 + 2) / 2.54 * 72
	for _, r := range runs(pg) {
		if strings.TrimSpace(r.text) == "IN" {
			if !near(r.x, want, 0.5) {
				t.Errorf("IN at x %.2f, want %.2f", r.x, want)
			}
			return
		}
	}
	t.Error("no IN")
}

// TestFrameOverflowsItsCell sizes a table cell by its text alone when
// the frame anchored in it does not follow the text flow, and grows it to
// the frame when it does.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, a 3 cm rectangle
//     anchored 0.5 cm below the top of a one-line cell's paragraph,
//     wrapped parallel, leaves the row one line tall under
//     flow-with-text false or none and grows it to the rectangle under
//     flow-with-text true.
func TestFrameOverflowsItsCell(t *testing.T) {
	for flow, grows := range map[string]bool{"": false, ` style:flow-with-text="false"`: false, ` style:flow-with-text="true"`: true} {
		auto := `<style:style style:name="gr1" style:family="graphic"><style:graphic-properties style:wrap="parallel" style:vertical-pos="from-top" style:vertical-rel="paragraph" style:horizontal-pos="from-left" style:horizontal-rel="paragraph"` + flow + `/></style:style>`
		body := `<table:table table:name="T"><table:table-column/><table:table-row><table:table-cell><text:p>Cell<draw:rect text:anchor-type="paragraph" draw:style-name="gr1" svg:width="3cm" svg:height="3cm" svg:x="2cm" svg:y="0.5cm"/></text:p></table:table-cell></table:table-row></table:table><text:p>After</text:p>`
		ls := lines(convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0])
		if len(ls) != 2 {
			t.Fatalf("flow %q: lines %+v", flow, ls)
		}
		// The rectangle ends 3.5 cm, 99 pt, below the cell's top.
		if got := ls[1].y-ls[0].y > 90; got != grows {
			t.Errorf("flow %q: After %.1f pt below Cell, grown %v, want %v", flow, ls[1].y-ls[0].y, got, grows)
		}
	}
}

// TestCharacterBorder locks that a character border draws around its run,
// its spacing between them, casts its shadow, and grows the line by the
// spacing, the border and the shadow above and below the text, as Writer
// lays a character border out (see [applyText] and the layout's
// writerEdges): a 0.2 cm spacing, a 1 point border and a shadow 0.1 cm
// down lower the next line by 0.2 cm, 1 point and 0.1 cm below the run,
// and the run's own text by 0.2 cm and 1 point above it.
func TestCharacterBorder(t *testing.T) {
	red := typeset.Color{R: 1, A: 1}
	plain := convertDoc(t, doc{body: `<text:p>ab<text:line-break/>cd</text:p>`}.pack(t), office.Options{}).Pages[0]
	auto := `<style:style style:name="T1" style:family="text"><style:text-properties fo:border="1pt solid #ff0000" fo:padding="0.2cm" style:shadow="#000000 0.1cm 0.1cm"/></style:style>`
	pg := convertDoc(t, doc{contentAuto: auto, body: `<text:p><text:span text:style-name="T1">ab</text:span><text:line-break/>cd</text:p>`}.pack(t), office.Options{}).Pages[0]
	if !pathColours(pg.Content)[red] {
		t.Errorf("no red border")
	}
	ys := func(pg *typeset.Page) map[string]float64 {
		out := map[string]float64{}
		for _, r := range runs(pg) {
			out[strings.TrimSpace(r.text)] = r.y
		}
		return out
	}
	p, b := ys(plain), ys(pg)
	cm := 72 / 2.54
	if d := b["ab"] - p["ab"]; !near(d, 0.2*cm+1, 0.05) {
		t.Errorf("bordered run lowered by %.2f, want %.2f", d, 0.2*cm+1)
	}
	if d := b["cd"] - p["cd"]; !near(d, 2*(0.2*cm+1)+0.1*cm, 0.05) {
		t.Errorf("next line lowered by %.2f, want %.2f", d, 2*(0.2*cm+1)+0.1*cm)
	}
}

// TestPageFrameRelations locks the area a frame anchored to the page
// measures svg:x from (see [pageAnchorWholePage], which holds the
// measurements): the page box under the relations to the paragraph, the
// frame and the character, the content area under their -content forms.
func TestPageFrameRelations(t *testing.T) {
	for _, tc := range []struct {
		rel  string
		want float64
	}{
		{"", 85.04}, {"paragraph", 85.04}, {"frame", 85.04}, {"char", 85.04}, {"paragraph-start-margin", 85.04},
		{"paragraph-content", 141.73}, {"frame-content", 141.73}, {"page-content", 141.73}, {"page", 85.04},
	} {
		rel := ""
		if tc.rel != "" {
			rel = ` style:horizontal-rel="` + tc.rel + `"`
		}
		auto := `<style:style style:name="fr" style:family="graphic"><style:graphic-properties fo:margin="0cm" fo:padding="0cm" fo:border="none" style:wrap="none" style:vertical-pos="from-top" style:vertical-rel="page" style:horizontal-pos="from-left"` + rel + `/></style:style>`
		body := `<draw:frame draw:style-name="fr" draw:name="F" text:anchor-type="page" text:anchor-page-number="1" svg:x="3cm" svg:y="10cm" svg:width="8cm" svg:height="1cm"><draw:text-box><text:p>FRAME</text:p></draw:text-box></draw:frame><text:p>Body</text:p>`
		d := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{})
		found := false
		for _, r := range runs(d.Pages[0]) {
			if strings.TrimSpace(r.text) == "FRAME" {
				found = true
				if !near(r.x, tc.want, 0.2) {
					t.Errorf("rel %q: FRAME at x %.2f, want %.2f", tc.rel, r.x, tc.want)
				}
			}
		}
		if !found {
			t.Errorf("rel %q: no FRAME", tc.rel)
		}
	}
}
