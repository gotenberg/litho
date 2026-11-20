package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

// assertRoundTrip marshals in to JSON, unmarshals the result into a fresh
// instance of T, and asserts the result is [reflect.DeepEqual] to the input.
// The input must populate every exported, non-function field with a
// non-zero sentinel so a stray `omitempty` would be caught.
func assertRoundTrip[T any](t *testing.T, in T) {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip mismatch:\n want: %+v\n  got: %+v\n JSON: %s", in, out, data)
	}
}

// assertRoundTripExceptOpen is the variant of [assertRoundTrip] for types
// that carry an unexportable lazy-content closure. It strips the closure
// from a clone of the input before comparison.
func assertRoundTripExceptOpen[T any](t *testing.T, in T, zeroOpen func(*T)) {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	expected := in
	zeroOpen(&expected)
	if !reflect.DeepEqual(expected, out) {
		t.Errorf("round trip mismatch (Open excluded):\n want: %+v\n  got: %+v\n JSON: %s",
			expected, out, data)
	}
}
