package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkPDFUA2FormStructure validates:
//   - §8.10.1 t02: Form structure element contains at most one widget annotation.
//   - §8.10.2.3 t01: Widget without label needs /Contents entry.
func checkPDFUA2FormStructure(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	var results []Violation

	// §8.10.1 t02: walk structure tree for Form elements with >1 widget OBJR.
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err == nil && treeRoot != nil {
		walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
			typeName, _ := document.ResolveName(ctx, doc, elem["S"])
			if typeName != "Form" {
				return
			}
			// Count OBJR children pointing to widget annotations.
			widgetCount := countOBJRWidgets(ctx, doc, elem)
			if widgetCount > 1 {
				results = append(results, failf(Rule_14289_2_8_10_1_t02,
					"Form structure element at %s contains %d widget annotations", path, widgetCount)...)
			}
		})
	}

	// §8.10.2.3 t01: Widget annotations without label need /Contents.
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		annotsArr, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for _, annotObj := range annotsArr {
			annot, _ := document.ResolveDict(ctx, doc, annotObj)
			if annot == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if subtype != "Widget" {
				continue
			}
			// veraPDF §8.10.2.3 t1 test:
			//   containsLbl || Contents != null || isFieldWidget == false ||
			//   structParentType == null || isArtifact == true
			// so it fires only for a FIELD widget that participates in the
			// structure tree and supplies neither a Lbl structure element nor a
			// /Contents entry. There is NO Hidden exemption (unlike the UA-1
			// §7.18.1 alt-text rule): a hidden field widget may still be exposed,
			// e.g. shown by script, so it must be labelled.
			if !isFieldWidget(ctx, doc, annot) {
				continue
			}
			// structParentType == null / isArtifact: a widget not resolved to an
			// enclosing structure element through its /StructParent is out of scope.
			structElem := widgetStructElem(ctx, doc, treeRoot, annot)
			if structElem == nil {
				continue
			}
			// containsLbl: an enclosing Lbl structure element supplies the label.
			if liHasLblChild(ctx, doc, structElem) {
				continue
			}
			// /Contents supplies description and context for the widget.
			if widgetHasContents(ctx, doc, annot) {
				continue
			}
			results = append(results, fail(Rule_14289_2_8_10_2_3_t01,
				"field Widget annotation has neither a Lbl structure element nor a /Contents entry")...)
		}
	}

	return results
}

// isFieldWidget reports whether a Widget annotation participates in an
// interactive form field. A widget that carries a field type (/FT), a partial
// field name (/T, the merged terminal-field case), or a field parent (/Parent)
// is a field widget; a pure decorative widget is not. veraPDF's §8.10.2.3 t1
// exempts non-field widgets (isFieldWidget == false).
func isFieldWidget(ctx context.Context, doc *document.Document, annot ast.PdfDict) bool {
	for _, k := range []ast.PdfName{"FT", "Parent", "T"} {
		if _, ok := annot[k]; ok {
			return true
		}
	}
	return false
}

// widgetStructElem resolves the structure element that encloses a widget through
// its /StructParent index into the ParentTree (a widget's entry is a single
// element reference). It returns nil when the widget is not linked into the
// structure tree (veraPDF structParentType == null / isArtifact).
func widgetStructElem(ctx context.Context, doc *document.Document, treeRoot, annot ast.PdfDict) ast.PdfDict {
	if treeRoot == nil {
		return nil
	}
	spObj, ok := annot["StructParent"]
	if !ok {
		return nil
	}
	key, err := document.ResolveInt(ctx, doc, spObj)
	if err != nil {
		return nil
	}
	ptDict, err := document.ResolveDict(ctx, doc, treeRoot["ParentTree"])
	if err != nil || ptDict == nil {
		return nil
	}
	elemObj := numberTreeLookupObj(ctx, doc, ptDict, int(key), 0)
	if elemObj == nil {
		return nil
	}
	elem, err := document.ResolveDict(ctx, doc, elemObj)
	if err != nil {
		return nil
	}
	return elem
}

// widgetHasContents reports whether a widget carries a non-empty /Contents entry.
func widgetHasContents(ctx context.Context, doc *document.Document, annot ast.PdfDict) bool {
	contentsObj, ok := annot["Contents"]
	if !ok {
		return false
	}
	s, err := document.ResolveString(ctx, doc, contentsObj)
	return err == nil && len(s) > 0
}

// countOBJRWidgets counts OBJR children of a structure element that point to
// Widget annotations.
func countOBJRWidgets(ctx context.Context, doc *document.Document, elem ast.PdfDict) int {
	kObj, ok := elem["K"]
	if !ok {
		return 0
	}
	count := 0
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Single child.
		if isWidgetOBJR(ctx, doc, kObj) {
			return 1
		}
		return 0
	}
	for _, item := range arr {
		if isWidgetOBJR(ctx, doc, item) {
			count++
		}
	}
	return count
}

func isWidgetOBJR(ctx context.Context, doc *document.Document, obj ast.PdfObject) bool {
	d, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || d == nil {
		return false
	}
	typeName, _ := document.ResolveName(ctx, doc, d["Type"])
	if typeName != "OBJR" {
		return false
	}
	// Resolve the referenced object and check if it's a Widget.
	ref, ok := d["Obj"].(ast.PdfIndirectRef)
	if !ok {
		return false
	}
	annot, err := document.ResolveDict(ctx, doc, ref)
	if err != nil || annot == nil {
		return false
	}
	subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
	return subtype == "Widget"
}

// checkStructureDestinations validates PDF/UA-2 §8.8:
//   - t01: Destinations in outline items, OpenAction, and Link annotations
//     must be structure destinations (array starting with struct element ref).
//   - t02: GoTo action destinations must be structure destinations.
func checkStructureDestinations(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	var results []Violation

	// Check outline destinations (t01 for /Dest, t02 for /A GoTo).
	outlinesObj, ok := catalog["Outlines"]
	if ok {
		outlines, _ := document.ResolveDict(ctx, doc, outlinesObj)
		if outlines != nil {
			walkOutlineStructDest(ctx, doc, outlines["First"], &results, 0, make(map[int]bool))
		}
	}

	// Check catalog /OpenAction (t01).
	if oaObj, ok := catalog["OpenAction"]; ok {
		if !isStructureDestination(ctx, doc, oaObj) {
			// Could be an action dict. Check if GoTo.
			oaDict, _ := document.ResolveDict(ctx, doc, oaObj)
			if oaDict != nil {
				if s, _ := document.ResolveName(ctx, doc, oaDict["S"]); s == "GoTo" && !goToHasStructDest(ctx, doc, oaDict) {
					results = append(results, fail(Rule_14289_2_8_8_t02,
						"OpenAction GoTo destination is not a structure destination")...)
				}
			} else {
				// Direct destination array.
				results = append(results, fail(Rule_14289_2_8_8_t01,
					"OpenAction destination is not a structure destination")...)
			}
		}
	}

	// ISO 14289-2 §8.8 requires every intra-document destination to be a
	// structure destination, regardless of where it is reached from. That
	// covers Link /Dest entries and GoTo actions hanging off any
	// annotation's /A or /AA (additional-actions) entries.
	for _, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
		}
		// Page /AA additional-actions (the /O open and /C close triggers) can
		// carry a GoTo whose destination must also be a structure destination.
		for _, actObj := range pageActionObjects(ctx, doc, page) {
			act, err := document.ResolveDict(ctx, doc, actObj)
			if err != nil || act == nil {
				continue
			}
			if s, _ := document.ResolveName(ctx, doc, act["S"]); s == "GoTo" && !goToHasStructDest(ctx, doc, act) {
				results = append(results, fail(Rule_14289_2_8_8_t02,
					"page additional-action GoTo destination is not a structure destination")...)
			}
		}

		annots, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for _, aObj := range annots {
			annot, _ := document.ResolveDict(ctx, doc, aObj)
			if annot == nil {
				continue
			}
			if st, _ := document.ResolveName(ctx, doc, annot["Subtype"]); st == "Link" {
				if destObj, ok := annot["Dest"]; ok && !isStructureDestination(ctx, doc, destObj) {
					results = append(results, fail(Rule_14289_2_8_8_t01,
						"Link annotation destination is not a structure destination")...)
				}
			}
			for _, actObj := range actionObjects(ctx, doc, annot) {
				act, err := document.ResolveDict(ctx, doc, actObj)
				if err != nil || act == nil {
					continue
				}
				if s, _ := document.ResolveName(ctx, doc, act["S"]); s == "GoTo" && !goToHasStructDest(ctx, doc, act) {
					results = append(results, fail(Rule_14289_2_8_8_t02,
						"annotation GoTo destination is not a structure destination")...)
				}
			}
		}
	}

	return results
}

// pageActionObjects collects a page's additional-action entries. Per ISO
// 32000-2 Table 197 a page dictionary defines only the /O (open) and /C (close)
// triggers; other /AA keys are annotation triggers that do not name a page
// action, so veraPDF does not treat their values as triggerable actions.
func pageActionObjects(ctx context.Context, doc *document.Document, page ast.PdfDict) []ast.PdfObject {
	aa, err := document.ResolveDict(ctx, doc, page["AA"])
	if err != nil || aa == nil {
		return nil
	}
	var out []ast.PdfObject
	for _, key := range []ast.PdfName{"O", "C"} {
		if v, ok := aa[key]; ok {
			out = append(out, v)
		}
	}
	return out
}

// actionObjects collects a dictionary's /A action plus every value of its /AA
// additional-actions dictionary. Used for annotations, whose /AA triggers are
// all valid actions.
func actionObjects(ctx context.Context, doc *document.Document, dict ast.PdfDict) []ast.PdfObject {
	var out []ast.PdfObject
	if a, ok := dict["A"]; ok {
		out = append(out, a)
	}
	if aa, err := document.ResolveDict(ctx, doc, dict["AA"]); err == nil && aa != nil {
		for _, v := range aa {
			out = append(out, v)
		}
	}
	return out
}

// walkOutlineStructDest checks one outline level's destinations, walking
// siblings iteratively via /Next and recursing into children via /First.
// visited tracks resolved item object numbers across the whole walk: a
// crafted /Next or /First cycle would otherwise recurse forever, and stack
// exhaustion is not recoverable in Go.
func walkOutlineStructDest(ctx context.Context, doc *document.Document, obj ast.PdfObject, results *[]Violation, depth int, visited map[int]bool) {
	if depth > maxActionChainDepth {
		return
	}
	for obj != nil {
		if ctx.Err() != nil {
			return
		}
		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			if visited[ref.ObjectNum] {
				return
			}
			visited[ref.ObjectNum] = true
		}

		item, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || item == nil {
			return
		}

		// Check /Dest on outline item (t01).
		if destObj, hasDest := item["Dest"]; hasDest {
			if !isStructureDestination(ctx, doc, destObj) {
				*results = append(*results, fail(Rule_14289_2_8_8_t01,
					"Outline item destination is not a structure destination")...)
			}
		}

		// Check /A action on outline item.
		if aObj, hasA := item["A"]; hasA {
			aDict, _ := document.ResolveDict(ctx, doc, aObj)
			if aDict != nil {
				s, _ := document.ResolveName(ctx, doc, aDict["S"])
				if s == "GoTo" {
					if !goToHasStructDest(ctx, doc, aDict) {
						*results = append(*results, fail(Rule_14289_2_8_8_t02,
							"Outline GoTo action destination is not a structure destination")...)
					}
				}
			}
		}

		// Recurse into children; siblings continue the loop.
		walkOutlineStructDest(ctx, doc, item["First"], results, depth+1, visited)

		obj = item["Next"]
	}
}

// isStructureDestination checks if a destination object is a "structure
// destination" per PDF 2.0: an array whose first element references a
// structure element (has /Type /StructElem or /S key) rather than a page.
// goToHasStructDest reports whether a GoTo action dict satisfies the
// PDF/UA-2 §8.8 t2 requirement: either the action carries an /SD entry
// (the PDF 2.0 dual-destination form, ISO 32000-2 Table 202, where /SD
// holds the structure destination) or its /D is itself a structure
// destination.
func goToHasStructDest(ctx context.Context, doc *document.Document, act ast.PdfDict) bool {
	if _, hasSD := act["SD"]; hasSD {
		return true
	}
	dObj, hasD := act["D"]
	if !hasD {
		return true // nothing to point anywhere; not a t2 case
	}
	return isStructureDestination(ctx, doc, dObj)
}

func isStructureDestination(ctx context.Context, doc *document.Document, destObj ast.PdfObject) bool {
	// Resolve named destination if it's a string/name.
	if nameObj, err := document.ResolveName(ctx, doc, destObj); err == nil && nameObj != "" {
		// Named destination. Look up in /Names/Dests.
		// For simplicity, assume named destinations are not structure destinations.
		return false
	}

	// Dictionary destination: a /SD entry makes it a structure
	// destination (ISO 32000-2 §12.3.2.3).
	if dict, err := document.ResolveDict(ctx, doc, destObj); err == nil && dict != nil {
		if _, hasSD := dict["SD"]; hasSD {
			return true
		}
	}

	arr, err := document.ResolveArray(ctx, doc, destObj)
	if err != nil || len(arr) == 0 {
		return false
	}

	// The first element of a structure destination is a reference to a
	// structure element dict (has /S key), not a page dict (has /Type /Page).
	firstDict, err := document.ResolveDict(ctx, doc, arr[0])
	if err != nil || firstDict == nil {
		return false
	}

	// Structure elements have /S key.
	if _, hasS := firstDict["S"]; hasS {
		return true
	}

	// Also check /Type /StructElem explicitly.
	typeName, _ := document.ResolveName(ctx, doc, firstDict["Type"])
	return typeName == "StructElem"
}
