package pdf

import "github.com/gotenberg/litho/pkg/internal/compliance"

// Sentinel errors for compliance violations.
// Re-exported from the internal compliance package for public API stability.
var (
	// PDF/A Level 'b' (Basic Visual Integrity) base rule errors.
	ErrEncryptionForbidden    = compliance.ErrEncryptionForbidden
	ErrFontNotEmbedded        = compliance.ErrFontNotEmbedded
	ErrMissingICCProfile      = compliance.ErrMissingICCProfile
	ErrLZWForbidden           = compliance.ErrLZWForbidden
	ErrActiveContentForbidden = compliance.ErrActiveContentForbidden

	// PDF/A Level 'u' (Unicode) errors.
	ErrMissingUnicodeMapping = compliance.ErrMissingUnicodeMapping

	// PDF/A Level 'a' (Accessible) errors.
	ErrUntaggedVisualElement = compliance.ErrUntaggedVisualElement

	// Additional compliance errors.
	ErrFontMissingToUnicode   = compliance.ErrFontMissingToUnicode
	ErrTransparencyForbidden  = compliance.ErrTransparencyForbidden
	ErrJPEG2000Forbidden      = compliance.ErrJPEG2000Forbidden
	ErrEmbeddedFilesForbidden = compliance.ErrEmbeddedFilesForbidden
	ErrVersionTooHighForA1    = compliance.ErrVersionTooHighForA1
	ErrNonPDFAAttachment      = compliance.ErrNonPDFAAttachment
	ErrAttachmentNotPDFA      = compliance.ErrAttachmentNotPDFA
	ErrAttachmentNoRelation   = compliance.ErrAttachmentNoRelation
	ErrMissingAFRelationship  = compliance.ErrMissingAFRelationship
	ErrMissingOCGUsageApp     = compliance.ErrMissingOCGUsageApp
	ErrVersionNotPDF20        = compliance.ErrVersionNotPDF20
	ErrMissing3DSubtype       = compliance.ErrMissing3DSubtype
	ErrMissing3DFallback      = compliance.ErrMissing3DFallback
	ErrMissingMarkInfo        = compliance.ErrMissingMarkInfo
	ErrMissingStructTreeRoot  = compliance.ErrMissingStructTreeRoot
	ErrMissingLang            = compliance.ErrMissingLang
	ErrImageMissingAltText    = compliance.ErrImageMissingAltText
	ErrArtifactTagMissing     = compliance.ErrArtifactTagMissing
	ErrInvalidProfile         = compliance.ErrInvalidProfile

	// PDF/UA-specific errors.
	ErrMissingXMPTitle           = compliance.ErrMissingXMPTitle
	ErrMissingFormFieldTooltip   = compliance.ErrMissingFormFieldTooltip
	ErrNonStandardTag            = compliance.ErrNonStandardTag
	ErrInvalidVersionForUA2      = compliance.ErrInvalidVersionForUA2
	ErrMissingNamespaces         = compliance.ErrMissingNamespaces
	ErrInvalidStructureNamespace = compliance.ErrInvalidStructureNamespace
	ErrLinkNotNested             = compliance.ErrLinkNotNested
	ErrFormulaMissingMathML      = compliance.ErrFormulaMissingMathML
)
