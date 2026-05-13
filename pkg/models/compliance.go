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

// String returns the canonical string form of the profile, e.g.
// "PDF/A-2b". The zero value renders as the empty string.
func (p PDFAProfile) String() string { return string(p) }

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
	// specification. Filename: factur-x.xml. AFRelationship: Data.
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
