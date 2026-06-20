package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// groupCSIsDevice reports whether a transparency group's /CS object is a device
// colour space (DeviceRGB/DeviceCMYK/DeviceGray), given as a bare name or the
// first element of a colour-space array.
func groupCSIsDevice(ctx context.Context, doc *document.Document, csObj ast.PdfObject) bool {
	name, err := document.ResolveName(ctx, doc, csObj)
	if err != nil || name == "" {
		if arr, aerr := document.ResolveArray(ctx, doc, csObj); aerr == nil && len(arr) > 0 {
			name, _ = document.ResolveName(ctx, doc, arr[0])
		}
	}
	switch name {
	case "DeviceRGB", "DeviceCMYK", "DeviceGray":
		return true
	default:
		return false
	}
}

// matchGroupCS matches transparency /Group CS violations: mismatched
// blending CS vs OutputIntent, or a page with transparency that lacks
// any /Group entry at all. Covers ISO 19005-2/3 §6.2.10 and
// ISO 19005-4 §6.2.9 test 2.
func matchGroupCS(v compliance.Violation) bool {
	// A device-colour transparency group blending space (ISO 19005 §6.2.3.3 /
	// §6.2.4.3, flagged with a "transparency group" context) is repaired by
	// dropping /CS so the blending space falls back to the OutputIntent
	// profile, not by installing an OutputIntent for it.
	if clausePrefix(v, "6.2.3.3", "6.2.4.3") && contextContains(v, "transparency group") {
		return true
	}
	if !clausePrefix(v, "6.2.9", "6.2.10") {
		return false
	}
	if contextContains(v, "blending CS") && contextContains(v, "incompatible") {
		return true
	}
	// Contexts like "[page 1]" (no detail) flag a page that contains
	// transparency but is missing the /Group key altogether.
	return descriptionContains(v, "shall include the Group key") ||
		descriptionContains(v, "include the Group key")
}

// fixGroupCS handles two repair paths on transparency /Group dicts:
//
//  1. When a /Group has a /CS entry that conflicts with the document
//     OutputIntent, drop /CS so the renderer falls back to the intent
//     profile (ISO 32000-1 §11.6.6).
//  2. When a page that requires a /Group has none, synthesize a minimal
//     transparency group so the page is well-formed.
func fixGroupCS(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	var count int
	// blendingCSRef is allocated lazily and shared by every page that
	// needs a synthesized transparency group.
	var blendingCSRef ast.PdfIndirectRef
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pageDict := livePageDict(ctx, alloc, doc, page)
		groupObj, hasGroup := pageDict["Group"]
		if !hasGroup {
			// /CS is required by ISO 19005-2 §6.2.10 test 2 when the
			// document lacks a matching PDF/A OutputIntent. Use an
			// ICCBased sRGB space: it is device independent, so the
			// blending rules of every part (including ISO 19005-4
			// §6.2.4.3, which keys device spaces to the OutputIntent
			// family) are satisfied without depending on which intent
			// other fixers install.
			if blendingCSRef.ObjectNum == 0 {
				blendingCSRef = alloc.Alloc(ast.NewStream(ast.PdfDict{"N": ast.PdfInt(3)}, sRGBICCProfile))
			}
			newPage := make(ast.PdfDict, len(pageDict)+1)
			maps.Copy(newPage, pageDict)
			newPage["Group"] = ast.PdfDict{
				"Type": ast.PdfName("Group"),
				"S":    ast.PdfName("Transparency"),
				"CS":   ast.PdfArray{ast.PdfName("ICCBased"), blendingCSRef},
			}
			if page.Ref.ObjectNum > 0 {
				alloc.AllocAt(page.Ref.ObjectNum, newPage)
				count++
			}
			continue
		}
		var groupDict ast.PdfDict
		if r, ok := groupObj.(ast.PdfIndirectRef); ok {
			groupDict = resolveFromAllocOrDoc(ctx, alloc, doc, r)
		} else if d, err := document.ResolveDict(ctx, doc, groupObj); err == nil {
			groupDict = d
		}
		if groupDict == nil {
			continue
		}
		csObj, hasCS := groupDict["CS"]
		if !hasCS {
			continue
		}
		newGroup := make(ast.PdfDict, len(groupDict))
		maps.Copy(newGroup, groupDict)
		if groupCSIsDevice(ctx, doc, csObj) {
			// A device blending space conforms only with a matching OutputIntent,
			// which the document may lack. Replace it with device-independent
			// ICCBased sRGB so the group is valid unconditionally, rather than
			// dropping /CS (which would leave a transparency page without a
			// blending space, itself an ISO 19005-2 §6.2.10 test 2 violation).
			if blendingCSRef.ObjectNum == 0 {
				blendingCSRef = alloc.Alloc(ast.NewStream(ast.PdfDict{"N": ast.PdfInt(3)}, sRGBICCProfile))
			}
			newGroup["CS"] = ast.PdfArray{ast.PdfName("ICCBased"), blendingCSRef}
		} else {
			// A non-device (e.g. ICCBased) blending space incompatible with the
			// OutputIntent: drop /CS so the renderer falls back to the intent
			// profile (ISO 32000-1 §11.6.6).
			delete(newGroup, "CS")
		}

		var groupRef ast.PdfIndirectRef
		if r, ok := groupObj.(ast.PdfIndirectRef); ok {
			groupRef = r
		}
		if groupRef.ObjectNum > 0 {
			alloc.AllocAt(groupRef.ObjectNum, newGroup)
		} else {
			newPage := make(ast.PdfDict, len(pageDict))
			maps.Copy(newPage, pageDict)
			newPage["Group"] = newGroup
			alloc.AllocAt(page.Ref.ObjectNum, newPage)
		}
		count++
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("normalised /Group on %d page(s)", count)}, nil
}
