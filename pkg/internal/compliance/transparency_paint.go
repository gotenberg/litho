package compliance

import (
	"context"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// paintTransparency answers whether a page contains transparency, the
// question ISO 19005-2/3, 6.2.10 t2 and ISO 19005-4, 6.2.9 t2 ask before they
// require a blending colour space, as veraPDF 1.30 answers it: at the
// operators that paint, under the graphics state in force there
// (OperatorFactory, TransparencyBehaviour and TransparencyGraphicsState,
// veraPDF-validation 1.30). A graphics state no painting operator paints under
// is no transparency, whatever it holds.
//
// A stream's state starts opaque, and q and Q save and restore it. A gs sets
// the soft mask and the blend mode of the state it names, absent ones
// included, which clears them, and the constant alphas it gives as numbers,
// leaving the others; a gs naming no graphics state changes nothing. At a
// painting operator the state is transparent when its soft mask is a
// dictionary or a stream, when its blend mode is transparency
// ([blendIsTransparency]), or when the alpha the operator paints with, /ca to
// fill and /CA to stroke, read as a single-precision number, is below 1:
//
//   - f, F and f* fill, S and s stroke, B, B*, b and b* do both, and each is
//     also transparent when the colour it paints with is a tiling pattern
//     whose cell paints under transparency.
//   - Tj, TJ, ' and " fill and stroke as the text rendering mode says
//     ([textModePaints]), with the same colour test, and are also
//     transparent when a Type 3 font shows a glyph whose description paints
//     under transparency. Modes 3 and 7 paint nothing and test nothing.
//   - sh fills. Do fills, and is also transparent when it paints a form with
//     a /Group whose /S is /Transparency or whose content paints under
//     transparency, or an image whose /SMask is a stream or whose
//     /SMaskInData is above 0. An inline image tests nothing.
//
// A form, a tiling cell and a glyph description start from an opaque state of
// their own, and inherit from the operator drawing them the colour it paints
// with and whether their colour operators run (see [paintEntry]). Their names
// resolve against their own resources first and the page's then, as
// [drawnStreamWalk] resolves them. A page also contains the transparency of
// its annotations (see [paintTransparency.annotationsTransparent]).
//
// Measured with veraPDF 1.30 at PDF/A-2b and PDF/A-4 on pages with no group
// and no output intent, each failing t2 exactly when it is listed as
// transparent. These pass: /ca 0.5 undone by Q before a fill, before a stroke
// alone, before only a clip, before an inline image and before an invisible
// or clipping text show; /CA 0.5 before a fill and before a form that
// strokes; /ca 1.5, /ca 0.99999999 and /ca written as a name before a fill.
// These are transparent: /ca 0.5 before a fill, an sh, a form or an image Do;
// /ca 0, -0.5, 0.9999999 and an indirect 0.5 before a fill; /CA 0.5 before a
// stroke or a stroking text show; a soft mask, or /BM /Multiply, /Compatible,
// [/Multiply], [/Normal /Multiply] or 5, before a fill, and a soft mask or a
// blend mode before an sh. So are a fill in a tiling pattern whose cell fills
// at /ca 0.5, a stroke in one, and an opaque form painted while the fill
// colour is one; a form painting a soft masked image, and a form with a
// transparency group that paints nothing; and a Type 3 glyph filling at /ca
// 0.5 shown in fill or stroke mode, past d1 or not, or painting a form with a
// transparency group. A gs with no /BM or /SMask after one with them, a /BM
// /Normal after /Multiply and an /SMask /None after a soft mask clear them,
// and a gs naming no graphics state keeps /ca 0.5. A pattern only selected, a
// shading pattern, an image whose /SMask is /None or a dictionary, a form
// setting /ca 0.5 and painting nothing, a glyph shown at code 200, which
// veraPDF reads as a negative byte and looks up no glyph for, and an
// annotation appearance setting /ca 0.5 and painting nothing pass; an
// appearance filling at /ca 0.5 or carrying a transparency group fails.
//
// A page's content is walked once for the page, and every other stream once
// for each state it is entered with (a handful of flags), its answer reused
// wherever it is drawn again: for every page when none of its names fell back
// to the page's resources, and for every page sharing those resources when
// one did. A walk stops at the first transparent operator. So a stream costs
// one walk however many Do operators, glyphs and annotations reach it, and one
// for each page resource dictionary only when it borrows a name from them. A
// stream that draws itself, directly or through others, reads as painting
// nothing where it is entered again, as veraPDF reads it
// (GFPDContentStream.parseOperators); a stream first entered inside such a
// cycle keeps that answer when another stream draws it again, where veraPDF
// would walk the cycle anew from there.
//
// # Corpus evidence
//
//   - All corpora: reading transparency at the painting operators moves no
//     count of ISO 19005-2, 6.2.10 t2 at PDF/A-2b or of ISO 19005-4, 6.2.9 t2
//     at PDF/A-4, and no other count at PDF/A-1b, PDF/A-2b or PDF/A-4.
//   - litho paintop_2b.pdf: seven pages each set a transparent graphics state
//     no operator paints under, which litho failed and veraPDF passes.
type paintTransparency struct {
	doc *document.Document
	// glyphResources reports that a glyph description with a /Resources of
	// its own reads it rather than its font's (see [drawnStreamWalk]).
	glyphResources bool
	memo           map[paintKey]paintResult
	// open holds the streams being walked, which a stream drawing one of
	// them enters again.
	open map[string]bool
}

// paintKey identifies one walk of a stream: the stream, the page resources
// its names fell back to, or none when no name did, and the state it was
// entered with.
type paintKey struct {
	stream, scope string
	entry         paintEntry
}

// paintResult is the answer of one walk: whether the stream paints under
// transparency, and whether a name it or a stream it draws uses resolved
// through the page's resources, which makes the answer the page's alone.
type paintResult struct {
	transparent, usedPage bool
}

// paintEntry is what a form, a tiling cell or a glyph description inherits
// from the operator drawing it: the fill and stroke colour it paints with,
// the text rendering mode, and whether its colour operators run (see
// [drawnScan]).
type paintEntry struct {
	fill, stroke paintInherited
	textRender   int
	colourOff    bool
}

// paintInherited is a colour a stream inherits: whether its space is a
// Pattern space, and whether it is a tiling pattern whose cell paints under
// transparency.
type paintInherited struct {
	space, transparent bool
}

// paintColour is the colour a painting operator paints with, for the one
// question veraPDF asks of it: whether it is a tiling pattern whose cell
// paints under transparency. space reports that the colour space is a Pattern
// space, so an scn naming a pattern selects it (ISO 32000-1, 8.7.3.1).
type paintColour struct {
	space, transparent bool
	// pattern is the /Resources/Pattern entry an scn or SCN selected, whose
	// cell is walked when an operator first paints with it, or nil once it
	// has been, and for a colour that is no pattern of the stream's own.
	pattern ast.PdfObject
}

// newPaintTransparency returns a walk over the pages of vctx.
func newPaintTransparency(vctx *validationCtx) *paintTransparency {
	return &paintTransparency{
		doc:            vctx.doc,
		glyphResources: PDFAPart(vctx.profile) != 4 && PDFUAPart(vctx.profile) != 2,
		memo:           make(map[paintKey]paintResult),
		open:           make(map[string]bool),
	}
}

// pageTransparent reports whether the content of page p paints under
// transparency.
func (w *paintTransparency) pageTransparent(ctx context.Context, p *pageCtx) bool {
	scope := &paintScope{own: p.resources, page: p.resources}
	return w.streamOps(ctx, p.parsedOps(ctx), scope, paintEntry{}, 0).transparent
}

// annotationsTransparent reports whether an annotation of page p paints
// under transparency, under the PDF/A part part: veraPDF raises the page's
// containsTransparency for an annotation whose normal, rollover or down
// appearance is a form that does (GFPDAnnot.addAppearance), each appearance
// starting from the initial graphics state with the page's resources behind
// its own, and under ISO 32000-2, whose annotations have a blend mode of
// their own (12.5.2), for one naming a blend mode other than /Normal
// (GFPDAnnot.parseBM).
//
// Measured with veraPDF 1.30 on pages whose one annotation names a blend mode
// over an opaque appearance, with no output intent: /BM /Multiply fails ISO
// 19005-4, 6.2.9 t2 and passes ISO 19005-2, 6.2.10 t2, and /BM [/Multiply]
// passes both. An appearance filling at /ca 0.5 or carrying a transparency
// group fails both, and one setting /ca 0.5 and painting nothing passes them.
//
// # Corpus evidence
//
//   - pdfjs annotation-as.pdf: its appearances carry image /SMask entries,
//     and it fails ISO 19005-2 §6.2.10 t2 in veraPDF.
//   - pdfjs stamps.pdf: its appearances carry a /Group /S /Transparency, and
//     it fails ISO 19005-2 §6.2.10 t2 in veraPDF.
//   - verapdf PDF_A-4__veraPDF_test_suite_6-2-9-t02-fail-c.pdf: its one
//     annotation names /BM /Multiply over an opaque appearance, and veraPDF
//     fails 6.2.10 t2 nowhere at PDF/A-2b, where litho failed it once.
func (w *paintTransparency) annotationsTransparent(ctx context.Context, p *pageCtx, part int) bool {
	annots, err := document.ResolveArray(ctx, w.doc, p.dict["Annots"])
	if err != nil {
		return false
	}
	for _, annotObj := range annots {
		if ctx.Err() != nil {
			return false
		}
		annot, err := document.ResolveDict(ctx, w.doc, annotObj)
		if err != nil || annot == nil {
			continue
		}
		if part >= 4 {
			if bm, ok := resolveNameObject(ctx, w.doc, annot["BM"]); ok && bm != "Normal" {
				return true
			}
		}
		apDict, err := document.ResolveDict(ctx, w.doc, annot["AP"])
		if err != nil || apDict == nil {
			continue
		}
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			apObj, ok := apDict[key]
			if !ok {
				continue
			}
			for _, streamObj := range appearanceStreamObjects(ctx, w.doc, apObj) {
				if w.formTransparent(ctx, streamObj, p.resources, paintEntry{}, 1).transparent {
					return true
				}
			}
		}
	}
	return false
}

// formTransparent reports whether the form obj holds, drawn at the given
// depth on the page whose resources are pageRes and entered with entry, is
// transparency: a /Group whose /S is /Transparency, which veraPDF reads
// before it walks the form's content (GFPDXForm.containsTransparency), or
// content that paints under transparency.
func (w *paintTransparency) formTransparent(ctx context.Context, obj ast.PdfObject, pageRes ast.PdfDict, entry paintEntry, depth int) paintResult {
	stream, err := document.ResolveStream(ctx, w.doc, obj)
	if err != nil || stream.Dict == nil {
		return paintResult{}
	}
	if group, err := document.ResolveDict(ctx, w.doc, stream.Dict["Group"]); err == nil && group != nil {
		if s, _ := document.ResolveName(ctx, w.doc, group["S"]); s == "Transparency" {
			return paintResult{transparent: true}
		}
	}
	res, _ := document.ResolveDict(ctx, w.doc, stream.Dict["Resources"]) // Best-effort: a /Resources that is no dictionary defines no name.
	return w.streamTransparent(ctx, obj, stream, &paintScope{own: res, page: pageRes, fallback: true}, entry, depth)
}

// streamTransparent reports whether the content stream obj holds, which
// stream resolves to, paints under transparency when entered with entry, its
// names resolving in scope. It walks each stream once for every entry and
// scope its answer depends on (see [paintTransparency]).
func (w *paintTransparency) streamTransparent(ctx context.Context, obj ast.PdfObject, stream ast.PdfStream, scope *paintScope, entry paintEntry, depth int) paintResult {
	if formTooDeep(w.doc, depth) || ctx.Err() != nil {
		return paintResult{}
	}
	id := paintStreamID(obj, stream)
	anyPage := paintKey{stream: id, entry: entry}
	thisPage := paintKey{stream: id, scope: formatDictAddr(scope.page), entry: entry}
	if r, ok := w.memo[anyPage]; ok {
		return r
	}
	if r, ok := w.memo[thisPage]; ok {
		return r
	}
	if w.open[id] {
		return paintResult{}
	}
	var ops []content.Operation
	if data, reads := decodeContentStream(ctx, w.doc, stream); reads {
		var err error
		if ops, err = content.ParseStreamData(ctx, data); err != nil {
			w.doc.Logger.Debug("compliance.transparency: cannot parse drawn stream", "error", err)
		}
	}
	w.open[id] = true
	r := w.streamOps(ctx, ops, scope, entry, depth)
	delete(w.open, id)
	if ctx.Err() != nil {
		return r
	}
	if r.usedPage {
		w.memo[thisPage] = r
	} else {
		w.memo[anyPage] = r
	}
	return r
}

// paintStreamID names the stream obj holds for [paintKey]: its object number,
// or the identity of its dictionary for a stream written directly, which ISO
// 32000-1, 7.3.8.1 does not allow.
func paintStreamID(obj ast.PdfObject, stream ast.PdfStream) string {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return strconv.Itoa(ref.ObjectNum)
	}
	return formatDictAddr(stream.Dict)
}

// paintAlpha is the part of the graphics state veraPDF's transparency test
// reads besides the colour (TransparencyGraphicsState): whether the soft mask
// is a dictionary or a stream, whether the blend mode is transparency, and
// the fill and stroke constant alphas, which veraPDF keeps as
// single-precision numbers.
type paintAlpha struct {
	softMask, blend        bool
	fillAlpha, strokeAlpha float32
}

// base reports whether the soft mask or the blend mode makes every painting
// operator transparent (TransparencyBehaviour.baseCheck).
func (a paintAlpha) base() bool {
	return a.softMask || a.blend
}

// transparencyState is the graphics state one walk tracks, which q and Q
// save and restore (ISO 32000-1, 8.4.2).
type transparencyState struct {
	alpha        paintAlpha
	fill, stroke paintColour
	textRender   int
	font         string
	colourOff    bool
}

// streamOps walks the operations ops of one stream, entered with entry, whose
// names resolve in scope, and stops at the first that paints under
// transparency.
func (w *paintTransparency) streamOps(ctx context.Context, ops []content.Operation, scope *paintScope, entry paintEntry, depth int) paintResult {
	st := transparencyState{
		alpha:      paintAlpha{fillAlpha: 1, strokeAlpha: 1},
		fill:       paintColour{space: entry.fill.space, transparent: entry.fill.transparent},
		stroke:     paintColour{space: entry.stroke.space, transparent: entry.stroke.transparent},
		textRender: entry.textRender,
		colourOff:  entry.colourOff,
	}
	var stack []transparencyState
	usedPage := false
	note := func(r paintResult) bool {
		usedPage = usedPage || r.usedPage
		return r.transparent
	}
	transparent := false
	for idx, op := range ops {
		if idx%100 == 0 && ctx.Err() != nil {
			break
		}
		switch op.Operator {
		case "q":
			stack = append(stack, st)
		case "Q":
			if n := len(stack); n > 0 {
				st = stack[n-1]
				stack = stack[:n-1]
			}
		case "gs":
			if gs := scope.lookup(ctx, w.doc, "ExtGState", lastNameOperand(op)); gs != nil {
				st.alpha = applyExtGStateAlpha(ctx, w.doc, gs, st.alpha)
			}
		case "d1":
			st.colourOff = true
		case "Tr":
			if len(op.Operands) >= 1 {
				if mode, ok := op.Operands[0].(ast.PdfInt); ok {
					st.textRender = int(mode)
				}
			}
		case "Tf":
			if len(op.Operands) >= 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					st.font = string(name)
				}
			}
		case "cs", "CS", "scn", "SCN", "g", "G", "rg", "RG", "k", "K":
			if !st.colourOff {
				w.setColour(ctx, op, scope, &st)
			}
		case "f", "F", "f*", "S", "s", "B", "B*", "b", "b*":
			fills, strokes := paintedSpaces(op.Operator)
			transparent = note(w.paints(ctx, &st, fills, strokes, scope, depth))
		case "sh":
			transparent = st.alpha.base() || st.alpha.fillAlpha < 1
		case "Do":
			transparent = note(w.paintsXObject(ctx, op, scope, &st, depth))
		case "Tj", "TJ", "'", `"`:
			transparent = note(w.showsText(ctx, op, scope, &st, depth))
		}
		if transparent {
			break
		}
	}
	return paintResult{transparent: transparent, usedPage: usedPage || scope.usedPage}
}

// applyExtGStateAlpha returns a once a gs sets the graphics state gsObj, as
// veraPDF merges one into its transparency state
// (TransparencyGraphicsState.copyPropertiesFormExtGState): the soft mask and
// the blend mode are replaced, an absent one included, and each constant
// alpha is replaced when the state gives it as a number. A graphics state
// that is no dictionary changes nothing.
func applyExtGStateAlpha(ctx context.Context, doc *document.Document, gsObj ast.PdfObject, a paintAlpha) paintAlpha {
	resolved, err := document.ResolveObject(ctx, doc, gsObj)
	if err != nil {
		return a
	}
	var gs ast.PdfDict
	switch v := resolved.(type) {
	case ast.PdfDict:
		gs = v
	case ast.PdfStream:
		gs = v.Dict
	default:
		return a
	}
	a.softMask = dictionaryBased(ctx, doc, gs["SMask"])
	a.blend = blendIsTransparency(ctx, doc, gs["BM"])
	if v, ok := resolvedNumber(ctx, doc, gs["ca"]); ok {
		a.fillAlpha = float32(v)
	}
	if v, ok := resolvedNumber(ctx, doc, gs["CA"]); ok {
		a.strokeAlpha = float32(v)
	}
	return a
}

// dictionaryBased reports whether obj resolves to a dictionary or a stream.
func dictionaryBased(ctx context.Context, doc *document.Document, obj ast.PdfObject) bool {
	if obj == nil {
		return false
	}
	switch v, _ := document.ResolveObject(ctx, doc, obj); v.(type) {
	case ast.PdfDict, ast.PdfStream:
		return true
	}
	return false
}

// resolvedNumber returns the value obj resolves to when it is a number.
func resolvedNumber(ctx context.Context, doc *document.Document, obj ast.PdfObject) (float64, bool) {
	if obj == nil {
		return 0, false
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return 0, false
	}
	return document.Number(resolved)
}

// blendIsTransparency reports whether the /BM value obj makes a painting
// operator transparent, as veraPDF reads it (TransparencyBehaviour.baseCheck):
// a name other than /Normal, /Compatible included, an array unless it holds
// exactly one element that is no name or is /Normal, and any other value but
// null.
//
// ISO 32000-1, 11.3.5 makes /Compatible behave as /Normal, and 11.6.3 has a
// viewer use the first blend mode of an array it recognises, so the reading
// is veraPDF's rather than the standard's. It decides only whether a page
// needs a blending colour space; which values /BM may take is the blend mode
// rule's concern (see [cosBlendMode]).
//
// # Corpus evidence
//
//   - govdocs thread_300_300345.pdf: an ExtGState naming /Compatible, which
//     veraPDF reports under 6.2.10 t2 on a page with no group.
func blendIsTransparency(ctx context.Context, doc *document.Document, obj ast.PdfObject) bool {
	if obj == nil {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return false
	}
	switch v := resolved.(type) {
	case nil, ast.PdfNull:
		return false
	case ast.PdfName:
		return v != "Normal"
	case ast.PdfArray:
		if len(v) != 1 {
			return true
		}
		name, ok := resolveNameObject(ctx, doc, v[0])
		return ok && name != "Normal"
	}
	return true
}

// setColour applies the colour operator op, whose names resolve in scope, to
// the fill or stroke colour of st.
func (w *paintTransparency) setColour(ctx context.Context, op content.Operation, scope *paintScope, st *transparencyState) {
	switch op.Operator {
	case "cs":
		st.fill = paintColour{space: w.patternSpace(ctx, scope, lastNameOperand(op))}
	case "CS":
		st.stroke = paintColour{space: w.patternSpace(ctx, scope, lastNameOperand(op))}
	case "scn":
		if st.fill.space {
			st.fill = paintColour{space: true, pattern: scope.lookup(ctx, w.doc, "Pattern", lastNameOperand(op))}
		}
	case "SCN":
		if st.stroke.space {
			st.stroke = paintColour{space: true, pattern: scope.lookup(ctx, w.doc, "Pattern", lastNameOperand(op))}
		}
	case "g", "rg", "k":
		st.fill = paintColour{}
	case "G", "RG", "K":
		st.stroke = paintColour{}
	}
}

// patternSpace reports whether the colour space a cs or CS names, resolved
// in scope, is a Pattern space.
func (w *paintTransparency) patternSpace(ctx context.Context, scope *paintScope, name string) bool {
	if name == "Pattern" {
		return true
	}
	entry := scope.lookup(ctx, w.doc, "ColorSpace", name)
	if entry == nil {
		return false
	}
	own, page := scope.category(ctx, w.doc, "ColorSpace")
	colorSpaces := own
	if _, ok := own[ast.PdfName(name)]; !ok {
		colorSpaces = page
	}
	return resolveCSName(ctx, w.doc, resolveColorSpaceName(ctx, w.doc, entry, colorSpaces)) == "Pattern"
}

// paints tests a painting operator that fills, strokes or both under st,
// whose tiling patterns are walked on the page of scope at depth.
func (w *paintTransparency) paints(ctx context.Context, st *transparencyState, fills, strokes bool, scope *paintScope, depth int) paintResult {
	if st.alpha.base() || (fills && st.alpha.fillAlpha < 1) || (strokes && st.alpha.strokeAlpha < 1) {
		return paintResult{transparent: true}
	}
	var r paintResult
	if fills {
		r = w.colourTransparent(ctx, &st.fill, scope, depth)
		if r.transparent {
			return r
		}
	}
	if strokes {
		s := w.colourTransparent(ctx, &st.stroke, scope, depth)
		s.usedPage = s.usedPage || r.usedPage
		return s
	}
	return r
}

// colourTransparent reports whether c is a tiling pattern whose cell paints
// under transparency (GFPDTilingPattern.isContainsTransparency), walking the
// cell the first time an operator paints with it. A cell starts from the
// initial graphics state, with its colour operators off when the pattern is
// uncoloured (ISO 32000-1, 8.7.3.3), and its names resolve against its own
// resources and then those of the page of scope.
func (w *paintTransparency) colourTransparent(ctx context.Context, c *paintColour, scope *paintScope, depth int) paintResult {
	if c.pattern == nil {
		return paintResult{transparent: c.transparent}
	}
	obj := c.pattern
	c.pattern = nil
	c.transparent = false
	tiling, uncoloured := tilingPatternKind(ctx, w.doc, obj)
	if !tiling {
		return paintResult{}
	}
	stream, err := document.ResolveStream(ctx, w.doc, obj)
	if err != nil || stream.Dict == nil {
		return paintResult{}
	}
	res, _ := document.ResolveDict(ctx, w.doc, stream.Dict["Resources"]) // Best-effort: a /Resources that is no dictionary defines no name.
	r := w.streamTransparent(ctx, obj, stream, &paintScope{own: res, page: scope.page, fallback: true}, paintEntry{colourOff: uncoloured}, depth+1)
	c.transparent = r.transparent
	return r
}

// entryOf returns the state a stream drawn under st starts from, resolving
// whether its fill and stroke colours are transparent tiling patterns, and
// whether resolving them read the page's resources.
func (w *paintTransparency) entryOf(ctx context.Context, st *transparencyState, scope *paintScope, depth int) (paintEntry, bool) {
	f := w.colourTransparent(ctx, &st.fill, scope, depth)
	s := w.colourTransparent(ctx, &st.stroke, scope, depth)
	return paintEntry{
		fill:       paintInherited{space: st.fill.space, transparent: f.transparent},
		stroke:     paintInherited{space: st.stroke.space, transparent: s.transparent},
		textRender: st.textRender,
		colourOff:  st.colourOff,
	}, f.usedPage || s.usedPage
}

// paintsXObject tests the Do operator op under st: it fills, and paints the
// XObject its name resolves to in scope.
func (w *paintTransparency) paintsXObject(ctx context.Context, op content.Operation, scope *paintScope, st *transparencyState, depth int) paintResult {
	if st.alpha.base() || st.alpha.fillAlpha < 1 {
		return paintResult{transparent: true}
	}
	obj := scope.lookup(ctx, w.doc, "XObject", lastNameOperand(op))
	if obj == nil {
		return paintResult{}
	}
	stream, err := document.ResolveStream(ctx, w.doc, obj)
	if err != nil || stream.Dict == nil {
		return paintResult{}
	}
	switch subtype, _ := document.ResolveName(ctx, w.doc, stream.Dict["Subtype"]); subtype {
	case "Form":
		entry, usedPage := w.entryOf(ctx, st, scope, depth)
		r := w.formTransparent(ctx, obj, scope.page, entry, depth+1)
		r.usedPage = r.usedPage || usedPage
		return r
	case "Image":
		return paintResult{transparent: imageTransparent(ctx, w.doc, stream.Dict)}
	}
	return paintResult{}
}

// imageTransparent reports whether the image whose dictionary is dict is
// transparency as veraPDF reads it (GFPDXImage.containsTransparency): an
// /SMask that is a stream, or an /SMaskInData above 0. ISO 32000-1, 8.9.5,
// Table 89 lets /SMask name a stream or /None, and gives /SMaskInData to
// JPXDecode images alone, where veraPDF reads it on any image. Measured with
// veraPDF 1.30 at PDF/A-2b and PDF/A-4: an image whose /SMask is /None or a
// dictionary passes 6.2.10 t2 and 6.2.9 t2, and an image with no filter and
// /SMaskInData 1 fails them.
//
// # Corpus evidence
//
//   - pdfjs issue14814.pdf: draws one image with /SMask /None on a page
//     carrying no /Group under no OutputIntent, and veraPDF reports nothing
//     there.
func imageTransparent(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	smask, _ := document.ResolveObject(ctx, doc, dict["SMask"]) // Best-effort: an /SMask that does not resolve is no stream.
	if _, stream := smask.(ast.PdfStream); stream {
		return true
	}
	v, ok := resolvedNumber(ctx, doc, dict["SMaskInData"])
	return ok && int64(v) > 0
}

// maxShownGlyphCode is the highest character code whose Type 3 glyph
// description a text show is tested through: veraPDF reads each code as a
// Java byte, which is negative from 128 up, and looks up no glyph for a
// negative code (TransparencyBehaviour.fontCheck and Encoding.getName).
const maxShownGlyphCode = 0x7F

// showsText tests the text-showing operator op under st: it paints as the
// text rendering mode says, and paints the glyph descriptions of a Type 3
// font for the codes it shows.
func (w *paintTransparency) showsText(ctx context.Context, op content.Operation, scope *paintScope, st *transparencyState, depth int) paintResult {
	fills, strokes := textModePaints(st.textRender)
	if !fills && !strokes {
		return paintResult{}
	}
	r := w.paints(ctx, st, fills, strokes, scope, depth)
	if r.transparent {
		return r
	}
	font := scope.type3Font(ctx, w.doc, st.font)
	if font == nil {
		return r
	}
	entry, usedPage := w.entryOf(ctx, st, scope, depth)
	r.usedPage = r.usedPage || usedPage
	for _, s := range shownStrings(op) {
		for i := range len(s) {
			if s[i] > maxShownGlyphCode {
				continue
			}
			g := w.glyphTransparent(ctx, font, font.encoding[s[i]], scope.page, entry, depth+1)
			r.usedPage = r.usedPage || g.usedPage
			if g.transparent {
				r.transparent = true
				return r
			}
		}
	}
	return r
}

// shownStrings returns the strings the text-showing operator op shows.
func shownStrings(op content.Operation) []ast.PdfString {
	var out []ast.PdfString
	for _, operand := range op.Operands {
		switch v := operand.(type) {
		case ast.PdfString:
			out = append(out, v)
		case ast.PdfArray:
			for _, el := range v {
				if s, ok := el.(ast.PdfString); ok {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// glyphTransparent reports whether the glyph description of font named name
// paints under transparency, entered with entry on the page whose resources
// are pageRes. Its names resolve against its font's resources, or its own
// under a profile built on ISO 32000-1 (see [drawnStreamWalk]), and then the
// page's.
func (w *paintTransparency) glyphTransparent(ctx context.Context, font *paintFont, name string, pageRes ast.PdfDict, entry paintEntry, depth int) paintResult {
	if name == "" {
		return paintResult{}
	}
	obj, ok := font.charProcs[ast.PdfName(name)]
	if !ok {
		return paintResult{}
	}
	stream, err := document.ResolveStream(ctx, w.doc, obj)
	if err != nil || stream.Dict == nil {
		return paintResult{}
	}
	owner := font.dict
	if w.glyphResources && knownKey(stream.Dict, "Resources") {
		owner = stream.Dict
	}
	res, _ := document.ResolveDict(ctx, w.doc, owner["Resources"]) // Best-effort: a /Resources that is no dictionary defines no name.
	return w.streamTransparent(ctx, obj, stream, &paintScope{own: res, page: pageRes, fallback: true}, entry, depth)
}

// paintFont is a Type 3 font a text show paints glyph descriptions of.
type paintFont struct {
	dict      ast.PdfDict
	charProcs ast.PdfDict
	encoding  map[byte]string
}

// paintScope resolves the resource names of one stream: against its own
// resources, then, for a stream a page draws, against the page's, which page
// holds. usedPage records that a name reached the page's.
type paintScope struct {
	own, page ast.PdfDict
	fallback  bool
	usedPage  bool
	resolved  map[ast.PdfName][2]ast.PdfDict
	fonts     map[string]*paintFont
}

// category returns the category dictionaries of the stream's own resources
// and of the page's.
func (s *paintScope) category(ctx context.Context, doc *document.Document, category ast.PdfName) (own, page ast.PdfDict) {
	if d, ok := s.resolved[category]; ok {
		return d[0], d[1]
	}
	own, _ = document.ResolveDict(ctx, doc, s.own[category]) // Best-effort: an unreadable category defines no name.
	if s.fallback {
		page, _ = document.ResolveDict(ctx, doc, s.page[category]) // Best-effort: as above.
	}
	if s.resolved == nil {
		s.resolved = make(map[ast.PdfName][2]ast.PdfDict)
	}
	s.resolved[category] = [2]ast.PdfDict{own, page}
	return own, page
}

// lookup returns the entry name names in category, or nil when neither
// resource dictionary defines it.
func (s *paintScope) lookup(ctx context.Context, doc *document.Document, category ast.PdfName, name string) ast.PdfObject {
	if name == "" {
		return nil
	}
	own, page := s.category(ctx, doc, category)
	if v, ok := own[ast.PdfName(name)]; ok {
		return v
	}
	if !s.fallback {
		return nil
	}
	s.usedPage = true
	if v, ok := page[ast.PdfName(name)]; ok {
		return v
	}
	return nil
}

// type3Font returns the Type 3 font name names, or nil when it names none.
func (s *paintScope) type3Font(ctx context.Context, doc *document.Document, name string) *paintFont {
	if f, ok := s.fonts[name]; ok {
		return f
	}
	var font *paintFont
	if dict, err := document.ResolveDict(ctx, doc, s.lookup(ctx, doc, "Font", name)); err == nil && dict != nil {
		if subtype, _ := document.ResolveName(ctx, doc, dict["Subtype"]); subtype == "Type3" {
			charProcs, _ := document.ResolveDict(ctx, doc, dict["CharProcs"]) // Best-effort: unreadable glyph descriptions paint nothing.
			font = &paintFont{dict: dict, charProcs: charProcs, encoding: resolveSimpleFontEncoding(ctx, doc, dict)}
		}
	}
	if s.fonts == nil {
		s.fonts = make(map[string]*paintFont)
	}
	s.fonts[name] = font
	return font
}
