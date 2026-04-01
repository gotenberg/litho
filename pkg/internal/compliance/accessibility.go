package compliance

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkAccessibilityGroup validates PDF/UA accessibility requirements.
// For non-PDF/UA profiles, it returns nil immediately.
func checkAccessibilityGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !isPDFUA(profile) {
		slog.Debug("compliance.accessibility: skipping for non-PDF/UA profile", "profile", profile)
		return nil
	}

	slog.Debug("compliance.accessibility: starting checks", "profile", profile)

	results := make([]Violation, 0, 16) //nolint:mnd // estimated capacity for accessibility checks.

	// 1. Language (§7.2/§8.2) — /Lang key required in catalog.
	results = append(results, checkLangRequired(ctx, doc, catalog, profile)...)

	// 1b. Outline language (§7.2 t2, PDF/UA-1) — outline entries must have determinable language.
	results = append(results, checkOutlineLang(ctx, doc, catalog, profile)...)

	// 2. DisplayDocTitle (§7.1) — /ViewerPreferences must have /DisplayDocTitle=true.
	results = append(results, checkDisplayDocTitle(ctx, doc, catalog, profile)...)

	// 3. Suspects (§7.1) — /MarkInfo /Suspects must be false or absent.
	results = append(results, checkSuspects(ctx, doc, catalog, profile)...)

	// 4. Tab order (§7.18.1, PDF/UA-1 only) — Page /Tabs must be /S.
	results = append(results, checkTabOrder(ctx, doc, catalog, profile)...)

	// 5. XMP title (PDF/UA) — XMP must contain dc:title.
	results = append(results, checkXMPTitle(ctx, doc, catalog, profile)...)

	// 6. Images alt text (PDF/UA) — Figure structure elements must have /Alt.
	results = append(results, checkFigureAltText(ctx, doc, catalog, profile)...)

	// 7. Artifact tagging (PDF/UA) — Headers/footers/page numbers should be tagged as Artifact.
	results = append(results, checkArtifactTagging(ctx, doc, catalog, profile)...)

	// 8. Annotation alt text (PDF/UA) — Non-Link/Widget annotations must have /Contents or structure element /Alt.
	results = append(results, checkAnnotationAltText(ctx, doc, catalog, profile)...)

	// 8b. Widget /AA check (PDF/UA) — Widget annotations must not have /AA key (§7.18.5 / §8.10.2.3).
	results = append(results, checkWidgetAA(ctx, doc, catalog, profile)...)

	// 9. OC config names (PDF/UA) — Optional content configuration dicts must have /Name key.
	results = append(results, checkOCConfigNames(ctx, doc, catalog, profile)...)

	// 10. PDF/UA identification — XMP pdfuaid namespace validation.
	results = append(results, checkPDFUAIdentification(ctx, doc, catalog, profile)...)

	// 10b. PDF/UA-id property prefix validation (§5 t4/t5).
	results = append(results, checkPDFUAIDPrefixes(ctx, doc, catalog, profile)...)

	// 11. Namespaces (PDF/UA-2) — /StructTreeRoot must have /Namespaces array.
	results = append(results, checkNamespaces(ctx, doc, catalog, profile)...)

	// 11b. Namespace role mapping validation (PDF/UA-2 §8.2.4).
	results = append(results, checkNamespaceRoleMapping(ctx, doc, catalog, profile)...)

	// 12. Link nesting (PDF/UA-2) — Link annotations must be nested in Link structure elements.
	results = append(results, checkLinkNesting(ctx, doc, catalog, profile)...)

	// 13. Formula MathML (PDF/UA-2) — Formula structure elements must have /AF with MathML.
	results = append(results, checkFormulaMathML(ctx, doc, catalog, profile)...)

	// 14. Invisible annotations in structure (PDF/UA-2) — annotations with Invisible flag must be artifacts.
	results = append(results, checkInvisibleAnnotArtifact(ctx, doc, catalog, profile)...)

	// 15. File specification /F and /UF (PDF/UA-1) — §7.11 t01.
	results = append(results, checkPDFUAFileSpecFUF(ctx, doc, catalog, profile)...)

	// 16. Form field TU key (PDF/UA-1 §7.18.1 t03).
	results = append(results, checkFormFieldTU(ctx, doc, catalog, profile)...)

	// 17. Link annotation Contents (PDF/UA-1 §7.18.5 t02).
	results = append(results, checkLinkContents(ctx, doc, catalog, profile)...)

	// 18. Media clip data CT and Alt (PDF/UA-1 §7.18.6.2 t01/t02).
	results = append(results, checkMediaClipData(ctx, doc, catalog, profile)...)

	// 19. Content marking (PDF/UA-1 §7.1 t3).
	results = append(results, checkContentMarking(ctx, doc, catalog, profile)...)

	// 20. Encryption permissions (PDF/UA-1 §7.16 t01).
	results = append(results, checkEncryptionPermissions(doc, profile)...)

	// 21. Annotation structure nesting (PDF/UA-1 §7.18.x).
	results = append(results, checkAnnotationNesting(ctx, doc, catalog, profile)...)

	// 22. Form XObject restrictions (PDF/UA-1 §7.20).
	results = append(results, checkFormXObjectRestrictions(ctx, doc, catalog, profile)...)

	// 23. PDF/UA-2 form structure checks (§8.10.1, §8.10.2.3).
	results = append(results, checkPDFUA2FormStructure(ctx, doc, catalog, profile)...)

	// 24. Structure destinations (PDF/UA-2 §8.8).
	results = append(results, checkStructureDestinations(ctx, doc, catalog, profile)...)

	slog.Debug("compliance.accessibility: checks complete", "violations", len(results))
	return results
}
