package typography

import (
	"context"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
)

// overdrawMaxOffset bounds, in ems of the run's font size, how far a run may
// lie from an earlier run spelling the same characters and still be the same
// text drawn again: a drop shadow, an emboss or a fake bold drawn two to four
// times. Not spec-mandated. Producers offset such a copy by a few hundredths
// of an em for a fake bold and by about a tenth for a shadow, while two
// pieces of text a reader tells apart, the cells of a column or a word
// repeated on a line, lie at least a glyph's advance apart.
const overdrawMaxOffset = 0.25

// overdrawMaxAdvanceShare bounds the same offset along the baseline as a
// share of each glyph's advance, so that a run set one glyph at a time, each
// glyph its own run, never matches its neighbour spelling the same character:
// "ll" in a narrow font lies under a quarter of an em apart, but a whole
// advance. Not spec-mandated: a copy drawn for an effect lies over the glyph
// it repeats, under half its advance from it.
const overdrawMaxAdvanceShare = 0.5

// overdrawSlack is how far, in ems, the offsets of two glyph pairs of one
// match may differ: a copy is the run moved as a whole, so every glyph moves
// alike, to the rounding of the producer's numbers. Not spec-mandated.
const overdrawSlack = 0.01

// overdrawSizeSlack is the relative difference of font size two runs may
// show and still be one text drawn twice. Not spec-mandated: it admits the
// rounding of a producer's matrices.
const overdrawSizeSlack = 0.01

// overdrawAngleSlack is the largest difference, in radians, between the
// baselines of two glyphs one copy holds. Not spec-mandated: it admits the
// rounding of a producer's matrices.
const overdrawAngleSlack = 0.01

// overdrawRun is one run of elements drawn by one show-text chain: a span of
// consecutive elements sharing a [models.TextElement.Chunk].
type overdrawRun struct {
	start, end int
	text       string
	size       float64
}

// overdrawCell keys the index of runs [dropOverdrawn] keeps: the run's text,
// its font size rounded, and the cell of its first origin on a grid of
// [overdrawMaxOffset] ems.
type overdrawCell struct {
	text string
	size int64
	x, y int64
}

// dropOverdrawn returns elements, and fonts parallel to them when not nil,
// without the runs a later run draws again over themselves: the same
// characters at the same size, along the same direction, moved as a whole by
// under [overdrawMaxOffset] ems on each axis and under
// [overdrawMaxAdvanceShare] of each glyph's advance along it. Of each set of
// such copies the last drawn stays, the one a reader sees on top (ISO
// 32000-1, 8.2: later marks paint over earlier ones).
//
// A copy drawn so is one piece of text shown several times for an effect, a
// drop shadow, an emboss, a fake bold, and the page shows it once. The text
// of a page is what it shows, so it is read once. PDF.js's getTextContent
// keeps every copy (src/core/evaluator.js holds no such filter); this reads
// the words a sighted reader reads.
//
// # Corpus evidence
//
//   - govdocs thread_200_200433.pdf and thread_200_200835.pdf: PowerPoint
//     slides draw each line in a dark shadow and again in white a tenth of an
//     em away, read "OutlineOutline" and "PurposePurpose" once the white copy
//     is judged visible.
func dropOverdrawn(ctx context.Context, elements []models.TextElement, fonts []runFont) ([]models.TextElement, []runFont, error) {
	runs := overdrawRuns(elements)
	if len(runs) < 2 {
		return elements, fonts, nil
	}
	dropped := make([]bool, len(runs))
	kept := make(map[overdrawCell][]int)
	any := false
	for k, run := range runs {
		if k%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		if strings.TrimSpace(run.text) == "" || !(run.size > 0) {
			continue
		}
		key := overdrawKey(elements, run)
		for dx := int64(-1); dx <= 1; dx++ {
			for dy := int64(-1); dy <= 1; dy++ {
				c := key
				c.x, c.y = c.x+dx, c.y+dy
				idx := kept[c]
				for n := 0; n < len(idx); n++ {
					if overdraws(elements, runs[idx[n]], run) {
						dropped[idx[n]] = true
						any = true
						idx = append(idx[:n], idx[n+1:]...)
						n--
					}
				}
				kept[c] = idx
			}
		}
		kept[key] = append(kept[key], k)
	}
	if !any {
		return elements, fonts, nil
	}
	outEl := make([]models.TextElement, 0, len(elements))
	var outFonts []runFont
	if fonts != nil {
		outFonts = make([]runFont, 0, len(fonts))
	}
	for k, run := range runs {
		if dropped[k] {
			continue
		}
		outEl = append(outEl, elements[run.start:run.end]...)
		if fonts != nil {
			outFonts = append(outFonts, fonts[run.start:run.end]...)
		}
	}
	return outEl, outFonts, nil
}

// overdrawRuns splits elements into runs of consecutive elements sharing a
// chunk.
func overdrawRuns(elements []models.TextElement) []overdrawRun {
	var runs []overdrawRun
	for i := 0; i < len(elements); {
		j := i + 1
		for j < len(elements) && elements[j].Chunk == elements[i].Chunk {
			j++
		}
		var b strings.Builder
		for _, el := range elements[i:j] {
			b.WriteString(el.Text)
		}
		runs = append(runs, overdrawRun{start: i, end: j, text: b.String(), size: math.Abs(elements[i].FontSize)})
		i = j
	}
	return runs
}

// overdrawKey returns the index cell of run.
func overdrawKey(elements []models.TextElement, run overdrawRun) overdrawCell {
	cell := overdrawMaxOffset * run.size
	first := elements[run.start]
	return overdrawCell{
		text: run.text,
		size: int64(math.Round(math.Log(run.size) / overdrawSizeSlack)),
		x:    int64(math.Floor(first.X / cell)),
		y:    int64(math.Floor(first.Y / cell)),
	}
}

// overdraws reports later drawing earlier again over itself (see
// [dropOverdrawn]).
func overdraws(elements []models.TextElement, earlier, later overdrawRun) bool {
	if earlier.text != later.text || earlier.end-earlier.start != later.end-later.start {
		return false
	}
	if math.Abs(earlier.size-later.size) > overdrawSizeSlack*math.Max(earlier.size, later.size) {
		return false
	}
	em := later.size
	var shift [2]float64
	for n := range later.end - later.start {
		a, b := elements[earlier.start+n], elements[later.start+n]
		if a.Text != b.Text || a.Vertical != b.Vertical || math.Abs(a.Angle-b.Angle) > overdrawAngleSlack {
			return false
		}
		dx, dy := b.X-a.X, b.Y-a.Y
		if n == 0 {
			shift = [2]float64{dx, dy}
		} else if math.Abs(dx-shift[0]) > overdrawSlack*em || math.Abs(dy-shift[1]) > overdrawSlack*em {
			return false
		}
		if math.Abs(dx) >= overdrawMaxOffset*em || math.Abs(dy) >= overdrawMaxOffset*em {
			return false
		}
		// Along the baseline, measured in the glyph's own direction.
		along := math.Abs(dx*math.Cos(b.Angle) + dy*math.Sin(b.Angle))
		if b.Vertical {
			along = math.Abs(-dx*math.Sin(b.Angle) + dy*math.Cos(b.Angle))
		}
		if w := math.Max(math.Abs(a.Width), math.Abs(b.Width)); w > 0 && along >= overdrawMaxAdvanceShare*w {
			return false
		}
	}
	return true
}
