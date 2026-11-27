package ods

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestRead_FormControls locks what Calc prints of a sheet's push button
// and check box: the button a grey box of its frame, its label centred
// across it; the check box a white box of its frame, an outlined square at
// its left edge centred on its height, the label right of it and the ✘
// mark of its checked state in it; the label of each on one line centred
// on the frame's height, in DejaVu Sans at 8 points. A control of another
// kind prints nothing, with a warning.
func TestRead_FormControls(t *testing.T) {
	dirs := []string{
		filepath.Join("..", "..", "..", "..", "test", "integration", "testdata", "fonts", "dejavu-2.37"),
		filepath.Join("..", "..", "..", "..", "test", "integration", "testdata", "fonts", "opensymbol-102.13"),
	}
	for _, d := range dirs {
		if _, err := os.Stat(d); err != nil {
			t.Skip("fixture fonts not fetched (make corpus-fetch CORPUS=fonts)")
		}
	}
	fonts, err := text.NewFonts(context.Background(), nil, dirs)
	if err != nil {
		t.Fatal(err)
	}
	const formNS = `xmlns:form="urn:oasis:names:tc:opendocument:xmlns:form:1.0" xmlns:xml="http://www.w3.org/XML/1998/namespace"`
	f := flat{
		body: `<table:table table:name="S"><office:forms ` + formNS + `><form:form form:name="Form">` +
			`<form:button form:name="B" form:id="c1" form:label="Push Button"/>` +
			`<form:checkbox form:name="C" xml:id="c2" form:id="c2" form:label="Check Box" form:current-state="checked"/>` +
			`<form:text form:name="T" form:id="c3"/>` +
			`</form:form></office:forms>` +
			`<table:shapes>` +
			`<draw:control svg:x="1in" svg:y="1in" svg:width="60pt" svg:height="20pt" draw:control="c1"/>` +
			`<draw:control svg:x="1in" svg:y="2in" svg:width="120pt" svg:height="40pt" draw:control="c2"/>` +
			`<draw:control svg:x="1in" svg:y="3in" svg:width="120pt" svg:height="40pt" draw:control="c3"/>` +
			`</table:shapes><table:table-row><table:table-cell/></table:table-row></table:table>`,
	}
	doc, w, err := Read(context.Background(), f.bytes(), fonts, office.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(w, warn.FormWidgetOmitted) {
		t.Errorf("no warning for the text field: %v", w)
	}
	ds := doc.Sheets[0].Drawings
	if len(ds) != 2 {
		t.Fatalf("%d drawings, want the button and the check box", len(ds))
	}
	type textAt struct {
		s       string
		x, y    float64
		size    float64
		clipped bool
	}
	scan := func(i int) (fills []typeset.Color, strokes []typeset.Rect, texts []textAt) {
		d := ds[i]
		var walk func(frags []typeset.Fragment, m typeset.Matrix, clipped bool)
		walk = func(frags []typeset.Fragment, m typeset.Matrix, clipped bool) {
			for _, f := range frags {
				switch v := f.(type) {
				case *typeset.Group:
					t := v.Transform
					if t == (typeset.Matrix{}) {
						t = typeset.Matrix{A: 1, D: 1}
					}
					walk(v.Content, mul(m, t), clipped || v.Clip != nil)
				case *typeset.Path:
					if v.Fill != nil {
						fills = append(fills, v.Fill.Color)
					}
					if v.Stroke != nil {
						p0, p2 := v.Ops[0].P[0], v.Ops[2].P[0]
						x0, y0 := apply(m, p0.X, p0.Y)
						x2, y2 := apply(m, p2.X, p2.Y)
						strokes = append(strokes, typeset.Rect{X: x0, Y: y0, Width: x2 - x0, Height: y2 - y0})
					}
				case *typeset.Text:
					var b strings.Builder
					for _, g := range v.Glyphs {
						b.WriteString(g.Text)
					}
					x, y := apply(m, v.Origin.X, v.Origin.Y)
					texts = append(texts, textAt{b.String(), x, y, v.Size, clipped})
				}
			}
		}
		walk(d.Picture.Vector(context.Background(), typeset.Rect{Width: d.Width, Height: d.Height}), typeset.Matrix{A: 1, D: 1}, false)
		return fills, strokes, texts
	}
	// DejaVu Sans at 8 points: ascent 7.424, descent 1.888.
	const baseBelowMiddle = (7.424 - 1.888) / 2

	fills, strokes, texts := scan(0)
	if len(fills) != 1 || !near(fills[0].R, 0xc0/255.0) || len(strokes) != 0 {
		t.Errorf("button fills %v strokes %v, want one grey fill", fills, strokes)
	}
	if len(texts) != 1 || texts[0].s != "Push Button" || !texts[0].clipped || texts[0].size != 8 ||
		math.Abs(texts[0].y-(10+baseBelowMiddle)) > 0.05 {
		t.Fatalf("button text %+v", texts)
	}
	l, _ := oneLine(context.Background(), fonts, "Push Button", controlFamily, controlSize)
	if math.Abs(texts[0].x-(60-l.Width)/2) > 0.01 {
		t.Errorf("button label at x %v, want centred at %v", texts[0].x, (60-l.Width)/2)
	}

	fills, strokes, texts = scan(1)
	if len(fills) != 1 || fills[0].R != 1 || len(strokes) != 1 {
		t.Fatalf("check box fills %v strokes %v, want one white fill and one square", fills, strokes)
	}
	sq := strokes[0]
	if !near(sq.X, checkLine) || !near(sq.Width, checkSide) || !near(sq.Y, 20-checkSide/2) {
		t.Errorf("square %+v, want %v wide at x %v centred on 20", sq, checkSide, checkLine)
	}
	if len(texts) != 2 || texts[0].s != "✘" || texts[1].s != "Check Box" {
		t.Fatalf("check box text %+v, want the mark then the label", texts)
	}
	if !near(texts[0].x, checkLine+checkMarkX) || !near(texts[0].y, 20+checkSide/2-checkMarkY) {
		t.Errorf("mark at (%v, %v)", texts[0].x, texts[0].y)
	}
	if lb := texts[1]; !near(lb.x, checkLine+checkSide+checkGap) || math.Abs(lb.y-(20+baseBelowMiddle)) > 0.05 || !lb.clipped {
		t.Errorf("label %+v", lb)
	}
}
