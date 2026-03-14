package heuristics

import (
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
