package compliance

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkGraphicsGroup validates ExtGState, halftones, rendering intent, and
// overprint restrictions required by PDF/A parts 1-4.
func checkGraphicsGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, profile := vctx.doc, vctx.profile
	part := vctx.part

	if part == 0 {
		return nil
	}

	doc.Logger.Debug("compliance.graphics: starting checks", "profile", profile, "part", part)

	var results []Violation

	// A graphics state written as an indirect object, and a halftone, is
	// judged once for the whole document; see [checkExtGStateGraphics].
	judgedGS := make(map[int]bool)
	// So is every content stream the pages draw besides their own content.
	hexStrings := newDrawnStreamHexStrings(profile)
	drawn := newDrawnStreamWalk(vctx, func(s *drawnStream) {
		results = append(results, checkDrawnStream(ctx, doc, s, part, hexStrings)...)
		scope := appearanceResources(ctx, doc, s.resources, s.pageRes)
		results = append(results, checkExtGStateGraphics(ctx, doc, scope, collectDrawnResources(s.ops), profile, part, s.pageNum, judgedGS)...)
	})

	for i := range vctx.pages {
		p := &vctx.pages[i]
		if ctx.Err() != nil {
			return results
		}

		res := p.resources
		// A content stream an earlier page shares under the same resources is
		// judged there alone; see [markJudgedContents].
		hasContent := len(p.content) > 0 && !p.contentJudged

		// §6.2.2 t2/t3: resource completeness; see [pageResourceScopes].
		var resourceResults []Violation
		if hasContent {
			ops := p.parsedOps(ctx)
			if ops != nil {
				ownRes, inheritedRes := pageResourceScopes(p)
				built := drawn.scan(ctx, ops, res, inheritedCSDefault(), false).built
				// A page's Default colour spaces are those of the resources it
				// uses, its own or inherited ones (ISO 32000-1, 7.7.3.4).
				defaultUndefined := ""
				if missingResourceRule(part).Specification != "" {
					defaultRes := ownRes
					if defaultRes == nil {
						defaultRes = inheritedRes
					}
					defaultUndefined = unreadableDefaultSelected(ctx, doc, ops, defaultRes, ownRes, inheritedRes, true, false)
				}
				resourceResults = checkContentStreamResourcesOps(ctx, doc, ops, built, false, ownRes, inheritedRes, part, i+1, false, defaultUndefined)
				results = append(results, resourceResults...)
				// Check inline image /Intent values from parsed BI operations,
				// which are the stream's bytes, judged on the first page that
				// shares it whatever the resources.
				if !p.contentBytesJudged {
					results = append(results, checkInlineImageIntent(ops, part, i+1)...)
				}
				// The inline image filters of a page's content are stated by
				// checkPageContent, over the same operations.
			}
		}
		// §6.2.2 t3 once for a content stream with no resource dictionary at
		// all, unless a name it uses stated it already; see
		// [pageWithoutResources].
		if rule := missingResourceRule(part); rule.Specification != "" && !p.contentJudged &&
			!slices.ContainsFunc(resourceResults, func(v Violation) bool { return v.Rule == rule }) &&
			pageWithoutResources(ctx, doc, p) {
			results = append(results, failf(rule, "page %d: content stream has no explicitly associated Resources dictionary: neither the page nor an ancestor states /Resources", i+1)...)
		}

		// The graphics states the page's own content sets. A page whose
		// content litho cannot read has every state its resources list judged
		// once, rather than none (see [pageDrawnResources]).
		if !p.contentJudged {
			results = append(results, checkExtGStateGraphics(ctx, doc, res, pageDrawnResources(ctx, p), profile, part, i+1, judgedGS)...)
		}

		// The form XObjects, tiling patterns, Type3 glyphs and annotation
		// appearances the page draws, and whatever those draw in turn, with
		// the graphics states each of them sets.
		drawn.walkPage(ctx, p, i+1)
	}

	doc.Logger.Debug("compliance.graphics: checks complete", "violations", len(results))
	return results
}

// pageResourceScopes returns the resource dictionaries a page's content
// resolves its names against for [checkContentStreamResourcesOps]: its own
// /Resources, and the one an ancestor in the page tree lends it.
//
// ISO 32000-1, 7.7.3.4 makes /Resources inheritable: a page with none of its
// own uses the nearest ancestor's, and a page with its own uses those alone.
// Every name the first resolves is an inherited reference, ISO 19005-2/3/4,
// 6.2.2 t2, and a name the second's dictionary lacks is undefined, ISO
// 19005-4, 6.2.2 t3, whatever an ancestor defines. Measured with veraPDF 1.30
// at PDF/A-2b and PDF/A-4 on one-page fixtures whose page tree defines /GS0:
// "/GS0 gs" fails t2 on a page with no /Resources, and fails nothing at
// PDF/A-2b and t3 at PDF/A-4 on a page whose /Resources hold no /ExtGState or
// another ExtGState name.
//
// # Corpus evidence
//
//   - pdfjs issue5954.pdf: its page's /Resources lack the /F1 its content
//     selects, which the page tree defines, and veraPDF fails no t2 at
//     PDF/A-2b, where litho failed it once. It is the only file whose count
//     moves.
func pageResourceScopes(p *pageCtx) (own, inherited ast.PdfDict) {
	if _, _, ancestor := p.inheritedHolder("Resources"); ancestor != nil {
		return nil, p.resources
	}
	return p.resources, nil
}

// pageWithoutResources reports whether p has a content stream and no resource
// dictionary at all: its /Contents is a stream or an array, and neither the
// page nor any ancestor in the page tree states /Resources
// ([pageObject.inheritedHolder]; an entry written null states none, ISO
// 32000-1, 7.3.7).
//
// ISO 32000-1, 7.7.3.3, Table 30 (ISO 32000-2, Table 31) makes /Resources a
// required, inheritable entry of a page, and a page that needs no resources
// states an empty dictionary. ISO 19005-4, 6.2.2 requires every content stream
// to have an explicitly associated resource dictionary enumerating the named
// resources it uses, and a page with none leaves its content with no
// dictionary to associate. veraPDF fails t3 once for that stream, whatever it
// paints: it resolves the stream's initial DeviceGray (ISO 32000-1, 8.6.8)
// through the resource dictionary when it starts the stream's graphics state,
// and records the name as undefined when there is none (veraPDF-validation
// 1.30, GraphicState and PDResourcesHandler.getColorSpace), so its finding
// names DeviceGray. PDF/A-1, -2 and -3 have no such test.
//
// Measured with veraPDF 1.30.2 at PDF/A-4 on fixtures with no /Resources on
// the pages or the page tree: content painting nothing, painting in
// DeviceGray, DeviceRGB or DeviceCMYK, with or without an OutputIntent, an
// empty stream, one holding a comment, one that does not decode, an empty
// /Contents array and one holding null each fail t3 once, as do a page and a
// page tree stating /Resources null; a stream that also names a font fails it
// once, not twice; two pages sharing one stream fail it once, and two pages
// with a stream each fail it twice. A page with no /Contents, or a /Contents
// that is null or a dictionary, fails nothing, and so does a page stating an
// empty dictionary, inheriting one, stating a number, or naming an undefined
// object or a null one. An annotation appearance stream fails nothing through
// it: veraPDF gives a form XObject resources even when it states none.
//
// The same missing dictionary leads veraPDF astray once more, where litho
// keeps to the specification: "/DeviceRGB cs" selects DeviceRGB with no
// resource lookup (ISO 32000-1, 8.6.8, Table 74), while veraPDF resolves the
// name through the absent dictionary, leaves the colour space unset and judges
// the paint that follows as the initial DeviceGray. With no OutputIntent,
// "/DeviceRGB cs 1 0 0 sc" and a fill fail ISO 19005-2/3/4, 6.2.4.3 t2 twice
// on litho and t4 twice on veraPDF 1.30.2, and ISO 19005-1, 6.2.3.3 t1 against
// t3; rg, k and g fall back to their own family on veraPDF and agree.
//
// # Corpus evidence
//
//   - pdfcpu pkg_testdata__test.pdf, pkg_testdata__testRot.pdf and
//     pkg_testdata__testWithText.pdf, pdfjs clippath.pdf, issue4260_reduced.pdf
//     and issue4461.pdf: one page each has no resource dictionary, and
//     veraPDF fails t3 once at PDF/A-4, where litho reported nothing.
//   - pdfjs outline_goto_action.pdf and poppler-85140-0.pdf: two pages and one
//     have none, and litho fails t3 twice and once. veraPDF cannot parse
//     either file.
//   - litho noresources_4.pdf: five content streams on pages with no resource
//     dictionary fail t3, as veraPDF reports.
//   - All corpora: no other page has content and no resource dictionary, none
//     of these selects a device colour space with cs or CS, and no other count
//     moves at PDF/A-4.
func pageWithoutResources(ctx context.Context, doc *document.Document, p *pageCtx) bool {
	if node, _, _ := p.inheritedHolder("Resources"); node != nil {
		return false
	}
	contents, ok := p.dict["Contents"]
	if !ok {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, contents)
	if err != nil {
		return false
	}
	switch resolved.(type) {
	case ast.PdfStream, ast.PdfArray:
		return true
	}
	return false
}

// checkDrawnStream states the content-stream rules for one stream
// [drawnStreamWalk] reaches: a form XObject, a tiling pattern cell, a Type3
// glyph description or an annotation appearance stream.
//
// Each resolves resource names against its own /Resources first, a glyph
// description against its font's. Per ISO 19005-2, 6.2.2, a name found only in
// the page's resources is an inherited reference (t2), so the page's serve as
// the fallback scope; see [checkContentStreamResourcesOps]. An appearance
// stream is a content stream (ISO 32000-1, 12.5.5) and veraPDF resolves its
// names the same way: measured with veraPDF 1.30 at PDF/A-1b, PDF/A-2b and
// PDF/A-4, an appearance stream fails t2, t3, the ri test and the inline
// image intent test exactly as a form XObject the page paints does.
//
// The undefined-operator rule, ISO 19005-1, 6.2.10 t1 and ISO 19005-2/3/4,
// 6.2.2 t1, and the inline image filter rule ([checkInlineImageFilters]) are
// stated here for these streams, and by [checkPageContent] for a page's own
// content alone, so each is stated exactly once per undefined operator and
// per inline image. The operand limits are stated by
// [checkContentStreamLimits].
//
// # Corpus evidence
//
//   - pdfjs issue14438.pdf: runs /OC /MC0 BDC in 2 of its 19 appearance
//     streams, whose own /Resources carry no /Properties, and veraPDF fails
//     ISO 19005-4, 6.2.2 t3 for exactly those 2.
//   - All corpora: stating the inline image filter rule in these streams
//     changes no count at PDF/A-1b or PDF/A-2b, veraPDF files at their own
//     profile.
func checkDrawnStream(ctx context.Context, doc *document.Document, s *drawnStream, part int, hexStrings *drawnStreamHexStrings) []Violation {
	// A drawn stream's Default colour spaces are those of its own resources
	// alone (see [linkStream]), and only an appearance stream begins in the
	// initial state rather than inheriting its drawer's.
	defaultUndefined := ""
	if missingResourceRule(part).Specification != "" {
		defaultUndefined = unreadableDefaultSelected(ctx, doc, s.ops, s.resources, s.resources, s.pageRes, s.kind == drawnAppearance, s.colourOff)
	}
	results := checkContentStreamResourcesOps(ctx, doc, s.ops, s.patterns, s.colourOff, s.resources, s.pageRes, part, s.pageNum, true, defaultUndefined)
	results = append(results, hexStrings.check(s.format, s.pageNum)...)
	results = append(results, checkInlineImageIntent(s.ops, part, s.pageNum)...)
	results = append(results, checkInlineImageFilters(s.ops, part, s.pageNum)...)
	if s.kind == drawnAppearance {
		for i := range results {
			results[i].Context += " in annotation appearance"
		}
	}
	return results
}

// appearanceStreamObjects returns the appearance streams an /AP entry holds.
// The entry is a stream, or a subdictionary keyed by appearance state when the
// annotation has several (ISO 32000-1, 12.5.5), as a checkbox does with /Off
// and /Yes.
func appearanceStreamObjects(ctx context.Context, doc *document.Document, apObj ast.PdfObject) []ast.PdfObject {
	if _, err := document.ResolveStream(ctx, doc, apObj); err == nil {
		return []ast.PdfObject{apObj}
	}
	stateDict, err := document.ResolveDict(ctx, doc, apObj)
	if err != nil || stateDict == nil {
		return nil
	}
	out := make([]ast.PdfObject, 0, len(stateDict))
	for _, name := range sortedNames(stateDict) {
		out = append(out, stateDict[name])
	}
	return out
}

// appearanceResources returns the resource dictionary an appearance stream's
// names resolve against: its own /Resources, with the page's standing in for
// every name the stream does not define itself.
//
// The fallback is per name rather than per category, measured against veraPDF
// 1.30.2. Defining /MC0 on the page alone silences §6.2.2 t3, and so does
// giving the appearance stream a /Properties holding a different name while the
// page defines /MC0: the name resolves, through the page, and fails the
// inherited-resource test t2 instead (see [checkContentStreamResourcesOps]).
// ISO 32000-1, 12.5.5 leaves an appearance stream's resources to the stream, so
// the page fills the gaps rather than replacing them.
//
// # Corpus evidence
//
//   - pdfjs issue14438.pdf: the file both /MC0 measurements were made on.
func appearanceResources(ctx context.Context, doc *document.Document, streamRes, pageRes ast.PdfDict) ast.PdfDict {
	if streamRes == nil {
		return pageRes
	}
	if pageRes == nil {
		return streamRes
	}

	merged := make(ast.PdfDict, len(streamRes)+len(pageRes))
	maps.Copy(merged, streamRes)
	for category, pageEntry := range pageRes {
		streamEntry, ok := streamRes[category]
		if !ok {
			merged[category] = pageEntry
			continue
		}
		streamSub, err := document.ResolveDict(ctx, doc, streamEntry)
		if err != nil || streamSub == nil {
			continue
		}
		pageSub, err := document.ResolveDict(ctx, doc, pageEntry)
		if err != nil || pageSub == nil {
			continue
		}
		sub := make(ast.PdfDict, len(pageSub)+len(streamSub))
		maps.Copy(sub, pageSub)
		maps.Copy(sub, streamSub)
		merged[category] = sub
	}
	return merged
}

// checkExtGStateGraphics judges the graphics states the gs operators of one
// content stream set, whose names resolve against resources and which drawn
// counts, against the transfer function, halftone and rendering intent rules:
// ISO 19005-1, 6.2.8 t1 and t2 and 6.2.9 t1, ISO 19005-2/3, 6.2.5 t1 to t6
// and 6.2.6 t1, and ISO 19005-4, 6.2.5 t1 to t6.
//
// veraPDF builds a PDExtGState only from a gs operator (GFOp_gs,
// veraPDF-validation 1.30), so a state no gs sets is judged nowhere, and it
// judges one model object per indirect object for the whole run: a state
// written as an indirect object fails once for the whole document, and one
// written directly once for every gs that sets it ([blendModeTimes]). The
// halftone a state names is a model object of its own (GFPDHalftone), which
// an indirect /HT makes one for the whole document ([objectJudgedOnce]),
// whatever states and gs operators reach it. Measured with veraPDF 1.30 at
// PDF/A-1b, PDF/A-2b and PDF/A-4:
//
//   - A direct state holding /TR or /RI /Foo that two gs set fails 6.2.8 t1
//     or 6.2.9 t1 at PDF/A-1b, and 6.2.5 t1 or 6.2.6 t1 at PDF/A-2b, twice,
//     and a direct and an indirect state no gs sets fail nothing.
//   - An indirect state holding /TR2 /Foo set twice on one page and once on
//     another fails t2 once.
//   - A direct state holding a direct type 6 halftone that two gs set fails
//     6.2.5 t4 twice, an indirect one set on two pages holding one fails it
//     once, and two direct states naming one indirect type 6 halftone, each
//     set once, fail it once.
//
// The streams a page draws are reached by [drawnStreamWalk], each judged once
// for the whole document with the states it sets itself: a form painted twice
// setting a direct state once fails once, and so does an appearance two
// annotations share, a form painted by a form, a selected tiling pattern cell
// and a shown Type3 glyph, while a form no Do paints and a soft mask group
// fail nothing.
//
// The work is one read of every name drawn gives and one judgement of the
// state it names, whatever the count of gs operators setting it; only the
// findings repeat, so a stream of millions of gs operators costs time linear
// in its operators. A nil drawn, from a page whose content litho cannot read,
// judges every state resources list once (see [pageDrawnResources]).
//
// # Corpus evidence
//
//   - govdocs thread_100_100666.pdf: references ExtGState object 190 from 30
//     pages, and veraPDF reports its /TR once. It backs judging a shared
//     dictionary once.
//   - pdfjs issue21089.pdf: six graphics states name one indirect type 5
//     halftone whose process colorants carry transfer functions, and veraPDF
//     fails ISO 19005-2, 6.2.5 t6 four times, where litho failed it 24 times.
//   - govdocs thread_300_300255.pdf: its two pages list /GS13, holding /TR,
//     and never set it, and veraPDF fails 6.2.5 t1 twice, where litho failed
//     it four times.
//   - All corpora: judging a state where a gs sets it moved 19 counts at
//     PDF/A-1b, PDF/A-2b and PDF/A-4, in 7 files, all toward veraPDF's and
//     13 onto it. The 6 then still apart were 6.2.5 t6, which [halftoneJudge]
//     now states as veraPDF does.
func checkExtGStateGraphics(ctx context.Context, doc *document.Document, resources ast.PdfDict, drawn *drawnResources, profile string, part, pageNum int, judged map[int]bool) []Violation {
	gsDict, err := document.ResolveDict(ctx, doc, resources["ExtGState"])
	if err != nil || gsDict == nil {
		return nil
	}

	names := sortedNames(gsDict)
	if drawn != nil {
		names = names[:0]
		for _, name := range slices.Sorted(maps.Keys(drawn.extGStates)) {
			if _, ok := gsDict[ast.PdfName(name)]; ok {
				names = append(names, ast.PdfName(name))
			}
		}
	}

	var results []Violation
	for _, gsName := range names {
		if ctx.Err() != nil {
			return results
		}
		gsObj := gsDict[gsName]
		times := blendModeTimes(gsObj, drawn.gsCount(string(gsName)), judged)
		if times == 0 {
			continue
		}
		gs, err := document.ResolveDict(ctx, doc, gsObj)
		if err != nil || gs == nil {
			continue
		}

		found := checkTransferFunction(ctx, doc, gs, profile, part, pageNum, string(gsName))
		found = append(found, checkHTPKey(ctx, doc, gs, profile, part, pageNum, string(gsName))...)
		found = append(found, checkRenderingIntent(ctx, doc, gs, profile, part, pageNum, string(gsName))...)
		found = append(found, checkOverprint(ctx, doc, gs, profile, part, pageNum, string(gsName))...)
		for range times {
			results = append(results, found...)
		}
		results = append(results, checkHalftone(ctx, doc, gs, part, times, judged, "page %d, ExtGState %s", pageNum, string(gsName))...)
	}
	return results
}

// trForbiddenRule returns the rule for TR key being forbidden, varying by part.
func trForbiddenRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_8_t01, Rule_19005_2_6_2_5_t01, Rule_19005_3_6_2_5_t01, Rule_19005_4_6_2_5_t01)
}

// tr2NotDefaultRule returns the rule for TR2 not being /Default.
func tr2NotDefaultRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_8_t02, Rule_19005_2_6_2_5_t02, Rule_19005_3_6_2_5_t02, Rule_19005_4_6_2_5_t02)
}

func checkTransferFunction(ctx context.Context, doc *document.Document, gs ast.PdfDict, _ string, part, pageNum int, gsName string) []Violation {
	var results []Violation

	// PDF/A-1: /TR key is completely forbidden.
	// PDF/A-2+/4: /TR key is also forbidden. A /TR written as null is absent
	// (ISO 32000-1, 7.3.7) and one naming any object counts, as veraPDF's
	// knownKey reads it ([knownKey]). Measured.
	if knownKey(gs, "TR") {
		results = append(results, failf(trForbiddenRule(part), "page %d, ExtGState %s", pageNum, gsName)...)
	}

	// PDF/A-1: /TR2 check uses same rule (6.2.8 t02).
	// PDF/A-2+/4: /TR2 must be /Default if present.
	if tr2Obj, hasTR2 := gs["TR2"]; hasTR2 {
		tr2Name, _ := document.ResolveName(ctx, doc, tr2Obj)
		if tr2Name != "Default" {
			results = append(results, failf(tr2NotDefaultRule(part), "page %d, ExtGState %s", pageNum, gsName)...)
		}
	}

	return results
}

// htpForbiddenRule returns the rule for HTP key being forbidden.
func htpForbiddenRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_5_t03, Rule_19005_3_6_2_5_t03, Rule_19005_4_6_2_5_t03)
}

func checkHTPKey(_ context.Context, _ *document.Document, gs ast.PdfDict, _ string, part, pageNum int, gsName string) []Violation {
	// The forbidden key differs by part: ISO 19005-2 and -3 §6.2.5 t3 read
	// "containsHTP == false", while ISO 19005-4 §6.2.5 t3 reads
	// "containsHTO == false". Checking both keys under every part reported
	// /HTP against part 4 and /HTO against parts 2 and 3, neither of which
	// the governing rule constrains.
	key := ast.PdfName("HTP")
	if part == 4 {
		key = ast.PdfName("HTO")
	}
	if part < 2 {
		return nil
	}
	if _, has := gs[key]; has {
		return failf(htpForbiddenRule(part), "page %d, ExtGState %s: %s key forbidden", pageNum, gsName, key)
	}
	return nil
}

// halftoneTypeRule returns the rule for invalid HalftoneType.
func halftoneTypeRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_5_t04, Rule_19005_3_6_2_5_t04, Rule_19005_4_6_2_5_t04)
}

// halftoneNameRule returns the rule for forbidden HalftoneName.
func halftoneNameRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_5_t05, Rule_19005_3_6_2_5_t05, Rule_19005_4_6_2_5_t05)
}

// halftoneTransferRule returns the rule for non-Default TransferFunction.
func halftoneTransferRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_5_t06, Rule_19005_3_6_2_5_t06, Rule_19005_4_6_2_5_t06)
}

// checkHalftone judges the halftone the graphics state gs names with /HT,
// which times gs operators set, against ISO 19005-2/3, 6.2.5 t4 to t6 and ISO
// 19005-4, 6.2.5 t4 to t6: a halftone type of 1 or 5, no /HalftoneName, and a
// /TransferFunction only where ISO 32000-1, 10.5.5.2, Table 130 requires one.
// format and args locate the state in each violation's context.
//
// veraPDF builds a PDHalftone from an /HT that is a name, a dictionary or a
// stream (PDExtGState.getHalftone, veraPDF-validation 1.30), and a type 5
// halftone gives one more for every entry whose value is a dictionary or a
// stream, each judged under its key as the colorant name
// (GFPDHalftone.getHalftones), and so on down; see [halftoneJudge]. A halftone
// written as an indirect object is judged once for the whole document, which
// judged records, whatever states, gs operators and type 5 halftones reach
// it, and one written directly once for every gs that sets the state holding
// it. Measured with veraPDF 1.30 at PDF/A-2b: a type 5 halftone written
// directly in a state two gs set fails for its direct components twice, and
// for a component it names as an indirect object once.
func checkHalftone(ctx context.Context, doc *document.Document, gs ast.PdfDict, part, times int, judged map[int]bool, format string, args ...any) []Violation {
	if part < 2 {
		return nil
	}
	htObj, ok := gs["HT"]
	if !ok || objectJudgedOnce(htObj, judged) {
		return nil
	}
	h := halftoneJudge{doc: doc, part: part, judged: judged, where: fmt.Sprintf(format, args...)}
	_, indirect := htObj.(ast.PdfIndirectRef)
	h.judge(ctx, htObj, halftoneComponent{}, !indirect, 0)
	results := h.once
	for range times {
		results = append(results, h.each...)
	}
	return results
}

// maxHalftoneNesting caps how deep [halftoneJudge] follows type 5 halftones
// named as components of type 5 halftones. Anti-DoS guard, not spec-mandated:
// ISO 32000-1, 10.5.5.6, Table 131 lets a type 5 halftone hold halftones of
// types 1, 6, 10 and 16 alone, so a conforming file nests none, and a cycle
// of indirect halftones already ends at the judged set.
const maxHalftoneNesting = 8

// halftoneCMYKColorants are the colorant names under which a type 5 halftone
// component may not carry a /TransferFunction: the four process colorants of a
// CMYK device, whose transfer functions the graphics state supplies (ISO
// 32000-1, 10.5.5.2, Table 130). Under any other name but Default one is
// required.
//
//nolint:gochecknoglobals // immutable lookup set.
var halftoneCMYKColorants = map[ast.PdfName]bool{"Cyan": true, "Magenta": true, "Yellow": true, "Black": true}

// halftoneJudge judges one halftone and the components a type 5 halftone
// names, as veraPDF 1.30 judges its PDHalftone objects (GFPDHalftone and the
// PDHalftone rules of its PDF/A-2, PDF/A-3 and PDF/A-4 profiles):
//
//   - A halftone named /Default reads as type 1 and any other name as having
//     no type and naming itself as its /HalftoneName, so it fails t4 and t5.
//     A dictionary or stream reads /HalftoneType as an integer, a missing one
//     failing t4 like any type but 1 and 5, a type 6 stream included.
//   - t6 reads /TransferFunction as absent when it is null or names a null
//     object. The halftone the state names has no colorant, so any other
//     value fails, /Default included. A component named Default passes
//     whatever it holds, one named Cyan, Magenta, Yellow or Black fails with a
//     transfer function, and one under any other name fails without one.
//   - Every entry of a type 5 halftone whose value is a dictionary or a stream
//     is a component, whatever its key: a /TransferFunction written as a
//     function dictionary is judged as a halftone of colorant
//     TransferFunction, which has no type and no transfer function.
//   - veraPDF lists the components in the iteration order of the Java HashMap
//     its COSDictionary keeps ([javaHashMapOrder]) and reaches them from the
//     last, judging a component written as an indirect object where it first
//     reaches it. Several colorants naming one object are judged once, under
//     the name reached first.
//
// Measured with veraPDF 1.30 at PDF/A-2b and PDF/A-4, each on a page of its
// own: /HT /Foo fails t4 and t5, /HT /Default nothing; a type 1 halftone with
// /TransferFunction /Default or /Identity fails t6, with a null or a reference
// to a null object nothing, and without /HalftoneType t4; a type 6 stream
// fails t4; a type 5 halftone with a type 6 stream component under /Spot fails
// t4 and t6 for it, a Default component with no type t4, and a
// /TransferFunction dictionary t6 for the halftone and t4 and t6 as a
// component; /Blue and /Yellow written in that order, naming one component
// with no transfer function, fail nothing, and written the other way round
// fail t6 once; a component with /HalftoneName fails t5; a type 5 component
// holding /Red with no transfer function fails t6 for /Red; and a /Red and a
// /Cyan component whose transfer function is null fail t6 for /Red alone.
//
// # Corpus evidence
//
//   - govdocs thread_100_100518.pdf: six type 5 halftones in which Red and
//     Cyan, Green and Magenta, Blue and Yellow, and Gray, Black and Default
//     share a component each, and veraPDF fails t6 12 times, for Green and
//     Blue, where litho failed it 24 times, once per non-process colorant.
//   - govdocs thread_400_400570.pdf: the same halftone with Blue written
//     before Yellow, where veraPDF fails t6 for Green alone.
//   - All corpora: judging halftones as veraPDF does moved 10 counts of t6 at
//     PDF/A-2b and PDF/A-4, in 5 govdocs files, each onto veraPDF's count,
//     and no other count at PDF/A-1b, PDF/A-2b or PDF/A-4. No file reaches a
//     halftone that is a name or a stream, or a top-level transfer function,
//     which litho paintop_2b.pdf plants.
type halftoneJudge struct {
	doc    *document.Document
	part   int
	judged map[int]bool
	where  string
	// once holds the findings of the halftones judged once for the whole
	// document, and each those of the ones judged at every gs setting the
	// state.
	once, each []Violation
}

// halftoneComponent names the colorant a type 5 halftone lists a halftone
// under. The zero value is the halftone a graphics state names, which has
// none.
type halftoneComponent struct {
	colorant ast.PdfName
	listed   bool
}

// judge judges the halftone obj holds, listed under component, and the
// components of a type 5 halftone in turn. repeat reports that the halftone
// has no object of its own and is judged at every gs setting the state (see
// [checkHalftone]).
func (h *halftoneJudge) judge(ctx context.Context, obj ast.PdfObject, component halftoneComponent, repeat bool, nesting int) {
	resolved, err := document.ResolveObject(ctx, h.doc, obj)
	if err != nil {
		return
	}
	var dict ast.PdfDict
	var named ast.PdfName
	isName := false
	switch v := resolved.(type) {
	case ast.PdfName:
		named, isName = v, true
	case ast.PdfDict:
		dict = v
	case ast.PdfStream:
		dict = v.Dict
	default:
		return
	}
	add := func(rule Rule, detail string) {
		where := h.where
		if component.listed {
			where += ", halftone component " + string(component.colorant)
		}
		v := failf(rule, "%s%s", where, detail)
		if repeat {
			h.each = append(h.each, v...)
		} else {
			h.once = append(h.once, v...)
		}
	}

	htType, typed := halftoneType(ctx, h.doc, dict, named, isName)
	if !typed || (htType != 1 && htType != 5) {
		add(halftoneTypeRule(h.part), ", no HalftoneType of 1 or 5")
	}
	if (isName && named != "Default") || (dict != nil && nonNullKeyPresent(ctx, h.doc, dict, "HalftoneName")) {
		add(halftoneNameRule(h.part), ", HalftoneName")
	}
	if component.colorant != "Default" {
		hasTF := dict != nil && nonNullKeyPresent(ctx, h.doc, dict, "TransferFunction")
		switch process := !component.listed || halftoneCMYKColorants[component.colorant]; {
		case process && hasTF:
			add(halftoneTransferRule(h.part), ", TransferFunction")
		case !process && !hasTF:
			add(halftoneTransferRule(h.part), ", no TransferFunction")
		}
	}

	if !typed || htType != 5 || dict == nil || nesting >= maxHalftoneNesting {
		return
	}
	components := halftoneComponents(ctx, h.doc, obj, dict)
	for i := len(components) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			return
		}
		key := components[i]
		sub := dict[key]
		listed := halftoneComponent{colorant: key, listed: true}
		if _, indirect := sub.(ast.PdfIndirectRef); indirect {
			if objectJudgedOnce(sub, h.judged) {
				continue
			}
			h.judge(ctx, sub, listed, false, nesting+1)
			continue
		}
		h.judge(ctx, sub, listed, repeat, nesting+1)
	}
}

// halftoneType returns the type of the halftone whose dictionary is dict, or
// which the name named stands for when isName is set, and false when it has
// none: /Default reads as type 1 and any other name as none, and
// /HalftoneType as an integer, a real read as its integer part.
func halftoneType(ctx context.Context, doc *document.Document, dict ast.PdfDict, named ast.PdfName, isName bool) (int64, bool) {
	if isName {
		return 1, named == "Default"
	}
	resolved, err := document.ResolveObject(ctx, doc, dict["HalftoneType"])
	if err != nil {
		return 0, false
	}
	n, ok := document.Number(resolved)
	if !ok {
		return 0, false
	}
	return int64(n), true
}

// halftoneComponents returns the keys of the type 5 halftone dict, which obj
// holds, whose values are dictionaries or streams, in the order veraPDF lists
// them ([javaHashMapOrder]). The order depends on the one the file writes the
// keys in, which [document.Document.DictKeyOrder] reads for a halftone
// written as an indirect object; a halftone written directly, or one whose
// bytes do not give back the keys it holds, is ordered as if the file wrote
// its keys sorted.
func halftoneComponents(ctx context.Context, doc *document.Document, obj ast.PdfObject, dict ast.PdfDict) []ast.PdfName {
	present := make(map[ast.PdfName]bool, len(dict))
	for key, value := range dict {
		if !resourceEntryNull(ctx, doc, value) || isReference(value) {
			present[key] = true
		}
	}
	var keys []ast.PdfName
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		keys = doc.DictKeyOrder(ctx, ref.ObjectNum)
	}
	if !sameKeySet(keys, present) {
		keys = slices.Sorted(maps.Keys(present))
	}
	var components []ast.PdfName
	for _, key := range javaHashMapOrder(keys) {
		switch v, _ := document.ResolveObject(ctx, doc, dict[key]); v.(type) {
		case ast.PdfDict, ast.PdfStream:
			components = append(components, key)
		}
	}
	return components
}

// isReference reports whether obj is an indirect reference.
func isReference(obj ast.PdfObject) bool {
	_, ok := obj.(ast.PdfIndirectRef)
	return ok
}

// sameKeySet reports whether keys lists exactly the names set holds.
func sameKeySet(keys []ast.PdfName, set map[ast.PdfName]bool) bool {
	if len(keys) != len(set) {
		return false
	}
	for _, key := range keys {
		if !set[key] {
			return false
		}
	}
	return true
}

// javaHashMapOrder returns keys, the keys of a dictionary in the order the
// file writes them, in the order veraPDF iterates them: its COSDictionary
// keeps its entries in a java.util.HashMap built with the default capacity
// and filled in file order, and an ASAtom hashes as its name string does
// (ASAtom.hashCode, veraPDF core 1.30).
//
// A HashMap iterates its table from the first bucket to the last, and the
// entries of one bucket in the order they were inserted. The table holds 16
// buckets and doubles whenever the entries outnumber three quarters of it,
// and an entry's bucket is its hash with the high half folded into the low
// one, masked to the table size (java.util.HashMap.hash). A bucket that grows
// past eight entries turns into a tree, which this does not model.
func javaHashMapOrder(keys []ast.PdfName) []ast.PdfName {
	buckets := javaHashMapInitialCapacity
	for len(keys) > buckets/4*3 {
		buckets *= 2
	}
	mask := uint32(buckets - 1) //nolint:gosec // G115: buckets is a power of two no larger than twice the key count.
	out := slices.Clone(keys)
	slices.SortStableFunc(out, func(a, b ast.PdfName) int {
		return cmp.Compare(javaHashSpread(javaStringHash(string(a)))&mask, javaHashSpread(javaStringHash(string(b)))&mask)
	})
	return out
}

// javaHashMapInitialCapacity is the table size of a java.util.HashMap built
// with its default constructor.
const javaHashMapInitialCapacity = 16

// javaStringHash returns java.lang.String.hashCode of s read as ISO 8859-1,
// one character per byte, which is how a name made only of ASCII characters
// reads in any encoding.
func javaStringHash(s string) uint32 {
	var h uint32
	for i := range len(s) {
		h = 31*h + uint32(s[i])
	}
	return h
}

// javaHashSpread folds the high half of a Java hash code into the low half,
// as java.util.HashMap.hash does before masking it to a bucket.
func javaHashSpread(h uint32) uint32 {
	return h ^ (h >> 16) //nolint:mnd // the fold is by half of 32 bits.
}

// validRenderingIntents lists the four standard PDF rendering intents.
var validRenderingIntents = map[string]bool{ //nolint:gochecknoglobals // constant lookup table.
	"RelativeColorimetric": true,
	"AbsoluteColorimetric": true,
	"Perceptual":           true,
	"Saturation":           true,
}

// renderingIntentRule returns the rule for invalid rendering intent.
func renderingIntentRule(part int) Rule {
	// PDF/A-4 has no separate rendering intent rule.
	return ruleByPart(part, Rule_19005_1_6_2_9_t01, Rule_19005_2_6_2_6_t01, Rule_19005_3_6_2_6_t01, Rule{})
}

func checkRenderingIntent(ctx context.Context, doc *document.Document, gs ast.PdfDict, _ string, part, pageNum int, gsName string) []Violation {
	// PDF/A-4 does not define a rendering intent rule.
	if part == 4 {
		return nil
	}

	riObj, hasRI := gs["RI"]
	if !hasRI {
		return nil
	}

	ri, _ := document.ResolveName(ctx, doc, riObj)
	if ri == "" {
		return nil
	}

	if !validRenderingIntents[string(ri)] {
		return failf(renderingIntentRule(part), "page %d, ExtGState %s, intent %s", pageNum, gsName, ri)
	}
	return nil
}

func checkOverprint(ctx context.Context, doc *document.Document, gs ast.PdfDict, _ string, part, _ int, _ string) []Violation {
	// Overprint checks apply to parts 2+ and 4 only.
	if part < 2 {
		return nil
	}

	opmObj, hasOPM := gs["OPM"]
	if !hasOPM {
		return nil
	}

	opm, err := document.ResolveInt(ctx, doc, opmObj)
	if err != nil {
		doc.Logger.Debug("compliance.graphics: cannot resolve OPM value", "error", err)
		return nil
	}

	// OPM shall not be 1 when overprint is enabled in certain conditions.
	// Only flag when overprint stroke (OP) or overprint fill (op) is true.
	if opm != 1 {
		return nil
	}

	// When OPM is 1, check whether overprint is active. If OP or op keys
	// are not present, overprint defaults to false and OPM=1 is harmless.
	_, hasOP := gs["OP"]
	_, hasop := gs["op"]
	if !hasOP && !hasop {
		return nil
	}

	// PDF/A parts 2-4 define no standalone rule for OPM=1; the restriction is
	// expressed through colour-space-specific overprint validation, not a bare
	// OPM check. Emitting a violation here without a matching rule would be a
	// false positive, so OPM=1 with active overprint is left to those checks.

	return nil
}
