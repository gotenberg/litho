package remediation

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchTransparency(v compliance.Violation) bool {
	return v.Rule.Specification == "ISO_19005_1" &&
		strings.HasPrefix(v.Rule.Clause, "6.4")
}

func fixTransparency(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	// PDF/A-1 (ISO 19005-1 §6.4) forbids transparency outright. The
	// only way a flagged file can become compliant is to flatten
	// alpha, blend modes, and SMask, so this fix runs regardless of
	// PreserveContent: there is no "preserve" alternative for files
	// that must conform to part 1.
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("getting catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("getting pages: %w", err)
	}

	var totalFixed int

	// Shared across the whole document. An object repaired once must never be
	// rebuilt from the stale original and written again: the second write would
	// discard the first repair. Appearance streams are shared between
	// annotations, and one form can be declared by several pages.
	visited := make(map[int]bool)

	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// The page object owns both repairs below: an /ExtGState stored inline
		// under its /Resources, and its own /Group. They share one working copy
		// and one write, because two writes to the same object number keep only
		// the second.
		pageDict := livePageDict(ctx, alloc, doc, page)
		workPage := make(ast.PdfDict, len(pageDict))
		maps.Copy(workPage, pageDict)
		pageDirty, pagePending := false, 0

		// One uniform walk of the page's resources: /ExtGState here, then the
		// XObjects, patterns and Type 3 fonts it names, recursively.
		if page.Resources != nil {
			n, pending, dirty := fixResourceTransparency(ctx, doc, alloc, page.Resources, workPage, visited, 0)
			totalFixed += n
			if dirty {
				pageDirty, pagePending = true, pagePending+pending
			}
		}

		// Remove page-level transparency group.
		groupRemoved := false
		if groupObj, ok := workPage["Group"]; ok {
			if isTransparencyGroup(ctx, doc, groupObj) {
				delete(workPage, "Group")
				pageDirty, groupRemoved = true, true
			}
		}

		if pageDirty && alloc.AllocAt(page.Ref.ObjectNum, workPage) {
			totalFixed += pagePending
			if groupRemoved {
				totalFixed++
			}
		}

		// Fix annotation appearance streams.
		if annotsObj, ok := pageDict["Annots"]; ok {
			totalFixed += fixAnnotationTransparency(ctx, doc, alloc, annotsObj, visited)
		}
	}

	if totalFixed == 0 {
		return nil, nil
	}

	return []string{fmt.Sprintf("normalized %d transparency properties to opaque (CA/ca=1.0, BM=Normal, SMask=None)", totalFixed)}, nil
}

// fixPatternTransparency fixes transparency in the resources of every pattern a
// page declares.
//
// ISO 19005-1 §6.4 forbids transparency wherever it renders, and the compliance
// check reaches a tiling pattern's own resources, but this repair walk did not:
// it covered page resources, Form XObject resources and annotation appearance
// resources, leaving an /ExtGState inside a Pattern flagged and unrepaired.
//
// A tiling pattern (PatternType 1) is a STREAM whose dictionary carries
// /Resources; a shading pattern (PatternType 2) is a plain dictionary with
// none. Both are resolved and branched on by type, for the same reason
// [recurseColorSpacePatch] does it.
//
// visited breaks reference cycles: a pattern's resources may name the pattern
// itself, and an unguarded walk would revisit it without bound.
func fixPatternTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, resources ast.PdfDict, visited map[int]bool, depth int) int {
	if resources == nil || depth > maxTransparencyResourceDepth {
		return 0
	}

	patObj, ok := resources["Pattern"]
	if !ok {
		return 0
	}
	patDict, err := document.ResolveDict(ctx, doc, patObj)
	if err != nil || patDict == nil {
		return 0
	}

	fixed := 0
	// Sorted, as every walk sharing visited is: see [fixXObjectTransparency].
	for _, name := range slices.Sorted(maps.Keys(patDict)) {
		entry := patDict[name]
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}

		// Read alloc-first, the shape walkIndirectObjects uses for an object
		// whose kind is not known in advance: recurseColorSpacePatch writes
		// pattern objects from output-intent, far ahead of transparency, and
		// rebuilding work from the stale original below would clobber that
		// write on AllocAt. A pattern entry is a stream (PatternType 1) or a
		// dictionary (PatternType 2), so the pending object is taken raw and
		// the type switches below branch on it unchanged.
		var resolved ast.PdfObject
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
			resolved = alloc.Pending()[ref.ObjectNum]
		}
		if resolved == nil {
			direct, err := document.ResolveObject(ctx, doc, entry)
			if err != nil || direct == nil {
				continue
			}
			resolved = direct
		}

		var patRes ast.PdfObject
		switch v := resolved.(type) {
		case ast.PdfStream:
			patRes = v.Dict["Resources"]
		case ast.PdfDict:
			patRes = v["Resources"]
		default:
			continue
		}

		res, err := document.ResolveDict(ctx, doc, patRes)
		if err != nil || res == nil {
			continue
		}

		// The pattern object is the nearest indirect ancestor of its resources.
		work := make(ast.PdfDict, len(res))
		switch v := resolved.(type) {
		case ast.PdfStream:
			maps.Copy(work, v.Dict)
		case ast.PdfDict:
			maps.Copy(work, v)
		}

		n, pending, dirty := fixResourceTransparency(ctx, doc, alloc, res, work, visited, depth+1)
		fixed += n
		if !dirty {
			continue
		}
		ref, isRef := entry.(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum <= 0 {
			continue
		}
		var written bool
		if st, isStream := resolved.(ast.PdfStream); isStream {
			written = alloc.AllocAt(ref.ObjectNum, ast.NewLazyStream(work, st.Bytes))
		} else {
			written = alloc.AllocAt(ref.ObjectNum, work)
		}
		if written {
			fixed += pending
		}
	}

	return fixed
}

// fixType3FontTransparency fixes transparency in the resources of every Type 3
// font a page declares.
//
// ISO 19005-1 §6.4 forbids transparency wherever it renders, and the compliance
// check reaches here ([compliance] walks page.Resources /Font to the Type 3 font
// dictionary), but this repair walk did not: it covered page resources, Form
// XObject resources, Pattern resources and annotation appearance resources,
// leaving an /ExtGState inside a Type 3 font flagged and unrepaired. A carrier
// can declare NO /ExtGState on the page at all, and then the existing walks find
// nothing to fix.
//
// The target is the FONT dictionary's own /Resources, which ISO 32000-1, 9.6.5
// scopes to the font's CharProcs. [fixType3FontResources] already walks this
// shape for resource completeness; the font's own /Resources is the dictionary
// both repairs read.
//
// One walk repairs four rules, because [fixExtGStateDict] normalises /SMask,
// /BM, /CA and /ca together: ISO 19005-1 §6.4 t1, t4, t5 and t6 respectively.
//
// visited breaks reference cycles: two font names may share one font object.
//
// # Corpus evidence
//
//   - All corpora: the carriers declare NO /ExtGState on the page at all, so
//     the existing walks find nothing to fix.
//   - All corpora: the carriers declare /Resources, /ExtGState and the /GS
//     entry all inline on the font, so there is nothing below the font to
//     rewrite.
//   - verapdf 6-2-10-t04-fail-b: the font holds one non-conforming /CA and its
//     three /CharProcs streams hold one each, so repairing only the font leaves
//     three behind, which veraPDF reports and litho does not.
func fixType3FontTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, resources ast.PdfDict, visited map[int]bool, depth int) int {
	if resources == nil || depth > maxTransparencyResourceDepth {
		return 0
	}

	fontDict, err := document.ResolveDict(ctx, doc, resources["Font"])
	if err != nil || fontDict == nil {
		return 0
	}

	fixed := 0
	// Sorted, as every walk sharing visited is: see [fixXObjectTransparency].
	for _, name := range slices.Sorted(maps.Keys(fontDict)) {
		entry := fontDict[name]
		var fontRef ast.PdfIndirectRef
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
			fontRef = ref
		}

		// Read through the allocator. patchType3FontColorSpace writes Type 3
		// font dictionaries from output-intent, far ahead of transparency, and
		// the document still holds the pre-repair copy, so rebuilding work from
		// it below would discard that write. The ref/direct split is
		// load-bearing: a font dictionary stored inline has no object number,
		// and resolving a zero reference would find nothing.
		var fd ast.PdfDict
		if fontRef.ObjectNum > 0 {
			fd = resolveFromAllocOrDoc(ctx, alloc, doc, fontRef)
		} else if direct, err := document.ResolveDict(ctx, doc, entry); err == nil {
			fd = direct
		}
		if fd == nil {
			continue
		}
		subtype, err := document.ResolveName(ctx, doc, fd["Subtype"])
		if err != nil || subtype != "Type3" {
			continue
		}

		// The font's own /Resources, when it declares one. The font object is
		// the nearest indirect ancestor: /Resources, /ExtGState and the /GS
		// entry are typically all inline on it, so there is nothing below the
		// font to rewrite.
		if own, err := document.ResolveDict(ctx, doc, fd["Resources"]); err == nil && own != nil {
			work := make(ast.PdfDict, len(fd))
			maps.Copy(work, fd)

			n, pending, dirty := fixResourceTransparency(ctx, doc, alloc, own, work, visited, depth+1)
			fixed += n
			if dirty {
				if ref, isRef := entry.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 &&
					alloc.AllocAt(ref.ObjectNum, work) {
					fixed += pending
				}
			}
		}

		// A glyph procedure may carry /Resources of its own, and the font's
		// own /Resources does not cover it, so repairing only the font leaves
		// the glyph procedures' graphics states behind.
		fixed += fixType3CharProcTransparency(ctx, doc, alloc, fd, visited, depth)
	}

	return fixed
}

// maxTransparencyResourceDepth caps recursion through nested resource
// dictionaries. Anti-DoS guard, not spec-mandated: a crafted file can nest Form
// XObjects arbitrarily and each level costs a resolve plus a dictionary copy.
// Ten is far beyond any real document while keeping the work bounded. It does
// not replace the visited set: a depth bound survives a cycle without ever
// detecting it.
//
// # Corpus evidence
//
//   - verapdf: the resource nesting peaks at two.
const maxTransparencyResourceDepth = 10

// fixResourceTransparency repairs every transparency construct reachable from
// one resource dictionary: the /ExtGState entries it declares, then the
// XObjects, patterns and Type 3 fonts it names, recursively.
//
// Every site walks the same shape, because ISO 19005-1 §6.4 forbids
// transparency wherever it renders: a resource dictionary reached through a
// page, a Form XObject, a glyph procedure or an annotation appearance is
// equally rendered content. Applying a different subset per site is what left
// each site leaking a different location.
//
// owner is the caller's working copy of the dictionary that holds res under
// /Resources; see [fixExtGStateDict] for how a fully inline chain is written.
// visited is shared across the whole document so an object repaired once is
// never rebuilt from the stale original and written again.
//
// # Corpus evidence
//
//   - verapdf: on the §6.4 test 3 carriers, a group survived in an appearance
//     form, in a form nested inside a form, and in a form a Type 3 font
//     declares.
func fixResourceTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, res ast.PdfDict, owner ast.PdfDict, visited map[int]bool, depth int) (int, int, bool) {
	if res == nil || depth > maxTransparencyResourceDepth {
		return 0, 0, false
	}

	fixed, pending, dirty := fixExtGStateDict(ctx, doc, alloc, res, owner)
	fixed += fixXObjectTransparency(ctx, doc, alloc, res, visited, depth)
	fixed += fixPatternTransparency(ctx, doc, alloc, res, visited, depth)
	fixed += fixType3FontTransparency(ctx, doc, alloc, res, visited, depth)

	return fixed, pending, dirty
}

// fixType3CharProcTransparency fixes transparency in the resources of every
// glyph procedure a Type 3 font declares.
//
// ISO 32000-1, 9.6.5 scopes a Type 3 font's /Resources to its /CharProcs, but
// a glyph procedure is a content stream and may declare /Resources of its own,
// and producers type theirs as Form XObjects with a full resource dictionary.
// Those are separate indirect objects, so each is written directly: the
// ancestor to rewrite is the glyph stream itself.
//
// visited breaks reference cycles and shared glyph procedures: two names in
// /CharProcs may address one stream.
//
// # Corpus evidence
//
//   - verapdf: the carriers type their glyph procedures as Form XObjects with a
//     full resource dictionary.
//   - All corpora: no carrier exercises a transparency group on a glyph
//     procedure, so the unit tests are its only evidence.
func fixType3CharProcTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fontDict ast.PdfDict, visited map[int]bool, depth int) int {
	if depth > maxTransparencyResourceDepth {
		return 0
	}

	charProcs, err := document.ResolveDict(ctx, doc, fontDict["CharProcs"])
	if err != nil || charProcs == nil {
		return 0
	}

	fixed := 0
	// Sorted, as every walk sharing visited is: see [fixXObjectTransparency].
	for _, name := range slices.Sorted(maps.Keys(charProcs)) {
		entry := charProcs[name]
		ref, isRef := entry.(ast.PdfIndirectRef)
		if isRef && ref.ObjectNum > 0 {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}

		obj, err := document.ResolveObject(ctx, doc, entry)
		if err != nil {
			continue
		}
		stream, ok := obj.(ast.PdfStream)
		if !ok {
			continue
		}
		work := make(ast.PdfDict, len(stream.Dict))
		maps.Copy(work, stream.Dict)
		changed := false

		// A glyph procedure typed as a Form XObject may carry its own
		// transparency group, which ISO 19005-1 §6.4 test 3 forbids wherever it
		// renders.
		if groupObj, ok := stream.Dict["Group"]; ok && isTransparencyGroup(ctx, doc, groupObj) {
			delete(work, "Group")
			changed = true
		}

		pending := 0
		if res, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && res != nil {
			n, p, dirty := fixResourceTransparency(ctx, doc, alloc, res, work, visited, depth+1)
			fixed += n
			if dirty {
				changed = true
				pending = p
			}
		}

		// One write per glyph stream. Dropping /Group and repairing an inline
		// graphics state both land in the same object, and two AllocAt calls
		// would keep only the second: an earlier version wrote inside the
		// inline branch and then skipped this one, so a failed write discarded
		// the group removal with nothing reported.
		if changed && isRef && ref.ObjectNum > 0 &&
			alloc.AllocAt(ref.ObjectNum, ast.NewLazyStream(work, stream.Bytes)) {
			fixed += pending
			if _, had := stream.Dict["Group"]; had {
				if _, kept := work["Group"]; !kept {
					fixed++
				}
			}
		}
	}

	return fixed
}

// fixExtGStateDict finds and fixes transparency properties in a Resources/ExtGState dict.
//
// owner is the caller's working copy of the dictionary that declares resources
// under /Resources, or nil when the caller has none. ISO 32000-1, 7.3.7 lets
// /Resources, /ExtGState and each graphics state parameter dictionary be direct
// objects; when every one of them is, there is no indirect reference at or
// below /ExtGState to rewrite and the repair can only be written through the
// enclosing page, Type 3 font, Form XObject or pattern. Producers commonly
// store the whole chain inline, and the repair silently wrote nothing for them.
//
// The returned bool reports that owner was updated in place. The caller writes
// owner itself, so a caller already rewriting that object (fixXObjectTransparency
// drops /Group from the same dictionary) writes once rather than racing itself
// and losing one of the two changes.
//
// The first int counts writes the allocator ACCEPTED. The second counts repairs
// that depend on the caller's own write of owner, and the caller adds them only
// once its [writer.ObjAllocator.AllocAt] returns true. Neither counts repairs
// merely attempted: AllocAt refuses object 0, and an earlier version reported
// every refusal, and every case with nothing to write through, as a success.
//
// # Corpus evidence
//
//   - verapdf: 14 of 16 §6.4 carriers store the whole chain inline, and the
//     repair silently wrote nothing for all of them.
//   - pdfcpu pkg_testdata__FOSDEM14_HPC_devroom_14_GoCUDA: at PDF/A-1b all 19
//     pages lost the /DefaultCMYK output-intent reported injecting, because
//     this rebuilt their indirect /Resources from the parsed copy.
//   - pdfjs bug1019475_1, issue14864, issue14982, issue18894: the same at
//     PDF/A-1b, 8 pages of issue14864 among them.
func fixExtGStateDict(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, resources ast.PdfDict, owner ast.PdfDict) (int, int, bool) {
	// Start from the owner's /Resources as queued, not the walk's parsed copy:
	// an inline chain rebuilds this dictionary and writes it back below, and
	// output-intent (registry 68) injects a default colour space into the same
	// /Resources ahead of transparency (107). An owner that inherits its
	// resources has no entry of its own, and the walk's copy is all there is.
	if owner != nil {
		if live := liveDictValue(ctx, alloc, doc, owner["Resources"]); live != nil {
			resources = live
		}
	}
	gsObj, ok := resources["ExtGState"]
	if !ok {
		return 0, 0, false
	}

	// Alloc-aware read. graphics (registry 106) and transparency (107) are
	// CONSECUTIVE registry entries that write the same /ExtGState objects, and
	// the registry drives every fixer against ONE allocator with no flush
	// between them. Rebuilding this dictionary from the on-disk copy discards
	// whatever graphics just wrote to it.
	var gsDict ast.PdfDict
	if ref, isRef := gsObj.(ast.PdfIndirectRef); isRef {
		gsDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	} else if d, derr := document.ResolveDict(ctx, doc, gsObj); derr == nil {
		gsDict = d
	}
	if gsDict == nil {
		return 0, 0, false
	}

	fixed := 0
	// Entries with no indirect reference of their own accumulate here and are
	// written together: rebuilding the parent once per entry would drop every
	// change but the last.
	var inlineUpdates ast.PdfDict
	inlineFixes := 0

	for gsName, gsEntry := range gsDict {
		var gsRef ast.PdfIndirectRef
		if ref, ok := gsEntry.(ast.PdfIndirectRef); ok {
			gsRef = ref
		}

		// Alloc-aware for the same reason, and this is the load-bearing one:
		// the entry is written back at gsRef.ObjectNum below, so reading it
		// from the document drops a peer's pending edit to that same object.
		var gs ast.PdfDict
		if gsRef.ObjectNum > 0 {
			gs = resolveFromAllocOrDoc(ctx, alloc, doc, gsRef)
		} else if d, derr := document.ResolveDict(ctx, doc, gsEntry); derr == nil {
			gs = d
		}
		if gs == nil {
			continue
		}

		changed := 0
		newGS := make(ast.PdfDict, len(gs))
		maps.Copy(newGS, gs)

		// CA (stroke alpha): must be 1.0
		if caObj, ok := gs["CA"]; ok {
			if ca := document.Float(caObj); !floatEquals1(ca) {
				newGS["CA"] = ast.PdfReal(1.0)
				changed++
			}
		}

		// ca (fill alpha): must be 1.0
		if caObj, ok := gs["ca"]; ok {
			if ca := document.Float(caObj); !floatEquals1(ca) {
				newGS["ca"] = ast.PdfReal(1.0)
				changed++
			}
		}

		// BM (blend mode): must be Normal or Compatible
		if bmObj, ok := gs["BM"]; ok {
			if name, ok := bmObj.(ast.PdfName); ok {
				bm := string(name)
				if bm != "Normal" && bm != "Compatible" {
					newGS["BM"] = ast.PdfName("Normal")
					changed++
				}
			}
		}

		// SMask: must be None or absent
		if smObj, ok := gs["SMask"]; ok {
			if name, ok := smObj.(ast.PdfName); ok && string(name) == "None" {
				// Already None, fine.
			} else {
				newGS["SMask"] = ast.PdfName("None")
				changed++
			}
		}

		if changed == 0 {
			continue
		}

		if gsRef.ObjectNum > 0 {
			if alloc.AllocAt(gsRef.ObjectNum, newGS) {
				fixed += changed
			}
			continue
		}

		if inlineUpdates == nil {
			inlineUpdates = make(ast.PdfDict, len(gsDict))
		}
		inlineUpdates[gsName] = newGS
		inlineFixes += changed
	}

	if len(inlineUpdates) == 0 {
		return fixed, 0, false
	}

	newGSDict := make(ast.PdfDict, len(gsDict))
	maps.Copy(newGSDict, gsDict)
	maps.Copy(newGSDict, inlineUpdates)

	// Walk outward to the first indirect link and rewrite that object.
	if ref, ok := gsObj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		if alloc.AllocAt(ref.ObjectNum, newGSDict) {
			fixed += inlineFixes
		}
		return fixed, 0, false
	}

	if owner == nil {
		return fixed, 0, false
	}
	resObj, ok := owner["Resources"]
	if !ok {
		return fixed, 0, false
	}

	newRes := make(ast.PdfDict, len(resources))
	maps.Copy(newRes, resources)
	newRes["ExtGState"] = newGSDict

	if ref, ok := resObj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		if alloc.AllocAt(ref.ObjectNum, newRes) {
			fixed += inlineFixes
		}
		return fixed, 0, false
	}

	owner["Resources"] = newRes
	return fixed, inlineFixes, true
}

// fixXObjectTransparency removes SMask from images and Group/Transparency from forms.
//
// The XObjects are walked in sorted name order, as every walk sharing visited
// is. visited marks what the walk reaches, so an object reached by two paths
// is repaired from the first, and [maxTransparencyResourceDepth] can stop
// that path where the other would not have stopped. Walked in Go's map order,
// which path came first changed from run to run (CONTRIBUTING "Deterministic
// output").
func fixXObjectTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, resources ast.PdfDict, visited map[int]bool, depth int) int {
	if resources == nil || depth > maxTransparencyResourceDepth {
		return 0
	}

	xoObj, ok := resources["XObject"]
	if !ok {
		return 0
	}

	xoDict, err := document.ResolveDict(ctx, doc, xoObj)
	if err != nil || xoDict == nil {
		return 0
	}

	fixed := 0
	for _, name := range slices.Sorted(maps.Keys(xoDict)) {
		xoEntry := xoDict[name]
		var xoRef ast.PdfIndirectRef
		if ref, ok := xoEntry.(ast.PdfIndirectRef); ok {
			xoRef = ref
			if ref.ObjectNum > 0 {
				if visited[ref.ObjectNum] {
					continue
				}
				visited[ref.ObjectNum] = true
			}
		}

		// Read through the allocator. graphics rewrites these same XObject
		// streams one registry entry earlier, and the document still holds the
		// pre-repair copy, so rebuilding newDict from it below would discard
		// that write. The /Resources read further down inherits the same
		// working copy.
		stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, xoRef, xoEntry)
		if !ok {
			continue
		}

		modified := false
		pendingInline := 0
		newDict := make(ast.PdfDict, len(stream.Dict))
		maps.Copy(newDict, stream.Dict)

		subtype, _ := stream.Dict["Subtype"].(ast.PdfName)

		switch string(subtype) {
		case "Image":
			if _, ok := stream.Dict["SMask"]; ok {
				delete(newDict, "SMask")
				modified = true
				fixed++
			}
		case "Form":
			if groupObj, ok := stream.Dict["Group"]; ok {
				if isTransparencyGroup(ctx, doc, groupObj) {
					delete(newDict, "Group")
					modified = true
					fixed++
				}
			}
			// Recurse into Form XObject Resources, the full walk: a form may
			// itself declare forms, patterns and Type 3 fonts, and a
			// transparency group nested one level deeper is equally rendered
			// content. newDict is the working copy written below, so an inline
			// repair rides the same write that drops /Group instead of being
			// overwritten by it.
			if formRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && formRes != nil {
				n, pending, dirty := fixResourceTransparency(ctx, doc, alloc, formRes, newDict, visited, depth+1)
				fixed += n
				if dirty {
					modified = true
					pendingInline += pending
				}
			}
		}

		if modified && xoRef.ObjectNum > 0 {
			if alloc.AllocAt(xoRef.ObjectNum, ast.NewLazyStream(newDict, stream.Bytes)) {
				fixed += pendingInline
			}
		}
	}

	return fixed
}

// fixAnnotationTransparency walks page annotations and fixes transparency in
// their appearance stream resources.
func fixAnnotationTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, annotsObj ast.PdfObject, visited map[int]bool) int {
	annots, err := document.ResolveArray(ctx, doc, annotsObj)
	if err != nil || annots == nil {
		return 0
	}

	fixed := 0
	for _, annotObj := range annots {
		annotDict, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil || annotDict == nil {
			continue
		}

		apDict, err := document.ResolveDict(ctx, doc, annotDict["AP"])
		if err != nil || apDict == nil {
			continue
		}

		// ISO 32000-1, 12.5.5: /N, /R and /D each hold either an appearance
		// stream or a subdictionary of named appearance states. Only /N as a
		// stream was handled before, so a group or graphics state reached
		// through a state subdictionary, or through /D or /R, survived.
		for _, which := range []ast.PdfName{"N", "R", "D"} {
			entry, ok := apDict[which]
			if !ok {
				continue
			}

			resolved, err := document.ResolveObject(ctx, doc, entry)
			if err != nil {
				continue
			}

			switch v := resolved.(type) {
			case ast.PdfStream:
				fixed += fixAppearanceStream(ctx, doc, alloc, entry, v, visited)
			case ast.PdfDict:
				for _, state := range sortedDictKeys(v) {
					stateEntry := v[state]
					stateObj, err := document.ResolveObject(ctx, doc, stateEntry)
					if err != nil {
						continue
					}
					if stream, ok := stateObj.(ast.PdfStream); ok {
						fixed += fixAppearanceStream(ctx, doc, alloc, stateEntry, stream, visited)
					}
				}
			}
		}
	}

	return fixed
}

// fixAppearanceStream repairs one appearance form XObject: its own transparency
// group, and every transparency construct its /Resources reach.
//
// An appearance stream is a Form XObject (ISO 32000-1, 12.5.5), so ISO 19005-1
// §6.4 test 3 forbids a /Group with /S /Transparency on it exactly as on any
// other form. The previous walk repaired the graphics states inside its
// resources but never the group on the stream itself, which left the rule
// standing on every carrier that hides its group there.
//
// entry is the appearance value as stored, so an indirect reference can be
// written back; a direct stream has no object number and cannot be rewritten.
func fixAppearanceStream(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, entry ast.PdfObject, stream ast.PdfStream, visited map[int]bool) int {
	ref, isRef := entry.(ast.PdfIndirectRef)
	if isRef && ref.ObjectNum > 0 {
		if visited[ref.ObjectNum] {
			return 0
		}
		visited[ref.ObjectNum] = true
	}

	// Re-read through the allocator and ignore the stream the caller passed in.
	// fixAnnotationTransparency resolves appearance streams with
	// document.ResolveObject at two sites, once for /N, /R and /D and again for
	// each state in an appearance subdictionary, so the value arriving here is
	// the pre-repair copy. repairAppearanceStream writes these same objects
	// from need-appearances, ahead of transparency, and rebuilding work from
	// the stale copy would discard that write. Defending at the write site
	// covers both call sites at once, and the body below rides the same live
	// value.
	if isRef && ref.ObjectNum > 0 {
		if live, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, ref, entry); ok {
			stream = live
		}
	}

	work := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(work, stream.Dict)

	fixed := 0
	changed := false
	if groupObj, ok := stream.Dict["Group"]; ok && isTransparencyGroup(ctx, doc, groupObj) {
		delete(work, "Group")
		changed = true
	}

	pending := 0
	if res, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && res != nil {
		n, p, dirty := fixResourceTransparency(ctx, doc, alloc, res, work, visited, 1)
		fixed += n
		if dirty {
			changed = true
			pending = p
		}
	}

	if changed && isRef && ref.ObjectNum > 0 &&
		alloc.AllocAt(ref.ObjectNum, ast.NewLazyStream(work, stream.Bytes)) {
		fixed += pending
		if _, hadGroup := stream.Dict["Group"]; hadGroup {
			if _, keptGroup := work["Group"]; !keptGroup {
				fixed++
			}
		}
	}

	return fixed
}

func isTransparencyGroup(ctx context.Context, doc *document.Document, groupObj ast.PdfObject) bool {
	d, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || d == nil {
		return false
	}
	if s, ok := d["S"].(ast.PdfName); ok {
		return string(s) == "Transparency"
	}
	return false
}

func floatEquals1(f float64) bool {
	return math.Abs(f-1.0) < 1e-6
}
