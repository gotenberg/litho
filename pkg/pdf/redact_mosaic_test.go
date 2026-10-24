package pdf

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// mosaicStrips draws /Im1 as n strips 2 points high, 4 points apart from
// y = 600, each under its own q, colour, cm ... Q, the widths tracing a
// diamond: a picture drawn as a mosaic of small images, as Word writes a
// logo.
func mosaicStrips(n int) string {
	var b strings.Builder
	for i := range n {
		width := 20 + 10*min(i, n-1-i)
		fmt.Fprintf(&b, "q 0 g %d 0 0 2 %d %d cm /Im1 Do Q\n", width, 300-width/2, 600+4*i)
	}
	return b.String()
}

// TestWriter_Redact_RemovesAMosaicTheRegionCoversWhole pins that the image
// placements a region covers whole are removed, with the q ... Q group that
// placed them, so a picture drawn as a mosaic of small images keeps no
// outline. Blanked in place, each strip painted black where it was, and the
// page redacted whole still showed the shape.
func TestWriter_Redact_RemovesAMosaicTheRegionCoversWhole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const strips = 10
	src := imageDoc([]string{mosaicStrips(strips)}, "", "", "")
	out, report, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: [4]float64{0, 0, 612, 792}}})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if report.RemovedImages != strips {
		t.Errorf("RemovedImages = %d, want %d", report.RemovedImages, strips)
	}
	for i := range strips {
		if got := renderedGray(t, out, 1, [2]float64{300, float64(601 + 4*i)}); !nearGray(got, 0xff) {
			t.Errorf("strip %d = %d, want the paper", i, got)
		}
	}
	content := pageContent(ctx, t, out, 1)
	for _, op := range []string{"cm", "Do"} {
		if strings.Contains(content, op) {
			t.Errorf("page content still holds %s, which places the strips: %q", op, content)
		}
	}
	// Every placement had every sample covered: the image itself is
	// blanked, so the file holds none of its samples.
	if got := imageSamples(t, out, 1); !allSamples(got, 0) {
		t.Errorf("/Im1 samples = %v, want all 0", got)
	}
}

// TestWriter_Redact_RemovesTheStripsInsideAndKeepsTheRest pins that a region
// over half a mosaic removes the strips it covers whole and leaves the
// others painting the image, which therefore keeps its samples: the removed
// placements are measured as covering every sample, so the image is
// reported with the images blanked in a copy and refused under
// WithStrictRedaction.
func TestWriter_Redact_RemovesTheStripsInsideAndKeepsTheRest(t *testing.T) {
	t.Parallel()

	src := imageDoc([]string{mosaicStrips(10)}, "", "", "")
	// From between the fifth and sixth strips up.
	regions := []RedactRegion{{Page: 1, Rect: [4]float64{0, 619, 612, 792}}}
	out, report, err := redactWith(t, src, regions)
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if report.RemovedImages != 5 {
		t.Errorf("RemovedImages = %d, want 5", report.RemovedImages)
	}
	if got := report.ImagesBlankedInCopy[1]; len(got) != 1 || got[0] != "Im1" {
		t.Errorf("ImagesBlankedInCopy = %v, want Im1 on page 1", report.ImagesBlankedInCopy)
	}
	for i := range 10 {
		want := uint8(0x80)
		if i >= 5 {
			want = 0xff
		}
		if got := renderedGray(t, out, 1, [2]float64{300, float64(601 + 4*i)}); !nearGray(got, want) {
			t.Errorf("strip %d = %d, want %d", i, got, want)
		}
	}
	if got := imageSamples(t, out, 1); !allSamples(got, 0x80) {
		t.Errorf("/Im1 samples = %v, want all 0x80 for the strips outside", got)
	}
	if _, _, err := redactWith(t, src, regions, WithStrictRedaction()); err == nil {
		t.Error("strict Redact succeeded over an image painted outside the region")
	}
}

// TestWriter_Redact_RemovesOnlyTheDoOfAGroupThatPaintsMore pins that a
// placement whose q ... Q group paints something else loses its Do alone:
// the group's path, outside the region, stays.
func TestWriter_Redact_RemovesOnlyTheDoOfAGroupThatPaintsMore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := imageDoc([]string{"q 0 0 1 rg 10 10 50 50 re f 100 0 0 100 300 500 cm /Im1 Do Q"}, "", "", "")
	out, report, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: wholeRight}})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if report.RemovedImages != 1 {
		t.Errorf("RemovedImages = %d, want 1", report.RemovedImages)
	}
	content := pageContent(ctx, t, out, 1)
	if strings.Contains(content, "Do") || !strings.Contains(content, "re") {
		t.Errorf("page content = %q, want the path kept and the Do gone", content)
	}
	if got := renderedGray(t, out, 1, copyRight); !nearGray(got, 0xff) {
		t.Errorf("removed placement = %d, want the paper", got)
	}
}
