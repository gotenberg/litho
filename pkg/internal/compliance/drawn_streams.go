package compliance

import (
	"context"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// drawnStreamKind says how a document draws a content stream other than the
// content of a page.
type drawnStreamKind int

const (
	// drawnForm is a form XObject a Do operator paints (ISO 32000-1, 8.10).
	drawnForm drawnStreamKind = iota
	// drawnPattern is the cell of a tiling pattern an scn or SCN operator
	// selects (ISO 32000-1, 8.7.3.1).
	drawnPattern
	// drawnGlyph is a glyph description of a Type3 font a text-showing
	// operator shows text with (ISO 32000-1, 9.6.5).
	drawnGlyph
	// drawnAppearance is an annotation appearance stream (ISO 32000-1,
	// 12.5.5).
	drawnAppearance
)

// drawnStream is one content stream [drawnStreamWalk] reaches, parsed once for
// every rule that judges it.
type drawnStream struct {
	kind drawnStreamKind
	// objNum is the object number of the stream, or 0 for a stream written
	// directly rather than referenced.
	objNum int
	// resources is the stream's own resource dictionary, or nil when it has
	// none. A glyph description reads the /Resources of its font.
	resources ast.PdfDict
	// pageRes is the resource dictionary of the page the walk reached the
	// stream from. It resolves every name the stream's own resources leave
	// undefined.
	pageRes ast.PdfDict
	pageNum int
	ops     []content.Operation
	// patterns names the patterns its scn and SCN operators select, resource
	// names the stream uses as much as its other operators' operands (see
	// [checkContentStreamResourcesOps]).
	patterns map[string]bool
	// overflow counts the integer literals past the 64-bit range the parse
	// read as reals (see [checkNumberLimits]).
	overflow map[float64]int
	// format holds the format violations the parse recorded.
	format []content.FormatViolation
}

// drawnStreamWalk reaches every content stream a document draws besides the
// content of its pages: the form XObjects a Do paints, the cells of the tiling
// patterns an scn or SCN selects, every glyph description of the Type3 fonts
// text is shown with, and the annotation appearance streams, together with
// every stream those draw in turn. The content-stream rules judge each one
// from a single parse: the hex string tests, ISO 19005-1/2/3, 6.1.6 t1 and t2
// (see [drawnStreamHexStrings]), the undefined-operator rule, ISO 19005-1,
// 6.2.10 t1 and ISO 19005-2/3/4, 6.2.2 t1, and the resource-name tests beside
// it, in [checkDrawnStream], and the operand limits in
// [checkContentStreamLimits].
//
// It follows veraPDF 1.30, measured at PDF/A-1b and PDF/A-2b on one-page and
// two-page fixtures:
//
//   - A stream is judged once for the whole document, however many streams or
//     pages draw it and under however many names: a form two pages paint, a
//     pattern two pages select and an appearance two pages' annotations share
//     each fail the undefined-operator test once. The first stream to draw it
//     sets the context it is judged in, and the streams it draws are reached
//     from there only: a form two pages paint, whose Do names a form each page
//     maps differently, reaches the form of the first page alone.
//   - A stream that also serves as the content of a page is that page's, and
//     is not judged again as a form. A page whose content an earlier page
//     shares draws nothing the walk reaches through that content, since
//     veraPDF walks it from the earlier page alone; see [markJudgedContents].
//   - A stream is reached only through the operator that draws it. A form no
//     Do paints, a pattern no scn or SCN selects, a Type3 font no text is
//     shown with, and the forms such a stream would paint, are never judged,
//     nor is anything the resources of a page with no content declare. So is
//     a form serving as a soft mask, which a gs selects without drawing.
//   - A name resolves against the drawing stream's own resources first and
//     then against the page's, never against the resources of the stream that
//     drew it: a form painted by a form whose resources alone define it is
//     never reached. ISO 32000-1, 7.8.3 likewise has a form XObject without
//     resources of its own use the page's. A glyph description reads the
//     resources of its font.
//   - A form starts from the colour-space selection in force at its Do (ISO
//     32000-1, 8.10.2), so a bare scn in it selects a pattern when the drawing
//     stream selected the Pattern space.
//
// One divergence remains. litho judges a glyph description on the names it
// uses itself, while veraPDF gives the glyph descriptions of one Type3 font a
// single set of inherited names: measured with veraPDF 1.30 at PDF/A-2b, a
// glyph naming only a font no dictionary defines fails ISO 19005-2, 6.2.2 t2
// once another glyph of its font, judged before it, names a font the page
// defines. veraPDF judges the glyphs in the order of a Java hash map, which
// litho does not model.
//
// A cycle of forms painting each other is broken by the judged set, since a
// stream already judged is not entered again. The chain is capped at the form
// budget, [formTooDeep], an anti-DoS guard: a stream a page's content draws is
// at depth 1, an appearance stream counts as a form the page paints, and a
// tiling pattern and a Type3 glyph description count as a level each, as a
// form does.
//
// A walk runs on one goroutine, so it takes no lock: each check group that
// needs one runs its own, and each judges every stream once.
//
// # Corpus evidence
//
//   - pdfjs issue6541.pdf: paints a chain of three forms, the innermost
//     selecting a pattern, and veraPDF states the inherited-resource test, ISO
//     19005-2, 6.2.2 t2, in all four streams, where litho reached the
//     outermost alone.
//   - pdfjs pr4922.pdf: two pages show text with the same Type3 fonts, and
//     veraPDF states 6.2.2 t2 for their glyph descriptions 20 times, where
//     litho judged each glyph once per page, 39 times. litho now states it 19
//     times: glyph /three names only /R98, which the first page does not
//     define, and fails in veraPDF through the font's shared set alone.
//   - pdfjs poppler-90-0-fuzzed.pdf: its pages share damaged Type3 fonts, and
//     litho stated the undefined-operator rule 1874 times in their glyph
//     descriptions, where veraPDF states it in none of them.
type drawnStreamWalk struct {
	doc *document.Document
	// judged holds the object numbers of the streams already judged, and of
	// every page's content stream. A stream written directly rather than
	// referenced can be reached only once.
	judged map[int]bool
	judge  func(*drawnStream)
}

// newDrawnStreamWalk returns a walk over the pages of vctx that hands every
// stream it reaches to judge.
func newDrawnStreamWalk(vctx *validationCtx, judge func(*drawnStream)) *drawnStreamWalk {
	w := &drawnStreamWalk{doc: vctx.doc, judged: make(map[int]bool), judge: judge}
	for i := range vctx.pages {
		if ref, ok := vctx.pages[i].dict["Contents"].(ast.PdfIndirectRef); ok {
			w.judged[ref.ObjectNum] = true
		}
	}
	return w
}

// walkPage reaches the streams page p, the pageNum-th, draws: those its
// content draws, then its annotation appearance streams. A content stream an
// earlier page shares was walked from that page alone; see
// [markJudgedContents].
func (w *drawnStreamWalk) walkPage(ctx context.Context, p *pageCtx, pageNum int) {
	if !p.contentJudged {
		w.descend(ctx, w.scan(ctx, p.parsedOps(ctx), p.resources, inheritedCSDefault()), p.resources, pageNum, 1)
	}

	annots, err := document.ResolveArray(ctx, w.doc, p.dict["Annots"])
	if err != nil {
		return
	}
	for _, annotObj := range annots {
		if ctx.Err() != nil {
			return
		}
		annot, _ := document.ResolveDict(ctx, w.doc, annotObj)
		if annot == nil {
			continue
		}
		apDict, _ := document.ResolveDict(ctx, w.doc, annot["AP"])
		if apDict == nil {
			continue
		}
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			apObj, ok := apDict[key]
			if !ok {
				continue
			}
			for _, streamObj := range appearanceStreamObjects(ctx, w.doc, apObj) {
				w.visit(ctx, streamObj, nil, drawnAppearance, p.resources, inheritedCSDefault(), pageNum, 1)
			}
		}
	}
}

// drawnScan is what one content stream draws, read from its operations: the
// forms its Do operators paint, the patterns its scn and SCN operators select
// and the colour-space selection in force at each Do.
type drawnScan struct {
	ops []content.Operation
	// scope resolves the names its operators use.
	scope     ast.PdfDict
	painted   map[string]bool
	selected  *usedResources
	doEntries map[string]inheritedCS
}

// scan reads what the stream whose operations are ops draws. scope resolves
// the names they use, and entry is the colour-space selection the stream
// starts from.
func (w *drawnStreamWalk) scan(ctx context.Context, ops []content.Operation, scope ast.PdfDict, entry inheritedCS) drawnScan {
	if len(ops) == 0 {
		return drawnScan{}
	}
	_, painted, _, selected, doEntries := scanContentStreamDeviceCSFrom(ctx, ops, collectPatternCSNames(ctx, w.doc, scope), entry)
	return drawnScan{ops: ops, scope: scope, painted: painted, selected: selected, doEntries: doEntries}
}

// patterns returns the names of the patterns the stream selects.
func (s drawnScan) patterns() map[string]bool {
	if s.selected == nil {
		return nil
	}
	return s.selected.patterns
}

// descend reaches the streams one content stream draws, which s holds, at
// depth levels below the page. pageRes is the page's resource dictionary.
func (w *drawnStreamWalk) descend(ctx context.Context, s drawnScan, pageRes ast.PdfDict, pageNum, depth int) {
	if len(s.ops) == 0 || ctx.Err() != nil {
		return
	}
	scope := s.scope

	xobjects, _ := document.ResolveDict(ctx, w.doc, scope["XObject"])
	for _, name := range slices.Sorted(maps.Keys(s.painted)) {
		obj, ok := xobjects[ast.PdfName(name)]
		if !ok || w.judgedAlready(obj) {
			continue
		}
		stream, err := document.ResolveStream(ctx, w.doc, obj)
		if err != nil {
			continue
		}
		if subtype, _ := document.ResolveName(ctx, w.doc, stream.Dict["Subtype"]); subtype != "Form" {
			continue
		}
		w.visit(ctx, obj, nil, drawnForm, pageRes, inheritedCSFor(s.doEntries, name), pageNum, depth)
	}

	patterns, _ := document.ResolveDict(ctx, w.doc, scope["Pattern"])
	for _, name := range slices.Sorted(maps.Keys(s.patterns())) {
		obj, ok := patterns[ast.PdfName(name)]
		if !ok || w.judgedAlready(obj) {
			continue
		}
		// A shading pattern is a dictionary with no content of its own.
		stream, err := document.ResolveStream(ctx, w.doc, obj)
		if err != nil {
			continue
		}
		if patternType, _ := document.ResolveInt(ctx, w.doc, stream.Dict["PatternType"]); patternType != 1 {
			continue
		}
		w.visit(ctx, obj, nil, drawnPattern, pageRes, inheritedCSDefault(), pageNum, depth)
	}

	// Every glyph of a Type3 font text is shown with is judged, the glyphs the
	// text never shows included: veraPDF reaches them all through the font of
	// the text-showing operator, and none of a font a Tf only selects.
	fonts, _ := document.ResolveDict(ctx, w.doc, scope["Font"])
	for _, name := range slices.Sorted(maps.Keys(collectFontUsage(s.ops))) {
		fontDict, err := document.ResolveDict(ctx, w.doc, fonts[ast.PdfName(name)])
		if err != nil || fontDict == nil {
			continue
		}
		if subtype, _ := document.ResolveName(ctx, w.doc, fontDict["Subtype"]); subtype != "Type3" {
			continue
		}
		charProcs, err := document.ResolveDict(ctx, w.doc, fontDict["CharProcs"])
		if err != nil || charProcs == nil {
			continue
		}
		for _, glyph := range sortedNames(charProcs) {
			w.visit(ctx, charProcs[glyph], fontDict, drawnGlyph, pageRes, inheritedCSDefault(), pageNum, depth)
		}
	}
}

// judgedAlready reports whether the stream obj references was judged.
func (w *drawnStreamWalk) judgedAlready(obj ast.PdfObject) bool {
	ref, ok := obj.(ast.PdfIndirectRef)
	return ok && w.judged[ref.ObjectNum]
}

// visit judges the stream obj holds, of the given kind, unless it was judged
// already, then reaches the streams it draws. owner is the dictionary whose
// /Resources the stream reads, or nil for the stream's own dictionary.
func (w *drawnStreamWalk) visit(ctx context.Context, obj ast.PdfObject, owner ast.PdfDict, kind drawnStreamKind, pageRes ast.PdfDict, entry inheritedCS, pageNum, depth int) {
	if formTooDeep(w.doc, depth) || ctx.Err() != nil {
		return
	}
	objNum := 0
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if w.judged[ref.ObjectNum] {
			return
		}
		w.judged[ref.ObjectNum] = true
		objNum = ref.ObjectNum
	}
	stream, err := document.ResolveStream(ctx, w.doc, obj)
	if err != nil {
		w.doc.Logger.Debug("compliance.graphics: cannot resolve drawn stream", "error", err)
		return
	}
	if owner == nil {
		owner = stream.Dict
	}
	// A /Resources that is no dictionary defines no name, so the page's
	// resolve them all.
	res, err := document.ResolveDict(ctx, w.doc, owner["Resources"])
	if err != nil {
		w.doc.Logger.Debug("compliance.graphics: cannot resolve drawn stream resources", "error", err)
	}
	data, err := w.doc.DecodeStream(ctx, stream)
	if err != nil || len(data) == 0 {
		return
	}
	ops, overflows, format, err := content.ParseStreamDataForCompliance(ctx, data)
	if err != nil {
		w.doc.Logger.Debug("compliance.graphics: cannot parse drawn stream", "error", err)
		return
	}
	scan := w.scan(ctx, ops, appearanceResources(ctx, w.doc, res, pageRes), entry)
	w.judge(&drawnStream{
		kind:      kind,
		objNum:    objNum,
		resources: res,
		pageRes:   pageRes,
		pageNum:   pageNum,
		ops:       ops,
		patterns:  scan.patterns(),
		overflow:  countOverflows(overflows),
		format:    format,
	})
	w.descend(ctx, scan, pageRes, pageNum, depth+1)
}
