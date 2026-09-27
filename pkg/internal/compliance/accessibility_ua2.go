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
	// veraPDF scopes the rule to its SEForm object, built for an element whose
	// standard type is Form, and types a widget's Lbl kid for §8.10.2.3 the same
	// way, so both lookups resolve types through the tree's role map.
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	typeState := &walkState{doc: doc, roleMap: buildRoleMapIndex(ctx, doc, treeRoot)}
	if err == nil && treeRoot != nil {
		walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
			typeName, _ := document.ResolveName(ctx, doc, elem["S"])
			if iso32005Key(ctx, typeState, elem, typeName) != "Form" {
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
	nsResolver := newNSRoleResolver(ctx, doc, treeRoot)
	// Each widget is judged in the unit veraPDF judges it in
	// ([judgedAnnotations]): a widget object two pages list fails once.
	for _, pa := range judgedAnnotations(ctx, doc, collectPages(ctx, doc, catalog)) {
		if ctx.Err() != nil {
			return results
		}
		annot := pa.annot
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
		// structParentType == null / isArtifact: a widget with no enclosing
		// element in the structure parent tree, or one under an Artifact, is
		// out of scope.
		structElem, underArtifact := annotStructScope(ctx, doc, treeRoot, nsResolver, annot)
		if structElem == nil || underArtifact {
			continue
		}
		// containsLbl: a Lbl kid of the enclosing element supplies the label.
		if hasLabelKid(ctx, typeState, structElem) {
			continue
		}
		// /Contents supplies description and context for the widget.
		if widgetHasContents(ctx, doc, annot) {
			continue
		}
		results = append(results, fail(Rule_14289_2_8_10_2_3_t01,
			"field Widget annotation has neither a Lbl structure element nor a /Contents entry")...)
	}

	return results
}

// hasLabelKid reports whether a structure element has a kid whose standard type
// is Lbl and that has content of its own, which is how veraPDF decides
// containsLbl for ISO 14289-2 §8.10.2.3 t1 (GFPDWidgetAnnot.getcontainsLbl).
// The kid is typed through the /RoleMap first, as [iso32005Key] types it, so a
// custom label mapped to Lbl labels the widget and a Lbl mapped away does not;
// an empty Lbl labels nothing.
func hasLabelKid(ctx context.Context, state *walkState, elem ast.PdfDict) bool {
	kObj, ok := elem["K"]
	if !ok {
		return false
	}
	items, err := document.ResolveArray(ctx, state.doc, kObj)
	if err != nil {
		items = ast.PdfArray{kObj}
	}
	for _, item := range items {
		kid, kerr := document.ResolveDict(ctx, state.doc, item)
		if kerr != nil || kid == nil {
			continue
		}
		s, serr := document.ResolveName(ctx, state.doc, kid["S"])
		if serr != nil || s == "" {
			continue
		}
		if iso32005Key(ctx, state, kid, s) == "Lbl" && structElemHasKids(ctx, state.doc, kid) {
			return true
		}
	}
	return false
}

// structElemHasKids reports whether a structure element's /K holds at least one
// kid: a marked-content identifier, a marked-content or object reference, or a
// structure element (ISO 32000-1, 14.7.2, Table 323), with or without its /S.
// veraPDF's getChildren counts any dictionary whose /Type is absent or
// StructElem as a structure element (TaggedPDFHelper.isStructElem), so a Lbl
// whose only kid carries no /S still labels its widget. Measured.
func structElemHasKids(ctx context.Context, doc *document.Document, elem ast.PdfDict) bool {
	kObj, ok := elem["K"]
	if !ok {
		return false
	}
	items, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		items = ast.PdfArray{kObj}
	}
	for _, item := range items {
		resolved, rerr := document.ResolveObject(ctx, doc, item)
		if rerr != nil || resolved == nil {
			continue
		}
		switch v := resolved.(type) {
		case ast.PdfInt:
			return true
		case ast.PdfDict:
			switch t, _ := document.ResolveName(ctx, doc, v["Type"]); t {
			case "", "StructElem", "MCR", "OBJR":
				return true
			}
		}
	}
	return false
}

// isFieldWidget reports whether a Widget annotation participates in an
// interactive form field, as veraPDF's GFPDWidgetAnnot.isFieldWidget decides: the
// widget is merged with its field (it carries the partial field name /T, the
// field test of PDFormField.isField) or it has a field /Parent. A field type
// /FT alone makes no field widget, and a /T or /Parent written as null is
// absent (ISO 32000-1, 7.3.7). A /T naming any object counts ([knownKey]); a
// /Parent counts only by what it resolves to. veraPDF's ISO 14289-2 §8.10.1 t1
// and §8.10.2.3 t1 exempt non-field widgets (isFieldWidget == false). Measured
// against veraPDF.
func isFieldWidget(ctx context.Context, doc *document.Document, annot ast.PdfDict) bool {
	return knownKey(annot, "T") || nonNullKeyPresent(ctx, doc, annot, "Parent")
}

// annotStructParent resolves the structure element that encloses an annotation
// through its /StructParent index into the structure parent tree (ISO 32000-1,
// 14.7.4.4), where an annotation's entry is a single element reference unlike a
// page's per-MCID array. veraPDF's GFPDAnnot.getParentDictionary reads the
// parent this way and no other, so an annotation an OBJR references without a
// /StructParent has none. It returns nil when the annotation is not linked into
// the structure tree (veraPDF structParentType == null).
func annotStructParent(ctx context.Context, doc *document.Document, treeRoot, annot ast.PdfDict) ast.PdfDict {
	elemObj := annotStructParentObj(ctx, doc, treeRoot, annot)
	if elemObj == nil {
		return nil
	}
	elem, err := document.ResolveDict(ctx, doc, elemObj)
	if err != nil {
		return nil
	}
	return elem
}

// annotStructParentObj returns the structure parent tree entry an annotation's
// /StructParent names, unresolved, so a caller can tell an indirect element from
// a direct one. It returns nil when there is none.
func annotStructParentObj(ctx context.Context, doc *document.Document, treeRoot, annot ast.PdfDict) ast.PdfObject {
	if treeRoot == nil {
		return nil
	}
	if _, ok := annot["StructParent"]; !ok {
		return nil
	}
	elem, _ := structParentEntry(ctx, doc, treeRoot, annot)
	return elem
}

// linkTargetConflict judges ISO 14289-2, 8.2.5.20 t2 for one Link annotation:
// Link annotations that target different locations shall be in separate
// structure elements. It records the structure destination the annotation
// targets ([linkAnnotDest]) under the element its /StructParent names, and
// reports whether an earlier Link annotation under that element targeted a
// different one. This is veraPDF's PDLinkAnnot differentTargetAnnotObjectKey,
// computed in page and /Annots order across the document, so every annotation
// after the first disagreeing one is reported, whatever the element's type. An
// annotation without a structure destination, or whose parent tree entry is not
// an indirect dictionary, is neither judged nor recorded. seen maps a parent's
// object number to the destinations recorded under it.
func linkTargetConflict(ctx context.Context, doc *document.Document, treeRoot, annot ast.PdfDict, seen map[int]map[string]bool) bool {
	ref, ok := annotStructParentObj(ctx, doc, treeRoot, annot).(ast.PdfIndirectRef)
	if !ok {
		return false
	}
	if parent, err := document.ResolveDict(ctx, doc, ref); err != nil || parent == nil {
		return false
	}
	dest := linkAnnotDest(ctx, doc, annot)
	if dest == "" {
		return false
	}
	dests := seen[ref.ObjectNum]
	if dests == nil {
		dests = make(map[string]bool)
		seen[ref.ObjectNum] = dests
	}
	conflict := len(dests) > 1 || (len(dests) == 1 && !dests[dest])
	dests[dest] = true
	return conflict
}

// widgetHasContents reports whether a widget carries a /Contents text string,
// empty or not. veraPDF's ISO 14289-2 §8.10.2.3 t1 test reads Contents != null,
// and its getStringKey returns null only for an absent or non-string entry, so
// an empty /Contents counts.
func widgetHasContents(ctx context.Context, doc *document.Document, annot ast.PdfDict) bool {
	// /Contents counts only as a string (stringKeyPresent): veraPDF reads it
	// with getStringKey, so a null or name value is absent, measured.
	return stringKeyPresent(ctx, doc, annot, "Contents")
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
//
// # Corpus evidence
//
//   - pdfjs issue16839.pdf: writes /OpenAction 0 0 R, the free-list head, and
//     veraPDF builds no destination from it.
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

	// Check catalog /OpenAction (t01). A value that resolves to null is no
	// entry at all: ISO 32000-1, 7.3.7 treats a null dictionary value as an
	// absent entry, and 7.3.10 makes a reference to an undefined object a
	// reference to null, and veraPDF builds no destination from it.
	oaObj, hasOpenAction := catalog["OpenAction"]
	if hasOpenAction {
		if resolved, err := document.ResolveObject(ctx, doc, oaObj); err == nil && isNullObject(resolved) {
			hasOpenAction = false
		}
	}
	if hasOpenAction {
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
	// annotation's /A or /AA (additional-actions) entries. Each annotation is
	// judged in the unit veraPDF judges it in ([judgedAnnotations]): an
	// annotation object two pages list reaches its destination once.
	annotsSeen := make(map[int]bool)
	for pageIdx, page := range collectPages(ctx, doc, catalog) {
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

		for _, pa := range judgedPageAnnotations(ctx, doc, page, pageIdx, annotsSeen) {
			annot := pa.annot
			if st, _ := document.ResolveName(ctx, doc, annot["Subtype"]); st == "Link" {
				// A /Dest written as null is absent (ISO 32000-1, 7.3.7): veraPDF builds
				// no destination from it, measured.
				if knownKey(annot, "Dest") && !isStructureDestination(ctx, doc, annot["Dest"]) {
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

		// Check /Dest on outline item (t01). A /Dest written as null is absent
		// (ISO 32000-1, 7.3.7), measured.
		if knownKey(item, "Dest") {
			if !isStructureDestination(ctx, doc, item["Dest"]) {
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

// goToHasStructDest reports whether a GoTo action dict satisfies the PDF/UA-2
// §8.8 t2 requirement, as veraPDF's PDAction.containsStructureDestination
// decides: the action carries a non-null /SD (the PDF 2.0 structure
// destination, ISO 32000-2 Table 202), or its /D is a name or string naming a
// structure destination. An explicit /D array is no structure destination here,
// and neither is an action with no /D at all. Measured on each.
func goToHasStructDest(ctx context.Context, doc *document.Document, act ast.PdfDict) bool {
	if knownKey(act, "SD") {
		return true
	}
	dObj, ok := act["D"]
	if !ok {
		return false
	}
	d, err := document.ResolveObject(ctx, doc, dObj)
	if err != nil {
		return false
	}
	switch d.(type) {
	case ast.PdfName, ast.PdfString:
		return isStructureDestination(ctx, doc, d)
	}
	return false
}

// isStructureDestination reports whether a destination is a structure
// destination, as veraPDF's PDDestination.getIsStructDestination decides for
// ISO 14289-2, 8.8 t1. A string names an entry of the /Names /Dests tree and a
// name an entry of the catalog /Dests dictionary (ISO 32000-1, 12.3.2.3), each
// judged by the value it maps to. A dictionary is one when its /SD is non-null,
// and an array when its first element carries a non-null /S, as a structure
// element does (ISO 32000-2, 12.3.2.2). Measured on each form.
func isStructureDestination(ctx context.Context, doc *document.Document, destObj ast.PdfObject) bool {
	dest, err := document.ResolveObject(ctx, doc, destObj)
	if err != nil {
		return false
	}
	switch v := dest.(type) {
	case ast.PdfString:
		dest = namedDestValue(ctx, doc, string(v), false)
	case ast.PdfName:
		dest = namedDestValue(ctx, doc, string(v), true)
	}
	switch v := dest.(type) {
	case ast.PdfDict:
		return knownKey(v, "SD")
	case ast.PdfArray:
		if len(v) == 0 {
			return false
		}
		first, err := document.ResolveDict(ctx, doc, v[0])
		return err == nil && first != nil && knownKey(first, "S")
	}
	return false
}

// namedDestValue returns the resolved value a named destination maps to, or nil
// when there is none. veraPDF looks a string up in the /Names /Dests name tree
// only, and a name in the catalog /Dests dictionary (PDF 1.1) only, so a string
// that only the catalog /Dests holds names nothing. Measured on both.
func namedDestValue(ctx context.Context, doc *document.Document, key string, legacy bool) ast.PdfObject {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		doc.Logger.Debug("compliance: structure destination: cannot resolve catalog", "error", err)
		return nil
	}
	var val ast.PdfObject
	if legacy {
		// A missing or non-dictionary /Dests names no destination.
		dests, _ := document.ResolveDict(ctx, doc, catalog.Dict["Dests"])
		val = dests[ast.PdfName(key)]
	} else {
		// A missing or non-dictionary /Names names no destination.
		names, _ := document.ResolveDict(ctx, doc, catalog.Dict["Names"])
		if names == nil {
			return nil
		}
		val, err = document.LookupNameTree(ctx, doc, names["Dests"], key)
		if err != nil {
			doc.Logger.Debug("compliance: structure destination: name tree lookup failed", "error", err)
			return nil
		}
	}
	if val == nil {
		return nil
	}
	resolved, err := document.ResolveObject(ctx, doc, val)
	if err != nil {
		return nil
	}
	return resolved
}
