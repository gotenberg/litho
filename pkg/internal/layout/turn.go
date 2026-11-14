package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// principalMode returns the principal writing mode of the document whose
// root is root, the one its pages are laid out in under
// [Options.BrowserRules]: the root's writing mode, or, as CSS Writing
// Modes 4, 8 asks of an HTML document, its body's, the first element
// child of the root as [viewportBox] takes it. HorizontalTB without the
// browser rules: the Office converters turn no page.
func principalMode(root *Node, opts Options) css.WritingMode {
	if !opts.BrowserRules || root == nil || root.Kind != Element || root.Style == nil {
		return css.HorizontalTB
	}
	wm := root.Style.WritingMode
	for _, k := range root.Children {
		if k != nil && k.Kind == Element && k.Style != nil {
			wm = k.Style.WritingMode
			break
		}
	}
	return wm
}

// turnedOptions returns opts for a document of the vertical principal
// writing mode wm: its flow is laid out as a horizontal one on pages
// turned a quarter turn, whose width is the height of the paper and
// whose margins are the paper's turned with them, and each page is
// turned back onto the paper once laid out ([engine.turnPage]). The
// initial containing block is the turned page area: the viewport height
// is the paper's width.
func turnedOptions(opts Options, wm css.WritingMode) Options {
	opts.Page = turnBox(opts.Page, wm)
	if at := opts.PageAt; at != nil {
		opts.PageAt = func(i int) PageBox { return turnBox(at(i), wm) }
	}
	if pf := opts.PageFor; pf != nil {
		opts.PageFor = func(info PageInfo) PageBox { return turnBox(pf(info), wm) }
	}
	// The viewport height and a first page fitted to its content measure
	// the paper's height, along which a turned flow sets its lines: the
	// initial containing block is the turned page area.
	opts.ViewportHeight, opts.FitFirstPage = 0, false
	return opts
}

// turnBox returns the page box pb of the paper as the flow of a document
// of the vertical writing mode wm lays out on it: its sides turned as
// [turnSides] turns a box's.
func turnBox(pb PageBox, wm css.WritingMode) PageBox {
	t := pb
	t.Width, t.Height = pb.Height, pb.Width
	from := turnSides(wm)
	for i := range 4 {
		t.Margin[i] = pb.Margin[from[i]]
	}
	return t
}

// paperBox returns the page box of the paper a turned page box pb was
// made of.
func paperBox(pb PageBox, wm css.WritingMode) PageBox {
	t := pb
	t.Width, t.Height = pb.Height, pb.Width
	from := turnSides(wm)
	for i := range 4 {
		t.Margin[from[i]] = pb.Margin[i]
	}
	return t
}

// turnMatrix maps the page box pb as a turned flow lays it out onto the
// paper: a quarter turn clockwise, which puts the top of the flow on the
// right of the paper, counter-clockwise in sideways-lr, or reflected
// across the diagonal in vertical-lr ([transposed]).
func turnMatrix(pb PageBox, wm css.WritingMode) typeset.Matrix {
	if transposed(wm) {
		return typeset.Matrix{B: 1, C: 1}
	}
	if wm == css.SidewaysLR {
		return typeset.Matrix{B: -1, C: 1, F: pb.Width}
	}
	return typeset.Matrix{B: 1, C: -1, E: pb.Height}
}

// turnPoint returns the point (x, y) of the sheet of the turned page box
// pb, bleed included, on the paper's sheet.
func turnPoint(pb PageBox, wm css.WritingMode, x, y float64) (float64, float64) {
	if transposed(wm) {
		return y, x
	}
	if wm == css.SidewaysLR {
		return y, pb.Width + float64(2*pb.Bleed) - x
	}
	return pb.Height + float64(2*pb.Bleed) - y, x
}

// turnRect returns the rectangle r of the sheet of the turned page box pb
// on the paper's sheet.
func turnRect(pb PageBox, wm css.WritingMode, r typeset.Rect) typeset.Rect {
	x0, y0 := turnPoint(pb, wm, r.X, r.Y)
	x1, y1 := turnPoint(pb, wm, r.X+r.Width, r.Y+r.Height)
	return typeset.Rect{X: min(x0, x1), Y: min(y0, y1), Width: math.Abs(x1 - x0), Height: math.Abs(y1 - y0)}
}

// turnPage turns the page pg, laid out on the turned page box pb, onto
// the paper: the sheet's background and marks before index from, drawn
// on the turned sheet, and the content from that index on, drawn on the
// turned page box, go into groups that turn them, but the page-margin
// content in paper, laid out on the paper itself; the page takes the
// paper's size.
func (e *engine) turnPage(pg *typeset.Page, pb PageBox, from int, paper map[typeset.Fragment]bool) {
	sheet := pb
	sheet.Width, sheet.Height = pb.Width+float64(2*pb.Bleed), pb.Height+float64(2*pb.Bleed)
	out := make([]typeset.Fragment, 0, len(pg.Content))
	if from > 0 {
		out = append(out, &typeset.Group{Transform: turnMatrix(sheet, e.turn), Opacity: 1, Content: pg.Content[:from:from]})
	}
	m := turnMatrix(pb, e.turn)
	for i := from; i < len(pg.Content); {
		if paper[pg.Content[i]] {
			out = append(out, pg.Content[i])
			i++
			continue
		}
		j := i + 1
		for j < len(pg.Content) && !paper[pg.Content[j]] {
			j++
		}
		out = append(out, &typeset.Group{Transform: m, Opacity: 1, Content: pg.Content[i:j:j]})
		i = j
	}
	pg.Content = out
	pg.Width, pg.Height = pg.Height, pg.Width
}
