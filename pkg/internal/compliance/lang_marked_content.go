package compliance

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// pdfaMarkedContentLang reports, under rule, every BDC operator whose property
// list carries a /Lang string that is not a language tag of the given PDF/A
// part (ISO 19005-1, 6.8.4; ISO 19005-2 and -3, 6.7.4, which name a property
// list alongside the catalog and the structure elements). The operators are
// the ones [forEachContentLang] reaches; the clause does not depend on a
// structure tree, and neither does the walk. A /Lang of another type is no
// CosLang for veraPDF, and passes.
//
// # Corpus evidence
//
//   - litho inlineimage_2b.pdf: has no structure tree and a BDC /Lang of
//     "-pt", and veraPDF reports it at PDF/A-1a, 2a and 3a. litho read no
//     content without a structure tree.
//   - pdfjs issue11678.pdf: a raw byte search read a "/Lang" out of a damaged
//     stretch of its content and reported it, which veraPDF does not.
//   - All corpora: over the 71 files veraPDF parses among the 72 whose pages
//     or forms spell "/Lang" in their content, the count equals veraPDF's on
//     every file at PDF/A-1a, 2a and 3a, where it differed on the two above.
func pdfaMarkedContentLang(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, rule Rule) []Violation {
	var results []Violation
	forEachContentLang(ctx, doc, catalog, func(pageNum int, langObj ast.PdfObject, origin string) {
		lang, err := document.ResolveString(ctx, doc, langObj)
		if err != nil {
			doc.Logger.Debug("compliance.structure: marked-content /Lang is no string", "page", pageNum, "error", err)
			return
		}
		if pdfaLangTagInvalid(lang, part) {
			results = append(results, Violation{
				Rule:    rule,
				Context: fmt.Sprintf("lang=%s, page %d %s", strings.TrimSpace(document.DecodePdfString(lang)), pageNum, origin),
			})
		}
	})
	return results
}

// forEachContentLang calls visit with the /Lang entry of the property list of
// every BDC operator a content walk reaches: each page's content stream, the
// content of every annotation appearance on the page (normal, rollover and
// down, each state of a state subdictionary included), and every Form XObject
// either draws, each form once however often it is drawn. A named property
// list resolves through the resources of the stream that names it, the page's
// standing in for a name the stream does not define (see
// [appearanceResources]). origin says where the operator sits, for a
// violation's context.
//
// That is the set of CosLang objects veraPDF 1.30.2 builds from content,
// measured at PDF/A-1a, PDF/A-2a, PDF/UA-1 and PDF/UA-2 with one crafted file
// per shape: every BDC operator of those streams is judged, and each counts
// once. A DP operator, a property list no operator names, and "/Lang" spelled
// inside a string operand are not.
func forEachContentLang(ctx context.Context, doc *document.Document, catalog ast.PdfDict, visit func(pageNum int, langObj ast.PdfObject, origin string)) {
	w := contentLangWalk{doc: doc, visit: visit, visited: make(map[int]bool)}
	pages := collectPageObjects(ctx, doc, catalog)
	for pageIdx := range pages {
		if ctx.Err() != nil {
			return
		}
		page := &pages[pageIdx]
		res := page.effectiveResources(ctx, doc)
		if data, err := resolvePageContentBytes(ctx, doc, page.dict); err == nil && len(data) > 0 {
			if ops, err := content.ParseStreamData(ctx, data); err == nil {
				w.walk(ctx, ops, res, pageIdx+1, "", 0)
			}
		}
		w.annotations(ctx, page.dict, res, pageIdx+1)
	}
}

// contentLangWalk carries one [forEachContentLang] run: the callback and the
// forms already walked.
type contentLangWalk struct {
	doc     *document.Document
	visit   func(pageNum int, langObj ast.PdfObject, origin string)
	visited map[int]bool
}

// annotations walks the normal, rollover and down appearances of every
// annotation on the page, each state of a state subdictionary included.
func (w *contentLangWalk) annotations(ctx context.Context, page, pageRes ast.PdfDict, pageNum int) {
	annots, err := document.ResolveArray(ctx, w.doc, page["Annots"])
	if err != nil {
		return
	}
	for _, annotObj := range annots {
		if ctx.Err() != nil {
			return
		}
		annot, err := document.ResolveDict(ctx, w.doc, annotObj)
		if err != nil || annot == nil {
			continue
		}
		apDict, err := document.ResolveDict(ctx, w.doc, annot["AP"])
		if err != nil || apDict == nil {
			continue
		}
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			states := appearanceStreamObjects(ctx, w.doc, apDict[key])
			if len(states) > maxAppearanceStateFonts {
				states = states[:maxAppearanceStateFonts]
			}
			for _, apObj := range states {
				w.form(ctx, apObj, pageRes, pageNum, " in annotation appearance", 0, true)
			}
		}
	}
}

// walk visits the BDC operators of one content stream and descends into the
// Form XObjects it draws.
func (w *contentLangWalk) walk(ctx context.Context, ops []content.Operation, res ast.PdfDict, pageNum int, where string, depth int) {
	var props, xobjects ast.PdfDict
	var propsRead, xobjectsRead bool
	for i, op := range ops {
		if i%contentOpCancelCheckInterval == 0 && ctx.Err() != nil {
			return
		}
		switch op.Operator {
		case "BDC":
			if len(op.Operands) < 2 {
				continue
			}
			var list ast.PdfDict
			origin := "inline BDC"
			switch v := op.Operands[1].(type) {
			case ast.PdfDict:
				list = v
			case ast.PdfName:
				if !propsRead {
					// Best-effort: no /Properties leaves every named list
					// unresolved, which veraPDF judges no further either.
					props, _ = document.ResolveDict(ctx, w.doc, res["Properties"])
					propsRead = true
				}
				// Best-effort: an unresolvable entry names no property list.
				list, _ = document.ResolveDict(ctx, w.doc, props[v])
				origin = "property " + string(v)
			}
			if langObj, ok := list["Lang"]; ok {
				w.visit(pageNum, langObj, origin+where)
			}
		case "Do":
			if len(op.Operands) < 1 {
				continue
			}
			name, ok := op.Operands[0].(ast.PdfName)
			if !ok {
				continue
			}
			if !xobjectsRead {
				// Best-effort: no /XObject dictionary draws no form.
				xobjects, _ = document.ResolveDict(ctx, w.doc, res["XObject"])
				xobjectsRead = true
			}
			if xobj, ok := xobjects[name]; ok {
				w.form(ctx, xobj, res, pageNum, where+" in Form XObject "+string(name), depth+1, false)
			}
		}
	}
}

// form walks the Form XObject obj, once per run, resolving the names its
// content uses against its own /Resources with parentRes filling the gaps. An
// appearance stream is a form whether or not it states /Subtype /Form (ISO
// 32000-1, 12.5.5); an XObject a Do draws is one only when it does.
func (w *contentLangWalk) form(ctx context.Context, obj ast.PdfObject, parentRes ast.PdfDict, pageNum int, where string, depth int, appearance bool) {
	if formTooDeep(w.doc, depth) || ctx.Err() != nil {
		return
	}
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef && w.visited[ref.ObjectNum] {
		return
	}
	stream, err := document.ResolveStream(ctx, w.doc, obj)
	if err != nil || stream.Dict == nil {
		return
	}
	// Best-effort: a /Subtype that is no name reads as absent.
	subtype, _ := document.ResolveName(ctx, w.doc, stream.Dict["Subtype"])
	if subtype != "Form" && (!appearance || subtype != "") {
		return
	}
	if isRef {
		w.visited[ref.ObjectNum] = true
	}
	// veraPDF reads no operator from a form it cannot decode
	// ([decodeContentStream]).
	data, reads := decodeContentStream(ctx, w.doc, stream)
	if !reads {
		return
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		w.doc.Logger.Debug("compliance: marked-content /Lang: cannot parse form content", "page", pageNum, "error", err)
		return
	}
	// Best-effort: a form with no readable /Resources names through its parent.
	own, _ := document.ResolveDict(ctx, w.doc, stream.Dict["Resources"])
	w.walk(ctx, ops, appearanceResources(ctx, w.doc, own, parentRes), pageNum, where, depth)
}
