package mutations

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestRedactPageRegions_RefusesAShadingReachingTheRegion pins that a shading
// paint the region may reach refuses. sh fills the whole clip in force, which
// here is the page, so redacting a region used to report it clear while the
// shading's ink stayed inside.
func TestRedactPageRegions_RefusesAShadingReachingTheRegion(t *testing.T) {
	t.Parallel()
	doc, page := redactPathFixture(t, "/Sh0 sh")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 90, 160, 160}})
	if !errors.Is(err, ErrRegionCoversAShading) {
		t.Errorf("err = %v, want ErrRegionCoversAShading", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

// TestRedactPageRegions_ShadingClippedAwayFromTheRegionDoesNotRefuse pins
// that a shading whose clip keeps it away from the region is left alone.
func TestRedactPageRegions_ShadingClippedAwayFromTheRegionDoesNotRefuse(t *testing.T) {
	t.Parallel()
	doc, page := redactPathFixture(t, "q 400 400 50 50 re W n /Sh0 sh Q 100 100 50 50 re f")
	alloc := writer.NewObjAllocator(20)

	if _, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 90, 160, 160}}); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
}
