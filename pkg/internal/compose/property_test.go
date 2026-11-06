package compose_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"math/rand"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/typography/embed"
	"github.com/gotenberg/litho/pkg/pdf"
)

// TestWriteRandomDocumentsOpen writes random small documents, every fragment
// kind, nesting, structure, links and outline included, and checks each one
// opens with its page count and extracts without error.
func TestWriteRandomDocumentsOpen(t *testing.T) {
	t.Parallel()
	font, err := embed.Load(sans.Program)
	if err != nil {
		t.Fatalf("embed.Load: %v", err)
	}
	for seed := range int64(60) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic test data, not security
			doc := randomDoc(rng, font)
			if err := doc.Validate(); err != nil {
				t.Fatalf("random document refused: %v", err)
			}
			out := write(t, doc)
			r := open(t, out)
			ctx := context.Background()
			if n, err := r.PageCount(ctx); err != nil || n != len(doc.Pages) {
				t.Fatalf("PageCount = %d, %v; want %d", n, err, len(doc.Pages))
			}
			for i := range doc.Pages {
				if _, err := r.ExtractText(ctx, i+1); err != nil && !errors.Is(err, pdf.ErrRequiresOCR) {
					t.Errorf("ExtractText(%d): %v", i+1, err)
				}
			}
			if _, err := r.RenderPage(ctx, 1, pdf.WithRenderDPI(36)); err != nil {
				t.Errorf("RenderPage: %v", err)
			}
			if !bytes.Equal(out, write(t, doc)) {
				t.Error("second write differs")
			}
		})
	}
}

func randomDoc(rng *rand.Rand, font *embed.Font) *compose.Document {
	doc := &compose.Document{Metadata: compose.Metadata{Title: fmt.Sprint("doc ", rng.Int())}}
	var elems []*compose.StructElem
	if rng.Intn(2) == 0 {
		doc.Structure = &compose.StructElem{Role: "Document"}
		for range 1 + rng.Intn(4) {
			e := &compose.StructElem{Role: []string{"P", "H1", "Figure", "Span"}[rng.Intn(4)]}
			doc.Structure.Kids = append(doc.Structure.Kids, e)
			elems = append(elems, e)
		}
	}
	parent := func() *compose.StructElem {
		if len(elems) == 0 || rng.Intn(3) == 0 {
			return nil
		}
		return elems[rng.Intn(len(elems))]
	}
	color := func() compose.Color {
		return compose.Color{R: rng.Float64(), G: rng.Float64(), B: rng.Float64(), A: rng.Float64()}
	}
	pic := &compose.ImageSource{Image: image.NewNRGBA(image.Rect(0, 0, 1+rng.Intn(4), 1+rng.Intn(4)))}
	pages := 1 + rng.Intn(3)
	var frags func(depth int) []compose.Fragment
	frags = func(depth int) []compose.Fragment {
		var out []compose.Fragment
		for range rng.Intn(6) {
			switch rng.Intn(4) {
			case 0:
				txt := &compose.Text{Face: []*compose.Face{sans, serif}[rng.Intn(2)], Size: 1 + rng.Float64()*30, Color: color(),
					Origin: compose.Point{X: rng.Float64() * 500, Y: rng.Float64() * 700}, Parent: parent()}
				x := 0.0
				for range rng.Intn(20) {
					r := rune(0x20 + rng.Intn(95))
					gid, _ := font.GID(r)
					txt.Glyphs = append(txt.Glyphs, compose.Glyph{GID: gid, X: x, Y: float64(rng.Intn(3)), Text: string(r)})
					x += rng.Float64() * 15
				}
				out = append(out, txt)
			case 1:
				out = append(out, &compose.Image{Rect: compose.Rect{X: rng.Float64() * 500, Y: rng.Float64() * 700, Width: rng.Float64() * 100, Height: rng.Float64() * 100}, Source: pic, Parent: parent()})
			case 2:
				p := &compose.Path{Ops: rect(rng.Float64()*500, rng.Float64()*700, rng.Float64()*100, rng.Float64()*100), Parent: parent()}
				if rng.Intn(2) == 0 {
					p.Fill = &compose.Fill{Color: color(), Rule: compose.FillRule(rng.Intn(2))}
					if rng.Intn(2) == 0 {
						p.Fill.Shading = randomShading(rng, color)
					}
				}
				if rng.Intn(2) == 0 {
					p.Stroke = &compose.Stroke{Color: color(), Width: rng.Float64() * 5, Cap: compose.LineCap(rng.Intn(3)), Join: compose.LineJoin(rng.Intn(3)), Dash: []float64{1 + rng.Float64(), rng.Float64()}}
				}
				out = append(out, p)
			case 3:
				if depth < 4 {
					g := &compose.Group{Opacity: rng.Float64(), Transform: compose.Matrix{A: 1, B: rng.Float64() - 0.5, C: rng.Float64() - 0.5, D: 1, E: rng.Float64() * 50, F: rng.Float64() * 50}, Content: frags(depth + 1)}
					if rng.Intn(2) == 0 {
						g.Clip = &compose.Path{Ops: rect(0, 0, rng.Float64()*400, rng.Float64()*400)}
					}
					out = append(out, g)
				}
			}
		}
		return out
	}
	for range pages {
		page := &compose.Page{Width: 100 + rng.Float64()*600, Height: 100 + rng.Float64()*800, Content: frags(0)}
		for range rng.Intn(3) {
			l := compose.Link{Rect: compose.Rect{X: rng.Float64() * 90, Y: rng.Float64() * 90, Width: 10, Height: 10}}
			if rng.Intn(2) == 0 {
				l.URI = "https://example.com/" + fmt.Sprint(rng.Int())
			} else {
				l.Dest = &compose.Dest{Page: 1 + rng.Intn(pages), X: 0, Y: rng.Float64() * 50}
			}
			if len(elems) > 0 && rng.Intn(2) == 0 {
				l.Parent = elems[rng.Intn(len(elems))]
			}
			page.Links = append(page.Links, l)
		}
		doc.Pages = append(doc.Pages, page)
	}
	for range rng.Intn(4) {
		o := compose.Outline{Title: fmt.Sprint("entry ", rng.Int()), Dest: compose.Dest{Page: 1 + rng.Intn(pages)}}
		if rng.Intn(2) == 0 {
			o.Kids = []compose.Outline{{Title: "kid é", Dest: compose.Dest{Page: 1}}}
		}
		doc.Outline = append(doc.Outline, o)
	}
	return doc
}

// randomShading returns an axial or radial shading of a few stops, some
// translucent, some sharing an offset.
func randomShading(rng *rand.Rand, color func() compose.Color) *compose.Shading {
	sh := &compose.Shading{
		Kind:     compose.ShadingKind(rng.Intn(2)),
		From:     compose.Point{X: rng.Float64() * 500, Y: rng.Float64() * 700},
		To:       compose.Point{X: rng.Float64() * 500, Y: rng.Float64() * 700},
		ToRadius: 1 + rng.Float64()*100, FromRadius: rng.Float64() * 10,
		Transform: compose.Matrix{A: 0.5 + rng.Float64(), B: rng.Float64() - 0.5, D: 0.5 + rng.Float64(), E: rng.Float64() * 20},
	}
	n := 2 + rng.Intn(5)
	off := 0.0
	for i := range n {
		if i == n-1 {
			off = 1
		} else if i > 0 && rng.Intn(3) > 0 {
			off += (1 - off) * rng.Float64()
		}
		c := color()
		if i == 0 {
			c.A = 1
		}
		sh.Stops = append(sh.Stops, compose.ColorStop{Offset: off, Color: c})
	}
	return sh
}
