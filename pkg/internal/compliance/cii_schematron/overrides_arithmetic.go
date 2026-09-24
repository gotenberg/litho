package cii_schematron

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/antchfx/xmlquery"
)

// EN 16931 / Factur-X / ZUGFeRD arithmetic-tolerance rules the antchfx
// XPath engine cannot evaluate: the source schematron expresses them
// with XPath 2.0 `for $V in ... return ...` let-lists,
// `abs()` (which antchfx does not implement), `xs:decimal()` casts in
// path-step position, and similar XPath 2.0 idioms. The
// transformations in pkg/internal/schematron/xpath_rewrite.go
// cover the common shapes but cannot generally convert
// `abs(X) +/- C op Y` into an XPath 1.0 form. The Go evaluators here
// replicate the arithmetic intent directly.
//
// Each function below follows the same pattern as the existing
// overrides in overrides.go: read the context node and a small subtree,
// extract numeric and string values via the helpers in overrides.go,
// and return (assertionResult, error) where assertionResult is true
// when the rule passes for the matched context node and false when
// it fires.

// monetaryTolerance is the ±1 window, in whole currency units rather than
// cents, that the EN 16931 schematron literals of BR-CO-17 and BR-S-09 allow
// between a declared and a computed VAT amount.
const monetaryTolerance = 1.0

// brCO17VATCalc implements BR-CO-17 (FX-SCH-A-000052): the VAT
// category tax amount equals the VAT category taxable amount
// multiplied by the VAT category rate divided by 100, rounded to
// two decimals. Spec wording in the schematron source:
//
//	(round(rate)=0 and round(CalculatedAmount)=0)
//	or (round(rate)!=0 and |CalculatedAmount| ∈ [computed-1, computed+1])
//	or (rate-not-present and round(CalculatedAmount)=0)
//
// where computed = round(|BasisAmount| × rate) / 100. The tolerance
// (±1) comes from the schematron literal, not from this Go port.
//
// Context: //ram:ApplicableHeaderTradeSettlement/ram:ApplicableTradeTax.
// Only VAT-typed entries (ram:TypeCode trimmed/upper-cased to "VAT")
// are checked; other types pass through.
func brCO17VATCalc(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil {
		return true, nil
	}
	if strings.ToUpper(strings.TrimSpace(childText(node, "TypeCode"))) != "VAT" {
		return true, nil
	}
	calculated, calcOK := floatChild(node, "CalculatedAmount")
	rateRaw, rateOK := floatChild(node, "RateApplicablePercent")
	basis, _ := floatChild(node, "BasisAmount")

	// "round(rate) = 0" path: zero rate or absent rate forces zero
	// calculated amount (rounded to integer).
	if !rateOK || math.Round(rateRaw) == 0 {
		if !calcOK {
			return true, nil
		}
		return math.Round(calculated) == 0, nil
	}

	// "round(rate) != 0" path: |CalculatedAmount| within ±1 of
	// rounded(|basis| × rate) / 100.
	computed := math.Round(math.Abs(basis)*rateRaw) / 100
	calcAbs := math.Abs(calculated)
	return calcAbs-monetaryTolerance <= computed && calcAbs+monetaryTolerance >= computed, nil
}

// brS09StandardVAT implements BR-S-09 (FX-SCH-A-000057): for VAT
// breakdowns whose category code is "S" ("Standard rated"), the
// calculated VAT amount equals the basis amount multiplied by the
// rate, within a ±1 tolerance per the schematron literal. Context
// node is the CategoryCode element so the override walks up to the
// parent ApplicableTradeTax for the rate / basis / calculated
// values.
func brS09StandardVAT(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil || node.Parent == nil {
		return true, nil
	}
	if strings.TrimSpace(node.InnerText()) != "S" {
		return true, nil
	}
	tradeTax := node.Parent
	calculated, calcOK := floatChild(tradeTax, "CalculatedAmount")
	rate, rateOK := floatChild(tradeTax, "RateApplicablePercent")
	basis, _ := floatChild(tradeTax, "BasisAmount")
	if !calcOK || !rateOK {
		// No values to compare against; spec falls back to the
		// equality which trivially holds against empty siblings.
		return true, nil
	}
	computed := math.Round(math.Abs(basis)*rate) / 100
	calcAbs := math.Abs(calculated)
	return calcAbs-monetaryTolerance < computed && calcAbs+monetaryTolerance > computed, nil
}

// brFXEXTCO11AllowanceTotal implements FX-SCH-A-000304 (BR-FXEXT-CO-11):
// |AllowanceTotalAmount - Σ Document-level allowance amounts| <= 0.01 ×
// number-of-allowance-items. Context node is
// SpecifiedTradeSettlementHeaderMonetarySummation.
func brFXEXTCO11AllowanceTotal(_ context.Context, node *xmlquery.Node) (bool, error) {
	return docLevelChargeAllowanceTolerance(node, false)
}

// brFXEXTCO12ChargeTotal implements FX-SCH-A-000305 (BR-FXEXT-CO-12):
// |ChargeTotalAmount - Σ Document-level charge amounts| <= 0.01 ×
// number-of-charge-items, where charges are
// SpecifiedTradeAllowanceCharge with ChargeIndicator/Indicator=true.
func brFXEXTCO12ChargeTotal(_ context.Context, node *xmlquery.Node) (bool, error) {
	return docLevelChargeAllowanceTolerance(node, true)
}

func docLevelChargeAllowanceTolerance(node *xmlquery.Node, isCharge bool) (bool, error) {
	if node == nil {
		return true, nil
	}
	root := topAncestor(node)
	headerSettlement := findFirstByPath(root, "CrossIndustryInvoice", "SupplyChainTradeTransaction", "ApplicableHeaderTradeSettlement")
	if headerSettlement == nil {
		headerSettlement = findFirstByPath(root, "SupplyChainTradeTransaction", "ApplicableHeaderTradeSettlement")
	}
	if headerSettlement == nil {
		return true, nil
	}
	indicator := "false"
	totalField := "AllowanceTotalAmount"
	if isCharge {
		indicator = "true"
		totalField = "ChargeTotalAmount"
	}
	items := []*xmlquery.Node{}
	for c := headerSettlement.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xmlquery.ElementNode || c.Data != "SpecifiedTradeAllowanceCharge" {
			continue
		}
		if indicatorText(c) != indicator {
			continue
		}
		items = append(items, c)
	}
	// Charges additionally include SpecifiedLogisticsServiceCharge
	// (BT-X-272 family) per the BR-FXEXT-CO-12 spec wording, summed
	// alongside the allowance/charge items at the same two-decimal
	// rounding precision and counted toward the tolerance.
	var logisticsItems []*xmlquery.Node
	if isCharge {
		for c := headerSettlement.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xmlquery.ElementNode && c.Data == "SpecifiedLogisticsServiceCharge" {
				logisticsItems = append(logisticsItems, c)
			}
		}
	}
	total, ok := floatChild(node, totalField)
	if len(items) == 0 && len(logisticsItems) == 0 && !ok {
		// Schematron OR branch: no items and no total → pass.
		return true, nil
	}
	var sumItems float64
	for _, it := range items {
		v, _ := floatChild(it, "ActualAmount")
		sumItems += v
	}
	sumItemsR := math.Round(sumItems*100) / 100
	var sumLogistics float64
	for _, it := range logisticsItems {
		v, _ := floatChild(it, "AppliedAmount")
		sumLogistics += v
	}
	sumLogisticsR := math.Round(sumLogistics*100) / 100
	expected := sumItemsR + sumLogisticsR
	tolerance := 0.01 * float64(len(items)+len(logisticsItems))
	if !ok {
		// Total missing but items present: spec compares against
		// the calculated sum on its own; rule passes when the sum is
		// within tolerance of zero.
		return math.Abs(0-expected) <= tolerance, nil
	}
	return math.Abs(total-expected) <= tolerance, nil
}

// brFXEXTCO10LineTotal implements FX-SCH-A-000365 (BR-FXEXT-CO-10):
// |LineTotalAmount - Σ line LineTotalAmount| <= 0.01 ×
// number-of-line-items. Context node is the header monetary
// summation; the sum walks the IncludedSupplyChainTradeLineItem
// entries whose AssociatedDocumentLineDocument/LineStatusReasonCode
// is absent or equals "DETAIL".
func brFXEXTCO10LineTotal(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil {
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
	totalAmount, ok := floatChild(node, "LineTotalAmount")
	if !ok {
		return true, nil
	}
	var sum float64
	var lineCount int
	for c := tx.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xmlquery.ElementNode || c.Data != "IncludedSupplyChainTradeLineItem" {
			continue
		}
		lineCount++
		if !isDetailLine(c) {
			continue
		}
		settlement := findChild(c, "SpecifiedLineTradeSettlement")
		if settlement == nil {
			continue
		}
		monetary := findChild(settlement, "SpecifiedTradeSettlementLineMonetarySummation")
		if monetary == nil {
			continue
		}
		v, _ := floatChild(monetary, "LineTotalAmount")
		sum += v
	}
	sumRounded := math.Round(sum*100) / 100
	tolerance := 0.01 * float64(lineCount)
	return math.Abs(totalAmount-sumRounded) <= tolerance, nil
}

// brFXEXTCO13TaxBasis implements FX-SCH-A-000366 (BR-FXEXT-CO-13):
// |TaxBasisTotalAmount - Σ line - Σ allowance + Σ charge| <=
// 0.01 × (lineCount + allowanceCount + chargeCount). Context node
// is the header monetary summation.
func brFXEXTCO13TaxBasis(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil {
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
	headerSettlement := findFirstByPath(tx, "ApplicableHeaderTradeSettlement")
	if headerSettlement == nil {
		return true, nil
	}
	taxBasis, ok := floatChild(node, "TaxBasisTotalAmount")
	if !ok {
		return true, nil
	}
	var lineSum, allowanceSum, chargeSum float64
	var lineCount, allowanceCount, chargeCount int
	for c := tx.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xmlquery.ElementNode || c.Data != "IncludedSupplyChainTradeLineItem" {
			continue
		}
		lineCount++
		if !isDetailLine(c) {
			continue
		}
		settlement := findChild(c, "SpecifiedLineTradeSettlement")
		if settlement == nil {
			continue
		}
		monetary := findChild(settlement, "SpecifiedTradeSettlementLineMonetarySummation")
		if monetary == nil {
			continue
		}
		v, _ := floatChild(monetary, "LineTotalAmount")
		lineSum += v
	}
	for c := headerSettlement.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xmlquery.ElementNode || c.Data != "SpecifiedTradeAllowanceCharge" {
			continue
		}
		v, _ := floatChild(c, "ActualAmount")
		if indicatorText(c) == "true" {
			chargeCount++
			chargeSum += v
		} else {
			allowanceCount++
			allowanceSum += v
		}
	}
	// Logistic service charges count as charges too.
	for c := headerSettlement.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xmlquery.ElementNode || c.Data != "SpecifiedLogisticsServiceCharge" {
			continue
		}
		v, _ := floatChild(c, "AppliedAmount")
		chargeCount++
		chargeSum += v
	}
	lineSumR := math.Round(lineSum*100) / 100
	allowanceR := math.Round(allowanceSum*100) / 100
	chargeR := math.Round(chargeSum*100) / 100
	expected := lineSumR - allowanceR + chargeR
	tolerance := 0.01 * float64(lineCount+allowanceCount+chargeCount)
	return math.Abs(taxBasis-expected) <= tolerance, nil
}

// brFXEXTS09StandardVAT implements FX-SCH-A-000357 (BR-FXEXT-S-09):
// for each VAT breakdown whose CategoryCode is "S", the calculated
// amount must equal basis × rate / 100 within 0.01 ×
// (lineCount + allowanceCount + chargeCount). Context node is the
// per-VAT-breakdown ApplicableTradeTax / CategoryCode[. = 'S'] (the
// extended profile schematron uses the same context selector as
// BR-S-09; the difference is the wider tolerance scaling).
func brFXEXTS09StandardVAT(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil {
		return true, nil
	}
	tradeTax := node
	if node.Parent != nil && strings.TrimSpace(node.InnerText()) == "S" {
		tradeTax = node.Parent
	}
	calculated, calcOK := floatChild(tradeTax, "CalculatedAmount")
	rate, rateOK := floatChild(tradeTax, "RateApplicablePercent")
	basis, _ := floatChild(tradeTax, "BasisAmount")
	if !calcOK || !rateOK {
		return true, nil
	}
	expected := math.Round(basis*rate*100) / (100 * 100)
	root := topAncestor(node)
	lineCount, allowanceCount, chargeCount := documentItemCounts(root)
	tolerance := 0.01 * float64(lineCount+allowanceCount+chargeCount)
	if tolerance == 0 {
		tolerance = 0.01
	}
	return math.Abs(calculated-expected) <= tolerance, nil
}

// brFXEXT06ParentLine implements FX-SCH-A-000374 (BR-FXEXT-06): an
// invoice line shall specify the LineStatusReasonCode whenever it
// declares a ParentLineID or is referenced as a parent line by
// another invoice line; otherwise the code may be omitted. Context
// node is the invoice line.
func brFXEXT06ParentLine(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil {
		return true, nil
	}
	parentLineID := strings.TrimSpace(lineParentLineID(node))
	hasCode := lineHasStatusReasonCode(node)
	// First half of the test: "declares ParentLineID and no status code".
	if parentLineID != "" && !hasCode {
		return false, nil
	}
	// Second half: "is referenced as parent by another line and no
	// status code".
	if hasCode {
		return true, nil
	}
	if node.Parent == nil {
		return true, nil
	}
	myLineID := strings.TrimSpace(lineLineID(node))
	if myLineID == "" {
		return true, nil
	}
	for sib := node.Parent.FirstChild; sib != nil; sib = sib.NextSibling {
		if sib.Type != xmlquery.ElementNode || sib.Data != "IncludedSupplyChainTradeLineItem" || sib == node {
			continue
		}
		if strings.TrimSpace(lineParentLineID(sib)) == myLineID {
			return false, nil
		}
	}
	return true, nil
}

// lineParentLineID extracts the ParentLineID associated with the
// invoice line node. The schematron looks at
// ram:AssociatedDocumentLineDocument/ram:ParentLineID.
func lineParentLineID(line *xmlquery.Node) string {
	doc := findChild(line, "AssociatedDocumentLineDocument")
	if doc == nil {
		return ""
	}
	return childText(doc, "ParentLineID")
}

// lineLineID extracts the LineID associated with the invoice line
// node. The schematron looks at
// ram:AssociatedDocumentLineDocument/ram:LineID.
func lineLineID(line *xmlquery.Node) string {
	doc := findChild(line, "AssociatedDocumentLineDocument")
	if doc == nil {
		return ""
	}
	return childText(doc, "LineID")
}

// lineHasStatusReasonCode reports whether the invoice line node
// carries a non-empty LineStatusReasonCode under any of the three
// placements the schematron lists.
func lineHasStatusReasonCode(line *xmlquery.Node) bool {
	if strings.TrimSpace(childText(line, "LineStatusReasonCode")) != "" {
		return true
	}
	if doc := findChild(line, "AssociatedDocumentLineDocument"); doc != nil {
		if strings.TrimSpace(childText(doc, "LineStatusReasonCode")) != "" {
			return true
		}
	}
	if settlement := findChild(line, "SpecifiedLineTradeSettlement"); settlement != nil {
		if strings.TrimSpace(childText(settlement, "LineStatusReasonCode")) != "" {
			return true
		}
	}
	return false
}

// isDetailLine reports whether the invoice line participates in the
// "DETAIL" arithmetic aggregation per the BR-FXEXT rule body: the
// line's LineStatusReasonCode is absent or equals "DETAIL".
func isDetailLine(line *xmlquery.Node) bool {
	doc := findChild(line, "AssociatedDocumentLineDocument")
	if doc == nil {
		return true
	}
	code := strings.TrimSpace(childText(doc, "LineStatusReasonCode"))
	return code == "" || code == "DETAIL"
}

// brFXENO4GermanDelivery implements FX-SCH-A-000354 (BR-FX-EN-04): a
// German invoice (the rule context has already filtered by
// CountryID='DE' on the seller/buyer/invoicee/invoicer triangle) must
// surface either an actual delivery date, a billing period at the
// header, or a per-line billing period. The schematron source uses
// XPath 2.0 string-join() and an every-quantifier over line items
// that antchfx cannot compile.
func brFXENO4GermanDelivery(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil {
		return true, nil
	}
	tx := node
	if node.Data != "SupplyChainTradeTransaction" {
		// Context can be the CrossIndustryInvoice predicate-matched
		// step; descend to SupplyChainTradeTransaction.
		tx = findChild(node, "SupplyChainTradeTransaction")
		if tx == nil {
			tx = node
		}
	}
	delivery := findChild(tx, "ApplicableHeaderTradeDelivery")
	headerSettlement := findChild(tx, "ApplicableHeaderTradeSettlement")
	actualDelivery := false
	if delivery != nil {
		if ev := findChild(delivery, "ActualDeliverySupplyChainEvent"); ev != nil {
			if strings.TrimSpace(childText(ev, "OccurrenceDateTime")) != "" {
				actualDelivery = true
			} else if findChild(ev, "OccurrenceDateTime") != nil {
				actualDelivery = true
			}
		}
	}
	headerBilling := headerSettlement != nil && findChild(headerSettlement, "BillingSpecifiedPeriod") != nil

	// Every-line billing-specified-period: only meaningful when at
	// least one line is present. The schematron uses the XPath 2.0
	// vacuous-true semantics for the empty case (every X in ()
	// satisfies P() = true), so a no-line invoice trivially passes
	// the every branch.
	everyLineHasBilling := true
	lineCount := 0
	for c := tx.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xmlquery.ElementNode || c.Data != "IncludedSupplyChainTradeLineItem" {
			continue
		}
		lineCount++
		settlement := findChild(c, "SpecifiedLineTradeSettlement")
		if settlement == nil || findChild(settlement, "BillingSpecifiedPeriod") == nil {
			everyLineHasBilling = false
		}
	}
	if lineCount == 0 {
		everyLineHasBilling = false
	}

	if !actualDelivery && !headerBilling && !everyLineHasBilling {
		return false, nil
	}
	// Second half: actual delivery is present OR the delivery branch
	// is non-empty (i.e. it carries other content beyond the empty
	// ActualDeliverySupplyChainEvent / a stub).
	if actualDelivery {
		return true, nil
	}
	if delivery == nil {
		return false, nil
	}
	return hasAnyNonEmptyText(delivery), nil
}

// hasAnyNonEmptyText reports whether the element or any of its
// descendants carries non-whitespace text content. The schematron
// expresses this as:
//
//	normalize-space(string-join(.//text(), ''))
func hasAnyNonEmptyText(n *xmlquery.Node) bool {
	if n == nil {
		return false
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type { //nolint:exhaustive // only TextNode and ElementNode carry the content the spec inspects.
		case xmlquery.TextNode:
			if strings.TrimSpace(c.Data) != "" {
				return true
			}
		case xmlquery.ElementNode:
			if hasAnyNonEmptyText(c) {
				return true
			}
			for _, a := range c.Attr {
				if strings.TrimSpace(a.Value) != "" {
					return true
				}
			}
		}
	}
	return false
}

// ciiSR119GrossPriceConsistency implements CII-SR-119: when an
// invoice line carries a gross price with an applied
// TradeAllowanceCharge, the charge indicator must be present and
// the actual amount must be supplied; otherwise both fields stay
// out. Context node is the invoice-line element.
func ciiSR119GrossPriceConsistency(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil {
		return true, nil
	}
	gross := findChild(findChild(node, "SpecifiedLineTradeAgreement"), "GrossPriceProductTradePrice")
	if gross == nil {
		return true, nil
	}
	allowance := findChild(gross, "AppliedTradeAllowanceCharge")
	if allowance == nil {
		return true, nil
	}
	indicator := findChild(allowance, "ChargeIndicator")
	actual := findChild(allowance, "ActualAmount")
	// First branch: indicator (with Indicator=false()) and actual.
	if indicator != nil && actual != nil {
		ind := strings.TrimSpace(childText(indicator, "Indicator"))
		if ind == "false" {
			return true, nil
		}
	}
	// Second branch: indicator absent and actual absent.
	if indicator == nil && actual == nil {
		return true, nil
	}
	return false, nil
}

// documentItemCounts returns the line / allowance / charge counts
// the BR-FXEXT-S-09 tolerance depends on. Lines walk
// IncludedSupplyChainTradeLineItem, allowances / charges walk
// SpecifiedTradeAllowanceCharge under the header settlement.
func documentItemCounts(root *xmlquery.Node) (line, allowance, charge int) {
	tx := findFirstByPath(root, "CrossIndustryInvoice", "SupplyChainTradeTransaction")
	if tx == nil {
		tx = findFirstByPath(root, "SupplyChainTradeTransaction")
	}
	if tx == nil {
		return
	}
	for c := tx.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xmlquery.ElementNode && c.Data == "IncludedSupplyChainTradeLineItem" && isDetailLine(c) {
			line++
		}
	}
	headerSettlement := findFirstByPath(tx, "ApplicableHeaderTradeSettlement")
	if headerSettlement == nil {
		return
	}
	for c := headerSettlement.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xmlquery.ElementNode || c.Data != "SpecifiedTradeAllowanceCharge" {
			continue
		}
		if indicatorText(c) == "true" {
			charge++
		} else {
			allowance++
		}
	}
	for c := headerSettlement.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xmlquery.ElementNode && c.Data == "SpecifiedLogisticsServiceCharge" {
			charge++
		}
	}
	return
}

// indicatorText returns the text content of a
// SpecifiedTradeAllowanceCharge's ChargeIndicator/Indicator value,
// trimmed and lower-cased to match the boolean literals the
// schematron compares against ('true' / 'false').
func indicatorText(charge *xmlquery.Node) string {
	if ind := findChild(charge, "ChargeIndicator"); ind != nil {
		return strings.ToLower(strings.TrimSpace(childText(ind, "Indicator")))
	}
	return ""
}

// floatChild reads the first child element with the given local
// name as a float64. Returns ok=false when the element is missing
// or its text content does not parse as a number; callers then
// short-circuit to the spec's existence-aware fallback branch
// rather than treating the absence as zero.
func floatChild(n *xmlquery.Node, name string) (float64, bool) {
	c := findChild(n, name)
	if c == nil {
		return 0, false
	}
	s := strings.TrimSpace(c.InnerText())
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
