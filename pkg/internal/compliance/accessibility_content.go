package compliance

import (
	"context"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// checkEncryptionPermissions validates PDF/UA-1 §7.16: if encrypted, the
// /Encrypt dictionary must contain a /P key whose 10th bit is true
// (accessibility content extraction permitted).
func checkEncryptionPermissions(ctx context.Context, doc *document.Document, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	if !doc.IsEncrypted {
		return nil
	}

	trailer := doc.Index.Trailer
	encObj, ok := trailer["Encrypt"]
	if !ok {
		return fail(Rule_14289_1_7_16_t01, "file is encrypted but no /Encrypt dictionary in trailer")
	}

	// Resolve the Encrypt dict (may be an indirect ref).
	var encDict ast.PdfDict
	switch v := encObj.(type) {
	case ast.PdfDict:
		encDict = v
	case ast.PdfIndirectRef:
		obj, err := doc.Resolve(ctx, v)
		if err != nil {
			return fail(Rule_14289_1_7_16_t01, "cannot resolve /Encrypt dictionary")
		}
		d, ok := obj.(ast.PdfDict)
		if !ok {
			return fail(Rule_14289_1_7_16_t01, "resolved /Encrypt is not a dictionary")
		}
		encDict = d
	default:
		return fail(Rule_14289_1_7_16_t01, "/Encrypt is not a dictionary")
	}

	pObj, hasP := encDict["P"]
	if !hasP {
		return fail(Rule_14289_1_7_16_t01, "encrypted file has no /P key in /Encrypt dictionary")
	}

	pVal, ok := pObj.(ast.PdfInt)
	if !ok {
		return fail(Rule_14289_1_7_16_t01, "/P is not an integer")
	}

	// Bit 10 (1-indexed) = bit 9 (0-indexed) = 0x200.
	const accessibilityBit = 1 << 9
	if int(pVal)&accessibilityBit == 0 {
		return failf(Rule_14289_1_7_16_t01,
			"10th bit of /P (%d) is false, accessibility content extraction not permitted", int(pVal))
	}

	return nil
}

// markOwner says how one level of marked content ties what it holds to the
// structure tree, the question veraPDF's isTaggedContent asks of a content
// item (GFSEGroupedContent).
type markOwner uint8

const (
	// ownerNone ties nothing: no /MCID, or one no ParentTree entry maps.
	ownerNone markOwner = iota
	// ownerReaches names a structure element whose /P chain reaches the
	// structure tree root, which tags the content.
	ownerReaches
	// ownerDetached names an element whose /P chain stops short of the root,
	// which leaves the content untagged.
	ownerDetached
	// ownerUnread stands for an MCID the ParentTree read, stopped by its depth
	// bound, did not reach. It counts as tagged: absence from a truncated read
	// is no evidence the file maps nothing, and a finding drawn from it would
	// be manufactured by the bound.
	ownerUnread
)

// markLevel is one level of the marked-content state an operator sits in: a
// BMC or BDC sequence still open, or the element a form XObject's
// /StructParent names, which holds everything the form draws.
type markLevel struct {
	// artifact is an /Artifact sequence.
	artifact bool
	owner    markOwner
	// tagged and inArtifact answer for this level and every one below it
	// ([pushLevel]), so asking costs one read however deep the nesting.
	tagged, inArtifact bool
}

// pushLevel opens level on top of levels. Content under it is tagged as the
// innermost level whose MCID the ParentTree maps decides, by whether its
// element reaches the root: a level mapping nothing passes the question
// outwards, and content no level decides is untagged. Measured against
// veraPDF 1.30.2: an unmapped /MCID inside a mapped one leaves the content
// tagged, a mapped one whose element has no /P inside a mapped one leaves it
// untagged, and a form's MCID yields to the page's MCID around the Do the same
// way. Content is an artifact under any /Artifact level.
func pushLevel(levels []markLevel, level markLevel) []markLevel {
	var below markLevel
	if n := len(levels); n > 0 {
		below = levels[n-1]
	}
	switch level.owner {
	case ownerReaches, ownerUnread:
		level.tagged = true
	case ownerDetached:
		level.tagged = false
	case ownerNone:
		level.tagged = below.tagged
	}
	level.inArtifact = level.artifact || below.inArtifact
	return append(levels, level)
}

// topLevel returns the innermost of levels, the zero level (untagged, no
// artifact) when none is open.
func topLevel(levels []markLevel) markLevel {
	if n := len(levels); n > 0 {
		return levels[n-1]
	}
	return markLevel{}
}

// markingFindings holds what a content walk found against ISO 14289-1, 7.1.
type markingFindings struct {
	// paints is real content neither tagged nor an artifact: 7.1 t3, and ISO
	// 14289-2, 8.2.2 t1.
	paints bool
	// untagged counts the content items behind paints. veraPDF states both
	// tests on SESimpleContentItem, one object per painting operator or
	// image, and reads each content stream once ([contentMarkingWalk]), so
	// each item it reaches first is a check of its own.
	untagged int
	// artifactInTagged is an /Artifact sequence opened inside tagged
	// content: 7.1 t1.
	artifactInTagged bool
	// taggedInArtifact is a tagged sequence under an /Artifact one: 7.1 t2.
	taggedInArtifact bool
}

// merge adds what o found to f.
func (f *markingFindings) merge(o markingFindings) {
	f.paints = f.paints || o.paints
	f.untagged += o.untagged
	f.artifactInTagged = f.artifactInTagged || o.artifactInTagged
	f.taggedInArtifact = f.taggedInArtifact || o.taggedInArtifact
}

// formMarkingKey identifies one walk of a form: what the form finds depends
// only on its content and on the state around the Do, which reduces to
// whether that state is tagged and whether it is an artifact.
type formMarkingKey struct {
	objNum   int
	tagged   bool
	artifact bool
	count    bool
}

// contentMarkingWalk carries one page's [checkContentMarking] walk: the page's
// resources, which name the property lists a form's own leave out, the forms
// already walked, per state around the Do, and the forms on the current chain,
// so a form that draws itself is not walked again.
//
// counted holds, for the whole document, the content streams whose items have
// been counted. veraPDF validates each object once, keyed by its object
// number, so a page content stream or a form XObject it meets again, on
// another page or through another Do, adds no content item: what it draws
// was counted, or skipped, the first time.
type contentMarkingWalk struct {
	doc        *document.Document
	structTree ast.PdfDict
	pageRes    ast.PdfDict
	partOne    bool
	walked     map[formMarkingKey]markingFindings
	onChain    map[int]bool
	counted    map[int]bool
}

// checkContentMarking validates PDF/UA-1 §7.1:
//   - t1: Artifact content must not appear inside tagged content
//   - t2: Tagged content must not appear inside Artifact
//   - t3: All painting operators must be inside a marked content span
//
// and ISO 14289-2, 8.2.2 t1, the PDF/UA-2 twin of t3.
//
// veraPDF builds a content item from each painting operator, and descends into
// the form XObject a Do draws, whose content inherits the marked-content state
// around the Do (ISO 32000-1, 14.6). A form that paints nothing is no content,
// an image XObject is, and the sequences a form opens are judged as the
// page's are. Content is tagged when [pushLevel] says so and exempt when an
// /Artifact sequence holds it. A sequence's /MCID comes from its property
// list, written inline or named from /Properties ([markedContentProps]). t1
// and t2 judge marked-content sequences rather than painting: an /Artifact
// sequence opened in tagged content fails both, being tagged content marked as
// an artifact, and a tagged sequence opened under an /Artifact one fails t2.
//
// This walk is the whole of t3 and 8.2.2 t1. A check that reported every /MCID
// the ParentTree does not map, wherever it sat, is retired: it failed an
// unmapped /MCID inside a mapped one, one wrapping no painting and one wrapping
// only an artifact, none of which veraPDF 1.30.2 reports, and it found nothing
// this walk does not.
//
// t1 and t2 are PDF/UA-1 concepts. PDF/UA-2 has no equivalent rule: its §8.2.2
// covers only unmarked content, so they stay gated to UA-1.
//
// Both profiles state t3 and 8.2.2 t1 on SESimpleContentItem, so each untagged
// content item is a check of its own, counted once per content stream
// ([contentMarkingWalk]). Every page is walked: stopping at the first untagged
// one also left t1 and t2 unjudged on the pages after it.
//
// # Corpus evidence
//
//   - pdfjs bug1019475_1.pdf: wraps its text in /H1 <</MCID 0>> BDC under a
//     catalog with no StructTreeRoot, and veraPDF reports it as untagged.
//   - govdocs thread_000_000632.pdf: wraps an image in /Artifact <</MCID 31>>
//     BDC that its /S /Artifact element owns, and veraPDF reports t1 and t2;
//     with the MCID left out of the ParentTree it reports neither.
//   - litho annothidden_2b.pdf: flattening draws each annotation's empty
//     appearance on an untagged page, and veraPDF reports neither t3 nor
//     8.2.2 t1 on the output, where litho did.
//   - pdfjs bug1703683_page2_reduced.pdf: draws dozens of empty forms outside
//     any marked content, and veraPDF reports neither t3 nor 8.2.2 t1, where
//     litho did.
//   - pdfjs issue3885.pdf: an untagged page drawing an empty form and a form
//     that draws only another empty form, and veraPDF reports neither t3 nor
//     8.2.2 t1, where litho did.
//   - All corpora: no other file changes its rule set at PDF/UA-1 or
//     PDF/UA-2.
//   - litho namedmcid_ua1.pdf: tags text through lists its page and three
//     forms name, and veraPDF reports neither t3 nor 8.2.2 t1, where litho
//     reported both.
//   - All corpora: dropping the orphan-MCID check moves no rule set at
//     PDF/UA-1, PDF/UA-2 or either WTPDF level. It lowers the t3 and 8.2.2 t1
//     counts, from 2 to 1 on 25 files and from 4 to 1 on govdocs
//     thread_200_200824.pdf.
//   - litho and verapdf corpora: counting per content item puts t3 and 8.2.2
//     t1 on veraPDF's count in 7500 of the 7502 cells where either fires, up
//     from 5814, and moves no rule set. The two left are litho
//     baretree_2b.pdf, whose root /Count lets veraPDF read four of its five
//     pages.
//   - litho imagereach_2b.pdf: two pages share one content stream, and
//     veraPDF reports only the first page's items.
//   - litho fontcopies_2b.pdf: forms two pages draw, which veraPDF reports
//     under the first page only.
//   - litho multipage_2b.pdf: a PostScript XObject drawn untagged, which
//     veraPDF does not count.
func checkContentMarking(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !IsPDFUA(profile) {
		return nil
	}
	// Best-effort: no readable structure tree tags nothing, which is how
	// veraPDF judges it too.
	structTree, _ := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])

	pages := collectPageObjects(ctx, doc, catalog)
	counted := make(map[int]bool)
	untaggedRule := Rule_14289_1_7_1_t03
	if profile == "PDF/UA-2" {
		untaggedRule = Rule_14289_2_8_2_2_t01
	}
	var results []Violation
	// firstPainting is the first page painting untagged content, 0 when none
	// does.
	items, firstPainting := 0, 0
	for i := range pages {
		if ctx.Err() != nil {
			return results
		}
		page := pages[i].dict
		data, err := resolvePageContentBytes(ctx, doc, page)
		if err != nil || len(data) == 0 {
			continue
		}
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			doc.Logger.Debug("compliance.accessibility: cannot parse content stream", "error", err)
			continue
		}
		// A page that states no /Resources inherits its nearest ancestor's (ISO
		// 32000-1 §7.7.3.4), so a Do whose XObject a /Pages node defines still
		// resolves; a page that states its own, even empty, does not inherit.
		pageRes := pages[i].effectiveResources(ctx, doc)
		w := contentMarkingWalk{
			doc:        doc,
			structTree: structTree,
			pageRes:    pageRes,
			partOne:    profile == "PDF/UA-1",
			walked:     make(map[formMarkingKey]markingFindings),
			onChain:    make(map[int]bool),
			counted:    counted,
		}
		props := &markedContentProps{doc: doc, page: page, resources: pageRes}
		found := w.stream(ctx, ops, page, pageRes, props, nil, 0, firstContentVisit(counted, page["Contents"]))
		if found.artifactInTagged {
			results = append(results, failf(Rule_14289_1_7_1_t01,
				"content marked as Artifact inside tagged content")...)
		}
		if found.taggedInArtifact {
			results = append(results, failf(Rule_14289_1_7_1_t02,
				"tagged content inside content marked as Artifact")...)
		}
		if found.paints && firstPainting == 0 {
			firstPainting = i + 1
		}
		for range found.untagged {
			items++
			results = append(results, failf(untaggedRule,
				"page %d content item %d not marked as Artifact or tagged", i+1, items)...)
		}
	}
	// Untagged painting that left no item to count, a PostScript XObject or a
	// stream veraPDF read on an earlier page, keeps the finding litho states
	// for the document.
	if items == 0 && firstPainting != 0 {
		results = append(results, failf(untaggedRule,
			"page %d content not marked as Artifact or tagged", firstPainting)...)
	}
	return results
}

// stream walks one content stream under the levels around it. holder is the
// dictionary whose /StructParents keys the stream's MCIDs into the ParentTree
// (the page, or the form), res resolves the XObjects it draws, props reads the
// property lists its BDC operators carry, inline or named, and depth is the form
// nesting, which [formTooDeep] bounds.
//
// A sequence the stream opens closes within it: an EMC with none of the
// stream's own open closes nothing around the Do, as veraPDF reads it.
func (w *contentMarkingWalk) stream(ctx context.Context, ops []content.Operation, holder, res ast.PdfDict, props *markedContentProps, outer []markLevel, depth int, count bool) markingFindings {
	owners, complete := w.mcidOwners(ctx, holder)
	levels := slices.Clone(outer)
	floor := len(levels)
	unclosed := unclosedMarkedTail(ops)
	var found markingFindings
	for i, op := range ops {
		if i%contentOpCancelCheckInterval == 0 && ctx.Err() != nil {
			return found
		}
		// Past unclosed, every operator lies in a sequence the stream never
		// closes, which marks nothing: its BMC, BDC and EMC open and close no
		// level, and what it paints stands in the state around the stream.
		if i >= unclosed && (op.Operator == "BMC" || op.Operator == "BDC" || op.Operator == "EMC") {
			continue
		}
		switch op.Operator {
		case "BMC", "BDC":
			level := markLevel{owner: ownerNone}
			if len(op.Operands) > 0 {
				tag, _ := op.Operands[0].(ast.PdfName)
				level.artifact = tag == "Artifact"
			}
			if mcid := props.mcid(ctx, op); mcid >= 0 && w.structTree != nil {
				reach, mapped := owners[mcid]
				switch {
				case mapped && reach:
					level.owner = ownerReaches
				case mapped:
					level.owner = ownerDetached
				case !complete:
					level.owner = ownerUnread
				}
			}
			levels = pushLevel(levels, level)
			if top := topLevel(levels); w.partOne && top.tagged {
				found.artifactInTagged = found.artifactInTagged || top.artifact
				found.taggedInArtifact = found.taggedInArtifact || top.inArtifact
			}
		case "EMC":
			if len(levels) > floor {
				levels = levels[:len(levels)-1]
			}
		case "Do":
			found.merge(w.do(ctx, op, res, topLevel(levels), depth, count))
		// BI is an inline image (ISO 32000-1, 8.9.7), which paints an image
		// exactly as a Do naming an image XObject does; veraPDF builds a
		// content item for it like any other.
		case "f", "F", "f*", "S", "s", "B", "B*", "b", "b*",
			"Tj", "TJ", "'", "\"", "sh", "BI":
			if top := topLevel(levels); !top.tagged && !top.inArtifact {
				found.paints = true
				if count {
					found.untagged++
				}
			}
		}
	}
	return found
}

// unclosedMarkedTail returns the index of the first operator past the last
// point where ops leave every marked-content sequence they open closed, or
// len(ops) when the stream closes all of them. ISO 32000-1, 14.6 delimits a
// sequence by its BMC or BDC and the matching EMC, so the operators from that
// index on lie in a sequence that never ends and is no marked-content
// sequence at all. veraPDF reads them the same way: its semantic content
// stream (GFPDSemanticContentStream.getContentItem) makes everything after
// the last balanced EMC one unmarked group when the stream ends with a
// sequence open. An EMC with no sequence open closes nothing.
//
// # Corpus evidence
//
//   - pdfjs poppler-90-0-fuzzed.pdf: page 2 opens a literal string that never
//     terminates, so the EMC remediation appends after its /Artifact BMC is
//     lexed inside the string, and veraPDF reports the page's first three
//     items under ISO 14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1.
func unclosedMarkedTail(ops []content.Operation) int {
	depth, tail := 0, 0
	for i, op := range ops {
		switch op.Operator {
		case "BMC", "BDC":
			depth++
		case "EMC":
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 {
				tail = i + 1
			}
		}
	}
	if depth == 0 {
		return len(ops)
	}
	return tail
}

// mcidOwners maps each MCID the ParentTree entry of holder's /StructParents
// names to whether its element reaches the root, and reports whether the
// read was whole. A document with no structure tree maps nothing.
func (w *contentMarkingWalk) mcidOwners(ctx context.Context, holder ast.PdfDict) (map[int]bool, bool) {
	if w.structTree == nil {
		return nil, true
	}
	return parentTreeMCIDReach(ctx, w.doc, w.structTree, holder)
}

// do judges the XObject a Do draws under the innermost open level, around. A
// name the resources do not define draws nothing (ISO 32000-1 §8.10.1). A form
// XObject draws its content, and any other XObject paints: veraPDF 1.30.2
// reports an image with a /StructParent as untagged, so only a form draws a
// tag from one. Only an image is a content item to count (SEImageXObjectItem);
// veraPDF builds none for a PostScript XObject (ISO 32000-1, 8.8.2), which
// litho still judges as painting, so a page drawing one alone keeps the
// finding it had. count says whether the stream holding the Do is read for
// the first time ([contentMarkingWalk]).
func (w *contentMarkingWalk) do(ctx context.Context, op content.Operation, res ast.PdfDict, around markLevel, depth int, count bool) markingFindings {
	if res == nil || len(op.Operands) < 1 {
		return markingFindings{}
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return markingFindings{}
	}
	// Best-effort: no /XObject dictionary defines no name.
	xobjects, _ := document.ResolveDict(ctx, w.doc, res["XObject"])
	if xobjects == nil {
		return markingFindings{}
	}
	obj, ok := xobjects[name]
	if !ok {
		return markingFindings{}
	}
	stream, err := document.ResolveStream(ctx, w.doc, obj)
	if err != nil || stream.Dict == nil {
		return markingFindings{}
	}
	// Best-effort: a /Subtype that is no name reads as absent.
	if subtype, _ := document.ResolveName(ctx, w.doc, stream.Dict["Subtype"]); subtype != "Form" {
		found := markingFindings{paints: !around.tagged && !around.inArtifact}
		if found.paints && count && subtype == "Image" {
			found.untagged = 1
		}
		return found
	}
	ref, _ := obj.(ast.PdfIndirectRef)
	return w.form(ctx, stream, ref.ObjectNum, res, around.tagged, around.inArtifact, depth+1, count)
}

// form walks a form XObject drawn in a state that is tagged or not and an
// artifact or not. Its names resolve against its own /Resources, the invoking
// stream's filling the gaps ([appearanceResources]). A form already on the
// chain that draws it adds nothing: its first visit counted what it paints.
//
// A form past the form budget ([formTooDeep]) is not read, and counts as
// painting in the state around its Do, as litho judged every form before it
// read them: the cap then withholds no finding a painting form deeper than the
// budget draws, which veraPDF, reading every level, reports.
//
// A form's findings are kept per state and reused within the page, whatever
// resources the next invocation lends: a form with no /Resources of its own
// resolves the names it borrows from the first stream that draws it. A form
// met before counts no item ([contentMarkingWalk]).
//
// # Corpus evidence
//
//   - verapdf PDF_A-1b__veraPDF_test_suite_6-1-12-t08-pass-c.pdf: paints
//     untagged at the end of a chain of 33 forms, and veraPDF reports ISO
//     14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1.
func (w *contentMarkingWalk) form(ctx context.Context, stream ast.PdfStream, objNum int, parentRes ast.PdfDict, tagged, artifact bool, depth int, count bool) markingFindings {
	if ctx.Err() != nil {
		return markingFindings{}
	}
	if formTooDeep(w.doc, depth) {
		found := markingFindings{paints: !tagged && !artifact}
		if found.paints && count {
			found.untagged = 1
		}
		return found
	}
	key := formMarkingKey{objNum: objNum, tagged: tagged, artifact: artifact, count: count}
	if objNum != 0 {
		if found, ok := w.walked[key]; ok {
			found.untagged = 0
			return found
		}
		if w.onChain[objNum] {
			return markingFindings{}
		}
		w.onChain[objNum] = true
		defer delete(w.onChain, objNum)
	}
	if count && objNum != 0 {
		count = firstContentVisit(w.counted, ast.PdfIndirectRef{ObjectNum: objNum})
	}
	var found markingFindings
	// veraPDF reads no operator from a form it cannot decode
	// ([decodeContentStream]).
	if data, reads := decodeContentStream(ctx, w.doc, stream); reads {
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			w.doc.Logger.Debug("compliance.accessibility: cannot parse Form XObject content stream", "error", err)
		} else {
			// The state around the Do stands as one level below the form's
			// own, deciding for them when none of theirs does.
			around := markLevel{artifact: artifact, owner: ownerDetached}
			if tagged {
				around.owner = ownerReaches
			}
			outer := pushLevel(nil, around)
			if level, ok := w.structParentLevel(ctx, stream.Dict); ok {
				outer = pushLevel(outer, level)
			}
			// Best-effort: a form with no readable /Resources names through
			// the stream that draws it.
			own, _ := document.ResolveDict(ctx, w.doc, stream.Dict["Resources"])
			props := &markedContentProps{doc: w.doc, page: stream.Dict, resources: own, pageResources: w.pageRes}
			found = w.stream(ctx, ops, stream.Dict, appearanceResources(ctx, w.doc, own, parentRes), props, outer, depth, count)
		}
	}
	if objNum != 0 {
		w.walked[key] = found
	}
	return found
}

// structParentLevel returns the level a form XObject's /StructParent adds
// around its content: the element the ParentTree names for it (ISO 32000-1,
// 14.7.4.4) tags everything the form draws when it reaches the root, and
// leaves it untagged when it does not. A /StructParent the ParentTree does
// not map adds nothing. Measured against veraPDF 1.30.2, which also descends
// into such a form for 7.1 t1 and t2.
func (w *contentMarkingWalk) structParentLevel(ctx context.Context, form ast.PdfDict) (markLevel, bool) {
	if w.structTree == nil {
		return markLevel{}, false
	}
	if _, ok := form["StructParent"]; !ok {
		return markLevel{}, false
	}
	elem, complete := structParentEntry(ctx, w.doc, w.structTree, form)
	switch {
	case elem != nil && !isNullObject(elem):
		if structElemReachesRoot(ctx, w.doc, elem) {
			return markLevel{owner: ownerReaches}, true
		}
		return markLevel{owner: ownerDetached}, true
	case !complete:
		return markLevel{owner: ownerUnread}, true
	}
	return markLevel{}, false
}

func ocConfigNameRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_7_t01
	}
	return Rule_14289_1_7_10_t01
}
