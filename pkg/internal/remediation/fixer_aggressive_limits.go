package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

const (
	// pdfAGraphicsDepth is the q/Q nesting cap from ISO 19005-1, 6.1.12
	// test 8 and ISO 19005-2/3, 6.1.13 test 8 (28 levels).
	pdfAGraphicsDepth = 28
	// pdfADictMax is the 4095-entry cap on a single PDF dictionary. ISO
	// 19005-1, 6.1.12 test 6 imposes it from the PDF Reference 1.4
	// architectural limits (Table C.1), which is the reference veraPDF's
	// PDF/A-1 profile cites. ISO 32000-1, Table C.1 lists no dictionary
	// limit, and no ISO 19005-2/3/4 rule in veraPDF's profiles states one,
	// so the cap binds PDF/A-1 alone. The detector is
	// compliance.maxDictCap1.
	pdfADictMax = 4095
	// pdfA1DeviceNComponents is the 8-component cap on DeviceN colour
	// spaces (ISO 19005-1, 6.1.12 test 9, from PDF Reference 1.4, Table
	// C.1).
	pdfA1DeviceNComponents = 8
	// pdfA23DeviceNComponents is the 32-colourant cap of ISO 19005-2/3,
	// 6.1.13 test 9, the ISO 32000-1, Table C.1 value. ISO 19005-4 states no
	// DeviceN limit. Both caps mirror compliance.checkDeviceNCS, which raises
	// the violation.
	pdfA23DeviceNComponents = 32
)

// matchAggressiveLimits matches implementation-limit violations that
// can only be fixed by dropping content: q/Q stack overflow, oversized
// dictionary and over-component DeviceN.
//
// An out-of-range CID value (ISO 19005-1, 6.1.12 test 10; ISO 19005-2/3,
// 6.1.13 test 10) is not matched: no repair here answers it (see
// [limitRepairsFor]), and file-structure and real-limits claim it already. A
// content-tier claim only put it on [Report.ContentResidual], which a rollback
// appends to the residual it revalidates, listing it twice.
func matchAggressiveLimits(v compliance.Violation) bool {
	if !clausePrefix(v, "6.1.12", "6.1.13") {
		return false
	}
	return contextContains(v, "q/Q nesting depth") ||
		contextContains(v, "dict capacity") ||
		contextContains(v, "DeviceN has")
}

// fixAggressiveLimits enforces the PDF/A implementation limits that can only
// be met by discarding material. Gated on [Options.PreserveContent]=false
// because each repair below is a lossy content rewrite: paths flatten,
// dictionaries lose entries, spot colours drop.
//
// Each repair runs only when the profile's part states the limit and a
// violation of that very rule is among those matched, so one violation never
// triggers an unrelated repair. See [limitRepairsFor].
func fixAggressiveLimits(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, violations []compliance.Violation, profile string) ([]string, error) {
	if opts.PreserveContent {
		return nil, nil
	}

	repairs := limitRepairsFor(violations, profile)
	var fixes []string

	if repairs.qNesting {
		if n, err := flattenGraphicsStack(ctx, alloc, doc); err != nil {
			return fixes, err
		} else if n > 0 {
			fixes = append(fixes, fmt.Sprintf("flattened q/Q nesting on %d page(s)", n))
		}
	}

	if repairs.dictCap {
		if n, err := truncateOversizedDicts(ctx, alloc, doc); err != nil {
			return fixes, err
		} else if n > 0 {
			fixes = append(fixes, fmt.Sprintf("truncated %d dictionary(ies) to %d entries", n, pdfADictMax))
		}
	}

	if repairs.deviceNMax > 0 {
		if n, err := trimDeviceNColorants(ctx, alloc, doc, repairs.deviceNMax); err != nil {
			return fixes, err
		} else if n > 0 {
			fixes = append(fixes, fmt.Sprintf("reduced %d DeviceN colour space(s) to %d components", n, repairs.deviceNMax))
		}
	}

	return fixes, nil
}

// limitRepairs selects the repairs one fixAggressiveLimits run performs.
type limitRepairs struct {
	// qNesting flattens q/Q nesting past [pdfAGraphicsDepth].
	qNesting bool
	// dictCap truncates dictionaries past [pdfADictMax].
	dictCap bool
	// deviceNMax is the colourant count DeviceN spaces are trimmed to, or 0
	// when no trim applies.
	deviceNMax int
}

// limitRepairsFor maps the matched violations to the repairs they call for
// under profile. A repair is selected only by the rule that states its limit
// in the profile's own part:
//
//   - q/Q nesting: ISO 19005-1, 6.1.12 test 8 and ISO 19005-2/3, 6.1.13
//     test 8, 28 levels in each. ISO 19005-4 states no nesting limit.
//   - dictionary entries: ISO 19005-1, 6.1.12 test 6 alone, from PDF
//     Reference 1.4, Table C.1. ISO 32000-1, Table C.1 lists no dictionary
//     limit and ISO 19005-2/3 6.1.13 has no such test, so a part 2 or 3
//     dictionary above 4095 entries conforms and keeps every entry.
//   - DeviceN colourants: 8 under ISO 19005-1, 6.1.12 test 9, and 32 under
//     ISO 19005-2/3, 6.1.13 test 9. Trimming a part 2 or 3 space to the
//     part 1 cap would drop up to 24 colourants the profile permits.
//
// Any other rule selects nothing.
func limitRepairsFor(violations []compliance.Violation, profile string) limitRepairs {
	part := compliance.PDFAPart(profile)
	var r limitRepairs
	for _, v := range violations {
		switch {
		case part == 1 && v.Rule == compliance.Rule_19005_1_6_1_12_t08,
			part == 2 && v.Rule == compliance.Rule_19005_2_6_1_13_t08,
			part == 3 && v.Rule == compliance.Rule_19005_3_6_1_13_t08:
			r.qNesting = true
		case part == 1 && v.Rule == compliance.Rule_19005_1_6_1_12_t06:
			r.dictCap = true
		case part == 1 && v.Rule == compliance.Rule_19005_1_6_1_12_t09:
			r.deviceNMax = pdfA1DeviceNComponents
		case part == 2 && v.Rule == compliance.Rule_19005_2_6_1_13_t09,
			part == 3 && v.Rule == compliance.Rule_19005_3_6_1_13_t09:
			r.deviceNMax = pdfA23DeviceNComponents
		}
	}
	return r
}

// flattenGraphicsStack appends balancing Q operators to any page content
// stream whose q/Q nesting exceeds pdfAGraphicsDepth. Pops run to the
// end of the stream, so visible drawing up to the overflow point is
// preserved; everything after is discarded with the overflow.
func flattenGraphicsStack(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (int, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return 0, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return 0, fmt.Errorf("resolving pages: %w", err)
	}

	count := 0
	for pageNum, page := range pages {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		ops, contentsObj, err := livePageContentOps(ctx, alloc, doc, page)
		if err != nil || contentsObj == nil {
			continue
		}
		depth, maxDepth := 0, 0
		for _, op := range ops {
			switch op.Operator {
			case "q":
				depth++
				if depth > maxDepth {
					maxDepth = depth
				}
			case "Q":
				if depth > 0 {
					depth--
				}
			}
		}
		if maxDepth <= pdfAGraphicsDepth {
			continue
		}
		clipped := clipQNesting(ops, pdfAGraphicsDepth)
		newData := serializeOps(clipped)
		if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum+1); err != nil {
			return count, fmt.Errorf("replacing content stream on page %d: %w", pageNum+1, err)
		}
		count++
	}
	return count, nil
}

// clipQNesting rewrites ops so no q/Q pair nests deeper than limit.
// Extra q tokens are dropped; orphaned Q tokens matching the dropped
// q's are removed alongside.
func clipQNesting(ops []content.Operation, limit int) []content.Operation {
	out := make([]content.Operation, 0, len(ops))
	depth := 0
	skippedDepth := 0
	for _, op := range ops {
		switch op.Operator {
		case "q":
			if depth >= limit {
				skippedDepth++
				continue
			}
			depth++
			out = append(out, op)
			continue
		case "Q":
			if skippedDepth > 0 {
				skippedDepth--
				continue
			}
			if depth > 0 {
				depth--
			}
			out = append(out, op)
			continue
		}
		out = append(out, op)
	}
	return out
}

// truncateOversizedDicts walks every indirect dict, array, or stream
// and deletes keys past the 4095-entry cap. Keys are ordered
// alphabetically so the result is deterministic. PDF/A-spec essentials
// (/Type, /Subtype, /Length, /Size, /Root) are preserved as first-class
// entries.
func truncateOversizedDicts(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (int, error) {
	truncDict := func(d ast.PdfDict) (ast.PdfDict, bool) {
		if len(d) <= pdfADictMax {
			return nil, false
		}
		keep := truncateDict(d)
		return keep, true
	}
	truncArr := func(a ast.PdfArray) (ast.PdfArray, bool) {
		return nil, false
	}
	return walkIndirectObjects(ctx, alloc, doc, truncDict, truncArr)
}

// truncateDict selects up to pdfADictMax entries from d. Priority keys
// (Type, Subtype, Length, Filter, Size, Root, Info, ID, Pages, Metadata)
// come first; the rest fill in alphabetical order.
func truncateDict(d ast.PdfDict) ast.PdfDict {
	priority := map[ast.PdfName]bool{
		"Type": true, "Subtype": true, "Length": true, "Filter": true,
		"Size": true, "Root": true, "Info": true, "ID": true,
		"Pages": true, "Metadata": true, "OutputIntents": true,
		"DecodeParms": true, "FormType": true, "BBox": true,
	}
	out := make(ast.PdfDict, pdfADictMax)
	for k := range priority {
		if v, ok := d[k]; ok {
			out[k] = v
		}
	}
	remaining := make([]ast.PdfName, 0, len(d))
	for k := range d {
		if priority[k] {
			continue
		}
		remaining = append(remaining, k)
	}
	sort.Slice(remaining, func(i, j int) bool {
		return string(remaining[i]) < string(remaining[j])
	})
	for _, k := range remaining {
		if len(out) >= pdfADictMax {
			break
		}
		out[k] = d[k]
	}
	return out
}

// trimDeviceNColorants caps the colourant names of every DeviceN colour space
// a page's /Resources /ColorSpace dictionary declares at limit, keeping the
// first limit. The tint-transform function won't be accurate for
// the reduced component set; aggressive mode accepts that loss.
//
// Every level is read through the allocator and written back where it lives:
// an indirect array at its own object number, an inline one into its
// /ColorSpace dictionary, an inline /ColorSpace into its /Resources, and
// inline /Resources into the page. aggressive-limits is the last registry
// entry, so every colour-space writer precedes it: devicen rewrites DeviceN
// arrays at their object numbers, and output-intent rewrites /Resources with
// the /ColorSpace dictionary inlined, leaving the indirect one unused. A read
// from the document there trims a dictionary the output no longer references.
// A space counts as trimmed only when its write lands.
//
// # Corpus evidence
//
//   - litho limits_2b.pdf and verapdf 6-1-12-t09-fail-a.pdf: the DeviceN
//     array, its /ColorSpace and the /Resources are all inline in the page,
//     the shape the trim used to give up on, so both kept their 33 and 12
//     colourants and their test 9 violation in aggressive mode. Trimmed,
//     neither output draws test 9 from litho or veraPDF, and
//     6-1-12-t09-fail-a.pdf passes veraPDF at PDF/A-1b.
func trimDeviceNColorants(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, limit int) (int, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return 0, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return 0, nil
	}
	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return 0, nil
	}
	var pageEntries []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pageEntries, make(map[int]bool), 0)

	count := 0
	for _, pe := range pageEntries {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}
		resObj, ok := pe.dict["Resources"]
		if !ok {
			continue
		}
		resDict := liveDictValue(ctx, alloc, doc, resObj)
		csObj, ok := resDict["ColorSpace"]
		if !ok {
			continue
		}
		csDict := liveDictValue(ctx, alloc, doc, csObj)
		if csDict == nil {
			continue
		}
		// Inline colour spaces accumulate: rebuilding the parent once per
		// entry would keep only the last, the trap fixExtGStateDict records.
		// Keyed by NAME, since an inline entry has no object number.
		var inlineUpdates ast.PdfDict
		for _, csName := range slices.Sorted(maps.Keys(csDict)) {
			entry := csDict[csName]
			newArr, trimmed := trimDeviceNArray(ctx, alloc, doc, entry, limit)
			if !trimmed {
				continue
			}
			if ref, isRef := entry.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
				alloc.AllocAt(ref.ObjectNum, newArr)
				count++
				continue
			}
			if inlineUpdates == nil {
				inlineUpdates = ast.PdfDict{}
			}
			inlineUpdates[csName] = newArr
		}
		if len(inlineUpdates) == 0 {
			continue
		}
		newCS := make(ast.PdfDict, len(csDict))
		maps.Copy(newCS, csDict)
		maps.Copy(newCS, inlineUpdates)
		if writePageColorSpace(alloc, pe, resObj, resDict, csObj, newCS) {
			count += len(inlineUpdates)
		}
	}
	return count, nil
}

// trimDeviceNArray returns the DeviceN colour space entry, read through the
// allocator, with its colourant names cut to the first limit, and false when
// entry is no DeviceN space or already within limit. The array is
// read alloc-first because devicen, which precedes this fixer, rewrites
// DeviceN arrays at their own object numbers, and the copy replaces the array.
func trimDeviceNArray(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, entry ast.PdfObject, limit int) (ast.PdfArray, bool) {
	arr := liveArray(ctx, alloc, doc, entry)
	if len(arr) < 2 {
		return nil, false
	}
	name, ok := arr[0].(ast.PdfName)
	if !ok || (name != "DeviceN" && name != "NChannel") {
		return nil, false
	}
	names := liveArray(ctx, alloc, doc, arr[1])
	if len(names) <= limit {
		return nil, false
	}
	newNames := make(ast.PdfArray, limit)
	copy(newNames, names[:limit])
	newArr := make(ast.PdfArray, len(arr))
	copy(newArr, arr)
	newArr[1] = newNames
	return newArr, true
}

// writePageColorSpace writes newCS where the page's /ColorSpace dictionary
// lives: at its own object number, else into the /Resources it sits in, at the
// /Resources object number or, when those are inline too, into the page.
// resDict and pe.dict are the live dictionaries the caller read, so the copies
// keep every earlier fixer's edit. Returns false when nothing addressable holds
// the dictionary, a page without an object number.
func writePageColorSpace(alloc *writer.ObjAllocator, pe annotPageEntry, resObj ast.PdfObject, resDict ast.PdfDict, csObj ast.PdfObject, newCS ast.PdfDict) bool {
	if ref, ok := csObj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newCS)
		return true
	}
	newRes := make(ast.PdfDict, len(resDict))
	maps.Copy(newRes, resDict)
	newRes["ColorSpace"] = newCS
	if ref, ok := resObj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newRes)
		return true
	}
	if pe.ref.ObjectNum == 0 {
		return false
	}
	newPage := make(ast.PdfDict, len(pe.dict))
	maps.Copy(newPage, pe.dict)
	newPage["Resources"] = newRes
	alloc.AllocAt(pe.ref.ObjectNum, newPage)
	return true
}
