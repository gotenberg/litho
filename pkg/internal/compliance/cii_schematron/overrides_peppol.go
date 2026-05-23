package cii_schematron

import (
	"context"
	"math"
	"strings"

	"github.com/antchfx/xmlquery"
)

// PEPPOL BIS 3.0 + XRechnung-overlay rules whose XPath source uses
// constructs the antchfx engine cannot evaluate: the XSLT helper
// function `u:slack(...)`, top-level `if (...) then ... else ...`
// expressions, and arithmetic-tolerance idioms wrapped in
// `for $V in ... return ...` let-lists. The Go evaluators here port
// each rule's spec intent directly so the verdict reflects the
// schematron rather than landing in the skipped-advisory slice.

// peppolR120LineNetAmount implements PEPPOL-EN16931-R120: the
// invoice line net amount equals the quantity multiplied by the
// item net price (per the price base quantity) plus charges minus
// allowances, within the `u:slack` tolerance the schematron
// computes from line counts.
//
// Spec wording (XPath 2.0 form):
//
//	u:slack($lineExtensionAmount,
//	        ($quantity * ($priceAmount div $baseQuantity)) +
//	        $chargesTotal - $allowancesTotal,
//	        $slackValue)
//
// where u:slack(a, b, t) returns true when |a-b| <= t. The
// schematron's $slackValue is 0.02 + 1/(10^scale_of_price), where
// scale_of_price counts the price's fractional digits; this Go
// port honours the same scale-aware tolerance so prices with many
// decimal places get the wider band the XSLT helper computes.
//
// Context: ram:IncludedSupplyChainTradeLineItem (the invoice line).
func peppolR120LineNetAmount(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil {
		return true, nil
	}
	settlement := findChild(node, "SpecifiedLineTradeSettlement")
	if settlement == nil {
		return true, nil
	}
	monetary := findChild(settlement, "SpecifiedTradeSettlementLineMonetarySummation")
	if monetary == nil {
		return true, nil
	}
	lineExt, ok := floatChild(monetary, "LineTotalAmount")
	if !ok {
		return true, nil
	}

	delivery := findChild(node, "SpecifiedLineTradeDelivery")
	quantity := 0.0
	if delivery != nil {
		quantity, _ = floatChild(delivery, "BilledQuantity")
	}

	agreement := findChild(node, "SpecifiedLineTradeAgreement")
	netPrice := findChild(agreement, "NetPriceProductTradePrice")
	priceAmount, _ := floatChild(netPrice, "ChargeAmount")
	priceScale := 0
	if c := findChild(netPrice, "ChargeAmount"); c != nil {
		priceScale = decimalScale(c.InnerText())
	}
	baseQuantity := 1.0
	if v, ok := floatChild(netPrice, "BasisQuantity"); ok && v != 0 {
		baseQuantity = v
	}

	var allowancesTotal, chargesTotal float64
	for c := settlement.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xmlquery.ElementNode || c.Data != "SpecifiedTradeAllowanceCharge" {
			continue
		}
		actual, _ := floatChild(c, "ActualAmount")
		if indicatorText(c) == "true" {
			chargesTotal += actual
		} else {
			allowancesTotal += actual
		}
	}

	computed := quantity*(priceAmount/baseQuantity) + chargesTotal - allowancesTotal
	// u:slack: |LineExtensionAmount - computed| <= 0.02 + 1/(10^scale).
	tolerance := 0.02 + math.Pow(10, -float64(priceScale))
	return math.Abs(lineExt-computed) <= tolerance, nil
}

// peppolR046GrossPrice implements PEPPOL-EN16931-R046: when an
// invoice line carries a gross price (ram:GrossPriceProductTradePrice/
// ram:ChargeAmount) the item net price equals the gross price minus
// any applied allowance amount. The schematron source uses
// `xs:decimal(...)` casts on path-step references which the rewriter
// cannot fully neutralise; the Go port reads the values directly.
//
// Context: ram:GrossPriceProductTradePrice. Skips when no gross
// charge amount is present (rule's "not(ram:ChargeAmount)" branch).
func peppolR046GrossPrice(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil {
		return true, nil
	}
	gross, ok := floatChild(node, "ChargeAmount")
	if !ok {
		return true, nil
	}
	allowance := 0.0
	if charge := findChild(node, "AppliedTradeAllowanceCharge"); charge != nil {
		allowance, _ = floatChild(charge, "ActualAmount")
	}
	parent := node.Parent
	if parent == nil {
		return true, nil
	}
	netPrice := findChild(parent, "NetPriceProductTradePrice")
	if netPrice == nil {
		return true, nil
	}
	netCharge, ok := floatChild(netPrice, "ChargeAmount")
	if !ok {
		return true, nil
	}
	return math.Abs(netCharge-(gross-allowance)) < 1e-6, nil
}

// peppolR054TaxCurrencyCount implements PEPPOL-EN16931-R054: when
// the document declares a TaxCurrencyCode (BT-6) distinct from the
// invoice currency, exactly one TaxTotalAmount must carry the
// tax currency; otherwise zero TaxTotalAmount entries should carry
// a divergent currency. Context: ram:ApplicableHeaderTradeSettlement.
func peppolR054TaxCurrencyCount(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil {
		return true, nil
	}
	invoiceCurrency := strings.TrimSpace(childText(node, "InvoiceCurrencyCode"))
	hasTaxCurrency := findChild(node, "TaxCurrencyCode") != nil
	monetary := findChild(node, "SpecifiedTradeSettlementHeaderMonetarySummation")
	if monetary == nil {
		return !hasTaxCurrency, nil
	}
	divergent := 0
	for c := monetary.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xmlquery.ElementNode || c.Data != "TaxTotalAmount" {
			continue
		}
		cur := attrValue(c, "currencyID")
		if cur != "" && cur != invoiceCurrency {
			divergent++
		}
	}
	want := 0
	if hasTaxCurrency {
		want = 1
	}
	return divergent == want, nil
}

// brDETMP32DeliveryHint implements BR-DE-TMP-32: an invoice should
// surface a delivery or invoicing period, either via the header
// ActualDeliverySupplyChainEvent / BillingSpecifiedPeriod or per
// invoice line via SpecifiedLineTradeSettlement/BillingSpecifiedPeriod.
// `flag="information"` in source → maps to Rule_cii_sch_Warning via
// the severity mapper. Context: SupplyChainTradeTransaction.
//
// The source uses `every $line in lines satisfies ...` which only
// peelQuantifier handles at the top level. Here the every sits
// inside an `or` chain so the override carries the same logic in Go.
func brDETMP32DeliveryHint(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil {
		return true, nil
	}
	if delivery := findChild(node, "ApplicableHeaderTradeDelivery"); delivery != nil {
		if ev := findChild(delivery, "ActualDeliverySupplyChainEvent"); ev != nil {
			if findChild(ev, "OccurrenceDateTime") != nil {
				return true, nil
			}
		}
	}
	if settlement := findChild(node, "ApplicableHeaderTradeSettlement"); settlement != nil {
		if findChild(settlement, "BillingSpecifiedPeriod") != nil {
			return true, nil
		}
	}
	lineCount := 0
	everyLine := true
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xmlquery.ElementNode || c.Data != "IncludedSupplyChainTradeLineItem" {
			continue
		}
		lineCount++
		ls := findChild(c, "SpecifiedLineTradeSettlement")
		if ls == nil || findChild(ls, "BillingSpecifiedPeriod") == nil {
			everyLine = false
		}
	}
	if lineCount == 0 {
		// XPath 2.0 vacuous truth: every() over empty sequence is
		// true. Without lines the assertion still passes per spec.
		return true, nil
	}
	return everyLine, nil
}

// brFXEXTIC08IntracommunityTaxBasis implements FX-SCH-A-000370 /
// BR-FXEXT-IC-08: when a VAT breakdown's CategoryCode is "K"
// (intra-community supply), the basis amount equals the sum of the
// matching K-category line net amounts plus K-category logistics
// charges minus K-category allowances, within
// 0.01 × (line + allowance + charge + logistic-charge counts).
//
// Context: ram:ApplicableTradeTax/ram:CategoryCode (the K value
// filter is checked by the override too in case the predicate
// drops out under the rewriter pipeline).
func brFXEXTIC08IntracommunityTaxBasis(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil || node.Parent == nil {
		return true, nil
	}
	if strings.TrimSpace(node.InnerText()) != "K" {
		return true, nil
	}
	tradeTax := node.Parent
	basis, ok := floatChild(tradeTax, "BasisAmount")
	if !ok {
		return true, nil
	}
	root := topAncestor(node)
	tx := findFirstByPath(root, "CrossIndustryInvoice", "SupplyChainTradeTransaction")
	if tx == nil {
		tx = findFirstByPath(root, "SupplyChainTradeTransaction")
	}
	if tx == nil {
		return true, nil
	}
	var lineSum, chargeSum, logisticSum, allowanceSum float64
	var lineCount, allowanceOrChargeCount, logisticCount int
	for c := tx.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xmlquery.ElementNode || c.Data != "IncludedSupplyChainTradeLineItem" {
			continue
		}
		settlement := findChild(c, "SpecifiedLineTradeSettlement")
		if settlement == nil {
			continue
		}
		if lineCategory(settlement) != "K" {
			continue
		}
		if !isDetailLine(c) {
			continue
		}
		lineCount++
		monetary := findChild(settlement, "SpecifiedTradeSettlementLineMonetarySummation")
		if monetary != nil {
			v, _ := floatChild(monetary, "LineTotalAmount")
			lineSum += v
		}
	}
	headerSettlement := findChild(tx, "ApplicableHeaderTradeSettlement")
	if headerSettlement != nil {
		for c := headerSettlement.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != xmlquery.ElementNode {
				continue
			}
			switch c.Data {
			case "SpecifiedTradeAllowanceCharge":
				cat := chargeCategory(c)
				if cat != "K" {
					continue
				}
				allowanceOrChargeCount++
				v, _ := floatChild(c, "ActualAmount")
				if indicatorText(c) == "true" {
					chargeSum += v
				} else {
					allowanceSum += v
				}
			case "SpecifiedLogisticsServiceCharge":
				if appliedTaxCategory(c) != "K" {
					continue
				}
				logisticCount++
				v, _ := floatChild(c, "AppliedAmount")
				logisticSum += v
			}
		}
	}
	lineR := math.Round(lineSum*100) / 100
	chargeR := math.Round(chargeSum*100) / 100
	logisticR := math.Round(logisticSum*100) / 100
	allowanceR := math.Round(allowanceSum*100) / 100
	expected := lineR + chargeR + logisticR - allowanceR
	tolerance := 0.01 * float64(lineCount+allowanceOrChargeCount+logisticCount)
	return math.Abs(basis-expected) <= tolerance, nil
}

// lineCategory returns the VAT category code declared on the
// line's ApplicableTradeTax child, or empty when the path is absent.
func lineCategory(lineSettlement *xmlquery.Node) string {
	tax := findChild(lineSettlement, "ApplicableTradeTax")
	if tax == nil {
		return ""
	}
	return strings.TrimSpace(childText(tax, "CategoryCode"))
}

// chargeCategory returns the VAT category code declared on the
// document-level allowance/charge entry's CategoryTradeTax child.
func chargeCategory(charge *xmlquery.Node) string {
	tax := findChild(charge, "CategoryTradeTax")
	if tax == nil {
		return ""
	}
	return strings.TrimSpace(childText(tax, "CategoryCode"))
}

// appliedTaxCategory returns the VAT category code declared on a
// document-level logistics-service-charge entry's AppliedTradeTax.
func appliedTaxCategory(charge *xmlquery.Node) string {
	tax := findChild(charge, "AppliedTradeTax")
	if tax == nil {
		return ""
	}
	return strings.TrimSpace(childText(tax, "CategoryCode"))
}

// decimalScale returns the number of fractional digits in s
// (e.g. "12.345" → 3). Leading / trailing whitespace is trimmed;
// non-decimal values return 0.
func decimalScale(s string) int {
	s = strings.TrimSpace(s)
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		return 0
	}
	return len(s) - dot - 1
}
