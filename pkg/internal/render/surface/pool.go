package surface

import "sync"

// maxPooledBuffers bounds the buffers a [Pool] keeps between uses. Anti-DoS
// guard, not spec-mandated: each buffer may hold a whole page at the render
// pixel budget, so the bound caps the memory a pool keeps idle. 4 holds the
// page-sized rasters a caller drawing one page three times releases together.
const maxPooledBuffers = 4

// Pool keeps the pixel buffers of rasters a caller is done with, so the next
// rasters of the same size reuse them: a caller that draws page after page
// at one size allocates their rasters once, rather than asking the runtime
// for, and the garbage collector to reclaim, a page's memory for each. A
// buffer taken from the pool is zeroed, so a raster made on one is the
// raster made without it.
//
// A Pool is safe for concurrent use. The zero value is an empty pool.
type Pool struct {
	mu   sync.Mutex
	free [][]float32
}

// get returns a zeroed buffer of n values, reused when the pool holds one of
// that length.
func (p *Pool) get(n int) []float32 {
	if p != nil {
		p.mu.Lock()
		for i, b := range p.free {
			if len(b) == n {
				p.free = append(p.free[:i], p.free[i+1:]...)
				p.mu.Unlock()
				clear(b)
				return b
			}
		}
		p.mu.Unlock()
	}
	return make([]float32, n)
}

// Release hands the pixels of rasters the caller no longer reads to the
// pool. Nothing may read or write them afterwards: the next raster made on
// the pool may hold them.
func (p *Pool) Release(buffers ...[]float32) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, b := range buffers {
		if len(b) == 0 {
			continue
		}
		if len(p.free) >= maxPooledBuffers {
			p.free = p.free[1:]
		}
		p.free = append(p.free, b)
	}
}

// NewPlane returns the plane [NewPlane] makes, its pixels taken from the pool
// when it holds a buffer of their size. A nil pool allocates them.
func (p *Pool) NewPlane(width, height, channels int, maxPixels int64) (*Plane, error) {
	plane, err := newPlaneHeader(width, height, channels, maxPixels)
	if err != nil {
		return nil, err
	}
	plane.Pix = p.get((channels + 1) * width * height)
	return plane, nil
}
