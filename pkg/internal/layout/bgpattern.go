package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// tileAxis is where the tiles of a background image lie along one axis:
// n tiles, the first at first and each next one step further.
type tileAxis struct {
	first, step, n float64
}

// axisTiles returns where the tiles, each size long, lie along one axis, the
// tile background-position places at start: what [positions] lists,
// counted rather than listed, so that no count is too large to say. Space
// spreads the tiles that fit whole over the area, one alone when fewer
// than two fit; repeat and round step by the size over the clip span
// (CSS Backgrounds 3, 3.4).
func axisTiles(rep css.Repeat, start, size, areaStart, areaSize, clipStart, clipSize float64) tileAxis {
	switch rep {
	case css.RepeatNoRepeat:
		return tileAxis{first: start, step: size, n: 1}
	case css.RepeatSpace:
		n := math.Floor(areaSize / size)
		if n < 2 {
			return tileAxis{first: start, step: size, n: 1}
		}
		gap := (areaSize - float64(n*size)) / (n - 1)
		return tileAxis{first: areaStart, step: size + gap, n: n}
	case css.RepeatRepeat, css.RepeatRound:
	}
	first := start - float64(math.Ceil((start-clipStart)/size)*size)
	n := math.Ceil((clipStart + clipSize - first) / size)
	return tileAxis{first: first, step: size, n: max(n, 0)}
}

// patternTiles returns the background layer l, whose image is the
// picture pic, positioned in area and painted within clip, as one path
// filled with a tiling pattern whose cell is one tile: the path covers the
// tiles' span within clip, and the pattern places a cell where each tile
// lies. It returns nil for a layer of one tile or none, which paints its
// tile as it is, and for a gradient, which paints its tiles one by one
// ([engine.gradientTiles]).
//
// A tiling pattern is exact: PDF repeats the cell at the steps the tiles
// are at (ISO 32000-1, 8.7.3.1), space's gaps included (CSS Backgrounds 3,
// 3.4), and the path bounds the span along an axis the image does not
// repeat on. One cell is written once whatever the number of tiles, so
// the count bounds no memory, and an image tile of any size covers its
// area.
//
// A gradient is no pattern: a reader fills a cell holding a shading to
// its device pixels, and poppler leaves a seam of paper between cells a
// fraction of a pixel wide, and loses the colours of a cell under about
// three pixels, so a fine stripe would vanish in every poppler-based
// viewer. Tiles show hairlines in pdf.js instead.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156's print writes a repeated or rounded image
//     as a tiling pattern of one image cell, and a repeated gradient or a
//     spaced image as one image of the page painted through a pattern of
//     one cell.
//   - probe, html: a 50% red and blue gradient tiled every 2, 6 and 10 CSS
//     px and written as a pattern draws, in poppler at 72 dpi, red and
//     white stripes for 2 px and white seams between cells for 6 and
//     10 px (4.5 and 7.5 device pixels); pdf.js and litho draw it
//     seamless.
func (e *engine) patternTiles(l *css.BackgroundLayer, pic *Picture, area, clip typeset.Rect) *typeset.Path {
	if l.Gradient != nil || pic == nil || pic.Gradient != nil {
		return nil
	}
	w, h, x0, y0, ok := tileSize(l, pic, area)
	if !ok {
		return nil
	}
	rep := l.Repeat
	ax := axisTiles(rep[0], x0, w, area.X, area.Width, clip.X, clip.Width)
	ay := axisTiles(rep[1], y0, h, area.Y, area.Height, clip.Y, clip.Height)
	count := float64(ax.n * ay.n)
	if count <= 1 {
		return nil
	}
	x1 := min(ax.first+float64((ax.n-1)*ax.step)+w, clip.X+clip.Width)
	y1 := min(ay.first+float64((ay.n-1)*ay.step)+h, clip.Y+clip.Height)
	span := typeset.Rect{X: max(ax.first, clip.X), Y: max(ay.first, clip.Y)}
	span.Width, span.Height = x1-span.X, y1-span.Y
	if !(span.Width > 0 && span.Height > 0) {
		return nil
	}
	cell := typeset.Rect{Width: w, Height: h}
	content := e.pictureFrag(pic, cell, nil)
	if content == nil {
		return nil
	}
	return &typeset.Path{Ops: rectOps(span), Fill: &typeset.Fill{Tiling: &typeset.Tiling{
		Content:   []typeset.Fragment{content},
		Width:     w,
		Height:    h,
		XStep:     ax.step,
		YStep:     ay.step,
		Transform: typeset.Matrix{A: 1, D: 1, E: ax.first, F: ay.first},
	}}}
}
