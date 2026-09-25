package compliance

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
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

// maxContentFormatViolationsPerStream bounds how many content-stream format
// findings one content stream contributes: a page's content, which ISO 32000-1,
// 7.7.3.3 makes one stream however many parts its /Contents array holds, or one
// form XObject, tiling pattern, Type3 glyph or appearance stream. Not
// spec-mandated: an anti-DoS guard. veraPDF keys the hex string rules on the
// CosString and the undefined-operator rule on the operator, and applies no
// such bound, but a conforming stream has no findings at all and a real
// defective producer emits a handful, so a stream reaching this many is
// crafted rather than merely wrong.
const maxContentFormatViolationsPerStream = 64

// checkContentStreamFormat parses page content streams with format checking
// enabled to detect hex string violations, odd digit count (§6.1.6 t1) and
// non-hex characters (§6.1.6 t2), and numbers written with an exponent where an
// operand stands, which read as an undefined operator.
//
// Every finding on the page is reported. Stopping at the first one lost the
// other test entirely when both defects were present: a page whose first bad
// string has an odd digit count and whose second holds a non-hex character
// answered t1 alone, where veraPDF answers both. A hex string answers only
// where veraPDF reads it, see [contentStringJudged]; the other streams a page
// draws are judged by [drawnStreamHexStrings].
//
// # Corpus evidence
//
//   - pdfjs sci-notation.pdf: sets a font size with "1e2 Tf" after the stray
//     keyword ETBT, and veraPDF states the undefined-operator test twice at
//     PDF/A-1b and PDF/A-2b, where litho stated it for ETBT alone.
func checkContentStreamFormat(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	oddRule := hexStringRule(profile)
	if oddRule.Specification == "" {
		return nil
	}
	charRule := hexStringInvalidCharRule(profile)
	operatorRule := undefinedOperatorRule(PDFAPart(profile))

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

		// A stream the parser gives up on still states what it read before
		// the failure, so the violations are judged either way.
		_, _, violations, err := content.ParseStreamDataForCompliance(ctx, streamData)
		if err != nil {
			doc.Logger.Debug("compliance.format: cannot parse the whole page content", "page", i+1, "error", err)
		}
		reported := 0
		for _, v := range violations {
			if reported == maxContentFormatViolationsPerStream {
				break
			}
			var rule Rule
			switch {
			case errors.Is(v.Err, parser.ErrNumberExponentOperand):
				// An operator ISO 32000-1 does not define, as
				// checkExponentOperands states for the other streams.
				rule = operatorRule
			case errors.Is(v.Err, parser.ErrNumberExponent):
				// Inside an array or a dictionary the token is neither a
				// number nor an operator, a syntax error no ISO 19005 rule
				// states. veraPDF gives up on the content stream and reports
				// nothing from it; measured with veraPDF 1.30 on a dash array,
				// a BDC property list and an inline image dictionary.
				continue
			case !contentStringJudged(v):
				continue
			case errors.Is(v.Err, parser.ErrHexStringInvalidChar):
				rule = charRule
			default:
				rule = oddRule
			}
			reported++
			results = append(results, Violation{
				Rule:    rule,
				Context: fmt.Sprintf("page %d: %s", i+1, v.Err.Error()),
			})
			if key := propertyListStringKey(v); key != "" && reported < maxContentFormatViolationsPerStream {
				reported++
				results = append(results, Violation{
					Rule:    rule,
					Context: fmt.Sprintf("page %d: %s, read again as the property list's /%s", i+1, v.Err.Error(), key),
				})
			}
		}
	}
	return results
}

// contentStringJudged reports whether veraPDF judges the syntax of the strings
// in the operand holding v, a violation recorded in a content stream.
//
// ISO 19005-1/2/3, 6.1.6 and ISO 19005-4, 6.1.5 state the hex string tests
// over every string a file writes, and veraPDF reaches a string of a content
// stream only through the operator that reads it: the string Tj, ' and " show,
// every string of the array TJ shows, and every string of the dictionary BDC
// and DP carry as their property list and BI as its inline image dictionary,
// nested arrays and dictionaries included. A string in any other operand, the
// operands of an operator ISO 32000-1 does not define and those no operator
// follows among them, is never judged. Measured with veraPDF 1.30 at PDF/A-2b:
// "<414> Tj" and "[<414>] TJ" fail t1, and "<414> zz", "<414> w",
// "<414> 5 Tj", "/Span <414> BDC" and a trailing "<414>" pass it.
//
// veraPDF judges some strings of a property list twice, see
// [propertyListStringKey].
func contentStringJudged(v content.FormatViolation) bool {
	if !v.Last {
		return false
	}
	var ok bool
	switch v.Operator {
	case "Tj", "'", `"`:
		_, ok = v.Operand.(ast.PdfString)
	case "TJ":
		_, ok = v.Operand.(ast.PdfArray)
	case "BDC", "DP", "BI":
		_, ok = v.Operand.(ast.PdfDict)
	}
	return ok
}

// propertyListStringKey returns the key of the property list entry holding v,
// a violation [contentStringJudged] accepts, when veraPDF judges that string a
// second time, and "" when it judges it once.
//
// veraPDF models the property list of BDC and DP (ISO 32000-1, 14.6.2) as a
// dictionary whose values it judges, and models three of its entries again as
// objects of their own: /ActualText (14.9.4) and /Alt (14.9.3) for both
// operators, and /Lang (14.9.2.2) for BDC alone. A bad hex string written as
// one of those values therefore fails the test twice, at
// .../operators[n]/properties[0]/values[m] and at .../operators[n]/actualText[0],
// alt[0] or Lang[0]. Measured with veraPDF 1.30 at PDF/A-1b, PDF/A-2b and
// PDF/A-4, for odd digits and for a non-hex character alike: /ActualText, /Alt
// and /Lang under BDC and /ActualText and /Alt under DP fail twice, while /E,
// /Lang under DP and an /ActualText nested in another entry fail once.
//
// A property list BDC names from /Resources/Properties is judged there as well,
// once as an object of the file and twice more for every BDC naming it. The
// file-level format check states such a string once, so that count stays below
// veraPDF's.
//
// # Corpus evidence
//
//   - All corpora: no content stream writes a bad hex string as one of those
//     values, under PDF/A-1b and PDF/A-2b and the verapdf files under their own
//     profile, so no count moves.
func propertyListStringKey(v content.FormatViolation) ast.PdfName {
	switch v.Key {
	case "ActualText", "Alt":
		if v.Operator == "BDC" || v.Operator == "DP" {
			return v.Key
		}
	case "Lang":
		if v.Operator == "BDC" {
			return v.Key
		}
	}
	return ""
}

// drawnStreamHexStrings states the hex string tests, ISO 19005-1/2/3, 6.1.6
// t1 and t2 and ISO 19005-4, 6.1.5 t1 and t2, for the content streams a
// document draws besides the content of its pages: the form XObjects, tiling
// patterns, Type3 glyphs and annotation appearances [drawnStreamWalk] reaches.
// [checkContentStreamFormat] states them for a page's own content.
//
// The walk parses each stream once, and the tests are stated from that parse.
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, a bad hex string in any
// of them fails the test once per string, nested in another form, an
// appearance or a pattern included; a stream a page declares and never draws
// passes it, be it a form no Do paints, a pattern no scn selects or a Type3
// font a Tf selects and no text shows; and a stream drawn twice, by one page
// or by two, or an appearance two annotations share, fails it once, since the
// walk judges each stream once for the whole document.
type drawnStreamHexStrings struct {
	oddRule, charRule Rule
}

// newDrawnStreamHexStrings returns the judge for profile, or nil when the
// profile states no hex string test.
func newDrawnStreamHexStrings(profile string) *drawnStreamHexStrings {
	oddRule := hexStringRule(profile)
	if oddRule.Specification == "" {
		return nil
	}
	return &drawnStreamHexStrings{
		oddRule:  oddRule,
		charRule: hexStringInvalidCharRule(profile),
	}
}

// check states the tests for the violations recorded while parsing one
// stream. A nil j states nothing.
func (j *drawnStreamHexStrings) check(violations []content.FormatViolation, pageNum int) []Violation {
	if j == nil {
		return nil
	}
	var results []Violation
	for _, v := range violations {
		if len(results) == maxContentFormatViolationsPerStream {
			break
		}
		rule := j.oddRule
		switch {
		case !errors.Is(v.Err, parser.ErrHexStringOddDigits) && !errors.Is(v.Err, parser.ErrHexStringInvalidChar):
			continue
		case !contentStringJudged(v):
			continue
		case errors.Is(v.Err, parser.ErrHexStringInvalidChar):
			rule = j.charRule
		}
		results = append(results, Violation{
			Rule:    rule,
			Context: fmt.Sprintf("page %d: %s", pageNum, v.Err.Error()),
		})
		if key := propertyListStringKey(v); key != "" && len(results) < maxContentFormatViolationsPerStream {
			results = append(results, Violation{
				Rule:    rule,
				Context: fmt.Sprintf("page %d: %s, read again as the property list's /%s", pageNum, v.Err.Error(), key),
			})
		}
	}
	return results
}

// checkExponentOperands states the undefined-operator rule, ISO 19005-1,
// 6.2.10 t1 and ISO 19005-2, 6.2.2 t1, once for every number written with an
// exponent where an operand stands, from the format violations recorded while
// parsing one content stream. ISO 32000-1, 7.3.3 gives a number no exponent, so
// the token is no operand: it stands where an operator does, and it is none ISO
// 32000-1 defines. veraPDF ends the number at the exponent letter and reports
// the rest of the token as that operator, once per token; measured with
// veraPDF 1.30 on "1.5e1 w", "1.5E-1 w" and "1e1 w".
//
// [checkContentStreamFormat] states it for a page's own content. The rules that
// judge the other streams a document draws, the ones [drawnStreamWalk]
// reaches, call this on the violations of the parse the undefined-operator rule
// reads, so the two findings reach the same streams. veraPDF parses each of
// those streams the same way: measured with veraPDF 1.30 at PDF/A-1b and
// PDF/A-2b, "1.5e1 w" in any one of them fails the undefined-operator test
// once, and "15 w" passes it.
//
// A number inside an array or a dictionary answers to no rule; see
// [checkContentStreamFormat].
func checkExponentOperands(violations []content.FormatViolation, part, pageNum int) []Violation {
	rule := undefinedOperatorRule(part)
	if rule.Specification == "" {
		return nil
	}
	var results []Violation
	for _, v := range violations {
		if len(results) == maxContentFormatViolationsPerStream {
			break
		}
		if errors.Is(v.Err, parser.ErrNumberExponentOperand) {
			results = append(results, Violation{
				Rule:    rule,
				Context: fmt.Sprintf("page %d: %s", pageNum, v.Err.Error()),
			})
		}
	}
	return results
}

// checkFormatViolations maps the parser-level format violations collected
// during [resolveAllObjects] onto rules. These include hex string format issues
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
	streamRule := streamFormatRule(profile)
	xrefRule := xrefFormatRule(profile)

	for _, violation := range vctx.formatViolations {
		// The violations are told apart by the sentinel of their class, never
		// by their message, so rewording a message cannot move a violation to
		// another rule. A violation of no class checked here, a hex string
		// with an odd digit count among them, keeps the hex string rule.
		rule := hexRule
		switch {
		case errors.Is(violation, parser.ErrNumberExponent):
			// ISO 32000-1, 7.3.3 gives a number no exponent, and no ISO 19005
			// rule states number syntax outside a content stream. veraPDF ends
			// the number at the exponent letter and cannot read the object
			// holding it: measured with veraPDF 1.30, a page dictionary or an
			// array holding 1.5e2 stops the validation with a parse error and
			// no rule. See [checkContentStreamFormat] for a content stream.
			continue
		case errors.Is(violation, document.ErrObjectHeaderFormat):
			// Object headers are judged by checkObjHeaderByteFormat, which
			// sweeps every cross-reference entry. What resolution happened to
			// record here is a subset of that, so passing it on would report
			// the same defect twice for any object that missed the cache.
			continue
		case errors.Is(violation, document.ErrXRefFormat):
			rule = xrefRule
			if PDFAPart(profile) == 1 {
				// ISO 19005-1, 6.1.4 splits the table in two:
				// t1 subsectionHeaderSpaceSeparated, t2 xrefEOLMarkersComplyPDFA.
				if errors.Is(violation, document.ErrXRefEOLMarkers) {
					rule = Rule_19005_1_6_1_4_t02
				}
			} else if !errors.Is(violation, document.ErrXRefEOLMarkers) {
				// Parts 2 and up kept only xrefEOLMarkersComplyPDFA and dropped
				// the subsection-header spacing requirement, so a spacing defect
				// has no rule to answer to. Measured: veraPDF 1.30.2 reports
				// nothing at all for an xref whose only defect is two spaces in
				// the subsection header.
				continue
			}
		case errors.Is(violation, parser.ErrStreamFormat):
			if errors.Is(violation, parser.ErrStreamLengthMismatch) {
				// checkStreamLengths sweeps every stream and states this rule
				// there, so passing resolution's copy on states it twice. A
				// stream with no /Length key at all is not swept, and its
				// violation is of another class.
				continue
			}
			eolRule := streamEOLRule(profile)
			keywordEOL := errors.Is(violation, parser.ErrStreamKeywordEOL)
			if keywordEOL && eolRule.Specification == "" {
				// ISO 19005-4, 6.1.6.1 states only Length == realLength and the
				// F/FFilter/FDecodeParms prohibition. Part 4 dropped the
				// stream-keyword EOL requirement, so there is no rule to answer
				// to and folding it into t1 states a defect the clause does not.
				continue
			}
			rule = streamRule
			// PDF/A-1/2/3 attribute a stream-keyword EOL defect to a dedicated
			// rule (t02), distinct from the /Length-mismatch rule (t01).
			if keywordEOL {
				rule = eolRule
			}
		case errors.Is(violation, parser.ErrHexStringInvalidChar):
			rule = hexStringInvalidCharRule(profile)
		}

		results = append(results, Violation{
			Rule:    rule,
			Context: violation.Error(),
		})
	}

	return results
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

	// Iterate the entries in object-number order, from a copy taken under the
	// document lock: ranging the map emitted the violations in a different
	// order on every run (CONTRIBUTING "Deterministic output") and raced the
	// lazy xref augmentation a concurrent reader can trigger.
	for objNum, entry := range doc.XRefEntries() {
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
			if errors.Is(v, document.ErrXRefEOLMarkers) {
				rule = Rule_19005_1_6_1_4_t02
			}
		} else if !errors.Is(v, document.ErrXRefEOLMarkers) {
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
