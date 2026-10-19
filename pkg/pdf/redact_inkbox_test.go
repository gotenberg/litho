package pdf

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/heuristics"
)

// inkBoxCase sets "SEC" and "RET" in two show operators of one text object,
// and "KEEP" in another, in a Type 3 font whose glyphs are solid boxes. The
// glyph's ink reaches past the font size, or off the side of the advance the
// size alone gives it, as the font matrix, the text matrix or the CTM has it
// (ISO 32000-1, 9.4.4 and 9.6.5).
type inkBoxCase struct {
	name string
	// fm is the font's /FontMatrix, glyph its ink box in glyph space and wx
	// its advance there.
	fm    [6]float64
	glyph [4]float64
	wx    float64
	// image draws each glyph as a one-sample image mask scaled over glyph
	// after d0, as bitmap fonts do, rather than a filled d1 box.
	image bool
	tfs   float64
	tm    [4]float64 // linear part of Tm; the runs start at inkBoxOrigin
	cm    [6]float64
}

var identityCM = [6]float64{1, 0, 0, 1, 0, 0}

var inkBoxCases = []inkBoxCase{
	// Glyphs drawn in hundredths of a text space unit, shown at Tf 1: each
	// inks 7 units high, where a zone a font size high reaches 1.
	{"Tf 1, FontMatrix 0.01", [6]float64{0.01, 0, 0, 0.01, 0, 0}, [4]float64{0, 0, 500, 700}, 600, false, 1, [4]float64{1, 0, 0, 1}, identityCM},
	{"Tf 1, FontMatrix 0.001", [6]float64{0.001, 0, 0, 0.001, 0, 0}, [4]float64{0, 0, 5000, 7000}, 6000, false, 1, [4]float64{1, 0, 0, 1}, identityCM},
	{"Tf 1, identity FontMatrix, bitmap glyphs", [6]float64{1, 0, 0, 1, 0, 0}, [4]float64{0, 0, 5, 7}, 6, true, 1, [4]float64{1, 0, 0, 1}, identityCM},
	// A negative X scale draws each glyph left of its origin and runs the
	// advance leftward.
	{"negative X FontMatrix", [6]float64{-0.001, 0, 0, 0.001, 0, 0}, [4]float64{0, 0, 500, 700}, 600, false, 12, [4]float64{1, 0, 0, 1}, identityCM},
	{"skewed Tm", [6]float64{0.001, 0, 0, 0.001, 0, 0}, [4]float64{0, 0, 500, 700}, 600, false, 12, [4]float64{1, 0, 1, 1}, identityCM},
	{"skewed CTM", [6]float64{0.001, 0, 0, 0.001, 0, 0}, [4]float64{0, 0, 500, 700}, 600, false, 12, [4]float64{1, 0, 0, 1}, [6]float64{1, 0, 1, 1, -400, 0}},
	{"rotated 90", [6]float64{0.001, 0, 0, 0.001, 0, 0}, [4]float64{0, 0, 500, 700}, 600, false, 12, [4]float64{0, 1, -1, 0}, identityCM},
}

// inkBoxOrigin is where "SECRET" starts, in the coordinates Tm translates to.
var inkBoxOrigin = [2]float64{300, 400}

// inkHeight is the height of a glyph's ink in text space, before the text
// matrix.
func (c inkBoxCase) inkHeight() float64 {
	h := 0.0
	for _, y := range []float64{c.glyph[1], c.glyph[3]} {
		for _, x := range []float64{c.glyph[0], c.glyph[2]} {
			h = max(h, math.Abs(c.fm[1]*x+c.fm[3]*y+c.fm[5]))
		}
	}
	return h * c.tfs
}

// toPage carries the text space point (x, y) of a run starting at origin to
// page space, through Tm and then the CTM.
func (c inkBoxCase) toPage(origin [2]float64, x, y float64) [2]float64 {
	ux := x*c.tm[0] + y*c.tm[2] + origin[0]
	uy := x*c.tm[1] + y*c.tm[3] + origin[1]
	m := c.cm
	return [2]float64{ux*m[0] + uy*m[2] + m[4], ux*m[1] + uy*m[3] + m[5]}
}

// keepOrigin is where "KEEP" starts: two ink heights above "SECRET" in text
// space, clear of its ink whichever way the matrices turn it.
func (c inkBoxCase) keepOrigin() [2]float64 {
	d := 2 * c.inkHeight()
	return [2]float64{inkBoxOrigin[0] + d*c.tm[2], inkBoxOrigin[1] + d*c.tm[3]}
}

// region returns a region over the top of the ink of "RET", the fourth to
// sixth glyphs of "SECRET": a band from 85 to 95 hundredths up their glyph
// boxes, along all but half a unit at either end of their ink. Removal is
// whole-operator, so the region meets every glyph "RET" shows, and none of
// "SEC". Except in the turned case, the band lies past the reach a zone sized
// by the font size gives the glyphs.
func (c inkBoxCase) region() RedactRegion {
	g, fm := c.glyph, c.fm
	txMin, txMax := math.Inf(1), math.Inf(-1)
	for k := 3; k <= 5; k++ {
		for _, gx := range []float64{g[0], g[2]} {
			tx := float64(k)*c.wx*fm[0]*c.tfs + (fm[0]*gx+fm[4])*c.tfs
			txMin, txMax = math.Min(txMin, tx), math.Max(txMax, tx)
		}
	}
	const inset = 0.5
	txMin, txMax = txMin+inset, txMax-inset
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, f := range []float64{0.85, 0.95} {
		ty := (fm[3]*(g[1]+f*(g[3]-g[1])) + fm[5]) * c.tfs
		for _, tx := range []float64{txMin, txMax} {
			p := c.toPage(inkBoxOrigin, tx, ty)
			minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
			minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
		}
	}
	return RedactRegion{Page: 1, Rect: [4]float64{minX, minY, maxX, maxY}}
}

// pdf returns the case's document.
func (c inkBoxCase) pdf() []byte {
	g := c.glyph
	proc := fmt.Sprintf("%g 0 %g %g %g %g d1 %g %g %g %g re f", c.wx, g[0], g[1], g[2], g[3], g[0], g[1], g[2]-g[0], g[3]-g[1])
	bbox := fmt.Sprintf("[%g %g %g %g]", g[0], g[1], g[2], g[3])
	if c.image {
		// A d0 glyph states no box, and the font states an empty one, so
		// only what the description paints bounds the glyph.
		proc = fmt.Sprintf("%g 0 d0 q %g 0 0 %g %g %g cm BI /W 1 /H 1 /BPC 1 /IM true ID \x00 EI Q", c.wx, g[2]-g[0], g[3]-g[1], g[0], g[1])
		bbox = "[0 0 0 0]"
	}
	run := func(origin [2]float64, shows string) string {
		return fmt.Sprintf("BT /F1 %g Tf %g %g %g %g %g %g Tm %s ET ", c.tfs, c.tm[0], c.tm[1], c.tm[2], c.tm[3], origin[0], origin[1], shows)
	}
	m := c.cm
	body := fmt.Sprintf("q %g %g %g %g %g %g cm ", m[0], m[1], m[2], m[3], m[4], m[5]) +
		run(inkBoxOrigin, "(SEC) Tj (RET) Tj") + run(c.keepOrigin(), "(KEEP) Tj") + "Q"
	var procs, objs string
	for i, name := range []string{"S", "E", "C", "R", "T", "K", "P"} {
		procs += fmt.Sprintf("/%s %d 0 R ", name, 7+i)
		objs += fmt.Sprintf("%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", 7+i, len(proc), proc)
	}
	w := strconv.FormatFloat(c.wx, 'g', -1, 64)
	widths := fmt.Sprintf("[%s 0 %s 0 0 0 0 0 %s 0 0 0 0 %s 0 %s %s %s]", w, w, w, w, w, w, w)
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		fmt.Sprintf("4 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox %s /FontMatrix [%g %g %g %g %g %g] ", bbox, c.fm[0], c.fm[1], c.fm[2], c.fm[3], c.fm[4], c.fm[5]) +
		"/CharProcs << " + procs + ">> /Encoding << /Type /Encoding /Differences [67 /C 69 /E 75 /K 80 /P 82 /R /S /T] >> " +
		"/FirstChar 67 /LastChar 84 /Widths " + widths + " /Resources << >> >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		objs +
		"trailer\n<< /Size 14 /Root 1 0 R >>\n%%EOF\n")
}

// TestWriter_Redact_RemovesTheGlyphWhoseInkARegionCovers pins that a region
// over the ink of the glyphs one show operator draws removes that operator
// and nothing else, wherever the font, text and transformation matrices put
// the ink.
//
// The redaction zone of a glyph reached a font size above its baseline and
// ran along its advance. A Type 3 glyph is sized by its /FontMatrix, not its
// font size (ISO 32000-1, 9.6.5), so one drawn in pixels and shown at Tf 1
// inked far above its zone; a negative X scale in the matrix drew it left of
// its origin, off the advance; and a skewed text matrix or CTM leaned it past
// the end of its advance. A region over that ink selected nothing, and the
// glyph survived the redaction.
func TestWriter_Redact_RemovesTheGlyphWhoseInkARegionCovers(t *testing.T) {
	t.Parallel()
	for _, c := range inkBoxCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := c.pdf()
			if got := pageLetters(t, src); got != "CEEEEKPRST" {
				t.Fatalf("source page letters = %q, want both runs", got)
			}
			out := redactAndWrite(t, src, []RedactRegion{c.region()})
			if got := pageLetters(t, out); got != "CEEEKPS" {
				t.Errorf("after redacting the ink of RET the page reads %q, want every letter but those of RET", got)
			}
		})
	}
}

// TestReader_ExtractTextElements_InkBoxHoldsTheInk pins that the elements
// extraction returns record ink boxes that meet the region over the ink of
// "RET", the zone [heuristics.DetectUnappliedRedactions] tests a /Redact mark
// against, and that the box of the run beside it does not.
func TestReader_ExtractTextElements_InkBoxHoldsTheInk(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, c := range inkBoxCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := c.pdf()
			r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			elements, err := r.ExtractTextElements(ctx, 1)
			if err != nil {
				t.Fatalf("ExtractTextElements: %v", err)
			}
			// A /FontMatrix with a negative X scale runs the advance
			// leftward (ISO 32000-1, 9.6.5), so its glyphs coalesce only
			// when the advance direction carries that sign.
			if len(elements) != 2 || elements[0].Text != "SECRET" || elements[1].Text != "KEEP" {
				var texts []string
				for _, el := range elements {
					texts = append(texts, el.Text)
				}
				t.Fatalf("elements = %q, want [SECRET KEEP]", texts)
			}
			zone := heuristics.ZoneFromRect(c.region().Rect)
			var met []string
			for _, el := range elements {
				if heuristics.ZonesOverlap(heuristics.ElementZone(el), zone) {
					met = append(met, el.Text)
				}
			}
			if !slices.Equal(met, []string{"SECRET"}) {
				t.Errorf("elements whose zone meets the region over RET's ink = %q, want [SECRET]", met)
			}
		})
	}
}
