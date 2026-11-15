package writer

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// maxPlacementPageTreeDepth caps the recursion of [measurePlacements] through
// the page tree. Anti-DoS guard, not spec-mandated: ISO 32000-1, 7.7.3.2
// makes the page tree a strict tree, and the reader's own cap
// (document.maxPageTreeDepth) is the same value, so a tree the reader reads
// whole is walked whole here.
const maxPlacementPageTreeDepth = 256

// maxPlacementOperators caps the content-stream operators one
// [measurePlacements] interprets, over every stream and every time a form,
// pattern or glyph is drawn again at another size. Anti-DoS guard, not
// spec-mandated: a form drawn ten times by a form drawn ten times, nested
// ten deep, asks for ten billion; real documents interpret a few million
// operators, each stream once a size. A walk cut short cannot tell how large
// any image is drawn, so no image is downsampled.
const maxPlacementOperators = 1 << 25

// maxPlacementsPerImage caps the placements one image may have. Anti-DoS
// guard, not spec-mandated: a placement costs no memory, only the work of
// the drawing that makes it, which [maxPlacementOperators] bounds; the cap
// keeps one image from absorbing that whole budget. An image drawn on every
// page of a long document stays far under it. An image past it is left at
// its resolution.
const maxPlacementsPerImage = 1 << 16

// maxPlacementDirectDepth caps how deep the census of [measurePlacements]
// descends through direct dictionaries and arrays inside one object.
// Anti-DoS guard, not spec-mandated: real documents nest direct objects a
// handful of levels deep, and a census cut short cannot prove where an image
// is referenced, so no image is downsampled.
const maxPlacementDirectDepth = 64

// maxPlacementRefChain caps how many references the walk follows when a
// reference names an object that is itself a reference. Anti-DoS guard, not
// spec-mandated: ISO 32000-1, 7.3.10 makes an indirect object's value a
// direct object, so a chain is already malformed.
const maxPlacementRefChain = 2

// placementCtxEvery is how many operators or objects pass between two checks
// of the context.
const placementCtxEvery = 100

// pointsPerInch is the size of a default user space unit: 1/72 inch (ISO
// 32000-1, 8.3.2.3).
const pointsPerInch = 72

// linear is the part of a transformation matrix [a b c d e f] that sizes
// what it maps: a, b, c and d (ISO 32000-1, 8.3.3). A translation moves an
// image and leaves its size alone.
type linear [4]float64

// identity is the identity transformation.
var identity = linear{1, 0, 0, 1}

// times returns m followed by n, the matrix m x n of ISO 32000-1, 8.3.4: a
// point p maps to p x m x n. Each product is rounded on its own, so the
// result is the same on every machine.
func (m linear) times(n linear) linear {
	return linear{
		float64(m[0]*n[0]) + float64(m[1]*n[2]),
		float64(m[0]*n[1]) + float64(m[1]*n[3]),
		float64(m[2]*n[0]) + float64(m[3]*n[2]),
		float64(m[2]*n[1]) + float64(m[3]*n[3]),
	}
}

// finite reports whether every entry of m is a finite number.
func (m linear) finite() bool {
	for _, v := range m {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// axisLengths returns the lengths, in inches, that m gives the unit square's
// sides: the image's width along its first row and its height along its
// first column (ISO 32000-1, 8.9.5: an image is drawn as the unit square
// under the current transformation matrix). A rotated or skewed placement
// measures each side along its own direction.
func (m linear) axisLengths() (float64, float64) {
	w := math.Sqrt(float64(m[0]*m[0])+float64(m[1]*m[1])) / pointsPerInch
	h := math.Sqrt(float64(m[2]*m[2])+float64(m[3]*m[3])) / pointsPerInch
	return w, h
}

// imagePlacement is what the walk found of one image: the largest length, in
// inches, any placement gives each of its sides, and how many placements it
// has.
type imagePlacement struct {
	width, height float64
	count         int
}

// unmeasuredReason says why the drawn size of an image is unknown.
type unmeasuredReason string

// placements is what [measurePlacements] found: the size each image is drawn
// at, by object number, and the images whose size it cannot vouch for.
type placements struct {
	drawn      map[int]*imagePlacement
	unmeasured map[int]unmeasuredReason
	// aborted gives why no size can be trusted, "" when the walk finished.
	aborted string
}

// size returns the largest drawn size of image num, false when it is drawn
// nowhere the walk saw or its size is unknown.
func (p *placements) size(num int) (imagePlacement, unmeasuredReason, bool) {
	if p.aborted != "" {
		return imagePlacement{}, unmeasuredReason(p.aborted), false
	}
	if r, ok := p.unmeasured[num]; ok {
		return imagePlacement{}, r, false
	}
	d, ok := p.drawn[num]
	if !ok {
		return imagePlacement{}, reasonNotDrawn, false
	}
	return *d, "", true
}

// The reasons an image's drawn size is unknown, and so the image is not
// downsampled.
const (
	reasonNotDrawn        unmeasuredReason = "drawn nowhere"
	reasonUnreadContent   unmeasuredReason = "drawn by a content stream that does not read"
	reasonTooDeep         unmeasuredReason = "drawn deeper than the form depth budget"
	reasonTooManyPlaces   unmeasuredReason = "drawn too many times"
	reasonReferencedApart unmeasuredReason = "referenced from outside the content read"
	reasonMaskOfUnknown   unmeasuredReason = "mask of an image whose size is unknown"
	// reasonUnmeasurableMatrix is a placement whose matrix overflows: the
	// image is drawn at a size no number holds.
	reasonUnmeasurableMatrix unmeasuredReason = "drawn under a matrix that overflows"
)

// ploc places a value in the object graph: the object it sits in, -1 for the
// trailer, and the keys and indexes leading to it from that object's value.
type ploc struct {
	obj  int
	path string
}

// slot returns the location of the entry key of the value at l.
func (l ploc) slot(key string) ploc {
	if l.path == "" {
		return ploc{obj: l.obj, path: key}
	}
	return ploc{obj: l.obj, path: l.path + "\x00" + key}
}

// pnode is a value and where it is written.
type pnode struct {
	val ast.PdfObject
	at  ploc
}

// dict returns the dictionary of a dictionary or stream node, nil otherwise.
func (n pnode) dict() ast.PdfDict {
	switch v := n.val.(type) {
	case ast.PdfDict:
		return v
	case ast.PdfStream:
		return v.Dict
	}
	return nil
}

// name returns the name entry key of the node's dictionary, "" when absent.
func (n pnode) name(key ast.PdfName) ast.PdfName {
	v, _ := n.dict()[key].(ast.PdfName)
	return v
}

// drawKey identifies a stream drawn under a transformation: the walk
// interprets a stream once a size, a translation moving nothing it measures.
type drawKey struct {
	stream ploc
	res    ploc
	m      linear
}

// placementWalk interprets the content of every page to find where each
// image is drawn. It lives for one [measurePlacements] call, whose context
// its methods are given.
type placementWalk struct {
	pending map[int]ast.PdfObject
	trailer ast.PdfDict
	dec     *filter.Decoder
	lim     limits.Limits
	out     *placements
	// xobjectDicts are the /XObject dictionaries of the resources the walk
	// drew streams with: an image named in another one may be drawn where
	// the walk did not look.
	xobjectDicts map[ploc]bool
	done         map[drawKey]bool
	operators    int
}

// measurePlacements walks the content of every page, its forms, tiling
// patterns, Type 3 glyphs, soft-mask groups and annotation appearances, and
// returns the largest size each image XObject is drawn at.
//
// An image has no resolution of its own: it is drawn as the unit square
// under the current transformation matrix at each Do that paints it (ISO
// 32000-1, 8.9.5 and 8.3.4), through the matrix of each form drawing it
// (8.10.1), of a tiling pattern over the space it is defined in (8.7.3.1),
// of a Type 3 font and the text state (9.6.5, 9.4.4) and of an annotation's
// appearance (12.5.5). A soft mask or a stencil /Mask is drawn where its
// image is (11.6.5.3, 8.9.6.3). The walk keeps, for each side of each image,
// the largest length any placement gives it.
//
// An image's size is unknown, and it is recorded as such, when a stream that
// can draw it does not decode or parse, when it is drawn deeper than the
// document's form depth budget or more than [maxPlacementsPerImage] times, or
// when something other than a resource dictionary the walk read or an
// image's mask entry refers to it.
func measurePlacements(ctx context.Context, pending map[int]ast.PdfObject, trailer ast.PdfDict, dec *filter.Decoder, lim limits.Limits) (*placements, error) {
	w := &placementWalk{
		pending: pending, trailer: trailer, dec: dec, lim: lim,
		out:          &placements{drawn: make(map[int]*imagePlacement), unmeasured: make(map[int]unmeasuredReason)},
		xobjectDicts: make(map[ploc]bool),
		done:         make(map[drawKey]bool),
	}
	root := pnode{val: trailer, at: ploc{obj: -1}}
	catalog, ok := w.child(root, "Root")
	if !ok {
		w.out.aborted = "no catalog"
		return w.out, nil
	}
	if pages, ok := w.child(catalog, "Pages"); ok {
		if err := w.pageTree(ctx, pages, pnode{}, false, 0, make(map[ploc]bool)); err != nil {
			return nil, err
		}
	}
	if w.out.aborted != "" {
		return w.out, nil
	}
	if err := w.census(ctx); err != nil {
		return nil, err
	}
	w.propagateMasks()
	return w.out, nil
}

// deref returns v, written at at, as a node: the object a reference names, at
// its own location, or v itself. A reference to nothing, or a chain past
// [maxPlacementRefChain], is null.
func (w *placementWalk) deref(v ast.PdfObject, at ploc) pnode {
	for range maxPlacementRefChain + 1 {
		ref, ok := v.(ast.PdfIndirectRef)
		if !ok {
			return pnode{val: v, at: at}
		}
		target, ok := w.pending[ref.ObjectNum]
		if !ok {
			return pnode{val: ast.PdfNull{}, at: ploc{obj: ref.ObjectNum}}
		}
		v, at = target, ploc{obj: ref.ObjectNum}
	}
	return pnode{val: ast.PdfNull{}, at: at}
}

// child returns the entry key of a dictionary or stream node, false when
// there is none or it is null.
func (w *placementWalk) child(n pnode, key ast.PdfName) (pnode, bool) {
	d := n.dict()
	if d == nil {
		return pnode{}, false
	}
	v, ok := d[key]
	if !ok {
		return pnode{}, false
	}
	got := w.deref(v, n.at.slot(string(key)))
	if _, null := got.val.(ast.PdfNull); null || got.val == nil {
		return pnode{}, false
	}
	return got, true
}

// elements returns the elements of an array node, a lone value as itself.
func (w *placementWalk) elements(n pnode) []pnode {
	a, ok := n.val.(ast.PdfArray)
	if !ok {
		return []pnode{n}
	}
	out := make([]pnode, 0, len(a))
	for i, v := range a {
		got := w.deref(v, n.at.slot(strconv.Itoa(i)))
		if _, null := got.val.(ast.PdfNull); null || got.val == nil {
			continue
		}
		out = append(out, got)
	}
	return out
}

// numbers returns the numbers of an array node, false when it is not an
// array of n numbers.
func (w *placementWalk) numbers(n pnode, count int) ([]float64, bool) {
	a, ok := n.val.(ast.PdfArray)
	if !ok || len(a) != count {
		return nil, false
	}
	out := make([]float64, count)
	for i, v := range a {
		f, ok := document.Number(w.deref(v, n.at).val)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, false
		}
		out[i] = f
	}
	return out, true
}

// matrixOf returns the /Matrix entry of a node as its linear part, the
// identity when it is absent or not six numbers.
func (w *placementWalk) matrixOf(n pnode) linear {
	m, ok := w.child(n, "Matrix")
	if !ok {
		return identity
	}
	v, ok := w.numbers(m, 6)
	if !ok {
		return identity
	}
	return linear{v[0], v[1], v[2], v[3]}
}

// pageTree walks the page tree node n, whose inherited resources are res
// (ISO 32000-1, 7.7.3.4), drawing the content and annotation appearances of
// every page.
func (w *placementWalk) pageTree(ctx context.Context, n, res pnode, hasRes bool, depth int, seen map[ploc]bool) error {
	if w.out.aborted != "" || seen[n.at] || n.dict() == nil {
		return nil
	}
	if depth > maxPlacementPageTreeDepth {
		w.out.aborted = fmt.Sprintf("a page tree deeper than %d", maxPlacementPageTreeDepth)
		return nil
	}
	seen[n.at] = true
	if own, ok := w.child(n, "Resources"); ok {
		res, hasRes = own, true
	}
	if kids, ok := w.child(n, "Kids"); ok && n.name("Type") != "Page" {
		for _, kid := range w.elements(kids) {
			if err := w.pageTree(ctx, kid, res, hasRes, depth+1, seen); err != nil {
				return err
			}
		}
		return nil
	}
	// A page's default user space unit is /UserUnit times 1/72 inch (ISO
	// 32000-1, 8.3.2.3, Table 30).
	base := identity
	if u, ok := w.child(n, "UserUnit"); ok {
		if f, ok := document.Number(u.val); ok && f > 0 && !math.IsInf(f, 0) {
			base = linear{f, 0, 0, f}
		}
	}
	if c, ok := w.child(n, "Contents"); ok {
		if err := w.draw(ctx, c, res, hasRes, base, 0); err != nil {
			return err
		}
	}
	annots, ok := w.child(n, "Annots")
	if !ok {
		return nil
	}
	for _, annot := range w.elements(annots) {
		if err := w.annotation(ctx, annot, res, hasRes, base); err != nil {
			return err
		}
	}
	return nil
}

// annotation draws every appearance stream of an annotation, each state of
// /N, /R and /D, where ISO 32000-1, 12.5.5 places it: its bounding box, under
// its matrix, fitted to the annotation's rectangle.
func (w *placementWalk) annotation(ctx context.Context, annot, res pnode, hasRes bool, base linear) error {
	rectNode, ok := w.child(annot, "Rect")
	if !ok {
		return nil
	}
	rect, ok := w.numbers(rectNode, 4)
	if !ok {
		return nil
	}
	ap, ok := w.child(annot, "AP")
	if !ok {
		return nil
	}
	var streams []pnode
	for _, key := range []ast.PdfName{"N", "R", "D"} {
		v, ok := w.child(ap, key)
		if !ok {
			continue
		}
		if _, isStream := v.val.(ast.PdfStream); isStream {
			streams = append(streams, v)
			continue
		}
		for _, state := range sortedKeys(v.dict()) {
			if s, ok := w.child(v, state); ok {
				if _, isStream := s.val.(ast.PdfStream); isStream {
					streams = append(streams, s)
				}
			}
		}
	}
	for _, s := range streams {
		bboxNode, ok := w.child(s, "BBox")
		if !ok {
			continue
		}
		bbox, ok := w.numbers(bboxNode, 4)
		if !ok {
			continue
		}
		m := w.matrixOf(s)
		fit, ok := fitAppearance(m, bbox, rect)
		if !ok {
			continue
		}
		formRes, formHasRes := res, hasRes
		if own, ok := w.child(s, "Resources"); ok {
			formRes, formHasRes = own, true
		}
		if err := w.draw(ctx, s, formRes, formHasRes, m.times(fit).times(base), 1); err != nil {
			return err
		}
	}
	return nil
}

// fitAppearance returns the scaling of ISO 32000-1, 12.5.5, step 2: the
// matrix A that maps the appearance's bounding box, transformed by its
// matrix m, onto the annotation's rectangle. It reports false for a box or
// a rectangle of no area, which draws nothing.
func fitAppearance(m linear, bbox, rect []float64) (linear, bool) {
	xs := [4]float64{bbox[0], bbox[2], bbox[0], bbox[2]}
	ys := [4]float64{bbox[1], bbox[1], bbox[3], bbox[3]}
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	for i := range 4 {
		x := float64(xs[i]*m[0]) + float64(ys[i]*m[2])
		y := float64(xs[i]*m[1]) + float64(ys[i]*m[3])
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	boxW, boxH := maxX-minX, maxY-minY
	rectW, rectH := math.Abs(rect[2]-rect[0]), math.Abs(rect[3]-rect[1])
	if !(boxW > 0) || !(boxH > 0) || !(rectW > 0) || !(rectH > 0) {
		return linear{}, false
	}
	a := linear{rectW / boxW, 0, 0, rectH / boxH}
	return a, a.finite()
}

// draw interprets a content stream, or a page's array of streams, drawn with
// resources res under the transformation m, at form depth depth.
func (w *placementWalk) draw(ctx context.Context, c, res pnode, hasRes bool, m linear, depth int) error {
	if w.out.aborted != "" || !m.finite() {
		return nil
	}
	key := drawKey{stream: c.at, m: m}
	if hasRes {
		key.res = res.at
	} else {
		key.res = ploc{obj: -2}
	}
	if w.done[key] {
		return nil
	}
	w.done[key] = true
	if hasRes {
		if xobjects, ok := w.child(res, "XObject"); ok {
			w.xobjectDicts[xobjects.at] = true
		}
	}
	data, ok, err := w.contentBytes(ctx, c)
	if err != nil {
		return err
	}
	if !ok {
		w.taint(res, hasRes, reasonUnreadContent, 0, make(map[ploc]bool))
		return nil
	}
	for _, opts := range [][]content.Option{nil, {content.SplitGluedOperators()}} {
		ops, err := content.ParseStreamData(content.WithWholeStreams(ctx), data, opts...)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("reading a content stream: %w", ctxErr)
		}
		if err != nil || (len(ops) == 0 && len(bytes.TrimSpace(data)) > 0) {
			// A stream the parser gives up on draws what a reader makes of
			// it, which the walk cannot tell.
			w.taint(res, hasRes, reasonUnreadContent, 0, make(map[ploc]bool))
			return nil //nolint:nilerr // A stream that does not parse leaves the images it may draw at their resolution; the write goes on.
		}
		if err := w.interpret(ctx, ops, res, hasRes, m, depth); err != nil {
			return err
		}
		if w.out.aborted != "" {
			return nil
		}
	}
	return nil
}

// contentBytes returns the decoded content of a stream, or of a page's
// streams joined with white space, which ISO 32000-1, 7.7.3.3 makes one
// content stream. ok is false when a stream does not decode whole or the
// content passes the document's budget for a page's content.
func (w *placementWalk) contentBytes(ctx context.Context, c pnode) ([]byte, bool, error) {
	var out []byte
	for _, n := range w.elements(c) {
		s, ok := n.val.(ast.PdfStream)
		if !ok {
			return nil, false, nil
		}
		if _, external := s.Dict["F"]; external {
			return nil, false, nil
		}
		source, ok := DirectFilterKeys(w.pending, s.Dict)
		if !ok {
			return nil, false, nil
		}
		data, ok, err := DecodeWhole(ctx, w.dec, ast.NewLazyStream(source, s.Bytes))
		if err != nil {
			return nil, false, fmt.Errorf("decoding a content stream: %w", err)
		}
		if !ok || int64(len(out))+int64(len(data)) > w.lim.MaxPageContentBytes() {
			return nil, false, nil
		}
		out = append(out, data...)
		out = append(out, '\n')
	}
	return out, true, nil
}

// textState is the part of the text state that sizes a Type 3 glyph (ISO
// 32000-1, 9.3 and 9.4.4): the linear part of the text matrix, the font and
// its size, and the horizontal scaling.
type textState struct {
	tm     linear
	font   ast.PdfName
	size   float64
	hscale float64
}

// interpret follows the transformation and text state through ops and
// records every image they draw.
func (w *placementWalk) interpret(ctx context.Context, ops []content.Operation, res pnode, hasRes bool, base linear, depth int) error {
	type gstate struct {
		ctm  linear
		text textState
	}
	cur := gstate{ctm: base, text: textState{tm: identity, hscale: 1}}
	var stack []gstate
	for _, op := range ops {
		w.operators++
		if w.operators%placementCtxEvery == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("interpreting a content stream: %w", err)
			}
		}
		if w.operators > maxPlacementOperators {
			w.out.aborted = fmt.Sprintf("more than %d operators", maxPlacementOperators)
			return nil
		}
		var err error
		switch op.Operator {
		case "q":
			stack = append(stack, cur)
		case "Q":
			if n := len(stack); n > 0 {
				cur, stack = stack[n-1], stack[:n-1]
			}
		case "cm":
			if v, ok := operandNumbers(op, 6); ok {
				cur.ctm = linear{v[0], v[1], v[2], v[3]}.times(cur.ctm)
			}
		case "BT":
			cur.text.tm = identity
		case "Tm":
			if v, ok := operandNumbers(op, 6); ok {
				cur.text.tm = linear{v[0], v[1], v[2], v[3]}
			}
		case "Tf":
			if len(op.Operands) == 2 {
				cur.text.font, _ = op.Operands[0].(ast.PdfName)
				cur.text.size, _ = document.Number(op.Operands[1])
			}
		case "Tz":
			if v, ok := operandNumbers(op, 1); ok {
				cur.text.hscale = v[0] / 100
			}
		case "Tj", "TJ", "'", "\"":
			err = w.showType3(ctx, cur.text, cur.ctm, res, hasRes, depth)
		case "Do":
			err = w.do(ctx, op, cur.ctm, res, hasRes, depth)
		case "gs":
			err = w.softMask(ctx, op, cur.ctm, res, hasRes, depth)
		case "scn", "SCN":
			err = w.pattern(ctx, op, base, res, hasRes, depth)
		}
		if err != nil {
			return err
		}
		if w.out.aborted != "" {
			return nil
		}
	}
	return nil
}

// operandNumbers returns the operands of op as n numbers, false when they
// are not.
func operandNumbers(op content.Operation, n int) ([]float64, bool) {
	if len(op.Operands) != n {
		return nil, false
	}
	out := make([]float64, n)
	for i, v := range op.Operands {
		f, ok := document.Number(v)
		if !ok {
			return nil, false
		}
		out[i] = f
	}
	return out, true
}

// resource returns the entry name of the category of res, false when absent.
func (w *placementWalk) resource(res pnode, hasRes bool, category, name ast.PdfName) (pnode, bool) {
	if !hasRes {
		return pnode{}, false
	}
	cat, ok := w.child(res, category)
	if !ok {
		return pnode{}, false
	}
	return w.child(cat, name)
}

// do records the image a Do paints under ctm, or draws the form it paints
// (ISO 32000-1, 8.8 and 8.10.1).
func (w *placementWalk) do(ctx context.Context, op content.Operation, ctm linear, res pnode, hasRes bool, depth int) error {
	if len(op.Operands) != 1 {
		return nil
	}
	name, _ := op.Operands[0].(ast.PdfName)
	x, ok := w.resource(res, hasRes, "XObject", name)
	if !ok {
		return nil
	}
	if _, isStream := x.val.(ast.PdfStream); !isStream {
		return nil
	}
	switch x.name("Subtype") {
	case "Image":
		if x.at.path == "" {
			w.place(x.at.obj, ctm)
		}
	case "Form":
		return w.form(ctx, x, w.matrixOf(x).times(ctm), res, hasRes, depth)
	}
	return nil
}

// form draws a form XObject, a tiling pattern, a soft-mask group or a glyph
// procedure s under the transformation m, with its own resources or, lacking
// them, those of the stream that draws it (ISO 32000-1, 7.8.3).
func (w *placementWalk) form(ctx context.Context, s pnode, m linear, res pnode, hasRes bool, depth int) error {
	formRes, formHasRes := res, hasRes
	if own, ok := w.child(s, "Resources"); ok {
		formRes, formHasRes = own, true
	}
	if depth+1 > w.lim.MaxFormDepth {
		// A reader that keeps to the budget does not draw past it either,
		// but another reader may.
		w.taint(formRes, formHasRes, reasonTooDeep, 0, make(map[ploc]bool))
		return nil
	}
	return w.draw(ctx, s, formRes, formHasRes, m, depth+1)
}

// softMask draws the group of the soft mask a graphics state sets, in the
// coordinates in force when gs sets it (ISO 32000-1, 11.6.5.2).
func (w *placementWalk) softMask(ctx context.Context, op content.Operation, ctm linear, res pnode, hasRes bool, depth int) error {
	if len(op.Operands) != 1 {
		return nil
	}
	name, _ := op.Operands[0].(ast.PdfName)
	gs, ok := w.resource(res, hasRes, "ExtGState", name)
	if !ok {
		return nil
	}
	mask, ok := w.child(gs, "SMask")
	if !ok {
		return nil
	}
	group, ok := w.child(mask, "G")
	if !ok {
		return nil
	}
	if _, isStream := group.val.(ast.PdfStream); !isStream {
		return nil
	}
	return w.form(ctx, group, w.matrixOf(group).times(ctm), res, hasRes, depth)
}

// pattern draws the tiling pattern a colour operator selects, under its
// matrix over the default coordinates of the stream it is a resource of,
// base (ISO 32000-1, 8.7.3.1). Selecting a pattern paints nothing yet, but
// the walk draws every pattern selected, which can only add placements.
func (w *placementWalk) pattern(ctx context.Context, op content.Operation, base linear, res pnode, hasRes bool, depth int) error {
	if len(op.Operands) == 0 {
		return nil
	}
	name, ok := op.Operands[len(op.Operands)-1].(ast.PdfName)
	if !ok {
		return nil
	}
	p, ok := w.resource(res, hasRes, "Pattern", name)
	if !ok {
		return nil
	}
	if _, isStream := p.val.(ast.PdfStream); !isStream {
		return nil
	}
	return w.form(ctx, p, w.matrixOf(p).times(base), res, hasRes, depth)
}

// showType3 draws every glyph procedure of the Type 3 font a text-showing
// operator shows with, under the glyph space of ISO 32000-1, 9.6.5: the
// font matrix, then the text rendering matrix of 9.4.4 without its
// translation. Every glyph of the font is drawn, not only those the string
// selects, which can only add placements.
func (w *placementWalk) showType3(ctx context.Context, text textState, ctm linear, res pnode, hasRes bool, depth int) error {
	font, ok := w.resource(res, hasRes, "Font", text.font)
	if !ok || font.name("Subtype") != "Type3" {
		return nil
	}
	procs, ok := w.child(font, "CharProcs")
	if !ok {
		return nil
	}
	fm := identity
	if node, ok := w.child(font, "FontMatrix"); ok {
		if v, ok := w.numbers(node, 6); ok {
			fm = linear{v[0], v[1], v[2], v[3]}
		}
	}
	scale := linear{float64(text.size * text.hscale), 0, 0, text.size}
	m := fm.times(scale).times(text.tm).times(ctm)
	for _, glyph := range sortedKeys(procs.dict()) {
		s, ok := w.child(procs, glyph)
		if !ok {
			continue
		}
		if _, isStream := s.val.(ast.PdfStream); !isStream {
			continue
		}
		// A glyph procedure takes the font's resources, else those of the
		// stream showing it (ISO 32000-1, 9.6.5, Table 112).
		glyphRes, glyphHasRes := res, hasRes
		if own, ok := w.child(font, "Resources"); ok {
			glyphRes, glyphHasRes = own, true
		}
		if err := w.form(ctx, s, m, glyphRes, glyphHasRes, depth); err != nil {
			return err
		}
		if w.out.aborted != "" {
			return nil
		}
	}
	return nil
}

// place records that image num is drawn under ctm.
func (w *placementWalk) place(num int, ctm linear) {
	if _, ok := w.out.unmeasured[num]; ok {
		return
	}
	p := w.out.drawn[num]
	if p == nil {
		p = &imagePlacement{}
		w.out.drawn[num] = p
	}
	p.count++
	if p.count > maxPlacementsPerImage {
		w.out.unmeasured[num] = reasonTooManyPlaces
		delete(w.out.drawn, num)
		return
	}
	width, height := ctm.axisLengths()
	if math.IsNaN(width) || math.IsNaN(height) || math.IsInf(width, 0) || math.IsInf(height, 0) {
		w.out.unmeasured[num] = reasonUnmeasurableMatrix
		delete(w.out.drawn, num)
		return
	}
	p.width, p.height = math.Max(p.width, width), math.Max(p.height, height)
}

// taint records that every image a stream drawing with res may draw has an
// unknown size: the images res names, and those of the forms, patterns,
// Type 3 glyphs and soft-mask groups it names, down to the form depth
// budget.
func (w *placementWalk) taint(res pnode, hasRes bool, reason unmeasuredReason, depth int, seen map[ploc]bool) {
	if !hasRes || seen[res.at] || depth > w.lim.MaxFormDepth {
		return
	}
	seen[res.at] = true
	streamsOf := func(category ast.PdfName) []pnode {
		cat, ok := w.child(res, category)
		if !ok {
			return nil
		}
		var out []pnode
		for _, name := range sortedKeys(cat.dict()) {
			if v, ok := w.child(cat, name); ok {
				out = append(out, v)
			}
		}
		return out
	}
	inner := func(s pnode) {
		if own, ok := w.child(s, "Resources"); ok {
			w.taint(own, true, reason, depth+1, seen)
		}
	}
	for _, x := range streamsOf("XObject") {
		if _, isStream := x.val.(ast.PdfStream); !isStream {
			continue
		}
		switch x.name("Subtype") {
		case "Image":
			if x.at.path == "" {
				w.out.unmeasured[x.at.obj] = reason
				delete(w.out.drawn, x.at.obj)
			}
		case "Form":
			inner(x)
		}
	}
	for _, p := range streamsOf("Pattern") {
		inner(p)
	}
	for _, f := range streamsOf("Font") {
		inner(f)
	}
	for _, gs := range streamsOf("ExtGState") {
		if mask, ok := w.child(gs, "SMask"); ok {
			if group, ok := w.child(mask, "G"); ok {
				inner(group)
			}
		}
	}
}

// census checks every reference to an image: one held by an /XObject
// dictionary the walk drew with, or by the /SMask or /Mask entry of an image
// (ISO 32000-1, 8.9.5.1, Table 89), is a place the walk measured. Any other
// may draw the image where the walk did not look, so its size is unknown.
func (w *placementWalk) census(ctx context.Context) error {
	nums := make([]int, 0, len(w.pending))
	for num := range w.pending {
		nums = append(nums, num)
	}
	slices.Sort(nums)
	w.scan(w.trailer, ploc{obj: -1}, "", false, 0)
	for i, num := range nums {
		if i%placementCtxEvery == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("finding the references to images: %w", err)
			}
		}
		obj := w.pending[num]
		s, isStream := obj.(ast.PdfStream)
		w.scan(obj, ploc{obj: num}, "", isStream && isImageStream(s.Dict), 0)
		if w.out.aborted != "" {
			return nil
		}
	}
	return nil
}

// scan checks the references v holds, written at the location given under
// key of its container; inImage reports that the container is an image's
// dictionary.
func (w *placementWalk) scan(v ast.PdfObject, at ploc, key string, inImage bool, depth int) {
	if depth > maxPlacementDirectDepth {
		w.out.aborted = fmt.Sprintf("a value nested more than %d levels deep", maxPlacementDirectDepth)
		return
	}
	switch v := v.(type) {
	case ast.PdfIndirectRef:
		target, ok := w.pending[v.ObjectNum].(ast.PdfStream)
		if !ok || !isImageStream(target.Dict) {
			return
		}
		container := ploc{obj: at.obj}
		if i := lastSep(at.path); i >= 0 {
			container.path = at.path[:i]
		}
		switch {
		case w.xobjectDicts[container]:
		case inImage && container.path == "" && (key == "SMask" || key == "Mask"):
		case key == "Obj":
			// An object reference of the structure tree names what a
			// structure element holds, which draws nothing (ISO 32000-1,
			// 14.7.4.3).
		default:
			w.out.unmeasured[v.ObjectNum] = reasonReferencedApart
			delete(w.out.drawn, v.ObjectNum)
		}
	case ast.PdfStream:
		w.scan(v.Dict, at, key, inImage, depth)
	case ast.PdfDict:
		for _, k := range sortedKeys(v) {
			w.scan(v[k], at.slot(string(k)), string(k), inImage && at.path == "", depth+1)
		}
	case ast.PdfArray:
		for i, elem := range v {
			w.scan(elem, at.slot(strconv.Itoa(i)), strconv.Itoa(i), false, depth+1)
		}
	}
}

// lastSep returns the index of the last separator of a [ploc] path, -1 when
// it has none.
func lastSep(path string) int {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == 0 {
			return i
		}
	}
	return -1
}

// propagateMasks gives each soft mask and stencil mask the placements of the
// images it masks, which draw it where they are drawn (ISO 32000-1, 11.6.5.3
// and 8.9.6.3), and an unknown size when one of them has one.
func (w *placementWalk) propagateMasks() {
	nums := make([]int, 0, len(w.pending))
	for num, obj := range w.pending {
		if s, ok := obj.(ast.PdfStream); ok && isImageStream(s.Dict) {
			nums = append(nums, num)
		}
	}
	slices.Sort(nums)
	masks := make(map[int]*imagePlacement)
	var maskNums []int
	for _, num := range nums {
		s := w.pending[num].(ast.PdfStream)
		for _, key := range []ast.PdfName{"SMask", "Mask"} {
			ref, ok := s.Dict[key].(ast.PdfIndirectRef)
			if !ok {
				continue
			}
			mask, ok := w.pending[ref.ObjectNum].(ast.PdfStream)
			if !ok || !isImageStream(mask.Dict) || ref.ObjectNum == num {
				continue
			}
			m := masks[ref.ObjectNum]
			if m == nil {
				m = &imagePlacement{}
				masks[ref.ObjectNum] = m
				maskNums = append(maskNums, ref.ObjectNum)
			}
			if _, unknown := w.out.unmeasured[num]; unknown {
				m.count = -1
				continue
			}
			if m.count < 0 {
				continue
			}
			if p, ok := w.out.drawn[num]; ok {
				m.width, m.height = math.Max(m.width, p.width), math.Max(m.height, p.height)
				m.count += p.count
			}
		}
	}
	slices.Sort(maskNums)
	for _, num := range maskNums {
		m := masks[num]
		if _, unknown := w.out.unmeasured[num]; unknown {
			continue
		}
		if m.count < 0 {
			w.out.unmeasured[num] = reasonMaskOfUnknown
			delete(w.out.drawn, num)
			continue
		}
		if m.count == 0 {
			continue
		}
		p := w.out.drawn[num]
		if p == nil {
			p = &imagePlacement{}
			w.out.drawn[num] = p
		}
		p.width, p.height = math.Max(p.width, m.width), math.Max(p.height, m.height)
		p.count += m.count
	}
}
