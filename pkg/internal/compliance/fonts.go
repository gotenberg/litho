package compliance

import (
	"bytes"
	"context"
	"math"
	"slices"

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
}

// collectFontUsage returns usage information for each font referenced in the
// page content stream: whether it's used for visible rendering and which
// character codes are referenced. Returns nil if ops is nil or empty (in which
// case all fonts should be checked).
func collectFontUsage(ops []content.Operation) map[string]*fontUsageInfo {
	if len(ops) == 0 {
		return nil
	}

	usage := make(map[string]*fontUsageInfo)
	currentFont := ""
	renderMode := 0 // default rendering mode is 0 (fill)

	getOrCreate := func(name string) *fontUsageInfo {
		if u, ok := usage[name]; ok {
			return u
		}
		u := &fontUsageInfo{charCodes: make(map[byte]bool)}
		usage[name] = u
		return u
	}

	collectStringBytes := func(obj ast.PdfObject) {
		if currentFont == "" {
			return
		}
		u := getOrCreate(currentFont)
		switch s := obj.(type) {
		case ast.PdfString:
			for _, b := range []byte(s) {
				u.charCodes[b] = true
			}
			u.rawStrings = append(u.rawStrings, s)
		case ast.PdfArray:
			for _, item := range s {
				if str, ok := item.(ast.PdfString); ok {
					for _, b := range []byte(str) {
						u.charCodes[b] = true
					}
					u.rawStrings = append(u.rawStrings, str)
				}
			}
		}
	}

	for _, op := range ops {
		switch op.Operator {
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
	// the page-resource name; checkedObjs keys on the resolved object number so
	// the annotation-appearance pass does not re-check a font also declared in a
	// page's /Resources/Font.
	checked := make(map[string]bool)
	checkedObjs := make(map[int]bool)

	for pageIdx := range vctx.pages {
		p := &vctx.pages[pageIdx]
		if ctx.Err() != nil {
			return results
		}

		results = append(results, checkPageDeclaredFonts(ctx, doc, catalog, p, pageIdx+1, profile, checked, checkedObjs)...)

		// Fonts referenced only from annotation appearance streams are never
		// reached by the page /Resources/Font walk above. veraPDF validates them
		// through the appearance content stream (for example
		// annots[0]/appearance[0]/contentStream[0]/operators[N]/font[0]); litho
		// matches by checking their subset glyph coverage here.
		results = append(results, checkAnnotationAppearanceFonts(ctx, doc, p.dict, pageIdx+1, profile, checkedObjs)...)

		// Fonts used only inside a Form XObject invoked from the page content
		// (a Do operator) are likewise not reached by the page /Resources/Font
		// walk. veraPDF descends into the form's content stream and applies the
		// same font rules there; litho matches by walking invoked forms.
		xobjVisited := make(map[int]bool)
		results = append(results, checkFormXObjectFonts(ctx, doc, p.parsedOps(ctx), p.resources, pageIdx+1, profile, checkedObjs, xobjVisited, 0)...)
	}

	doc.Logger.Debug("compliance: fonts: font checks complete",
		"violations", len(results), "fonts_checked", len(checked))
	return results
}

// checkPageDeclaredFonts runs the full font-check suite over the fonts declared
// in a page's /Resources/Font. It records each resolved font object number in
// checkedObjs so [checkAnnotationAppearanceFonts] does not re-check a font that
// is also declared on the page.
func checkPageDeclaredFonts(ctx context.Context, doc *document.Document, catalog ast.PdfDict, p *pageCtx, pageNum int, profile string, checked map[string]bool, checkedObjs map[int]bool) []Violation {
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
	// visible (rendering mode != 3) and which character codes are used.
	fontUsage := collectFontUsage(p.parsedOps(ctx))
	// The conservative "check every declared font" fallback applies only
	// when content is PRESENT but unparseable (parseFailed). Embedding and
	// width rules (§6.2.11.4.1) constrain glyphs actually rendered, so a
	// font declared in /Font but never selected and shown renders no glyph
	// and is out of scope. Empty or fully-parsed content with no glyphs
	// rendered therefore exempts a declared-but-unused font. Note: empty
	// page content is one '\n' separator after stream concatenation, so a
	// length test cannot distinguish it; parseFailed keys off the parser
	// error.
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
		if isIndirect {
			if checkedObjs[ref.ObjectNum] {
				continue
			}
		} else if checked[fontKey] {
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
			checkedObjs[ref.ObjectNum] = true
		}
		// Skip width/embedding checks for fonts not used for visible rendering.
		var usage *fontUsageInfo
		if fontUsage != nil {
			usage = fontUsage[fontKey]
		}
		isVisible := parseFailed || (usage != nil && usage.visible)

		results = append(results, checkSingleFont(ctx, doc, font, fontKey, pageNum, profile, isVisible, usage)...)

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
// Recursion into nested forms is bounded by [maxFormXObjectDepth]; visited
// guards resource cycles; checkedObjs deduplicates against fonts already checked
// on the page and across forms.
func checkFormXObjectFonts(ctx context.Context, doc *document.Document, ops []content.Operation, res ast.PdfDict, pageNum int, profile string, checkedObjs, visited map[int]bool, depth int) []Violation {
	if depth > maxFormXObjectDepth || ctx.Err() != nil {
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
		results = append(results, checkResourceFonts(ctx, doc, xobjRes, subOps, pageNum, profile, checkedObjs)...)
		results = append(results, checkFormXObjectFonts(ctx, doc, subOps, xobjRes, pageNum, profile, checkedObjs, visited, depth+1)...)
	}
	return results
}

// checkResourceFonts runs the full single-font check suite over the fonts a
// content stream selects for rendering, resolved from res /Resources/Font. Only
// fonts actually used (selected with Tf and shown) are checked; a font declared
// in the resources but never selected renders no glyph and is out of scope.
func checkResourceFonts(ctx context.Context, doc *document.Document, res ast.PdfDict, ops []content.Operation, pageNum int, profile string, checkedObjs map[int]bool) []Violation {
	fontDict, _ := document.ResolveDict(ctx, doc, res["Font"])
	if len(fontDict) == 0 {
		return nil
	}
	usage := collectFontUsage(ops)
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
		if ref, ok := fontRef.(ast.PdfIndirectRef); ok {
			if checkedObjs[ref.ObjectNum] {
				continue
			}
			checkedObjs[ref.ObjectNum] = true
		}
		font, err := document.ResolveDict(ctx, doc, fontRef)
		if err != nil || font == nil {
			continue
		}
		results = append(results, checkSingleFont(ctx, doc, font, string(fontName), pageNum, profile, fontUsage.visible, fontUsage)...)
	}
	return results
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
// appearance stream actually renders. checkedObjs deduplicates against fonts
// already checked on the page and across appearance streams.
func checkAnnotationAppearanceFonts(ctx context.Context, doc *document.Document, page ast.PdfDict, pageNum int, profile string, checkedObjs map[int]bool) []Violation {
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
			results = append(results, checkAppearanceStreamFonts(ctx, doc, apObj, pageNum, profile, checkedObjs, visited, 0)...)
		}
	}
	return results
}

// checkAppearanceStreamFonts resolves one appearance entry (a stream, or a state
// subdictionary of streams) and checks the subset glyph coverage of every font
// the stream actually uses for rendering. visited guards against parsing the
// same appearance stream twice.
func checkAppearanceStreamFonts(ctx context.Context, doc *document.Document, apObj ast.PdfObject, pageNum int, profile string, checkedObjs, visited map[int]bool, stateCount int) []Violation {
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
			results = append(results, checkAppearanceStreamFonts(ctx, doc, subDict[stateName], pageNum, profile, checkedObjs, visited, stateCount)...)
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
	fontDict, _ := document.ResolveDict(ctx, doc, res["Font"])
	if len(fontDict) == 0 {
		return nil
	}

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
	usage := collectFontUsage(ops)
	if usage == nil {
		return nil
	}

	var results []Violation
	for _, fontName := range sortedNames(fontDict) {
		fontUsage := usage[string(fontName)]
		if fontUsage == nil {
			continue // declared in the appearance resources but never selected
		}
		fontRef := fontDict[fontName]
		if ref, ok := fontRef.(ast.PdfIndirectRef); ok {
			if checkedObjs[ref.ObjectNum] {
				continue
			}
			checkedObjs[ref.ObjectNum] = true
		}
		font, err := document.ResolveDict(ctx, doc, fontRef)
		if err != nil || font == nil {
			continue
		}
		results = append(results, checkSingleFont(ctx, doc, font, string(fontName), pageNum, profile, fontUsage.visible, fontUsage)...)
	}
	return results
}

// checkSingleFont runs all font checks against a single font dictionary.
// If isVisible is false, width consistency and embedding checks are skipped
// (the font is not used for visible rendering). Usage provides per-charCode
// usage information for ToUnicode exemption checks.
func checkSingleFont(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isVisible bool, usage *fontUsageInfo) []Violation {
	var results []Violation

	subtype := fontSubtype(ctx, doc, fontDict)

	// Required font-dictionary keys (PDFont): §6.3.2 / §6.2.11.2 / §6.2.10.2.
	results = append(results, checkFontRequiredKeys(ctx, doc, fontDict, subtype, PDFAPart(profile))...)

	// Type 3 fonts have no font program, so skip embedding checks.
	isType3 := isType3Font(ctx, doc, fontDict)

	// Type 0 (composite) fonts: check descendant CIDFonts.
	if subtype == "Type0" {
		results = append(results, checkType0Font(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
		return results
	}

	// Required keys for simple fonts: /FirstChar, /LastChar, /Widths.
	results = append(results, checkSimpleFontRequiredKeys(ctx, doc, fontDict, PDFAPart(profile))...)

	// Font embedding: only for fonts used for visible rendering.
	if isVisible {
		results = append(results, checkFontEmbedding(ctx, doc, fontDict, fontName, pageNum, profile, isType3)...)
	}

	// ToUnicode mapping.
	results = append(results, checkToUnicode(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// TrueType encoding.
	if subtype == "TrueType" {
		results = append(results, checkTrueTypeEncoding(ctx, doc, fontDict, fontName, pageNum, profile)...)
	}

	// Font name UTF-8.
	results = append(results, checkFontNameUTF8(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// Glyph width consistency: only for fonts used for visible rendering.
	if isVisible {
		results = append(results, checkGlyphWidths(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
		// Glyph presence (§6.3.8 / §6.2.11.4.1 t2 / §6.2.10.4.1 t2): every
		// glyph referenced for visible rendering must exist in the program.
		results = append(results, checkSimpleGlyphPresence(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
	}

	// .notdef glyph.
	results = append(results, checkNotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// Glyph coverage (CharSet for Type1 subsets).
	results = append(results, checkGlyphCoverage(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// ToUnicode CMap values.
	results = append(results, checkToUnicodeCMapValues(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	return results
}

// checkPUAWithoutActualText flags a font whose ToUnicode maps a rendered
// character to the Unicode Private Use Area when no /ActualText covers that
// character (ISO 19005-2/3 §6.2.11.7.3, ISO 14289-2 §8.4.3, mirroring veraPDF's
// per-glyph actualTextPresent). For a simple (one-byte) font it walks the page
// content stream and fires only for a PUA code whose enclosing marked-content
// sequence carries no /ActualText, so an /ActualText covering a different span
// does not mask an uncovered PUA character. For a composite (Type 0) font, whose
// multi-byte code decoding needs the font CMap, it stays conservative and
// exempts the font when any marked-content /ActualText is present.
func checkPUAWithoutActualText(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, page ast.PdfDict, catalog ast.PdfDict) []Violation {
	rule := puaActualTextRule(profile)
	if rule.Specification == "" {
		return nil
	}

	tuObj, ok := fontDict["ToUnicode"]
	if !ok {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, tuObj)
	if err != nil {
		doc.Logger.Debug("compliance: cannot resolve ToUnicode stream", "error", err)
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil || !containsPUAMapping(string(data)) {
		return nil
	}

	treeRoot, _ := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	contentData, _ := resolvePageContentBytes(ctx, doc, page)
	hasContentActualText := len(contentData) > 0 && bytes.Contains(contentData, []byte("/ActualText"))

	// Composite fonts (and any font whose PUA codes litho cannot enumerate) use
	// multi-byte codes that need the font CMap to decode, so litho stays
	// conservative: any /ActualText anywhere (structure tree or content stream)
	// exempts the font. Over-exemption is an under-report, never a false positive.
	puaCodes := puaSourceCodes(string(data))
	conservative := func() []Violation {
		if (treeRoot != nil && structTreeHasActualText(ctx, doc, treeRoot, 0)) || hasContentActualText {
			return nil
		}
		return failf(rule, "font %s on page %d: ToUnicode maps to PUA without ActualText", fontName, pageNum)
	}
	if fontSubtype(ctx, doc, fontDict) == "Type0" || len(puaCodes) == 0 {
		return conservative()
	}
	ops, perr := content.ParseStreamData(ctx, contentData)
	if perr != nil {
		return conservative()
	}

	// A PUA character is covered when an enclosing marked-content sequence carries
	// /ActualText, or when the structure element of its /MCID (or an ancestor)
	// does. mcidCovered maps each MCID on the page to the latter.
	mcidCovered := mcidActualTextCoverage(ctx, doc, treeRoot, page)

	var props ast.PdfDict // page /Properties, resolved lazily for named BDC lists
	propsResolved := false
	resolveProps := func() ast.PdfDict {
		if !propsResolved {
			if res := resolveResources(ctx, doc, page); res != nil {
				props, _ = document.ResolveDict(ctx, doc, res["Properties"])
			}
			propsResolved = true
		}
		return props
	}

	var actualTextStack []bool // per open BDC/BMC: does it carry /ActualText?
	var mcidStack []int        // per open BDC/BMC: the effective /MCID (-1 if none)
	currentFont := ""
	currentMCID := -1
	for _, op := range ops {
		switch op.Operator {
		case "BDC", "BMC":
			actualTextStack = append(actualTextStack, bdcHasActualText(ctx, doc, op, resolveProps))
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
			if showPaintsPUACode(op, puaCodes) {
				return failf(rule, "font %s on page %d: rendered character mapped to PUA has no ActualText", fontName, pageNum)
			}
		}
	}
	return nil
}

// mcidActualTextCoverage maps each marked-content identifier on a page to whether
// the structure element it belongs to (resolved through the page /StructParents
// entry in the ParentTree) or one of its ancestors carries an /ActualText entry.
func mcidActualTextCoverage(ctx context.Context, doc *document.Document, treeRoot, page ast.PdfDict) map[int]bool {
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
	arr, err := document.ResolveArray(ctx, doc, numberTreeLookupObj(ctx, doc, ptDict, int(key), 0))
	if err != nil {
		return out
	}
	for mcid, elemRef := range arr {
		elem, derr := document.ResolveDict(ctx, doc, elemRef)
		if derr != nil || elem == nil {
			continue
		}
		if elemOrAncestorHasActualText(ctx, doc, elem, 0) {
			out[mcid] = true
		}
	}
	return out
}

// elemOrAncestorHasActualText reports whether a structure element or one of its
// /P ancestors carries an /ActualText entry.
func elemOrAncestorHasActualText(ctx context.Context, doc *document.Document, elem ast.PdfDict, depth int) bool {
	if elem == nil || depth > maxStructTreeWalkDepth {
		return false
	}
	if _, ok := elem["ActualText"]; ok {
		return true
	}
	parent, err := document.ResolveDict(ctx, doc, elem["P"])
	if err != nil {
		return false
	}
	return elemOrAncestorHasActualText(ctx, doc, parent, depth+1)
}

// bdcMCID returns the /MCID value of a BDC operator's property list (inline or
// named via the page /Properties), or -1 when absent.
func bdcMCID(ctx context.Context, doc *document.Document, op content.Operation, props func() ast.PdfDict) int {
	if op.Operator != "BDC" || len(op.Operands) < 2 {
		return -1
	}
	var pd ast.PdfDict
	switch v := op.Operands[1].(type) {
	case ast.PdfDict:
		pd = v
	case ast.PdfName:
		if p := props(); p != nil {
			pd, _ = document.ResolveDict(ctx, doc, p[v])
		}
	}
	if pd == nil {
		return -1
	}
	mcid, err := document.ResolveInt(ctx, doc, pd["MCID"])
	if err != nil {
		return -1
	}
	return int(mcid)
}

// bdcHasActualText reports whether a BDC/BMC operator's property list (inline
// dictionary or a name resolved through the page /Properties) carries an
// /ActualText entry.
func bdcHasActualText(ctx context.Context, doc *document.Document, op content.Operation, props func() ast.PdfDict) bool {
	if op.Operator != "BDC" || len(op.Operands) < 2 {
		return false
	}
	var pd ast.PdfDict
	switch v := op.Operands[1].(type) {
	case ast.PdfDict:
		pd = v
	case ast.PdfName:
		if p := props(); p != nil {
			pd, _ = document.ResolveDict(ctx, doc, p[v])
		}
	}
	if pd == nil {
		return false
	}
	_, ok := pd["ActualText"]
	return ok
}

// showPaintsPUACode reports whether a text-showing operator paints any one-byte
// code present in puaCodes.
func showPaintsPUACode(op content.Operation, puaCodes map[uint32]bool) bool {
	check := func(s ast.PdfString) bool {
		for _, b := range []byte(s) {
			if puaCodes[uint32(b)] {
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
			kidDict, _ := document.ResolveDict(ctx, doc, kid)
			if kidDict != nil && structTreeHasActualText(ctx, doc, kidDict, depth+1) {
				return true
			}
		}
	} else if kidDict, _ := document.ResolveDict(ctx, doc, kObj); kidDict != nil {
		return structTreeHasActualText(ctx, doc, kidDict, depth+1)
	}
	return false
}

// checkType0Font validates composite font properties including descendant
// CIDFonts, CIDSystemInfo, CIDToGIDMap, and ToUnicode.
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

	cidSubtype := fontSubtype(ctx, doc, cidFontDict)

	// Font embedding for the CIDFont.
	results = append(results, checkFontEmbedding(ctx, doc, cidFontDict, fontName, pageNum, profile, false)...)

	// Font name UTF-8 for the Type0 font.
	results = append(results, checkFontNameUTF8(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// CIDToGIDMap. A font with no recorded usage is treated as visible so the
	// check still runs when content-stream usage could not be collected.
	visible := usage == nil || usage.visible
	results = append(results, checkCIDToGIDMap(ctx, doc, cidFontDict, cidSubtype, fontName, pageNum, profile, visible)...)

	// CIDSystemInfo consistency.
	results = append(results, checkCIDSystemInfo(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile)...)

	// ToUnicode for composite fonts.
	results = append(results, checkType0ToUnicode(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile)...)

	// Per-glyph ToUnicode coverage: a present map must cover every code
	// rendered through the font (§6.2.11.7.2 t2 / UA 7.21.7 / 8.4.5.8).
	results = append(results, checkType0PerGlyphToUnicode(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// CMap embedding and WMode.
	results = append(results, checkCMapEmbedding(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// CID value limit (t10: max CID 65535).
	results = append(results, checkCMapCIDValues(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// .notdef glyph.
	results = append(results, checkNotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, nil)...)
	results = append(results, checkType0NotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
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
				if unitsPerEm == 0 {
					unitsPerEm = 1000
				}
				rule := glyphWidthRule(profile)
				if rule.Specification != "" {
					results = append(results, checkCIDFontWidths(ctx, doc, cidFontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule)...)
					results = append(results, checkType0NotdefWidth(ctx, doc, fontDict, cidFontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, usage)...)
				}
			}
		}
	}

	// CIDFont glyph coverage (CIDSet for CIDFont subsets). Completeness
	// is scoped to USED CIDs: a CIDSet that omits a CID never rendered does
	// not break the embedded subset for any glyph the page actually draws, so
	// only used CIDs are required to be present.
	results = append(results, checkCIDFontGlyphCoverage(ctx, doc, cidFontDict, fontName, pageNum, profile, usage != nil && len(usage.charCodes) > 0, type0UsedCIDs(ctx, doc, fontDict, usage))...)

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
	if subtype == "Type1" || subtype == "MMType1" {
		enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
		if len(enc) > 0 {
			// A symbolic Type1 font resolves a code absent from /Differences
			// through the font program's built-in encoding (ISO 32000-1
			// §9.6.6.2), which litho does not parse. Such a control-range code
			// is therefore not necessarily .notdef, so litho declines to report
			// the implicit case rather than emit a false positive (mirroring the
			// symbolic-TrueType handling below). An explicit ".notdef" in
			// /Differences is still a real reference and fires regardless.
			symbolic := false
			if fd := document.ResolveFontDescriptor(ctx, doc, fontDict); fd != nil {
				symbolic = isSymbolicFont(ctx, doc, fd)
			}
			for code := range usage.charCodes {
				glyphName := enc[code]
				if glyphName == ".notdef" {
					return failf(rule, "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
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
				// first (PDF 9.6.6.4): charCode 0 used but absent from an
				// explicit /Encoding renders .notdef regardless of the font's
				// cmap. litho's no-post-table path otherwise trusts the cmap and
				// misses this.
				if len(enc) > 0 && usage.charCodes[0] && enc[0] == "" {
					return failf(rule, "font %s on page %d: charCode 0 maps to .notdef", fontName, pageNum)
				}
				glyphNames := typography.ExtractGlyphNames(ctx, fontData)
				if glyphNames != nil {
					// Build glyphName→GID reverse map.
					nameToGID := make(map[string]uint16, len(glyphNames))
					for gid, gn := range glyphNames {
						if _, ok := nameToGID[gn]; !ok {
							nameToGID[gn] = gid
						}
					}
					for code := range usage.charCodes {
						glyphName := enc[code]
						if glyphName == "" || glyphName == ".notdef" {
							return failf(rule, "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
						}
						if gid, ok := nameToGID[glyphName]; ok && gid == 0 {
							return failf(rule, "font %s on page %d: charCode %d maps to GID 0 (.notdef)", fontName, pageNum, code)
						}
					}
				} else {
					// No post table glyph names, so use cmap + loca + /Widths.
					charToGID := typography.ExtractCharCodeToGID(ctx, fontData)
					if charToGID != nil {
						// GID 0 is always .notdef.
						if usage.charCodes[0] && charToGID[0] == 0 {
							return failf(rule, "font %s on page %d: charCode 0 maps to GID 0 (.notdef)", fontName, pageNum)
						}
						// For non-zero GIDs: empty glyph data + PDF width=0 → .notdef.
						emptyGIDs := typography.ExtractEmptyGIDs(ctx, fontData)
						if emptyGIDs != nil {
							firstChar, widths := simpleFontWidths(ctx, doc, fontDict)
							for code := range usage.charCodes {
								gid, inCmap := charToGID[uint16(code)]
								if !inCmap {
									continue // No cmap entry, can't determine GID.
								}
								if gid == 0 {
									return failf(rule, "font %s on page %d: charCode %d maps to GID 0 (.notdef)", fontName, pageNum, code)
								}
								if emptyGIDs[gid] && simpleFontWidthIsZero(firstChar, widths, code) {
									return failf(rule, "font %s on page %d: charCode %d maps to empty glyph with width 0 (.notdef)", fontName, pageNum, code)
								}
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
// they reference. It returns (cids, notdef, ok):
//   - ok is false when litho cannot decode the /Encoding to CIDs (an embedded
//     CMap stream, or an unrecognized predefined CMap); the caller skips the
//     check rather than risk a false positive.
//   - notdef is true when decoding reached a character code that unambiguously
//     renders as the .notdef glyph (an Identity odd-length tail).
//
// Identity-H/V treats each 2-byte unit as the CID directly. A predefined CMap
// (Adobe-Japan1 etc.) is decoded codespace-aware through its bundled bcmap; the
// resolvable CIDs are returned for the caller's embedded-font charset check,
// while unmapped codes are skipped to stay free of false positives.
func collectType0CIDs(encName string, embedded *cmap.CIDMap, usage *fontUsageInfo) (cids map[uint16]bool, notdef, ok bool) {
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
					continue
				}
				// A CMap mapping a used code to a CID beyond 65535
				// references a glyph no font can contain (ISO 32000-2
				// limits CIDs to 0..65535): the renderer draws .notdef,
				// which is exactly what the caller's notdef path reports.
				if c.CID < 0 || c.CID > 0xFFFF {
					return nil, true, true
				}
				cids[uint16(c.CID)] = true
			}
		}
		return cids, false, true
	}
	switch encName {
	case "Identity-H", "Identity-V":
		cids = make(map[uint16]bool)
		for _, s := range usage.rawStrings {
			data := []byte(s)
			// An odd-length string under a 2-byte CMap leaves a trailing byte
			// that cannot form a CID; the renderer draws .notdef.
			if len(data)%2 != 0 {
				return nil, true, true
			}
			for i := 0; i+1 < len(data); i += 2 {
				cids[uint16(data[i])<<8|uint16(data[i+1])] = true
			}
		}
		return cids, false, true
	default:
		// Any other named encoding is a predefined CMap, decoded through its
		// bundled bcmap. A nil map means the name is not a recognized
		// predefined CMap (or /Encoding is an embedded stream, which resolves
		// to an empty name here), so the check is skipped.
		//
		// An unmapped code is NOT treated as .notdef here: litho does not
		// resolve a bcmap's usecmap base, so a code could be unmapped merely
		// because its mapping lives in an unresolved base CMap. Flagging it
		// would be a false positive. Only the CIDs we can resolve are collected
		// and verified against the embedded font program by the caller, which
		// is independent of CMap completeness. This trades a possibly missed
		// .notdef (a false negative) for zero false positives.
		cm := cmap.LoadPredefinedCIDMap(encName)
		if cm == nil {
			return nil, false, false
		}
		cids = make(map[uint16]bool)
		for _, s := range usage.rawStrings {
			for _, c := range cm.Decode([]byte(s)) {
				if c.Mapped && c.CID >= 0 && c.CID <= 0xFFFF {
					cids[uint16(c.CID)] = true
				}
			}
		}
		return cids, false, true
	}
}

// checkType0NotdefGlyph checks if a Type0 (composite) font references CID 0
// (.notdef) in any text operator string, or references a CID that is absent
// from the embedded font program's charset (also renders as .notdef). It
// handles Identity-H/V and predefined CMap encodings (see [collectType0CIDs]);
// embedded CMap streams are not decoded and are left unchecked.
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
	cids, _, ok := collectType0CIDs(string(encName), embedded, usage)
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
func checkType0NotdefWidth(ctx context.Context, doc *document.Document, fontDict, cidFontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string, pageNum int, rule Rule, usage *fontUsageInfo) []Violation {
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
	usedCIDs, notdef, ok := collectType0CIDs(string(encName), embedded, usage)
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
	if math.Abs(declared-expected) > widthTolerance {
		return failf(rule, "font %s on page %d: .notdef glyph width mismatch (declared=%.1f, font=%.1f)",
			fontName, pageNum, declared, expected)
	}
	return nil
}

func checkType0NotdefGlyph(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
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
	usedCIDs, notdef, ok := collectType0CIDs(string(encName), embedded, usage)
	if !ok {
		// litho cannot decode this /Encoding to CIDs (an unparseable
		// embedded CMap stream or an unrecognized predefined CMap); leave
		// it unchecked rather than risk a false positive.
		return nil
	}
	if notdef {
		// A decode-level .notdef (odd-length tail or a CID beyond 65535)
		// also means no real glyph backs the rendered code, so both the
		// .notdef rule and the glyph-presence rule apply: the rendered code
		// resolves to .notdef and to no glyph in the embedded program.
		results := failf(rule, "font %s on page %d: text renders .notdef (odd-length code tail or CID beyond 65535)", fontName, pageNum)
		if pr := glyphPresenceRule(profile); pr.Specification != "" {
			results = append(results, failf(pr,
				"font %s on page %d: rendered code resolves to no glyph in the embedded font program", fontName, pageNum)...)
		}
		return results
	}

	// CID 0 is always .notdef.
	if usedCIDs[0] {
		return failf(rule, "font %s on page %d: text references CID 0 (.notdef)", fontName, pageNum)
	}

	// Resolve descendant CIDFont to verify referenced CIDs exist in the font program.
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return nil
	}
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		return nil
	}

	fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return nil
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil
	}

	cidSubtype := fontSubtype(ctx, doc, cidFontDict)

	switch cidSubtype {
	case "CIDFontType0":
		// CFF font: check if CIDs exist in the charset. Sort for a
		// deterministic context when several CIDs are absent.
		validCIDs := typography.ExtractValidCIDs(fontData)
		if validCIDs == nil {
			return nil
		}
		cids := make([]uint16, 0, len(usedCIDs))
		for cid := range usedCIDs {
			cids = append(cids, cid)
		}
		slices.Sort(cids)
		for _, cid := range cids {
			if !validCIDs[cid] {
				// A used CID absent from the CFF charset has no glyph in
				// the program, so both the .notdef rule and the
				// glyph-presence rule apply: the code renders .notdef and
				// no glyph is present for it.
				results := failf(rule, "font %s on page %d: CID %d not in font charset (.notdef)", fontName, pageNum, cid)
				if pr := glyphPresenceRule(profile); pr.Specification != "" {
					results = append(results, failf(pr,
						"font %s on page %d: CID %d not present in embedded font program", fontName, pageNum, cid)...)
				}
				return results
			}
		}

	case "CIDFontType2":
		// TrueType CIDFont: with CIDToGIDMap=/Identity, CID == GID.
		cidToGIDMap, _ := document.ResolveName(ctx, doc, cidFontDict["CIDToGIDMap"])
		if cidToGIDMap != "Identity" {
			return nil
		}
		// Empty glyph data + PDF width=0 → .notdef.
		// Empty glyph data + PDF width>0 → valid invisible glyph (space, etc.).
		emptyGIDs := typography.ExtractEmptyGIDs(ctx, fontData)
		if emptyGIDs != nil {
			for cid := range usedCIDs {
				if emptyGIDs[cid] && lookupCIDWidth(ctx, doc, cidFontDict, cid) == 0 {
					return failf(rule, "font %s on page %d: CID %d maps to empty glyph with width 0 (.notdef)", fontName, pageNum, cid)
				}
			}
		}
	}

	return nil
}

// absentType0CIDs returns the set of used CIDs whose glyph is absent from the
// composite font's embedded program, plus the descendant CIDFont subtype. A CID
// is absent when a CIDFontType0 (CFF) charset omits it, or a CIDFontType2
// (TrueType, Identity CIDToGIDMap) maps it to an empty glyf entry with a zero
// advance; CID 0 is always .notdef. Such CIDs render .notdef and have no glyph,
// so they fire both the glyph-presence and the .notdef rule. The subtype lets
// callers apply the CFF-specific per-glyph ToUnicode exemption: a CFF charset
// that omits a CID has no glyph for it at all, so there is no glyph to map and
// the per-glyph ToUnicode rule does not apply. A TrueType empty glyph still
// exists in the glyf table (it is present, just empty), so it remains a mappable
// glyph and the ToUnicode rule still applies. ok is false when litho cannot
// decode the encoding to CIDs; callers then stay conservative.
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
	usedCIDs, notdef, decodeOK := collectType0CIDs(string(encName), embedded, usage)
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
		if cidToGIDMap, _ := document.ResolveName(ctx, doc, cidFontDict["CIDToGIDMap"]); cidToGIDMap == "Identity" {
			if emptyGIDs := typography.ExtractEmptyGIDs(ctx, fontData); emptyGIDs != nil {
				for cid := range usedCIDs {
					if emptyGIDs[cid] && lookupCIDWidth(ctx, doc, cidFontDict, cid) == 0 {
						absent[cid] = true
					}
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
	// this path, so honor the decode signal here for parity.
	if !notdef && len(absent) == 0 {
		return nil
	}
	return failf(rule, "font %s on page %d: rendered code resolves to no glyph in the embedded font program", fontName, pageNum)
}
