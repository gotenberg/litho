package remediation

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

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

// groupHasBlendingSpace reports whether the page group dict group gives the
// page the blending colour space ISO 19005-2/3, 6.2.10 t2 and ISO 19005-4,
// 6.2.9 t2 ask for: /S /Transparency, and a /CS that is not the null object,
// which ISO 32000-1, 7.3.7 reads as an absent entry.
func groupHasBlendingSpace(ctx context.Context, doc *document.Document, group ast.PdfDict) bool {
	// Best-effort: an unreadable /S names no transparency group.
	if s, _ := document.ResolveName(ctx, doc, group["S"]); s != "Transparency" {
		return false
	}
	csObj, ok := group["CS"]
	if !ok {
		return false
	}
	cs, err := document.ResolveObject(ctx, doc, csObj)
	if err != nil {
		doc.Logger.Debug("remediation: group-cs: cannot resolve the page group /CS", "error", err)
		return false
	}
	_, isNull := cs.(ast.PdfNull)
	return cs != nil && !isNull
}

// matchGroupCS matches transparency /Group CS violations: mismatched
// blending CS vs OutputIntent, or a page with transparency that lacks
// any /Group entry at all. Covers ISO 19005-2/3 §6.2.10 and
// ISO 19005-4 §6.2.9 test 2.
func matchGroupCS(v compliance.Violation) bool {
	// A device-colour transparency group blending space (ISO 19005 §6.2.3.3 /
	// §6.2.4.3, flagged with a "transparency group" context) is repaired by
	// replacing it with a device-independent one, not by installing an
	// OutputIntent for it.
	if clausePrefix(v, "6.2.3.3", "6.2.4.3") && contextContains(v, "transparency group") {
		return true
	}
	if !clausePrefix(v, "6.2.9", "6.2.10") {
		return false
	}
	if contextContains(v, "blending CS") && contextContains(v, "incompatible") {
		return true
	}
	return requiresPageGroup(v)
}

// requiresPageGroup reports whether v is the rule that asks a page for a
// transparency group with a blending colour space: ISO 19005-2 and 19005-3,
// 6.2.10 t2 and ISO 19005-4, 6.2.9 t2. Each flags a page that contains
// transparency in a document without a PDF/A OutputIntent, whether its /Group
// is missing or carries no /CS, and names it "page N".
func requiresPageGroup(v compliance.Violation) bool {
	return v.Rule == compliance.Rule_19005_2_6_2_10_t02 ||
		v.Rule == compliance.Rule_19005_3_6_2_10_t02 ||
		v.Rule == compliance.Rule_19005_4_6_2_9_t02
}

// pagesRequiringGroup returns the 1-based numbers of the pages violations
// flag under [requiresPageGroup].
func pagesRequiringGroup(violations []compliance.Violation) map[int]bool {
	pages := make(map[int]bool)
	for _, v := range violations {
		if !requiresPageGroup(v) {
			continue
		}
		num, ok := strings.CutPrefix(v.Context, "page ")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(num); err == nil && n > 0 {
			pages[n] = true
		}
	}
	return pages
}

// fixGroupCS handles two repair paths on page transparency /Group dicts:
//
//  1. When a /Group blends in a device colour space, which conforms only
//     with a matching OutputIntent, replace it with ICCBased sRGB.
//  2. When a page that requires a blending colour space has no /Group, or a
//     /Group without one, synthesize a minimal transparency group or give
//     it the sRGB /CS.
//
// A page gets a blending space only when a violation asks for it (see
// [requiresPageGroup]). ISO 19005-2/3, 6.2.10 t2 and ISO 19005-4, 6.2.9 t2
// require a group only on a page that contains transparency, and ISO
// 19005-1, 6.4 t3 forbids a page transparency group outright. The
// transparency fixer runs first and removes the page groups 6.4 t3 flags, and
// synthesizing a group on every page lacking one put them back: at PDF/A-1
// the output failed 6.4 t3 again, and only a second remediation round
// removed them.
//
// A device-independent blending space is kept. The fixer used to drop every
// /CS that was not a device space, for a finding that named one incompatible
// with the OutputIntent and that validation no longer reports: it stripped
// valid ICCBased blending spaces, its own sRGB among them on a later round,
// and left those pages failing 6.2.10 t2 where the source passed.
//
// # Corpus evidence
//
//   - All corpora: of 516 sampled files, 130 took that second PDF/A-1b
//     round, default and aggressive alike, and one round now repairs them to
//     the same residual: 67 verapdf, 45 pdfjs, 9 govdocs and 9 pdfcpu files.
//     Runs taking more than one round fell from 149 to 19.
//   - govdocs thread_000_000163: its source fails 6.2.10 t2 on one page, and
//     four pages blend in an ICCBased space. Its PDF/A-2b remediation took two
//     rounds and left seven pages failing 6.2.10 t2; one round now leaves
//     none, and veraPDF 1.30 passes the output.
//   - All corpora: over the PDF/A-1b, PDF/A-2b and PDF/UA-1 remediations,
//     default and aggressive, of those 516 files, no residual violation
//     rises, and veraPDF 1.30 reports the same failed rules on every PDF/A-1b
//     output that changes.
func fixGroupCS(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, violations []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	needsGroup := pagesRequiringGroup(violations)
	var count int
	// blendingCSRef is allocated lazily and shared by every page that
	// needs a synthesized transparency group.
	var blendingCSRef ast.PdfIndirectRef
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pageDict := livePageDict(ctx, alloc, doc, page)
		groupObj, hasGroup := pageDict["Group"]
		if !hasGroup {
			if !needsGroup[i+1] {
				continue
			}
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
		newGroup := make(ast.PdfDict, len(groupDict)+1)
		maps.Copy(newGroup, groupDict)
		switch {
		case hasCS && groupCSIsDevice(ctx, doc, csObj):
			// A device blending space conforms only with a matching OutputIntent,
			// which the document may lack. Replace it with device-independent
			// ICCBased sRGB so the group is valid unconditionally, rather than
			// dropping /CS (which would leave a transparency page without a
			// blending space, itself an ISO 19005-2 §6.2.10 test 2 violation).
		case needsGroup[i+1] && !groupHasBlendingSpace(ctx, doc, groupDict):
			// The page ISO 19005-2/3 §6.2.10 t2 or ISO 19005-4 §6.2.9 t2
			// flags has a group, but no transparency group blending space: it
			// takes the one a synthesized group carries.
			newGroup["S"] = ast.PdfName("Transparency")
		default:
			// A device-independent blending space conforms under every part:
			// ISO 19005-2/3 §6.2.4.3 and ISO 19005-4 §6.2.4.3 constrain device
			// spaces alone, and §6.2.10 t2 / §6.2.9 t2 ask for the /CS it is.
			continue
		}
		if blendingCSRef.ObjectNum == 0 {
			blendingCSRef = alloc.Alloc(ast.NewStream(ast.PdfDict{"N": ast.PdfInt(3)}, sRGBICCProfile))
		}
		newGroup["CS"] = ast.PdfArray{ast.PdfName("ICCBased"), blendingCSRef}

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
