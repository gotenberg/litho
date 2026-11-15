package svg

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestStyle_PropertiesTheCascadeKnows locks that an HTML style sheet
// holding a property this package reads warns of nothing: the cascade of
// HTML elements either reads it too or lists it as an SVG property.
func TestStyle_PropertiesTheCascadeKnows(t *testing.T) {
	t.Parallel()
	names := []string{"marker", "mix-blend-mode", "isolation", "background-color"}
	for n := range presentationAttrs {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		sheet, err := css.Parse(context.Background(), []byte("rect { "+n+": inherit }"), css.ParseOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range sheet.Warnings() {
			if w.Code == warn.CSSUnsupported {
				t.Errorf("%s: %s", n, w.Message)
			}
		}
	}
}
