package blend

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

const testBudget = 1 << 20

// element is one painted object: a colour over a rectangle, with a coverage
// standing in for anti-aliased shape and an opacity.
type element struct {
	mode     Mode
	color    Components
	rect     image.Rectangle
	coverage float32
	opacity  float32
}

func (e element) paint(g *Group) {
	for y := e.rect.Min.Y; y < e.rect.Max.Y; y++ {
		for x := e.rect.Min.X; x < e.rect.Max.X; x++ {
			g.Paint(x, y, e.mode, e.color, e.coverage, e.coverage*e.opacity)
		}
	}
}

// newPage returns a 4x4 page group painted with an opaque backdrop whose
// left half is one colour and right half another, so blend modes have
// something to act on.
func newPage(t testing.TB) *Group {
	t.Helper()
	page, err := NewPage(4, 4, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	element{Normal, Components{0.9, 0.6, 0.2}, image.Rect(0, 0, 2, 4), 1, 1}.paint(page)
	element{Normal, Components{0.1, 0.4, 0.7}, image.Rect(2, 0, 4, 4), 1, 0.5}.paint(page)
	return page
}

func sameCanvas(t *testing.T, got, want *surface.Canvas) {
	t.Helper()
	for i := range want.Pix {
		if !near32(got.Pix[i], want.Pix[i]) {
			t.Fatalf("pixel %d channel %d = %g, want %g", i/4, i%4, got.Pix[i], want.Pix[i])
		}
	}
}

// TestIsolatedGroupOfOneObject checks that an isolated group holding one
// object, composited with the object's own mode and opacity, paints what the
// object painted directly does (ISO 32000-1, 11.4.5: the group's colour,
// shape and alpha are the object's).
func TestIsolatedGroupOfOneObject(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []Mode{Normal, Multiply, SoftLight, Luminosity} {
		obj := element{mode, Components{0.3, 0.8, 0.5}, image.Rect(1, 1, 4, 3), 0.6, 0.7}

		direct := newPage(t)
		obj.paint(direct)

		grouped := newPage(t)
		g, err := NewGroup(image.Rect(0, 0, 4, 4), grouped.Backdrop(), Attributes{Isolated: true}, testBudget)
		if err != nil {
			t.Fatal(err)
		}
		element{Normal, obj.color, obj.rect, obj.coverage, 1}.paint(g)
		if err := g.Into(ctx, grouped, Params{Mode: mode, Opacity: surface.NewOpacity(float64(obj.opacity))}); err != nil {
			t.Fatal(err)
		}
		sameCanvas(t, grouped.Canvas(), direct.Canvas())
	}
}

// TestNonIsolatedGroupIsTransparent checks ISO 32000-1, 11.4.4, NOTE 5: a
// non-isolated group with its parent's knockout attribute, composited with
// Normal at full shape and opacity, paints what its elements painted directly
// do, blend modes against the backdrop included. This is what the backdrop
// removal of the group result exists for.
func TestNonIsolatedGroupIsTransparent(t *testing.T) {
	ctx := context.Background()
	elements := []element{
		{Multiply, Components{0.3, 0.8, 0.5}, image.Rect(1, 0, 4, 3), 0.6, 0.7},
		{Screen, Components{0.9, 0.1, 0.2}, image.Rect(0, 1, 3, 4), 1, 0.5},
		{Color, Components{0.2, 0.2, 0.9}, image.Rect(1, 1, 3, 3), 0.8, 1},
	}

	direct := newPage(t)
	for _, e := range elements {
		e.paint(direct)
	}

	grouped := newPage(t)
	g, err := NewGroup(image.Rect(0, 0, 4, 4), grouped.Backdrop(), Attributes{}, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range elements {
		e.paint(g)
	}
	if err := g.Into(ctx, grouped, Params{}); err != nil {
		t.Fatal(err)
	}
	sameCanvas(t, grouped.Canvas(), direct.Canvas())
}

func pixel(g *Group, x, y int) [4]float32 {
	r, gg, b, a := g.Canvas().At(x-g.bounds.Min.X, y-g.bounds.Min.Y)
	return [4]float32{r, gg, b, a}
}

func near4(a, b [4]float32) bool {
	return near32(a[0], b[0]) && near32(a[1], b[1]) && near32(a[2], b[2]) && near32(a[3], b[3])
}

// TestKnockout paints two overlapping half-opaque objects into isolated
// groups. Without knockout they composite with each other (11.4.4); with
// it the later one replaces the earlier where it covers it fully, and
// replaces a fraction of it where its shape is fractional (11.4.6).
func TestKnockout(t *testing.T) {
	red := element{Normal, Components{1, 0, 0}, image.Rect(0, 0, 3, 1), 1, 0.5}
	blue := element{Normal, Components{0, 0, 1}, image.Rect(1, 0, 4, 1), 1, 0.5}
	// The blue object's last pixel has half coverage: shape 0.5.
	bounds := image.Rect(0, 0, 4, 1)

	plain, err := NewGroup(bounds, Backdrop{}, Attributes{Isolated: true}, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	knock, err := NewGroup(bounds, Backdrop{}, Attributes{Isolated: true, Knockout: true}, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range []*Group{plain, knock} {
		red.paint(g)
		blue.paint(g)
		g.Paint(2, 0, Normal, blue.color, 0, 0) // zero shape: no effect.
	}
	// Overlap at x = 1: source-over gives alpha 0.75 and colour
	// (0.25, 0, 0.5) premultiplied; knockout keeps blue alone.
	if got, want := pixel(plain, 1, 0), [4]float32{0.25, 0, 0.5, 0.75}; !near4(got, want) {
		t.Errorf("non-knockout overlap = %v, want %v", got, want)
	}
	if got, want := pixel(knock, 1, 0), [4]float32{0, 0, 0.5, 0.5}; !near4(got, want) {
		t.Errorf("knockout overlap = %v, want %v", got, want)
	}
	// Red alone at x = 0 is the same in both.
	for _, g := range []*Group{plain, knock} {
		if got, want := pixel(g, 0, 0), [4]float32{0.5, 0, 0, 0.5}; !near4(got, want) {
			t.Errorf("red alone = %v, want %v", got, want)
		}
	}

	// A fractional shape knocks out that fraction: at shape 0.5 and alpha
	// 0.25, half the red stays and the blue composites over the transparent
	// initial backdrop for the other half (11.4.8: αg = 0.5·0.5 + 0.25).
	g, err := NewGroup(bounds, Backdrop{}, Attributes{Isolated: true, Knockout: true}, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	red.paint(g)
	g.Paint(0, 0, Normal, blue.color, 0.5, 0.25)
	if got, want := pixel(g, 0, 0), [4]float32{0.25, 0, 0.25, 0.5}; !near4(got, want) {
		t.Errorf("fractional knockout = %v, want %v", got, want)
	}
	// The group shape is the union of the element shapes, knockout or not.
	if f := g.shape.A[0]; !near32(f, 1) {
		t.Errorf("group shape = %g, want Union(1, 0.5) = 1", f)
	}
}

// TestNonIsolatedKnockoutBlendsWithInitialBackdrop checks that in a
// non-isolated knockout group each element blends with the group's initial
// backdrop, never with an earlier element (11.4.6), and that a non-isolated
// group nested in it starts from that same initial backdrop (NOTE 6).
func TestNonIsolatedKnockoutBlendsWithInitialBackdrop(t *testing.T) {
	ctx := context.Background()
	page, err := NewPage(1, 1, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	page.Paint(0, 0, Normal, Components{0.5, 0.5, 0.5}, 1, 1)

	knock, err := NewGroup(page.Bounds(), page.Backdrop(), Attributes{Knockout: true}, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	knock.Paint(0, 0, Normal, Components{1, 0, 0}, 1, 1)
	knock.Paint(0, 0, Multiply, Components{0.5, 1, 1}, 1, 1)
	// Multiply with the grey backdrop, not with the red: (0.25, 0.5, 0.5).
	if got, want := pixel(knock, 0, 0), [4]float32{0.25, 0.5, 0.5, 1}; !near4(got, want) {
		t.Errorf("knockout multiply = %v, want %v", got, want)
	}

	inner, err := NewGroup(knock.Bounds(), knock.Backdrop(), Attributes{}, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pixel(inner, 0, 0), [4]float32{0.5, 0.5, 0.5, 1}; !near4(got, want) {
		t.Errorf("nested group starts at %v, want the knockout's initial backdrop %v", got, want)
	}
	inner.Paint(0, 0, Multiply, Components{0, 1, 1}, 1, 1)
	if err := inner.Into(ctx, knock, Params{}); err != nil {
		t.Fatal(err)
	}
	if got, want := pixel(knock, 0, 0), [4]float32{0, 0.5, 0.5, 1}; !near4(got, want) {
		t.Errorf("after the nested group = %v, want %v", got, want)
	}
}

// TestSoftMaskParams checks the source shape and alpha of 11.3.7.2: a soft
// mask and a constant alpha are opacity unless AIS makes them shape.
func TestSoftMaskParams(t *testing.T) {
	m, err := surface.NewMask(2, 1, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	m.Set(1, 0, 0.5)
	p := Params{Opacity: surface.NewOpacity(0.8), SoftMask: NewMask(m, image.Point{}, 0)}
	if fs, as := p.Source(1, 0, 0.9, 0.9); !near32(fs, 0.9) || !near32(as, 0.36) {
		t.Errorf("opacity mask: fs, as = %g, %g, want 0.9, 0.36", fs, as)
	}
	p.AlphaIsShape = true
	if fs, as := p.Source(1, 0, 0.9, 0.9); !near32(fs, 0.36) || !near32(as, 0.36) {
		t.Errorf("shape mask: fs, as = %g, %g, want 0.36, 0.36", fs, as)
	}
	if fs, as := p.Source(0, 0, 1, 1); fs != 0 || as != 0 {
		t.Errorf("masked out: fs, as = %g, %g, want 0, 0", fs, as)
	}
}

func TestGroupRefusesBudget(t *testing.T) {
	if _, err := NewGroup(image.Rect(0, 0, 100, 100), Backdrop{}, Attributes{}, 99); !errors.Is(err, surface.ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
	if _, err := NewPage(0, 4, testBudget); !errors.Is(err, surface.ErrTooLarge) {
		t.Errorf("empty page: err = %v, want ErrTooLarge", err)
	}
}

func TestIntoHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	page := newPage(t)
	g, err := NewGroup(page.Bounds(), Backdrop{}, Attributes{Isolated: true}, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Into(ctx, page, Params{}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestDefaultParamsAreOpaque(t *testing.T) {
	fs, as := Params{}.Source(0, 0, 0.5, 0.75)
	if fs != 0.5 || as != 0.75 {
		t.Errorf("Params{}.Source = %v, %v; want the object's own 0.5, 0.75", fs, as)
	}
}
