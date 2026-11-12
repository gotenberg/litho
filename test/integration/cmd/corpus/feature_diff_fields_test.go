package main

import (
	"encoding/json"
	"testing"
)

func TestFieldRecall(t *testing.T) {
	t.Parallel()
	ref := []diffWidget{
		{Kind: "text", Value: "Ada", Page: 1, Rect: [4]float64{100, 700, 200, 714}},
		{Kind: "checkbox", Value: "on", Page: 1, Rect: [4]float64{50, 650, 60, 660}},
		{Kind: "text", Value: "Two", Page: 2, Rect: [4]float64{100, 700, 200, 714}},
		{Kind: "list", Value: "One\nThree", Page: 2, Rect: [4]float64{100, 600, 200, 650}},
	}
	got := []diffWidget{
		// Two points beside the reference: in place.
		{Kind: "text", Value: "Ada", Page: 1, Rect: [4]float64{98, 698, 198, 712}},
		// Another kind.
		{Kind: "radio", Value: "on", Page: 1, Rect: [4]float64{50, 650, 60, 660}},
		// Another page.
		{Kind: "text", Value: "Two", Page: 1, Rect: [4]float64{100, 700, 200, 714}},
		{Kind: "list", Value: "One\nThree", Page: 2, Rect: [4]float64{101, 601, 199, 649}},
	}
	if r := fieldRecall(ref, got); r != 0.5 {
		t.Errorf("fieldRecall = %v, want 0.5", r)
	}
	if r := fieldRecall(nil, got); r != 1 {
		t.Errorf("fieldRecall of no reference widget = %v, want 1", r)
	}
	far := []diffWidget{{Kind: "text", Value: "Ada", Page: 1, Rect: [4]float64{300, 700, 400, 714}}}
	if r := fieldRecall(ref[:1], far); r != 0 {
		t.Errorf("fieldRecall of a widget out of place = %v, want 0", r)
	}
}

func TestQPDFWidgetValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		field qpdfWidget
		kind  string
		value string
	}{
		{qpdfWidget{FieldType: "/Tx", Value: json.RawMessage(`"u:Line one\r\nLine two "`)}, "text", "Line one\nLine two"},
		{qpdfWidget{FieldType: "/Btn", Value: json.RawMessage(`"/Yes"`)}, "checkbox", "on"},
		{qpdfWidget{FieldType: "/Btn", Value: json.RawMessage(`"/Off"`)}, "checkbox", "Off"},
		{qpdfWidget{FieldType: "/Btn", FieldFlags: fieldFlagRadio, Value: json.RawMessage(`"/blue"`)}, "radio", "Off"},
		{qpdfWidget{FieldType: "/Ch", FieldFlags: fieldFlagCombo, Value: json.RawMessage(`"u:Paris"`)}, "combo", "Paris"},
		{qpdfWidget{FieldType: "/Ch", Value: json.RawMessage(`["u:One","u:Three"]`)}, "list", "One\nThree"},
		{qpdfWidget{FieldType: "/Btn", FieldFlags: fieldFlagPushbutton}, "", ""},
	} {
		if k, v := tc.field.kind(), tc.field.value(); k != tc.kind || (k != "" && v != tc.value) {
			t.Errorf("%+v: kind %q value %q, want %q %q", tc.field, k, v, tc.kind, tc.value)
		}
	}
	on := qpdfWidget{FieldType: "/Btn", FieldFlags: fieldFlagRadio, Value: json.RawMessage(`"/blue"`)}
	on.Annotation.AppearanceState = "/blue"
	if v := on.value(); v != "on" {
		t.Errorf("the radio button that is on reads %q", v)
	}
}
