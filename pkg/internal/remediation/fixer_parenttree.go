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
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// maxParentTreeMCID bounds a marked-content id read from a structure element.
// It is an anti-DoS guard, not a spec limit: ISO 32000-2, 14.7.4.4 puts no
// ceiling on an MCID, but the ParentTree is indexed BY that id, so one element
// saying /K 12000000 sizes a 12,000,001 entry array and then writes a null into
// every slot. Measured: a 578-byte file allocated 183 MB that way, and at
// 16 bytes per entry an id near the int32 maximum reaches tens of gigabytes.
// The cap sits far above the ids real content streams carry, and it bounds one
// page's array at about 16 MiB.
//
// # Corpus evidence
//
//   - All corpora, 5171 files: the largest id is 12522 and the p99 is 8877,
//     reading raw and Flate-compressed content streams, so the cap leaves ~84x
//     headroom.
//   - govdocs thread_000_000314.pdf: carries that largest id, 12522.
//   - pdfjs: the largest id is 9757.
const maxParentTreeMCID = 1 << 20

// matchParentTree matches the PDF/UA untagged-content violations the
// ParentTree rebuild repairs: ISO 14289-1 §7.1 t3 and ISO 14289-2 §8.2.2 t1
// fire when marked content is not reachable through the ParentTree even
// though the structure tree parents it correctly.
func matchParentTree(v compliance.Violation) bool {
	return (v.Rule.Clause == "7.1" && v.Rule.TestNumber == 3) ||
		(v.Rule.Clause == "8.2.2" && v.Rule.TestNumber == 1)
}

// fixParentTree rebuilds the StructTreeRoot /ParentTree from the structure
// tree: every structure element holding integer /K marked-content ids with a
// /Pg page gets registered under that page's /StructParents key. Per ISO
// 32000-2 §14.7.4.4, the /ParentTree is the sole reverse link from a marked
// content sequence back to its structure element: a consumer maps the page's
// /StructParents key plus the operator's /MCID into the tree to decide whether
// content is tagged. Content the wrapping fixers parent only through the /K
// chain therefore reads as untagged until that reverse link exists, which is
// what ISO 14289-1 §7.1 t3 and ISO 14289-2 §8.2.2 t1 flag. Existing Nums
// entries and /StructParents keys are preserved; only missing registrations
// are added.
func fixParentTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	stRef, _ := catalogDict["StructTreeRoot"].(ast.PdfIndirectRef)
	stDict := resolveFromAllocOrDoc(ctx, alloc, doc, stRef)
	if stDict == nil {
		return nil, nil
	}

	// Collect per-page MCID → element registrations from every object
	// sweptObjects yields: structure elements are always indirect and may
	// live outside the reachable /K chains while remediation rounds are in
	// flight.
	perPage := make(map[int]map[int]ast.PdfIndirectRef)
	for objNum := range sweptObjects(doc) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		d := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if d == nil {
			continue
		}
		if t, _ := document.ResolveName(ctx, doc, d["Type"]); t != "" && t != "StructElem" {
			continue
		}
		if _, isSE := d["S"]; !isSE {
			continue
		}
		pgRef, ok := d["Pg"].(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		for _, mcid := range structElemMCIDs(ctx, doc, d["K"]) {
			if perPage[pgRef.ObjectNum] == nil {
				perPage[pgRef.ObjectNum] = make(map[int]ast.PdfIndirectRef)
			}
			perPage[pgRef.ObjectNum][mcid] = ast.PdfIndirectRef{ObjectNum: objNum}
		}
	}
	if len(perPage) == 0 {
		return nil, nil
	}

	// Existing flat Nums (bail on a Kids-structured tree: merging into it
	// safely needs the full number-tree writer, and the wrapping fixers
	// only ever produce flat trees).
	nums, nextKey, flat := existingParentTreeNums(ctx, alloc, doc, stDict)
	if !flat {
		return nil, nil
	}
	existingKeys := make(map[int]bool)
	for i := 0; i+1 < len(nums); i += 2 {
		if k, err := document.ResolveInt(ctx, doc, nums[i]); err == nil {
			existingKeys[intconv.Clamp(k)] = true
		}
	}

	registered := 0
	pageNums := make([]int, 0, len(perPage))
	for pn := range perPage {
		pageNums = append(pageNums, pn)
	}
	slices.Sort(pageNums)
	for _, pageNum := range pageNums {
		pageDict := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: pageNum})
		if pageDict == nil {
			continue
		}
		key := -1
		if spObj, ok := pageDict["StructParents"]; ok {
			if k, err := document.ResolveInt(ctx, doc, spObj); err == nil {
				key = intconv.Clamp(k)
			}
		}
		if key >= 0 && existingKeys[key] {
			continue // page already registered
		}
		if key < 0 {
			key = nextKey
			nextKey++
			newPage := make(ast.PdfDict, len(pageDict)+1)
			maps.Copy(newPage, pageDict)
			newPage["StructParents"] = ast.PdfInt(key)
			alloc.AllocAt(pageNum, newPage)
		}

		mcids := perPage[pageNum]
		maxMCID := -1
		for mcid := range mcids {
			if mcid > maxMCID {
				maxMCID = mcid
			}
		}
		arr := make(ast.PdfArray, maxMCID+1)
		for i := range arr {
			arr[i] = ast.PdfNull{}
		}
		for mcid, ref := range mcids {
			arr[mcid] = ref
		}
		nums = append(nums, ast.PdfInt(key), arr)
		registered++
	}
	if registered == 0 {
		return nil, nil
	}

	newST := make(ast.PdfDict, len(stDict)+2)
	maps.Copy(newST, stDict)
	newST["ParentTree"] = ast.PdfDict{"Nums": nums}
	newST["ParentTreeNextKey"] = ast.PdfInt(nextKey)
	if stRef.ObjectNum > 0 {
		alloc.AllocAt(stRef.ObjectNum, newST)
	}
	return []string{fmt.Sprintf("registered %d page(s) in the structure ParentTree", registered)}, nil
}

// structElemMCIDs extracts the integer marked-content ids from a structure
// element's /K: a bare integer, integers inside a /K array, and MCR
// dictionaries ({/Type /MCR /MCID n}).
func structElemMCIDs(ctx context.Context, doc *document.Document, kObj ast.PdfObject) []int {
	var out []int
	collect := func(item ast.PdfObject) {
		switch v := item.(type) {
		case ast.PdfInt:
			if v >= 0 && v <= maxParentTreeMCID {
				out = append(out, intconv.Clamp(v))
			}
		default:
			if d, err := document.ResolveDict(ctx, doc, item); err == nil && d != nil {
				if t, _ := document.ResolveName(ctx, doc, d["Type"]); t == "MCR" {
					if n, ok := d["MCID"].(ast.PdfInt); ok && n >= 0 && n <= maxParentTreeMCID {
						out = append(out, intconv.Clamp(n))
					}
				}
			}
		}
	}
	if kObj == nil {
		return nil
	}
	if arr, err := document.ResolveArray(ctx, doc, kObj); err == nil && arr != nil {
		for _, item := range arr {
			collect(item)
		}
		return out
	}
	collect(kObj)
	return out
}
