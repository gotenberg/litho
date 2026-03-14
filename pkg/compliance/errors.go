package compliance

import "errors"

// Sentinel errors for compliance violations.
var (
	// PDF/A Level 'b' (Basic Visual Integrity) base rule errors.
	ErrEncryptionForbidden    = errors.New("compliance: encryption is forbidden in PDF/A")
	ErrFontNotEmbedded        = errors.New("compliance: font is not embedded")
	ErrMissingICCProfile      = errors.New("compliance: /OutputIntents missing ICC profile for color reproduction")
	ErrLZWForbidden           = errors.New("compliance: LZW compression is forbidden in PDF/A")
	ErrActiveContentForbidden = errors.New("compliance: active content (JavaScript/Launch/Sound/Movie) is forbidden in PDF/A")

	// PDF/A Level 'u' (Unicode) errors.
	ErrMissingUnicodeMapping = errors.New("compliance: font has no /ToUnicode CMap and no standard Adobe encoding")

	// PDF/A Level 'a' (Accessible) errors.
	ErrUntaggedVisualElement = errors.New("compliance: visual element (Figure) missing /Alt text in structure tree")

	// Additional compliance errors.
	ErrFontMissingToUnicode   = errors.New("compliance: font missing /ToUnicode mapping")
	ErrTransparencyForbidden  = errors.New("compliance: transparency is forbidden in PDF/A-1")
	ErrJPEG2000Forbidden      = errors.New("compliance: JPEG2000 images are forbidden in PDF/A-1")
	ErrEmbeddedFilesForbidden = errors.New("compliance: embedded files are forbidden in PDF/A-1")
	ErrVersionTooHighForA1    = errors.New("compliance: PDF/A-1 requires PDF version 1.4 or lower")
	ErrNonPDFAAttachment      = errors.New("compliance: embedded file is not a PDF/A-compliant PDF")
	ErrAttachmentNotPDFA      = errors.New("compliance: embedded file is not PDF/A compliant")
	ErrAttachmentNoRelation   = errors.New("compliance: embedded file missing /AFRelationship key")
	ErrMissingAFRelationship  = errors.New("compliance: embedded file missing or invalid /AFRelationship")
	ErrMissingOCGUsageApp     = errors.New("compliance: /OCProperties missing /AS (Usage Application Dictionaries)")
	ErrVersionNotPDF20        = errors.New("compliance: PDF/A-4 requires PDF version 2.0")
	ErrMissing3DSubtype       = errors.New("compliance: 3D annotation stream missing valid /Subtype (U3D or PRC)")
	ErrMissing3DFallback      = errors.New("compliance: 3D annotation missing 2D fallback appearance (/AP /N)")
	ErrMissingMarkInfo        = errors.New("compliance: /MarkInfo << /Marked true >> required for conformance level 'a'")
	ErrMissingStructTreeRoot  = errors.New("compliance: /StructTreeRoot required for conformance level 'a'")
	ErrMissingLang            = errors.New("compliance: /Lang key missing from document catalog")
	ErrImageMissingAltText    = errors.New("compliance: image XObject missing /Alt text in structure tree")
	ErrArtifactTagMissing     = errors.New("compliance: headers/footers/page numbers must be tagged as /Artifact")
	ErrInvalidProfile         = errors.New("compliance: unknown or invalid profile")

	// PDF/UA-specific errors.
	ErrMissingXMPTitle           = errors.New("compliance: XMP metadata missing dc:title")
	ErrMissingFormFieldTooltip   = errors.New("compliance: form field missing /TU (tooltip) for accessibility")
	ErrNonStandardTag            = errors.New("compliance: non-standard structure tag without /RoleMap entry")
	ErrInvalidVersionForUA2      = errors.New("compliance: PDF/UA-2 requires PDF version 2.0")
	ErrMissingNamespaces         = errors.New("compliance: PDF/UA-2 requires /Namespaces in /StructTreeRoot")
	ErrInvalidStructureNamespace = errors.New("compliance: structure namespace missing required /NS URI")
	ErrLinkNotNested             = errors.New("compliance: /Link annotation not nested in /Link structure element")
	ErrFormulaMissingMathML      = errors.New("compliance: /Formula structure element missing /AF with MathML")
)
