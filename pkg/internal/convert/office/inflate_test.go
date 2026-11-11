package office

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"testing"
)

// TestRead_InflateRatio pins [MaxInflateRatio]: an entry inflating past
// it is refused, whatever its header declares, and one under it or under
// the floor reads.
func TestRead_InflateRatio(t *testing.T) {
	t.Parallel()

	noise := make([]byte, 2<<20)
	r := rand.New(rand.NewPCG(1, 2))
	for i := range noise {
		noise[i] = byte(r.Uint32())
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range map[string][]byte{
		"bomb":  make([]byte, 4<<20),
		"small": make([]byte, inflateRatioFloor),
		"noise": noise,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p, err := OpenPackage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := p.Read(ctx, "bomb", false, 0); !errors.Is(err, ErrInputTooLarge) {
		t.Errorf("4 MiB of zeros: %v, want ErrInputTooLarge", err)
	}
	for _, name := range []string{"small", "noise"} {
		if _, err := p.Read(ctx, name, false, 0); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := InflateLimit(10, 1<<30); got != inflateRatioFloor {
		t.Errorf("InflateLimit(10) = %d, want the floor", got)
	}
	if got := InflateLimit(1<<40, 1<<30); got != 1<<30 {
		t.Errorf("InflateLimit(2^40) = %d, want the limit", got)
	}
}
