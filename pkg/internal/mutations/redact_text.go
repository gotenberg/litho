package mutations

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// textSelection is what [selectText] decided for the show-text operators of
// one content stream: which of them the zones cover, and what removing the
// covered glyphs takes.
type textSelection struct {
	// selected lists, ascending, the operators with a glyph a zone covers.
	selected []int
	// removed names the operators removed whole: every glyph they drew is
	// covered. The others of selected are split at their glyphs.
	removed map[int]bool
	// splits maps a split operator to the run that replaces it.
	splits map[int][]byte
	// removedText, glyphBoxes and glyphOrigins are
	// [RedactReport.RemovedText], [RedactReport.RemovedGlyphBoxes] and
	// [RedactReport.RemovedGlyphOrigins] for this stream.
	removedText  []string
	glyphBoxes   [][4]float64
	glyphOrigins [][2]float64
	// removedMCIDs and survivingMCIDs name the marked-content identifiers
	// with content removed and kept.
	removedMCIDs, survivingMCIDs map[int]bool
	// fontsFullyRemoved is [RedactReport.FontsFullyRemovedOnPage] for this
	// stream, ascending.
	fontsFullyRemoved []int
}

// selectText tallies the glyphs elements, drawn by ops, place against the
// zones, splits each operator they cover only partly, and refuses what it
// cannot remove exactly (see [RedactPageRegions]). attr is the attribution
// [typography.ExtractElementsAttributed] returned with elements, and machine
// the one it ran on.
//
// A glyph a form XObject drew has no bytes in ops. formGlyph is called for
// each such glyph a zone covers, with the Do on this stream that drew the
// form; its error refuses the selection.
func selectText(
	ctx context.Context,
	ops []content.Operation,
	elements []models.TextElement,
	attr typography.Attribution,
	machine *graphics.Machine,
	zones []heuristics.Zone,
	formGlyph func(el models.TextElement, pageOp int) error,
) (textSelection, error) {
	origins, fontRefs := attr.Origins, attr.FontRefs

	// Per-font counts, so a caller can be told which fonts this stream no
	// longer draws with once the removal is done.
	fontGlyphs := make(map[int]int)
	fontRemoved := make(map[int]int)

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
		// boxes and origins run parallel to glyphs too: the box each glyph
		// paints ([glyphBox]), and its origin.
		boxes   [][4]float64
		origins [][2]float64
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
				pageOp := -1
				if i < len(attr.PageOps) {
					pageOp = attr.PageOps[i]
				}
				if err := formGlyph(el, pageOp); err != nil {
					return textSelection{}, err
				}
			}
			continue
		}
		if origin < 0 || origin >= len(ops) {
			return textSelection{}, fmt.Errorf("element %d reports operation %d, which is not one of %d", i, origin, len(ops))
		}
		c := tally[origin]
		if c == nil {
			c = &coverage{fontRef: typography.FontUnattributable}
			tally[origin] = c
		}
		c.glyphs = append(c.glyphs, el.Text)
		c.picked = append(c.picked, selected)
		c.boxes = append(c.boxes, glyphBox(box))
		c.origins = append(c.origins, [2]float64{el.X, el.Y})
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
	sel := textSelection{removed: make(map[int]bool), splits: make(map[int][]byte)}
	for origin, c := range tally {
		if c.inside > 0 {
			sel.selected = append(sel.selected, origin)
		}
	}
	sort.Ints(sel.selected)

	// An operator with glyphs on both sides is split rather than removed:
	// its covered glyphs go and the rest stay where they were. See
	// splitShowReplacement, which refuses whatever it cannot split exactly.
	// Only whole removals count as removed for the pen, since a split
	// operator keeps its full advance.
	for _, origin := range sel.selected {
		if tally[origin].outside == 0 {
			sel.removed[origin] = true
		}
	}
	if err := checkShownCodes(machine.GlyphAdvances(), sel.removed); err != nil {
		return textSelection{}, err
	}

	for _, origin := range sel.selected {
		if c := tally[origin]; c.outside > 0 {
			split, err := splitShowReplacement(ctx, ops, origin, c.picked, c.glyphs, sel.removed,
				machine.GlyphAdvances()[origin], machine.ShowAdvances()[origin])
			if err != nil {
				return textSelection{}, err
			}
			sel.splits[origin] = split.replacement
			if c.fontRef != typography.FontUnattributable {
				fontRemoved[c.fontRef] += c.inside
			}
			sel.removedText = append(sel.removedText, split.removedText...)
			for k, b := range c.boxes {
				if c.picked[k] {
					sel.glyphBoxes = append(sel.glyphBoxes, b)
					sel.glyphOrigins = append(sel.glyphOrigins, c.origins[k])
				}
			}
			continue
		}
		sel.glyphBoxes = append(sel.glyphBoxes, tally[origin].boxes...)
		sel.glyphOrigins = append(sel.glyphOrigins, tally[origin].origins...)
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
			sel.removedText = append(sel.removedText, run)
		}
	}
	// A font every one of whose glyphs went is one this stream no longer draws
	// with, so its embedded outlines now describe only removed text.
	for fontRef, drawn := range fontGlyphs {
		if drawn > 0 && fontRemoved[fontRef] == drawn {
			sel.fontsFullyRemoved = append(sel.fontsFullyRemoved, fontRef)
		}
	}
	sort.Ints(sel.fontsFullyRemoved)

	// Which marked content went and which stayed. An identifier can appear on
	// both sides when it covers operators the region split between, and the
	// scrub has to see that rather than clear an element describing both.
	sel.removedMCIDs = make(map[int]bool)
	sel.survivingMCIDs = make(map[int]bool)
	selectedOrigins := make(map[int]bool, len(sel.selected))
	for _, origin := range sel.selected {
		selectedOrigins[origin] = true
		c := tally[origin]
		for k, mcid := range c.glyphMCIDs {
			switch {
			case mcid < 0:
			case c.picked[k] || c.outside == 0:
				sel.removedMCIDs[mcid] = true
			default:
				sel.survivingMCIDs[mcid] = true
			}
		}
	}
	for origin, c := range tally {
		if selectedOrigins[origin] {
			continue
		}
		for _, mcid := range c.mcids {
			sel.survivingMCIDs[mcid] = true
		}
	}
	return sel, nil
}

// edits returns the edits removing the glyphs s selected from ops, whose
// bytes ranges address: a split operator's run in its place, and an operator
// removed whole replaced by what puts its advance back (see
// [removedShowReplacement]).
func (s textSelection) edits(ctx context.Context, ops []content.Operation, ranges []content.ByteRange, machine *graphics.Machine) ([]SpanEdit, error) {
	edits := make([]SpanEdit, 0, len(s.selected))
	for _, origin := range s.selected {
		if replacement, ok := s.splits[origin]; ok {
			edits = append(edits, SpanEdit{Span: ranges[origin], Replacement: replacement})
			continue
		}
		replacement, err := removedShowReplacement(ctx, ops, origin, s.removed, machine.ShowAdvances())
		if err != nil {
			return nil, err
		}
		edits = append(edits, SpanEdit{Span: ranges[origin], Replacement: replacement})
	}
	return edits, nil
}
