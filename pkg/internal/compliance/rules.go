package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// catalogCheckFunc validates a specific compliance rule against the document catalog.
type catalogCheckFunc func(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error

// docCheckFunc validates a compliance rule that doesn't require the catalog.
type docCheckFunc func(ctx context.Context, doc *document.Document) []error

// ruleSet defines the complete set of validation checks for a profile.
type ruleSet struct {
	docChecks     []docCheckFunc
	catalogChecks []catalogCheckFunc
}

// runRuleSet retrieves the catalog and executes all checks in the rule set.
func runRuleSet(ctx context.Context, doc *document.Document, rules ruleSet) []error {
	catalog, err := getCatalog(ctx, doc)
	if err != nil {
		return []error{err}
	}

	errs := make([]error, 0, 10) //nolint:mnd // multiple check categories.
	for _, check := range rules.docChecks {
		errs = append(errs, check(ctx, doc)...)
	}
	for _, check := range rules.catalogChecks {
		errs = append(errs, check(ctx, doc, catalog)...)
	}
	return errs
}

// --- PDF/A rule sets ---

// pdfaRuleSets maps each PDF/A profile to its complete validation rule set.
// Populated in init() to break the initialization cycle caused by
// checkAttachmentsPDFA → validateEmbeddedFilePDFA → ValidatePDFA → pdfaRuleSets.
//
//nolint:gochecknoglobals // package-level rule tables are the core design.
var pdfaRuleSets map[models.PDFAProfile]ruleSet

// pdfaDocChecks are the document-level checks common to all PDF/A profiles.
func pdfaDocChecks() []docCheckFunc {
	return []docCheckFunc{checkFileHeader, checkTrailerID, checkEncryptionForbidden}
}

// pdfaBaseChecks returns the catalog checks common to all PDF/A profiles.
func pdfaBaseChecks(profile models.PDFAProfile) []catalogCheckFunc {
	return []catalogCheckFunc{
		checkFontsEmbedded,
		checkGlyphWidthConsistency,
		checkICCProfile,
		checkOutputIntentProfileClass,
		checkContentStreamCompliance,
		checkNoLZW,
		checkInlineImageFilters,
		checkNoReferenceXObjects,
		checkStreamExternalKeys,
		checkRenderingIntent,
		makeXMPCheck(profile),
		checkPageSizeBounds,
		checkNameLengths,
		makeStringLengthCheck(profile),
		checkCatalogVersionFormat,
		checkPermissionsDict,
		checkDocMDPSignatureRefs,
	}
}

// pdfaParts1to3Base returns base checks for PDF/A parts 1–3, which include
// the active content prohibition.
func pdfaParts1to3Base(profile models.PDFAProfile) []catalogCheckFunc {
	checks := pdfaBaseChecks(profile)
	checks = append(checks, checkNoActiveContent)
	return checks
}

// pdfA1Checks returns the common catalog checks for all PDF/A-1 conformance levels.
func pdfA1Checks(profile models.PDFAProfile) []catalogCheckFunc {
	checks := pdfaParts1to3Base(profile)
	checks = append(checks,
		makeAnnotationCheck(extraForbiddenAnnots3DRichMedia, false),
		checkFormFieldAppearances,
		checkPDFA1Version,
		checkNoTransparency,
		checkNoJPX,
		checkNoAttachments,
		checkLangValues,
	)
	return checks
}

func pdfA1bRules() ruleSet {
	return ruleSet{docChecks: pdfaDocChecks(), catalogChecks: pdfA1Checks(models.PDFA1b)}
}

func pdfA1aRules() ruleSet {
	checks := pdfA1Checks(models.PDFA1a)
	checks = append(checks,
		checkUnicodeMapping,
		checkMarkInfo,
		checkStructTreeRoot,
	)
	return ruleSet{docChecks: pdfaDocChecks(), catalogChecks: checks}
}

// pdfA2Checks returns the common catalog checks for all PDF/A-2 conformance levels.
func pdfA2Checks(profile models.PDFAProfile) []catalogCheckFunc {
	checks := pdfaParts1to3Base(profile)
	checks = append(checks,
		makeAnnotationCheck(extraForbiddenAnnots3DRichMedia, true),
		checkFormXObjectCompliance,
		checkExtGStateKeys,
		checkBlendModes,
		checkTransparencyBlendingCS,
		checkWidgetActions,
		checkNoXFA,
		checkFormFieldAppearances,
		checkAAKeyRestrictions,
		checkImageAlternates,
		checkImageOPI,
		checkImageInterpolate,
		checkNoAlternatePresentations,
		checkNoRequirements,
		checkAttachmentsPDFA,
		checkOCGUsageApplication,
		checkCIDToGIDMap,
		checkCIDSystemInfoConsistency,
		checkFontNameUTF8,
		checkTrueTypeEncoding,
		checkAnnotationAPOnlyN,
		checkWidgetButtonAppearance,
		checkExtGStateHTP,
	)
	return checks
}

func pdfA2bRules() ruleSet {
	return ruleSet{docChecks: pdfaDocChecks(), catalogChecks: pdfA2Checks(models.PDFA2b)}
}

func pdfA2uRules() ruleSet {
	checks := pdfA2Checks(models.PDFA2u)
	checks = append(checks, checkUnicodeMapping)
	return ruleSet{docChecks: pdfaDocChecks(), catalogChecks: checks}
}

func pdfA2aRules() ruleSet {
	checks := pdfA2Checks(models.PDFA2a)
	checks = append(checks,
		checkUnicodeMapping,
		checkLangEntry,
		checkMarkInfo,
		checkStructTreeRoot,
	)
	return ruleSet{docChecks: pdfaDocChecks(), catalogChecks: checks}
}

// pdfA3Checks returns the common catalog checks for all PDF/A-3 conformance levels.
func pdfA3Checks(profile models.PDFAProfile) []catalogCheckFunc {
	checks := pdfaParts1to3Base(profile)
	checks = append(checks,
		makeAnnotationCheck(extraForbiddenAnnots3DRichMedia, true),
		checkFormXObjectCompliance,
		checkExtGStateKeys,
		checkBlendModes,
		checkTransparencyBlendingCS,
		checkWidgetActions,
		checkNoXFA,
		checkFormFieldAppearances,
		checkAAKeyRestrictions,
		checkImageAlternates,
		checkImageOPI,
		checkImageInterpolate,
		checkNoAlternatePresentations,
		checkNoRequirements,
		checkAFRelationship,
		checkOCGUsageApplication,
		checkCIDToGIDMap,
		checkCIDSystemInfoConsistency,
		checkFontNameUTF8,
		checkTrueTypeEncoding,
		checkAnnotationAPOnlyN,
		checkWidgetButtonAppearance,
		checkExtGStateHTP,
	)
	return checks
}

func pdfA3bRules() ruleSet {
	return ruleSet{docChecks: pdfaDocChecks(), catalogChecks: pdfA3Checks(models.PDFA3b)}
}

func pdfA3uRules() ruleSet {
	checks := pdfA3Checks(models.PDFA3u)
	checks = append(checks, checkUnicodeMapping)
	return ruleSet{docChecks: pdfaDocChecks(), catalogChecks: checks}
}

func pdfA3aRules() ruleSet {
	checks := pdfA3Checks(models.PDFA3a)
	checks = append(checks,
		checkUnicodeMapping,
		checkLangEntry,
		checkMarkInfo,
		checkStructTreeRoot,
	)
	return ruleSet{docChecks: pdfaDocChecks(), catalogChecks: checks}
}

func pdfA4DocChecks() []docCheckFunc {
	dc := pdfaDocChecks()
	dc = append(dc, checkInfoDictPart4)
	return dc
}

func pdfA4Rules(profile models.PDFAProfile) ruleSet {
	checks := pdfaBaseChecks(profile)
	checks = append(checks,
		makeAnnotationCheck(extraForbiddenAnnotsPart4, true),
		checkFormXObjectCompliance,
		checkExtGStateKeysPart4,
		checkBlendModes,
		checkTransparencyBlendingCS,
		checkWidgetActions,
		checkNoXFA,
		checkFormFieldAppearances,
		checkAAKeyRestrictions,
		checkImageAlternates,
		checkImageOPI,
		checkImageInterpolate,
		checkNoAlternatePresentations,
		checkNoRequirements,
		checkNoActiveContentPart4,
		checkPDFA4Version,
		checkOCGUsageApplication,
		checkUnicodeMapping,
		checkCIDToGIDMap,
		checkCIDSystemInfoConsistency,
		checkFontNameUTF8,
		checkTrueTypeEncoding,
		checkAnnotationAPOnlyN,
		checkWidgetButtonAppearance,
		checkExtGStateHTP,
		checkFileSpecFUF,
	)
	return ruleSet{docChecks: pdfA4DocChecks(), catalogChecks: checks}
}

func pdfA4eRules() ruleSet {
	// PDF/A-4e allows 3D/RichMedia annotations (with compliance requirements).
	checks := pdfaBaseChecks(models.PDFA4e)
	checks = append(checks,
		makeAnnotationCheck(nil, true),
		checkFormXObjectCompliance,
		checkExtGStateKeysPart4,
		checkBlendModes,
		checkTransparencyBlendingCS,
		checkWidgetActions,
		checkNoXFA,
		checkFormFieldAppearances,
		checkAAKeyRestrictions,
		checkImageAlternates,
		checkImageOPI,
		checkImageInterpolate,
		checkNoAlternatePresentations,
		checkNoRequirements,
		checkPDFA4Version,
		checkOCGUsageApplication,
		checkUnicodeMapping,
		check3DStreamCompliance,
		checkCIDToGIDMap,
		checkCIDSystemInfoConsistency,
		checkFontNameUTF8,
		checkTrueTypeEncoding,
		checkAnnotationAPOnlyN,
		checkWidgetButtonAppearance,
		checkExtGStateHTP,
		checkFileSpecFUF,
	)
	return ruleSet{docChecks: pdfA4DocChecks(), catalogChecks: checks}
}

func pdfA4fRules() ruleSet {
	// PDF/A-4f allows FileAttachment annotations (unlike base 4).
	checks := pdfaBaseChecks(models.PDFA4f)
	checks = append(checks,
		makeAnnotationCheck(extraForbiddenAnnots3DRichMedia, true),
		checkFormXObjectCompliance,
		checkExtGStateKeysPart4,
		checkBlendModes,
		checkTransparencyBlendingCS,
		checkWidgetActions,
		checkNoXFA,
		checkFormFieldAppearances,
		checkAAKeyRestrictions,
		checkImageAlternates,
		checkImageOPI,
		checkImageInterpolate,
		checkNoAlternatePresentations,
		checkNoRequirements,
		checkEmbeddedFilesKeyPart4f,
		checkPDFA4Version,
		checkOCGUsageApplication,
		checkUnicodeMapping,
		checkAFRelationship,
		checkCIDToGIDMap,
		checkCIDSystemInfoConsistency,
		checkFontNameUTF8,
		checkTrueTypeEncoding,
		checkAnnotationAPOnlyN,
		checkWidgetButtonAppearance,
		checkExtGStateHTP,
		checkFileSpecFUF,
	)
	return ruleSet{docChecks: pdfA4DocChecks(), catalogChecks: checks}
}

// --- PDF/UA rule sets ---

// pdfuaRuleSets maps each PDF/UA profile to its complete validation rule set.
//
//nolint:gochecknoglobals // package-level rule tables are the core design.
var pdfuaRuleSets map[models.PDFUAProfile]ruleSet

func init() { //nolint:gochecknoinits // breaks checkAttachmentsPDFA → ValidatePDFA → pdfaRuleSets cycle.
	pdfaRuleSets = map[models.PDFAProfile]ruleSet{
		models.PDFA1b: pdfA1bRules(),
		models.PDFA1a: pdfA1aRules(),
		models.PDFA2b: pdfA2bRules(),
		models.PDFA2u: pdfA2uRules(),
		models.PDFA2a: pdfA2aRules(),
		models.PDFA3b: pdfA3bRules(),
		models.PDFA3u: pdfA3uRules(),
		models.PDFA3a: pdfA3aRules(),
		models.PDFA4:  pdfA4Rules(models.PDFA4),
		models.PDFA4e: pdfA4eRules(),
		models.PDFA4f: pdfA4fRules(),
	}
	pdfuaRuleSets = map[models.PDFUAProfile]ruleSet{
		models.PDFUA1: pdfUA1Rules(),
		models.PDFUA2: pdfUA2Rules(),
	}
}

// pdfuaBaseChecks returns the catalog checks common to all PDF/UA profiles.
func pdfuaBaseChecks() []catalogCheckFunc {
	return []catalogCheckFunc{
		checkLang,
		checkStructTreeRoot,
		checkMarkInfo,
		checkSuspects,
		checkDisplayDocTitle,
		checkXMPTitle,
		checkFontsToUnicodeUA,
		checkFormFieldTooltips,
		checkImagesHaveAltText,
		checkArtifactTagging,
	}
}

func pdfUA1Rules() ruleSet {
	checks := pdfuaBaseChecks()
	checks = append(checks, checkStandardTags, checkTabOrder, checkLangValues, checkOCConfigNames,
		checkAnnotationAltText, checkTableSectionChildren, checkListItemChildren, checkCaptionFirstChild)
	return ruleSet{catalogChecks: checks}
}

func pdfUA2Rules() ruleSet {
	checks := pdfuaBaseChecks()
	checks = append(checks,
		checkPDFVersionForUA2,
		checkNamespaces,
		checkLinkAnnotationNesting,
		checkFormulaMathML,
		checkPDFUAIdentification,
		checkAnnotationAltText,
		checkLangValues,
	)
	return ruleSet{catalogChecks: checks}
}

// --- Factory functions ---

// makeAnnotationCheck returns a check function for annotation compliance
// (6.3.1 forbidden types, 6.3.2 flags, 6.3.3 appearance streams) with the
// given configuration captured via closure.
func makeAnnotationCheck(extraForbidden map[ast.PdfName]bool, linkExemptFromAP bool) catalogCheckFunc {
	return func(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
		return checkAnnotationCompliance(ctx, doc, catalog, extraForbidden, linkExemptFromAP)
	}
}

// makeXMPCheck returns a check function for XMP metadata compliance with the
// profile captured via closure.
func makeXMPCheck(profile models.PDFAProfile) catalogCheckFunc {
	return func(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
		return checkXMPCompliance(ctx, doc, catalog, profile)
	}
}

// --- Entry points ---

// ValidatePDFA audits the document against a specific PDF/A profile,
// returning all compliance violations found. An empty slice means the
// document passes.
func ValidatePDFA(ctx context.Context, doc *document.Document, profile models.PDFAProfile) []error {
	rules, ok := pdfaRuleSets[profile]
	if !ok {
		return []error{fmt.Errorf("%w: %s", models.ErrInvalidProfile, profile)}
	}
	return runRuleSet(ctx, doc, rules)
}

// ValidatePDFUA audits the document against a specific PDF/UA profile,
// returning all compliance violations found. An empty slice means the
// document passes.
func ValidatePDFUA(ctx context.Context, doc *document.Document, profile models.PDFUAProfile) []error {
	rules, ok := pdfuaRuleSets[profile]
	if !ok {
		return []error{fmt.Errorf("%w: %s", models.ErrInvalidProfile, profile)}
	}
	return runRuleSet(ctx, doc, rules)
}
