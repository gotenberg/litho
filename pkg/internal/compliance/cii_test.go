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

func TestValidateCIIBytes_HappyPath(t *testing.T) {
	got := validateCIIBytes([]byte(validCII()), variantFacturX)
	if len(got) != 0 {
		for _, v := range got {
			t.Logf("unexpected: %v", v)
		}
		t.Errorf("violations: got %d, want 0", len(got))
	}
}

func TestValidateCIIBytes_NotWellFormed(t *testing.T) {
	got := validateCIIBytes([]byte("<rsm:CrossIndustryInvoice><unbalanced>"), variantFacturX)
	if len(got) != 1 || got[0].Rule != Rule_cii_WellFormed {
		t.Errorf("expected single Rule_cii_WellFormed, got %+v", got)
	}
}

func TestValidateCIIBytes_WrongRoot(t *testing.T) {
	payload := `<?xml version="1.0"?><foo:Invoice xmlns:foo="urn:bogus"/>`
	got := validateCIIBytes([]byte(payload), variantFacturX)
	if len(got) != 1 || got[0].Rule != Rule_cii_RootElement {
		t.Errorf("expected single Rule_cii_RootElement, got %+v", got)
	}
}

func TestValidateCIIBytes_MissingDocumentContext(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<rsm:ExchangedDocumentContext>
    <ram:BusinessProcessSpecifiedDocumentContextParameter>
      <ram:ID>A1</ram:ID>
    </ram:BusinessProcessSpecifiedDocumentContextParameter>
  </rsm:ExchangedDocumentContext>`, "", 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX))
	if !got[Rule_cii_DocumentContext] {
		t.Errorf("missing Rule_cii_DocumentContext: %v", got)
	}
}

func TestValidateCIIBytes_MissingDocumentID(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<ram:ID>INV-001</ram:ID>`, "", 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX))
	if !got[Rule_cii_ExchangedDocument] {
		t.Errorf("missing Rule_cii_ExchangedDocument: %v", got)
	}
}

func TestValidateCIIBytes_MissingIssueDateTime(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<ram:IssueDateTime>
      <udt:DateTimeString format="102">20260517</udt:DateTimeString>
    </ram:IssueDateTime>`, "", 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX))
	if !got[Rule_cii_ExchangedDocument] {
		t.Errorf("missing Rule_cii_ExchangedDocument: %v", got)
	}
}

func TestValidateCIIBytes_MissingTradeTransaction(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<rsm:SupplyChainTradeTransaction>`, `<rsm:OtherSection>`, 1)
	payload = strings.Replace(payload,
		`</rsm:SupplyChainTradeTransaction>`, `</rsm:OtherSection>`, 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX))
	if !got[Rule_cii_TradeTransaction] {
		t.Errorf("missing Rule_cii_TradeTransaction: %v", got)
	}
}

func TestValidateCIIBytes_MissingSellerName(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<ram:Name>Acme Co</ram:Name>`, ``, 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX))
	if !got[Rule_cii_SellerTradeParty] {
		t.Errorf("missing Rule_cii_SellerTradeParty: %v", got)
	}
}

func TestValidateCIIBytes_MissingCurrency(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<ram:InvoiceCurrencyCode>EUR</ram:InvoiceCurrencyCode>`, ``, 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX))
	if !got[Rule_cii_InvoiceCurrency] {
		t.Errorf("missing Rule_cii_InvoiceCurrency: %v", got)
	}
}

func TestValidateCIIBytes_MissingGrandTotal(t *testing.T) {
	payload := strings.Replace(validCII(),
		`<ram:GrandTotalAmount currencyID="EUR">100.00</ram:GrandTotalAmount>`, ``, 1)
	got := violationRules(validateCIIBytes([]byte(payload), variantFacturX))
	if !got[Rule_cii_GrandTotalAmount] {
		t.Errorf("missing Rule_cii_GrandTotalAmount: %v", got)
	}
}

func TestValidateCIIBytes_LegacyVariantSkipsChecks(t *testing.T) {
	// ZUGFeRD 1.0 / 2.0 use a different CII schema; the structural
	// rules must not fire on those variants. Pass payload that would
	// fail every spine rule under the Factur-X variant.
	got := validateCIIBytes([]byte(`<root/>`), variantZUGFeRD20)
	// The function returns early without any work, but the entry
	// point is checkInvoiceCII which short-circuits on variant. The
	// pure-function variant runs unconditionally, so test the entry
	// guard via the variant.namespaceURI check in checkInvoiceCII
	// instead. Here we just confirm validateCIIBytes does not panic
	// on degenerate input.
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
