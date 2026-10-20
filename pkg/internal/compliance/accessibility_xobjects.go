package compliance

import (
	"context"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// checkFormXObjectRestrictions validates PDF/UA-1 §7.20:
//   - t01: Reference XObjects are forbidden (Form XObject with /Ref key); see
//     [referenceXObjectsUA].
//   - t02: Form XObjects with MCIDs must not be referenced more than once.
func checkFormXObjectRestrictions(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog := vctx.doc, vctx.catalog
	if vctx.profile != "PDF/UA-1" {
		return nil
	}

	results := referenceXObjectsUA(ctx, vctx)

	// t02: Count Do operator invocations per Form XObject across all pages.
	doRefCounts := make(map[int]int) // Form XObject objNum -> Do invocation count

	pages := collectPageObjects(ctx, doc, catalog)
	for i := range pages {
		if ctx.Err() != nil {
			return results
		}
		page := pages[i].dict
		res := pages[i].effectiveResources(ctx, doc)
		if res == nil {
			continue
		}

		// t02: parse content stream for Do operators, resolve names to objNums.
		xobjDict, _ := document.ResolveDict(ctx, doc, res["XObject"])
		if xobjDict == nil {
			continue
		}
		streamData, err := resolvePageContentBytes(ctx, doc, page)
		if err != nil || len(streamData) == 0 {
			continue
		}
		ops, err := content.ParseStreamData(ctx, streamData)
		if err != nil {
			doc.Logger.Debug("compliance.accessibility: cannot parse content stream", "error", err)
			continue
		}
		for _, op := range ops {
			if op.Operator != "Do" || len(op.Operands) < 1 {
				continue
			}
			name, ok := op.Operands[0].(ast.PdfName)
			if !ok {
				continue
			}
			xobjRef, ok := xobjDict[name].(ast.PdfIndirectRef)
			if !ok {
				continue
			}
			doRefCounts[xobjRef.ObjectNum]++
		}
	}

	// t02: Form XObjects with MCIDs must not be referenced more than once.
	// Sorted object numbers keep violation order deterministic
	// (CONTRIBUTING "Deterministic output").
	refNums := make([]int, 0, len(doRefCounts))
	for objNum := range doRefCounts {
		refNums = append(refNums, objNum)
	}
	slices.Sort(refNums)
	for _, objNum := range refNums {
		count := doRefCounts[objNum]
		if count <= 1 {
			continue
		}
		// Form XObjects with /StructParents contain MCIDs.
		stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			doc.Logger.Debug("compliance.accessibility: cannot resolve form XObject stream", "error", err)
			continue
		}
		if _, hasSP := stream.Dict["StructParents"]; hasSP {
			results = append(results, failf(Rule_14289_1_7_20_t02,
				"Form XObject (obj %d) contains MCIDs and is referenced %d times", objNum, count)...)
		}
	}

	return results
}

// referenceXObjectsUA judges ISO 14289-1, 7.20 t1, which forbids reference
// XObjects: a form XObject whose dictionary carries /Ref (ISO 32000-1, 8.10.4,
// Table 95 gives /Ref to forms alone). It restates ISO 19005-1, 6.2.6 t1, and
// veraPDF states both on the same PDXForm object (containsRef), so the rule
// reaches a form where [checkXObjectGroup] does: at every Do that draws it, in
// a page's content and in every stream [drawnStreamWalk] reaches (the forms a
// Do paints, the cells of the tiling patterns an scn selects, the glyphs of
// the Type 3 fonts text is shown with, annotation appearances), and at every
// appearance entry, each annotation once for the document
// ([annotationAppearances]). A form a /Resources lists and no Do draws is
// never judged.
//
// Measured with veraPDF 1.30.2 at PDF/UA-1, with ISO 19005-2, 6.2.9 t2 at
// PDF/A-2b stating the same count in every case: a form carrying /Ref fails t1
// once drawn by a tiling cell, by a Type 3 glyph a page or a cell shows, by an
// appearance stream and by the cell of a pattern a form paints with, twice
// drawn twice by one page, by two pages or by a cell two pages select, and
// never when only a page, a form, a cell or a Type 3 font lists it, when the
// cell's pattern is never selected, or when a Tf selects the font and no text
// is shown. Measured with veraPDF 1.30: a normal appearance carrying /Ref
// fails t1 once, and twice when two annotations share it.
//
// # Corpus evidence
//
//   - litho refreach_ua1.pdf: plants one reference form per path, and litho
//     stated t1 2 times, where veraPDF states it 6 times.
//   - All corpora, at PDF/UA-1: reaching the forms the streams draw, instead
//     of the forms the resources list, moves no count on any other file.
func referenceXObjectsUA(ctx context.Context, vctx *validationCtx) []Violation {
	doc := vctx.doc
	var results []Violation
	judgeDraws := func(scope ast.PdfDict, ops []content.Operation, pageNum int) {
		xobjects, _ := document.ResolveDict(ctx, doc, scope["XObject"]) // Best-effort: unreadable XObjects draw no form.
		if len(xobjects) == 0 {
			return
		}
		for i, op := range ops {
			if i%contentOpCancelCheckInterval == 0 && ctx.Err() != nil {
				return
			}
			if op.Operator != "Do" || len(op.Operands) == 0 {
				continue
			}
			name, ok := op.Operands[0].(ast.PdfName)
			if !ok {
				continue
			}
			obj, ok := xobjects[name]
			if !ok {
				continue
			}
			// Some producers write an XObject as a bare dictionary, which the
			// PDF/A form rules judge too ([checkDrawnXObject]).
			dict, err := document.ResolveDict(ctx, doc, obj)
			if err != nil {
				stream, serr := document.ResolveStream(ctx, doc, obj)
				if serr != nil {
					continue
				}
				dict = stream.Dict
			}
			if subtype, _ := document.ResolveName(ctx, doc, dict["Subtype"]); subtype == "Form" && knownKey(dict, "Ref") {
				results = append(results, failf(Rule_14289_1_7_20_t01, "page %d, XObject %s", pageNum, name)...)
			}
		}
	}

	annots := make(map[string]bool)
	drawn := newDrawnStreamWalk(vctx, func(s *drawnStream) {
		judgeDraws(appearanceResources(ctx, doc, s.resources, s.pageRes), s.ops, s.pageNum)
	})
	for i := range vctx.pages {
		if ctx.Err() != nil {
			return results
		}
		p := &vctx.pages[i]
		// A content stream an earlier page shares under the same resources is
		// judged there alone; see [markJudgedContents].
		if !p.contentJudged {
			judgeDraws(p.resources, p.parsedOps(ctx), i+1)
		}
		for _, ap := range annotationAppearances(ctx, doc, p.dict, annots) {
			if knownKey(ap.stream.Dict, "Ref") {
				results = append(results, failf(Rule_14289_1_7_20_t01, "page %d, annotation appearance %s", i+1, ap.key)...)
			}
		}
		drawn.walkPage(ctx, p, i+1)
	}
	return results
}
