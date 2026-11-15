package odf

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// objectOf parses a draw:object holding inner, in a flat document's
// namespaces.
func objectOf(t testing.TB, inner string) *Element {
	t.Helper()
	el, err := ParseXML(context.Background(), []byte(`<draw:object xmlns:draw="`+NSDraw+`" xmlns:office="`+NSOffice+`" xmlns:math="`+NSMathML+`" xmlns:xlink="`+NSXLink+`">`+inner+`</draw:object>`))
	if err != nil {
		t.Fatal(err)
	}
	return el
}

// treeOf writes a formula tree as name(children) with token texts.
func treeOf(e *formula.Element) string {
	if e == nil {
		return "<nil>"
	}
	var b strings.Builder
	b.WriteString(e.Name)
	if e.Text != "" {
		fmt.Fprintf(&b, "%q", e.Text)
	}
	if len(e.Children) > 0 {
		b.WriteString("(")
		for i, c := range e.Children {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(treeOf(c))
		}
		b.WriteString(")")
	}
	return b.String()
}

func TestReadFormula_Inline(t *testing.T) {
	obj := objectOf(t, `<math xmlns="http://www.w3.org/1998/Math/MathML" display="block"><semantics>`+
		`<mrow><msup><mi mathvariant="italic">a</mi><mn>2</mn></msup><mo stretchy="false">+</mo>`+
		`<mfenced open="[" separators=";"><mi>x</mi><mi>y</mi><mi>z</mi></mfenced>`+
		`<mstyle color="red" fontsize="14pt"><mi>q</mi></mstyle></mrow>`+
		`<annotation encoding="StarMath 5.0">a^2 + left[x;y;z right] + color red q</annotation></semantics></math>`)
	f, err := ReadFormula(context.Background(), nil, obj)
	if err != nil || f == nil {
		t.Fatalf("ReadFormula = %v, %v", f, err)
	}
	want := `math(semantics(mrow(msup(mi"a" mn"2") mo"+" mrow(mo"[" mrow(mi"x" mo";" mi"y" mo";" mi"z") mo")") mstyle(mi"q"))))`
	if got := treeOf(f.Root); got != want {
		t.Errorf("tree\n got %s\nwant %s", got, want)
	}
	if f.Size != 12 || f.Families[0] != "Liberation Serif" || f.Operators[0] != "OpenSymbol" || f.ScriptScale != 0.6 {
		t.Errorf("defaults: size %g, families %v, operators %v, script scale %g", f.Size, f.Families, f.Operators, f.ScriptScale)
	}
	st := f.Root.Children[0].Children[0].Children[3]
	if v, _ := st.Attr("mathcolor"); v != "red" {
		t.Errorf("mstyle color mapped to mathcolor %q, want red", v)
	}
	if v, _ := st.Attr("mathsize"); v != "14pt" {
		t.Errorf("mstyle fontsize mapped to mathsize %q, want 14pt", v)
	}
	fence := f.Root.Children[0].Children[0].Children[2].Children[0]
	if v, _ := fence.Attr("fence"); v != "true" {
		t.Errorf("mfenced's open fence has fence %q, want true", v)
	}
	if v, _ := fence.Attr("lspace"); v != operatorSpace {
		t.Errorf("operator lspace %q, want %q", v, operatorSpace)
	}
}

func TestReadFormula_FencedExpansions(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`<mfenced/>`, `mrow(mo"(" mo")")`},
		{`<mfenced><mi>a</mi></mfenced>`, `mrow(mo"(" mi"a" mo")")`},
		{`<mfenced open="" close="|" separators=""><mi>a</mi><mi>b</mi></mfenced>`, `mrow(mrow(mi"a" mi"b") mo"|")`},
		{`<mfenced separators=" , ; "><mi>a</mi><mi>b</mi><mi>c</mi><mi>d</mi></mfenced>`, `mrow(mo"(" mrow(mi"a" mo"," mi"b" mo";" mi"c" mo";" mi"d") mo")")`},
	} {
		f, err := ReadFormula(context.Background(), nil, objectOf(t, `<math xmlns="http://www.w3.org/1998/Math/MathML">`+tc.in+`</math>`))
		if err != nil || f == nil {
			t.Fatalf("%s: %v, %v", tc.in, f, err)
		}
		if got := treeOf(f.Root.Children[0]); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.in, got, tc.want)
		}
	}
}

func TestReadFormula_ElementsCoreLacks(t *testing.T) {
	f, err := ReadFormula(context.Background(), nil, objectOf(t, `<math:math><math:mtable>`+
		`<math:mlabeledtr><math:mtd><math:mtext>(1)</math:mtext></math:mtd><math:mtd><math:mi>x</math:mi><math:maligngroup/></math:mtd></math:mlabeledtr>`+
		`</math:mtable><math:menclose notation="box"><math:mi>y</math:mi></math:menclose><math:mi><math:mglyph src="a.png"/></math:mi>`+
		`<math:mmultiscripts><math:mi>z</math:mi><math:none/><math:mn>1</math:mn></math:mmultiscripts></math:math>`))
	if err != nil || f == nil {
		t.Fatal(err)
	}
	want := `math(mtable(mtr(mtd(mi"x"))) menclose(mi"y") mi mmultiscripts(mi"z" mrow mn"1"))`
	if got := treeOf(f.Root); got != want {
		t.Errorf("tree\n got %s\nwant %s", got, want)
	}
	var msgs []string
	for _, w := range f.Warnings {
		msgs = append(msgs, w.Message)
	}
	all := strings.Join(msgs, "|")
	for _, w := range []string{"labels", "alignment", "mglyph"} {
		if !strings.Contains(all, w) {
			t.Errorf("no warning of %s in %q", w, all)
		}
	}
}

func TestReadFormula_StarMathOnly(t *testing.T) {
	f, err := ReadFormula(context.Background(), nil, objectOf(t, `<math:math><math:semantics><math:mrow/><math:annotation math:encoding="StarMath 5.0">a+b</math:annotation></math:semantics></math:math>`))
	if err != nil || f == nil {
		t.Fatalf("ReadFormula = %v, %v", f, err)
	}
	if f.Root != nil {
		t.Errorf("a formula holding only its StarMath text has the tree %s, want none", treeOf(f.Root))
	}
}

func TestReadFormula_Bounds(t *testing.T) {
	deep := strings.Repeat("<mrow>", formula.MaxDepth+5) + "<mi>x</mi>" + strings.Repeat("</mrow>", formula.MaxDepth+5)
	f, err := ReadFormula(context.Background(), nil, objectOf(t, `<math xmlns="http://www.w3.org/1998/Math/MathML">`+deep+`</math>`))
	if err != nil || f == nil {
		t.Fatal(err)
	}
	if len(f.Warnings) != 1 || !strings.Contains(f.Warnings[0].Message, "nested") {
		t.Errorf("warnings %v, want one of the nesting", f.Warnings)
	}
	r := &mathReader{count: formula.MaxElements}
	if e := r.element(&Element{}, 0); e != nil || len(r.warnings) != 1 {
		t.Errorf("an element past the element bound gives %v and warnings %v", e, r.warnings)
	}
}

func TestReadFormula_Package(t *testing.T) {
	manifest := `<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0">` +
		`<manifest:file-entry manifest:full-path="/" manifest:media-type="application/vnd.oasis.opendocument.text"/>` +
		`<manifest:file-entry manifest:full-path="Object 1/" manifest:media-type="application/vnd.oasis.opendocument.formula"/>` +
		`<manifest:file-entry manifest:full-path="Object 2/" manifest:media-type="application/vnd.oasis.opendocument.chart"/>` +
		`</manifest:manifest>`
	settings := `<office:document-settings xmlns:office="` + NSOffice + `" xmlns:config="` + configNS + `"><office:settings><config:config-item-set config:name="ooo:configuration-settings">` +
		`<config:config-item config:name="BaseFontHeight" config:type="short">20</config:config-item>` +
		`<config:config-item config:name="FontNameVariables" config:type="string">DejaVu Serif</config:config-item>` +
		`<config:config-item config:name="RelativeFontHeightIndices" config:type="short">50</config:config-item>` +
		`<config:config-item config:name="ViewAreaWidth" config:type="long">886</config:config-item>` +
		`<config:config-item config:name="ViewAreaHeight" config:type="long">301</config:config-item>` +
		`<config:config-item config:name="BaseLine" config:type="int">439</config:config-item>` +
		`</config:config-item-set></office:settings></office:document-settings>`
	data := zipOf(t, "mimetype", MimeText, "META-INF/manifest.xml", manifest, "content.xml", minimalContent,
		"Object 1/content.xml", `<math xmlns="http://www.w3.org/1998/Math/MathML"><mi>x</mi></math>`, "Object 1/settings.xml", settings,
		"Object 2/content.xml", `<math xmlns="http://www.w3.org/1998/Math/MathML"><mi>x</mi></math>`,
		"Object 3/content.xml", `<math xmlns="http://www.w3.org/1998/Math/MathML"><mi>y</mi></math>`)
	pkg, err := Open(context.Background(), data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ref := func(href string) *Element { return objectOf(t, ``).withHref(href) }
	f, err := ReadFormula(context.Background(), pkg, ref("./Object 1"))
	if err != nil || f == nil {
		t.Fatalf("ReadFormula = %v, %v", f, err)
	}
	if f.Size != 20 || f.Families[0] != "DejaVu Serif" || f.ScriptScale != 0.5 || treeOf(f.Root) != `math(mi"x")` {
		t.Errorf("formula: size %g, families %v, script scale %g, tree %s", f.Size, f.Families, f.ScriptScale, treeOf(f.Root))
	}
	if want := (formulaView{width: 44.3, height: 15.05, baseline: float64(439*72) / 2540}); f.view != want {
		t.Errorf("view %+v, want %+v", f.view, want)
	}
	if f, err := ReadFormula(context.Background(), pkg, ref("Object 2")); f != nil || err != nil {
		t.Errorf("a chart object reads as the formula %v, %v", f, err)
	}
	if f, err := ReadFormula(context.Background(), pkg, ref("Object 3")); f == nil || err != nil {
		t.Errorf("an object the manifest does not list whose content is MathML reads as %v, %v", f, err)
	}
	if f, err := ReadFormula(context.Background(), pkg, ref("./Object 9")); f != nil || err != nil {
		t.Errorf("a missing object reads as %v, %v", f, err)
	}
}

// withHref returns e with the xlink:href href.
func (e *Element) withHref(href string) *Element {
	c := *e
	c.Attr = append(c.Attr[:len(c.Attr):len(c.Attr)], xml.Attr{Name: xml.Name{Space: NSXLink, Local: "href"}, Value: href})
	return &c
}

// libreOfficeFonts holds the reference image's Liberation faces and
// OpenSymbol, provisioned with the corpus fonts.
var libreOfficeFonts = []string{
	"../../../test/integration/testdata/fonts/gotenberg/truetype/liberation",
	"../../../test/integration/testdata/fonts/gotenberg/truetype/libreoffice",
}

// fittedOf lays the MathML math out in the reference image's fonts,
// skipping the test when they are absent.
func fittedOf(t *testing.T, math string) (*Formula, *FittedFormula, *text.Fonts) {
	t.Helper()
	if _, err := os.Stat(libreOfficeFonts[1]); err != nil {
		t.Skipf("fixture fonts absent: %v", err)
	}
	fonts, err := text.NewFonts(context.Background(), nil, libreOfficeFonts)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ReadFormula(context.Background(), nil, objectOf(t, math))
	if err != nil {
		t.Fatal(err)
	}
	ff, err := f.Lay(context.Background(), formula.NewSetter(fonts, nil), fonts)
	if err != nil || ff == nil {
		t.Fatalf("Lay = %v, %v", ff, err)
	}
	return f, ff, fonts
}

// glyphRuns returns the text runs of frags with the transform of their
// groups, as a page would draw them.
func glyphRuns(frags []typeset.Fragment, m typeset.Matrix, out *[][2]any) {
	for _, fr := range frags {
		switch v := fr.(type) {
		case *typeset.Text:
			*out = append(*out, [2]any{v, m})
		case *typeset.Group:
			n := v.Transform
			if n == (typeset.Matrix{}) {
				n = typeset.Identity
			}
			glyphRuns(v.Content, typeset.Matrix{
				A: float64(m.A*n.A) + float64(m.C*n.B), B: float64(m.B*n.A) + float64(m.D*n.B),
				C: float64(m.A*n.C) + float64(m.C*n.D), D: float64(m.B*n.C) + float64(m.D*n.D),
				E: float64(m.A*n.E) + float64(m.C*n.F) + m.E, F: float64(m.B*n.E) + float64(m.D*n.F) + m.F,
			}, out)
		}
	}
}

// TestFittedFormula_FillsAFrameWithoutView locks the fill of a formula
// whose object records no visible area: its box is scaled to the frame
// on each axis, as Impress draws sd/Math.fodp.
func TestFittedFormula_FillsAFrameWithoutView(t *testing.T) {
	_, ff, _ := fittedOf(t, `<math xmlns="http://www.w3.org/1998/Math/MathML" display="block"><msup><mi>a</mi><mn>2</mn></msup></math>`)
	w, h := ff.Size()
	r := typeset.Rect{X: 10, Y: 20, Width: float64(w * 3), Height: float64(h * 2)}
	var runs [][2]any
	glyphRuns(ff.Draw(r), typeset.Identity, &runs)
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2", len(runs))
	}
	m := runs[0][1].(typeset.Matrix)
	if !nearly(m.A, 3) || !nearly(m.D, 2) || !nearly(m.E, 10) {
		t.Errorf("formula drawn by %+v, want scaled 3 by 2 from the frame's left", m)
	}
	if run := runs[0][0].(*typeset.Text); !strings.Contains(run.Face.Key, "Italic") {
		t.Errorf("a drawn in %s, want Liberation Serif Italic", run.Face.Key)
	}
	if run := runs[1][0].(*typeset.Text); !nearly(run.Size, 7.2) {
		t.Errorf("superscript at %g pt, want 60 per cent of 12", run.Size)
	}
}

// TestFittedFormula_KeepsTheViewScale locks the fit of a formula whose
// object records its visible area: drawn at the scale of that area to
// the frame, its baseline on the recorded baseline, shrunk alike on both
// axes when wider than the area.
func TestFittedFormula_KeepsTheViewScale(t *testing.T) {
	_, ff, _ := fittedOf(t, `<math xmlns="http://www.w3.org/1998/Math/MathML"><mi>x</mi></math>`)
	w, h := ff.Size()
	ff.view = formulaView{width: float64(w * 2), height: float64(h * 2), baseline: ff.ascent}
	r := typeset.Rect{Width: float64(w * 2), Height: float64(h * 2)}
	var runs [][2]any
	glyphRuns(ff.Draw(r), typeset.Identity, &runs)
	m := runs[0][1].(typeset.Matrix)
	if !nearly(m.A, 1) || !nearly(m.D, 1) || !nearly(m.F, 0) {
		t.Errorf("formula in a frame of its view drawn by %+v, want unscaled with its top at the frame's", m)
	}
	ff.view = formulaView{width: w / 2, height: h, baseline: ff.ascent}
	runs = nil
	glyphRuns(ff.Draw(typeset.Rect{Width: w, Height: float64(h * 2)}), typeset.Identity, &runs)
	m = runs[0][1].(typeset.Matrix)
	if !nearly(m.A, 1) || !nearly(m.D, 1) {
		t.Errorf("formula twice as wide as its view in a frame twice its view drawn by %+v, want unscaled", m)
	}
}

// nearly reports whether a and b differ by less than a thousandth.
func nearly(a, b float64) bool { return a-b < 1e-3 && b-a < 1e-3 }

func FuzzReadFormula(f *testing.F) {
	full := `<math xmlns="http://www.w3.org/1998/Math/MathML" display="block"><semantics><mrow><mfrac><mi>a</mi><mfenced><mn>2</mn><mn>3</mn></mfenced></mfrac>` +
		`<mstyle color="red"><mtable><mlabeledtr><mtd><mn>1</mn></mtd><mtd><msubsup><mi>x</mi><mn>1</mn><mn>2</mn></msubsup></mtd></mlabeledtr></mtable></mstyle></mrow>` +
		`<annotation encoding="StarMath 5.0">a over (2,3)</annotation></semantics></math>`
	f.Add([]byte(full))
	f.Add([]byte(full[:len(full)/2]))
	var fonts *text.Fonts
	if _, err := os.Stat(libreOfficeFonts[0]); err == nil {
		fonts, _ = text.NewFonts(context.Background(), nil, libreOfficeFonts[:1])
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		root, err := ParseXML(context.Background(), data)
		if err != nil {
			return
		}
		obj := &Element{Name: xml.Name{Space: NSDraw, Local: "object"}, Children: []*Element{root}}
		fm, err := ReadFormula(context.Background(), nil, obj)
		if err != nil || fm == nil || fonts == nil {
			return
		}
		ff, err := fm.Lay(context.Background(), formula.NewSetter(fonts, nil), fonts)
		if err != nil || ff == nil {
			return
		}
		ff.Draw(typeset.Rect{Width: 100, Height: 20})
	})
}

// paths returns the paths of frags, groups opened.
func paths(frags []typeset.Fragment, out *[]*typeset.Path) {
	for _, fr := range frags {
		switch v := fr.(type) {
		case *typeset.Path:
			*out = append(*out, v)
		case *typeset.Group:
			paths(v.Content, out)
		}
	}
}

// TestFittedFormula_DrawsOverstrikeAlone draws LibreOffice's overstrike,
// menclose notation="horizontalstrike", 0.298 em above the baseline, and
// no other notation, as LibreOffice draws a formula (libreOfficeEnclosure).
func TestFittedFormula_DrawsOverstrikeAlone(t *testing.T) {
	for _, c := range []struct {
		notation string
		lines    int
	}{{"horizontalstrike", 1}, {"box", 0}} {
		_, ff, _ := fittedOf(t, `<math xmlns="http://www.w3.org/1998/Math/MathML"><menclose notation="`+c.notation+`"><mi>abc</mi></menclose></math>`)
		w, h := ff.Size()
		ff.view = formulaView{width: w, height: h, baseline: ff.ascent}
		var ps []*typeset.Path
		paths(ff.Draw(typeset.Rect{Width: w, Height: h}), &ps)
		if len(ps) != c.lines {
			t.Fatalf("%s: %d lines, want %d", c.notation, len(ps), c.lines)
		}
		if c.lines == 1 {
			top, bottom := ps[0].Ops[0].P[0].Y, ps[0].Ops[2].P[0].Y
			if mid := ff.ascent - float64((top+bottom)/2); !nearly(mid, float64(0.298*12)) || !nearly(bottom-top, float64(0.034*12)) {
				t.Fatalf("strike %g up and %g thick, want 3.576 and 0.408", mid, bottom-top)
			}
		}
	}
}

// TestFittedFormula_LaysOutByLibreOfficeDistances draws a display sum
// at 1.455 times the base size and raises a superscript half an em, as
// LibreOffice Math does in fonts without a MATH table
// (libreOfficeDistances).
func TestFittedFormula_LaysOutByLibreOfficeDistances(t *testing.T) {
	_, ff, _ := fittedOf(t, `<math xmlns="http://www.w3.org/1998/Math/MathML" display="block"><mrow><munderover><mo>∑</mo><mi>i</mi><mi>n</mi></munderover><msup><mi>a</mi><mn>2</mn></msup></mrow></math>`)
	w, h := ff.Size()
	ff.view = formulaView{width: w, height: h, baseline: ff.ascent}
	var runs [][2]any
	glyphRuns(ff.Draw(typeset.Rect{Width: w, Height: h}), typeset.Identity, &runs)
	var sum, a, two *typeset.Text
	for _, r := range runs {
		tx := r[0].(*typeset.Text)
		switch tx.Glyphs[0].Text {
		case "∑":
			sum = tx
		case "a":
			a = tx
		case "2":
			two = tx
		}
	}
	if sum == nil || !nearly(sum.Size, 12*1.455) {
		t.Fatalf("sum %+v, want at 17.46 pt", sum)
	}
	if a == nil || two == nil || !nearly(float64(a.Origin.Y-two.Origin.Y), 6) {
		t.Fatalf("superscript raised %g, want 6", a.Origin.Y-two.Origin.Y)
	}
}
