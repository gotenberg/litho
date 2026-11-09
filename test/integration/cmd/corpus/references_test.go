package main

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/gotenberg/litho/test/integration/fixture"
)

// TestChromiumFieldsCoverTheOptions pins that the Chromium references post
// every key of an options.json as the route's field of the same name, but
// header and footer, which name the templates posted as files.
func TestChromiumFieldsCoverTheOptions(t *testing.T) {
	t.Parallel()

	got := []string{"header", "footer"}
	for _, d := range chromiumDefaults {
		got = append(got, d[0])
	}
	got = append(got, chromiumOptional...)
	want := slices.Clone(fixture.OptionNames)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("posted fields %v, options %v", got, want)
	}
}

// TestFormValue pins how an options.json value becomes a form field value:
// a string as itself, an object as compact JSON, anything else as its JSON.
func TestFormValue(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		`"1-3"`:                               "1-3",
		`8.5`:                                 "8.5",
		`true`:                                "true",
		`false`:                               "false",
		`{ "prefers-color-scheme" : "dark" }`: `{"prefers-color-scheme":"dark"}`,
	} {
		if got := formValue(json.RawMessage(in)); got != want {
			t.Errorf("formValue(%s) = %q, want %q", in, got, want)
		}
	}
}
