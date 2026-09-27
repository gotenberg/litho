package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// Annotation flag bits, ISO 32000-1, 12.5.3, Table 165.
const (
	annotFlagInvisible    = 1 << 0 // bit 1
	annotFlagHidden       = 1 << 1 // bit 2
	annotFlagPrint        = 1 << 2 // bit 3
	annotFlagNoView       = 1 << 5 // bit 6
	annotFlagToggleNoView = 1 << 8 // bit 9
)

// annotFlagsForbidden are the flags ISO 19005-2 to ISO 19005-4, 6.3.2 require
// clear. ISO 19005-1, 6.5.3 names the first three: ToggleNoView postdates PDF
// 1.4, which reads nothing in bit 9.
const annotFlagsForbidden = annotFlagHidden | annotFlagInvisible | annotFlagNoView | annotFlagToggleNoView

// ISO 19005-2, 19005-3 and 19005-4 number the annotation /F flag 6.3.2. ISO
// 19005-1 gives that number to the font dictionary keys and numbers its own
// annotation /F rule 6.5.3, which the description arm claims instead.
func matchAnnotFlags(v compliance.Violation) bool {
	return (clausePrefix(v, "6.3.2") && v.Rule.Specification != "ISO_19005_1") ||
		(descriptionContains(v, "annotation") && descriptionContains(v, "F key"))
}

// fixAnnotFlags answers the annotation flag rules, ISO 19005-1, 6.5.3 and ISO
// 19005-2 to ISO 19005-4, 6.3.2: every annotation carries /F, a pop-up exempt
// from part 2, with Print set and Hidden, Invisible, NoView and ToggleNoView
// clear.
//
// An annotation that shows neither on screen nor in print is removed (see
// [neverShownReason]): conforming flags would put on the page content its
// author kept off it. Its pop-up and its replies go with it (see
// [planNeverShown]), a widget takes its form field when it was the field's only
// one (see [pruneRemovedWidgets]), and on a tagged document its structure
// element and its /ParentTree entry go too
// ([mutations.UntagRemovedAnnotations]). The removal answers a PDF/A profile
// alone: ISO 14289-1 and ISO 14289-2 do not forbid these flags, and a signed
// signature field keeps its widget (see [signedSignatureWidget]).
//
// Every other annotation but a pop-up gets Print set and the four flags
// cleared. One that shows in print alone, NoView with Print, then shows on
// screen too, and one that shows on screen alone then prints.
//
// # Corpus evidence
//
//   - pdfjs bug1905623.pdf: carries an inline /Link whose /F flag was reported
//     as added on every run and never appeared in the output while the repair
//     wrote only through the allocator.
//   - All corpora: 49 annotations in 22 files carry Hidden or NoView. 38 of
//     them, in 19 files, show neither on screen nor in print, and the other 11
//     are NoView annotations that print. None is a signature widget.
//   - All corpora: over the PDF/A-1b, 2a, 2b, 2u, 3b and 4 remediations,
//     default and aggressive, of those 22 files and 12 other files with forms
//     or annotations, 216 outputs change, all from the 18 files whose removable
//     annotation reaches the fixer (verapdf PDF_UA-1__7.18.1-t02-pass-g.pdf's
//     /FREETEXT goes with the unknown subtypes first). 18 more outputs pass on
//     litho and veraPDF alike, veraPDF reports 54 fewer rule classes and none
//     new, the two agree on every output's rule set, and every page's extracted
//     text is unchanged. No PDF/UA-1 or PDF/UA-2 output changes.
//   - litho annothidden_2b.pdf: carries the constructs no other file does. Its
//     remediations at the six PDF/A manifest profiles, default and aggressive,
//     now pass on both tools, where each kept the flag finding of its hidden
//     pop-up.
func fixAnnotFlags(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return nil, nil
	}

	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool), 0)
	sheets := readAnnotSheets(ctx, alloc, doc, pages)

	var plan removalPlan
	if compliance.PDFAPart(profile) > 0 {
		plan = planNeverShown(ctx, doc, sheets)
	}

	var (
		fixes            []string
		removed          []mutations.RemovedAnnotation
		removedWidgets   = make(map[int]bool)
		flagged, unowned int
	)
	for si := range sheets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sh := &sheets[si]

		kept := make(ast.PdfArray, 0, len(sh.annots))
		dropped := false
		var inlineFixed map[int]ast.PdfDict
		inlineFlagged, inlineUnowned := 0, 0
		for i, annotObj := range sh.annots {
			dict := sh.dicts[i]
			if dict == nil {
				kept = append(kept, annotObj)
				continue
			}
			if reason, gone := plan.reason(si, i, annotObj); gone {
				dropped = true
				fixes = append(fixes, fmt.Sprintf("%s from page %d", reason, sh.number))
				removed = append(removed, mutations.RemovedAnnotation{Obj: annotObj, Dict: dict})
				if ref, isRef := annotObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 && sh.subtypes[i] == "Widget" {
					removedWidgets[ref.ObjectNum] = true
				}
				continue
			}

			repaired, flags, popup := repairKeptAnnot(dict, sh.subtypes[i], plan)
			if repaired == nil {
				kept = append(kept, annotObj)
				continue
			}
			if ref, isRef := annotObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
				alloc.AllocAt(ref.ObjectNum, repaired)
				kept = append(kept, annotObj)
				flagged += boolToCount(flags)
				unowned += boolToCount(popup)
				continue
			}

			// ISO 32000-1, 12.5.2 does not require an annotation to be an
			// indirect object. One written inline in /Annots has no object
			// number to address, so writing only through the allocator dropped
			// the repair while still counting it. It goes back through the
			// array instead.
			kept = append(kept, repaired)
			if inlineFixed == nil {
				inlineFixed = make(map[int]ast.PdfDict, 1)
			}
			inlineFixed[i] = repaired
			inlineFlagged += boolToCount(flags)
			inlineUnowned += boolToCount(popup)
		}

		landed := false
		switch {
		case dropped:
			landed = writeSheetAnnots(ctx, alloc, doc, sh, kept)
		case len(inlineFixed) > 0:
			landed = installInlineAnnots(ctx, alloc, doc, sh.page, sh.annotsObj, sh.annots, inlineFixed) > 0
		}
		if landed {
			flagged += inlineFlagged
			unowned += inlineUnowned
		}
	}

	if len(removed) > 0 {
		fixes = append(fixes, pruneRemovedWidgets(ctx, alloc, doc, catalogRef, removedWidgets)...)
		if err := mutations.UntagRemovedAnnotations(ctx, alloc, doc, catalogRef, removed); err != nil {
			return nil, fmt.Errorf("removing annotations that never show: %w", err)
		}
		// Null the annotation object as well, as the annotations fixer does
		// for the subtypes it removes: dropped from /Annots alone, it stays a
		// live object that the rules sweeping every object still judge.
		for _, r := range removed {
			if ref, isRef := r.Obj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
				alloc.AllocAt(ref.ObjectNum, ast.PdfNull{})
			}
		}
	}
	if unowned > 0 {
		fixes = append(fixes, fmt.Sprintf("removed /Popup from %d annotations whose pop-up was removed", unowned))
	}
	if flagged > 0 {
		fixes = append(fixes, fmt.Sprintf("added /F flag to %d annotations", flagged))
	}
	return fixes, nil
}

// boolToCount returns 1 for true and 0 for false.
func boolToCount(b bool) int {
	if b {
		return 1
	}
	return 0
}

// repairKeptAnnot returns the annotation dictionary a kept annotation takes,
// nil when it stays as it is, and whether its flags were repaired and its
// /Popup dropped. A pop-up the plan removes leaves the /Popup of its parent,
// and every annotation but a pop-up gets Print set and the forbidden flags
// cleared. A pop-up needs no /F (ISO 19005-2 to ISO 19005-4, 6.3.2 test 1),
// and its flags are left as they are.
func repairKeptAnnot(dict ast.PdfDict, subtype ast.PdfName, plan removalPlan) (ast.PdfDict, bool, bool) {
	var out ast.PdfDict
	edit := func() {
		if out == nil {
			out = make(ast.PdfDict, len(dict)+1)
			maps.Copy(out, dict)
		}
	}

	unowned := false
	if popup, isRef := dict["Popup"].(ast.PdfIndirectRef); isRef && plan.removesObject(popup.ObjectNum) {
		edit()
		delete(out, "Popup")
		unowned = true
	}

	flagged := false
	if subtype != "Popup" {
		currentF := 0
		fObj, hasF := dict["F"]
		if fInt, isInt := fObj.(ast.PdfInt); isInt {
			currentF = int(fInt)
		}
		if !hasF || currentF&annotFlagPrint == 0 || currentF&annotFlagsForbidden != 0 {
			edit()
			out["F"] = ast.PdfInt((currentF | annotFlagPrint) &^ annotFlagsForbidden)
			flagged = true
		}
	}
	return out, flagged, unowned
}

// annotSheet is one page's /Annots as [fixAnnotFlags] reads it, alloc first:
// its entries, the dictionary of each, nil when an entry does not resolve to
// one, and the /Subtype of each.
type annotSheet struct {
	page      annotPageEntry
	number    int
	annotsObj ast.PdfObject
	annots    ast.PdfArray
	dicts     []ast.PdfDict
	subtypes  []ast.PdfName
	// writable reports whether the array has an object to go back into once
	// an annotation leaves it: the page, or else an indirect /Annots array.
	writable bool
}

// readAnnotSheets reads the /Annots of every page that has one.
func readAnnotSheets(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []annotPageEntry) []annotSheet {
	var sheets []annotSheet
	for n, pe := range pages {
		if ctx.Err() != nil {
			return sheets
		}
		annotsObj, ok := pe.dict["Annots"]
		if !ok {
			continue
		}
		// Alloc-aware, and the helper matches the object KIND: liveArray for
		// an array. annotations (registry 121) nils forbidden annotation slots
		// and compacts the array before writing it at its own object, and
		// annot-flags runs after it, so a stale copy here restores annotations
		// that were deliberately removed.
		annots := liveArray(ctx, alloc, doc, annotsObj)
		if len(annots) == 0 {
			continue
		}
		sh := annotSheet{
			page:      pe,
			number:    n + 1,
			annotsObj: annotsObj,
			annots:    annots,
			dicts:     make([]ast.PdfDict, len(annots)),
			subtypes:  make([]ast.PdfName, len(annots)),
		}
		for i, annotObj := range annots {
			var dict ast.PdfDict
			if ref, isRef := annotObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
				dict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
			}
			if dict == nil {
				resolved, err := document.ResolveDict(ctx, doc, annotObj)
				if err != nil || resolved == nil {
					continue
				}
				dict = resolved
			}
			sh.dicts[i] = dict
			// Best-effort: an unreadable /Subtype yields an empty name, which
			// is no subtype the fixer treats apart.
			sh.subtypes[i], _ = document.ResolveName(ctx, doc, dict["Subtype"])
		}
		annotsRef, isRef := annotsObj.(ast.PdfIndirectRef)
		sh.writable = pe.ref.ObjectNum > 0 || (isRef && annotsRef.ObjectNum > 0)
		sheets = append(sheets, sh)
	}
	return sheets
}

// writeSheetAnnots writes a page's /Annots once annotations left it, and
// reports whether it had an object to write.
//
// The page takes the array whenever it has an object number, even when /Annots
// was an indirect array. Several fixers that run later read an indirect array
// as the file wrote it (document.ResolveArray): one written back at its own
// object number would show them the removed annotations, which they read past
// their pending null and write back with a repair. Each of them reads the array
// through the page. A page with no object number of its own, which ISO
// 32000-1, 7.7.3.2 does not allow, leaves the indirect array to write.
func writeSheetAnnots(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, sh *annotSheet, kept ast.PdfArray) bool {
	if sh.page.ref.ObjectNum > 0 {
		pageDict := resolveFromAllocOrDoc(ctx, alloc, doc, sh.page.ref)
		if pageDict == nil {
			pageDict = sh.page.dict
		}
		newPage := make(ast.PdfDict, len(pageDict))
		maps.Copy(newPage, pageDict)
		if len(kept) == 0 {
			delete(newPage, "Annots")
		} else {
			newPage["Annots"] = kept
		}
		return alloc.AllocAt(sh.page.ref.ObjectNum, newPage)
	}
	if annotsRef, isRef := sh.annotsObj.(ast.PdfIndirectRef); isRef && annotsRef.ObjectNum > 0 {
		return alloc.AllocAt(annotsRef.ObjectNum, kept)
	}
	return false
}

// annotSlot places an annotation written inline: the sheet and the entry of
// its /Annots array.
type annotSlot struct{ sheet, entry int }

// removalPlan names the annotations [fixAnnotFlags] removes, each with the
// start of the line its report gives: indirect ones by object number, so an
// annotation listed twice goes from both places, and inline ones by slot.
type removalPlan struct {
	byObject map[int]string
	inline   map[annotSlot]string
}

// reason returns the report line of the entry at slot (sheet, entry) holding
// obj, and whether the plan removes it.
func (p removalPlan) reason(sheet, entry int, obj ast.PdfObject) (string, bool) {
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		r, gone := p.byObject[ref.ObjectNum]
		return r, gone
	}
	r, gone := p.inline[annotSlot{sheet, entry}]
	return r, gone
}

// removesObject reports whether the plan removes the annotation at objNum.
func (p removalPlan) removesObject(objNum int) bool {
	_, gone := p.byObject[objNum]
	return gone
}

// neverShownReason reports whether the annotation's /F keeps it off the screen
// and off paper, and returns the start of the line reporting its removal.
//
// ISO 32000-1, 12.5.3, Table 165 gives the flags their meaning. A Hidden
// annotation is neither displayed nor printed, whatever its type. A NoView one
// is not displayed, and prints only when its Print flag is set. ToggleNoView
// inverts NoView on events a viewer still receives, rolling the pointer over
// the annotation among them, so a NoView annotation carrying it shows on
// screen. Invisible hides only an annotation whose type the viewer has no
// handler for, which no type the annotations fixer leaves on a PDF/A page is.
//
// Up to part 3, nothing in a conforming file can show such an annotation later:
// ISO 19005-1, 6.6.1 and ISO 19005-2 and ISO 19005-3, 6.5.1 forbid the Hide and
// JavaScript actions that change flags at run time. ISO 19005-4, 6.6.1 permits
// JavaScript, which may clear a flag at run time, and 6.3.2 forbids the flag
// all the same: the removal follows the flags as the file states them.
func neverShownReason(ctx context.Context, doc *document.Document, dict ast.PdfDict, subtype ast.PdfName) (string, bool) {
	if _, hasF := dict["F"]; !hasF {
		return "", false
	}
	f := annotFlags(ctx, doc, dict)
	switch {
	case f&annotFlagHidden != 0:
		return fmt.Sprintf("removed Hidden %s annotation", annotSubtypeLabel(subtype)), true
	case f&annotFlagNoView != 0 && f&annotFlagPrint == 0 && f&annotFlagToggleNoView == 0:
		return fmt.Sprintf("removed NoView %s annotation, which does not print,", annotSubtypeLabel(subtype)), true
	}
	return "", false
}

// annotSubtypeLabel names an annotation subtype in a report line, without the
// slash [ast.PdfName.String] writes.
func annotSubtypeLabel(subtype ast.PdfName) string {
	if subtype == "" {
		return "untyped"
	}
	return string(subtype)
}

// popupRemovedReason is the report line of a pop-up that goes with its parent.
const popupRemovedReason = "removed Popup annotation of a removed annotation"

// planNeverShown returns the annotations [fixAnnotFlags] removes: each one that
// shows neither on screen nor in print ([neverShownReason]), and each one that
// goes with a removed one.
//
// A pop-up does not appear alone but with its parent annotation (ISO 32000-1,
// 12.5.6.14), so the pop-up of a removed annotation goes, named by its /Popup
// or naming it by /Parent. A reply is displayed together with the annotation
// it replies to and never on its own (Table 170, RT), so an annotation whose
// /IRT names a removed one goes too, and the replies to it after it. The walk
// follows each reference once, so it is linear in the number of annotations.
//
// A signed signature field keeps its widget (see [signedSignatureWidget]), and
// a page with no object to write its /Annots into keeps every annotation.
func planNeverShown(ctx context.Context, doc *document.Document, sheets []annotSheet) removalPlan {
	plan := removalPlan{byObject: make(map[int]string), inline: make(map[annotSlot]string)}

	// positions lists where each annotation object appears, and dependents the
	// entries that go when it goes: pop-ups naming it by /Parent, and
	// annotations naming it by /IRT.
	positions := make(map[int][]annotSlot)
	dependents := make(map[int][]annotSlot)
	for si, sh := range sheets {
		if !sh.writable {
			continue
		}
		for i, obj := range sh.annots {
			dict := sh.dicts[i]
			if dict == nil {
				continue
			}
			if ref, isRef := obj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
				positions[ref.ObjectNum] = append(positions[ref.ObjectNum], annotSlot{si, i})
			}
			if parent, isRef := dict["Parent"].(ast.PdfIndirectRef); isRef && sh.subtypes[i] == "Popup" {
				dependents[parent.ObjectNum] = append(dependents[parent.ObjectNum], annotSlot{si, i})
			}
			if irt, isRef := dict["IRT"].(ast.PdfIndirectRef); isRef {
				dependents[irt.ObjectNum] = append(dependents[irt.ObjectNum], annotSlot{si, i})
			}
		}
	}

	var queue []annotSlot
	mark := func(slot annotSlot, reason string) {
		sh := sheets[slot.sheet]
		if signedSignatureWidget(ctx, doc, sh.dicts[slot.entry], sh.subtypes[slot.entry]) {
			return
		}
		obj := sh.annots[slot.entry]
		if ref, isRef := obj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
			if _, done := plan.byObject[ref.ObjectNum]; done {
				return
			}
			plan.byObject[ref.ObjectNum] = reason
		} else {
			if _, done := plan.inline[slot]; done {
				return
			}
			plan.inline[slot] = reason
		}
		queue = append(queue, slot)
	}

	for si, sh := range sheets {
		if !sh.writable {
			continue
		}
		for i, dict := range sh.dicts {
			if dict == nil {
				continue
			}
			if reason, never := neverShownReason(ctx, doc, dict, sh.subtypes[i]); never {
				mark(annotSlot{si, i}, reason)
			}
		}
	}

	for len(queue) > 0 && ctx.Err() == nil {
		slot := queue[0]
		queue = queue[1:]
		sh := sheets[slot.sheet]
		if popup, isRef := sh.dicts[slot.entry]["Popup"].(ast.PdfIndirectRef); isRef {
			for _, at := range positions[popup.ObjectNum] {
				mark(at, popupRemovedReason)
			}
		}
		ref, isRef := sh.annots[slot.entry].(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum <= 0 {
			continue
		}
		for _, at := range dependents[ref.ObjectNum] {
			dependent := sheets[at.sheet]
			if dependent.subtypes[at.entry] == "Popup" {
				mark(at, popupRemovedReason)
				continue
			}
			mark(at, fmt.Sprintf("removed %s annotation in reply to a removed annotation", annotSubtypeLabel(dependent.subtypes[at.entry])))
		}
	}
	return plan
}

// signedSignatureWidget reports whether the annotation is the widget of a
// signed signature field: its field type, inherited through /Parent (ISO
// 32000-1, 12.7.3.1), is Sig, and its /V is the signature dictionary that holds
// the signature (12.7.4.5, Table 232). An unsigned signature field has no /V.
//
// A signature is reached through its field, so removing a hidden signed
// signature widget with its field would take the signature out of the
// document. It keeps today's flag repair instead.
func signedSignatureWidget(ctx context.Context, doc *document.Document, annot ast.PdfDict, subtype ast.PdfName) bool {
	if subtype != "Widget" || inheritedFieldType(ctx, doc, annot) != "Sig" {
		return false
	}
	dict := annot
	seen := make(map[int]bool)
	for depth := 0; depth < maxFieldParentDepth && dict != nil; depth++ {
		if value, has := dict["V"]; has {
			sig, err := document.ResolveDict(ctx, doc, value)
			return err == nil && sig != nil
		}
		_, parent, ok := document.ResolveFieldParent(ctx, doc, dict, seen)
		if !ok {
			return false
		}
		dict = parent
	}
	return false
}

// pruneRemovedWidgets takes the removed widgets out of the interactive form,
// and returns the lines reporting the fields that went.
//
// AcroForm /Fields lists the root fields and a field's /Kids its children (ISO
// 32000-1, 12.7.2 and 12.7.3.1). A widget merged with its field (12.5.6.19)
// leaves the array that lists it, and a field whose every kid went leaves its
// own parent the same way: no widget presents it any more, so no reader of the
// page sees or fills it. A field that keeps a widget keeps its place. /CO lists
// fields (Table 218) and loses the ones that went.
//
// Every level is read alloc first, and an array goes into the dictionary that
// holds it rather than back to an object of its own, for the reason
// [writeSheetAnnots] gives: later fixers read the form alloc first through its
// dictionaries.
func pruneRemovedWidgets(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogRef ast.PdfIndirectRef, widgets map[int]bool) []string {
	if len(widgets) == 0 {
		return nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil
	}
	acroFormObj, ok := catalogDict["AcroForm"]
	if !ok {
		return nil
	}
	acroForm := liveDictValue(ctx, alloc, doc, acroFormObj)
	if acroForm == nil {
		return nil
	}

	p := &fieldPrune{alloc: alloc, doc: doc, widgets: widgets, gone: maps.Clone(widgets), seen: make(map[int]bool)}
	fields, fieldsChanged := p.list(ctx, acroForm["Fields"], 0)

	var co ast.PdfArray
	coChanged := false
	if coObj, has := acroForm["CO"]; has {
		entries := liveArray(ctx, alloc, doc, coObj)
		co = make(ast.PdfArray, 0, len(entries))
		for _, entry := range entries {
			if ref, isRef := entry.(ast.PdfIndirectRef); isRef && p.gone[ref.ObjectNum] {
				continue
			}
			co = append(co, entry)
		}
		coChanged = len(co) != len(entries)
	}
	if !fieldsChanged && !coChanged {
		return p.fixes
	}

	newForm := make(ast.PdfDict, len(acroForm))
	maps.Copy(newForm, acroForm)
	if fieldsChanged {
		newForm["Fields"] = fields
	}
	if coChanged {
		if len(co) == 0 {
			delete(newForm, "CO")
		} else {
			newForm["CO"] = co
		}
	}
	if ref, isRef := acroFormObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newForm)
		return p.fixes
	}
	newCatalog := make(ast.PdfDict, len(catalogDict))
	maps.Copy(newCatalog, catalogDict)
	newCatalog["AcroForm"] = newForm
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	return p.fixes
}

// fieldPrune carries one [pruneRemovedWidgets] walk. widgets are the removed
// widgets, gone those and the fields that went with them, and seen the fields
// visited, so a shared or cyclic /Kids is walked once.
type fieldPrune struct {
	alloc   *writer.ObjAllocator
	doc     *document.Document
	widgets map[int]bool
	gone    map[int]bool
	seen    map[int]bool
	fixes   []string
}

// list returns the /Fields or /Kids array arrObj without the entries that go,
// and whether any went.
func (p *fieldPrune) list(ctx context.Context, arrObj ast.PdfObject, depth int) (ast.PdfArray, bool) {
	arr := liveArray(ctx, p.alloc, p.doc, arrObj)
	kept := make(ast.PdfArray, 0, len(arr))
	for _, item := range arr {
		ref, isRef := item.(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum <= 0 {
			kept = append(kept, item)
			continue
		}
		if p.gone[ref.ObjectNum] {
			p.reportMergedField(ctx, ref)
			continue
		}
		if !p.field(ctx, ref, depth+1) {
			continue
		}
		kept = append(kept, item)
	}
	return kept, len(kept) != len(arr)
}

// field reports whether the field at ref stays, writing it with the /Kids it
// keeps when some of them went.
func (p *fieldPrune) field(ctx context.Context, ref ast.PdfIndirectRef, depth int) bool {
	if ctx.Err() != nil || depth > maxFieldTreeDepth || p.seen[ref.ObjectNum] {
		return true
	}
	p.seen[ref.ObjectNum] = true
	dict := resolveFromAllocOrDoc(ctx, p.alloc, p.doc, ref)
	if dict == nil {
		return true
	}
	kidsObj, has := dict["Kids"]
	if !has {
		return true
	}
	kids, changed := p.list(ctx, kidsObj, depth)
	if !changed {
		return true
	}
	// A field that is a widget itself stays while its page shows it.
	if subtype, _ := document.ResolveName(ctx, p.doc, dict["Subtype"]); len(kids) == 0 && subtype != "Widget" {
		p.gone[ref.ObjectNum] = true
		p.fixes = append(p.fixes, fmt.Sprintf("removed form field %s, left with no widget", fieldLabel(ctx, p.doc, dict)))
		return false
	}
	newField := make(ast.PdfDict, len(dict))
	maps.Copy(newField, dict)
	if len(kids) == 0 {
		delete(newField, "Kids")
	} else {
		newField["Kids"] = kids
	}
	p.alloc.AllocAt(ref.ObjectNum, newField)
	return true
}

// reportMergedField reports a removed widget a field array names: a field
// merged with its widget (ISO 32000-1, 12.5.6.19) goes with it. A widget of a
// field that keeps it apart is reported with its annotation, and nothing more
// is said here.
func (p *fieldPrune) reportMergedField(ctx context.Context, ref ast.PdfIndirectRef) {
	if !p.widgets[ref.ObjectNum] || p.seen[ref.ObjectNum] {
		return
	}
	p.seen[ref.ObjectNum] = true
	dict := resolveFromAllocOrDoc(ctx, p.alloc, p.doc, ref)
	if dict == nil {
		return
	}
	if _, named := dict["T"]; !named {
		return
	}
	p.fixes = append(p.fixes, fmt.Sprintf("removed form field %s with its widget", fieldLabel(ctx, p.doc, dict)))
}

// fieldLabel names a field in a report line: its partial name /T, quoted, or
// "without a name" when it states none.
func fieldLabel(ctx context.Context, doc *document.Document, field ast.PdfDict) string {
	name, err := document.ResolveString(ctx, doc, field["T"])
	if err != nil || name == "" {
		return "without a name"
	}
	return fmt.Sprintf("%q", document.DecodePdfString(name))
}

// installInlineAnnots writes annotations that were repaired in place, returning
// how many landed.
//
// An inline annotation is addressed only through its page's /Annots array, so
// the array is rebuilt with the replacements and written to whichever object
// owns it: the array itself when indirect, the page otherwise. A page with no
// object number of its own cannot be written at all, and nothing is reported.
func installInlineAnnots(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pe annotPageEntry,
	annotsObj ast.PdfObject,
	annots ast.PdfArray,
	fixed map[int]ast.PdfDict,
) int {
	if len(fixed) == 0 {
		return 0
	}

	newArr := make(ast.PdfArray, len(annots))
	copy(newArr, annots)
	for i, dict := range fixed {
		if i < len(newArr) {
			newArr[i] = dict
		}
	}

	if annotsRef, isRef := annotsObj.(ast.PdfIndirectRef); isRef && annotsRef.ObjectNum > 0 {
		alloc.AllocAt(annotsRef.ObjectNum, newArr)
		return len(fixed)
	}

	if pe.ref.ObjectNum <= 0 {
		return 0
	}

	pageDict := resolveFromAllocOrDoc(ctx, alloc, doc, pe.ref)
	if pageDict == nil {
		pageDict = pe.dict
	}
	newPage := make(ast.PdfDict, len(pageDict))
	maps.Copy(newPage, pageDict)
	newPage["Annots"] = newArr
	alloc.AllocAt(pe.ref.ObjectNum, newPage)
	return len(fixed)
}
