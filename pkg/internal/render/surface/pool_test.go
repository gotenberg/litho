package surface

import "testing"

func TestPoolReusesZeroedBuffers(t *testing.T) {
	t.Parallel()
	var pool Pool
	p, err := pool.NewPlane(3, 2, 4, 100)
	if err != nil {
		t.Fatal(err)
	}
	for i := range p.Pix {
		p.Pix[i] = 1
	}
	pool.Release(p.Pix)
	q, err := pool.NewPlane(3, 2, 4, 100)
	if err != nil {
		t.Fatal(err)
	}
	if &q.Pix[0] != &p.Pix[0] {
		t.Error("a plane of a released plane's size did not reuse its pixels")
	}
	for i, v := range q.Pix {
		if v != 0 {
			t.Fatalf("reused pixel value %d is %v, want 0", i, v)
		}
	}
	other, err := pool.NewPlane(2, 2, 4, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Pix) != 5*2*2 {
		t.Errorf("plane of %d values, want %d", len(other.Pix), 5*2*2)
	}
	for range maxPooledBuffers + 2 {
		pool.Release(make([]float32, 7))
	}
	if len(pool.free) != maxPooledBuffers {
		t.Errorf("pool keeps %d buffers, want at most %d", len(pool.free), maxPooledBuffers)
	}
	if _, err := pool.NewPlane(3, 2, MaxPlaneChannels+1, 100); err == nil {
		t.Error("a plane of too many channels was made")
	}
	var none *Pool
	none.Release(make([]float32, 3))
	if plane, err := none.NewPlane(1, 1, 1, 1); err != nil || len(plane.Pix) != 2 {
		t.Errorf("a nil pool made %v, %v", plane, err)
	}
}
