package heuristics

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

func TestDetectUnappliedRedactions_RectBased(t *testing.T) {
	elements := []models.TextElement{
		{Text: "inside", X: 50, Y: 50},
		{Text: "outside", X: 200, Y: 200},
	}
	annots := []models.Annotation{
		{Subtype: "Redact", Rect: [4]float64{10, 10, 100, 100}},
	}

	result := DetectUnappliedRedactions(elements, annots)

	if !result[0].Redacted {
		t.Error("expected 'inside' element to be redacted")
	}
	if result[1].Redacted {
		t.Error("expected 'outside' element to NOT be redacted")
	}
}

func TestDetectUnappliedRedactions_QuadPointsBased(t *testing.T) {
	elements := []models.TextElement{
		{Text: "hit", X: 50, Y: 50},
		{Text: "miss", X: 300, Y: 300},
	}
	// QuadPoints defining a region around (10,10)-(100,100).
	qp := [8]float64{10, 100, 100, 100, 100, 10, 10, 10}
	annots := []models.Annotation{
		{Subtype: "Redact", QuadPoints: [][8]float64{qp}},
	}

	result := DetectUnappliedRedactions(elements, annots)

	if !result[0].Redacted {
		t.Error("expected 'hit' element to be redacted")
	}
	if result[1].Redacted {
		t.Error("expected 'miss' element to NOT be redacted")
	}
}

func TestDetectUnappliedRedactions_NoRedactAnnots(t *testing.T) {
	elements := []models.TextElement{
		{Text: "text", X: 50, Y: 50},
	}
	annots := []models.Annotation{
		{Subtype: "Highlight", Rect: [4]float64{10, 10, 100, 100}},
	}

	result := DetectUnappliedRedactions(elements, annots)

	if result[0].Redacted {
		t.Error("non-Redact annotations should not flag elements")
	}
}

func TestDetectUnappliedRedactions_Empty(t *testing.T) {
	result := DetectUnappliedRedactions(nil, nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestDetectUnappliedRedactions_InvertedRect(t *testing.T) {
	elements := []models.TextElement{
		{Text: "inside", X: 50, Y: 50},
	}
	// Inverted rect coordinates (maxX < minX, maxY < minY).
	annots := []models.Annotation{
		{Subtype: "Redact", Rect: [4]float64{100, 100, 10, 10}},
	}

	result := DetectUnappliedRedactions(elements, annots)

	if !result[0].Redacted {
		t.Error("inverted rect should still detect element inside")
	}
}

func TestDetectUnappliedRedactions_MultipleZones(t *testing.T) {
	elements := []models.TextElement{
		{Text: "zone1", X: 50, Y: 50},
		{Text: "zone2", X: 250, Y: 250},
		{Text: "none", X: 500, Y: 500},
	}
	annots := []models.Annotation{
		{Subtype: "Redact", Rect: [4]float64{10, 10, 100, 100}},
		{Subtype: "Redact", Rect: [4]float64{200, 200, 300, 300}},
	}

	result := DetectUnappliedRedactions(elements, annots)

	if !result[0].Redacted {
		t.Error("expected 'zone1' to be redacted")
	}
	if !result[1].Redacted {
		t.Error("expected 'zone2' to be redacted")
	}
	if result[2].Redacted {
		t.Error("expected 'none' to NOT be redacted")
	}
}

func TestDetectUnappliedRedactions_OriginalUnmodified(t *testing.T) {
	elements := []models.TextElement{
		{Text: "text", X: 50, Y: 50},
	}
	annots := []models.Annotation{
		{Subtype: "Redact", Rect: [4]float64{10, 10, 100, 100}},
	}

	result := DetectUnappliedRedactions(elements, annots)

	if elements[0].Redacted {
		t.Error("original element should not be modified")
	}
	if !result[0].Redacted {
		t.Error("result element should be redacted")
	}
}

func TestDetectUnappliedRedactions_RunStraddlesZoneEdge(t *testing.T) {
	// A coalesced run starts well left of the mark and continues under it.
	// Its origin is outside the zone, so an origin-only test cleared it.
	elements := []models.TextElement{
		{Text: "CONFIDENTIAL", X: 0, Y: 50, Width: 100, FontSize: 10},
	}
	annots := []models.Annotation{
		{Subtype: "Redact", Rect: [4]float64{60, 40, 200, 70}},
	}

	result := DetectUnappliedRedactions(elements, annots)

	if !result[0].Redacted {
		t.Error("a run extending under the mark must be reported, not just one starting under it")
	}
}

func TestDetectUnappliedRedactions_AscenderUnderZone(t *testing.T) {
	// The mark covers the glyph bodies but sits above the baseline, so the
	// origin point falls below it.
	elements := []models.TextElement{
		{Text: "secret", X: 10, Y: 0, Width: 40, FontSize: 12},
	}
	annots := []models.Annotation{
		{Subtype: "Redact", Rect: [4]float64{0, 5, 100, 20}},
	}

	result := DetectUnappliedRedactions(elements, annots)

	if !result[0].Redacted {
		t.Error("a mark covering glyph ink above the baseline must be reported")
	}
}

func TestDetectUnappliedRedactions_DescenderUnderZone(t *testing.T) {
	// The mark sits just below the baseline, over the descenders only.
	elements := []models.TextElement{
		{Text: "pgy", X: 10, Y: 20, Width: 30, FontSize: 10},
	}
	annots := []models.Annotation{
		{Subtype: "Redact", Rect: [4]float64{0, 14, 100, 19}},
	}

	result := DetectUnappliedRedactions(elements, annots)

	if !result[0].Redacted {
		t.Error("a mark covering descender ink must be reported")
	}
}

func TestDetectUnappliedRedactions_RotatedRun(t *testing.T) {
	// Rotated 90 degrees: the run advances up the Y axis from its origin.
	elements := []models.TextElement{
		{Text: "rotated", X: 50, Y: 0, Width: 100, FontSize: 10, Angle: math.Pi / 2},
	}
	annots := []models.Annotation{
		{Subtype: "Redact", Rect: [4]float64{45, 80, 48, 90}},
	}

	result := DetectUnappliedRedactions(elements, annots)

	if !result[0].Redacted {
		t.Error("a rotated run passing under the mark must be reported")
	}
}

func TestDetectUnappliedRedactions_VerticalRun(t *testing.T) {
	// Vertical writing mode advances along Y with glyphs centred on the
	// baseline, so the mark meets the run away from its origin.
	elements := []models.TextElement{
		{Text: "CJK", X: 50, Y: 0, Width: 40, FontSize: 10, Vertical: true},
	}
	annots := []models.Annotation{
		{Subtype: "Redact", Rect: [4]float64{46, 30, 54, 35}},
	}

	result := DetectUnappliedRedactions(elements, annots)

	if !result[0].Redacted {
		t.Error("a vertical run passing under the mark must be reported")
	}
}

func TestDetectUnappliedRedactions_DistantRunNotFlagged(t *testing.T) {
	// Widening the test to a box must not start flagging unrelated text.
	elements := []models.TextElement{
		{Text: "elsewhere", X: 300, Y: 300, Width: 50, FontSize: 10},
	}
	annots := []models.Annotation{
		{Subtype: "Redact", Rect: [4]float64{10, 10, 100, 100}},
	}

	result := DetectUnappliedRedactions(elements, annots)

	if result[0].Redacted {
		t.Error("text clear of every mark must not be reported")
	}
}

func TestDetectUnappliedRedactions_BoxContainsOrigin(t *testing.T) {
	// The box must always contain the element origin: that is what makes
	// widening the test unable to drop a detection the point test made.
	cases := []models.TextElement{
		{Text: "h", X: 10, Y: 20, Width: 30, FontSize: 12},
		{Text: "r", X: 10, Y: 20, Width: 30, FontSize: 12, Angle: math.Pi / 3},
		{Text: "v", X: 10, Y: 20, Width: 30, FontSize: 12, Vertical: true},
		{Text: "z", X: 10, Y: 20},
	}
	for _, el := range cases {
		z := ElementZone(el)
		if el.X < z.minX || el.X > z.maxX || el.Y < z.minY || el.Y > z.maxY {
			t.Errorf("origin (%g,%g) outside its own box %+v", el.X, el.Y, z)
		}
	}
}

func TestZoneContains(t *testing.T) {
	outer := ZoneFromRect([4]float64{10, 10, 100, 100})

	cases := []struct {
		name  string
		inner [4]float64
		want  bool
	}{
		{"wholly inside", [4]float64{20, 20, 50, 50}, true},
		{"identical, edges included", [4]float64{10, 10, 100, 100}, true},
		{"flush against one edge", [4]float64{10, 20, 50, 50}, true},
		{"reaches past the right", [4]float64{50, 20, 120, 50}, false},
		{"reaches below the bottom", [4]float64{20, 5, 50, 50}, false},
		{"encloses the outer box", [4]float64{0, 0, 200, 200}, false},
		{"disjoint", [4]float64{200, 200, 300, 300}, false},
	}
	for _, tc := range cases {
		if got := ZoneContains(outer, ZoneFromRect(tc.inner)); got != tc.want {
			t.Errorf("%s: ZoneContains = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestZoneContains_IsStricterThanOverlap(t *testing.T) {
	// The distinction removal depends on. A straddling mark overlaps the
	// region, which is what selects it, and is not contained by it, which is
	// what refuses it. If the two ever agreed, a path reaching outside the
	// region would be excised along with everything else it draws.
	region := ZoneFromRect([4]float64{10, 10, 100, 100})
	straddling := ZoneFromRect([4]float64{50, 50, 150, 150})

	if !ZonesOverlap(region, straddling) {
		t.Error("a straddling box must overlap: that is what selects it")
	}
	if ZoneContains(region, straddling) {
		t.Error("a straddling box must not be contained: that is what refuses it")
	}
}
