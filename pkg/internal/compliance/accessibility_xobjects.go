package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// checkFormXObjectRestrictions validates PDF/UA-1 §7.20: reference XObjects
// are forbidden (t1), and a form XObject holding MCIDs is referenced once
// (t2). See [formXObjectDrawsUA].
func checkFormXObjectRestrictions(ctx context.Context, vctx *validationCtx) []Violation {
	if vctx.profile != "PDF/UA-1" {
		return nil
	}
	return formXObjectDrawsUA(ctx, vctx)
}

// formXObjectDrawsUA judges the two rules ISO 14289-1, 7.20 states on a form
// XObject at every reference to it: a reference made by a Do in a page's
// content and in every stream [drawnStreamWalk] reaches (the forms a Do
// paints, the cells of the tiling patterns an scn selects, the glyphs of the
// Type 3 fonts text is shown with, annotation appearances), and a reference
// made by an appearance entry, each annotation once for the document
// ([annotationAppearances]). A form a /Resources lists and no Do draws is
// never referenced.
//
// Test 1 forbids reference XObjects: a form XObject whose dictionary carries
// /Ref (ISO 32000-1, 8.10.4, Table 95 gives /Ref to forms alone). It restates
// ISO 19005-1, 6.2.6 t1, and veraPDF states both on the same PDXForm object
// (containsRef), so every reference to such a form fails it.
//
// Test 2 has the content of a form XObject incorporated into the structure
// tree as ISO 32000-1, 14.7.2 describes, which the rule's message states as a
// form XObject holding MCIDs referenced once: the form's /StructParents names
// the one parent tree entry its marked-content sequences resolve through (ISO
// 32000-1, 8.10.1, Table 95, and 14.7.4.4), so a second reference paints them
// again under the same parents. veraPDF reads it as
// GFPDXForm.getisUniqueSemanticParent does (veraPDF-validation 1.30): a form
// carrying /StructParents ([knownKey]) passes at its first reference and fails
// at every later one, a form without it never fails, and nor does one carrying
// /StructParent, the key of a form that is one structural content item itself
// (Table 95).
//
// A content stream is parsed once for the document, so a Do counts once
// however many pages or streams draw the stream that holds it. A page whose
// /Contents an earlier page already holds adds no reference through it, the
// pages' resources equal or not, unless its own resources map the Do to a
// form the earlier readings did not draw. veraPDF walks the shared stream from
// the first page alone and never reaches that form; litho counts it, since the
// second page paints it.
//
// Measured with veraPDF 1.30.2 at PDF/UA-1, test 1 with ISO 19005-2, 6.2.9
// t2 at PDF/A-2b stating the same count in every case: a form carrying /Ref
// fails test 1 once drawn by a tiling cell, by a Type 3 glyph a page or a cell
// shows, by an appearance stream and by the cell of a pattern a form paints
// with, twice drawn twice by one page, by two pages or by a cell two pages
// select, once drawn by the content two pages share under resources of their
// own, and never when only a page, a form, a cell or a Type 3 font lists it,
// when the cell's pattern is never selected, or when a Tf selects the font and
// no text is shown. A normal appearance carrying /Ref fails test 1 once, and
// twice when two annotations share it. A form carrying /StructParents fails
// test 2 once drawn twice by one page, under one name or two, by two pages, by
// a page and a cell, a glyph, a form or an appearance stream it draws, or by a
// cell or a form drawing it twice, twice drawn three times by one page or
// named by one annotation's /N, /R and /D, and once when it is the appearance
// of two annotations, or one annotation's and drawn by the page. It never
// fails drawn once, nor when a page selects twice the cell, shows twice the
// glyph, or draws twice the form that draws it once, when two pages draw that
// cell, glyph or form, when two annotations share the appearance that draws
// it, or when two pages share the content that draws it.
//
// # Corpus evidence
//
//   - litho refreach_ua1.pdf: plants one reference form per path, and litho
//     stated test 1 2 times, where veraPDF states it 6 times.
//   - All corpora, at PDF/UA-1: reaching the forms the streams draw, instead
//     of the forms the resources list, moves no test 1 count on any other
//     file.
//   - litho formreuse_ua1.pdf: plants one second reference per path, and
//     litho stated test 2 in none of them, where veraPDF states it 6 times.
//     litho formshare_ua1.pdf: litho stated test 2 twice for the forms the
//     content two pages share draws, where veraPDF states it nowhere.
//   - All corpora, at PDF/UA-1: counting every reference moves test 2 on two
//     files only, both to veraPDF's count: pdfcpu pkg_testdata__RA_CI.pdf
//     from 1 to 9 and verapdf PDF_UA-1__7.20-t02-fail-a.pdf from 1 to 2.
func formXObjectDrawsUA(ctx context.Context, vctx *validationCtx) []Violation {
	doc := vctx.doc
	var results []Violation
	// referenced holds the marked forms referenced once already; see
	// GFPDXForm.getisUniqueSemanticParent, veraPDF-validation 1.30.
	referenced := make(map[int]bool)
	judge := func(dict ast.PdfDict, objNum int, where string) {
		if knownKey(dict, "Ref") {
			results = append(results, failf(Rule_14289_1_7_20_t01, "%s", where)...)
		}
		// A form written directly has no object to be referenced again.
		if objNum == 0 || !knownKey(dict, "StructParents") {
			return
		}
		if referenced[objNum] {
			results = append(results, failf(Rule_14289_1_7_20_t02,
				"%s: form XObject (obj %d) contains MCIDs and is referenced more than once", where, objNum)...)
			return
		}
		referenced[objNum] = true
	}
	// sharedDraws holds, per page content stream two pages share, the forms
	// each Do of it drew already: {stream, operation index, form}.
	sharedDraws := make(map[[3]int]bool)
	judgeDraws := func(scope ast.PdfDict, ops []content.Operation, pageNum, contentNum int) {
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
			objNum := 0
			if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
				objNum = ref.ObjectNum
			}
			if contentNum != 0 {
				draw := [3]int{contentNum, i, objNum}
				if sharedDraws[draw] {
					continue
				}
				sharedDraws[draw] = true
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
			if subtype, _ := document.ResolveName(ctx, doc, dict["Subtype"]); subtype == "Form" {
				judge(dict, objNum, fmt.Sprintf("page %d, XObject %s", pageNum, name))
			}
		}
	}

	annots := make(map[string]bool)
	drawn := newDrawnStreamWalk(vctx, func(s *drawnStream) {
		judgeDraws(appearanceResources(ctx, doc, s.resources, s.pageRes), s.ops, s.pageNum, 0)
	})
	for i := range vctx.pages {
		if ctx.Err() != nil {
			return results
		}
		p := &vctx.pages[i]
		// A content stream an earlier page shares under the same resources
		// draws what it drew there; see [markJudgedContents].
		if !p.contentJudged {
			contentNum := 0
			if ref, ok := p.dict["Contents"].(ast.PdfIndirectRef); ok {
				contentNum = ref.ObjectNum
			}
			judgeDraws(p.resources, p.parsedOps(ctx), i+1, contentNum)
		}
		for _, ap := range annotationAppearances(ctx, doc, p.dict, annots) {
			judge(ap.stream.Dict, ap.objNum, fmt.Sprintf("page %d, annotation appearance %s", i+1, ap.key))
		}
		drawn.walkPage(ctx, p, i+1)
	}
	return results
}
