package compliance

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// --- Form XObject restrictions (PDF/UA-1 §7.20) ---

// checkFormXObjectRestrictions validates PDF/UA-1 §7.20:
//   - t01: Reference XObjects are forbidden (Form XObject with /Ref key)
//   - t02: Form XObjects with MCIDs must not be referenced more than once
func checkFormXObjectRestrictions(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	var results []Violation
	checkedForRef := make(map[int]bool)

	// Get inherited resources from the Pages tree root (fallback for pages
	// without their own /Resources).
	pagesDict, _ := document.ResolveDict(ctx, doc, catalog["Pages"])
	inheritedRes := getResources(ctx, doc, pagesDict)

	// t01: Scan XObject resources for /Ref key (reference XObjects forbidden).
	// t02: Count Do operator invocations per Form XObject across all pages.
	doRefCounts := make(map[int]int) // Form XObject objNum -> Do invocation count

	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		if ctx.Err() != nil {
			return results
		}
		res := getResources(ctx, doc, page)
		if res == nil {
			res = inheritedRes
		}
		if res == nil {
			continue
		}

		// t01: check for /Ref in Form XObjects (only once per object).
		scanFormXObjectsUA(ctx, doc, res, &results, checkedForRef, 0)

		// t02: parse content stream for Do operators, resolve names to objNums.
		xobjDict, _ := document.ResolveDict(ctx, doc, res["XObject"])
		if xobjDict == nil {
			continue
		}
		streamData, err := getPageContentBytes(ctx, doc, page)
		if err != nil || len(streamData) == 0 {
			continue
		}
		ops, err := content.ParseStreamData(ctx, streamData)
		if err != nil {
			slog.Debug("compliance.accessibility: cannot parse content stream", "error", err)
			continue
		}
		for _, op := range ops {
			if op.Operator != "Do" || len(op.Operands) < 1 {
				slog.Debug("compliance.accessibility: cannot parse content stream", "error", err)
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
	for objNum, count := range doRefCounts {
		if count <= 1 {
			continue
		}
		// Form XObjects with /StructParents contain MCIDs.
		stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			slog.Debug("compliance.accessibility: cannot resolve form XObject stream", "error", err)
			continue
		}
		if _, hasSP := stream.Dict["StructParents"]; hasSP {
			results = append(results, failf(Rule_14289_1_7_20_t02,
				"Form XObject (obj %d) contains MCIDs and is referenced %d times", objNum, count)...)
		}
	}

	return results
}

// scanFormXObjectsUA scans XObject resources for PDF/UA-1 §7.20 t01: /Ref
// key forbidden. XObjects are streams, so we use ResolveStream.
// checkedForRef prevents reporting the same object twice.
func scanFormXObjectsUA(ctx context.Context, doc *document.Document, res ast.PdfDict, results *[]Violation, checkedForRef map[int]bool, depth int) {
	if depth > 10 || ctx.Err() != nil {
		return
	}

	xobjectsObj, ok := res["XObject"]
	if !ok {
		return
	}
	xobjects, err := document.ResolveDict(ctx, doc, xobjectsObj)
	if err != nil || xobjects == nil {
		return
	}

	for _, xobjObj := range xobjects {
		ref, isRef := xobjObj.(ast.PdfIndirectRef)
		if isRef && checkedForRef[ref.ObjectNum] {
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, xobjObj)
		if err != nil {
			slog.Debug("compliance.accessibility: cannot resolve XObject stream", "error", err)
			continue
		}
		xobjDict := stream.Dict
		if xobjDict == nil {
			slog.Debug("compliance.accessibility: cannot resolve XObject stream", "error", err)
			continue
		}
		subtype, _ := document.ResolveName(ctx, doc, xobjDict["Subtype"])
		if subtype != "Form" {
			continue
		}

		if isRef {
			checkedForRef[ref.ObjectNum] = true
		}

		// t01: /Ref key forbidden (reference XObject).
		if _, hasRef := xobjDict["Ref"]; hasRef {
			*results = append(*results, fail(Rule_14289_1_7_20_t01,
				"Form XObject contains /Ref key (reference XObject)")...)
		}

		// Recurse into sub-resources.
		subRes, _ := document.ResolveDict(ctx, doc, xobjDict["Resources"])
		if subRes != nil {
			scanFormXObjectsUA(ctx, doc, subRes, results, checkedForRef, depth+1)
		}
	}
}
