package mutations

import (
	"context"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// Margins [PageContentExtent] adds around what it measures, in default user
// space units. Not spec-mandated: the machine records a path's geometry
// without the stroke's width, and a text element its origin, advance and
// size without the glyph outlines, so each is widened to hold what it paints.
// A margin too wide only keeps a page at the size a viewer showed.
const (
	// extentStrokeMargin widens a painted path for the half of a stroke
	// width that lies outside its geometry. Real strokes sit well under it.
	extentStrokeMargin = 5.0
	// extentAscentFactor and extentDescentFactor, times the font size,
	// reach above and below a text element's baseline, past the ascender
	// and descender of every ordinary font.
	extentAscentFactor  = 1.2
	extentDescentFactor = 0.5
)

// showOperators are the operators that show text (ISO 32000-1, 9.4.3,
// Table 109).
var showOperators = map[string]bool{"Tj": true, "TJ": true, "'": true, "\"": true}

// PageContentExtent measures what page paints within the region a viewer
// shows of it, as earlier stages staged the page in alloc, nil for the source
// document alone: the text it shows, the paths it paints, the images it
// places, those of the form XObjects it draws, and the rectangles of its
// annotations, whose appearances draw inside them (ISO 32000-1, 12.5.5). A
// write that gives a page a media box it did not have holds that region whole
// (see [document.MediaBoxDefault]).
//
// The measure is conservative. Anything it cannot place makes the extent
// unknown, which only the size a viewer showed holds: content that does not
// parse, an XObject that does not resolve, a shading operator, which paints
// the whole clip (ISO 32000-1, 8.7.4.2), and a show operator of the page's
// own content that yields no text, whose font did not resolve. What it
// places, it widens (see [extentStrokeMargin]). Only a cancelled ctx is an
// error.
func PageContentExtent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) (document.ContentExtent, error) {
	unknown := document.ContentExtent{}
	if alloc == nil {
		alloc = writer.NewObjAllocator(0)
	}
	page = latestPage(ctx, alloc, doc, page)
	viewed := page.ViewedBox()
	vx0, vy0, vx1, vy1 := rectangleCorners(viewed)

	data, err := collectLatestPageContents(ctx, alloc, doc, page)
	if err != nil {
		return extentFailure(ctx, doc, "reading the page content", err)
	}
	ops, err := content.ParseStreamData(ctx, data, content.SplitGluedOperators())
	if err != nil {
		return extentFailure(ctx, doc, "parsing the page content", err)
	}
	machine := graphics.NewMachine([4]float64{vx0, vy0, vx1, vy1})
	machine.IgnoreOCVisibility = true
	machine.RecordImageBoxes = true
	machine.RecordPathBoxes = true
	machine.RecordUnknownOperators = true
	machine.Staged = alloc.Pending()
	elements, origins, _, err := typography.ExtractElementsWithAttribution(ctx, doc, machine, page, ops)
	if err != nil {
		return extentFailure(ctx, doc, "extracting the page text", err)
	}
	if machine.UnresolvedXObjects() > 0 {
		return unknown, nil
	}
	for _, op := range machine.UnknownOperators() {
		if op.Operator == "sh" {
			return unknown, nil
		}
	}
	shown := make(map[int]bool, len(origins))
	for _, origin := range origins {
		shown[origin] = true
	}
	for i, op := range ops {
		if showOperators[op.Operator] && showsGlyphs(op) && !shown[i] {
			return unknown, nil
		}
	}

	var region boxUnion
	for _, e := range elements {
		region.add(rectArray(textElementBox(e)))
	}
	for _, b := range machine.PathBoxes() {
		region.add(rectArray([4]float64{b.MinX - extentStrokeMargin, b.MinY - extentStrokeMargin, b.MaxX + extentStrokeMargin, b.MaxY + extentStrokeMargin}))
	}
	for _, b := range machine.ImageBoxes() {
		region.add(rectArray([4]float64{b.MinX, b.MinY, b.MaxX, b.MaxY}))
	}
	annots, err := resolveArrayAllocFirst(ctx, alloc, doc, page.Dict["Annots"])
	if err == nil {
		for _, a := range annots {
			annot, err := resolveDictAllocFirst(ctx, alloc, doc, a)
			if err != nil || annot == nil {
				continue
			}
			if rect := stagedRectangle(ctx, alloc, doc, annot["Rect"]); len(rect) == 4 {
				x0, y0, x1, y1 := rectangleCorners(rect)
				region.add(rectArray([4]float64{x0, y0, x1, y1}))
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	if !region.set {
		return document.ContentExtent{Known: true}, nil
	}
	// What lies outside the region a viewer shows was never shown, and no
	// size has to hold it.
	box := [4]float64{max(region.x0, vx0), max(region.y0, vy0), min(region.x1, vx1), min(region.y1, vy1)}
	if box[2] <= box[0] || box[3] <= box[1] {
		return document.ContentExtent{Known: true}, nil
	}
	return document.ContentExtent{Known: true, Painted: true, Box: box}, nil
}

// PageDictContentExtent is [PageContentExtent] for the page at ref whose
// dictionary, as alloc stages it, is dict, and whose nearest /Resources entry,
// its own or an ancestor's, is resources, nil for none. It reads the page as a
// writer holds it, with no page tree walk behind it: the boxes it states, and
// US Letter as the region a viewer shows when it states no media box, which
// holds whatever an inherited crop box shows.
func PageDictContentExtent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef, dict ast.PdfDict, resources ast.PdfObject) (document.ContentExtent, error) {
	if alloc == nil {
		alloc = writer.NewObjAllocator(0)
	}
	page := stagedPage(ctx, alloc, doc, ref, dict)
	if page.Resources == nil && resources != nil {
		if res, err := resolveDictAllocFirst(ctx, alloc, doc, resources); err == nil {
			page.Resources = res
		}
	}
	return PageContentExtent(ctx, alloc, doc, page)
}

// extentFailure returns the unknown extent of a page whose content could not
// be read, and ctx's error when that is why.
func extentFailure(ctx context.Context, doc *document.Document, step string, err error) (document.ContentExtent, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return document.ContentExtent{}, ctxErr
	}
	doc.Logger.Debug("page content extent unknown", slog.String("step", step), slog.Any("err", err))
	return document.ContentExtent{}, nil
}

// showsGlyphs reports whether op, a show operator, names any byte to show.
func showsGlyphs(op content.Operation) bool {
	for _, operand := range op.Operands {
		switch v := operand.(type) {
		case ast.PdfString:
			if len(v) > 0 {
				return true
			}
		case ast.PdfArray:
			for _, item := range v {
				if s, ok := item.(ast.PdfString); ok && len(s) > 0 {
					return true
				}
			}
		}
	}
	return false
}

// rectArray returns the rectangle r as the array [boxUnion.add] reads.
func rectArray(r [4]float64) ast.PdfArray {
	return ast.PdfArray{ast.PdfReal(r[0]), ast.PdfReal(r[1]), ast.PdfReal(r[2]), ast.PdfReal(r[3])}
}

// textElementBox returns a rectangle holding the glyphs of e: its advance
// along the baseline, widened by the font size above and below it, and a
// square around its origin wide enough for any turn when the text is turned
// or written vertically, grown to the ink box extraction records
// ([models.TextElement.InkBox]), which reaches the ink of a Type 3 glyph
// sized through its /FontMatrix and of a skewed matrix.
func textElementBox(e models.TextElement) [4]float64 {
	box := sizedTextElementBox(e)
	if ink := e.InkBox; ink != ([4]float64{}) {
		box = [4]float64{min(box[0], ink[0]), min(box[1], ink[1]), max(box[2], ink[2]), max(box[3], ink[3])}
	}
	return box
}

// sizedTextElementBox is the part of [textElementBox] the font size gives.
func sizedTextElementBox(e models.TextElement) [4]float64 {
	size := math.Abs(e.FontSize)
	if e.Angle != 0 || e.Vertical {
		r := math.Abs(e.Width) + (extentAscentFactor+extentDescentFactor)*size
		return [4]float64{e.X - r, e.Y - r, e.X + r, e.Y + r}
	}
	x0, x1 := e.X, e.X+e.Width
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	// Text advancing along +X that inks the right of its advance inks below
	// its baseline: see [models.TextElement.InkRight].
	below, above := extentDescentFactor, extentAscentFactor
	if e.InkRight {
		below, above = above, below
	}
	return [4]float64{x0 - size*extentDescentFactor, e.Y - size*below, x1 + size*extentDescentFactor, e.Y + size*above}
}
