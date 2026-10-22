package page

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// pointsPerInch is the size of a default user space unit, 1/72 inch (ISO
// 32000-1, 8.3.2.3).
const pointsPerInch = 72

// sizeEpsilon absorbs the rounding of a box edge times a scale: a page 612
// units wide at 72 dpi is 612 pixels even when the product lands a hair above.
const sizeEpsilon = 1e-6

// Geometry is where a page lands in device space: the matrix from its default
// user space to device pixels, origin at the top-left corner, and the size of
// the raster.
type Geometry struct {
	Matrix        geom.Matrix
	Width, Height int
}

// Box selects the page boundary a render draws (ISO 32000-1, 14.11.2): the
// region of the page the raster holds.
type Box uint8

const (
	// CropBox is the region a viewer shows and prints (14.11.2.1), the
	// default.
	CropBox Box = iota
	// MediaBox is the whole medium the page is drawn on.
	MediaBox
	// BleedBox is the region production clips the page to, the bleed
	// around the trimmed page included.
	BleedBox
	// TrimBox is the finished page after trimming.
	TrimBox
	// ArtBox is the page's meaningful content as its creator meant it.
	ArtBox
)

// region returns the boundary b of pg as [Box] describes it, read as a
// viewer reads page boxes: the effective boxes of [document.Page], each
// defaulting to the crop box (ISO 32000-1, 7.7.3.3, Table 30) and reduced to
// the media box (14.11.2.1), and the region a viewer shows
// ([document.Page.ViewedBox]) where the box is none a viewer uses
// ([document.UsableBox]). A Box no constant names is the crop box.
func (b Box) region(pg *document.Page) ast.PdfArray {
	var box ast.PdfArray
	switch b {
	case MediaBox:
		return pg.ViewedMediaBox()
	case BleedBox:
		box = pg.BleedBox
	case TrimBox:
		box = pg.TrimBox
	case ArtBox:
		box = pg.ArtBox
	case CropBox:
	}
	if document.UsableBox(box) {
		return box
	}
	return pg.ViewedBox()
}

// PageGeometry returns the device geometry of pg at dpi: its crop box as a
// viewer reads it ([document.Page.ViewedBox]) scaled by dpi/72 and its
// /UserUnit, then turned clockwise by its /Rotate (ISO 32000-1, 7.7.3.3,
// Table 30). The raster holds the turned box, each side rounded up to whole
// pixels as pdftoppm rounds it.
func PageGeometry(pg *document.Page, dpi float64) (Geometry, error) {
	return BoxGeometry(pg, CropBox, dpi)
}

// BoxGeometry returns the device geometry of [PageGeometry] for the page
// boundary b rather than the crop box: content outside it is cut off, and
// content between it and a smaller crop box is drawn, as a production tool
// images the bleed. A /Rotate turns the boundary as it turns the crop box.
func BoxGeometry(pg *document.Page, b Box, dpi float64) (Geometry, error) {
	if !(dpi > 0) || math.IsInf(dpi, 0) {
		return Geometry{}, fmt.Errorf("%w: resolution %v dpi", ErrInvalidOptions, dpi)
	}
	box := b.region(pg)
	x0, y0 := document.Float(box[0]), document.Float(box[1])
	x1, y1 := document.Float(box[2]), document.Float(box[3])
	x0, x1 = min(x0, x1), max(x0, x1)
	y0, y1 = min(y0, y1), max(y0, y1)
	unit := pg.UserUnit
	// Table 30 makes /UserUnit a positive number; a reader of another value
	// keeps the default of 1.
	if !(unit > 0) || math.IsInf(unit, 0) {
		unit = 1
	}
	s := dpi / pointsPerInch * unit
	w, h := (x1-x0)*s, (y1-y0)*s
	if math.IsNaN(w) || math.IsNaN(h) || math.IsInf(w, 0) || math.IsInf(h, 0) || w > math.MaxInt32 || h > math.MaxInt32 {
		return Geometry{}, fmt.Errorf("%w: page of %v x %v pixels", ErrTooLarge, w, h)
	}
	base := geom.Matrix{s, 0, 0, -s, -x0 * s, y1 * s}

	g := Geometry{Width: pixels(w), Height: pixels(h)}
	// Table 30: /Rotate turns the page clockwise when displayed and shall be a
	// multiple of 90, read modulo 360. Deliberate leniency: a value that is no
	// multiple of 90 draws unturned, as poppler's GfxState draws it, where
	// the table admits none.
	switch ((pg.Rotate % 360) + 360) % 360 {
	case 90:
		g.Matrix = base.Mul(geom.Matrix{0, 1, -1, 0, h, 0})
		g.Width, g.Height = g.Height, g.Width
	case 180:
		g.Matrix = base.Mul(geom.Matrix{-1, 0, 0, -1, w, h})
	case 270:
		g.Matrix = base.Mul(geom.Matrix{0, -1, 1, 0, 0, w})
		g.Width, g.Height = g.Height, g.Width
	default:
		g.Matrix = base
	}
	return g, nil
}

// pixels rounds a side up to whole pixels, at least one.
func pixels(v float64) int {
	return max(1, int(math.Ceil(v-sizeEpsilon)))
}
