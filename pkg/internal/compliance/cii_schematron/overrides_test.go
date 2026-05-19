package cii_schematron

import (
	"context"
	"strings"
	"testing"

	"github.com/antchfx/xmlquery"

	"github.com/gotenberg/litho/pkg/internal/schematron"
)

func TestIBANChecksum_Vectors(t *testing.T) {
	for _, c := range []struct {
		name string
		iban string
		want bool
	}{
		// ISO 7064 reference vectors. The German DE89 and French
		// FR14 entries appear verbatim in IBAN registries.
		{"valid_DE", "DE89370400440532013000", true},
		{"valid_FR", "FR1420041010050500013M02606", true},
		{"valid_GB", "GB82WEST12345698765432", true},
		{"flipped_digits", "DE98370400440532013000", false},
		{"too_short", "DE89", false},
		{"non_alnum", "DE89370400440532013 00", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ibanChecksumOK(c.iban); got != c.want {
				t.Errorf("ibanChecksumOK(%q) = %v, want %v", c.iban, got, c.want)
			}
		})
	}
}

func TestBuiltinRuleOverrides_BRDE19(t *testing.T) {
	overrides := BuiltinRuleOverrides()
	fn, ok := overrides["BR-DE-19"]
	if !ok {
		t.Fatal("BR-DE-19 missing from BuiltinRuleOverrides()")
	}

	cases := []struct {
		name string
		xml  string
		want bool
	}{
		{
			name: "type_not_58_passes",
			xml: `<rsm:SpecifiedTradeSettlementPaymentMeans xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100" xmlns:ram="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100">
  <ram:TypeCode>30</ram:TypeCode>
</rsm:SpecifiedTradeSettlementPaymentMeans>`,
			want: true,
		},
		{
			name: "type_58_valid_iban",
			xml: `<rsm:SpecifiedTradeSettlementPaymentMeans xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100" xmlns:ram="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100">
  <ram:TypeCode>58</ram:TypeCode>
  <ram:PayeePartyCreditorFinancialAccount>
    <ram:IBANID>DE89 3704 0044 0532 0130 00</ram:IBANID>
  </ram:PayeePartyCreditorFinancialAccount>
</rsm:SpecifiedTradeSettlementPaymentMeans>`,
			want: true,
		},
		{
			name: "type_58_invalid_checksum",
			xml: `<rsm:SpecifiedTradeSettlementPaymentMeans xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100" xmlns:ram="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100">
  <ram:TypeCode>58</ram:TypeCode>
  <ram:PayeePartyCreditorFinancialAccount>
    <ram:IBANID>DE98370400440532013000</ram:IBANID>
  </ram:PayeePartyCreditorFinancialAccount>
</rsm:SpecifiedTradeSettlementPaymentMeans>`,
			want: false,
		},
		{
			name: "type_58_missing_account",
			xml: `<rsm:SpecifiedTradeSettlementPaymentMeans xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100" xmlns:ram="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100">
  <ram:TypeCode>58</ram:TypeCode>
</rsm:SpecifiedTradeSettlementPaymentMeans>`,
			want: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, err := xmlquery.Parse(strings.NewReader(c.xml))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			ctxNode := xmlquery.FindOne(doc, "//*[local-name()='SpecifiedTradeSettlementPaymentMeans']")
			if ctxNode == nil {
				t.Fatal("no context node")
			}
			got, err := fn(context.Background(), ctxNode)
			if err != nil {
				t.Fatalf("override: %v", err)
			}
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestOverride_BRDE19_EndToEnd_NoAdvisory(t *testing.T) {
	// With the override registered, BR-DE-19 must not appear in the
	// schematron Skipped slice on a payload that drives the rule.
	const payload = `<?xml version="1.0"?>
<rsm:CrossIndustryInvoice xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"
                          xmlns:ram="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100">
  <rsm:SupplyChainTradeTransaction>
    <ram:ApplicableHeaderTradeSettlement>
      <ram:SpecifiedTradeSettlementPaymentMeans>
        <ram:TypeCode>58</ram:TypeCode>
        <ram:PayeePartyCreditorFinancialAccount>
          <ram:IBANID>DE89370400440532013000</ram:IBANID>
        </ram:PayeePartyCreditorFinancialAccount>
      </ram:SpecifiedTradeSettlementPaymentMeans>
    </ram:ApplicableHeaderTradeSettlement>
  </rsm:SupplyChainTradeTransaction>
</rsm:CrossIndustryInvoice>`

	ctx := context.Background()
	sch, err := LoadXRechnung(ctx)
	if err != nil {
		t.Fatalf("LoadXRechnung: %v", err)
	}
	violations, skipped, err := schematron.Validate(ctx, sch, []byte(payload),
		schematron.WithPhase("xrechnung-model"),
		schematron.WithRuleOverrides(BuiltinRuleOverrides()),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, s := range skipped {
		if s.RuleID == "BR-DE-19" {
			t.Errorf("BR-DE-19 still in skipped: %s", s.Reason)
		}
	}
	for _, v := range violations {
		if v.RuleID == "BR-DE-19" {
			t.Errorf("BR-DE-19 fired on a valid IBAN: %s", v.Error())
		}
	}
}
