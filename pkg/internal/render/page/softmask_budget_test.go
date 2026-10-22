package page

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// overBudget is the render pixel budget of the tests below: a 100 x 100
// page fits it, a group over the whole page does not.
var overBudget = limits.Limits{MaxRenderPixels: 12_000}

// TestSoftMaskPastBudget checks that a soft mask of the graphics state whose
// group would pass the render pixel budget still masks (ISO 32000-1,
// 11.6.5.2), rendered coarser, where it painted the red square unmasked.
func TestSoftMaskPastBudget(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"Luminosity", "Alpha"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := fixture{
				page:    "/Resources << /ExtGState << /M << /SMask << /S /" + kind + " /G 5 0 R >> >> >> >>",
				content: "0 0 1 rg 0 0 100 100 re f /M gs 1 0 0 rg 0 0 100 100 re f",
				objs: []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /CS /DeviceGray >>",
					"1 g 0 0 50 100 re f")},
			}
			doc, pg := open(t, f.bytes(), overBudget)
			c, report, err := Render(context.Background(), doc, pg, f.options())
			if err != nil {
				t.Fatal(err)
			}
			if report.Unsupported[TransparencyOverBudget] != 1 {
				t.Errorf("report %v, want one soft mask over budget", report.Unsupported)
			}
			expect(t, c.Image(), 2, map[[2]int]rgb{{5, 50}: red, {40, 10}: red, {60, 50}: blue, {95, 90}: blue})
		})
	}
}

// TestSoftMaskPastReduction checks that a soft mask that does not fit the
// render pixel budget even at [maxMaskReduction] hides what it masks rather
// than showing it unmasked. Six nested knockout groups, which no renderer
// elides (11.4.6), hold the budget until a page-wide mask group has too few
// pixels left even at 1/16 of the resolution.
func TestSoftMaskPastReduction(t *testing.T) {
	t.Parallel()
	sizes := []int{560, 320, 180, 160, 126, 80}
	objs := make([]string, 0, len(sizes)+1)
	for i, size := range sizes {
		body := "/G Do"
		res := fmt.Sprintf("/Resources << /XObject << /G %d 0 R >> >>", 7+i)
		if i == len(sizes)-1 {
			body = "/M gs 1 0 0 rg 0 0 100 100 re f"
			res = "/Resources << /ExtGState << /M << /SMask << /S /Luminosity /G 5 0 R >> >> >> >>"
		}
		objs = append(objs, stream(fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [0 0 %d %d] /Group << /S /Transparency /I true /K true >> %s", size, size, res), body))
	}
	mask := stream("/Type /XObject /Subtype /Form /BBox [0 0 1000 1000] /Group << /S /Transparency /CS /DeviceGray >>", "1 g 0 0 1000 1000 re f")
	f := fixture{
		page:    "/MediaBox [0 0 1000 1000] /Resources << /XObject << /G 6 0 R >> >>",
		content: "0 0 1 rg 0 0 1000 1000 re f /G Do",
		objs:    append([]string{mask}, objs...),
	}
	doc, pg := open(t, f.bytes(), limits.Limits{MaxRenderPixels: 1_000_000})
	c, report, err := Render(context.Background(), doc, pg, f.options())
	if err != nil {
		t.Fatal(err)
	}
	if report.Unsupported[TransparencyOverBudget] != 1 {
		t.Errorf("report %v, want one soft mask over budget", report.Unsupported)
	}
	expect(t, c.Image(), 0, map[[2]int]rgb{{40, 960}: blue, {500, 500}: blue})
}

// TestGroupPastBudgetKeepsEffects checks that a transparency group past the
// render pixel budget, painted into the page without its group, still
// composites at the alpha and through the soft mask in force at its Do
// (ISO 32000-1, 11.6.6), where it painted its content opaque and unmasked.
func TestGroupPastBudgetKeepsEffects(t *testing.T) {
	t.Parallel()
	group := stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency >>", "1 0 0 rg 0 0 100 100 re f")
	for _, tc := range []struct {
		name, state string
		want        map[[2]int]rgb
	}{
		{"alpha", "/ca 0.5", map[[2]int]rgb{{50, 50}: {128, 0, 128}}},
		{"soft mask", "/SMask << /S /Alpha /G 6 0 R >>", map[[2]int]rgb{{20, 50}: red, {80, 50}: blue}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := fixture{
				page:    "/Resources << /XObject << /I 5 0 R >> /ExtGState << /A << " + tc.state + " >> >> >>",
				content: "0 0 1 rg 0 0 100 100 re f /A gs /I Do",
				objs: []string{group, stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency >>",
					"0 g 0 0 50 100 re f")},
			}
			doc, pg := open(t, f.bytes(), overBudget)
			c, report, err := Render(context.Background(), doc, pg, f.options())
			if err != nil {
				t.Fatal(err)
			}
			if report.Unsupported[TransparencyOverBudget] == 0 {
				t.Errorf("report %v, want the group over budget", report.Unsupported)
			}
			expect(t, c.Image(), 2, tc.want)
		})
	}
}
