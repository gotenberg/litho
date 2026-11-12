package html

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// conv converts src with the real layout engine and checks the document
// is valid.
func conv(t *testing.T, src string, o Options) *compose.Document {
	t.Helper()
	doc, err := Convert(context.Background(), strings.NewReader(src), testFonts(t), o)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return doc
}

// placedRun is a text run's baseline origin and size on its page, its
// groups' transforms applied.
type placedRun struct {
	x, y, size float64
}

// runs returns the text runs of pg in paint order, placed.
func runs(pg *compose.Page) []placedRun {
	var out []placedRun
	var walk func(fs []compose.Fragment, m compose.Matrix)
	walk = func(fs []compose.Fragment, m compose.Matrix) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Group:
				walk(v.Content, mul(v.Transform, m))
			case *compose.Text:
				x, y := m.A*v.Origin.X+m.C*v.Origin.Y+m.E, m.B*v.Origin.X+m.D*v.Origin.Y+m.F
				out = append(out, placedRun{x, y, v.Size * math.Hypot(m.A, m.B)})
			}
		}
	}
	walk(pg.Content, compose.Identity)
	return out
}

// mul returns the transform a then b.
func mul(a, b compose.Matrix) compose.Matrix {
	if a == (compose.Matrix{}) {
		a = compose.Identity
	}
	return compose.Matrix{
		A: a.A*b.A + a.B*b.C, B: a.A*b.B + a.B*b.D,
		C: a.C*b.A + a.D*b.C, D: a.C*b.B + a.D*b.D,
		E: a.E*b.A + a.F*b.C + b.E, F: a.E*b.B + a.F*b.D + b.F,
	}
}

// placementOf returns the transform of the group a placed page wraps its
// content in, the identity for a page drawn as laid out: [placement.page]
// makes it the page's only fragment, clipped to the laid out page whose
// top-left corner is the group's origin.
func placementOf(pg *compose.Page) compose.Matrix {
	if len(pg.Content) != 1 {
		return compose.Identity
	}
	// The layout's own groups are clipped to the page area, from the left
	// margin's negative offset, or translated only.
	g, ok := pg.Content[0].(*compose.Group)
	if !ok || g.Clip == nil || len(g.Clip.Ops) != 5 || g.Clip.Ops[0].P[0] != (compose.Point{}) || g.Transform == compose.Identity {
		return compose.Identity
	}
	return g.Transform
}

func near6(a, b float64) bool { return math.Abs(a-b) < 1e-6*max(1, math.Abs(a)) }

func TestConvert_LandscapeTurnsThePaper(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		o    Options
		w, h float64
	}{
		{"A4", Options{Landscape: true}, DefaultPageHeight, DefaultPageWidth},
		{"custom", Options{Landscape: true, PageWidth: 300, PageHeight: 200}, 200, 300},
		{"before @page orientation", Options{Landscape: true, PreferCSSPageSize: true}, DefaultPageHeight, DefaultPageWidth},
	}
	for _, tt := range tests {
		doc := conv(t, `<style>@page { margin: 10pt }</style><p>x`, tt.o)
		if pg := doc.Pages[0]; !near6(pg.Width, tt.w) || !near6(pg.Height, tt.h) {
			t.Errorf("%s: page %vx%v, want %vx%v", tt.name, pg.Width, pg.Height, tt.w, tt.h)
		}
	}
	// The margins stay on their sides when the paper turns.
	doc := conv(t, `<body style="margin:0">x`, Options{Landscape: true, PageWidth: 300, PageHeight: 200, MarginLeft: 50})
	if r := runs(doc.Pages[0]); len(r) == 0 || !near6(r[0].x, 50) {
		t.Errorf("runs %v, want the text at the left margin", r)
	}
}

func TestConvert_CSSPageSizeFitsThePaper(t *testing.T) {
	t.Parallel()
	a5w, a5h := 148*72/25.4, 210*72/25.4
	a3w, a3h := 297*72/25.4, 420*72/25.4
	tests := []struct {
		name   string
		css    string
		prefer bool
		w, h   float64
		m      compose.Matrix
	}{
		{"no size", "", false, DefaultPageWidth, DefaultPageHeight, compose.Identity},
		{
			"smaller, centred", "size: A5", false, DefaultPageWidth, DefaultPageHeight,
			compose.Matrix{A: 1, D: 1, E: (DefaultPageWidth - a5w) / 2, F: (DefaultPageHeight - a5h) / 2},
		},
		{"smaller, preferred", "size: A5", true, a5w, a5h, compose.Identity},
		{
			"larger, scaled down", "size: A3", false, DefaultPageWidth, DefaultPageHeight,
			compose.Matrix{A: DefaultPageWidth / a3w, D: DefaultPageWidth / a3w, F: (DefaultPageHeight - a3h*DefaultPageWidth/a3w) / 2},
		},
		{"orientation alone", "size: landscape", false, DefaultPageHeight, DefaultPageWidth, compose.Identity},
		{
			"landscape size turns the sheet", "size: 400pt 200pt", false, DefaultPageHeight, DefaultPageWidth,
			compose.Matrix{A: 1, D: 1, E: (DefaultPageHeight - 400) / 2, F: (DefaultPageWidth - 200) / 2},
		},
		{
			"square keeps the sheet", "size: 300pt", false, DefaultPageWidth, DefaultPageHeight,
			compose.Matrix{A: 1, D: 1, E: (DefaultPageWidth - 300) / 2, F: (DefaultPageHeight - 300) / 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := conv(t, `<style>@page { `+tt.css+`; margin: 20pt }</style><p>x`, Options{PreferCSSPageSize: tt.prefer})
			pg := doc.Pages[0]
			if !near6(pg.Width, tt.w) || !near6(pg.Height, tt.h) {
				t.Errorf("page %vx%v, want %vx%v", pg.Width, pg.Height, tt.w, tt.h)
			}
			m := placementOf(pg)
			for i, pair := range [][2]float64{{m.A, tt.m.A}, {m.D, tt.m.D}, {m.E, tt.m.E}, {m.F, tt.m.F}} {
				if !near6(pair[0], pair[1]) {
					t.Errorf("placement %+v, want %+v (term %d)", m, tt.m, i)
					break
				}
			}
		})
	}
}

func TestConvert_ScaleDividesTheLayoutArea(t *testing.T) {
	t.Parallel()
	// The viewport the media queries see is the page area divided by the
	// scale: 595 points wide at 1, 1190 at 0.5.
	src := `<style>p { font-size: 10pt; margin: 0 } @media (min-width: 800pt) { p { font-size: 20pt } }</style><p>x`
	tests := []struct {
		scale, size, a float64
	}{
		{1, 10, 1},
		{0.5, 10, 0.5},
		{2, 20, 2},
	}
	for _, tt := range tests {
		doc := conv(t, src, Options{Scale: tt.scale})
		pg := doc.Pages[0]
		if !near6(pg.Width, DefaultPageWidth) || !near6(pg.Height, DefaultPageHeight) {
			t.Errorf("scale %v: page %vx%v, want A4", tt.scale, pg.Width, pg.Height)
		}
		if m := placementOf(pg); !near6(m.A, tt.a) {
			t.Errorf("scale %v: placement %+v, want a scale of %v", tt.scale, m, tt.a)
		}
		if r := runs(pg); len(r) != 1 || !near6(r[0].size, tt.size) {
			t.Errorf("scale %v: runs %v, want one drawn at %v points", tt.scale, r, tt.size)
		}
	}
	// The margins keep their size: the content starts at them.
	doc := conv(t, `<body style="margin:0">x`, Options{Scale: 0.5, MarginLeft: 40, MarginTop: 60})
	if r := runs(doc.Pages[0]); len(r) != 1 || !near6(r[0].x, 40) || r[0].y < 60 || r[0].y > 70 {
		t.Errorf("runs %v, want the text at the margins", r)
	}
}

func TestConvert_ScaleWarnsPastThePageLimit(t *testing.T) {
	t.Parallel()
	// A 10,000 point page laid out 10 times larger passes the 14,400 point
	// limit: the factor drops to 1.44, with a warning naming the scale asked.
	doc := conv(t, `<style>@page { size: 10000pt }</style><p>x`, Options{Scale: 0.1, PreferCSSPageSize: true})
	var msgs []string
	for _, w := range doc.Warnings {
		if w.Code == CodePageBox {
			msgs = append(msgs, w.Message)
		}
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "cannot be laid out 10 times larger") {
		t.Errorf("page box warnings %q, want one about the factor of 10", msgs)
	}
}

func TestConvert_ShrinksWideContentToFit(t *testing.T) {
	t.Parallel()
	w := DefaultPageWidth
	tests := []struct {
		name string
		body string
		o    Options
		a    float64
	}{
		{"fits", `<div style="width:500pt;height:5pt"></div>`, Options{}, 1},
		{"wider", `<div style="width:800pt;height:5pt"></div>`, Options{}, w / 800},
		{"capped at 1.5", `<div style="width:2000pt;height:5pt"></div>`, Options{}, 1 / 1.5},
		{"refused", `<div style="width:800pt;height:5pt"></div>`, Options{NoShrinkToFit: true}, 1},
		{"with a scale", `<div style="width:2000pt;height:5pt"></div>`, Options{Scale: 0.5}, 1 / 3.0},
		{"clipped overflow", `<div style="width:100pt;overflow:hidden"><div style="width:2000pt;height:5pt"></div></div>`, Options{}, 1},
		{"unbreakable text", `<p style="white-space:nowrap;margin:0">` + strings.Repeat("x", 120) + `</p>`, Options{}, w / (120 * 6)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := conv(t, `<body style="margin:0">`+tt.body, tt.o)
			pg := doc.Pages[0]
			if m := placementOf(pg); !near6(m.A, tt.a) || !near6(m.D, tt.a) {
				t.Errorf("placement %+v, want a scale of %v", m, tt.a)
			}
			if !near6(pg.Width, w) {
				t.Errorf("page width %v, want the paper's", pg.Width)
			}
		})
	}
}

func TestConvert_SinglePage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		body  string
		o     Options
		w, h  float64
		scale float64
	}{
		{
			"short content takes the screen's height", `<p style="margin:0">a</p>`,
			Options{MarginTop: 7, MarginBottom: 3},
			DefaultPageWidth, screenHeight + 10, 1,
		},
		{
			"forced breaks ignored by the measure", `<div style="height:400pt"></div><div style="break-before:page;height:300pt"></div>`,
			Options{MarginTop: 7, MarginBottom: 3},
			DefaultPageWidth, 710, 1,
		},
		{
			"landscape takes the content's width", `<div style="width:1000pt;height:40pt"></div>`,
			Options{Landscape: true, MarginLeft: 10, MarginRight: 20},
			1030, screenHeight, 1,
		},
		{
			"landscape takes the screen's width at least", `<p>a</p>`,
			Options{Landscape: true, MarginLeft: 10, MarginRight: 20},
			screenWidth + 30, screenHeight, 1,
		},
		{
			"portrait shrinks wide content to the paper", `<div style="width:1000pt;height:40pt"></div>`,
			Options{},
			DefaultPageWidth, screenHeight, 1 / 1.5,
		},
		{
			"capped at the PDF limit", `<div style="height:20000pt"></div>`,
			Options{},
			DefaultPageWidth, maxPageSide, 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.o.SinglePage = true
			doc := conv(t, `<body style="margin:0">`+tt.body, tt.o)
			if len(doc.Pages) != 1 {
				t.Fatalf("%d pages, want 1", len(doc.Pages))
			}
			pg := doc.Pages[0]
			// The paper comes out as printToPDF sizes it: whole points,
			// then whole 300 dpi dots.
			w, h := tt.w, min(chromiumPaper(tt.h), maxPageSide)
			if tt.o.Landscape {
				w = chromiumPaper(w)
			}
			if !near6(pg.Width, w) || !near6(pg.Height, h) {
				t.Errorf("page %vx%v, want %vx%v", pg.Width, pg.Height, w, h)
			}
			if m := placementOf(pg); !near6(m.A, tt.scale) {
				t.Errorf("placement %+v, want a scale of %v", m, tt.scale)
			}
		})
	}
}

func TestConvert_OmitOutline(t *testing.T) {
	t.Parallel()
	if doc := conv(t, `<h1>A</h1><h2>B</h2>`, Options{OmitOutline: true}); len(doc.Outline) != 0 {
		t.Errorf("outline %+v, want none", doc.Outline)
	}
	if doc := conv(t, `<h1>A</h1><h2>B</h2>`, Options{}); len(doc.Outline) != 1 {
		t.Errorf("outline %+v, want one entry", doc.Outline)
	}
}

func TestConvert_ScaledLinksAndAnchors(t *testing.T) {
	t.Parallel()
	src := `<body style="margin:0"><a href="#t" style="display:block;height:20pt">go</a><div style="height:100pt"></div><h1 id=t style="margin:0">T</h1>`
	doc := conv(t, src, Options{Scale: 0.5, MarginLeft: 10, MarginTop: 10})
	pg := doc.Pages[0]
	if len(pg.Links) != 1 {
		t.Fatalf("%d links", len(pg.Links))
	}
	l := pg.Links[0]
	if !near6(l.Rect.X, 10) || !near6(l.Rect.Y, 10) || !near6(l.Rect.Height, 10) {
		t.Errorf("link rectangle %+v, want at the margins, half as tall", l.Rect)
	}
	if l.Dest == nil || !near6(l.Dest.Y, 10+60) || !near6(doc.Outline[0].Dest.Y, 70) {
		t.Errorf("destination %+v, outline %+v, want 70 points down", l.Dest, doc.Outline[0].Dest)
	}
}

func FuzzConvertPrintOptions(f *testing.F) {
	f.Add(`<style>@page{size:A3;margin:1cm}</style><h1>a</h1><div style="width:900pt">w</div><p style="break-before:page">b`, uint8(0))
	f.Add(`<style>@page :first{size:landscape}@page{background:red}</style><a href="#x">l</a><p id=x style="break-before:page">x`, uint8(0x2b))
	f.Add(`<html style="print-color-adjust:exact;background:blue"><body style="color:#fff">t`, uint8(0xff))
	f.Fuzz(func(t *testing.T, src string, flags uint8) {
		o := Options{
			Landscape:         flags&1 != 0,
			PreferCSSPageSize: flags&2 != 0,
			SinglePage:        flags&4 != 0,
			OmitBackgrounds:   flags&8 != 0,
			OmitOutline:       flags&16 != 0,
			NoShrinkToFit:     flags&32 != 0,
			Scale:             []float64{1, MinScale, 0.5, MaxScale}[flags>>6],
			MaxInputBytes:     1 << 12,
			MaxPages:          8,
			MarginTop:         10, MarginLeft: 20,
		}
		doc, err := Convert(context.Background(), strings.NewReader(src), testFonts(t), o)
		if errors.Is(err, ErrInvalidOptions) || errors.Is(err, ErrInputTooLarge) || errors.Is(err, ErrTooManyPages) ||
			errors.Is(err, ErrTooDeep) || errors.Is(err, ErrTooManyElements) || errors.Is(err, css.ErrTooComplex) ||
			errors.Is(err, layout.ErrTooManyBoxes) {
			return
		}
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if err := doc.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})
}

// prepare parses data, cascades its stylesheets and builds the content
// tree for a first layout, as [Convert] does.
func prepare(ctx context.Context, data []byte, o Options, fonts *text.Fonts) (*prepared, error) {
	doc, err := parse(data)
	if err != nil {
		return nil, err
	}
	return prepareDoc(ctx, doc, o, fonts, newPrintSetup(o))
}
