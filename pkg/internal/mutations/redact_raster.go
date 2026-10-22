package mutations

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// maxRasterFormCopies bounds the copies of forms showing text that the raster
// fallback edits on one page: one per placement of a form whose glyphs a
// region covers, a nested form's included. Anti-DoS guard, not spec-mandated:
// a form drawing a form twice at each of its levels reaches 2^depth
// placements within the form-depth budget, each a parse and an extraction.
// Real pages place a handful; a page past the cap is refused.
const maxRasterFormCopies = 1024

// RasterFunc draws the units of page that begin at the operations ops, each
// the Do of a form XObject or an sh, as images of the page that paint nothing
// inside the regions and hold nothing of them, removing the units from the
// content (see [RedactOptions.Rasterise]). regions are [llx, lly, urx, ury]
// in page space.
type RasterFunc func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page, ops []int, regions [][4]float64) (RasterResult, error)

// RasterResult is what a [RasterFunc] did to a page.
type RasterResult struct {
	// Units counts the painting elements whose painting the images took
	// over: the units asked for, and those drawn among them that the images
	// had to take with them.
	Units int
	// Images holds the object numbers of the images drawn, which already
	// hold nothing of the regions.
	Images []int
	// TextForms lists the forms put in place of the removed units that
	// showed text: copies holding only that text, each drawn once.
	TextForms []TextFormPlacement
}

// TextFormPlacement is a copy of a form holding only its text, drawn once on
// the page: its reference, and the CTM in force at the Do that draws it.
type TextFormPlacement struct {
	Ref ast.PdfIndirectRef
	CTM [6]float64
}

// RedactOptions configures [RedactPageRegionsWith].
type RedactOptions struct {
	// Rasterise, when not nil, is the raster fallback. A page whose
	// redaction meets content drawn by a form XObject
	// ([ErrRegionInFormXObject]), an image among it, or a shading paint
	// ([ErrRegionCoversAShading]) has those units drawn by Rasterise instead:
	// it composites them, with everything they must take with them, into
	// opaque images of the page cut out of the regions, and keeps the text
	// of a form as a copy holding only its text. The redaction then
	// removes the covered glyphs of those copies and runs on the page as
	// rewritten. Nil refuses such a page.
	Rasterise RasterFunc
	// PrivateCopies names the image objects to blank in a copy rather than
	// in place. Blanking in place changes every placement of the image, so
	// it is exact only when every placement had the same samples covered,
	// which [SharedBlankedImages] decides across the pages of a redaction.
	// An image named here is copied instead: the copy's covered samples are
	// blanked, the copy joins the page's resources under a fresh name, and
	// only the Do the regions cover is repointed at it (see
	// [blankPrivateCopies]). Every other placement keeps painting the image
	// as it was, and each covered placement gets a copy of its own. A
	// covered placement a form XObject draws has no Do in the page's stream
	// to repoint, and reports [ErrImageShared].
	PrivateCopies map[int]bool
}

// RedactPageRegionsWith is [RedactPageRegions] with options; see
// [RedactOptions].
//
// The fallback runs only on a page the plain redaction refuses for content a
// form draws or a shading paint, and rasterises only the units that drew what
// the regions select there (see [rasterSeeds]), and those drawn among them
// that an opaque image over them would otherwise hide or misorder. ISO
// 32000-2, 8.10.1 gives a form's content no byte range in the page's stream
// and 8.7.4.2 gives a shading paint no geometry to cut, so no excision
// removes them exactly; an opaque image of what they painted, cut out of the
// regions, replaces their ink without keeping what lay inside them. Text is never rasterised: it stays text, and the glyphs the regions
// cover are removed from it as from the page's own.
func RedactPageRegionsWith(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	regions [][4]float64,
	opts RedactOptions,
) (RedactReport, error) {
	// Blanking an image queues its replacement before the remaining checks run,
	// and a refusal after that has to leave nothing behind: a refused redaction
	// registers no objects at all, or a caller that handled the error would
	// still write a half-edited document. Restore is lossless here because
	// every edit is a fresh object handed to AllocAt, never an AST mutated in
	// place, which is the condition writer.ObjAllocator.Snapshot documents.
	snap := alloc.Snapshot()
	report, err := redactPageRegions(ctx, alloc, doc, page, regions,
		pageRedaction{formImages: opts.Rasterise != nil, private: opts.PrivateCopies})
	if err != nil && opts.Rasterise != nil && rasterable(err) {
		alloc.Restore(snap)
		report, err = rasterThenRedact(ctx, alloc, doc, page, regions, opts)
	}
	if err != nil {
		alloc.Restore(snap)
		return RedactReport{}, err
	}
	return report, nil
}

// rasterable reports whether err is a refusal the raster fallback lifts.
func rasterable(err error) bool {
	return errors.Is(err, ErrRegionInFormXObject) || errors.Is(err, ErrRegionCoversAShading)
}

// rasterThenRedact runs the raster fallback on page, then the redaction on
// the page it leaves.
func rasterThenRedact(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	regions [][4]float64,
	opts RedactOptions,
) (RedactReport, error) {
	page = latestPage(ctx, alloc, doc, page)
	zones := regionZones(regions)
	seeds, err := rasterSeeds(ctx, alloc, doc, page, zones)
	if err != nil {
		return RedactReport{}, err
	}
	if len(seeds) == 0 {
		// The plain redaction refused, and nothing here names what it
		// refused over: refusing again beats rewriting the page for nothing.
		return RedactReport{}, fmt.Errorf("%w: no unit to rasterise", ErrRegionInFormXObject)
	}
	raster, err := opts.Rasterise(ctx, alloc, doc, page, seeds, regions)
	if err != nil {
		return RedactReport{}, fmt.Errorf("rasterising %d unit(s): %w", len(seeds), err)
	}
	var formText textRemoval
	budget := maxRasterFormCopies
	for _, placement := range raster.TextForms {
		if err := redactFormText(ctx, alloc, doc, placement.Ref, placement.CTM, zones, 1, &budget, &formText); err != nil {
			return RedactReport{}, err
		}
	}
	composites := make(map[int]bool, len(raster.Images))
	for _, n := range raster.Images {
		composites[n] = true
	}
	report, err := redactPageRegions(ctx, alloc, doc, page, regions,
		pageRedaction{composites: composites, formImages: true, private: opts.PrivateCopies})
	if err != nil {
		return RedactReport{}, err
	}
	report.RemovedText = append(report.RemovedText, formText.text...)
	report.RemovedGlyphBoxes = append(report.RemovedGlyphBoxes, formText.boxes...)
	report.RasterisedUnits = raster.Units
	if len(report.DroppedPageEntries) == 0 {
		// The page changed even where the redaction above found nothing left
		// to remove, so its own copies go as they would for any edit.
		report.DroppedPageEntries = stripPageCopies(alloc, page)
	}
	return report, nil
}

// rasterSeeds returns, ascending, the page-level operations whose content
// the zones select and the redaction cannot remove: each Do of a form
// XObject that drew a covered glyph, a covered painted path or a covered
// image (none of which has bytes in the page's stream, and an image a form
// draws cannot be blanked for that placement alone), and each sh,
// on the page or in a form, whose clip meets a zone (see
// [ErrRegionCoversAShading]). Selection is the redaction's own: a glyph's
// box, a path's geometry, an image's placement, a shading's clip bounds.
func rasterSeeds(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page, zones []heuristics.Zone) ([]int, error) {
	data, err := collectLatestPageContents(ctx, alloc, doc, page)
	if err != nil {
		return nil, fmt.Errorf("assembling page content: %w", err)
	}
	ops, err := content.ParseStreamData(ctx, data, content.SplitGluedOperators())
	if err != nil {
		return nil, fmt.Errorf("parsing page content: %w", err)
	}
	machine := graphics.NewMachine(pageBox(ctx, alloc, doc, page))
	machine.IgnoreOCVisibility = true
	machine.RecordImageBoxes = true
	machine.RecordPathBoxes = true
	machine.RecordShadingBoxes = true
	machine.Staged = alloc.Pending()
	elements, attr, err := typography.ExtractElementsAttributed(ctx, doc, machine, page, ops)
	if err != nil {
		return nil, fmt.Errorf("extracting page text: %w", err)
	}
	meets := func(box [4]float64) bool {
		painted := heuristics.ZoneFromRect(box)
		for _, z := range zones {
			if heuristics.ZonesOverlap(painted, z) {
				return true
			}
		}
		return false
	}
	seeds := make(map[int]bool)
	seed := func(op int) {
		if op >= 0 && op < len(ops) && (ops[op].Operator == "Do" || ops[op].Operator == "sh") {
			seeds[op] = true
		}
	}
	for i, el := range elements {
		if attr.Origins[i] != typography.OriginUnattributable || i >= len(attr.PageOps) {
			continue
		}
		if meets(glyphBox(heuristics.ElementZone(el))) {
			seed(attr.PageOps[i])
		}
	}
	for _, box := range machine.PathBoxes() {
		if box.FormDepth > 0 && meets([4]float64{box.MinX, box.MinY, box.MaxX, box.MaxY}) {
			seed(box.PageOp)
		}
	}
	for _, box := range machine.ImageBoxes() {
		if box.FormDepth > 0 && meets([4]float64{box.MinX, box.MinY, box.MaxX, box.MaxY}) {
			seed(box.PageOp)
		}
	}
	pageOps := machine.ShadingPageOps()
	for k, box := range machine.ShadingBoxes() {
		if k < len(pageOps) && meets([4]float64{box.MinX, box.MinY, box.MaxX, box.MaxY}) {
			seed(pageOps[k])
		}
	}
	return slices.Sorted(maps.Keys(seeds)), nil
}

// textRemoval collects what [redactFormText] removed.
type textRemoval struct {
	text  []string
	boxes [][4]float64
}

// redactFormText removes the glyphs the zones cover from the form ref, a
// copy holding only text that one Do draws under ctm, as the page's own are
// removed (see [selectText]), and writes it back in place. A form it draws
// whose glyphs the zones cover is copied first, so the edit reaches that
// placement alone, and edited the same way; depth is how deep ref sits below
// the page, and budget counts the copies left to make.
//
// The marked content of a form belongs to the form's own /StructParents
// entry, not to the page's identifiers, so its removed identifiers are not
// scrubbed from the structure tree here: a copy of the text the tree holds is
// left for the document-wide scan, which refuses it.
func redactFormText(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	ref ast.PdfIndirectRef,
	ctm [6]float64,
	zones []heuristics.Zone,
	depth int,
	budget *int,
	out *textRemoval,
) error {
	if depth > doc.Limits().WithDefaults().MaxFormDepth {
		return fmt.Errorf("%w: a form showing text nests past the form-depth budget", ErrRegionInFormXObject)
	}
	obj, err := resolveAllocFirst(ctx, alloc, doc, ref)
	if err != nil {
		return fmt.Errorf("resolving a form showing text: %w", err)
	}
	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return fmt.Errorf("%w: object %d is no form", ErrRegionInFormXObject, ref.ObjectNum)
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return fmt.Errorf("decoding a form showing text: %w", err)
	}
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.SplitGluedOperators())
	if err != nil {
		return fmt.Errorf("parsing a form showing text: %w", err)
	}
	if i := ambiguousTextPosition(ops); i >= 0 {
		return fmt.Errorf("%w: operation %d of a form", ErrTextPositionAmbiguous, i)
	}
	resources, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	formCTM := [6]float64(formMatrix(ctx, doc, stream.Dict).Mul(geom.Matrix(ctm)))

	machine := graphics.NewMachine([4]float64{})
	machine.Current().CTM = formCTM
	machine.IgnoreOCVisibility = true
	machine.RecordShowAdvances = true
	machine.RecordGlyphAdvances = true
	machine.Staged = alloc.Pending()
	// The form is read as a page of its own: its operations are the ones
	// the glyphs are attributed to, and its names resolve in its resources.
	pseudo := &document.Page{Dict: ast.PdfDict{}, Resources: resources, DrawResources: resources}
	elements, attr, err := typography.ExtractElementsAttributed(ctx, doc, machine, pseudo, ops)
	if err != nil {
		return fmt.Errorf("extracting a form's text: %w", err)
	}
	nested := make(map[int]bool)
	text, err := selectText(ctx, ops, elements, attr, machine, zones, func(_ models.TextElement, pageOp int) error {
		if pageOp < 0 || pageOp >= len(ops) || ops[pageOp].Operator != "Do" {
			return fmt.Errorf("%w: a glyph no Do of the form drew", ErrRegionInFormXObject)
		}
		nested[pageOp] = true
		return nil
	})
	if err != nil {
		return err
	}
	edits, err := text.edits(ctx, ops, ranges, machine)
	if err != nil {
		return err
	}
	out.text = append(out.text, text.removedText...)
	out.boxes = append(out.boxes, text.glyphBoxes...)

	newResources := maps.Clone(resources)
	if len(nested) > 0 {
		xobjects, err := document.ResolveDict(ctx, doc, machine.StagedObject(resources["XObject"]))
		if err != nil || xobjects == nil {
			return fmt.Errorf("%w: a nested form showing text does not resolve", ErrRegionInFormXObject)
		}
		xobjects = maps.Clone(xobjects)
		ctms := doCTMs(ctx, ops, formCTM, resources, doc)
		for _, i := range slices.Sorted(maps.Keys(nested)) {
			*budget--
			if *budget < 0 {
				return fmt.Errorf("%w: more than %d placements of forms showing text", ErrRegionInFormXObject, maxRasterFormCopies)
			}
			var name ast.PdfName
			if len(ops[i].Operands) == 1 {
				name, _ = ops[i].Operands[0].(ast.PdfName)
			}
			if name == "" {
				return fmt.Errorf("%w: operation %d of a form names no XObject", ErrRegionInFormXObject, i)
			}
			innerObj, err := resolveAllocFirst(ctx, alloc, doc, xobjects[name])
			if err != nil {
				return fmt.Errorf("resolving a nested form showing text: %w", err)
			}
			inner, ok := innerObj.(ast.PdfStream)
			if !ok {
				return fmt.Errorf("%w: XObject %q is no form", ErrRegionInFormXObject, string(name))
			}
			innerData, err := doc.DecodeStream(ctx, inner)
			if err != nil {
				return fmt.Errorf("decoding a nested form showing text: %w", err)
			}
			// A copy of its own, so removing glyphs for this placement leaves
			// every other placement of the form as it was.
			copied := alloc.Alloc(ast.NewStream(plainStreamDict(inner.Dict, len(innerData)), innerData))
			fresh := newXObjectName(xobjects, "Rt")
			xobjects[fresh] = copied
			edits = append(edits, SpanEdit{Span: ranges[i], Replacement: []byte(" /" + string(fresh) + " Do ")})
			if err := redactFormText(ctx, alloc, doc, copied, ctms[i], zones, depth+1, budget, out); err != nil {
				return err
			}
		}
		if newResources == nil {
			newResources = ast.PdfDict{}
		}
		newResources["XObject"] = xobjects
	}
	if len(edits) == 0 {
		return nil
	}
	edited, err := EditSpans(data, edits)
	if err != nil {
		return fmt.Errorf("editing a form showing text: %w", err)
	}
	dict := plainStreamDict(stream.Dict, len(edited))
	if newResources != nil {
		dict["Resources"] = newResources
	}
	if !alloc.AllocAt(ref.ObjectNum, ast.NewStream(dict, edited)) {
		return fmt.Errorf("%w: object %d", ErrRegionInFormXObject, ref.ObjectNum)
	}
	return nil
}

// RasterArtifacts reports whether the raster fallback marks an image it draws
// outside marked content as an artifact: by the rule [BakeAnnotations] marks
// its drawing by, when the catalog holds a structure tree or holds none and
// its metadata claims PDF/UA or WTPDF (see [untaggedArtifacts]).
func RasterArtifacts(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) bool {
	return bakesArtifacts(ctx, alloc, doc)
}

// plainStreamDict returns a copy of dict for a stream of n decoded bytes: no
// filter and its /Length.
func plainStreamDict(dict ast.PdfDict, n int) ast.PdfDict {
	out := maps.Clone(dict)
	if out == nil {
		out = ast.PdfDict{}
	}
	for _, key := range []ast.PdfName{"Filter", "DecodeParms", "DL"} {
		delete(out, key)
	}
	out["Length"] = ast.PdfInt(n)
	return out
}

// newXObjectName returns a name of prefix that dict does not define.
func newXObjectName(dict ast.PdfDict, prefix string) ast.PdfName {
	for n := 1; ; n++ {
		name := ast.PdfName(prefix + strconv.Itoa(n))
		if _, taken := dict[name]; !taken {
			return name
		}
	}
}

// formMatrix returns a form's /Matrix, the identity when it states none
// (ISO 32000-2, 8.10.2, Table 93).
func formMatrix(ctx context.Context, doc *document.Document, dict ast.PdfDict) geom.Matrix {
	m := geom.Identity
	if arr, err := document.ResolveArray(ctx, doc, dict["Matrix"]); err == nil && len(arr) == 6 {
		for k := range 6 {
			m[k] = document.Float(arr[k])
		}
	}
	return m
}

// doCTMs returns the CTM in force at each Do of ops, run from ctm.
func doCTMs(ctx context.Context, ops []content.Operation, ctm [6]float64, resources ast.PdfDict, doc *document.Document) map[int][6]float64 {
	m := graphics.NewMachine([4]float64{})
	m.Doc, m.Resources = doc, resources
	m.Current().CTM = ctm
	out := make(map[int][6]float64)
	for i, op := range ops {
		if i%100 == 0 && ctx.Err() != nil { //nolint:mnd // amortized cancellation check, as the text extraction checks
			return out
		}
		if op.Operator == "Do" {
			out[i] = m.Current().CTM
		}
		// The machine runs every operator it models and steps over the rest;
		// an error is a cancelled ctx, which the caller's next read reports.
		_ = m.ExecuteOne(ctx, op) //nolint:errcheck // Deliberate: an operator the machine cannot model leaves the state as a viewer would.
	}
	return out
}
