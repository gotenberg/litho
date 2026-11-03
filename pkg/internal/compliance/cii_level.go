package compliance

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
)

// InvoiceLevel reads the conformance level a Cross-Industry Invoice payload
// declares in its specification identifier, rsm:ExchangedDocumentContext/
// ram:GuidelineSpecifiedDocumentContextParameter/ram:ID. EN 16931-1 makes the
// identifier (BT-24) mandatory, with a cardinality of 1..1, and Factur-X
// 1.0.07, 4.3 fixes its value for each level.
//
// It returns the level and the identifier as the payload spells it. The level
// is empty when the identifier names no Factur-X level, or is absent. A
// payload that is not well-formed XML, or whose root is not CrossIndustryInvoice
// in the CII:100 namespace (EN 16931-3-3, the CII syntax binding), returns an
// error wrapping [ErrInvoiceNotCII], as [ValidateFacturX] reports it.
//
// The identifier of an XRechnung CIUS returns [models.FacturXXRechnung].
func InvoiceLevel(payload []byte) (models.FacturXLevel, string, error) {
	// Strict, as the validator reads the payload without WithLenient.
	_, rootSeen, wellFormed, parseErr := scanCIIPresence(payload, false)
	if !wellFormed {
		return "", "", fmt.Errorf("%w: the XML is not well-formed: %v", ErrInvoiceNotCII, parseErr)
	}
	if !rootSeen {
		return "", "", fmt.Errorf("%w: the root element is not CrossIndustryInvoice in namespace %q", ErrInvoiceNotCII, ciiRootNS)
	}
	guideline := strings.TrimSpace(scanCIIProfileID(payload))
	return levelOfGuideline(guideline), guideline, nil
}

// levelOfGuideline maps a CII specification identifier to the Factur-X level
// it declares, per Factur-X 1.0.07, 4.3, or "" for none. The XRechnung CIUS
// identifiers vary with its version and issuing body
// (urn:xoev-de:kosit:standard:xrechnung_2.x, urn:xeinkauf.de:kosit:xrechnung_3.0),
// so any identifier naming it maps to XRECHNUNG.
func levelOfGuideline(guideline string) models.FacturXLevel {
	if strings.Contains(strings.ToLower(guideline), "xrechnung") {
		return models.FacturXXRechnung
	}
	switch guideline {
	case "urn:factur-x.eu:1p0:minimum":
		return models.FacturXMinimum
	case "urn:factur-x.eu:1p0:basicwl":
		return models.FacturXBasicWL
	case "urn:cen.eu:en16931:2017#compliant#urn:factur-x.eu:1p0:basic":
		return models.FacturXBasic
	case "urn:cen.eu:en16931:2017":
		return models.FacturXEN16931
	case "urn:cen.eu:en16931:2017#conformant#urn:factur-x.eu:1p0:extended":
		return models.FacturXExtended
	}
	return ""
}
