package effect

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// benchContent returns, by name, one effect's content over a region of
// 160 by 120 points: a text box, a picture, a gradient shape and a group
// of twenty shapes.
func benchContent(b *testing.B) map[string][]typeset.Fragment {
	b.Helper()
	k := contentKinds(b)
	box := []typeset.Fragment{&typeset.Path{Ops: rect(2, 2, 158, 118), Fill: &typeset.Fill{Color: rgba(1, 1, 0.9, 1)}, Stroke: &typeset.Stroke{Color: typeset.Black, Width: 1}}}
	for i := range 8 {
		box = append(box, textRun(b, 11, 8, 18+float64(i)*13, "ABBA BAAB AB BA ABBA BA AB", typeset.Black))
	}
	return map[string][]typeset.Fragment{
		"text":     box,
		"picture":  k["picture"][:1],
		"gradient": k["gradient"],
		"shapes":   k["shapes"],
	}
}

// BenchmarkPaint times each step of [Paint] for one content: the write,
// the open, the render and the quantisation, and the whole call.
func BenchmarkPaint(b *testing.B) {
	ctx := context.Background()
	for name, frags := range benchContent(b) {
		for _, scale := range []float64{1, 2, 4} {
			c := Content{Fragments: frags, Grid: Grid{Scale: scale}, W: int(160 * scale), H: int(120 * scale)}
			pg, err := contentPage(&c)
			if err != nil {
				b.Fatal(err)
			}
			doc := &typeset.Document{Pages: []*typeset.Page{pg}}
			data, err := writeContent(ctx, doc)
			if err != nil {
				b.Fatal(err)
			}
			open := func() (*document.Document, *document.Page) {
				d, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
				if err != nil {
					b.Fatal(err)
				}
				cat, err := document.ResolveCatalog(ctx, d)
				if err != nil {
					b.Fatal(err)
				}
				pages, err := document.ResolvePages(ctx, d, cat)
				if err != nil {
					b.Fatal(err)
				}
				return d, pages[0]
			}
			d, p := open()
			opts := page.Options{DPI: float64(scale * pointsPerInch)}
			canvas, _, err := page.Render(ctx, d, p, opts)
			if err != nil {
				b.Fatal(err)
			}
			tag := fmt.Sprintf("%s/%gx", name, scale)
			b.Run(tag+"/write", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := writeContent(ctx, doc); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(len(data)), "pdf-bytes")
			})
			b.Run(tag+"/open", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					open()
				}
			})
			b.Run(tag+"/render", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, _, err := page.Render(ctx, d, p, opts); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run(tag+"/quantise", func(b *testing.B) {
				s := newSurface(b, RGBA, c.W, c.H)
				b.ReportAllocs()
				for b.Loop() {
					quantiseInto(s, canvas)
				}
			})
			b.Run(tag+"/paint", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					s, err := Paint(ctx, []Content{c})
					if err != nil {
						b.Fatal(err)
					}
					s[0].Release()
				}
			})
		}
	}
}

// BenchmarkPaint_Batch paints eight text boxes of one face at 2 pixels a
// point, in one call and in a call each: one call writes the face once.
func BenchmarkPaint_Batch(b *testing.B) {
	ctx := context.Background()
	box := benchContent(b)["text"]
	cs := make([]Content, 8)
	for i := range cs {
		cs[i] = Content{Fragments: box, Grid: Grid{Scale: 2}, W: 320, H: 240}
	}
	release := func(s []*Surface) {
		for _, x := range s {
			x.Release()
		}
	}
	b.Run("one-call", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s, err := Paint(ctx, cs)
			if err != nil {
				b.Fatal(err)
			}
			release(s)
		}
	})
	b.Run("call-each", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			for _, c := range cs {
				s, err := Paint(ctx, []Content{c})
				if err != nil {
					b.Fatal(err)
				}
				release(s)
			}
		}
	})
}
