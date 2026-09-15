package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func checkFigureAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	rule := figureAltRule(profile)

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	roleMap := LoadRoleMap(ctx, doc, treeRoot)

	var results []Violation
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		if FigureNeedsAltText(ctx, doc, elem, roleMap, profile) {
			results = append(results, Violation{Rule: rule, Context: path})
		}
	})

	return results
}

// FigureNeedsAltText reports whether elem is a Figure structure element that
// lacks the alternate text PDF/UA requires. An element is a Figure when its type
// stands for Figure: the role map applies first, as veraPDF builds its SEFigure
// from the standard type, so a standard Figure the /RoleMap remaps is no Figure,
// and a namespaced type resolves through its namespace's /RoleMapNS
// (ISO 32000-2:2020, 14.8.6). A remediation fixer calls it so it repairs exactly
// the figures this validator flags, with no drift. roleMap comes from
// [LoadRoleMap]; profile selects the PDF/UA-1 §7.3 or PDF/UA-2 §8.2.5.28.2
// requirement.
func FigureNeedsAltText(ctx context.Context, doc *document.Document, elem ast.PdfDict, roleMap map[ast.PdfName]ast.PdfName, profile string) bool {
	typeName, _ := document.ResolveName(ctx, doc, elem["S"])
	if iso32005Key(ctx, &walkState{doc: doc, roleMap: roleMap}, elem, typeName) != "Figure" {
		return false
	}
	return !figureSatisfiesAltText(ctx, doc, elem, profile)
}

// figureSatisfiesAltText reports whether a Figure already carries the alternate
// text its profile requires. PDF/UA-1 §7.3 needs a non-empty /Alt or a present
// /ActualText; PDF/UA-2 §8.2.5.28.2 a present /Alt or /ActualText (presence
// alone suffices, even when empty). The UA-2 clause lists those two properties
// alone, so a Caption kid does not stand in for them: veraPDF reports a Figure
// whose only alternative is a Caption.
//
// Presence is read as veraPDF reads it: /Alt counts only as a string
// ([stringKeyPresent]) and /ActualText as a string or a name
// ([actualTextPresent]); a null value is absent (ISO 32000-1, 7.3.7).
func figureSatisfiesAltText(ctx context.Context, doc *document.Document, elem ast.PdfDict, profile string) bool {
	if profile == "PDF/UA-1" {
		return hasNonEmptyStringKey(ctx, doc, elem, "Alt") ||
			actualTextPresent(ctx, doc, elem)
	}
	return stringKeyPresent(ctx, doc, elem, "Alt") ||
		actualTextPresent(ctx, doc, elem)
}

func checkArtifactTagging(ctx context.Context, _ *document.Document, _ ast.PdfDict, _ string) []Violation {
	// Artifact tagging checks require deep content stream analysis to determine
	// if headers/footers/page numbers are properly marked as Artifact. This is
	// a heuristic check that cannot be fully validated without rendering, so
	// no violations are emitted. A future enhancement may add heuristic
	// detection of repeated header/footer content.
	return nil
}

// markupAnnotationSubtypes lists the markup annotation types per ISO
// 32000-2, 12.5.6.2 (annotations that carry a /T entry and convey user
// commentary). RichMedia, Screen, 3D, Movie, and the other non-markup types
// are deliberately absent. Projection is also absent: ISO 32000-2 §12.5.6.2
// does not classify it as a markup annotation, and veraPDF's markup model
// (PDMarkupAnnot) omits it, so §8.9.2.3 t1 (markup-in-Annot nesting) does not
// apply to a Projection annotation.
//
//nolint:gochecknoglobals // static spec table.
var markupAnnotationSubtypes = map[ast.PdfName]bool{
	"Text": true, "FreeText": true, "Line": true, "Square": true,
	"Circle": true, "Polygon": true, "PolyLine": true, "Highlight": true,
	"Underline": true, "Squiggly": true, "StrikeOut": true, "Caret": true,
	"Stamp": true, "Ink": true, "FileAttachment": true, "Sound": true,
	"Redact": true,
}

func checkAnnotationAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	// PDF/UA-2 has no generic "markup annotation needs Contents/Alt" rule: it
	// keys alternate descriptions per annotation subtype (§8.9.2.4.x, handled by
	// checkPDFUA2AnnotationConstraints), and §8.9.2.3 t1 is a nesting rule
	// (markup must be enclosed in Annot, handled by checkPDFUA2AnnotStructNesting),
	// not an alt-text rule. This UA-1 §7.18.1 t2 model does not apply under UA-2.
	if profile == "PDF/UA-2" {
		return nil
	}

	rule := annotAltRule(profile)

	// veraPDF reads an annotation's Alt from the structure element its
	// /StructParent names in the structure parent tree (GFPDAnnot.getAlt), not
	// from an element whose OBJR references it. Measured with an /Alt reached
	// one way and not the other.
	treeRoot, trErr := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if trErr != nil {
		doc.Logger.Debug("compliance.accessibility: cannot resolve structure tree root", "error", trErr)
	}

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
			doc.Logger.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
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

			// Widget annotations are excluded from this check. Link is NOT
			// excluded under PDF/UA-1: ISO 14289-1 §7.18.1 t2 exempts only
			// Widget/hidden/out-of-cropbox, so a Link lacking both /Contents
			// and an enclosing-element /Alt fails (verified live on the Link in
			// 6-3-2-t01-fail-b remediated to PDF/UA-1). Under PDF/UA-2 the
			// markup-subtype filter below still exempts Link (not a markup type).
			if subtype == "Widget" {
				continue
			}
			// A Popup is not exempt, with or without a /Parent. ISO 14289-1
			// §7.18.1 t2 exempts only Widget, hidden and out-of-cropbox
			// annotations, and veraPDF follows the clause: measured on
			// annotstruct_ua2, whose two Popups carry a /Parent, carry no
			// /Contents and both draw the rule. litho used to treat a parented
			// Popup as inheriting its parent's content, which the clause does
			// not say. Under PDF/UA-2 the markup-subtype filter below still
			// exempts every Popup (Popup is not a markup type).

			// ISO 14289-2 §8.9.2.3 covers markup annotations only, so a
			// non-markup subtype is out of its scope. A /Contents-less
			// RichMedia annotation fails its per-subtype description rule
			// (§8.9.2.4.19), never §8.9.2.3. PDF/UA-1 §7.18.1 keeps the
			// wider scope.
			if profile == "PDF/UA-2" && !markupAnnotationSubtypes[subtype] {
				continue
			}

			// Skip hidden annotations or those outside the crop box.
			if isAnnotHidden(ctx, doc, annot) || isAnnotOutsideCropBox(ctx, doc, annot, page) {
				continue
			}

			// ISO 14289-1 §7.18.1 t2: a non-empty /Contents, or a non-empty /Alt
			// on the enclosing structure element ([hasNonEmptyStringKey]).
			if hasNonEmptyStringKey(ctx, doc, annot, "Contents") {
				continue
			}
			if parent := annotStructParent(ctx, doc, treeRoot, annot); parent != nil && hasNonEmptyStringKey(ctx, doc, parent, "Alt") {
				continue
			}

			loc := fmt.Sprintf("page %d, annot %d (/%s)", pageIdx, annotIdx, subtype)
			results = append(results, Violation{Rule: rule, Context: loc})
		}
	}

	return results
}

// checkAnnotContentsAltMatch validates PDF/UA-2 §8.9.4.2 t1: when an annotation
// has a /Contents entry and its enclosing structure element has an /Alt entry,
// the two decoded values shall be identical. The enclosing element is the one
// the annotation's /StructParent names ([annotStructParent]), as veraPDF's
// GFPDAnnot.getAlt reads it; an OBJR that references the annotation does not
// make an element its enclosing one. Measured with an /Alt reached one way and
// not the other.
func checkAnnotContentsAltMatch(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	var results []Violation
	for pageIdx, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
		}
		annots, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for annotIdx, annotObj := range annots {
			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}
			contents, ok := decodedStringKey(ctx, doc, annot, "Contents")
			if !ok {
				continue
			}
			parent := annotStructParent(ctx, doc, treeRoot, annot)
			if parent == nil {
				continue
			}
			if alt, ok := decodedStringKey(ctx, doc, parent, "Alt"); ok && alt != contents {
				results = append(results, failf(Rule_14289_2_8_9_4_2_t01,
					"page %d, annot %d: /Contents differs from enclosing structure element /Alt", pageIdx, annotIdx)...)
			}
		}
	}
	return results
}

// checkWidgetAA validates that Widget annotations do not have /AA (additional actions)
// entries. For PDF/UA-1, the presence of /AA on a Widget is forbidden per §7.18.
// For PDF/UA-2, if /AA is present, the Widget must have a /Contents entry (§8.10.2.3 t02).
func checkWidgetAA(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: cannot resolve structure tree root", "error", err)
	}
	r := newNSRoleResolver(ctx, doc, treeRoot)
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
			doc.Logger.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
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

			// veraPDF's containsAA is a knownKey on /AA, so an /AA written as null is
			// absent (ISO 32000-1, 7.3.7), measured on a widget without /Contents.
			if !nonNullKeyPresent(ctx, doc, annot, "AA") {
				continue
			}

			// Only PDF/UA-2 restricts a Widget that carries /AA (§8.10.2.3 t02:
			// it must also carry /Contents). PDF/UA-1 has no Widget-/AA rule:
			// its 7.18.1 t3 is the TU-or-Alt requirement (checked elsewhere),
			// not an /AA prohibition, so firing it here was a false positive.
			if profile != "PDF/UA-2" {
				continue
			}
			// ISO 14289-2 §8.10.2.3 t02 applies to a widget linked into the
			// structure tree and not enclosed by an Artifact: veraPDF gates it on
			// structParentType == null || isArtifact, read through the structure
			// parent tree ([annotStructScope]), so a bare /StructParent the
			// parent tree does not map leaves the widget exempt.
			if parent, underArtifact := annotStructScope(ctx, doc, treeRoot, r, annot); parent == nil || underArtifact {
				continue
			}
			loc := fmt.Sprintf("page %d, widget annot %d", pageIdx+1, annotIdx)
			// /Contents counts only as a string ([stringKeyPresent]): veraPDF
			// reports a widget whose /Contents is null or a name, measured.
			if !stringKeyPresent(ctx, doc, annot, "Contents") {
				results = append(results, Violation{
					Rule:    Rule_14289_2_8_10_2_3_t02,
					Context: loc,
				})
			}
		}
	}

	return results
}
