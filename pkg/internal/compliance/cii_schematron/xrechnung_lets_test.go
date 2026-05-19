package cii_schematron

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/schematron"
)

// Regression coverage for BR-DE-30 / BR-DE-31. Both rules declare
// rule-scoped sch:let bindings whose values are path expressions
// (e.g. "rsm:.../ram:DirectDebitMandateID"). Evaluating those lets
// to a literal would flatten them to an empty string and flip the
// boolean coercion verdicts that the assertions rely on, so the
// evaluator must thread let values through as textual XPath
// subexpressions. The two cases below exercise the BG-19-absent
// and BG-19-partial branches separately.

const xrechnungLetsNoBG19 = `<?xml version="1.0"?>
<rsm:CrossIndustryInvoice xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"
                          xmlns:ram="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100"
                          xmlns:udt="urn:un:unece:uncefact:data:standard:UnqualifiedDataType:100">
  <rsm:ExchangedDocumentContext>
    <ram:GuidelineSpecifiedDocumentContextParameter><ram:ID>urn:cen.eu:en16931:2017#compliant#urn:xeinkauf.de:kosit:xrechnung_3.0</ram:ID></ram:GuidelineSpecifiedDocumentContextParameter>
  </rsm:ExchangedDocumentContext>
  <rsm:ExchangedDocument>
    <ram:ID>INV-1</ram:ID>
    <ram:TypeCode>380</ram:TypeCode>
    <ram:IssueDateTime><udt:DateTimeString format="102">20260513</udt:DateTimeString></ram:IssueDateTime>
  </rsm:ExchangedDocument>
  <rsm:SupplyChainTradeTransaction>
    <ram:ApplicableHeaderTradeAgreement>
      <ram:BuyerReference>X</ram:BuyerReference>
      <ram:SellerTradeParty><ram:Name>Seller</ram:Name></ram:SellerTradeParty>
      <ram:BuyerTradeParty><ram:Name>Buyer</ram:Name></ram:BuyerTradeParty>
    </ram:ApplicableHeaderTradeAgreement>
    <ram:ApplicableHeaderTradeDelivery/>
    <ram:ApplicableHeaderTradeSettlement>
      <ram:InvoiceCurrencyCode>EUR</ram:InvoiceCurrencyCode>
      <ram:SpecifiedTradeSettlementPaymentMeans>
        <ram:TypeCodeName>30</ram:TypeCodeName>
      </ram:SpecifiedTradeSettlementPaymentMeans>
      <ram:SpecifiedTradeSettlementHeaderMonetarySummation>
        <ram:GrandTotalAmount currencyID="EUR">100.00</ram:GrandTotalAmount>
        <ram:DuePayableAmount currencyID="EUR">100.00</ram:DuePayableAmount>
      </ram:SpecifiedTradeSettlementHeaderMonetarySummation>
    </ram:ApplicableHeaderTradeSettlement>
  </rsm:SupplyChainTradeTransaction>
</rsm:CrossIndustryInvoice>`

const xrechnungLetsPartialBG19 = `<?xml version="1.0"?>
<rsm:CrossIndustryInvoice xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"
                          xmlns:ram="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100"
                          xmlns:udt="urn:un:unece:uncefact:data:standard:UnqualifiedDataType:100">
  <rsm:ExchangedDocumentContext/>
  <rsm:ExchangedDocument>
    <ram:ID>INV-2</ram:ID>
    <ram:TypeCode>380</ram:TypeCode>
    <ram:IssueDateTime><udt:DateTimeString format="102">20260513</udt:DateTimeString></ram:IssueDateTime>
  </rsm:ExchangedDocument>
  <rsm:SupplyChainTradeTransaction>
    <ram:ApplicableHeaderTradeAgreement/>
    <ram:ApplicableHeaderTradeDelivery/>
    <ram:ApplicableHeaderTradeSettlement>
      <ram:SpecifiedTradePaymentTerms>
        <ram:DirectDebitMandateID>MANDATE-1</ram:DirectDebitMandateID>
      </ram:SpecifiedTradePaymentTerms>
    </ram:ApplicableHeaderTradeSettlement>
  </rsm:SupplyChainTradeTransaction>
</rsm:CrossIndustryInvoice>`

func TestXRechnung_PathValuedLets_Resolve(t *testing.T) {
	ctx := context.Background()
	sch, err := LoadXRechnung(ctx)
	if err != nil {
		t.Fatalf("LoadXRechnung: %v", err)
	}

	t.Run("no_BG19_passes", func(t *testing.T) {
		violations, skipped, err := schematron.Validate(ctx, sch, []byte(xrechnungLetsNoBG19), schematron.WithPhase("xrechnung-model"))
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		for _, s := range skipped {
			if s.RuleID == "BR-DE-30" || s.RuleID == "BR-DE-31" {
				t.Errorf("rule %s is still skipped: %s", s.RuleID, s.Reason)
			}
		}
		for _, v := range violations {
			if v.RuleID == "BR-DE-30" || v.RuleID == "BR-DE-31" {
				t.Errorf("unexpected violation on BG-19-absent payload: %s", v.Error())
			}
		}
	})

	t.Run("partial_BG19_fires", func(t *testing.T) {
		violations, skipped, err := schematron.Validate(ctx, sch, []byte(xrechnungLetsPartialBG19), schematron.WithPhase("xrechnung-model"))
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		for _, s := range skipped {
			if s.RuleID == "BR-DE-30" || s.RuleID == "BR-DE-31" {
				t.Errorf("rule %s skipped on partial BG-19: %s", s.RuleID, s.Reason)
			}
		}
		fired := map[string]bool{}
		for _, v := range violations {
			fired[v.RuleID] = true
		}
		if !fired["BR-DE-30"] {
			t.Errorf("BR-DE-30 should fire when BT-89 present but BT-90 missing; got rules: %v", uniqueRuleIDs(violations))
		}
		if !fired["BR-DE-31"] {
			t.Errorf("BR-DE-31 should fire when BT-89 present but BT-91 missing; got rules: %v", uniqueRuleIDs(violations))
		}
	})
}

func uniqueRuleIDs(vs []schematron.Violation) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range vs {
		if seen[v.RuleID] {
			continue
		}
		seen[v.RuleID] = true
		out = append(out, v.RuleID)
	}
	return out
}
