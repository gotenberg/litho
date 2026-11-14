package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// letter returns the initial letter node of text s, initial-letter size
// and sink.
func letter(s string, size float64, sink int) *Node {
	st := sty(func(s *css.Style) {
		s.Float = css.FloatLeft
		s.InitialLetter = css.InitialLetter{Size: size, Sink: sink}
	})
	n := el(st, txt(st, s))
	n.InitialLetter = true
	return n
}

// TestInitialLetter_Geometry checks an initial letter's size and place in
// Ahem 10px on 10px lines (CSS Inline 3, 4.4.3 and 4.6.1, as Chromium
// places it): its cap height spans the size's lines, (size - 1) * 10 +
// 8 px, so its em is that over 0.8; the lines beside it start at its
// right edge, rounded up to a pixel; its cap height sits on the first
// line's; a raised or sunken letter moves the first line down by the
// lines it rises.
func TestInitialLetter_Geometry(t *testing.T) {
	tests := []struct {
		name       string
		size       float64
		sink       int
		letterSize float64
		// baseline is the letter's baseline; lines the left edge and
		// baseline of the text's lines, in pixels.
		baseline float64
		lines    [][2]float64
	}{
		{"drop 3", 3, 3, 35, 28, [][2]float64{{35, 8}, {35, 18}, {35, 28}, {35, 38}, {0, 48}}},
		{"sunk 3 2", 3, 2, 35, 28, [][2]float64{{35, 18}, {35, 28}, {35, 38}, {0, 48}}},
		{"raised 3 1", 3, 1, 35, 28, [][2]float64{{35, 28}, {35, 38}, {0, 48}}},
		{"fractional 2.5", 2.5, 2, 28.75, 23, [][2]float64{{29, 8}, {29, 18}, {29, 28}, {0, 38}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := el(sty(), letter("A", tt.size, tt.sink), txt(sty(), "bb cc dd ee ff gg hh"))
			res := runWith(t, doc(p), Options{Page: page(60, 200)})
			var got [][2]float64
			for _, r := range texts(res, 1) {
				if r.text == "A" {
					if !near(r.run.Size, px(tt.letterSize)) || !near(r.y/css.Pt, tt.baseline) || !near(r.x, 0) {
						t.Errorf("letter at %v, %v px, size %v", r.x/css.Pt, r.y/css.Pt, r.run.Size/css.Pt)
					}
					continue
				}
				if len(got) == 0 || !near(got[len(got)-1][1], r.y/css.Pt) {
					got = append(got, [2]float64{r.x / css.Pt, r.y / css.Pt})
				}
			}
			if len(got) < len(tt.lines) {
				t.Fatalf("lines at %v, want %v first", got, tt.lines)
			}
			for i, w := range tt.lines {
				if !near(got[i][0], w[0]) || !near(got[i][1], w[1]) {
					t.Errorf("lines at %v, want %v first", got, tt.lines)
					break
				}
			}
		})
	}
}

// TestInitialLetter_Clearance checks that the paragraph after a short one
// wraps around its initial letter, and that a paragraph that starts with
// an initial letter goes below it (CSS Inline 3, 4.9.2).
func TestInitialLetter_Clearance(t *testing.T) {
	first := el(sty(), letter("A", 3, 3), txt(sty(), "bb"))
	wraps := el(sty(), txt(sty(), "cc"))
	clears := el(sty(), letter("D", 2, 2), txt(sty(), "ee"))
	res := runWith(t, doc(first, wraps, clears), Options{Page: page(100, 200)})
	at := map[string][2]float64{}
	for _, r := range texts(res, 1) {
		at[r.text] = [2]float64{r.x / css.Pt, r.y / css.Pt}
	}
	if got := at["cc"]; !near(got[0], 35) || !near(got[1], 18) {
		t.Errorf("cc at %v, want beside the letter at 35, 18", got)
	}
	// The letter A ends at 35px: D's paragraph starts there, its letter's
	// baseline on its second line, (2 - 1) * 10 + 8 px down.
	if got := at["D"]; !near(got[0], 0) || !near(got[1], 35+18) {
		t.Errorf("D at %v, want 0, 53", got)
	}
	if got := at["ee"]; !near(got[1], 35+8) {
		t.Errorf("ee at %v, want on the line at 43", got)
	}
}

// TestInitialLetter_Indent checks that the text-indent of the first line
// moves an initial letter by twice its value and leaves the text beside
// it at its edge, as Chromium lays it out.
func TestInitialLetter_Indent(t *testing.T) {
	ind := func(s *css.Style) { s.TextIndent = css.Pts(px(5)) }
	p := el(sty(ind), letter("A", 2, 2), txt(sty(ind), "bb cc dd"))
	res := runWith(t, doc(p), Options{Page: page(100, 200)})
	var line []string
	for _, r := range texts(res, 1) {
		line = append(line, r.text)
		if r.text == "A" && !near(r.x/css.Pt, 10) {
			t.Errorf("letter at %v px, want 10", r.x/css.Pt)
		}
		if strings.HasPrefix(r.text, "bb") && !near(r.x/css.Pt, 10+23) {
			t.Errorf("text at %v px, want 33", r.x/css.Pt)
		}
	}
	if len(line) == 0 {
		t.Fatal("nothing drawn")
	}
}

// TestInitialLetter_MarginMovesTheLetter locks that the margins of an
// initial letter move it, its glyphs with its border box, by its
// block-start and inline-start margins, as Chromium draws it (CSS Inline
// 3, 4.6).
func TestInitialLetter_MarginMovesTheLetter(t *testing.T) {
	l := letter("A", 3, 3)
	l.Style.Margin = dims(4, 0, 0, 3)
	p := el(sty(), l, txt(sty(), "bb cc dd ee"))
	res := runWith(t, doc(p), Options{Page: page(60, 200)})
	for _, r := range texts(res, 1) {
		if r.text == "A" && (!near(r.x/css.Pt, 3) || !near(r.y/css.Pt, 28+4)) {
			t.Errorf("letter at %v, %v px, want 3, 32", r.x/css.Pt, r.y/css.Pt)
		}
	}
}

// TestInitialLetter_InheritsFromFirstLine locks CSS Pseudo 4, 2.1.3: an
// initial letter, though laid out as a float, inherits from the
// ::first-line of its paragraph, here its colour.
func TestInitialLetter_InheritsFromFirstLine(t *testing.T) {
	l := letter("A", 3, 3)
	p := el(sty(), l, txt(sty(), "bb cc dd ee"))
	o := Options{Page: page(60, 200), FirstLine: lineStyler{
		pseudo:  map[*Node]func(*css.Style){p: fg(blue)},
		restyle: map[*Node]func(*css.Style){l: func(*css.Style) {}},
	}}
	img := render(t, runWith(t, doc(p), o), 1)
	if got := code(img.At(10, 20)); got != 'B' {
		t.Errorf("letter %c, want blue", got)
	}
}

// TestInitialLetter_FloatsBeside locks CSS Inline 3, 7.9.3: a left float
// that starts on a line beside a left initial letter clears it, and the
// clear of a float ignores the letter, a right float of clear: left
// staying on the line it starts on.
func TestInitialLetter_FloatsBeside(t *testing.T) {
	for _, c := range []struct {
		name string
		mod  func(*css.Style)
		y    float64
	}{
		{"left", float(css.FloatLeft), 35},
		{"right clear left", func(s *css.Style) { s.Float, s.Clear = css.FloatRight, css.ClearLeft }, 10},
	} {
		t.Run(c.name, func(t *testing.T) {
			fl := el(sty(width(5), height(5), c.mod))
			p := el(sty(width(60)), letter("A", 3, 3), txt(sty(), "bb"), br(sty()), fl, txt(sty(), "cc dd ee"))
			b := boxes(t, doc(p), page(60, 200))
			var got *box
			var walk func(*box)
			walk = func(k *box) {
				for _, x := range k.kids {
					if x.node == fl {
						got = x
					}
					walk(x)
				}
			}
			walk(b)
			if got == nil || !near(got.y/css.Pt, c.y) {
				t.Errorf("float %v, want its top at %v px", got, c.y)
			}
		})
	}
}

// TestInitialLetter_Tab checks a preserved tab that starts an initial
// letter: it advances to the tab stop of its paragraph's font, eight
// spaces of 10px (CSS Text 3, 4.2, the space of the nearest block
// container), and stays in the letter's box before its glyph.
//
// # Corpus evidence
//
//   - wpt: css-inline/initial-letter/initial-letter-with-tab and
//     initial-letter-with-tab-rtl.
func TestInitialLetter_Tab(t *testing.T) {
	pre := func(s *css.Style) { s.WhiteSpace = css.Pre }
	l := letter("\tA", 3, 3)
	l.Style.WhiteSpace = css.Pre
	l.Children[0].Style = l.Style
	p := el(sty(pre), l, txt(sty(pre), "bb"))
	res := runWith(t, doc(p), Options{Page: page(300, 200)})
	for _, r := range texts(res, 1) {
		switch strings.TrimLeft(r.text, "\t") {
		case "A":
			if !near(r.x/css.Pt, 80) {
				t.Errorf("letter at %v px, want 80", r.x/css.Pt)
			}
		case "bb":
			if !near(r.x/css.Pt, 115) {
				t.Errorf("text beside the letter at %v px, want 115", r.x/css.Pt)
			}
		}
	}
}
