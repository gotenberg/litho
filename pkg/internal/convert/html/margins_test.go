package html

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// placed is a text run as it lands on a page: its text, the left edge of
// its baseline origin and the baseline, in page coordinates.
type placed struct {
	text string
	x, y float64
	size float64
}

// pageTexts returns the text runs of pg in paint order, groups' transforms
// applied (translations only, as the layout writes them).
func pageTexts(pg *compose.Page) []placed {
	var out []placed
	var walkFrags func(frags []compose.Fragment, dx, dy float64)
	walkFrags = func(frags []compose.Fragment, dx, dy float64) {
		for _, f := range frags {
			switch v := f.(type) {
			case *compose.Group:
				m := v.Transform
				if m == (compose.Matrix{}) {
					m = compose.Identity
				}
				walkFrags(v.Content, dx+m.E, dy+m.F)
			case *compose.Text:
				var b strings.Builder
				for _, g := range v.Glyphs {
					b.WriteString(g.Text)
				}
				out = append(out, placed{text: b.String(), x: dx + v.Origin.X, y: dy + v.Origin.Y, size: v.Size})
			}
		}
	}
	walkFrags(pg.Content, 0, 0)
	return out
}

// find returns the first run of pg that holds s, or with the runs after
// it on its baseline starts with s, at that run's origin.
func find(t *testing.T, pg *compose.Page, s string) placed {
	t.Helper()
	runs := pageTexts(pg)
	for i := range runs {
		line := runs[i]
		for j := i + 1; j < len(runs) && runs[j].y == line.y; j++ {
			line.text += runs[j].text
		}
		if strings.Contains(runs[i].text, s) || strings.HasPrefix(line.text, s) {
			return line
		}
	}
	t.Fatalf("no run holds %q in %+v", s, pageTexts(pg))
	return placed{}
}

// hasText reports whether a run of pg contains s.
func hasText(pg *compose.Page, s string) bool {
	for _, p := range pageTexts(pg) {
		if strings.Contains(p.text, s) {
			return true
		}
	}
	return false
}

// structured reports whether a text run of frags hangs from the structure
// tree.
func structured(frags []compose.Fragment) bool {
	for _, f := range frags {
		switch v := f.(type) {
		case *compose.Group:
			if structured(v.Content) {
				return true
			}
		case *compose.Text:
			if v.Parent != nil {
				return true
			}
		}
	}
	return false
}

// convertDoc converts src with o and the test fonts.
func convertDoc(t *testing.T, src string, o Options) *compose.Document {
	t.Helper()
	doc, err := Convert(context.Background(), strings.NewReader(src), testFonts(t), o)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return doc
}

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestMarginBoxes_PositionsAndCounters(t *testing.T) {
	src := `<style>
		@page { size: 400pt 300pt; margin: 40pt 50pt; font: 10pt "Test Serif";
			@top-left { content: "TL" }
			@top-center { content: "TC" }
			@bottom-right { content: "Page " counter(page) " of " counter(pages, upper-roman) }
			@top-left-corner { content: "K"; text-align: left; vertical-align: top }
			@left-middle { content: "LM" }
		}
		@page :first { @top-center { content: none } }
		body { margin: 0 } p { margin: 0; break-after: page }
	</style><p>one</p><p>two</p>`
	doc := convertDoc(t, src, Options{PreferCSSPageSize: true})
	if len(doc.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(doc.Pages))
	}
	p1, p2 := doc.Pages[0], doc.Pages[1]
	// Text Serif's glyphs are half an em wide: "TL" is 10pt wide. With no
	// centre box, top-left takes the whole available width, left aligned.
	if tl := find(t, p1, "TL"); !approx(tl.x, 50, 0.01) {
		t.Errorf("top-left at x %v, want 50", tl.x)
	}
	if hasText(p1, "TC") || !hasText(p2, "TC") {
		t.Error("top-center: none on the first page, drawn on the second")
	}
	// top-center is centred in the 300pt between the corners: its text
	// at 200-5.
	if tc := find(t, p2, "TC"); !approx(tc.x, 195, 0.01) {
		t.Errorf("top-center at x %v, want 195", tc.x)
	}
	// Middle of the 40pt top margin: the line box is centred.
	tl, k := find(t, p1, "TL"), find(t, p1, "K")
	if !(tl.y > 20 && tl.y < 40) || !(k.y < tl.y) {
		t.Errorf("top-left baseline %v, corner %v: want the corner top-aligned above the centred box", tl.y, k.y)
	}
	if !approx(k.x, 0, 0.01) {
		t.Errorf("corner at x %v, want 0", k.x)
	}
	// bottom-right is right aligned at the page area's right edge, 350.
	br := find(t, p2, "Page 2 of II")
	if w := float64(len("Page 2 of II")) * 5; !approx(br.x+w, 350, 0.01) {
		t.Errorf("bottom-right ends at %v, want 350", br.x+w)
	}
	if br.y < 260 {
		t.Errorf("bottom-right baseline %v, want in the bottom margin", br.y)
	}
	if lm := find(t, p1, "LM"); !approx(lm.x, 25-5, 0.01) || !(lm.y > 140 && lm.y < 160) {
		t.Errorf("left-middle at %v,%v: want centred in the left margin, mid-page", lm.x, lm.y)
	}
}

func TestMarginBoxes_VariableWidths(t *testing.T) {
	// CSS Page 3, 5.3.2.2: max-content widths 30pt and 20pt share the
	// 250pt left over in proportion: 30+150 and 20+100.
	src := `<style>
		@page { size: 400pt 300pt; margin: 40pt 50pt; font: 10pt "Test Serif";
			@top-left { content: "xxxxxx"; text-align: right }
			@top-right { content: "yyyy"; text-align: left }
		}
	</style><p>x</p>`
	p := convertDoc(t, src, Options{PreferCSSPageSize: true}).Pages[0]
	if r := find(t, p, "xxxxxx"); !approx(r.x+30, 50+180, 0.01) {
		t.Errorf("top-left ends at %v, want 230", r.x+30)
	}
	if r := find(t, p, "yyyy"); !approx(r.x, 230, 0.01) {
		t.Errorf("top-right starts at %v, want 230", r.x)
	}
}

func TestFixedAxis_Rules(t *testing.T) {
	st := css.Initial()
	auto := css.Dimension{Kind: css.Auto}
	tests := []struct {
		name           string
		size, m1, m2   css.Dimension
		outer          bool
		wantM1, wantSz float64
	}{
		{"auto size fills", auto, css.Pts(2), css.Pts(3), true, 2, 95},
		{"over-constrained, outer start gives way", css.Pts(10), css.Pts(0), css.Pts(0), true, 90, 10},
		{"over-constrained, outer end gives way", css.Pts(10), css.Pts(0), css.Pts(0), false, 0, 10},
		{"both margins auto centre", css.Pts(10), auto, auto, true, 45, 10},
		{"auto margins zero when overflowing", css.Pts(200), auto, auto, false, 0, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := fixedAxis(st, tt.size, tt.m1, tt.m2, 0, 100, 100, tt.outer)
			if !approx(a.m1, tt.wantM1, 1e-9) || !approx(a.size, tt.wantSz, 1e-9) {
				t.Errorf("m1 %v size %v, want %v %v", a.m1, a.size, tt.wantM1, tt.wantSz)
			}
		})
	}
}

func TestVariableSizes(t *testing.T) {
	tests := []struct {
		name string
		s    [3]sizing
		want [3]float64
	}{
		{"one box takes all", [3]sizing{{gen: true, auto: true, minO: 10, maxO: 20}}, [3]float64{100, 0, 0}},
		{"proportional to max-content", [3]sizing{{gen: true, auto: true, minO: 30, maxO: 30}, {}, {gen: true, auto: true, minO: 20, maxO: 20}}, [3]float64{60, 0, 40}},
		{"min-content when max overflows", [3]sizing{{gen: true, auto: true, minO: 10, maxO: 110}, {}, {gen: true, auto: true, minO: 10, maxO: 10}}, [3]float64{90, 0, 10}},
		// WPT css-page dimensions-005: A 3, B 2, C 2 in 20 give B 5, A and C
		// 7.5.
		{"centre box keeps the middle", [3]sizing{{gen: true, auto: true, minO: 3, maxO: 3}, {gen: true, auto: true, minO: 2, maxO: 2}, {gen: true, auto: true, minO: 2, maxO: 2}}, [3]float64{7.5, 5, 7.5}},
		{"definite side", [3]sizing{{gen: true, used: 30, minO: 30, maxO: 30}, {}, {gen: true, auto: true, minO: 5, maxO: 5}}, [3]float64{30, 0, 70}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avail := 100.0
			if tt.name == "centre box keeps the middle" {
				avail = 20
			}
			got := variableSizes(avail, tt.s)
			for i := range got {
				if !approx(got[i], tt.want[i], 1e-9) {
					t.Fatalf("sizes %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestNamedPages_SizesAndBlank(t *testing.T) {
	src := `<style>
		@page { size: 300pt 200pt; margin: 10pt }
		@page wide { size: 400pt 200pt }
		@page :blank { size: 100pt 100pt }
		body { margin: 0 } p { margin: 0 }
		.wide { page: wide }
		.left { break-before: left }
	</style><p>a</p><div class="wide"><p>b</p></div><p class="left">c</p>`
	doc := convertDoc(t, src, Options{PreferCSSPageSize: true})
	want := [][2]float64{{300, 200}, {400, 200}, {100, 100}, {300, 200}}
	if len(doc.Pages) != len(want) {
		t.Fatalf("%d pages, want %d", len(doc.Pages), len(want))
	}
	for i, w := range want {
		if pg := doc.Pages[i]; !approx(pg.Width, w[0], 0.01) || !approx(pg.Height, w[1], 0.01) {
			t.Errorf("page %d is %vx%v, want %vx%v", i+1, pg.Width, pg.Height, w[0], w[1])
		}
	}
	if hasText(doc.Pages[2], "c") {
		t.Error("the blank page holds content")
	}
}

func TestChromiumDate(t *testing.T) {
	for _, tt := range []struct {
		t    time.Time
		want string
	}{
		{time.Time{}, ""},
		{time.Date(2026, 11, 6, 10, 30, 0, 0, time.UTC), "11/6/26, 10:30 AM"},
		{time.Date(2005, 1, 2, 0, 5, 0, 0, time.UTC), "1/2/05, 12:05 AM"},
		{time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC), "12/31/26, 12:00 PM"},
		{time.Date(2026, 7, 4, 23, 59, 0, 0, time.UTC), "7/4/26, 11:59 PM"},
	} {
		if got := chromiumDate(tt.t); got != tt.want {
			t.Errorf("chromiumDate(%v) = %q, want %q", tt.t, got, tt.want)
		}
	}
}

func TestHeaderFooter_Positions(t *testing.T) {
	src := `<html><head><title>T1</title><style>body { margin: 0 } p { margin: 0; break-after: page }</style></head><body><p>one</p><p>two</p></body></html>`
	o := Options{
		PageWidth: 400, PageHeight: 300, MarginTop: 50, MarginBottom: 60, MarginLeft: 30, MarginRight: 30,
		HeaderTemplate: `<div class="text"><span class="title"></span> <span class="pageNumber"></span>/<span class="totalPages"></span></div>`,
		FooterTemplate: `<style>body { font-size: 10pt }</style><p><span class="date"></span> <span class="url"></span></p>`,
		Date:           time.Date(2026, 11, 6, 10, 30, 0, 0, time.UTC),
		BaseURL:        "https://example.com/doc.html",
	}
	doc := convertDoc(t, src, o)
	if len(doc.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(doc.Pages))
	}
	h := find(t, doc.Pages[1], "T1 2/2")
	// The header's .text is 8pt, below the header's 15pt padding: its
	// baseline under 15+8*1.2 and over 15.
	if !approx(h.x, 0, 0.01) || !(h.y > 15 && h.y < 15+8*1.25) || h.size != 8 {
		t.Errorf("header at %v,%v size %v", h.x, h.y, h.size)
	}
	f := find(t, doc.Pages[0], "11/6/26, 10:30 AM https://example.com/doc.html")
	// The footer's paragraph sits on its 15pt padding, its 10pt bottom
	// margin above it: the baseline above 300-25.
	if !(f.y < 300-25 && f.y > 300-25-12) || f.size != 10 {
		t.Errorf("footer baseline %v size %v, want just above 275", f.y, f.size)
	}
}

func TestHeaderFooter_HidesTextInPageArea(t *testing.T) {
	src := `<p>body</p>`
	tpl := `<div class="text">HIDE</div><div>KEEP</div>`
	for _, tt := range []struct {
		name   string
		margin float64
		shown  bool
	}{{"room in the margin", 72, true}, {"no margin", 0, false}} {
		t.Run(tt.name, func(t *testing.T) {
			o := Options{MarginTop: tt.margin, MarginBottom: tt.margin, HeaderTemplate: tpl}
			pg := convertDoc(t, src, o).Pages[0]
			if hasText(pg, "HIDE") != tt.shown {
				t.Errorf("text element shown %v, want %v", hasText(pg, "HIDE"), tt.shown)
			}
			if !hasText(pg, "KEEP") {
				t.Error("an element without the text class was hidden")
			}
		})
	}
}

func TestHeaderFooter_Artifacts(t *testing.T) {
	doc := convertDoc(t, `<p>x</p>`, Options{MarginTop: 72, HeaderTemplate: `<p>head</p>`})
	pg := doc.Pages[0]
	if !hasText(pg, "head") {
		t.Fatal("no header")
	}
	// The page's own content comes first; the header, appended after it,
	// paints over it as an artifact.
	n := len(pg.Content)
	if structured(pg.Content[n-1:]) {
		t.Error("the header hangs from the structure tree")
	}
}
