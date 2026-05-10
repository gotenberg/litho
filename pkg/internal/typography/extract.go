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
func fallbackFont() *Font {
	return &Font{
		Subtype:  "Type1",
		Encoding: copyEncoding(encodings.WinAnsiEncoding),
	}
}

// maxFormXObjectDepth is the maximum recursion depth for nested Form XObjects.
// This prevents stack overflows and excessive processing time from circular
// references or deeply nested XObjects in malformed PDFs.
const maxFormXObjectDepth = 10

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
// excessively deep nesting. annotAppearances contains object numbers of
// annotation appearance streams to skip.
//
// This function is a thin dispatcher: the actual per-operator work lives in
// per-operator helpers (handleTfOp, handleShowTextOp, handleTJOp, handleDoOp)
// so that each piece of the extraction state machine can be audited in
// isolation.
func extractTextRecursive(ctx context.Context, doc *document.Document, machine *graphics.Machine, resources ast.PdfDict, ops []content.Operation, elements *[]models.TextElement, visited map[int]bool, depth int, annotAppearances map[int]bool) error {
	if depth > maxFormXObjectDepth {
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

	// Make resources available to the machine for pattern resolution.
	machine.Doc = doc
	machine.Resources = resources

	populateOCsFromResources(ctx, doc, machine, resources)

	var currentFont *Font
	for opIdx, op := range ops {
		// Check for context cancellation every 100 operators to respect
		// deadlines without adding per-operator overhead.
		if opIdx%100 == 0 && opIdx > 0 { //nolint:mnd // amortized cancellation check
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		// Errors from Execute are non-fatal: unknown/unsupported operators
		// return errors but must not abort extraction of the rest.
		_ = machine.ExecuteOne(ctx, op) //nolint:errcheck

		state := machine.Current()

		// PDF.js's text extraction returns the rendered glyphs and
		// does not honour /ActualText (the substitution is reserved
		// for accessibility consumers). Drop any PendingActualText
		// that BDC may have set so it does not affect downstream
		// state, and let the glyph-rendering operators emit normally.
		machine.PendingActualText = ""

		if state.SkipExtraction {
			continue
		}

		switch op.Operator {
		case "Tf":
			currentFont = handleTfOp(ctx, doc, resources, op, currentFont)
		case "Tj":
			if err := handleShowTextOp(ctx, machine, currentFont, op, 0, elements); err != nil {
				return err
			}
		case "'":
			if err := handleShowTextOp(ctx, machine, currentFont, op, 0, elements); err != nil {
				return err
			}
		case "\"":
			if err := handleShowTextOp(ctx, machine, currentFont, op, 2, elements); err != nil {
				return err
			}
		case "TJ":
			if err := handleTJOp(ctx, machine, currentFont, op, elements); err != nil {
				return err
			}
		case "Do":
			if err := handleDoOp(ctx, doc, machine, resources, op, elements, visited, depth, annotAppearances); err != nil {
				return err
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
func handleTfOp(ctx context.Context, doc *document.Document, resources ast.PdfDict, op content.Operation, prev *Font) *Font {
	if len(op.Operands) < 1 {
		return prev
	}
	fontName, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return prev
	}

	if font := resolveFontFromResources(ctx, doc, resources, fontName); font != nil {
		return font
	}

	// Fall back to a default font rather than aborting the page. Text may
	// be garbled, but extraction continues. Best-effort is our contract.
	if doc != nil {
		doc.Logger.Debug("font not found, using fallback",
			slog.String("font", string(fontName)),
		)
	}
	return fallbackFont()
}

// resolveFontFromResources looks up a font name in the resource dict's
// /Font sub-dict, consulting the document's font cache first and
// populating it on cache miss. Returns nil if the font cannot be resolved.
func resolveFontFromResources(ctx context.Context, doc *document.Document, resources ast.PdfDict, fontName ast.PdfName) *Font {
	resObj, ok := resources["Font"]
	if !ok {
		return nil
	}
	resDictRoot, err := document.ResolveDict(ctx, doc, resObj)
	if err != nil {
		doc.Logger.Debug("resolving Font resource dict", slog.Any("err", err))
	}
	if resDictRoot == nil {
		return nil
	}
	fontRefObj, ok := resDictRoot[fontName]
	if !ok {
		return nil
	}

	// Cache hit by indirect reference object number.
	if ref, ok := fontRefObj.(ast.PdfIndirectRef); ok && doc != nil {
		if cached, ok := doc.CachedFont(ref.ObjectNum); ok {
			if f, ok := cached.(*Font); ok {
				return f
			}
		}
	}

	// Cache miss: parse the font dict and store it back.
	fontDict, err := document.ResolveDict(ctx, doc, fontRefObj)
	if err != nil || fontDict == nil {
		return nil
	}
	font, err := ResolveFont(ctx, doc, fontDict)
	if err != nil {
		return nil
	}
	if ref, ok := fontRefObj.(ast.PdfIndirectRef); ok && doc != nil {
		doc.SetCachedFont(ref.ObjectNum, font)
	}
	return font
}

// handleShowTextOp processes Tj ("), and " operators. strIndex selects the
// operand that holds the PdfString: 0 for Tj and ', 2 for the
// word/char-spacing form ".
func handleShowTextOp(ctx context.Context, machine *graphics.Machine, currentFont *Font, op content.Operation, strIndex int, elements *[]models.TextElement) error {
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
	return processText(ctx, str, currentFont, machine, elements)
}

// handleTJOp processes the TJ (show text with kerning) operator. Numeric
// array elements advance the text matrix instead of drawing a glyph, so
// they must be dispatched separately from string elements.
func handleTJOp(ctx context.Context, machine *graphics.Machine, currentFont *Font, op content.Operation, elements *[]models.TextElement) error {
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
			if err := processText(ctx, str, currentFont, machine, elements); err != nil {
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
func handleDoOp(ctx context.Context, doc *document.Document, machine *graphics.Machine, resources ast.PdfDict, op content.Operation, elements *[]models.TextElement, visited map[int]bool, depth int, annotAppearances map[int]bool) error {
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
	xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
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

	stream, err := document.ResolveStream(ctx, doc, targetObj)
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

	formOps, err := content.ParsePageContent(ctx, doc, stream)
	if err != nil {
		return nil
	}

	// Determine the Form's own /Resources; inherit the parent's on absence.
	formRes := resources
	if resObj, ok := stream.Dict["Resources"]; ok {
		resDict, err := document.ResolveDict(ctx, doc, resObj)
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
	_ = machine.Execute(ctx, []content.Operation{{Operator: "q"}}) //nolint:errcheck
	if matrixObj, ok := stream.Dict["Matrix"]; ok {
		matrixArr, err := document.ResolveArray(ctx, doc, matrixObj)
		if err != nil {
			return err
		}
		if len(matrixArr) == 6 {
			_ = machine.Execute(ctx, []content.Operation{{ //nolint:errcheck
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
	err = extractTextRecursive(ctx, doc, machine, formRes, formOps, elements, visited, depth+1, annotAppearances)
	if objNum != 0 {
		delete(visited, objNum)
	}
	if err != nil {
		return err
	}

	_ = machine.Execute(ctx, []content.Operation{{Operator: "Q"}}) //nolint:errcheck
	return nil
}

// ExtractionConfig holds options for text extraction behavior.
type ExtractionConfig struct {
	StripInvisible bool // If true, completely omits text matched by invisibility heuristics (Tr=3, white-on-white)
	StripHidden    bool // If true, completely omits text outside the graphics state clipping path (W/W*)
	StripClipped   bool // If true, completely omits text outside the page CropBox
	StripArtifacts bool // If true, discards text inside /Artifact marked content (headers, footers, page numbers)
}

// ExtractElements extracts text from a page's content [content.Operation] values as raw spatial [models.TextElement] entries.
func ExtractElements(ctx context.Context, doc *document.Document, machine *graphics.Machine, page *document.Page, ops []content.Operation) ([]models.TextElement, error) {
	// Collect annotation appearance object numbers so we can skip them during
	// text extraction. Annotation appearances are visual overlays, not page content.
	annotAppearances := collectAnnotAppearanceObjects(ctx, doc, page.Dict)

	// Pre-allocate with a capacity estimate: roughly 1 text element per 3 operators.
	estimatedCap := max(len(ops)/3,
		//nolint:mnd // minimum capacity to avoid early reallocations
		64)
	elements := make([]models.TextElement, 0, estimatedCap)
	if err := extractTextRecursive(ctx, doc, machine, page.Resources, ops, &elements, make(map[int]bool), 0, annotAppearances); err != nil {
		return nil, err
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
	// right or bottom edge of the page.
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
		// Un-clip a single glyph that continues a non-clipped word past
		// the right edge by a hairline margin. Viewers render a trailing
		// letter whose origin has drifted a few points past maxX when
		// the glyph itself sits on the baseline of its in-bounds
		// neighbour; without this relaxation "Checkliste Service"
		// becomes "Checkliste Servic". The chain is capped at one glyph
		// per non-clipped run and at a small absolute overshoot (a few
		// points), so a long runaway tail cannot propagate back in:
		// ShowText-ShadingPattern repeats "Helvetica" many points past
		// the margin and must stay clipped.
		const edgeOvershoot = 3.0
		originallyClipped := make([]bool, len(elements))
		for i := range elements {
			originallyClipped[i] = elements[i].IsClipped
		}
		for i := 1; i < len(elements); i++ {
			if !originallyClipped[i] {
				continue
			}
			if originallyClipped[i-1] {
				continue
			}
			prev := &elements[i-1]
			if math.Abs(elements[i].Y-prev.Y) > 0.5 {
				continue
			}
			expectedX := prev.X + prev.Width
			if math.Abs(elements[i].X-expectedX) > 0.5 {
				continue
			}
			if elements[i].X > maxX+edgeOvershoot {
				continue
			}
			elements[i].IsClipped = false
		}
	}

	return elements, nil
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
	// perpendicular offset must be near zero.
	spaceW := a.SpaceWidth
	if spaceW <= 0 {
		spaceW = a.FontSize * 0.25
	}
	const kernTolerance = 0.3 // up to 30% of a space counts as kerning.
	maxGap := spaceW * kernTolerance
	baselineTolerance := a.FontSize * 0.2

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
func ExtractText(ctx context.Context, doc *document.Document, machine *graphics.Machine, page *document.Page, ops []content.Operation, config ExtractionConfig) (string, error) {
	machine.StripArtifacts = config.StripArtifacts

	elements, err := ExtractElements(ctx, doc, machine, page, ops)
	if err != nil {
		return "", err
	}

	// Filter elements based on config
	var filtered []models.TextElement
	for _, el := range elements {
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

	return ReconstructText(ctx, filtered, pageBeads), nil
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
		minX, maxX := bead.Rect[0], bead.Rect[2]
		if minX > maxX {
			minX, maxX = maxX, minX
		}
		minY, maxY := bead.Rect[1], bead.Rect[3]
		if minY > maxY {
			minY, maxY = maxY, minY
		}
		if el.X >= minX && el.X <= maxX && el.Y >= minY && el.Y <= maxY {
			return true
		}
	}
	return false
}
