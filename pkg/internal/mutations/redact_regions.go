package mutations

import (
	"context"
	"fmt"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

// RedactPageRegions removes the text a page draws inside the given regions and
// writes the remaining content back as the page's only content stream.
// Regions are [llx, lly, urx, ury] in page space.
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
//   - A region selecting content a form XObject draws reports
//     [ErrRegionInFormXObject], whose bytes are in the form's stream.
//   - A region meeting an image reports [ErrRegionCoversAnImage]. Removal takes
//     whole operators and cannot blank pixels, so the picture would survive.
//   - A Do the machine cannot follow reports [ErrXObjectUnresolved], because
//     content it cannot place is content it cannot rule out of the region.
//   - A page with no object number of its own reports [ErrPageNotAddressable].
//
// Refusing beats removing more than was asked, and beats reporting success over
// content that is still in the file.
//
// This removes text. Vector paths and the document-wide copies of page text
// (the structure tree, annotation values, outlines, metadata) are still
// untouched, so the result is not a redacted document on its own. An image is
// no longer stepped over silently: a region meeting one is refused, because
// stripping the text off a picture and leaving the picture reads as success
// while the content is still there.
func RedactPageRegions(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	regions [][4]float64,
) error {
	if len(regions) == 0 {
		return nil
	}
	if page.Ref.ObjectNum == 0 {
		return ErrPageNotAddressable
	}

	// The spans below index this buffer, so the parse and the later excision
	// both have to read the page the same way.
	data, err := collectPageContents(ctx, doc, page)
	if err != nil {
		return fmt.Errorf("assembling page content: %w", err)
	}
	if len(data) == 0 {
		return nil
	}

	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data)
	if err != nil {
		return fmt.Errorf("parsing page content: %w", err)
	}

	machine := graphics.NewMachine(pageBox(page))
	// An optional-content group that is off still holds its content in the
	// file, so its visibility must not gate removal.
	machine.IgnoreOCVisibility = true
	// Where images land decides whether this can proceed at all: one inside the
	// region is content that has to be accounted for, not stepped over.
	machine.RecordImageBoxes = true

	elements, origins, err := typography.ExtractElementsWithOrigins(ctx, doc, machine, page, ops)
	if err != nil {
		return fmt.Errorf("extracting page text: %w", err)
	}

	zones := make([]heuristics.Zone, 0, len(regions))
	for _, r := range regions {
		zones = append(zones, heuristics.ZoneFromRect(r))
	}

	// An image is taken whole or not at all, and its pixels cannot be blanked
	// here, so a region meeting one cannot be cleared by removing operators.
	for _, box := range machine.ImageBoxes() {
		painted := heuristics.ZoneFromRect([4]float64{box.MinX, box.MinY, box.MaxX, box.MaxY})
		for _, z := range zones {
			if heuristics.ZonesOverlap(painted, z) {
				return fmt.Errorf("%w: %q", ErrRegionCoversAnImage, box.Name)
			}
		}
	}

	// Page-wide rather than region-scoped on purpose: an XObject that could not
	// be placed is one that cannot be ruled out of the region either.
	if n := machine.UnresolvedXObjects(); n > 0 {
		return fmt.Errorf("%w: %d on this page", ErrXObjectUnresolved, n)
	}

	// Tally each operator's glyphs, because an operator leaves whole or stays
	// whole: every glyph it drew has to agree about the regions.
	type coverage struct{ inside, outside int }
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
				return fmt.Errorf("%w: %q", ErrRegionInFormXObject, el.Text)
			}
			continue
		}
		if origin < 0 || origin >= len(ops) {
			return fmt.Errorf("element %d reports operation %d, which is not one of %d", i, origin, len(ops))
		}
		c := tally[origin]
		if c == nil {
			c = &coverage{}
			tally[origin] = c
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

	spans := make([]content.ByteRange, 0, len(selected))
	for _, origin := range selected {
		if c := tally[origin]; c.outside > 0 {
			return fmt.Errorf("%w: operation %d draws %d glyphs inside and %d outside",
				ErrPartialOperatorCoverage, origin, c.inside, c.outside)
		}
		spans = append(spans, ranges[origin])
	}
	if len(spans) == 0 {
		return nil
	}

	return ExcisePageSpans(ctx, alloc, doc, page, spans)
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
