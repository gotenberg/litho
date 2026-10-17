package compliance

import (
	"bytes"
	"context"
	"slices"
	"strconv"
	"strings"

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
	// scope names the resource dictionary of the stream and the resource name
	// the font is selected through; see [fontJudgements.readsProgram].
	scope string
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

// textStateWalk follows the text font and the text rendering mode through the
// operators of one content stream.
//
// Per ISO 32000-1, 8.4.2 the graphics state includes the text state, and Table
// 104 counts the text font and the text rendering mode among its parameters,
// so Q restores both. Reading Tf as if it persisted past the enclosing Q
// attributes character codes to whichever font happens to have been selected
// last, which is how an unrelated font came to be reported for glyphs it never
// showed. The stack holds one entry per q operator already present in the
// materialized operator slice, so it needs no bound of its own.
//
// A Form XObject is painted with the graphics state in force at the Do that
// draws it (ISO 32000-1, 8.10.1), so its stream starts in the rendering mode
// that Do leaves, and veraPDF judges its glyphs in that mode. Measured with
// veraPDF 1.30 at PDF/A-1b and PDF/A-2b on a CID a TrueType CIDFont lacks,
// shown in a form: drawn after "BT 3 Tr ET", it fails neither ISO 19005-1,
// 6.3.5 t1 nor ISO 19005-2, 6.2.11.4.1 t2, which exempt rendering mode 3, and
// still fails ISO 19005-2, 6.2.11.8 t1.
type textStateWalk struct {
	font  string
	mode  int
	saved []textState
}

// textState is the part of the graphics state a [textStateWalk] follows.
type textState struct {
	font string
	mode int
}

// step applies op to the text state.
func (w *textStateWalk) step(op content.Operation) {
	switch op.Operator {
	case "q":
		w.saved = append(w.saved, textState{font: w.font, mode: w.mode})
	case "Q":
		if n := len(w.saved); n > 0 {
			w.font, w.mode = w.saved[n-1].font, w.saved[n-1].mode
			w.saved = w.saved[:n-1]
		}
	case "Tf":
		if len(op.Operands) >= 1 {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				w.font = string(name)
			}
		}
	case "Tr":
		if len(op.Operands) >= 1 {
			if mode, ok := op.Operands[0].(ast.PdfInt); ok {
				w.mode = int(mode)
			}
		}
	}
}

// shownOperand returns the operand a text-showing operator paints, a string
// or an array of strings and adjustments, and whether op shows text at all
// (ISO 32000-1, 9.4.3, Table 109).
func shownOperand(op content.Operation) (ast.PdfObject, bool) {
	switch op.Operator {
	case "Tj", "TJ":
		if len(op.Operands) >= 1 {
			return op.Operands[0], true
		}
		return nil, true
	case "'", "\"":
		if len(op.Operands) >= 1 {
			return op.Operands[len(op.Operands)-1], true
		}
		return nil, true
	}
	return nil, false
}

// collectFontUsageAt is [collectFontUsage] for the content stream site names,
// which keys the marked-content sequences its shows are recorded in; see
// [markedContentKey].
func collectFontUsageAt(ops []content.Operation, site showSite) map[string]*fontUsageInfo {
	if len(ops) == 0 {
		return nil
	}

	usage := make(map[string]*fontUsageInfo)
	text := textStateWalk{mode: site.mode}
	// Marked content is not part of the graphics state (ISO 32000-1, 14.6), so
	// q and Q leave this stack alone. It holds at most one entry per operator
	// already materialized.
	var mcStack []string

	getOrCreate := func(name string) *fontUsageInfo {
		if u, ok := usage[name]; ok {
			return u
		}
		u := &fontUsageInfo{charCodes: make(map[byte]bool), scope: site.key + "/" + name}
		usage[name] = u
		return u
	}

	record := func(u *fontUsageInfo, s ast.PdfString) {
		for _, b := range []byte(s) {
			u.charCodes[b] = true
		}
		u.rawStrings = append(u.rawStrings, s)
		at := glyphContext{mode: text.mode, owner: site.owner}
		if n := len(mcStack); n > 0 {
			at.mc = mcStack[n-1]
		}
		u.shows = append(u.shows, glyphShow{str: s, at: at})
	}

	collectStringBytes := func(obj ast.PdfObject) {
		if text.font == "" {
			return
		}
		u := getOrCreate(text.font)
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
		text.step(op)
		switch op.Operator {
		case "BDC", "BMC":
			mcStack = append(mcStack, markedContentKey(op, i, site))
		case "EMC":
			if n := len(mcStack); n > 0 {
				mcStack = mcStack[:n-1]
			}
		}
		shown, isShow := shownOperand(op)
		if !isShow {
			continue
		}
		if text.font != "" && text.mode != invisibleRenderingMode {
			getOrCreate(text.font).visible = true
		}
		if shown != nil {
			collectStringBytes(shown)
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

	// judged keys every statement on the font object it is about
	// ([fontIdentity]), so no pass re-checks a font another pass met.
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
		judged.pageRes = p.resources
		// A content stream an earlier page shares is judged there alone, with
		// the fonts that page's resources name, so this page selects no font
		// through it; see [markJudgedContents].
		judgeContent := !p.contentJudged
		site := pageSite(ctx, doc, p, pageIdx+1)

		// veraPDF reads a font's program where it first parses the font in a
		// rendering mode, and it parses a form where the page draws it, so the
		// page's text and its forms claim their modes in the order the page
		// draws them, before any is judged. Where the page's annotation
		// appearances stand in that order depends on the part; see
		// [annotationsParsedFirst].
		//
		// The same walks find the mode each Type 3 font is first shown in, the
		// page's content before its annotations whatever the part; see
		// [checkType3CharProcFonts].
		contentModes, annotModes := make(map[string]int), make(map[string]int)
		annotsFirst := annotationsParsedFirst(profile)
		if annotsFirst {
			judged.claimAnnotationReads(ctx, doc, p.dict, pageIdx+1, annotModes)
		}
		if judgeContent {
			walk := claimWalk{pageNum: pageIdx + 1, visited: make(map[int]bool), forms: pageFormReads(profile), type3: make(map[int]*type3Procs), type3Modes: contentModes}
			judged.claimStreamReads(ctx, doc, walk, p.parsedOps(ctx), p.resources, site, paintState{}, 0, false)
		}
		if !annotsFirst {
			judged.claimAnnotationReads(ctx, doc, p.dict, pageIdx+1, annotModes)
		}
		judged.recordType3Modes(contentModes)
		judged.recordType3Modes(annotModes)

		if judgeContent {
			results = append(results, checkPageDeclaredFonts(ctx, doc, catalog, p, site, pageIdx+1, profile, judged, visibleAnywhere)...)

			// Fonts used only inside a Form XObject invoked from the page
			// content (a Do operator) are likewise not reached by the page
			// /Resources/Font walk. veraPDF descends into the form's content
			// stream and applies the same font rules there; litho matches by
			// walking invoked forms.
			xobjVisited := make(map[int]bool)
			results = append(results, checkFormXObjectFonts(ctx, doc, p.parsedOps(ctx), 0, p.resources, treeRoot, pageIdx+1, profile, judged, xobjVisited, 0)...)

			// A tiling pattern's cell is another content stream the page walk
			// above never enters. See checkPatternFonts.
			results = append(results, checkPatternFonts(ctx, doc, p.parsedOps(ctx), p.resources, treeRoot, pageIdx+1, profile, judged, xobjVisited, 0)...)

			// A Type3 glyph procedure is another content stream that may select
			// fonts of its own, and the page walk above stops at the Type3 font
			// itself. veraPDF applies the font rules inside those procedures
			// too.
			results = append(results, checkType3CharProcFonts(ctx, doc, p.parsedOps(ctx), p.resources, site, treeRoot, pageIdx+1, profile, judged, xobjVisited, 0)...)
		}

		// Fonts referenced only from annotation appearance streams are never
		// reached by the page /Resources/Font walk above. veraPDF validates them
		// through the appearance content stream (for example
		// annots[0]/appearance[0]/contentStream[0]/operators[N]/font[0]), after
		// the page's content and the forms it draws, which decides where it
		// reads a font's program ([fontJudgements.readsProgram]).
		results = append(results, checkAnnotationAppearanceFonts(ctx, doc, p.dict, treeRoot, pageIdx+1, profile, judged)...)
	}

	doc.Logger.Debug("compliance: fonts: font checks complete",
		"violations", len(results), "fonts_checked", len(judged.dict))
	return results
}

// pageSite returns the [showSite] of a page's own content stream.
func pageSite(ctx context.Context, doc *document.Document, p *pageCtx, pageNum int) showSite {
	site := showSite{key: "page " + strconv.Itoa(pageNum), owner: structureKey(ctx, doc, p.dict), fonts: pageFontsKey(p, pageNum)}
	site.props, _ = document.ResolveDict(ctx, doc, p.resources["Properties"])
	return site
}

// pageFontsKey returns the [fontDictKey] of a page's /Font dictionary. The
// resources are the page's own or, when it has none, those of its nearest
// ancestor in the page tree, as [buildValidationCtx] resolves them, so pages
// that inherit one dictionary share one key.
func pageFontsKey(p *pageCtx, pageNum int) string {
	node, _, ancestor := p.inheritedHolder("Resources")
	holder := "page " + strconv.Itoa(pageNum)
	switch {
	case ancestor != nil:
		holder = ancestor.key
	case p.objNum != 0:
		holder = "obj " + strconv.Itoa(p.objNum)
	}
	return fontDictKey(holder, node["Resources"], p.resources)
}

// fontJudgements records, for one run of the font group, what it has stated
// about each font object, so every rule is stated as often as veraPDF states
// it: veraPDF judges an object it reaches again only the first time. Every map
// is keyed by [fontIdentity] or [descendantIdentity].
type fontJudgements struct {
	// dict holds the font objects whose dictionary rules were judged.
	dict map[string]bool
	// descendant holds the descendant CIDFonts whose own rules were judged;
	// see [fontJudgements.descendantRules].
	descendant map[string]bool
	// subset holds the objects the subset rule failed for, a simple font or
	// a composite font's descendant CIDFont; see
	// [fontJudgements.subsetCoverage].
	subset map[string]bool
	// glyph holds the glyph-level statements already made; see
	// [fontJudgements.glyphs].
	glyph map[glyphKey]bool
	// programMode holds the pairs of font object and rendering mode already
	// shown, and programScope the scopes that showed one of them first; see
	// [fontJudgements.readsProgram].
	programMode  map[string]bool
	programScope map[string]bool
	// type3Open holds the object numbers of the Type 3 fonts whose glyph
	// procedures the walk is inside; see [checkType3CharProcFonts].
	type3Open map[int]bool
	// type3Mode holds the rendering mode each Type 3 font object is first
	// shown in, and type3Proc the object numbers of the glyph procedures
	// already judged; see [checkType3CharProcFonts].
	type3Mode map[string]int
	type3Proc map[int]bool
	// pageRes holds the resources of the page the walk is on, which name a
	// property list the resources of a form it draws leave out; see
	// [markedContentProps].
	pageRes ast.PdfDict
}

// newFontJudgements returns an empty record.
func newFontJudgements() *fontJudgements {
	return &fontJudgements{
		dict:         make(map[string]bool),
		descendant:   make(map[string]bool),
		subset:       make(map[string]bool),
		glyph:        make(map[glyphKey]bool),
		programMode:  make(map[string]bool),
		programScope: make(map[string]bool),
		type3Open:    make(map[int]bool),
		type3Mode:    make(map[string]int),
		type3Proc:    make(map[int]bool),
	}
}

// recordType3Modes records in j the modes a claim walk found each Type 3 font
// object first shown in, for the fonts no earlier walk recorded.
func (j *fontJudgements) recordType3Modes(modes map[string]int) {
	for id, mode := range modes {
		if _, ok := j.type3Mode[id]; !ok {
			j.type3Mode[id] = mode
		}
	}
}

// subsetCoverage states the rule a font subset's /CharSet or /CIDSet answers
// (see [checkSubsetCoverage]) against the codes one content stream shows
// through fontRef, unless it already failed for the object it belongs to.
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
// The /CIDSet rule belongs to the descendant CIDFont, so composite fonts that
// share one descendant object fail it once between them. Measured with
// veraPDF 1.30 at PDF/A-1b and PDF/A-2b: two Type0 fonts naming one CIDFont
// object whose /CIDSet leaves out the shown CIDs fail ISO 19005-1, 6.3.5 t3
// and ISO 19005-2, 6.2.11.4.2 t2 once, and two naming a CIDFont object each,
// over one shared descriptor, twice.
//
// A font or descendant written directly in its container is keyed on the
// entry that writes it ([fontIdentity], [descendantIdentity]). Measured with
// veraPDF 1.30 at PDF/A-1b: a Type1 subset with no /CharSet, and a Type0
// subset with no /CIDSet whose descendant is written inline, fail ISO
// 19005-1, 6.3.5 t2 or t3 twice when two pages each write the font inline,
// and once when one form that writes it is drawn on two pages.
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
//   - pdfjs issue6127.pdf: names 24 Type0 fonts over 8 descendant CIDFonts,
//     11 of them over one, and veraPDF fails ISO 19005-1, 6.3.5 t3 6 times
//     and ISO 19005-2, 6.2.11.4.2 t2 5 times, where litho failed them 17 and
//     18 times.
//   - All corpora: keying the /CIDSet rule on the descendant moved the counts
//     of ISO 19005-1, 6.3.5 t3 toward veraPDF's in 14 files and those of ISO
//     19005-2, 6.2.11.4.2 t2 in 8, and none away; their summed distance fell
//     from 59 to 3 and from 106 to 0.
func (j *fontJudgements) subsetCoverage(ctx context.Context, doc *document.Document, id string, font ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	key := subsetRuleObject(ctx, doc, id, font)
	if j.subset[key] {
		return nil
	}
	results := checkSubsetCoverage(ctx, doc, font, fontName, pageNum, profile, usage)
	if len(results) > 0 {
		j.subset[key] = true
	}
	return results
}

// subsetRuleObject names the object veraPDF states the subset rule on: the
// descendant CIDFont of a composite font, and the font id names otherwise.
func subsetRuleObject(ctx context.Context, doc *document.Document, id string, font ast.PdfDict) string {
	if fontSubtype(ctx, doc, font) == "Type0" {
		descArr, err := document.ResolveArray(ctx, doc, font["DescendantFonts"])
		if err == nil && len(descArr) > 0 {
			return descendantIdentity(id, descArr[0])
		}
	}
	return id
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
// in a page's /Resources/Font, whose content stream site names. It records each
// font object in judged ([fontIdentity]) so no later page or pass re-checks its
// dictionary.
//
// # Corpus evidence
//
//   - pdfjs issue11678.pdf: its page 2 stream declares /Length 4218 over no
//     data, and judging glyphs on unparsable content reports a .notdef
//     against recovered bytes veraPDF never reads.
func checkPageDeclaredFonts(ctx context.Context, doc *document.Document, catalog ast.PdfDict, p *pageCtx, site showSite, pageNum int, profile string, judged *fontJudgements, visibleAnywhere map[string]bool) []Violation {
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
	fontUsage := collectFontUsageAt(p.parsedOps(ctx), site)
	parseFailed := p.parseFailed

	var results []Violation
	for _, fontName := range sortedNames(fontDict) {
		fontRef := fontDict[fontName]
		if ctx.Err() != nil {
			return results
		}

		fontKey := string(fontName)
		// Dedup by the font object, not the resource name: different pages
		// may bind the same name to different font objects (e.g. an embedded
		// subset "TT1" on one page and an unembedded standard "TT1" on another),
		// and veraPDF checks each distinct font object, a font written directly
		// in the resources included ([fontIdentity]). A font object already seen
		// has had its dictionary rules reported, but this page's usage is still
		// unjudged: the codes it shows here may differ from those on the page
		// that first selected it.
		id := fontIdentity(fontRef, fontName, site)
		dictRulesDone := judged.dict[id]

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

		judged.dict[id] = true
		// Skip width checks for fonts not used for visible rendering here.
		isVisible := parseFailed || (usage != nil && usage.visible)
		// Embedding is judged once per object, so it asks whether any page
		// renders the font visibly.
		embedVisible := isVisible || visibleAnywhere[id]

		var fontResults []Violation
		if !dictRulesDone {
			fontResults = append(fontResults, checkFontDictRules(ctx, doc, font, fontKey, pageNum, profile, embedVisible)...)
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
				fontResults = append(fontResults, judged.descendantRules(ctx, doc, id, font, fontKey, pageNum, profile, isVisible)...)
				fontResults = append(fontResults, checkType0Font(ctx, doc, font, fontKey, pageNum, profile, usage)...)
			}
		} else {
			fontResults = append(fontResults, checkFontUsageRules(ctx, doc, font, fontKey, pageNum, profile, isVisible, usage)...)
		}
		results = append(results, judged.glyphs(ctx, doc, id, font, usage, profile, fontResults)...)
		// The subset rule judges glyphs too, so the unparsable content above
		// states nothing more for a composite font judged on an earlier page.
		if !isType0 || !dictRulesDone || !parseFailed {
			results = append(results, judged.subsetCoverage(ctx, doc, id, font, fontKey, pageNum, profile, usage)...)
		}

		// PUA ActualText check (needs page and catalog context).
		results = append(results, checkPUAWithoutActualText(ctx, doc, font, fontKey, pageNum, profile, p.dict, p.resources, catalog)...)
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
// already checked on the page and across forms. mode is the text rendering
// mode ops starts in, which each form it draws inherits as the Do leaves it
// ([textStateWalk]).
func checkFormXObjectFonts(ctx context.Context, doc *document.Document, ops []content.Operation, mode int, res, treeRoot ast.PdfDict, pageNum int, profile string, judged *fontJudgements, visited map[int]bool, depth int) []Violation {
	if formTooDeep(doc, depth) || ctx.Err() != nil {
		return nil
	}
	xobjDict, _ := document.ResolveDict(ctx, doc, res["XObject"])
	if len(xobjDict) == 0 {
		return nil
	}

	var results []Violation
	text := textStateWalk{mode: mode}
	for _, op := range ops {
		text.step(op)
		form, ok := resolveFontForm(ctx, doc, xobjDict, op, visited, pageNum)
		if !ok {
			continue
		}
		site := streamSite(ctx, doc, form.ref, form.dict["Resources"], form.res)
		site.mode = text.mode
		results = append(results, checkResourceFonts(ctx, doc, form.res, form.ops, site, pageNum, profile, judged)...)
		results = append(results, checkFormPUA(ctx, doc, form.res, judged.pageRes, form.ops, form.data, form.dict, treeRoot, pageNum, profile)...)
		results = append(results, checkDrawnStreamFonts(ctx, doc, form.ops, form.res, site, treeRoot, pageNum, profile, judged, visited, depth+1)...)
	}
	return results
}

// checkDrawnStreamFonts checks the fonts of the content streams one content
// stream draws, whose operations are ops, resources res and [showSite] site:
// the Form XObjects it paints ([checkFormXObjectFonts]), the tiling patterns
// it selects ([checkPatternFonts]) and the glyph procedures of the Type 3
// fonts it shows ([checkType3CharProcFonts]), each walked the same way in
// turn. visited and depth are the walk's, as those checks take them.
//
// veraPDF reaches a stream's fonts wherever its model reaches the stream,
// whatever stream draws it. Measured with veraPDF 1.30 at PDF/A-1b, PDF/A-2b,
// PDF/A-2u, PDF/UA-1, PDF/UA-2 and WTPDF 1.0 on a Type 3 glyph procedure, and
// a tiling pattern cell, showing a CID a TrueType CIDFont lacks: the glyph
// fails ISO 19005-2, 6.2.11.8 t1 and 6.2.11.7.2 t1 once, and at PDF/A
// 6.2.11.4.1 t2 once, where a form the page draws shows the Type 3 glyph; and
// the cell's glyph fails the .notdef rule where a Type 3 glyph procedure or an
// annotation's appearance stream paints with the pattern.
//
// # Corpus evidence
//
//   - pdfjs ContentStreamCycleType3insideType3.pdf: a Type 3 procedure paints
//     text with a pattern whose cell shows the same Type 3 font again.
//     Judging the cell's glyphs of that font raised ISO 19005-2, 6.2.11.7.2
//     t1 at PDF/A-2u from 3 to 5 against veraPDF's 3, which judging none of
//     them there keeps at 3 ([checkType3CharProcFonts]).
//   - All corpora, at PDF/A-1b, PDF/A-2b and PDF/A-2u, and at their own
//     profile for the verapdf files: reaching the fonts of these streams moves
//     no other count. litho fontclaims_2b.pdf shows a Type 3 glyph in a form.
func checkDrawnStreamFonts(ctx context.Context, doc *document.Document, ops []content.Operation, res ast.PdfDict, site showSite, treeRoot ast.PdfDict, pageNum int, profile string, judged *fontJudgements, visited map[int]bool, depth int) []Violation {
	results := checkFormXObjectFonts(ctx, doc, ops, site.mode, res, treeRoot, pageNum, profile, judged, visited, depth)
	results = append(results, checkPatternFonts(ctx, doc, ops, res, treeRoot, pageNum, profile, judged, visited, depth)...)
	return append(results, checkType3CharProcFonts(ctx, doc, ops, res, site, treeRoot, pageNum, profile, judged, visited, depth)...)
}

// fontForm is a Form XObject a Do operator draws, with what the font walks
// read from it: the object that holds it, its dictionary and resources, and its
// decoded content with the operators parsed from it.
type fontForm struct {
	ref  ast.PdfObject
	dict ast.PdfDict
	res  ast.PdfDict
	data []byte
	ops  []content.Operation
}

// resolveFontForm returns the Form XObject the operator op draws from the
// /XObject dictionary xobjDict, and marks it in visited. ok is false for an
// operator other than Do, an XObject that is no form, a form visited already,
// or one that carries no /Resources or whose stream does not decode or parse:
// the font walks judge such a form nowhere, so none of them reads it.
func resolveFontForm(ctx context.Context, doc *document.Document, xobjDict ast.PdfDict, op content.Operation, visited map[int]bool, pageNum int) (fontForm, bool) {
	if op.Operator != "Do" || len(op.Operands) < 1 {
		return fontForm{}, false
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return fontForm{}, false
	}
	xobjRef := xobjDict[name]
	ref, isRef := xobjRef.(ast.PdfIndirectRef)
	if isRef && visited[ref.ObjectNum] {
		return fontForm{}, false
	}
	stream, err := document.ResolveStream(ctx, doc, xobjRef)
	if err != nil || stream.Dict == nil {
		return fontForm{}, false
	}
	if subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]); subtype != "Form" {
		return fontForm{}, false
	}
	if isRef {
		visited[ref.ObjectNum] = true
	}
	xobjRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if xobjRes == nil {
		return fontForm{}, false
	}
	data, reads := decodeContentStream(ctx, doc, stream)
	if !reads {
		return fontForm{}, false
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: cannot parse Form XObject", "page", pageNum, "error", err)
		return fontForm{}, false
	}
	return fontForm{ref: xobjRef, dict: stream.Dict, res: xobjRes, data: data, ops: ops}, true
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
		if op.Operator != "scn" && op.Operator != "SCN" {
			continue
		}
		patternRef := selectedPattern(op, patternDict)
		if patternRef == nil {
			continue
		}
		pattern, ok := resolveFontPattern(ctx, doc, patternRef, visited, pageNum)
		if !ok {
			continue
		}
		site := streamSite(ctx, doc, pattern.ref, pattern.dict["Resources"], pattern.res)
		results = append(results, checkResourceFonts(ctx, doc, pattern.res, pattern.ops, site, pageNum, profile, judged)...)
		results = append(results, checkDrawnStreamFonts(ctx, doc, pattern.ops, pattern.res, site, treeRoot, pageNum, profile, judged, visited, depth+1)...)
	}
	return results
}

// resolveFontPattern returns the tiling pattern patternRef names, a /Pattern
// resource, and marks it in visited, as [resolveFontForm] returns a form. ok
// is false for a shading pattern, which is a dictionary and shows no glyph, an
// uncoloured tiling pattern, which the font walks leave out as veraPDF leaves
// it ([checkPatternFonts]), a pattern visited already, or one that carries no
// /Resources or whose cell does not decode or parse.
func resolveFontPattern(ctx context.Context, doc *document.Document, patternRef ast.PdfObject, visited map[int]bool, pageNum int) (fontForm, bool) {
	ref, isRef := patternRef.(ast.PdfIndirectRef)
	if isRef && visited[ref.ObjectNum] {
		return fontForm{}, false
	}
	stream, err := document.ResolveStream(ctx, doc, patternRef)
	if err != nil || stream.Dict == nil {
		return fontForm{}, false
	}
	// ISO 32000-1, 8.7.3.1, Table 75: PatternType 1 is a tiling pattern, and
	// PaintType 2 an uncoloured one.
	if patternType, _ := document.ResolveInt(ctx, doc, stream.Dict["PatternType"]); patternType != 1 {
		return fontForm{}, false
	}
	if paintType, _ := document.ResolveInt(ctx, doc, stream.Dict["PaintType"]); paintType == 2 {
		return fontForm{}, false
	}
	if isRef {
		visited[ref.ObjectNum] = true
	}
	res, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if res == nil {
		return fontForm{}, false
	}
	data, reads := decodeContentStream(ctx, doc, stream)
	if !reads {
		return fontForm{}, false
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: cannot parse tiling pattern", "page", pageNum, "error", err)
		return fontForm{}, false
	}
	return fontForm{ref: patternRef, dict: stream.Dict, res: res, data: data, ops: ops}, true
}

// checkType3CharProcFonts checks fonts used for rendering inside the glyph
// procedures of a Type3 font that a content stream selects. A Type3 glyph
// procedure is a content stream in its own right (ISO 32000-1 §9.6.5) and may
// select fonts from the Type3 font's own /Resources, including another Type3
// font. veraPDF descends through those procedures and applies the same font
// rules to what they select, so a non-embedded font reached only that way
// (ISO 19005-2 §6.2.11.4.1) is otherwise never checked.
//
// veraPDF reaches every procedure of a Type 3 font some operator shows text
// with (GFPDType3Font.getCharStrings lists the whole /CharProcs), the
// procedures of glyphs no operator shows included, and a font a Tf only
// selects brings none into scope. It parses them once for the document, in the
// text rendering mode of the operator that first shows the font, mode 3
// included, and never again for another mode: the font object it validates is
// the first one it builds, and each procedure's content stream is one object
// (GFPDContentStream is identified by its object number). The first operator is
// the first in drawing order, a page's content, with the forms it draws where
// they are drawn, before its annotations, and the pages in order; the claim
// walk records it in judged.type3Mode ([claimWalk]), and a font it did not
// record takes the mode of the first operator showing it in ops. A procedure
// judged is recorded in judged.type3Proc and not judged again, through this
// font or another.
//
// Measured with veraPDF 1.30 at PDF/A-1b, PDF/A-2b and PDF/A-2u on a Type 3
// glyph whose procedure shows code 1 through an unembedded Helvetica, which
// StandardEncoding names .notdef: shown in rendering modes 0, 1, 2, 4, 5, 6
// and 7 on one page, on two pages in modes 0 and 1, or through two procedures
// shown in modes 0 and 1, it fails ISO 19005-2, 6.2.11.8 t1 and 6.2.11.7.2 t2
// once; shown in mode 3 first, then 0, only in mode 3, in mode 3 on a first
// page and 0 on the next, in mode 3 by a form the page draws before its text
// shows it in mode 0, or in mode 3 by the page and 1 by an annotation, it
// fails them once and ISO 19005-1, 6.3.4 t1 and ISO 19005-2, 6.2.11.4.1 t1 no
// time, the Helvetica being built in mode 3; in mode 0 first, those once. A
// procedure of a glyph the page does not show fails them as one it shows does.
//
// Recursion is bounded by the form budget ([formTooDeep]). visited is the
// walk's, which [checkDrawnStreamFonts] shares with the Form XObject and
// pattern walks; judged.type3Proc breaks a resource cycle through nested Type3
// fonts. site is the stream ops come from; a procedure's fonts are the entries
// of the Type3 font's /Resources, which [fontIdentity] keys on that font.
//
// A stream the walk reaches from a font's glyph procedures that shows the
// same font again has none of that font's glyphs judged there, as veraPDF,
// reading the font's procedures, judges none: judged.type3Open holds the
// fonts whose procedures the walk is inside. Measured with veraPDF 1.30 at
// PDF/A-2b and PDF/A-2u on a Type 3 font whose glyph rect, shown by the page
// in rendering mode 2, paints text with a tiling pattern whose cell shows the
// glyph again in mode 0: ISO 19005-2, 6.2.11.7.2 t1 fails for the page's rect
// and not for the cell's, and a CID the cell also shows through another font
// fails it and 6.2.11.8 t1; another Type 3 font's glyph in the cell fails it.
//
// # Corpus evidence
//
//   - pdfjs ContentStreamCycleType3insideType3.pdf: reaches an unembedded
//     Helvetica through two nested Type3 fonts, and its name warns of the
//     resource cycle the procedure set breaks.
//   - All corpora, at PDF/A-1b, PDF/A-2b and PDF/A-2u, and at their own
//     profile for the verapdf files: judging every procedure once, in the
//     mode the font is first shown in, moves no count; litho
//     unembednames_2u.pdf plants it.
func checkType3CharProcFonts(ctx context.Context, doc *document.Document, ops []content.Operation, res ast.PdfDict, site showSite, treeRoot ast.PdfDict, pageNum int, profile string, judged *fontJudgements, visited map[int]bool, depth int) []Violation {
	if formTooDeep(doc, depth) || ctx.Err() != nil {
		return nil
	}
	fontDict, _ := document.ResolveDict(ctx, doc, res["Font"])
	if len(fontDict) == 0 {
		return nil
	}
	usage := collectFontUsageAt(ops, showSite{mode: site.mode})

	var results []Violation
	for _, fontName := range sortedNames(fontDict) {
		fontUsage := usage[string(fontName)]
		if fontUsage == nil || len(fontUsage.charCodes) == 0 || len(fontUsage.shows) == 0 {
			continue
		}
		fontRef := fontDict[fontName]
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
		mode, recorded := judged.type3Mode[fontIdentity(fontRef, fontName, site)]
		if !recorded {
			mode = fontUsage.shows[0].at.mode
		}
		fontNum := 0
		if ref, ok := fontRef.(ast.PdfIndirectRef); ok && !judged.type3Open[ref.ObjectNum] {
			fontNum = ref.ObjectNum
			judged.type3Open[fontNum] = true
		}
		for _, glyphName := range sortedNames(charProcs) {
			procRef := charProcs[glyphName]
			if ref, ok := procRef.(ast.PdfIndirectRef); ok {
				if judged.type3Proc[ref.ObjectNum] {
					continue
				}
				judged.type3Proc[ref.ObjectNum] = true
			}
			procOps, ok := parseType3Proc(ctx, doc, procRef, string(glyphName), pageNum)
			if !ok {
				continue
			}
			procSite := type3ProcSite(ctx, doc, fontRef, fontName, font, t3Res, site, procRef)
			procSite.mode = mode
			results = append(results, checkResourceFonts(ctx, doc, t3Res, procOps, procSite, pageNum, profile, judged)...)
			results = append(results, checkDrawnStreamFonts(ctx, doc, procOps, t3Res, procSite, treeRoot, pageNum, profile, judged, visited, depth+1)...)
		}
		if fontNum != 0 {
			delete(judged.type3Open, fontNum)
		}
	}
	return results
}

// parseType3Proc returns the operators of the Type 3 glyph procedure procRef,
// the procedure of glyphName, shown on page pageNum. ok is false when it does
// not resolve to a stream, veraPDF reads no operator from it
// ([decodeContentStream]), or it does not parse.
func parseType3Proc(ctx context.Context, doc *document.Document, procRef ast.PdfObject, glyphName string, pageNum int) ([]content.Operation, bool) {
	stream, err := document.ResolveStream(ctx, doc, procRef)
	if err != nil {
		return nil, false
	}
	data, reads := decodeContentStream(ctx, doc, stream)
	if !reads {
		return nil, false
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: cannot parse Type3 CharProc",
			"page", pageNum, "glyph", glyphName, "error", err)
		return nil, false
	}
	return ops, true
}

// type3ProcSite returns the [showSite] of the glyph procedure procRef of the
// Type 3 font font, whose resources are t3Res, shown through the resource name
// fontName, which names fontRef, in the stream site names. The procedure's
// glyphs belong to the structure key of the font, and its fonts are entries of
// the font's /Resources, which [fontIdentity] keys on that font.
func type3ProcSite(ctx context.Context, doc *document.Document, fontRef ast.PdfObject, fontName ast.PdfName, font, t3Res ast.PdfDict, site showSite, procRef ast.PdfObject) showSite {
	procSite := streamSite(ctx, doc, procRef, font["Resources"], t3Res)
	procSite.owner = structureKey(ctx, doc, font)
	procSite.fonts = fontDictKey(fontIdentity(fontRef, fontName, site), font["Resources"], t3Res)
	return procSite
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
		// A Type 3 font shown inside its own glyph procedures has no glyph
		// judged there; see [checkType3CharProcFonts].
		if ref, ok := fontRef.(ast.PdfIndirectRef); ok && judged.type3Open[ref.ObjectNum] {
			continue
		}
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
	id := fontIdentity(fontRef, fontName, site)
	dictRulesDone := j.dict[id]
	j.dict[id] = true
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
		fontResults = j.singleFont(ctx, doc, id, font, string(fontName), pageNum, profile, usage.visible, usage)
	}
	results := j.glyphs(ctx, doc, id, font, usage, profile, fontResults)
	return append(results, j.subsetCoverage(ctx, doc, id, font, string(fontName), pageNum, profile, usage)...)
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
// resources are res, written as rawRes in the stream's own dictionary. The
// stream owns itself for [structureKey].
func streamSite(ctx context.Context, doc *document.Document, ref, rawRes ast.PdfObject, res ast.PdfDict) showSite {
	site := showSite{key: "stream"}
	if r, ok := ref.(ast.PdfIndirectRef); ok {
		site.key = "obj " + strconv.Itoa(r.ObjectNum)
	}
	site.props, _ = document.ResolveDict(ctx, doc, res["Properties"])
	if stream, err := document.ResolveStream(ctx, doc, ref); err == nil {
		site.owner = structureKey(ctx, doc, stream.Dict)
	}
	site.fonts = fontDictKey(site.key, rawRes, res)
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

	data, reads := decodeContentStream(ctx, doc, stream)
	if !reads {
		return nil
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: cannot parse appearance stream", "page", pageNum, "error", err)
		return nil
	}
	site := streamSite(ctx, doc, apObj, stream.Dict["Resources"], res)
	usage := collectFontUsageAt(ops, site)
	if usage == nil {
		return nil
	}

	// A Form XObject the appearance invokes, a tiling pattern it selects and a
	// Type 3 glyph procedure it shows are content streams the walk above never
	// enters, and veraPDF reaches their fonts through the appearance. The
	// colour scan already descends the same way (see
	// scanAnnotationAppearanceCS). checkDrawnStreamFonts carries the Do
	// gating, the depth cap and the visited set, so a shared form is walked
	// once.
	results := checkDrawnStreamFonts(ctx, doc, ops, res, site, treeRoot, pageNum, profile, judged, visited, 0)

	for _, fontName := range sortedNames(fontDict) {
		fontUsage := usage[string(fontName)]
		if fontUsage == nil {
			continue // declared in the appearance resources but never selected
		}
		results = append(results, judged.streamFont(ctx, doc, fontDict[fontName], fontName, site, pageNum, profile, fontUsage)...)
	}
	return results
}

// singleFont runs all font checks against the font dictionary id names but the
// subset rule, which its callers state once per font object through
// [fontJudgements.subsetCoverage]. A composite font's descendant is judged
// through [fontJudgements.descendantRules], once per descendant.
// If isVisible is false, width consistency and embedding checks are skipped
// (the font is not used for visible rendering). Usage provides per-charCode
// usage information for ToUnicode exemption checks.
func (j *fontJudgements) singleFont(ctx context.Context, doc *document.Document, id string, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isVisible bool, usage *fontUsageInfo) []Violation {
	results := checkFontDictRules(ctx, doc, fontDict, fontName, pageNum, profile, isVisible)
	if fontSubtype(ctx, doc, fontDict) == "Type0" {
		results = append(results, j.descendantRules(ctx, doc, id, fontDict, fontName, pageNum, profile, isVisible)...)
		return append(results, checkType0Font(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
	}
	return append(results, checkFontUsageRules(ctx, doc, fontDict, fontName, pageNum, profile, isVisible, usage)...)
}

// descendantRules states the rules veraPDF holds a composite font's descendant
// CIDFont to, as opposed to the composite font itself: its /Type (ISO 19005-1,
// 6.3.2 t1, ISO 19005-2, 6.2.11.2 t1), its embedding (ISO 19005-1, 6.3.4 t1,
// ISO 19005-2, 6.2.11.4.1 t1, whose test exempts a Type0 font and so falls to
// its descendant) and its /CIDToGIDMap (ISO 19005-1, 6.3.3.2 t1, ISO 19005-2,
// 6.2.11.3.2 t1). veraPDF judges the descendant once, so composite fonts that
// share one are judged once between them, and the font id names is judged here
// only when its descendant is new ([descendantIdentity]).
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on a descendant with no
// /Type: one Type0 font over it fails ISO 19005-1, 6.3.2 t1 and ISO 19005-2,
// 6.2.11.2 t1 twice, once for the Type0 font, which takes /Type from it (see
// [checkFontDictRules]), and once for the descendant; two Type0 fonts over it
// fail them three times.
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on a Type0 font whose
// descendant has no /CIDToGIDMap and no embedded program: two Type0 objects
// naming one descendant object, or two pages each writing the Type0 font
// inline over it, fail the embedding and /CIDToGIDMap rules once and the
// /CIDSystemInfo and CMap rules twice; two pages each writing the descendant
// inline too fail every one of them twice.
//
// # Corpus evidence
//
//   - All corpora: at PDF/A-1b and PDF/A-2b, the counts of ISO 19005-1,
//     6.3.3.2 t1 and 6.3.4 t1 and ISO 19005-2, 6.2.11.3.2 t1 and 6.2.11.4.1
//     t1 moved onto veraPDF's for 21 pairs of file and profile and away for
//     none; their summed distance fell from 70 to 0.
//   - pdfjs issue6127.pdf: names 24 Type0 fonts over 8 descendant CIDFonts,
//     and litho failed ISO 19005-1, 6.3.4 t1 24 times, twice more than
//     veraPDF's 22, by judging a shared descendant once per composite font.
//   - pdfjs issue7544.pdf: its descendant with no /Type fails ISO 19005-1,
//     6.3.2 t1 and ISO 19005-2, 6.2.11.2 t1 twice at PDF/A-1b, PDF/A-2b and
//     PDF/A-2u, as veraPDF does, where litho failed them once. No other
//     count of them moved on any corpus.
func (j *fontJudgements) descendantRules(ctx context.Context, doc *document.Document, id string, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isVisible bool) []Violation {
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return nil
	}
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		return nil
	}
	key := descendantIdentity(id, descArr[0])
	if j.descendant[key] {
		return nil
	}
	j.descendant[key] = true

	// The descendant's PDFont reads its /BaseFont again, as the composite
	// font's does; see [fontNameLimits].
	results := fontNameLimits(ctx, doc, cidFontDict, "descendant CIDFont of font "+fontName, profile)
	// ISO 32000-1, 9.7.4, Table 117 requires /Type /Font on a CIDFont, and
	// veraPDF states ISO 19005-2, 6.2.11.2 t1 on the descendant as a PDFont of
	// its own. Only /Type is judged: both CIDFont subtypes are valid, so t2
	// cannot fire, t3 is left out, and t7 reads the descriptor
	// [document.ResolveFontDescriptor] already reaches through the descendant,
	// so the whole check would judge one descriptor twice.
	if rule := fontReqKeyRule(PDFAPart(profile), 1); rule.Specification != "" {
		results = append(results, descendantTypeViolations(ctx, doc, cidFontDict, rule, "descendant CIDFont ")...)
	}

	// The rendering-mode-3 exemption is not scoped to simple fonts: ISO 19005-1
	// §6.3.4 t1, ISO 19005-2/3 §6.2.11.4.1 t1, ISO 19005-4 §6.2.10.4.1 t1, ISO
	// 14289-1 §7.21.4.1 t1 and ISO 14289-2 §8.4.5.5.1 t1 all exempt a font "used
	// exclusively with text rendering mode 3", which is not rendered. veraPDF
	// applies the same escape to the descendant CIDFont (its PDCIDFont is a
	// PDFont), so an unembedded composite font drawn only invisibly must stay
	// silent here too.
	if isVisible {
		results = append(results, checkFontEmbedding(ctx, doc, cidFontDict, fontName, pageNum, profile, false)...)
	}
	// A font with no recorded usage is treated as visible so the check still
	// runs when content-stream usage could not be collected. Every caller
	// supplies usage whenever it can prove non-use, so isVisible already
	// carries that.
	return append(results, checkCIDToGIDMap(ctx, doc, cidFontDict, fontSubtype(ctx, doc, cidFontDict), fontName, pageNum, profile, isVisible)...)
}

// descendantTypeViolations states rule when the CIDFont dictionary cid has no
// /Type /Font, prefixing the context with subject. Absent, null and not a name
// are three defects, and only the last case is "the file names the wrong
// type"; see [checkFontRequiredKeys] for why %q on the empty name printed a
// bare slash.
func descendantTypeViolations(ctx context.Context, doc *document.Document, cid ast.PdfDict, rule Rule, subject string) []Violation {
	typeObj, typePresent := cid["Type"]
	typeName, typeErr := document.ResolveName(ctx, doc, typeObj)
	switch {
	case typeErr != nil:
		return failf(rule, "%s/Type is not a name", subject)
	case !typePresent || typeName == "":
		return failf(rule, "%smissing /Type", subject)
	case typeName != "Font":
		return failf(rule, "%s/Type is %s, not Font", subject, typeName)
	}
	return nil
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
func checkFontDictRules(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isVisible bool) []Violation {
	var results []Violation

	subtype := fontSubtype(ctx, doc, fontDict)

	// Required font-dictionary keys (PDFont): §6.3.2 / §6.2.11.2 / §6.2.10.2.
	required := checkFontRequiredKeys(ctx, doc, fontDict, subtype, PDFAPart(profile))
	results = append(results, required...)
	// The PDFont reads its name again, as a name of its own.
	if pdFontSubtypes[subtype] {
		results = append(results, fontNameLimits(ctx, doc, fontDict, "font "+fontName, profile)...)
	}
	if subtype == "Type0" {
		// veraPDF reads a Type0 font's /Type from its descendant CIDFont
		// (PDType0Font is a PDCIDFont over the descendant's dictionary, and
		// PDFont.getType reads that dictionary), so a descendant with no /Type
		// fails ISO 19005-2, 6.2.11.2 t1 for the Type0 font too, besides once
		// for itself ([fontJudgements.descendantRules]). ISO 32000-1, 9.7.5,
		// Table 121 requires /Type /Font on the Type0 dictionary itself, which
		// litho keeps judging, so the Type0 font is stated once when either
		// dictionary fails.
		rule := fontReqKeyRule(PDFAPart(profile), 1)
		if rule.Specification == "" || slices.ContainsFunc(required, func(v Violation) bool { return v.Rule == rule }) {
			return results
		}
		if descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"]); err == nil && len(descArr) > 0 {
			if cid, err := document.ResolveDict(ctx, doc, descArr[0]); err == nil && cid != nil {
				results = append(results, descendantTypeViolations(ctx, doc, cid, rule, "Type0 font read through its descendant CIDFont: ")...)
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
	return append(results, checkCharSetDeclaredCoverage(ctx, doc, fontDict, fontName, pageNum, profile)...)
}

// checkFontUsageRules runs the checks whose verdict depends on which character
// codes a content stream actually shows. veraPDF builds a glyph per
// text-showing operator, so these hold per page rather than per font object: a
// font object shared by several pages must be judged against each page's own
// usage, or a glyph defect that first appears on a later page is never seen.
// A glyph two pages show is still stated once, by [fontJudgements.glyphs].
// Simple fonts only; [checkType0Font] covers the composite path. The /CharSet
// rule is stated once per font object, by [fontJudgements.subsetCoverage].
//
// The width rule belongs here too. It used to run once per font object,
// because judging it on every page reported widths veraPDF does not; that
// was veraPDF reading the program for the first page alone
// ([fontJudgements.readsProgram]), which also judges a later page that shows
// the font in a new rendering mode.
//
// # Corpus evidence
//
//   - All corpora, at PDF/A-1b, PDF/A-2b and PDF/A-2u, and at their own
//     profile for the verapdf files: judging the width rule here rather than
//     once per font object moves no count.
func checkFontUsageRules(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isVisible bool, usage *fontUsageInfo) []Violation {
	var results []Violation

	// ToUnicode mapping.
	results = append(results, checkToUnicode(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// Glyph presence (§6.3.8 / §6.2.11.4.1 t2 / §6.2.10.4.1 t2): every glyph
	// referenced for visible rendering must exist in the program.
	if isVisible {
		results = append(results, checkSimpleGlyphPresence(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
	}

	// Glyph widths, which veraPDF judges on every page it reads the program
	// for ([fontJudgements.readsProgram]), and on every page for a Type 3
	// font.
	if isVisible {
		results = append(results, checkGlyphWidths(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
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
// res is the page's resources, its own or inherited ones, which name the
// property lists its marked content carries.
func checkPUAWithoutActualText(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, page, res, catalog ast.PdfDict) []Violation {
	if puaActualTextRule(profile).Specification == "" {
		return nil
	}
	treeRoot, _ := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	contentData, _ := resolvePageContentBytes(ctx, doc, page)
	return checkStreamPUAWithoutActualText(ctx, doc, fontDict, fontName, pageNum, profile, treeRoot,
		contentData, page, &markedContentProps{doc: doc, page: page, resources: res})
}

// checkFormPUA runs the PUA check over the fonts a Form XObject's own content
// selects. It does not consult judged: a font already judged where the
// page shows it can still show an uncovered PUA glyph inside the form. res
// is the form's resources and pageRes those of the page it is drawn on, which
// name a property list the form's leave out.
func checkFormPUA(ctx context.Context, doc *document.Document, res, pageRes ast.PdfDict, ops []content.Operation, data []byte, form, treeRoot ast.PdfDict, pageNum int, profile string) []Violation {
	if puaActualTextRule(profile).Specification == "" {
		return nil
	}
	fontDict, _ := document.ResolveDict(ctx, doc, res["Font"])
	usage := collectFontUsage(ops)
	props := &markedContentProps{doc: doc, page: form, resources: res, pageResources: pageRes}
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
			treeRoot, data, form, props)...)
	}
	return results
}

// checkStreamPUAWithoutActualText is the PUA check for the glyphs one content
// stream shows: a page's, or a Form XObject's, since veraPDF builds a Glyph in
// each and a form's glyphs are covered only by marked content inside the form.
// holder is the page or form dictionary whose /StructParents keys the stream's
// MCIDs in the structure parent tree; props reads the property lists its
// marked content carries, inline or named ([markedContentProps]).
//
// It walks the stream and fires only for a shown PUA code whose enclosing
// marked-content sequence, or whose MCID's structure element, carries no
// /ActualText, so an /ActualText covering a different span does not mask an
// uncovered PUA character. A composite font is walked the same way under
// Identity-H or Identity-V, where each two-byte code is the CID the /ToUnicode
// CMap is keyed by; any other encoding needs a CMap this check does not carry,
// so it stays conservative and any /ActualText exempts the font.
//
// A /ToUnicode CMap that builds on another (ISO 32000-1, 9.7.5) takes its
// values from the base as well, merged as [resolveUsedCMaps] merges them for
// the other /ToUnicode rules, so a Private Use value only the base supplies
// counts and one the base replaces does not. A composite font whose encoding
// this check cannot read is then left unjudged rather than judged
// conservatively, since an Adobe UCS2 base maps Private Use values the font may
// never show.
//
// # Corpus evidence
//
//   - litho usecmap_2u.pdf: C2 shows CID 44DF through /Adobe-CNS1-UCS2
//     usecmap, which maps it to U+F538 through bfchar. veraPDF reports it at
//     PDF/A-2a, PDF/A-3a and PDF/UA-2, and litho, reading the program alone,
//     did not.
//   - All corpora: no other file moves at those three profiles. pdfjs
//     issue5010.pdf, the one other file whose /ToUnicode builds on another
//     CMap, names Adobe-Korea1-UCS2, which maps no Private Use value.
func checkStreamPUAWithoutActualText(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, treeRoot ast.PdfDict, contentData []byte, holder ast.PdfDict, props *markedContentProps) []Violation {
	rule := puaActualTextRule(profile)
	if rule.Specification == "" {
		return nil
	}

	// A glyph's Unicode value comes from the /ToUnicode CMap when the CMap maps
	// its code, and otherwise, for a simple font, from its encoding's glyph name
	// (ISO 32000-1, 9.10.2; veraPDF PDSimpleFont.toUnicode), so a PUA value can
	// come from either.
	tuData, based := toUnicodeBasedView(ctx, doc, fontDict)
	tuHasPUA := false
	puaCodes := make(map[uint32]bool)
	switch {
	case based != nil:
		puaCodes = mappingPUACodes(based.StrictMapping())
	case tuData != nil && containsPUAMapping(string(tuData)):
		tuHasPUA = true
		puaCodes = puaSourceCodes(string(tuData))
	}
	if fontSubtype(ctx, doc, fontDict) != "Type0" {
		for code := range glyphNamePUACodes(ctx, doc, fontDict, tuData, based) {
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
			if !tuHasPUA {
				return nil // a base's Private Use values say nothing of the codes shown
			}
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
			actualTextStack = append(actualTextStack, bdcHasStringKey(ctx, doc, op, props, "ActualText") ||
				(altCovers && bdcHasStringKey(ctx, doc, op, props, "Alt")))
			// Only an integer /MCID counts: an absent or null one read as 0
			// let text under no identifier borrow the /ActualText of the
			// element owning MCID 0, where veraPDF reports ISO 14289-2,
			// 8.4.3 t1. Measured.
			mcid := props.mcid(ctx, op)
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

// bdcHasStringKey reports whether a BDC operator's property list, written
// inline or named ([markedContentProps]), carries key with a text-string value,
// the shape veraPDF's getInheritedStringAttribute accepts. A BMC carries no
// property list.
func bdcHasStringKey(ctx context.Context, doc *document.Document, op content.Operation, props *markedContentProps, key ast.PdfName) bool {
	if op.Operator != "BDC" {
		return false
	}
	return dictHasString(ctx, doc, props.dict(ctx, op), key)
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
// risk reporting a glyph the CMap in fact covers. based, when not nil, is the
// CMap tuData holds merged with the CMaps it builds on ([toUnicodeBasedView]),
// whose codes take precedence as well.
func glyphNamePUACodes(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, tuData []byte, based *cmap.CMap) map[uint32]bool {
	var mapped map[int]bool
	switch {
	case based != nil:
		mapped = cmapSourceCodeValues(based)
	case tuData != nil:
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

// toUnicodeBasedView returns the decoded bytes of fontDict's /ToUnicode CMap,
// nil when the font has none or it cannot be read, and, when that CMap builds
// on another through the usecmap operator or a /UseCMap entry (ISO 32000-1,
// 9.7.5), the CMap merged with its bases as [resolveUsedCMaps] merges them.
// based is nil for a CMap that builds on nothing, or that does not parse or
// carries a token its syntax cannot produce ([cmap.CMap.Malformed]), which
// the other /ToUnicode rules read as mapping nothing.
func toUnicodeBasedView(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (tuData []byte, based *cmap.CMap) {
	tuObj, ok := fontDict["ToUnicode"]
	if !ok {
		return nil, nil
	}
	stream, err := document.ResolveStream(ctx, doc, tuObj)
	if err != nil {
		doc.Logger.Debug("compliance: cannot resolve ToUnicode stream", "error", err)
		return nil, nil
	}
	tuData, err = doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance: cannot decode ToUnicode stream", "error", err)
		return nil, nil
	}
	if _, entry := stream.Dict["UseCMap"]; !entry && !bytes.Contains(tuData, []byte("usecmap")) {
		return tuData, nil
	}
	parsed, err := cmap.Parse(ctx, tuData)
	if err != nil || parsed == nil || parsed.Malformed() {
		return tuData, nil
	}
	if _, entry := stream.Dict["UseCMap"]; !entry && parsed.UseCMap() == "" {
		return tuData, nil
	}
	return tuData, resolveUsedCMaps(ctx, doc, tuObj, stream.Dict, parsed)
}

// mappingPUACodes returns the value of every source code of a /ToUnicode
// mapping whose Unicode string holds a Private Use Area code point, veraPDF's
// per-glyph unicodePUA (PUAHelper.containPUA). Codes are read by integer
// value, as [showPaintsPUACode] compares them; a source wider than four bytes
// names no code a font shows.
func mappingPUACodes(mapping map[string]string) map[uint32]bool {
	out := make(map[uint32]bool)
	for src, uni := range mapping {
		if len(src) == 0 || len(src) > 4 {
			continue
		}
		if !strings.ContainsFunc(uni, func(r rune) bool {
			return isPUAScalar(uint32(r)) //nolint:gosec // a decoded rune is non-negative.
		}) {
			continue
		}
		v := uint32(0)
		for i := range len(src) {
			v = v<<8 | uint32(src[i])
		}
		out[v] = true
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
		if m := embeddedEncodingCIDMap(ctx, doc, fontDict["Encoding"]); m != nil {
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
// visited bounds a crafted /K graph, holding each element and each /K array
// written as an object of its own the search has read: direct elements can
// each name their own /K array again. This walk fans out over every kid, so
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
	if depth > maxStructAttrSearchDepth || ctx.Err() != nil {
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
		if document.VisitedBefore(kObj, visited) {
			return false
		}
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

// checkType0Font validates composite font properties including CIDSystemInfo
// and the CMap, then the glyphs the first stream shows ([checkType0UsageRules]).
// The rules veraPDF states on the descendant CIDFont alone, its embedding and
// its /CIDToGIDMap, are stated once per descendant by
// [fontJudgements.descendantRules].
func checkType0Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
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

	// CIDSystemInfo consistency.
	results = append(results, checkCIDSystemInfo(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile)...)

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
// [checkFontUsageRules]. The /CIDSet rule is stated once per descendant
// CIDFont, by [fontJudgements.subsetCoverage]. The presence and width rules
// judge a page's glyphs only where veraPDF reads the program
// ([fontJudgements.readsProgram]).
//
// # Corpus evidence
//
//   - govdocs thread_100_100870.pdf: selects one Times-Roman Identity-H font on
//     fourteen pages, and page 20 shows CID 0 and a CID its CFF charset does
//     not hold in rendering mode 1, which no earlier page used. veraPDF
//     reports both under ISO 19005-2 §6.2.11.8 and the second under
//     §6.2.11.4.1 t2; litho judged the font on page 9 and never looked again.
//   - All corpora but verapdf, at PDF/A-2u: judging a composite font's
//     ToUnicode presence on every page, not the first alone, moved the counts
//     of ISO 19005-2, 6.2.11.7.2 t1 onto veraPDF's in 17 files and away in
//     none; their summed distance fell from 441 to 0.
//   - pdfjs Brotli-Prototype-FileA.pdf: the one other count that moved, from
//     23 to 42, which veraPDF 1.30 cannot check, as it cannot read the file's
//     Brotli-compressed cross-reference stream.
//   - verapdf: at each file's own profile, no count moved.
func checkType0UsageRules(ctx context.Context, doc *document.Document, fontDict, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo, malformed judgeMalformedCodes) []Violation {
	results := make([]Violation, 0, 4)

	// ToUnicode presence, which veraPDF states on every glyph a page shows, the
	// glyphs of later pages included (ISO 19005-2, 6.2.11.7.2 t1). A statement
	// naming no glyph speaks for the font as a whole, and only the font's first
	// judgement, the one that also judges malformed codes, makes it.
	for _, v := range checkType0ToUnicode(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile, usage) {
		if v.glyph.named || bool(malformed) {
			results = append(results, v)
		}
	}

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
					usedCIDs := type0UsedCIDs(ctx, doc, fontDict, usage)
					results = append(results, checkCIDFontWidths(ctx, doc, cidFontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, usedCIDs)...)
					results = append(results, checkType0NotdefWidth(ctx, doc, fontDict, cidFontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, usage)...)
					results = append(results, checkType2MissingGlyphWidths(ctx, doc, cidFontDict, fontData, binaryWidths, unitsPerEm, fontName, pageNum, rule, usedCIDs)...)
					results = append(results, checkCFFMissingGlyphWidths(ctx, doc, cidFontDict, fontData, unitsPerEm, fontName, pageNum, rule, usedCIDs)...)
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

	// Every shown code is its own glyph, stated once; see
	// [fontJudgements.glyphs]. The first reason found for a code stands.
	faults := newGlyphFaults(rule)

	subtype := fontSubtype(ctx, doc, fontDict)

	// A font that embeds no program is judged by the name veraPDF gives each
	// code, its /Differences entries included; see [checkUnembeddedNotdef].
	// Measured with veraPDF 1.30 at PDF/A-2u: a symbolic unembedded TrueType
	// Arial whose /Differences names code 0x41 .notdef, showing codes 0 and
	// 0x41, fails ISO 19005-2, 6.2.11.8 t1 once, for code 0.
	embedsProgram := false
	if fd := document.ResolveFontDescriptor(ctx, doc, fontDict); fd != nil {
		embedsProgram = extractFontProgramData(ctx, doc, fd) != nil
	}
	if (subtype == "Type1" || subtype == "MMType1" || subtype == "TrueType") && !embedsProgram {
		checkUnembeddedNotdef(ctx, doc, fontDict, subtype, fontName, pageNum, faults, usage)
	} else {
		// Check /Encoding /Differences for a used ".notdef" entry.
		checkNotdefInDifferences(ctx, doc, fontDict, fontName, pageNum, faults, usage)
	}

	// For an embedded Type1/CFF: if the encoding explicitly maps a used
	// charCode to ".notdef", it's a violation. For an embedded TrueType, a
	// charCode below 0x20 (control range) with no encoding entry is .notdef
	// too, since the standard encodings define no glyph for control
	// characters.
	if (subtype == "Type1" || subtype == "MMType1" || subtype == "TrueType") && embedsProgram {
		enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
		if len(enc) > 0 {
			// A C0 control code (< 0x20) with no encoding entry renders .notdef
			// because the ISO 32000-1 Annex D standard encodings define no glyph
			// for control characters. DEL (0x7F) is deliberately excluded: some
			// fonts do define a glyph there when they embed a program. A
			// symbolic font resolves codes through its built-in encoding (ISO
			// 32000-1 §9.6.6.2), which litho does not parse, so the implicit case
			// is declined rather than reported as a false positive.
			symbolic := false
			if fd := document.ResolveFontDescriptor(ctx, doc, fontDict); fd != nil {
				symbolic = isSymbolicFont(ctx, doc, fd)
			}
			// An embedded Type 1 font is judged by the name its /Encoding gives
			// the code (ISO 32000-1, 9.6.6.1): .notdef where the predefined base
			// table leaves the code undefined, whatever the symbolic flag says,
			// which is how veraPDF names it. veraPDF's Encoding holds a table for
			// MacRomanEncoding, MacExpertEncoding and WinAnsiEncoding only, so
			// under StandardEncoding, a /Differences-only dictionary or no
			// /Encoding a code the dictionary leaves alone gets no name and no
			// report, while a code WinAnsiEncoding leaves undefined is reported
			// even through a symbolic subset. An embedded program is not judged
			// by the table alone otherwise, because it can still supply a code
			// the encoding omits (ISO 32000-1, 9.6.6.2).
			embeddedType1 := subtype == "Type1" || subtype == "MMType1"
			type1Table := ""
			var type1Differences map[byte]string
			if embeddedType1 {
				type1Table = type1EncodingTable(ctx, doc, fontDict)
				type1Differences = fontDifferencesNames(ctx, doc, fontDict)
			}
			for _, code := range sortedByteKeys(usage.charCodes) {
				glyphName := enc[code]
				if glyphName == ".notdef" {
					faults.add(uint32(code), "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
					continue
				}
				if embeddedType1 {
					if _, named := type1Differences[code]; !named && type1UndefinedCode(type1Table, code) {
						faults.add(uint32(code), "font %s on page %d: charCode %d is undefined in %s, so it maps to .notdef",
							fontName, pageNum, code, type1Table)
					}
					continue
				}
				if glyphName == "" && code < 0x20 && !symbolic {
					faults.add(uint32(code), "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
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
					return faults.violations()
				}
				enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
				// A TrueType simple font resolves a glyph through its /Encoding
				// first (ISO 32000-1, 9.6.6.4): charCode 0 used but absent from an
				// explicit /Encoding renders .notdef regardless of the font's
				// cmap. litho's no-post-table path otherwise trusts the cmap and
				// misses this.
				if len(enc) > 0 && usage.charCodes[0] && enc[0] == "" {
					faults.add(0, "font %s on page %d: charCode 0 maps to .notdef", fontName, pageNum)
				}
				if glyphNames := typography.ExtractGlyphNames(ctx, fontData); glyphNames != nil {
					// veraPDF names a simple font's glyph from its /Encoding
					// (GFGlyph: the encoding's name for the code, the program only
					// when the encoding gives none) and tests name != ".notdef"
					// (ISO 19005-2, 6.2.11.8). A code the encoding names is judged
					// by that name alone, even when the program stores the glyph at
					// GID 0, as a subset with no .notdef glyph can.
					// A font that declares no /Encoding names no code at all, so an
					// empty name here is the absence of a declaration rather than
					// evidence of .notdef: ISO 32000-1, 9.6.6.4 resolves such a
					// TrueType through the program's own cmap, which the no-names
					// branch below consults. Condemning the empty name regardless
					// reported every code a font shows. A declared encoding that
					// leaves a code undefined still fires, which is the
					// MacExpertEncoding case above.
					declaresEncoding := len(enc) > 0
					for _, code := range sortedByteKeys(usage.charCodes) {
						glyphName := enc[code]
						if glyphName == ".notdef" || (glyphName == "" && declaresEncoding) {
							faults.add(uint32(code), "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
						}
					}
				} else {
					// No post table glyph names, so use cmap + loca + /Widths.
					charToGID := typography.ExtractCharCodeToGID(ctx, fontData)
					if charToGID != nil {
						// GID 0 is always .notdef.
						if usage.charCodes[0] && charToGID[0] == 0 {
							faults.add(0, "font %s on page %d: charCode 0 maps to GID 0 (.notdef)", fontName, pageNum)
						}
						// A code renders .notdef only when its cmap resolves to
						// GID 0. An in-range non-zero GID is a real glyph even with
						// no contours and a zero advance (a "space" or control
						// glyph), so an empty outline is not .notdef: ISO 19005-2
						// §6.2.11.8 (veraPDF object Glyph, test name != ".notdef")
						// keys on the glyph being .notdef, not on emptiness.
						for _, code := range sortedByteKeys(usage.charCodes) {
							if gid, inCmap := charToGID[uint16(code)]; inCmap && gid == 0 {
								faults.add(uint32(code), "font %s on page %d: charCode %d maps to GID 0 (.notdef)", fontName, pageNum, code)
							}
						}
					}
				}
			}
		}
	}

	return faults.violations()
}

// collectType0CIDs decodes a Type0 font's shown strings into the set of CIDs
// they reference. It returns (cids, notdef, unmappedZero, ok):
//   - ok is false when litho cannot decode the /Encoding to CIDs (an embedded
//     CMap stream it could not parse, or an unrecognized predefined CMap); the
//     caller skips the check rather than risk a false positive.
//   - notdef is true when decoding reached a character code that unambiguously
//     renders as the .notdef glyph (an Identity odd-length tail, or an embedded
//     CMap mapping a code to a CID beyond 65535).
//   - unmappedZero is true when an embedded CMap leaves a shown code other
//     than 0 unmapped, which selects CID 0.
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
			codes := embedded.Codes([]byte(s))
			for i, c := range embedded.Decode([]byte(s)) {
				if !c.Mapped {
					// A code the CMap leaves unmapped selects CID 0 when litho
					// holds all its mappings (ISO 32000-1, 9.7.6.3); see
					// cmap.CIDMap.UnmappedNotdef. unmappedZero separates that
					// from a code written as CID 0: the first reaches no glyph
					// at all, the second reaches the .notdef glyph, which the
					// program does contain. Code 0 is the exception: veraPDF
					// (GFGlyph.initForNotType3) counts the glyph of code 0
					// present whatever it maps to. Bytes matching no codespace
					// are left out: veraPDF splits them otherwise
					// (cmap.CIDCode.Undefined).
					if embedded.UnmappedNotdef() && !c.Undefined {
						cids[0] = true
						if i < len(codes) && codes[i] != 0 {
							unmappedZero = true
						}
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
		embedded = embeddedEncodingCIDMap(ctx, doc, fontDict["Encoding"])
	}
	cids, _, _, ok := collectType0CIDs(string(encName), embedded, usage)
	if !ok || len(cids) == 0 {
		return nil
	}
	return cids
}

// defaultCIDWidth returns the CIDFont /DW value, or the PDF default of 1000
// when /DW is not a number (ISO 32000-1, 9.7.4.3, Table 117). An explicit 0
// declares a width like any other, which veraPDF reads
// (PDCIDFont.getDefaultWidth), as [lookupCIDWidth] does.
func defaultCIDWidth(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict) float64 {
	r, _ := document.ResolveObject(ctx, doc, cidFontDict["DW"])
	if w, ok := document.Number(r); ok {
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
		embedded = embeddedEncodingCIDMap(ctx, doc, fontDict["Encoding"])
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
	if !widthsInconsistent(declared, expected) {
		return nil
	}
	// A code that does not decode names no CID of its own to be judged once
	// for; a shown CID 0 is that glyph.
	if notdef {
		return failf(rule, "font %s on page %d: .notdef glyph "+WidthMismatchContext+" (declared=%.1f, font=%.1f)",
			fontName, pageNum, declared, expected)
	}
	return glyphFailf(rule, 0, "font %s on page %d: .notdef glyph "+WidthMismatchContext+" (declared=%.1f, font=%.1f)",
		fontName, pageNum, declared, expected)
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
//   - litho glyphs_2b.pdf: shows CID 5 past the end of a TrueType program,
//     once visibly and once in rendering mode 3 through another font, and
//     veraPDF fails ISO 19005-2, 6.2.11.8 t1 for both, where litho failed it
//     for neither.
func checkType0NotdefGlyph(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo, malformed judgeMalformedCodes) []Violation {
	rule := notdefRule(profile)
	if rule.Specification == "" || usage == nil || len(usage.rawStrings) == 0 {
		return nil
	}

	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	var embedded *cmap.CIDMap
	if encName == "" {
		embedded = embeddedEncodingCIDMap(ctx, doc, fontDict["Encoding"])
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

	// Every used CID is its own glyph, judged on its own; see
	// [fontJudgements.glyphs]. A glyph can fail both rules.
	notdefs := newGlyphFaults(rule)
	presence := newGlyphFaults(glyphPresenceRule(profile))
	checksPresence := presence.rule.Specification != "" && usage.visible
	judged := func() []Violation {
		return append(notdefs.violations(), presence.violations()...)
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
		if (unmappedZero || type0MappedCIDZeroAbsent(ctx, doc, fontDict, embedded, usage)) && checksPresence && type0HasEmbeddedProgram(ctx, doc, fontDict) {
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
		// CFF font: check whether the program holds a glyph for each CID, in
		// its charset or, for a name-keyed program, among its charstrings
		// ([cidFontType0Glyphs]). Sort for a deterministic context when
		// several CIDs are absent.
		holds, _, _, ok := cidFontType0Glyphs(fontData)
		if !ok {
			return judged()
		}
		for _, cid := range sortedCIDs(usedCIDs) {
			if cid == 0 || holds(cid) {
				continue
			}
			// A used CID the CFF program holds no glyph for renders .notdef,
			// so both the .notdef rule and the glyph-presence rule apply: the
			// code renders .notdef and no glyph is present for it. Glyph
			// presence asks only about glyphs referenced for rendering, so
			// renderingMode == 3 exempts it, as in the CIDFontType2 branch
			// below; the .notdef rule keeps firing, as veraPDF reports 6.2.11.8
			// and not 6.2.11.4.1 when every show operator is mode 3.
			notdefs.add(uint32(cid), "font %s on page %d: CID %d not in font charset (.notdef)", fontName, pageNum, cid)
			if checksPresence {
				presence.add(uint32(cid), "font %s on page %d: CID %d not present in embedded font program", fontName, pageNum, cid)
			}
		}

	case "CIDFontType2":
		// A TrueType CIDFont renders .notdef when a used CID names no glyph of
		// the program ([missingType2CIDs]), besides CID 0 and the decode-level
		// notdef flag covered above. ISO 19005-2 §6.2.11.8 (veraPDF object
		// Glyph, test name != ".notdef") keys on the glyph being .notdef, and
		// veraPDF names a composite font's glyph .notdef exactly when the
		// program lacks it (GFGlyph: glyphIsPresent ? null : ".notdef"), NOT on
		// an empty outline: an in-range non-zero GID is a real glyph even with
		// no contours and a zero advance (a control-character glyph such as
		// uni0000), so treating "empty outline + width 0" as .notdef was a false
		// positive. A non-zero CID that a /CIDToGIDMap stream remaps to GID 0
		// is left unflagged too, as veraPDF leaves it: GID 0 is in the program.
		//
		// Such a CID has no glyph, so the glyph-presence rule applies as well.
		// renderingMode == 3 exempts it from that rule alone, so a font every
		// one of whose uses was invisible answers only for the .notdef rule.
		// Measured with veraPDF 1.30 at PDF/A-2b: CID 256 of a 38-glyph program
		// fails ISO 19005-2, 6.2.11.4.1 t2 and 6.2.11.8 t1 under an Identity
		// map or none, and CID 1 fails both when a map stream sends it to GID
		// 100 or ends before it; shown only in rendering mode 3, CID 256 fails
		// 6.2.11.8 t1 alone.
		for _, cid := range missingType2CIDs(ctx, doc, cidFontDict, fontData, usedCIDs) {
			notdefs.add(uint32(cid), "font %s on page %d: CID %d names no glyph in the embedded font program (.notdef)",
				fontName, pageNum, cid)
			if checksPresence {
				presence.add(uint32(cid), "font %s on page %d: CID %d not present in embedded font program",
					fontName, pageNum, cid)
			}
		}
	}

	return judged()
}

// missingType2CIDs returns, in ascending order, the used CIDs a TrueType
// CIDFont's embedded program holds no glyph for, as veraPDF reads the program
// (CIDFontType2Program.containsCID): a CID other than 0 has a glyph when the
// /CIDToGIDMap maps it to a glyph index below the numGlyphs of 'maxp'. ISO
// 32000-1, 9.7.4.2, Table 117 makes /Identity the default map, so an absent
// entry maps every CID to itself, and a map stream maps only the CIDs its
// bytes reach ([cidToGIDMapped]).
//
// Such a CID names no glyph at all, which is a different thing from naming an
// empty one: there is no glyf entry to be empty. An unreadable or zero
// numGlyphs, a map stream that does not decode or a map named other than
// /Identity yields nothing rather than a guess.
//
// # Corpus evidence
//
//   - pdfjs Brotli-Prototype-FileA.pdf: shows CID 65535 through a 655-glyph
//     CIDFontType2 with no /CIDToGIDMap, which now fails the glyph-presence,
//     .notdef and width rules at PDF/A-2b and the width rule at PDF/A-1b.
//     veraPDF 1.30 cannot parse the file, whose cross-reference stream is
//     Brotli-compressed, so it gives no count to compare; the counts are
//     those it gives a fixture of the same shape, CID 256 of a 38-glyph
//     program with no /CIDToGIDMap.
func missingType2CIDs(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, fontData []byte, usedCIDs map[uint16]bool) []uint16 {
	numGlyphs := typography.ExtractNumGlyphs(fontData)
	if numGlyphs <= 0 {
		return nil
	}
	mapped, ok := cidToGIDMapped(ctx, doc, cidFontDict)
	if !ok {
		return nil
	}
	var missing []uint16
	for cid := range usedCIDs {
		if cid == 0 {
			continue
		}
		gid, covered := cid, true
		if mapped != nil {
			gid, covered = mapped(cid)
		}
		if !covered || int(gid) >= numGlyphs {
			missing = append(missing, cid)
		}
	}
	slices.Sort(missing)
	return missing
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
// (TrueType) program holds no glyph for it ([missingType2CIDs]); CID 0 is
// always .notdef. Such CIDs have no glyph, so
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
		embedded = embeddedEncodingCIDMap(ctx, doc, fontDict["Encoding"])
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
		if holds, _, _, ok := cidFontType0Glyphs(fontData); ok {
			for cid := range usedCIDs {
				if !holds(cid) {
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
		// [missingType2CIDs] reads the map that way.
		//
		// renderingMode == 3 exempts a glyph outright, and usage.visible is
		// false only when every use of this font was in that mode.
		if usage.visible {
			for _, cid := range missingType2CIDs(ctx, doc, cidFontDict, fontData, usedCIDs) {
				absent[cid] = true
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
		embedded = embeddedEncodingCIDMap(ctx, doc, fontDict["Encoding"])
	}
	if _, _, unmappedZero, decodeOK := collectType0CIDs(string(encName), embedded, usage); decodeOK && (unmappedZero || type0MappedCIDZeroAbsent(ctx, doc, fontDict, embedded, usage)) &&
		usage.visible && type0HasEmbeddedProgram(ctx, doc, fontDict) {
		faults.add(0, "font %s on page %d: the encoding CMap maps a rendered code to no glyph in the embedded font program", fontName, pageNum)
	}
	return faults.violations()
}

// collectVisibleFontObjects returns the font objects ([fontIdentity]) that at
// least one page renders visibly. The embedding rule is reported once per font
// object, so it cannot ask a single page whether the font is visible: a font
// drawn in rendering mode 3 on the page that happens to declare it first, and
// normally on a later page, is embedded-or-not regardless of which page is
// asked. A page whose content stream an earlier page shares renders nothing
// through it; see [markJudgedContents].
func collectVisibleFontObjects(ctx context.Context, doc *document.Document, vctx *validationCtx) map[string]bool {
	visible := make(map[string]bool)
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
		site := showSite{fonts: pageFontsKey(p, pageIdx+1)}
		pageUsage := collectFontUsage(p.parsedOps(ctx))
		for name, ref := range fontDict {
			if u := pageUsage[string(name)]; p.parseFailed || (u != nil && u.visible) {
				visible[fontIdentity(ref, name, site)] = true
			}
		}
	}
	return visible
}
