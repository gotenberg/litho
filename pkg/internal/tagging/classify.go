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
// Scope: single-column text. Text objects are segmented per BT…ET, classified
// as headings (by font size) or paragraphs, and adjacent same-role lines are
// merged into one element by baseline proximity, so a wrapped paragraph or
// heading is a single element. Content order is taken as reading order.
//
// Known limitations deferred to later passes: text inside Form XObjects (Do)
// is not descended into; font size is read from Tf only, ignoring Tm/CTM scale,
// so a base font scaled large reads as body; a text object mixing sizes is
// classified by its dominant size; and tables, figures, lists, and multi-column
// reading-order segmentation are not done. Content not placed in a structure
// element (graphics, and text the heuristic misses) is marked as an artifact by
// [Emit] so the output stays fully marked.
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
		blocks, data, err := pageTextBlocks(ctx, doc, pages[pi])
		if err != nil || len(blocks) == 0 {
			continue // skip unparseable or text-free pages
		}
		root.Kids = append(root.Kids, classifyBlocks(blocks, pi, data)...)
	}
	return root, nil
}

// textBlock is one text object (BT…ET) that painted text: its byte range, the
// dominant font size across its show-text operators, the total byte length of
// the strings it showed (a proxy for text quantity), and the baselines of its
// first and last text lines (used to merge adjacent lines into paragraphs).
//
// Baselines are tracked from the text positioning operators only
// (Tm translation, Td/TD/T*/TL). The CTM (cm) translation and scale, Tm's own
// vertical scale and shear, text rise (Ts, intentionally — it is a per-glyph
// offset, not a line advance), and rotation are all ignored, so the baseline is
// the true device Y only for upright, unit-scale, non-cm-repositioned text. The
// merge pass guards against the resulting errors by refusing to merge across
// any intervening operator (see classifyBlocks).
type textBlock struct {
	startByte, endByte int
	fontSize           float64
	textLen            int
	y, lastY           float64
	hasY               bool
}

// pageTextBlocks segments a page's content into per-text-object blocks (one per
// BT…ET) and returns them with the length of the decoded content they index
// into. Marking whole text objects keeps marked-content sequences cleanly
// nested with BT/ET and lets the gaps between them be artifacted as a unit.
func pageTextBlocks(ctx context.Context, doc *document.Document, page *document.Page) ([]textBlock, []byte, error) {
	data, err := pageContentBytes(ctx, doc, page)
	if err != nil || len(data) == 0 {
		return nil, data, err
	}
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data)
	if err != nil {
		return nil, data, err
	}

	var blocks []textBlock
	var cur *textBlock
	curFont := 0.0
	textY, leading := 0.0, 0.0
	sizeChars := make(map[float64]int) // dominant-size accumulator for the current block

	flush := func() {
		if cur != nil && cur.textLen > 0 {
			cur.fontSize = dominantSize(sizeChars)
			blocks = append(blocks, *cur)
		}
		cur = nil
		clear(sizeChars)
	}
	extend := func(end int) {
		if cur != nil {
			cur.endByte = end
		}
	}

	for i, op := range ops {
		if i%100 == 0 { //nolint:mnd // per-~100-operator ctx checkpoint, per CONTRIBUTING
			if err := ctx.Err(); err != nil {
				return nil, data, err
			}
		}
		r := ranges[i]
		switch op.Operator {
		case "BT":
			flush()
			cur = &textBlock{startByte: r.Start, endByte: r.End}
			textY, leading = 0, 0 // BT resets the text and line matrices to identity
		case "ET":
			extend(r.End)
			flush()
		case "Tf":
			if len(op.Operands) >= 2 {
				if s, ok := toFloat(op.Operands[1]); ok {
					curFont = s
				}
			}
			extend(r.End)
		case "Tm":
			if len(op.Operands) >= 6 {
				if f, ok := toFloat(op.Operands[5]); ok {
					textY = f
				}
			}
			extend(r.End)
		case "Td":
			if len(op.Operands) >= 2 {
				if ty, ok := toFloat(op.Operands[1]); ok {
					textY += ty
				}
			}
			extend(r.End)
		case "TD":
			if len(op.Operands) >= 2 {
				if ty, ok := toFloat(op.Operands[1]); ok {
					textY += ty
					leading = -ty
				}
			}
			extend(r.End)
		case "TL":
			if len(op.Operands) >= 1 {
				if l, ok := toFloat(op.Operands[0]); ok {
					leading = l
				}
			}
			extend(r.End)
		case "T*":
			textY -= leading
			extend(r.End)
		default:
			// Show-text operators; ' and " advance to the next line first.
			if op.Operator == "'" || op.Operator == "\"" {
				textY -= leading
			}
			if cur != nil {
				cur.endByte = r.End
				if n := showTextLen(op); n > 0 {
					cur.textLen += n
					sizeChars[roundHalf(curFont)] += n
					cur.lastY = textY
					if !cur.hasY {
						cur.y, cur.hasY = textY, true
					}
				}
			}
		}
	}
	flush()
	return blocks, data, nil
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

// lineHeightFactor estimates a text line's height as font size times this.
const lineHeightFactor = 1.2

// maxLineGapRatio is the largest vertical gap, in line-heights, between two
// lines that still merge into one paragraph. A wider gap reads as a paragraph
// break. Kept near a single leading so a new short paragraph at typical spacing
// breaks rather than folds. A tuning constant, not spec-mandated.
const maxLineGapRatio = 1.3

// classifyBlocks assigns a role to each block, merges adjacent paragraph lines
// into multi-line paragraphs, and builds the leaf elements for one page in
// content (reading) order.
//
// Only paragraphs are merged: a wrapped heading authored as separate text
// objects stays as separate heading elements, which over-segments (safe) rather
// than folding two distinct same-size headings into one (which would drop a
// heading from the hierarchy). Two blocks merge only when nothing but
// whitespace lies between them, so intervening graphics (a rule, a background)
// is never swallowed into a paragraph and stays an artifact.
func classifyBlocks(blocks []textBlock, pageIndex int, data []byte) []*Element {
	if len(blocks) == 0 {
		return nil
	}
	body := bodyFontSize(blocks)
	levels := headingLevels(blocks, body)

	roles := make([]Role, len(blocks))
	for i, b := range blocks {
		roles[i] = roleOf(b, body, levels)
	}

	var elems []*Element
	for i := 0; i < len(blocks); {
		role := roles[i]
		start, end := blocks[i].startByte, blocks[i].endByte
		j := i + 1
		for j < len(blocks) && role == RoleP && roles[j] == RoleP &&
			blankGap(data, blocks[j-1].endByte, blocks[j].startByte) &&
			lineAdjacent(blocks[j-1], blocks[j]) {
			end = blocks[j].endByte
			j++
		}
		i = j

		if start < 0 {
			start = 0
		}
		if end > len(data) {
			end = len(data)
		}
		if start >= end {
			continue
		}
		elems = append(elems, &Element{
			Role: role,
			Span: &ContentSpan{PageIndex: pageIndex, Start: start, End: end},
		})
	}
	return elems
}

// blankGap reports whether the bytes between aEnd and bStart are absent or only
// whitespace. Because operation byte ranges are contiguous, a non-blank gap
// means an intervening operator (graphics, a cm reposition), across which two
// text objects must not be merged.
func blankGap(data []byte, aEnd, bStart int) bool {
	if aEnd < 0 || bStart > len(data) || aEnd > bStart {
		return false
	}
	return isBlank(data[aEnd:bStart])
}

// roleOf classifies one block as a heading (by font size relative to body) or a
// paragraph.
func roleOf(b textBlock, body float64, levels map[float64]int) Role {
	if body > 0 && b.fontSize >= body*headingSizeRatio {
		return headingRole(levels[roundHalf(b.fontSize)])
	}
	return RoleP
}

// lineAdjacent reports whether block b is the next line of block a: its first
// baseline sits below a's last baseline (PDF Y increases upward) by no more than
// maxLineGapRatio line-heights. Using a's last line lets a multi-line block join
// a following continuation. Blocks emitted out of top-to-bottom order yield a
// non-positive gap and never merge, the intended fail-safe. Blocks without a
// tracked baseline never merge.
func lineAdjacent(a, b textBlock) bool {
	if !a.hasY || !b.hasY {
		return false
	}
	lh := math.Max(a.fontSize, b.fontSize) * lineHeightFactor
	if lh <= 0 {
		return false
	}
	gap := a.lastY - b.y
	return gap > 0 && gap <= lh*maxLineGapRatio
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
