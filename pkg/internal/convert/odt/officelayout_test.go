package odt

import (
	"context"
	"encoding/xml"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestConvert_LineSpacing lays two lines of 12pt Liberation Serif out in
// each of Writer's line spacings, one paragraph after the other, and
// checks their baselines against LibreOffice's print of the same document
// (the pinned image; the ink of every line matches at 72 dpi).
func TestConvert_LineSpacing(t *testing.T) {
	auto := `<style:style style:name="A" style:family="paragraph"><style:paragraph-properties fo:line-height="150%"/></style:style>` +
		`<style:style style:name="B" style:family="paragraph"><style:paragraph-properties fo:line-height="1cm"/></style:style>` +
		`<style:style style:name="C" style:family="paragraph"><style:paragraph-properties style:line-height-at-least="1cm"/></style:style>` +
		`<style:style style:name="D" style:family="paragraph"><style:paragraph-properties style:line-spacing="0.5cm"/></style:style>` +
		`<style:style style:name="E" style:family="paragraph"><style:paragraph-properties fo:line-height="80%"/></style:style>`
	var body strings.Builder
	for _, s := range []string{"A", "B", "C", "D", "E"} {
		body.WriteString(`<text:p text:style-name="` + s + `">Xg<text:line-break/>Xg</text:p>`)
	}
	body.WriteString(`<text:p>Xg</text:p>`)
	res := convertDoc(t, doc{contentAuto: auto, body: body.String()}.pack(t), office.Options{})
	want := []float64{67.9, 88.6, 120.8, 149.1, 180.5, 208.9, 236.9, 264.8, 290.4, 301.5, 314.6}
	ls := lines(res.Pages[0])
	if len(ls) != len(want) {
		t.Fatalf("lines = %+v", ls)
	}
	for i, l := range ls {
		if !near(l.y, want[i], 1) {
			t.Errorf("line %d baseline %.1f, want %.1f", i+1, l.y, want[i])
		}
	}
}

// TestLineRule_Fields checks the css.Style fields each line spacing sets.
func TestLineRule_Fields(t *testing.T) {
	b := &builder{}
	ts := textState{size: 12}
	cases := []struct {
		p    paraState
		rule css.LineRule
		lh   css.Dimension
		min  float64
		pad  float64
	}{
		{paraState{line: lineNormal}, css.LineRuleMultiple, css.Dimension{Kind: css.Number, Value: 1}, 0, 0},
		{paraState{line: lineProportional, lineVal: 1.5}, css.LineRuleMultiple, css.Dimension{Kind: css.Number, Value: 1.5}, 0, 0},
		{paraState{line: lineFixed, lineVal: 20}, css.LineRuleExact, css.Pts(20), 0, 0},
		{paraState{line: lineAtLeast, lineVal: 20}, css.LineRuleMultiple, css.Dimension{Kind: css.Number, Value: 1}, 20, 0},
		{paraState{line: lineSpacing, lineVal: 13.8}, css.LineRuleMultiple, css.Dimension{Kind: css.Number, Value: 2}, 0, 13.8},
	}
	for _, tc := range cases {
		st := baseStyle()
		b.lineRule(st, tc.p, ts)
		if st.LineRule != tc.rule || !st.WriterLines || !near(st.LineHeight.Value, tc.lh.Value, 1e-9) || st.LineHeight.Kind != tc.lh.Kind ||
			st.MinLineHeight != tc.min || !near(st.Padding[css.Top].Value, tc.pad, 1e-9) {
			t.Errorf("%+v: rule %v, line height %+v, min %v, padding %+v", tc.p, st.LineRule, st.LineHeight, st.MinLineHeight, st.Padding[css.Top])
		}
	}
}

// TestConvert_SingleSpacing lays paragraphs that set no line height out
// and checks their baselines against LibreOffice's print of the same
// paragraphs: a line takes its height from its own text, not from its
// paragraph's font, and a picture alone on a line makes the line as tall
// as the picture.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, Liberation Serif 24
//     point paragraphs, the middle two of 8 point text, print their
//     baselines at 79.101, 91.801, 101.001 and 125.101 points; 12 point
//     paragraphs around one holding a picture 0.2 cm tall alone at 67.901
//     and 87.401 points.
func TestConvert_SingleSpacing(t *testing.T) {
	auto := `<style:style style:name="P24" style:family="paragraph"><style:text-properties fo:font-size="24pt"/></style:style>` +
		`<style:style style:name="T8" style:family="text"><style:text-properties fo:font-size="8pt"/></style:style>`
	cases := []struct {
		name, body string
		want       []float64
	}{
		{"small text", `<text:p text:style-name="P24">Hxg line</text:p>` +
			strings.Repeat(`<text:p text:style-name="P24"><text:span text:style-name="T8">Hxg line</text:span></text:p>`, 2) +
			`<text:p text:style-name="P24">Hxg line</text:p>`, []float64{79.101, 91.801, 101.001, 125.101}},
		{"picture alone", `<text:p>Hxg line</text:p><text:p><draw:frame text:anchor-type="as-char" svg:width="0.2cm" svg:height="0.2cm"><draw:image xlink:href="Pictures/a.png"/></draw:frame></text:p><text:p>Hxg line</text:p>`,
			[]float64{67.901, 87.401}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := convertDoc(t, doc{contentAuto: auto, body: tc.body, pictures: map[string][]byte{"Pictures/a.png": tinyPNG(t)}}.pack(t), office.Options{})
			ls := lines(res.Pages[0])
			if len(ls) != len(tc.want) {
				t.Fatalf("lines = %+v", ls)
			}
			for i, l := range ls {
				if !near(l.y, tc.want[i], 0.1) {
					t.Errorf("line %d baseline %.3f, want %.3f", i+1, l.y, tc.want[i])
				}
			}
		})
	}
}

// TestConvert_PageAnchoredFrames places text boxes against the page and
// its content area, anchored to the page and to a paragraph, and checks
// their corners against LibreOffice's print of the same documents.
func TestConvert_PageAnchoredFrames(t *testing.T) {
	auto := `<style:style style:name="f1" style:family="graphic"><style:graphic-properties style:vertical-pos="middle" style:vertical-rel="page" style:horizontal-pos="center" style:horizontal-rel="page" style:wrap="run-through"/></style:style>` +
		`<style:style style:name="f2" style:family="graphic"><style:graphic-properties style:vertical-pos="bottom" style:vertical-rel="page-content" style:horizontal-pos="right" style:horizontal-rel="page-content" style:wrap="run-through"/></style:style>` +
		`<style:style style:name="f3" style:family="graphic"><style:graphic-properties style:vertical-pos="from-top" style:vertical-rel="page" style:horizontal-pos="from-left" style:horizontal-rel="page" style:wrap="run-through"/></style:style>`
	frames := func(anchor string) string {
		return `<draw:frame draw:style-name="f1" ` + anchor + ` svg:width="2cm" svg:height="1cm"><draw:text-box><text:p>A</text:p></draw:text-box></draw:frame>` +
			`<draw:frame draw:style-name="f2" ` + anchor + ` svg:width="3cm" svg:height="1cm"><draw:text-box><text:p>B</text:p></draw:text-box></draw:frame>` +
			`<draw:frame draw:style-name="f3" ` + anchor + ` svg:x="1cm" svg:y="5cm" svg:width="1cm" svg:height="1cm"><draw:text-box><text:p>C</text:p></draw:text-box></draw:frame>`
	}
	// LibreOffice: A's box at (269.3, 406.8), B's at (453.5, 756.8), C's
	// at (28.3, 141.7); the body's line stays at the top.
	want := map[string][2]float64{"A": {269.3, 406.8}, "B": {453.5, 756.8}, "C": {28.3, 141.7}, "Xg": {56.7, 56.7}}
	for _, tc := range []struct{ name, body string }{
		{"anchored to the page", frames(`text:anchor-type="page" text:anchor-page-number="1"`) + `<text:p>Xg</text:p>`},
		{"anchored to a paragraph", `<text:p>Xg` + frames(`text:anchor-type="paragraph"`) + `</text:p>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := convertDoc(t, doc{contentAuto: auto, body: tc.body}.pack(t), office.Options{})
			got := map[string]run{}
			for _, r := range runs(res.Pages[0]) {
				got[r.text] = r
			}
			for text, w := range want {
				r, ok := got[text]
				// The run's baseline sits within the box's first line.
				if !ok || !near(r.x, w[0], 1) || r.y < w[1] || r.y > w[1]+15 {
					t.Errorf("%s at %+v, want its box at %v", text, r, w)
				}
			}
		})
	}
}

// TestConvert_BordersCloned checks that a bordered paragraph, a table
// and its cells draw their whole border on each page they span, as
// Writer closes and reopens them at a page cut.
func TestConvert_BordersCloned(t *testing.T) {
	auto := `<style:style style:name="B" style:family="paragraph"><style:paragraph-properties fo:border="2pt solid #000000" fo:padding="0.2cm"/></style:style>` +
		`<style:style style:name="C" style:family="table-cell"><style:table-cell-properties fo:border="2pt solid #000000"/></style:style>`
	b := newTestBuilder(t, doc{contentAuto: auto, body: `<text:p text:style-name="B">a</text:p><table:table><table:table-column/><table:table-row><table:table-cell table:style-name="C"><text:p>c</text:p></table:table-cell></table:table-row></table:table>`})
	root, _ := b.document(odfBody(b))
	var found []string
	walkNodes(root, func(n *layout.Node) {
		if n.Style != nil && n.Style.BoxDecorationBreak == css.DecorationCloneAll {
			found = append(found, n.Role)
		}
	})
	if strings.Join(found, ",") != "P,Table,TD" {
		t.Errorf("cloned decorations on %q, want the paragraph, the table and the cell", found)
	}
}

// TestConvert_Kerning checks the kerning of text per style:letter-kerning:
// LibreOffice kerns "AV" pairs of Liberation Serif when the property is
// true alone, absent and false print the glyphs at their advances (the
// pinned image: 14 such letters at 24pt span 241 pt unkerned, 201 pt
// kerned).
func TestConvert_Kerning(t *testing.T) {
	auto := `<style:style style:name="K" style:family="text"><style:text-properties style:letter-kerning="true"/></style:style>` +
		`<style:style style:name="N" style:family="text"><style:text-properties style:letter-kerning="false"/></style:style>` +
		`<style:style style:name="PK" style:family="paragraph"><style:text-properties style:letter-kerning="true"/></style:style>`
	b := newTestBuilder(t, doc{contentAuto: auto, body: `<text:p>absent<text:span text:style-name="K">true</text:span><text:span text:style-name="N">false</text:span></text:p>` +
		`<text:p text:style-name="PK">inherited<text:span text:style-name="N">off</text:span></text:p>`})
	root, _ := b.document(odfBody(b))
	want := map[string]css.FontKerning{"absent": css.KerningNone, "true": css.KerningNormal, "false": css.KerningNone, "inherited": css.KerningNormal, "off": css.KerningNone}
	got := map[string]css.FontKerning{}
	walkNodes(root, func(n *layout.Node) {
		if n.Kind == layout.Text && n.Style != nil {
			got[n.Text] = n.Style.FontKerning
		}
	})
	for text, k := range want {
		if g, ok := got[text]; !ok || g != k {
			t.Errorf("%s: kerning %v (found %v), want %v", text, g, ok, k)
		}
	}
}

// TestFootnoteRule maps style:footnote-sep onto the page box's footnote
// rule, Writer's defaults where it is absent (LibreOffice draws a 0.5 pt
// rule a quarter of the area wide on the left, 2.85 pt from the notes; a
// 0.1 cm rule half as wide, centred, as the second case says).
func TestFootnoteRule(t *testing.T) {
	cm := 72 / 2.54
	layoutOf := func(sep string) string {
		return `<style:page-layout style:name="pmf"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" fo:margin-top="2cm" fo:margin-bottom="2cm" fo:margin-left="2cm" fo:margin-right="2cm">` +
			sep + `</style:page-layout-properties></style:page-layout>`
	}
	cases := []struct {
		name, sep string
		want      layout.FootnoteRule
	}{
		{"absent", ``, layout.FootnoteRule{Width: 0.25, Thickness: 0.5, Color: css.Color{A: 1}, Align: css.AlignLeft, Line: css.Solid, SpaceAbove: 2.85, SpaceBelow: 2.85}},
		{
			"centred", `<style:footnote-sep style:width="0.1cm" style:distance-before-sep="0.5cm" style:distance-after-sep="0.3cm" style:line-style="solid" style:adjustment="center" style:rel-width="50%" style:color="#ff0000"/>`,
			layout.FootnoteRule{Width: 0.5, Thickness: 0.1 * cm, Color: css.Color{R: 1, A: 1}, Align: css.AlignCenter, Line: css.Solid, SpaceAbove: 0.5 * cm, SpaceBelow: 0.3 * cm},
		},
		{
			"dashed right", `<style:footnote-sep style:width="1pt" style:line-style="dash" style:adjustment="right"/>`,
			layout.FootnoteRule{Width: 0.25, Thickness: 1, Color: css.Color{A: 1}, Align: css.AlignRight, Line: css.Dashed, SpaceAbove: 2.85, SpaceBelow: 2.85},
		},
		{
			"no thickness", `<style:footnote-sep style:width="0cm" style:distance-before-sep="0.2cm"/>`,
			layout.FootnoteRule{Width: 0.25, Color: css.Color{A: 1}, Align: css.AlignLeft, Line: css.BorderNone, SpaceAbove: 0.2 * cm, SpaceBelow: 2.85},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newTestBuilder(t, doc{autoStyles: layoutOf(tc.sep), masters: `<style:master-page style:name="Standard" style:page-layout-name="pmf"/>`, body: `<text:p>a</text:p>`})
			r := b.pages.geom(b.styles.Master("Standard"), false, false).box.FootnoteRule
			if r == nil {
				t.Fatal("no footnote rule")
			}
			got, want := *r, tc.want
			if !near(got.Thickness, want.Thickness, 0.01) || !near(got.SpaceAbove, want.SpaceAbove, 0.01) || !near(got.SpaceBelow, want.SpaceBelow, 0.01) ||
				!near(got.Width, want.Width, 1e-9) || got.Align != want.Align || got.Line != want.Line || !near(got.Color.R, want.Color.R, 0.01) || got.Color.A != want.Color.A {
				t.Errorf("rule = %+v, want %+v", got, want)
			}
		})
	}
}

// TestConvert_PinnedFrames places two frames anchored to a paragraph
// without text at their offsets from its corner, side by side, the next
// paragraph right below the empty one, as LibreOffice prints them (the
// pinned image: the frames' text at the same pixels at 72 dpi).
func TestConvert_PinnedFrames(t *testing.T) {
	auto := `<style:style style:name="fr" style:family="graphic"><style:graphic-properties style:wrap="parallel" style:vertical-pos="from-top" style:vertical-rel="paragraph" style:horizontal-pos="from-left" style:horizontal-rel="paragraph"/></style:style>`
	body := `<text:p>Before</text:p><text:p><draw:frame draw:style-name="fr" text:anchor-type="paragraph" svg:x="1cm" svg:y="1cm" svg:width="2cm" svg:height="1cm"><draw:text-box><text:p>A</text:p></draw:text-box></draw:frame>` +
		`<draw:frame draw:style-name="fr" text:anchor-type="paragraph" svg:x="4cm" svg:y="0.5cm" svg:width="2cm" svg:height="1cm"><draw:text-box><text:p>B</text:p></draw:text-box></draw:frame></text:p><text:p>After</text:p>`
	res := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{})
	got := map[string]run{}
	for _, r := range runs(res.Pages[0]) {
		got[r.text] = r
	}
	want := map[string][2]float64{"Before": {56.7, 67.6}, "After": {56.7, 95.2}, "A": {85.0, 109.8}, "B": {170.1, 95.6}}
	for text, w := range want {
		if r, ok := got[text]; !ok || !near(r.x, w[0], 1) || !near(r.y, w[1], 1) {
			t.Errorf("%s at %+v, want %v", text, r, w)
		}
	}
}

// TestConvert_FormControls checks the frames Writer draws around form
// controls whatever their fo:border: a 0.2 point black line around a text
// field, none around a check box, whose mark draws its own square (see
// controlFontSize and markScale).
func TestConvert_FormControls(t *testing.T) {
	auto := `<style:style style:name="gr1" style:family="graphic"><style:graphic-properties fo:border="solid"/></style:style>` +
		`<style:style style:name="gr2" style:family="graphic"><style:graphic-properties fo:border="groove"/></style:style>`
	body := `<office:forms xmlns:form="urn:oasis:names:tc:opendocument:xmlns:form:1.0"><form:form form:name="F"><form:text form:id="c1"/><form:checkbox form:id="c2"/></form:form></office:forms>` +
		`<text:p>Name <draw:control text:anchor-type="as-char" draw:style-name="gr1" svg:width="1in" svg:height="0.25in" draw:control="c1"/></text:p>` +
		`<text:p>Agree <draw:control text:anchor-type="as-char" draw:style-name="gr2" svg:width="0.13in" svg:height="0.13in" draw:control="c2"/></text:p>`
	b := newTestBuilder(t, doc{contentAuto: auto, body: body})
	root, _ := b.document(odfBody(b))
	var widths []float64
	walkNodes(root, func(n *layout.Node) {
		if n.Role == "Form" {
			widths = append(widths, n.Style.Border[css.Top].Width)
			if n.Style.Border[css.Top].Width > 0 && (n.Style.Border[css.Top].Color != (css.Color{A: 1}) || n.Style.Border[css.Top].Style != css.Solid) {
				t.Errorf("border = %+v", n.Style.Border[css.Top])
			}
		}
	})
	if len(widths) != 2 || widths[0] != 0.2 || widths[1] != 0 {
		t.Errorf("control border widths = %v, want [0.2 0]", widths)
	}
}

// TestConvert_ShapeText draws the text a shape holds over its fill, in
// the text area as draw:textarea-vertical-align places it: LibreOffice
// prints "Mid" of a 3 cm high rectangle 1 cm below its paragraph with
// its baseline 131.5 pt down the page, its fill from 85 pt.
func TestConvert_ShapeText(t *testing.T) {
	auto := `<style:style style:name="gm" style:family="graphic"><style:graphic-properties draw:fill="solid" draw:fill-color="#dddddd" draw:stroke="none" draw:textarea-vertical-align="middle" fo:padding="0.2cm" style:wrap="run-through" style:vertical-pos="from-top" style:vertical-rel="paragraph" style:horizontal-pos="from-left" style:horizontal-rel="paragraph"/></style:style>`
	body := `<text:p>Xg<draw:rect draw:style-name="gm" text:anchor-type="paragraph" svg:x="1cm" svg:y="1cm" svg:width="4cm" svg:height="3cm"><text:p>Mid</text:p></draw:rect></text:p>`
	res := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{})
	var order []string
	fillTop := -1.0
	var walk func(fs []typeset.Fragment, dy float64)
	walk = func(fs []typeset.Fragment, dy float64) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				if v.Fill != nil {
					order = append(order, "fill")
					top := math.Inf(1)
					for _, op := range v.Ops {
						if op.Kind != typeset.ClosePath {
							top = math.Min(top, op.P[0].Y)
						}
					}
					fillTop = dy + top
				}
			case *typeset.Text:
				var sb strings.Builder
				for _, g := range v.Glyphs {
					sb.WriteString(g.Text)
				}
				order = append(order, sb.String())
				if sb.String() == "Mid" && !near(v.Origin.Y+dy, 131.5, 1.5) {
					t.Errorf("Mid's baseline at %.1f, want 131.5", v.Origin.Y+dy)
				}
			case *typeset.Group:
				walk(v.Content, dy+v.Transform.F)
			}
		}
	}
	walk(res.Pages[0].Content, 0)
	if strings.Join(order, ",") != "Xg,fill,Mid" {
		t.Errorf("paint order %v, want the fill under the shape's text", order)
	}
	if !near(fillTop, 85, 1) {
		t.Errorf("fill from %.1f, want 85", fillTop)
	}
}

// TestConvert_ContextualSpacingAutomatic checks that contextual spacing
// compares the common styles of paragraphs whose automatic styles
// differ, and reads LibreOffice's loext:contextual-spacing: the two
// paragraphs sit a line apart, no margin between them.
func TestConvert_ContextualSpacingAutomatic(t *testing.T) {
	styles := `<style:style style:name="Data" style:family="paragraph"><style:paragraph-properties fo:margin-bottom="1cm" loext:contextual-spacing="true"/></style:style>`
	auto := `<style:style style:name="P1" style:family="paragraph" style:parent-style-name="Data"><style:paragraph-properties fo:margin-left="1cm"/></style:style>` +
		`<style:style style:name="P2" style:family="paragraph" style:parent-style-name="Data"/>`
	res := convertDoc(t, doc{styles: styles, contentAuto: auto, body: `<text:p text:style-name="P1">a</text:p><text:p text:style-name="P2">b</text:p>`}.pack(t), office.Options{})
	ls := lines(res.Pages[0])
	if len(ls) != 2 || !near(ls[1].y-ls[0].y, 13.8, 0.3) {
		t.Errorf("lines = %+v, want them 13.8 pt apart", ls)
	}
}

// TestColumnTracks computes the tracks of columns of relative widths
// with their indents: each a share of the width less its indents, the
// gap between two the end indent of one and the start indent of the
// next, the outer indents the container's padding.
func TestColumnTracks(t *testing.T) {
	cm := 72 / 2.54
	b := &builder{}
	col := func(rel, start, end string) string {
		return `<style:column style:rel-width="` + rel + `" fo:start-indent="` + start + `" fo:end-indent="` + end + `"/>`
	}
	cases := []struct {
		name, cols  string
		tracks      []columnTrack
		first, last float64
		ok          bool
	}{
		{
			"equal", col("32767*", "0cm", "0.25cm") + col("32768*", "0.25cm", "0cm"),
			[]columnTrack{{Width: 32767.0/65535*400 - 0.25*cm, Gap: 0.5 * cm}, {Width: 32768.0/65535*400 - 0.25*cm}},
			0, 0, true,
		},
		{
			"unequal with outer indents", col("1*", "1cm", "0.5cm") + col("3*", "0.5cm", "0cm"),
			[]columnTrack{{Width: 100 - 1.5*cm, Gap: 1 * cm}, {Width: 300 - 0.5*cm}},
			1 * cm, 0, true,
		},
		{"unreadable share", col("x", "0cm", "0cm") + col("1*", "0cm", "0cm"), nil, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			el, err := odf.ParseXML(context.Background(), []byte(`<style:columns `+nsDecl+`>`+tc.cols+`</style:columns>`))
			if err != nil {
				t.Fatal(err)
			}
			tracks, first, last, ok := b.columnTracks(el.All(odf.NSStyle, "column"), 400)
			if ok != tc.ok || len(tracks) != len(tc.tracks) || !near(first, tc.first, 1e-6) || !near(last, tc.last, 1e-6) {
				t.Fatalf("tracks %+v, %v, %v, %v", tracks, first, last, ok)
			}
			for i := range tracks {
				if !near(tracks[i].Width, tc.tracks[i].Width, 1e-6) || !near(tracks[i].Gap, tc.tracks[i].Gap, 1e-6) {
					t.Errorf("track %d = %+v, want %+v", i, tracks[i], tc.tracks[i])
				}
			}
		})
	}
}

func TestApplyColumnTracks_FillsTheEngineTracks(t *testing.T) {
	t.Parallel()
	st := css.Initial()
	st.Padding[css.Left], st.Padding[css.Right] = css.Pts(2), css.Pts(3)
	applyColumnTracks(st, []columnTrack{{Width: 100, Gap: 10}, {Width: 200}}, 5, 7)
	want := []css.ColumnTrack{{Width: 100, Gap: 10}, {Width: 200}}
	if len(st.Columns.Tracks) != 2 || st.Columns.Tracks[0] != want[0] || st.Columns.Tracks[1] != want[1] {
		t.Errorf("tracks %+v, want %+v", st.Columns.Tracks, want)
	}
	if st.Padding[css.Left] != css.Pts(7) || st.Padding[css.Right] != css.Pts(10) {
		t.Errorf("padding %v %v, want the outer indents added, 7 and 10", st.Padding[css.Left], st.Padding[css.Right])
	}
}

func TestApplyWritingMode_TurnsTheBox(t *testing.T) {
	t.Parallel()
	for mode, want := range map[string]css.WritingMode{"bt-lr": css.SidewaysLR, "tb-rl": css.VerticalRL, "lr-tb": css.HorizontalTB, "": css.HorizontalTB} {
		st := css.Initial()
		applyWritingMode(st, mode)
		if st.WritingMode != want {
			t.Errorf("%q: writing mode %v, want %v", mode, st.WritingMode, want)
		}
	}
}

// TestConvert_FrameAnchorPage puts a frame anchored to the page its
// text:anchor-page-number names, a page the text does not reach included
// (LibreOffice prints anchor-page-number 3 of a one-line document on a
// third page), and clips a text box of a fixed height while one that
// grows, or one whose text continues in a linked box, does not.
func TestConvert_FrameAnchorPage(t *testing.T) {
	auto := `<style:style style:name="fr" style:family="graphic"><style:graphic-properties style:wrap="run-through" style:vertical-pos="from-top" style:vertical-rel="page" style:horizontal-pos="from-left" style:horizontal-rel="page"/></style:style>`
	frame := func(page, box string) string {
		return `<draw:frame draw:style-name="fr" text:anchor-type="page" text:anchor-page-number="` + page + `" svg:x="2cm" svg:y="2cm" svg:width="4cm" svg:height="1cm">` + box + `<text:p>P` + page + `</text:p></draw:text-box></draw:frame>`
	}
	tests := []struct {
		name  string
		page  string
		box   string
		pages int
		clip  bool
	}{
		{"third page, fixed", "3", `<draw:text-box>`, 3, true},
		{"first page, grows", "1", `<draw:text-box fo:min-height="1cm">`, 1, false},
		{"first page, linked", "1", `<draw:text-box draw:chain-next-name="F2">`, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := doc{contentAuto: auto, body: frame(tt.page, tt.box) + `<text:p>Xg</text:p>`}
			res := convertDoc(t, d.pack(t), office.Options{})
			if len(res.Pages) != tt.pages || !strings.Contains(pageText(res.Pages[tt.pages-1]), "P"+tt.page) {
				t.Fatalf("%d pages, last %q", len(res.Pages), pageText(res.Pages[len(res.Pages)-1]))
			}
			b := newTestBuilder(t, d)
			root, _ := b.document(odfBody(b))
			clipped := false
			walkNodes(root, func(n *layout.Node) {
				if n.Role == "Figure" && n.Style.Overflow == css.OverflowHidden {
					clipped = true
				}
			})
			if clipped != tt.clip {
				t.Errorf("text box clipped %v, want %v", clipped, tt.clip)
			}
		})
	}
}

// TestConvert_LongWordBreaks breaks a word wider than its line where the
// line ends, as Writer does, rather than let it overflow the page.
func TestConvert_LongWordBreaks(t *testing.T) {
	res := convertDoc(t, doc{body: `<text:p>` + strings.Repeat("x", 300) + `</text:p>`}.pack(t), office.Options{})
	if n := len(lines(res.Pages[0])); n < 2 {
		t.Errorf("%d lines, want the word broken", n)
	}
}

func TestApplyFrameWrap_PageAnchoredFrames(t *testing.T) {
	t.Parallel()
	for wrap, want := range map[string]css.FrameWrap{
		"none": css.FrameWrapNone, "left": css.FrameWrapLeft, "right": css.FrameWrapRight, "parallel": css.FrameWrapBoth,
		"dynamic": css.FrameWrapDynamic, "biggest": css.FrameWrapLargest, "run-through": css.FrameWrapThrough, "": css.FrameWrapThrough, "contour": css.FrameWrapThrough,
	} {
		st := css.Initial()
		applyFrameWrap(st, wrap, [4]float64{1, 2, 3, 4})
		if st.FrameWrap != want || st.WrapGap != [4]float64{1, 2, 3, 4} {
			t.Errorf("%q: wrap %v gap %v, want %v", wrap, st.FrameWrap, st.WrapGap, want)
		}
	}
	auto := `<style:style style:name="fr1" style:family="graphic"><style:graphic-properties style:wrap="dynamic" fo:margin="0.1in"/></style:style>`
	b := newTestBuilder(t, doc{contentAuto: auto, body: `<draw:frame draw:style-name="fr1" text:anchor-type="page" text:anchor-page-number="2" svg:x="1in" svg:y="1in" svg:width="2in" svg:height="1in"><draw:text-box><text:p>framed</text:p></draw:text-box></draw:frame><text:p>body</text:p>`})
	root, _ := b.document(odfBody(b))
	found := false
	walkNodes(root, func(n *layout.Node) {
		if n.Style.PageAnchor {
			found = true
			if n.Style.FrameWrap != css.FrameWrapDynamic || n.Style.WrapGap != [4]float64{7.2, 7.2, 7.2, 7.2} {
				t.Errorf("wrap %v gap %v, want dynamic, 7.2pt", n.Style.FrameWrap, n.Style.WrapGap)
			}
		}
	})
	if !found {
		t.Fatal("no page-anchored frame")
	}
}

// TestConvert_ParallelWrapBothSides sets the lines beside a page-anchored
// frame of parallel wrap on both its sides, the left filled first, as
// LibreOffice prints the same document (probe through the pinned image:
// one word left of the frame, the rest of the line right of it).
func TestConvert_ParallelWrapBothSides(t *testing.T) {
	auto := `<style:style style:name="fr1" style:family="graphic"><style:graphic-properties fo:margin="0cm" fo:padding="0cm" fo:border="none" style:wrap="parallel" style:horizontal-pos="from-left" style:horizontal-rel="page" style:vertical-pos="from-top" style:vertical-rel="page"/></style:style>` +
		`<style:style style:name="PA" style:family="paragraph"><style:paragraph-properties fo:margin-top="1cm" fo:margin-left="2cm" fo:margin-right="1cm"/></style:style>`
	var words strings.Builder
	for i := 10; i < 100; i++ {
		words.WriteString(" w" + strconv.Itoa(i) + "x")
	}
	body := `<draw:frame draw:style-name="fr1" text:anchor-type="page" svg:x="5cm" svg:y="4cm" svg:width="3cm" svg:height="2cm"><draw:text-box><text:p>FRAME</text:p></draw:text-box></draw:frame>` +
		`<text:p>Before</text:p><text:p text:style-name="PA">` + words.String() + `</text:p>`
	res := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{})
	// The frame spans 141.7 to 226.8 points across, 113.4 to 170.1 down.
	var left, right bool
	for _, r := range runs(res.Pages[0]) {
		if r.y < 120 || r.y > 165 || !strings.HasPrefix(strings.TrimSpace(r.text), "w") {
			continue
		}
		switch {
		case r.x < 141:
			left = true
		case r.x >= 226:
			right = true
		}
	}
	if !left || !right {
		t.Errorf("text left of the frame %v, right of it %v, want both", left, right)
	}
}

// TestConvert_ParagraphFramesAtOffsets places frames anchored to a
// paragraph at their offsets: across from the column whatever the
// paragraph's indent, down from the top of the paragraph's area, its top
// margin included, or of its content area, overlapping one another, the
// text wrapping them. The positions are LibreOffice's print of the same
// documents (probes through the pinned image).
func TestConvert_ParagraphFramesAtOffsets(t *testing.T) {
	var words strings.Builder
	for i := 10; i < 100; i++ {
		words.WriteString("w" + strconv.Itoa(i) + "x ")
	}
	frame := func(name, x, y string) string {
		return `<draw:frame draw:style-name="fr1" draw:name="` + name + `" text:anchor-type="paragraph" svg:x="` + x + `" svg:y="` + y + `" svg:width="3cm" svg:height="2cm"><draw:text-box><text:p>` + name + `</text:p></draw:text-box></draw:frame>`
	}
	style := func(wrap, rel string) string {
		return `<style:style style:name="fr1" style:family="graphic"><style:graphic-properties fo:margin="0cm" fo:padding="0cm" fo:border="none" style:wrap="` + wrap + `" style:vertical-pos="from-top" style:vertical-rel="` + rel + `" style:horizontal-pos="from-left" style:horizontal-rel="` + rel + `"/></style:style>` +
			`<style:style style:name="PA" style:family="paragraph"><style:paragraph-properties fo:margin-top="1cm" fo:margin-left="2cm" fo:margin-right="1cm"/></style:style>` +
			`<style:style style:name="PB" style:family="paragraph"><style:paragraph-properties fo:margin-bottom="0.5cm"/></style:style>`
	}
	type at struct {
		text string
		x, y float64
	}
	tests := []struct {
		name, auto, body string
		want             []at
	}{
		{"paragraph", style("parallel", "paragraph"), `<text:p>Before</text:p><text:p text:style-name="PA">` + frame("F1", "3cm", "1cm") + words.String() + `</text:p>`,
			[]at{{"F1", 141.7, 110.05}, {"w10x", 113.4, 110.05}, {"w11x", 226.8, 110.05}}},
		{"content area", style("parallel", "paragraph-content"), `<text:p>Before</text:p><text:p text:style-name="PA">` + frame("F1", "3cm", "1cm") + words.String() + `</text:p>`,
			[]at{{"F1", 198.4, 138.4}}},
		{"bottom margin before", style("parallel", "paragraph"), `<text:p text:style-name="PB">Before</text:p><text:p text:style-name="PA">` + frame("F1", "3cm", "0cm") + words.String() + `</text:p>`,
			[]at{{"F1", 141.7, 95.85}}},
		{"overlapping", style("parallel", "paragraph"), `<text:p>Before</text:p><text:p text:style-name="PA">` + frame("F1", "3cm", "0.5cm") + frame("F2", "4cm", "1cm") + words.String() + `</text:p>`,
			[]at{{"F1", 141.7, 95.85}, {"F2", 170.1, 110.05}}},
		{"none wrap", style("none", "paragraph"), `<text:p>Before</text:p><text:p text:style-name="PA">` + frame("F1", "3cm", "1cm") + words.String() + `</text:p>`,
			[]at{{"F1", 141.7, 110.05}, {"w10x", 113.4, 166.8}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := convertDoc(t, doc{contentAuto: tt.auto, body: tt.body}.pack(t), office.Options{})
			rs := runs(res.Pages[0])
			for _, w := range tt.want {
				found := false
				for _, r := range rs {
					if strings.HasPrefix(strings.TrimSpace(r.text), w.text) {
						found = true
						if !near(r.x, w.x, 0.5) || !near(r.y, w.y, 0.5) {
							t.Errorf("%s at %.2f, %.2f, want %.2f, %.2f", w.text, r.x, r.y, w.x, w.y)
						}
					}
				}
				if !found {
					t.Errorf("%s not drawn", w.text)
				}
			}
		})
	}
}

// TestScanChains_MergingChainsDeterministic gives the frame two chains
// run into the same place every time: the chain whose first frame's name
// sorts first.
func TestScanChains_MergingChainsDeterministic(t *testing.T) {
	t.Parallel()
	frame := func(name, next string) *odf.Element {
		tb := &odf.Element{Name: xmlName(odf.NSDraw, "text-box")}
		if next != "" {
			tb.Attr = []xml.Attr{xmlAttr(odf.NSDraw, "chain-next-name", next)}
		}
		return &odf.Element{Name: xmlName(odf.NSDraw, "frame"), Attr: []xml.Attr{xmlAttr(odf.NSDraw, "name", name)}, Children: []*odf.Element{tb}}
	}
	root := &odf.Element{Name: xmlName(odf.NSText, "p"), Children: []*odf.Element{
		frame("B", "C"), frame("A", "C"), frame("D", "C"), frame("C", ""),
	}}
	for range 64 {
		if got := scanChains(root)["C"]; got != (chainLink{name: "A", index: 1}) {
			t.Fatalf("C = %+v, want the place of A's chain", got)
		}
	}
}

func TestScanChains_LinkedTextBoxes(t *testing.T) {
	t.Parallel()
	frame := func(name, next, text string) string {
		attr := ""
		if next != "" {
			attr = ` draw:chain-next-name="` + next + `"`
		}
		return `<draw:frame draw:name="` + name + `" text:anchor-type="paragraph" svg:width="2in" svg:height="1in"><draw:text-box` + attr + `>` + text + `</draw:text-box></draw:frame>`
	}
	body := `<text:p>` + frame("Frame3", "", "") + frame("Frame1", "Frame2", "<text:p>chained</text:p>") + frame("Frame2", "Frame3", "") +
		frame("Loop1", "Loop2", "") + frame("Loop2", "Loop1", "") + frame("Alone", "", "<text:p>alone</text:p>") + `</text:p>`
	b := newTestBuilder(t, doc{body: body})
	got := scanChains(odfBody(b))
	want := map[string]chainLink{"Frame1": {"Frame1", 0}, "Frame2": {"Frame1", 1}, "Frame3": {"Frame1", 2}}
	if len(got) != len(want) {
		t.Fatalf("chains %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %v, want %v", k, got[k], v)
		}
	}
	if scanChains(nil) != nil {
		t.Error("a missing body has chains")
	}
	root, _ := b.document(odfBody(b))
	var chained []string
	walkNodes(root, func(n *layout.Node) {
		if n.Style.Chain != "" {
			chained = append(chained, n.Style.Chain+":"+strconv.Itoa(n.Style.ChainIndex))
		}
		if n.Style.Chain != "" && !n.Style.StrokeBorders {
			t.Error("a frame's borders are not stroked")
		}
	})
	if strings.Join(chained, " ") != "Frame1:2 Frame1:0 Frame1:1" {
		t.Errorf("links %q, want Frame1:2 Frame1:0 Frame1:1", chained)
	}
}

func TestFamilies_GenericBeforeAsian(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		t    textState
		want string
	}{
		{textState{family: [3]string{"Missing", "EA Face", "CTL Face"}, generic: "roman"}, "Missing Noto Serif serif EA Face CTL Face"},
		{textState{family: [3]string{"Missing", "", ""}}, "Missing Noto Sans"},
		{textState{family: [3]string{"Mono", "", ""}, generic: "modern"}, "Mono monospace"},
		{textState{family: [3]string{"", "EA Face", ""}, generic: "roman"}, "EA Face serif"},
		{textState{family: [3]string{"Face", "Face", ""}}, "Face Noto Sans"},
		{textState{}, "serif"},
	} {
		if got := strings.Join(tc.t.families(), " "); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.t.family, got, tc.want)
		}
	}
}

func TestApplyExactHeight_Rows(t *testing.T) {
	t.Parallel()
	auto := `<style:style style:name="R1" style:family="table-row"><style:table-row-properties style:row-height="1cm"/></style:style>` +
		`<style:style style:name="R2" style:family="table-row"><style:table-row-properties style:row-height="1cm" style:use-optimal-row-height="true"/></style:style>` +
		`<style:style style:name="R3" style:family="table-row"><style:table-row-properties style:min-row-height="1cm"/></style:style>`
	var body strings.Builder
	body.WriteString(`<table:table><table:table-column/>`)
	for _, r := range []string{"R1", "R2", "R3"} {
		body.WriteString(`<table:table-row table:style-name="` + r + `"><table:table-cell><text:p>x</text:p></table:table-cell></table:table-row>`)
	}
	body.WriteString(`</table:table>`)
	b := newTestBuilder(t, doc{contentAuto: auto, body: body.String()})
	root, _ := b.document(odfBody(b))
	var got []bool
	walkNodes(root, func(n *layout.Node) {
		if n.Style.Display == css.TableRow {
			got = append(got, n.Style.ExactHeight)
		}
	})
	if len(got) != 3 || !got[0] || got[1] || got[2] {
		t.Errorf("exact rows %v, want [true false false]", got)
	}
}

// TestColumnTracks_NaNShare refuses a style:rel-width that reads as NaN,
// which strconv parses: its tracks were NaN wide.
func TestColumnTracks_NaNShare(t *testing.T) {
	t.Parallel()
	col := func(w string) *odf.Element {
		return &odf.Element{Name: xmlName(odf.NSStyle, "column"), Attr: []xml.Attr{xmlAttr(odf.NSStyle, "rel-width", w)}}
	}
	b := &builder{}
	if tracks, _, _, ok := b.columnTracks([]*odf.Element{col("NaN*"), col("1*")}, 400); ok {
		t.Errorf("tracks %v from a NaN share", tracks)
	}
	if _, _, _, ok := b.columnTracks([]*odf.Element{col("1*"), col("1*")}, 400); !ok {
		t.Error("equal shares refused")
	}
}

// TestConvert_ShapeOutlineWhole draws a rectangle's and an ellipse's
// outline whole, half the line outside their box (ODF 1.3, Part 3,
// 10.3.2 and 10.3.4; svg:stroke-width), as LibreOffice prints them: no
// clip at the box cuts the right and bottom halves of the line.
func TestConvert_ShapeOutlineWhole(t *testing.T) {
	auto := `<style:style style:name="gr" style:family="graphic"><style:graphic-properties draw:stroke="solid" svg:stroke-color="#ff0000" svg:stroke-width="2pt" draw:fill="solid" draw:fill-color="#ffff00" style:wrap="none" style:vertical-pos="from-top" style:vertical-rel="page" style:horizontal-pos="from-left" style:horizontal-rel="page"/></style:style>`
	body := `<draw:rect text:anchor-type="page" text:anchor-page-number="1" draw:style-name="gr" svg:width="5cm" svg:height="2cm" svg:x="3cm" svg:y="3cm"/>` +
		`<draw:ellipse text:anchor-type="page" text:anchor-page-number="1" draw:style-name="gr" svg:width="5cm" svg:height="2cm" svg:x="3cm" svg:y="8cm"/><text:p>x</text:p>`
	res := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{})
	// A clip of the shape's own width, 5 cm, would cut its outline.
	const boxW = 5 / 2.54 * 72
	strokes, clipped := 0, 0
	var walk func(fs []typeset.Fragment, clip bool)
	walk = func(fs []typeset.Fragment, clip bool) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				if v.Stroke != nil {
					strokes++
					if clip {
						clipped++
					}
				}
			case *typeset.Group:
				own := false
				if v.Clip != nil {
					x0, x1 := math.Inf(1), math.Inf(-1)
					for _, op := range v.Clip.Ops {
						if op.Kind != typeset.ClosePath {
							x0, x1 = math.Min(x0, op.P[0].X), math.Max(x1, op.P[0].X)
						}
					}
					own = near(x1-x0, boxW, 0.5)
				}
				walk(v.Content, clip || own)
			}
		}
	}
	walk(res.Pages[0].Content, false)
	if strokes != 2 || clipped != 0 {
		t.Errorf("%d outlines, %d of them clipped at their box; want 2, none", strokes, clipped)
	}
}

// TestConvert_ShapeTextCentred places a shape's text across its text area
// as draw:textarea-horizontal-align center says (ODF 1.3, Part 3,
// 20.136): the line's middle on the rectangle's.
func TestConvert_ShapeTextCentred(t *testing.T) {
	auto := `<style:style style:name="gm" style:family="graphic"><style:graphic-properties draw:fill="solid" draw:fill-color="#dddddd" draw:stroke="none" draw:textarea-horizontal-align="center" draw:textarea-vertical-align="middle" fo:padding="0cm" style:wrap="run-through" style:vertical-pos="from-top" style:vertical-rel="page" style:horizontal-pos="from-left" style:horizontal-rel="page"/></style:style>`
	body := `<draw:rect draw:style-name="gm" text:anchor-type="page" text:anchor-page-number="1" svg:x="3cm" svg:y="3cm" svg:width="6cm" svg:height="3cm"><text:p>Mid</text:p></draw:rect><text:p>x</text:p>`
	res := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{})
	// The 6 cm rectangle is 170 points wide and "Mid" some 20: centred,
	// the line starts some 75 points in, left-aligned at the edge.
	left := 3 / 2.54 * 72
	found := false
	for _, l := range lines(res.Pages[0]) {
		if l.text == "Mid" {
			found = true
			if l.x < left+50 || l.x > left+110 {
				t.Errorf("Mid starts at %.1f, %.1f into the rectangle, want it centred", l.x, l.x-left)
			}
		}
	}
	if !found {
		t.Error("no line Mid")
	}
}

// TestConvert_MarginAreaFrames places text boxes against the page's top
// and bottom margin areas, which LibreOffice names in loext:vertical-rel:
// the top one measured from the page's top edge, the bottom one from the
// top of the bottom margin.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfexport/page-content-top.odt prints its 10 cm
//     square 3 cm below the page's top edge, and
//     odfexport/page-content-bottom.odt its square of svg:y -10.998 cm
//     with its top 423.5 points below the top of a 27.94 cm page of 2 cm
//     margins, 311.8 points above the bottom margin.
func TestConvert_MarginAreaFrames(t *testing.T) {
	auto := `<style:style style:name="t" style:family="graphic"><style:graphic-properties style:vertical-pos="from-top" style:vertical-rel="paragraph" loext:vertical-rel="page-content-top" style:horizontal-pos="from-left" style:horizontal-rel="paragraph" style:wrap="run-through"/></style:style>` +
		`<style:style style:name="b" style:family="graphic"><style:graphic-properties style:vertical-pos="from-top" style:vertical-rel="paragraph" loext:vertical-rel="page-content-bottom" style:horizontal-pos="from-left" style:horizontal-rel="paragraph" style:wrap="run-through"/></style:style>`
	body := `<text:p>Xg<draw:frame draw:style-name="t" text:anchor-type="paragraph" svg:y="1cm" svg:width="2cm" svg:height="1cm"><draw:text-box><text:p>T</text:p></draw:text-box></draw:frame>` +
		`<draw:frame draw:style-name="b" text:anchor-type="paragraph" svg:y="-5cm" svg:width="2cm" svg:height="1cm"><draw:text-box><text:p>B</text:p></draw:text-box></draw:frame></text:p>`
	res := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{})
	got := map[string]run{}
	for _, r := range runs(res.Pages[0]) {
		got[r.text] = r
	}
	// The default page is 29.7 cm tall with 2 cm margins: the bottom
	// margin starts 785.2 points down.
	for text, top := range map[string]float64{"T": 28.35, "B": 785.2 - 141.73} {
		if r, ok := got[text]; !ok || r.y < top || r.y > top+15 {
			t.Errorf("%s at %+v, want its box's top at %v", text, r, top)
		}
	}
}

// TestPageBackground_Area locks where a page's fill is painted: a page
// layout's fo:background-color alone inside the margins, a draw:fill over
// the page unless draw:background-size says border, a stretched bitmap
// that names no size inside the margins, and the master page's
// drawing-page fill over the page layout's.
func TestPageBackground_Area(t *testing.T) {
	layout := func(name, props string) string {
		return `<style:page-layout style:name="` + name + `"><style:page-layout-properties fo:page-width="10cm" fo:page-height="10cm" fo:margin-top="1cm" fo:margin-bottom="1cm" fo:margin-left="1cm" fo:margin-right="1cm" ` + props + `/></style:page-layout>`
	}
	d := doc{
		styles: `<draw:fill-image draw:name="pic" xlink:href="Pictures/p.png"/>` +
			`<style:style style:name="dp" style:family="drawing-page"><style:drawing-page-properties draw:fill="solid" draw:fill-color="#00ff00" draw:background-size="border"/></style:style>`,
		autoStyles: layout("legacy", `fo:background-color="#ff0000"`) + layout("solid", `draw:fill="solid" draw:fill-color="#0000ff"`) +
			layout("stretch", `draw:fill="bitmap" draw:fill-image-name="pic" style:repeat="stretch"`) + layout("tile", `draw:fill="bitmap" draw:fill-image-name="pic"`),
		masters: `<style:master-page style:name="legacy" style:page-layout-name="legacy"/><style:master-page style:name="solid" style:page-layout-name="solid"/>` +
			`<style:master-page style:name="stretch" style:page-layout-name="stretch"/><style:master-page style:name="tile" style:page-layout-name="tile"/>` +
			`<style:master-page style:name="drawn" style:page-layout-name="solid" draw:style-name="dp"/>`,
		body: `<text:p>a</text:p>`,
	}
	b := newTestBuilder(t, d)
	m := 72 / 2.54
	for name, want := range map[string]struct {
		inset float64
		color css.Color
		image bool
	}{
		"legacy":  {m, css.Color{R: 1, A: 1}, false},
		"solid":   {0, css.Color{B: 1, A: 1}, false},
		"stretch": {m, css.Color{}, true},
		"tile":    {0, css.Color{}, true},
		"drawn":   {m, css.Color{G: 1, A: 1}, false},
	} {
		g := b.pages.buildGeom(b.styles.Master(name), false, false, false)
		bg, in := g.box.Background, g.box.BackgroundInset
		if bg == nil || in == nil {
			t.Errorf("%s: no background", name)
			continue
		}
		if !near(in[css.Top], want.inset, 0.01) || !near(in[css.Left], want.inset, 0.01) || bg.BackgroundColor != want.color || (bg.BackgroundImage != "") != want.image {
			t.Errorf("%s: inset %v, colour %+v, image %q; want inset %v, colour %+v, image %v", name, *in, bg.BackgroundColor, bg.BackgroundImage, want.inset, want.color, want.image)
		}
	}
}
