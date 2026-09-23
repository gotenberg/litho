package jpx

import (
	"container/heap"
	"context"
	"fmt"
)

// maxPacketSteps bounds the packet slots one tile's progression walks,
// including the slots it skips because an earlier progression volume already
// read them. It is an anti-DoS guard, not spec-mandated: every packet read
// costs at least a byte, but skipped slots cost nothing, and a POC marker
// segment of some ten thousand volumes, each spanning every layer of every
// precinct, would walk trillions of them. The busiest tile in the corpora
// reads about 5 000 packets; 2^26 slots take well under a second.
const maxPacketSteps = 1 << 26

// Progression orders, T.800 Table A.16.
const (
	orderLRCP = 0
	orderRLCP = 1
	orderRPCL = 2
	orderPCRL = 3
)

// packetSlot names one packet: its layer, resolution level, component and
// precinct.
type packetSlot struct {
	layer, r, c int
	p           *precinct
}

// progression walks the packets of one tile in the order of its progression
// volumes (T.800, B.12), handing each packet not yet read to visit.
type progression struct {
	ctx   context.Context
	comps []tileComponent
	// dx and dy hold each component's sample separation, and tx0 and ty0 the
	// tile's origin on the reference grid, for the position orders.
	dx, dy   []int
	tx0, ty0 int
	steps    int
	visit    func(packetSlot) error
}

// step counts one packet slot against maxPacketSteps and checks for
// cancellation now and then.
func (w *progression) step() error {
	w.steps++
	if w.steps > maxPacketSteps {
		return fmt.Errorf("%w: progression walks more than %d packet slots", ErrLimit, maxPacketSteps)
	}
	if w.steps%1024 == 0 {
		return w.ctx.Err()
	}
	return nil
}

// emit reads the packet of layer from precinct k of resolution level r of
// component c if it is the precinct's next packet. A progression volume
// restarts its layer loop at 0, and T.800, B.12.2 says the packets it already
// covered are not repeated.
func (w *progression) emit(layer, r, c, k int) error {
	if err := w.step(); err != nil {
		return err
	}
	p := &w.comps[c].resolutions[r].precincts[k]
	if layer != p.layers {
		return nil
	}
	if err := w.visit(packetSlot{layer: layer, r: r, c: c, p: p}); err != nil {
		return err
	}
	p.layers++
	return nil
}

// levels returns the number of resolution levels of component c.
func (w *progression) levels(c int) int {
	return len(w.comps[c].resolutions)
}

// run walks one progression volume (T.800, B.12.1 bounded as in B.12.2).
func (w *progression) run(v progressionChange) error {
	switch v.order {
	case orderLRCP:
		for l := range v.layerEnd {
			for r := v.resStart; r < v.resEnd; r++ {
				if err := w.layerLevel(l, r, v); err != nil {
					return err
				}
			}
		}
	case orderRLCP:
		for r := v.resStart; r < v.resEnd; r++ {
			for l := range v.layerEnd {
				if err := w.layerLevel(l, r, v); err != nil {
					return err
				}
			}
		}
	case orderRPCL:
		for r := v.resStart; r < v.resEnd; r++ {
			var streams []*precinctStream
			for c := v.compStart; c < v.compEnd; c++ {
				streams = w.addStream(streams, c, r, c)
			}
			if err := w.merge(streams, v.layerEnd); err != nil {
				return err
			}
		}
	case orderPCRL:
		var streams []*precinctStream
		for c := v.compStart; c < v.compEnd; c++ {
			for r := v.resStart; r < v.resEnd; r++ {
				streams = w.addStream(streams, c, r, c*(maxLevels+1)+r)
			}
		}
		return w.merge(streams, v.layerEnd)
	default:
		for c := v.compStart; c < v.compEnd; c++ {
			var streams []*precinctStream
			for r := v.resStart; r < v.resEnd; r++ {
				streams = w.addStream(streams, c, r, r)
			}
			if err := w.merge(streams, v.layerEnd); err != nil {
				return err
			}
		}
	}
	return nil
}

// layerLevel walks the component and precinct loops shared by the two
// layer-major orders.
func (w *progression) layerLevel(l, r int, v progressionChange) error {
	for c := v.compStart; c < v.compEnd; c++ {
		if r >= w.levels(c) {
			continue
		}
		for k := range w.comps[c].resolutions[r].precincts {
			if err := w.emit(l, r, c, k); err != nil {
				return err
			}
		}
	}
	return nil
}

// precinctStream yields the precincts of one resolution level of one
// component in raster order, with the reference grid point at which the
// position loops of T.800, B.12.1.3 to B.12.1.5 reach each: the first grid
// point whose coordinates satisfy their divisibility conditions and at which
// Equation (B-20) names the precinct.
type precinctStream struct {
	c, r int
	res  *resolution
	// k is the next precinct, x and y its grid point.
	k    int
	x, y int
	// rank breaks ties between streams whose next precincts share a grid
	// point, in the order of the loops the progression nests inside the
	// position loops.
	rank int
}

// addStream appends the precinct stream of resolution level r of component c
// if that level exists and has precincts.
func (w *progression) addStream(streams []*precinctStream, c, r, rank int) []*precinctStream {
	if r >= w.levels(c) {
		return streams
	}
	res := &w.comps[c].resolutions[r]
	if len(res.precincts) == 0 {
		return streams
	}
	s := &precinctStream{c: c, r: r, res: res, rank: rank}
	s.locate(w.tx0, w.ty0, w.levels(c)-1-r, w.dx[c], w.dy[c])
	return append(streams, s)
}

// locate sets the grid point of precinct s.k, where shift is NL - r. The
// first precinct of a row or column starts at the tile's edge when the level's
// origin is not a multiple of the precinct size; every other one starts at a
// multiple of XRsiz * 2^(PPx + NL - r) across and YRsiz * 2^(PPy + NL - r)
// down, the divisibility conditions of T.800, B.12.1.3.
func (s *precinctStream) locate(tx0, ty0, shift, dx, dy int) {
	px, py := s.k%s.res.pw, s.k/s.res.pw
	s.x = ((s.res.x0>>s.res.ppx + px) << (s.res.ppx + shift)) * dx
	if px == 0 && s.res.x0%(1<<s.res.ppx) != 0 {
		s.x = tx0
	}
	s.y = ((s.res.y0>>s.res.ppy + py) << (s.res.ppy + shift)) * dy
	if py == 0 && s.res.y0%(1<<s.res.ppy) != 0 {
		s.y = ty0
	}
}

// streamHeap orders precinct streams by the grid point of their next precinct,
// row first, then by rank.
type streamHeap []*precinctStream

func (h streamHeap) Len() int { return len(h) }
func (h streamHeap) Less(i, j int) bool {
	a, b := h[i], h[j]
	if a.y != b.y {
		return a.y < b.y
	}
	if a.x != b.x {
		return a.x < b.x
	}
	return a.rank < b.rank
}
func (h streamHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *streamHeap) Push(x any)   { *h = append(*h, x.(*precinctStream)) }
func (h *streamHeap) Pop() any {
	old := *h
	s := old[len(old)-1]
	*h = old[:len(old)-1]
	return s
}

// merge walks the precincts of streams in the order the position loops reach
// them, reading each precinct's layers below layerEnd.
//
// Walking the loops of T.800, B.12.1 literally visits every grid point of the
// tile, which for a tile of 2^32 x 2^32 points never ends. Only the grid
// points that start a precinct matter, and each stream yields them in
// increasing order, so merging the streams visits the same precincts in the
// same order in time proportional to their number.
func (w *progression) merge(streams []*precinctStream, layerEnd int) error {
	h := streamHeap(streams)
	heap.Init(&h)
	for h.Len() > 0 {
		// Counted here too, so a volume whose layers were all read already
		// still pays for the precincts it walks.
		if err := w.step(); err != nil {
			return err
		}
		s := h[0]
		for l := s.res.precincts[s.k].layers; l < layerEnd; l++ {
			if err := w.emit(l, s.r, s.c, s.k); err != nil {
				return err
			}
		}
		s.k++
		if s.k == len(s.res.precincts) {
			heap.Pop(&h)
			continue
		}
		s.locate(w.tx0, w.ty0, w.levels(s.c)-1-s.r, w.dx[s.c], w.dy[s.c])
		heap.Fix(&h, 0)
	}
	return nil
}
