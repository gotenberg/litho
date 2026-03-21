package models

import "errors"

// PDFAProfile identifies a specific PDF/A conformance level.
type PDFAProfile string

// PDF/A profile constants covering all ISO 19005 parts and conformance levels.
const (
	PDFA1b PDFAProfile = "PDF/A-1b"
	PDFA1a PDFAProfile = "PDF/A-1a"
	PDFA2b PDFAProfile = "PDF/A-2b"
	PDFA2u PDFAProfile = "PDF/A-2u"
	PDFA2a PDFAProfile = "PDF/A-2a"
	PDFA3b PDFAProfile = "PDF/A-3b"
	PDFA3u PDFAProfile = "PDF/A-3u"
	PDFA3a PDFAProfile = "PDF/A-3a"
	PDFA4  PDFAProfile = "PDF/A-4"
	PDFA4e PDFAProfile = "PDF/A-4e"
	PDFA4f PDFAProfile = "PDF/A-4f"
)

// PDFUAProfile identifies a specific PDF/UA conformance level.
type PDFUAProfile string

// PDF/UA profile constants covering ISO 14289 parts.
const (
	PDFUA1 PDFUAProfile = "PDF/UA-1"
	PDFUA2 PDFUAProfile = "PDF/UA-2"
)

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
	ErrMissingCIDToGIDMap    = errors.New("compliance: embedded Type 2 CIDFont must have /CIDToGIDMap entry")
	ErrFontNameNotUTF8       = errors.New("compliance: font name must be valid UTF-8 after hex escape decoding")

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

	// PDF/A XMP metadata errors (ISO 19005, clauses 6.6 and 6.7).
	ErrXMPSchemaViolation    = errors.New("compliance: XMP property uses undeclared schema namespace")
	ErrDocInfoXMPMismatch    = errors.New("compliance: document info and XMP metadata are inconsistent")
	ErrMissingPDFAId         = errors.New("compliance: XMP missing PDF/A identification (pdfaid:part/conformance)")
	ErrXMPPacketInvalid      = errors.New("compliance: XMP packet header uses forbidden 'bytes' or 'encoding' attribute")
	ErrXMPExtensionInvalid   = errors.New("compliance: PDF/A extension schema is missing required fields")
	ErrMissingCatalogMetdata = errors.New("compliance: document catalog missing /Metadata stream")

	// PDF/A annotation errors (ISO 19005, clause 6.3).
	ErrAnnotationMissingFlags      = errors.New("compliance: annotation missing /F key")
	ErrAnnotationInvalidFlags      = errors.New("compliance: annotation /F flags invalid (Print must be set, Hidden/Invisible/NoView must be clear)")
	ErrAnnotationMissingAppearance = errors.New("compliance: annotation missing appearance stream (/AP /N)")
	ErrForbiddenAnnotationType     = errors.New("compliance: forbidden annotation type for PDF/A")
	ErrReferenceXObjectForbidden   = errors.New("compliance: reference XObjects are forbidden in PDF/A")
	ErrInvalidFileHeader           = errors.New("compliance: file header must start with %PDF-")
	ErrMissingTrailerID            = errors.New("compliance: file trailer must contain /ID key")
	ErrMissingBinaryMarker         = errors.New("compliance: file header missing binary comment marker (% + 4 high bytes)")
	ErrStreamExternalFileKeys      = errors.New("compliance: stream dictionary must not contain /F, /FFilter, or /FDecodeParms keys")
	ErrFormXObjectForbiddenKey     = errors.New("compliance: Form XObject contains forbidden key (/OPI, /Subtype2 /PS, or /PS)")
	ErrPostScriptXObjectForbidden  = errors.New("compliance: PostScript XObjects (/Subtype /PS) are forbidden in PDF/A")
	ErrMissingBlendingColorSpace   = errors.New("compliance: page with transparency must have /Group with /CS when no OutputIntent ICC profile is present")
	ErrInvalidBlendMode            = errors.New("compliance: blend mode is not one of the standard modes defined in ISO 32000-1")
	ErrExtGStateForbiddenTR        = errors.New("compliance: ExtGState contains forbidden /TR key")
	ErrExtGStateInvalidTR2         = errors.New("compliance: ExtGState /TR2 must be /Default or absent")
	ErrInvalidHalftoneType         = errors.New("compliance: halftone /HalftoneType must be 1 or 5")
	ErrHalftoneNameForbidden       = errors.New("compliance: halftone must not contain /HalftoneName key")
	ErrHalftoneTransferFunction    = errors.New("compliance: halftone /TransferFunction must be /Default or absent")
	ErrInvalidICCProfileClass      = errors.New("compliance: OutputIntent ICC profile must be output (prtr) or monitor (mntr) class with GRAY/RGB/CMYK color space")

	// PDF/UA-specific errors.
	ErrMissingXMPTitle           = errors.New("compliance: XMP metadata missing dc:title")
	ErrSuspectsTrue              = errors.New("compliance: /MarkInfo /Suspects must be false")
	ErrMissingDisplayDocTitle    = errors.New("compliance: /ViewerPreferences /DisplayDocTitle must be true")
	ErrTabOrderNotStructure      = errors.New("compliance: page /Tabs must be /S (Structure) for PDF/UA")
	ErrInvalidLangTag            = errors.New("compliance: /Lang value is not a valid RFC 3066 language tag")
	ErrInvalidRenderingIntent    = errors.New("compliance: rendering intent must be RelativeColorimetric, AbsoluteColorimetric, Perceptual, or Saturation")
	ErrOverprintModeForbidden    = errors.New("compliance: overprint mode (OPM) shall not be 1 with ICCBased CMYK colour space")
	ErrRoleMapCircular           = errors.New("compliance: circular mapping in /RoleMap")
	ErrRoleMapNotTerminating     = errors.New("compliance: /RoleMap mapping does not terminate at a standard structure type")
	ErrStandardTagRemapped       = errors.New("compliance: standard structure tag must not be remapped in /RoleMap")
	ErrOCConfigMissingName       = errors.New("compliance: optional content configuration dictionary must contain /Name key")
	ErrWidgetHasAction           = errors.New("compliance: widget annotation must not contain /A or /AA keys")
	ErrXFAForbidden              = errors.New("compliance: /AcroForm must not contain /XFA key")
	ErrAlternatePresentations    = errors.New("compliance: name dictionary must not contain /AlternatePresentations")
	ErrRequirementsKey           = errors.New("compliance: document catalog must not contain /Requirements key")
	ErrFormFieldMissingAP        = errors.New("compliance: form field must have an appearance dictionary")
	ErrMissingEmbeddedFilesKey   = errors.New("compliance: PDF/A-4f must have /EmbeddedFiles in name dictionary")
	ErrInfoDictForbiddenPart4    = errors.New("compliance: PDF/A-4 trailer /Info forbidden unless /PieceInfo exists in catalog")
	ErrInfoDictExtraKeysPart4    = errors.New("compliance: PDF/A-4 document info dict may only contain /ModDate")
	ErrAAInvalidKey              = errors.New("compliance: /AA dictionary contains forbidden key (only E/X/D/U/Fo/Bl allowed)")
	ErrImageAlternatesForbidden  = errors.New("compliance: image dictionary must not contain /Alternates key")
	ErrMissingFormFieldTooltip   = errors.New("compliance: form field missing /TU (tooltip) for accessibility")
	ErrNonStandardTag            = errors.New("compliance: non-standard structure tag without /RoleMap entry")
	ErrInvalidVersionForUA2      = errors.New("compliance: PDF/UA-2 requires PDF version 2.0")
	ErrMissingNamespaces         = errors.New("compliance: PDF/UA-2 requires /Namespaces in /StructTreeRoot")
	ErrInvalidStructureNamespace = errors.New("compliance: structure namespace missing required /NS URI")
	ErrLinkNotNested             = errors.New("compliance: /Link annotation not nested in /Link structure element")
	ErrFormulaMissingMathML      = errors.New("compliance: /Formula structure element missing /AF with MathML")

	// Content stream compliance errors (Phase 1).
	ErrDeviceCSWithoutDefault = errors.New("compliance: device-dependent color space used without Default override or OutputIntent")
	ErrForbiddenOperator      = errors.New("compliance: forbidden content stream operator")

	// Implementation limit errors (Phase 4).
	ErrPageSizeOutOfBounds  = errors.New("compliance: page MediaBox dimensions outside allowed range [3, 14400]")
	ErrNameTooLong          = errors.New("compliance: PDF name exceeds 127-byte limit")
	ErrStringTooLong        = errors.New("compliance: PDF string exceeds maximum byte length")
	ErrCIDOutOfRange        = errors.New("compliance: CID value exceeds 65535")
	ErrInvalidHeaderVersion = errors.New("compliance: file header version does not match declared profile")

	// Structure tree validation errors (Phase 3).
	ErrTableStructureInvalid    = errors.New("compliance: table structure element has invalid children")
	ErrTableColumnCountMismatch = errors.New("compliance: table rows have inconsistent column counts")
	ErrListStructureInvalid     = errors.New("compliance: list structure element has invalid children")
	ErrHeadingSkipped           = errors.New("compliance: heading levels must not be skipped")
	ErrMissingTHScope           = errors.New("compliance: TH element missing /Scope attribute")

	// XMP container type errors (Phase 2).
	ErrXMPContainerTypeMismatch = errors.New("compliance: XMP property uses wrong container type")

	// Content stream operator errors (6.2.2).
	ErrUnknownOperator = errors.New("compliance: content stream contains unknown operator")

	// PDF/UA identification and annotation accessibility errors.
	ErrMissingPDFUAId        = errors.New("compliance: XMP missing PDF/UA identification (pdfuaid:part)")
	ErrAnnotationMissingAlt  = errors.New("compliance: annotation missing /Contents or /Alt in structure element")
	ErrLinkMissingAltContent = errors.New("compliance: Link annotation missing alternate description via /Contents")

	// PDF/A annotation appearance errors (6.3.3).
	ErrAnnotationAPExtraKeys    = errors.New("compliance: annotation appearance dictionary must contain only /N key")
	ErrWidgetButtonAPNotSubdict = errors.New("compliance: Widget button /AP /N must be an appearance subdictionary")

	// PDF/A ExtGState errors (6.2.5).
	ErrExtGStateHTP = errors.New("compliance: ExtGState must not contain /HTP key")

	// PDF/A Image dict errors (6.2.8).
	ErrImageOPIForbidden         = errors.New("compliance: Image dictionary must not contain /OPI key")
	ErrImageInterpolateForbidden = errors.New("compliance: Image /Interpolate must be false")

	// PDF/A inline image filter errors (6.1.9).
	ErrInlineImageForbiddenFilter = errors.New("compliance: inline image uses forbidden filter (LZW/Crypt)")

	// Font CIDSystemInfo errors (6.2.11.3.1).
	ErrCIDSystemInfoMismatch = errors.New("compliance: CIDFont and CMap CIDSystemInfo entries are incompatible")

	// Font glyph width consistency errors (6.2.11.5 / 6.2.10.5).
	ErrGlyphWidthMismatch = errors.New("compliance: font glyph width in dictionary does not match embedded font program")

	// .notdef glyph reference error (6.2.11.8 / 6.2.10.9).
	ErrNotdefGlyphUsed = errors.New("compliance: .notdef glyph referenced in content stream")

	// File specification errors (7.11).
	ErrFileSpecMissingFUF = errors.New("compliance: file specification dictionary missing /F or /UF key")

	// ICCBased colour space errors (6.2.4.2).
	ErrICCBasedSameAsOutputIntent = errors.New("compliance: ICCBased colour space profile is identical to OutputIntent profile")

	// Numeric and stream limit errors (6.1.12/6.1.13/6.1.7.1).
	ErrRealValueTooLarge    = errors.New("compliance: absolute real value exceeds 32767.0")
	ErrIntegerOutOfRange    = errors.New("compliance: integer value outside [-2147483648, 2147483647]")
	ErrStreamLengthMismatch = errors.New("compliance: stream /Length does not match actual data length")
	ErrQNestingTooDeep      = errors.New("compliance: graphics state nesting (q/Q) exceeds 28 levels")
)
