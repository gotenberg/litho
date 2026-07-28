package compliance

import (
	"context"
)

// checkAccessibilityGroup validates PDF/UA accessibility requirements.
// For non-PDF/UA profiles, it returns nil immediately.
func checkAccessibilityGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile

	if !vctx.ua {
		return nil
	}

	doc.Logger.Debug("compliance.accessibility: starting checks", "profile", profile)

	results := make([]Violation, 0, 16)

	// Language (§7.2/§8.2): /Lang key required in catalog.
	results = append(results, checkLangRequired(ctx, doc, catalog, profile)...)

	// Marked-content /Lang format (§7.2 t29 / §8.4.4 t2): a /Lang set on a BDC
	// sequence must be a valid language tag.
	results = append(results, checkMarkedContentLang(ctx, doc, catalog, profile)...)

	// Span marked-content language (§7.2 t30/t31/t32, PDF/UA-1): text in the
	// /ActualText, /Alt and /E entries of a /Span sequence needs a determinable
	// language.
	results = append(results, checkSpanMarkedContentLang(ctx, doc, catalog, profile)...)

	// Outline language (§7.2 t2, PDF/UA-1): outline entries must have determinable language.
	results = append(results, checkOutlineLang(ctx, doc, catalog, profile)...)

	// DisplayDocTitle (§7.1): /ViewerPreferences must have /DisplayDocTitle=true.
	results = append(results, checkDisplayDocTitle(ctx, doc, catalog, profile)...)

	// Suspects (§7.1): /MarkInfo /Suspects must be false or absent.
	results = append(results, checkSuspects(ctx, doc, catalog, profile)...)

	// Tab order (§7.18.1, PDF/UA-1 only): Page /Tabs must be /S.
	results = append(results, checkTabOrder(ctx, doc, catalog, profile)...)

	// XMP title (PDF/UA): XMP must contain dc:title.
	results = append(results, checkXMPTitle(ctx, doc, catalog, profile)...)

	// Figure alt text (PDF/UA): Figure structure elements must have /Alt.
	results = append(results, checkFigureAltText(ctx, doc, catalog, profile)...)

	// Artifact tagging (PDF/UA): headers/footers/page numbers should be tagged as Artifact.
	results = append(results, checkArtifactTagging(ctx, doc, catalog, profile)...)

	// Annotation alt text (PDF/UA): non-Link/Widget annotations must have /Contents or structure element /Alt.
	results = append(results, checkAnnotationAltText(ctx, doc, catalog, profile)...)

	// Annotation /Contents vs enclosing /Alt (PDF/UA-2 §8.9.4.2 t1): if both are
	// present, they must be identical.
	results = append(results, checkAnnotContentsAltMatch(ctx, doc, catalog, profile)...)

	// Orphan MCIDs (PDF/UA §7.1 t3 / §8.2.2 t1): marked content must be
	// reachable through the ParentTree, not just parented in the tree.
	results = append(results, checkOrphanMCIDs(ctx, doc, catalog, profile)...)

	// Widget /AA (PDF/UA): Widget annotations must not have /AA key (§7.18.5 / §8.10.2.3).
	results = append(results, checkWidgetAA(ctx, doc, catalog, profile)...)

	// OC config names (PDF/UA): Optional content configuration dicts must have /Name key.
	results = append(results, checkOCConfigNames(ctx, doc, catalog, profile)...)

	// PDF/UA identification: XMP pdfuaid namespace validation.
	results = append(results, checkPDFUAIdentification(ctx, doc, catalog, profile)...)

	// Catalog /Metadata stream /Type and /Subtype (UA-1 §7.1 t8, UA-2 §8.11.1 t2).
	results = append(results, checkMetadataTypeSubtype(ctx, doc, catalog, profile)...)

	// PDF/UA-id property prefix validation (§5 t4/t5).
	results = append(results, checkPDFUAIDPrefixes(ctx, doc, catalog, profile)...)

	// Namespace role mapping validation (PDF/UA-2 §8.2.4). A missing or
	// empty /Namespaces array is not itself a violation: §8.2.4 constrains
	// only non-standard structure types, and unnamespaced standard types
	// belong to the default standard namespace per ISO 32000-2 §14.8.6,
	// so they need no explicit role mapping.
	results = append(results, checkNamespaceRoleMapping(ctx, doc, catalog, profile)...)

	// Link nesting (PDF/UA-2): Link annotations must be nested in Link structure elements.
	results = append(results, checkLinkNesting(ctx, doc, catalog, profile)...)

	// Annotation artifact flags (PDF/UA-2 §8.9.2.2): Invisible (t1) or
	// NoView-without-ToggleNoView (t2) annotations must be artifacts.
	results = append(results, checkAnnotArtifactFlags(ctx, doc, catalog, profile)...)

	// File specification /F and /UF (PDF/UA-1), §7.11 t01.
	results = append(results, checkPDFUAFileSpecFUF(ctx, doc, catalog, profile)...)

	// Form field TU key (PDF/UA-1 §7.18.1 t03).
	results = append(results, checkFormFieldTU(ctx, doc, catalog, profile)...)

	// Link annotation Contents (PDF/UA-1 §7.18.5 t02).
	results = append(results, checkLinkContents(ctx, doc, catalog, profile)...)

	// Media clip data CT and Alt (PDF/UA-1 §7.18.6.2 t01/t02).
	results = append(results, checkMediaClipData(ctx, doc, catalog, profile)...)

	// Per-annotation-type constraints (PDF/UA-2 §8.9.2.4).
	results = append(results, checkPDFUA2AnnotationConstraints(ctx, doc, catalog, profile)...)

	// Annotation-in-structure nesting (PDF/UA-2 §8.10.1 widget/Form, §8.9.2.4.16 Watermark/Annot).
	results = append(results, checkPDFUA2AnnotStructNesting(ctx, doc, catalog, profile)...)

	// Namespace-resolved struct-type constraints (PDF/UA-2 §8.2.5.14/.23/.24).
	results = append(results, checkPDFUA2NamespaceStructTypes(ctx, doc, catalog, profile)...)
	results = append(results, checkPDFUA2FENoteRefs(ctx, doc, catalog, profile)...)

	// Content marking (PDF/UA-1 §7.1 t3).
	results = append(results, checkContentMarking(ctx, doc, catalog, profile)...)

	// Encryption permissions (PDF/UA-1 §7.16 t01).
	results = append(results, checkEncryptionPermissions(ctx, doc, profile)...)

	// Annotation structure nesting (PDF/UA-1 §7.18.x).
	results = append(results, checkAnnotationNesting(ctx, doc, catalog, profile)...)

	// Form element Role attribute / single-widget-child (PDF/UA-1 §7.18.4 t02).
	results = append(results, checkPDFUA1FormRole(ctx, doc, catalog, profile)...)

	// Form XObject restrictions (PDF/UA-1 §7.20).
	results = append(results, checkFormXObjectRestrictions(ctx, doc, catalog, profile)...)

	// PDF/UA-2 form structure checks (§8.10.1, §8.10.2.3).
	results = append(results, checkPDFUA2FormStructure(ctx, doc, catalog, profile)...)

	// Structure destinations (PDF/UA-2 §8.8).
	results = append(results, checkStructureDestinations(ctx, doc, catalog, profile)...)

	doc.Logger.Debug("compliance.accessibility: checks complete", "violations", len(results))
	return results
}
