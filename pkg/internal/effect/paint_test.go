package effect

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/embed"
)

// contentFont is a TrueType face with quadratic outlines for A, B and the
// space, shared with the renderer's tests.
var contentFont = sync.OnceValues(func() ([]byte, error) {
	return os.ReadFile(filepath.Join("..", "render", "page", "testdata", "dejavu_sans_ab.ttf"))
})

// textRun returns a run of s, made of A, B and spaces, at size points
// from (x, y), laid out at the face's advances.
func textRun(t testing.TB, size, x, y float64, s string, c typeset.Color) *typeset.Text {
	t.Helper()
	prog, err := contentFont()
	if err != nil {
		t.Fatal(err)
	}
	f, err := embed.Load(context.Background(), prog)
	if err != nil {
		t.Fatal(err)
	}
	upem := float64(f.UnitsPerEm())
	run := &typeset.Text{Face: &typeset.Face{Program: prog, Key: "dejavu-ab"}, Size: size, Color: c, Origin: typeset.Point{X: x, Y: y}}
	gx := 0.0
	for _, r := range s {
		gid, ok := f.GID(r)
		if !ok {
			t.Fatalf("face lacks %q", r)
		}
		w, _ := f.Width(gid)
		run.Glyphs = append(run.Glyphs, typeset.Glyph{GID: gid, X: gx, Text: string(r)})
		gx += float64(float64(w)*size) / upem
	}
	return run
}

func rgba(r, g, b, a float64) typeset.Color { return typeset.Color{R: r, G: g, B: b, A: a} }

// picture returns a 37 by 23 picture with alpha, encoded once.
func picture(t testing.TB) *typeset.ImageSource {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 37, 23))
	for y := range 23 {
		for x := range 37 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 11), B: uint8((x*y*13 + 40) % 256), A: uint8(80 + (x+y)*4)}) //nolint:gosec // every value is under 256
		}
	}
	src, err := typeset.EncodeImage(context.Background(), img)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// photo returns a JPEG picture, read from the decoder's tests.
func photo(t testing.TB) *typeset.ImageSource {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "filter", "dct", "testdata", "restart.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	src, err := typeset.JPEGImage(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// contentKinds returns, by name, the kinds of content an effect reads, in
// page coordinates on a page of 160 by 120 points: a text box, pictures
// scaled and turned, a gradient shape, a group of twenty shapes, and the
// transparency constructs a converter writes.
func contentKinds(t testing.TB) map[string][]typeset.Fragment {
	t.Helper()
	axial := &typeset.Shading{Kind: typeset.Axial, From: typeset.Point{X: 10, Y: 10}, To: typeset.Point{X: 150, Y: 100}, Stops: []typeset.ColorStop{
		{Offset: 0, Color: rgba(1, 0, 0, 1)}, {Offset: 0.4, Color: rgba(0.1, 0.9, 0.2, 0.5)}, {Offset: 1, Color: rgba(0, 0, 1, 1)},
	}}
	radial := &typeset.Shading{Kind: typeset.Radial, From: typeset.Point{X: 70, Y: 60}, To: typeset.Point{X: 80, Y: 64}, FromRadius: 3, ToRadius: 50, Stops: []typeset.ColorStop{
		{Offset: 0, Color: rgba(1, 1, 0, 1)}, {Offset: 1, Color: rgba(0.3, 0, 0.5, 0.9)},
	}}
	var shapes []typeset.Fragment
	for i := range 20 {
		x, y := 5+float64(i%5)*30.5, 8+float64(i/5)*27.25
		shapes = append(shapes, &typeset.Path{
			Ops:    ellipse(x+12, y+10, 11.3+float64(i%3), 9.1),
			Fill:   &typeset.Fill{Color: rgba(float64(i%4)/3, 0.5, 1-float64(i%5)/4, 0.85)},
			Stroke: &typeset.Stroke{Color: typeset.Black, Width: 0.7 + float64(i%3)/2, Dash: []float64{3, 1.5}},
		})
	}
	turn := typeset.Matrix{A: 0.866, B: 0.5, C: -0.5, D: 0.866, E: 45, F: -20}
	return map[string][]typeset.Fragment{
		"text": {
			textRun(t, 9, 6.5, 20, "ABBA BAAB AB", typeset.Black),
			textRun(t, 23.7, 4, 55, "AB BA", rgba(0.2, 0.3, 0.7, 0.8)),
			&typeset.Group{Transform: turn, Content: []typeset.Fragment{textRun(t, 31, 30, 100, "BAB", rgba(0.7, 0.1, 0.1, 1))}},
		},
		"picture": {
			&typeset.Image{Rect: typeset.Rect{X: 3.3, Y: 4.7, Width: 71.3, Height: 53.1}, Source: photo(t)},
			&typeset.Group{Transform: turn, Opacity: 0.7, Content: []typeset.Fragment{&typeset.Image{Rect: typeset.Rect{X: 60, Y: 40, Width: 80, Height: 50}, Source: picture(t)}}},
		},
		"gradient": {
			&typeset.Path{Ops: rect(4.5, 4.5, 155.5, 115.5), Fill: &typeset.Fill{Shading: axial}},
			&typeset.Path{Ops: ellipse(80, 62, 55, 40), Fill: &typeset.Fill{Shading: radial}},
		},
		"shapes": shapes,
		"transparency": {
			&typeset.Path{Ops: rect(0, 60, 160, 120), Fill: &typeset.Fill{Color: rgba(0.9, 0.9, 0.3, 1)}},
			&typeset.Group{Clip: &typeset.Path{Ops: ellipse(50, 50, 45, 35)}, Opacity: 0.5, Transparency: &typeset.TransparencyGroup{}, Content: shapes[:6]},
			&typeset.Group{Blend: typeset.BlendMultiply, Transparency: &typeset.TransparencyGroup{Isolated: true, Mask: &typeset.SoftMask{
				Kind:    typeset.MaskLuminosity,
				Content: []typeset.Fragment{&typeset.Path{Ops: rect(80, 0, 160, 120), Fill: &typeset.Fill{Shading: axial}}},
			}}, Content: shapes[10:]},
		},
	}
}

// pageRender renders frags written as one page of w by h points at scale
// pixels a point, as a converter's page is written and rendered, and
// returns the quantised pixels of the region of pw by ph pixels at
// (px, py).
func pageRender(t testing.TB, frags []typeset.Fragment, w, h, scale float64, px, py, pw, ph int) []uint8 {
	t.Helper()
	ctx := context.Background()
	var buf bytes.Buffer
	if err := typeset.Write(ctx, &typeset.Document{Pages: []*typeset.Page{{Width: w, Height: h, Content: frags}}}, &buf, typeset.Options{}); err != nil {
		t.Fatal(err)
	}
	d, err := document.NewDocument(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := document.ResolveCatalog(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := document.ResolvePages(ctx, d, cat)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := page.Render(ctx, d, pages[0], page.Options{DPI: float64(scale * 72)})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]uint8, 0, 4*pw*ph)
	for y := py; y < py+ph; y++ {
		for x := px; x < px+pw; x++ {
			r, g, b, a := c.At(x, y)
			qa := quantise(a)
			out = append(out, min(quantise(r), qa), min(quantise(g), qa), min(quantise(b), qa), qa)
		}
	}
	return out
}

// paintOne paints one content and fails the test on error.
func paintOne(t testing.TB, ctx context.Context, c Content) *Surface {
	t.Helper()
	s, err := Paint(ctx, []Content{c})
	if err != nil {
		t.Fatalf("Paint: %v", err)
	}
	return s[0]
}

// TestPaint_MatchesPageRender paints each kind of content and compares
// the surface, byte for byte, with the renderer's drawing of the same
// content written as a page whose top-left corner and size are the
// region's, quantised alike: the adapter adds nothing to and loses
// nothing of what the renderer draws. A region that is the whole page
// takes the fragments as they are; one inside it, the fragments moved by
// a group, as a converter that lays content out under a translation
// writes them.
//
// The oracle is not a crop of a larger page: the renderer's pixels depend
// on where the content sits on the page and on the page's size, for these
// kinds by up to 16 levels on at most 54 bytes, at the edges of the
// stroked shapes and the transparency groups.
func TestPaint_MatchesPageRender(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	regions := []struct{ x, y, w, h float64 }{{0, 0, 160, 120}, {13, 7, 140, 100}}
	for name, frags := range contentKinds(t) {
		for _, r := range regions {
			for _, scale := range []float64{1, 2, 4, 300.0 / 72} {
				page := frags
				if r.x != 0 || r.y != 0 {
					page = []typeset.Fragment{&typeset.Group{Transform: typeset.Matrix{A: 1, D: 1, E: -r.x, F: -r.y}, Content: frags}}
				}
				pw, ph := pixelSide(float64(r.w*scale)), pixelSide(float64(r.h*scale))
				// The page holds the region's whole pixels, as the
				// surface does.
				want := pageRender(t, page, float64(pw)/scale, float64(ph)/scale, scale, 0, 0, pw, ph)
				got := paintOne(t, ctx, Content{Fragments: frags, Grid: Grid{X: r.x, Y: r.y, Scale: scale}, W: pw, H: ph})
				if got.W != pw || got.H != ph || got.F != RGBA {
					t.Fatalf("%s at %v: surface %d by %d of format %d", name, scale, got.W, got.H, got.F)
				}
				diff, most := 0, 0
				for i := range want {
					if got.Pix[i] != want[i] {
						diff++
						most = max(most, int(got.Pix[i])-int(want[i]), int(want[i])-int(got.Pix[i]))
					}
				}
				if diff > 0 {
					t.Errorf("%s in %v at %v px/pt: %d of %d bytes differ from the page's render, by up to %d", name, r, scale, diff, len(want), most)
				}
				painted := 0
				for i := 3; i < len(got.Pix); i += 4 {
					if got.Pix[i] > 0 {
						painted++
					}
				}
				if painted < len(got.Pix)/4/50 {
					t.Errorf("%s in %v at %v px/pt: %d pixels painted", name, r, scale, painted)
				}
				got.Release()
			}
		}
	}
}

// TestPaint_Transform paints content held in its own coordinates under a
// transform to the page, and compares it with the same content written in
// page coordinates.
func TestPaint_Transform(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	frags := contentKinds(t)["shapes"]
	g := Grid{X: 20, Y: 10, Scale: 2}
	want := paintOne(t, ctx, Content{Fragments: []typeset.Fragment{&typeset.Group{Transform: typeset.Matrix{A: 1, D: 1, E: 30, F: 15}, Content: frags}}, Grid: g, W: 200, H: 160})
	got := paintOne(t, ctx, Content{Fragments: frags, Transform: typeset.Matrix{A: 1, D: 1, E: 30, F: 15}, Grid: g, W: 200, H: 160})
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Error("content under Transform differs from content moved by a group")
	}
	painted := false
	for i := 3; i < len(got.Pix); i += 4 {
		painted = painted || got.Pix[i] > 0
	}
	if !painted {
		t.Error("nothing painted")
	}
}

// TestPaint_UntaggedAndShared paints content whose fragments hang from a
// structure element, several contents in one call sharing a face and a
// picture, and checks each equals its own call, the fragments unchanged.
func TestPaint_UntaggedAndShared(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	elem := &typeset.StructElem{Role: "P"}
	run := textRun(t, 14, 3, 20, "AB BA", typeset.Black)
	run.Parent = elem
	img := &typeset.Image{Rect: typeset.Rect{X: 2, Y: 25, Width: 40, Height: 20}, Source: picture(t), Parent: elem}
	tagged := []typeset.Fragment{run, &typeset.Group{Content: []typeset.Fragment{img}}}
	cs := []Content{
		{Fragments: tagged, Grid: Grid{Scale: 2}, W: 100, H: 100},
		{Fragments: tagged, Grid: Grid{X: 10, Y: 5, Scale: 1}, W: 40, H: 50},
	}
	both, err := Paint(ctx, cs)
	if err != nil {
		t.Fatalf("Paint: %v", err)
	}
	for i, c := range cs {
		one := paintOne(t, ctx, c)
		if !bytes.Equal(one.Pix, both[i].Pix) {
			t.Errorf("content %d painted with another differs from it painted alone", i)
		}
	}
	if run.Parent != elem || img.Parent != elem {
		t.Error("Paint changed the fragments' parents")
	}
}

// TestPaint_BudgetReturnsToZero checks that the surfaces are the only
// memory Paint leaves charged, and that releasing them returns the
// budget to zero, on success and on a refusal.
func TestPaint_BudgetReturnsToZero(t *testing.T) {
	t.Parallel()
	ctx, budget, done := limits.WithBudget(context.Background(), limits.Limits{})
	defer done()
	s, err := Paint(ctx, []Content{
		{Fragments: contentKinds(t)["gradient"], Grid: Grid{Scale: 2}, W: 320, H: 240},
		{Fragments: contentKinds(t)["text"], Grid: Grid{Scale: 1}, W: 160, H: 120},
	})
	if err != nil {
		t.Fatalf("Paint: %v", err)
	}
	if got, want := budget.Held(), int64(4*(320*240+160*120)); got != want {
		t.Errorf("held %d bytes after Paint, want the surfaces' %d", got, want)
	}
	if peak := budget.Peak(); peak < 16*321*241 {
		t.Errorf("peak %d bytes, under the renderer's canvas", peak)
	}
	for _, x := range s {
		x.Release()
	}
	if got := budget.Held(); got != 0 {
		t.Errorf("held %d bytes after Release, want 0", got)
	}

	small, _, done2 := limits.WithBudget(context.Background(), limits.Limits{MaxConvertMemoryBytes: 1 << 20})
	defer done2()
	if _, err := Paint(small, []Content{{Fragments: contentKinds(t)["gradient"], Grid: Grid{Scale: 4}, W: 640, H: 480}}); !errors.Is(err, limits.ErrMemoryBudget) {
		t.Errorf("Paint past the budget: %v, want ErrMemoryBudget", err)
	}
	if got := limits.BudgetFrom(small).Held(); got != 0 {
		t.Errorf("held %d bytes after a refusal, want 0", got)
	}
}

func TestPaint_RefusesBadGrids(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	frags := contentKinds(t)["shapes"]
	cases := []struct {
		name string
		c    Content
		want error
	}{
		{"zero scale", Content{Fragments: frags, W: 10, H: 10}, ErrGrid},
		{"infinite origin", Content{Fragments: frags, Grid: Grid{X: math.Inf(1), Scale: 1}, W: 10, H: 10}, ErrGrid},
		{"no pixels", Content{Fragments: frags, Grid: Grid{Scale: 1}, W: 0, H: 10}, ErrTooLarge},
		{"past the pixel cap", Content{Fragments: frags, Grid: Grid{Scale: 4}, W: 8192, H: 4097}, ErrTooLarge},
		{"past a page side", Content{Fragments: frags, Grid: Grid{Scale: 0.5}, W: 8000, H: 10}, ErrTooLarge},
	}
	for _, tc := range cases {
		if _, err := Paint(ctx, []Content{tc.c}); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	if s, err := Paint(ctx, nil); s != nil || err != nil {
		t.Errorf("Paint(nil) = %v, %v", s, err)
	}
}

// pinnedPaintHashes holds the SHA-256 of the surface [Paint] draws of each
// kind of content at each scale of [TestPaint_PinnedHashes]. Every
// architecture must give these bytes: the renderer evaluates no product
// fused with a sum, and the quantisation rounds each product on its own.
var pinnedPaintHashes = map[string]string{
	"gradient 1.000":     "4f59f4d2fb32da0a480ef30a5c419ecaec878f5fc59a8bb67976cfbeaf0c3f07",
	"gradient 2.000":     "abb02c04b6b3456ec615ff7d25f0a46f834bf78140c35fdebf07fe4beb0b2b2d",
	"gradient 4.167":     "9f45d78c1400a3bdb20b66ffdd8b8907192dc09e60b93348f222d6c329832293",
	"picture 1.000":      "ee1219dd729bf7777e5a41e4606fc2af823f8c28eb9b61def9d84313228c537a",
	"picture 2.000":      "be27bd62636cdae0e33bf2d2b5716faa30bc0381b62d88ab338694021a759b99",
	"picture 4.167":      "dcdc89f2a40fbfcdc0ca8a36519ab66f52ca8cf64bac7fb79855b02ac6a714ca",
	"shapes 1.000":       "ce4e496973b0b18b7e8664e128ecb3325fa23fc552b9d4b7d4ff60fbbbc060b6",
	"shapes 2.000":       "f0cab2bb377abb6675f12782d5f1423bda274dc6d1d076b6564895e39022b61f",
	"shapes 4.167":       "46a12261aa3ea817be35a54ed851f4a510b1e83f44ba52c9c0031e1c30b5e3eb",
	"text 1.000":         "30819ed6f2a1690fd92ea438848c65134ac8222b4915749c84f765d48c287c70",
	"text 2.000":         "1497fc85fdee4755a08fd57a7d99531cfe0b068868af0ef6aa2f41a84e060705",
	"text 4.167":         "2ec1035ede0a50aeda84f5f83568a2cacc3e34f2fad6249ff2603620dc22a14c",
	"transparency 1.000": "b663377f803cb7e458cbe89b3b9fa563eacd59fd44e76b57201acf29afeee781",
	"transparency 2.000": "60272a2853b72e0bbbf9ff8ff1075bfc170790d407ba536912880e19b06a0899",
	"transparency 4.167": "d3ad70c5429769dce0bf6f13f897929686321be2009165a4a0d06adf758fc38c",
}

// TestPaint_PinnedHashes paints each kind of content at three scales and
// compares the SHA-256 of each surface with the hash pinned for it, the
// same on every architecture.
func TestPaint_PinnedHashes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, frags := range contentKinds(t) {
		for _, scale := range []float64{1, 2, 300.0 / 72} {
			c := Content{Fragments: frags, Grid: Grid{X: 7, Y: 5, Scale: scale}, W: pixelSide(float64(150 * scale)), H: pixelSide(float64(110 * scale))}
			s := paintOne(t, ctx, c)
			sum := sha256.Sum256(s.Pix)
			key := fmt.Sprintf("%s %.3f", name, scale)
			if got := hex.EncodeToString(sum[:]); got != pinnedPaintHashes[key] {
				t.Errorf("%q: sha256 %s, pinned %s", key, got, pinnedPaintHashes[key])
			}
			s.Release()
		}
	}
}
