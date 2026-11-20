package css

import (
	"context"
	"math"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/models"
)

func TestStylesShareEqualValues(t *testing.T) {
	tb := NewStyles()
	a := Initial()
	a.FontFamilies = []string{"Liberation Sans", "sans-serif"}
	b := Initial()
	b.FontFamilies = []string{"Liberation Sans", "sans-serif"}
	if got := tb.Share(a); got != a {
		t.Fatal("the first style is not adopted")
	}
	if got := tb.Share(b); got != a {
		t.Fatal("an equal style is not shared")
	}
	if got := tb.Share(a); got != a {
		t.Fatal("a held style does not share itself")
	}
	if tb.Len() != 1 {
		t.Fatalf("Len = %d, want 1", tb.Len())
	}
}

func TestStylesKeepUnequalValuesApart(t *testing.T) {
	g := &Gradient{}
	cases := map[string]func(*Style){
		"font size":           func(s *Style) { s.FontSize = 11 },
		"negative zero":       func(s *Style) { s.TextIndent = Pts(math.Copysign(0, -1)) },
		"family":              func(s *Style) { s.FontFamilies = []string{"serif", "x"} },
		"empty tab stops":     func(s *Style) { s.TabStops = []TabStop{} },
		"gradient":            func(s *Style) { s.BackgroundGradient = g },
		"other gradient":      func(s *Style) { s.BackgroundGradient = &Gradient{} },
		"unexported":          func(s *Style) { s.currentColor = slotOutline },
		"nested slice":        func(s *Style) { s.Content.Items = []ContentItem{{Text: "a"}} },
		"nested slice other":  func(s *Style) { s.Content.Items = []ContentItem{{Text: "b"}} },
		"shadow current":      func(s *Style) { s.BoxShadow = []Shadow{{current: true}} },
		"shadow not current":  func(s *Style) { s.BoxShadow = []Shadow{{}} },
		"office":              func(s *Style) { s.ExactHeight = true },
		"monospace keyword":   func(s *Style) { s.fontSizeKeyword = 3 },
		"quotes":              func(s *Style) { s.Quotes.Pairs = [][2]string{{"«", "»"}} },
		"grid areas absent":   func(s *Style) { s.GridTemplateAreas = nil },
		"transform rotate":    func(s *Style) { s.Transform = []TransformFunc{{Kind: TransformRotate}} },
		"transform translate": func(s *Style) { s.Transform = []TransformFunc{{Kind: TransformTranslate}} },
	}
	tb := NewStyles()
	base := tb.Share(Initial())
	seen := map[*Style]string{base: "initial"}
	for name, set := range cases {
		st := Initial()
		set(st)
		got := tb.Share(st)
		if prev, ok := seen[got]; ok && name != "grid areas absent" {
			t.Errorf("%s shares the style of %s", name, prev)
		}
		seen[got] = name
	}
	again := Initial()
	again.Content.Items = []ContentItem{{Text: "a"}}
	if seen[tb.Share(again)] != "nested slice" {
		t.Error("an equal nested slice is not shared")
	}
}

func TestStylesNil(t *testing.T) {
	var tb *Styles
	st := Initial()
	if tb.Share(st) != st || tb.Len() != 0 {
		t.Fatal("a nil table shares")
	}
	if NewStyles().Share(nil) != nil {
		t.Fatal("a nil style is not returned")
	}
}

func TestStylesConcurrent(t *testing.T) {
	tb := NewStyles()
	var wg sync.WaitGroup
	out := make([]*Style, 8)
	for i := range out {
		wg.Go(func() { out[i] = tb.Share(Initial()) })
	}
	wg.Wait()
	for _, st := range out[1:] {
		if st != out[0] {
			t.Fatal("equal styles shared from goroutines differ")
		}
	}
}

func BenchmarkStylesShare(b *testing.B) {
	tb := NewStyles()
	st := Initial()
	st.FontFamilies = []string{"Liberation Serif"}
	tb.Share(st)
	for b.Loop() {
		c := *st
		tb.Share(&c)
	}
}

func TestComputeSharesStylesOfElementsStyledAlike(t *testing.T) {
	c := compute(t, `<p id=a>x</p><p id=b class=k>y</p><p id=c>z</p><p id=d class=k>w</p>`, `.k { color: red }`)
	if c.of("a") != c.of("c") || c.of("b") != c.of("d") {
		t.Error("elements styled alike hold distinct styles")
	}
	if c.of("a") == c.of("b") {
		t.Error("elements styled apart share a style")
	}
}

func TestComputeChargesAStyleOnce(t *testing.T) {
	charged := func(n int) int64 {
		ctx, b, done := limits.WithBudget(context.Background(), models.Limits{})
		defer done()
		root := parseHTML(t, "<p>x</p>"+strings.Repeat("<p>y</p>", n))
		r := NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}})
		if _, err := r.Compute(ctx, root, ComputeOptions{Hints: HTMLHints}); err != nil {
			t.Fatalf("Compute: %v", err)
		}
		return b.Held()
	}
	if one, many := charged(1), charged(500); one != many {
		t.Errorf("500 paragraphs styled alike hold %d bytes of styles, one %d", many, one)
	}
}

func TestComputedReportsTheStylesItCharged(t *testing.T) {
	c := compute(t, `<p id=a>x</p><p id=b>y</p>`, `#b { color: red }`)
	for _, id := range []string{"a", "b"} {
		if st := c.of(id); !c.c.Charged(st) {
			t.Errorf("the style of #%s is not reported charged", id)
		}
	}
	other := *c.of("a")
	if c.c.Charged(&other) {
		t.Error("a copy of a computed style is reported charged")
	}
	if c.c.Charged(nil) || (*Computed)(nil).Charged(c.of("a")) {
		t.Error("a nil style or a nil Computed reports a charge")
	}
}

// TestStyleCostCoversAStyle holds [limits.StyleCost] to what a distinct
// style shared through a table holds on the heap.
func TestStyleCostCoversAStyle(t *testing.T) {
	const n = 20_000
	tb := NewStyles()
	keep := make([]*Style, 0, n)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := range n {
		st := *Initial()
		st.FontSize = float64(i + 1)
		keep = append(keep, tb.ShareCopy(&st))
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	per := (int64(after.HeapAlloc) - int64(before.HeapAlloc)) / n
	if per > limits.StyleCost {
		t.Errorf("a distinct style holds %d bytes, past limits.StyleCost, %d", per, limits.StyleCost)
	}
	runtime.KeepAlive(keep)
}

func TestStylesShareCopyNeverHoldsItsArgument(t *testing.T) {
	tb := NewStyles()
	scratch := Initial()
	first, adopted := tb.shareCopy(scratch)
	if !adopted || first == scratch || !equalStyle(first, scratch) {
		t.Fatal("a new style is not adopted as a copy")
	}
	scratch.FontSize = 20
	if first.FontSize == 20 {
		t.Fatal("the adopted copy follows a write to the argument")
	}
	scratch.FontSize = first.FontSize
	if got, adopted := tb.shareCopy(scratch); got != first || adopted {
		t.Fatal("an equal style is not shared")
	}
	if allocs := testing.AllocsPerRun(10, func() { tb.shareCopy(scratch) }); allocs != 0 {
		t.Errorf("sharing an equal style allocates %v times", allocs)
	}
	var none *Styles
	if got, adopted := none.shareCopy(scratch); got == scratch || adopted || !equalStyle(got, scratch) {
		t.Error("a nil table does not return a copy")
	}
}

func TestStylesShareCopyKeepsAStackStyleOnTheStack(t *testing.T) {
	tb := NewStyles()
	base := Initial()
	tb.ShareCopy(base)
	if allocs := testing.AllocsPerRun(10, func() {
		st := *base
		st.FontSize = base.FontSize
		tb.ShareCopy(&st)
	}); allocs != 0 {
		t.Errorf("sharing a copy held on the stack allocates %v times", allocs)
	}
}

func TestStylesIntactSeesAWriteToASharedStyle(t *testing.T) {
	tb := NewStyles()
	a := tb.ShareCopy(Initial())
	b := Initial()
	b.FontSize = 20
	tb.Share(b)
	if !tb.Intact() {
		t.Fatal("a table whose styles nobody wrote is not intact")
	}
	b.FontSize = 21
	if tb.Intact() {
		t.Error("a write to a shared style goes unseen")
	}
	b.FontSize = 20
	a.Display = Block
	if tb.Intact() {
		t.Error("a write to a shared copy goes unseen")
	}
	var none *Styles
	if !none.Intact() {
		t.Error("the nil table is not intact")
	}
}
