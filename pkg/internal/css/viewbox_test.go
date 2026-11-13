package css

import "testing"

// TestObjectViewBox_Parse holds object-view-box to CSS Images 5, 4.1:
// none or a <basic-shape-rect>, which computes to an inset.
func TestObjectViewBox_Parse(t *testing.T) {
	t.Parallel()
	for decl, want := range map[string]bool{
		"object-view-box: none": false, "object-view-box: inset(10px)": true,
		"object-view-box: xywh(0 0 50% 50%)": true, "object-view-box: rect(0 10px 10px 0)": true,
		"object-view-box: circle(10px)": false,
	} {
		if got := styleOf(t, decl).ObjectViewBox; (got != nil) != want || got != nil && got.Kind != ShapeInset {
			t.Errorf("%s = %+v, want set %v", decl, got, want)
		}
	}
}
