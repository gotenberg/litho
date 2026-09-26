package compliance

import (
	"bytes"
	"context"
	"slices"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// fontUsageInfo tracks how a font is used in a content stream.
type fontUsageInfo struct {
	visible    bool            // true if used with rendering mode != 3
	charCodes  map[byte]bool   // character codes referenced by Tj/TJ
	rawStrings []ast.PdfString // raw string operands from text operators (for CID .notdef detection)
	shows      []glyphShow     // rawStrings again, each with the context it is shown in
}

// ShownCharCodes returns, per font resource name, the one-byte character codes
// the content stream's text operators actually show.
//
// Remediation shares this with the checks so a generated /ToUnicode is held to
// the same set of codes the validator holds it to: ISO 19005-2, 6.2.11.7.2 asks
// for a mapping of the codes a page USES, and a CMap built from a font's
// /Encoding alone need not cover every one of them.
func ShownCharCodes(ops []content.Operation) map[string]map[byte]bool {
	usage := collectFontUsage(ops)
	if usage == nil {
		return nil
	}
	out := make(map[string]map[byte]bool, len(usage))
	for name, u := range usage {
		if len(u.charCodes) > 0 {
			out[name] = u.charCodes
		}
	}
	return out
}

// collectFontUsage returns usage information for each font referenced in the
// page content stream: whether it's used for visible rendering and which
// character codes are referenced. Returns nil if ops is nil or empty (in which
// case all fonts should be checked).
func collectFontUsage(ops []content.Operation) map[string]*fontUsageInfo {
	return collectFontUsageAt(ops, showSite{})
}

// collectFontUsageAt is [collectFontUsage] for the content stream site names,
// which keys the marked-content sequences its shows are recorded in; see
// [markedContentKey].
func collectFontUsageAt(ops []content.Operation, site showSite) map[string]*fontUsageInfo {
	if len(ops) == 0 {
		return nil
	}

	usage := make(map[string]*fontUsageInfo)
	currentFont := ""
	renderMode := 0 // default rendering mode is 0 (fill)

	// Per ISO 32000-1, 8.4.2 the graphics state includes the text state, and
	// Table 104 counts the text font and the text rendering mode among its
	// parameters, so Q restores both. Reading Tf as if it persisted past the
	// enclosing Q attributes character codes to whichever font happens to have
	// been selected last, which is how an unrelated font came to be reported
	// for glyphs it never showed. The stack holds one entry per q operator
	// already present in the materialized operator slice, so it needs no bound
	// of its own.
	type savedTextState struct {
		font       string
		renderMode int
	}
	var gsStack []savedTextState
	// Marked content is not part of the graphics state (ISO 32000-1, 14.6), so
	// q and Q leave this stack alone. Like gsStack it holds at most one entry
	// per operator already materialized.
	var mcStack []string

	getOrCreate := func(name string) *fontUsageInfo {
		if u, ok := usage[name]; ok {
			return u
		}
		u := &fontUsageInfo{charCodes: make(map[byte]bool)}
		usage[name] = u
		return u
	}

	record := func(u *fontUsageInfo, s ast.PdfString) {
		for _, b := range []byte(s) {
			u.charCodes[b] = true
		}
		u.rawStrings = append(u.rawStrings, s)
		at := glyphContext{mode: renderMode}
		if n := len(mcStack); n > 0 {
			at.mc = mcStack[n-1]
		}
		u.shows = append(u.shows, glyphShow{str: s, at: at})
	}

	collectStringBytes := func(obj ast.PdfObject) {
		if currentFont == "" {
			return
		}
		u := getOrCreate(currentFont)
		switch s := obj.(type) {
		case ast.PdfString:
			record(u, s)
		case ast.PdfArray:
			for _, item := range s {
				if str, ok := item.(ast.PdfString); ok {
					record(u, str)
				}
			}
		}
	}

	for i, op := range ops {
		switch op.Operator {
		case "q":
			gsStack = append(gsStack, savedTextState{font: currentFont, renderMode: renderMode})
		case "Q":
			if n := len(gsStack); n > 0 {
				currentFont, renderMode = gsStack[n-1].font, gsStack[n-1].renderMode
				gsStack = gsStack[:n-1]
			}
		case "BDC", "BMC":
			mcStack = append(mcStack, markedContentKey(op, i, site))
		case "EMC":
			if n := len(mcStack); n > 0 {
				mcStack = mcStack[:n-1]
			}
		case "Tf":
			if len(op.Operands) >= 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					currentFont = string(name)
				}
			}
		case "Tr":
			if len(op.Operands) >= 1 {
				if modeInt, ok := op.Operands[0].(ast.PdfInt); ok {
					renderMode = int(modeInt)
				}
			}
		case "Tj":
			if currentFont != "" && renderMode != 3 {
				getOrCreate(currentFont).visible = true
			}
			if len(op.Operands) >= 1 {
				collectStringBytes(op.Operands[0])
			}
		case "TJ":
			if currentFont != "" && renderMode != 3 {
				getOrCreate(currentFont).visible = true
			}
			if len(op.Operands) >= 1 {
				collectStringBytes(op.Operands[0])
			}
		case "'", "\"":
			if currentFont != "" && renderMode != 3 {
				getOrCreate(currentFont).visible = true
			}
			if len(op.Operands) >= 1 {
				collectStringBytes(op.Operands[len(op.Operands)-1])
			}
		}
	}

	return usage
}

// checkFontGroup validates font-related compliance rules across all pages.
func checkFontGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile

	doc.Logger.Debug("compliance: fonts: starting font checks", "profile", profile)

	if len(vctx.pages) == 0 {
		return nil
	}

	var results []Violation

	// Track fonts already checked to avoid duplicate violations. checked keys on
	// the page-resource name; judged keys on the resolved object number so the
	// annotation-appearance pass does not re-check a font also declared in a
	// page's /Resources/Font.
	checked := make(map[string]bool)
	judged := newFontJudgements()

	// The embedding rule runs once per font object, on the first page that
	// selects it, but it is gated on visible rendering. A font drawn invisibly
	// on that page and visibly on a later one must still be judged, so
	// visibility is answered for the whole document rather than one page.
	visibleAnywhere := collectVisibleFontObjects(ctx, doc, vctx)

	// Form XObject glyphs reach the PUA check through the structure tree root,
	// which keys their marked content as the page's does.
	treeRoot, _ := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])

	for pageIdx := range vctx.pages {
		p := &vctx.pages[pageIdx]
		if ctx.Err() != nil {
			return results
		}
		// A content stream an earlier page shares is judged there alone, with
		// the fonts that page's resources name, so this page selects no font
		// through it; see [markJudgedContents].
		judgeContent := !p.contentJudged

		if judgeContent {
			results = append(results, checkPageDeclaredFonts(ctx, doc, catalog, p, pageIdx+1, profile, checked, judged, visibleAnywhere)...)
		}

		// Fonts referenced only from annotation appearance streams are never
		// reached by the page /Resources/Font walk above. veraPDF validates them
		// through the appearance content stream (for example
		// annots[0]/appearance[0]/contentStream[0]/operators[N]/font[0]); litho
		// matches by checking their subset glyph coverage here.
		results = append(results, checkAnnotationAppearanceFonts(ctx, doc, p.dict, treeRoot, pageIdx+1, profile, judged)...)

		if !judgeContent {
			continue
		}

		// Fonts used only inside a Form XObject invoked from the page content
		// (a Do operator) are likewise not reached by the page /Resources/Font
		// walk. veraPDF descends into the form's content stream and applies the
		// same font rules there; litho matches by walking invoked forms.
		xobjVisited := make(map[int]bool)
		results = append(results, checkFormXObjectFonts(ctx, doc, p.parsedOps(ctx), p.resources, treeRoot, pageIdx+1, profile, judged, xobjVisited, 0)...)

		// A tiling pattern's cell is another content stream the page walk above
		// never enters. See checkPatternFonts.
		results = append(results, checkPatternFonts(ctx, doc, p.parsedOps(ctx), p.resources, treeRoot, pageIdx+1, profile, judged, xobjVisited, 0)...)

		// A Type3 glyph procedure is another content stream that may select
		// fonts of its own, and the page walk above stops at the Type3 font
		// itself. veraPDF applies the font rules inside those procedures too.
		results = append(results, checkType3CharProcFonts(ctx, doc, p.parsedOps(ctx), p.resources, treeRoot, pageIdx+1, profile, judged, xobjVisited, 0)...)
	}

	doc.Logger.Debug("compliance: fonts: font checks complete",
		"violations", len(results), "fonts_checked", len(checked))
	return results
}

// fontJudgements records, for one run of the font group, what it has stated
// about each font object, so every rule is stated as often as veraPDF states
// it: veraPDF judges an object it reaches again only the first time.
type fontJudgements struct {
	// dict holds the font objects whose dictionary rules were judged.
	dict map[int]bool
	// subset holds the font objects the subset rule failed for; see
	// [fontJudgements.subsetCoverage].
	subset map[int]bool
	// glyph holds the glyph-level statements already made; see
	// [fontJudgements.glyphs].
	glyph map[glyphKey]bool
}

// newFontJudgements returns an empty record.
func newFontJudgements() *fontJudgements {
	return &fontJudgements{dict: make(map[int]bool), subset: make(map[int]bool), glyph: make(map[glyphKey]bool)}
}

// subsetCoverage states the rule a font subset's /CharSet or /CIDSet answers
// (see [checkSubsetCoverage]) against the codes one content stream shows
// through fontRef, unless it already failed for that font object.
//
// veraPDF states the rule on the font object, PDType1Font for /CharSet and
// PDCIDFont for /CIDSet, and judges that object once for the whole document.
// ISO 19005-1, 6.3.5 t2 and t3 are deferred rules, which veraPDF evaluates
// once every glyph of the document has been read, so a subset fails them when
// any page renders a glyph the set leaves out. Stating the rule for the first
// failing page alone gives that count. Measured with veraPDF 1.30 at PDF/A-1b
// and PDF/A-2b: two pages showing the same codes through one Type1 subset
// with no /CharSet, or one Type0 subset whose /CIDSet leaves them out, fail
// ISO 19005-1, 6.3.5 t2 or t3 and ISO 19005-2, 6.2.11.4.2 t2 once, whether
// the pages share one content stream or draw one each.
//
// A font written directly in a resource dictionary has no object to key on,
// and is judged wherever it is used.
//
// # Corpus evidence
//
//   - pdfjs issue14847.pdf: shows two Type0 subsets whose /CIDSet leaves out
//     rendered CIDs on four pages, and veraPDF fails ISO 19005-1, 6.3.5 t3
//     and ISO 19005-2, 6.2.11.4.2 t2 twice, once per font, where litho failed
//     them eight times.
//   - All corpora: 237 counts of ISO 19005-1, 6.3.5 t2 and t3 and ISO
//     19005-2, 6.2.11.4.2 t2 at PDF/A-1b and PDF/A-2b moved toward veraPDF's
//     and none away, 217 of them onto it; their summed distance from veraPDF
//     fell from 4497 to 84.
func (j *fontJudgements) subsetCoverage(ctx context.Context, doc *document.Document, fontRef ast.PdfObject, font ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	ref, indirect := fontRef.(ast.PdfIndirectRef)
	if indirect && j.subset[ref.ObjectNum] {
		return nil
	}
	results := checkSubsetCoverage(ctx, doc, font, fontName, pageNum, profile, usage)
	if indirect && len(results) > 0 {
		j.subset[ref.ObjectNum] = true
	}
	return results
}

// checkSubsetCoverage judges the /CharSet of a Type1 subset or the /CIDSet of
// the descendant of a Type0 subset against the codes usage shows. See
// [checkGlyphCoverage] and [checkCIDFontGlyphCoverage].
func checkSubsetCoverage(ctx context.Context, doc *document.Document, font ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	if fontSubtype(ctx, doc, font) != "Type0" {
		return checkGlyphCoverage(ctx, doc, font, fontName, pageNum, profile, usage)
	}
	descArr, err := document.ResolveArray(ctx, doc, font["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return nil
	}
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		return nil
	}
	// The shown codes decide whether the font renders at all, and the CIDs
	// they select scope the checks that judge rendered glyphs alone.
	return checkCIDFontGlyphCoverage(ctx, doc, cidFontDict, fontName, pageNum, profile, usage != nil && len(usage.charCodes) > 0, type0UsedCIDs(ctx, doc, font, usage))
}

// checkPageDeclaredFonts runs the full font-check suite over the fonts declared
// in a page's /Resources/Font. It records each resolved font object number in
// judged so [checkAnnotationAppearanceFonts] does not re-check a font that is
// also declared on the page.
//
// # Corpus evidence
//
//   - pdfjs issue11678.pdf: its page 2 stream declares /Length 4218 over no
//     data, and judging glyphs on unparsable content reports a .notdef
//     against recovered bytes veraPDF never reads.
func checkPageDeclaredFonts(ctx context.Context, doc *document.Document, catalog ast.PdfDict, p *pageCtx, pageNum int, profile string, checked map[string]bool, judged *fontJudgements, visibleAnywhere map[int]bool) []Violation {
	resources := p.resources
	if resources == nil {
		return nil
	}

	fontDictObj, ok := resources["Font"]
	if !ok {
		return nil
	}

	fontDict, err := document.ResolveDict(ctx, doc, fontDictObj)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: failed to resolve /Font dict",
			"page", pageNum, "error", err)
		return nil
	}

	// Collect font usage from the page content stream: which fonts are
	// visible (rendering mode != 3) and which character codes are used. The
	// per-font skip below uses this to match veraPDF's "check only fonts a
	// content operator selects" model; parseFailed drives the conservative
	// "check every declared font" fallback for present-but-unparseable content.
	site := showSite{key: "page " + strconv.Itoa(pageNum)}
	site.props, _ = document.ResolveDict(ctx, doc, resources["Properties"])
	fontUsage := collectFontUsageAt(p.parsedOps(ctx), site)
	parseFailed := p.parseFailed

	var results []Violation
	for _, fontName := range sortedNames(fontDict) {
		fontRef := fontDict[fontName]
		if ctx.Err() != nil {
			return results
		}

		fontKey := string(fontName)
		// Dedup by resolved object identity, not resource name: different pages
		// may bind the same name to different font objects (e.g. an embedded
		// subset "TT1" on one page and an unembedded standard "TT1" on another),
		// and veraPDF checks each distinct font object. A direct (inline) font
		// dict has no object number and is page-local, so it falls back to
		// name dedup.
		ref, isIndirect := fontRef.(ast.PdfIndirectRef)
		// A font object already seen has had its dictionary rules reported, but
		// this page's usage is still unjudged: the codes it shows here may
		// differ from those on the page that first selected it.
		dictRulesDone := false
		if isIndirect {
			dictRulesDone = judged.dict[ref.ObjectNum]
		} else if checked[fontKey] {
			continue
		}

		// veraPDF builds a font object only when a content operator selects it
		// (Tf) and shows text with it, so a font declared in /Resources/Font but
		// never selected on this page renders no glyph here and is out of scope
		// for the page pass. Checking it would report encoding (§6.2.11.6),
		// embedding (§6.2.11.4.1), or width defects veraPDF never raises, since
		// those rules constrain glyphs actually shown. A font rendered instead by
		// an annotation appearance or a Form XObject is reached by the dedicated
		// passes in checkFontGroup with that context's usage, so it is left
		// unmarked in judged here to avoid starving them. Only present but
		// unparseable content forces the conservative "check every declared font"
		// fallback (litho cannot prove non-use).
		var usage *fontUsageInfo
		if fontUsage != nil {
			usage = fontUsage[fontKey]
		}
		if usage == nil && !parseFailed {
			continue
		}

		font, err := document.ResolveDict(ctx, doc, fontRef)
		if err != nil || font == nil {
			doc.Logger.Debug("compliance: fonts: failed to resolve font dict",
				"font", fontKey, "page", pageNum, "error", err)
			continue
		}

		checked[fontKey] = true
		if isIndirect {
			judged.dict[ref.ObjectNum] = true
		}
		// Skip width checks for fonts not used for visible rendering here.
		isVisible := parseFailed || (usage != nil && usage.visible)
		// Embedding is judged once per object, so it asks whether any page
		// renders the font visibly.
		embedVisible := isVisible
		if isIndirect && visibleAnywhere[ref.ObjectNum] {
			embedVisible = true
		}

		var fontResults []Violation
		if !dictRulesDone {
			fontResults = append(fontResults, checkFontDictRules(ctx, doc, font, fontKey, pageNum, profile, embedVisible, usage)...)
		}
		isType0 := fontSubtype(ctx, doc, font) == "Type0"
		if isType0 {
			if dictRulesDone {
				// The dictionary was reported on an earlier page, but this page's
				// glyphs are still unjudged. See checkType0UsageRules. Content
				// litho could not parse names no glyphs it can judge: the
				// conservative "check every declared font" fallback exists for the
				// dictionary rules, and judging glyphs there reports a .notdef
				// against recovered bytes veraPDF never reads.
				if descArr, err := document.ResolveArray(ctx, doc, font["DescendantFonts"]); err == nil && len(descArr) > 0 && !parseFailed {
					if cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0]); err == nil && cidFontDict != nil {
						fontResults = append(fontResults, checkType0UsageRules(ctx, doc, font, cidFontDict, fontKey, pageNum, profile, usage, withoutMalformedCodes)...)
					}
				}
			} else {
				fontResults = append(fontResults, checkType0Font(ctx, doc, font, fontKey, pageNum, profile, isVisible, usage)...)
			}
		} else {
			fontResults = append(fontResults, checkFontUsageRules(ctx, doc, font, fontKey, pageNum, profile, isVisible, usage)...)
		}
		results = append(results, judged.glyphs(ctx, doc, fontIdentity(fontRef, fontName, site), font, usage, profile, fontResults)...)
		// The subset rule judges glyphs too, so the unparsable content above
		// states nothing more for a composite font judged on an earlier page.
		if !isType0 || !dictRulesDone || !parseFailed {
			results = append(results, judged.subsetCoverage(ctx, doc, fontRef, font, fontKey, pageNum, profile, usage)...)
		}

		// PUA ActualText check (needs page and catalog context).
		results = append(results, checkPUAWithoutActualText(ctx, doc, font, fontKey, pageNum, profile, p.dict, catalog)...)
	}
	return results
}

// checkFormXObjectFonts checks fonts used for rendering inside Form XObjects
// invoked (via a Do operator) from a content stream's operations. veraPDF
// descends into a Form XObject's content stream and applies the same font rules
// to the fonts it selects (for example a non-embedded font referenced only from
// a Do-invoked form, ISO 19005-2 §6.2.11.4.1); litho's page pass walks
// /Resources/Font on the page only, so such a font is otherwise never reached.
// Recursion into nested forms is bounded by the form budget ([formTooDeep]);
// visited guards resource cycles; judged deduplicates against fonts
// already checked on the page and across forms.
func checkFormXObjectFonts(ctx context.Context, doc *document.Document, ops []content.Operation, res, treeRoot ast.PdfDict, pageNum int, profile string, judged *fontJudgements, visited map[int]bool, depth int) []Violation {
	if formTooDeep(doc, depth) || ctx.Err() != nil {
		return nil
	}
	xobjDict, _ := document.ResolveDict(ctx, doc, res["XObject"])
	if len(xobjDict) == 0 {
		return nil
	}

	var results []Violation
	for _, op := range ops {
		if op.Operator != "Do" || len(op.Operands) < 1 {
			continue
		}
		name, ok := op.Operands[0].(ast.PdfName)
		if !ok {
			continue
		}
		xobjRef := xobjDict[name]
		ref, isRef := xobjRef.(ast.PdfIndirectRef)
		if isRef && visited[ref.ObjectNum] {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, xobjRef)
		if err != nil || stream.Dict == nil {
			continue
		}
		if subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]); subtype != "Form" {
			continue
		}
		if isRef {
			visited[ref.ObjectNum] = true
		}
		xobjRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if xobjRes == nil {
			continue
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			doc.Logger.Debug("compliance: fonts: cannot decode Form XObject", "page", pageNum, "error", err)
			continue
		}
		subOps, err := content.ParseStreamData(ctx, data)
		if err != nil {
			doc.Logger.Debug("compliance: fonts: cannot parse Form XObject", "page", pageNum, "error", err)
			continue
		}
		results = append(results, checkResourceFonts(ctx, doc, xobjRes, subOps, streamSite(ctx, doc, xobjRef, xobjRes), pageNum, profile, judged)...)
		results = append(results, checkFormPUA(ctx, doc, xobjRes, subOps, data, stream.Dict, treeRoot, pageNum, profile)...)
		results = append(results, checkFormXObjectFonts(ctx, doc, subOps, xobjRes, treeRoot, pageNum, profile, judged, visited, depth+1)...)
		results = append(results, checkPatternFonts(ctx, doc, subOps, xobjRes, treeRoot, pageNum, profile, judged, visited, depth+1)...)
	}
	return results
}

// checkPatternFonts checks fonts used for rendering inside the tiling patterns a
// content stream selects as a colour. A coloured tiling pattern (ISO 32000-1,
// 8.7.3.3, PatternType 1 with PaintType 1) paints its cell with a content stream
// of its own, resolved against its own /Resources, and veraPDF descends into it
// from the scn or SCN operator that selects it, applying the same font rules to
// the glyphs the cell shows.
//
// Only a pattern that scn or SCN names comes into scope: one merely declared in
// /Resources/Pattern paints nothing. An uncoloured pattern (PaintType 2) is left
// out, as veraPDF leaves it, and a cell that shows no text judges no font. A
// pattern selected inside a Form XObject is reached through the form walk.
// Measured against veraPDF on a fixture of each shape. Recursion shares the Form
// XObject walk's visited set and depth bound.
//
// # Corpus evidence
//
//   - pdfjs issue15716.pdf: fills a rectangle with a pattern whose cell shows a
//     glyph from an unembedded ZapfDingbats, which veraPDF reports under
//     ISO 19005-2, 6.2.11.4.1 and litho never reached.
func checkPatternFonts(ctx context.Context, doc *document.Document, ops []content.Operation, res, treeRoot ast.PdfDict, pageNum int, profile string, judged *fontJudgements, visited map[int]bool, depth int) []Violation {
	if formTooDeep(doc, depth) || ctx.Err() != nil {
		return nil
	}
	patternDict, _ := document.ResolveDict(ctx, doc, res["Pattern"])
	if len(patternDict) == 0 {
		return nil
	}

	var results []Violation
	for _, op := range ops {
		if (op.Operator != "scn" && op.Operator != "SCN") || len(op.Operands) == 0 {
			continue
		}
		name, ok := op.Operands[len(op.Operands)-1].(ast.PdfName)
		if !ok {
			continue
		}
		patternRef := patternDict[name]
		ref, isRef := patternRef.(ast.PdfIndirectRef)
		if isRef && visited[ref.ObjectNum] {
			continue
		}
		// A shading pattern is a dictionary, not a stream, and shows no glyph.
		stream, err := document.ResolveStream(ctx, doc, patternRef)
		if err != nil || stream.Dict == nil {
			continue
		}
		if patternType, _ := document.ResolveInt(ctx, doc, stream.Dict["PatternType"]); patternType != 1 {
			continue
		}
		if paintType, _ := document.ResolveInt(ctx, doc, stream.Dict["PaintType"]); paintType == 2 {
			continue
		}
		if isRef {
			visited[ref.ObjectNum] = true
		}
		patternRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if patternRes == nil {
			continue
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			doc.Logger.Debug("compliance: fonts: cannot decode tiling pattern", "page", pageNum, "error", err)
			continue
		}
		subOps, err := content.ParseStreamData(ctx, data)
		if err != nil {
			doc.Logger.Debug("compliance: fonts: cannot parse tiling pattern", "page", pageNum, "error", err)
			continue
		}
		results = append(results, checkResourceFonts(ctx, doc, patternRes, subOps, streamSite(ctx, doc, patternRef, patternRes), pageNum, profile, judged)...)
		results = append(results, checkFormXObjectFonts(ctx, doc, subOps, patternRes, treeRoot, pageNum, profile, judged, visited, depth+1)...)
		results = append(results, checkPatternFonts(ctx, doc, subOps, patternRes, treeRoot, pageNum, profile, judged, visited, depth+1)...)
	}
	return results
}

// checkType3CharProcFonts checks fonts used for rendering inside the glyph
// procedures of a Type3 font that a content stream selects. A Type3 glyph
// procedure is a content stream in its own right (ISO 32000-1 §9.6.5) and may
// select fonts from the Type3 font's own /Resources, including another Type3
// font. veraPDF descends through those procedures and applies the same font
// rules to what they select, so a non-embedded font reached only that way
// (ISO 19005-2 §6.2.11.4.1) is otherwise never checked.
//
// Only the procedures for character codes the stream actually shows are
// walked, so a glyph that is never painted brings no font into scope.
// Recursion is bounded by the form budget ([formTooDeep]); visited holds the
// object numbers already descended into, which breaks a resource cycle through
// nested Type3 fonts. Object numbers are unique document-wide, so sharing one
// visited set with the Form XObject walk cannot confuse a font for a form.
//
// # Corpus evidence
//
//   - pdfjs ContentStreamCycleType3insideType3.pdf: reaches an unembedded
//     Helvetica through two nested Type3 fonts, and its name warns of the
//     resource cycle the visited set breaks.
func checkType3CharProcFonts(ctx context.Context, doc *document.Document, ops []content.Operation, res, treeRoot ast.PdfDict, pageNum int, profile string, judged *fontJudgements, visited map[int]bool, depth int) []Violation {
	if formTooDeep(doc, depth) || ctx.Err() != nil {
		return nil
	}
	fontDict, _ := document.ResolveDict(ctx, doc, res["Font"])
	if len(fontDict) == 0 {
		return nil
	}
	usage := collectFontUsage(ops)

	var results []Violation
	for _, fontName := range sortedNames(fontDict) {
		fontUsage := usage[string(fontName)]
		// A Type3 font shown only in rendering mode 3 paints nothing, so its
		// glyph procedures select nothing for rendering.
		if fontUsage == nil || !fontUsage.visible || len(fontUsage.charCodes) == 0 {
			continue
		}
		fontRef := fontDict[fontName]
		if ref, ok := fontRef.(ast.PdfIndirectRef); ok {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}
		font, err := document.ResolveDict(ctx, doc, fontRef)
		if err != nil || font == nil {
			continue
		}
		if subtype, _ := document.ResolveName(ctx, doc, font["Subtype"]); subtype != "Type3" {
			continue
		}
		// Per ISO 32000-1, Table 112, /Resources is optional: a procedure may
		// instead rely on the resources in effect where the font is used. litho
		// has no such font to attribute a violation to, so leave it unchecked
		// rather than risk a false positive.
		t3Res, _ := document.ResolveDict(ctx, doc, font["Resources"])
		if t3Res == nil {
			continue
		}
		charProcs, _ := document.ResolveDict(ctx, doc, font["CharProcs"])
		if len(charProcs) == 0 {
			continue
		}
		encoding := resolveSimpleFontEncoding(ctx, doc, font)
		for _, glyphName := range shownGlyphNames(encoding, fontUsage.charCodes) {
			stream, err := document.ResolveStream(ctx, doc, charProcs[ast.PdfName(glyphName)])
			if err != nil {
				continue
			}
			data, err := doc.DecodeStream(ctx, stream)
			if err != nil {
				doc.Logger.Debug("compliance: fonts: cannot decode Type3 CharProc",
					"page", pageNum, "glyph", glyphName, "error", err)
				continue
			}
			procOps, err := content.ParseStreamData(ctx, data)
			if err != nil {
				doc.Logger.Debug("compliance: fonts: cannot parse Type3 CharProc",
					"page", pageNum, "glyph", glyphName, "error", err)
				continue
			}
			results = append(results, checkResourceFonts(ctx, doc, t3Res, procOps, streamSite(ctx, doc, charProcs[ast.PdfName(glyphName)], t3Res), pageNum, profile, judged)...)
			results = append(results, checkType3CharProcFonts(ctx, doc, procOps, t3Res, treeRoot, pageNum, profile, judged, visited, depth+1)...)
			results = append(results, checkFormXObjectFonts(ctx, doc, procOps, t3Res, treeRoot, pageNum, profile, judged, visited, depth+1)...)
		}
	}
	return results
}

// shownGlyphNames returns, sorted, the glyph names a Type3 font's encoding
// gives the character codes a content stream shows. A code the encoding leaves
// unmapped names no procedure and is skipped.
func shownGlyphNames(encoding map[byte]string, charCodes map[byte]bool) []string {
	names := make([]string, 0, len(charCodes))
	for code := range charCodes {
		if name := encoding[code]; name != "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// checkResourceFonts runs the full single-font check suite over the fonts a
// content stream selects for rendering, resolved from res /Resources/Font. Only
// fonts actually used (selected with Tf and shown) are checked; a font declared
// in the resources but never selected renders no glyph and is out of scope.
// site identifies the stream the operations come from.
func checkResourceFonts(ctx context.Context, doc *document.Document, res ast.PdfDict, ops []content.Operation, site showSite, pageNum int, profile string, judged *fontJudgements) []Violation {
	fontDict, _ := document.ResolveDict(ctx, doc, res["Font"])
	if len(fontDict) == 0 {
		return nil
	}
	usage := collectFontUsageAt(ops, site)
	if usage == nil {
		return nil
	}

	var results []Violation
	for _, fontName := range sortedNames(fontDict) {
		fontUsage := usage[string(fontName)]
		if fontUsage == nil {
			continue // declared in the form resources but never selected
		}
		fontRef := fontDict[fontName]
		results = append(results, judged.streamFont(ctx, doc, fontRef, fontName, site, pageNum, profile, fontUsage)...)
	}
	return results
}

// streamFont judges one font a stream other than a page's own content shows
// through: the whole single-font suite the first time the font object is met,
// and after that the glyph rules alone, for the glyphs they name. The glyphs
// this stream shows are its own, and a glyph an earlier stream showed is
// stated once, by [fontJudgements.glyphs]; checking the dictionary again would
// report it once per stream. A glyph-rule violation that names no glyph speaks
// for the font as a whole, which the stream that first met it already stated,
// so it is not repeated here.
func (j *fontJudgements) streamFont(ctx context.Context, doc *document.Document, fontRef ast.PdfObject, fontName ast.PdfName, site showSite, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	dictRulesDone := false
	if ref, ok := fontRef.(ast.PdfIndirectRef); ok {
		dictRulesDone = j.dict[ref.ObjectNum]
		j.dict[ref.ObjectNum] = true
	}
	font, err := document.ResolveDict(ctx, doc, fontRef)
	if err != nil || font == nil {
		return nil
	}
	var fontResults []Violation
	if dictRulesDone {
		for _, v := range checkGlyphUsageRules(ctx, doc, font, string(fontName), pageNum, profile, usage) {
			if v.glyph.named {
				fontResults = append(fontResults, v)
			}
		}
	} else {
		fontResults = checkSingleFont(ctx, doc, font, string(fontName), pageNum, profile, usage.visible, usage)
	}
	results := j.glyphs(ctx, doc, fontIdentity(fontRef, fontName, site), font, usage, profile, fontResults)
	return append(results, j.subsetCoverage(ctx, doc, fontRef, font, string(fontName), pageNum, profile, usage)...)
}

// checkGlyphUsageRules runs the rules that judge the glyphs a stream shows,
// for a font whose dictionary was judged elsewhere: [checkFontUsageRules] for
// a simple font and [checkType0UsageRules] for a composite one.
func checkGlyphUsageRules(ctx context.Context, doc *document.Document, font ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	if fontSubtype(ctx, doc, font) != "Type0" {
		return checkFontUsageRules(ctx, doc, font, fontName, pageNum, profile, usage.visible, usage)
	}
	descArr, err := document.ResolveArray(ctx, doc, font["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return nil
	}
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		return nil
	}
	return checkType0UsageRules(ctx, doc, font, cidFontDict, fontName, pageNum, profile, usage, withoutMalformedCodes)
}

// streamSite returns the [showSite] of the content stream ref names, whose
// resources are res.
func streamSite(ctx context.Context, doc *document.Document, ref ast.PdfObject, res ast.PdfDict) showSite {
	site := showSite{key: "stream"}
	if r, ok := ref.(ast.PdfIndirectRef); ok {
		site.key = "obj " + strconv.Itoa(r.ObjectNum)
	}
	site.props, _ = document.ResolveDict(ctx, doc, res["Properties"])
	return site
}

// maxAppearanceStateFonts bounds how many sub-appearances a single /AP entry's
// state subdictionary is walked for fonts. This is an anti-DoS guard, not
// spec-mandated: an appearance state dictionary names one stream per widget
// state, which is a handful in any real document, so 256 is far above any
// legitimate count while still capping a crafted dictionary with thousands of
// states.
const maxAppearanceStateFonts = 256

// checkAnnotationAppearanceFonts checks fonts used for rendering inside a page's
// annotation appearance streams. veraPDF reaches these fonts through the
// appearance content stream and applies the same §6.2.11 font rules it applies
// to page fonts (for example a non-embedded font or a .notdef reference in a
// widget appearance); litho's page pass only walks /Resources/Font on the page,
// so a font referenced solely from an annotation appearance is otherwise never
// reached. The full single-font suite runs here, scoped to the glyphs the
// appearance stream actually renders. judged deduplicates against fonts
// already checked on the page and across appearance streams.
func checkAnnotationAppearanceFonts(ctx context.Context, doc *document.Document, page, treeRoot ast.PdfDict, pageNum int, profile string, judged *fontJudgements) []Violation {
	annots, err := document.ResolveArray(ctx, doc, page["Annots"])
	if err != nil || len(annots) == 0 {
		return nil
	}

	var results []Violation
	visited := make(map[int]bool) // appearance-stream object numbers already parsed
	for _, annotObj := range annots {
		if ctx.Err() != nil {
			return results
		}
		annot, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil || annot == nil {
			continue
		}
		apDict, err := document.ResolveDict(ctx, doc, annot["AP"])
		if err != nil || apDict == nil {
			continue
		}
		// /N (normal), /R (rollover), /D (down) appearances, matching the
		// surface litho already walks for annotation colour space usage.
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			apObj, ok := apDict[key]
			if !ok {
				continue
			}
			results = append(results, checkAppearanceStreamFonts(ctx, doc, apObj, treeRoot, pageNum, profile, judged, visited, 0)...)
		}
	}
	return results
}

// checkAppearanceStreamFonts resolves one appearance entry (a stream, or a state
// subdictionary of streams) and checks the subset glyph coverage of every font
// the stream actually uses for rendering. visited guards against parsing the
// same appearance stream twice.
//
// # Corpus evidence
//
//   - pdfjs bug854315.pdf: draws a subset CIDFont at
//     annots[0]/appearance[0]/contentStream[0]/operators[N]/xObject[0]/contentStream[0],
//     reached only through a Form XObject the appearance invokes.
//   - pdfjs issue17069.pdf: shows both routes at once, one font named by the
//     appearance itself and one only by the form it invokes.
func checkAppearanceStreamFonts(ctx context.Context, doc *document.Document, apObj ast.PdfObject, treeRoot ast.PdfDict, pageNum int, profile string, judged *fontJudgements, visited map[int]bool, stateCount int) []Violation {
	stream, err := document.ResolveStream(ctx, doc, apObj)
	if err != nil {
		// A state subdictionary: each value is an appearance stream.
		subDict, err := document.ResolveDict(ctx, doc, apObj)
		if err != nil || subDict == nil {
			return nil
		}
		var results []Violation
		for _, stateName := range sortedNames(subDict) {
			if stateCount >= maxAppearanceStateFonts {
				break
			}
			stateCount++
			results = append(results, checkAppearanceStreamFonts(ctx, doc, subDict[stateName], treeRoot, pageNum, profile, judged, visited, stateCount)...)
		}
		return results
	}

	if ref, ok := apObj.(ast.PdfIndirectRef); ok {
		if visited[ref.ObjectNum] {
			return nil
		}
		visited[ref.ObjectNum] = true
	}

	res, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if res == nil {
		return nil
	}
	// An appearance whose own /Resources name no font may still draw text: a Do
	// inside it invokes a Form XObject that carries the font. Decoding the stream
	// before that test is what reaches those, and the walk below judges them.
	fontDict, _ := document.ResolveDict(ctx, doc, res["Font"])

	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: cannot decode appearance stream", "page", pageNum, "error", err)
		return nil
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: cannot parse appearance stream", "page", pageNum, "error", err)
		return nil
	}
	site := streamSite(ctx, doc, apObj, res)
	usage := collectFontUsageAt(ops, site)
	if usage == nil {
		return nil
	}

	// A Form XObject the appearance invokes is another content stream the walk
	// above never enters, and veraPDF reaches its fonts through the appearance.
	// The colour scan already descends the same way (see
	// scanAnnotationAppearanceCS).
	// checkFormXObjectFonts carries the Do gating, the depth cap and the
	// visited set, so a shared form is walked once.
	results := checkFormXObjectFonts(ctx, doc, ops, res, treeRoot, pageNum, profile, judged, visited, 0)

	for _, fontName := range sortedNames(fontDict) {
		fontUsage := usage[string(fontName)]
		if fontUsage == nil {
			continue // declared in the appearance resources but never selected
		}
		results = append(results, judged.streamFont(ctx, doc, fontDict[fontName], fontName, site, pageNum, profile, fontUsage)...)
	}
	return results
}

// checkSingleFont runs all font checks against a single font dictionary but
// the subset rule, which its callers state once per font object through
// [fontJudgements.subsetCoverage]. If isVisible is false, width consistency
// and embedding checks are skipped (the font is not used for visible
// rendering). Usage provides per-charCode usage information for ToUnicode
// exemption checks.
func checkSingleFont(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isVisible bool, usage *fontUsageInfo) []Violation {
	results := checkFontDictRules(ctx, doc, fontDict, fontName, pageNum, profile, isVisible, usage)
	if fontSubtype(ctx, doc, fontDict) == "Type0" {
		return append(results, checkType0Font(ctx, doc, fontDict, fontName, pageNum, profile, isVisible, usage)...)
	}
	return append(results, checkFontUsageRules(ctx, doc, fontDict, fontName, pageNum, profile, isVisible, usage)...)
}

// checkFontDictRules runs the checks that read only the font dictionary, so
// they hold for the font object wherever it is used. A page pass runs them once
// per font object: repeating them per page would report the same dictionary
// defect once for every page that selects the font.
//
// # Corpus evidence
//
//   - pdfjs issue7544.pdf: writes a /CIDFontType2 descendant carrying
//     /BaseFont, /CIDSystemInfo, /CIDToGIDMap, /FontDescriptor, /Subtype and
//     /W but no /Type, confirmed in the file's own bytes, and veraPDF reports
//     §6.2.11.2 t1 against it.
//   - All corpora: the descendant CIDFont's t3 fires on no file.
func checkFontDictRules(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isVisible bool, usage *fontUsageInfo) []Violation {
	var results []Violation

	subtype := fontSubtype(ctx, doc, fontDict)

	// Required font-dictionary keys (PDFont): §6.3.2 / §6.2.11.2 / §6.2.10.2.
	results = append(results, checkFontRequiredKeys(ctx, doc, fontDict, subtype, PDFAPart(profile))...)
	if subtype == "Type0" {
		// A Type0's descendant CIDFont is a font dictionary in its own right,
		// and ISO 32000-1, 9.7.4, Table 117 requires /Type /Font on it. veraPDF
		// models it as a PDFont of its own and reports §6.2.11.2 t1 against it
		// when the descendant carries no /Type.
		//
		// Only /Type is judged here. t2 cannot fire, since both CIDFont
		// subtypes are already valid; t3 is left out; and t7 reads
		// the descriptor that document.ResolveFontDescriptor already reaches
		// THROUGH the descendant, so running the whole check again would judge
		// one descriptor twice.
		if descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"]); err == nil && len(descArr) > 0 {
			if cid, err := document.ResolveDict(ctx, doc, descArr[0]); err == nil && cid != nil {
				// Absent, null and not-a-name are three defects, and only the
				// last of the cases below is "the file names the wrong type".
				// See checkFontRequiredKeys for why %q on the empty name
				// printed a bare slash.
				typeObj, typePresent := cid["Type"]
				typeName, typeErr := document.ResolveName(ctx, doc, typeObj)
				rule := fontReqKeyRule(PDFAPart(profile), 1)
				switch {
				case typeErr != nil:
					results = append(results, failf(rule, "descendant CIDFont /Type is not a name")...)
				case !typePresent || typeName == "":
					results = append(results, failf(rule, "descendant CIDFont missing /Type")...)
				case typeName != "Font":
					results = append(results, failf(rule,
						"descendant CIDFont /Type is %s, not Font", typeName)...)
				}
			}
		}
		return results
	}

	// Required keys for simple fonts: /FirstChar, /LastChar, /Widths. These are
	// scoped to veraPDF's PDSimpleFont, which its font factory builds only for a
	// known simple subtype. A dictionary whose /Subtype is absent or is not a
	// font type at all is not a simple font, and reporting a missing /FirstChar
	// against it accused it of three further defects on top of the invalid
	// subtype checkFontRequiredKeys already reports.
	if simpleFontSubtypes[subtype] {
		results = append(results, checkSimpleFontRequiredKeys(ctx, doc, fontDict, PDFAPart(profile))...)
	}

	// Font embedding: only for fonts used for visible rendering. Type 3 fonts
	// have no font program, so they are exempt.
	if isVisible {
		results = append(results, checkFontEmbedding(ctx, doc, fontDict, fontName, pageNum, profile, isType3Font(ctx, doc, fontDict))...)
	}

	// TrueType encoding.
	if subtype == "TrueType" {
		results = append(results, checkTrueTypeEncoding(ctx, doc, fontDict, fontName, pageNum, profile)...)
	}

	// /CharSet completeness compares the declared glyph list against the whole
	// font program, which is likewise a property of the object.
	results = append(results, checkCharSetDeclaredCoverage(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// Glyph widths compare the /Widths array against the font program, which is
	// a property of the font object rather than of one page's codes, so this
	// belongs with the dictionary rules. Judging it per page reported width
	// defects veraPDF does not raise.
	if isVisible {
		results = append(results, checkGlyphWidths(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
	}

	return results
}

// checkFontUsageRules runs the checks whose verdict depends on which character
// codes a content stream actually shows. veraPDF builds a glyph per
// text-showing operator, so these hold per page rather than per font object: a
// font object shared by several pages must be judged against each page's own
// usage, or a glyph defect that first appears on a later page is never seen.
// A glyph two pages show is still stated once, by [fontJudgements.glyphs].
// Simple fonts only; [checkType0Font] covers the composite path. The /CharSet
// rule is stated once per font object, by [fontJudgements.subsetCoverage].
func checkFontUsageRules(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isVisible bool, usage *fontUsageInfo) []Violation {
	var results []Violation

	// ToUnicode mapping.
	results = append(results, checkToUnicode(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// Glyph presence (§6.3.8 / §6.2.11.4.1 t2 / §6.2.10.4.1 t2): every glyph
	// referenced for visible rendering must exist in the program.
	if isVisible {
		results = append(results, checkSimpleGlyphPresence(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
	}

	// .notdef glyph.
	results = append(results, checkNotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// ToUnicode CMap values.
	results = append(results, checkToUnicodeCMapValues(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	return results
}

// checkPUAWithoutActualText flags a font whose ToUnicode maps a character the
// page content shows to the Unicode Private Use Area when no /ActualText covers
// that character (ISO 19005-2/3 §6.2.11.7.3, ISO 14289-2 §8.4.3, mirroring
// veraPDF's per-glyph actualTextPresent). See [checkStreamPUAWithoutActualText].
func checkPUAWithoutActualText(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, page ast.PdfDict, catalog ast.PdfDict) []Violation {
	if puaActualTextRule(profile).Specification == "" {
		return nil
	}
	treeRoot, _ := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	contentData, _ := resolvePageContentBytes(ctx, doc, page)
	return checkStreamPUAWithoutActualText(ctx, doc, fontDict, fontName, pageNum, profile, treeRoot,
		contentData, page, resolveResources(ctx, doc, page))
}

// checkFormPUA runs the PUA check over the fonts a Form XObject's own content
// selects. It does not consult judged: a font already judged where the
// page shows it can still show an uncovered PUA glyph inside the form.
func checkFormPUA(ctx context.Context, doc *document.Document, res ast.PdfDict, ops []content.Operation, data []byte, form, treeRoot ast.PdfDict, pageNum int, profile string) []Violation {
	if puaActualTextRule(profile).Specification == "" {
		return nil
	}
	fontDict, _ := document.ResolveDict(ctx, doc, res["Font"])
	usage := collectFontUsage(ops)
	var results []Violation
	for _, fontName := range sortedNames(fontDict) {
		if usage[string(fontName)] == nil {
			continue
		}
		font, err := document.ResolveDict(ctx, doc, fontDict[fontName])
		if err != nil || font == nil {
			continue
		}
		results = append(results, checkStreamPUAWithoutActualText(ctx, doc, font, string(fontName), pageNum, profile,
			treeRoot, data, form, res)...)
	}
	return results
}

// checkStreamPUAWithoutActualText is the PUA check for the glyphs one content
// stream shows: a page's, or a Form XObject's, since veraPDF builds a Glyph in
// each and a form's glyphs are covered only by marked content inside the form.
// holder is the page or form dictionary whose /StructParents keys the stream's
// MCIDs in the structure parent tree; res supplies its /Properties.
//
// It walks the stream and fires only for a shown PUA code whose enclosing
// marked-content sequence, or whose MCID's structure element, carries no
// /ActualText, so an /ActualText covering a different span does not mask an
// uncovered PUA character. A composite font is walked the same way under
// Identity-H or Identity-V, where each two-byte code is the CID the /ToUnicode
// CMap is keyed by; any other encoding needs a CMap this check does not carry,
// so it stays conservative and any /ActualText exempts the font.
func checkStreamPUAWithoutActualText(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, treeRoot ast.PdfDict, contentData []byte, holder, res ast.PdfDict) []Violation {
	rule := puaActualTextRule(profile)
	if rule.Specification == "" {
		return nil
	}

	// A glyph's Unicode value comes from the /ToUnicode CMap when the CMap maps
	// its code, and otherwise, for a simple font, from its encoding's glyph name
	// (ISO 32000-1, 9.10.2; veraPDF PDSimpleFont.toUnicode), so a PUA value can
	// come from either.
	var tuData []byte
	if tuObj, ok := fontDict["ToUnicode"]; ok {
		stream, err := document.ResolveStream(ctx, doc, tuObj)
		if err != nil {
			doc.Logger.Debug("compliance: cannot resolve ToUnicode stream", "error", err)
		} else if data, derr := doc.DecodeStream(ctx, stream); derr != nil {
			doc.Logger.Debug("compliance: cannot decode ToUnicode stream", "error", derr)
		} else {
			tuData = data
		}
	}
	tuHasPUA := tuData != nil && containsPUAMapping(string(tuData))
	puaCodes := make(map[uint32]bool)
	if tuHasPUA {
		puaCodes = puaSourceCodes(string(tuData))
	}
	if fontSubtype(ctx, doc, fontDict) != "Type0" {
		for code := range glyphNamePUACodes(ctx, doc, fontDict, tuData) {
			puaCodes[code] = true
		}
	}
	if !tuHasPUA && len(puaCodes) == 0 {
		return nil
	}

	hasContentActualText := len(contentData) > 0 && bytes.Contains(contentData, []byte("/ActualText"))

	// Over-exemption is an under-report, never a false positive.
	conservative := func() []Violation {
		if (treeRoot != nil && structTreeHasActualText(ctx, doc, treeRoot, 0)) || hasContentActualText {
			return nil
		}
		return failf(rule, "font %s on page %d: ToUnicode maps to PUA without ActualText", fontName, pageNum)
	}
	splitCodes := singleByteCodes
	if fontSubtype(ctx, doc, fontDict) == "Type0" {
		if splitCodes = type0CodeSplitter(ctx, doc, fontDict); splitCodes == nil {
			return conservative()
		}
	}
	if len(puaCodes) == 0 {
		return conservative()
	}
	ops, perr := content.ParseStreamData(ctx, contentData)
	if perr != nil {
		return conservative()
	}

	// A PUA character is covered when an enclosing marked-content sequence carries
	// /ActualText, or when the structure element of its /MCID does.
	// mcidCovered maps each MCID of the stream to the latter.
	//
	// ISO 14289-2, 8.4.3 accepts an /Alt wherever it accepts an /ActualText
	// (veraPDF altPresent reads the same places); ISO 19005-2/3, 6.2.11.7.3 asks
	// for /ActualText alone.
	altCovers := IsPDFUA(profile)
	mcidCovered := mcidActualTextCoverage(ctx, doc, treeRoot, holder, altCovers)

	var props ast.PdfDict // the stream's /Properties, resolved lazily for named BDC lists
	propsResolved := false
	resolveProps := func() ast.PdfDict {
		if !propsResolved {
			if res != nil {
				props, _ = document.ResolveDict(ctx, doc, res["Properties"])
			}
			propsResolved = true
		}
		return props
	}

	var actualTextStack []bool // per open BDC/BMC: does it carry /ActualText (or, under PDF/UA-2, /Alt)?
	var mcidStack []int        // per open BDC/BMC: the effective /MCID (-1 if none)
	var fontStack []string     // per open q: the text font Q restores
	currentFont := ""
	currentMCID := -1
	for _, op := range ops {
		switch op.Operator {
		case "q":
			// The text font is part of the graphics state, so Q restores it.
			// See ISO 32000-1, 8.4.2 and Table 104.
			fontStack = append(fontStack, currentFont)
		case "Q":
			if n := len(fontStack); n > 0 {
				currentFont = fontStack[n-1]
				fontStack = fontStack[:n-1]
			}
		case "BDC", "BMC":
			actualTextStack = append(actualTextStack, bdcHasStringKey(ctx, doc, op, resolveProps, "ActualText") ||
				(altCovers && bdcHasStringKey(ctx, doc, op, resolveProps, "Alt")))
			mcid := bdcMCID(ctx, doc, op, resolveProps)
			if mcid < 0 {
				mcid = currentMCID // inherit the enclosing MCID
			}
			mcidStack = append(mcidStack, mcid)
			currentMCID = mcid
		case "EMC":
			if n := len(actualTextStack); n > 0 {
				actualTextStack = actualTextStack[:n-1]
			}
			if n := len(mcidStack); n > 0 {
				mcidStack = mcidStack[:n-1]
			}
			currentMCID = -1
			if n := len(mcidStack); n > 0 {
				currentMCID = mcidStack[n-1]
			}
		case "Tf":
			if len(op.Operands) >= 1 {
				if n, ok := op.Operands[0].(ast.PdfName); ok {
					currentFont = string(n)
				}
			}
		case "Tj", "TJ", "'", "\"":
			if currentFont != fontName || sliceAnyTrue(actualTextStack) || mcidCovered[currentMCID] {
				continue
			}
			if showPaintsPUACode(op, puaCodes, splitCodes) {
				return failf(rule, "font %s on page %d: rendered character mapped to PUA has no ActualText", fontName, pageNum)
			}
		}
	}
	return nil
}

// mcidActualTextCoverage maps each marked-content identifier on a page to whether
// the structure element it belongs to (resolved through the page /StructParents
// entry in the ParentTree) carries an /ActualText text string, and with withAlt
// an /Alt one too. veraPDF's actualTextPresent and altPresent read the element
// the MCID names and not its ancestors, and accept only a string value
// (MarkedContentHelper.containsStringKey), so a parent's /ActualText does not
// cover a child element that has none.
func mcidActualTextCoverage(ctx context.Context, doc *document.Document, treeRoot, page ast.PdfDict, withAlt bool) map[int]bool {
	out := make(map[int]bool)
	if treeRoot == nil {
		return out
	}
	spObj, ok := page["StructParents"]
	if !ok {
		return out
	}
	key, err := document.ResolveInt(ctx, doc, spObj)
	if err != nil {
		return out
	}
	ptDict, err := document.ResolveDict(ctx, doc, treeRoot["ParentTree"])
	if err != nil || ptDict == nil {
		return out
	}
	arr, err := document.ResolveArray(ctx, doc, numberTreeLookupObj(ctx, doc, ptDict, int(key), 0, make(map[int]bool)))
	if err != nil {
		return out
	}
	for mcid, elemRef := range arr {
		elem, derr := document.ResolveDict(ctx, doc, elemRef)
		if derr != nil || elem == nil {
			continue
		}
		if dictHasString(ctx, doc, elem, "ActualText") || (withAlt && dictHasString(ctx, doc, elem, "Alt")) {
			out[mcid] = true
		}
	}
	return out
}

// bdcProps returns a BDC operator's property list, written inline or named
// through the stream's /Properties, or nil for a BMC or a list that does not
// resolve.
func bdcProps(ctx context.Context, doc *document.Document, op content.Operation, props func() ast.PdfDict) ast.PdfDict {
	if op.Operator != "BDC" || len(op.Operands) < 2 {
		return nil
	}
	switch v := op.Operands[1].(type) {
	case ast.PdfDict:
		return v
	case ast.PdfName:
		if p := props(); p != nil {
			pd, _ := document.ResolveDict(ctx, doc, p[v])
			return pd
		}
	}
	return nil
}

// bdcMCID returns the /MCID value of a BDC operator's property list (inline or
// named via the page /Properties), or -1 when it carries none. Only an integer
// /MCID counts ([mcidValue]): document.ResolveInt read an absent or null one as
// 0, so text under no identifier borrowed the /ActualText of the element that
// owns MCID 0, where veraPDF reports ISO 14289-2, 8.4.3 t1. Measured.
func bdcMCID(ctx context.Context, doc *document.Document, op content.Operation, props func() ast.PdfDict) int {
	pd := bdcProps(ctx, doc, op, props)
	if pd == nil {
		return -1
	}
	mcid, ok := mcidValue(ctx, doc, pd["MCID"])
	if !ok {
		return -1
	}
	return mcid
}

// bdcHasStringKey reports whether a BDC operator's property list carries key
// with a text-string value, the shape veraPDF's getInheritedStringAttribute
// accepts.
func bdcHasStringKey(ctx context.Context, doc *document.Document, op content.Operation, props func() ast.PdfDict, key ast.PdfName) bool {
	return dictHasString(ctx, doc, bdcProps(ctx, doc, op, props), key)
}

// dictHasString reports whether d carries key with a string value.
func dictHasString(ctx context.Context, doc *document.Document, d ast.PdfDict, key ast.PdfName) bool {
	obj, ok := d[key]
	if !ok {
		return false
	}
	_, err := document.ResolveString(ctx, doc, obj)
	return err == nil
}

// showPaintsPUACode reports whether a text-showing operator paints any code
// present in puaCodes, splitting its strings into character codes with
// splitCodes.
func showPaintsPUACode(op content.Operation, puaCodes map[uint32]bool, splitCodes func([]byte) []uint32) bool {
	check := func(s ast.PdfString) bool {
		for _, code := range splitCodes([]byte(s)) {
			if puaCodes[code] {
				return true
			}
		}
		return false
	}
	for _, operand := range op.Operands {
		switch v := operand.(type) {
		case ast.PdfString:
			if check(v) {
				return true
			}
		case ast.PdfArray:
			for _, item := range v {
				if s, ok := item.(ast.PdfString); ok && check(s) {
					return true
				}
			}
		}
	}
	return false
}

// glyphNamePUACodes returns the codes a simple font's encoding names with a
// glyph the Adobe Glyph List puts in a Private Use Area ([aglPUAGlyphNames]),
// leaving out any code its /ToUnicode CMap maps, whose value takes precedence
// (veraPDF PDSimpleFont.toUnicode). A /ToUnicode that cannot be parsed leaves no
// code to judge by name, so the font keeps the CMap route alone rather than
// risk reporting a glyph the CMap in fact covers.
func glyphNamePUACodes(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, tuData []byte) map[uint32]bool {
	var mapped map[int]bool
	if tuData != nil {
		parsed, err := cmap.Parse(ctx, tuData)
		if err != nil || parsed == nil {
			return nil
		}
		mapped = cmapSourceCodeValues(parsed)
	}
	// A subset CFF declaring a predefined encoding names only the codes whose
	// glyph it embeds, so cffPredefinedEncodingFilled restores the rest: the
	// encoding assigns them whether or not the subset carries the glyph.
	names := cffPredefinedEncodingFilled(ctx, doc, fontDict,
		resolveSimpleFontEncoding(ctx, doc, fontDict))

	out := make(map[uint32]bool)
	for code, name := range names {
		if aglPUAGlyphNames[name] && !mapped[int(code)] {
			out[uint32(code)] = true
		}
	}
	return out
}

// singleByteCodes reads a simple font's string one byte per character code.
func singleByteCodes(data []byte) []uint32 {
	codes := make([]uint32, len(data))
	for i, b := range data {
		codes[i] = uint32(b)
	}
	return codes
}

// type0CodeSplitter returns how a composite font's /Encoding splits a shown
// string into character codes (ISO 32000-1, 9.7.6.2): two bytes per code under
// Identity-H or Identity-V, and the codespace ranges of an embedded or
// predefined CMap otherwise. It returns nil when the encoding cannot be read.
func type0CodeSplitter(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) func([]byte) []uint32 {
	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	switch encName {
	case "Identity-H", "Identity-V":
		return func(data []byte) []uint32 {
			codes := make([]uint32, 0, len(data)/2)
			for i := 0; i+1 < len(data); i += 2 {
				codes = append(codes, uint32(data[i])<<8|uint32(data[i+1]))
			}
			return codes
		}
	case "":
		stream, err := document.ResolveStream(ctx, doc, fontDict["Encoding"])
		if err != nil {
			doc.Logger.Debug("compliance: fonts: cannot resolve Type0 encoding CMap", "error", err)
			return nil
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			doc.Logger.Debug("compliance: fonts: cannot decode Type0 encoding CMap", "error", err)
			return nil
		}
		if m := cmap.ParseEmbeddedCIDMap(ctx, data); m != nil {
			return m.Codes
		}
		return nil
	default:
		if m := cmap.LoadPredefinedCIDMap(string(encName)); m != nil {
			return m.Codes
		}
		return nil
	}
}

func sliceAnyTrue(b []bool) bool {
	for _, v := range b {
		if v {
			return true
		}
	}
	return false
}

// structTreeHasActualText recursively checks if any structure element has /ActualText.
func structTreeHasActualText(ctx context.Context, doc *document.Document, node ast.PdfDict, depth int) bool {
	return structTreeHasActualTextAt(ctx, doc, node, make(map[int]bool), depth)
}

// structTreeHasActualTextAt is [structTreeHasActualText] carrying the elements
// already examined.
//
// visited bounds a crafted /K graph. This walk fans out over every kid, so
// [maxStructAttrSearchDepth] caps the work at branch^depth rather than at
// depth: an element whose /K names the same child twice costs 2^30 visits while
// sitting entirely inside the bound. The answer cannot change, since the
// question is only whether some element carries /ActualText, so this is a limit
// rather than a repair.
//
// Reaching this search at all takes some doing, which is why no fan-out
// fixture covered it before. The call sits inside the conservative() closure
// of [checkStreamPUAWithoutActualText], and that closure runs on only three
// branches. A font whose /ToUnicode maps into a private use area is necessary
// but NOT sufficient: an Identity-H encoding splits its codes successfully and
// takes the precise per-MCID path instead, never calling the closure. An
// encoding naming a CMap litho cannot load leaves [type0CodeSplitter] nil, and
// that is what routes a document here.
func structTreeHasActualTextAt(ctx context.Context, doc *document.Document, node ast.PdfDict, visited map[int]bool, depth int) bool {
	if depth > maxStructAttrSearchDepth {
		return false
	}
	if _, ok := node["ActualText"]; ok {
		return true
	}
	kObj := node["K"]
	if kObj == nil {
		return false
	}
	if arr, _ := document.ResolveArray(ctx, doc, kObj); len(arr) > 0 {
		for _, kid := range arr {
			if ref, isRef := kid.(ast.PdfIndirectRef); isRef {
				if visited[ref.ObjectNum] {
					continue
				}
				visited[ref.ObjectNum] = true
			}
			kidDict, _ := document.ResolveDict(ctx, doc, kid)
			if kidDict != nil && structTreeHasActualTextAt(ctx, doc, kidDict, visited, depth+1) {
				return true
			}
		}
		return false
	}
	if ref, isRef := kObj.(ast.PdfIndirectRef); isRef {
		if visited[ref.ObjectNum] {
			return false
		}
		visited[ref.ObjectNum] = true
	}
	if kidDict, _ := document.ResolveDict(ctx, doc, kObj); kidDict != nil {
		return structTreeHasActualTextAt(ctx, doc, kidDict, visited, depth+1)
	}
	return false
}

// checkType0Font validates composite font properties including descendant
// CIDFonts, CIDSystemInfo, CIDToGIDMap, and ToUnicode. isVisible reports whether
// a text-showing operator paints the font with a rendering mode other than 3; it
// gates the embedding rule exactly as it does for simple fonts in
// [checkSingleFont].
func checkType0Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isVisible bool, usage *fontUsageInfo) []Violation {
	results := make([]Violation, 0, 4)

	// Resolve /DescendantFonts array.
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		doc.Logger.Debug("compliance: fonts: Type0 font has no DescendantFonts",
			"font", fontName, "error", err)
		// Without descendants no further checks are possible, but the font is
		// still subject to embedding checks at the Type0 level.
		return results
	}

	// PDF spec requires exactly one descendant CIDFont.
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		doc.Logger.Debug("compliance: fonts: failed to resolve descendant CIDFont",
			"font", fontName, "error", err)
		return results
	}

	cidSubtype := fontSubtype(ctx, doc, cidFontDict)

	// Font embedding for the CIDFont. The rendering-mode-3 exemption is not
	// scoped to simple fonts: ISO 19005-1 §6.3.4 t1, ISO 19005-2/3 §6.2.11.4.1
	// t1, ISO 19005-4 §6.2.10.4.1 t1, ISO 14289-1 §7.21.4.1 t1 and ISO 14289-2
	// §8.4.5.5.1 t1 all exempt a font "used exclusively with text rendering mode
	// 3", which is not rendered. veraPDF applies the same escape to the
	// descendant CIDFont (its PDCIDFont is a PDFont), so an unembedded composite
	// font drawn only invisibly must stay silent here too.
	if isVisible {
		results = append(results, checkFontEmbedding(ctx, doc, cidFontDict, fontName, pageNum, profile, false)...)
	}

	// CIDToGIDMap. A font with no recorded usage is treated as visible so the
	// check still runs when content-stream usage could not be collected. Every
	// checkSingleFont caller supplies usage whenever it can prove non-use, so
	// isVisible already carries that.
	results = append(results, checkCIDToGIDMap(ctx, doc, cidFontDict, cidSubtype, fontName, pageNum, profile, isVisible)...)

	// CIDSystemInfo consistency.
	results = append(results, checkCIDSystemInfo(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile)...)

	// ToUnicode for composite fonts.
	results = append(results, checkType0ToUnicode(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile, usage)...)

	// CMap embedding and WMode.
	results = append(results, checkCMapEmbedding(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// CID value limit (t10: max CID 65535).
	results = append(results, checkCMapCIDValues(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// .notdef glyph.
	results = append(results, checkNotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, nil)...)

	return append(results, checkType0UsageRules(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile, usage, withMalformedCodes)...)
}

// checkType0UsageRules applies the composite-font rules that judge the glyphs
// one page shows, as opposed to those that judge the font dictionary. A font
// object's dictionary is reported once, but every page that selects the font
// shows its own codes, so these run again for each. veraPDF builds a glyph per
// show operator, and a glyph two pages show is stated once, by
// [fontJudgements.glyphs]. Simple fonts already work this way through
// [checkFontUsageRules]. The /CIDSet rule is stated once per font object, by
// [fontJudgements.subsetCoverage].
//
// # Corpus evidence
//
//   - govdocs thread_100_100870.pdf: selects one Times-Roman Identity-H font on
//     fourteen pages, and only page 20 shows CID 0 and a CID its CFF charset
//     does not hold. veraPDF reports both under ISO 19005-2 §6.2.11.8 and
//     §6.2.11.4.1 t2; litho judged the font on page 9 and never looked again.
func checkType0UsageRules(ctx context.Context, doc *document.Document, fontDict, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo, malformed judgeMalformedCodes) []Violation {
	results := make([]Violation, 0, 4)

	// Per-glyph ToUnicode coverage: a present map must cover every code
	// rendered through the font (§6.2.11.7.2 t2 / UA 7.21.7 / 8.4.5.8).
	results = append(results, checkType0PerGlyphToUnicode(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	results = append(results, checkType0NotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, usage, malformed)...)
	results = append(results, checkType0GlyphPresence(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// Glyph width consistency for CIDFont. Widths are CID-keyed: CID-keyed
	// CFF subsets renumber GIDs densely while the /W array stays
	// CID-indexed, so a GID-keyed map reads the wrong charstrings.
	cidFd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if cidFd != nil {
		fontData := extractFontProgramData(ctx, doc, cidFd)
		if fontData != nil {
			binaryWidths := typography.ExtractCIDBinaryWidths(ctx, fontData)
			if binaryWidths != nil {
				unitsPerEm := typography.ReadUnitsPerEm(fontData)
				if unitsPerEm < minUnitsPerEm || unitsPerEm > maxUnitsPerEm {
					unitsPerEm = defaultUnitsPerEm
				}
				rule := glyphWidthRule(profile)
				if rule.Specification != "" {
					results = append(results, checkCIDFontWidths(ctx, doc, cidFontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, type0UsedCIDs(ctx, doc, fontDict, usage))...)
					results = append(results, checkType0NotdefWidth(ctx, doc, fontDict, cidFontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, usage)...)
				}
			}
		}
	}

	// ToUnicode CMap values.
	results = append(results, checkToUnicodeCMapValues(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	return results
}

// checkNotdefGlyph checks for .notdef glyph references. A character code that
// maps to .notdef (GID 0 or glyph name ".notdef") is a violation.
//
// For simple fonts (Type1/TrueType): resolves encoding and checks each used
// character code. For TrueType with embedded programs: also checks the font's
// cmap for GID 0 mappings.
//
// # Corpus evidence
//
//   - pdfjs bug1354114.pdf: shows WinAnsi code 10 through a symbolic
//     NimbusRomNo9L subset and veraPDF reports it; the same code taken from
//     StandardEncoding it does not.
//   - govdocs thread_000_000153.pdf: shows code 127 of an unembedded NewYork
//     declared /MacRomanEncoding, which veraPDF reports under ISO 14289-1,
//     7.21.8 t1, so DEL is not exempt when no program is embedded.
//   - pdfjs issue13107_reduced.pdf: embeds a DejaVuSans subset with no .notdef
//     glyph whose post table names GID 0 "space", so a code the encoding names
//     is judged by that name alone.
//   - pdfjs issue6410.pdf: embeds four nonsymbolic TrueTypes with no /Encoding
//     whose post 2.0 table sends them down the glyph-name branch. litho named
//     charCode 32 though all three cmap subtables map it to "space", while
//     veraPDF judged 149 glyphs there and found no .notdef.
func checkNotdefGlyph(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := notdefRule(profile)
	if rule.Specification == "" {
		return nil
	}

	if usage == nil || len(usage.charCodes) == 0 {
		return nil
	}

	// Check /Encoding /Differences for a used ".notdef" entry.
	if r := checkNotdefInDifferences(ctx, doc, fontDict, fontName, pageNum, rule, usage); r != nil {
		return r
	}

	subtype := fontSubtype(ctx, doc, fontDict)

	// For Type1/CFF: if the encoding explicitly maps a used charCode to
	// ".notdef", it's a violation. For charCodes below 0x20 (control range)
	// with no encoding entry, the glyph is .notdef since standard encodings
	// don't define glyphs for control characters. For charCodes >= 0x20,
	// absence of explicit encoding doesn't imply .notdef (standard encoding
	// provides defaults for printable ASCII).
	if subtype == "Type1" || subtype == "MMType1" || subtype == "TrueType" {
		enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
		if len(enc) > 0 {
			// An explicit /Differences ".notdef" fires for any used code. A C0
			// control code (< 0x20) with no encoding entry renders .notdef
			// because the ISO 32000-1 Annex D standard encodings define no glyph
			// for control characters; this holds for a nonsymbolic simple font
			// regardless of embedding, which is why it is evaluated here for
			// Type1 and TrueType alike (the TrueType glyph-program path below
			// cannot run for a non-embedded font, exactly where control-code
			// .notdef references hide). DEL (0x7F) is deliberately excluded: some
			// fonts do define a glyph there when they embed a program.
			// A symbolic font resolves codes through its built-in encoding (ISO
			// 32000-1 §9.6.6.2), which litho does not parse, so the implicit case
			// is declined rather than reported as a false positive.
			symbolic := false
			if fd := document.ResolveFontDescriptor(ctx, doc, fontDict); fd != nil {
				symbolic = isSymbolicFont(ctx, doc, fd)
			}
			// veraPDF names a glyph ".notdef" exactly when the font program does
			// not contain the code (GFGlyph: containsCode(code) ? getGlyphName
			// : ".notdef"). With no program embedded there is nothing to consult,
			// so the declared encoding decides: a code its table leaves undefined
			// selects no glyph. MacExpertEncoding is the case that exposed this,
			// defining nothing at 'A' or 'B'. An embedded program is NOT judged
			// this way, because it can still supply a code the encoding omits
			// (ISO 32000-1, 9.6.6.2); answering that needs the program's own
			// charset, which this check does not read, so it stays silent there.
			namedTable := ""
			embedded := false
			if fd := document.ResolveFontDescriptor(ctx, doc, fontDict); fd == nil ||
				extractFontProgramData(ctx, doc, fd) == nil {
				namedTable = namedEncodingTable(ctx, doc, fontDict)
			} else {
				embedded = true
			}
			// An embedded Type 1 font is judged by the name its /Encoding gives
			// the code (ISO 32000-1, 9.6.6.1): .notdef where the predefined base
			// table leaves the code undefined, whatever the symbolic flag says,
			// which is how veraPDF names it. veraPDF's Encoding holds a table for
			// MacRomanEncoding, MacExpertEncoding and WinAnsiEncoding only, so
			// under StandardEncoding, a /Differences-only dictionary or no
			// /Encoding a code the dictionary leaves alone gets no name and no
			// report, while a code WinAnsiEncoding leaves undefined is reported
			// even through a symbolic subset.
			embeddedType1 := embedded && (subtype == "Type1" || subtype == "MMType1")
			type1Table := ""
			var type1Differences map[byte]string
			if embeddedType1 {
				type1Table = type1EncodingTable(ctx, doc, fontDict)
				type1Differences = fontDifferencesNames(ctx, doc, fontDict)
			}
			for _, code := range sortedByteKeys(usage.charCodes) {
				glyphName := enc[code]
				if glyphName == ".notdef" {
					return failf(rule, "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
				}
				if embeddedType1 {
					if _, named := type1Differences[code]; !named && type1UndefinedCode(type1Table, code) {
						return failf(rule, "font %s on page %d: charCode %d is undefined in %s, so it maps to .notdef",
							fontName, pageNum, code, type1Table)
					}
					continue
				}
				// The symbolic exemption below applies here too: a
				// symbolic font resolves codes through its built-in encoding
				// (ISO 32000-1, 9.6.6.2), which this check does not read. DEL is
				// not exempt here: this branch runs only when the font embeds
				// no program, so nothing can define a glyph at 0x7F, and none
				// of the four predefined tables does. veraPDF agrees, reading
				// predefinedEncoding[code] in Encoding.getName with no carve
				// out for DEL.
				if glyphName == "" && namedTable != "" && !symbolic {
					return failf(rule, "font %s on page %d: charCode %d is undefined in %s, so it maps to .notdef",
						fontName, pageNum, code, namedTable)
				}
				if glyphName == "" && code < 0x20 && !symbolic {
					return failf(rule, "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
				}
			}
		}
	}

	// For TrueType: compose PDF encoding → glyph name → GID via post table.
	// If post table absent, use cmap directly for charCode 0 (always .notdef).
	if subtype == "TrueType" {
		fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
		if fd != nil {
			fontData := extractFontProgramData(ctx, doc, fd)
			if fontData != nil {
				// A symbolic TrueType font (no /Encoding) resolves codes through
				// its built-in (3,0)/(1,0) cmap, not glyph names (ISO 32000-1
				// §9.6.6.4), so the glyph-name path below (which would read the
				// absent encoding as every code mapping to .notdef) does not apply.
				// A subset symbolic font routinely maps its unused codes to
				// .notdef, and veraPDF does not treat that as a reference from a
				// text-showing operator, so litho declines to report it too rather
				// than emit a false positive.
				if fontDict["Encoding"] == nil && isSymbolicFont(ctx, doc, fd) {
					return nil
				}
				enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
				// A TrueType simple font resolves a glyph through its /Encoding
				// first (ISO 32000-1, 9.6.6.4): charCode 0 used but absent from an
				// explicit /Encoding renders .notdef regardless of the font's
				// cmap. litho's no-post-table path otherwise trusts the cmap and
				// misses this.
				if len(enc) > 0 && usage.charCodes[0] && enc[0] == "" {
					return failf(rule, "font %s on page %d: charCode 0 maps to .notdef", fontName, pageNum)
				}
				if glyphNames := typography.ExtractGlyphNames(ctx, fontData); glyphNames != nil {
					// veraPDF names a simple font's glyph from its /Encoding
					// (GFGlyph: the encoding's name for the code, the program only
					// when the encoding gives none) and tests name != ".notdef"
					// (ISO 19005-2, 6.2.11.8). A code the encoding names is judged
					// by that name alone, even when the program stores the glyph at
					// GID 0, as a subset with no .notdef glyph can.
					// Lowest offending code; see lowestOffender.
					// A font that declares no /Encoding names no code at all, so an
					// empty name here is the absence of a declaration rather than
					// evidence of .notdef: ISO 32000-1, 9.6.6.4 resolves such a
					// TrueType through the program's own cmap, which the no-names
					// branch below consults. Condemning the empty name regardless
					// reported every code a font shows. A declared encoding that
					// leaves a code undefined still fires, which is the
					// MacExpertEncoding case above.
					declaresEncoding := len(enc) > 0
					var worst lowestOffender[byte]
					for code := range usage.charCodes {
						glyphName := enc[code]
						if glyphName == ".notdef" || (glyphName == "" && declaresEncoding) {
							worst.offer(code)
						}
					}
					if code, any := worst.result(); any {
						return failf(rule, "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
					}
				} else {
					// No post table glyph names, so use cmap + loca + /Widths.
					charToGID := typography.ExtractCharCodeToGID(ctx, fontData)
					if charToGID != nil {
						// GID 0 is always .notdef.
						if usage.charCodes[0] && charToGID[0] == 0 {
							return failf(rule, "font %s on page %d: charCode 0 maps to GID 0 (.notdef)", fontName, pageNum)
						}
						// A code renders .notdef only when its cmap resolves to
						// GID 0. An in-range non-zero GID is a real glyph even with
						// no contours and a zero advance (a "space" or control
						// glyph), so an empty outline is not .notdef: ISO 19005-2
						// §6.2.11.8 (veraPDF object Glyph, test name != ".notdef")
						// keys on the glyph being .notdef, not on emptiness.
						for _, code := range sortedByteKeys(usage.charCodes) {
							if gid, inCmap := charToGID[uint16(code)]; inCmap && gid == 0 {
								return failf(rule, "font %s on page %d: charCode %d maps to GID 0 (.notdef)", fontName, pageNum, code)
							}
						}
					}
				}
			}
		}
	}

	return nil
}

// collectType0CIDs decodes a Type0 font's shown strings into the set of CIDs
// they reference. It returns (cids, notdef, unmappedZero, ok):
//   - ok is false when litho cannot decode the /Encoding to CIDs (an embedded
//     CMap stream it could not parse, or an unrecognized predefined CMap); the
//     caller skips the check rather than risk a false positive.
//   - notdef is true when decoding reached a character code that unambiguously
//     renders as the .notdef glyph (an Identity odd-length tail, or an embedded
//     CMap mapping a code to a CID beyond 65535).
//   - unmappedZero is true when an embedded CMap leaves a shown code unmapped,
//     which selects CID 0.
//
// Identity-H/V treats each 2-byte unit as the CID directly. An embedded CMap
// stream is decoded through embedded. A predefined CMap
// (Adobe-Japan1 etc.) is decoded codespace-aware through its bundled bcmap; the
// resolvable CIDs are returned for the caller's embedded-font charset check,
// while unmapped codes are skipped to stay free of false positives.
func collectType0CIDs(encName string, embedded *cmap.CIDMap, usage *fontUsageInfo) (cids map[uint16]bool, notdef, unmappedZero, ok bool) {
	// An embedded CMap stream resolves to an empty name; decode through the
	// parsed stream instead so the used-glyph checks cover fonts that map
	// codes to CIDs with a custom CMap. Without this, a font whose /Encoding
	// is a stream yields no used CIDs and every glyph check is silently
	// skipped.
	if encName == "" && embedded != nil {
		cids = make(map[uint16]bool)
		for _, s := range usage.rawStrings {
			for _, c := range embedded.Decode([]byte(s)) {
				if !c.Mapped {
					// A code the CMap leaves unmapped selects CID 0 when litho
					// holds all its mappings (ISO 32000-1, 9.7.6.3); see
					// cmap.CIDMap.UnmappedNotdef. unmappedZero separates that
					// from a code written as CID 0: the first reaches no glyph
					// at all, the second reaches the .notdef glyph, which the
					// program does contain.
					if embedded.UnmappedNotdef() {
						cids[0] = true
						unmappedZero = true
					}
					continue
				}
				// A CMap mapping a used code to a CID beyond 65535
				// references a glyph no font can contain (ISO 32000-2
				// limits CIDs to 0..65535): the renderer draws .notdef,
				// which is exactly what the caller's notdef path reports.
				if c.CID < 0 || c.CID > 0xFFFF {
					return nil, true, false, true
				}
				cids[uint16(c.CID)] = true
			}
		}
		return cids, false, unmappedZero, true
	}
	switch encName {
	case "Identity-H", "Identity-V":
		cids = make(map[uint16]bool)
		for _, s := range usage.rawStrings {
			data := []byte(s)
			// An odd-length string under a 2-byte CMap leaves a trailing byte
			// that cannot form a CID; the renderer draws .notdef.
			if len(data)%2 != 0 {
				return nil, true, false, true
			}
			for i := 0; i+1 < len(data); i += 2 {
				cids[uint16(data[i])<<8|uint16(data[i+1])] = true
			}
		}
		return cids, false, false, true
	default:
		// Any other named encoding is a predefined CMap, decoded through its
		// bundled bcmap. A nil map means the name is not a recognized
		// predefined CMap (or /Encoding is an embedded stream, which resolves
		// to an empty name here), so the check is skipped.
		//
		// An unmapped code is NOT treated as .notdef here: litho does not
		// resolve a bcmap's usecmap base, so a code could be unmapped merely
		// because its mapping lives in an unresolved base CMap. Flagging it
		// would be a false positive. Only the CIDs litho can resolve are
		// collected and verified against the embedded font program by the
		// caller, which is independent of CMap completeness. This trades a
		// possibly missed .notdef (a false negative) for zero false positives.
		cm := cmap.LoadPredefinedCIDMap(encName)
		if cm == nil {
			return nil, false, false, false
		}
		cids = make(map[uint16]bool)
		for _, s := range usage.rawStrings {
			for _, c := range cm.Decode([]byte(s)) {
				if c.Mapped && c.CID >= 0 && c.CID <= 0xFFFF {
					cids[uint16(c.CID)] = true
				}
			}
		}
		return cids, false, false, true
	}
}

// type0UsedCIDs resolves the CIDs a Type0 font's text operators reference,
// through the same /Encoding decoding the notdef check uses (predefined
// CMaps, Identity, or an embedded CMap stream). Returns nil when the
// encoding cannot be decoded or nothing is used: callers treat nil as
// "usage unknown" and skip usage-scoped checks rather than over-fire.
func type0UsedCIDs(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) map[uint16]bool {
	if usage == nil || len(usage.rawStrings) == 0 {
		return nil
	}
	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	var embedded *cmap.CIDMap
	if encName == "" {
		if stream, err := document.ResolveStream(ctx, doc, fontDict["Encoding"]); err == nil {
			if data, derr := doc.DecodeStream(ctx, stream); derr == nil {
				embedded = cmap.ParseEmbeddedCIDMap(ctx, data)
			}
		}
	}
	cids, _, _, ok := collectType0CIDs(string(encName), embedded, usage)
	if !ok || len(cids) == 0 {
		return nil
	}
	return cids
}

// defaultCIDWidth returns the CIDFont /DW value, or the PDF default of 1000.
func defaultCIDWidth(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict) float64 {
	r, _ := document.ResolveObject(ctx, doc, cidFontDict["DW"])
	if w := document.Float(r); w != 0 {
		return w
	}
	return 1000
}

// checkType0NotdefWidth flags the glyph-width rule when a Type0 font renders
// .notdef and the .notdef (GID 0) program advance differs from its declared
// width. Per ISO 19005-2 §6.2.11.5 / 19005-4 §6.2.10.5, veraPDF resolves every
// shown code to a GID and compares the program advance against the declared
// width; a code shown as CID 0, or a decode-level .notdef from an odd-length
// code tail or an out-of-range CID, resolves to GID 0. The /W-array width check
// ([checkCIDFontWidths]) skips CID 0 and never produces the padded CID, so it
// under-reports the width rule on the .notdef-rendering case. The declared width
// is /DW for an out-of-/W padded code, and the CID 0 entry (/W[0] or /DW) for a
// directly shown CID 0. Usage-scoped: fires only when .notdef is actually shown,
// so it cannot flag a conforming file (a strict subset of the per-used-glyph
// comparison veraPDF performs).
func checkType0NotdefWidth(ctx context.Context, doc *document.Document, fontDict, cidFontDict ast.PdfDict, binaryWidths map[uint16]float64, unitsPerEm uint16, fontName string, pageNum int, rule Rule, usage *fontUsageInfo) []Violation {
	if usage == nil || len(usage.rawStrings) == 0 {
		return nil
	}
	bw, ok := binaryWidths[0]
	if !ok {
		return nil
	}

	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	var embedded *cmap.CIDMap
	if encName == "" {
		if stream, err := document.ResolveStream(ctx, doc, fontDict["Encoding"]); err == nil {
			if data, derr := doc.DecodeStream(ctx, stream); derr == nil {
				embedded = cmap.ParseEmbeddedCIDMap(ctx, data)
			}
		}
	}
	usedCIDs, notdef, _, ok := collectType0CIDs(string(encName), embedded, usage)
	if !ok {
		return nil
	}

	var declared float64
	switch {
	case notdef:
		declared = defaultCIDWidth(ctx, doc, cidFontDict)
	case usedCIDs[0]:
		declared = lookupCIDWidth(ctx, doc, cidFontDict, 0)
	default:
		return nil
	}

	expected := float64(bw) * 1000.0 / float64(unitsPerEm)
	if widthsInconsistent(declared, expected) {
		return failf(rule, "font %s on page %d: .notdef glyph "+WidthMismatchContext+" (declared=%.1f, font=%.1f)",
			fontName, pageNum, declared, expected)
	}
	return nil
}

// judgeMalformedCodes selects whether [checkType0NotdefGlyph] may conclude
// ".notdef is rendered" from a code string that does not decode, as opposed to
// from a CID the font program lacks. The two differ in what they trust: the
// second reads the program, while the first reads bytes that a damaged content
// stream can invent. zlib resynchronises on damaged input, so a page whose
// stream decompresses only partway yields operators and strings the document
// never held, and a hex string with an odd digit count under Identity-H looks
// exactly like a rendered .notdef. litho judges that once per font object,
// where it has always been judged, and does not re-run it for every page that
// selects the font.
//
// # Corpus evidence
//
//   - pdfjs issue11678.pdf: a partly decompressed stream inflates into a hex
//     string with an odd digit count, which under Identity-H looks exactly
//     like a rendered .notdef.
type judgeMalformedCodes bool

const (
	withMalformedCodes    judgeMalformedCodes = true
	withoutMalformedCodes judgeMalformedCodes = false
)

// checkType0NotdefGlyph checks if a Type0 (composite) font references CID 0
// (.notdef) in any text operator string, or references a CID that is absent
// from the embedded font program's charset (also renders as .notdef). It
// handles Identity-H/V, predefined CMap and embedded CMap stream encodings (see
// [collectType0CIDs]); an encoding litho cannot decode to CIDs is left
// unchecked. malformed selects whether a code string that does not decode may
// be judged (see [judgeMalformedCodes]).
//
// # Corpus evidence
//
//   - govdocs thread_200_200565.pdf: its 411 show operators are all mode 3,
//     and veraPDF reports 6.2.11.8 and not 6.2.11.4.1 for a used CID absent
//     from the CFF charset.
func checkType0NotdefGlyph(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo, malformed judgeMalformedCodes) []Violation {
	rule := notdefRule(profile)
	if rule.Specification == "" || usage == nil || len(usage.rawStrings) == 0 {
		return nil
	}

	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	var embedded *cmap.CIDMap
	if encName == "" {
		if stream, err := document.ResolveStream(ctx, doc, fontDict["Encoding"]); err == nil {
			if data, derr := doc.DecodeStream(ctx, stream); derr == nil {
				embedded = cmap.ParseEmbeddedCIDMap(ctx, data)
			}
		}
	}
	usedCIDs, notdef, unmappedZero, ok := collectType0CIDs(string(encName), embedded, usage)
	if !ok {
		// litho cannot decode this /Encoding to CIDs (an unparseable
		// embedded CMap stream or an unrecognized predefined CMap); leave
		// it unchecked rather than risk a false positive.
		return nil
	}
	if notdef && bool(malformed) {
		// A decode-level .notdef (odd-length tail or a CID beyond 65535)
		// also means no real glyph backs the rendered code, so both the
		// .notdef rule and the glyph-presence rule apply: the rendered code
		// resolves to .notdef and to no glyph in the embedded program. The
		// glyph-presence half needs an embedded program to be missing from;
		// see type0HasEmbeddedProgram.
		// renderingMode == 3 exempts the glyph-presence half, which asks only
		// about glyphs referenced for rendering; the .notdef rule carries no
		// such exemption.
		results := failf(rule, "font %s on page %d: text renders .notdef (odd-length code tail or CID beyond 65535)", fontName, pageNum)
		if pr := glyphPresenceRule(profile); pr.Specification != "" && usage.visible && type0HasEmbeddedProgram(ctx, doc, fontDict) {
			results = append(results, failf(pr,
				"font %s on page %d: rendered code resolves to no glyph in the embedded font program", fontName, pageNum)...)
		}
		return results
	}

	// Every used CID is its own glyph for the presence rule; see
	// [fontJudgements.glyphs]. The .notdef rule is stated for the first alone.
	notdefs := newGlyphFaults(rule)
	presence := newGlyphFaults(glyphPresenceRule(profile))
	checksPresence := presence.rule.Specification != "" && usage.visible
	judged := func() []Violation {
		return append(notdefs.first(), presence.violations()...)
	}

	// CID 0 is always .notdef.
	if usedCIDs[0] {
		notdefs.add(0, "font %s on page %d: text references CID 0 (.notdef)", fontName, pageNum)
		// A code the encoding CMap maps nowhere reaches no glyph at all, which
		// the glyph-presence rule covers as well: veraPDF reports both for it.
		// A code written as CID 0 reaches the .notdef glyph, which the program
		// does contain, so that keeps firing the .notdef rule alone (both tools
		// agree there; see absentType0CIDs). renderingMode == 3 exempts the
		// glyph-presence half, as in the decode-level branch above.
		if unmappedZero && checksPresence && type0HasEmbeddedProgram(ctx, doc, fontDict) {
			presence.add(0, "font %s on page %d: the encoding CMap maps a rendered code to no glyph in the embedded font program", fontName, pageNum)
		}
	}

	// Resolve descendant CIDFont to verify referenced CIDs exist in the font program.
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return judged()
	}
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		return judged()
	}

	fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return judged()
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return judged()
	}

	cidSubtype := fontSubtype(ctx, doc, cidFontDict)

	switch cidSubtype {
	case "CIDFontType0":
		// CFF font: check if CIDs exist in the charset. Sort for a
		// deterministic context when several CIDs are absent.
		validCIDs := typography.ExtractValidCIDs(fontData)
		if validCIDs == nil {
			return judged()
		}
		for _, cid := range sortedCIDs(usedCIDs) {
			if cid == 0 || validCIDs[cid] {
				continue
			}
			// A used CID absent from the CFF charset has no glyph in the
			// program, so both the .notdef rule and the glyph-presence rule
			// apply: the code renders .notdef and no glyph is present for it.
			// Glyph presence asks only about glyphs referenced for rendering,
			// so renderingMode == 3 exempts it, as in the CIDFontType2 branch
			// below; the .notdef rule keeps firing, as veraPDF reports 6.2.11.8
			// and not 6.2.11.4.1 when every show operator is mode 3.
			notdefs.add(uint32(cid), "font %s on page %d: CID %d not in font charset (.notdef)", fontName, pageNum, cid)
			if checksPresence {
				presence.add(uint32(cid), "font %s on page %d: CID %d not present in embedded font program", fontName, pageNum, cid)
			}
		}

	case "CIDFontType2":
		// A TrueType CIDFont renders .notdef only when a used CID resolves to
		// GID 0 (the ".notdef" glyph), already covered above by the CID 0 check
		// (Identity /CIDToGIDMap) and the decode-level notdef flag. ISO 19005-2
		// §6.2.11.8 (veraPDF object Glyph, test name != ".notdef") keys on the
		// glyph being .notdef, NOT on an empty outline: an in-range non-zero GID
		// is a real glyph even with no contours and a zero advance (a control-
		// character glyph such as uni0000), so treating "empty outline + width 0"
		// as .notdef was a false positive. A non-zero CID that a /CIDToGIDMap
		// stream remaps to GID 0 is left unflagged here too: whether it is truly
		// rendered depends on the font's embedded /Encoding CMap, which veraPDF
		// decodes differently, so firing would reintroduce false positives.
		//
		// Glyph presence is a separate question, and one case of it is not
		// ambiguous at all: a CID at or beyond numGlyphs names a glyph the
		// program does not contain, so there is no outline to be uncertain
		// about. That fires the glyph-presence rule on its own, without the
		// .notdef rule the reasoning above rules out.
		// renderingMode == 3 exempts the glyph, so a font every one of whose
		// uses was invisible has nothing to answer for.
		if checksPresence {
			if cidToGIDMap, _ := document.ResolveName(ctx, doc, cidFontDict["CIDToGIDMap"]); cidToGIDMap == "Identity" {
				for _, cid := range outOfProgramCIDs(fontData, usedCIDs) {
					presence.add(uint32(cid), "font %s on page %d: CID %d not present in embedded font program",
						fontName, pageNum, cid)
				}
			}
		}
	}

	return judged()
}

// outOfProgramCIDs returns the used CIDs that an Identity /CIDToGIDMap resolves
// past the end of the embedded program, in ascending order.
//
// Under an Identity map the glyph index equals the CID (ISO 32000-2, 9.7.4.2),
// so numGlyphs from 'maxp' bounds what the program can define. Such a CID names
// no glyph at all, which is a different thing from naming an empty one: there is
// no glyf entry to be empty, and no width to consult either, so the zero-width
// condition that guards the empty-glyph case does not apply. An unreadable or
// zero numGlyphs yields nothing rather than a guess.
func outOfProgramCIDs(fontData []byte, usedCIDs map[uint16]bool) []uint16 {
	numGlyphs := typography.ExtractNumGlyphs(fontData)
	if numGlyphs <= 0 {
		return nil
	}
	var outside []uint16
	for cid := range usedCIDs {
		if int(cid) >= numGlyphs {
			outside = append(outside, cid)
		}
	}
	slices.Sort(outside)
	return outside
}

// type0HasEmbeddedProgram reports whether a composite font's descendant CIDFont
// carries a readable embedded font program. The glyph-presence rule
// (ISO 19005-1 §6.3.5 t1, ISO 19005-2/3 §6.2.11.4.1 t2, ISO 19005-4 §6.2.10.4.1
// t2, ISO 14289-1 §7.21.4.1 t2, ISO 14289-2 §8.4.5.5.1 t2) constrains embedded
// programs only: "Embedded font programs shall define all font glyphs referenced
// for rendering". With nothing embedded there is no program a glyph could be
// missing from, which is why veraPDF's test passes on `isGlyphPresent == null`.
// The unembedded case is the embedding rule's concern (§6.3.4 t1 and its
// per-part spellings), not this one.
func type0HasEmbeddedProgram(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return false
	}
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		return false
	}
	fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return false
	}
	return extractFontProgramData(ctx, doc, fd) != nil
}

// absentType0CIDs returns the set of used CIDs whose glyph is absent from the
// composite font's embedded program, plus the descendant CIDFont subtype. A CID
// is absent when a CIDFontType0 (CFF) charset omits it, or when a CIDFontType2
// (TrueType) under an Identity CIDToGIDMap names a glyph index past the
// program's glyph count; CID 0 is always .notdef. Such CIDs have no glyph, so
// they fire the glyph-presence rule. The subtype lets callers apply the
// CFF-specific per-glyph ToUnicode exemption: a CFF charset that omits a CID
// has no glyph for it at all, so there is no glyph to map and the per-glyph
// ToUnicode rule does not apply. ok is false when litho cannot decode the
// encoding to CIDs; callers then stay conservative.
//
// An empty glyf entry with a zero advance is not absent: the program defines
// that glyph, only without an outline, which is what ISO 19005-1, 6.3.5 asks
// ("define all font glyphs referenced for rendering"). [checkType0NotdefGlyph]
// reads a CIDFontType2 the same way for parts 2 to 4. Measured with veraPDF
// 1.30 at PDF/A-1b: such glyphs pass 6.3.5 t1.
//
// # Corpus evidence
//
//   - pdfjs issue5874.pdf: shows two CIDs whose glyf entries are empty and
//     whose /W widths are 0, and veraPDF reports no ISO 19005-1, 6.3.5 t1 for
//     them, with or without the /CIDToGIDMap /Identity ISO 19005-1, 6.3.3.2
//     requires, which remediation adds.
func absentType0CIDs(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) (absent map[uint16]bool, subtype string, notdef bool, ok bool) {
	absent = make(map[uint16]bool)
	if usage == nil || len(usage.rawStrings) == 0 {
		return absent, "", false, false
	}

	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	var embedded *cmap.CIDMap
	if encName == "" {
		if stream, err := document.ResolveStream(ctx, doc, fontDict["Encoding"]); err == nil {
			if data, derr := doc.DecodeStream(ctx, stream); derr == nil {
				embedded = cmap.ParseEmbeddedCIDMap(ctx, data)
			}
		}
	}
	usedCIDs, notdef, _, decodeOK := collectType0CIDs(string(encName), embedded, usage)
	if !decodeOK {
		return absent, "", notdef, false
	}
	// CID 0 (.notdef) is deliberately NOT treated as absent here: its glyph IS
	// present in the program (it is the .notdef glyph), so neither the
	// glyph-presence nor the per-glyph ToUnicode rule fires on it. A reference
	// to CID 0 is a .notdef-reference concern handled by checkType0NotdefGlyph
	// instead.

	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return absent, "", notdef, true
	}
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		return absent, "", notdef, true
	}
	subtype = fontSubtype(ctx, doc, cidFontDict)
	fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return absent, subtype, notdef, true
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return absent, subtype, notdef, true
	}

	switch subtype {
	case "CIDFontType0":
		if validCIDs := typography.ExtractValidCIDs(fontData); validCIDs != nil {
			for cid := range usedCIDs {
				if !validCIDs[cid] {
					absent[cid] = true
				}
			}
		}
	case "CIDFontType2":
		// ISO 32000-1, Table 117 gives /CIDToGIDMap the default value Identity,
		// so an absent entry maps CID to GID exactly as the explicit name does
		// and must be judged the same way. Reading only the explicit name made
		// the verdict depend on whether the producer wrote a redundant entry,
		// so a remediation that added the /CIDToGIDMap /Identity ISO 19005-1
		// §6.3.3.2 requires flipped a pass to a fail. A repair that only writes
		// a documented default cannot change what the file means.
		cidToGIDMap, _ := document.ResolveName(ctx, doc, cidFontDict["CIDToGIDMap"])
		_, hasCIDToGID := cidFontDict["CIDToGIDMap"]
		if cidToGIDMap == "Identity" || !hasCIDToGID {
			// renderingMode == 3 exempts a glyph outright, and usage.visible is
			// false only when every use of this font was in that mode.
			if usage.visible {
				for _, cid := range outOfProgramCIDs(fontData, usedCIDs) {
					absent[cid] = true
				}
			}
		}
	}
	return absent, subtype, notdef, true
}

// checkType0GlyphPresence emits the glyph-presence rule for a composite font's
// used CIDs that are absent from the embedded program. PDF/A parts 2-4 and
// PDF/UA emit glyph presence alongside the .notdef rule in
// [checkType0NotdefGlyph]; this covers PDF/A-1, whose profiles define §6.3.5 t1
// (glyph presence) but no .notdef rule, so checkType0NotdefGlyph returns before
// reaching the emission. A used CID absent from the embedded program has no
// glyph, which violates §6.3.5 t1 regardless of the part, so PDF/A-1 must fire
// it from here.
func checkType0GlyphPresence(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := glyphPresenceRule(profile)
	if rule.Specification == "" {
		return nil
	}
	// Parts with a .notdef rule already emit glyph presence via
	// checkType0NotdefGlyph; only PDF/A-1 reaches this path.
	if notdefRule(profile).Specification != "" {
		return nil
	}
	absent, _, notdef, ok := absentType0CIDs(ctx, doc, fontDict, usage)
	if !ok {
		return nil
	}
	// A decode-level .notdef (odd-length code tail or a CID beyond 65535) means
	// the rendered code resolves to no glyph. Parts 2-4 fire this via
	// checkType0NotdefGlyph; PDF/A-1 has no Type0 .notdef rule and reaches only
	// this path, so honor the decode signal here for parity. It names no CID.
	// A non-empty absent set already implies an embedded program (absentType0CIDs
	// populates it only from decoded program bytes); the decode signal does not,
	// so recheck. See type0HasEmbeddedProgram for why an unembedded font is out
	// of scope for §6.3.5 t1.
	if notdef {
		if !type0HasEmbeddedProgram(ctx, doc, fontDict) {
			return nil
		}
		return failf(rule, "font %s on page %d: rendered code resolves to no glyph in the embedded font program", fontName, pageNum)
	}
	// Every absent CID is its own glyph; see [fontJudgements.glyphs].
	faults := newGlyphFaults(rule)
	for _, cid := range sortedCIDs(absent) {
		faults.add(uint32(cid), "font %s on page %d: CID %d resolves to no glyph in the embedded font program", fontName, pageNum, cid)
	}
	// A code the encoding CMap maps nowhere reaches no glyph at all. Parts 2 to
	// 4 report it from checkType0NotdefGlyph beside the .notdef rule; PDF/A-1
	// has no .notdef rule and reaches only this path, so honor the signal here.
	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	var embedded *cmap.CIDMap
	if encName == "" {
		if stream, serr := document.ResolveStream(ctx, doc, fontDict["Encoding"]); serr == nil {
			if data, derr := doc.DecodeStream(ctx, stream); derr == nil {
				embedded = cmap.ParseEmbeddedCIDMap(ctx, data)
			}
		}
	}
	if _, _, unmappedZero, decodeOK := collectType0CIDs(string(encName), embedded, usage); decodeOK && unmappedZero &&
		usage.visible && type0HasEmbeddedProgram(ctx, doc, fontDict) {
		faults.add(0, "font %s on page %d: the encoding CMap maps a rendered code to no glyph in the embedded font program", fontName, pageNum)
	}
	return faults.violations()
}

// collectVisibleFontObjects returns the font object numbers that at least one
// page renders visibly. The embedding rule is reported once per font object, so
// it cannot ask a single page whether the font is visible: a font drawn in
// rendering mode 3 on the page that happens to declare it first, and normally on
// a later page, is embedded-or-not regardless of which page is asked. A page
// whose content stream an earlier page shares renders nothing through it; see
// [markJudgedContents].
func collectVisibleFontObjects(ctx context.Context, doc *document.Document, vctx *validationCtx) map[int]bool {
	visible := make(map[int]bool)
	for pageIdx := range vctx.pages {
		if ctx.Err() != nil {
			return visible
		}
		p := &vctx.pages[pageIdx]
		if p.resources == nil || p.contentJudged {
			continue
		}
		fontDict, err := document.ResolveDict(ctx, doc, p.resources["Font"])
		if err != nil || fontDict == nil {
			continue
		}
		pageUsage := collectFontUsage(p.parsedOps(ctx))
		for name, ref := range fontDict {
			indirect, ok := ref.(ast.PdfIndirectRef)
			if !ok {
				continue
			}
			if p.parseFailed {
				visible[indirect.ObjectNum] = true
				continue
			}
			if u := pageUsage[string(name)]; u != nil && u.visible {
				visible[indirect.ObjectNum] = true
			}
		}
	}
	return visible
}
