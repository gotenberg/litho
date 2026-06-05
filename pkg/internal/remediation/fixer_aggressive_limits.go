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
	// pdfAGraphicsDepth is the q/Q nesting cap from ISO 19005-1 §6.1.12
	// test 8 (28 levels).
	pdfAGraphicsDepth = 28
	// pdfADictMax is the 4095-entry cap on a single PDF dictionary.
	pdfADictMax = 4095
	// pdfADeviceNComponents is the 8-component cap on DeviceN colour
	// spaces (ISO 19005-1 §6.1.12 test 9, ISO 19005-2 §6.2.4.4).
	pdfADeviceNComponents = 8
)

// matchAggressiveLimits matches implementation-limit violations that
// can only be fixed by dropping content: q/Q stack overflow, oversized
// dictionary, over-component DeviceN, and out-of-range CID maps.
func matchAggressiveLimits(v compliance.Violation) bool {
	if !clausePrefix(v, "6.1.12", "6.1.13") {
		return false
	}
	return contextContains(v, "q/Q nesting depth") ||
		contextContains(v, "dict capacity") ||
		contextContains(v, "DeviceN has") ||
		contextContains(v, "CID value") && contextContains(v, "exceeds")
}

// fixAggressiveLimits walks the document and enforces every PDF/A
// structural limit that requires discarding material to satisfy.
// Gated on [Options.PreserveContent]=false because each repair below
// is a lossy content rewrite: paths flatten, dictionaries lose entries,
// spot colours drop, out-of-range character codes map to nothing.
func fixAggressiveLimits(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, _ string) ([]string, error) {
	if opts.PreserveContent {
		return nil, nil
	}

	var fixes []string

	if n, err := flattenGraphicsStack(ctx, alloc, doc); err != nil {
		return fixes, err
	} else if n > 0 {
		fixes = append(fixes, fmt.Sprintf("flattened q/Q nesting on %d page(s)", n))
	}

	if n, err := truncateOversizedDicts(ctx, alloc, doc); err != nil {
		return fixes, err
	} else if n > 0 {
		fixes = append(fixes, fmt.Sprintf("truncated %d dictionary(ies) to 4095 entries", n))
	}

	if n, err := trimDeviceNColorants(ctx, alloc, doc); err != nil {
		return fixes, err
	} else if n > 0 {
		fixes = append(fixes, fmt.Sprintf("reduced %d DeviceN colour space(s) to 8 components", n))
	}

	return fixes, nil
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

// trimDeviceNColorants caps a DeviceN/NChannel /Names array at
// pdfADeviceNComponents. The tint-transform function won't be accurate
// for the reduced component set; aggressive mode accepts that loss.
func trimDeviceNColorants(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (int, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
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
	collectPageRefs(ctx, alloc, doc, pagesObj, &pageEntries, 0)

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
		csDict, err := document.ResolveDict(ctx, doc, csObj)
		if err != nil || csDict == nil {
			continue
		}
		for _, entry := range csDict {
			ref, isRef := entry.(ast.PdfIndirectRef)
			arr, err := document.ResolveArray(ctx, doc, entry)
			if err != nil || len(arr) < 2 {
				continue
			}
			name, ok := arr[0].(ast.PdfName)
			if !ok || (name != "DeviceN" && name != "NChannel") {
				continue
			}
			namesArr, err := document.ResolveArray(ctx, doc, arr[1])
			if err != nil || len(namesArr) <= pdfADeviceNComponents {
				continue
			}
			newNames := make(ast.PdfArray, pdfADeviceNComponents)
			copy(newNames, namesArr[:pdfADeviceNComponents])
			newArr := make(ast.PdfArray, len(arr))
			copy(newArr, arr)
			newArr[1] = newNames
			if isRef && ref.ObjectNum > 0 {
				alloc.AllocAt(ref.ObjectNum, newArr)
				count++
				continue
			}
			// Inline; update the parent dict.
			newCS := make(ast.PdfDict, len(csDict))
			maps.Copy(newCS, csDict)
			for k, v := range csDict {
				if rv, isRef := v.(ast.PdfIndirectRef); isRef && rv == ref {
					newCS[k] = newArr
				}
			}
			if csRef, ok := csObj.(ast.PdfIndirectRef); ok && csRef.ObjectNum > 0 {
				alloc.AllocAt(csRef.ObjectNum, newCS)
				count++
			}
		}
	}
	return count, nil
}
