package compliance

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

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

// maxHexStringViolationsPerPage bounds how many hex-string findings one page
// contributes. Not spec-mandated: an anti-DoS guard. veraPDF keys the rule on
// the CosString and applies no such bound, but a conforming page has no
// findings at all and a real defective producer emits a handful, so a page
// reaching this many is crafted rather than merely wrong.
const maxHexStringViolationsPerPage = 64

// checkContentStreamHexStrings parses page content streams with format
// checking enabled to detect hex string violations: odd digit count
// (§6.1.6 t1) and non-hex characters (§6.1.6 t2).
//
// Every finding on the page is reported. Stopping at the first one lost the
// other test entirely when both defects were present: a page whose first bad
// string has an odd digit count and whose second holds a non-hex character
// answered t1 alone, where veraPDF answers both.
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
		for n, v := range violations {
			if n == maxHexStringViolationsPerPage {
				break
			}
			msg := v.Error()
			rule := oddRule
			if bytes.Contains([]byte(msg), []byte("invalid character")) {
				rule = charRule
			}
			results = append(results, Violation{
				Rule:    rule,
				Context: fmt.Sprintf("page %d: %s", i+1, msg),
			})
		}
	}
	return results
}

// checkFormatViolations maps the parser-level format violations collected
// during resolveAllObjects onto rules. These include hex string format issues
// (§6.1.6) and object header format issues (§6.1.8/§6.1.9) detected by the
// parser when ComplianceMode is enabled.
//
// It reads the snapshot frozen in the validation context, not the live
// document: the check groups run concurrently and keep resolving objects, so
// a live read reported however many violations had been recorded at that
// instant.
func checkFormatViolations(vctx *validationCtx, profile string) []Violation {
	if len(vctx.formatViolations) == 0 {
		return nil
	}

	var results []Violation
	hexRule := hexStringRule(profile)
	objRule := objHeaderRule(profile)
	streamRule := streamFormatRule(profile)
	xrefRule := xrefFormatRule(profile)

	for _, violation := range vctx.formatViolations {
		msg := violation.Error()

		// Object headers are judged by checkObjHeaderByteFormat, which sweeps
		// every cross-reference entry. What resolution happened to record here
		// is a subset of that, so passing it on would report the same defect
		// twice for any object that missed the cache.
		if containsObjHeaderPattern(msg) && !containsXRefPattern(msg) && !containsStreamPattern(msg) {
			continue
		}

		rule := hexRule
		//nolint:gocritic // if-else chain uses function-call predicates; switch is not applicable.
		if containsXRefPattern(msg) {
			rule = xrefRule
			if PDFAPart(profile) == 1 {
				// ISO 19005-1, 6.1.4 splits the table in two:
				// t1 subsectionHeaderSpaceSeparated, t2 xrefEOLMarkersComplyPDFA.
				if isXrefEOLViolation(msg) {
					rule = Rule_19005_1_6_1_4_t02
				}
			} else if !isXrefEOLViolation(msg) {
				// Parts 2 and up kept only xrefEOLMarkersComplyPDFA and dropped
				// the subsection-header spacing requirement, so a spacing defect
				// has no rule to answer to. Measured: veraPDF 1.30.2 reports
				// nothing at all for an xref whose only defect is two spaces in
				// the subsection header.
				continue
			}
		} else if containsStreamPattern(msg) {
			if isStreamLengthMismatch(msg) {
				// checkStreamLengths sweeps every stream and states this rule
				// there, so passing resolution's copy on states it twice. A
				// stream with no /Length key at all is not swept, and its
				// message is not this one.
				continue
			}
			eolRule := streamEOLRule(profile)
			if isStreamEOLViolation(msg) && eolRule.Specification == "" {
				// ISO 19005-4, 6.1.6.1 states only Length == realLength and the
				// F/FFilter/FDecodeParms prohibition. Part 4 dropped the
				// stream-keyword EOL requirement, so there is no rule to answer
				// to and folding it into t1 states a defect the clause does not.
				continue
			}
			rule = streamRule
			// PDF/A-1/2/3 attribute a stream-keyword EOL defect to a dedicated
			// rule (t02), distinct from the /Length-mismatch rule (t01).
			if eolRule.Specification != "" && isStreamEOLViolation(msg) {
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
// isStreamLengthMismatch reports the parser message for a declared /Length that
// disagrees with the bytes found. It deliberately does not match "stream missing
// /Length key": checkStreamLengths skips a stream with no /Length, so that one
// still has to come through here.
func isStreamLengthMismatch(msg string) bool {
	b := []byte(msg)
	return bytes.Contains(b, []byte("stream /Length")) &&
		bytes.Contains(b, []byte("does not match actual byte count"))
}

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

// checkObjHeaderByteFormat performs a direct raw-byte scan of every indirect
// object header in the xref table (§6.1.8/§6.1.9).
//
// It is the only place object headers are judged. Validating them during
// resolution instead reaches an object only when it misses the cache, so
// whatever pdf.Open resolved before compliance mode was enabled, the catalog
// among it, was never checked at all, and which objects those were varied with
// the shape of the file. Iterating the cross-reference entries judges every
// object, and in object-number order, so the findings are also deterministic.
func checkObjHeaderByteFormat(doc *document.Document, profile string) []Violation {
	r := doc.RawReader()
	if r == nil {
		return nil
	}

	rule := objHeaderRule(profile)
	var results []Violation

	// Iterate the entries in object-number order, from a snapshot: ranging
	// the map emitted the violations in a different order on every run
	// (CONTRIBUTING "Deterministic output") and raced the lazy xref
	// augmentation another check group can trigger.
	entries := doc.XRefEntriesSnapshot()
	for _, objNum := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[objNum]
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

		// veraPDF keys the rule on the CosIndirect object and asks it the single
		// boolean spacingCompliesPDFA, which covers the separators and the EOLs
		// alike, so an object failing several of them answers once. Collect the
		// reasons and state them together.
		var reasons []string

		// The byte immediately before the first digit must be an EOL (LF or CR).
		prevByte := data[digitPos-1]
		if prevByte != '\n' && prevByte != '\r' {
			reasons = append(reasons, fmt.Sprintf(
				"compliance: obj header %d: object number not preceded by EOL marker (0x%02X)",
				objNum, prevByte))
		}

		// The separators inside the header, and the EOL around the obj and
		// endobj keywords, are judged from the same offset.
		for _, violation := range doc.ValidateObjectHeaderFormat(entry.Offset, objNum) {
			reasons = append(reasons, violation.Error())
		}

		if len(reasons) > 0 {
			results = append(results, Violation{
				Rule:    rule,
				Context: strings.Join(reasons, "; "),
			})
		}
	}

	return results
}

// checkXRefTableByteFormat validates xref table raw-byte formatting by calling
// the document's ValidateXRefTableFormat for each recorded xref table offset,
// then maps what it returns to rules. Reading the findings back out of the
// document's shared violation slice used to hand this check whatever another
// check group had recorded in the meantime, labelling an object-header defect
// as an xref one.
func checkXRefTableByteFormat(doc *document.Document, profile string) []Violation {
	if len(doc.XRefTableOffsets) == 0 {
		return nil
	}

	var violations []error
	for _, offset := range doc.XRefTableOffsets {
		violations = append(violations, doc.ValidateXRefTableFormat(offset)...)
	}

	// Map the violations to rules.
	var results []Violation
	for _, v := range violations {
		rule := xrefFormatRule(profile)
		if PDFAPart(profile) == 1 {
			// ISO 19005-1, 6.1.4 distinguishes t01 (subsection header spacing)
			// from t02 (EOL after the xref keyword).
			if isXrefEOLViolation(v.Error()) {
				rule = Rule_19005_1_6_1_4_t02
			}
		} else if !isXrefEOLViolation(v.Error()) {
			// Parts 2 and up kept only xrefEOLMarkersComplyPDFA. They have no
			// subsection-header spacing requirement, so a spacing defect has no
			// rule to answer to. Measured: veraPDF 1.30.2 reports nothing at all
			// for an xref whose only defect is two spaces in its subsection
			// header, while ISO 19005-1 answers 6.1.4 t1 for the same bytes.
			continue
		}
		results = append(results, Violation{
			Rule:    rule,
			Context: v.Error(),
		})
	}
	return results
}
