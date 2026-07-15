package compliance

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// PDFUA2ExpectedRev is the literal value pdfuaid:rev must carry for a
// PDF/UA-2 file. ISO 14289-2 §5 fixes the value at "2024"; any other
// year-shaped string violates the spec even if it parses as a valid
// year. Lift the constant when ISO 14289-2 ships a revision. It is
// exported so the remediation fixer emits exactly the value the validator
// requires, leaving no room for the two to drift.
const PDFUA2ExpectedRev = "2024"

// pdfua2ExpectedRev is the package-internal alias for [PDFUA2ExpectedRev].
const pdfua2ExpectedRev = PDFUA2ExpectedRev

func checkOCConfigNames(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	rule := ocConfigNameRule(profile)

	ocProps, err := document.ResolveDict(ctx, doc, catalog["OCProperties"])
	if err != nil || ocProps == nil {
		return nil
	}

	var results []Violation

	// ISO 14289-2 §8.7 t1 requires a /Name on every OC config dict (including the
	// default /D) ONLY when /OCProperties has a /Configs entry with at least one
	// config dict; a UA-2 document with only /D and no /Configs passes. ISO
	// 14289-1 §7.10 t1 has no such condition: UA-1 requires /Name on /D
	// unconditionally. The /AS prohibition (t2) applies always.
	configs, _ := document.ResolveArray(ctx, doc, ocProps["Configs"])
	nameRequired := profile != "PDF/UA-2" || len(configs) >= 1

	// Check default config (/D).
	dObj, ok := ocProps["D"]
	if ok {
		dConfig, err := document.ResolveDict(ctx, doc, dObj)
		if err == nil && dConfig != nil {
			if nameRequired && !hasNonEmptyName(ctx, doc, dConfig) {
				results = append(results, Violation{Rule: rule, Context: "default config (D)"})
			}
			// §7.10 t2 (PDF/UA-1) / §8.7 t2 (PDF/UA-2): /AS key forbidden in any
			// OC config dict.
			if _, hasAS := dConfig["AS"]; hasAS {
				results = append(results, Violation{
					Rule: asForbiddenRule(profile), Context: "default config (D)",
				})
			}
		}
	}

	for i, cfgObj := range configs {
		if ctx.Err() != nil {
			return results
		}

		cfg, err := document.ResolveDict(ctx, doc, cfgObj)
		if err != nil || cfg == nil {
			continue
		}

		if !hasNonEmptyName(ctx, doc, cfg) {
			results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("config %d", i)})
		}
		// §7.10 t2 (PDF/UA-1) / §8.7 t2 (PDF/UA-2): /AS key forbidden.
		if _, hasAS := cfg["AS"]; hasAS {
			results = append(results, Violation{
				Rule: asForbiddenRule(profile), Context: fmt.Sprintf("config %d", i),
			})
		}
	}

	return results
}

// checkMetadataTypeSubtype validates ISO 14289-1 §7.1 t8 and ISO 14289-2
// §8.11.1 t2: the catalog /Metadata entry shall be a metadata stream whose
// dictionary has /Type Metadata and /Subtype XML. Both name values must be
// correct for the entry to conform.
func checkMetadataTypeSubtype(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !IsPDFUA(profile) {
		return nil
	}
	if stream, err := document.ResolveStream(ctx, doc, catalog["Metadata"]); err == nil {
		typeName, _ := document.ResolveName(ctx, doc, stream.Dict["Type"])
		subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
		if typeName == "Metadata" && subtype == "XML" {
			return nil
		}
	}
	return fail(metadataTypeSubtypeRule(profile),
		"catalog /Metadata is missing or lacks /Type Metadata and /Subtype XML")
}

// metadataTypeSubtypeRule selects the catalog /Metadata stream Type/Subtype rule
// by UA profile: ISO 14289-1 §7.1 t8 and ISO 14289-2 §8.11.1 t2.
func metadataTypeSubtypeRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_11_1_t02
	}
	return Rule_14289_1_7_1_t08
}

// asForbiddenRule selects the "/AS forbidden in an OC configuration dict" rule
// by UA profile: ISO 14289-1 §7.10 t2 and ISO 14289-2 §8.7 t2.
func asForbiddenRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_7_t02
	}
	return Rule_14289_1_7_10_t02
}

// hasNonEmptyName checks if a dict has a non-empty /Name string entry.
func hasNonEmptyName(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	nameObj, ok := dict["Name"]
	if !ok {
		return false
	}
	nameStr, err := document.ResolveString(ctx, doc, nameObj)
	if err != nil {
		return false
	}
	return string(nameStr) != ""
}

func checkPDFUAIdentification(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !IsPDFUA(profile) {
		return nil
	}

	xmpData, ok := readCatalogXMP(ctx, doc, catalog)
	if !ok {
		// No XMP metadata at all: veraPDF scopes the §5 identifier rule to the
		// XMP package object, so with no package it does not fire. The missing
		// metadata itself is reported by the §7.1 metadata-presence check, so
		// firing the identifier rule here over-reports (verapdf-corpus
		// 7.1-t08-fail-a fires only 7.1 t8).
		return nil
	}

	return checkPDFUAIdentificationIn(ctx, xmpData, profile)
}

// checkPDFUAIdentificationIn is the pure-logic core of
// [checkPDFUAIdentification] exposed for unit testing. It assumes the XMP
// packet is present (the no-XMP case is handled by the I/O-bound wrapper) and
// reports the PDF/UA identification-schema violations for the given profile.
func checkPDFUAIdentificationIn(ctx context.Context, xmpData []byte, profile string) []Violation {
	switch profile {
	case "PDF/UA-1":
		// ISO 14289-1 §5 splits presence (t1) from value correctness
		// (t2): a packet with no pdfuaid:part at all fails t1, the
		// missing-identifier rule, not t2, which targets a wrong value.
		if !containsPDFUAIDProperty(ctx, xmpData) {
			return fail(Rule_14289_1_5_t01, "pdfuaid:part missing from XMP")
		}
		if !containsPDFUAIDPart(ctx, xmpData, "1") {
			return fail(Rule_14289_1_5_t02, "pdfuaid:part is not 1")
		}
	case "PDF/UA-2":
		if !containsPDFUAIDProperty(ctx, xmpData) {
			return fail(Rule_14289_2_5_t01, "pdfuaid:part missing from XMP")
		}
		if !containsPDFUAIDPart(ctx, xmpData, "2") {
			return fail(Rule_14289_2_5_t02, "pdfuaid:part is not 2")
		}
		// ISO 14289-2 §5 also requires pdfuaid:rev=2024. A present-but-wrong
		// value is reported by checkPDFUAIDPrefixes (t05); detect the missing
		// case here, where pdfuaid:part=2 confirms the file claims UA-2.
		if !containsPDFUAIDRev(ctx, xmpData) {
			return fail(Rule_14289_2_5_t05, "pdfuaid:rev missing")
		}
	}

	return nil
}

// checkPDFUAIDPrefixes validates that pdfuaid properties use the correct
// "pdfuaid" namespace prefix and that pdfuaid:rev is a 4-digit year. The
// checks operate only on the PDF/UA Identification namespace
// (http://www.aiim.org/pdfua/ns/id/); identically named properties from
// the PDF/A Identification namespace (pdfaid:part, pdfaid:rev, etc.) are
// unrelated and must not trigger these rules.
func checkPDFUAIDPrefixes(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !IsPDFUA(profile) {
		return nil
	}
	xmpData, ok := readCatalogXMP(ctx, doc, catalog)
	if !ok {
		return nil
	}
	return checkPDFUAIDPrefixesIn(ctx, xmpData, profile)
}

// checkPDFUAIDPrefixesIn is the pure-logic core of [checkPDFUAIDPrefixes]
// exposed for unit testing. It takes the decoded XMP bytes and the
// profile name and returns the same violations the I/O-bound wrapper
// would.
func checkPDFUAIDPrefixesIn(ctx context.Context, xmpData []byte, profile string) []Violation {
	const pdfuaIDNamespace = "http://www.aiim.org/pdfua/ns/id/"
	// Enumerate every prefix bound to the PDF/UA Identification
	// namespace URI. XMP permits a URI to be aliased to several prefixes
	// (seen in the wild as dual bindings like pdfuaid + pdfuaia → UA
	// URI). A single-value lookup would return only one of them and
	// miss the "wrong-prefix" violation when the document uses a
	// non-canonical alias for the target property.
	uaPrefixes := xmpExtractAllPrefixesForURI(ctx, xmpData, pdfuaIDNamespace)

	var results []Violation
	raw := string(xmpData)

	// wrongPrefix reports whether any prefix bound to the PDF/UA URI,
	// other than the canonical "pdfuaid", carries the named property.
	// Returns the offending prefix on the first match.
	wrongPrefix := func(property string) (string, bool) {
		for _, p := range uaPrefixes {
			if p == "pdfuaid" {
				continue
			}
			if strings.Contains(raw, p+":"+property) {
				return p, true
			}
		}
		return "", false
	}

	if profile == "PDF/UA-1" {
		if prefix, bad := wrongPrefix("part"); bad && !strings.Contains(raw, "pdfuaid:part") {
			results = append(results, failf(Rule_14289_1_5_t03,
				"property 'part' uses prefix %q instead of 'pdfuaid'", prefix)...)
		}
		if prefix, bad := wrongPrefix("amd"); bad && !strings.Contains(raw, "pdfuaid:amd") {
			results = append(results, failf(Rule_14289_1_5_t04,
				"property 'amd' uses prefix %q instead of 'pdfuaid'", prefix)...)
		}
		if prefix, bad := wrongPrefix("corr"); bad && !strings.Contains(raw, "pdfuaid:corr") {
			results = append(results, failf(Rule_14289_1_5_t05,
				"property 'corr' uses prefix %q instead of 'pdfuaid'", prefix)...)
		}
	}

	if profile == "PDF/UA-2" {
		if prefix, bad := wrongPrefix("part"); bad && !strings.Contains(raw, "pdfuaid:part") {
			results = append(results, failf(Rule_14289_2_5_t03,
				"property 'part' uses prefix %q instead of 'pdfuaid'", prefix)...)
		}
		if prefix, bad := wrongPrefix("rev"); bad && !strings.Contains(raw, "pdfuaid:rev") {
			results = append(results, failf(Rule_14289_2_5_t04,
				"property 'rev' uses prefix %q instead of 'pdfuaid'", prefix)...)
		}
		// t5: pdfuaid:rev must be the literal value pdfua2ExpectedRev.
		// Read the value from the canonical prefix first, then from
		// any other prefix bound to the PDF/UA URI. The PDF/A
		// pdfaid:rev (under a different namespace URI) must never be
		// consulted.
		revValue := extractXMPPropertyValue(raw, "pdfuaid", "rev")
		if revValue == "" {
			for _, p := range uaPrefixes {
				if p == "pdfuaid" {
					continue
				}
				revValue = extractXMPPropertyValue(raw, p, "rev")
				if revValue != "" {
					break
				}
			}
		}
		if revValue != "" && revValue != pdfua2ExpectedRev {
			results = append(results, failf(Rule_14289_2_5_t05,
				"pdfuaid:rev=%q must be %q", revValue, pdfua2ExpectedRev)...)
		}
	}

	return results
}

// extractXMPPropertyValue extracts the value of prefix:localName from raw
// XMP. Handles both attribute form (prefix:name="value") and element
// form (<prefix:name>value</prefix:name>).
//
// The function is namespace-blind: it matches a literal prefix:localName
// substring. Callers must verify that prefix is bound to the intended
// namespace URI (via [xmpExtractPrefixMap] or equivalent) before calling,
// or they risk confusing distinct properties that share a local name
// across namespaces. For example pdfaid:rev (PDF/A) and pdfuaid:rev
// (PDF/UA) both look like ":rev" under different URIs.
func extractXMPPropertyValue(raw, prefix, localName string) string {
	key := prefix + ":" + localName
	// Attribute form: key="value"
	if idx := strings.Index(raw, key+"=\""); idx >= 0 {
		start := idx + len(key) + 2
		end := strings.Index(raw[start:], "\"")
		if end >= 0 {
			return raw[start : start+end]
		}
	}
	// Element form: <key>value</key>
	openTag := "<" + key + ">"
	closeTag := "</" + key + ">"
	if idx := strings.Index(raw, openTag); idx >= 0 {
		start := idx + len(openTag)
		end := strings.Index(raw[start:], closeTag)
		if end >= 0 {
			return strings.TrimSpace(raw[start : start+end])
		}
	}
	return ""
}
