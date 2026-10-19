package compliance

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
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
// non-hex characters (§6.1.6 t2).
//
// Every finding on the page is reported. Stopping at the first one lost the
// other test entirely when both defects were present: a page whose first bad
// string has an odd digit count and whose second holds a non-hex character
// answered t1 alone, where veraPDF answers both. A hex string answers only
// where veraPDF reads it, see [contentStringJudged]; the other streams a page
// draws are judged by [drawnStreamHexStrings]. A content stream an earlier page
// shares is judged there alone, whatever resources the later page reads it
// under, since its strings are its bytes; see [markJudgedContents].
//
// A number written with an exponent records nothing here. The parse reads the
// stream as veraPDF does ([content.ParseStreamData]), which ends the number at
// the exponent letter, so "1.5e1" is the number 1.5 and the operator e, which
// the undefined-operator rule, ISO 19005-1, 6.2.10 t1 and ISO 19005-2/3/4,
// 6.2.2 t1, states with the other operators ISO 32000-1 does not define.
func checkContentStreamFormat(ctx context.Context, vctx *validationCtx) []Violation {
	doc, profile := vctx.doc, vctx.profile
	oddRule := hexStringRule(profile)
	if oddRule.Specification == "" {
		return nil
	}
	charRule := hexStringInvalidCharRule(profile)

	var results []Violation

	for i := range vctx.pages {
		if ctx.Err() != nil {
			return results
		}
		page := &vctx.pages[i]
		if page.contentBytesJudged || len(page.content) == 0 {
			continue
		}

		// A stream the parser gives up on still states what it read before
		// the failure, so the violations are judged either way.
		_, _, violations, err := content.ParseStreamDataForCompliance(ctx, page.content)
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
// A property list BDC or DP names from /Resources/Properties is judged the
// same way for every operator naming it, see [checkPropertyListHexStrings].
//
// # Corpus evidence
//
//   - All corpora: no content stream writes a bad hex string as one of those
//     values, under PDF/A-1b and PDF/A-2b and the verapdf files under their own
//     profile, so no count moves.
func propertyListStringKey(v content.FormatViolation) ast.PdfName {
	if propertyListJudgesTwice(v.Operator, v.Key) {
		return v.Key
	}
	return ""
}

// propertyListJudgesTwice reports whether veraPDF judges a string written as
// the value of key, an entry of the property list of operator, a second time
// (see [propertyListStringKey]).
func propertyListJudgesTwice(operator string, key ast.PdfName) bool {
	switch key {
	case "ActualText", "Alt":
		return operator == "BDC" || operator == "DP"
	case "Lang":
		return operator == "BDC"
	}
	return false
}

// checkPropertyListHexStrings states the hex string tests, ISO 19005-1/2/3,
// 6.1.6 t1 and t2 and ISO 19005-4, 6.1.5 t1 and t2, for the property lists the
// BDC and DP operators of a document name from the /Properties of their
// resources (ISO 32000-1, 14.6.2).
//
// veraPDF judges every string of such a property list once as part of the
// file, which [checkFormatViolations] states, and again for every operator
// naming it: once as a value of the property list, nested dictionaries and
// arrays included, and once more for an /ActualText or /Alt, or a /Lang under
// BDC, written as its value (see [propertyListStringKey]). A property list is
// judged per operator whether it is an object of its own or written in the
// /Properties dictionary. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b:
// a property list whose /ActualText is a bad hex string fails the test three
// times when one BDC names it and five times when two do, on one page or on
// two; one whose /E is fails it once per BDC more; a form painted twice
// judges its BDC once; and a name /Properties does not define, or a property
// list no operator names, adds nothing.
//
// The operators are those of the content streams veraPDF reads: every page's
// content, and the streams [drawnStreamWalk] reaches, each judged once. A name
// resolves against the stream's own resources, then against the page's.
// Measured the same way: a form with no /Properties naming the page's
// property list, and an appearance stream naming its own twice, fail the test
// as a page does, and PDF/A-4 counts as PDF/A-2b.
//
// The strings are found from the format violations the parse of the object
// holding the property list recorded (see [propertyListSite]), whether that
// object is written uncompressed or in an object stream. A property list
// written directly in the resources of a Type3 font, whose object the walk
// does not know, adds nothing. Each content stream contributes at most
// [maxContentFormatViolationsPerStream] of these findings.
//
// # Corpus evidence
//
//   - All corpora: 9 files fail a hex string test, under PDF/A-1b and
//     PDF/A-2b and the verapdf files under their own profile, and none holds
//     the bad string in a property list an operator names, so no count moves.
func checkPropertyListHexStrings(ctx context.Context, vctx *validationCtx) []Violation {
	oddRule := hexStringRule(vctx.profile)
	if oddRule.Specification == "" || !hasEntryHexString(vctx.formatViolations) {
		return nil
	}
	j := &propertyListJudge{
		doc:      vctx.doc,
		oddRule:  oddRule,
		charRule: hexStringInvalidCharRule(vctx.profile),
		objects:  make(map[int][]error),
	}
	homes := make([]resourcesHome, len(vctx.pages))
	var results []Violation
	for i := range vctx.pages {
		if ctx.Err() != nil {
			return results
		}
		homes[i] = pageResourcesHome(&vctx.pages[i])
		results = append(results, j.judge(ctx, vctx.pages[i].parsedOps(ctx), resourcesHome{}, homes[i], i+1)...)
	}
	walk := newDrawnStreamWalk(vctx, func(s *drawnStream) {
		results = append(results, j.judge(ctx, s.ops, drawnResourcesHome(ctx, vctx.doc, s), homes[s.pageNum-1], s.pageNum)...)
	})
	for i := range vctx.pages {
		if ctx.Err() != nil {
			return results
		}
		walk.walkPage(ctx, &vctx.pages[i], i+1)
	}
	return results
}

// hasEntryHexString reports whether violations holds a hex string finding in
// a string read inside a dictionary, the only kind a property list holds.
func hasEntryHexString(violations []error) bool {
	for _, v := range violations {
		if _, ok := errors.AsType[*parser.EntryViolationError](v); ok && isHexStringViolation(v) {
			return true
		}
	}
	return false
}

// isHexStringViolation reports whether v is a hex string finding.
func isHexStringViolation(v error) bool {
	return errors.Is(v, parser.ErrHexStringOddDigits) || errors.Is(v, parser.ErrHexStringInvalidChar)
}

// propertyListSite is where a direct object lies in the file: the indirect
// object holding it and the keys leading to it from that object's
// dictionary, none when it is the object itself. objNum 0 locates nothing.
type propertyListSite struct {
	objNum int
	keys   []ast.PdfName
}

// child returns the site of the value of key in the dictionary at site, whose
// value as written is raw: raw's own object when it is an indirect reference.
func (site propertyListSite) child(key ast.PdfName, raw ast.PdfObject) propertyListSite {
	if ref, ok := raw.(ast.PdfIndirectRef); ok {
		return propertyListSite{objNum: ref.ObjectNum}
	}
	if site.objNum == 0 {
		return propertyListSite{}
	}
	return propertyListSite{objNum: site.objNum, keys: append(slices.Clone(site.keys), key)}
}

// holds reports whether the string ev names lies in the dictionary at site,
// ev a finding of the object site.objNum, and returns the key of the entry of
// that dictionary whose value the string is, or "" when it lies deeper.
func (site propertyListSite) holds(ev *parser.EntryViolationError) (ast.PdfName, bool) {
	n := len(site.keys)
	if len(ev.Path) <= n {
		return "", false
	}
	// The keys of site are dictionaries written as values of each other, and
	// the dictionary at site holds the string, so each step is one level
	// below the one before it.
	base := ev.Path[0].Depth
	for i := 0; i <= n; i++ {
		if ev.Path[i].Depth != base+i || (i < n && ev.Path[i].Key != site.keys[i]) {
			return "", false
		}
	}
	if len(ev.Path) == n+1 && ev.Depth == ev.Path[n].Depth {
		return ev.Path[n].Key, true
	}
	return "", true
}

// resourcesHome is a resource dictionary a content stream reads names from,
// with its site in the file, the zero site when it is not known.
type resourcesHome struct {
	dict ast.PdfDict
	site propertyListSite
}

// pageResourcesHome returns the resources of page p, which [buildValidationCtx]
// took from the page or else from its nearest ancestor in the page tree, and
// their site in the node that states them.
func pageResourcesHome(p *pageCtx) resourcesHome {
	home := resourcesHome{dict: p.resources}
	if node, site, _ := p.inheritedHolder("Resources"); node != nil && p.resources != nil {
		home.site = site.child("Resources", node["Resources"])
	}
	return home
}

// drawnResourcesHome returns the own resources of s. A Type3 glyph reads the
// resources of its font, whose site the walk does not know.
func drawnResourcesHome(ctx context.Context, doc *document.Document, s *drawnStream) resourcesHome {
	home := resourcesHome{dict: s.resources}
	if s.kind == drawnGlyph || s.objNum == 0 || s.resources == nil {
		return home
	}
	stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: s.objNum})
	if err != nil {
		doc.Logger.Debug("compliance.format: cannot resolve drawn stream", "object", s.objNum, "error", err)
		return home
	}
	home.site = propertyListSite{objNum: s.objNum}.child("Resources", stream.Dict["Resources"])
	return home
}

// propertyListJudge states [checkPropertyListHexStrings] for one document. It
// runs on one goroutine, so it takes no lock.
type propertyListJudge struct {
	doc               *document.Document
	oddRule, charRule Rule
	// objects caches the format violations recorded per object.
	objects map[int][]error
}

// judge states the tests for the property lists the operations ops name,
// resolving a name against own, then page, on page pageNum.
func (j *propertyListJudge) judge(ctx context.Context, ops []content.Operation, own, page resourcesHome, pageNum int) []Violation {
	var results []Violation
	for idx, op := range ops {
		if idx%100 == 0 && ctx.Err() != nil {
			return results
		}
		if (op.Operator != "BDC" && op.Operator != "DP") || len(op.Operands) < 2 {
			continue
		}
		name, ok := op.Operands[len(op.Operands)-1].(ast.PdfName)
		if !ok {
			continue
		}
		site, found := j.propertyList(ctx, own, name)
		if !found {
			site, found = j.propertyList(ctx, page, name)
		}
		if !found || site.objNum == 0 {
			continue
		}
		for _, v := range j.objectViolations(site.objNum) {
			ev, ok := errors.AsType[*parser.EntryViolationError](v)
			if !ok || !isHexStringViolation(v) {
				continue
			}
			key, inList := site.holds(ev)
			if !inList {
				continue
			}
			rule := j.oddRule
			if errors.Is(v, parser.ErrHexStringInvalidChar) {
				rule = j.charRule
			}
			at := fmt.Sprintf("page %d: %s, in the property list /%s that %s operator %d names", pageNum, v.Error(), name, op.Operator, idx)
			results = append(results, Violation{Rule: rule, Context: at})
			if propertyListJudgesTwice(op.Operator, key) {
				results = append(results, Violation{Rule: rule, Context: at + ", read again as its /" + string(key)})
			}
			if len(results) >= maxContentFormatViolationsPerStream {
				return results[:maxContentFormatViolationsPerStream]
			}
		}
	}
	return results
}

// propertyList returns the site of the property list the /Properties of home
// defines as name, and whether it defines one. The site is the zero one when
// home's is not known.
func (j *propertyListJudge) propertyList(ctx context.Context, home resourcesHome, name ast.PdfName) (propertyListSite, bool) {
	if home.dict == nil {
		return propertyListSite{}, false
	}
	props, err := document.ResolveDict(ctx, j.doc, home.dict["Properties"])
	if err != nil || props == nil {
		return propertyListSite{}, false
	}
	raw, ok := props[name]
	if !ok {
		return propertyListSite{}, false
	}
	// A property list is a dictionary (ISO 32000-1, 14.6.2), and veraPDF
	// reaches no string through a name defining anything else.
	if list, err := document.ResolveDict(ctx, j.doc, raw); err != nil || list == nil {
		return propertyListSite{}, false
	}
	return home.site.child("Properties", home.dict["Properties"]).child(name, raw), true
}

// objectViolations returns the format violations recorded for object objNum.
func (j *propertyListJudge) objectViolations(objNum int) []error {
	vs, ok := j.objects[objNum]
	if !ok {
		vs = j.doc.ObjectFormatViolations(objNum)
		j.objects[objNum] = vs
	}
	return vs
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

// checkFormatViolations maps the parser-level format violations collected
// during [resolveAllObjects] onto rules. These include hex string format issues
// (§6.1.6) and object header format issues (§6.1.8/§6.1.9) detected by the
// parser when ComplianceMode is enabled.
//
// It reads the snapshot frozen in the validation context, not the live
// document: the check groups run concurrently and keep resolving objects, so
// a live read reported however many violations had been recorded at that
// instant.
//
// The violations of an object in twice are stated again; see
// [cosReachedTwice]. They are read from the document by object, which reads
// the snapshot's list: [resolveAllObjects] records every object's violations
// before the context is built, and the first recording for an object wins.
// So is a hex string finding in a value of typed, once for each time a typed
// object of veraPDF's model reads the value again; see [typedValues].
//
// # Corpus evidence
//
//   - All corpora: no file writes a stream in an object stream, so the /Length
//     of such a stream, which no check judges, costs no finding veraPDF
//     states.
func checkFormatViolations(vctx *validationCtx, profile string, twice map[int]bool, typed []typedValue) []Violation {
	var results []Violation
	for _, violation := range vctx.formatViolations {
		if rule, ok := formatViolationRule(violation, profile); ok {
			results = append(results, Violation{Rule: rule, Context: violation.Error()})
		}
	}
	for _, objNum := range slices.Sorted(maps.Keys(twice)) {
		for _, violation := range vctx.doc.ObjectFormatViolations(objNum) {
			if rule, ok := formatViolationRule(violation, profile); ok {
				results = append(results, Violation{Rule: rule, Context: violation.Error()})
			}
		}
	}
	objects := make(map[int][]error)
	for _, v := range typed {
		if v.site.objNum == 0 {
			continue
		}
		violations, cached := objects[v.site.objNum]
		if !cached {
			violations = vctx.doc.ObjectFormatViolations(v.site.objNum)
			objects[v.site.objNum] = violations
		}
		for _, violation := range violations {
			if !isHexStringViolation(violation) || !v.holds(violation) {
				continue
			}
			if rule, ok := formatViolationRule(violation, profile); ok {
				results = append(results, Violation{Rule: rule, Context: violation.Error() + ", read again as the " + v.where})
			}
		}
	}
	return results
}

// trailerFormatViolations returns the violations the format violations in the
// bytes of the trailer dictionary answer to, which the object sweep never
// reaches since the trailer is no indirect object.
//
// veraPDF judges the trailer as its CosTrailer (root/trailer[0]) and every
// value in it once, the strings of /ID among them. A document information
// dictionary written directly in the trailer it judges twice, as a value of
// the trailer and as the CosInfo it builds from it (root/trailer[0]/Info[0]),
// as it does one written as an indirect object ([cosReachedTwice]). It reads
// the trailer of the section startxref designates alone, which
// [document.Document.TrailerFormatViolations] holds. Measured with veraPDF
// 1.30 at PDF/A-1b, PDF/A-2b and PDF/A-4: a hex string with an odd digit count
// or a non-hex character in /ID or in another trailer entry fails ISO
// 19005-1/2, 6.1.6 t1 or t2, and ISO 19005-4, 6.1.5 t1 or t2, once, one in a
// direct /Info, as a value or nested in an array or a dictionary there, fails
// it twice, and one in the trailer, or the direct /Info, of an older section
// an incremental update left behind fails nothing.
//
// A cross-reference stream holds the trailer in the entries of its dictionary
// that ISO 32000-1, Table 15 defines ([document.Document.TrailerAsWritten]).
// Its dictionary is an indirect object, which the object sweep judges whole,
// and veraPDF judges those entries once more as the CosTrailer it builds from
// them, and a direct /Info in them a third time. Measured with veraPDF 1.30 at
// PDF/A-1b, PDF/A-2b and PDF/A-4: a hex string with an odd digit count or a
// non-hex character in the /ID of a cross-reference stream fails the rule
// twice, one in its direct /Info three times, and one in an entry Table 15
// does not define, or in an older section's stream, once.
//
// # Corpus evidence
//
//   - All corpora: judging the trailer's strings, and a direct /Info twice,
//     changes no count at PDF/A-1b or PDF/A-2b, veraPDF files at their own
//     profile.
//   - pdfjs issue9540.pdf: the one file whose trailer writes its /Info
//     directly, and nothing in it fails a rule.
//   - All corpora: judging the trailer entries of a cross-reference stream
//     once more, and its direct /Info twice more, changes no count at
//     PDF/A-1b or PDF/A-2b, veraPDF files at their own profile.
func trailerFormatViolations(doc *document.Document, profile string) []Violation {
	_, directInfo := doc.TrailerAsWritten()["Info"].(ast.PdfDict)
	var results []Violation
	for _, violation := range doc.TrailerFormatViolations() {
		rule, ok := formatViolationRule(violation, profile)
		if !ok {
			continue
		}
		found := Violation{Rule: rule, Context: "trailer: " + violation.Error()}
		results = append(results, found)
		if directInfo && inTrailerInfo(violation) {
			results = append(results, found)
		}
	}
	return results
}

// inTrailerInfo reports whether a format violation in the trailer lies in the
// value of its /Info entry: the outermost entry enclosing it is /Info.
func inTrailerInfo(violation error) bool {
	entry, ok := errors.AsType[*parser.EntryViolationError](violation)
	return ok && len(entry.Path) > 0 && entry.Path[0].Key == "Info"
}

// formatViolationRule returns the rule a parser-level format violation
// answers to, and false for one [checkFormatViolations] leaves to another
// check or no rule states.
func formatViolationRule(violation error, profile string) (Rule, bool) {
	// The violations are told apart by the sentinel of their class, never by
	// their message, so rewording a message cannot move a violation to another
	// rule. A violation of no class checked here, a hex string with an odd
	// digit count among them, keeps the hex string rule.
	rule := hexStringRule(profile)
	switch {
	case errors.Is(violation, parser.ErrNumberExponent):
		// ISO 32000-1, 7.3.3 gives a number no exponent, and no ISO 19005
		// rule states number syntax outside a content stream. veraPDF ends
		// the number at the exponent letter and cannot read the object
		// holding it: measured with veraPDF 1.30, a page dictionary or an
		// array holding 1.5e2 stops the validation with a parse error and
		// no rule. See [checkContentStreamFormat] for a content stream.
		return Rule{}, false
	case errors.Is(violation, document.ErrObjectHeaderFormat):
		// Object headers are judged by checkObjHeaderByteFormat, which
		// sweeps every cross-reference entry. What resolution happened to
		// record here is a subset of that, so passing it on would report
		// the same defect twice for any object that missed the cache.
		return Rule{}, false
	case errors.Is(violation, document.ErrXRefFormat):
		rule = xrefFormatRule(profile)
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
			return Rule{}, false
		}
	case errors.Is(violation, parser.ErrStreamFormat):
		if errors.Is(violation, parser.ErrStreamLengthMismatch) {
			// checkStreamLengths sweeps every stream and states this rule
			// there, so passing resolution's copy on states it twice. A
			// stream with no /Length key at all is not swept, and its
			// violation is of another class.
			//
			// TODO: a stream written in an object stream, which ISO
			// 32000-1, 7.5.7 forbids, is not swept either, and veraPDF
			// 1.30 states this rule for a /Length that misses its data
			// there. This finding cannot stand in for the sweep: the
			// parse accepts a /Length that counts the EOL before
			// endstream, which the rule does not.
			return Rule{}, false
		}
		eolRule := streamEOLRule(profile)
		keywordEOL := errors.Is(violation, parser.ErrStreamKeywordEOL)
		if keywordEOL && eolRule.Specification == "" {
			// ISO 19005-4, 6.1.6.1 states only Length == realLength and the
			// F/FFilter/FDecodeParms prohibition. Part 4 dropped the
			// stream-keyword EOL requirement, so there is no rule to answer
			// to and folding it into t1 states a defect the clause does not.
			return Rule{}, false
		}
		rule = streamFormatRule(profile)
		// PDF/A-1/2/3 attribute a stream-keyword EOL defect to a dedicated
		// rule (t02), distinct from the /Length-mismatch rule (t01).
		if keywordEOL {
			rule = eolRule
		}
	case errors.Is(violation, parser.ErrHexStringInvalidChar):
		rule = hexStringInvalidCharRule(profile)
	}

	return rule, true
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
	// lazy xref augmentation a concurrent reader can trigger. An object that
	// augmentation added is never judged, whatever ran before
	// ([sweptXRefEntries]).
	for objNum, entry := range sweptXRefEntries(doc) {
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
