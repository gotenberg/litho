package models

import "testing"

func TestFormField_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, FormField{Name: "fullname", Value: "Alice"})
}
