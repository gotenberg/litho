package typography

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// maxType3InkDepth bounds how deeply bounding a Type 3 glyph descends into
// the Type 3 fonts its description shows text in: a glyph showing text in a
// Type 3 font, whose glyph shows text in another, resolves each font in turn.
// Anti-DoS guard, not spec-mandated: ISO 32000-1, 9.6.5 sets no limit. It
// mirrors maxType3Depth in render/page, the nesting the renderer draws, so a
// glyph the renderer draws whole is bounded whole. A glyph nested deeper is
// bounded by the clip it is shown under ([type3InkBounds]), which holds
// whatever it paints.
const maxType3InkDepth = 4

// type3ChainKey is the context key of the [type3Chain] in force.
type type3ChainKey struct{}

// type3Chain is the Type 3 fonts whose glyphs are being bounded below the
// font now resolved: the object numbers of the fonts their descriptions show
// text in, the fonts whose glyphs are being bounded against the resources of
// the stream showing them ([Font.type3ScopedInk]), and how many such
// descents are in progress. A font already on the chain is one a glyph shows
// itself through, a cycle the renderer stops only at its nesting bound, so
// the glyph is bounded by its clip instead.
type type3Chain struct {
	fonts  map[int]bool
	scoped map[*Font]bool
	depth  int
}

// type3ChainFrom returns the chain ctx carries, the empty one when none.
func type3ChainFrom(ctx context.Context) type3Chain {
	if c, ok := ctx.Value(type3ChainKey{}).(type3Chain); ok {
		return c
	}
	return type3Chain{}
}

// descend returns the chain one descent below c into the font objNum.
func (c type3Chain) descend(objNum int) type3Chain {
	fonts := make(map[int]bool, len(c.fonts)+1)
	for n := range c.fonts {
		fonts[n] = true
	}
	fonts[objNum] = true
	return type3Chain{fonts: fonts, scoped: c.scoped, depth: c.depth + 1}
}

// descendScoped returns the chain one descent below c into the glyphs of
// font, bounded against the resources of the stream showing them.
func (c type3Chain) descendScoped(font *Font) type3Chain {
	scoped := make(map[*Font]bool, len(c.scoped)+1)
	for f := range c.scoped {
		scoped[f] = true
	}
	scoped[font] = true
	return type3Chain{fonts: c.fonts, scoped: scoped, depth: c.depth + 1}
}

// type3Painter resolves what a Type 3 glyph description names: resources is
// the font's /Resources dictionary (ISO 32000-1, 9.6.5, Table 112), nil when
// the font states none and its glyphs take those of whatever stream shows
// them.
type type3Painter struct {
	doc       *document.Document
	resources ast.PdfDict
}

// named returns the resource name selects in category, nil when the font's
// resources do not name it.
func (p type3Painter) named(ctx context.Context, category ast.PdfName, name ast.PdfObject) ast.PdfObject {
	key, ok := name.(ast.PdfName)
	if !ok || p.resources == nil {
		return nil
	}
	dict, err := document.ResolveDict(ctx, p.doc, p.resources[category])
	if err != nil || dict == nil {
		return nil
	}
	obj, err := document.ResolveObject(ctx, p.doc, dict[key])
	if err != nil {
		return nil
	}
	return obj
}

// xobjectBox returns the bounds of what the XObject name paints, in the
// space of the Do that paints it: the unit square of an image (ISO 32000-1,
// 8.9.5), the /BBox of a form through its /Matrix, which clips the form's
// content (8.10.1). It returns false for an XObject it cannot place.
func (p type3Painter) xobjectBox(ctx context.Context, name ast.PdfObject) ([4][2]float64, bool) {
	stream, ok := p.named(ctx, "XObject", name).(ast.PdfStream)
	if !ok {
		return [4][2]float64{}, false
	}
	subtype, _ := stream.Dict["Subtype"].(ast.PdfName)
	switch subtype {
	case "Image":
		return rectCorners(0, 0, 1, 1), true
	case "Form":
		bbox, err := document.ResolveArray(ctx, p.doc, stream.Dict["BBox"])
		if err != nil || len(bbox) != 4 {
			return [4][2]float64{}, false
		}
		m := [6]float64{1, 0, 0, 1, 0, 0}
		if arr, err := document.ResolveArray(ctx, p.doc, stream.Dict["Matrix"]); err == nil && len(arr) == 6 {
			for i := range 6 {
				m[i] = document.Float(arr[i])
			}
		}
		var out [4][2]float64
		for i, c := range rectCorners(document.Float(bbox[0]), document.Float(bbox[1]), document.Float(bbox[2]), document.Float(bbox[3])) {
			out[i] = [2]float64{m[0]*c[0] + m[2]*c[1] + m[4], m[1]*c[0] + m[3]*c[1] + m[5]}
		}
		return out, true
	}
	return [4][2]float64{}, false
}

// shadingBox returns the /BBox of the shading name, in the space of the sh
// that paints it, which the shading is clipped to (ISO 32000-1, 8.7.4.3,
// Table 77), false when it states none.
func (p type3Painter) shadingBox(ctx context.Context, name ast.PdfObject) ([4][2]float64, bool) {
	var dict ast.PdfDict
	switch v := p.named(ctx, "Shading", name).(type) {
	case ast.PdfDict:
		dict = v
	case ast.PdfStream:
		dict = v.Dict
	default:
		return [4][2]float64{}, false
	}
	bbox, err := document.ResolveArray(ctx, p.doc, dict["BBox"])
	if err != nil || len(bbox) != 4 {
		return [4][2]float64{}, false
	}
	return rectCorners(document.Float(bbox[0]), document.Float(bbox[1]), document.Float(bbox[2]), document.Float(bbox[3])), true
}

// setsFont reports whether the graphics state parameter dictionary name sets
// a font (ISO 32000-1, 8.4.5, Table 58, /Font), or may: one the font's
// resources do not name might.
func (p type3Painter) setsFont(ctx context.Context, name ast.PdfObject) bool {
	dict, err := document.ResolveDict(ctx, p.doc, p.named(ctx, "ExtGState", name))
	if err != nil || dict == nil {
		return true
	}
	_, ok := dict["Font"]
	return ok
}

// lineParams returns the line width, line cap, line join and miter limit
// that the graphics state parameter dictionary name sets (ISO 32000-1, 8.4.5,
// Table 58, /LW, /LC, /LJ and /ML), each one it leaves out or that is not a
// number as given in w, lineCap, join and miterLimit. A dictionary the
// font's resources do not name sets none.
func (p type3Painter) lineParams(ctx context.Context, name ast.PdfObject, w float64, lineCap, join int, miterLimit float64) (float64, int, int, float64) {
	dict, err := document.ResolveDict(ctx, p.doc, p.named(ctx, "ExtGState", name))
	if err != nil || dict == nil {
		return w, lineCap, join, miterLimit
	}
	number := func(key ast.PdfName, v float64) float64 {
		obj, err := document.ResolveObject(ctx, p.doc, dict[key])
		if err != nil {
			return v
		}
		if f, ok := document.Number(obj); ok {
			return f
		}
		return v
	}
	w = number("LW", w)
	miterLimit = number("ML", miterLimit)
	lineCap = int(number("LC", float64(lineCap)))
	join = int(number("LJ", float64(join)))
	return w, lineCap, join, miterLimit
}

// cachedFont resolves the font name selects into the document's font cache
// and reports whether it is there, so that bounding text shown in it costs a
// cache hit however many glyphs show it. A font named directly rather than
// by reference has no cache entry, and a font on chain, or one more descent
// past [maxType3InkDepth], is not resolved: either would resolve again for
// every glyph that names it.
func (p type3Painter) cachedFont(ctx context.Context, chain type3Chain, name ast.PdfObject) bool {
	key, ok := name.(ast.PdfName)
	if !ok || p.resources == nil || p.doc == nil {
		return false
	}
	fonts, err := document.ResolveDict(ctx, p.doc, p.resources["Font"])
	if err != nil || fonts == nil {
		return false
	}
	ref, ok := fonts[key].(ast.PdfIndirectRef)
	if !ok || chain.fonts[ref.ObjectNum] {
		return false
	}
	if _, ok := p.doc.CachedFont(ref.ObjectNum); ok {
		return true
	}
	if chain.depth+1 >= maxType3InkDepth {
		return false
	}
	inner := context.WithValue(ctx, type3ChainKey{}, chain.descend(ref.ObjectNum))
	if font, _ := resolveFontFromResources(inner, p.doc, p.resources, key); font == nil {
		return false
	}
	_, ok = p.doc.CachedFont(ref.ObjectNum)
	return ok
}

// type3PaintBounds returns the bounds, in glyph space, of the marks the
// operators ops of a Type 3 glyph description paint, nil when they paint
// none, and whether it paints past every box in glyph space.
//
// It follows the current transformation matrix through q, Q and cm (ISO
// 32000-1, 8.4.4), so a bitmap glyph drawn as an image (8.9.5: the unit
// square through the CTM) and a path drawn in pixels under a scaling both
// land where they paint. A stroke widens its path by the reach of its pen
// ([strokeReach]): the line width, cap, join and miter limit that w, J, j
// and M or a gs set (8.4.3.2 to 8.4.3.5, 8.4.5). A path that is only
// clipped or ended by n paints nothing, and a clip it sets bounds every mark
// after it until Q restores the clip before it (8.5.4), the clip held as the
// box of its path. A form XObject paints inside its /BBox (8.10.1), and a
// shading inside its /BBox (8.7.4.3) or else the whole clip (8.7.4.2). Text the description shows is bounded by
// running the description through the text extractor in glyph space, each
// glyph by its own ink, which a Type 3 font's glyphs reach through this walk
// in turn.
//
// What the walk cannot bound, a shading or text over a clip the description
// never set, an XObject or a font its resources do not name, a font on the
// [type3Chain], paints anywhere the clip lets it: within the clip the
// description set, or past every box when it set none.
func type3PaintBounds(ctx context.Context, p type3Painter, ops []content.Operation) (*[4]float64, bool) {
	// tf says what the font of the text state is: the description's own
	// Tf set none, so the font is the one showing the glyph; a Tf set one
	// the walk can bound text in; or one it cannot.
	const (
		tfOuter = iota
		tfKnown
		tfUnknown
	)
	type gstate struct {
		m [6]float64
		// lw, lc, lj and ml are the line width, cap, join and miter limit
		// a stroke is widened by ([strokeReach]).
		lw     float64
		lc, lj int
		ml     float64
		// clip is the box of the clip the description set, nil for none.
		// A box is never changed in place, so a saved state keeps its own.
		clip *[4]float64
		tf   int
	}
	// TODO: a description runs under the graphics state of the text that
	// shows it (9.6.5), so a stroke before any w, J, j, M or gs uses the line
	// parameters in force there, which this walk, run once per font, reads as
	// the initial ones; the redaction pixel check catches the pen it misses.
	cur := gstate{m: [6]float64{1, 0, 0, 1, 0, 0}, lw: 1, ml: initialMiterLimit}
	// The stack is bounded by the operator count of an already parsed
	// stream, so an unbalanced q costs no more than the stream itself.
	var stack []gstate
	var painted, path *[4]float64
	unbounded, clipping := false, false
	// segs counts the segments of the current subpath, and joined says the
	// path joins two segments somewhere: a subpath of two segments or more,
	// or a closed one (ISO 32000-1, 8.5.2.1). A path that joins none has no
	// join a miter could reach past its pen from.
	segs, joined := 0, false
	segment := func() {
		segs++
		if segs >= 2 {
			joined = true
		}
	}
	closePath := func() {
		if segs > 0 {
			joined = true
		}
	}
	chain := type3ChainFrom(ctx)
	transform := func(x, y float64) [2]float64 {
		m := cur.m
		return [2]float64{m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]}
	}
	// mark adds a mark's box, cut to the clip.
	mark := func(box [4]float64) {
		if c := cur.clip; c != nil {
			box = [4]float64{max(box[0], c[0]), max(box[1], c[1]), min(box[2], c[2]), min(box[3], c[3])}
			if box[0] > box[2] || box[1] > box[3] {
				return
			}
		}
		painted = unionBox(painted, &box)
	}
	// anywhere adds a mark the walk cannot place: the whole clip.
	anywhere := func() {
		if cur.clip == nil {
			unbounded = true
			return
		}
		mark(*cur.clip)
	}
	markCorners := func(corners [4][2]float64) {
		var box *[4]float64
		for _, c := range corners {
			q := transform(c[0], c[1])
			box = unionBox(box, &[4]float64{q[0], q[1], q[0], q[1]})
		}
		mark(*box)
	}
	addPath := func(x, y float64) { path = unionBox(path, &[4]float64{x, y, x, y}) }
	// endPath ends the path, which narrows the clip when W or W* marked it.
	endPath := func() {
		if clipping && path != nil {
			clip := *path
			if c := cur.clip; c != nil {
				clip = [4]float64{max(clip[0], c[0]), max(clip[1], c[1]), min(clip[2], c[2]), min(clip[3], c[3])}
			}
			cur.clip = &clip
		}
		clipping = false
		path = nil
		segs, joined = 0, false
	}
	paint := func(stroke bool) {
		if path != nil {
			box := *path
			if stroke {
				pad := strokeReach(cur.lw, cur.lj, cur.lc, cur.ml, cur.m, joined)
				box = [4]float64{box[0] - pad, box[1] - pad, box[2] + pad, box[3] + pad}
			}
			mark(box)
		}
		endPath()
	}
	// A Bezier segment through an affine map is the segment through the
	// mapped control points, so its bounds are taken after the map.
	curve := func(p [4][2]float64) {
		for i := range p {
			p[i] = transform(p[i][0], p[i][1])
		}
		bezierBounds(p, addPath)
	}
	// shown holds the clip of each show operator in the font a Tf set, for
	// the text pass after the walk; textOK whether every font the
	// description selects is one that pass can resolve from the cache.
	shown := make(map[int]*[4]float64)
	textOK := true
	var cx, cy float64
	for i, op := range ops {
		// A cancelled walk bounds nothing it has not seen, so the glyph
		// paints anywhere.
		if i%100 == 0 && i > 0 && ctx.Err() != nil { //nolint:mnd // amortized cancellation check
			return painted, true
		}
		switch op.Operator {
		case "q":
			stack = append(stack, cur)
		case "Q":
			if n := len(stack); n > 0 {
				cur, stack = stack[n-1], stack[:n-1]
			}
		case "cm":
			if len(op.Operands) >= 6 {
				var a [6]float64
				for k := range 6 {
					a[k] = document.Float(op.Operands[k])
				}
				cur.m = concatMatrix(a, cur.m)
			}
		case "w":
			if len(op.Operands) >= 1 {
				cur.lw = document.Float(op.Operands[0])
			}
		case "J":
			if len(op.Operands) >= 1 {
				cur.lc = int(document.Float(op.Operands[0]))
			}
		case "j":
			if len(op.Operands) >= 1 {
				cur.lj = int(document.Float(op.Operands[0]))
			}
		case "M":
			if len(op.Operands) >= 1 {
				cur.ml = document.Float(op.Operands[0])
			}
		case "W", "W*":
			clipping = true
		case "f", "F", "f*":
			paint(false)
		case "S", "B", "B*":
			paint(true)
		case "s", "b", "b*":
			closePath()
			paint(true)
		case "h":
			closePath()
		case "n":
			endPath()
		case "BI":
			markCorners(rectCorners(0, 0, 1, 1))
		case "Do":
			if len(op.Operands) < 1 {
				continue
			}
			if corners, ok := p.xobjectBox(ctx, op.Operands[0]); ok {
				markCorners(corners)
			} else {
				anywhere()
			}
		case "sh":
			if len(op.Operands) < 1 {
				continue
			}
			if corners, ok := p.shadingBox(ctx, op.Operands[0]); ok {
				markCorners(corners)
			} else {
				anywhere()
			}
		case "Tf":
			cur.tf = tfUnknown
			if len(op.Operands) >= 1 && p.cachedFont(ctx, chain, op.Operands[0]) {
				cur.tf = tfKnown
			} else {
				textOK = false
			}
		case "gs":
			if len(op.Operands) < 1 {
				continue
			}
			cur.lw, cur.lc, cur.lj, cur.ml = p.lineParams(ctx, op.Operands[0], cur.lw, cur.lc, cur.lj, cur.ml)
			if p.setsFont(ctx, op.Operands[0]) {
				cur.tf = tfUnknown
				textOK = false
			}
		case "Tj", "TJ", "'", "\"":
			if !showsBytes(op) {
				continue
			}
			if cur.tf != tfKnown {
				textOK = false
			}
			shown[i] = cur.clip
		default:
			v, ok := pathOperands(op)
			if !ok {
				continue
			}
			switch op.Operator {
			case "m", "l":
				if op.Operator == "m" {
					segs = 0
				} else {
					segment()
				}
				cx, cy = v[0], v[1]
				q := transform(cx, cy)
				addPath(q[0], q[1])
			case "re":
				// A rectangle is a closed subpath of four segments (8.5.2.1).
				segs, joined = 0, true
				cx, cy = v[0], v[1]
				for _, p := range rectCorners(v[0], v[1], v[0]+v[2], v[1]+v[3]) {
					q := transform(p[0], p[1])
					addPath(q[0], q[1])
				}
			case "c":
				segment()
				curve([4][2]float64{{cx, cy}, {v[0], v[1]}, {v[2], v[3]}, {v[4], v[5]}})
				cx, cy = v[4], v[5]
			case "v":
				segment()
				curve([4][2]float64{{cx, cy}, {cx, cy}, {v[0], v[1]}, {v[2], v[3]}})
				cx, cy = v[2], v[3]
			case "y":
				segment()
				curve([4][2]float64{{cx, cy}, {v[0], v[1]}, {v[2], v[3]}, {v[2], v[3]}})
				cx, cy = v[2], v[3]
			}
		}
	}
	if len(shown) == 0 || unbounded {
		return painted, unbounded
	}
	var boxes map[int][][4]float64
	if textOK {
		boxes, textOK = type3TextBoxes(ctx, p, ops)
	}
	for i, clip := range shown {
		cur.clip = clip
		got, ok := boxes[i]
		if !textOK || !ok {
			// No glyph to bound: what the operator paints is anywhere its
			// clip lets it.
			anywhere()
			continue
		}
		for _, b := range got {
			if !finiteBox(b) {
				anywhere()
				continue
			}
			mark(b)
		}
	}
	return painted, unbounded
}

// type3TextBoxes runs the Type 3 glyph description ops through the text
// extractor in glyph space, and returns the ink box of each glyph it shows,
// by the index in ops of the operator that showed it, against the fonts p's
// resources name, all of them in the font cache ([type3Painter.cachedFont]).
// A glyph a form XObject shows is the form's, which the walk bounds by its
// box. It returns false when the extraction fails.
//
// The machine's clip starts unbounded, so a nested Type 3 glyph that paints
// past every box reports a box that is not finite ([clippedInkBox]).
func type3TextBoxes(ctx context.Context, p type3Painter, ops []content.Operation) (map[int][][4]float64, bool) {
	machine := graphics.NewMachine([4]float64{math.Inf(-1), math.Inf(-1), math.Inf(1), math.Inf(1)})
	machine.IgnoreOCVisibility = true
	var elements []models.TextElement
	var origins []int
	if err := extractTextRecursive(ctx, p.doc, machine, p.resources, ops, &elements, make(map[int]bool), 0, nil, &origins, nil, nil, nil); err != nil {
		return nil, false
	}
	boxes := make(map[int][][4]float64)
	for k, el := range elements {
		if k >= len(origins) || origins[k] < 0 {
			continue
		}
		boxes[origins[k]] = append(boxes[origins[k]], el.InkBox)
	}
	return boxes, true
}

// finiteBox reports whether every coordinate of box is a finite number.
func finiteBox(box [4]float64) bool {
	for _, v := range box {
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return false
		}
	}
	return true
}

// showsBytes reports whether op, a show operator, names any byte to show.
func showsBytes(op content.Operation) bool {
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
