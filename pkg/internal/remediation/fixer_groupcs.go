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
// 6.2.9 t2 ask for: /S /Transparency, and a /CS that reads as a colour space
// with no resources, as validation reads it
// ([compliance.GroupHasBlendingSpace]). A /CS naming a /ColorSpace entry, or
// resolving to null, gives none.
func groupHasBlendingSpace(ctx context.Context, doc *document.Document, group ast.PdfDict) bool {
	// Best-effort: an unreadable /S names no transparency group.
	if s, _ := document.ResolveName(ctx, doc, group["S"]); s != "Transparency" {
		return false
	}
	return compliance.GroupHasBlendingSpace(ctx, doc, group)
}

// matchGroupCS matches transparency /Group CS violations: a page group
// blending in a device colour space no OutputIntent covers (see
// [flagsDeviceGroup]), or a page with transparency that lacks a blending
// colour space (see [requiresPageGroup]).
func matchGroupCS(v compliance.Violation) bool {
	return flagsDeviceGroup(v) || requiresPageGroup(v)
}

// flagsDeviceGroup reports whether v flags the device colour space the page's
// transparency group blends in, which no OutputIntent covers: ISO 19005-1,
// 6.2.3.3 and ISO 19005-2/3/4, 6.2.4.3, with the "page N: transparency group"
// context the detector gives the page's own group alone. It is repaired by
// replacing the blending space with a device-independent one, not by
// installing an OutputIntent for it.
func flagsDeviceGroup(v compliance.Violation) bool {
	return clausePrefix(v, "6.2.3.3", "6.2.4.3") && contextContains(v, "transparency group")
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

// flaggedPages returns the 1-based numbers of the pages the violations flag
// matches name, in a context that is "page N" or begins "page N:".
func flaggedPages(violations []compliance.Violation, flags func(compliance.Violation) bool) map[int]bool {
	pages := make(map[int]bool)
	for _, v := range violations {
		if !flags(v) {
			continue
		}
		num, ok := strings.CutPrefix(v.Context, "page ")
		if !ok {
			continue
		}
		num, _, _ = strings.Cut(num, ":")
		if n, err := strconv.Atoi(num); err == nil && n > 0 {
			pages[n] = true
		}
	}
	return pages
}

// fixGroupCS handles two repair paths on page transparency /Group dicts:
//
//  1. When a violation flags the device colour space a page /Group blends
//     in, which conforms only with a matching OutputIntent, replace it with
//     ICCBased sRGB (see [flagsDeviceGroup]).
//  2. When a page that requires a blending colour space has no /Group, or a
//     /Group without one, synthesize a minimal transparency group or give
//     it the sRGB /CS.
//
// A page gets a blending space only when a violation asks for it (see
// [requiresPageGroup]), and keeps a device one no violation flags. ISO
// 19005-2/3, 6.2.10 t2 and ISO 19005-4, 6.2.9 t2 require a group only on a
// page that contains transparency, and ISO 19005-1, 6.4 t3 forbids a page
// transparency group outright. The transparency fixer runs first and removes
// the page groups 6.4 t3 flags, and synthesizing a group on every page lacking
// one put them back: at PDF/A-1 the output failed 6.4 t3 again, and only a
// second remediation round removed them.
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
//   - All corpora: 562 files give a page a transparency group blending in a
//     device space. On the 514 where group-cs runs at PDF/A-1b, PDF/A-2b or
//     PDF/A-4, a finding names every such group, so keeping the others moves
//     no corpus output; litho repairleft_4.pdf carries one no finding names.
func fixGroupCS(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, violations []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	needsGroup := flaggedPages(violations, requiresPageGroup)
	deviceGroup := flaggedPages(violations, flagsDeviceGroup)
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
		case deviceGroup[i+1] && hasCS && groupCSIsDevice(ctx, doc, csObj):
			// A device blending space conforms only with a matching OutputIntent,
			// and this one has none. Replace it with device-independent ICCBased
			// sRGB so the group is valid unconditionally, rather than dropping
			// /CS (which would leave a transparency page without a blending
			// space, itself an ISO 19005-2 §6.2.10 test 2 violation). A device
			// blending space no violation flags is covered (ISO 19005-2/3/4,
			// 6.2.4.3), and replacing it would change how the group composites.
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
