package remediation

import (
	"context"
	"fmt"
	"maps"
	"math"
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

		// Fix ExtGState entries in page Resources.
		if page.Resources != nil {
			n, pending, dirty := fixExtGStateDict(ctx, doc, alloc, page.Resources, workPage)
			totalFixed += n
			if dirty {
				pageDirty, pagePending = true, pagePending+pending
			}
		}

		// Fix XObjects in page Resources (SMask on images, Group on forms).
		totalFixed += fixXObjectTransparency(ctx, doc, alloc, page.Resources)

		// Fix ExtGState inside the resources of a pattern the page declares.
		totalFixed += fixPatternTransparency(ctx, doc, alloc, page.Resources)

		// Fix ExtGState inside the resources of a Type 3 font the page declares.
		totalFixed += fixType3FontTransparency(ctx, doc, alloc, page.Resources)

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

		// Fix annotation appearance stream resources.
		if annotsObj, ok := pageDict["Annots"]; ok {
			totalFixed += fixAnnotationTransparency(ctx, doc, alloc, annotsObj)
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
func fixPatternTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, resources ast.PdfDict) int {
	if resources == nil {
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

	visited := make(map[int]bool, len(patDict))
	fixed := 0
	for _, entry := range patDict {
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}

		resolved, err := document.ResolveObject(ctx, doc, entry)
		if err != nil || resolved == nil {
			continue
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

		n, pending, dirty := fixExtGStateDict(ctx, doc, alloc, res, work)
		fixed += n
		if !dirty {
			continue
		}
		ref, isRef := entry.(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum <= 0 {
			continue
		}
		written := false
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
// leaving an /ExtGState inside a Type 3 font flagged and unrepaired. Measured on
// the corpus, the carriers declare NO /ExtGState on the page at all, so the
// existing walks find nothing to fix.
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
func fixType3FontTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, resources ast.PdfDict) int {
	if resources == nil {
		return 0
	}

	fontDict, err := document.ResolveDict(ctx, doc, resources["Font"])
	if err != nil || fontDict == nil {
		return 0
	}

	visited := make(map[int]bool, len(fontDict))
	fixed := 0
	for _, entry := range fontDict {
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}

		fd, err := document.ResolveDict(ctx, doc, entry)
		if err != nil || fd == nil {
			continue
		}
		subtype, err := document.ResolveName(ctx, doc, fd["Subtype"])
		if err != nil || subtype != "Type3" {
			continue
		}

		// The font's own /Resources, when it declares one. The font object is
		// the nearest indirect ancestor: the corpus carriers declare
		// /Resources, /ExtGState and the /GS entry all inline on it, so there
		// is nothing below the font to rewrite.
		if own, err := document.ResolveDict(ctx, doc, fd["Resources"]); err == nil && own != nil {
			work := make(ast.PdfDict, len(fd))
			maps.Copy(work, fd)

			n, pending, dirty := fixExtGStateDict(ctx, doc, alloc, own, work)
			fixed += n
			if dirty {
				if ref, isRef := entry.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 &&
					alloc.AllocAt(ref.ObjectNum, work) {
					fixed += pending
				}
			}
		}

		// A glyph procedure may carry /Resources of its own, and the font's
		// own /Resources does not cover it. Measured on veraPDF 6-2-10-t04-fail-b:
		// the font holds one non-conforming /CA and its three /CharProcs
		// streams hold one each, so repairing only the font leaves three
		// behind, which veraPDF reports and litho does not.
		fixed += fixType3CharProcTransparency(ctx, doc, alloc, fd)
	}

	return fixed
}

// fixType3CharProcTransparency fixes transparency in the resources of every
// glyph procedure a Type 3 font declares.
//
// ISO 32000-1, 9.6.5 scopes a Type 3 font's /Resources to its /CharProcs, but
// a glyph procedure is a content stream and may declare /Resources of its own;
// the veraPDF carriers type theirs as Form XObjects with a full resource
// dictionary. Those are separate indirect objects, so each is written
// directly: the ancestor to rewrite is the glyph stream itself.
//
// visited breaks reference cycles and shared glyph procedures: two names in
// /CharProcs may address one stream.
func fixType3CharProcTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fontDict ast.PdfDict) int {
	charProcs, err := document.ResolveDict(ctx, doc, fontDict["CharProcs"])
	if err != nil || charProcs == nil {
		return 0
	}

	visited := make(map[int]bool, len(charProcs))
	fixed := 0
	for _, entry := range charProcs {
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
		res, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if err != nil || res == nil {
			continue
		}

		work := make(ast.PdfDict, len(stream.Dict))
		maps.Copy(work, stream.Dict)

		n, pending, dirty := fixExtGStateDict(ctx, doc, alloc, res, work)
		fixed += n
		if dirty && isRef && ref.ObjectNum > 0 &&
			alloc.AllocAt(ref.ObjectNum, ast.NewLazyStream(work, stream.Bytes)) {
			fixed += pending
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
// enclosing page, Type 3 font, Form XObject or pattern. Measured on the corpus,
// 14 of 16 veraPDF §6.4 carriers store the whole chain inline, and the repair
// silently wrote nothing for all of them.
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
func fixExtGStateDict(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, resources ast.PdfDict, owner ast.PdfDict) (int, int, bool) {
	gsObj, ok := resources["ExtGState"]
	if !ok {
		return 0, 0, false
	}

	gsDict, err := document.ResolveDict(ctx, doc, gsObj)
	if err != nil || gsDict == nil {
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

		gs, err := document.ResolveDict(ctx, doc, gsEntry)
		if err != nil || gs == nil {
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
func fixXObjectTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, resources ast.PdfDict) int {
	if resources == nil {
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
	for _, xoEntry := range xoDict {
		var xoRef ast.PdfIndirectRef
		if ref, ok := xoEntry.(ast.PdfIndirectRef); ok {
			xoRef = ref
		}

		resolved, err := document.ResolveObject(ctx, doc, xoEntry)
		if err != nil {
			continue
		}

		stream, ok := resolved.(ast.PdfStream)
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
			// Recurse into Form XObject Resources. newDict is the working copy
			// written below, so an inline repair rides the same write that drops
			// /Group instead of being overwritten by it.
			if formRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && formRes != nil {
				n, pending, dirty := fixExtGStateDict(ctx, doc, alloc, formRes, newDict)
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
func fixAnnotationTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, annotsObj ast.PdfObject) int {
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

		apObj, ok := annotDict["AP"]
		if !ok {
			continue
		}

		apDict, err := document.ResolveDict(ctx, doc, apObj)
		if err != nil || apDict == nil {
			continue
		}

		// Check /N (normal appearance).
		if nObj, ok := apDict["N"]; ok {
			resolved, err := document.ResolveObject(ctx, doc, nObj)
			if err == nil {
				if stream, ok := resolved.(ast.PdfStream); ok {
					if res, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && res != nil {
						// The appearance stream is the nearest indirect ancestor.
						work := make(ast.PdfDict, len(stream.Dict))
						maps.Copy(work, stream.Dict)

						n, pending, dirty := fixExtGStateDict(ctx, doc, alloc, res, work)
						fixed += n
						if ref, isRef := nObj.(ast.PdfIndirectRef); dirty && isRef && ref.ObjectNum > 0 &&
							alloc.AllocAt(ref.ObjectNum, ast.NewLazyStream(work, stream.Bytes)) {
							fixed += pending
						}
					}
				}
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
