package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTextElement_JSONRoundTrip(t *testing.T) {
	// SpaceWidth is intentionally zero here: it has json:"-" and is covered
	// by TestTextElement_SpaceWidthNotSerialized.
	assertRoundTrip(t, TextElement{
		Text:          "Hello",
		X:             100,
		Y:             200,
		Width:         50.5,
		FontSize:      12,
		MCID:          3,
		Hidden:        true,
		Invisible:     true,
		Vertical:      true,
		Angle:         45.5,
		Redacted:      true,
		IsClipped:     true,
		PatternType:   "tiling",
		Language:      "en",
		ExpansionText: "hello world",
	})
}

func TestTextElement_SpaceWidthNotSerialized(t *testing.T) {
	in := TextElement{
		Text:       "Hi",
		FontSize:   12,
		SpaceWidth: 42.5,
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(strings.ToLower(string(data)), "spacewidth") {
		t.Errorf("SpaceWidth leaked into JSON: %s", data)
	}
	var out TextElement
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SpaceWidth != 0 {
		t.Errorf("SpaceWidth: got %v, want 0 (not serialized)", out.SpaceWidth)
	}
	if out.Text != "Hi" || out.FontSize != 12 {
		t.Errorf("other fields lost: %+v", out)
	}
}

func TestLine_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Line{X1: 10, Y1: 20, X2: 30, Y2: 40})
}
