package compliance

import (
	"context"
	"fmt"
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
	// none. A glyph description reads the /Resources of its font, or its own
	// (see [drawnStreamWalk]).
	resources ast.PdfDict
	// resourcesNum is the object number of resources when the dictionary
	// naming it writes it as an indirect reference, and 0 otherwise.
	resourcesNum int
	// pageRes is the resource dictionary of the page the walk reached the
	// stream from. It resolves every name the stream's own resources leave
	// undefined.
	pageRes ast.PdfDict
	pageNum int
	ops     []content.Operation
	// patterns names the patterns its scn and SCN operators select while it
	// processes its colour operators ([drawnScan]), resource names the stream
	// uses as much as its other operators' operands (see
	// [checkContentStreamResourcesOps]).
	patterns map[string]bool
	// overflow counts the integer literals past the 64-bit range the parse
	// read as reals (see [checkNumberLimits]).
	overflow map[float64]int
	// format holds the format violations the parse recorded.
	format []content.FormatViolation
	// colourOff reports that the stream starts with the colour operators it
	// holds ignored (see [NewColourState] and [drawnScan]).
	colourOff bool
}

// drawnStreamWalk reaches every content stream a document draws besides the
// content of its pages: the form XObjects a Do paints, the cells of the tiling
// patterns an scn or SCN selects, every glyph description of the Type3 fonts
// text is shown with, and the annotation appearance streams, together with
// every stream those draw in turn. The content-stream rules judge each one
// from a single parse: the hex string tests, ISO 19005-1/2/3, 6.1.6 t1 and t2
// (see [drawnStreamHexStrings]), the undefined-operator rule, ISO 19005-1,
// 6.2.10 t1 and ISO 19005-2/3/4, 6.2.2 t1, the resource-name tests beside it
// and the inline image filter rule, in [checkDrawnStream], and the operand
// limits in [checkContentStreamLimits].
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
//     shares under the same resources draws nothing the walk reaches through
//     that content, since it paints the same there; under resources of its
//     own, it draws what they name, where veraPDF walks the content from the
//     earlier page alone; see [markJudgedContents].
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
//     resources of its font (ISO 32000-1, 9.6.5), unless it carries a
//     /Resources of its own under a profile built on ISO 32000-1, which it
//     then reads in the font's stead (GFPDType3Font.getResourcesFromCharProcs,
//     veraPDF-validation 1.30). Measured at PDF/A-2b and PDF/A-4: a glyph
//     setting /GS1 fails ISO 19005-2, 6.2.10 t1 when its own resources give
//     /GS1 the blend mode /Foo and its font's give it /Normal, passes it the
//     other way round, and passes it when its own resources lack /GS1 and its
//     font's name /Foo; ISO 19005-4, 6.2.9 t1 answers each the other way.
//   - A form starts from the colour-space selection in force at its Do (ISO
//     32000-1, 8.10.2), so a bare scn in it selects a pattern when the drawing
//     stream selected the Pattern space.
//   - A form and a Type 3 glyph start with the colour operators they hold
//     ignored when the stream drawing them ignores its own at the Do or the
//     text show, and the cells of the tiling patterns a stream selects when it
//     began that way or selects an uncoloured pattern anywhere (see
//     [drawnScan]): an integer past the limit given to g fails ISO 19005-1,
//     6.1.12 t1 and ISO 19005-2, 6.1.13 t1 in none of a form an uncoloured
//     cell or a glyph past its d1 paints, a glyph an uncoloured cell shows,
//     or a coloured cell of a page that also selects an uncoloured pattern.
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
//   - All corpora: reading the resources of a glyph description that has its
//     own changes no count at PDF/A-1b or PDF/A-2b, veraPDF files at their
//     own profile.
type drawnStreamWalk struct {
	doc *document.Document
	// judged holds the object numbers of the streams already judged, and of
	// every page's content stream. A stream written directly rather than
	// referenced can be reached only once.
	judged map[int]bool
	judge  func(*drawnStream)
	// glyphResources reports that a glyph description with a /Resources of
	// its own reads it rather than its font's: the profile is built on ISO
	// 32000-1.
	glyphResources bool
}

// newDrawnStreamWalk returns a walk over the pages of vctx that hands every
// stream it reaches to judge. The profile of vctx says which resources a
// glyph description reads; see [drawnStreamWalk].
func newDrawnStreamWalk(vctx *validationCtx, judge func(*drawnStream)) *drawnStreamWalk {
	w := &drawnStreamWalk{
		doc:            vctx.doc,
		judged:         make(map[int]bool),
		judge:          judge,
		glyphResources: PDFAPart(vctx.profile) != 4 && PDFUAPart(vctx.profile) != 2,
	}
	for i := range vctx.pages {
		if ref, ok := vctx.pages[i].dict["Contents"].(ast.PdfIndirectRef); ok {
			w.judged[ref.ObjectNum] = true
		}
	}
	return w
}

// ColourOffStreams returns, by object number, the content streams the pages
// of doc draw that the content-stream checks judge from a start with their
// colour operators off (see [drawnScan] and [ColourState]): the cell of an
// uncoloured tiling pattern, every tiling cell a stream paints once it selects
// one or when it began that way itself, and a form or a Type 3 glyph
// description first drawn while the drawing stream's colour operators are
// off. A page's content, an appearance stream and a stream no page draws are
// not listed. profile names the part whose rules say which resources a glyph
// description reads (see [drawnStreamWalk]).
//
// It reads the objects the file lists alone, as a validation run does (see
// [document.OpenedObjectsOnly]).
func ColourOffStreams(ctx context.Context, doc *document.Document, profile string) (map[int]bool, error) {
	ctx = document.OpenedObjectsOnly(ctx)
	catalog, err := resolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	vctx := &validationCtx{doc: doc, profile: profile, part: PDFAPart(profile), pages: collectPageContexts(ctx, doc, catalog)}
	off := make(map[int]bool)
	walk := newDrawnStreamWalk(vctx, func(s *drawnStream) {
		if s.colourOff && s.objNum != 0 {
			off[s.objNum] = true
		}
	})
	for i := range vctx.pages {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("walking the streams page %d draws: %w", i+1, err)
		}
		walk.walkPage(ctx, &vctx.pages[i], i+1)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("walking the streams the pages draw: %w", err)
	}
	return off, nil
}

// walkPage reaches the streams page p, the pageNum-th, draws: those its
// content draws, then its annotation appearance streams. A content stream an
// earlier page shares under the same resources was walked from that page
// alone; see [markJudgedContents].
func (w *drawnStreamWalk) walkPage(ctx context.Context, p *pageCtx, pageNum int) {
	if !p.contentJudged {
		w.descend(ctx, w.scan(ctx, p.parsedOps(ctx), p.resources, inheritedCSDefault(), false), p.resources, pageNum, 1)
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
				w.visit(ctx, streamObj, nil, drawnAppearance, p.resources, inheritedCSDefault(), false, pageNum, 1)
			}
		}
	}
}

// drawnScan is what one content stream draws, read from its operations: the
// forms its Do operators paint, the patterns its scn and SCN operators select
// and the colour-space selection in force at each Do.
//
// It also says which streams it draws start with their colour operators
// ignored, as veraPDF passes that part of the graphics state on
// (GraphicState.processColorOperators, veraPDF-validation 1.30): formsOff
// and fontsOff name the forms and the Type 3 fonts whose first Do or text
// show runs with the stream's own colour operators ignored, which a d1 or the
// state the stream began in decides, and cellsOff reports that the cells of
// the tiling patterns it selects start so. veraPDF builds every tiling cell a
// stream paints from one state, the one the stream began in, which each
// uncoloured pattern the stream builds turns off for all of them (see
// [colorLinkWalk.cellEntry]).
//
// built names the patterns an scn or SCN selects while the stream processes
// its colour operators, the only ones veraPDF builds and so the only cells it
// reaches: an scn past a d1, or in a stream that began with its colour
// operators off, selects nothing (ISO 32000-1, 9.6.5, Table 113, and 8.7.3.3).
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b: an integer past the
// limit given to w in the cell of a pattern an scn selects past a d1, in an
// uncoloured cell or in a form such a cell paints, fails nothing, and one in
// the cell of a pattern an scn selects past the Q closing a d1 fails ISO
// 19005-1, 6.1.12 t1 and ISO 19005-2, 6.1.13 t1.
//
// # Corpus evidence
//
//   - All corpora: carrying the colour state into the streams a stream draws
//     moves no implementation limit count at PDF/A-1b and PDF/A-2b, veraPDF
//     files at their own profile.
//   - All corpora: reaching only the cells of the patterns in built moves no
//     count at PDF/A-1b or PDF/A-2b.
type drawnScan struct {
	ops []content.Operation
	// scope resolves the names its operators use.
	scope               ast.PdfDict
	painted             map[string]bool
	selected            *usedResources
	doEntries           map[string]inheritedCS
	formsOff, fontsOff  map[string]bool
	built               map[string]bool
	cellsOff, colourOff bool
}

// scan reads what the stream whose operations are ops draws. scope resolves
// the names they use, entry is the colour-space selection the stream starts
// from, and colourOff reports that it starts with its colour operators
// ignored.
func (w *drawnStreamWalk) scan(ctx context.Context, ops []content.Operation, scope ast.PdfDict, entry inheritedCS, colourOff bool) drawnScan {
	if len(ops) == 0 {
		return drawnScan{}
	}
	_, painted, _, selected, doEntries := scanContentStreamDeviceCSFrom(ctx, ops, collectPatternCSNames(ctx, w.doc, scope), entry)
	s := drawnScan{ops: ops, scope: scope, painted: painted, selected: selected, doEntries: doEntries, colourOff: colourOff}
	w.scanColourStates(ctx, &s, entry)
	return s
}

// scanColourStates fills the colour states of s (see [drawnScan]), which
// starts from the colour-space selection entry.
func (w *drawnStreamWalk) scanColourStates(ctx context.Context, s *drawnScan, entry inheritedCS) {
	s.formsOff = make(map[string]bool)
	s.fontsOff = make(map[string]bool)
	s.built = make(map[string]bool)
	s.cellsOff = s.colourOff
	colorSpaces, _ := document.ResolveDict(ctx, w.doc, s.scope["ColorSpace"]) // Best-effort: unreadable colour spaces select no Pattern space.
	patterns, _ := document.ResolveDict(ctx, w.doc, s.scope["Pattern"])       // Best-effort: unreadable patterns are none.
	// Only whether a space is a Pattern space and which pattern scn selects
	// matter here, so every other space is DeviceGray. A form starts in the
	// Pattern space its Do left selected (ISO 32000-1, 8.10.2), so a bare scn
	// in it selects a pattern.
	other := csRef{family: "DeviceGray"}
	start := csPaintState{colourOff: s.colourOff}
	if entry.fillPattern {
		start.fill = csRef{family: "Pattern", isPattern: true}
	}
	if entry.strokePattern {
		start.stroke = csRef{family: "Pattern", isPattern: true}
	}
	walkColorSpaceLinksWith(ctx, s.ops, start, csLinkHooks{
		resolve: func(name string) csRef {
			if obj, ok := colorSpaces[ast.PdfName(name)]; ok && resolveCSName(ctx, w.doc, resolveColorSpaceName(ctx, w.doc, obj, colorSpaces)) == "Pattern" || name == "Pattern" {
				return csRef{name: name, isPattern: true}
			}
			return other
		},
		pattern: func(name string) csRef {
			if name == "" {
				return other
			}
			s.built[name] = true
			return csRef{pattern: name, isPattern: true}
		},
		construct: func(ref csRef, _ csPaintState) {
			if ref.pattern == "" {
				return
			}
			if _, uncoloured := tilingPatternKind(ctx, w.doc, patterns[ast.PdfName(ref.pattern)]); uncoloured {
				s.cellsOff = true
			}
		},
		link: func(csRef, int, string, overprintMode) {},
		paint: func(name string, st csPaintState) {
			if _, seen := s.formsOff[name]; !seen {
				s.formsOff[name] = st.colourOff
			}
		},
		show: func(st csPaintState) {
			if _, seen := s.fontsOff[st.font]; !seen && st.font != "" {
				s.fontsOff[st.font] = st.colourOff
			}
		},
	})
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
		w.visit(ctx, obj, nil, drawnForm, pageRes, inheritedCSFor(s.doEntries, name), s.formsOff[name], pageNum, depth)
	}

	patterns, _ := document.ResolveDict(ctx, w.doc, scope["Pattern"])
	for _, name := range slices.Sorted(maps.Keys(s.patterns())) {
		obj, ok := patterns[ast.PdfName(name)]
		if !ok || !s.built[name] || w.judgedAlready(obj) {
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
		// Building an uncoloured pattern turns its own cell off too.
		_, uncoloured := tilingPatternKind(ctx, w.doc, obj)
		w.visit(ctx, obj, nil, drawnPattern, pageRes, inheritedCSDefault(), s.cellsOff || uncoloured, pageNum, depth)
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
			w.visit(ctx, charProcs[glyph], fontDict, drawnGlyph, pageRes, inheritedCSDefault(), s.fontsOff[name], pageNum, depth)
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
// /Resources the stream reads, or nil for the stream's own dictionary, and
// colourOff reports that the stream starts with its colour operators ignored
// (see [drawnScan]).
func (w *drawnStreamWalk) visit(ctx context.Context, obj ast.PdfObject, owner ast.PdfDict, kind drawnStreamKind, pageRes ast.PdfDict, entry inheritedCS, colourOff bool, pageNum, depth int) {
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
	if owner == nil || (kind == drawnGlyph && w.glyphResources && knownKey(stream.Dict, "Resources")) {
		owner = stream.Dict
	}
	// A /Resources that is no dictionary defines no name, so the page's
	// resolve them all.
	res, err := document.ResolveDict(ctx, w.doc, owner["Resources"])
	if err != nil {
		w.doc.Logger.Debug("compliance.graphics: cannot resolve drawn stream resources", "error", err)
	}
	data, reads := decodeContentStream(ctx, w.doc, stream)
	if !reads || len(data) == 0 {
		return
	}
	ops, overflows, format, err := content.ParseStreamDataForCompliance(ctx, data)
	if err != nil {
		w.doc.Logger.Debug("compliance.graphics: cannot parse drawn stream", "error", err)
		return
	}
	scan := w.scan(ctx, ops, appearanceResources(ctx, w.doc, res, pageRes), entry, colourOff)
	resourcesNum := 0
	if ref, ok := owner["Resources"].(ast.PdfIndirectRef); ok {
		resourcesNum = ref.ObjectNum
	}
	w.judge(&drawnStream{
		kind:         kind,
		objNum:       objNum,
		resources:    res,
		resourcesNum: resourcesNum,
		pageRes:      pageRes,
		pageNum:      pageNum,
		ops:          ops,
		patterns:     scan.built,
		overflow:     countOverflows(overflows),
		format:       format,
		colourOff:    colourOff,
	})
	w.descend(ctx, scan, pageRes, pageNum, depth+1)
}
