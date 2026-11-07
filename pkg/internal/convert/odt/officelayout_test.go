package odt

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
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
		{paraState{line: lineProportional, lineVal: 1.5}, css.LineRuleMultiple, css.Dimension{Kind: css.Number, Value: 1.5}, 0, 0},
		{paraState{line: lineFixed, lineVal: 20}, css.LineRuleExact, css.Pts(20), 0, 0},
		{paraState{line: lineAtLeast, lineVal: 20}, css.LineRuleMultiple, css.Dimension{Kind: css.Number, Value: 1}, 20, 0},
		{paraState{line: lineSpacing, lineVal: 13.8}, css.LineRuleMultiple, css.Dimension{Kind: css.Number, Value: 2}, 0, 13.8},
	}
	for _, tc := range cases {
		st := baseStyle()
		b.lineRule(st, tc.p, ts)
		if st.LineRule != tc.rule || !near(st.LineHeight.Value, tc.lh.Value, 1e-9) || st.LineHeight.Kind != tc.lh.Kind ||
			st.MinLineHeight != tc.min || !near(st.Padding[css.Top].Value, tc.pad, 1e-9) {
			t.Errorf("%+v: rule %v, line height %+v, min %v, padding %+v", tc.p, st.LineRule, st.LineHeight, st.MinLineHeight, st.Padding[css.Top])
		}
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

// TestConvert_BordersCloned checks that a bordered paragraph and a
// bordered cell draw their whole border on each page they span, as
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
	if strings.Join(found, ",") != "P,TD" {
		t.Errorf("cloned decorations on %q, want the paragraph and the cell", found)
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
// rule a quarter of the area wide on the left, 0.1 cm from the notes; a
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
		{"absent", ``, layout.FootnoteRule{Width: 0.25, Thickness: 0.5, Color: css.Color{A: 1}, Align: css.AlignLeft, Line: css.Solid, SpaceAbove: 0.1 * cm, SpaceBelow: 0.1 * cm}},
		{"centred", `<style:footnote-sep style:width="0.1cm" style:distance-before-sep="0.5cm" style:distance-after-sep="0.3cm" style:line-style="solid" style:adjustment="center" style:rel-width="50%" style:color="#ff0000"/>`,
			layout.FootnoteRule{Width: 0.5, Thickness: 0.1 * cm, Color: css.Color{R: 1, A: 1}, Align: css.AlignCenter, Line: css.Solid, SpaceAbove: 0.5 * cm, SpaceBelow: 0.3 * cm}},
		{"dashed right", `<style:footnote-sep style:width="1pt" style:line-style="dash" style:adjustment="right"/>`,
			layout.FootnoteRule{Width: 0.25, Thickness: 1, Color: css.Color{A: 1}, Align: css.AlignRight, Line: css.Dashed, SpaceAbove: 0.1 * cm, SpaceBelow: 0.1 * cm}},
		{"no thickness", `<style:footnote-sep style:width="0cm" style:distance-before-sep="0.2cm"/>`,
			layout.FootnoteRule{Width: 0.25, Color: css.Color{A: 1}, Align: css.AlignLeft, Line: css.BorderNone, SpaceAbove: 0.2 * cm, SpaceBelow: 0.1 * cm}},
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
// controls whatever their fo:border: a thin black line around a text
// field, a point-wide one around a check box (the pinned image prints
// form.odt so).
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
			if n.Style.Border[css.Top].Color != (css.Color{A: 1}) || n.Style.Border[css.Top].Style != css.Solid {
				t.Errorf("border = %+v", n.Style.Border[css.Top])
			}
		}
	})
	if len(widths) != 2 || widths[0] != 0.5 || widths[1] != 1 {
		t.Errorf("control border widths = %v, want [0.5 1]", widths)
	}
}
