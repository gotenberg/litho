package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// stripsActionType reports whether the fixer removes an action of type s under
// profile. The verdict is the validator's own ([compliance.IsForbiddenActionType]):
// a type outside the whitelist ISO 19005-1 6.6.1 test 1, ISO 19005-2/3 6.5.1
// test 1 or ISO 19005-4 6.6.1 test 1 states for the profile's part and
// conformance. An action the profile permits, such as JavaScript under
// PDF/A-4, is never stripped.
//
// The fixer keeps no list of its own. A hand list of forbidden types used to
// nominate what was stripped, and it had drifted from the whitelists: GoToE is
// outside the part 1 whitelist, RichMediaExecute and GoToDp outside the part 1
// to 3 ones, and the validator flagged all three while the fixer kept them.
//
// # Corpus evidence
//
//   - litho forbidden_mix.pdf: remediated as PDF/A-4, PDF/A-4e or PDF/A-4f, it
//     lost the /Names/JavaScript tree those profiles permit, because its other
//     forbidden actions ran this fixer. The residual rules were the same
//     either way.
//   - pdfjs issue17056.pdf, issue8844.pdf, issue18030.pdf: remediated as
//     PDF/A-1b, they kept 30, 1 and 1 GoToE link actions that litho and
//     veraPDF both report under 6.6.1 test 1. Stripped, neither reports any.
//   - verapdf PDF_UA-1__7.11-t01-fail-a.pdf, -fail-b.pdf, -pass-a.pdf and
//     PDF_UA-2__8.14.1-t01-fail-a.pdf: the same, one GoToE action each.
func stripsActionType(s ast.PdfName, profile string) bool {
	return compliance.IsForbiddenActionType(s, profile)
}

func matchJavaScript(v compliance.Violation) bool {
	return descriptionContains(v, "JavaScript") ||
		clausePrefix(v, "6.6.1", "6.5.1")
}

func fixJavaScript(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	var fixes []string
	catalogChanged := false
	s := newActionSplice(alloc, doc, profile)

	// Remove forbidden entries from catalog /AA.
	if aaObj, ok := catalogDict["AA"]; ok {
		catalogFixes, newCatalog := removeForbiddenFromAA(ctx, s, catalogDict, aaObj, "catalog")
		if newCatalog != nil {
			catalogDict = newCatalog
			catalogChanged = true
		}
		fixes = append(fixes, catalogFixes...)
	}

	// Cut the forbidden actions out of /OpenAction, removing it when none is
	// left.
	if oaObj, ok := catalogDict["OpenAction"]; ok {
		cut := s.cut
		value, keep, changed := s.entry(ctx, oaObj)
		if changed {
			catalogDict = maps.Clone(catalogDict)
			if keep {
				catalogDict["OpenAction"] = value
			} else {
				delete(catalogDict, "OpenAction")
			}
			catalogChanged = true
		}
		if fix := s.report(cut, keep, "removed forbidden /OpenAction from catalog", "/OpenAction of catalog"); fix != "" {
			fixes = append(fixes, fix)
		}
	}

	// Remove /Names/JavaScript from catalog. The tree's presence is the
	// violation only where the profile forbids JavaScript actions, which
	// compliance.checkJavaScriptNameTree mirrors: every PDF/A-4 conformance
	// permits them.
	if namesObj, ok := catalogDict["Names"]; ok && stripsActionType("JavaScript", profile) {
		if newCatalog, removed := removeNamesJavaScript(ctx, alloc, doc, catalogDict, namesObj); removed {
			if newCatalog != nil {
				catalogDict = newCatalog
				catalogChanged = true
			}
			fixes = append(fixes, "removed /Names/JavaScript from catalog")
		}
	}

	// Form fields carry their own /AA, which the validator judges through the
	// AcroForm field tree rather than through any page. The walk runs before
	// the catalog write because an inline field tree can only be written back
	// through the catalog.
	fieldFixes, newCatalog := fixForbiddenActionsOnFields(ctx, s, catalogDict)
	if newCatalog != nil {
		catalogDict = newCatalog
		catalogChanged = true
	}
	fixes = append(fixes, fieldFixes...)

	if catalogChanged {
		alloc.AllocAt(catalogRef.ObjectNum, catalogDict)
	}

	// Walk pages to remove forbidden actions from page /AA and annotations.
	pageFixes, err := fixForbiddenActionsOnPages(ctx, s, catalogDict)
	if err != nil {
		return fixes, fmt.Errorf("fixing forbidden actions on pages: %w", err)
	}
	fixes = append(fixes, pageFixes...)

	// Outlines live outside the page tree but can still carry forbidden
	// /A actions (PDF/A-2 §6.5.1 covers outline items too).
	outlineFixes := fixForbiddenActionsOnOutlines(ctx, s, catalogDict)
	fixes = append(fixes, outlineFixes...)

	return fixes, nil
}

// maxActionChainDepth bounds how deep [actionSplice] follows the /Next tree of
// one action (ISO 32000-1, 12.6.2, Table 193), which it walks recursively.
// Anti-DoS guard, not spec-mandated: the spec puts no length on a chain, and a
// crafted one of direct dictionaries nested thousands deep would otherwise grow
// the goroutine stack with it. Fan-out needs no cap of its own: the splice
// handles each indirect action once and reuses its result, so a /Next array
// that repeats an action, or a chain that loops back on itself, adds no work.
// The value is the depth compliance's walkActionChain judges to (its
// maxActionChainDepth), so the splice reaches every action the validator
// flags, and an action beyond it is neither flagged nor cut.
const maxActionChainDepth = 200

// actionSplice cuts the actions a profile forbids out of action trees. An
// action's /Next holds one action dictionary or an array of them, performed
// after it (ISO 32000-1, 12.6.2, Table 193), so the actions form a tree, and
// ISO 19005-1 6.6.1, ISO 19005-2/3 6.5.1 and ISO 19005-4 6.6.1 restrict every
// action in it: compliance's walkActionChain judges each one.
//
// A forbidden action is replaced, in its parent's sequence, by its own /Next
// actions, spliced in turn. The tree runs in pre-order (the action, then its
// /Next in order), so the permitted actions keep their order: a GoTo whose
// /Next is a JavaScript action keeps the GoTo, and a JavaScript action whose
// /Next is a GoTo leaves the GoTo in its place. Dropping the whole entry
// instead would be valid too, but it discards every permitted action behind
// the first forbidden one, such as the navigation of every outline item whose
// GoTo is followed by a script.
//
// Every action is read through the allocator. An indirect action is rewritten
// at its own object number, since its spliced form is the same whichever
// parent reaches it; a direct one is rebuilt in its parent. One splice serves a
// whole fixer run, so an action several parents share is spliced once. Not
// safe for concurrent use.
//
// # Corpus evidence
//
//   - pdfjs issue20914.pdf: 503 outline GoTo actions hold a JavaScript /Next.
//     Remediated as PDF/A-1b or PDF/A-2b, the output kept all 503, which
//     litho and veraPDF both report, while the fixer judged the first action
//     of a chain alone. Spliced, neither reports any, and all 683 outline
//     items keep their GoTo.
//   - All corpora: the 343 profile runs of files that hold a forbidden action
//     but no /Next chain remediate to the same residual rules and fix
//     descriptions as before the splice.
type actionSplice struct {
	alloc   *writer.ObjAllocator
	doc     *document.Document
	profile string

	// done holds, per indirect action spliced, the actions that replace it
	// in a parent's sequence: itself, or its successors when it is cut.
	done map[int]splicedAction
	// active holds the verdict of each indirect action whose splice is under
	// way, so a /Next cycle back to it ends there.
	active map[int]bool
	// cut counts the forbidden actions cut out of the entries spliced so far.
	// An action two entries share counts in each.
	cut int
}

// splicedAction is the outcome of splicing one action: the actions that take
// its place, whether they differ from the action itself, and whether the
// action itself is cut.
type splicedAction struct {
	actions []ast.PdfObject
	changed bool
	cut     bool
}

// newActionSplice returns an [actionSplice] for one run of the fixer.
func newActionSplice(alloc *writer.ObjAllocator, doc *document.Document, profile string) *actionSplice {
	return &actionSplice{
		alloc:   alloc,
		doc:     doc,
		profile: profile,
		done:    make(map[int]splicedAction),
		active:  make(map[int]bool),
	}
}

// entry splices the action tree an entry that holds one action holds (an /A,
// an /AA trigger, an /OpenAction), and returns the value the entry takes.
// keep is false when no action is left, and changed reports whether the value
// differs from obj. A value that is not an action dictionary, such as a
// destination array under /OpenAction, is left as it is.
//
// When several actions are left, the first becomes the entry's action with the
// others appended to its /Next, which performs them in the same order. It is a
// direct copy: another parent may share the original.
func (s *actionSplice) entry(ctx context.Context, obj ast.PdfObject) (value ast.PdfObject, keep, changed bool) {
	out := s.splice(ctx, obj, 0)
	if !out.changed {
		return obj, true, false
	}
	switch len(out.actions) {
	case 0:
		return nil, false, true
	case 1:
		return out.actions[0], true, true
	}
	// A leading entry that is no dictionary, such as a reference to a missing
	// object, performs nothing, and only a dictionary can head the entry.
	for i, obj := range out.actions {
		first := liveDictValue(ctx, s.alloc, s.doc, obj)
		if first == nil {
			continue
		}
		head := maps.Clone(first)
		head["Next"] = ast.PdfArray(slices.Concat(s.nextActions(ctx, first["Next"]), out.actions[i+1:]))
		return head, true, true
	}
	return out.actions[0], true, true
}

// report returns the fix description for one entry spliced when the count of
// cut actions stood at cut: removed when no action is left, a count of the
// actions cut out of at's chain when some are, and "" when it lost none.
func (s *actionSplice) report(cut int, keep bool, removed, at string) string {
	switch {
	case !keep:
		return removed
	case s.cut > cut:
		return fmt.Sprintf("cut %d forbidden action(s) out of the action chain at %s", s.cut-cut, at)
	}
	return ""
}

// splice returns the actions that replace obj in its parent's sequence. See
// [actionSplice].
func (s *actionSplice) splice(ctx context.Context, obj ast.PdfObject, depth int) splicedAction {
	unchanged := splicedAction{actions: []ast.PdfObject{obj}}
	ref, isRef := obj.(ast.PdfIndirectRef)
	isRef = isRef && ref.ObjectNum > 0
	if isRef {
		if out, ok := s.done[ref.ObjectNum]; ok {
			if out.cut {
				s.cut++
			}
			return out
		}
		// A /Next cycle came back to an action under way. A permitted one keeps
		// the reference, since it stays in the tree; a forbidden one is cut, so
		// the edge back to it goes.
		if forbidden, ok := s.active[ref.ObjectNum]; ok {
			if forbidden {
				return splicedAction{changed: true}
			}
			return unchanged
		}
	}
	if ctx.Err() != nil || depth > maxActionChainDepth {
		return unchanged
	}
	action := liveDictValue(ctx, s.alloc, s.doc, obj)
	if action == nil {
		return unchanged
	}

	forbidden := isForbiddenActionDict(ctx, s.doc, action, s.profile)
	if isRef {
		s.active[ref.ObjectNum] = forbidden
	}

	var next []ast.PdfObject
	nextChanged := false
	nextObj, hasNext := action["Next"]
	if hasNext {
		for _, item := range s.nextActions(ctx, nextObj) {
			out := s.splice(ctx, item, depth+1)
			next = append(next, out.actions...)
			nextChanged = nextChanged || out.changed
		}
	}

	out := unchanged
	switch {
	case forbidden:
		s.cut++
		out = splicedAction{actions: next, changed: true, cut: true}
	case nextChanged:
		rewritten := maps.Clone(action)
		switch len(next) {
		case 0:
			delete(rewritten, "Next")
		case 1:
			rewritten["Next"] = next[0]
		default:
			rewritten["Next"] = ast.PdfArray(next)
		}
		if isRef {
			s.alloc.AllocAt(ref.ObjectNum, rewritten)
		} else {
			out = splicedAction{actions: []ast.PdfObject{rewritten}, changed: true}
		}
	}

	if isRef {
		delete(s.active, ref.ObjectNum)
		s.done[ref.ObjectNum] = out
	}
	return out
}

// nextActions returns the actions a /Next value lists: the items of an array,
// read through the allocator, or the value itself.
func (s *actionSplice) nextActions(ctx context.Context, nextObj ast.PdfObject) []ast.PdfObject {
	switch v := nextObj.(type) {
	case nil:
		return nil
	case ast.PdfArray:
		return v
	case ast.PdfIndirectRef:
		if pending, ok := s.alloc.Pending()[v.ObjectNum]; ok {
			if arr, isArr := pending.(ast.PdfArray); isArr {
				return arr
			}
			return []ast.PdfObject{nextObj}
		}
		if arr, err := document.ResolveArray(ctx, s.doc, v); err == nil {
			return arr
		}
	}
	return []ast.PdfObject{nextObj}
}

// removeNamesJavaScript drops the /JavaScript name tree from the catalog's
// /Names dictionary, reading it through the allocator: catalog-cleanup
// precedes this fixer and writes an indirect /Names back at its own object
// number when it removes /AlternatePresentations, so a copy of the document's
// dictionary would resurrect that entry.
//
// An indirect /Names is written back at its own object number and the catalog
// is left alone, so removed reports true with a nil catalog. An inline one
// moves to a new object, and the returned catalog points at it.
func removeNamesJavaScript(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict, namesObj ast.PdfObject) (newCatalog ast.PdfDict, removed bool) {
	namesDict := liveDictValue(ctx, alloc, doc, namesObj)
	if namesDict == nil {
		return nil, false
	}
	if _, ok := namesDict["JavaScript"]; !ok {
		return nil, false
	}
	newNames := make(ast.PdfDict, len(namesDict))
	maps.Copy(newNames, namesDict)
	delete(newNames, "JavaScript")

	if ref, ok := namesObj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newNames)
		return nil, true
	}
	newCatalog = make(ast.PdfDict, len(catalogDict))
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Names"] = alloc.Alloc(newNames)
	return newCatalog, true
}

// fixForbiddenActionsOnOutlines walks the outline tree and cuts the actions
// profile forbids out of each item's /A (ISO 19005-2 §6.5.1).
func fixForbiddenActionsOnOutlines(ctx context.Context, s *actionSplice, catalog ast.PdfDict) []string {
	outlinesObj, ok := catalog["Outlines"]
	if !ok {
		return nil
	}
	visited := make(map[int]bool)
	var fixes []string
	walkOutlineItem(ctx, s, outlinesObj, visited, &fixes, 0)
	return fixes
}

// walkOutlineItem recurses into /First and /Next sibling chains, bounded
// by visited to break cycles in malformed outlines.
func walkOutlineItem(ctx context.Context, s *actionSplice, obj ast.PdfObject, visited map[int]bool, fixes *[]string, depth int) {
	if ctx.Err() != nil || depth > maxPageTreeDepth {
		return
	}
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if visited[ref.ObjectNum] {
			return
		}
		visited[ref.ObjectNum] = true
	}

	var dict ast.PdfDict
	if isRef && ref.ObjectNum > 0 {
		dict = resolveFromAllocOrDoc(ctx, s.alloc, s.doc, ref)
	}
	if dict == nil {
		resolved, err := document.ResolveDict(ctx, s.doc, obj)
		if err != nil || resolved == nil {
			return
		}
		dict = resolved
	}

	if aObj, hasA := dict["A"]; hasA {
		cut := s.cut
		value, keep, changed := s.entry(ctx, aObj)
		if changed {
			dict = maps.Clone(dict)
			if keep {
				dict["A"] = value
			} else {
				delete(dict, "A")
			}
			if isRef && ref.ObjectNum > 0 {
				s.alloc.AllocAt(ref.ObjectNum, dict)
			}
		}
		if fix := s.report(cut, keep, "removed forbidden /A action from outline item", "/A of outline item"); fix != "" {
			*fixes = append(*fixes, fix)
		}
	}

	if first, ok := dict["First"]; ok {
		walkOutlineItem(ctx, s, first, visited, fixes, depth+1)
	}
	if next, ok := dict["Next"]; ok {
		walkOutlineItem(ctx, s, next, visited, fixes, depth+1)
	}
}

// fixForbiddenActionsOnPages walks the page tree and removes forbidden /A and
// /AA entries from page dicts and their annotations.
func fixForbiddenActionsOnPages(ctx context.Context, s *actionSplice, catalog ast.PdfDict) ([]string, error) {
	alloc, doc := s.alloc, s.doc
	pagesObj, ok := catalog["Pages"]
	if !ok {
		return nil, nil
	}

	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool), 0)

	var fixes []string
	for i, pe := range pages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		pageDict := pe.dict
		pageNum := i + 1

		// Remove forbidden entries from page /AA.
		if aaObj, ok := pageDict["AA"]; ok {
			pageFixes, newPage := removeForbiddenFromAA(ctx, s, pageDict, aaObj, fmt.Sprintf("page %d", pageNum))
			if newPage != nil {
				pageDict = newPage
				// A page written directly in its parent's /Kids is written
				// back there ([writeTreeNode]).
				writeTreeNode(ctx, alloc, doc, pe, pageDict)
			}
			fixes = append(fixes, pageFixes...)
		}

		// Walk annotations and remove forbidden /A and /AA. The array is read
		// alloc-first: forbidden-annots precedes this fixer and filters
		// /Annots, and an annotation it dropped must not be rewritten here.
		annotsObj, ok := pageDict["Annots"]
		if !ok {
			continue
		}

		annots := liveArray(ctx, alloc, doc, annotsObj)
		if len(annots) == 0 {
			continue
		}

		var inlineFixed map[int]ast.PdfDict
		var inlineFixes []string
		for i, annotObj := range annots {
			var annotRef ast.PdfIndirectRef
			if r, ok := annotObj.(ast.PdfIndirectRef); ok {
				annotRef = r
			}

			var annotDict ast.PdfDict
			if annotRef.ObjectNum > 0 {
				annotDict = resolveFromAllocOrDoc(ctx, alloc, doc, annotRef)
			}
			if annotDict == nil {
				resolved, err := document.ResolveDict(ctx, doc, annotObj)
				if err != nil || resolved == nil {
					continue
				}
				annotDict = resolved
			}

			newAnnot, annotFixes := stripAnnotationActions(ctx, s, annotDict, pageNum)
			if newAnnot == nil {
				// Only actions rewritten at their own object numbers, if any,
				// which have been written already.
				fixes = append(fixes, annotFixes...)
				continue
			}
			if annotRef.ObjectNum > 0 {
				alloc.AllocAt(annotRef.ObjectNum, newAnnot)
				fixes = append(fixes, annotFixes...)
				continue
			}
			// ISO 32000-1, 12.5.2 does not require an annotation to be an
			// indirect object. One written inline in /Annots has no object
			// number to address, so the repair was reported and never
			// written. It goes back through the array, and is reported only
			// once that write lands.
			if inlineFixed == nil {
				inlineFixed = make(map[int]ast.PdfDict, 1)
			}
			inlineFixed[i] = newAnnot
			inlineFixes = append(inlineFixes, annotFixes...)
		}
		if installInlineAnnots(ctx, alloc, doc, pe, annotsObj, annots, inlineFixed) > 0 {
			fixes = append(fixes, inlineFixes...)
		}
	}

	return fixes, nil
}

// stripAnnotationActions returns a copy of annotDict with the forbidden actions
// cut out of its /A and /AA, with the fix descriptions, or nil when annotDict
// itself needs no change. The descriptions also cover the actions cut from
// indirect actions it reaches, which [actionSplice] rewrites in place.
func stripAnnotationActions(ctx context.Context, s *actionSplice, annotDict ast.PdfDict, pageNum int) (ast.PdfDict, []string) {
	var newAnnot ast.PdfDict
	var fixes []string
	clone := func() {
		if newAnnot == nil {
			newAnnot = maps.Clone(annotDict)
		}
	}

	if aObj, hasA := annotDict["A"]; hasA {
		cut := s.cut
		value, keep, changed := s.entry(ctx, aObj)
		if changed {
			clone()
			if keep {
				newAnnot["A"] = value
			} else {
				delete(newAnnot, "A")
			}
		}
		if fix := s.report(cut, keep, fmt.Sprintf("removed forbidden /A action from annotation on page %d", pageNum), fmt.Sprintf("/A of annotation on page %d", pageNum)); fix != "" {
			fixes = append(fixes, fix)
		}
	}

	if aaObj, hasAA := annotDict["AA"]; hasAA {
		aaFixes, cleaned := removeForbiddenFromAADict(ctx, s, aaObj, fmt.Sprintf("annotation on page %d", pageNum))
		if cleaned != nil {
			clone()
			if len(cleaned) == 0 {
				delete(newAnnot, "AA")
			} else {
				newAnnot["AA"] = cleaned
			}
		}
		fixes = append(fixes, aaFixes...)
	}
	return newAnnot, fixes
}

// maxFieldTreeDepth bounds the descent of the AcroForm field tree through /Kids
// in [fieldActionWalk]. Anti-DoS guard, not spec-mandated: ISO 32000-1,
// 12.7.3.1 puts no depth on the field tree, and the visited set is what ends a
// cyclic /Kids. The value is the bound compliance's walkFormFields descends to
// (its maxActionChainDepth), so the fixer reaches every field whose actions
// the validator judges. [maxFieldParentDepth] bounds the upward /Parent walk,
// a different shape.
const maxFieldTreeDepth = 200

// fixForbiddenActionsOnFields strips the forbidden actions from the /AA of
// every form field the AcroForm /Fields tree lists (ISO 19005-1 6.6.1, ISO
// 19005-2/3 6.5.1, ISO 19005-4 6.6.1: the action-type restrictions reach every
// action in the file). The page walk cannot reach them: a non-terminal field
// is no annotation, and a terminal one merged with its widget is judged there
// under the annotation triggers alone.
//
// Every level is read through the allocator and written back where it lives:
// an indirect field or /Kids array at its own object number, an inline one
// into its parent. It returns the catalog to write when the AcroForm
// dictionary is inline in it and changed, or nil.
//
// # Corpus evidence
//
//   - litho annots_1b.pdf: the non-terminal field "parent" holds a JavaScript
//     action under /AA/K, and at PDF/A-2b the output kept 6.5.1 test 1 and
//     6.4.1 test 2 for it.
//   - pdfjs issue15092.pdf, issue16863.pdf, issue19753.pdf: remediated as
//     PDF/A-2b they kept 3, 8 and 18 field actions under 6.5.1 test 1, and 1,
//     4 and 10 fields under 6.4.1 test 2, as PDF/A-1b the same counts under
//     6.6.1 test 1 and 6.6.2 test 2. The walk clears every one, in litho and
//     in veraPDF, since each /AA held nothing else.
func fixForbiddenActionsOnFields(ctx context.Context, s *actionSplice, catalog ast.PdfDict) ([]string, ast.PdfDict) {
	return rewriteFieldAA(ctx, s.alloc, s.doc, catalog, func(ctx context.Context, aaObj ast.PdfObject, location string) ([]string, ast.PdfDict) {
		return removeForbiddenFromAADict(ctx, s, aaObj, location)
	})
}

// rewriteFieldAA walks every form field the AcroForm /Fields tree lists and
// replaces each one's /AA with what cleanAA returns for it (see
// [fieldActionWalk]). It returns the fix descriptions and the catalog to write
// when the AcroForm dictionary is inline in it and changed, or nil.
func rewriteFieldAA(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, cleanAA fieldAACleaner) ([]string, ast.PdfDict) {
	acroFormObj, ok := catalog["AcroForm"]
	if !ok {
		return nil, nil
	}
	acroForm := liveDictValue(ctx, alloc, doc, acroFormObj)
	if acroForm == nil {
		return nil, nil
	}
	fieldsObj, ok := acroForm["Fields"]
	if !ok {
		return nil, nil
	}

	w := fieldActionWalk{alloc: alloc, doc: doc, cleanAA: cleanAA, seen: make(map[int]bool)}
	newFields, changed := w.array(ctx, fieldsObj, true, 0)
	if !changed {
		return w.fixes, nil
	}
	newAcroForm := maps.Clone(acroForm)
	newAcroForm["Fields"] = newFields
	if ref, isRef := acroFormObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newAcroForm)
		return w.fixes, nil
	}
	newCatalog := maps.Clone(catalog)
	newCatalog["AcroForm"] = newAcroForm
	return w.fixes, newCatalog
}

// fieldAACleaner returns the fix descriptions for one form field's /AA entry
// aaObj and the /AA the field keeps: an empty dictionary removes the entry,
// and nil leaves the field as it is. location names the field in the fixes.
type fieldAACleaner func(ctx context.Context, aaObj ast.PdfObject, location string) ([]string, ast.PdfDict)

// fieldActionWalk carries the state of one [rewriteFieldAA] walk. seen
// processes each field object once, so a shared or cyclic /Kids tree cannot
// force exponential work or loop.
type fieldActionWalk struct {
	alloc   *writer.ObjAllocator
	doc     *document.Document
	cleanAA fieldAACleaner
	seen    map[int]bool
	fixes   []string
}

// array walks one /Fields or /Kids array. It returns the value the parent
// holds and true when that value changed: an indirect array is written at its
// own object number and the parent keeps its reference.
//
// listedByField says whether arrObj is the /Fields array or a form field's
// /Kids, the only lists compliance's walkFormFields draws form fields from.
func (w *fieldActionWalk) array(ctx context.Context, arrObj ast.PdfObject, listedByField bool, depth int) (ast.PdfObject, bool) {
	if ctx.Err() != nil || depth > maxFieldTreeDepth {
		return arrObj, false
	}
	arr := liveArray(ctx, w.alloc, w.doc, arrObj)
	var newArr ast.PdfArray
	for i, item := range arr {
		if ctx.Err() != nil {
			break
		}
		ref, isRef := item.(ast.PdfIndirectRef)
		if isRef {
			if w.seen[ref.ObjectNum] {
				continue
			}
			w.seen[ref.ObjectNum] = true
		}
		field := liveDictValue(ctx, w.alloc, w.doc, item)
		if field == nil {
			continue
		}
		location := "inline form field"
		if isRef {
			location = fmt.Sprintf("form field %d", ref.ObjectNum)
		}
		newField := w.field(ctx, field, location, listedByField, depth)
		if newField == nil {
			continue
		}
		if isRef && ref.ObjectNum > 0 {
			w.alloc.AllocAt(ref.ObjectNum, newField)
			continue
		}
		if newArr == nil {
			newArr = slices.Clone(arr)
		}
		newArr[i] = newField
	}
	if newArr == nil {
		return arrObj, false
	}
	if ref, isRef := arrObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		w.alloc.AllocAt(ref.ObjectNum, newArr)
		return arrObj, false
	}
	return newArr, true
}

// field returns a copy of one field dictionary with the /AA the walk's cleaner
// leaves and with its rewritten /Kids, or nil when neither changed.
//
// Only a form field's /AA is judged, mirroring compliance's walkFormFields:
// every dictionary /Fields lists, and below a field only the kids carrying a
// non-null partial name /T (ISO 32000-1, 12.7.3.1; a null entry is absent per
// 7.3.7). Any other kid is a widget, whose /AA the page walk judges as an
// annotation's.
func (w *fieldActionWalk) field(ctx context.Context, field ast.PdfDict, location string, listedByField bool, depth int) ast.PdfDict {
	isField := listedByField && (depth == 0 || hasNonNullEntry(field, "T"))

	var newField ast.PdfDict
	if aaObj, hasAA := field["AA"]; hasAA && isField {
		aaFixes, cleaned := w.cleanAA(ctx, aaObj, location)
		if cleaned != nil {
			newField = maps.Clone(field)
			if len(cleaned) == 0 {
				delete(newField, "AA")
			} else {
				newField["AA"] = cleaned
			}
		}
		w.fixes = append(w.fixes, aaFixes...)
	}

	if kidsObj, hasKids := field["Kids"]; hasKids {
		if newKids, changed := w.array(ctx, kidsObj, isField, depth+1); changed {
			if newField == nil {
				newField = maps.Clone(field)
			}
			newField["Kids"] = newKids
		}
	}
	return newField
}

// hasNonNullEntry reports whether dict holds key with a value other than the
// null object, which ISO 32000-1, 7.3.7 makes equivalent to an absent entry.
func hasNonNullEntry(dict ast.PdfDict, key ast.PdfName) bool {
	v, ok := dict[key]
	if !ok {
		return false
	}
	_, isNull := v.(ast.PdfNull)
	return !isNull
}

// isForbiddenActionDict reports whether profile forbids the action d is, by
// its type (see [stripsActionType]) or its name. The caller reads d through
// the allocator, so the verdict judges the action an earlier fixer left. Only
// the action itself is judged: [actionSplice] walks its /Next.
func isForbiddenActionDict(ctx context.Context, doc *document.Document, d ast.PdfDict, profile string) bool {
	// A /S that is absent or no name gives "", the type of no whitelist, which
	// the validator reports as veraPDF does (ISO 32000-1, Table 193 requires
	// /S), so the error is the verdict and the action goes: it performs
	// nothing a viewer can name.
	s, _ := document.ResolveName(ctx, doc, d["S"])
	if stripsActionType(s, profile) {
		return true
	}
	// A Named action is forbidden unless its /N is one of the standard named
	// actions, in every PDF/A part: ISO 19005-1 6.6.1 test 2, ISO 19005-2/3
	// 6.5.1 test 2, ISO 19005-4 6.6.1 test 2. Mirror the validator (which
	// reuses the same compliance.AllowedNamedActions set): only a present,
	// non-standard /N is a violation, so a malformed action without /N is left
	// alone.
	if s == "Named" && compliance.PDFAPart(profile) != 0 {
		if n, _ := document.ResolveName(ctx, doc, d["N"]); n != "" {
			return !compliance.AllowedNamedActions[n]
		}
	}
	return false
}

// removeForbiddenFromAA cuts the forbidden actions out of the /AA dict on the
// given parent dict. Returns the fix descriptions and a new parent dict when
// the /AA dict changed, or nil when it did not; the descriptions also cover
// the actions cut from indirect actions, which [actionSplice] rewrites in
// place.
func removeForbiddenFromAA(ctx context.Context, s *actionSplice, parentDict ast.PdfDict, aaObj ast.PdfObject, location string) ([]string, ast.PdfDict) {
	aaFixes, cleaned := removeForbiddenFromAADict(ctx, s, aaObj, location)
	if cleaned == nil {
		return aaFixes, nil
	}

	newParent := make(ast.PdfDict, len(parentDict))
	maps.Copy(newParent, parentDict)
	if len(cleaned) == 0 {
		delete(newParent, "AA")
	} else {
		newParent["AA"] = cleaned
	}
	return aaFixes, newParent
}

// removeForbiddenFromAADict resolves an /AA dict and cuts the forbidden actions
// out of the action tree under each entry, removing an entry left with none.
// Returns fix descriptions and the cleaned dict, or nil when the dict itself
// needs no change. The dict is read alloc-first: the cleaned copy replaces it
// in the parent, so a copy of the document's version would discard an earlier
// fixer's edit to an indirect /AA.
func removeForbiddenFromAADict(ctx context.Context, s *actionSplice, aaObj ast.PdfObject, location string) ([]string, ast.PdfDict) {
	aaDict := liveDictValue(ctx, s.alloc, s.doc, aaObj)
	if aaDict == nil {
		return nil, nil
	}

	// Sorted: the caller turns these into report lines, and ranging the
	// dictionary ordered them randomly between runs of the same file.
	var cleaned ast.PdfDict
	var fixes []string
	for _, k := range slices.Sorted(maps.Keys(aaDict)) {
		if ctx.Err() != nil {
			break
		}
		cut := s.cut
		value, keep, changed := s.entry(ctx, aaDict[k])
		if changed {
			if cleaned == nil {
				cleaned = maps.Clone(aaDict)
			}
			if keep {
				cleaned[k] = value
			} else {
				delete(cleaned, k)
			}
		}
		if fix := s.report(cut, keep, fmt.Sprintf("removed forbidden /AA/%s action from %s", string(k), location), fmt.Sprintf("/AA/%s of %s", string(k), location)); fix != "" {
			fixes = append(fixes, fix)
		}
	}
	return fixes, cleaned
}
