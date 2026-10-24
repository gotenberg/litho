package flatten

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

// opsPerContextCheck is how many operators run between two checks of ctx.
const opsPerContextCheck = 100

// maxRegionPasses bounds the passes that grow the regions of one page to a
// fixed point. Anti-DoS guard, not spec-mandated: every pass but the last
// grows a region or merges two, so a page of n units reaches its fixed point
// in at most 2n passes, and the cap stops a page of millions of units from
// spending quadratic time. 4096 passes is far past what real pages need,
// whose regions settle in a handful; a page past it is refused.
const maxRegionPasses = 4096

// Kind tells the painting elements of a content stream apart.
type Kind uint8

const (
	// KindPath is a path, from its first construction operator to the
	// operator that paints it (ISO 32000-2, 8.5.3).
	KindPath Kind = iota
	// KindImage is the Do of an image XObject (8.9.5).
	KindImage
	// KindInlineImage is an inline image (8.9.7).
	KindInlineImage
	// KindShading is an sh operator (8.7.4.2).
	KindShading
	// KindForm is the Do of a form XObject (8.10).
	KindForm
	// KindText is a text object, BT to ET (9.4.1).
	KindText
)

// Unit is one painting element of a content stream.
type Unit struct {
	Kind Kind
	// First and Last are the indices of its first and last operations.
	First, Last int
	// Box holds everything it can paint, in default user space, cut to the
	// clipping region's box and the page's crop box.
	Box Rect
	// Transparent reports that it paints with transparency.
	Transparent bool
	// HasText reports text it shows: a text object's own, or the text of a
	// form XObject.
	HasText bool
	// Clips reports a path whose painting operator also sets the clipping
	// path (8.5.4): removing it must keep the clip.
	Clips bool
	// Group reports a form holding a transparency group, its own or one of
	// a form it paints.
	Group bool
	// hides says which colours a form is painted under a constant alpha of
	// 0 for: what its content paints in them shows nothing.
	hides paints
	// blended reports a text object whose colours were composited over
	// the paper: exact unless something drawn before it lies under it.
	blended bool
	// textTransparent reports a form showing text with a transparency its
	// copy holding only its text approximates.
	textTransparent bool
	// Overprint reports a unit that paints under overprint (ISO 32000-1,
	// 8.6.7), or a form whose content does.
	Overprint bool
}

// Region is a part of the page composited into one image.
type Region struct {
	// Box is the area composited, in default user space.
	Box Rect
	// Insert is the index of the operation the image is drawn before.
	Insert int
	// Removed lists, in order, the units whose painting the image takes
	// over; a form among them that shows text is replaced by its text.
	Removed []int
	// Clipped lists, in order, the units drawn after the image and before
	// the region's last transparent unit that stay, clipped to paint
	// outside the region only: inside it the image holds what they paint.
	Clipped []int
	// Text lists, in order, the units showing text inside Box that stay in
	// the content, drawn above the image.
	Text []int
	// Last is the index of the region's last transparent unit: text drawn
	// before it can lie under what the image composites.
	Last int
}

// position is the graphics state in force before one operation.
type position struct {
	ctm geom.Matrix
	// clip is the clipping region's box, and clipped whether a W or W*
	// narrowed it from the crop box.
	clip    Rect
	clipped bool
	// exact reports that the clipping region is the box clip itself.
	exact bool
	// insertable reports that an image may be drawn here: outside a text
	// object and with no path under construction (8.2, Figure 9).
	insertable bool
	// marked is the marked-content nesting depth (14.6).
	marked int
}

// Analysis is what [Analyze] reads from a content stream.
type Analysis struct {
	Units   []Unit
	Regions []Region
	// TransparentText lists the text units whose transparency the output
	// only approximates (see [scanner.textOutcome]): text is not
	// rasterised, so a soft mask or a blend mode it painted with is dropped,
	// and its colours composited over the paper differ where it lay over
	// other content.
	TransparentText []int
	// TextEdits maps the show operators that painted with transparency to
	// what is drawn around them once it is gone (see [scanner.textOutcome]).
	TextEdits map[int]TextEdit
	// TextPatterns names the patterns text paints with.
	TextPatterns map[string]bool
	// Hidden lists the transparent units, other than text, that paint
	// nothing on the page: no clip and crop box leave them area. Removing
	// them changes nothing a viewer shows.
	Hidden    []int
	positions []position
	// crop is the page's crop box, as a viewer reads it.
	crop Rect
}

// clipFrame is the part of the clipping state the graphics state stack
// carries that the machine does not.
type clipFrame struct {
	exact bool
}

// walker holds the state of one walk over a page's operations.
type walker struct {
	ctx    context.Context
	doc    *document.Document
	scan   *scanner
	m      *graphics.Machine
	crop   Rect
	res    ast.PdfDict
	frames []clipFrame
	units  []Unit
	// textFor maps each show operator to its text unit, and shows lists
	// those of the text object open.
	textFor map[int]int
	shows   []int
	// textEdits maps a show operator to what is drawn around it once
	// transparency is gone; see [scanner.textOutcome].
	textEdits map[int]TextEdit
	// textPatterns names the patterns text paints with.
	textPatterns map[string]bool

	pathStart, pathOps int
	pathRect, clipping bool

	inText              bool
	textStart           int
	textAlpha, textClip bool
	// textBlended reports colours of the text object open composited over
	// the paper, exact where the text lies on the paper.
	textBlended bool
	textShows   bool
}

// Analyze reads ops, the content of page, into units and regions.
func Analyze(ctx context.Context, doc *document.Document, page *document.Page, ops []content.Operation) (*Analysis, error) {
	return analyzeSeeded(ctx, doc, page, ops, nil)
}

// analyzeSeeded is [Analyze]. With seeds not nil, the regions grow from the units
// that begin at the operations seeds names instead of the transparent ones:
// a unit counts as [Unit.Transparent] when it is a form XObject or a shading
// paint that begins there, and as nothing else, so the regions hold the
// seeds and what an opaque image of them must take with them (see
// [Redaction]). A seed whose box is empty paints nothing a viewer shows and
// is listed in [Analysis.Hidden].
func analyzeSeeded(ctx context.Context, doc *document.Document, page *document.Page, ops []content.Operation, seeds map[int]bool) (*Analysis, error) {
	box := page.ViewedBox()
	crop := rectOf(document.Float(box[0]), document.Float(box[1]), document.Float(box[2]), document.Float(box[3]))
	w := &walker{
		ctx:     ctx,
		doc:     doc,
		scan:    newScanner(doc, page.DrawResources),
		m:       graphics.NewMachine([4]float64{crop.MinX, crop.MinY, crop.MaxX, crop.MaxY}),
		crop:    crop,
		res:     page.DrawResources,
		frames:  []clipFrame{{exact: true}},
		textFor: make(map[int]int),

		textEdits: make(map[int]TextEdit),

		textPatterns: make(map[string]bool),
	}
	w.m.Doc, w.m.Resources = doc, page.DrawResources
	w.m.ActiveOCs = document.PropertiesOCs(ctx, doc, page.DrawResources)
	w.m.RecordPathBoxes = true

	a := &Analysis{positions: make([]position, len(ops)+1), crop: crop}
	for i, op := range ops {
		if i%opsPerContextCheck == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		a.positions[i] = w.position()
		w.m.OpIndex = i
		w.step(i, op)
	}
	a.positions[len(ops)] = w.position()
	a.Units = w.units
	if err := w.textBoxes(page, ops, a.Units); err != nil {
		return nil, err
	}
	if seeds != nil {
		for i := range a.Units {
			u := &a.Units[i]
			u.Transparent = seeds[u.First] && (u.Kind == KindForm || u.Kind == KindShading)
			if u.Transparent && u.Box.Empty() {
				a.Hidden = append(a.Hidden, i)
			}
		}
		if err := a.regions(); err != nil {
			return nil, err
		}
		return a, nil
	}
	a.TextEdits, a.TextPatterns = w.textEdits, w.textPatterns
	for i, u := range a.Units {
		if u.blended && !u.Transparent && a.drawnUnder(i) {
			a.Units[i].Transparent = true
			u.Transparent = true
		}
		switch {
		case u.Kind == KindText && u.Transparent:
			a.TransparentText = append(a.TransparentText, i)
		case u.Kind != KindText && u.Transparent && u.Box.Empty():
			a.Hidden = append(a.Hidden, i)
		}
	}
	if err := a.regions(); err != nil {
		return nil, err
	}
	return a, nil
}

func (w *walker) position() position {
	st := w.m.Current()
	return position{
		ctm:        geom.Matrix(st.CTM),
		clip:       rectOf(st.ClippingPath.MinX, st.ClippingPath.MinY, st.ClippingPath.MaxX, st.ClippingPath.MaxY).Intersect(w.crop),
		clipped:    st.ClipApplied,
		exact:      w.frames[len(w.frames)-1].exact,
		insertable: !w.inText && w.pathOps == 0,
		marked:     len(w.m.MarkedContent),
	}
}

// visible returns r cut to the clipping region's box and the crop box, and
// no box inside marked content optional content hides (ISO 32000-1,
// 8.11.3.2), which paints nothing a viewer shows. Such a unit joins no
// region: the image a region gets is drawn before its first unit, and drawn
// inside hidden content it would hide what it took over from the units
// shown.
//
// # Corpus evidence
//
//   - pdfjs issue269_2: the raster fallback of the p1-quarter redaction drew
//     the image of the board's middle at the place of a form in a hidden
//     layer, and removed the forms the layer shown drew there: 57,957
//     pixels outside the region went white on litho's rendering, once the
//     renderer hid the layers the file's membership dictionaries hide.
func (w *walker) visible(r Rect) Rect {
	st := w.m.Current()
	if st.SkipExtraction {
		return Rect{}
	}
	clip := rectOf(st.ClippingPath.MinX, st.ClippingPath.MinY, st.ClippingPath.MaxX, st.ClippingPath.MaxY)
	return r.Intersect(clip).Intersect(w.crop)
}

// step runs op, the operation at index i, recording the unit it ends.
func (w *walker) step(i int, op content.Operation) {
	before := len(w.m.PathBoxes())
	st := w.m.Current()
	ctm := geom.Matrix(st.CTM)
	switch op.Operator {
	case "m", "l", "c", "v", "y", "h", "re":
		if w.pathOps == 0 {
			w.pathStart, w.pathRect = i, op.Operator == "re"
		} else {
			w.pathRect = false
		}
		w.pathOps++
	case "W", "W*":
		w.clipping = true
	}
	// The machine runs every operator it models and steps over the rest;
	// an error is a cancelled ctx, which the caller checks.
	_ = w.m.ExecuteOne(w.ctx, op) //nolint:errcheck // Deliberate: ctx is checked by the caller; an operator the machine cannot model leaves the state as a viewer would.
	st = w.m.Current()

	switch op.Operator {
	case "q":
		w.frames = append(w.frames, w.frames[len(w.frames)-1])
	case "Q":
		if len(w.frames) > 1 {
			w.frames = w.frames[:len(w.frames)-1]
		}
	case "n":
		w.endPath(ctm)
	case "BT":
		w.inText, w.textStart, w.shows = true, i, w.shows[:0]
		w.textAlpha, w.textClip, w.textShows, w.textBlended = false, false, false, false
	case "ET":
		if w.inText {
			w.endText(i)
		}
	case "sh":
		if !w.inText {
			w.units = append(w.units, Unit{Kind: KindShading, First: i, Last: i, Box: w.visible(w.crop), Transparent: stateTransparent(st, paintFill), Overprint: overprints(st, paintFill)})
		}
	case "BI":
		if !w.inText {
			w.units = append(w.units, Unit{Kind: KindInlineImage, First: i, Last: i, Box: w.visible(transformRect(ctm, Rect{0, 0, 1, 1})), Transparent: stateTransparent(st, paintFill), Overprint: overprints(st, paintFill)})
		}
	case "Do":
		if !w.inText {
			w.do(i, op, st, ctm)
		}
	}
	if showText(op.Operator) && w.inText {
		w.textShows = true
		w.shows = append(w.shows, i)
		if p := textPaints(st.Text.Tmode); p.fill && st.Fill.Pattern != "" {
			w.textPatterns[st.Fill.Pattern] = true
		}
		if p := textPaints(st.Text.Tmode); p.stroke && st.Stroke.Pattern != "" {
			w.textPatterns[st.Stroke.Pattern] = true
		}
		outcome := w.scan.textOutcome(w.ctx, st, w.res)
		if outcome.edited {
			w.textEdits[i] = outcome.edit
		}
		w.textAlpha = w.textAlpha || outcome.approx
		w.textBlended = w.textBlended || outcome.blended
		if st.Text.Tmode >= 4 {
			w.textClip = true
		}
	}
	if p, ok := pathPaints(op.Operator); ok {
		if boxes := w.m.PathBoxes(); len(boxes) > before && !w.inText {
			pb := boxes[len(boxes)-1]
			reach := 0.0
			if p.stroke {
				reach = strokeReach(st, ctm)
			}
			r := pathBox(pb.MinX, pb.MinY, pb.MaxX, pb.MaxY, reach)
			w.units = append(w.units, Unit{
				Kind:        KindPath,
				First:       w.pathStart,
				Last:        i,
				Box:         w.visible(r),
				Transparent: w.scan.painting(w.ctx, st, p, w.res, 0, nil),
				Clips:       w.clipping,
				Overprint:   overprints(st, p),
			})
		}
		w.endPath(ctm)
	}
}

// endPath ends the path under construction. A clip it set stays the box
// the machine holds only when the path was one rectangle with its sides
// along the axes of default user space.
func (w *walker) endPath(ctm geom.Matrix) {
	if w.clipping {
		axis := (ctm[1] == 0 && ctm[2] == 0) || (ctm[0] == 0 && ctm[3] == 0)
		f := &w.frames[len(w.frames)-1]
		f.exact = f.exact && w.pathOps == 1 && w.pathRect && axis
	}
	w.pathOps, w.pathRect, w.clipping = 0, false, false
}

// endText records the text object that ends at operation i. A text object
// in a clipping mode adds its glyphs to the clip at ET (ISO 32000-2,
// 9.3.6), which leaves the clip no box.
func (w *walker) endText(i int) {
	w.inText = false
	if w.textClip {
		w.frames[len(w.frames)-1].exact = false
	}
	for _, op := range w.shows {
		w.textFor[op] = len(w.units)
	}
	w.shows = w.shows[:0]
	w.units = append(w.units, Unit{Kind: KindText, First: w.textStart, Last: i, Transparent: w.textAlpha, HasText: w.textShows, blended: w.textBlended})
}

// do records the Do at operation i.
func (w *walker) do(i int, op content.Operation, st *graphics.State, ctm geom.Matrix) {
	obj, stream, subtype, ok := w.scan.xobject(w.ctx, w.res, op)
	if !ok {
		return
	}
	// An XObject its own /OC hides paints nothing (ISO 32000-1, 8.11.3.3),
	// as content marked hidden paints nothing ([walker.visible]).
	hidden := !document.OCVisible(w.ctx, w.doc, stream.Dict["OC"])
	defer func() {
		if hidden && len(w.units) > 0 && w.units[len(w.units)-1].First == i {
			w.units[len(w.units)-1].Box = Rect{}
		}
	}()
	switch subtype {
	case "Image":
		w.units = append(w.units, Unit{
			Kind:        KindImage,
			First:       i,
			Last:        i,
			Box:         w.visible(transformRect(ctm, Rect{0, 0, 1, 1})),
			Transparent: stateTransparent(st, paintFill) || imageTransparent(w.ctx, w.doc, stream.Dict),
			Overprint:   overprints(st, paintFill),
		})
	case "Form":
		info := w.scan.form(w.ctx, obj, stream, w.res, 1, nil)
		// A transparency group of opaque content paints exactly as that
		// content painted without the group: an element of shape and
		// opacity 1 composited with Normal replaces the backdrop whatever
		// the group's isolation or knockout (ISO 32000-2, 11.4.5, 11.4.6,
		// 11.6.6). Such a form is no transparent unit, and only its /Group
		// entry is dropped.
		w.units = append(w.units, Unit{
			Kind:        KindForm,
			First:       i,
			Last:        i,
			Box:         w.visible(formBox(w.ctx, w.doc, stream.Dict, info.bounds, ctm)),
			Transparent: info.transparent || stateTransparent(st, paintBoth),
			HasText:     info.text,
			Group:       info.group,

			textTransparent: info.textTransparent,
			hides:           paints{fill: st.FillAlpha <= 0, stroke: st.StrokeAlpha <= 0},
			Overprint:       info.overprint || overprints(st, paintBoth),
		})
	}
}

// formBox returns the box of bounds, what a form paints in its own space,
// through its /Matrix and ctm (ISO 32000-2, 8.10.1).
func formBox(ctx context.Context, doc *document.Document, dict ast.PdfDict, bounds Rect, ctm geom.Matrix) Rect {
	matrix, _ := formGeometry(ctx, doc, dict)
	return transformRect(matrix.Mul(ctm), bounds)
}

// formGeometry returns a form's /Matrix, the identity when it states none,
// and its /BBox, empty when it states none (ISO 32000-2, 8.10.2, Table 93).
func formGeometry(ctx context.Context, doc *document.Document, dict ast.PdfDict) (geom.Matrix, Rect) {
	matrix := geom.Identity
	if arr, err := document.ResolveArray(ctx, doc, dict["Matrix"]); err == nil && len(arr) == 6 {
		for k := range 6 {
			matrix[k] = document.Float(arr[k])
		}
	}
	bbox, err := document.ResolveArray(ctx, doc, dict["BBox"])
	if err != nil || len(bbox) != 4 {
		return matrix, Rect{}
	}
	clamp := func(o ast.PdfObject) float64 {
		return max(-maxBBoxCoordinate, min(document.Float(o), maxBBoxCoordinate))
	}
	return matrix, rectOf(clamp(bbox[0]), clamp(bbox[1]), clamp(bbox[2]), clamp(bbox[3]))
}

// maxBBoxCoordinate bounds each /BBox coordinate a form's box is read with,
// as the renderer bounds the clip it draws the form under (see the constant
// of the same name in package render/page). Anti-overflow guard, not
// spec-mandated: a /BBox of ±9×10^307 through a scaling matrix overflows to
// infinity, which reads as a box of no area, and a form that paints the
// page would read as painting nothing. A billion units holds the largest
// page (ISO 32000-1, Annex C.2).
const maxBBoxCoordinate = 1e9

// strokeReach returns how far a stroke under st reaches past its path, in
// default user space: half the line width, times the miter limit where a
// miter join can extend a corner that far (ISO 32000-2, 8.4.3.5), through
// the larger scale of ctm. A width of 0 strokes the thinnest line a device
// draws (8.4.3.2), read as one unit.
func strokeReach(st *graphics.State, ctm geom.Matrix) float64 {
	lw := math.Abs(st.LineWidth)
	if lw == 0 || math.IsNaN(lw) {
		lw = 1
	}
	factor := 1.0
	if st.LineJoin == graphics.LineJoinMiter && st.MiterLimit > 1 {
		factor = st.MiterLimit
	}
	scale := max(math.Hypot(ctm[0], ctm[1]), math.Hypot(ctm[2], ctm[3]))
	reach := lw / 2 * factor * scale
	if math.IsNaN(reach) || math.IsInf(reach, 0) {
		return 0
	}
	return reach + 1
}

// textBoxes sets the box of each text unit: the ink of the glyphs its show
// operators paint, as the text extractor bounds them. A text object whose
// glyphs the extractor does not bound gets the clipping region's box at its
// BT, which holds everything it can paint.
func (w *walker) textBoxes(page *document.Page, ops []content.Operation, units []Unit) error {
	m := graphics.NewMachine([4]float64{})
	elements, origins, err := typography.ExtractElementsWithOrigins(w.ctx, w.doc, m, page, ops)
	if err != nil {
		return fmt.Errorf("bounding the page's text: %w", err)
	}
	bounded := make(map[int]bool)
	for k, e := range elements {
		u, ok := w.textFor[origins[k]]
		if !ok {
			continue
		}
		ink := rectOf(e.InkBox[0], e.InkBox[1], e.InkBox[2], e.InkBox[3])
		units[u].Box = units[u].Box.Union(ink.Intersect(w.crop))
		bounded[u] = true
	}
	for u := range units {
		if units[u].Kind == KindText && units[u].HasText && !bounded[u] {
			units[u].Box = w.crop
		}
	}
	return nil
}

// regions groups the transparent units into regions, each grown to a fixed
// point:
//
//   - every transparent unit its box overlaps joins it;
//   - its image is drawn before its first transparent unit, or before the
//     first unit showing text inside it when that comes earlier, at the
//     latest position from there where an image can be drawn whole: outside
//     a text object, with no path under construction, under an invertible
//     CTM and a clip that holds the region;
//   - every unit other than a text object from there to its last
//     transparent unit that overlaps it either stays, clipped to paint
//     outside the region only, or, when it cannot be clipped so, is removed
//     whole, its painting taken over by the image, and the region grows to
//     hold it.
//
// Inside the region the image shows what everything drawn up to its last
// transparent unit composites to, text left out. The units drawn before the
// image stay, under it. A unit clipped out of the region paints outside it
// what it painted there. The units drawn after its last transparent unit
// stay, above it, and are opaque: the image holds what they paint too, and
// they paint it again. Text stays wherever it is, above the image.
func (a *Analysis) regions() error {
	var regs []*growing
	for _, u := range a.Units {
		if u.Transparent && u.Kind != KindText && !u.Box.Empty() {
			regs = append(regs, &growing{box: u.Box})
		}
	}
	if len(regs) == 0 {
		return nil
	}
	for pass := 0; ; pass++ {
		if pass >= maxRegionPasses {
			return fmt.Errorf("%w: regions still growing after %d passes", ErrTooComplex, maxRegionPasses)
		}
		var changed bool
		regs, changed = mergeRegions(regs)
		for _, r := range regs {
			first, last := -1, -1
			for i, u := range a.Units {
				if u.Transparent && u.Kind != KindText && u.Box.Overlaps(r.box) {
					if first < 0 {
						first = i
					}
					last = i
					grown := r.box.Union(u.Box)
					changed = changed || grown != r.box
					r.box = grown
				}
			}
			if first < 0 {
				continue
			}
			start := first
			for i := range first {
				if a.Units[i].HasText && a.Units[i].Box.Overlaps(r.box) {
					start = i
					break
				}
			}
			r.insert = a.insertion(a.Units[start].First, r.box)
			r.last = last
			r.transparent = r.transparent[:0]
			for i := range last + 1 {
				if u := a.Units[i]; u.Transparent && u.Kind != KindText && u.Box.Overlaps(r.box) {
					r.transparent = append(r.transparent, i)
				}
			}
			for i := range last + 1 {
				u := a.Units[i]
				if u.First < r.insert || u.Kind == KindText || !u.Box.Overlaps(r.box) || !a.removed(u) || (!u.Transparent && !r.coveredLater(a, i)) {
					continue
				}
				grown := r.box.Union(u.Box)
				changed = changed || grown != r.box
				r.box = grown
			}
		}
		if !changed {
			break
		}
	}
	sort.Slice(regs, func(i, j int) bool { return regs[i].insert < regs[j].insert })
	for _, r := range regs {
		out := Region{Box: r.box, Insert: r.insert, Last: r.last}
		for i, u := range a.Units {
			if u.First < r.insert || !u.Box.Overlaps(r.box) {
				continue
			}
			switch {
			case u.Kind == KindText:
				out.Text = append(out.Text, i)
			case i <= r.last && !u.Transparent && !r.coveredLater(a, i):
				// No transparency is drawn over it: it paints what the image
				// holds where it stands, so it stays as it is.
			case i <= r.last && a.removed(u):
				out.Removed = append(out.Removed, i)
			case i <= r.last:
				out.Clipped = append(out.Clipped, i)
			case u.HasText:
				out.Text = append(out.Text, i)
			}
		}
		a.Regions = append(a.Regions, out)
	}
	return nil
}

// removed reports whether a unit a region's image has to take over is
// removed rather than clipped out of the region: a transparent unit, whose
// painting the image composites; a form showing text, whose text has to go
// above the image; a path that also clips, whose clip outlives it (ISO
// 32000-2, 8.5.4) and so cannot be wrapped in a q and Q; and a unit under a
// CTM that does not invert, which no clip in default user space reaches.
func (a *Analysis) removed(u Unit) bool {
	if u.Transparent || u.HasText || u.Clips {
		return true
	}
	_, ok := a.positions[u.First].ctm.Invert()
	return !ok
}

// drawnUnder reports whether a unit drawn before unit i paints where it
// does, so that i's colours composited over the paper only approximate what
// it composited over.
func (a *Analysis) drawnUnder(i int) bool {
	for j := range i {
		if a.Units[j].Box.Overlaps(a.Units[i].Box) {
			return true
		}
	}
	return false
}

// coveredLater reports whether a transparent unit of the region drawn after
// unit i overlaps it: the image composites that unit over what i paints, so
// i, drawn again where it stands, would paint over the composite.
func (r *growing) coveredLater(a *Analysis, i int) bool {
	for _, j := range r.transparent {
		if j > i && a.Units[j].Box.Overlaps(a.Units[i].Box) {
			return true
		}
	}
	return false
}

// insertion returns the latest position at or before op where an image of
// box can be drawn: one [position.insertable], under an invertible CTM, and
// whose clipping region holds box, either because no W narrowed it or
// because it is a box that holds it. The start of the stream always is one.
func (a *Analysis) insertion(op int, box Rect) int {
	for p := op; p > 0; p-- {
		pos := a.positions[p]
		if !pos.insertable {
			continue
		}
		if _, ok := pos.ctm.Invert(); !ok {
			continue
		}
		if !pos.clipped || (pos.exact && pos.clip.Contains(box)) {
			return p
		}
	}
	return 0
}

// growing is a region while [Analysis.regions] grows it.
type growing struct {
	box Rect
	// insert is the index of the operation the image is drawn before, and
	// last the index of the region's last transparent unit.
	insert, last int
	// transparent lists, in order, the region's transparent units.
	transparent []int
}

// mergeRegions joins regions whose boxes overlap, reporting whether any did.
func mergeRegions(regs []*growing) ([]*growing, bool) {
	merged := false
	for i := 0; i < len(regs); i++ {
		for j := i + 1; j < len(regs); {
			if regs[i].box.Overlaps(regs[j].box) {
				regs[i].box = regs[i].box.Union(regs[j].box)
				regs = append(regs[:j], regs[j+1:]...)
				merged = true
				j = i + 1
				continue
			}
			j++
		}
	}
	return regs, merged
}
