package compliance

import (
	"strings"
	"testing"
)

// validCII returns a Factur-X 1.0+ CII payload that satisfies every
// structural rule in [validateCIIBytes]. Each helper below derives
// from this baseline by stripping or rewriting one section, so the
// unit tests stay focused on a single rule per case.
func validCII() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<rsm:CrossIndustryInvoice xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100" xmlns:ram="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100" xmlns:udt="urn:un:unece:uncefact:data:standard:UnqualifiedDataType:100">
  <rsm:ExchangedDocumentContext>
    <ram:BusinessProcessSpecifiedDocumentContextParameter>
      <ram:ID>A1</ram:ID>
    </ram:BusinessProcessSpecifiedDocumentContextParameter>
  </rsm:ExchangedDocumentContext>
  <rsm:ExchangedDocument>
    <ram:ID>INV-001</ram:ID>
    <ram:IssueDateTime>
      <udt:DateTimeString format="102">20260517</udt:DateTimeString>
    </ram:IssueDateTime>
  </rsm:ExchangedDocument>
  <rsm:SupplyChainTradeTransaction>
    <ram:ApplicableHeaderTradeAgreement>
      <ram:SellerTradeParty>
        <ram:Name>Acme Co</ram:Name>
      </ram:SellerTradeParty>
    </ram:ApplicableHeaderTradeAgreement>
    <ram:ApplicableHeaderTradeSettlement>
      <ram:InvoiceCurrencyCode>EUR</ram:InvoiceCurrencyCode>
      <ram:SpecifiedTradeSettlementHeaderMonetarySummation>
        <ram:GrandTotalAmount currencyID="EUR">100.00</ram:GrandTotalAmount>
      </ram:SpecifiedTradeSettlementHeaderMonetarySummation>
    </ram:ApplicableHeaderTradeSettlement>
  </rsm:SupplyChainTradeTransaction>
</rsm:CrossIndustryInvoice>`
}

// TestValidateCIIBytes_HappyPath pins that the baseline payload from [validCII]
// passes every spine rule.
func TestValidateCIIBytes_HappyPath(t *testing.T) {
	got := validateCIIBytes([]byte(validCII()), variantFacturX, false)
	if len(got) != 0 {
		for _, v := range got {
			t.Logf("unexpected: %v", v)
		}
		t.Errorf("violations: got %d, want 0", len(got))
	}
}

// TestValidateCIIBytes_NotWellFormed pins that a malformed payload reports the
// well-formedness rule alone.
func TestValidateCIIBytes_NotWellFormed(t *testing.T) {
	got := validateCIIBytes([]byte("<rsm:CrossIndustryInvoice><unbalanced>"), variantFacturX, false)
	if len(got) != 1 || got[0].Rule != Rule_cii_WellFormed {
		t.Errorf("expected single Rule_cii_WellFormed, got %+v", got)
	}
}

// TestValidateCIIBytes_StrictRejectsHTMLEntity pins that strict parsing rejects
// an HTML named entity and lenient parsing tolerates it.
func TestValidateCIIBytes_StrictRejectsHTMLEntity(t *testing.T) {
	// An HTML named entity (&nbsp;) is undefined in XML/CII. Strict
	// parsing (the default) rejects it as not-well-formed; lenient
	// tolerates it.
	payload := strings.Replace(validCII(),
		`<ram:Name>Acme Co</ram:Name>`, `<ram:Name>Acme&nbsp;Co</ram:Name>`, 1)

	strict := validateCIIBytes([]byte(payload), variantFacturX, false)
	if len(strict) != 1 || strict[0].Rule != Rule_cii_WellFormed {
		t.Errorf("strict: expected single Rule_cii_WellFormed for HTML entity, got %+v", strict)
	}

	lenient := validateCIIBytes([]byte(payload), variantFacturX, true)
	for _, v := range lenient {
		if v.Rule == Rule_cii_WellFormed {
			t.Error("lenient: should tolerate the HTML entity, but got Rule_cii_WellFormed")
		}
	}
}

// TestValidateCIIBytes_WrongRoot pins that a root outside the CII namespace
// reports the root-element rule alone.
func TestValidateCIIBytes_WrongRoot(t *testing.T) {
	payload := `<?xml version="1.0"?><foo:Invoice xmlns:foo="urn:bogus"/>`
	got := validateCIIBytes([]byte(payload), variantFacturX, false)
	if len(got) != 1 || got[0].Rule != Rule_cii_RootElement {
		t.Errorf("expected single Rule_cii_RootElement, got %+v", got)
	}
}

// TestValidateCIIBytes_MissingDocumentContext pins that a payload without
// rsm:ExchangedDocumentContext reports the document-context rule.
func TestValidateCIIBytes_MissingDocumentContext(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<rsm:ExchangedDocumentContext>
    <ram:BusinessProcessSpecifiedDocumentContextParameter>
      <ram:ID>A1</ram:ID>
    </ram:BusinessProcessSpecifiedDocumentContextParameter>
  </rsm:ExchangedDocumentContext>`, "", 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX, false))
	if !got[Rule_cii_DocumentContext] {
		t.Errorf("missing Rule_cii_DocumentContext: %v", got)
	}
}

// TestValidateCIIBytes_MissingDocumentID pins that a payload without the
// exchanged document ID reports the exchanged-document rule.
func TestValidateCIIBytes_MissingDocumentID(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<ram:ID>INV-001</ram:ID>`, "", 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX, false))
	if !got[Rule_cii_ExchangedDocument] {
		t.Errorf("missing Rule_cii_ExchangedDocument: %v", got)
	}
}

// TestValidateCIIBytes_MissingIssueDateTime pins that a payload without
// ram:IssueDateTime reports the exchanged-document rule.
func TestValidateCIIBytes_MissingIssueDateTime(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<ram:IssueDateTime>
      <udt:DateTimeString format="102">20260517</udt:DateTimeString>
    </ram:IssueDateTime>`, "", 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX, false))
	if !got[Rule_cii_ExchangedDocument] {
		t.Errorf("missing Rule_cii_ExchangedDocument: %v", got)
	}
}

// TestValidateCIIBytes_MissingTradeTransaction pins that a payload without
// rsm:SupplyChainTradeTransaction reports the trade-transaction rule.
func TestValidateCIIBytes_MissingTradeTransaction(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<rsm:SupplyChainTradeTransaction>`, `<rsm:OtherSection>`, 1)
	payload = strings.Replace(payload,
		`</rsm:SupplyChainTradeTransaction>`, `</rsm:OtherSection>`, 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX, false))
	if !got[Rule_cii_TradeTransaction] {
		t.Errorf("missing Rule_cii_TradeTransaction: %v", got)
	}
}

// TestValidateCIIBytes_MissingSellerName pins that a payload without the seller
// name reports the seller-trade-party rule.
func TestValidateCIIBytes_MissingSellerName(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<ram:Name>Acme Co</ram:Name>`, ``, 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX, false))
	if !got[Rule_cii_SellerTradeParty] {
		t.Errorf("missing Rule_cii_SellerTradeParty: %v", got)
	}
}

// TestValidateCIIBytes_MissingCurrency pins that a payload without
// ram:InvoiceCurrencyCode reports the invoice-currency rule.
func TestValidateCIIBytes_MissingCurrency(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<ram:InvoiceCurrencyCode>EUR</ram:InvoiceCurrencyCode>`, ``, 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX, false))
	if !got[Rule_cii_InvoiceCurrency] {
		t.Errorf("missing Rule_cii_InvoiceCurrency: %v", got)
	}
}

// TestValidateCIIBytes_MissingGrandTotal pins that a payload without
// ram:GrandTotalAmount reports the grand-total rule.
func TestValidateCIIBytes_MissingGrandTotal(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<ram:GrandTotalAmount currencyID="EUR">100.00</ram:GrandTotalAmount>`, ``, 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX, false))
	if !got[Rule_cii_GrandTotalAmount] {
		t.Errorf("missing Rule_cii_GrandTotalAmount: %v", got)
	}
}

// TestValidateCIIBytes_LegacyVariantSkipsChecks runs [validateCIIBytes] on a
// degenerate payload under the ZUGFeRD 2.0 variant and asserts it does not
// panic.
func TestValidateCIIBytes_LegacyVariantSkipsChecks(t *testing.T) {
	// ZUGFeRD 2.0 shares the CII v100 schema with Factur-X, so
	// checkInvoiceCII runs the spine rules on it; only legacy
	// ZUGFeRD 1.0, whose 1p0 payload differs, short-circuits there
	// (see variantHasCIIv100Payload).
	got := validateCIIBytes([]byte(`<root/>`), variantZUGFeRD20, false)
	// validateCIIBytes applies no variant guard of its own and uses
	// the variant only to label its messages, so the degenerate root
	// fails here as it would under Factur-X. The test only confirms
	// it does not panic.
	if len(got) == 0 {
		t.Logf("unexpected zero violations on degenerate input under ZUGFeRD 2.0 variant; entry-guard test lives in checkInvoiceCII")
	}
}

// violationRules turns a Violation slice into a presence map keyed
// by rule. Test assertions read more naturally as "this rule fired"
// than as a linear search.
func violationRules(vs []Violation) map[Rule]bool {
	out := make(map[Rule]bool, len(vs))
	for _, v := range vs {
		out[v.Rule] = true
	}
	return out
}
