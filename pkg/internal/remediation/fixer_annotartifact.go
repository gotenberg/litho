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

// Annotation flag bits, ISO 32000-1 Table 165. Only the flags the
// artifact-presence rules examine are defined here.
const (
	annotFlagInvisible    = 1 << 0 // bit 1
	annotFlagNoView       = 1 << 5 // bit 6
	annotFlagToggleNoView = 1 << 8 // bit 9
)

// matchAnnotArtifact matches the PDF/UA-2 violations that require an annotation to
// be an artifact (absent from the logical structure): Popup in the structure tree
// (§8.9.2.4.9), PrinterMark not an artifact (§8.9.2.4.14), and an Invisible or
// NoView annotation present in the structure (§8.9.2.2).
func matchAnnotArtifact(v compliance.Violation) bool {
	if v.Rule.Specification != "ISO_14289_2" {
		return false
	}
	switch v.Rule.Clause {
	case "8.9.2.4.9", "8.9.2.4.14", "8.9.2.2":
		return true
	}
	return false
}

// fixAnnotArtifact makes the offending annotations artifacts by removing their
// /StructParent entry, which is the back-pointer that places an annotation in the
// logical structure. Once /StructParent is absent the annotation is no longer in
// the structure, which is exactly what ISO 14289-2 §8.9.2.4.9, §8.9.2.4.14, and
// §8.9.2.2 require, and removal is the semantically correct repair: Popups and
// PrinterMarks are auxiliary content, and Invisible/NoView annotations must not
// appear in the reading order.
//
// Only /StructParent is removed; the forward OBJR reference in the structure tree
// is intentionally left in place. litho's annotation alt-text check exempts an
// annotation whose enclosing structure element carries /Alt by following that
// OBJR, so removing the OBJR could withdraw a valid exemption and raise a new
// violation, whereas removing only /StructParent cannot. The edit changes a logical
// back-pointer, not rendering, so it is registered as safetyResource.
func fixAnnotArtifact(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
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
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, 0)

	processed := make(map[int]bool)
	cleared := 0
	for _, pe := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		annots, err := document.ResolveArray(ctx, doc, pe.dict["Annots"])
		if err != nil {
			continue
		}
		for _, annotObj := range annots {
			ref, ok := annotObj.(ast.PdfIndirectRef)
			if !ok || ref.ObjectNum == 0 || processed[ref.ObjectNum] {
				continue
			}
			annot := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
			if annot == nil {
				continue
			}
			if !annotShouldBeArtifact(ctx, doc, annot) {
				continue
			}
			processed[ref.ObjectNum] = true

			patched := make(ast.PdfDict, len(annot))
			maps.Copy(patched, annot)
			delete(patched, "StructParent")
			alloc.AllocAt(ref.ObjectNum, patched)
			cleared++
		}
	}

	if cleared == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("made %d annotation(s) artifacts by removing /StructParent", cleared)}, nil
}

// annotShouldBeArtifact reports whether an annotation in the logical structure
// must instead be an artifact, mirroring the three detection conditions exactly:
// a Popup or PrinterMark subtype, or an Invisible flag, or a NoView flag without a
// ToggleNoView flag. Only annotations that already carry /StructParent qualify.
func annotShouldBeArtifact(ctx context.Context, doc *document.Document, annot ast.PdfDict) bool {
	if _, has := annot["StructParent"]; !has {
		return false
	}
	if subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"]); subtype == "Popup" || subtype == "PrinterMark" {
		return true
	}
	f := 0
	if fObj, ok := annot["F"]; ok {
		if flags, err := document.ResolveInt(ctx, doc, fObj); err == nil {
			f = int(flags)
		}
	}
	if f&annotFlagInvisible != 0 {
		return true
	}
	return f&annotFlagNoView != 0 && f&annotFlagToggleNoView == 0
}
