package compliance

import "testing"

// ciiInvoiceXML is a spine-shaped CrossIndustryInvoice: the root, the document
// context carrying a guideline ID, and the exchanged document. It exists so the
// fuzzer starts inside the element walk instead of failing at the root check.
const ciiInvoiceXML = `<?xml version="1.0" encoding="UTF-8"?>` +
	`<rsm:CrossIndustryInvoice ` +
	`xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100" ` +
	`xmlns:ram="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100">` +
	`<rsm:ExchangedDocumentContext>` +
	`<ram:GuidelineSpecifiedDocumentContextParameter>` +
	`<ram:ID>urn:cen.eu:en16931:2017</ram:ID>` +
	`</ram:GuidelineSpecifiedDocumentContextParameter>` +
	`</rsm:ExchangedDocumentContext>` +
	`<rsm:ExchangedDocument><ram:ID>INV-1</ram:ID>` +
	`<ram:IssueDateTime><udt:DateTimeString format="102" ` +
	`xmlns:udt="urn:un:unece:uncefact:data:standard:UnqualifiedDataType:100">20260101</udt:DateTimeString>` +
	`</ram:IssueDateTime></rsm:ExchangedDocument>` +
	`</rsm:CrossIndustryInvoice>`

// FuzzCII feeds arbitrary bytes to the Cross-Industry Invoice readers and
// asserts crash-freedom.
//
// The payload is the decoded content of an embedded file stream: a PDF names
// the invoice through /AF and litho decodes and parses whatever bytes it finds
// there, so this is untrusted input on the ordinary Factur-X validation path.
// Nothing fuzzed any of it; measured against the corpora, all four readers in
// cii.go sat at zero coverage.
//
// Both decoder modes run because leniency changes the parser itself: it turns
// off strictness and installs the HTML entity table, which is a different set
// of paths through encoding/xml, not merely a different verdict.
func FuzzCII(f *testing.F) {
	f.Add([]byte(ciiInvoiceXML))
	f.Add([]byte(ciiInvoiceXML[:len(ciiInvoiceXML)/2]))
	f.Add([]byte{})
	f.Add([]byte(`<rsm:CrossIndustryInvoice/>`))
	// An HTML named entity, which CII never defines: strict mode must reject
	// it and lenient mode must resolve it.
	f.Add([]byte(`<?xml version="1.0"?><a>&nbsp;</a>`))
	// An internal entity declaration, the shape a nested-expansion attack
	// takes. The readers must not expand it without bound.
	f.Add([]byte(`<?xml version="1.0"?><!DOCTYPE a [<!ENTITY x "yyyyyyyyyy">]><a>&x;&x;&x;</a>`))
	// A declared encoding the bytes are not.
	f.Add([]byte(`<?xml version="1.0" encoding="UTF-16"?><rsm:CrossIndustryInvoice/>`))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}

		_ = scanCIIProfileID(data)

		for _, lenient := range []bool{false, true} {
			_, _, _, _ = scanCIIPresence(data, lenient)
			for _, v := range []hybridInvoiceVariant{variantFacturX, variantZUGFeRD20, variantZUGFeRD10} {
				_ = validateCIIBytes(data, v, lenient)
			}
		}
	})
}

// TestCIIInvoiceXMLIsRead pins that the seed reaches the element walk: it must
// be recognised as a CII root and yield the guideline ID the profile router
// keys on. A seed rejected at the root would leave the walk unexercised while
// the target looked well seeded.
func TestCIIInvoiceXMLIsRead(t *testing.T) {
	t.Parallel()

	data := []byte(ciiInvoiceXML)

	if got := scanCIIProfileID(data); got != "urn:cen.eu:en16931:2017" {
		t.Errorf("scanCIIProfileID = %q, want the guideline ID", got)
	}

	_, rootSeen, wellFormed, err := scanCIIPresence(data, false)
	if err != nil {
		t.Fatalf("scanCIIPresence: %v", err)
	}
	if !rootSeen {
		t.Error("the CII root was not recognised")
	}
	if !wellFormed {
		t.Error("the seed was judged not well-formed")
	}
}
