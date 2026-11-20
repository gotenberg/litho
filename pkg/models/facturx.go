package models

// FacturX defaults applied by
// [github.com/gotenberg/litho/pkg/pdf.Writer.RemediateFacturX] when the
// corresponding [FacturXConfig] field is zero.
const (
	// DefaultFacturXProfile is the PDF/A-3 conformance level used when
	// [FacturXConfig.Profile] is empty.
	DefaultFacturXProfile = PDFA3b
	// DefaultFacturXDocumentType is the value written into the XMP
	// `fx:DocumentType` property when [FacturXConfig.DocumentType] is
	// empty.
	DefaultFacturXDocumentType = "INVOICE"
)

// FacturXConfig describes the inputs to
// [github.com/gotenberg/litho/pkg/pdf.Writer.RemediateFacturX]. Concurrent
// reads of a value are safe; a write needs synchronization.
type FacturXConfig struct {
	// InvoiceXML is the Cross-Industry Invoice (CII) XML payload that
	// will be embedded as the Factur-X data file. Required, non-empty.
	InvoiceXML []byte `json:"invoiceXML"`
	// Level is the Factur-X conformance level. Required; must satisfy
	// [FacturXLevel.IsValid].
	Level FacturXLevel `json:"level"`
	// Profile is the PDF/A-3 conformance level used as the underlying
	// archival base. Optional; defaults to [DefaultFacturXProfile].
	// Must be one of [PDFA3a], [PDFA3b], or
	// [PDFA3u].
	Profile PDFAProfile `json:"profile"`
	// FileName is the embedded file name. Optional; defaults to
	// "xrechnung.xml" for [FacturXXRechnung] and "factur-x.xml"
	// for every other level (Factur-X 1.07.2, 6.2 and 7.7). The same
	// value is written into the XMP `fx:DocumentFileName` property.
	FileName string `json:"fileName"`
	// DocumentType is the value of the XMP `fx:DocumentType` property.
	// Optional; defaults to [DefaultFacturXDocumentType].
	DocumentType string `json:"documentType"`
	// Relationship is the embedded-file `/AFRelationship` value.
	// Optional; defaults to "Data" for [FacturXMinimum] and
	// [FacturXBasicWL], whose XML carries less than the visual
	// part, and "Alternative" for every other level (Factur-X 1.07.2,
	// 6.2.2 and 7.7). BASIC, EN 16931 and EXTENDED also admit "Data" or
	// "Source" in France, depending on how the PDF was made; Germany
	// admits "Alternative" alone.
	Relationship string `json:"relationship"`
}
