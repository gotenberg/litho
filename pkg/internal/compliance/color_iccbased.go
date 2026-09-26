package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func iccBasedProfileRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_3_2_t01, Rule_19005_2_6_2_4_2_t01, Rule_19005_3_6_2_4_2_t01, Rule_19005_4_6_2_4_2_t01)
}

// iccBasedNRule returns the rule that measures /N against the profile's own
// colour space.
func iccBasedNRule(part int) Rule {
	// Only ISO 19005-1, 6.2.3.2 test 2 states this requirement. Parts 2 to 4
	// have no equivalent: their 6.2.4.2 test 1 judges whether the profile
	// conforms to the ICC specifications, which iccBasedProfileRule already
	// answers for, and their remaining 6.2.4.2 tests judge CMYK overprint and
	// OutputIntent identity. Reporting a component-count mismatch under test 1
	// there labelled it with a requirement it does not break.
	return ruleByPart(part, Rule_19005_1_6_2_3_2_t02, Rule{}, Rule{}, Rule{})
}

// overprintModeRule returns the rule for overprint mode 1 with overprinting
// on for an ICCBased CMYK colour space, which ISO 19005-1 does not state.
func overprintModeRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_4_2_t02, Rule_19005_3_6_2_4_2_t02, Rule_19005_4_6_2_4_2_t02)
}

// iccBasedIdentityRule returns the rule for ICCBased profiles identical to
// the OutputIntent.
func iccBasedIdentityRule(part int) Rule {
	// The "ICCBased profile identical to the OutputIntent" prohibition exists
	// ONLY in PDF/A-4 (6.2.4.2 t3). Parts 1-3 have no such rule (their 6.2.4.2
	// is just ICCInputProfile t1 + overprint t2), so firing there mislabelled
	// the overprint rule and was a false positive on otherwise-compliant files.
	return ruleByPart(part, Rule{}, Rule{}, Rule{}, Rule_19005_4_6_2_4_2_t03)
}

// checkICCBasedProfiles validates ICCBased colour space profiles per
// §6.2.3.2 (part 1) or §6.2.4.2 (parts 2-4).
// checkICCBasedProfiles takes the already-resolved pages so it can reuse their
// parsed operations; the page dictionaries are the same ones [collectPages]
// yields, in the same order.
//
// It judges every ICCBased space [colorLinkWalk] reaches, once per link veraPDF
// makes to it: an ICCBased space written directly is judged wherever a link
// reaches it, and an indirect one once for the whole document. veraPDF judges
// the embedded profile of each PDICCBased it reaches, which has no identity of
// its own, so a direct space three operators link to fails the profile test
// three times. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on an
// abstract profile that cs selects, scn sets and f fills: ISO 19005-1, 6.2.3.2
// t1 and ISO 19005-2, 6.2.4.2 t1 fail three times for a direct /ICCBased array
// and once for an indirect one.
//
// A four-component ICCBased space is the exception: veraPDF judges it as a
// PDICCBasedCMYK, whose identity joins the array's own to the overprint mode
// and flag the link reaches it under (see [overprintMode]), and a direct
// array's own identity is none, so every direct four-component space of the
// document shares one per mode (veraPDF-validation 1.30,
// GFPDICCBasedCMYK.getID and ColorSpaceFactory.getColorSpaceUniqueIdentifier).
// It exists for ISO 19005-2/3/4, 6.2.4.2 t2, which fails once for each such
// identity whose mode is 1 with overprinting on. Measured the same way: a
// direct CMYK space fails once however many operators link to it, and two
// direct ones over different profiles fail once between them; a fill, a
// stroke or a text show after gs selects /OPM 1 with /op or /OP true, the
// fill falling back to /OP, fails the profile test once more and t2 once, and
// so does one of an indirect space; gs selecting /op alone or /OPM alone
// makes a mode of its own, which fails the profile test again but not t2; a
// Q undoes the mode, and a form filling after the gs fills under it; and an
// image, an /Indexed base and the colour operators link under the zero mode.
// Two pages painting under /OPM 1 with the same direct space, or with one
// indirect space, fail t2 once between them.
//
// A device family a Default colour space replaces reaches the Default space
// in its place (ISO 32000-1, 8.6.5.6; see [colorLinkWalk]), which is judged
// here when it is ICCBased: a DefaultCMYK over an abstract profile that k
// selects and f fills before and after a gs selecting /OPM 1 with /op true
// fails the profile test twice and t2 once, as with veraPDF.
//
// # Corpus evidence
//
//   - All corpora: under PDF/A-1b and PDF/A-2b, and the verapdf files under
//     their own profile, eight profile test counts move toward veraPDF's and
//     none away, and no verdict moves.
//   - pdfjs issue17689.pdf: fails ISO 19005-1, 6.2.3.2 t1 600 times with
//     veraPDF, once per link, where litho failed it once per page, seven
//     times.
//   - govdocs thread_400_400765.pdf: sixteen pages use one direct CMYK space,
//     and veraPDF fails the profile test once.
//   - All corpora: under PDF/A-1b and PDF/A-2b, and the verapdf files under
//     their own profile, following the overprint mode moves eight counts on
//     five files, each to veraPDF's count, and no verdict: three of the
//     profile tests and five of ISO 19005-2, 6.2.4.2 t2, which litho stated
//     once per page for fills and once for strokes.
//   - govdocs thread_200_200973.pdf: paints its direct CMYK space under three
//     overprint modes, and veraPDF fails the profile test and ISO 19005-1,
//     6.2.3.2 t2 three times each, and 6.2.4.2 t2 once, where litho failed
//     the first two once and t2 four times.
//   - govdocs thread_000_000372.pdf, thread_000_000314.pdf and
//     thread_400_400567.pdf: veraPDF fails 6.2.4.2 t2 once, where litho
//     failed it 40, 7 and 9 times.
//   - govdocs thread_200_200087.pdf: veraPDF fails 6.2.4.2 t2 three times,
//     where litho failed it 7 times.
func checkICCBasedProfiles(ctx context.Context, doc *document.Document, catalog ast.PdfDict, pages []pageCtx, part int, results *[]Violation) {
	// Collect the OutputIntent ICC profile info for identity comparison (parts 2+).
	var outputIntentRef ast.PdfIndirectRef
	var outputIntentHdr iccProfileHeader
	var outputIntentData []byte
	if part >= 2 {
		outputIntentRef, outputIntentHdr, outputIntentData = resolveOutputIntentICCInfo(ctx, doc, catalog)
	}

	// For page-level OutputIntents (PDF/A-4 allows them), use the page's
	// OutputIntent ICC info if the catalog-level one is absent.
	type intent struct {
		ref      ast.PdfIndirectRef
		hdr      iccProfileHeader
		data     []byte
		resolved bool
	}
	pageIntents := make([]intent, len(pages))
	intentOf := func(pageIdx int) intent {
		if part < 2 || len(outputIntentData) > 0 {
			return intent{ref: outputIntentRef, hdr: outputIntentHdr, data: outputIntentData}
		}
		pi := &pageIntents[pageIdx]
		if !pi.resolved {
			pi.ref, pi.hdr, pi.data = resolveOutputIntentICCInfo(ctx, doc, pages[pageIdx].dict)
			pi.resolved = true
		}
		return *pi
	}

	// One profile judged on one page against one blending space yields the
	// same violations every time a link reaches it, so they are worked out
	// once and repeated.
	type judgement struct {
		page, profile, blend int
	}
	judged := make(map[judgement][]Violation)
	directCMYK := make(map[overprintMode]bool)
	walkColorLinks(ctx, doc, pages, part, func(n colorNode) {
		if n.kind != "ICCBased" {
			return
		}
		if n.direct && iccBasedComponents(ctx, doc, n.arr) == cmykComponents {
			if directCMYK[n.mode] {
				return
			}
			directCMYK[n.mode] = true
		}
		// ISO 19005-2/3/4, 6.2.4.2 t2: overprint mode 1 with overprinting on
		// for the CMYK space painted with; n.mode is set on a CMYK space alone.
		if n.mode.on && n.mode.opm != 0 {
			if rule := overprintModeRule(part); rule.Specification != "" {
				*results = append(*results, Violation{Rule: rule, Context: fmt.Sprintf(
					"page %d: ICCBased CMYK colour space painted with overprinting on under overprint mode %d", n.page+1, n.mode.opm)})
			}
		}
		key := judgement{page: n.page, blend: n.in.blendRef.ObjectNum}
		if len(n.arr) >= 2 {
			if ref, ok := n.arr[1].(ast.PdfIndirectRef); ok {
				key.profile = ref.ObjectNum
			}
		}
		if vs, ok := judged[key]; ok && key.profile != 0 {
			*results = append(*results, vs...)
			return
		}
		var vs []Violation
		oi := intentOf(n.page)
		checkICCBasedArray(ctx, doc, n.arr, part, n.page, "ICCBased colour space", oi.ref, oi.hdr, oi.data, n.in.blendRef, &vs)
		judged[key] = vs
		*results = append(*results, vs...)
	})
}

// cmykComponents is the number of components of a CMYK colour space (ISO
// 32000-1, 8.6.4.4), the count veraPDF reads an ICCBased space's /N against to
// judge it as a PDICCBasedCMYK.
const cmykComponents = 4

// iccBasedComponents returns the /N the profile stream of the ICCBased array
// arr declares, or 0 when it declares none that can be read.
func iccBasedComponents(ctx context.Context, doc *document.Document, arr ast.PdfArray) int {
	if len(arr) < 2 {
		return 0
	}
	stream, err := document.ResolveStream(ctx, doc, arr[1])
	if err != nil {
		return 0
	}
	n, err := document.ResolveInt(ctx, doc, stream.Dict["N"])
	if err != nil {
		return 0
	}
	return int(n)
}

// reportUnreadableICCN reports /N against a profile whose own component count
// cannot be read.
//
// ISO 19005-1 §6.2.3.2 test 2 requires /N to match the number of components in
// the embedded profile. A profile that will not decode, or whose header is
// truncated before the data colour space at byte 16, states no count at all, so
// the requirement stands unmet rather than unmeasured. Only the profile stream
// being unresolvable leaves this silent, because there is then no profile to
// judge (see [checkICCBasedArray]).
func reportUnreadableICCN(nRule Rule, nVal int, pageIdx int, where string, results *[]Violation) {
	if nRule.Specification == "" {
		return
	}
	*results = append(*results, failf(nRule,
		"page %d, %s: /N=%d cannot be matched against an unreadable embedded profile", pageIdx+1, where, nVal)...)
}

// checkICCBasedArray validates one [/ICCBased stream] colour space array.
// where names the site the violation message reports.
//
// The rules are part-dependent: /N against the profile's own colour space is
// stated only by ISO 19005-1 (see [iccBasedNRule]), and the OutputIntent
// identity prohibition only by ISO 19005-4 (see [iccBasedIdentityRule]).
func checkICCBasedArray(ctx context.Context, doc *document.Document, arr ast.PdfArray, part int, pageIdx int, where string, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, results *[]Violation) {
	if len(arr) < 2 {
		return
	}

	stream, err := document.ResolveStream(ctx, doc, arr[1])
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve ICCBased stream", "page", pageIdx, "where", where, "error", err)
		*results = append(*results, failf(iccBasedProfileRule(part),
			"page %d, %s: cannot resolve ICCBased profile stream", pageIdx+1, where)...)
		return
	}

	nRule := iccBasedNRule(part)
	nVal, err := document.ResolveInt(ctx, doc, stream.Dict["N"])
	if err != nil {
		if nRule.Specification != "" {
			*results = append(*results, failf(nRule,
				"page %d, %s: ICCBased profile missing /N entry", pageIdx+1, where)...)
		}
		return
	}

	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot decode ICCBased stream", "page", pageIdx, "where", where, "error", err)
		*results = append(*results, failf(iccBasedProfileRule(part),
			"page %d, %s: cannot decode ICCBased profile", pageIdx+1, where)...)
		reportUnreadableICCN(nRule, int(nVal), pageIdx, where, results)
		return
	}

	hdr, ok := parseICCHeader(data)
	if !ok {
		*results = append(*results, failf(iccBasedProfileRule(part),
			"page %d, %s: ICC profile header too short", pageIdx+1, where)...)
		reportUnreadableICCN(nRule, int(nVal), pageIdx, where, results)
		return
	}

	if problem := iccInputHeaderProblem(hdr, part); problem != "" {
		*results = append(*results, failf(iccBasedProfileRule(part),
			"page %d, %s: %s", pageIdx+1, where, problem)...)
	}

	expectedN := expectedComponentCount(hdr.colorSpace)
	if nRule.Specification != "" && (expectedN == 0 || int(nVal) != expectedN) {
		*results = append(*results, failf(nRule,
			"page %d, %s: /N=%d does not match ICC colour space %q", pageIdx+1, where, nVal, hdr.colorSpace)...)
	}

	if part >= 2 && hdr.colorSpace == "CMYK" {
		checkICCBasedIdentity(ctx, doc, arr[1], data, part, pageIdx, where, outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results)
	}
}

// checkICCBasedIdentity reports a CMYK ICCBased profile identical to the
// OutputIntent profile or to the transparency blending colour space. Only
// ISO 19005-4 §6.2.4.2 t3 prohibits it; parts 1 to 3 carry no equivalent
// clause, so firing there mislabelled the overprint rule and was a false
// positive on otherwise-compliant files.
func checkICCBasedIdentity(ctx context.Context, doc *document.Document, profileObj ast.PdfObject, data []byte, part int, pageIdx int, where string, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, results *[]Violation) {
	iccRef, _ := profileObj.(ast.PdfIndirectRef)
	identical := false

	if len(outputIntentData) > 0 && outputIntentHdr.colorSpace == "CMYK" {
		if iccRef.ObjectNum > 0 &&
			outputIntentRef.ObjectNum > 0 &&
			iccRef.ObjectNum == outputIntentRef.ObjectNum &&
			iccRef.GenNum == outputIntentRef.GenNum {
			identical = true
		}
		if !identical && ICCProfilesEqual(data, outputIntentData) {
			identical = true
		}
	}

	if !identical && blendingCSRef.ObjectNum > 0 {
		if iccRef.ObjectNum > 0 &&
			iccRef.ObjectNum == blendingCSRef.ObjectNum &&
			iccRef.GenNum == blendingCSRef.GenNum {
			identical = true
		}
		if !identical {
			blendStream, err := document.ResolveStream(ctx, doc, blendingCSRef)
			if err == nil {
				blendData, err := doc.DecodeStream(ctx, blendStream)
				if err == nil && ICCProfilesEqual(data, blendData) {
					identical = true
				}
			}
		}
	}

	if identical && part == 4 {
		*results = append(*results, failf(iccBasedIdentityRule(part),
			"page %d, %s: ICCBased profile is identical to OutputIntent", pageIdx+1, where)...)
	}
}

// resolveFormBlendingRef extracts the ICCBased blending CS ref from a Form
// XObject's Group CS, falling back to the parent's blending ref.
func resolveFormBlendingRef(ctx context.Context, doc *document.Document, stream ast.PdfStream, parentRef ast.PdfIndirectRef) ast.PdfIndirectRef {
	grp, _ := document.ResolveDict(ctx, doc, stream.Dict["Group"])
	if grp == nil {
		return parentRef
	}
	csObj, ok := grp["CS"]
	if !ok {
		return parentRef
	}
	csArr, err := document.ResolveArray(ctx, doc, csObj)
	if err != nil || len(csArr) < 2 {
		return parentRef
	}
	csT, _ := document.ResolveName(ctx, doc, csArr[0])
	if csT != "ICCBased" {
		return parentRef
	}
	if ref, ok := csArr[1].(ast.PdfIndirectRef); ok {
		return ref
	}
	return parentRef
}

// resolvePageBlendingICCRef returns the indirect reference to the ICCBased profile
// stream used as the page's transparency blending CS, or a zero ref if absent.
func resolvePageBlendingICCRef(ctx context.Context, doc *document.Document, page ast.PdfDict) ast.PdfIndirectRef {
	groupObj, ok := page["Group"]
	if !ok {
		return ast.PdfIndirectRef{}
	}
	group, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || group == nil {
		return ast.PdfIndirectRef{}
	}
	s, _ := document.ResolveName(ctx, doc, group["S"])
	if s != "Transparency" {
		return ast.PdfIndirectRef{}
	}
	csObj, ok := group["CS"]
	if !ok {
		return ast.PdfIndirectRef{}
	}
	arr, err := document.ResolveArray(ctx, doc, csObj)
	if err != nil || len(arr) < 2 {
		return ast.PdfIndirectRef{}
	}
	csType, _ := document.ResolveName(ctx, doc, arr[0])
	if csType != "ICCBased" {
		return ast.PdfIndirectRef{}
	}
	if ref, ok := arr[1].(ast.PdfIndirectRef); ok {
		return ref
	}
	return ast.PdfIndirectRef{}
}

// expectedComponentCount returns the expected /N value for a given ICC colour
// space signature. A Lab profile has three components (ISO 15076-1, Table 19),
// and veraPDF's ISO 19005-1, 6.2.3.2 t2 test accepts N 3 for "Lab " as for
// "RGB ", measured on an output intent and on an ICCBased colour space.
func expectedComponentCount(colorSpace string) int {
	switch colorSpace {
	case "GRAY":
		return 1
	case "RGB ", "Lab ":
		return 3
	case "CMYK":
		return 4
	default:
		return 0
	}
}
