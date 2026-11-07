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
