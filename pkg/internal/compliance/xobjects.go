package compliance

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// checkXObjectGroup validates XObject restrictions for PDF/A conformance.
// It checks reference XObjects, form XObject forbidden keys, image /Alternates,
// image /OPI, and image /Interpolate constraints across all pages.
//
// veraPDF judges an XObject where a content stream it parses draws it, and it
// parses every stream [drawnStreamWalk] reaches: a page's content, the forms
// a Do paints, the cells of the tiling patterns an scn or SCN selects, every
// glyph procedure of a Type 3 font text is shown with, and every annotation
// appearance stream, each once for the document. So the image and form rules
// judge the XObjects each of those draws, and the inline images each holds,
// wherever they stand ([checkDrawnXObjects], [checkInlineImageInterpolate]),
// and an appearance stream is itself a form ([checkAppearanceForms]).
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on an image whose
// /Intent is /Bogus and /Interpolate true: drawn by an annotation's normal,
// down or rollover appearance, a hidden annotation's too, by a glyph a page
// shows, by a glyph of that font the page does not show, by a glyph of a font
// shown only in rendering mode 3 or 7, and by the cell of a pattern a form
// paints with, it fails ISO 19005-1, 6.2.4 t3 and 6.2.9 t1 and ISO 19005-2,
// 6.2.8 t3 and 6.2.6 t1 once each; drawn by a glyph of a font a Tf selects and
// no text shows, neither.
//
// # Corpus evidence
//
//   - pdfjs bug1771477.pdf: draws an image with /Interpolate true inside a
//     tiling pattern.
//   - pdfjs pr12564.pdf: draws images whose /Interpolate is true, and soft
//     masks whose /Interpolate is true, through its 13 annotation appearances,
//     and veraPDF states ISO 19005-1, 6.2.4 t3 and ISO 19005-2, 6.2.8 t3 22
//     times, where litho stated 2.
//   - pdfcpu pkg_testdata__FOSDEM14_HPC_devroom_14_GoCUDA.pdf: draws 6 such
//     images in the cells of patterns its forms paint with, and veraPDF states
//     both 17 times, where litho stated 11.
//   - pdfjs issue11306.pdf: draws one more through its annotation
//     appearance; veraPDF states both 5 times, where litho stated 4.
//   - All corpora, at PDF/A-1b, PDF/A-2b and PDF/A-2u, and at their own
//     profile for the verapdf files: reaching these streams, with the inline
//     image and form counts ([checkInlineImageInterpolate],
//     [formPostScriptFaults]), moved the counts of 8 files onto veraPDF's
//     and none away, and no other count.
func checkXObjectGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, profile := vctx.doc, vctx.profile

	if vctx.ua {
		return nil
	}

	part := vctx.part
	if part == 0 {
		return nil
	}

	doc.Logger.Debug("compliance.xobjects: starting checks", "profile", profile, "part", part)

	var results []Violation
	visited := make(map[string]bool)
	drawn := newDrawnStreamWalk(vctx, func(s *drawnStream) {
		scope := appearanceResources(ctx, doc, s.resources, s.pageRes)
		results = append(results, checkDrawnXObjects(ctx, doc, scope, s.ops, s.pageNum, part, profile, visited)...)
		results = append(results, checkInlineImageInterpolate(s.ops, s.pageNum, part)...)
	})

	for i := range vctx.pages {
		if ctx.Err() != nil {
			return results
		}
		p := &vctx.pages[i]

		// A content stream an earlier page shares is judged there alone, and
		// what it draws is resolved through that page's resources; see
		// [markJudgedContents].
		if !p.contentJudged {
			ops := p.parsedOps(ctx)
			results = append(results, checkDrawnXObjects(ctx, doc, p.resources, ops, i+1, part, profile, visited)...)
			results = append(results, checkInlineImageInterpolate(ops, i+1, part)...)
		}
		results = append(results, checkAppearanceForms(ctx, doc, p.dict, i+1, part, profile, visited)...)
		drawn.walkPage(ctx, p, i+1)
	}

	doc.Logger.Debug("compliance.xobjects: checks complete", "violations", len(results))
	return results
}

// checkInlineImageInterpolate checks parsed BI operations for inline images
// with /I true or /Interpolate true, one violation for each such image:
// veraPDF judges every inline image as an image of its own. Measured with
// veraPDF 1.30 at PDF/A-1b and PDF/A-2b: two such images in one content
// stream fail ISO 19005-1, 6.2.4 t3 and ISO 19005-2, 6.2.8 t3 twice.
//
// # Corpus evidence
//
//   - pdfjs issue14256.pdf: writes both /I and /Interpolate on one inline
//     image.
//   - govdocs thread_100_100877.pdf: holds 97 inline images with /I true on 9
//     pages, and veraPDF states ISO 19005-1, 6.2.4 t3 and ISO 19005-2, 6.2.8
//     t3 97 times, where litho stated 9.
//   - pdfjs TAMReview.pdf, bug1065245.pdf and issue20722.pdf: the same, 2, 3
//     and 13 times where litho stated 1, 1 and 8.
func checkInlineImageInterpolate(ops []content.Operation, pageNum, part int) []Violation {
	if len(ops) == 0 {
		return nil
	}

	// Select the appropriate rule.
	var rule Rule
	switch part {
	case 1:
		rule = Rule_19005_1_6_2_4_t03
	case 2:
		rule = Rule_19005_2_6_2_8_t03
	case 3:
		rule = Rule_19005_3_6_2_8_t03
	case 4:
		rule = Rule_19005_4_6_2_7_1_t03
	default:
		return nil
	}

	var results []Violation
	for _, op := range ops {
		if op.Operator != "BI" || len(op.Operands) == 0 {
			continue
		}
		dict, ok := op.Operands[0].(ast.PdfDict)
		if !ok {
			continue
		}
		// /I is the abbreviation of /Interpolate (ISO 32000-1, 8.9.7, Table
		// 93). A dictionary that writes both is judged by /I, whichever comes
		// first: veraPDF passes /I false /Interpolate true and fails
		// /Interpolate false /I true.
		val, exists := dict["I"]
		if !exists {
			val = dict["Interpolate"]
		}
		if b, ok := val.(ast.PdfBool); ok && bool(b) {
			results = append(results, failf(rule, "page %d: inline image has Interpolate=true", pageNum)...)
		}
	}
	return results
}

// checkDrawnXObjects applies the XObject rules to the XObjects the Do operators
// of one content stream draw, whose operations are ops and whose names resolve
// against scope: an image or other XObject once for the document, which
// visited records, and a form at every Do that draws it.
//
// veraPDF reaches an XObject through the Do operator that draws it
// (GFOp_Do), so an entry of a stream's /Resources its content never draws is
// judged nowhere. It judges the model object it builds for an image once, and
// the one it builds for a form at every Do, since GFPDXForm.getID answers null
// and the validator only skips an object whose identifier it has met. A Form
// XObject's own content is a stream [drawnStreamWalk] reaches once. Measured
// with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on an image whose /Intent is
// /Bogus: listed by a form the page draws, or by a form such a form draws, and
// drawn by neither, it fails neither ISO 19005-1, 6.2.9 t1 nor ISO 19005-2,
// 6.2.6 t1; drawn twice on a page, under two names, on two pages, or by a form
// two pages draw, it fails each once; named by the page alone and drawn by a
// form whose resources lack it, or that has none, once. On a form carrying
// /OPI: drawn twice on a page, under two names, or on two pages, it fails ISO
// 19005-2, 6.2.9 t1 twice, and drawn by a form the page draws twice, once.
//
// # Corpus evidence
//
//   - govdocs thread_400_400019.pdf: veraPDF reports a soft mask's
//     /Interpolate 15 times against 57 plain XObjects (72 findings from
//     veraPDF, 57 from litho).
//   - govdocs thread_100_100173.pdf: draws 41 images whose /Intent is invalid,
//     one per object, and veraPDF and litho state ISO 19005-1, 6.2.9 t1 and
//     ISO 19005-2, 6.2.6 t1 41 times each.
//   - All corpora, at PDF/A-1b, PDF/A-2b and PDF/A-2u, and at their own
//     profile for the verapdf files: judging only the XObjects a form draws,
//     and none through a page whose content an earlier page judged, moves no
//     count; litho imagereach_2b.pdf plants both.
func checkDrawnXObjects(ctx context.Context, doc *document.Document, scope ast.PdfDict, ops []content.Operation, pageNum, part int, profile string, visited map[string]bool) []Violation {
	xobjectDict, err := document.ResolveDict(ctx, doc, scope["XObject"])
	if err != nil || len(xobjectDict) == 0 || len(ops) == 0 {
		return nil
	}
	var results []Violation
	for i, op := range ops {
		if i%contentOpCancelCheckInterval == 0 && ctx.Err() != nil {
			return results
		}
		if op.Operator != "Do" || len(op.Operands) == 0 {
			continue
		}
		name, ok := op.Operands[0].(ast.PdfName)
		if !ok {
			continue
		}
		if obj, ok := xobjectDict[name]; ok {
			results = append(results, checkDrawnXObject(ctx, doc, name, obj, pageNum, part, profile, visited)...)
		}
	}
	return results
}

// checkDrawnXObject applies the XObject rules to the XObject obj that a Do
// draws under the name name, and to the soft mask and stencil mask of an
// image: a form at every draw, anything else unless visited holds it
// already; see [checkDrawnXObjects].
func checkDrawnXObject(ctx context.Context, doc *document.Document, name ast.PdfName, obj ast.PdfObject, pageNum, part int, profile string, visited map[string]bool) []Violation {
	// XObjects are streams. Resolve as stream to access the dict.
	stream, err := document.ResolveStream(ctx, doc, obj)
	if err == nil {
		if subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]); subtype == "Form" {
			return checkXObject(ctx, doc, stream, stream.Dict, subtype, name, pageNum, part, profile, false)
		}
	}
	if seenObject(obj, visited, "xo") {
		return nil
	}
	if err != nil {
		// Fallback: try as plain dict (some producers use dict-only XObjects).
		xobjDict, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || xobjDict == nil {
			return nil
		}
		subtype, _ := document.ResolveName(ctx, doc, xobjDict["Subtype"])
		return checkXObject(ctx, doc, ast.PdfStream{}, xobjDict, subtype, name, pageNum, part, profile, false)
	}

	xobjDict := stream.Dict
	subtype, _ := document.ResolveName(ctx, doc, xobjDict["Subtype"])
	results := checkXObject(ctx, doc, stream, xobjDict, subtype, name, pageNum, part, profile, false)
	if subtype != "Image" {
		return results
	}

	// An image's soft mask and stencil mask are image dictionaries in their
	// own right, and the image rules bind to them too: veraPDF builds a
	// PDSMaskImage for /SMask and reports its /Interpolate. Fixtures confirm
	// it states the rule for /Mask as well. /Mask may instead be a colour-key
	// ARRAY (ISO 32000-1, 8.9.6.4), which is no image and resolves to no
	// stream.
	for _, maskKey := range []ast.PdfName{"SMask", "Mask"} {
		maskObj, hasMask := xobjDict[maskKey]
		if !hasMask || seenObject(maskObj, visited, string(maskKey)) {
			continue
		}
		maskStream, err := document.ResolveStream(ctx, doc, maskObj)
		if err != nil {
			continue
		}
		maskSubtype, _ := document.ResolveName(ctx, doc, maskStream.Dict["Subtype"])
		results = append(results, checkXObject(ctx, doc, maskStream, maskStream.Dict, maskSubtype,
			name+"/"+maskKey, pageNum, part, profile, maskKey == "Mask")...)
	}
	return results
}

// seenObject reports whether visited already holds the object obj references
// in the given role, and records it when it does not. An object written
// directly rather than referenced is never recorded, so it is judged at every
// reach.
//
// veraPDF judges an object once per model object it builds from it, and the
// role decides the model: a drawn image is a PDXImage, an image's /SMask a
// PDSMaskImage and its /Mask a PDMaskImage. So one image both drawn and named
// as another's mask is judged in each role. A form is judged at every draw
// instead ([checkDrawnXObjects]).
func seenObject(obj ast.PdfObject, visited map[string]bool, role string) bool {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok {
		return false
	}
	key := fmt.Sprintf("%s:%d", role, ref.ObjectNum)
	if visited[key] {
		return true
	}
	visited[key] = true
	return false
}

// checkAppearanceForms applies the Form XObject rules to the appearance
// streams of the annotations of page, the pageNum-th, at every appearance
// entry of every annotation, each annotation once for the document, which
// visited records.
//
// An appearance stream is a form XObject (ISO 32000-1, 12.5.5), and veraPDF
// builds each as one (GFPDAnnot.addAppearance builds a GFPDXForm), whatever
// its /Subtype says and whether or not its content decodes, so the form rules
// judge its dictionary. The streams it draws are reached by [drawnStreamWalk].
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on a normal appearance:
// carrying /OPI and /PS it fails ISO 19005-1, 6.2.4 t2 and 6.2.5 t1 and ISO
// 19005-2, 6.2.9 t1 once each, with no /Subtype and /OPI, or with /OPI and
// content that does not decode, the /OPI rules once, and carrying /Ref ISO
// 19005-1, 6.2.6 t1 and ISO 19005-2, 6.2.9 t2 once. As a drawn form is judged
// at every Do ([checkDrawnXObjects]), an appearance is judged at every entry
// naming it: one appearance two annotations share, or one annotation names
// as both its normal and its down appearance, fails them twice, and so does a
// form the page draws that is also an annotation's appearance.
func checkAppearanceForms(ctx context.Context, doc *document.Document, page ast.PdfDict, pageNum, part int, profile string, visited map[string]bool) []Violation {
	appearances := annotationAppearances(ctx, doc, page, visited)
	results := make([]Violation, 0, len(appearances))
	for _, ap := range appearances {
		results = append(results, checkXObject(ctx, doc, ap.stream, ap.stream.Dict, "Form", "AP/"+ap.key, pageNum, part, profile, false)...)
	}
	return results
}

// appearance is one appearance stream of an annotation and the /AP entry, N,
// R or D, it stands under.
type appearance struct {
	stream ast.PdfStream
	key    ast.PdfName
}

// annotationAppearances returns the appearance streams of the annotations of
// page, in the order of /Annots and, for each annotation, of its normal,
// rollover and down appearances, a state subdictionary's streams by state
// name ([appearanceStreamObjects]). An entry that resolves to no stream is
// left out, and so is an annotation visited holds: veraPDF judges an
// annotation object once, however many pages list it.
func annotationAppearances(ctx context.Context, doc *document.Document, page ast.PdfDict, visited map[string]bool) []appearance {
	annots, err := document.ResolveArray(ctx, doc, page["Annots"])
	if err != nil || len(annots) == 0 {
		return nil
	}
	var out []appearance
	for _, annotObj := range annots {
		if ctx.Err() != nil {
			return out
		}
		if seenObject(annotObj, visited, "annot") {
			continue
		}
		annot, _ := document.ResolveDict(ctx, doc, annotObj) // Best-effort: an unreadable annotation has no appearance.
		if annot == nil {
			continue
		}
		apDict, _ := document.ResolveDict(ctx, doc, annot["AP"]) // Best-effort: an unreadable /AP has no appearance.
		if apDict == nil {
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
				out = append(out, appearance{stream: stream, key: key})
			}
		}
	}
	return out
}

// checkXObject validates a single XObject against the constraints for the given
// PDF/A part. stencil reports that the image is reached as another image's
// /Mask; see [checkImageBitsPerComponent].
func checkXObject(
	ctx context.Context,
	doc *document.Document,
	stream ast.PdfStream,
	xobjDict ast.PdfDict,
	subtype ast.PdfName,
	name ast.PdfName,
	pageNum int,
	part int,
	profile string,
	stencil bool,
) []Violation {
	var results []Violation

	// §6.2.9: Reference XObjects forbidden (all parts).
	// A /Ref key in a form XObject's dict makes it a reference XObject (ISO
	// 32000-1, 8.10.4, Table 95 gives /Ref to forms alone), and every profile
	// states the rule on PDXForm: measured with veraPDF 1.30 at PDF/A-1b and
	// PDF/A-2b, an image carrying /Ref fails neither ISO 19005-1, 6.2.6 t1
	// nor ISO 19005-2, 6.2.9 t2. The XObject presence tests here (/Ref, /OPI,
	// /PS, /Alternates) read the key as veraPDF's knownKey does ([knownKey]):
	// a direct null is absent (ISO 32000-1, 7.3.7), a reference counts.
	// Measured on each.
	if subtype == "Form" && knownKey(xobjDict, "Ref") {
		results = append(results, refXObjectRule(part, pageNum, name)...)
	}

	// PostScript XObjects forbidden (all parts).
	if subtype == "PS" {
		results = append(results, psXObjectRule(part, pageNum, name)...)
	}

	// Parts 2+/4 have additional form and image XObject checks.
	if part >= 2 {
		switch subtype {
		case "Form":
			results = append(results, checkFormXObject(ctx, doc, xobjDict, name, pageNum, part, profile)...)
		case "Image":
			results = append(results, checkImageXObject(ctx, doc, xobjDict, name, pageNum, part, stencil)...)
			// §6.2.8.3 / §6.2.7.3: JPEG2000 codestream constraints.
			if hasJPXFilter(ctx, doc, xobjDict) {
				results = append(results, checkJPXImage(ctx, doc, stream, name, pageNum, part)...)
			}
		}
	} else {
		// PDF/A-1 also checks form XObject forbidden keys (§6.2.5).
		if subtype == "Form" {
			results = append(results, checkFormXObjectPart1(ctx, doc, xobjDict, name, pageNum)...)
		}
		// PDF/A-1 also checks image constraints (§6.2.4).
		if subtype == "Image" {
			results = append(results, checkImageXObjectPart1(ctx, doc, xobjDict, name, pageNum, stencil)...)
		}
	}

	return results
}

// checkFormXObjectPart1 checks form XObject constraints for PDF/A-1 (§6.2.5).
func checkFormXObjectPart1(
	ctx context.Context,
	doc *document.Document,
	formDict ast.PdfDict,
	name ast.PdfName,
	pageNum int,
) []Violation {
	var results []Violation

	// /OPI key forbidden (ISO 19005-1 §6.2.4 t2 covers Image and Form
	// XObjects; the image path is checked in checkImageXObjectPart1).
	if knownKey(formDict, "OPI") {
		results = append(results, failf(Rule_19005_1_6_2_4_t02, "page %d, XObject %s", pageNum, name)...)
	}

	// A /Subtype2 of /PS and a /PS key are forbidden, one test for both.
	if faults := formPostScriptFaults(ctx, doc, formDict); len(faults) > 0 {
		results = append(results, failf(Rule_19005_1_6_2_5_t01, "page %d, XObject %s: %s", pageNum, name, strings.Join(faults, ", "))...)
	}

	return results
}

// formPostScriptFaults returns what makes a form XObject a PostScript one: a
// /Subtype2 of /PS and a /PS key (ISO 32000-1, 8.10.1 and 8.8.2). veraPDF
// states ISO 19005-1, 6.2.5 t1 and ISO 19005-2 and -3, 6.2.9 t1 as one test
// on the form, so a form carrying several of these fails it once. Measured
// with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on a form carrying /OPI and /PS.
//
// # Corpus evidence
//
//   - pdfjs issue9940.pdf: draws 10 forms each carrying /Subtype2 /PS and a
//     /PS key, and veraPDF states ISO 19005-1, 6.2.5 t1 and ISO 19005-2,
//     6.2.9 t1 10 times each, where litho stated 20.
func formPostScriptFaults(ctx context.Context, doc *document.Document, formDict ast.PdfDict) []string {
	var faults []string
	if sub2Obj, hasSub2 := formDict["Subtype2"]; hasSub2 {
		if sub2Name, _ := document.ResolveName(ctx, doc, sub2Obj); sub2Name == "PS" {
			faults = append(faults, "Subtype2 is PS")
		}
	}
	if knownKey(formDict, "PS") {
		faults = append(faults, "PS key present")
	}
	return faults
}

// checkImageXObjectPart1 checks image XObject constraints for PDF/A-1 (§6.2.4).
func checkImageXObjectPart1(
	ctx context.Context,
	doc *document.Document,
	imgDict ast.PdfDict,
	name ast.PdfName,
	pageNum int,
	stencil bool,
) []Violation {
	var results []Violation

	// /Alternates forbidden.
	if knownKey(imgDict, "Alternates") {
		results = append(results, failf(Rule_19005_1_6_2_4_t01, "page %d, XObject %s", pageNum, name)...)
	}

	// /OPI forbidden.
	if knownKey(imgDict, "OPI") {
		results = append(results, failf(Rule_19005_1_6_2_4_t02, "page %d, XObject %s", pageNum, name)...)
	}

	// /Interpolate must be false or absent.
	if interpObj, hasInterp := imgDict["Interpolate"]; hasInterp {
		interpVal, err := document.ResolveBool(ctx, doc, interpObj)
		if err == nil && interpVal {
			results = append(results, failf(Rule_19005_1_6_2_4_t03, "page %d, XObject %s", pageNum, name)...)
		}
	}

	// /Intent must be a valid rendering intent (§6.2.9), the same constraint
	// parts 2-4 enforce in checkImageXObject.
	if intentObj, hasIntent := imgDict["Intent"]; hasIntent {
		intent, _ := document.ResolveName(ctx, doc, intentObj)
		if !validRenderingIntents[string(intent)] {
			results = append(results, failf(renderingIntentRule(1),
				"page %d, XObject %s: invalid rendering intent %q", pageNum, name, intent)...)
		}
	}

	results = append(results, checkImageBitsPerComponent(ctx, doc, imgDict, name, pageNum, 1, stencil)...)

	return results
}

// checkFormXObject checks form XObject constraints for PDF/A parts 2+ and 4.
// ISO 19005-2 and -3, 6.2.9 t1 is one test over /OPI, /Subtype2 and /PS, so a
// form fails it once whatever it carries ([formPostScriptFaults]).
func checkFormXObject(
	ctx context.Context,
	doc *document.Document,
	formDict ast.PdfDict,
	name ast.PdfName,
	pageNum int,
	part int,
	_ string,
) []Violation {
	// Select the appropriate rule based on part.
	var formRule Rule
	switch part {
	case 2:
		formRule = Rule_19005_2_6_2_9_t01
	case 3:
		formRule = Rule_19005_3_6_2_9_t01
	case 4:
		formRule = Rule_19005_4_6_2_8_1_t01
	}

	// /OPI key forbidden in form XObject.
	var faults []string
	if knownKey(formDict, "OPI") {
		faults = append(faults, "OPI key present")
	}

	// ISO 19005-4, 6.2.8.1 forbids the OPI key and nothing else: ISO 32000-2
	// removed PostScript XObjects from the format, so part 4 has no PostScript
	// prohibition left to make. Only ISO 19005-2 and -3, 6.2.9 fold the three
	// prohibitions into one rule.
	if part != 4 {
		faults = append(faults, formPostScriptFaults(ctx, doc, formDict)...)
	}
	if len(faults) == 0 {
		return nil
	}
	return failf(formRule, "page %d, XObject %s: %s", pageNum, name, strings.Join(faults, ", "))
}

// checkImageXObject checks image XObject constraints for PDF/A parts 2+ and 4.
func checkImageXObject(
	ctx context.Context,
	doc *document.Document,
	imgDict ast.PdfDict,
	name ast.PdfName,
	pageNum int,
	part int,
	stencil bool,
) []Violation {
	var results []Violation

	// Select the appropriate rules based on part.
	var altRule, opiRule, interpRule Rule
	switch part {
	case 2:
		altRule = Rule_19005_2_6_2_8_t01
		opiRule = Rule_19005_2_6_2_8_t02
		interpRule = Rule_19005_2_6_2_8_t03
	case 3:
		altRule = Rule_19005_3_6_2_8_t01
		opiRule = Rule_19005_3_6_2_8_t02
		interpRule = Rule_19005_3_6_2_8_t03
	case 4:
		altRule = Rule_19005_4_6_2_7_1_t01
		opiRule = Rule_19005_4_6_2_7_1_t02
		interpRule = Rule_19005_4_6_2_7_1_t03
	}

	// /Alternates forbidden (§6.2.7/6.2.8).
	if knownKey(imgDict, "Alternates") {
		results = append(results, failf(altRule, "page %d, XObject %s", pageNum, name)...)
	}

	// /OPI forbidden (§6.2.8).
	if knownKey(imgDict, "OPI") {
		results = append(results, failf(opiRule, "page %d, XObject %s", pageNum, name)...)
	}

	// /Interpolate must be false or absent (§6.2.8).
	if interpObj, hasInterp := imgDict["Interpolate"]; hasInterp {
		interpVal, err := document.ResolveBool(ctx, doc, interpObj)
		if err == nil && interpVal {
			results = append(results, failf(interpRule, "page %d, XObject %s", pageNum, name)...)
		}
	}

	// /Intent must be a valid rendering intent (§6.2.6).
	if intentObj, hasIntent := imgDict["Intent"]; hasIntent {
		intent, _ := document.ResolveName(ctx, doc, intentObj)
		if !validRenderingIntents[string(intent)] {
			riRule := renderingIntentRule(part)
			results = append(results, failf(riRule,
				"page %d, XObject %s: invalid rendering intent %q", pageNum, name, intent)...)
		}
	}

	results = append(results, checkImageBitsPerComponent(ctx, doc, imgDict, name, pageNum, part, stencil)...)

	return results
}

// refXObjectRule returns the appropriate reference XObject rule violation for
// the given PDF/A part.
func refXObjectRule(part int, pageNum int, name ast.PdfName) []Violation {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_2_6_t01, "page %d, XObject %s", pageNum, name)
	case 2:
		return failf(Rule_19005_2_6_2_9_t02, "page %d, XObject %s", pageNum, name)
	case 3:
		return failf(Rule_19005_3_6_2_9_t02, "page %d, XObject %s", pageNum, name)
	case 4:
		return failf(Rule_19005_4_6_2_8_2_t01, "page %d, XObject %s", pageNum, name)
	}
	return nil
}

// psXObjectRule returns the appropriate PostScript XObject rule violation for
// the given PDF/A part.
func psXObjectRule(part int, pageNum int, name ast.PdfName) []Violation {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_2_7_t01, "page %d, XObject %s", pageNum, name)
	case 2:
		return failf(Rule_19005_2_6_2_9_t03, "page %d, XObject %s", pageNum, name)
	case 3:
		return failf(Rule_19005_3_6_2_9_t03, "page %d, XObject %s", pageNum, name)
		// PDF/A-4 does not have a separate PS XObject rule; it's covered by form checks.
	}
	return nil
}

// bpcRule selects the image /BitsPerComponent value rule for the given part.
func bpcRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_4_t04, Rule_19005_2_6_2_8_t04, Rule_19005_3_6_2_8_t04, Rule_19005_4_6_2_7_1_t04)
}

// bpcMaskRule selects the image-mask /BitsPerComponent rule for the given part.
func bpcMaskRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_4_t05, Rule_19005_2_6_2_8_t05, Rule_19005_3_6_2_8_t05, Rule_19005_4_6_2_7_1_t05)
}

// checkImageBitsPerComponent validates an image XObject's /BitsPerComponent.
// A regular image allows 1, 2, 4, or 8, and additionally 16 for PDF/A-2 and
// later (part 1: §6.2.4 t04, parts 2-3: §6.2.8 t04, part 4: §6.2.7.1 t04); an
// image reached as another image's /Mask, stencil, must use 1 (the
// corresponding t05). An absent key is permitted.
//
// veraPDF states t05 on PDMaskImage, the model object it builds for an
// image's /Mask alone, and t04 on every PDXImage unless its isMask, which only
// a PDMaskImage answers true, whatever /ImageMask says. Measured with veraPDF
// 1.30 at PDF/A-2b: a drawn image mask with /BitsPerComponent 2 fails neither
// ISO 19005-2, 6.2.8 t4 nor t5, and with 3 fails t4 alone; a /Mask with 2,
// or with 8 and no /ImageMask, fails t5 alone.
//
// JPXDecode images are skipped: their bit depth is defined by the JPEG2000
// codestream (validated separately) and /BitsPerComponent should be absent, so
// the value rule does not apply. This is a detection-only check: correcting the
// bit depth would require decompressing and resampling the pixel data, which
// risks visible corruption and is out of scope for remediation.
func checkImageBitsPerComponent(ctx context.Context, doc *document.Document, imgDict ast.PdfDict, name ast.PdfName, pageNum, part int, stencil bool) []Violation {
	bpcObj, ok := imgDict["BitsPerComponent"]
	if !ok {
		return nil
	}
	if hasJPXFilter(ctx, doc, imgDict) {
		return nil
	}
	bpc, err := document.ResolveInt(ctx, doc, bpcObj)
	if err != nil {
		return nil
	}

	if stencil {
		if bpc != 1 {
			return failf(bpcMaskRule(part), "page %d, XObject %s: image mask BitsPerComponent=%d (must be 1)", pageNum, name, bpc)
		}
		return nil
	}

	valid := bpc == 1 || bpc == 2 || bpc == 4 || bpc == 8 || (part >= 2 && bpc == 16)
	if !valid {
		return failf(bpcRule(part), "page %d, XObject %s: BitsPerComponent=%d", pageNum, name, bpc)
	}
	return nil
}
