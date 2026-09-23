package mutations

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

// RedactPageRegions clears what a page draws inside the given regions: it
// removes the text and filled paths they cover, blanks the samples of any image
// they cover, and writes the remaining content back as the page's only content
// stream. Regions are [llx, lly, urx, ury] in page space.
//
// It reports what it removed. Clearing a page is not clearing a document: the
// same text survives in the structure tree, annotation values, outlines and
// metadata, so a caller hands [RedactReport.RemovedText] to a scan to find the
// copies this did not touch. Which scan depends on how the output is written.
// [ScanForTokens] reads the document as parsed, which answers for the bytes on
// disk and for an incremental update, since that appends and leaves every
// superseded object in the file. [ScanOutputForTokens] resolves through this
// allocator and reaches outward from the roots a rewrite copies, so it answers
// for the file [FullRewrite] would write.
//
// Selection is geometric and visibility-independent: a glyph counts when the
// box it paints meets a region, whatever its colour, its text rendering mode,
// or the optional-content group it sits in. Invisible text is exactly what a
// failed redaction leaves behind, so removal cannot key on whether a viewer
// shows it.
//
// It removes operators, so it refuses whatever it cannot remove exactly:
//
//   - A region covering only part of what one operator draws reports
//     [ErrPartialOperatorCoverage]. Excision deletes byte ranges and cannot
//     rewrite an operator, so removing that one would take the rest of its text.
//   - A painted path reaching outside the region reports
//     [ErrPathStraddlesRegion], one painted by a stroking operator reports
//     [ErrRegionCoversAStrokedPath], and one a W or W* took part in reports
//     [ErrRegionCoversAClippedPath].
//   - A region selecting content a form XObject draws reports
//     [ErrRegionInFormXObject], whose bytes are in the form's stream.
//   - An image whose samples cannot be rewritten reports one of
//     [ErrInlineImageInRegion] (its pixels live in the content stream, so there
//     is no object to replace), [ErrImageNotAddressable],
//     [ErrImageNotDecodable], [ErrImagePlacementDegenerate],
//     [ErrImageCarriesSampleEntries] or [ErrImageIsStencilMask], which is the
//     mask carrying /SMask, the one stencil blanking cannot keep a mask.
//   - A region that meets an image but falls between its sample centres reports
//     [ErrImageRegionCoversNoSample]: it covers picture the caller asked to
//     clear while clearing none of it, so returning success would leave that
//     content in the file.
//   - A Do the machine cannot follow reports [ErrXObjectUnresolved], because
//     content it cannot place is content it cannot rule out of the region.
//   - An annotation inside the region that cannot be detached reports
//     [ErrAnnotationNotPlaceable] (an unreadable /Rect cannot be ruled out of
//     the region either), [ErrAnnotationStraddlesRegion] (an annotation is
//     atomic, so removing one reaching outside takes behaviour the caller never
//     selected), [ErrAnnotationIsFormWidget] (a /Parent written inline, or a
//     widget written directly into /Annots, so there is nothing to rewrite) or
//     [ErrFormNotAddressable] (the AcroForm, or the catalog carrying it inline,
//     has no object number for the replacement to take).
//   - A structure element describing both removed and surviving content reports
//     [ErrStructElementPartlyRemoved], and one with no object number of its own
//     reports [ErrStructElementNotAddressable]. The tree holds a second copy of
//     the page's text, so neither may be walked past.
//   - A byte span outside the content reports [ErrSpanOutOfRange], and two
//     spans covering the same bytes report [ErrSpansOverlap]. Either means the
//     parse and the bytes being rewritten have diverged, so the excision would
//     cut whatever occupies those offsets now.
//   - A page with no object number of its own reports [ErrPageNotAddressable].
//
// Refusing beats removing more than was asked, and beats reporting success over
// content that is still in the file.
//
// This removes text and the filled paths a region covers. A stroked or
// clipping path is refused rather than removed, because a stroke's ink spreads
// past the geometry the machine records and a clipping path also constrains
// content outside the region. The document-wide copies of page text (the
// structure tree, annotation values, outlines, metadata) are still untouched,
// so the result is not a redacted document on its own. An image the regions
// cover is blanked rather than removed: one Do carries the whole picture, so
// dropping it would take the parts the caller never selected, while overwriting
// the covered samples clears exactly what was asked. A blanked image comes back
// as 8-bit DeviceRGB under /FlateDecode whatever it arrived as, which is what
// lets an image in a filter litho cannot write be cleared at all.
func RedactPageRegions(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	regions [][4]float64,
) (RedactReport, error) {
	// Blanking an image queues its replacement before the remaining checks run,
	// and a refusal after that has to leave nothing behind: a refused redaction
	// registers no objects at all, or a caller that handled the error would
	// still write a half-edited document. Restore is lossless here because
	// every edit is a fresh object handed to AllocAt, never an AST mutated in
	// place, which is the condition [writer.ObjAllocator.Snapshot] documents.
	snap := alloc.Snapshot()
	report, err := redactPageRegions(ctx, alloc, doc, page, regions)
	if err != nil {
		alloc.Restore(snap)
		return RedactReport{}, err
	}
	return report, nil
}

// RedactReport says what a redaction removed, per vector.
//
// It exists because removing content from a page does not remove the copies of
// it held elsewhere in the file. [ScanForTokens] takes RemovedText and looks for
// those copies, which is the only way to tell a cleared page from a cleared
// document.
type RedactReport struct {
	// RemovedText holds the text of each show-text operator that was removed,
	// one string per operator, in the order the page drew them.
	//
	// One string per operator rather than per glyph: an extracted text element
	// is a single glyph on this path, and a list of single characters would be
	// useless to a scan, since "e" appears in nearly every object of a real
	// file. Runs that decode to nothing are dropped for the same reason an
	// empty token is refused outright.
	RemovedText []string
	// BlankedImages names the image XObjects whose covered samples were
	// overwritten.
	BlankedImages []string
	// RemovedPaths counts the painted paths removed. Paths carry no text, so
	// there is nothing for a scan to look for; the count is for the caller's
	// report of what it did.
	RemovedPaths int
	// RemovedMCIDs lists the marked-content identifiers whose content was
	// removed, ascending. They are what reaches the structure tree: a tagged
	// page keeps a parallel copy of its text there, keyed by these.
	RemovedMCIDs []int
	// DroppedPageEntries names the page entries dropped because they held a
	// copy of what was removed, such as /Thumb.
	DroppedPageEntries []string
	// FontsFullyRemovedOnPage lists the object numbers of fonts whose every
	// glyph on this page was removed, ascending.
	//
	// Page-scoped on purpose, and the distinction matters: it says this page
	// draws nothing with that font any more, not that the document does. The
	// font may still be used elsewhere, and its embedded program still carries
	// the outlines either way. Reporting rather than refusing, because a
	// heading set in its own font and redacted whole is the ordinary case, not
	// a corner one: refusing there would block redaction on everyday documents.
	// A caller that must also strip the outlines has to re-subset the font,
	// which litho cannot do across the three embedded formats yet.
	FontsFullyRemovedOnPage []int
	// RemovedAnnotations lists the object numbers of the annotations dropped
	// because their rectangle sat inside a region, ascending.
	//
	// Only the annotations the page named by reference appear here. One written
	// directly into /Annots is removed just the same, since removing an entry
	// needs no object number, but it has none to report.
	RemovedAnnotations []int
}

func redactPageRegions(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	regions [][4]float64,
) (RedactReport, error) {
	if len(regions) == 0 {
		return RedactReport{}, nil
	}
	if page.Ref.ObjectNum == 0 {
		return RedactReport{}, ErrPageNotAddressable
	}

	// The spans below index this buffer, so the parse and the later excision
	// both have to read the page the same way.
	data, err := collectPageContents(ctx, doc, page)
	if err != nil {
		return RedactReport{}, fmt.Errorf("assembling page content: %w", err)
	}
	if len(data) == 0 {
		return RedactReport{}, nil
	}

	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data)
	if err != nil {
		return RedactReport{}, fmt.Errorf("parsing page content: %w", err)
	}

	machine := graphics.NewMachine(pageBox(page))
	// An optional-content group that is off still holds its content in the
	// file, so its visibility must not gate removal.
	machine.IgnoreOCVisibility = true
	// Where images land decides whether this can proceed at all: one inside the
	// region is content that has to be accounted for, not stepped over.
	machine.RecordImageBoxes = true
	// A filled path inside the region is content to remove, and a stroked or
	// clipping one is content to refuse over. Both need its geometry.
	machine.RecordPathBoxes = true

	elements, origins, fontRefs, err := typography.ExtractElementsWithAttribution(ctx, doc, machine, page, ops)
	if err != nil {
		return RedactReport{}, fmt.Errorf("extracting page text: %w", err)
	}

	// Per-font counts, so a caller can be told which fonts this page no longer
	// draws with once the removal is done.
	fontGlyphs := make(map[int]int)
	fontRemoved := make(map[int]int)

	zones := make([]heuristics.Zone, 0, len(regions))
	for _, r := range regions {
		zones = append(zones, heuristics.ZoneFromRect(r))
	}

	var report RedactReport

	// Page-wide rather than region-scoped on purpose: an XObject that could not
	// be placed is one that cannot be ruled out of the region either. Checked
	// before anything is blanked, because content the machine could not place
	// is a reason to refuse outright rather than to edit and roll back.
	if n := machine.UnresolvedXObjects(); n > 0 {
		return RedactReport{}, fmt.Errorf("%w: %d on this page", ErrXObjectUnresolved, n)
	}

	// An image cannot be cleared by removing operators: a single Do carries the
	// whole picture, so dropping it would take the parts the region never
	// selected. Blanking the covered samples clears exactly what was asked, and
	// refuses when it cannot. See [blankImage].
	for _, box := range machine.ImageBoxes() {
		painted := heuristics.ZoneFromRect([4]float64{box.MinX, box.MinY, box.MaxX, box.MaxY})
		covered := false
		for _, z := range zones {
			if heuristics.ZonesOverlap(painted, z) {
				covered = true
				break
			}
		}
		if !covered {
			continue
		}
		if err := blankImage(ctx, alloc, doc, page, box, zones); err != nil {
			return RedactReport{}, err
		}
		report.BlankedImages = append(report.BlankedImages, box.Name)
	}

	// Before any content is excised, so a refusal here happens while the page is
	// otherwise untouched. An annotation is not drawn by the content stream, so
	// clearing the stream leaves it whole: its /Contents and /A /URI survive,
	// and its /Rect goes on marking exactly where the removed content sat.
	removedAnnots, err := removeRegionAnnotations(ctx, alloc, doc, page, zones)
	if err != nil {
		return RedactReport{}, err
	}
	report.RemovedAnnotations = removedAnnots

	vectorSpans, err := pathSpans(machine.PathBoxes(), ops, ranges, zones)
	if err != nil {
		return RedactReport{}, err
	}
	report.RemovedPaths = len(vectorSpans)

	// Tally each operator's glyphs, because an operator leaves whole or stays
	// whole: every glyph it drew has to agree about the regions.
	//
	// The glyphs are also where the removed text lives. One element is one
	// glyph on this path ([typography.CoalesceElements] merges them into runs
	// and is deliberately not called here), so the run an operator drew is its
	// glyphs joined in the order they were appended.
	type coverage struct {
		inside, outside int
		glyphs          []string
		mcids           []int
		// fontRef is the font every glyph of this operator was drawn with: one
		// show-text operator paints with whatever Tf last selected. It holds
		// [typography.FontUnattributable] until a glyph names a font, so an
		// operator drawn with a font that has no object number is never
		// mistaken for one drawn with object zero.
		fontRef int
	}
	tally := make(map[int]*coverage)

	for i, el := range elements {
		selected := false
		box := heuristics.ElementZone(el)
		for _, z := range zones {
			if heuristics.ZonesOverlap(box, z) {
				selected = true
				break
			}
		}
		origin := origins[i]
		if origin == typography.OriginUnattributable {
			if selected {
				return RedactReport{}, fmt.Errorf("%w: %q", ErrRegionInFormXObject, el.Text)
			}
			continue
		}
		if origin < 0 || origin >= len(ops) {
			return RedactReport{}, fmt.Errorf("element %d reports operation %d, which is not one of %d", i, origin, len(ops))
		}
		c := tally[origin]
		if c == nil {
			c = &coverage{fontRef: typography.FontUnattributable}
			tally[origin] = c
		}
		c.glyphs = append(c.glyphs, el.Text)
		if el.MCID >= 0 {
			c.mcids = append(c.mcids, el.MCID)
		}
		// The three slices are parallel, but guard the index rather than trust
		// it: a mismatch would attribute glyphs to the wrong font silently.
		if i < len(fontRefs) && fontRefs[i] != typography.FontUnattributable {
			c.fontRef = fontRefs[i]
			fontGlyphs[fontRefs[i]]++
		}
		if selected {
			c.inside++
		} else {
			c.outside++
		}
	}

	// Sorted, so a refusal names the same operator every run.
	selected := make([]int, 0, len(tally))
	for origin, c := range tally {
		if c.inside > 0 {
			selected = append(selected, origin)
		}
	}
	sort.Ints(selected)

	spans := make([]content.ByteRange, 0, len(selected)+len(vectorSpans))
	for _, origin := range selected {
		if c := tally[origin]; c.outside > 0 {
			return RedactReport{}, fmt.Errorf("%w: operation %d draws %d glyphs inside and %d outside",
				ErrPartialOperatorCoverage, origin, c.inside, c.outside)
		}
		spans = append(spans, ranges[origin])
		// Counted here rather than from the selected set, so it is reached only
		// after the partial-coverage refusal above: an operator that stays is
		// not a removal.
		if c := tally[origin]; c.fontRef != typography.FontUnattributable {
			fontRemoved[c.fontRef] += c.inside
		}
		// An unresolved glyph decodes to nothing, so a run of only those joins
		// to "". Reporting that as removed text would hand a scan a token
		// contained in every string, which is the same answer as scanning for
		// nothing at all.
		if run := strings.Join(tally[origin].glyphs, ""); run != "" {
			report.RemovedText = append(report.RemovedText, run)
		}
	}
	// A font every one of whose glyphs went is one this page no longer draws
	// with, so its embedded outlines now describe only removed text.
	for fontRef, drawn := range fontGlyphs {
		if drawn > 0 && fontRemoved[fontRef] == drawn {
			report.FontsFullyRemovedOnPage = append(report.FontsFullyRemovedOnPage, fontRef)
		}
	}
	sort.Ints(report.FontsFullyRemovedOnPage)

	// ExcisePageSpans sorts and rejects overlaps, so the two sets need no
	// ordering here. A path run that somehow enclosed a show-text operator
	// would be caught there as an overlap rather than silently merged.
	// Which marked content went and which stayed. An identifier can appear on
	// both sides when it covers operators the region split between, and the
	// scrub has to see that rather than clear an element describing both.
	removedMCIDs := make(map[int]bool)
	survivingMCIDs := make(map[int]bool)
	selectedOrigins := make(map[int]bool, len(selected))
	for _, origin := range selected {
		selectedOrigins[origin] = true
		for _, mcid := range tally[origin].mcids {
			removedMCIDs[mcid] = true
		}
	}
	for origin, c := range tally {
		if selectedOrigins[origin] {
			continue
		}
		for _, mcid := range c.mcids {
			survivingMCIDs[mcid] = true
		}
	}
	for mcid := range removedMCIDs {
		report.RemovedMCIDs = append(report.RemovedMCIDs, mcid)
	}
	sort.Ints(report.RemovedMCIDs)

	// The structure tree holds a second copy of the page's text, so clearing
	// the page alone leaves it readable there.
	if err := scrubStructTreeText(ctx, alloc, doc, page, removedMCIDs, survivingMCIDs); err != nil {
		return RedactReport{}, err
	}

	spans = append(spans, vectorSpans...)
	if len(spans) == 0 {
		// An image may still have been blanked, so the report stands even when
		// no operator was removed, and the page's own copies still have to go.
		if len(report.BlankedImages) > 0 {
			report.DroppedPageEntries = stripPageCopies(alloc, page)
		}
		return report, nil
	}

	if err := ExcisePageSpans(ctx, alloc, doc, page, spans); err != nil {
		return RedactReport{}, err
	}
	// After the content is gone, not before: a page that refused partway
	// through keeps its thumbnail along with everything else. Run last so the
	// alloc-first read picks up the /Contents the excision just queued.
	report.DroppedPageEntries = stripPageCopies(alloc, page)
	return report, nil
}

// pathSpans returns the byte ranges of the painted paths the zones cover, and
// refuses any path it cannot remove exactly.
//
// A path is removed as a whole run, from its first construction operator
// through the paint operator that ended it, so it qualifies only when it sits
// wholly inside one region. Overlapping is what selects it; containment is what
// permits removing it.
func pathSpans(boxes []graphics.PathBox, ops []content.Operation, ranges []content.ByteRange, zones []heuristics.Zone) ([]content.ByteRange, error) {
	spans := make([]content.ByteRange, 0, len(boxes))

	for _, box := range boxes {
		painted := heuristics.ZoneFromRect([4]float64{box.MinX, box.MinY, box.MaxX, box.MaxY})

		selected := false
		for _, z := range zones {
			if heuristics.ZonesOverlap(painted, z) {
				selected = true
				break
			}
		}
		if !selected {
			continue
		}

		// The operators that drew it are in the form's own stream, where no
		// offset into this one reaches them.
		if box.FormDepth > 0 {
			return nil, fmt.Errorf("%w: a painted path", ErrRegionInFormXObject)
		}
		if box.Clipped {
			return nil, fmt.Errorf("%w: operations %d to %d", ErrRegionCoversAClippedPath, box.StartOp, box.EndOp)
		}
		if box.StartOp < 0 || box.StartOp > box.EndOp || box.EndOp >= len(ops) {
			return nil, fmt.Errorf("painted path reports operations %d to %d, which is not a run of the %d parsed",
				box.StartOp, box.EndOp, len(ops))
		}
		if pathPaintStrokes(ops[box.EndOp].Operator) {
			return nil, fmt.Errorf("%w: operation %d paints with %q",
				ErrRegionCoversAStrokedPath, box.EndOp, ops[box.EndOp].Operator)
		}

		contained := false
		for _, z := range zones {
			if heuristics.ZoneContains(z, painted) {
				contained = true
				break
			}
		}
		if !contained {
			return nil, fmt.Errorf("%w: operations %d to %d", ErrPathStraddlesRegion, box.StartOp, box.EndOp)
		}

		spans = append(spans, content.ByteRange{
			Start: ranges[box.StartOp].Start,
			End:   ranges[box.EndOp].End,
		})
	}

	return spans, nil
}

// pathPaintStrokes reports whether a path-painting operator strokes.
//
// Per ISO 32000-1, 8.5.3.2 the stroking painters are S and s, plus B, B*, b and
// b*, which fill and stroke in one operation. Only f, F and f* paint by filling
// alone, and only those keep their ink inside the path geometry the machine
// records.
func pathPaintStrokes(operator string) bool {
	switch operator {
	case "S", "s", "B", "B*", "b", "b*":
		return true
	}
	return false
}

// pageBox returns the page's visible region for the graphics machine,
// preferring the /CropBox and falling back to the /MediaBox. A zero box leaves
// [graphics.NewMachine] to supply its own bounds.
func pageBox(page *document.Page) [4]float64 {
	arr := page.CropBox
	if len(arr) != 4 {
		arr = page.MediaBox
	}
	if len(arr) != 4 {
		return [4]float64{}
	}
	return [4]float64{
		document.Float(arr[0]),
		document.Float(arr[1]),
		document.Float(arr[2]),
		document.Float(arr[3]),
	}
}
