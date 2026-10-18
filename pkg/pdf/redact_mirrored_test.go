package pdf

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/heuristics"
)

// mirroredCase is one way of setting text whose glyphs the rendering matrix
// [Tfs×Th 0 0 Tfs 0 Trise] × Tm × CTM of ISO 32000-1, 9.4.4 turns, mirrors,
// or both. adv and ink are the page-space unit vectors along which the glyphs
// advance and along which their ink rises from the baseline, worked out by
// hand from that matrix rather than read from litho.
type mirroredCase struct {
	name     string
	tfs, tz  float64
	tm       [4]float64 // linear part of Tm
	flipCTM  bool       // 1 0 0 -1 0 792 cm
	adv, ink [2]float64
	// type3, when "above" or "below", sets the runs in a Type 3 font whose
	// /FontMatrix flips Y (ISO 32000-1, 9.6.5) and whose glyphs ink above or
	// below the glyph-space baseline.
	type3 string
}

var mirroredCases = []mirroredCase{
	{"negative Tz", 12, -100, [4]float64{1, 0, 0, 1}, false, [2]float64{-1, 0}, [2]float64{0, 1}, ""},
	{"negative Tf", -12, 100, [4]float64{1, 0, 0, 1}, false, [2]float64{-1, 0}, [2]float64{0, -1}, ""},
	{"mirrored Tm", 12, 100, [4]float64{-1, 0, 0, 1}, false, [2]float64{-1, 0}, [2]float64{0, 1}, ""},
	{"mirrored CTM", 12, 100, [4]float64{1, 0, 0, 1}, true, [2]float64{1, 0}, [2]float64{0, -1}, ""},
	{"negative Tz and Tf", -12, -100, [4]float64{1, 0, 0, 1}, false, [2]float64{1, 0}, [2]float64{0, -1}, ""},
	{"negative Tf, mirrored CTM", -12, 100, [4]float64{1, 0, 0, 1}, true, [2]float64{-1, 0}, [2]float64{0, 1}, ""},
	{"negative Tz, mirrored Tm", 12, -100, [4]float64{-1, 0, 0, 1}, false, [2]float64{1, 0}, [2]float64{0, 1}, ""},
	{"mirrored Tm and CTM", 12, 100, [4]float64{1, 0, 0, -1}, true, [2]float64{1, 0}, [2]float64{0, 1}, ""},
	{"negative Tz, mirrored Tm and CTM", 12, -100, [4]float64{-1, 0, 0, 1}, true, [2]float64{1, 0}, [2]float64{0, -1}, ""},
	{"swapped axes Tm", 12, 100, [4]float64{0, 1, 1, 0}, false, [2]float64{0, 1}, [2]float64{1, 0}, ""},
	{"negative Tz, turned Tm", 12, -100, [4]float64{0, 1, -1, 0}, false, [2]float64{0, -1}, [2]float64{-1, 0}, ""},
	{"Type 3 font matrix flipping Y", 12, 100, [4]float64{1, 0, 0, 1}, false, [2]float64{1, 0}, [2]float64{0, -1}, "above"},
	{"Type 3 font matrix and CTM flipping Y", 12, 100, [4]float64{1, 0, 0, 1}, true, [2]float64{1, 0}, [2]float64{0, 1}, "above"},
	// Glyphs drawn below the glyph-space baseline, Y down, as Ghostscript
	// writes bitmap fonts: the flipping matrix sets them upright.
	{"Type 3 glyphs below a font matrix flipping Y", 12, 100, [4]float64{1, 0, 0, 1}, false, [2]float64{1, 0}, [2]float64{0, 1}, "below"},
	{"Type 3 glyphs below, CTM flipping Y", 12, 100, [4]float64{1, 0, 0, 1}, true, [2]float64{1, 0}, [2]float64{0, -1}, "below"},
}

// type3PDF is layoutPDF with /F1 a Type 3 font whose /FontMatrix flips Y.
// Each glyph is 600 units wide and fills a 500 by 700 box, above its glyph
// space baseline unless inkBelow, which its d1 states as its bounding box
// (ISO 32000-1, 9.6.5).
func type3PDF(body string, inkBelow bool) []byte {
	proc := "600 0 0 0 500 700 d1 0 0 500 700 re f"
	if inkBelow {
		proc = "600 0 0 -700 500 0 d1 0 -700 500 700 re f"
	}
	var procs, objs string
	for i, name := range []string{"S", "E", "C", "R", "T", "K", "P"} {
		procs += fmt.Sprintf("/%s %d 0 R ", name, 7+i)
		objs += fmt.Sprintf("%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", 7+i, len(proc), proc)
	}
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox [0 0 500 700] /FontMatrix [0.001 0 0 -0.001 0 0] " +
		"/CharProcs << " + procs + ">> /Encoding << /Type /Encoding /Differences [67 /C 69 /E 75 /K 80 /P 82 /R /S /T] >> " +
		"/FirstChar 67 /LastChar 84 /Widths [600 0 600 0 0 0 0 0 600 0 0 0 0 600 0 600 600 600] /Resources << >> >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		objs +
		"trailer\n<< /Size 14 /Root 1 0 R >>\n%%EOF\n")
}

// mirroredOrigin is where both runs of a case start, in page space.
var mirroredOrigin = [2]float64{300, 400}

// mirroredKeepOffset is how far along the ink direction the "KEEP" run sits
// from "SECRET": past the descender of its own ink, and close enough that a
// zone on the wrong side of its baseline reaches the region over "SECRET".
const mirroredKeepOffset = 16

// mirroredRunPDF draws "SECRET" from mirroredOrigin and "KEEP" offset along
// the ink direction, each in its own text object, set as c says.
func mirroredRunPDF(c mirroredCase) []byte {
	run := func(text string, at [2]float64) string {
		tx, ty := at[0], at[1]
		if c.flipCTM {
			ty = 792 - ty
		}
		return fmt.Sprintf("BT /F1 %g Tf %g Tz %g %g %g %g %g %g Tm (%s) Tj ET ",
			c.tfs, c.tz, c.tm[0], c.tm[1], c.tm[2], c.tm[3], tx, ty, text)
	}
	keep := [2]float64{mirroredOrigin[0] + c.ink[0]*mirroredKeepOffset, mirroredOrigin[1] + c.ink[1]*mirroredKeepOffset}
	body := "q "
	if c.flipCTM {
		body += "1 0 0 -1 0 792 cm "
	}
	body += run("SECRET", mirroredOrigin) + run("KEEP", keep) + "Q"
	if c.type3 != "" {
		return type3PDF(body, c.type3 == "below")
	}
	return layoutPDF(body)
}

// mirroredInkRegion returns a region strictly inside the ink of "SECRET": a
// band from 3 to 8 units into the ink side of its baseline, under the cap
// height of either font, along all but the ends of its advance (48.672 units
// for Helvetica's S, E, C, R, E, T at 12pt, 43.2 for the Type 3 font's). The
// band stays clear of the baseline by more than the descender reach a zone
// adds, so only a zone on the ink side meets it.
func mirroredInkRegion(c mirroredCase) RedactRegion {
	width := 48.672
	if c.type3 != "" {
		width = 43.2
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, s := range []float64{1, width - 1} {
		for _, t := range []float64{3, 8} {
			x := mirroredOrigin[0] + c.adv[0]*s + c.ink[0]*t
			y := mirroredOrigin[1] + c.adv[1]*s + c.ink[1]*t
			minX, maxX = math.Min(minX, x), math.Max(maxX, x)
			minY, maxY = math.Min(minY, y), math.Max(maxY, y)
		}
	}
	return RedactRegion{Page: 1, Rect: [4]float64{minX, minY, maxX, maxY}}
}

// pageLetters returns every glyph extraction finds on page 1, sorted, so the
// check does not depend on the order reversed text reads in.
func pageLetters(t *testing.T, pdf []byte) string {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	elements, err := r.ExtractTextElements(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}
	var letters []string
	for _, el := range elements {
		letters = append(letters, strings.Split(el.Text, "")...)
	}
	slices.Sort(letters)
	return strings.Join(letters, "")
}

// TestWriter_Redact_ReflectedGlyphsRedactWhereTheirInkIs pins that a region
// over the ink of a run removes that run and nothing else, whichever way the
// rendering matrix turns or mirrors its glyphs (ISO 32000-1, 9.4.4).
//
// A negative Th, a mirrored Tm and a mirrored CTM each reflect a glyph, so
// its ink rises on the right of its advance instead of the left; a negative
// Tfs turns it a half turn instead. The redaction zone put the ink on the
// left whatever the reflection, so a region over a reflected run's ink missed
// it, a leak, and took the run beside it, whose zone it did meet. Removal is
// whole-operator, so the runs sit in separate text objects and "KEEP" sits on
// the far side of "SECRET"'s ink.
func TestWriter_Redact_ReflectedGlyphsRedactWhereTheirInkIs(t *testing.T) {
	t.Parallel()
	for _, c := range mirroredCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := mirroredRunPDF(c)
			if got := pageLetters(t, src); got != "CEEEEKPRST" {
				t.Fatalf("source page letters = %q, want both runs", got)
			}
			out := redactAndWrite(t, src, []RedactRegion{mirroredInkRegion(c)})
			if got := pageLetters(t, out); got != "EEKP" {
				t.Errorf("after redacting SECRET's ink the page reads %q, want only KEEP's letters", got)
			}
		})
	}
}

// TestReader_ExtractTextElements_TurnedAndReflectedRunsCoalesce pins that
// each run of [mirroredCases] comes out of extraction as one element that
// says which side of its advance it inks, and whose zone meets the region
// over its own ink and not its neighbour's: the zone
// [heuristics.DetectUnappliedRedactions] tests a /Redact mark against.
//
// Glyphs coalesce within a tolerance proportional to the font size, and the
// size is negative when Tfs is, so a run set at a negative Tfs stayed one
// element per glyph.
func TestReader_ExtractTextElements_TurnedAndReflectedRunsCoalesce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, c := range mirroredCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := mirroredRunPDF(c)
			r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			elements, err := r.ExtractTextElements(ctx, 1)
			if err != nil {
				t.Fatalf("ExtractTextElements: %v", err)
			}
			if len(elements) != 2 || elements[0].Text != "SECRET" || elements[1].Text != "KEEP" {
				var texts []string
				for _, el := range elements {
					texts = append(texts, el.Text)
				}
				t.Fatalf("elements = %q, want [SECRET KEEP]", texts)
			}
			// The ink rises clockwise from the advance exactly when the
			// glyphs are reflected.
			wantInkRight := c.adv[0]*c.ink[1]-c.adv[1]*c.ink[0] < 0
			region := mirroredInkRegion(c).Rect
			zone := heuristics.ZoneFromRect(region)
			for i, el := range elements {
				if el.InkRight != wantInkRight {
					t.Errorf("%s: InkRight = %v, want %v", el.Text, el.InkRight, wantInkRight)
				}
				if got, want := heuristics.ZonesOverlap(heuristics.ElementZone(el), zone), i == 0; got != want {
					t.Errorf("%s: zone meets the region over SECRET's ink = %v, want %v", el.Text, got, want)
				}
			}
		})
	}
}
