package compliance

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// --- 9. OC config names ---

func checkOCConfigNames(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	rule := ocConfigNameRule(profile)

	ocProps, err := document.ResolveDict(ctx, doc, catalog["OCProperties"])
	if err != nil || ocProps == nil {
		return nil
	}

	var results []Violation

	// Check default config (/D).
	dObj, ok := ocProps["D"]
	if ok {
		dConfig, err := document.ResolveDict(ctx, doc, dObj)
		if err == nil && dConfig != nil {
			if !hasNonEmptyName(ctx, doc, dConfig) {
				results = append(results, Violation{Rule: rule, Context: "default config (D)"})
			}
			// §7.10 t02 (PDF/UA-1): /AS key forbidden in any OC config dict.
			if profile == "PDF/UA-1" {
				if _, hasAS := dConfig["AS"]; hasAS {
					results = append(results, Violation{
						Rule: Rule_14289_1_7_10_t02, Context: "default config (D)",
					})
				}
			}
		}
	}

	// Check /Configs array.
	configsObj, ok := ocProps["Configs"]
	if !ok {
		return results
	}

	configs, err := document.ResolveArray(ctx, doc, configsObj)
	if err != nil {
		return results
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
		// §7.10 t02 (PDF/UA-1): /AS key forbidden.
		if profile == "PDF/UA-1" {
			if _, hasAS := cfg["AS"]; hasAS {
				results = append(results, Violation{
					Rule: Rule_14289_1_7_10_t02, Context: fmt.Sprintf("config %d", i),
				})
			}
		}
	}

	return results
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

// --- 10. PDF/UA identification (PDF/UA-2) ---

func checkPDFUAIdentification(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !isPDFUA(profile) {
		return nil
	}

	xmpData, ok := readCatalogXMP(ctx, doc, catalog)
	if !ok {
		if profile == "PDF/UA-1" {
			return fail(Rule_14289_1_5_t01, "no XMP metadata")
		}
		return fail(Rule_14289_2_5_t01, "no XMP metadata")
	}

	switch profile {
	case "PDF/UA-1":
		if !containsPDFUAIDPart(xmpData, "1") {
			return fail(Rule_14289_1_5_t02, "pdfuaid:part is not 1")
		}
	case "PDF/UA-2":
		if !containsPDFUAIDPart(xmpData, "2") {
			return fail(Rule_14289_2_5_t02, "pdfuaid:part is not 2")
		}
	}

	return nil
}

// checkPDFUAIDPrefixes validates that pdfuaid properties use the correct
// "pdfuaid" namespace prefix and that pdfuaid:rev is a 4-digit year.
func checkPDFUAIDPrefixes(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !isPDFUA(profile) {
		return nil
	}

	xmpData, ok := readCatalogXMP(ctx, doc, catalog)
	if !ok {
		return nil
	}

	// Extract the prefix used for the pdfuaid namespace URI.
	// PDF/UA-2 may use "http://www.aiim.org/pdfua/ns/id/" instead of the
	// PDF/A ID namespace.
	const pdfuaIDNamespace = "http://www.aiim.org/pdfua/ns/id/"
	prefixMap := xmpExtractPrefixMap(xmpData)
	pdfuaidPrefix := prefixMap[xmpPdfaIDNamespace]
	pdfuaPrefix := prefixMap[pdfuaIDNamespace]

	var results []Violation
	raw := string(xmpData)

	if profile == "PDF/UA-1" {
		// t3: "part" property must use "pdfuaid" prefix.
		if !strings.Contains(raw, "pdfuaid:part") {
			// Check if "part" exists under a different prefix.
			if pdfuaidPrefix != "" && pdfuaidPrefix != "pdfuaid" && strings.Contains(raw, pdfuaidPrefix+":part") {
				results = append(results, failf(Rule_14289_1_5_t03,
					"property 'part' uses prefix %q instead of 'pdfuaid'", pdfuaidPrefix)...)
			}
		}
		// t4: "amd" property must use "pdfuaid" prefix.
		if strings.Contains(raw, ":amd") && pdfuaidPrefix != "pdfuaid" {
			// Only flag if "amd" appears with a non-pdfuaid prefix.
			if pdfuaidPrefix != "" && strings.Contains(raw, pdfuaidPrefix+":amd") {
				// The prefix is wrong but the property exists.
			} else if !strings.Contains(raw, "pdfuaid:amd") && strings.Contains(raw, ":amd") {
				results = append(results, failf(Rule_14289_1_5_t04,
					"property 'amd' does not use 'pdfuaid' prefix")...)
			}
		}
		// t5: "corr" property must use "pdfuaid" prefix.
		if strings.Contains(raw, ":corr") && pdfuaidPrefix != "pdfuaid" {
			if !strings.Contains(raw, "pdfuaid:corr") && strings.Contains(raw, ":corr") {
				results = append(results, failf(Rule_14289_1_5_t05,
					"property 'corr' does not use 'pdfuaid' prefix")...)
			}
		}
	}

	if profile == "PDF/UA-2" {
		// t4: "rev" property must use "pdfuaid" prefix.
		// Check both pdfa and pdfua namespace URIs.
		effectivePrefix := pdfuaidPrefix
		if effectivePrefix == "" {
			effectivePrefix = pdfuaPrefix
		}
		if effectivePrefix != "" && effectivePrefix != "pdfuaid" {
			if strings.Contains(raw, effectivePrefix+":rev") {
				results = append(results, failf(Rule_14289_2_5_t04,
					"property 'rev' uses prefix %q instead of 'pdfuaid'", effectivePrefix)...)
			}
		}
		// t5: pdfuaid:rev must be a 4-digit year.
		revValue := extractXMPPropertyValue(raw, "pdfuaid", "rev")
		if revValue == "" && effectivePrefix != "" {
			revValue = extractXMPPropertyValue(raw, effectivePrefix, "rev")
		}
		if revValue != "" && !isFourDigitYear(revValue) {
			results = append(results, failf(Rule_14289_2_5_t05,
				"pdfuaid:rev value %q is not a 4-digit year", revValue)...)
		}
	}

	return results
}

// extractXMPPropertyValue extracts the value of prefix:localName from raw XMP.
// Handles both attribute form (prefix:name="value") and element form (<prefix:name>value</prefix:name>).
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

func isFourDigitYear(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
