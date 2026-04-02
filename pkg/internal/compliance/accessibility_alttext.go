package compliance

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// --- 6. Figure alt text ---

func checkFigureAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	rule := figureAltRule(profile)

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	roleMap := loadRoleMap(ctx, doc, treeRoot)

	var results []Violation
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		// Resolve through RoleMap to catch non-standard types mapped to Figure.
		stdType := resolveStandardType(typeName, roleMap, profile)
		if stdType != "Figure" {
			return
		}

		if profile == "PDF/UA-1" {
			// PDF/UA-1 §7.3: Figure must have non-empty /Alt, or /ActualText (presence suffices).
			hasAlt := hasNonEmptyStringKey(ctx, doc, elem, "Alt")
			hasActualText := hasStringKeyPresent(ctx, doc, elem, "ActualText")
			if !hasAlt && !hasActualText {
				results = append(results, Violation{Rule: rule, Context: path})
			}
		} else {
			// PDF/UA-2 §8.2.5.28.2: Figure must have /Alt, /ActualText, or a /Caption child.
			// Presence (even empty) satisfies the requirement for PDF/UA-2.
			hasAlt := hasStringKeyPresent(ctx, doc, elem, "Alt")
			hasActualText := hasStringKeyPresent(ctx, doc, elem, "ActualText")
			hasCaptionChild := figureHasCaptionChild(ctx, doc, elem)
			if !hasAlt && !hasActualText && !hasCaptionChild {
				results = append(results, Violation{Rule: rule, Context: path})
			}
		}
	})

	return results
}

// --- 7. Artifact tagging ---

func checkArtifactTagging(ctx context.Context, _ *document.Document, _ ast.PdfDict, _ string) []Violation {
	// Artifact tagging checks require deep content stream analysis to determine
	// if headers/footers/page numbers are properly marked as Artifact. This is
	// a heuristic check that cannot be fully validated without rendering. We log
	// the check but return no violations — a future enhancement may add
	// heuristic detection of repeated header/footer content.
	return nil
}

// --- 8. Annotation alt text ---

func checkAnnotationAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	rule := annotAltRule(profile)

	// Build a map from annotation object number to whether the enclosing
	// structure element has a non-empty /Alt entry.
	structAltMap := buildAnnotStructAltMap(ctx, doc, catalog)

	pages := collectPages(ctx, doc, catalog)
	var results []Violation

	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			slog.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
			continue
		}

		for annotIdx, annotObj := range annots {
			if ctx.Err() != nil {
				return results
			}

			// Grab the object number before resolving, for structure tree lookup.
			annotObjNum := 0
			if ref, ok := annotObj.(ast.PdfIndirectRef); ok {
				annotObjNum = ref.ObjectNum
			}

			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}

			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])

			// Link, Widget, and Popup annotations are excluded from this check.
			// Popup annotations are explicitly exempt — they inherit content
			// from their parent annotation and do not need /Contents or /Alt.
			if subtype == "Link" || subtype == "Widget" || subtype == "Popup" {
				continue
			}

			// Check if annotation is hidden or has Rect outside the crop-box.
			if isAnnotHidden(ctx, doc, annot) || isAnnotOutsideCropBox(ctx, doc, annot, page) {
				continue
			}

			// Check /Contents key — must be present and non-empty.
			if contentsObj, hasContents := annot["Contents"]; hasContents {
				contentsStr, err := document.ResolveString(ctx, doc, contentsObj)
				if err == nil && strings.TrimSpace(string(contentsStr)) != "" {
					continue
				}
			}

			// Annotation lacks /Contents — check the enclosing structure element for /Alt.
			if annotObjNum > 0 && structAltMap[annotObjNum] {
				continue
			}

			loc := fmt.Sprintf("page %d, annot %d (/%s)", pageIdx, annotIdx, subtype)
			results = append(results, Violation{Rule: rule, Context: loc})
		}
	}

	return results
}

// buildAnnotStructAltMap walks the structure tree and returns a set of annotation
// object numbers whose enclosing structure element has a non-empty /Alt entry.
// PDF structure elements reference annotations via OBJR (object reference) dicts
// in their /K children: { /Type /OBJR /Obj <indirect ref to annotation> }.
func buildAnnotStructAltMap(ctx context.Context, doc *document.Document, catalog ast.PdfDict) map[int]bool {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	result := make(map[int]bool)
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, _ string) {
		// Check if this structure element has a non-empty /Alt.
		if !hasNonEmptyStringKey(ctx, doc, elem, "Alt") {
			return
		}

		// Scan /K children for OBJR dicts pointing to annotations.
		kObj, ok := elem["K"]
		if !ok {
			return
		}
		collectOBJRAnnotObjNums(ctx, doc, kObj, result)
	})

	return result
}

// collectOBJRAnnotObjNums finds OBJR dicts in the /K children of a structure element
// and records the object numbers of their /Obj references.
func collectOBJRAnnotObjNums(ctx context.Context, doc *document.Document, kObj ast.PdfObject, dest map[int]bool) {
	// /K can be a single item or an array.
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Try as a single dict.
		checkOBJR(ctx, doc, kObj, dest)
		return
	}
	for _, item := range arr {
		checkOBJR(ctx, doc, item, dest)
	}
}

// checkOBJR checks if obj is an OBJR dict and records the /Obj object number.
func checkOBJR(ctx context.Context, doc *document.Document, obj ast.PdfObject, dest map[int]bool) {
	dict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || dict == nil {
		return
	}
	typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
	if typeName != "OBJR" {
		return
	}
	// /Obj is an indirect reference to the annotation.
	if ref, ok := dict["Obj"].(ast.PdfIndirectRef); ok {
		dest[ref.ObjectNum] = true
	}
}

// --- 8b. Widget /AA ---

// checkWidgetAA validates that Widget annotations do not have /AA (additional actions)
// entries. For PDF/UA-1, the presence of /AA on a Widget is forbidden per §7.18.
// For PDF/UA-2, if /AA is present, the Widget must have a /Contents entry (§8.10.2.3 t02).
func checkWidgetAA(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	pages := collectPages(ctx, doc, catalog)
	var results []Violation

	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			slog.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
			continue
		}

		for annotIdx, annotObj := range annots {
			if ctx.Err() != nil {
				return results
			}

			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}

			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if subtype != "Widget" {
				continue
			}

			_, hasAA := annot["AA"]
			if !hasAA {
				continue
			}

			loc := fmt.Sprintf("page %d, widget annot %d", pageIdx+1, annotIdx)

			if profile == "PDF/UA-2" {
				// PDF/UA-2 §8.10.2.3 t02: if Widget has /AA, /Contents must be present.
				if _, hasContents := annot["Contents"]; !hasContents {
					results = append(results, Violation{
						Rule:    Rule_14289_2_8_10_2_3_t02,
						Context: loc,
					})
				}
			} else {
				// PDF/UA-1: Widget annotations should not have /AA trigger actions.
				results = append(results, Violation{
					Rule:    Rule_14289_1_7_18_1_t03,
					Context: fmt.Sprintf("widget annotation has /AA at %s", loc),
				})
			}
		}
	}

	return results
}
