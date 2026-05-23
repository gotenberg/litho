package cii_schematron

import (
	"context"
	"regexp"
	"strings"

	"github.com/antchfx/xmlquery"

	"github.com/gotenberg/litho/pkg/internal/schematron"
)

// BuiltinRuleOverrides returns Go evaluators for Schematron rules
// whose XPath the bundled engine either cannot compile or evaluates
// wrongly. Callers pass the map to [schematron.WithRuleOverrides]
// so each listed rule fires through the Go implementation instead
// of landing in the skipped slice as an advisory.
//
// Current overrides:
//
//   - BR-DE-19: SEPA IBAN format + ISO 7064 mod-97 checksum. The
//     XRechnung source mixes for/return, if/then/else,
//     string-to-codepoints, and xs:integer; antchfx/xpath supports
//     none of those.
//   - BR-CO-18: existence of one ApplicableTradeTax under the
//     header. antchfx evaluates "//foo" relative to the context
//     node rather than the document root, so the rule wrongly
//     skips the header tax when fired from a deep line context.
//   - BR-S-02: existence of one Seller VAT identifier (or tax
//     representative) when a line uses VAT category S. Same
//     antchfx "//foo from non-root context" issue.
//   - FX-SCH-A-000052 / BR-CO-17 + FX-SCH-A-000057 / BR-S-09:
//     VAT category tax-amount tolerance arithmetic. The source
//     mixes abs(), xs:decimal-cast path steps, and a sentinel
//     "round(rate) = 0" branch; the engine cannot generally
//     compile abs(), so the rules silently land in the skipped
//     slice without a Go port.
//   - FX-SCH-A-000304 / BR-FXEXT-CO-11 +
//     FX-SCH-A-000305 / BR-FXEXT-CO-12: document-level
//     allowance/charge total tolerance.
//   - FX-SCH-A-000365 / BR-FXEXT-CO-10: line-net-amount sum
//     tolerance.
//   - FX-SCH-A-000366 / BR-FXEXT-CO-13: tax-basis arithmetic
//     tolerance.
//   - FX-SCH-A-000357 / BR-FXEXT-S-09: standard-rated VAT amount
//     with wider tolerance.
//   - FX-SCH-A-000374 / BR-FXEXT-06: line ParentLineID /
//     LineStatusReasonCode coherence.
//   - FX-SCH-A-000354 / BR-FX-EN-04: German invoice delivery
//     date / billing period requirement.
//   - CII-SR-119: gross price AppliedTradeAllowanceCharge
//     consistency.
//
// The returned map is fresh on each call so callers can mutate or
// extend it without disturbing the package's internal state.
func BuiltinRuleOverrides() map[string]schematron.RuleOverride {
	return map[string]schematron.RuleOverride{
		"BR-DE-19":        brDE19IBANCheck,
		"BR-CO-18":        brCO18HeaderTaxBreakdown,
		"BR-S-02":         brS02SellerVATIdentifier,
		"FX-SCH-A-000052": brCO17VATCalc,
		"FX-SCH-A-000057": brS09StandardVAT,
		"FX-SCH-A-000304": brFXEXTCO11AllowanceTotal,
		"FX-SCH-A-000305": brFXEXTCO12ChargeTotal,
		"FX-SCH-A-000365": brFXEXTCO10LineTotal,
		"FX-SCH-A-000366": brFXEXTCO13TaxBasis,
		"FX-SCH-A-000357": brFXEXTS09StandardVAT,
		"FX-SCH-A-000374": brFXEXT06ParentLine,
		"FX-SCH-A-000354": brFXENO4GermanDelivery,
		"CII-SR-119":      ciiSR119GrossPriceConsistency,
	}
}

// ibanFormat mirrors XRechnung's regex: two letters, two digits, up
// to thirty alphanumeric characters. The mod-97 check below relies
// on the format pass already happening upstream of this regex.
var ibanFormat = regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[a-zA-Z0-9]{0,30}$`)

// brDE19IBANCheck implements BR-DE-19: when the payment means type
// code is "58" (SEPA credit transfer), the IBAN under
// PayeePartyCreditorFinancialAccount/IBANID must satisfy both the
// format regex above and the ISO 7064 mod-97 = 1 checksum. For any
// other type code the rule passes unconditionally.
//
// Context node is the SpecifiedTradeSettlementPaymentMeans element
// the rule's context selector already filtered to TypeCode in
// ('30','58'); this override only has to look one step deeper.
func brDE19IBANCheck(_ context.Context, node *xmlquery.Node) (bool, error) {
	if node == nil {
		return true, nil
	}
	typeCode := strings.TrimSpace(childText(node, "TypeCode"))
	if typeCode != "58" {
		return true, nil
	}
	account := findChild(node, "PayeePartyCreditorFinancialAccount")
	if account == nil {
		return false, nil
	}
	ibanRaw := strings.TrimSpace(childText(account, "IBANID"))
	iban := stripIBANWhitespace(ibanRaw)
	if !ibanFormat.MatchString(iban) {
		return false, nil
	}
	return ibanChecksumOK(iban), nil
}

// stripIBANWhitespace mirrors the XRechnung replace() call: drop
// every space, tab, CR, LF, and similar whitespace from the IBAN so
// presentational separators do not break the checksum.
func stripIBANWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\r', '\n':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ibanChecksumOK runs ISO 7064 mod-97 over the IBAN as XRechnung
// transcribes it: move the first four characters to the end,
// uppercase the relocated country code, and map each character to a
// numeric value (digit → digit, letter → ord - 55). The numeric
// string then has to satisfy n mod 97 = 1.
//
// The schematron walks codepoints one by one and concatenates string
// fragments; this Go translation does the same arithmetic
// progressively to avoid materialising a >30-digit big integer.
func ibanChecksumOK(iban string) bool {
	if len(iban) < 4 {
		return false
	}
	moved := iban[4:] + strings.ToUpper(iban[:2]) + iban[2:4]
	var rem int
	for _, r := range moved {
		var v int
		switch {
		case r >= '0' && r <= '9':
			v = int(r - '0')
		case r >= 'A' && r <= 'Z':
			v = int(r-'A') + 10
		case r >= 'a' && r <= 'z':
			v = int(r-'a') + 10
		default:
			return false
		}
		if v >= 10 {
			rem = (rem*100 + v) % 97
		} else {
			rem = (rem*10 + v) % 97
		}
	}
	return rem == 1
}

// brCO18HeaderTaxBreakdown checks that the document carries at
// least one ApplicableTradeTax under
// SupplyChainTradeTransaction/ApplicableHeaderTradeSettlement.
// The Schematron source expresses this as an absolute "//" path
// but antchfx evaluates "//" relative to the context node rather
// than the document root, so the rule misses the header tax when
// the rule fires on a deep line-item context.
func brCO18HeaderTaxBreakdown(_ context.Context, node *xmlquery.Node) (bool, error) {
	root := topAncestor(node)
	cii := findFirstByPath(root, "CrossIndustryInvoice", "SupplyChainTradeTransaction", "ApplicableHeaderTradeSettlement", "ApplicableTradeTax")
	if cii == nil {
		// Try alternate root path where the first descendant of the
		// document node is CrossIndustryInvoice already.
		cii = findFirstByPath(root, "SupplyChainTradeTransaction", "ApplicableHeaderTradeSettlement", "ApplicableTradeTax")
	}
	return cii != nil, nil
}

// brS02SellerVATIdentifier checks that the document carries a
// Seller VAT identifier (BT-31), a Seller tax registration
// identifier (BT-32), or a Seller tax representative VAT identifier
// (BT-63). Required whenever an invoice line uses VAT category "S"
// (Standard rated). Same antchfx absolute-path workaround applies.
func brS02SellerVATIdentifier(_ context.Context, node *xmlquery.Node) (bool, error) {
	root := topAncestor(node)
	agreement := findFirstByPath(root, "CrossIndustryInvoice", "SupplyChainTradeTransaction", "ApplicableHeaderTradeAgreement")
	if agreement == nil {
		agreement = findFirstByPath(root, "SupplyChainTradeTransaction", "ApplicableHeaderTradeAgreement")
	}
	if agreement == nil {
		return false, nil
	}
	if seller := findChild(agreement, "SellerTradeParty"); seller != nil {
		for reg := seller.FirstChild; reg != nil; reg = reg.NextSibling {
			if reg.Type != xmlquery.ElementNode || reg.Data != "SpecifiedTaxRegistration" {
				continue
			}
			if id := findChild(reg, "ID"); id != nil {
				switch attrValue(id, "schemeID") {
				case "VA", "FC":
					return true, nil
				}
			}
		}
	}
	if rep := findChild(agreement, "SellerTaxRepresentativeTradeParty"); rep != nil {
		for reg := rep.FirstChild; reg != nil; reg = reg.NextSibling {
			if reg.Type != xmlquery.ElementNode || reg.Data != "SpecifiedTaxRegistration" {
				continue
			}
			if id := findChild(reg, "ID"); id != nil {
				if attrValue(id, "schemeID") == "VA" {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// topAncestor walks parent pointers to the topmost node, mirroring
// the schematron evaluator's documentRoot helper but private to
// this package.
func topAncestor(n *xmlquery.Node) *xmlquery.Node {
	for n != nil && n.Parent != nil {
		n = n.Parent
	}
	return n
}

// findFirstByPath walks a sequence of element names from n, taking
// the first child with each local name. Returns the deepest matched
// element, or nil if any step fails.
func findFirstByPath(n *xmlquery.Node, names ...string) *xmlquery.Node {
	cur := n
	for _, name := range names {
		if cur == nil {
			return nil
		}
		// Document nodes have no Data; descend into the first element
		// child until we land on something with the requested name.
		next := findChild(cur, name)
		if next == nil {
			// Walk through wrapper/document children that aren't
			// the desired name to reach the first matching element.
			for c := cur.FirstChild; c != nil && next == nil; c = c.NextSibling {
				if c.Type == xmlquery.ElementNode && c.Data == name {
					next = c
				}
			}
		}
		cur = next
	}
	return cur
}

// attrValue returns the value of n's named attribute, or empty
// string when the attribute is absent.
func attrValue(n *xmlquery.Node, name string) string {
	for _, a := range n.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// childText returns the trimmed text content of the first direct
// child of n whose local name matches name. Empty string when
// nothing matches. Nil-safe via [findChild].
func childText(n *xmlquery.Node, name string) string {
	c := findChild(n, name)
	if c == nil {
		return ""
	}
	return c.InnerText()
}

// findChild walks n's element children and returns the first whose
// local name matches name (namespace-agnostic; the CII schema keeps
// local names unambiguous). Nil-safe: returns nil when n is nil so
// callers can chain findChild(findChild(...), ...) without an
// intermediate nil check.
func findChild(n *xmlquery.Node, name string) *xmlquery.Node {
	if n == nil {
		return nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xmlquery.ElementNode {
			continue
		}
		if c.Data == name {
			return c
		}
	}
	return nil
}
