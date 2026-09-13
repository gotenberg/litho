package compliance

import (
	"bytes"
	"context"
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
		case "q":
			gsStack = append(gsStack, savedTextState{font: currentFont, renderMode: renderMode})
		case "Q":
			if n := len(gsStack); n > 0 {
				currentFont, renderMode = gsStack[n-1].font, gsStack[n-1].renderMode
				gsStack = gsStack[:n-1]
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
	// the page-resource name; checkedObjs keys on the resolved object number so
	// the annotation-appearance pass does not re-check a font also declared in a
	// page's /Resources/Font.
	checked := make(map[string]bool)
	checkedObjs := make(map[int]bool)

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

		results = append(results, checkPageDeclaredFonts(ctx, doc, catalog, p, pageIdx+1, profile, checked, checkedObjs, visibleAnywhere)...)

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
		results = append(results, checkFormXObjectFonts(ctx, doc, p.parsedOps(ctx), p.resources, treeRoot, pageIdx+1, profile, checkedObjs, xobjVisited, 0)...)

		// A Type3 glyph procedure is another content stream that may select
		// fonts of its own, and the page walk above stops at the Type3 font
		// itself. veraPDF applies the font rules inside those procedures too.
		results = append(results, checkType3CharProcFonts(ctx, doc, p.parsedOps(ctx), p.resources, treeRoot, pageIdx+1, profile, checkedObjs, xobjVisited, 0)...)
	}

	doc.Logger.Debug("compliance: fonts: font checks complete",
		"violations", len(results), "fonts_checked", len(checked))
	return results
}

// checkPageDeclaredFonts runs the full font-check suite over the fonts declared
// in a page's /Resources/Font. It records each resolved font object number in
// checkedObjs so [checkAnnotationAppearanceFonts] does not re-check a font that
// is also declared on the page.
func checkPageDeclaredFonts(ctx context.Context, doc *document.Document, catalog ast.PdfDict, p *pageCtx, pageNum int, profile string, checked map[string]bool, checkedObjs map[int]bool, visibleAnywhere map[int]bool) []Violation {
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
	fontUsage := collectFontUsage(p.parsedOps(ctx))
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
			dictRulesDone = checkedObjs[ref.ObjectNum]
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
		// passes in [checkFontGroup] with that context's usage, so it is left
		// unmarked in checkedObjs here to avoid starving them. Only present but
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
			checkedObjs[ref.ObjectNum] = true
		}
		// Skip width checks for fonts not used for visible rendering here.
		isVisible := parseFailed || (usage != nil && usage.visible)
		// Embedding is judged once per object, so it asks whether any page
		// renders the font visibly.
		embedVisible := isVisible
		if isIndirect && visibleAnywhere[ref.ObjectNum] {
			embedVisible = true
		}

		if !dictRulesDone {
			results = append(results, checkFontDictRules(ctx, doc, font, fontKey, pageNum, profile, embedVisible, usage)...)
		}
		if fontSubtype(ctx, doc, font) == "Type0" {
			if dictRulesDone {
				// The dictionary was reported on an earlier page, but this page's
				// glyphs are still unjudged. See [checkType0UsageRules]. Content
				// litho could not parse names no glyphs it can judge: the
				// conservative "check every declared font" fallback exists for the
				// dictionary rules, and pdfjs issue11678.pdf, whose page 2 stream
				// declares /Length 4218 over no data, otherwise reports a .notdef
				// against recovered bytes veraPDF never reads.
				if descArr, err := document.ResolveArray(ctx, doc, font["DescendantFonts"]); err == nil && len(descArr) > 0 && !parseFailed {
					if cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0]); err == nil && cidFontDict != nil {
						results = append(results, checkType0UsageRules(ctx, doc, font, cidFontDict, fontKey, pageNum, profile, usage, withoutMalformedCodes)...)
					}
				}
			} else {
				results = append(results, checkType0Font(ctx, doc, font, fontKey, pageNum, profile, isVisible, usage)...)
			}
		} else {
			results = append(results, checkFontUsageRules(ctx, doc, font, fontKey, pageNum, profile, isVisible, usage)...)
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
// Recursion into nested forms is bounded by [maxFormXObjectDepth]; visited
// guards resource cycles; checkedObjs deduplicates against fonts already checked
// on the page and across forms.
func checkFormXObjectFonts(ctx context.Context, doc *document.Document, ops []content.Operation, res, treeRoot ast.PdfDict, pageNum int, profile string, checkedObjs, visited map[int]bool, depth int) []Violation {
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
		results = append(results, checkFormPUA(ctx, doc, xobjRes, subOps, data, stream.Dict, treeRoot, pageNum, profile)...)
		results = append(results, checkFormXObjectFonts(ctx, doc, subOps, xobjRes, treeRoot, pageNum, profile, checkedObjs, visited, depth+1)...)
	}
	return results
}

// checkType3CharProcFonts checks fonts used for rendering inside the glyph
// procedures of a Type3 font that a content stream selects. A Type3 glyph
// procedure is a content stream in its own right (ISO 32000-1 §9.6.5) and may
// select fonts from the Type3 font's own /Resources, including another Type3
// font. veraPDF descends through those procedures and applies the same font
// rules to what they select, so a non-embedded font reached only that way
// (ISO 19005-2 §6.2.11.4.1) is otherwise never checked: pdfjs
// ContentStreamCycleType3insideType3.pdf reaches an unembedded Helvetica
// through two nested Type3 fonts.
//
// Only the procedures for character codes the stream actually shows are
// walked, so a glyph that is never painted brings no font into scope.
// Recursion is bounded by [maxFormXObjectDepth]; visited holds the object
// numbers already descended into, which breaks the resource cycle the fixture
// name warns about. Object numbers are unique document-wide, so sharing one
// visited set with the Form XObject walk cannot confuse a font for a form.
func checkType3CharProcFonts(ctx context.Context, doc *document.Document, ops []content.Operation, res, treeRoot ast.PdfDict, pageNum int, profile string, checkedObjs, visited map[int]bool, depth int) []Violation {
	if depth > maxFormXObjectDepth || ctx.Err() != nil {
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
			results = append(results, checkResourceFonts(ctx, doc, t3Res, procOps, pageNum, profile, checkedObjs)...)
			results = append(results, checkType3CharProcFonts(ctx, doc, procOps, t3Res, treeRoot, pageNum, profile, checkedObjs, visited, depth+1)...)
			results = append(results, checkFormXObjectFonts(ctx, doc, procOps, t3Res, treeRoot, pageNum, profile, checkedObjs, visited, depth+1)...)
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
func checkFontDictRules(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isVisible bool, usage *fontUsageInfo) []Violation {
	var results []Violation

	subtype := fontSubtype(ctx, doc, fontDict)

	// Required font-dictionary keys (PDFont): §6.3.2 / §6.2.11.2 / §6.2.10.2.
	results = append(results, checkFontRequiredKeys(ctx, doc, fontDict, subtype, PDFAPart(profile))...)
	if subtype == "Type0" {
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
// Simple fonts only; [checkType0Font] covers the composite path.
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

	// Glyph coverage (CharSet for Type1 subsets).
	results = append(results, checkGlyphCoverage(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

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
// selects. It does not consult checkedObjs: a font already judged where the
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
// named via the page /Properties), or -1 when absent.
func bdcMCID(ctx context.Context, doc *document.Document, op content.Operation, props func() ast.PdfDict) int {
	pd := bdcProps(ctx, doc, op, props)
	if pd == nil {
		return -1
	}
	mcid, err := document.ResolveInt(ctx, doc, pd["MCID"])
	if err != nil {
		return -1
	}
	return int(mcid)
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
	out := make(map[uint32]bool)
	for code, name := range resolveSimpleFontEncoding(ctx, doc, fontDict) {
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
	// [checkSingleFont] caller supplies usage whenever it can prove non-use, so
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
// shows its own codes, so these run again for each: govdocs
// thread_100_100870.pdf selects one Times-Roman Identity-H font on fourteen
// pages, and only page 20 shows CID 0 and a CID its CFF charset does not hold.
// veraPDF, which builds a glyph per show operator, reports both under
// ISO 19005-2 §6.2.11.8 and §6.2.11.4.1 t2; litho judged the font on page 9 and
// never looked again. Simple fonts already work this way through
// [checkFontUsageRules].
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
				if unitsPerEm == 0 {
					unitsPerEm = 1000
				}
				rule := glyphWidthRule(profile)
				if rule.Specification != "" {
					results = append(results, checkCIDFontWidths(ctx, doc, cidFontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, type0UsedCIDs(ctx, doc, fontDict, usage))...)
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
			// fonts do define a glyph there, so it is not universally .notdef.
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
			if fd := document.ResolveFontDescriptor(ctx, doc, fontDict); fd == nil ||
				extractFontProgramData(ctx, doc, fd) == nil {
				namedTable = namedEncodingTable(ctx, doc, fontDict)
			}
			for _, code := range sortedByteKeys(usage.charCodes) {
				glyphName := enc[code]
				if glyphName == ".notdef" {
					return failf(rule, "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
				}
				// The symbolic and DEL exemptions below apply here too: a
				// symbolic font resolves codes through its built-in encoding
				// (ISO 32000-1, 9.6.6.2) and some fonts do define a DEL glyph.
				if glyphName == "" && namedTable != "" && !symbolic && code != 0x7F {
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
				// first (PDF 9.6.6.4): charCode 0 used but absent from an
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
					// GID 0: pdfjs issue13107_reduced.pdf embeds a DejaVuSans subset
					// with no .notdef glyph whose post table names GID 0 "space".
					for code := range usage.charCodes {
						glyphName := enc[code]
						if glyphName == "" || glyphName == ".notdef" {
							return failf(rule, "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
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
	if widthsInconsistent(declared, expected) {
		return failf(rule, "font %s on page %d: .notdef glyph width mismatch (declared=%.1f, font=%.1f)",
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
// never held: pdfjs issue11678.pdf inflates one into a hex string with an odd
// digit count, which under Identity-H looks exactly like a rendered .notdef.
// litho judges that once per font object, where it has always been judged, and
// does not re-run it for every page that selects the font.
type judgeMalformedCodes bool

const (
	withMalformedCodes    judgeMalformedCodes = true
	withoutMalformedCodes judgeMalformedCodes = false
)

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
	usedCIDs, notdef, ok := collectType0CIDs(string(encName), embedded, usage)
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
		// see [type0HasEmbeddedProgram].
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
				// no glyph is present for it. Glyph presence asks only about
				// glyphs referenced for rendering, so renderingMode == 3
				// exempts it, as in the CIDFontType2 branch below; the
				// .notdef rule keeps firing. veraPDF reports 6.2.11.8 and not
				// 6.2.11.4.1 on govdocs thread_200_200565.pdf, whose 411 show
				// operators are all mode 3.
				results := failf(rule, "font %s on page %d: CID %d not in font charset (.notdef)", fontName, pageNum, cid)
				if pr := glyphPresenceRule(profile); pr.Specification != "" && usage.visible {
					results = append(results, failf(pr,
						"font %s on page %d: CID %d not present in embedded font program", fontName, pageNum, cid)...)
				}
				return results
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
		if pr := glyphPresenceRule(profile); pr.Specification != "" && usage.visible {
			if cidToGIDMap, _ := document.ResolveName(ctx, doc, cidFontDict["CIDToGIDMap"]); cidToGIDMap == "Identity" {
				if outside := outOfProgramCIDs(fontData, usedCIDs); len(outside) > 0 {
					return failf(pr, "font %s on page %d: CID %d not present in embedded font program",
						fontName, pageNum, outside[0])
				}
			}
		}
	}

	return nil
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
			// renderingMode == 3 exempts a glyph outright, and usage.visible is
			// false only when every use of this font was in that mode.
			if usage.visible {
				for _, cid := range outOfProgramCIDs(fontData, usedCIDs) {
					absent[cid] = true
				}
			}
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
	// A non-empty absent set already implies an embedded program (absentType0CIDs
	// populates it only from decoded program bytes); the decode signal does not,
	// so recheck. See [type0HasEmbeddedProgram] for why an unembedded font is out
	// of scope for §6.3.5 t1.
	if !type0HasEmbeddedProgram(ctx, doc, fontDict) {
		return nil
	}
	return failf(rule, "font %s on page %d: rendered code resolves to no glyph in the embedded font program", fontName, pageNum)
}

// collectVisibleFontObjects returns the font object numbers that at least one
// page renders visibly. The embedding rule is reported once per font object, so
// it cannot ask a single page whether the font is visible: a font drawn in
// rendering mode 3 on the page that happens to declare it first, and normally on
// a later page, is embedded-or-not regardless of which page is asked.
func collectVisibleFontObjects(ctx context.Context, doc *document.Document, vctx *validationCtx) map[int]bool {
	visible := make(map[int]bool)
	for pageIdx := range vctx.pages {
		if ctx.Err() != nil {
			return visible
		}
		p := &vctx.pages[pageIdx]
		if p.resources == nil {
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
