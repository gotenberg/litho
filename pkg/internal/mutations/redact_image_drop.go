package mutations

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
)

// stateOnlyOperators are the operators that set the graphics state and paint
// nothing: the general graphics state operators but q and Q (ISO 32000-1,
// 8.4.4, Table 57) and the colour operators (8.6.8, Table 74). A q ... Q
// group holding only these around a removed image paints nothing once the
// image is gone, and the Q restores every parameter they set (8.4.2), so the
// group goes whole.
var stateOnlyOperators = []string{
	"cm", "w", "J", "j", "M", "d", "ri", "i", "gs",
	"CS", "cs", "SC", "SCN", "sc", "scn", "G", "g", "RG", "rg", "K", "k",
}

// removablePlacement reports whether the page's own content draws box at
// operation box.Op, a Do naming box.Name or a BI, and the box lies wholly
// inside one of the zones.
//
// Such a placement paints nothing the regions do not cover, so removing it
// takes nothing the caller did not select. Blanking it instead paints the
// covered samples black where the picture was, which keeps the placement's
// outline on the page: a picture drawn as a mosaic of small images, one
// image a run of pixels, keeps its whole shape that way, the logo of a page
// redacted whole still readable in black.
//
// # Corpus evidence
//
//   - govdocs thread_100_100791: Word drew page 1's two logos as 1,566 image
//     placements one sample high, each under its own q, gs, cm ... Q. A
//     whole-page redaction blanked every one in place, and the page showed
//     both logos and the agency's name around them in black.
//   - govdocs, pdfjs, pdfcpu, litho: the redact-render scenarios remove
//     placements in 334, 246, 17 and 182 fixtures, up to 50,385 on one
//     page (govdocs thread_400_400230). No scenario fails a check it
//     passed, and poppler's inside check stops failing on four mosaics,
//     govdocs thread_000_000344, thread_000_000672, thread_100_100791 and
//     thread_400_400762 (284, 10, 53 and 23 pixels).
//   - govdocs thread_100_100853, pdfjs issue16454, issue19494: a whole-page
//     redaction was incomplete over removed digits the kept placements
//     spelled in their operands and names (154.32 0 0 .78 306 123.9 cm
//     /Im123 Do), and now succeeds.
func removablePlacement(box graphics.ImageBox, ops []content.Operation, zones []heuristics.Zone) bool {
	if box.FormDepth > 0 || box.Op < 0 || box.Op >= len(ops) {
		return false
	}
	op := ops[box.Op]
	switch {
	case box.Name == graphics.InlineImageName:
		if op.Operator != "BI" {
			return false
		}
	case op.Operator != "Do" || len(op.Operands) != 1:
		return false
	default:
		// A Type 3 glyph's image records the show operator that ran it, so
		// the operator has to be the Do that named this image.
		if name, ok := op.Operands[0].(ast.PdfName); !ok || string(name) != box.Name {
			return false
		}
	}
	painted := heuristics.ZoneFromRect([4]float64{box.MinX, box.MinY, box.MaxX, box.MaxY})
	return slices.ContainsFunc(zones, func(z heuristics.Zone) bool { return heuristics.ZoneContains(z, painted) })
}

// placementSpan returns the bytes to remove for the image operation at op:
// the q ... Q group around it when the group holds nothing else that paints
// or marks (see [stateOnlyOperators]), else the operation alone. The group
// goes whole because its cm places the image: kept, its operands still
// spell where each piece of a mosaic sat.
func placementSpan(ops []content.Operation, ranges []content.ByteRange, op int) content.ByteRange {
	span := ranges[op]
	first := -1
	for i := op - 1; i >= 0; i-- {
		if ops[i].Operator == "q" {
			first = i
			break
		}
		if !slices.Contains(stateOnlyOperators, ops[i].Operator) {
			return span
		}
	}
	if first < 0 {
		return span
	}
	for i := op + 1; i < len(ops); i++ {
		if ops[i].Operator == "Q" {
			if i >= len(ranges) {
				return span
			}
			return content.ByteRange{Start: ranges[first].Start, End: ranges[i].End}
		}
		if !slices.Contains(stateOnlyOperators, ops[i].Operator) {
			return span
		}
	}
	return span
}
