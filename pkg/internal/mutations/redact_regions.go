package mutations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
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
// metadata, so a caller hands [RedactReport.RemovedText] to
// [ScanOutputForTokens] to find the copies this did not touch. That scan
// resolves through this allocator and reaches outward from the roots a rewrite
// copies, so it answers for the file [FullRewrite] would write. It answers for
// no incremental update, which appends and leaves every superseded object, the
// original content stream among them, in the file.
//
// Selection is geometric and visibility-independent: a glyph counts when the
// box it paints meets a region, whatever its colour, its text rendering mode,
// or the optional-content group it sits in. Invisible text is exactly what a
// failed redaction leaves behind, so removal cannot key on whether a viewer
// shows it.
//
// It removes operators, so it refuses whatever it cannot remove exactly:
//
//   - A region covering only some of the glyphs one show-text operator draws
//     splits the operator at its glyphs: the covered ones go and the others
//     stay where they were, positioned by a TJ number standing for the removed
//     run (see splitShowReplacement). An operator that cannot be split exactly
//     reports [ErrPartialOperatorCoverage]: a code read at a length the font's
//     encoding does not give it, text added to the clipping path, or a TJ
//     number under a horizontal scaling other than 100.
//   - A show-text operator removed whole whose glyphs add to the clipping path
//     reports [ErrRegionCoversClippingText], since removing it would widen the
//     clip for everything after it; and a show-text operator kept, whole or
//     split, that shows a code extraction read no glyph for reports
//     [ErrTextNotPlaceable], since that glyph cannot be ruled out of the
//     region. A page showing text after a Q inside a text object that undoes
//     a moved text matrix reports [ErrTextPositionAmbiguous]: viewers place
//     that text in two places.
//   - A removed show-text operator whose advance later text on its line
//     depends on, and which no TJ number can reproduce, reports
//     [ErrTextAdvanceUnknown]. The removal puts that advance back otherwise,
//     so the rest of the line stays where it was.
//   - A painted path reaching outside the region is rewritten (see
//     [RedactReport.ClippedPaths]) unless it cannot be exactly: one it cannot
//     parse, cut or clip reports [ErrPathStraddlesRegion], a stroke whose
//     width cannot be read or whose dashes a cut would move reports
//     [ErrRegionCoversAStrokedPath], and one a W or W* took part in reports
//     [ErrRegionCoversAClippedPath].
//   - A shading paint (sh) whose clip in force the region meets reports
//     [ErrRegionCoversAShading]: it fills that clip and has no geometry to cut.
//   - A region selecting content a form XObject draws reports
//     [ErrRegionInFormXObject], whose bytes are in the form's stream.
//   - An image whose samples cannot be rewritten reports one of
//     [ErrInlineImageInRegion] (an inline image whose bytes or colour space
//     no replacement spliced into the stream can keep, see
//     [blankInlineImage]), [ErrImageNotAddressable],
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
// This removes text and the painted paths a region covers. A path whose ink,
// a stroke's width included, reaches outside the regions loses its geometry
// inside them and paints the rest under a clip that excludes them, so the page
// outside keeps every mark it had. A clipping path is refused rather than
// removed, because it also constrains content outside the region. The
// document-wide copies of page text (the structure tree, annotation values,
// outlines, metadata) are still untouched, so the result is not a redacted
// document on its own. An image the regions
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
	// place, which is the condition writer.ObjAllocator.Snapshot documents.
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
// it held elsewhere in the file. [ScanOutputForTokens] takes RemovedText and
// looks for those copies, which is the only way to tell a cleared page from a
// cleared document.
type RedactReport struct {
	// RemovedText holds the text of each show-text operator that was removed,
	// one string per operator, in the order the page drew them. An operator
	// split at its glyphs gives one string per run of removed glyphs.
	//
	// One string per operator rather than per glyph: an extracted text element
	// is a single glyph on this path, and a list of single characters would be
	// useless to a scan, since "e" appears in nearly every object of a real
	// file. Runs that decode to nothing are dropped for the same reason an
	// empty token is refused outright.
	RemovedText []string
	// RemovedGlyphBoxes holds, in user space, the box each removed glyph
	// paints, in the order the page drew them: every glyph, whatever its
	// character, a space among them, and one with no character. A glyph
	// counts when this box meets a region, so its ink can reach past the
	// region, and these are where the page changed for it.
	RemovedGlyphBoxes [][4]float64
	// BlankedImages names the images whose covered samples were overwritten:
	// an image XObject by its resource name, an inline image as
	// [graphics.InlineImageName].
	BlankedImages []string
	// BlankedImageObjects holds the object number of each image XObject
	// blanked, once each. An image is rewritten in place, so every other place
	// that paints the same object shows the blanked samples too; a caller hands
	// these to [CheckBlankedImageUses] to refuse a blanking that would reach
	// past the regions.
	BlankedImageObjects []int
	// RemovedPaths counts the painted paths removed. Paths carry no text, so
	// there is nothing for a scan to look for; the count is for the caller's
	// report of what it did.
	RemovedPaths int
	// ClippedPaths counts the painted paths that reached outside the regions
	// and were rewritten: their geometry inside the regions removed, the rest
	// painted under a clip excluding the regions.
	ClippedPaths int
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

	// Read the page as earlier stages staged it, not as the source holds it: a
	// stage that rewrote /Contents (auto-tagging, remediation, an earlier
	// redaction) queued a new stream, and editing the original instead would
	// write back content that stage had already changed or removed.
	page = latestPage(ctx, alloc, doc, page)

	// The spans below index this buffer, so the parse and the later excision
	// both have to read the page the same way.
	data, err := collectLatestPageContents(ctx, alloc, doc, page)
	if err != nil {
		return RedactReport{}, fmt.Errorf("assembling page content: %w", err)
	}
	if len(data) == 0 {
		// A page that draws nothing still carries its annotations: ISO
		// 32000-1, Table 30 lets /Contents be absent, and no content stream
		// draws an annotation (12.5.1). Returning before this pass kept every
		// annotation inside the region and reported a success.
		removedAnnots, err := removeRegionAnnotations(ctx, alloc, doc, page, regionZones(regions))
		if err != nil {
			return RedactReport{}, err
		}
		return RedactReport{RemovedAnnotations: removedAnnots}, nil
	}

	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.SplitGluedOperators())
	if err != nil {
		return RedactReport{}, fmt.Errorf("parsing page content: %w", err)
	}
	if i := ambiguousTextPosition(ops); i >= 0 {
		return RedactReport{}, fmt.Errorf("%w: operation %d", ErrTextPositionAmbiguous, i)
	}

	machine := graphics.NewMachine(pageBox(ctx, alloc, doc, page))
	// An optional-content group that is off still holds its content in the
	// file, so its visibility must not gate removal.
	machine.IgnoreOCVisibility = true
	// Where images land decides whether this can proceed at all: one inside the
	// region is content that has to be accounted for, not stepped over.
	machine.RecordImageBoxes = true
	// A filled path inside the region is content to remove, and a stroked or
	// clipping one is content to refuse over. Both need its geometry.
	machine.RecordPathBoxes = true
	// A shading paint fills the clip in force and has no geometry to cut, so
	// one that may reach the region is refused. See ErrRegionCoversAShading.
	machine.RecordShadingBoxes = true
	// A removed show operator takes its advance with it, and later text on the
	// same line is positioned by that advance. See removedShowReplacement.
	machine.RecordShowAdvances = true
	// An operator a region covers only partly is split at its glyphs, which
	// needs each code's bytes and advance. See splitShowReplacement.
	machine.RecordGlyphAdvances = true
	// An XObject an earlier stage drew, a stamp's, is queued at a number only
	// alloc holds: resolved against the document alone it was unresolved, and
	// the page was refused below.
	machine.Staged = alloc.Pending()

	elements, origins, fontRefs, err := typography.ExtractElementsWithAttribution(ctx, doc, machine, page, ops)
	if err != nil {
		return RedactReport{}, fmt.Errorf("extracting page text: %w", err)
	}

	// Per-font counts, so a caller can be told which fonts this page no longer
	// draws with once the removal is done.
	fontGlyphs := make(map[int]int)
	fontRemoved := make(map[int]int)

	zones := regionZones(regions)

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
	// refuses when it cannot. See blankImage, and blankInlineImage for an image
	// whose samples sit in this content, which is rewritten with the text
	// edits below.
	var (
		inlineEdits []SpanEdit
		inlineEnds  *content.InlineImageEnds
	)
	for _, box := range machine.ShadingBoxes() {
		painted := heuristics.ZoneFromRect([4]float64{box.MinX, box.MinY, box.MaxX, box.MaxY})
		for _, z := range zones {
			if heuristics.ZonesOverlap(painted, z) {
				return RedactReport{}, ErrRegionCoversAShading
			}
		}
	}
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
		if box.Name == graphics.InlineImageName {
			if inlineEnds == nil {
				inlineEnds = content.NewInlineImageEnds(data)
			}
			edit, err := blankInlineImage(ctx, alloc, doc, page, data, ops, ranges, inlineEnds, box, zones)
			if err != nil {
				return RedactReport{}, err
			}
			inlineEdits = append(inlineEdits, edit)
			report.BlankedImages = append(report.BlankedImages, box.Name)
			continue
		}
		objNum, err := blankImage(ctx, alloc, doc, page, box, zones)
		if err != nil {
			return RedactReport{}, err
		}
		report.BlankedImages = append(report.BlankedImages, box.Name)
		if !slices.Contains(report.BlankedImageObjects, objNum) {
			report.BlankedImageObjects = append(report.BlankedImageObjects, objNum)
		}
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

	paths, err := pathEdits(ctx, alloc, doc, page, data, machine.PathBoxes(), ops, ranges, zones)
	if err != nil {
		return RedactReport{}, err
	}
	report.RemovedPaths = paths.removed
	report.ClippedPaths = paths.clipped

	// Tally each operator's glyphs, because an operator leaves whole or stays
	// whole: every glyph it drew has to agree about the regions.
	//
	// The glyphs are also where the removed text lives. One element is one
	// glyph on this path (typography.CoalesceElements merges them into runs
	// and is deliberately not called here), so the run an operator drew is its
	// glyphs joined in the order they were appended.
	type coverage struct {
		inside, outside int
		glyphs          []string
		mcids           []int
		// picked and glyphMCIDs run parallel to glyphs: whether each glyph
		// met a region, and its marked-content identifier (-1 for none). A
		// split operator removes some glyphs and keeps others, so its
		// marked content can go on both sides.
		picked     []bool
		glyphMCIDs []int
		// boxes runs parallel to glyphs too: the box each glyph paints
		// ([glyphBox]).
		boxes [][4]float64
		// fontRef is the font every glyph of this operator was drawn with: one
		// show-text operator paints with whatever Tf last selected. It holds
		// typography.FontUnattributable until a glyph names a font, so an
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
		c.picked = append(c.picked, selected)
		c.boxes = append(c.boxes, glyphBox(box))
		c.glyphMCIDs = append(c.glyphMCIDs, el.MCID)
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

	// An operator with glyphs on both sides is split rather than removed:
	// its covered glyphs go and the rest stay where they were. See
	// splitShowReplacement, which refuses whatever it cannot split exactly.
	// Only whole removals count as removed for the pen, since a split
	// operator keeps its full advance.
	removedOps := make(map[int]bool, len(selected))
	for _, origin := range selected {
		if tally[origin].outside == 0 {
			removedOps[origin] = true
		}
	}
	if err := checkShownCodes(machine.GlyphAdvances(), removedOps); err != nil {
		return RedactReport{}, err
	}
	splits := make(map[int][]byte)

	spans := make([]content.ByteRange, 0, len(selected))
	for _, origin := range selected {
		if c := tally[origin]; c.outside > 0 {
			split, err := splitShowReplacement(ctx, ops, origin, c.picked, c.glyphs, removedOps,
				machine.GlyphAdvances()[origin], machine.ShowAdvances()[origin])
			if err != nil {
				return RedactReport{}, err
			}
			splits[origin] = split.replacement
			spans = append(spans, ranges[origin])
			if c.fontRef != typography.FontUnattributable {
				fontRemoved[c.fontRef] += c.inside
			}
			report.RemovedText = append(report.RemovedText, split.removedText...)
			for k, b := range c.boxes {
				if c.picked[k] {
					report.RemovedGlyphBoxes = append(report.RemovedGlyphBoxes, b)
				}
			}
			continue
		}
		report.RemovedGlyphBoxes = append(report.RemovedGlyphBoxes, tally[origin].boxes...)
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

	// EditPageSpans sorts and rejects overlaps, so the two sets need no
	// ordering here. A path run that somehow enclosed a show-text operator
	// would be caught there as an overlap rather than silently merged.
	//
	// Which marked content went and which stayed. An identifier can appear on
	// both sides when it covers operators the region split between, and the
	// scrub has to see that rather than clear an element describing both.
	removedMCIDs := make(map[int]bool)
	survivingMCIDs := make(map[int]bool)
	selectedOrigins := make(map[int]bool, len(selected))
	for _, origin := range selected {
		selectedOrigins[origin] = true
		c := tally[origin]
		for k, mcid := range c.glyphMCIDs {
			switch {
			case mcid < 0:
			case c.picked[k] || c.outside == 0:
				removedMCIDs[mcid] = true
			default:
				survivingMCIDs[mcid] = true
			}
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

	if len(spans) == 0 && len(inlineEdits) == 0 && len(paths.edits) == 0 {
		// An image may still have been blanked, so the report stands even when
		// no operator was removed, and the page's own copies still have to go.
		if len(report.BlankedImages) > 0 {
			report.DroppedPageEntries = stripPageCopies(alloc, page)
		}
		return report, nil
	}

	edits := make([]SpanEdit, 0, len(spans)+len(inlineEdits)+len(paths.edits))
	edits = append(edits, inlineEdits...)
	for _, origin := range selected {
		if replacement, ok := splits[origin]; ok {
			edits = append(edits, SpanEdit{Span: ranges[origin], Replacement: replacement})
			continue
		}
		replacement, err := removedShowReplacement(ctx, ops, origin, removedOps, machine.ShowAdvances())
		if err != nil {
			return RedactReport{}, err
		}
		edits = append(edits, SpanEdit{Span: ranges[origin], Replacement: replacement})
	}
	edits = append(edits, paths.edits...)
	if err := EditPageSpans(ctx, alloc, doc, page, edits); err != nil {
		return RedactReport{}, err
	}
	// After the content is gone, not before: a page that refused partway
	// through keeps its thumbnail along with everything else. Run last so the
	// alloc-first read picks up the /Contents the excision just queued.
	report.DroppedPageEntries = stripPageCopies(alloc, page)
	return report, nil
}

// regionZones turns the regions, each [llx, lly, urx, ury], into the zones the
// geometric tests compare against.
func regionZones(regions [][4]float64) []heuristics.Zone {
	zones := make([]heuristics.Zone, 0, len(regions))
	for _, r := range regions {
		zones = append(zones, heuristics.ZoneFromRect(r))
	}
	return zones
}

// pathEditSet is what [pathEdits] decided for the painted paths of a page.
type pathEditSet struct {
	edits            []SpanEdit
	removed, clipped int
}

// pathEdits returns the edits clearing the painted paths the zones cover, and
// refuses any path it cannot clear exactly.
//
// A path is selected when its ink meets a region: its geometry for a fill, and
// for a stroke its geometry widened by the stroke's reach (see [strokeReach]),
// since a stroke paints past the path it follows. One whose ink sits wholly
// inside one region loses every construction operator of its run, from the
// first through the paint operator that ended it: one left behind would merge
// into the next path and be painted by its operator. One reaching outside is
// rewritten in place by [clipPathRun]: its geometry inside the regions is
// removed and the rest painted under a clip excluding them, so the page outside
// keeps every mark it had.
//
// data is the buffer ops and ranges were parsed from.
func pathEdits(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	data []byte,
	boxes []graphics.PathBox,
	ops []content.Operation,
	ranges []content.ByteRange,
	zones []heuristics.Zone,
) (pathEditSet, error) {
	var out pathEditSet
	var states map[int]pathPaintState

	meets := func(z heuristics.Zone) []heuristics.Zone {
		var hit []heuristics.Zone
		for _, r := range zones {
			if heuristics.ZonesOverlap(z, r) {
				hit = append(hit, r)
			}
		}
		return hit
	}

	for _, box := range boxes {
		painted := heuristics.ZoneFromRect([4]float64{box.MinX, box.MinY, box.MaxX, box.MaxY})

		// The operators that drew it are in the form's own stream, where no
		// offset into this one reaches them, and neither does its state.
		if box.FormDepth > 0 {
			if len(meets(painted)) > 0 {
				return pathEditSet{}, fmt.Errorf("%w: a painted path", ErrRegionInFormXObject)
			}
			continue
		}
		validRun := box.StartOp >= 0 && box.StartOp <= box.EndOp && box.EndOp < len(ops)

		inked := painted
		var reach, factor float64
		var state pathPaintState
		if validRun {
			if states == nil {
				var err error
				if states, err = pagePaintStates(ctx, alloc, doc, page, ops); err != nil {
					return pathEditSet{}, err
				}
			}
			state = states[box.EndOp]
		}
		strokes := validRun && pathPaintStrokes(ops[box.EndOp].Operator)
		if strokes {
			if !state.stroke.known {
				// Without the line width the ink cannot be placed, so a stroke
				// whose geometry meets the region cannot be cleared exactly.
				if len(meets(painted)) > 0 {
					return pathEditSet{}, fmt.Errorf("%w: operation %d strokes with a line width or join that cannot be read",
						ErrRegionCoversAStrokedPath, box.EndOp)
				}
				continue
			}
			// The joins the path actually makes decide how far it reaches; one
			// that cannot be read is bounded by the miter limit.
			factor = miterCeiling(state.stroke)
			if subs, _, err := parsePathRun(ops, box.StartOp, box.EndOp); err == nil {
				factor = joinFactor(subs, pathPaintCloses(ops[box.EndOp].Operator), state.stroke)
			}
			reach = strokeReach(state.stroke, box.CTM, factor)
			inked = heuristics.ZoneFromRect([4]float64{box.MinX - reach, box.MinY - reach, box.MaxX + reach, box.MaxY + reach})
		}

		hit := meets(inked)
		if len(hit) == 0 {
			continue
		}
		if box.Clipped {
			return pathEditSet{}, fmt.Errorf("%w: operations %d to %d", ErrRegionCoversAClippedPath, box.StartOp, box.EndOp)
		}
		if !validRun {
			return pathEditSet{}, fmt.Errorf("painted path reports operations %d to %d, which is not a run of the %d parsed",
				box.StartOp, box.EndOp, len(ops))
		}
		span := content.ByteRange{Start: ranges[box.StartOp].Start, End: ranges[box.EndOp].End}

		if slices.ContainsFunc(hit, func(z heuristics.Zone) bool { return heuristics.ZoneContains(z, inked) }) {
			// Operator by operator rather than the whole run: an operator a
			// producer wrote inside the path, a colour or even a text object,
			// still does what it did, and text the region selects is removed
			// by its own edit.
			for i := box.StartOp; i <= box.EndOp; i++ {
				if i == box.EndOp || pathConstruction(ops[i].Operator) {
					out.edits = append(out.edits, SpanEdit{Span: ranges[i]})
				}
			}
			out.removed++
			continue
		}

		if state.inText {
			return pathEditSet{}, fmt.Errorf("%w: operations %d to %d paint inside a text object, where no clip can be set",
				ErrPathStraddlesRegion, box.StartOp, box.EndOp)
		}
		var stroke *strokeParams
		if strokes {
			stroke = &state.stroke
		}
		replacement, paints, err := clipPathRun(ctx, data, ops, ranges, box, hit, inked, stroke, factor, !strokes || !state.stroke.dashed)
		switch {
		case errors.Is(err, errPathCutNeedsUndashedStroke):
			return pathEditSet{}, fmt.Errorf("%w: operations %d to %d: %w", ErrRegionCoversAStrokedPath, box.StartOp, box.EndOp, err)
		case err != nil:
			return pathEditSet{}, fmt.Errorf("%w: operations %d to %d: %w", ErrPathStraddlesRegion, box.StartOp, box.EndOp, err)
		}
		out.edits = append(out.edits, SpanEdit{Span: span, Replacement: []byte(replacement)})
		if paints {
			out.clipped++
		} else {
			out.removed++
		}
	}

	return out, nil
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

// pageBox returns the region the graphics machine seeds its clipping path
// with for a redaction: the widest region any reading of the page could show,
// never the narrower one a viewer shows.
//
// It is the one place a write reads a page boundary other than as a viewer
// does (see [document.UsableBox]), and on purpose: a redaction answers for
// every reader of the file, not for the one litho matches elsewhere, so text a
// lenient reader shows must not be left in the page because pdf.js and poppler
// hide it. The region is the union of the viewer region, [document.Page.ViewedBox]
// and [document.Page.ViewedMediaBox], and every box the page or an ancestor
// /Pages node states: /MediaBox and /CropBox, which ISO 32000-1, 7.7.3.3,
// Table 30 makes inheritable, and the page's /BleedBox, /TrimBox and /ArtBox.
// Each stated box contributes the first four entries it holds, read as numbers
// where they are numbers and as zero where not, whatever it holds beyond them
// and whether or not they enclose an area, so a five-entry box or one whose
// corners meet widens the region all the same; a box of fewer than four
// entries describes no rectangle, and a reader substitutes the default Table
// 30 gives it, which the viewer region already holds. The ancestors are read
// because poppler passes over a page's unusable box to the nearest ancestor's
// while pdf.js reads none, and a crop box that ISO 32000-1, 14.11.2.1 would
// reduce to the media box is taken whole, as a reader that skips the reduction
// shows it.
//
// Selection itself is geometric and reads no clip (see [RedactPageRegions]),
// so the region decides what the machine reports as clipped, not what is
// removed. It is read wide so that no such report narrows a redaction.
//
// The page dictionary is read as earlier stages staged it, from alloc first,
// so a box a crop stage wrote is read too.
func pageBox(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) [4]float64 {
	region := boxUnion{}
	region.add(page.ViewedBox())
	region.add(page.ViewedMediaBox())
	for _, box := range []ast.PdfArray{
		page.MediaBox, page.DeclaredCropBox, page.DeclaredBleedBox, page.DeclaredTrimBox, page.DeclaredArtBox,
	} {
		region.add(box)
	}

	own := latestPageDict(alloc, page)
	for _, key := range []ast.PdfName{"MediaBox", "CropBox", "BleedBox", "TrimBox", "ArtBox"} {
		region.add(stagedBox(ctx, alloc, doc, own[key]))
	}

	// The /Parent chain is followed with a visited set as well as a depth cap:
	// a cycle of /Parent references would otherwise be read up to the cap.
	visited := make(map[int]bool)
	if page.Ref.ObjectNum != 0 {
		visited[page.Ref.ObjectNum] = true
	}
	parentObj := own["Parent"]
	for depth := 0; depth < document.MaxPageTreeDepth && parentObj != nil; depth++ {
		if document.VisitedBefore(parentObj, visited) {
			break
		}
		obj, err := resolveAllocFirst(ctx, alloc, doc, parentObj)
		if err != nil {
			doc.Logger.Debug("redaction page box: an ancestor did not resolve, reading no further",
				slog.Int("page", page.Ref.ObjectNum), slog.Any("err", err))
			break
		}
		node, ok := obj.(ast.PdfDict)
		if !ok {
			break
		}
		region.add(stagedBox(ctx, alloc, doc, node["MediaBox"]))
		region.add(stagedBox(ctx, alloc, doc, node["CropBox"]))
		parentObj = node["Parent"]
	}

	// The viewed media box always holds four numbers, so the union is set.
	return [4]float64{region.x0, region.y0, region.x1, region.y1}
}

// stagedBox resolves a page box entry, from alloc first, into an array whose
// references to numbers are resolved (see [document.ResolveRectangle]). It
// returns nil for an absent entry or one that is no array.
func stagedBox(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, entry ast.PdfObject) ast.PdfArray {
	if entry == nil {
		return nil
	}
	obj, err := resolveAllocFirst(ctx, alloc, doc, entry)
	if err != nil {
		doc.Logger.Debug("redaction page box: a box entry did not resolve", slog.Any("err", err))
		return nil
	}
	arr, ok := obj.(ast.PdfArray)
	if !ok {
		return nil
	}
	return document.ResolveRectangle(ctx, doc, arr)
}

// boxUnion accumulates the smallest rectangle holding every box added to it.
type boxUnion struct {
	x0, y0, x1, y1 float64
	set            bool
}

// glyphBox returns the rectangle zone covers, the box selection tests a glyph
// by ([heuristics.ElementZone]).
func glyphBox(zone heuristics.Zone) [4]float64 {
	minX, minY, maxX, maxY := heuristics.ZoneBounds(zone)
	return [4]float64{minX, minY, maxX, maxY}
}

// add widens the union by the rectangle box's first four entries describe, a
// non-number entry read as zero. A box of fewer than four entries describes
// none and is passed over.
func (u *boxUnion) add(box ast.PdfArray) {
	if len(box) < 4 {
		return
	}
	a, b := document.Float(box[0]), document.Float(box[2])
	c, d := document.Float(box[1]), document.Float(box[3])
	x0, x1 := math.Min(a, b), math.Max(a, b)
	y0, y1 := math.Min(c, d), math.Max(c, d)
	if !u.set {
		u.x0, u.y0, u.x1, u.y1, u.set = x0, y0, x1, y1, true
		return
	}
	u.x0, u.y0 = math.Min(u.x0, x0), math.Min(u.y0, y0)
	u.x1, u.y1 = math.Max(u.x1, x1), math.Max(u.y1, y1)
}

// checkShownCodes refuses what the page's show-text operators make unsafe to
// edit: an operator removed whole whose glyphs add to the clipping path
// ([ErrRegionCoversClippingText]), and an operator kept, whole or split, that
// shows a code extraction read no glyph for ([ErrTextNotPlaceable]). glyphs
// holds each operator's codes by its index; removed names the operators
// removed whole. Operators are checked in order, so a refusal names the same
// one every run.
func checkShownCodes(glyphs map[int][]graphics.GlyphAdvance, removed map[int]bool) error {
	for _, origin := range slices.Sorted(maps.Keys(glyphs)) {
		for _, g := range glyphs[origin] {
			switch {
			case removed[origin] && g.RenderingMode >= 4 && g.RenderingMode <= 7: //nolint:mnd // ISO 32000-1, 9.3.6, Table 106: modes 4 to 7 add to the clipping path
				return fmt.Errorf("%w: operation %d, text rendering mode %d", ErrRegionCoversClippingText, origin, g.RenderingMode)
			case !removed[origin] && !g.Emitted && g.Bytes > 0:
				return fmt.Errorf("%w: operation %d", ErrTextNotPlaceable, origin)
			}
		}
	}
	return nil
}

// ambiguousTextPosition returns the index of the first show-text operator of
// ops placed after a Q, inside a text object, that undoes a q saved inside it
// with the text matrix moved in between (see [ErrTextPositionAmbiguous]), or
// -1. A Tm sets the matrix outright, so the two readings agree again after
// one.
func ambiguousTextPosition(ops []content.Operation) int {
	inText, ambiguous := false, false
	var frames []bool
	moved := func() {
		for i := range frames {
			frames[i] = true
		}
	}
	for i, op := range ops {
		switch op.Operator {
		case "BT":
			inText, ambiguous, frames = true, false, frames[:0]
		case "ET":
			inText = false
		case "q":
			if inText {
				frames = append(frames, false)
			}
		case "Q":
			if inText && len(frames) > 0 {
				ambiguous = ambiguous || frames[len(frames)-1]
				frames = frames[:len(frames)-1]
			}
		case "Tm":
			if inText {
				moved()
				ambiguous = false
			}
		case "Td", "TD", "T*":
			if inText {
				moved()
			}
		case "Tj", "TJ", "'", "\"":
			if inText {
				if ambiguous {
					return i
				}
				moved()
			}
		}
	}
	return -1
}
