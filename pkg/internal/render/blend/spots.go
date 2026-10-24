package blend

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// MaxSpots bounds the spot colorant planes a group keeps beside its process
// colorants. Not spec-mandated: ISO 32000-1, 8.6.6.4 lets a page name any
// number of colorants, and a device prints as many as it has inks. Four is
// the width of [Components] and of a [surface.Plane], and the number of spot
// planes poppler 26.09.0's overprint preview keeps (SPOT_NCOMPS); a colour
// naming a fifth spot colorant reaches the group through its alternate space.
const MaxSpots = surface.MaxPlaneChannels

// AllSpots has the [Params.Keep] bit of every spot colorant plane.
const AllSpots uint8 = 0xf0

// SpotBit returns the [Params.Keep] bit of spot colorant plane j.
func SpotBit(j int) uint8 { return 1 << (4 + j) }

// SpotTable maps the ink of one spot colorant to the process inks it prints
// as: entry i is the CMYK components, amounts of ink, of the colorant at
// ink i/(len-1), its alternate space's colour (ISO 32000-1, 8.6.6.4).
type SpotTable []Components

// at returns the process inks of ink t in [0, 1], interpolated between the
// table's entries.
func (t SpotTable) at(t0 float32) Components {
	n := len(t) - 1
	if n < 1 {
		if n == 0 {
			return t[0]
		}
		return Components{}
	}
	f := clamp01(t0) * float32(n)
	i := min(int(f), n-1)
	w := f - float32(i)
	var c Components
	for k := range 4 {
		c[k] = t[i][k] + (t[i+1][k]-t[i][k])*w
	}
	return c
}

// EnableSpots gives a CMYK group [MaxSpots] spot colorant planes, holding no
// ink, which [Params.Spots] paints and [Params.Keep] leaves, refused beyond
// maxPixels. Only an isolated group keeps them, the page group of an
// overprint preview: a group composited into a parent passes its process
// colour alone ([Group.Into]). A second call keeps the planes.
func (g *Group) EnableSpots(maxPixels int64) error {
	if g.spots != nil {
		return nil
	}
	if g.space.Kind() != CMYK || !g.attrs.Isolated {
		return fmt.Errorf("%w: spot colorant planes beside a group of another space or a non-isolated group", ErrNotCMYK)
	}
	spots, err := surface.NewPlane(g.bounds.Dx(), g.bounds.Dy(), MaxSpots, maxPixels)
	if err != nil {
		return fmt.Errorf("allocating spot colorant planes: %w", err)
	}
	g.spots = spots
	return nil
}

// HasSpots reports whether the group keeps spot colorant planes.
func (g *Group) HasSpots() bool { return g.spots != nil }

// CompositeSpots folds the spot colorant planes into the group's process
// colorants and drops them: plane j's ink adds the process inks tables[j]
// gives it to each pixel's, each sum clipped to full ink, as a CMYK device
// proofs a spot ink through its alternate colour (ISO 32000-1, 8.6.6.4) and
// as poppler 26.09.0's overprint preview converts its spot planes. A plane
// with no table is dropped. It does nothing to a group without spot planes.
func (g *Group) CompositeSpots(ctx context.Context, tables []SpotTable) error {
	if g.spots == nil {
		return nil
	}
	n, s := g.n, g.stride
	const ss = MaxSpots + 1
	w, h := g.bounds.Dx(), g.bounds.Dy()
	for y := range h {
		if y%rowsPerContextCheck == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("compositing spot colorants: %w", err)
			}
		}
		for x := range w {
			i := y*w + x
			q := g.spots.Pix[ss*i : ss*i+MaxSpots]
			p := g.color.Pix[s*i : s*i+s]
			a := p[n]
			if a <= 0 {
				continue
			}
			var add Components
			painted := false
			for j, t := range tables {
				if j >= MaxSpots || q[j] <= 0 || len(t) == 0 {
					continue
				}
				c := t.at(q[j] / a)
				for k := range 4 {
					add[k] += c[k]
				}
				painted = true
			}
			if !painted {
				continue
			}
			// The plane holds each process ink's complement premultiplied
			// ([Space.store]): its ink is a - p.
			for k := range n {
				ink := clamp01((a-p[k])/a + add[k])
				p[k] = a * (1 - ink)
			}
		}
	}
	g.spots = nil
	return nil
}
