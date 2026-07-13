package compliance

import (
	"bytes"
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func objHeaderRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_8_t01, Rule_19005_2_6_1_9_t01, Rule_19005_3_6_1_9_t01, Rule_19005_4_6_1_8_t01)
}

func hexStringRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_6_t01, Rule_19005_2_6_1_6_t01, Rule_19005_3_6_1_6_t01, Rule_19005_4_6_1_5_t01)
}

func hexStringInvalidCharRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_6_t02, Rule_19005_2_6_1_6_t02, Rule_19005_3_6_1_6_t02, Rule_19005_4_6_1_5_t02)
}

func xrefStreamForbiddenRule(_ string) Rule {
	return Rule_19005_1_6_1_4_t03
}

// xrefFormatRule returns the rule for xref table format violations.
func xrefFormatRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_4_t01, Rule_19005_2_6_1_4_t02, Rule_19005_3_6_1_4_t02, Rule_19005_4_6_1_4_t01)
}

// streamFormatRule returns the rule for stream format violations
// (/Length mismatch, EOL after stream keyword, indirect /Length). It mirrors
// [streamLengthRule]: parts 2/3 attribute these to §6.1.7.1 t1 (CosStream), not
// §6.1.8 (which is the CosUnicodeName UTF-8 rule). Attributing a /Length mismatch
// to §6.1.8 produced a spurious UTF-8-name violation alongside the correct
// §6.1.7.1 one.
func streamFormatRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_7_t01, Rule_19005_2_6_1_7_1_t01, Rule_19005_3_6_1_7_1_t01, Rule_19005_4_6_1_6_1_t01)
}

// checkContentStreamHexStrings parses page content streams with format
// checking enabled to detect hex string violations: odd digit count
// (§6.1.6 t1) and non-hex characters (§6.1.6 t2).
func checkContentStreamHexStrings(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	oddRule := hexStringRule(profile)
	if oddRule.Specification == "" {
		return nil
	}
	charRule := hexStringInvalidCharRule(profile)

	pages := collectPages(ctx, doc, catalog)
	var results []Violation

	for i, page := range pages {
		if ctx.Err() != nil {
			return results
		}
		streamData, err := resolvePageContentBytes(ctx, doc, page)
		if err != nil || len(streamData) == 0 {
			continue
		}

		_, violations := content.ParseStreamDataFormatChecked(ctx, streamData)
		for _, v := range violations {
			msg := v.Error()
			rule := oddRule
			if bytes.Contains([]byte(msg), []byte("invalid character")) {
				rule = charRule
			}
			results = append(results, Violation{
				Rule:    rule,
				Context: fmt.Sprintf("page %d: %s", i+1, msg),
			})
			break // one violation per page is sufficient
		}
	}
	return results
}

// checkFormatViolations checks parser-level format violations collected during
// resolveAllObjects. These include hex string format issues (§6.1.6) and
// object header format issues (§6.1.8/§6.1.9) detected by the parser when
// ComplianceMode is enabled.
func checkFormatViolations(doc *document.Document, profile string) []Violation {
	if len(doc.FormatViolations) == 0 {
		return nil
	}

	var results []Violation
	hexRule := hexStringRule(profile)
	objRule := objHeaderRule(profile)
	streamRule := streamFormatRule(profile)
	xrefRule := xrefFormatRule(profile)

	for _, violation := range doc.FormatViolations {
		msg := violation.Error()

		rule := hexRule
		//nolint:gocritic // if-else chain uses function-call predicates; switch is not applicable.
		if containsXRefPattern(msg) {
			rule = xrefRule
			// PDF/A-1 distinguishes t01 (spacing) from t02 (EOL).
			if PDFAPart(profile) == 1 && isXrefEOLViolation(msg) {
				rule = Rule_19005_1_6_1_4_t02
			}
		} else if containsStreamPattern(msg) {
			rule = streamRule
			// PDF/A-1/2/3 attribute a stream-keyword EOL defect to a dedicated
			// rule (t02), distinct from the /Length-mismatch rule (t01).
			if eolRule := streamEOLRule(profile); eolRule.Specification != "" && isStreamEOLViolation(msg) {
				rule = eolRule
			}
		} else if containsObjHeaderPattern(msg) {
			rule = objRule
		} else if bytes.Contains([]byte(msg), []byte("invalid character")) {
			rule = hexStringInvalidCharRule(profile)
		}

		results = append(results, Violation{
			Rule:    rule,
			Context: msg,
		})
	}

	return results
}

// containsXRefPattern returns true if the error message relates to xref
// table formatting (subsection header separator, EOL after xref keyword).
func containsXRefPattern(msg string) bool {
	return bytes.Contains([]byte(msg), []byte("xref"))
}

// containsStreamPattern returns true if the error message relates to
// stream formatting (Length mismatch, EOL after stream keyword, indirect Length).
func containsStreamPattern(msg string) bool {
	return bytes.Contains([]byte(msg), []byte("stream"))
}

// streamEOLRule returns the "stream keyword not followed by a valid EOL" rule
// for PDF/A-1 (§6.1.7 t2) and PDF/A-2/3 (§6.1.7.1 t2). It returns the zero Rule
// for other parts, which keep the /Length-mismatch attribution.
func streamEOLRule(profile string) Rule {
	switch PDFAPart(profile) {
	case 1:
		return Rule_19005_1_6_1_7_t02
	case 2:
		return Rule_19005_2_6_1_7_1_t02
	case 3:
		return Rule_19005_3_6_1_7_1_t02
	default:
		return Rule{}
	}
}

// isStreamEOLViolation reports whether a format-violation message describes the
// stream keyword not being followed by a valid EOL, as opposed to a /Length
// defect (which keeps the t01 attribution).
func isStreamEOLViolation(msg string) bool {
	b := []byte(msg)
	return bytes.Contains(b, []byte("stream keyword")) &&
		(bytes.Contains(b, []byte("CR only")) || bytes.Contains(b, []byte("not followed by EOL")))
}

// containsObjHeaderPattern returns true if the error message relates to
// object header formatting (obj/endobj keywords, header line).
func containsObjHeaderPattern(msg string) bool {
	for _, pat := range []string{"obj", "endobj", "header"} {
		if bytes.Contains([]byte(msg), []byte(pat)) {
			return true
		}
	}
	return false
}

// isXrefEOLViolation returns true if the violation message relates to
// the EOL marker after the xref keyword (t02), not subsection header spacing (t01).
func isXrefEOLViolation(msg string) bool {
	return bytes.Contains([]byte(msg), []byte("EOL")) || bytes.Contains([]byte(msg), []byte("not followed by"))
}

// checkObjHeaderLeadingWhitespace performs a direct raw-byte scan of every
// indirect object header in the xref table. The existing validateObjectHeaderFormat
// (called during object resolution) only runs for uncached objects. Objects that
// were resolved during pdf.Open (before ComplianceMode was enabled) are already
// cached and skip header validation. This function reads from the underlying
// io.ReaderAt to check that there is no non-EOL whitespace between the
// preceding EOL marker and the object number digits (§6.1.8/§6.1.9).
func checkObjHeaderLeadingWhitespace(doc *document.Document, profile string) []Violation {
	r := doc.RawReader()
	if r == nil {
		return nil
	}

	rule := objHeaderRule(profile)
	var results []Violation

	for objNum, entry := range doc.Index.Entries {
		if entry.Free || entry.Type != xref.XRefTypeNormal {
			continue
		}
		if entry.Offset == 0 {
			continue
		}

		// Read a small window starting a few bytes before the xref offset
		// to inspect what's between the preceding EOL and the object number.
		// Some PDFs have xref offsets pointing to the EOL before the object
		// number, so look ahead past any EOL chars.
		const windowSize = 20
		startOff := max(entry.Offset-1, 0)
		var buf [windowSize]byte
		n, _ := r.ReadAt(buf[:], startOff)
		if n < 2 {
			continue
		}
		data := buf[:n]

		// Find the first digit in the window (this is the start of the object number).
		digitPos := -1
		for i := range data {
			if data[i] >= '0' && data[i] <= '9' {
				digitPos = i
				break
			}
		}
		if digitPos < 1 {
			continue
		}

		// The byte immediately before the first digit must be an EOL (LF or CR).
		prevByte := data[digitPos-1]
		if prevByte != '\n' && prevByte != '\r' {
			results = append(results, Violation{
				Rule:    rule,
				Context: fmt.Sprintf("compliance.file_structure: obj header %d: object number not preceded by EOL marker (0x%02X)", objNum, prevByte),
			})
		}
	}

	return results
}

// checkXRefTableByteFormat validates xref table raw-byte formatting by calling
// the document's ValidateXRefTableFormat for each recorded xref table offset.
// Any violations are collected in doc.FormatViolations and then mapped to rules.
func checkXRefTableByteFormat(doc *document.Document, profile string) []Violation {
	if len(doc.XRefTableOffsets) == 0 {
		return nil
	}

	// Record count before validation so only new violations get processed.
	prevCount := len(doc.FormatViolations)

	for _, offset := range doc.XRefTableOffsets {
		doc.ValidateXRefTableFormat(offset)
	}

	if len(doc.FormatViolations) == prevCount {
		return nil
	}

	// Map new violations to rules.
	var results []Violation
	for _, v := range doc.FormatViolations[prevCount:] {
		rule := xrefFormatRule(profile)
		// PDF/A-1 distinguishes t01 (subsection header spacing) from
		// t02 (EOL after xref keyword). Other parts use a single rule.
		if PDFAPart(profile) == 1 && isXrefEOLViolation(v.Error()) {
			rule = Rule_19005_1_6_1_4_t02
		}
		results = append(results, Violation{
			Rule:    rule,
			Context: v.Error(),
		})
	}
	return results
}
