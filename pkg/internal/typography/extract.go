package typography

import (
	"context"
	"log/slog"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
	"github.com/gotenberg/litho/pkg/models"
)

// fallbackFont returns a minimal font that uses WinAnsiEncoding, matching
// a minimal Helvetica/WinAnsiEncoding font dictionary.
// This allows text extraction to continue when a font cannot be resolved,
// producing best-effort output rather than aborting the page.
//
// It carries Helvetica's widths, as that dictionary does (see
// [injectStandard14Widths]), and as pdf.js lays out a font the resources do not
// define: its fallback font takes Helvetica's metrics. Without them every glyph
// advanced one em, twice Helvetica's average, so text in the fallback reached
// twice as far across the page as any reader draws it, and the part past the
// page's edge was dropped as clipped.
//
// # Corpus evidence
//
//   - pdfjs issue9105_other.pdf: draws "Adobe tweet" at 105 points in a font
//     its resources do not define, 583.69 points wide in pdf.js. At one em a
//     glyph it reached 1155, and once a rewrite gave the page the US Letter
//     media box it lacks, litho read only "Adobe" from it and the PDF/A-2b
//     remediation refused its own output for the text it lost.
//   - All corpora: six files draw text in a font their resources do not
//     define, and the text litho extracts from each, and its auto-tagging of
//     each, are unchanged byte for byte.
func fallbackFont() *Font {
	font := &Font{
		Subtype:  "Type1",
		Encoding: copyEncoding(encodings.WinAnsiEncoding),
	}
	injectStandard14Widths(font, "Helvetica", false)
	return font
}

// collectAnnotAppearanceObjects returns the set of object numbers referenced by
// annotation appearance streams (/AP) on this page. These streams render visual
// overlays (stamps, widgets, markup) and are excluded from text extraction to
// avoid duplicating content that is not part of the page's semantic text flow.
func collectAnnotAppearanceObjects(ctx context.Context, doc *document.Document, pageDict ast.PdfDict) map[int]bool {
	result := make(map[int]bool)
	annotsObj, ok := pageDict["Annots"]
	if !ok {
		return result
	}
	annotsArr, err := document.ResolveArray(ctx, doc, annotsObj)
	if err != nil {
		return result
	}
	if annotsArr == nil {
		return result
	}
	for _, annotObj := range annotsArr {
		annotDict, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil {
			continue
		}
		if annotDict == nil {
			continue
		}
		apObj, ok := annotDict["AP"]
		if !ok {
			continue
		}
		apDict, err := document.ResolveDict(ctx, doc, apObj)
		if err != nil {
			continue
		}
		if apDict == nil {
			continue
		}
		// Collect object numbers from /N (normal), /R (rollover), /D (down) appearances.
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			apEntry, ok := apDict[key]
			if !ok {
				continue
			}
			if ref, ok := apEntry.(ast.PdfIndirectRef); ok {
				result[ref.ObjectNum] = true
			}
			// An appearance can also be a dict of state-specific appearances.
			stateDict, err := document.ResolveDict(ctx, doc, apEntry)
			if err != nil {
				continue
			}
			for _, stateObj := range stateDict {
				if ref, ok := stateObj.(ast.PdfIndirectRef); ok {
					result[ref.ObjectNum] = true
				}
			}
		}
	}
	return result
}

// extractTextRecursive recursively extracts text from operations, supporting
// Form XObjects. The visited set tracks indirect reference object numbers to
// break circular references, and depth guards against non-circular but
// excessively deep nesting: it is how many forms the operations sit below the
// page, 0 for the page's own, and a form deeper than the document's form
// budget (limits.DefaultMaxFormDepth by default) has no text extracted.
// annotAppearances contains object numbers of annotation appearance streams to
// skip.
//
// It is a thin dispatcher: the actual per-operator work lives in per-operator
// helpers ([handleTfOp], [handleShowTextOp], [handleTJOp], [handleDoOp]) so
// that each piece of the extraction state machine can be audited in
// isolation.
//
// When origins is non-nil, one entry is appended to it per element appended to
// elements, naming the index in ops of the operation that drew it.
func extractTextRecursive(ctx context.Context, doc *document.Document, machine *graphics.Machine, resources ast.PdfDict, ops []content.Operation, elements *[]models.TextElement, visited map[int]bool, depth int, annotAppearances map[int]bool, origins *[]int, fontRefs *[]int, runs *runRecorder) error {
	if depth > doc.Limits().MaxFormDepth {
		if doc != nil {
			doc.Logger.Debug("form xobject max depth reached",
				slog.Int("depth", depth),
			)
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Make resources available to the machine for pattern resolution, and tell
	// it how deep this stream sits so recorded geometry can say whether the page
	// or a form drew it. handleDoOp puts both back after it recurses.
	machine.Doc = doc
	machine.Resources = resources
	machine.FormDepth = depth

	populateOCsFromResources(ctx, doc, machine, resources)

	// This stream starts a text item of its own, and so does every Do in it:
	// see [runFont] stream and segment. Its marked-content operators end
	// one: see [runFont] marked.
	var stream, segment, marked int
	if runs != nil {
		stream = runs.next()
		segment = runs.next()
		marked = runs.next()
	}

	var currentFont *Font
	var currentFontName string
	// The object number of the font dictionary currently selected by Tf, so a
	// caller can tell which font drew each glyph.
	currentFontRef := FontUnattributable
	syncFont := func(state *graphics.State) {
		// q/Q save/restore the Tf state on the graphics stack. The
		// currentFont variable lives outside that stack, so a Q that
		// pops back to a different font must re-resolve to keep the
		// next Tj/TJ from decoding bytes through the wrong CMap.
		if state.Text.Tf == currentFontName {
			return
		}
		currentFontName = state.Text.Tf
		if state.Text.Tf == "" {
			currentFont = nil
			currentFontRef = FontUnattributable
			return
		}
		if font, objNum := resolveFontFromResources(ctx, doc, resources, ast.PdfName(state.Text.Tf)); font != nil {
			currentFont = font
			currentFontRef = objNum
			return
		}
		// A font litho could not resolve is drawn with a stand-in, and the
		// stand-in is not in the file: nothing to attribute it to.
		currentFont = fallbackFont()
		currentFontRef = FontUnattributable
	}
	for opIdx, op := range ops {
		// Check for context cancellation every 100 operators to respect
		// deadlines without adding per-operator overhead.
		if opIdx%100 == 0 && opIdx > 0 { //nolint:mnd // amortized cancellation check
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		// Name the operator before it runs, so any geometry it records can say
		// which operation drew it.
		machine.OpIndex = opIdx
		// Errors from Execute are non-fatal: unknown/unsupported operators
		// return errors but must not abort extraction of the rest.
		_ = machine.ExecuteOne(ctx, op) //nolint:errcheck // Deliberate: an unsupported operator must not abort extraction of the rest, as the comment above says.

		if runs != nil && (op.Operator == "BMC" || op.Operator == "BDC" || op.Operator == "EMC") {
			marked = runs.next()
		}

		state := machine.Current()

		// Text extraction returns the rendered glyphs and does not
		// honour /ActualText; the substitution is reserved for
		// accessibility consumers and would surface lowercase 'v' in
		// place of the small-caps 'V' actually drawn on the page. Drop
		// any PendingActualText that BDC may have set so it does not
		// affect downstream state, and let the glyph-rendering
		// operators emit normally.
		machine.PendingActualText = ""

		if state.SkipExtraction {
			continue
		}

		// Operators that touch the graphics state (q, Q, cm, gs, …) may
		// have popped a different Tf onto the active state. Re-resolve
		// before any text-rendering operator so handleShowTextOp /
		// handleTJOp see the correct font.
		syncFont(state)

		before := len(*elements)

		// The operator has already run, so for ' and " the line move and the
		// spacing it sets are in Tm: what is taken here is the glyphs' own
		// advance, the part a caller removing the operator has to put back.
		isShow := op.Operator == "Tj" || op.Operator == "TJ" || op.Operator == "'" || op.Operator == "\""
		showStart := state.Text.Tm

		switch op.Operator {
		case "Tf":
			// syncFont above has already resolved this font and captured its
			// reference: Execute applied the Tf before the loop reached here,
			// so the state carries the new name and syncFont's early return
			// does not fire. Capturing again keeps the reference from drifting
			// away from currentFont if either path changes, and both resolve
			// through the same cache, so the second lookup costs nothing.
			currentFont, currentFontRef = handleTfOp(ctx, doc, resources, op, currentFont, currentFontRef)
			// A malformed content stream can carry a Tf with no operands;
			// guard the operand access so a crafted page cannot panic.
			if len(op.Operands) > 0 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					currentFontName = string(name)
				}
			}
		case "Tj":
			if err := handleShowTextOp(ctx, machine, currentFont, op, 0, elements, runs); err != nil {
				return err
			}
		case "'":
			if err := handleShowTextOp(ctx, machine, currentFont, op, 0, elements, runs); err != nil {
				return err
			}
		case "\"":
			if err := handleShowTextOp(ctx, machine, currentFont, op, 2, elements, runs); err != nil {
				return err
			}
		case "TJ":
			if err := handleTJOp(ctx, machine, currentFont, op, elements, runs); err != nil {
				return err
			}
		case "Do":
			if err := handleDoOp(ctx, doc, machine, resources, op, elements, visited, depth, annotAppearances, runs); err != nil {
				return err
			}
		}

		if isShow && depth == 0 {
			end := machine.Current()
			machine.RecordShowAdvance(opIdx, graphics.ShowAdvance{
				Before:          showStart,
				After:           end.Text.Tm,
				FontSize:        end.Text.Tfs,
				HorizontalScale: end.Text.Tz / 100,
				Vertical:        currentFont != nil && currentFont.WMode == 1,
			})
		}

		// Name the operation that drew each new element, so a caller can map an
		// element back to the bytes that produced it. A Do invokes a form
		// XObject whose elements come from the form's own content stream, so
		// their bytes are not in ops at all: mark them unattributable rather
		// than name the Do, which would invite a caller to remove a whole form
		// to reach one glyph drawn inside it.
		if origins != nil {
			origin := opIdx
			if op.Operator == "Do" {
				origin = OriginUnattributable
			}
			for range len(*elements) - before {
				*origins = append(*origins, origin)
			}
		}

		// Name the font that drew each new element, so a caller can tell which
		// embedded font a glyph came from. Attributed here rather than at the
		// append itself because this is where the count of new elements is
		// known, and the font selected by Tf is in scope.
		if fontRefs != nil {
			fontRef := currentFontRef
			if op.Operator == "Do" {
				// A form resolves fonts through its own /Resources, so the font
				// selected out here did not draw them.
				fontRef = FontUnattributable
			}
			for range len(*elements) - before {
				*fontRefs = append(*fontRefs, fontRef)
			}
		}

		// Name the font each new element was shown in, so reconstruction can
		// tell where a change of font starts a new run. A form's elements were
		// named by the recursion, against the fonts of its own resources;
		// whatever it did not name takes the font selected out here.
		if runs != nil {
			cur := machine.Current()
			key := runFont{name: currentFontName, font: currentFont, size: cur.Text.Tfs, rise: riseOnPage(cur), height: heightOnPage(cur) * currentFont.type3HeightScale(cur.Text.Tfs), trm: renderingMatrixOnPage(cur), stream: stream, segment: segment, marked: marked, unadvanced: cur.Text.Unadvanced}
			// A show operator advances the state's shift past each glyph PDF.js
			// does not advance past. A Do's glyphs moved the form's own state,
			// which its synthetic Q has already put back.
			for len(runs.fonts) < len(*elements) {
				key.gapWidth = runs.gapWidths[len(runs.fonts)]
				runs.fonts = append(runs.fonts, key)
				if el := (*elements)[len(runs.fonts)-1]; isShow && skipsAdvance(el.Text) {
					key.unadvanced += el.Width
				}
			}
			if isShow {
				cur.Text.Unadvanced = key.unadvanced
			}
			// PDF.js ends the text item before it paints any XObject, an
			// image as well as a form (src/core/evaluator.js, getTextContent,
			// OPS.paintXObject), so the glyphs after a Do start one.
			if op.Operator == "Do" {
				segment = runs.next()
			}
		}
	}
	return nil
}

// populateOCsFromResources reads /Properties from the given resource
// dictionary and populates machine.ActiveOCs with the visibility state of
// each OCG referenced there. Called at the top of every content-stream
// recursion level so nested Form XObjects inherit the correct optional-
// content visibility.
func populateOCsFromResources(ctx context.Context, doc *document.Document, machine *graphics.Machine, resources ast.PdfDict) {
	propsObj, ok := resources["Properties"]
	if !ok {
		return
	}
	propsDict, err := document.ResolveDict(ctx, doc, propsObj)
	if err != nil {
		doc.Logger.Debug("resolving Properties dict", slog.Any("err", err))
	}
	for k, v := range propsDict {
		dict, err := document.ResolveDict(ctx, doc, v)
		if err != nil {
			doc.Logger.Debug("resolving Properties entry", slog.String("key", string(k)), slog.Any("err", err))
		}
		if dict == nil {
			continue
		}
		subtypeObj, ok := dict["Type"]
		if !ok {
			continue
		}
		subtypeName, err := document.ResolveName(ctx, doc, subtypeObj)
		if err != nil {
			doc.Logger.Debug("resolving OCG type name", slog.Any("err", err))
		}
		if subtypeName != "OCG" {
			continue
		}
		machine.ActiveOCs[string(k)] = ocgVisible(v, doc.OCConfig)
	}
}

// ocgVisible returns the effective visibility of an optional-content group
// given its indirect reference and the document's OC configuration. The
// precedence is /Off → /On → /BaseState, matching PDF 32000 §8.11.
func ocgVisible(v ast.PdfObject, cfg *document.OCConfig) bool {
	if cfg == nil {
		return true
	}
	ref, ok := v.(ast.PdfIndirectRef)
	if !ok {
		return true
	}
	switch {
	case cfg.Off[ref.ObjectNum]:
		return false
	case cfg.On[ref.ObjectNum]:
		return true
	default:
		return cfg.BaseState != "OFF"
	}
}

// handleTfOp resolves the font named by a Tf operator through the current
// page's /Font resource dict and the document-wide font cache. Returns
// the resolved font, or a best-effort [fallbackFont] if nothing can be
// found. Never returns an error, extraction always continues.
//
// It also returns the object number of the font dictionary, which is what
// identifies a font across the document, or [FontUnattributable] when there is
// no object to name. A Tf this cannot read leaves the font unchanged, so it
// gives back the caller's current number rather than clearing it.
func handleTfOp(
	ctx context.Context,
	doc *document.Document,
	resources ast.PdfDict,
	op content.Operation,
	prev *Font,
	prevRef int,
) (*Font, int) {
	if len(op.Operands) < 1 {
		return prev, prevRef
	}
	fontName, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return prev, prevRef
	}

	if font, objNum := resolveFontFromResources(ctx, doc, resources, fontName); font != nil {
		return font, objNum
	}

	// Fall back to a default font rather than aborting the page. Text may
	// be garbled, but extraction continues. Best-effort is the contract.
	//
	// The stand-in is not in the file, so there is nothing to attribute its
	// glyphs to.
	if doc != nil {
		doc.Logger.Debug("font not found, using fallback",
			slog.String("font", string(fontName)),
		)
	}
	return fallbackFont(), FontUnattributable
}

// resolveFontFromResources returns the font a resource name binds, and the
// object number of the font dictionary behind it. It looks the name up in the
// resource dict's /Font sub-dict, consulting the document's font cache first
// and populating it on cache miss.
//
// The object number is what identifies a font across the document: a resource
// name is scoped to one page's resources, so two pages may bind /F1 to
// different fonts.
//
// It reports [FontUnattributable] whenever there is no object to name: the
// resources hold the font dictionary directly rather than referencing it, the
// name is absent, or the font could not be read at all. The last two return a
// nil font as well, so a caller that checks the font first never sees them.
func resolveFontFromResources(ctx context.Context, doc *document.Document, resources ast.PdfDict, fontName ast.PdfName) (*Font, int) {
	resObj, ok := resources["Font"]
	if !ok {
		return nil, FontUnattributable
	}
	resDictRoot, err := document.ResolveDict(ctx, doc, resObj)
	if err != nil {
		doc.Logger.Debug("resolving Font resource dict", slog.Any("err", err))
	}
	if resDictRoot == nil {
		return nil, FontUnattributable
	}
	fontRefObj, ok := resDictRoot[fontName]
	if !ok {
		return nil, FontUnattributable
	}

	objNum := FontUnattributable
	if ref, ok := fontRefObj.(ast.PdfIndirectRef); ok {
		objNum = ref.ObjectNum
	}

	// Cache hit by indirect reference object number.
	if ref, ok := fontRefObj.(ast.PdfIndirectRef); ok && doc != nil {
		if cached, ok := doc.CachedFont(ref.ObjectNum); ok {
			if f, ok := cached.(*Font); ok {
				return f, objNum
			}
		}
	}

	// Cache miss: parse the font dict and store it back.
	fontDict, err := document.ResolveDict(ctx, doc, fontRefObj)
	if err != nil || fontDict == nil {
		return nil, FontUnattributable
	}
	font, err := ResolveFont(ctx, doc, fontDict)
	if err != nil {
		return nil, FontUnattributable
	}
	if ref, ok := fontRefObj.(ast.PdfIndirectRef); ok && doc != nil {
		doc.SetCachedFont(ref.ObjectNum, font)
	}
	return font, objNum
}

// handleShowTextOp processes the Tj, ' and " operators. strIndex selects the
// operand that holds the PdfString: 0 for Tj and ', 2 for the
// word/char-spacing form ".
func handleShowTextOp(ctx context.Context, machine *graphics.Machine, currentFont *Font, op content.Operation, strIndex int, elements *[]models.TextElement, runs *runRecorder) error {
	if machine.IgnoreTextOps || currentFont == nil {
		return nil
	}
	if strIndex >= len(op.Operands) {
		return nil
	}
	str, ok := op.Operands[strIndex].(ast.PdfString)
	if !ok {
		return nil
	}
	return processText(ctx, str, currentFont, machine, elements, runs)
}

// handleTJOp processes the TJ (show text with kerning) operator. Numeric
// array elements advance the text matrix instead of drawing a glyph, so
// they must be dispatched separately from string elements.
func handleTJOp(ctx context.Context, machine *graphics.Machine, currentFont *Font, op content.Operation, elements *[]models.TextElement, runs *runRecorder) error {
	if machine.IgnoreTextOps || currentFont == nil {
		return nil
	}
	if len(op.Operands) != 1 {
		return nil
	}
	arr, ok := op.Operands[0].(ast.PdfArray)
	if !ok {
		return nil
	}
	for _, item := range arr {
		if str, ok := item.(ast.PdfString); ok {
			if err := processText(ctx, str, currentFont, machine, elements, runs); err != nil {
				return err
			}
			continue
		}
		var numVal float64
		switch num := item.(type) {
		case ast.PdfInt:
			numVal = float64(num)
		case ast.PdfReal:
			numVal = float64(num)
		default:
			continue
		}
		applyTJKerning(machine, currentFont, numVal)
	}
	return nil
}

// applyTJKerning shifts the text matrix by a TJ numeric-array adjustment.
// Horizontal fonts advance along the X axis; vertical fonts (WMode 1)
// advance along the Y axis.
func applyTJKerning(machine *graphics.Machine, currentFont *Font, numVal float64) {
	state := machine.Current()
	fontSize := state.Text.Tfs
	if fontSize == 0 {
		fontSize = 12
	}
	adjustment := -(numVal / 1000.0) * fontSize
	if currentFont.WMode == 1 {
		state.Text.Tm[4] += adjustment * state.Text.Tm[2]
		state.Text.Tm[5] += adjustment * state.Text.Tm[3]
		return
	}
	state.Text.Tm[4] += adjustment * state.Text.Tm[0]
	state.Text.Tm[5] += adjustment * state.Text.Tm[1]
}

// handleDoOp processes the Do operator, invoking a Form XObject by name
// from the current resource dict. Non-Form XObjects (images, for example)
// are silently skipped. Only Form streams carry content-stream ops the
// extractor can recurse into.
func handleDoOp(ctx context.Context, doc *document.Document, machine *graphics.Machine, resources ast.PdfDict, op content.Operation, elements *[]models.TextElement, visited map[int]bool, depth int, annotAppearances map[int]bool, runs *runRecorder) error {
	if len(op.Operands) != 1 {
		return nil
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return nil
	}
	xobjObj, ok := resources["XObject"]
	if !ok {
		return nil
	}
	// The machine's staged objects come first: a caller reading content as an
	// edit staged it, a redaction after a PDF stamp, sets them, and the stamp's
	// form exists nowhere else.
	xobjDict, err := document.ResolveDict(ctx, doc, machine.StagedObject(xobjObj))
	if err != nil {
		doc.Logger.Debug("resolving XObject dict for Do", slog.Any("err", err))
	}
	if xobjDict == nil {
		return nil
	}
	targetObj, ok := xobjDict[name]
	if !ok {
		return nil
	}

	// Break circular references and skip annotation appearance streams.
	objNum := 0
	if ref, ok := targetObj.(ast.PdfIndirectRef); ok {
		objNum = ref.ObjectNum
		if visited[objNum] || annotAppearances[objNum] {
			return nil
		}
	}

	stream, err := document.ResolveStream(ctx, doc, machine.StagedObject(targetObj))
	if err != nil {
		doc.Logger.Debug("resolving Form XObject stream", slog.Any("err", err))
	}
	if stream.Dict == nil {
		return nil
	}
	subtypeObj, ok := stream.Dict["Subtype"]
	if !ok {
		return nil
	}
	subtypeName, err := document.ResolveName(ctx, doc, subtypeObj)
	if err != nil {
		doc.Logger.Debug("resolving XObject subtype", slog.Any("err", err))
	}
	if subtypeName != "Form" {
		return nil
	}

	formOps, err := content.ParsePageContent(ctx, doc, stream, content.SplitGluedOperators())
	if err != nil {
		return nil //nolint:nilerr // A form whose content does not parse contributes no text.
	}

	// Determine the Form's own /Resources; inherit the parent's on absence.
	formRes := resources
	if resObj, ok := stream.Dict["Resources"]; ok {
		resDict, err := document.ResolveDict(ctx, doc, machine.StagedObject(resObj))
		if err != nil {
			doc.Logger.Debug("resolving Form XObject resources", slog.Any("err", err))
		}
		if resDict != nil {
			formRes = resDict
		}
	}

	// q / cm / recurse / Q. These trivial state operators cannot fail in
	// practice and the machine doesn't use the return values from
	// Execute, so the errcheck suppressions are intentional.
	_ = machine.Execute(ctx, []content.Operation{{Operator: "q"}}) //nolint:errcheck // Deliberate: a synthetic q cannot fail meaningfully, and a failure must not abort extraction.
	if matrixObj, ok := stream.Dict["Matrix"]; ok {
		matrixArr, err := document.ResolveArray(ctx, doc, matrixObj)
		if err != nil {
			return err
		}
		if len(matrixArr) == 6 {
			_ = machine.Execute(ctx, []content.Operation{{ //nolint:errcheck // Deliberate: a synthetic state operation must not abort extraction.
				Operator: "cm",
				Operands: matrixArr,
			}})
		}
	}

	// Mark as visited before recursing; unmark after so the same XObject
	// can appear under sibling parents in the same content stream.
	if objNum != 0 {
		visited[objNum] = true
	}
	// A form's marked-content spans belong to the form's own stream
	// (ISO 32000-1, 14.6). Close any it leaves open before returning to the
	// parent, or an unbalanced /Artifact would suppress the rest of the page.
	mcSpans := machine.SnapshotMarkedContent()
	// The recursion points the machine at the form's resources and depth, and
	// nothing else puts them back: the synthetic Q restores the graphics state,
	// but these ride the machine rather than the state stack. Left unrestored,
	// every later lookup on this page resolves against the form's resources,
	// and every later mark is attributed to the form rather than the page.
	prevResources := machine.Resources
	prevDepth := machine.FormDepth
	prevOpIndex := machine.OpIndex
	// No origins below the top level: these elements are drawn by the form's own
	// content stream, and the caller's operation indices do not address it. No
	// font references either, and for a sharper reason: the form selects its
	// fonts from its own /Resources, so the reference held out here names a
	// different font than the one that drew them. The run fonts do pass
	// down: they name the font by what the form selected, not by an object
	// a caller could act on.
	err = extractTextRecursive(ctx, doc, machine, formRes, formOps, elements, visited, depth+1, annotAppearances, nil, nil, runs)
	machine.Resources = prevResources
	machine.FormDepth = prevDepth
	machine.OpIndex = prevOpIndex
	machine.RestoreMarkedContent(mcSpans)
	if objNum != 0 {
		delete(visited, objNum)
	}
	if err != nil {
		return err
	}

	_ = machine.Execute(ctx, []content.Operation{{Operator: "Q"}}) //nolint:errcheck // Deliberate: the matching Q restores state; a failure must not abort extraction.
	return nil
}

// ExtractionConfig holds options for text extraction behavior.
type ExtractionConfig struct {
	StripInvisible bool // If true, completely omits text matched by invisibility heuristics (Tr=3, white-on-white)
	StripHidden    bool // If true, completely omits text outside the graphics state clipping path (W/W*)
	StripClipped   bool // If true, completely omits text outside the page CropBox
	StripArtifacts bool // If true, discards text inside /Artifact marked content (headers, footers, page numbers)
}

// extractElements does the work behind [ExtractElements],
// [ExtractElementsWithOrigins], [ExtractElementsWithAttribution] and
// [ExtractText]. It reports origins, font references and run fonts only
// when asked, so the common path allocates nothing extra.
//
// # Corpus evidence
//
//   - pdfjs ShowText-ShadingPattern.pdf: repeats "Helvetica" many points past
//     the right margin, and that runaway tail must stay clipped.
//   - pdfjs issue14824.pdf: "Photoshop" set across the right edge of an A4
//     page read "Photos", its "s" 0.4 points past the edge let back in as
//     a hairline overshoot that continued the word. govdocs
//     thread_200_200620.pdf read a stray "e" the same way. Dropping the
//     overshoot rule makes words match PDF.js on these 2 files and on none
//     fewer; pdfjs bug920426.pdf, whose "Checkliste Service" the rule was
//     written for, keeps its words, its "e" 24 points inside the edge.
func extractElements(ctx context.Context, doc *document.Document, machine *graphics.Machine, page *document.Page, ops []content.Operation, record extractionRecord) (extraction, error) {
	// Collect annotation appearance object numbers to skip them during text
	// extraction. Annotation appearances are visual overlays, not page content.
	annotAppearances := collectAnnotAppearanceObjects(ctx, doc, page.Dict)

	// Pre-allocate with a capacity estimate: roughly 1 text element per 3 operators.
	estimatedCap := max(len(ops)/3,
		//nolint:mnd // minimum capacity to avoid early reallocations
		64)
	elements := make([]models.TextElement, 0, estimatedCap)
	var origins []int
	var originsPtr *[]int
	if record.origins {
		origins = make([]int, 0, estimatedCap)
		originsPtr = &origins
	}
	var fontRefs []int
	var fontRefsPtr *[]int
	if record.fontRefs {
		fontRefs = make([]int, 0, estimatedCap)
		fontRefsPtr = &fontRefs
	}
	var runs *runRecorder
	if record.runFonts {
		runs = &runRecorder{fonts: make([]runFont, 0, estimatedCap)}
	}
	resources := page.DrawResources
	if record.merged {
		resources = page.Resources
	}
	if err := extractTextRecursive(ctx, doc, machine, resources, ops, &elements, make(map[int]bool), 0, annotAppearances, originsPtr, fontRefsPtr, runs); err != nil {
		return extraction{}, err
	}

	if doc != nil {
		doc.Logger.Debug("text elements extracted",
			slog.Int("count", len(elements)),
			slog.Int("operators", len(ops)),
			slog.Int("annot_appearances_skipped", len(annotAppearances)),
		)
	}

	// Flag elements that fall outside the CropBox (the visible region).
	// The check uses the glyph's start point, matching PDF viewers
	// that reject a glyph when its text-matrix position sits past the
	// right or bottom edge of the page. A glyph past the right edge stays
	// out even when it continues a word inside: upright text advances
	// rightward from its origin, so the whole glyph paints outside the
	// visible region (ISO 32000-1, 14.11.2), and PDF.js drops it on the
	// same test (src/core/evaluator.js, compareWithLastPosition).
	//
	// When a page declares a /CropBox that lies entirely outside the
	// /MediaBox, the stated clip is nonsensical and viewers fall back to
	// the /MediaBox for visibility. Treating the empty-intersection case
	// as "no clip" recovers text from the page rather than discarding
	// every glyph as out-of-bounds.
	cropBox := boxRectFromArray(page.CropBox)
	mediaBox := boxRectFromArray(page.MediaBox)
	if cropBox != [4]float64{} && rectsIntersect(cropBox, mediaBox) {
		minX, minY, maxX, maxY := cropBox[0], cropBox[1], cropBox[2], cropBox[3]
		if minX > maxX {
			minX, maxX = maxX, minX
		}
		if minY > maxY {
			minY, maxY = maxY, minY
		}
		for i := range elements {
			if elements[i].X < minX || elements[i].X > maxX ||
				elements[i].Y < minY || elements[i].Y > maxY {
				elements[i].IsClipped = true
			}
		}
	}

	var runFonts []runFont
	if runs != nil {
		runFonts = runs.fonts
	}
	return extraction{elements: elements, origins: origins, fontRefs: fontRefs, runFonts: runFonts}, nil
}

// extractionRecord names what [extractElements] records beside the
// elements, and which resources the page's names resolve in.
type extractionRecord struct {
	origins  bool
	fontRefs bool
	runFonts bool
	// merged resolves the page's names in [document.Page.Resources], the
	// merge of every ancestor's, rather than in the dictionary a viewer
	// draws with, [document.Page.DrawResources]. A caller removing or
	// measuring content reads what any lenient viewer may draw, so a
	// redaction misses no text a viewer resolving past the page's own
	// /Resources shows.
	merged bool
}

// extraction is what [extractElements] returns: the elements and, parallel
// to them, whatever its [extractionRecord] asked for (nil otherwise).
type extraction struct {
	elements []models.TextElement
	origins  []int
	fontRefs []int
	runFonts []runFont
}

// OriginUnattributable marks an element whose bytes are not in the operations
// its origins were taken over. A form XObject paints from its own content
// stream, so no offset into the invoking stream addresses the glyph it drew.
const OriginUnattributable = -1

// FontUnattributable marks an element whose font has no object number of its
// own, because the page's /Font resource holds the font dictionary directly
// rather than naming it (ISO 32000-1, 7.3.10 allows either).
//
// A caller deciding whether a font is left carrying nothing but removed text
// identifies fonts by object number, so one without a number cannot be judged
// and must not be mistaken for a font that was never used. It is a separate
// constant from [OriginUnattributable] because the two say different things: an
// unattributable origin means the bytes are elsewhere, an unattributable font
// means there is no object to point at.
const FontUnattributable = -1

// ExtractElements extracts text from a page's content [content.Operation]
// values as raw spatial [models.TextElement] entries. The page's names
// resolve in [document.Page.DrawResources], as a viewer draws them.
func ExtractElements(ctx context.Context, doc *document.Document, machine *graphics.Machine, page *document.Page, ops []content.Operation) ([]models.TextElement, error) {
	ex, err := extractElements(ctx, doc, machine, page, ops, extractionRecord{})
	return ex.elements, err
}

// ExtractElementsWithOrigins extracts elements and, for each, the index in ops
// of the operation that drew it. The two slices are the same length.
//
// Pair it with [content.ParseStreamDataWithOffsets] over the bytes ops was
// parsed from: the ranges it returns are parallel to ops, so an element's
// origin indexes straight into them and a caller can reach the bytes that drew
// the element. Offsets taken over a different assembly of the page address
// different bytes.
//
// An element painted inside a form XObject reports [OriginUnattributable],
// because its bytes live in the form's stream rather than in ops. A caller
// removing content has to refuse such an element: excising the Do that invoked
// the form would take everything else the form paints with it.
//
// The origins describe these elements as returned. [CoalesceElements] merges
// and drops entries, so origins do not survive it; correlate first.
//
// The page's names resolve in [document.Page.Resources], the merge of every
// ancestor's: a caller editing content by its origins reaches all a lenient
// viewer may draw.
func ExtractElementsWithOrigins(ctx context.Context, doc *document.Document, machine *graphics.Machine, page *document.Page, ops []content.Operation) ([]models.TextElement, []int, error) {
	ex, err := extractElements(ctx, doc, machine, page, ops, extractionRecord{origins: true, merged: true})
	return ex.elements, ex.origins, err
}

// ExtractElementsWithAttribution extracts elements and, for each, both the
// index in ops of the operation that drew it and the object number of the font
// dictionary it was drawn with. All three slices are the same length.
//
// The font reference is what tells a caller which embedded font a glyph came
// from, which a redaction needs to judge whether a font is left describing
// nothing but removed text. A resource name cannot answer that: /F1 is scoped
// to one page's resources, so two pages may bind it to different fonts.
//
// An element drawn inside a form XObject reports [OriginUnattributable] and
// [FontUnattributable]: its bytes are in the form's own stream, and the form
// selects its fonts from its own resources. The page's names resolve as
// [ExtractElementsWithOrigins] resolves them.
func ExtractElementsWithAttribution(ctx context.Context, doc *document.Document, machine *graphics.Machine, page *document.Page, ops []content.Operation) ([]models.TextElement, []int, []int, error) {
	ex, err := extractElements(ctx, doc, machine, page, ops, extractionRecord{origins: true, fontRefs: true, merged: true})
	return ex.elements, ex.origins, ex.fontRefs, err
}

// CoalesceElements merges consecutive per-glyph [models.TextElement]
// entries that share styling and marked-content context and sit on the
// same baseline, then drops runs that carry only whitespace.
// Reader.ExtractTextElements applies this so the public API returns
// text-run-level items rather than per-glyph fragments; internal
// callers that need per-glyph positions (text reconstruction, bidi,
// crop-clip) keep using the raw output from [ExtractElements]. The
// context is checked periodically so large pages can be cancelled.
func CoalesceElements(ctx context.Context, in []models.TextElement) []models.TextElement {
	coalesced := coalesceTextElements(ctx, in)
	out := coalesced[:0]
	for i, el := range coalesced {
		if i%coalesceCancelStride == 0 && ctx.Err() != nil {
			return coalesced
		}
		if strings.TrimSpace(el.Text) == "" {
			continue
		}
		out = append(out, el)
	}
	return out
}

// coalesceCancelStride is the number of elements to process between
// ctx.Err() checks inside the coalesce and filter loops. Text pages
// routinely carry thousands of glyphs; a per-iteration check would add
// measurable overhead, while a stride this size keeps latency bounded
// on cancellation without changing the common-case fast path.
const coalesceCancelStride = 512

// coalesceTextElements merges consecutive per-glyph [models.TextElement]
// entries that share font size, direction, vertical mode, marked-content
// context, visibility flags, and are spatially adjacent along the
// baseline. The raw emission path produces one element per glyph byte,
// which inflates item counts and fragments text layout for downstream
// consumers. Grouping glyphs that came from the same text-showing
// operator restores run-level granularity for the public API without
// losing the per-glyph positions accumulated elsewhere in the pipeline
// (text-reconstruction, bidi, crop-clip).
func coalesceTextElements(ctx context.Context, in []models.TextElement) []models.TextElement {
	if len(in) < 2 {
		return in
	}
	out := make([]models.TextElement, 0, len(in))
	out = append(out, in[0])
	for i := 1; i < len(in); i++ {
		if i%coalesceCancelStride == 0 && ctx.Err() != nil {
			return append(out, in[i:]...)
		}
		last := &out[len(out)-1]
		next := in[i]
		if !canMergeTextElements(*last, next) {
			out = append(out, next)
			continue
		}
		last.Text += next.Text
		last.InkBox = [4]float64{
			min(last.InkBox[0], next.InkBox[0]), min(last.InkBox[1], next.InkBox[1]),
			max(last.InkBox[2], next.InkBox[2]), max(last.InkBox[3], next.InkBox[3]),
		}
		// Width is the total advance from the run's starting position
		// to the end of the final glyph, measured along the text
		// baseline direction (cos, sin of the rotation angle).
		dirX, dirY := baselineDirection(*last)
		dx := (next.X + next.Width*dirX) - last.X
		dy := (next.Y + next.Width*dirY) - last.Y
		last.Width = dx*dirX + dy*dirY
	}
	return out
}

// canMergeTextElements reports whether two consecutive per-glyph
// elements belong to the same run and can be concatenated. Mergeable
// elements share every style and visibility property and sit along the
// expected baseline advance with a sub-space gap; anything larger is a
// legitimate word boundary and stays split.
func canMergeTextElements(a, b models.TextElement) bool {
	if a.FontSize != b.FontSize ||
		a.Vertical != b.Vertical ||
		a.InkRight != b.InkRight ||
		a.Hidden != b.Hidden ||
		a.Invisible != b.Invisible ||
		a.Redacted != b.Redacted ||
		a.IsClipped != b.IsClipped ||
		a.MCID != b.MCID ||
		a.PatternType != b.PatternType ||
		a.Language != b.Language ||
		a.ExpansionText != b.ExpansionText ||
		a.MathML != b.MathML {
		return false
	}
	if math.Abs(a.Angle-b.Angle) > 1e-6 {
		return false
	}

	// Spatial adjacency projected onto the baseline direction. The
	// along-baseline gap must be small (sub-space kerning only) and the
	// perpendicular offset must be near zero. Both tolerances are lengths,
	// so they take magnitudes: a negative font size or horizontal scaling
	// reverses the advance (ISO 32000-1, 9.4.4), which Angle carries, and
	// signs the size and the space advance with it.
	size := math.Abs(a.FontSize)
	spaceW := math.Abs(a.SpaceWidth)
	if spaceW == 0 {
		spaceW = size * 0.25
	}
	const kernTolerance = 0.3 // up to 30% of a space counts as kerning.
	maxGap := spaceW * kernTolerance
	baselineTolerance := size * 0.2

	dirX, dirY := baselineDirection(a)
	perpX, perpY := -dirY, dirX

	dx := b.X - a.X
	dy := b.Y - a.Y
	alongDelta := dx*dirX + dy*dirY
	perpDelta := dx*perpX + dy*perpY

	expected := a.Width
	if alongDelta < expected-baselineTolerance || alongDelta > expected+maxGap {
		return false
	}
	if math.Abs(perpDelta) > baselineTolerance {
		return false
	}
	return true
}

// baselineDirection returns the unit vector pointing along the text
// baseline for an element. Vertical text advances down the Y axis of
// text space; horizontal text advances along its rotation angle.
func baselineDirection(e models.TextElement) (float64, float64) {
	if e.Vertical {
		return math.Sin(e.Angle), math.Cos(e.Angle)
	}
	return math.Cos(e.Angle), math.Sin(e.Angle)
}

// boxRectFromArray converts a PdfArray [llx, lly, urx, ury] to [4]float64.
func boxRectFromArray(arr ast.PdfArray) [4]float64 {
	if len(arr) < 4 {
		return [4]float64{}
	}
	return [4]float64{document.Float(arr[0]), document.Float(arr[1]), document.Float(arr[2]), document.Float(arr[3])}
}

// rectsIntersect reports whether two PDF box rectangles (given as
// [llx, lly, urx, ury] quadruples with possibly swapped corners) share
// any area. A zero rect on the second operand is treated as unbounded:
// callers routinely pass a missing /MediaBox as the empty [0 0 0 0]
// rect, and the intersection check must not reject the page because the
// page tree never declared a media box.
func rectsIntersect(a, b [4]float64) bool {
	if b == [4]float64{} {
		return true
	}
	aMinX, aMaxX := a[0], a[2]
	if aMinX > aMaxX {
		aMinX, aMaxX = aMaxX, aMinX
	}
	aMinY, aMaxY := a[1], a[3]
	if aMinY > aMaxY {
		aMinY, aMaxY = aMaxY, aMinY
	}
	bMinX, bMaxX := b[0], b[2]
	if bMinX > bMaxX {
		bMinX, bMaxX = bMaxX, bMinX
	}
	bMinY, bMaxY := b[1], b[3]
	if bMinY > bMaxY {
		bMinY, bMaxY = bMaxY, bMinY
	}
	return aMinX < bMaxX && aMaxX > bMinX && aMinY < bMaxY && aMaxY > bMinY
}

// ExtractText extracts text from a page's content operations and reconstructs it via [ReconstructText].
// The page's names resolve as [ExtractElements] resolves them.
func ExtractText(ctx context.Context, doc *document.Document, machine *graphics.Machine, page *document.Page, ops []content.Operation, config ExtractionConfig) (string, error) {
	machine.StripArtifacts = config.StripArtifacts

	ex, err := extractElements(ctx, doc, machine, page, ops, extractionRecord{runFonts: true})
	if err != nil {
		return "", err
	}
	elements, runFonts := ex.elements, ex.runFonts

	// Filter elements based on config, keeping each element's run font
	// beside it. Fonts that do not pair one to one with the elements are
	// dropped rather than misattributed.
	if len(runFonts) != len(elements) {
		runFonts = nil
	}
	var filtered []models.TextElement
	var filteredFonts []runFont
	for i, el := range elements {
		if config.StripInvisible && el.Invisible {
			continue
		}
		if config.StripHidden && el.Hidden {
			continue
		}
		if config.StripClipped && el.IsClipped {
			continue
		}
		filtered = append(filtered, el)
		if runFonts != nil {
			filteredFonts = append(filteredFonts, runFonts[i])
		}
	}

	if doc != nil {
		doc.Logger.Debug("text elements filtered",
			slog.Int("before", len(elements)),
			slog.Int("after", len(filtered)),
			slog.Bool("strip_invisible", config.StripInvisible),
			slog.Bool("strip_hidden", config.StripHidden),
			slog.Bool("strip_clipped", config.StripClipped),
			slog.Bool("strip_artifacts", config.StripArtifacts),
		)
	}

	var pageBeads []semantics.Bead
	allThreads, _ := semantics.ResolveThreads(ctx, doc) // Non-fatal: proceed without thread reordering on error.
	for _, thread := range allThreads {
		for _, bead := range thread.Beads {
			if bead.PageRef.ObjectNum == page.Ref.ObjectNum && bead.PageRef.GenNum == page.Ref.GenNum {
				pageBeads = append(pageBeads, bead)
			}
		}
	}

	// Article threads are a viewer-navigation construct intended for
	// click-through reading, not for text extraction. Honour them as a
	// reading-order hint only when every text element on the page sits
	// inside some bead; a partial thread (a sidebar bead alone, for
	// example) would otherwise drag elements outside any bead to the
	// end of the output and place the sidebar before the main body.
	// When coverage is incomplete the page is reconstructed in
	// content-stream order instead.
	if !beadsCoverAllElements(filtered, pageBeads) {
		pageBeads = nil
	}

	return reconstructText(ctx, filtered, filteredFonts, pageBeads), nil
}

// beadsCoverAllElements reports whether every element in elements
// falls inside at least one bead's rectangle. Used as the gate on
// thread-driven reading-order reconstruction.
func beadsCoverAllElements(elements []models.TextElement, beads []semantics.Bead) bool {
	if len(beads) == 0 || len(elements) == 0 {
		return false
	}
	for _, el := range elements {
		if !elementInsideAnyBead(el, beads) {
			return false
		}
	}
	return true
}

func elementInsideAnyBead(el models.TextElement, beads []semantics.Bead) bool {
	for _, bead := range beads {
		if beadContains(bead, el) {
			return true
		}
	}
	return false
}

// riseOnPage returns how far up the page the text rise of state lifts a
// glyph: the image of (0, Trise) in text space under Tm and the CTM, less
// that of the origin (ISO 32000-1, 9.4.4).
func riseOnPage(state *graphics.State) float64 {
	tm, ctm := state.Text.Tm, state.CTM
	return state.Text.Ts * (tm[2]*ctm[1] + tm[3]*ctm[3])
}

// renderingMatrixOnPage returns the linear part [a b c d] of the text
// rendering matrix of state, [Tfs×Th 0 0 Tfs] × Tm × CTM (ISO 32000-1,
// 9.4.4): the image of the text space x axis, then of its y axis.
func renderingMatrixOnPage(state *graphics.State) [4]float64 {
	tm, ctm := state.Text.Tm, state.CTM
	sx := state.Text.Tfs * state.Text.Tz / 100
	sy := state.Text.Tfs
	a, b := sx*tm[0], sx*tm[1]
	c, d := sy*tm[2], sy*tm[3]
	return [4]float64{
		a*ctm[0] + b*ctm[2], a*ctm[1] + b*ctm[3],
		c*ctm[0] + d*ctm[2], c*ctm[1] + d*ctm[3],
	}
}

// heightOnPage returns the size of the font of state across the line on
// the page: the length of the image of the text space y axis under Tm and
// the CTM, times the Tf size (ISO 32000-1, 9.4.4). PDF.js measures a line
// change against this size (src/core/evaluator.js, ensureTextContentItem).
func heightOnPage(state *graphics.State) float64 {
	tm, ctm := state.Text.Tm, state.CTM
	return math.Abs(state.Text.Tfs) * math.Hypot(tm[2]*ctm[0]+tm[3]*ctm[2], tm[2]*ctm[1]+tm[3]*ctm[3])
}
