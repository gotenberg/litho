package remediation

import (
	"context"
	"fmt"
	"maps"
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
// dictionary, over-component DeviceN, and out-of-range CID values, which
// no repair answers yet (see [limitRepairsFor]).
func matchAggressiveLimits(v compliance.Violation) bool {
	if !clausePrefix(v, "6.1.12", "6.1.13") {
		return false
	}
	return contextContains(v, "q/Q nesting depth") ||
		contextContains(v, "dict capacity") ||
		contextContains(v, "DeviceN has") ||
		contextContains(v, "CID value") && contextContains(v, "exceeds")
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
// A CID-value violation (6.1.12 test 10, 6.1.13 test 10) matches this fixer
// too, but no repair here answers it, so it selects none.
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

// trimDeviceNColorants caps a DeviceN/NChannel /Names array at limit
// colourants. The tint-transform function won't be accurate for the reduced
// component set; aggressive mode accepts that loss.
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
		resDict, err := document.ResolveDict(ctx, doc, resObj)
		if err != nil || resDict == nil {
			continue
		}
		csObj, ok := resDict["ColorSpace"]
		if !ok {
			continue
		}
		// Alloc-aware, dict kind: the parent dictionary is rebuilt and written
		// back at csRef.ObjectNum below, so reading it from the document
		// discards a peer's pending edit to that same object.
		var csDict ast.PdfDict
		if csRef, isRef := csObj.(ast.PdfIndirectRef); isRef {
			csDict = resolveFromAllocOrDoc(ctx, alloc, doc, csRef)
		} else if d, derr := document.ResolveDict(ctx, doc, csObj); derr == nil {
			csDict = d
		}
		if csDict == nil {
			continue
		}
		// Inline colour spaces accumulate: rebuilding the parent once per
		// entry would keep only the last, the trap fixExtGStateDict records.
		var inlineUpdates ast.PdfDict
		for csName, entry := range csDict {
			ref, isRef := entry.(ast.PdfIndirectRef)
			// Alloc-aware, and the helper matches the object KIND: liveArray
			// for an array, since resolveFromAllocOrDoc above returns a dict.
			// The array is rebuilt wholesale below and written back at
			// ref.ObjectNum, so reading it from the document discards a peer's
			// pending edit. aggressive-limits is the LAST registry entry
			// (fixer.go:157), so every colour-space writer precedes it, and
			// devicen (:115) rewrites DeviceN arrays at these same objects.
			arr := liveArray(ctx, alloc, doc, entry)
			if len(arr) < 2 {
				continue
			}
			name, ok := arr[0].(ast.PdfName)
			if !ok || (name != "DeviceN" && name != "NChannel") {
				continue
			}
			namesArr, err := document.ResolveArray(ctx, doc, arr[1])
			if err != nil || len(namesArr) <= limit {
				continue
			}
			newNames := make(ast.PdfArray, limit)
			copy(newNames, namesArr[:limit])
			newArr := make(ast.PdfArray, len(arr))
			copy(newArr, arr)
			newArr[1] = newNames
			if isRef && ref.ObjectNum > 0 {
				alloc.AllocAt(ref.ObjectNum, newArr)
				count++
				continue
			}
			// Inline: key the replacement by NAME. Matching an indirect
			// reference equal to ref cannot work here, because an inline entry
			// leaves ref at its zero value, so the trimmed array was dropped
			// and the untouched dictionary still counted as a repair.
			if inlineUpdates == nil {
				inlineUpdates = ast.PdfDict{}
			}
			inlineUpdates[csName] = newArr
		}
		if len(inlineUpdates) == 0 {
			continue
		}
		csRef, isRef := csObj.(ast.PdfIndirectRef)
		if !isRef || csRef.ObjectNum == 0 {
			continue
		}
		newCS := make(ast.PdfDict, len(csDict))
		maps.Copy(newCS, csDict)
		maps.Copy(newCS, inlineUpdates)
		alloc.AllocAt(csRef.ObjectNum, newCS)
		count += len(inlineUpdates)
	}
	return count, nil
}
