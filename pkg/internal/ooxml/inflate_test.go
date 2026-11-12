package ooxml

import (
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// TestPackage_Read_InflateRatio checks that a part inflating past
// office.MaxInflateRatio is refused, and that one under the ratio or under
// its floor reads: 4 MiB of zeros deflate to a few kilobytes, 1 MiB of
// zeros stays under the floor, and noise does not deflate at all.
func TestPackage_Read_InflateRatio(t *testing.T) {
	t.Parallel()
	noise := make([]byte, 2<<20)
	r := rand.New(rand.NewPCG(1, 2))
	for i := range noise {
		noise[i] = byte(r.Uint32())
	}
	files := minimalPackage()
	files["bomb"] = string(make([]byte, 4<<20))
	files["small"] = string(make([]byte, 1<<20))
	files["noise"] = string(noise)
	p, err := Open(buildZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Read("bomb"); !errors.Is(err, limits.ErrInputTooLarge) {
		t.Errorf("4 MiB of zeros: %v, want limits.ErrInputTooLarge", err)
	}
	for _, name := range []string{"small", "noise"} {
		if _, err := p.Read(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
