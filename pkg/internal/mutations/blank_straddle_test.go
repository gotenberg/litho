package mutations

import (
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
)

// clearedSamples returns the samples of a w x 1 image [forCoveredSamples]
// clears under box for the regions, in order.
func clearedSamples(t *testing.T, box graphics.ImageBox, w int, regions ...[4]float64) []int {
	t.Helper()
	zones := make([]heuristics.Zone, 0, len(regions))
	for _, r := range regions {
		zones = append(zones, heuristics.ZoneFromRect(r))
	}
	var got []int
	n, err := forCoveredSamples(box, zones, w, 1, func(px, _ int) { got = append(got, px) })
	if err != nil {
		t.Fatalf("forCoveredSamples: %v", err)
	}
	if n != len(got) {
		t.Fatalf("reported %d cleared, cleared %d", n, len(got))
	}
	slices.Sort(got)
	return got
}

// TestForCoveredSamples_ClearsASampleWhoseCellTheRegionCuts places a 4 by 1
// image 50 points a sample over x 100 to 300: sample 1 fills 150 to 200, its
// centre at 175. A region whose left edge cuts that cell right of the centre
// holds the centres of samples 2 and 3 only, and sample 1 still painted its
// picture over the part of its cell inside the region, up to 25 points deep.
// It is cleared with them; a sample whose cell only touches the region's edge
// is not.
func TestForCoveredSamples_ClearsASampleWhoseCellTheRegionCuts(t *testing.T) {
	t.Parallel()
	box := graphics.ImageBox{CTM: [6]float64{200, 0, 0, 100, 100, 300}}
	for _, tc := range []struct {
		name string
		edge float64
		want []int
	}{
		{"edge left of the centre", 170, []int{1, 2, 3}},
		{"edge on the centre", 175, []int{1, 2, 3}},
		{"edge right of the centre", 176, []int{1, 2, 3}},
		{"edge near the cell's end", 199, []int{1, 2, 3}},
		{"edge on the cell boundary", 200, []int{2, 3}},
		{"edge a hair past the boundary", 200 + 1e-12, []int{2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := clearedSamples(t, box, 4, [4]float64{tc.edge, 250, 400, 450}); !slices.Equal(got, tc.want) {
				t.Fatalf("cleared %v, want %v", got, tc.want)
			}
		})
	}
}

// TestForCoveredSamples_ClearsNothingWithoutACentre keeps the refusal of a
// region between the centres: one inside sample 1's cell, right of its
// centre, holds none, and clearing the cell would clear a stretched
// sample's picture over an area the caller never named.
func TestForCoveredSamples_ClearsNothingWithoutACentre(t *testing.T) {
	t.Parallel()
	box := graphics.ImageBox{CTM: [6]float64{200, 0, 0, 100, 100, 300}}
	if got := clearedSamples(t, box, 4, [4]float64{180, 250, 195, 450}); len(got) != 0 {
		t.Fatalf("cleared %v, want none", got)
	}
}

// TestForCoveredSamples_ClearsCutCellsUnderRotation rotates the same image a
// quarter turn about its origin, so its samples run up x 0 to -100 by 50
// points each along y 100 to 300: a region cutting sample 1's cell (y 150 to
// 200) above its centre clears it, and the bounding box of a cell turned
// another way is not what decides.
func TestForCoveredSamples_ClearsCutCellsUnderRotation(t *testing.T) {
	t.Parallel()
	box := graphics.ImageBox{CTM: [6]float64{0, 200, -100, 0, 0, 100}}
	if got := clearedSamples(t, box, 4, [4]float64{-200, 180, 100, 400}); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("cleared %v, want [1 2 3]", got)
	}

	// Turned by 45 degrees, a cell's bounding box reaches a region its
	// diamond does not. A 2 by 1 image of square cells of side 100 stands on
	// a corner at the origin: sample 0 is the diamond (0,0) (s,s) (0,2s)
	// (-s,s), sample 1 the one above it across x+y = 2s, its centre at
	// (s,2s). A small region round that centre meets the bounding box of
	// sample 0, [-s 0 s 2s], and none of its diamond.
	s := 100 / math.Sqrt2
	rot := graphics.ImageBox{CTM: [6]float64{2 * s, 2 * s, -s, s, 0, 0}}
	region := [4]float64{s - 1, 2*s - 1, s + 1, 2*s + 1}
	if got := clearedSamples(t, rot, 2, region); !slices.Equal(got, []int{1}) {
		t.Fatalf("cleared %v, want [1]", got)
	}
}
