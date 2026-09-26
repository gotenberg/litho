package compliance

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// checkTransparencyGroup validates transparency restrictions required by
// PDF/A parts 1-4. PDF/UA profiles have no transparency checks.
func checkTransparencyGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile
	part := vctx.part

	if part == 0 {
		return nil
	}

	doc.Logger.Debug("compliance.transparency: starting checks", "profile", profile, "part", part)

	var results []Violation
	if part == 1 {
		results = checkTransparencyPart1(ctx, doc, vctx.pages)
	} else {
		results = checkTransparencyParts2Plus(ctx, doc, catalog, vctx.pages, profile, part)
	}

	doc.Logger.Debug("compliance.transparency: checks complete", "violations", len(results))
	return results
}

// checkTransparencyPart1 applies the ISO 19005-1, 6.4 transparency rules to
// each page: its own /Group, what its content stream draws, its annotation
// appearances, and what every content stream [drawnStreamWalk] reaches from
// them draws.
//
// A page whose content stream an earlier page shares draws nothing more, since
// veraPDF judges that stream on the first page alone, with what it draws (see
// [markJudgedContents]). Measured with veraPDF 1.30 at PDF/A-1b: two pages
// sharing one stream that paints a soft masked image and a form with a
// transparency group and sets two graphics states with constant alpha fail
// 6.4 t2, t3, t5 and t6 once each, as one of the pages alone does.
//
// veraPDF judges an object it reaches again only the first time, and the
// survey states each rule as often. Measured the same way:
//
//   - A soft masked image and a graphics state written as an indirect object
//     fail 6.4 t2 and the graphics state rules once for the whole document,
//     however many pages, forms or Do and gs operators draw them.
//   - A graphics state written directly in a resource dictionary has no
//     object of its own, and fails once for every gs that sets it in a
//     content stream, which is itself judged once.
//   - A form's own /SMask, and a transparency /Group written directly in it,
//     fail once for every Do that paints it, and an annotation appearance's
//     once for every annotation. A group written as an indirect object fails
//     once for the whole document; see [transparencyGroup1].
//   - The streams a form, a pattern or a glyph description draws, and an
//     appearance stream's, are judged once for the whole document: a form two
//     pages paint, or an appearance two annotations share, fails the rules of
//     what it draws once.
//
// The forms, tiling patterns, Type3 glyph descriptions and appearance streams
// are reached as veraPDF reaches them, which [drawnStreamWalk] follows: a
// stream only through the operator that draws it, and a name it uses through
// its own resources first and the page's then. Measured the same way:
//
//   - A form with no /Resources, or with resources lacking the name, that
//     sets a graphics state only the page defines fails t4 once, and t1, t5
//     and t6 once each for a state with a soft mask and constant alpha. A
//     tiling pattern, a Type3 glyph description or an appearance stream with
//     no resources fails t4 the same way.
//   - A form with no /Resources painting a soft masked image or a form with a
//     transparency group only the page defines fails t2 or t3 once, and one
//     painting a form only the page defines fails what that form draws.
//   - A tiling pattern no scn selects and a Type3 font no text is shown with
//     fail nothing, whatever graphics states they set, and a Type3 font whose
//     resources define two states fails t4 for the one its glyph sets alone.
//   - A graphics state set only by the soft mask group a graphics state names,
//     or named only by a shading pattern, fails nothing.
//   - A content stream that is also the form its page paints is judged as the
//     page's content alone: one setting a state with a soft mask and painting
//     a form with a transparency group, each defined by both the page and the
//     form's own resources, fails t1 and t3 once each.
//
// judged holds the image XObjects, graphics states and transparency groups
// judged so far.
//
// # Corpus evidence
//
//   - All corpora: 104 counts of ISO 19005-1, 6.4 t1 to t6 at PDF/A-1b moved
//     toward veraPDF's and none away, 103 of them onto it, when litho stopped
//     judging each object once per page, per resource dictionary and per
//     annotation; their summed distance from veraPDF fell from 18171 to 6.
//   - govdocs thread_000_000146.pdf: names the same indirect graphics states
//     from the resources of many of its 163 forms, and veraPDF fails 6.4 t4,
//     t5 and t6 two or three times, where litho failed them 19 or 20 times.
//   - All corpora: reaching the streams a page draws as [drawnStreamWalk]
//     does moved 4 counts of 6.4 t1 to t3 at PDF/A-1b, all onto veraPDF's,
//     and no other count at PDF/A-1b or PDF/A-2b, veraPDF files at their own
//     profile.
//   - pdfjs issue19634.pdf: the resources of its Type3 fonts declare the forms
//     their glyphs use as soft masks, each with a transparency group, and
//     litho, surveying those resources whole, failed t3 30 times, where
//     veraPDF fails it nowhere.
//   - pdfjs issue18466.pdf: declares a tiling pattern that paints a soft
//     masked image, and no scn selects it: veraPDF fails t2 twice, where
//     litho failed it three times.
//   - pdfjs issue18894.pdf: the content stream of its page is also the form
//     the page paints, and litho judged what the stream draws once more
//     through the form's own resources, failing t1 and t3 twice each, where
//     veraPDF fails them once.
func checkTransparencyPart1(ctx context.Context, doc *document.Document, pages []pageCtx) []Violation {
	var results []Violation
	judged := make(map[int]bool)
	walk := newDrawnStreamWalk(&validationCtx{doc: doc, pages: pages}, func(s *drawnStream) {
		scope := appearanceResources(ctx, doc, s.resources, s.pageRes)
		drawn := collectDrawnResources(s.ops)
		results = append(results, checkExtGStateTransparency1(ctx, doc, scope, s.pageNum, drawn, judged)...)
		results = append(results, checkXObjectTransparency1(ctx, doc, scope, s.pageNum, drawn, judged)...)
	})
	for i := range pages {
		page := pages[i].dict
		if ctx.Err() != nil {
			return results
		}

		results = append(results, checkPageTransparencyGroup1(ctx, doc, page, i+1, judged)...)

		res := pages[i].resources
		if res != nil && !pages[i].contentJudged {
			drawn := pageDrawnResources(ctx, &pages[i])
			results = append(results, checkExtGStateTransparency1(ctx, doc, res, i+1, drawn, judged)...)
			results = append(results, checkXObjectTransparency1(ctx, doc, res, i+1, drawn, judged)...)
		}

		results = append(results, checkAnnotAPTransparency1(ctx, doc, page, i+1, judged)...)
		walk.walkPage(ctx, &pages[i], i+1)
	}
	return results
}

// checkAnnotAPTransparency1 applies the ISO 19005-1, 6.4 soft mask and group
// checks to the appearance streams of each annotation on a page. An appearance
// stream is a form XObject that paints the annotation, so a transparency group
// or a soft mask on it is rendered content. Its keys are judged once for every
// annotation that shows it, and what it draws once for the whole document, by
// [drawnStreamWalk]; see [checkTransparencyPart1].
//
// veraPDF reaches the normal, rollover and down appearances alone (ISO
// 32000-1, 12.5.5, Table 168): measured with veraPDF 1.30 at PDF/A-1b, a form
// with a transparency group under an /AP key other than /N, /R and /D fails
// 6.4 t3 nowhere.
func checkAnnotAPTransparency1(ctx context.Context, doc *document.Document, page ast.PdfDict, pageNum int, judged map[int]bool) []Violation {
	annots, err := document.ResolveArray(ctx, doc, page["Annots"])
	if err != nil || len(annots) == 0 {
		return nil
	}

	var results []Violation
	for _, annotObj := range annots {
		if ctx.Err() != nil {
			return results
		}
		annotDict, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil || annotDict == nil {
			continue
		}
		apDict, err := document.ResolveDict(ctx, doc, annotDict["AP"])
		if err != nil || apDict == nil {
			continue
		}
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			apObj, ok := apDict[key]
			if !ok {
				continue
			}
			for _, streamObj := range appearanceStreamObjects(ctx, doc, apObj) {
				stream, err := document.ResolveStream(ctx, doc, streamObj)
				if err != nil {
					continue
				}
				results = append(results, formXObjectTransparency1(ctx, doc, stream.Dict, pageNum, "annotation appearance", 1, judged)...)
			}
		}
	}
	return results
}

// sortedNames returns the keys of a dict in deterministic order.
func sortedNames(d ast.PdfDict) []ast.PdfName {
	names := make([]ast.PdfName, 0, len(d))
	for name := range d {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// sortedByteKeys returns the keys of a byte-keyed set in ascending order, so a
// check that reports the first matching character code is deterministic.
func sortedByteKeys(m map[byte]bool) []byte {
	keys := make([]byte, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// checkPageTransparencyGroup1 checks for forbidden transparency Group on a
// page (PDF/A-1); see [transparencyGroup1].
func checkPageTransparencyGroup1(ctx context.Context, doc *document.Document, page ast.PdfDict, pageNum int, judged map[int]bool) []Violation {
	return transparencyGroup1(ctx, doc, page["Group"], 1, judged, "page %d", pageNum)
}

// checkExtGStateTransparency1 checks all ExtGState entries for transparency
// (SMask, BM, CA, ca) which are forbidden in PDF/A-1.
//
// # Corpus evidence
//
//   - pdfjs issue20914.pdf: declares /GDIModeMask with a /Darken blend mode
//     and never sets it, and veraPDF reports only the /Transparent state a
//     form actually applies.
func checkExtGStateTransparency1(ctx context.Context, doc *document.Document, resources ast.PdfDict, pageNum int, drawn *drawnResources, judged map[int]bool) []Violation {
	gsDict, err := document.ResolveDict(ctx, doc, resources["ExtGState"])
	if err != nil || gsDict == nil {
		return nil
	}

	var results []Violation
	for _, gsName := range sortedNames(gsDict) {
		gsObj := gsDict[gsName]
		if ctx.Err() != nil {
			return results
		}
		// A resource dictionary lists what the content MAY set, not what it
		// sets; see [drawnResources]. veraPDF builds its PDExtGState from the
		// gs operator that selects it, so a state nothing selects is reported
		// nowhere.
		if !drawn.setsExtGState(string(gsName)) {
			continue
		}
		// A state written as an indirect object is judged once for the whole
		// document, and one written here once for every gs that sets it; see
		// [checkTransparencyPart1].
		times := drawn.gsCount(string(gsName))
		if ref, ok := gsObj.(ast.PdfIndirectRef); ok {
			if judged[ref.ObjectNum] {
				continue
			}
			judged[ref.ObjectNum] = true
			times = 1
		}

		gs, err := document.ResolveDict(ctx, doc, gsObj)
		if err != nil || gs == nil {
			continue
		}

		var found []Violation
		// SMask must be None or absent. veraPDF reads the key with knownKey
		// ([knownKey]): a direct null is absent (ISO 32000-1, 7.3.7), a reference
		// to a null object is an SMask that is not None. Measured.
		if smaskObj := gs["SMask"]; knownKey(gs, "SMask") {
			smaskName, _ := document.ResolveName(ctx, doc, smaskObj)
			if smaskName != "None" {
				found = append(found, failf(Rule_19005_1_6_4_t01, "page %d, ExtGState %s", pageNum, string(gsName))...)
			}
		}

		if bm, ok := blendModeForbidden1(ctx, doc, gs); ok {
			found = append(found, failf(Rule_19005_1_6_4_t04, "page %d, ExtGState %s, %s", pageNum, string(gsName), bm)...)
		}

		// CA (stroke alpha) must be 1.0.
		if caObj, hasCA := gs["CA"]; hasCA {
			ca := document.Float(caObj)
			if !floatEquals1(ca) {
				found = append(found, failf(Rule_19005_1_6_4_t05, "page %d, ExtGState %s, CA=%g", pageNum, string(gsName), ca)...)
			}
		}

		// ca (fill alpha) must be 1.0.
		if caObj, hasCa := gs["ca"]; hasCa {
			ca := document.Float(caObj)
			if !floatEquals1(ca) {
				found = append(found, failf(Rule_19005_1_6_4_t06, "page %d, ExtGState %s, ca=%g", pageNum, string(gsName), ca)...)
			}
		}
		for range times {
			results = append(results, found...)
		}
	}
	return results
}

// blendModeForbidden1 reports whether the /BM of the graphics state gs fails
// ISO 19005-1, 6.4 t4, which admits a blend mode key only with the value
// Normal or Compatible, and describes the value for the violation's context.
//
// veraPDF states the test as containsBM == false || BMNameValue == "Normal" ||
// BMNameValue == "Compatible", where containsBM reads the key as [knownKey]
// does and BMNameValue is the name /BM resolves to, null for anything else.
// ISO 32000-1, 11.6.3 also lets /BM be an array of blend modes, of which a
// viewer uses the first it recognises, but the test admits the name alone: an
// array fails it whatever it holds, [/Normal] included, and so does any other
// value that is no name. Measured with veraPDF 1.30 at PDF/A-1b: /BM
// [/Normal /Foo], [1 /Foo], [/Normal], [/Compatible], [], [5 0 R] where 5
// is /Normal, the empty name, 1, and references to null, to an undefined
// object and to [/Normal] fail t4 once each, and /Normal, a direct null and a
// reference to /Normal pass it.
//
// # Corpus evidence
//
//   - All corpora: failing every /BM that is no name changes no count at
//     PDF/A-1b.
//   - pdfjs issue11279.pdf: the one file that sets a graphics state whose /BM
//     is no name, and veraPDF fails t4 once for it, as litho did before.
func blendModeForbidden1(ctx context.Context, doc *document.Document, gs ast.PdfDict) (string, bool) {
	if !knownKey(gs, "BM") {
		return "", false
	}
	name, ok := resolveNameObject(ctx, doc, gs["BM"])
	switch {
	case !ok:
		return "BM is no name", true
	case name == "Normal" || name == "Compatible":
		return "", false
	}
	return "BM=" + string(name), true
}

// checkXObjectTransparency1 checks the XObjects a content stream paints for a
// forbidden SMask or transparency group (PDF/A-1).
//
// A resource dictionary lists what the content MAY paint; see
// [drawnResources]. veraPDF builds its PDXObject from the Do operator that
// paints one, so an XObject nothing paints is reported nowhere, and neither is
// anything its own resources hold, which [drawnStreamWalk] never reaches.
// Measured with veraPDF 1.30 at PDF/A-1b: a page declaring a soft
// masked image and a form with a transparency group, and painting neither,
// passes ISO 19005-1, 6.4 t2 and t3.
//
// # Corpus evidence
//
//   - pdfjs issue14847.pdf: declares two soft masked images on three pages
//     whose content paints no XObject, and veraPDF fails ISO 19005-1, 6.4 t2
//     nowhere, where litho failed it six times.
//   - pdfjs issue6961.pdf: each of its two pages declares 270 forms with a
//     transparency group and paints one or two, and litho failed ISO
//     19005-1, 6.4 t3 73172 times, where veraPDF fails it 272 times, as
//     litho now does.
//   - All corpora: 26 counts of ISO 19005-1, 6.4 t2, t3, t5 and t6 at
//     PDF/A-1b moved toward veraPDF's and none away, 19 of them onto it.
func checkXObjectTransparency1(ctx context.Context, doc *document.Document, resources ast.PdfDict, pageNum int, drawn *drawnResources, judged map[int]bool) []Violation {
	xobjDict, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err != nil || xobjDict == nil {
		return nil
	}

	var results []Violation
	for _, xoName := range sortedNames(xobjDict) {
		xoObj := xobjDict[xoName]
		if ctx.Err() != nil {
			return results
		}
		if !drawn.drawsXObject(string(xoName)) {
			continue
		}

		// XObjects are streams: ResolveDict rejects ast.PdfStream, which
		// left this check dead for every real Form/Image XObject. Resolve
		// the stream and use its dict; keep the dict fallback for
		// malformed producers.
		var xo ast.PdfDict
		if stream, err := document.ResolveStream(ctx, doc, xoObj); err == nil {
			xo = stream.Dict
		} else if d, derr := document.ResolveDict(ctx, doc, xoObj); derr == nil {
			xo = d
		}
		if xo == nil {
			continue
		}
		// An image is judged once for the whole document, and anything else
		// once for every Do that paints it; see [checkTransparencyPart1].
		times := drawn.doCount(string(xoName))
		if subtype, _ := document.ResolveName(ctx, doc, xo["Subtype"]); subtype == "Image" {
			if ref, ok := xoObj.(ast.PdfIndirectRef); ok {
				if judged[ref.ObjectNum] {
					continue
				}
				judged[ref.ObjectNum] = true
			}
			times = 1
		}
		results = append(results, formXObjectTransparency1(ctx, doc, xo, pageNum, "XObject "+string(xoName), times, judged)...)
	}
	return results
}

// formXObjectTransparency1 fires the PDF/A-1 §6.4 SMask (t2) and
// transparency-Group (t3) checks for a single XObject dictionary that times
// Do operators, or annotations, draw. label identifies the XObject in the
// violation context. The SMask check fails once for every draw, and the group
// check as [transparencyGroup1] says.
func formXObjectTransparency1(ctx context.Context, doc *document.Document, xo ast.PdfDict, pageNum int, label string, times int, judged map[int]bool) []Violation {
	var results []Violation

	// SMask key must not be present on XObjects: a direct null is absent, a
	// reference counts ([knownKey]). Measured.
	if knownKey(xo, "SMask") {
		for range times {
			results = append(results, failf(Rule_19005_1_6_4_t02, "page %d, %s", pageNum, label)...)
		}
	}

	// Transparency Group is forbidden on form XObjects.
	return append(results, transparencyGroup1(ctx, doc, xo["Group"], times, judged, "page %d, %s", pageNum, label)...)
}

// transparencyGroup1 fires ISO 19005-1, 6.4 t3 when groupObj is a
// transparency group, which times pages, Do operators or annotations reach.
//
// veraPDF judges a group written as an indirect object once for the whole
// document, which judged records, and a direct one at every reach. Measured
// with veraPDF 1.30 at PDF/A-1b: one indirect group that a form painted twice,
// two forms, or a page and the form it paints twice name fails t3 once, and a
// direct group on a form painted twice fails it twice.
//
// # Corpus evidence
//
//   - pdfjs issue6342.pdf: its page and the form it paints twice name one
//     indirect transparency group, and veraPDF fails t3 once.
func transparencyGroup1(ctx context.Context, doc *document.Document, groupObj ast.PdfObject, times int, judged map[int]bool, format string, args ...any) []Violation {
	if groupObj == nil {
		return nil
	}
	group, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || group == nil {
		return nil
	}
	if sName, _ := document.ResolveName(ctx, doc, group["S"]); sName != "Transparency" {
		return nil
	}
	if ref, ok := groupObj.(ast.PdfIndirectRef); ok {
		if judged[ref.ObjectNum] {
			return nil
		}
		judged[ref.ObjectNum] = true
		times = 1
	}
	var results []Violation
	for range times {
		results = append(results, failf(Rule_19005_1_6_4_t03, format, args...)...)
	}
	return results
}

// standardBlendModes lists all blend modes defined in ISO 32000-1:2008.
var standardBlendModes = map[string]bool{ //nolint:gochecknoglobals // constant lookup table.
	"Normal":     true,
	"Compatible": true,
	"Multiply":   true,
	"Screen":     true,
	"Overlay":    true,
	"Darken":     true,
	"Lighten":    true,
	"ColorDodge": true,
	"ColorBurn":  true,
	"HardLight":  true,
	"SoftLight":  true,
	"Difference": true,
	"Exclusion":  true,
	"Hue":        true,
	"Saturation": true,
	"Color":      true,
	"Luminosity": true,
}

// blendModeRule returns the rule for invalid blend mode, varying by part.
func blendModeRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_10_t01, Rule_19005_3_6_2_10_t01, Rule_19005_4_6_2_9_t01)
}

// blendingCSRule returns the rule for missing blending color space on
// transparent pages.
func blendingCSRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_10_t02, Rule_19005_3_6_2_10_t02, Rule_19005_4_6_2_9_t02)
}

// checkTransparencyParts2Plus applies the PDF/A-2, PDF/A-3, and PDF/A-4 page
// transparency rules: the blend mode an ExtGState names, and the blending
// colour space a page that uses transparency must have.
//
// A page whose content stream an earlier page shares sets no state through it,
// since veraPDF judges that stream on the first page alone (see
// [markJudgedContents]), but it still uses the transparency the stream paints.
// Measured with veraPDF 1.30 at PDF/A-2b: two pages sharing one stream that
// sets two states with a blend mode no table lists fail ISO 19005-2, 6.2.10
// t1 twice, as one of the pages alone does, and t2 once per page. The blend
// mode rule counts as ISO 19005-1, 6.4 does; see [blendModeTimes].
//
// The blend mode rule judges every graphics state veraPDF builds from a gs
// operator, in whichever content stream it stands: a page's own, and every
// form XObject, tiling pattern, Type3 glyph description and annotation
// appearance [drawnStreamWalk] reaches from it (see [drawnStreamBlendModes]).
// A form serving as a soft mask is not among them, since a gs selects it
// without drawing it, and neither is the graphics state of a shading pattern.
// Measured with veraPDF 1.30 at PDF/A-2b: a state naming a blend mode no table
// lists fails t1 once when a painted form, a nested form, a selected tiling
// pattern, a shown Type3 glyph or an appearance sets it, twice when a form
// sets a direct state twice, once when two pages paint the form or two
// annotations share the appearance, and never from a soft mask group or a
// shading pattern. PDF/A-4 also judges the /BM of an annotation (see
// [annotationBlendModes]).
//
// # Corpus evidence
//
//   - pdfjs issue6741.pdf: declares /G1 (/BM /Darken) and /G2 (/BM /Lighten),
//     sets only /G0 (/BM /Normal), carries no page /Group and no OutputIntent,
//     and veraPDF reports nothing for it while litho, surveying every declared
//     ExtGState, fired §6.2.10 t2.
//   - All corpora: judging the blend modes of the drawn streams and of
//     annotations changes no count at PDF/A-1b or PDF/A-2b, veraPDF files at
//     their own profile, where the 7 veraPDF files failing 6.2.10 t1 or 6.2.9
//     t1 already matched their golden counts.
func checkTransparencyParts2Plus(ctx context.Context, doc *document.Document, catalog ast.PdfDict, pages []pageCtx, profile string, part int) []Violation {
	hasOutputIntent, _ := documentOutputIntentInfo(ctx, doc, catalog)

	var results []Violation
	judged := make(map[int]bool)
	walk := newDrawnStreamWalk(&validationCtx{doc: doc, profile: profile, part: part, pages: pages}, func(s *drawnStream) {
		results = append(results, drawnStreamBlendModes(ctx, doc, s, part, judged)...)
	})
	for i := range pages {
		page := pages[i].dict
		if ctx.Err() != nil {
			return results
		}

		// The graphics states the page's forms, patterns, glyphs and
		// annotation appearances set, and the blend modes its annotations
		// name, answer to the blend mode rule as the page's own do.
		walk.walkPage(ctx, &pages[i], i+1)
		results = append(results, annotationBlendModes(ctx, doc, page, i+1, part, judged)...)

		res := pages[i].resources
		if res == nil {
			continue
		}

		pageUsesTransparency := false

		// A page with its own transparency Group is itself using transparency.
		if groupObj, hasGroup := page["Group"]; hasGroup {
			group, gErr := document.ResolveDict(ctx, doc, groupObj)
			if gErr == nil && group != nil {
				s, _ := document.ResolveName(ctx, doc, group["S"])
				if s == "Transparency" {
					pageUsesTransparency = true
				}
			}
		}

		// A resource dictionary lists what the content MAY set, not what it
		// sets, and veraPDF builds its PDExtGState from the gs operator that
		// selects one, so a state nothing selects contributes no transparency.
		// [checkExtGStateTransparency1] and [resourcesUseTransparency] already
		// scope themselves this way; this loop did not, and it runs first.
		// A nil drawn admits every name, which keeps a page whose content
		// litho cannot read unscoped rather than silently clean.
		drawn := pageDrawnResources(ctx, &pages[i])

		// Check blend modes in ExtGState.
		gsDict, err := document.ResolveDict(ctx, doc, res["ExtGState"])
		if err == nil && gsDict != nil {
			for _, gsName := range sortedNames(gsDict) {
				gsObj := gsDict[gsName]
				if ctx.Err() != nil {
					return results
				}
				if !drawn.setsExtGState(string(gsName)) {
					continue
				}

				gs, err := document.ResolveDict(ctx, doc, gsObj)
				if err != nil || gs == nil {
					continue
				}

				// A content stream an earlier page shares sets its states there
				// alone; see [markJudgedContents].
				if !pages[i].contentJudged {
					results = append(results, blendModeViolations(ctx, doc, gs, gsObj, drawn.gsCount(string(gsName)), part, judged,
						"page %d, ExtGState %s", i+1, string(gsName))...)
				}
				if bmObj, hasBM := gs["BM"]; hasBM && blendModeIsTransparency(resolveBlendMode(ctx, doc, bmObj)) {
					pageUsesTransparency = true
				}

				// SMask (other than None) indicates transparency. One that resolves
				// to null masks nothing (ISO 32000-1, 7.3.7 and 7.3.10): veraPDF sees
				// no transparency in either a null or a reference to a null object.
				// Measured, and so throughout the transparency detection here.
				if smaskObj, hasSMask := gs["SMask"]; hasSMask && !resourceEntryNull(ctx, doc, smaskObj) {
					smaskName, _ := document.ResolveName(ctx, doc, smaskObj)
					if smaskName != "None" {
						pageUsesTransparency = true
					}
				}

				// Non-1.0 alpha values indicate transparency.
				if caObj, hasCA := gs["CA"]; hasCA {
					if !floatEquals1(document.Float(caObj)) {
						pageUsesTransparency = true
					}
				}
				if caObj, hasCa := gs["ca"]; hasCa {
					if !floatEquals1(document.Float(caObj)) {
						pageUsesTransparency = true
					}
				}
			}
		}

		// Check XObjects for transparency, recursing into Form XObjects whose
		// own resources may nest soft-mask images, blend modes, or further
		// forms. veraPDF walks the full resource tree, so a page that carries
		// its transparency only inside a Form XObject still requires a blending
		// colour space (ISO 19005-2 §6.2.10 t2).
		if !pageUsesTransparency {
			pageUsesTransparency = resourcesUseTransparency(ctx, doc, res, 0, map[string]bool{}, drawn)
		}

		// Check tiling patterns for transparency triggers.
		if !pageUsesTransparency {
			pageUsesTransparency = checkPatternTransparency(ctx, doc, res)
		}

		// Check Type 3 fonts for transparency triggers.
		// Type 3 font CharProcs may use ExtGState with transparency or
		// reference Form XObjects with transparency groups.
		if !pageUsesTransparency {
			pageUsesTransparency = checkType3FontTransparency(ctx, doc, &pages[i], res)
		}

		// Also check annotations for transparency triggers.
		if !pageUsesTransparency {
			pageUsesTransparency = checkAnnotationTransparency(ctx, doc, page)
		}

		// PDF/A-4: page-level OutputIntents supplement catalog-level.
		pageHasOI := false
		if part == 4 {
			pageHasOI, _ = checkPageOutputIntent(ctx, doc, page)
		}
		effectiveHasOI := hasOutputIntent || pageHasOI

		// A page that uses transparency must have a blending colour space: an
		// explicit /Group /CS, or the document/page OutputIntent ICC profile as a
		// default. veraPDF scopes §6.2.9 t2 / §6.2.10 t2 to that existence check
		// (gOutputCS != null || containsGroupCS == true || containsTransparency ==
		// false). It does NOT require a device /Group /CS to match the OutputIntent's
		// colour space: a DeviceRGB blending CS under a CMYK OutputIntent is reported
		// by §6.2.4.3, not here.
		if pageUsesTransparency && !effectiveHasOI && !pageHasBlendingCS(ctx, doc, page) {
			results = append(results, failf(blendingCSRule(part), "page %d", i+1)...)
		}
	}
	return results
}

// blendModeTimes returns how many times the graphics state gsObj fails the
// blend mode rule when a page's content sets it with times gs operators: once
// for the whole document when it is an indirect object, which judged records,
// and once for every gs otherwise. Measured with veraPDF 1.30 at PDF/A-2b: an indirect state and a
// direct one, each naming a blend mode no table lists, fail ISO 19005-2,
// 6.2.10 t1 three times when one page sets both twice or two pages set both
// from a stream each.
func blendModeTimes(gsObj ast.PdfObject, times int, judged map[int]bool) int {
	ref, ok := gsObj.(ast.PdfIndirectRef)
	if !ok {
		return times
	}
	if judged[ref.ObjectNum] {
		return 0
	}
	judged[ref.ObjectNum] = true
	return 1
}

// blendModeViolations fails the blend mode rule, ISO 19005-2/3, 6.2.10 t1 and
// ISO 19005-4, 6.2.9 t1, for the graphics state gs, which gsObj holds and
// times gs operators set, when its /BM names a blend mode ISO 32000-1, 11.3.5,
// Tables 136 and 137 do not list. It fails as often as [blendModeTimes]
// counts, and format and args locate the state.
func blendModeViolations(ctx context.Context, doc *document.Document, gs ast.PdfDict, gsObj ast.PdfObject, times, part int, judged map[int]bool, format string, args ...any) []Violation {
	bm, ok := cosBlendMode(ctx, doc, gs["BM"], part)
	if !ok || standardBlendModes[bm] {
		return nil
	}
	where := fmt.Sprintf(format, args...)
	var results []Violation
	for range blendModeTimes(gsObj, times, judged) {
		results = append(results, failf(blendModeRule(part), "%s, BM=%s", where, bm)...)
	}
	return results
}

// cosBlendMode returns the blend mode the blend mode rule judges in the /BM
// value obj, and false when it judges none.
//
// veraPDF states the rule over its CosBM object, which it builds from a /BM
// that resolves to a name, the empty name included. ISO 32000-1, 11.6.3 also
// lets /BM be an array of names, of which a viewer uses the first it
// recognises, and under the parts built on ISO 32000-1 veraPDF judges the
// first element of the array that resolves to a name. PDF/A-4 is built on ISO
// 32000-2, which deprecates the array form, and there veraPDF judges no array.
// Measured with veraPDF 1.30: /BM [1 /Foo], /BM [/Foo] and a /BM naming /Foo
// through an indirect reference fail t1 at PDF/A-2b, /BM [/Normal /Foo] passes
// it, /BM / fails it at PDF/A-2b and PDF/A-4, and /BM [/Foo] fails nothing at
// PDF/A-4.
func cosBlendMode(ctx context.Context, doc *document.Document, obj ast.PdfObject, part int) (string, bool) {
	if obj == nil {
		return "", false
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return "", false
	}
	if name, ok := resolved.(ast.PdfName); ok {
		return string(name), true
	}
	arr, ok := resolved.(ast.PdfArray)
	if !ok || part >= 4 {
		return "", false
	}
	for _, elem := range arr {
		if name, ok := resolveNameObject(ctx, doc, elem); ok {
			return string(name), true
		}
	}
	return "", false
}

// resolveNameObject returns the name obj resolves to, and false when it
// resolves to anything else. Unlike [document.ResolveName], it tells the empty
// name, which is a name (ISO 32000-1, 7.3.5), from null.
func resolveNameObject(ctx context.Context, doc *document.Document, obj ast.PdfObject) (ast.PdfName, bool) {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return "", false
	}
	name, ok := resolved.(ast.PdfName)
	return name, ok
}

// drawnStreamBlendModes fails the blend mode rule for the graphics states the
// gs operators of s, a stream [drawnStreamWalk] reaches, set. A name resolves
// against the stream's own resources first and then the page's, as veraPDF
// resolves it (see [drawnStreamWalk]), and each state fails as often as
// [blendModeTimes] counts: a state written as an indirect object once for the
// whole document, one written directly once for every gs in a stream, which
// is itself judged once.
func drawnStreamBlendModes(ctx context.Context, doc *document.Document, s *drawnStream, part int, judged map[int]bool) []Violation {
	sets := collectDrawnResources(s.ops).extGStates
	if len(sets) == 0 {
		return nil
	}
	scope := appearanceResources(ctx, doc, s.resources, s.pageRes)
	states, err := document.ResolveDict(ctx, doc, scope["ExtGState"])
	if err != nil || states == nil {
		return nil
	}
	label := drawnStreamLabel(s)
	var results []Violation
	for _, name := range slices.Sorted(maps.Keys(sets)) {
		gsObj, ok := states[ast.PdfName(name)]
		if !ok {
			continue
		}
		gs, err := document.ResolveDict(ctx, doc, gsObj)
		if err != nil || gs == nil {
			continue
		}
		results = append(results, blendModeViolations(ctx, doc, gs, gsObj, sets[name], part, judged,
			"page %d, %s, ExtGState %s", s.pageNum, label, name)...)
	}
	return results
}

// drawnStreamLabel names the stream s in a violation's context.
func drawnStreamLabel(s *drawnStream) string {
	var kind string
	switch s.kind {
	case drawnForm:
		kind = "form XObject"
	case drawnPattern:
		kind = "tiling pattern"
	case drawnGlyph:
		kind = "Type3 glyph"
	case drawnAppearance:
		kind = "annotation appearance"
	}
	if s.objNum == 0 {
		return kind
	}
	return fmt.Sprintf("%s object %d", kind, s.objNum)
}

// annotationBlendModes fails the blend mode rule for the /BM of each
// annotation page, the pageNum-th, lists. ISO 32000-2, 12.5.2 gives an
// annotation a blend mode of its own, which ISO 32000-1 does not, so only
// PDF/A-4 judges it: veraPDF builds a CosBM from an annotation's /BM under ISO
// 32000-2 alone. An annotation written as an indirect object fails once for
// the whole document ([blendModeTimes]). Measured with veraPDF 1.30: an
// annotation naming /BM /Foo fails ISO 19005-4, 6.2.9 t1 once, also when two
// pages list it, and passes ISO 19005-2, 6.2.10 t1 at PDF/A-2b, and one
// written directly in /Annots with /BM [/Foo] fails nothing at PDF/A-4.
func annotationBlendModes(ctx context.Context, doc *document.Document, page ast.PdfDict, pageNum, part int, judged map[int]bool) []Violation {
	if part < 4 {
		return nil
	}
	annots, err := document.ResolveArray(ctx, doc, page["Annots"])
	if err != nil {
		return nil
	}
	var results []Violation
	for i, annotObj := range annots {
		if ctx.Err() != nil {
			return results
		}
		annot, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil || annot == nil {
			continue
		}
		results = append(results, blendModeViolations(ctx, doc, annot, annotObj, 1, part, judged,
			"page %d, annotation %d", pageNum, i+1)...)
	}
	return results
}

// drawnResources records the resource names a content stream invokes: the
// XObjects it paints with Do and the graphics states it sets with gs.
//
// A resource dictionary lists what a page MAY reach, not what it draws.
// ISO 19005-2, 6.2.10 and ISO 19005-4, 6.2.9 ask whether the page "contains
// transparency", which veraPDF answers from the content stream: its
// PDPage.containsTransparency is raised while walking operators, so a soft
// masked image nothing paints leaves it false. Two fixtures identical but for a
// single Do settle it.
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: declares eleven soft masked images while drawing
//     only FXX15, which has no /SMask, and veraPDF reports nothing at 2b or
//     at 4.
//   - pdfjs issue14847.pdf: declares two soft masked images and draws nothing
//     at all, and veraPDF reports nothing at 2b or at 4.
type drawnResources struct {
	// xobjects and extGStates count the Do and gs operators naming each
	// resource.
	xobjects   map[string]int
	extGStates map[string]int
}

// collectDrawnResources reads the XObject and ExtGState names ops invokes.
func collectDrawnResources(ops []content.Operation) *drawnResources {
	drawn := &drawnResources{
		xobjects:   make(map[string]int),
		extGStates: make(map[string]int),
	}
	for _, op := range ops {
		var target map[string]int
		switch op.Operator {
		case "Do":
			target = drawn.xobjects
		case "gs":
			target = drawn.extGStates
		default:
			continue
		}
		if len(op.Operands) == 0 {
			continue
		}
		if name, ok := op.Operands[len(op.Operands)-1].(ast.PdfName); ok {
			target[string(name)]++
		}
	}
	return drawn
}

// drawsXObject reports whether the content stream painted the named XObject
// with Do. A nil receiver admits every name, which is what callers holding no
// parsed content stream need.
func (d *drawnResources) drawsXObject(name string) bool {
	return d.doCount(name) > 0
}

// setsExtGState reports whether the content stream selected the named graphics
// state with gs. A nil receiver admits every name.
func (d *drawnResources) setsExtGState(name string) bool {
	return d.gsCount(name) > 0
}

// doCount returns how many Do operators paint the named XObject. A nil
// receiver, which holds no parsed content stream, counts one for every name.
func (d *drawnResources) doCount(name string) int {
	if d == nil {
		return 1
	}
	return d.xobjects[name]
}

// gsCount returns how many gs operators set the named graphics state. A nil
// receiver counts one for every name.
func (d *drawnResources) gsCount(name string) int {
	if d == nil {
		return 1
	}
	return d.extGStates[name]
}

// pageDrawnResources reads the resource names a page's own content stream
// invokes. Returns nil when the content cannot be read, which leaves the
// transparency survey unscoped rather than silently emptying it: a page whose
// content litho cannot parse must not be reported as transparency-free.
func pageDrawnResources(ctx context.Context, p *pageCtx) *drawnResources {
	// No content, or empty content, paints nothing: the drawn set is empty
	// rather than unknown, so a resource the page never reaches stays out of
	// scope. Only a stream litho cannot read falls back to surveying them all,
	// which is what a nil result means to the caller.
	if p.contentUnreadable {
		return nil
	}
	ops := p.parsedOps(ctx)
	if p.parseFailed {
		return nil
	}
	return collectDrawnResources(ops)
}

// formDrawnResources parses a Form XObject's own content stream so recursion
// into it judges what the form paints rather than what it declares. Returns nil
// when the stream cannot be read, which keeps the survey of that form's
// resources unscoped rather than silently emptying it.
func formDrawnResources(ctx context.Context, doc *document.Document, stream ast.PdfStream) *drawnResources {
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return nil
	}
	return collectDrawnResources(ops)
}

// blendModeIsTransparency reports whether an ExtGState /BM value makes the
// content transparent for the blending-colour-space rules.
//
// /Compatible counts, which reads oddly: ISO 32000-1, 11.3.5 calls it "the same
// as Normal" and ISO 19005-2 §6.2.10 t1 lists it beside Normal among the values
// the blend mode may take, so litho read it as no transparency at all. The
// question this answers is a different one, and veraPDF's
// PDPage.containsTransparency is what §6.2.10 t2 acts on: given an ExtGState
// naming /Compatible, no page /Group and no PDF/A OutputIntent it reports "The
// page contains transparent objects with no blending colour space defined",
// while the same file naming /Normal passes. The rule that judges which values
// /BM may take is a separate check and still admits Compatible.
//
// # Corpus evidence
//
//   - govdocs thread_300_300345.pdf: the corpus case, an ExtGState naming
//     /Compatible.
func blendModeIsTransparency(bm string) bool {
	return bm != "" && bm != "Normal"
}

// imageSoftMasked reports whether an image XObject carries a soft mask.
//
// Per ISO 32000-1, Table 89, the /SMask entry of an image is either a stream or
// the name None, and None states that the image has no soft mask. Testing only
// that the key is present and not null therefore read an explicit /SMask /None
// as transparency and demanded a blending colour space the page does not need.
// Two fixtures differing only in that value settle it. The ExtGState paths
// already draw the same distinction inline.
//
// # Corpus evidence
//
//   - pdfjs issue14814.pdf: draws one image with /SMask /None on a page
//     carrying no /Group under no OutputIntent, and veraPDF reports nothing
//     there.
func imageSoftMasked(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	if !nonNullKeyPresent(ctx, doc, dict, "SMask") {
		return false
	}
	name, _ := document.ResolveName(ctx, doc, dict["SMask"])
	return name != "None"
}

// resourcesUseTransparency reports whether a resource dictionary reaches any
// transparency the content stream actually draws: an ExtGState with a
// non-isolating blend mode, a soft mask, or a non-opaque constant alpha; an
// Image XObject with a /SMask; or a Form XObject with a transparency /Group. It
// recurses into Form XObject resources, bounded by the form budget
// ([formTooDeep]) and a visited set of form-dict identities, so nested
// transparency is detected while cyclic references cannot loop.
//
// drawn narrows the scan to the names the content stream invokes; see
// [drawnResources]. A nil drawn admits every name, for callers that hold no
// content stream to consult.
func resourcesUseTransparency(ctx context.Context, doc *document.Document, res ast.PdfDict, depth int, visited map[string]bool, drawn *drawnResources) bool {
	if res == nil || formTooDeep(doc, depth) || ctx.Err() != nil {
		return false
	}

	if gsDict, err := document.ResolveDict(ctx, doc, res["ExtGState"]); err == nil && gsDict != nil {
		for _, name := range sortedNames(gsDict) {
			if !drawn.setsExtGState(string(name)) {
				continue
			}
			gs, err := document.ResolveDict(ctx, doc, gsDict[name])
			if err != nil || gs == nil {
				continue
			}
			if bmObj, ok := gs["BM"]; ok {
				if blendModeIsTransparency(resolveBlendMode(ctx, doc, bmObj)) {
					return true
				}
			}
			if smObj, ok := gs["SMask"]; ok && !resourceEntryNull(ctx, doc, smObj) {
				if nm, _ := document.ResolveName(ctx, doc, smObj); nm != "None" {
					return true
				}
			}
			if v, ok := gs["CA"]; ok && !floatEquals1(document.Float(v)) {
				return true
			}
			if v, ok := gs["ca"]; ok && !floatEquals1(document.Float(v)) {
				return true
			}
		}
	}

	xobjDict, err := document.ResolveDict(ctx, doc, res["XObject"])
	if err != nil || xobjDict == nil {
		return false
	}
	for _, name := range sortedNames(xobjDict) {
		if ctx.Err() != nil {
			return false
		}
		if !drawn.drawsXObject(string(name)) {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, xobjDict[name])
		if err != nil {
			continue
		}
		subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
		switch subtype {
		case "Image":
			if imageSoftMasked(ctx, doc, stream.Dict) {
				return true
			}
			// A JPXDecode image can carry its opacity channel inside the JPEG2000
			// codestream instead of an explicit /SMask, signalled by /SMaskInData
			// 1 or 2 (ISO 32000-1 Table 89). That is still transparency, so the
			// page needs a blending colour space (veraPDF §6.2.10 t2).
			if document.Float(stream.Dict["SMaskInData"]) > 0 {
				return true
			}
		case "Form":
			if group, err := document.ResolveDict(ctx, doc, stream.Dict["Group"]); err == nil && group != nil {
				if s, _ := document.ResolveName(ctx, doc, group["S"]); s == "Transparency" {
					return true
				}
			}
			addr := formatDictAddr(stream.Dict)
			if visited[addr] {
				continue
			}
			visited[addr] = true
			formRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
			// A drawn form paints its own content, so recurse under the names
			// that form invokes rather than the page's.
			formDrawn := drawn
			if drawn != nil {
				formDrawn = formDrawnResources(ctx, doc, stream)
			}
			if resourcesUseTransparency(ctx, doc, formRes, depth+1, visited, formDrawn) {
				return true
			}
		}
	}
	return false
}

// checkAnnotationTransparency checks whether any annotation on the page
// uses transparency (via appearance streams with transparency groups or
// ExtGState with transparency settings).
//
// # Corpus evidence
//
//   - pdfjs annotation-as.pdf: its appearances carry image /SMask entries,
//     and it fails ISO 19005-2 §6.2.10 t2 in veraPDF.
//   - pdfjs stamps.pdf: its appearances carry a /Group /S /Transparency, and
//     it fails ISO 19005-2 §6.2.10 t2 in veraPDF.
func checkAnnotationTransparency(ctx context.Context, doc *document.Document, page ast.PdfDict) bool {
	annotsArr, err := document.ResolveArray(ctx, doc, page["Annots"])
	if err != nil || len(annotsArr) == 0 {
		return false
	}
	for _, annotObj := range annotsArr {
		if ctx.Err() != nil {
			return false
		}
		annot, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil || annot == nil {
			continue
		}
		// Check /BM (blend mode) directly on the annotation dict.
		// PDF annotations can carry their own blend mode (ISO 32000-2 Table 165).
		if bmObj, hasBM := annot["BM"]; hasBM {
			if blendModeIsTransparency(resolveBlendMode(ctx, doc, bmObj)) {
				return true
			}
		}
		apDict, err := document.ResolveDict(ctx, doc, annot["AP"])
		if err != nil || apDict == nil {
			continue
		}
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			apObj, ok := apDict[key]
			if !ok {
				continue
			}
			for _, stream := range appearanceStreams(ctx, doc, apObj) {
				// Transparency group on the appearance Form XObject.
				if group, err := document.ResolveDict(ctx, doc, stream.Dict["Group"]); err == nil && group != nil {
					if s, _ := document.ResolveName(ctx, doc, group["S"]); s == "Transparency" {
						return true
					}
				}
				// Transparency in the appearance's own resources. This walks the
				// same tree as the page pass: an appearance routinely paints a
				// soft-masked image or a transparency-group form rather than
				// setting an ExtGState, and veraPDF counts those toward the
				// page's containsTransparency.
				if apRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && apRes != nil {
					if resourcesUseTransparency(ctx, doc, apRes, 0, map[string]bool{},
						formDrawnResources(ctx, doc, stream)) {
						return true
					}
				}
			}
		}
	}
	return false
}

// appearanceStreams resolves an /AP entry to its appearance stream(s). The value
// is either a single Form XObject stream, or a sub-dictionary of appearance
// states (e.g. /On and /Off for a checkbox or radio-button widget) whose values
// are each an appearance stream.
func appearanceStreams(ctx context.Context, doc *document.Document, apObj ast.PdfObject) []ast.PdfStream {
	if stream, err := document.ResolveStream(ctx, doc, apObj); err == nil {
		return []ast.PdfStream{stream}
	}
	sub, err := document.ResolveDict(ctx, doc, apObj)
	if err != nil || sub == nil {
		return nil
	}
	var streams []ast.PdfStream
	for _, name := range sortedNames(sub) {
		if stream, err := document.ResolveStream(ctx, doc, sub[name]); err == nil {
			streams = append(streams, stream)
		}
	}
	return streams
}

// documentOutputIntentInfo returns whether the document has an OutputIntent
// with a DestOutputProfile stream, and the ICC colour space of that profile.
func documentOutputIntentInfo(ctx context.Context, doc *document.Document, catalog ast.PdfDict) (bool, string) {
	oiObj, hasOI := catalog["OutputIntents"]
	if !hasOI {
		return false, ""
	}
	arr, err := document.ResolveArray(ctx, doc, oiObj)
	if err != nil || len(arr) == 0 {
		return false, ""
	}
	for _, intentObj := range arr {
		intent, err := document.ResolveDict(ctx, doc, intentObj)
		if err != nil || intent == nil {
			continue
		}
		// Only PDF/A OutputIntents provide coverage. The subtype is
		// GTS_PDFA1 for every PDF/A part (ISO 32000-2 Table 401);
		// "GTS_PDFA" does not exist.
		s, _ := document.ResolveName(ctx, doc, intent["S"])
		if s != "GTS_PDFA1" {
			continue
		}
		destObj, ok := intent["DestOutputProfile"]
		if !ok {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, destObj)
		if err != nil {
			doc.Logger.Debug("compliance.transparency: cannot resolve output profile stream", "error", err)
			continue
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil || len(data) < 20 {
			return true, "" // Has OutputIntent but can't read color space
		}
		cs := string(data[16:20])
		return true, cs
	}
	return false, ""
}

// checkPatternTransparency checks whether any tiling pattern in the page
// resources uses transparency (via ExtGState with alpha or blend mode).
func checkPatternTransparency(ctx context.Context, doc *document.Document, pageRes ast.PdfDict) bool {
	patDict, err := document.ResolveDict(ctx, doc, pageRes["Pattern"])
	if err != nil || patDict == nil {
		return false
	}
	for _, patObj := range patDict {
		stream, err := document.ResolveStream(ctx, doc, patObj)
		if err != nil {
			doc.Logger.Debug("compliance.transparency: cannot resolve pattern stream", "error", err)
			continue
		}
		patRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if err != nil || patRes == nil {
			continue
		}
		gsDict, err := document.ResolveDict(ctx, doc, patRes["ExtGState"])
		if err != nil || gsDict == nil {
			continue
		}
		for _, gsObj := range gsDict {
			gs, err := document.ResolveDict(ctx, doc, gsObj)
			if err != nil || gs == nil {
				continue
			}
			if bmObj, hasBM := gs["BM"]; hasBM {
				if blendModeIsTransparency(resolveBlendMode(ctx, doc, bmObj)) {
					return true
				}
			}
			if smaskObj, hasSMask := gs["SMask"]; hasSMask && !resourceEntryNull(ctx, doc, smaskObj) {
				smaskName, _ := document.ResolveName(ctx, doc, smaskObj)
				if smaskName != "None" {
					return true
				}
			}
			if caObj, hasCA := gs["CA"]; hasCA {
				if !floatEquals1(document.Float(caObj)) {
					return true
				}
			}
			if caObj, hasCa := gs["ca"]; hasCa {
				if !floatEquals1(document.Float(caObj)) {
					return true
				}
			}
		}
	}
	return false
}

// checkType3FontTransparency checks whether any Type 3 font in the page
// resources uses transparency (via ExtGState or transparency group XObjects
// in its own resources).
func checkType3FontTransparency(ctx context.Context, doc *document.Document, p *pageCtx, pageRes ast.PdfDict) bool {
	fontDict, err := document.ResolveDict(ctx, doc, pageRes["Font"])
	if err != nil || fontDict == nil {
		return false
	}
	rendered := pageType3RenderedCodes(ctx, p)
	for fontName, fontObj := range fontDict {
		fd, err := document.ResolveDict(ctx, doc, fontObj)
		if err != nil || fd == nil {
			continue
		}
		if subtype, _ := document.ResolveName(ctx, doc, fd["Subtype"]); subtype != "Type3" {
			continue
		}
		fontRes, err := document.ResolveDict(ctx, doc, fd["Resources"])
		if err != nil || fontRes == nil {
			continue
		}
		charProcs, err := document.ResolveDict(ctx, doc, fd["CharProcs"])
		if err != nil || charProcs == nil {
			continue
		}

		// veraPDF counts a Type 3 font's transparency only for glyphs the content
		// actually paints: a transparent glyph that is never shown does not make
		// the page transparent (ISO 19005-2 §6.2.10 / 19005-4 §6.2.9, PDPage
		// containsTransparency). Scope the scan to the rendered glyphs' CharProcs.
		// If the rendered set cannot be determined (unparsable content), fall back
		// to every CharProc so real transparency is never missed.
		var glyphs []ast.PdfName
		if codes := rendered[string(fontName)]; len(codes) > 0 {
			codeToName := type3EncodingCodeToName(ctx, doc, fd)
			for code := range codes {
				if name, ok := codeToName[code]; ok {
					glyphs = append(glyphs, ast.PdfName(name))
				}
			}
		} else {
			for name := range charProcs {
				glyphs = append(glyphs, name)
			}
		}

		for _, gname := range glyphs {
			stream, err := document.ResolveStream(ctx, doc, charProcs[gname])
			if err != nil {
				continue
			}
			if type3CharProcTransparent(ctx, doc, stream, fontRes) {
				return true
			}
		}
	}
	return false
}

// pageType3RenderedCodes returns, per font resource name, the set of single-byte
// character codes shown by the page's own content stream. It scans only the page
// content (matching the resource scope of [checkType3FontTransparency]).
func pageType3RenderedCodes(ctx context.Context, p *pageCtx) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	ops := p.parsedOps(ctx)
	if len(ops) == 0 {
		return out
	}
	current := ""
	// The text font belongs to the graphics state, so Q restores it. See ISO
	// 32000-1, 8.4.2 and Table 104. Without this the codes shown after a
	// restore are attributed to whichever font Tf named last.
	var fontStack []string
	record := func(s ast.PdfString) {
		if current == "" {
			return
		}
		codes := out[current]
		if codes == nil {
			codes = map[int]bool{}
			out[current] = codes
		}
		for _, b := range []byte(s) {
			codes[int(b)] = true
		}
	}
	for _, op := range ops {
		switch op.Operator {
		case "q":
			fontStack = append(fontStack, current)
		case "Q":
			if n := len(fontStack); n > 0 {
				current = fontStack[n-1]
				fontStack = fontStack[:n-1]
			}
		case "Tf":
			if len(op.Operands) >= 1 {
				if n, ok := op.Operands[0].(ast.PdfName); ok {
					current = string(n)
				}
			}
		case "Tj", "'", "\"":
			for _, operand := range op.Operands {
				if s, ok := operand.(ast.PdfString); ok {
					record(s)
				}
			}
		case "TJ":
			if len(op.Operands) >= 1 {
				if arr, ok := op.Operands[0].(ast.PdfArray); ok {
					for _, el := range arr {
						if s, ok := el.(ast.PdfString); ok {
							record(s)
						}
					}
				}
			}
		}
	}
	return out
}

// type3EncodingCodeToName maps character codes to glyph names from a Type 3
// font's /Encoding /Differences array. Codes absent from the array are omitted.
func type3EncodingCodeToName(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) map[int]string {
	out := map[int]string{}
	encDict, err := document.ResolveDict(ctx, doc, fontDict["Encoding"])
	if err != nil || encDict == nil {
		return out
	}
	diffArr, err := document.ResolveArray(ctx, doc, encDict["Differences"])
	if err != nil {
		return out
	}
	code := 0
	for _, item := range diffArr {
		switch v := item.(type) {
		case ast.PdfInt:
			code = int(v)
		case ast.PdfName:
			out[code] = string(v)
			code++
		}
	}
	return out
}

// type3CharProcTransparent reports whether a Type 3 glyph procedure paints with
// transparency: a gs operator selecting an ExtGState with a non-opaque alpha,
// soft mask, or non-standard blend mode, or a Do operator painting a Form
// XObject that is a transparency group. The referenced resources resolve through
// the font's own /Resources.
//
// # Corpus evidence
//
//   - pdfjs issue10717.pdf: keeps its /SMask images in the Type3 font's own
//     /Resources, where only this walk reaches them.
func type3CharProcTransparent(ctx context.Context, doc *document.Document, charProc ast.PdfStream, fontRes ast.PdfDict) bool {
	data, err := doc.DecodeStream(ctx, charProc)
	if err != nil {
		return false
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return false
	}
	gsDict, _ := document.ResolveDict(ctx, doc, fontRes["ExtGState"])
	xobjDict, _ := document.ResolveDict(ctx, doc, fontRes["XObject"])
	for _, op := range ops {
		switch op.Operator {
		case "gs":
			if gsDict == nil || len(op.Operands) < 1 {
				continue
			}
			name, ok := op.Operands[0].(ast.PdfName)
			if !ok {
				continue
			}
			if gs, err := document.ResolveDict(ctx, doc, gsDict[name]); err == nil && gs != nil && extGStateTransparent(ctx, doc, gs) {
				return true
			}
		case "Do":
			if xobjDict == nil || len(op.Operands) < 1 {
				continue
			}
			name, ok := op.Operands[0].(ast.PdfName)
			if !ok {
				continue
			}
			stream, err := document.ResolveStream(ctx, doc, xobjDict[name])
			if err != nil {
				continue
			}
			switch st, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]); st {
			case "Image":
				// A glyph procedure that paints a soft-masked image makes the
				// page transparent exactly as the page content would (ISO
				// 32000-1 §11.6.5). Such images can sit in the Type3 font's
				// own /Resources, where only this walk reaches them.
				if imageSoftMasked(ctx, doc, stream.Dict) {
					return true
				}
				if document.Float(stream.Dict["SMaskInData"]) > 0 {
					return true
				}
			case "Form":
				if group, err := document.ResolveDict(ctx, doc, stream.Dict["Group"]); err == nil && group != nil {
					if s, _ := document.ResolveName(ctx, doc, group["S"]); s == "Transparency" {
						return true
					}
				}
				formRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
				if resourcesUseTransparency(ctx, doc, formRes, 0, map[string]bool{},
					formDrawnResources(ctx, doc, stream)) {
					return true
				}
			}
		}
	}
	return false
}

// extGStateTransparent reports whether an ExtGState dictionary establishes
// transparency: a non-standard blend mode, a soft mask, or a non-opaque
// stroking or non-stroking constant alpha.
func extGStateTransparent(ctx context.Context, doc *document.Document, gs ast.PdfDict) bool {
	if bmObj, ok := gs["BM"]; ok {
		if blendModeIsTransparency(resolveBlendMode(ctx, doc, bmObj)) {
			return true
		}
	}
	if smaskObj, ok := gs["SMask"]; ok && !resourceEntryNull(ctx, doc, smaskObj) {
		if smaskName, _ := document.ResolveName(ctx, doc, smaskObj); smaskName != "None" {
			return true
		}
	}
	if caObj, ok := gs["CA"]; ok && !floatEquals1(document.Float(caObj)) {
		return true
	}
	if caObj, ok := gs["ca"]; ok && !floatEquals1(document.Float(caObj)) {
		return true
	}
	return false
}

// resolveBlendMode resolves a blend mode value which may be a name or the
// first element of an array of names.
func resolveBlendMode(ctx context.Context, doc *document.Document, obj ast.PdfObject) string {
	// BM can be a single name.
	if name, _ := document.ResolveName(ctx, doc, obj); name != "" {
		return string(name)
	}
	// Or an array of names. Use the first entry.
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) == 0 {
		return ""
	}
	name, _ := document.ResolveName(ctx, doc, arr[0])
	return string(name)
}

// alphaEpsilon is the tolerance [floatEquals1] allows around 1.0.
const alphaEpsilon = 1e-6

// floatEquals1 tests whether a float64 is approximately 1.0.
func floatEquals1(v float64) bool {
	return math.Abs(v-1.0) < alphaEpsilon
}

// pageHasBlendingCS checks whether a page dictionary has a Group dict with
// /S = Transparency and a /CS entry.
//
// # Corpus evidence
//
//   - pdfjs issue11045.pdf: points its group /CS at a free object, so the
//     page has no blending colour space.
func pageHasBlendingCS(ctx context.Context, doc *document.Document, page ast.PdfDict) bool {
	groupObj, hasGroup := page["Group"]
	if !hasGroup {
		return false
	}

	group, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || group == nil {
		return false
	}

	sName, _ := document.ResolveName(ctx, doc, group["S"])
	if sName != "Transparency" {
		return false
	}

	csObj, hasCS := group["CS"]
	if !hasCS {
		return false
	}
	// Per ISO 32000-1 §7.3.9 an indirect reference to a non-existent object is
	// the null object, and per §7.3.7 a dictionary entry whose value is null is
	// equivalent to the entry being absent. A group /CS pointing at a free
	// object leaves the page with no blending colour space, and it still owes
	// ISO 19005-2 §6.2.10 t2 an answer.
	cs, err := document.ResolveObject(ctx, doc, csObj)
	if err != nil {
		return false
	}
	return !isNullObject(cs)
}
