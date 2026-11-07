package html

import (
	"context"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestParseSrcset(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want []srcCandidate
	}{
		{"", nil},
		{"a.png", []srcCandidate{{url: "a.png", density: 1}}},
		{" a.png 1x, b.png 2x ", []srcCandidate{{url: "a.png", density: 1}, {url: "b.png", density: 2}}},
		{"a.png 100w,b.png 200w 50h", []srcCandidate{{url: "a.png", width: 100}, {url: "b.png", width: 200}}},
		// The URL runs to the first whitespace, commas included.
		{"a.png,,b.png 1.5x", []srcCandidate{{url: "a.png,,b.png", density: 1.5}}},
		{"a.png,, b.png 1.5x", []srcCandidate{{url: "a.png", density: 1}, {url: "b.png", density: 1.5}}},
		// Descriptors past parentheses, and a URL with commas inside.
		{"data:image/png;base64,AA== 2x", []srcCandidate{{url: "data:image/png;base64,AA==", density: 2}}},
		{"a.png (x, y) 2x, b.png", []srcCandidate{{url: "b.png", density: 1}}},
		// Invalid candidates are dropped, the rest kept.
		{"a.png 2x 3x, b.png 0w, c.png 10h, d.png 1x 10w, e.png 1e1x, f.png -1x, g.png 1.x, h.png 2y, i.png 0x",
			[]srcCandidate{{url: "e.png", density: 10}}},
	} {
		if got := parseSrcset(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseSrcset(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
	many := ""
	for range maxSrcsetCandidates + 10 {
		many += "a.png 1x,"
	}
	if got := len(parseSrcset(many)); got != maxSrcsetCandidates {
		t.Errorf("%d candidates kept", got)
	}
}

func TestPickCandidate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cands []srcCandidate
		slot  float64
		want  string
	}{
		{[]srcCandidate{{url: "a", density: 2}, {url: "b", density: 1}, {url: "c", density: 1.5}}, 100, "b"},
		{[]srcCandidate{{url: "a", density: 0.5}, {url: "b", density: 0.75}}, 100, "b"},
		{[]srcCandidate{{url: "a", density: 3}, {url: "b", density: 1.5}}, 100, "b"},
		{[]srcCandidate{{url: "s", width: 300}, {url: "m", width: 800}, {url: "l", width: 1600}}, 794, "m"},
		{[]srcCandidate{{url: "s", width: 300}, {url: "m", width: 800}, {url: "l", width: 1600}}, 400, "m"},
		{[]srcCandidate{{url: "s", width: 300}, {url: "m", width: 800}}, 1000, "m"},
		{[]srcCandidate{{url: "a", density: 1}, {url: "b", density: 1}}, 100, "a"},
	} {
		if got, _ := pickCandidate(tc.cands, tc.slot); got != tc.want {
			t.Errorf("pickCandidate(%+v, %v) = %q, want %q", tc.cands, tc.slot, got, tc.want)
		}
	}
}

func TestSlotSize(t *testing.T) {
	t.Parallel()
	// An A4 page area, 793.7 CSS pixels wide.
	b := &builder{media: css.Media{Type: "print", Width: DefaultPageWidth, Height: DefaultPageHeight}}
	vw := DefaultPageWidth / css.Pt
	ctx := context.Background()
	for _, tc := range []struct {
		in   string
		want float64
	}{
		{"", vw},
		{"300px", 300},
		{"50vw", vw / 2},
		{"(min-width: 2000px) 10px, 20em", 320},
		{"(max-width: 2000px) 10px, 20em", 10},
		// A media type is no media condition.
		{"print 5px", vw},
		{"not (min-width: 1px) 5px, 6px", 6},
		{"not (min-width: 9000px) 5px, 6px", 5},
		{"(orientation: portrait) 1in", 96},
		{"calc(100vw - 10px), 7px", 7},
		{"-5px, bogus, 3pt", 4},
		{"(min-width: 1px) {} 5px, 6px", 6},
	} {
		if got := b.slotSize(ctx, tc.in); got < tc.want-1e-9 || got > tc.want+1e-9 {
			t.Errorf("slotSize(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPrepare_SelectsPictureSources(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{}
	// Each picture is told apart by its pixel width.
	for name, w := range map[string]int{"src.png": 1, "x1.png": 2, "x2.png": 3, "w400.png": 4, "w900.png": 5, "webp.png": 6, "screen.png": 7, "print.png": 8, "avif.png": 9} {
		fsys[name] = &fstest.MapFile{Data: pngBytes(t, w, 1)}
	}
	// An A4 page area is 793.7 CSS pixels wide, the default slot.
	vw := DefaultPageWidth / css.Pt
	for _, tc := range []struct {
		src     string
		want    int
		density float64
	}{
		{`<img id=i src=src.png>`, 1, 1},
		{`<img id=i src=src.png srcset="x2.png 2x">`, 1, 1},
		{`<img id=i src=src.png srcset="x1.png 1x, x2.png 2x">`, 2, 1},
		{`<img id=i srcset="x2.png 2x">`, 3, 2},
		{`<img id=i srcset="x2.png 0.5x">`, 3, 0.5},
		{`<img id=i src=src.png srcset="w400.png 400w, w900.png 900w">`, 5, 900 / vw},
		{`<img id=i src=src.png srcset="w400.png 400w, w900.png 900w" sizes="400px">`, 4, 1},
		{`<picture><source srcset=avif.png type=image/avif><source srcset=webp.png type=image/webp><img id=i src=src.png></picture>`, 6, 1},
		{`<picture><source srcset=screen.png media="screen"><source srcset=print.png media="print"><img id=i src=src.png></picture>`, 8, 1},
		{`<picture><source srcset="w400.png 400w, w900.png 900w" sizes="(min-width: 100px) 300px"><img id=i src=src.png></picture>`, 4, 400.0 / 300},
		{`<picture><source srcset=screen.png media="screen"><img id=i src=src.png srcset="x1.png"></picture>`, 2, 1},
		// A source after the img does not count.
		{`<picture><img id=i src=src.png><source srcset=print.png></picture>`, 1, 1},
		{`<picture><source src=print.png><img id=i src=src.png></picture>`, 1, 1},
	} {
		p := prep(t, tc.src, Options{Resources: fsys})
		pic := byID(t, p.root, "i").Picture
		if pic == nil || pic.Source == nil || pic.Source.Image == nil || pic.Source.Image.Bounds().Dx() != tc.want {
			t.Errorf("%s: picture %+v, want the one %d px wide; warnings %+v", tc.src, pic, tc.want, p.warnings)
			continue
		}
		// The density-corrected natural size.
		if w := float64(tc.want) * css.Pt / tc.density; pic.Width < w-1e-9 || pic.Width > w+1e-9 {
			t.Errorf("%s: width %v, want %v", tc.src, pic.Width, w)
		}
	}
	// Screen media picks the screen source.
	p := prep(t, `<picture><source srcset=screen.png media="screen"><source srcset=print.png><img id=i src=src.png></picture>`, Options{Resources: fsys, Media: MediaScreen})
	if pic := byID(t, p.root, "i").Picture; pic == nil || pic.Width != 7*css.Pt {
		t.Errorf("screen: %+v", pic)
	}
	// The decoded picture is shared, its size per element.
	p = prep(t, `<img id=a srcset="x2.png 2x"><img id=b src=x2.png>`, Options{Resources: fsys})
	if a, b := byID(t, p.root, "a").Picture, byID(t, p.root, "b").Picture; a.Width != 1.5*css.Pt || b.Width != 3*css.Pt || a.Source != b.Source {
		t.Errorf("shared picture: %+v %+v", a, b)
	}
}

func FuzzParseSrcset(f *testing.F) {
	for _, s := range []string{"a.png 1x, b.png 2x", "a 100w, b 200w 10h", "data:x,y (a,b) 2x,,", "a 1e400x", ""} {
		f.Add(s, "(max-width: 10px) 5vw, 100vw")
	}
	b := &builder{media: css.Media{Type: "print", Width: DefaultPageWidth, Height: DefaultPageHeight}}
	f.Fuzz(func(t *testing.T, srcset, sizes string) {
		cands := parseSrcset(srcset)
		if len(cands) > maxSrcsetCandidates {
			t.Fatalf("%d candidates", len(cands))
		}
		for _, c := range cands {
			if c.url == "" || c.width < 0 || c.density < 0 {
				t.Fatalf("candidate %+v", c)
			}
		}
		slot := b.slotSize(context.Background(), sizes)
		if !(slot > 0) {
			t.Fatalf("slot %v", slot)
		}
		if len(cands) > 0 {
			_, _ = pickCandidate(cands, slot)
		}
	})
}
