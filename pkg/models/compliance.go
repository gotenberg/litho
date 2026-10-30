package models

// PDFAProfile identifies a specific PDF/A conformance level.
// Values are immutable and safe for concurrent use.
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
// Values are immutable and safe for concurrent use.
type PDFUAProfile string

// PDF/UA profile constants covering ISO 14289 parts.
const (
	PDFUA1 PDFUAProfile = "PDF/UA-1"
	PDFUA2 PDFUAProfile = "PDF/UA-2"
)

// WTPDFProfile identifies a Well-Tagged PDF (WTPDF) 1.0 conformance
// level. WTPDF builds on ISO 14289-2 (PDF/UA-2): both levels carry the
// PDF/UA-2 requirements except the identification rules of its clause 5,
// and each adds a PDF Declaration naming the level.
// Values are immutable and safe for concurrent use.
type WTPDFProfile string

// WTPDF 1.0 profile constants. Accessibility is the stricter level;
// Reuse relaxes the requirements that serve assistive technology rather
// than content reuse.
const (
	WTPDF1Accessibility WTPDFProfile = "WTPDF-1.0-Accessibility"
	WTPDF1Reuse         WTPDFProfile = "WTPDF-1.0-Reuse"
)

// String returns the canonical string form of the profile, e.g.
// "PDF/A-2b". The zero value renders as the empty string.
func (p PDFAProfile) String() string { return string(p) }

// String returns the canonical string form of the profile, e.g.
// "WTPDF-1.0-Reuse". The zero value renders as the empty string.
func (p WTPDFProfile) String() string { return string(p) }

// IsValid reports whether p matches one of the documented WTPDF
// profile constants. Use this to validate user input before passing
// the value to validators that assume a known profile.
func (p WTPDFProfile) IsValid() bool {
	switch p {
	case WTPDF1Accessibility, WTPDF1Reuse:
		return true
	}
	return false
}

// IsValid reports whether p matches one of the documented PDF/A
// profile constants. Use this to validate user input before passing
// the value to validators or remediation engines that assume a known
// profile.
func (p PDFAProfile) IsValid() bool {
	switch p {
	case PDFA1a, PDFA1b,
		PDFA2a, PDFA2b, PDFA2u,
		PDFA3a, PDFA3b, PDFA3u,
		PDFA4, PDFA4e, PDFA4f:
		return true
	}
	return false
}

// String returns the canonical string form of the profile, e.g.
// "PDF/UA-1". The zero value renders as the empty string.
func (p PDFUAProfile) String() string { return string(p) }

// IsValid reports whether p matches one of the documented PDF/UA
// profile constants. Use this to validate user input before passing
// the value to validators or remediation engines that assume a known
// profile.
func (p PDFUAProfile) IsValid() bool {
	switch p {
	case PDFUA1, PDFUA2:
		return true
	}
	return false
}

// PDFXProfile identifies a specific PDF/X conformance level, as a document
// DECLARES it. litho reads the declaration; it does not validate against it.
// Values are immutable and safe for concurrent use.
type PDFXProfile string

// PDF/X profile constants covering the ten live ISO 15930 conformance levels.
//
// PDF/X-2 (ISO 15930-5:2003) is deliberately absent: it was a partial-exchange
// variant that saw effectively no adoption and is superseded by X-5.
const (
	PDFX1a  PDFXProfile = "PDF/X-1a"
	PDFX3   PDFXProfile = "PDF/X-3"
	PDFX4   PDFXProfile = "PDF/X-4"
	PDFX4p  PDFXProfile = "PDF/X-4p"
	PDFX5g  PDFXProfile = "PDF/X-5g"
	PDFX5pg PDFXProfile = "PDF/X-5pg"
	PDFX5n  PDFXProfile = "PDF/X-5n"
	PDFX6   PDFXProfile = "PDF/X-6"
	PDFX6p  PDFXProfile = "PDF/X-6p"
	PDFX6n  PDFXProfile = "PDF/X-6n"
)

// String returns the canonical string form of the profile, e.g. "PDF/X-4".
// The zero value renders as the empty string.
func (p PDFXProfile) String() string { return string(p) }

// IsValid reports whether p matches one of the documented PDF/X profile
// constants. Use this to validate user input before passing the value on.
func (p PDFXProfile) IsValid() bool {
	switch p {
	case PDFX1a, PDFX3,
		PDFX4, PDFX4p,
		PDFX5g, PDFX5pg, PDFX5n,
		PDFX6, PDFX6p, PDFX6n:
		return true
	}
	return false
}

// FacturXLevel identifies a Factur-X conformance level. The levels match
// the values that appear verbatim in the XMP `fx:ConformanceLevel`
// property defined by the Factur-X 1.0.07 specification. ZUGFeRD 2.x
// shares the same identifiers.
// Values are immutable and safe for concurrent use.
type FacturXLevel string

// Factur-X conformance levels.
const (
	// FacturXMinimum corresponds to the MINIMUM profile: header data and
	// totals only.
	FacturXMinimum FacturXLevel = "MINIMUM"
	// FacturXBasicWL corresponds to the BASIC WL profile: BASIC without
	// line items.
	FacturXBasicWL FacturXLevel = "BASIC WL"
	// FacturXBasic corresponds to the BASIC profile: BASIC WL with line
	// items.
	FacturXBasic FacturXLevel = "BASIC"
	// FacturXEN16931 corresponds to the EN 16931 profile, also called
	// COMFORT. Mandatory level for cross-border B2B invoices in the EU.
	FacturXEN16931 FacturXLevel = "EN 16931"
	// FacturXExtended corresponds to the EXTENDED profile: superset of
	// EN 16931 with bilateral fields.
	FacturXExtended FacturXLevel = "EXTENDED"
	// FacturXXRechnung corresponds to the XRECHNUNG profile shipped by
	// ZUGFeRD 2.1+. Mandatory format for invoices addressed to German
	// federal public-sector buyers under the E-Rechnungsverordnung.
	// Aligned with the KoSIT XRechnung specification.
	FacturXXRechnung FacturXLevel = "XRECHNUNG"
)

// ZUGFeRDVersion identifies a ZUGFeRD generation. The legacy 1.0 and
// 2.0 generations carry their own filename, namespace, and conformance
// profiles; 2.1 onwards is the joint specification shared with
// Factur-X 1.0+.
// Values are immutable and safe for concurrent use.
type ZUGFeRDVersion string

// ZUGFeRD generation constants.
const (
	// ZUGFeRD1 identifies the 2014 ZUGFeRD 1.0 specification.
	// Filename: ZUGFeRD-invoice.xml. AFRelationship: Alternative.
	// Profiles: BASIC, COMFORT, EXTENDED.
	ZUGFeRD1 ZUGFeRDVersion = "1.0"
	// ZUGFeRD2 identifies the 2018 ZUGFeRD 2.0 specification.
	// Filename: zugferd-invoice.xml. AFRelationship: Alternative.
	// Profiles: MINIMUM, BASIC, COMFORT, EXTENDED.
	ZUGFeRD2 ZUGFeRDVersion = "2.0"
	// ZUGFeRD21 identifies the joint ZUGFeRD 2.1+ / Factur-X 1.0+
	// specification. Filename: factur-x.xml, or xrechnung.xml for
	// XRECHNUNG. AFRelationship: Data for MINIMUM and BASIC WL,
	// Alternative for XRECHNUNG, and Data, Source or Alternative for the
	// other levels.
	// Profiles: MINIMUM, BASIC WL, BASIC, EN 16931, EXTENDED, XRECHNUNG.
	ZUGFeRD21 ZUGFeRDVersion = "2.1"
)

// String returns the canonical string form of the version, e.g.
// "2.1". The zero value renders as the empty string.
func (v ZUGFeRDVersion) String() string { return string(v) }

// IsValid reports whether v matches one of the documented ZUGFeRD
// version constants.
func (v ZUGFeRDVersion) IsValid() bool {
	switch v {
	case ZUGFeRD1, ZUGFeRD2, ZUGFeRD21:
		return true
	}
	return false
}

// String returns the canonical string form of the level, e.g.
// "EN 16931". The zero value renders as the empty string.
func (l FacturXLevel) String() string { return string(l) }

// IsValid reports whether l matches one of the documented Factur-X
// level constants. Use this to validate user input before passing the
// value to a remediation engine that assumes a known level.
func (l FacturXLevel) IsValid() bool {
	switch l {
	case FacturXMinimum, FacturXBasicWL, FacturXBasic, FacturXEN16931, FacturXExtended, FacturXXRechnung:
		return true
	}
	return false
}
