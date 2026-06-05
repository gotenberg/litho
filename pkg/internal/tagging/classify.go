package tagging

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// headingSizeRatio is the font-size multiple over body text at or above which a
// text block is treated as a heading. 1.15 (15% larger) reuses the same ratio
// as the bookmark heading heuristic; it is a tuning constant, not spec-mandated.
const headingSizeRatio = 1.15

// maxHeadingLevels caps synthesized heading depth at H6, the deepest standard
// heading role in ISO 32000-1 Table 337.
const maxHeadingLevels = 6

// AutoTag classifies each page's text into a logical structure tree and emits
// it through [Emit]. It is a no-op on an already-tagged document (one with a
// /StructTreeRoot or /MarkInfo /Marked true), leaving the existing structure
// intact. Pages whose content cannot be parsed are skipped. It returns Emit's
// descriptions, or nil with no error when there is nothing to tag.
//
// AutoTag mutates alloc and is not safe for concurrent use.
func AutoTag(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) ([]string, error) {
	tagged, err := alreadyTagged(ctx, doc)
	if err != nil {
		return nil, err
	}
	if tagged {
		return nil, nil
	}
	root, err := Classify(ctx, doc)
	if err != nil {
		return nil, err
	}
	if len(root.Kids) == 0 {
		return nil, nil
	}
	return Emit(ctx, alloc, doc, root)
}

// alreadyTagged reports whether the document already declares a tagged
// structure, in which case AutoTag must not overwrite it.
func alreadyTagged(ctx context.Context, doc *document.Document) (bool, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return false, nil
	}
	catalogDict, err := document.ResolveDict(ctx, doc, catalogRef)
	if err != nil {
		return false, fmt.Errorf("tagging: resolving catalog: %w", err)
	}
	if _, ok := catalogDict["StructTreeRoot"]; ok {
		return true, nil
	}
	if mi, err := document.ResolveDict(ctx, doc, catalogDict["MarkInfo"]); err == nil {
		if mi["Marked"] == ast.PdfBool(true) {
			return true, nil
		}
	}
	return false, nil
}

// Classify analyzes the document's text and returns a logical structure tree
// (a Document with H1..Hn and P leaves) ready for [Emit]. It does not mutate
// the document. Headings are detected by font size relative to body text and
// numbered into a no-skip H1..Hn hierarchy.
//
// PR1+PR2 scope: single-column text, one element per text object (BT…ET).
// Known limitations deferred to later passes: text inside Form XObjects (Do)
// is not descended into; font size is read from Tf only, ignoring Tm/CTM scale,
// so a base font scaled large reads as body; a single text object mixing sizes
// is classified by its dominant size; and tables, figures, reading-order
// inference, and merging text objects into multi-line paragraphs are not done.
// Content not placed in a structure element (graphics, and text the heuristic
// misses) is marked as an artifact by [Emit] so the output stays fully marked.
func Classify(ctx context.Context, doc *document.Document) (*Element, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("tagging: resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("tagging: resolving pages: %w", err)
	}

	root := &Element{Role: RoleDocument}
	for pi := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		blocks, contentLen, err := pageTextBlocks(ctx, doc, pages[pi])
		if err != nil || len(blocks) == 0 {
			continue // skip unparseable or text-free pages
		}
		root.Kids = append(root.Kids, classifyBlocks(blocks, pi, contentLen)...)
	}
	return root, nil
}

// textBlock is one text object (BT…ET) that painted text: its byte range, the
// dominant font size across its show-text operators, and the total byte length
// of the strings it showed (a proxy for text quantity, used to weight body-size
// detection).
type textBlock struct {
	startByte, endByte int
	fontSize           float64
	textLen            int
}

// pageTextBlocks segments a page's content into per-text-object blocks (one per
// BT…ET) and returns them with the length of the decoded content they index
// into. Marking whole text objects keeps marked-content sequences cleanly
// nested with BT/ET and lets the gaps between them be artifacted as a unit.
func pageTextBlocks(ctx context.Context, doc *document.Document, page *document.Page) ([]textBlock, int, error) {
	data, err := pageContentBytes(ctx, doc, page)
	if err != nil || len(data) == 0 {
		return nil, len(data), err
	}
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data)
	if err != nil {
		return nil, len(data), err
	}

	var blocks []textBlock
	var cur *textBlock
	curFont := 0.0
	sizeChars := make(map[float64]int) // dominant-size accumulator for the current block

	flush := func() {
		if cur != nil && cur.textLen > 0 {
			cur.fontSize = dominantSize(sizeChars)
			blocks = append(blocks, *cur)
		}
		cur = nil
		clear(sizeChars)
	}

	for i, op := range ops {
		if i%100 == 0 { //nolint:mnd // per-~100-operator ctx checkpoint, per CONTRIBUTING
			if err := ctx.Err(); err != nil {
				return nil, len(data), err
			}
		}
		r := ranges[i]
		switch op.Operator {
		case "BT":
			flush()
			cur = &textBlock{startByte: r.Start, endByte: r.End}
		case "ET":
			if cur != nil {
				cur.endByte = r.End
			}
			flush()
		case "Tf":
			if len(op.Operands) >= 2 {
				if s, ok := toFloat(op.Operands[1]); ok {
					curFont = s
				}
			}
			if cur != nil {
				cur.endByte = r.End
			}
		default:
			if cur != nil {
				cur.endByte = r.End
				if n := showTextLen(op); n > 0 {
					cur.textLen += n
					sizeChars[roundHalf(curFont)] += n
				}
			}
		}
	}
	flush()
	return blocks, len(data), nil
}

// dominantSize returns the rounded font size that painted the most characters,
// breaking ties toward the larger size so a heading is not demoted by a shorter
// trailing run at body size.
func dominantSize(sizeChars map[float64]int) float64 {
	var best float64
	bestN := 0
	for sz, n := range sizeChars {
		if n > bestN || (n == bestN && sz > best) {
			best, bestN = sz, n
		}
	}
	return best
}

// classifyBlocks assigns a role to each block and builds the leaf elements for
// one page, in content order.
func classifyBlocks(blocks []textBlock, pageIndex, contentLen int) []*Element {
	if len(blocks) == 0 {
		return nil
	}
	body := bodyFontSize(blocks)
	levels := headingLevels(blocks, body)

	var elems []*Element
	for _, b := range blocks {
		start, end := b.startByte, b.endByte
		if start < 0 {
			start = 0
		}
		if end > contentLen {
			end = contentLen
		}
		if start >= end {
			continue
		}
		role := RoleP
		if body > 0 && b.fontSize >= body*headingSizeRatio {
			role = headingRole(levels[roundHalf(b.fontSize)])
		}
		elems = append(elems, &Element{
			Role: role,
			Span: &ContentSpan{PageIndex: pageIndex, Start: start, End: end},
		})
	}
	return elems
}

// bodyFontSize returns the dominant text size: the rounded size carrying the
// most characters across the page's blocks.
func bodyFontSize(blocks []textBlock) float64 {
	weight := make(map[float64]int)
	for _, b := range blocks {
		if b.fontSize > 0 {
			weight[roundHalf(b.fontSize)] += b.textLen
		}
	}
	var best float64
	bestW := 0
	for sz, w := range weight {
		if w > 0 && (w > bestW || (w == bestW && sz < best)) {
			best, bestW = sz, w
		}
	}
	return best
}

// headingLevels maps each distinct heading size (rounded) to a no-skip level:
// the largest size is H1, the next H2, and so on. Sizes below the heading
// threshold are absent from the map.
func headingLevels(blocks []textBlock, body float64) map[float64]int {
	seen := make(map[float64]bool)
	var sizes []float64
	for _, b := range blocks {
		if body > 0 && b.fontSize >= body*headingSizeRatio {
			sz := roundHalf(b.fontSize)
			if !seen[sz] {
				seen[sz] = true
				sizes = append(sizes, sz)
			}
		}
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i] > sizes[j] })

	levels := make(map[float64]int, len(sizes))
	for i, sz := range sizes {
		levels[sz] = i + 1 // contiguous H1..Hn, no skips
	}
	return levels
}

func headingRole(level int) Role {
	if level < 1 {
		level = 1
	}
	if level > maxHeadingLevels {
		level = maxHeadingLevels
	}
	return Role(fmt.Sprintf("H%d", level))
}

// roundHalf rounds to the nearest 0.5 point so near-equal font sizes cluster.
func roundHalf(x float64) float64 {
	return math.Round(x*2) / 2
}

func toFloat(o ast.PdfObject) (float64, bool) {
	switch v := o.(type) {
	case ast.PdfInt:
		return float64(v), true
	case ast.PdfReal:
		return float64(v), true
	default:
		return 0, false
	}
}

// showTextLen returns the byte length of the strings a text-showing operator
// paints, a proxy for text quantity (not a decoded character count: multi-byte
// or CID-encoded strings overcount, which only skews the relative weighting).
func showTextLen(op content.Operation) int {
	switch op.Operator {
	case "Tj", "'":
		if len(op.Operands) >= 1 {
			if s, ok := op.Operands[0].(ast.PdfString); ok {
				return len(string(s))
			}
		}
	case "\"":
		if len(op.Operands) >= 3 {
			if s, ok := op.Operands[2].(ast.PdfString); ok {
				return len(string(s))
			}
		}
	case "TJ":
		if len(op.Operands) >= 1 {
			if arr, ok := op.Operands[0].(ast.PdfArray); ok {
				n := 0
				for _, el := range arr {
					if s, ok := el.(ast.PdfString); ok {
						n += len(string(s))
					}
				}
				return n
			}
		}
	}
	return 0
}
