package slides

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// TestText_HorizontalAnchor checks that a block of lines anchored at the
// centre or the end is as wide as its widest line, each line aligned in
// it.
func TestText_HorizontalAnchor(t *testing.T) {
	a20 := arial(20)
	long, short := para("Wide line", a20), para("Hi", a20)
	for _, tc := range []struct {
		name   string
		anchor HorizontalAnchor
		align  css.TextAlign
	}{
		{"start", HorizontalStart, css.AlignStart},
		{"centre", HorizontalCenter, css.AlignStart},
		{"end", HorizontalEnd, css.AlignStart},
		{"centre right-aligned", HorizontalCenter, css.AlignRight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, s := long, short
			l.Align, s.Align = tc.align, tc.align
			sh := box(100, 100, 400, 100, l, s)
			sh.Text.HorizontalAnchor = tc.anchor
			pg := convert(t, deck(&Slide{Shapes: []*Shape{sh}}), office.Options{}).Pages[0]
			w, hi := find(t, pg, "Wide"), find(t, pg, "Hi")
			wide := widthOf(t, "Wide line", a20)
			var wantX float64
			switch tc.anchor {
			case HorizontalStart:
				wantX = 100
				if tc.align == css.AlignRight {
					wantX = 500 - wide
				}
			case HorizontalCenter:
				wantX = 100 + (400-wide)/2
			case HorizontalEnd:
				wantX = 500 - wide
			}
			if !near(w.x, wantX, 0.5) {
				t.Errorf("wide line at %v, want %v", w.x, wantX)
			}
			if tc.align == css.AlignStart && !near(hi.x, w.x, 0.5) {
				t.Errorf("short line at %v, want the block's start %v", hi.x, w.x)
			}
		})
	}
}

// widthOf returns the advance of s set in rs.
func widthOf(t *testing.T, s string, rs RunStyle) float64 {
	t.Helper()
	b := &builder{ctx: t.Context(), fonts: pinnedFonts(t), metrics: map[string]text.Metrics{}}
	return b.width(s, b.textStyle(&rs, 1))
}

// TestText_ParagraphFields checks a paragraph's default tab interval, its
// line height floor, a run's explicit rise and a run's jump.
func TestText_ParagraphFields(t *testing.T) {
	a20 := arial(20)
	t.Run("default tab", func(t *testing.T) {
		p := para("a\tb", a20)
		p.DefaultTab = 50
		pg := convert(t, deck(&Slide{Shapes: []*Shape{box(100, 100, 400, 100, p)}}), office.Options{}).Pages[0]
		var bx float64
		for _, r := range runs(pg) {
			if r.text == "b" || r.text == "\tb" {
				bx = r.x
			}
		}
		if !near(bx, 150, 0.5) {
			t.Errorf("b at %v, want 150 in %v", bx, runs(pg))
		}
	})
	t.Run("line height floor", func(t *testing.T) {
		one, two := para("One", a20), para("Two", a20)
		two.LineSpacingAtLeast = 60
		pg := convert(t, deck(&Slide{Shapes: []*Shape{box(100, 100, 400, 200, one, two)}}), office.Options{}).Pages[0]
		// A 20-point line is 24 points tall, its baseline 20 below its
		// top; the floor adds 36 above the second line's glyphs.
		if a, b := find(t, pg, "One"), find(t, pg, "Two"); !near(b.y-a.y, 24+36, 0.3) {
			t.Errorf("baselines %v apart, want 60", b.y-a.y)
		}
		low := para("Low", a20)
		low.LineSpacingAtLeast = 10
		pg = convert(t, deck(&Slide{Shapes: []*Shape{box(100, 100, 400, 200, one, low)}}), office.Options{}).Pages[0]
		if a, b := find(t, pg, "One"), find(t, pg, "Low"); !near(b.y-a.y, 24, 0.3) {
			t.Errorf("baselines %v apart, want 24: a lower floor changes nothing", b.y-a.y)
		}
	})
	t.Run("rise", func(t *testing.T) {
		up := a20
		up.Script, up.Rise = Superscript, 0.5
		down := a20
		down.Rise = -0.25
		p := Paragraph{Runs: []Run{{Text: "Base ", Style: a20}, {Text: "Up", Style: up}, {Text: " Down", Style: down}}, Style: a20}
		pg := convert(t, deck(&Slide{Shapes: []*Shape{box(100, 100, 400, 100, p)}}), office.Options{}).Pages[0]
		base, u, d := find(t, pg, "Base"), find(t, pg, "Up"), find(t, pg, "Down")
		if !near(base.y-u.y, 10, 0.3) || !near(u.size, 20*scriptScale, 0.01) {
			t.Errorf("superscript %v up at %v, want 10 at %v", base.y-u.y, u.size, 20*scriptScale)
		}
		if !near(d.y-base.y, 5, 0.3) || d.size != 20 {
			t.Errorf("lowered run %v down at %v, want 5 at 20", d.y-base.y, d.size)
		}
	})
	t.Run("jump", func(t *testing.T) {
		p := Paragraph{Runs: []Run{{Text: "Next", Style: a20, Jump: 3}, {Text: " gone", Style: a20, Jump: 2}}, Style: a20}
		hidden := &Slide{Hidden: true}
		doc := deck(&Slide{Shapes: []*Shape{box(100, 100, 400, 100, p)}}, hidden, &Slide{})
		pg := convert(t, doc, office.Options{}).Pages[0]
		if len(pg.Links) != 1 || pg.Links[0].Dest == nil || pg.Links[0].Dest.Page != 2 || pg.Links[0].URI != "" {
			t.Fatalf("links %+v, want one to page 2", pg.Links)
		}
	})
}

// TestText_Stretch checks that a stretched body fills its rectangle: its
// glyphs scaled to the width, its lines to the height.
func TestText_Stretch(t *testing.T) {
	a20 := arial(20)
	sh := box(100, 100, 400, 96, para("Hi", a20), para("Wide", a20))
	sh.Text.Autofit = AutofitStretch
	pg := convert(t, deck(&Slide{Shapes: []*Shape{sh}}), office.Options{}).Pages[0]
	hi, wide := find(t, pg, "Hi"), find(t, pg, "Wide")
	// Two 24-point lines fill 96 points: twice as tall.
	if !near(wide.y-hi.y, 48, 0.3) || !near(hi.y, 100+40, 0.3) {
		t.Errorf("baselines at %v and %v, want 140 and 188", hi.y, wide.y)
	}
	if want := 400 / widthOf(t, "Wide", a20); !near(groupScaleX(pg), want, 0.01) {
		t.Errorf("horizontal scale %v, want %v", groupScaleX(pg), want)
	}
	if !near(wide.x, 100, 0.5) {
		t.Errorf("wide line at %v, want the rectangle's left edge", wide.x)
	}
}

// groupScaleX returns the horizontal scale of the first group of pg.
func groupScaleX(pg *compose.Page) float64 {
	for _, f := range pg.Content {
		if g, ok := f.(*compose.Group); ok {
			return g.Transform.A
		}
	}
	return 1
}

// TestText_ShrinkGroup checks that the bodies of a shrink group share the
// smallest scale any of them needs.
func TestText_ShrinkGroup(t *testing.T) {
	a20 := arial(20)
	full := box(100, 100, 200, 30, para("One", a20), para("Two", a20), para("Three", a20))
	alone := box(400, 100, 200, 30, para("Fits", a20))
	for _, sh := range []*Shape{full, alone} {
		sh.Text.Autofit, sh.Text.ShrinkGroup = AutofitShrink, 7
	}
	other := box(100, 300, 200, 30, para("Other", a20))
	other.Text.Autofit, other.Text.ShrinkGroup = AutofitShrink, 8
	pg := convert(t, deck(&Slide{Shapes: []*Shape{full, alone, other}}), office.Options{}).Pages[0]
	one, fits, oth := find(t, pg, "One"), find(t, pg, "Fits"), find(t, pg, "Other")
	if one.size >= 20 || fits.size != one.size {
		t.Errorf("sizes %v and %v, want one shrunk size", one.size, fits.size)
	}
	if oth.size != 20 {
		t.Errorf("another group's body at %v, want 20", oth.size)
	}
}

// TestShape_SkewX checks that a sheared shape's outline and text shear
// about the box's centre.
func TestShape_SkewX(t *testing.T) {
	sh := box(100, 100, 200, 100, para("Skew", arial(20)))
	sh.Kind, sh.SkewX = KindGeometry, 30
	sh.Fill = Fill{Kind: FillSolid, Color: css.Color{R: 1, A: 1}}
	pg := convert(t, deck(&Slide{Shapes: []*Shape{sh}}), office.Options{}).Pages[0]
	n := 0
	for _, f := range pg.Content {
		g, ok := f.(*compose.Group)
		if !ok {
			continue
		}
		n++
		m := g.Transform
		if !near(m.C, -0.57735, 1e-4) || !near(m.A, 1, 1e-9) || !near(m.D, 1, 1e-9) {
			t.Errorf("transform %+v, want a shear of tan 30 degrees", m)
		}
		// The centre stays put.
		if x, y := apply(m, 200, 150); !near(x, 200, 1e-6) || !near(y, 150, 1e-6) {
			t.Errorf("centre maps to %v, %v", x, y)
		}
	}
	if n != 2 {
		t.Errorf("%d sheared groups, want the outline's and the text's", n)
	}
}

// TestNotes_HasNotes checks that a slide whose notes page holds no text
// still prints that page with the notes master's boxes.
func TestNotes_HasNotes(t *testing.T) {
	doc := deck(&Slide{HasNotes: true}, &Slide{})
	doc.NotesWidth, doc.NotesHeight = 540, 720
	doc.NotesSlideBox = compose.Rect{X: 90, Y: 54, Width: 360, Height: 270}
	out := convert(t, doc, office.Options{NotesPages: true})
	frame := func(pg *compose.Page) compose.Rect {
		for _, f := range pg.Content {
			if p, ok := f.(*compose.Path); ok && p.Stroke != nil && len(p.Ops) > 2 {
				x0, y0 := p.Ops[0].P[0].X, p.Ops[0].P[0].Y
				x1, y1 := p.Ops[2].P[0].X, p.Ops[2].P[0].Y
				return compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
			}
		}
		return compose.Rect{}
	}
	if got := frame(out.Pages[2]); got != doc.NotesSlideBox {
		t.Errorf("slide with notes framed at %+v, want the master's box", got)
	}
	if got := frame(out.Pages[3]); got == doc.NotesSlideBox {
		t.Errorf("slide without notes framed at the master's box")
	}
}

// TestTable_VerticalCells checks that a row grows to the length of its
// vertical cells' lines, at most the cell's width, and that a cell's text
// turned clockwise sits at its left edge.
func TestTable_VerticalCells(t *testing.T) {
	a20 := arial(20)
	long := para("A rather long line of vertical text", a20)
	cell := func(p Paragraph, v css.WritingMode) TableCell {
		return TableCell{Text: &TextBody{Paragraphs: []Paragraph{p}, Wrap: true, Vertical: v}, Fill: Fill{Kind: FillSolid, Color: css.Color{B: 1, A: 1}}}
	}
	tbl := &Shape{Kind: KindTable, Box: compose.Rect{X: 100, Y: 100, Width: 300, Height: 50}, Table: &Table{
		ColumnWidths: []float64{150, 150},
		Rows:         []TableRow{{Height: 50, Cells: []TableCell{cell(long, css.SidewaysRL), cell(para("Short", a20), css.SidewaysLR)}}},
	}}
	pg := convert(t, deck(&Slide{Shapes: []*Shape{tbl}}), office.Options{}).Pages[0]
	// The long line wraps at the cell's width: the row is 150 tall.
	var bottom float64
	for _, f := range pg.Content {
		if p, ok := f.(*compose.Path); ok && p.Fill != nil {
			for _, op := range p.Ops {
				bottom = max(bottom, op.P[0].Y)
			}
		}
	}
	if !near(bottom, 250, 0.5) {
		t.Errorf("row ends at %v, want 250", bottom)
	}
	// Three 24-point lines from the cell's left edge, the first rightmost.
	if r := find(t, pg, "A"); r.x > 100+72 || r.x < 100+48 {
		t.Errorf("clockwise text's first line at %v, want between 148 and 172", r.x)
	}
}

// TestInsetRect checks the text rectangle of insets that overlap, exceed
// the box or are negative (sd-data/TextDistancesInsets3.pptx).
func TestInsetRect(t *testing.T) {
	box := compose.Rect{X: 0, Y: 100, Width: 200, Height: 56.69}
	ins := func(top, bottom float64) [4]float64 {
		var in [4]float64
		in[css.Top], in[css.Bottom] = top, bottom
		return in
	}
	for _, tc := range []struct {
		name         string
		in           [4]float64
		wantY, wantH float64
	}{
		{"fits", ins(10, 10), 110, 36.69},
		{"bottom past the top", ins(0, 113.39), 71.65, 0},
		{"top past the bottom", ins(113.39, 0), 156.69, 0},
		{"overlap", ins(28.35, 56.69), 114.17, 0},
		{"negative", ins(-10, -10), 90, 76.69},
	} {
		got := insetRect(box, tc.in)
		if !near(got.Y, tc.wantY, 0.01) || !near(got.Height, tc.wantH, 0.01) || got.Width != 200 {
			t.Errorf("%s: %+v, want y %v height %v", tc.name, got, tc.wantY, tc.wantH)
		}
	}
}

// TestText_OverflowAnchor checks that overflowing OpenDocument text
// anchored at the bottom starts at its rectangle's top, and PresentationML
// text overflows upwards.
func TestText_OverflowAnchor(t *testing.T) {
	a20 := arial(20)
	for _, tc := range []struct {
		name   string
		odf    bool
		lo, hi float64
	}{
		// Two lines of about 23 points: the first baseline in the first
		// line below the top, or above the box when they end at its bottom.
		{"odf", true, 100, 125},
		{"ooxml", false, 60, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := box(100, 100, 300, 10, para("One", a20), para("Two", a20))
			sh.Text.Anchor, sh.Text.FontLineSpacing = AnchorBottom, tc.odf
			pg := convert(t, deck(&Slide{Shapes: []*Shape{sh}}), office.Options{}).Pages[0]
			if y := find(t, pg, "One").y; y < tc.lo || y > tc.hi {
				t.Errorf("first baseline at %v, want between %v and %v", y, tc.lo, tc.hi)
			}
		})
	}
}

// TestShape_PictureFill checks that a picture's fill paints under it and
// its line over it.
func TestShape_PictureFill(t *testing.T) {
	pic := &Shape{Kind: KindPicture, Box: compose.Rect{X: 10, Y: 10, Width: 100, Height: 100}, Picture: tinyPicture(),
		Fill: Fill{Kind: FillSolid, Color: css.Color{A: 1}}, Line: Line{Width: 2, Color: css.Color{R: 1, A: 1}}}
	pg := convert(t, deck(&Slide{Shapes: []*Shape{pic}}), office.Options{}).Pages[0]
	var kinds []string
	for _, f := range pg.Content {
		switch v := f.(type) {
		case *compose.Path:
			if v.Fill != nil {
				kinds = append(kinds, "fill")
			} else if v.Stroke != nil {
				kinds = append(kinds, "line")
			}
		case *compose.Image:
			kinds = append(kinds, "picture")
		}
	}
	if len(kinds) < 3 || kinds[0] != "fill" || kinds[1] != "picture" || kinds[len(kinds)-1] != "line" {
		t.Errorf("paint order %v, want fill, picture, line", kinds)
	}
}
