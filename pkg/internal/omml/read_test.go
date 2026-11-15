package omml

import (
	"context"
	"math"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// zone parses body as the content of an m:oMath, or as an m:oMathPara
// holding it when para is set.
func zone(t *testing.T, body string, para bool) *ooxml.Element {
	t.Helper()
	x := `<m:oMath xmlns:m="` + ooxml.NSM + `" xmlns:w="` + ooxml.NSW + `">` + body + `</m:oMath>`
	if para {
		x = `<m:oMathPara xmlns:m="` + ooxml.NSM + `" xmlns:w="` + ooxml.NSW + `">` + x + `</m:oMathPara>`
	}
	root, err := ooxml.Parse(context.Background(), []byte(x), nil)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// black is the style of every run of the tests.
func black(*ooxml.Element) RunStyle { return RunStyle{Size: 11, Color: typeset.Color{A: 1}} }

// read reads body with the default settings.
func read(t *testing.T, body string) *Equation {
	t.Helper()
	q := Read(zone(t, body, false), Options{Settings: DefaultSettings(), Run: black})
	if q == nil {
		t.Fatal("no equation")
	}
	return q
}

// tree writes e as a compact S-expression: name, attributes, text,
// children.
func tree(e *formula.Element) string {
	var sb strings.Builder
	var walk func(e *formula.Element)
	walk = func(e *formula.Element) {
		sb.WriteString("(" + e.Name)
		for _, a := range e.Attrs {
			sb.WriteString(" " + a.Name + "=" + a.Value)
		}
		if e.Text != "" {
			sb.WriteString(" '" + e.Text + "'")
		}
		for _, c := range e.Children {
			sb.WriteByte(' ')
			walk(c)
		}
		sb.WriteByte(')')
	}
	walk(e)
	return sb.String()
}

// r returns a math run of text s with the math run properties props.
func r(s string, props ...string) string {
	pr := ""
	if len(props) > 0 {
		pr = "<m:rPr>" + strings.Join(props, "") + "</m:rPr>"
	}
	return "<m:r>" + pr + "<m:t>" + s + "</m:t></m:r>"
}

const runOp = " stretchy=false largeop=false movablelimits=false"

// TestRead_RunTokens maps a run's letters to identifiers in their
// mathematical italic forms, its digits to numbers, the rest to
// operators that keep their size, a hyphen-minus to a minus sign and a
// space to a space (22.1.2.87).
func TestRead_RunTokens(t *testing.T) {
	t.Parallel()
	q := read(t, r("E=mc")+r("12.5-x y"))
	want := "(math (mi mathvariant=normal '𝐸') (mo" + runOp + " '=') (mi mathvariant=normal '𝑚') (mi mathvariant=normal '𝑐') " +
		"(mn '12.5') (mo" + runOp + " '−') (mi mathvariant=normal '𝑥') (mspace width=0.2222em) (mi mathvariant=normal '𝑦'))"
	if got := tree(q.root); got != want {
		t.Fatalf("tree\n%s\nwant\n%s", got, want)
	}
	if got := q.AltText(); got != "𝐸 = 𝑚 𝑐 12.5 − 𝑥 𝑦" {
		t.Errorf("alt text %q", got)
	}
}

// TestRead_RunStyles sets a run's letters by its sty and scr (22.1.2.111,
// 22.1.2.94): plain runs whole and upright, bold, script, fraktur and
// double-struck in the Mathematical Alphanumeric Symbols, the letters
// Unicode keeps in Letterlike Symbols among them, and normal text
// (22.1.2.74 nor) as one text token.
func TestRead_RunStyles(t *testing.T) {
	t.Parallel()
	sty := func(v string) string { return `<m:sty m:val="` + v + `"/>` }
	scr := func(v string) string { return `<m:scr m:val="` + v + `"/>` }
	for _, c := range []struct {
		run, want string
	}{
		{r("sin", sty("p")), "(mi mathvariant=normal 'sin')"},
		{r("b2", sty("b")), "(mi mathvariant=normal '𝐛') (mn '𝟐')"},
		{r("x", sty("bi")), "(mi mathvariant=normal '𝒙')"},
		{r("R", scr("double-struck")), "(mi mathvariant=normal 'ℝ')"},
		{r("L", scr("script")), "(mi mathvariant=normal 'ℒ')"},
		{r("A", scr("script")), "(mi mathvariant=normal '𝒜')"},
		{r("g", scr("fraktur")), "(mi mathvariant=normal '𝔤')"},
		{r("h"), "(mi mathvariant=normal 'ℎ')"},
		{r("α", sty("b")), "(mi mathvariant=normal '𝛂')"},
		{r("a b", "<m:nor/>"), "(mtext 'a b')"},
	} {
		if got := tree(read(t, c.run).root); got != "(math "+c.want+")" {
			t.Errorf("%s: %s, want %s", c.run, got, c.want)
		}
	}
}

// TestStyled_Holes maps the letters Unicode leaves out of the Mathematical
// Alphanumeric Symbols to Letterlike Symbols, and the Greek letters by
// their order in each block.
func TestStyled_Holes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   rune
		v    variant
		want rune
	}{
		{'C', doubleStruck, 'ℂ'}, {'D', doubleStruck, '𝔻'}, {'Z', fraktur, 'ℨ'}, {'e', script, 'ℯ'},
		{'a', monospace, '𝚊'}, {'9', sansBold, '𝟵'}, {'Ω', italic, '𝛺'}, {'ϖ', bold, '𝛡'},
		{'ϴ', bold, '𝚹'}, {'∇', italic, '𝛻'}, {'ı', italic, '𝚤'}, {'+', bold, '+'}, {'x', normal, 'x'},
	} {
		if got := styled(c.in, c.v); got != c.want {
			t.Errorf("styled(%q, %d) = %q, want %q", c.in, c.v, got, c.want)
		}
	}
}

// TestRead_Objects maps each math object of 22.1.2 to its MathML Core
// elements.
func TestRead_Objects(t *testing.T) {
	t.Parallel()
	e := func(tag, body string) string { return "<m:" + tag + ">" + body + "</m:" + tag + ">" }
	x := e("e", r("x"))
	mi := func(s string) string { return "(mrow (mi mathvariant=normal '" + s + "'))" }
	for _, c := range []struct {
		name, body, want string
	}{
		{"fraction", e("f", e("num", r("a"))+e("den", r("b"))), "(mfrac " + mi("𝑎") + " " + mi("𝑏") + ")"},
		{"fraction without a bar", e("f", `<m:fPr><m:type m:val="noBar"/></m:fPr>`+e("num", r("a"))+e("den", r("b"))),
			"(mfrac linethickness=0 " + mi("𝑎") + " " + mi("𝑏") + ")"},
		{"linear fraction", e("f", `<m:fPr><m:type m:val="lin"/></m:fPr>`+e("num", r("a"))+e("den", r("b"))),
			"(mrow " + mi("𝑎") + " (mo stretchy=false '⁄') " + mi("𝑏") + ")"},
		{"square root", e("rad", `<m:radPr><m:degHide m:val="1"/></m:radPr><m:deg/>`+x), "(msqrt " + mi("𝑥") + ")"},
		{"cube root", e("rad", e("deg", r("3"))+x), "(mroot " + mi("𝑥") + " (mrow (mn '3')))"},
		{"scripts", e("sSubSup", x+e("sub", r("i"))+e("sup", r("2"))),
			"(msubsup " + mi("𝑥") + " " + mi("𝑖") + " (mrow (mn '2')))"},
		{"prescripts", e("sPre", e("sub", r("1"))+e("sup", r("n"))+x),
			"(mmultiscripts " + mi("𝑥") + " (mprescripts) (mrow (mn '1')) " + mi("𝑛") + ")"},
		{"sum", e("nary", `<m:naryPr><m:chr m:val="∑"/></m:naryPr>`+e("sub", r("i"))+e("sup", r("n"))+x),
			"(mrow (munderover (mo largeop=true movablelimits=true '∑') " + mi("𝑖") + " " + mi("𝑛") + ") " + mi("𝑥") + ")"},
		{"integral", e("nary", `<m:naryPr><m:supHide m:val="1"/></m:naryPr>`+e("sub", r("D"))+e("sup", "")+x),
			"(mrow (msub (mo largeop=true movablelimits=false '∫') " + mi("𝐷") + ") " + mi("𝑥") + ")"},
		{"delimiters", e("d", `<m:dPr><m:begChr m:val="["/><m:sepChr m:val=","/><m:endChr m:val=""/></m:dPr>`+x+x),
			"(mrow (mo form=prefix fence=true stretchy=true symmetric=true '[') " + mi("𝑥") +
				" (mo separator=true stretchy=true symmetric=true ',') " + mi("𝑥") + ")"},
		{"group character", e("groupChr", x), "(munder " + mi("𝑥") + " (mo stretchy=true '⏟'))"},
		{"accent", e("acc", `<m:accPr><m:chr m:val="̃"/></m:accPr>`+x), "(mover " + mi("𝑥") + " (mo scriptlevel=+0 '˜'))"},
		{"function", e("func", e("fName", r("sin", `<m:sty m:val="p"/>`))+x),
			"(mrow (mrow (mi mathvariant=normal 'sin')) (mspace width=0.1667em) " + mi("𝑥") + ")"},
		{"lower limit", e("limLow", x+e("lim", r("0"))), "(munder " + mi("𝑥") + " (mrow (mn '0')))"},
		{"phantom", e("phant", `<m:phantPr><m:show m:val="0"/><m:zeroWid/></m:phantPr>`+x),
			"(mphantom (mpadded width=0 " + mi("𝑥") + "))"},
		{"operator box", e("box", `<m:boxPr><m:opEmu/></m:boxPr>`+e("e", r("=="))), "(mo '==')"},
		{"border box", e("borderBox", `<m:borderBoxPr><m:hideTop/><m:strikeH/></m:borderBoxPr>`+x),
			"(menclose notation=bottom left right horizontalstrike " + mi("𝑥") + ")"},
		{"argument size", e("sSup", x+e("sup", `<m:argPr><m:argSz m:val="-1"/></m:argPr>`+r("2"))),
			"(msup " + mi("𝑥") + " (mrow scriptlevel=+1 (mn '2')))"},
	} {
		if got := tree(read(t, c.body).root); got != "(math "+c.want+")" {
			t.Errorf("%s:\n%s\nwant\n%s", c.name, got, "(math "+c.want+")")
		}
	}
}

// TestRead_OperatorsInScripts sets the operators of a script argument
// without their spaces, as Word does.
//
// # Corpus evidence
//
//   - litho/docx equations-basic: the limit n→∞ under lim is 19.4 points
//     wide at a script size of 8 points, its three glyphs side by side.
func TestRead_OperatorsInScripts(t *testing.T) {
	t.Parallel()
	got := tree(read(t, "<m:limLow><m:e>"+r("lim")+"</m:e><m:lim>"+r("n→∞")+"</m:lim></m:limLow>").root)
	if !strings.Contains(got, "(mo"+runOp+" lspace=0 rspace=0 '→')") {
		t.Fatalf("tree %s: the arrow keeps its spaces", got)
	}
}

// TestRead_RunSizeAndColour states the size and colour of a run that
// differ from the equation's first run on its tokens.
func TestRead_RunSizeAndColour(t *testing.T) {
	t.Parallel()
	body := r("a") + `<m:r><w:rPr><w:sz w:val="44"/></w:rPr><m:t>b</m:t></m:r>`
	q := Read(zone(t, body, false), Options{Settings: DefaultSettings(), Run: func(e *ooxml.Element) RunStyle {
		if e != nil && e.Find(ooxml.NSW, "sz") != nil {
			return RunStyle{Size: 22, Color: typeset.Color{R: 1, A: 1}}
		}
		return RunStyle{Size: 11, Color: typeset.Color{A: 1}}
	}})
	want := "(math (mi mathvariant=normal '𝑎') (mi mathvariant=normal mathsize=200.00% mathcolor=#ff0000 '𝑏'))"
	if got := tree(q.root); got != want {
		t.Fatalf("tree %s, want %s", got, want)
	}
	if st := q.Style(); st.Size != 11 || st.Color != (typeset.Color{A: 1}) || st.Families[0] != "Cambria Math" || st.Families[1] != "math" || st.Enclose == nil {
		t.Errorf("style %+v", st)
	}
}

// TestRead_MathParagraph reads an m:oMathPara as a display equation, its
// m:oMath children on one line (MS-OI29500, 2.1.1689, a), justified as
// its m:jc says.
func TestRead_MathParagraph(t *testing.T) {
	t.Parallel()
	x := `<m:oMathPara xmlns:m="` + ooxml.NSM + `"><m:oMathParaPr><m:jc m:val="right"/></m:oMathParaPr>` +
		`<m:oMath>` + r("a") + `</m:oMath><m:oMath>` + r("b") + `</m:oMath></m:oMathPara>`
	root, err := ooxml.Parse(context.Background(), []byte(x), nil)
	if err != nil {
		t.Fatal(err)
	}
	q := Read(root, Options{Settings: DefaultSettings(), Run: black})
	if !q.Display() || q.Justify() != Right || !q.Style().Display {
		t.Fatalf("display %v, justify %v", q.Display(), q.Justify())
	}
	if got := tree(q.root); got != "(math display=block (mi mathvariant=normal '𝑎') (mi mathvariant=normal '𝑏'))" {
		t.Errorf("tree %s", got)
	}
	if Read(zone(t, "", false), Options{Settings: DefaultSettings(), Run: black}) != nil {
		t.Error("an empty equation reads as one")
	}
}

// TestReadSettings reads m:mathPr over Word's defaults (22.1.2.62).
func TestReadSettings(t *testing.T) {
	t.Parallel()
	if got := ReadSettings(nil); got != DefaultSettings() {
		t.Fatalf("no mathPr: %+v", got)
	}
	x := `<m:mathPr xmlns:m="` + ooxml.NSM + `"><m:mathFont m:val="STIX Two Math"/><m:smallFrac/><m:dispDef m:val="0"/>` +
		`<m:lMargin m:val="1440"/><m:defJc m:val="left"/><m:intLim m:val="undOvr"/><m:naryLim m:val="subSup"/></m:mathPr>`
	root, err := ooxml.Parse(context.Background(), []byte(x), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{Font: "STIX Two Math", Justify: Left, LeftMargin: 72, IntegralLimitsUnder: true, SmallFractions: true}
	if got := ReadSettings(root); got != want {
		t.Fatalf("settings %+v, want %+v", got, want)
	}
}

// TestRead_Bounds leaves out what lies past the nesting and text bounds,
// with a warning.
func TestRead_Bounds(t *testing.T) {
	t.Parallel()
	deep := strings.Repeat("<m:sSup><m:e>", maxDepth+2) + r("x") + strings.Repeat("</m:e><m:sup>"+r("2")+"</m:sup></m:sSup>", maxDepth+2)
	q := read(t, deep)
	if !hasWarning(q, warn.LimitReached) {
		t.Error("no warning past the depth")
	}
	q = read(t, r(strings.Repeat("x", maxTextBytes+10)))
	if !hasWarning(q, warn.LimitReached) || len(q.root.Children) != maxTextBytes {
		t.Errorf("%d tokens past the text bound", len(q.root.Children))
	}
}

// hasWarning reports whether q warns with code.
func hasWarning(q *Equation, code warn.Code) bool {
	for _, w := range q.Warnings() {
		if w.Code == code {
			return true
		}
	}
	return false
}

// fixtureDejaVu holds the DejaVu faces of the reference image, its math
// font among them, provisioned with the corpus fonts.
const fixtureDejaVu = "../../../test/integration/testdata/fonts/gotenberg/truetype/dejavu"

var (
	dejavuOnce  sync.Once
	dejavuFonts *text.Fonts
	dejavuErr   error
)

// setter returns a formula setter over the DejaVu faces, skipping the
// test when the fixture fonts are absent.
func setter(t *testing.T) *formula.Setter {
	t.Helper()
	dejavuOnce.Do(func() {
		if _, dejavuErr = os.Stat(fixtureDejaVu + "/DejaVuMathTeXGyre.ttf"); dejavuErr != nil {
			return
		}
		dejavuFonts, dejavuErr = text.NewFonts(context.Background(), nil, []string{fixtureDejaVu})
	})
	if dejavuErr != nil {
		t.Skipf("fixture fonts absent: %v", dejavuErr)
	}
	return formula.NewSetter(dejavuFonts, nil)
}

// glyphX returns the left ends of the glyphs of b drawn at the origin
// that stand for s, in drawing order.
func glyphX(b *formula.Box, s string) []float64 {
	var out []float64
	for _, f := range b.Draw(0, 0) {
		if t, ok := f.(*typeset.Text); ok {
			for _, g := range t.Glyphs {
				if g.Text == s {
					out = append(out, float64(t.Origin.X+g.X))
				}
			}
		}
	}
	return out
}

// TestLayout_AlignsAnEquationArray sets the rows of an equation array so
// that their alignment marks stand one under the other (22.1.2.34), the
// first column flush right, the second flush left.
func TestLayout_AlignsAnEquationArray(t *testing.T) {
	s := setter(t)
	body := "<m:eqArr><m:e>" + r("x+y&amp;=5") + "</m:e><m:e>" + r("2x−y&amp;=1") + "</m:e><m:e>" + r("x&amp;=200") + "</m:e></m:eqArr>"
	q := read(t, body)
	st := q.Style()
	st.Families = []string{"DejaVu Math TeX Gyre"}
	b, err := q.Layout(context.Background(), s, st)
	if err != nil {
		t.Fatal(err)
	}
	eq := glyphX(b, "=")
	if len(eq) != 3 || math.Abs(eq[0]-eq[1]) > 0.01 || math.Abs(eq[0]-eq[2]) > 0.01 {
		t.Fatalf("equals signs at %v, want one column", eq)
	}
	// Laid out again, at another size, the marks still align.
	st.Size = 20
	if b, err = q.Layout(context.Background(), s, st); err != nil {
		t.Fatal(err)
	}
	if eq = glyphX(b, "="); math.Abs(eq[0]-eq[2]) > 0.05 {
		t.Fatalf("equals signs at %v at 20 points", eq)
	}
}

// TestLayout_BarSpansItsBase draws the rule of an m:bar as wide as its
// base (22.1.2.7).
func TestLayout_BarSpansItsBase(t *testing.T) {
	s := setter(t)
	q := read(t, `<m:bar><m:barPr><m:pos m:val="top"/></m:barPr><m:e>`+r("ABCD")+`</m:e></m:bar>`)
	st := q.Style()
	st.Families = []string{"DejaVu Math TeX Gyre"}
	b, err := q.Layout(context.Background(), s, st)
	if err != nil {
		t.Fatal(err)
	}
	var rule *typeset.Path
	for _, f := range b.Draw(0, 0) {
		if p, ok := f.(*typeset.Path); ok {
			rule = p
		}
	}
	if rule == nil {
		t.Fatal("no rule")
	}
	w := rule.Ops[1].P[0].X - rule.Ops[0].P[0].X
	if math.Abs(w-b.Width) > 0.05 {
		t.Fatalf("rule %g wide over a base %g wide", w, b.Width)
	}
}

// FuzzRead reads arbitrary Office Math without failing or exceeding its
// bounds.
func FuzzRead(f *testing.F) {
	f.Add(`<m:oMath xmlns:m="` + ooxml.NSM + `"><m:f><m:num><m:r><m:t>1</m:t></m:r></m:num><m:den><m:r><m:t>2</m:t></m:r></m:den></m:f></m:oMath>`)
	f.Add(`<m:oMath xmlns:m="` + ooxml.NSM + `"><m:eqArr><m:e><m:r><m:t>a&amp;=`)
	f.Fuzz(func(t *testing.T, x string) {
		root, err := ooxml.Parse(context.Background(), []byte(x), nil)
		if err != nil {
			return
		}
		q := Read(root, Options{Settings: DefaultSettings(), Run: black})
		if q != nil {
			_ = q.AltText()
		}
	})
}

// TestLayout_RowSpacing sets the rows of an equation array exactly rSp
// twips apart under rSpRule 3 (22.1.2.92, 22.1.2.93).
//
// # Corpus evidence
//
//   - openxmlsdk/docx O14ISOStrict/Word/rSp-Row_Spacing_Equation-Array_Function_-val-1080:
//     Word sets the rows a, b and c 54 points apart.
func TestLayout_RowSpacing(t *testing.T) {
	s := setter(t)
	q := read(t, `<m:eqArr><m:eqArrPr><m:rSpRule m:val="3"/><m:rSp m:val="1080"/></m:eqArrPr><m:e>`+r("a")+`</m:e><m:e>`+r("b")+`</m:e><m:e>`+r("c")+`</m:e></m:eqArr>`)
	st := q.Style()
	st.Families = []string{"DejaVu Math TeX Gyre"}
	b, err := q.Layout(context.Background(), s, st)
	if err != nil {
		t.Fatal(err)
	}
	var ys []float64
	for _, f := range b.Draw(0, 0) {
		if tx, ok := f.(*typeset.Text); ok {
			ys = append(ys, tx.Origin.Y)
		}
	}
	if len(ys) != 3 || math.Abs(ys[1]-ys[0]-54) > 0.01 || math.Abs(ys[2]-ys[1]-54) > 0.01 {
		t.Fatalf("baselines at %v, want 54 points apart", ys)
	}
}
