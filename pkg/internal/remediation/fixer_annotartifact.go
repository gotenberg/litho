package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// markupAnnotSubtypes lists the markup annotation types per ISO 32000-2,
// 12.5.6.2. It mirrors compliance.markupAnnotationSubtypes (kept in sync with
// that spec table): for these subtypes ISO 14289-2 §8.9.2.3 requires the
// annotation stay enclosed in an Annot structure element, so the §8.9.2.2 repair
// must not remove it from the structure.
//
//nolint:gochecknoglobals // static spec table, mirrors compliance.markupAnnotationSubtypes
var markupAnnotSubtypes = map[ast.PdfName]bool{
	"Text": true, "FreeText": true, "Line": true, "Square": true,
	"Circle": true, "Polygon": true, "PolyLine": true, "Highlight": true,
	"Underline": true, "Squiggly": true, "StrikeOut": true, "Caret": true,
	"Stamp": true, "Ink": true, "FileAttachment": true, "Sound": true,
	"Redact": true,
}

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

// annotRepair names how fixAnnotArtifact repairs one annotation.
type annotRepair int

const (
	// annotRepairNone leaves the annotation untouched.
	annotRepairNone annotRepair = iota
	// annotRepairRemoveStructParent removes /StructParent so the annotation
	// becomes an artifact, absent from the logical structure.
	annotRepairRemoveStructParent
	// annotRepairClearInvisible clears the Invisible flag and keeps
	// /StructParent. The Invisible flag has no effect for standard annotation
	// types (ISO 32000-2:2020, Table 167: it suppresses display only of an
	// unknown annotation type with no handler), so clearing it on a markup
	// annotation satisfies §8.9.2.2 t1 without breaking §8.9.2.3, which requires
	// the markup annotation stay enclosed in its Annot structure element.
	annotRepairClearInvisible
)

// fixAnnotArtifact repairs annotations that ISO 14289-2 §8.9.2.4.9, §8.9.2.4.14,
// and §8.9.2.2 require be artifacts (absent from the logical structure). Most are
// repaired by removing /StructParent, the back-pointer that places an annotation
// in the structure: Popups and PrinterMarks are auxiliary content, and Invisible
// or NoView annotations must not appear in the reading order.
//
// A markup annotation flagged Invisible is the exception. §8.9.2.3 requires it
// stay enclosed in its Annot structure element, so removing /StructParent would
// trade the §8.9.2.2 violation for a §8.9.2.3 one. Because the Invisible flag is a
// no-op for standard annotation types (ISO 32000-2 Table 167), clearing it
// satisfies §8.9.2.2 without disturbing the structure. See [annotArtifactRepair].
//
// When /StructParent is removed, the forward OBJR reference in the structure tree
// is intentionally left in place. litho's annotation alt-text check exempts an
// annotation whose enclosing structure element carries /Alt by following that
// OBJR, so removing the OBJR could withdraw a valid exemption and raise a new
// violation, whereas removing only /StructParent cannot. Neither edit changes
// rendering, so both are registered as safetyResource.
func fixAnnotArtifact(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
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
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool))

	processed := make(map[int]bool)
	removed, clearedInvisible := 0, 0
	for _, pe := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Read through the allocator: annotations and forbidden-annots run
		// first and write the /Annots they filtered at its own object number.
		for _, annotObj := range liveArray(ctx, alloc, doc, pe.dict["Annots"]) {
			ref, ok := annotObj.(ast.PdfIndirectRef)
			if !ok || ref.ObjectNum == 0 || processed[ref.ObjectNum] {
				continue
			}
			annot := liveAnnotation(ctx, alloc, doc, ref)
			if annot == nil {
				continue
			}
			repair := annotArtifactRepair(ctx, doc, annot)
			if repair == annotRepairNone {
				continue
			}
			processed[ref.ObjectNum] = true

			patched := make(ast.PdfDict, len(annot))
			maps.Copy(patched, annot)
			switch repair {
			case annotRepairClearInvisible:
				f := annotFlags(ctx, doc, annot)
				patched["F"] = ast.PdfInt(f &^ annotFlagInvisible)
				clearedInvisible++
			case annotRepairRemoveStructParent:
				delete(patched, "StructParent")
				removed++
			case annotRepairNone:
				// Filtered out above; listed for switch exhaustiveness.
			}
			alloc.AllocAt(ref.ObjectNum, patched)
		}
	}

	var descs []string
	if removed > 0 {
		descs = append(descs, fmt.Sprintf("made %d annotation(s) artifacts by removing /StructParent", removed))
	}
	if clearedInvisible > 0 {
		descs = append(descs, fmt.Sprintf("cleared the Invisible flag on %d markup annotation(s)", clearedInvisible))
	}
	return descs, nil
}

// annotFlags returns the annotation's /F flag bits, or 0 if absent or unreadable.
func annotFlags(ctx context.Context, doc *document.Document, annot ast.PdfDict) int {
	fObj, ok := annot["F"]
	if !ok {
		return 0
	}
	flags, err := document.ResolveInt(ctx, doc, fObj)
	if err != nil {
		return 0
	}
	return intconv.Clamp(flags)
}

// annotArtifactRepair decides how an annotation that trips a §8.9.2.2 / §8.9.2.4
// artifact-presence rule must be repaired. The three detection conditions are a
// Popup or PrinterMark subtype, an Invisible flag, or a NoView flag without a
// ToggleNoView flag; only annotations that already carry /StructParent qualify.
//
// A markup annotation (ISO 32000-2 §12.5.6.2) that trips solely on the Invisible
// flag is repaired by clearing that flag rather than removing /StructParent: the
// flag is a no-op for standard annotation types (Table 167), while §8.9.2.3
// requires the markup annotation stay enclosed in its Annot structure element, so
// removing /StructParent would trade a §8.9.2.2 violation for a §8.9.2.3 one. All
// other cases (Popup/PrinterMark, NoView, or a non-markup Invisible annotation)
// are repaired by removing /StructParent.
func annotArtifactRepair(ctx context.Context, doc *document.Document, annot ast.PdfDict) annotRepair {
	if _, has := annot["StructParent"]; !has {
		return annotRepairNone
	}
	subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
	if subtype == "Popup" || subtype == "PrinterMark" {
		return annotRepairRemoveStructParent
	}
	f := annotFlags(ctx, doc, annot)
	if f&annotFlagNoView != 0 && f&annotFlagToggleNoView == 0 {
		return annotRepairRemoveStructParent
	}
	if f&annotFlagInvisible != 0 {
		if markupAnnotSubtypes[subtype] {
			return annotRepairClearInvisible
		}
		return annotRepairRemoveStructParent
	}
	return annotRepairNone
}
