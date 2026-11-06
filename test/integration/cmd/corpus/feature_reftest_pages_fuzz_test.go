package main

import (
	"image"
	"testing"
)

// FuzzParseReftestPages checks that no reftest-pages value makes the parser
// panic, and that an accepted one selects pages within the drawing.
func FuzzParseReftestPages(f *testing.F) {
	f.Add("1, 3-5, 7-")
	f.Add("2-")
	f.Fuzz(func(t *testing.T, spec string) {
		ranges, err := parseReftestPages(spec)
		if err != nil {
			return
		}
		pages := make([]image.Image, 3)
		if got := selectReftestPages(pages, ranges); len(got) > len(pages) {
			t.Errorf("%q selects %d of %d pages", spec, len(got), len(pages))
		}
	})
}
