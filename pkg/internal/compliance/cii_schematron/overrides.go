package cii_schematron

import (
	"context"
	"regexp"
	"strings"

	"github.com/antchfx/xmlquery"

	"github.com/gotenberg/litho/pkg/internal/schematron"
)

// BuiltinRuleOverrides returns Go evaluators for Schematron rules
// whose XPath the bundled engine cannot compile. Callers pass the
// map to [schematron.WithRuleOverrides] so each listed rule fires
// through the Go implementation instead of landing in the skipped
// slice as an advisory.
//
// Current overrides:
//
//   - BR-DE-19: SEPA IBAN format + ISO 7064 mod-97 checksum. The
//     XRechnung source mixes for/return, if/then/else,
//     string-to-codepoints, and xs:integer; antchfx/xpath supports
//     none of those.
//
// The returned map is fresh on each call so callers can mutate or
// extend it without disturbing the package's internal state.
func BuiltinRuleOverrides() map[string]schematron.RuleOverride {
	return map[string]schematron.RuleOverride{
		"BR-DE-19": brDE19IBANCheck,
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

// childText returns the trimmed text content of the first direct
// child of n whose local name matches name. Empty string when
// nothing matches.
func childText(n *xmlquery.Node, name string) string {
	c := findChild(n, name)
	if c == nil {
		return ""
	}
	return c.InnerText()
}

// findChild walks n's element children and returns the first whose
// local name matches name (namespace-agnostic; the CII schema keeps
// local names unambiguous).
func findChild(n *xmlquery.Node, name string) *xmlquery.Node {
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
