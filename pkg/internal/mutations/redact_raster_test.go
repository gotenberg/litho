package mutations

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestRedactPageRegionsWith_RasterisesWhatItCannotRemove pins which units the
// fallback is asked to rasterise: the Do of a form whose glyph, path or image
// the region covers, and an sh whose clip meets it, and nothing the
// redaction removes on its own. A page the plain redaction clears does not
// reach the fallback.
func TestRedactPageRegionsWith_RasterisesWhatItCannotRemove(t *testing.T) {
	t.Parallel()
	errStop := errors.New("stop")
	cases := []struct {
		name, body, form string
		want             []int
	}{
		{"form text", "BT /F1 12 Tf 150 150 Td (OUT) Tj ET /Fm0 Do", "BT /F1 12 Tf 20 20 Td (IN) Tj ET", []int{5}},
		{"form path", "/Fm0 Do", "20 20 30 30 re f", []int{0}},
		{"form image", "/Fm0 Do", "q 40 0 0 40 20 20 cm /Im0 Do Q", []int{0}},
		{"shading", "10 10 20 20 re f q 0 0 200 200 re W n /Sh0 sh Q", "", []int{6}},
		{"form clear of the region", "/Fm0 Do q 0 0 50 50 re W n /Sh0 sh Q", "BT /F1 12 Tf 150 150 Td (OUT) Tj ET", []int{5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, page := rasterFixture(t, tc.body, tc.form)
			alloc := writer.NewObjAllocator(20)
			var got []int
			_, err := RedactPageRegionsWith(context.Background(), alloc, doc, page, [][4]float64{{0, 0, 100, 100}}, RedactOptions{
				Rasterise: func(_ context.Context, _ *writer.ObjAllocator, _ *document.Document, _ *document.Page, ops []int, _ [][4]float64) (RasterResult, error) {
					got = ops
					return RasterResult{}, errStop
				},
			})
			if !errors.Is(err, errStop) {
				t.Fatalf("err = %v, want the fallback reached", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("rasterised %v, want %v", got, tc.want)
			}
			if n := len(alloc.Pending()); n != 0 {
				t.Errorf("registered %d objects after a refusal", n)
			}
		})
	}

	doc, page := rasterFixture(t, "BT /F1 12 Tf 20 20 Td (IN) Tj ET /Fm0 Do", "BT /F1 12 Tf 150 150 Td (OUT) Tj ET")
	_, err := RedactPageRegionsWith(context.Background(), writer.NewObjAllocator(20), doc, page, [][4]float64{{0, 0, 100, 100}}, RedactOptions{
		Rasterise: func(context.Context, *writer.ObjAllocator, *document.Document, *document.Page, []int, [][4]float64) (RasterResult, error) {
			return RasterResult{}, errStop
		},
	})
	if err != nil {
		t.Errorf("a page the plain redaction clears: %v", err)
	}
}
