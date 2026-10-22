package page

import (
	"encoding/binary"
	"hash/maphash"
	"math"
	"slices"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
)

// Shared carries what several renders of one page at one resolution compute
// alike, passed to each in [Options.Shared]: the coverage of each glyph
// outline they fill, and the content of each form XObject they paint. A
// caller that renders the page whole and its text alone rasterises each glyph
// once. The values a render computes do not depend on it. The document must
// not change between the renders that share one.
//
// A Shared is safe for concurrent use.
type Shared struct {
	forms *formCache

	mu     sync.Mutex
	seed   maphash.Seed
	glyphs map[uint64][]sharedCoverage
	// pixels counts the coverage values held, which [Shared.limit] bounds.
	pixels int64
	limit  int64
}

// sharedCoverage is the coverage of one device path over a raster of width
// × height pixels.
type sharedCoverage struct {
	path          geom.Path
	width, height int
	cov           *raster.Coverage
}

// NewShared returns an empty [Shared] holding at most maxPixels coverage
// values, the render pixel budget of the document its renders draw from: a
// glyph past it is rasterised by each render as it would be without one.
func NewShared(maxPixels int64) *Shared {
	return &Shared{forms: newFormCache(), seed: maphash.MakeSeed(), glyphs: make(map[uint64][]sharedCoverage), limit: maxPixels}
}

// fill returns the coverage of the device path p filled with the nonzero
// rule over a raster of width × height pixels, as [raster.Fill] computes it
// with no clip, computed once across the renders sharing s. The coverage is
// shared: the caller copies it before changing it.
func (s *Shared) fill(p geom.Path, width, height int) (*raster.Coverage, error) {
	key := s.key(p, width, height)
	s.mu.Lock()
	for _, e := range s.glyphs[key] {
		if e.width == width && e.height == height && slices.Equal(e.path.Ops, p.Ops) && samePoints(e.path.Pts, p.Pts) {
			s.mu.Unlock()
			return e.cov, nil
		}
	}
	s.mu.Unlock()
	cov, err := raster.Fill(p, raster.NonZero, width, height, nil)
	if err != nil {
		return nil, err
	}
	n := int64(cov.Width) * int64(cov.Height)
	s.mu.Lock()
	if s.pixels+n <= s.limit {
		s.pixels += n
		s.glyphs[key] = append(s.glyphs[key], sharedCoverage{
			path:  geom.Path{Ops: slices.Clone(p.Ops), Pts: slices.Clone(p.Pts)},
			width: width, height: height, cov: cov,
		})
	}
	s.mu.Unlock()
	return cov, nil
}

// key hashes a path and a raster size.
func (s *Shared) key(p geom.Path, width, height int) uint64 {
	var h maphash.Hash
	h.SetSeed(s.seed)
	var b [8]byte
	put := func(v uint64) {
		binary.LittleEndian.PutUint64(b[:], v)
		_, _ = h.Write(b[:]) //nolint:errcheck // maphash.Hash.Write never fails.
	}
	put(uint64(width))  //nolint:gosec // a raster side is positive and fits in 63 bits.
	put(uint64(height)) //nolint:gosec // a raster side is positive and fits in 63 bits.
	for _, op := range p.Ops {
		_ = h.WriteByte(byte(op)) //nolint:errcheck // maphash.Hash.WriteByte never fails.
	}
	for _, pt := range p.Pts {
		put(math.Float64bits(pt.X))
		put(math.Float64bits(pt.Y))
	}
	return h.Sum64()
}

// samePoints reports whether a and b hold the same points bit for bit, so
// that two paths that compare equal give the same coverage.
func samePoints(a, b []geom.Point) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Float64bits(a[i].X) != math.Float64bits(b[i].X) || math.Float64bits(a[i].Y) != math.Float64bits(b[i].Y) {
			return false
		}
	}
	return true
}
