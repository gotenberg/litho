package page

import (
	"context"
	"image"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// drawWith renders f at 72 dpi over white, simulating overprint or not.
func drawWith(t *testing.T, f fixture, overprint bool) *image.NRGBA {
	t.Helper()
	doc, pg := open(t, f.bytes(), limits.Limits{})
	opts := f.options()
	opts.Overprint = overprint
	c, _, err := Render(context.Background(), doc, pg, opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return c.Image()
}

// overprintFixture paints cyan over the page, then content under a graphics
// state that overprints with mode 1.
func overprintFixture(content string) fixture {
	return fixture{
		page:    "/Resources << /ExtGState << /OP 5 0 R >> /ColorSpace << /Ma 6 0 R /Sp 7 0 R >> >>",
		content: "1 0 0 0 k 0 0 100 100 re f /OP gs " + content,
		objs: []string{
			"<< /Type /ExtGState /OP true /op true /OPM 1 >>",
			"[/Separation /Magenta /DeviceCMYK << /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 1 0 0] /N 1 >>]",
			"[/Separation /Gold /DeviceCMYK << /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 0.2 1 0] /N 1 >>]",
		},
	}
}

func TestOverprintSimulation(t *testing.T) {
	// ISO 32000-1, 8.6.7: under overprint mode 1 the zero components of a
	// DeviceCMYK colour leave the cyan below as it was, so magenta over cyan
	// prints as cyan and magenta together; a Separation of the process
	// colorant Magenta paints magenta alone. A spot colorant paints a plane
	// of its own, which prints its alternate over the cyan it leaves.
	reference := drawWith(t, fixture{content: "1 1 0 0 k 0 0 100 100 re f"}, true)
	both := at(t, reference, 50, 50)
	cyanGold := at(t, drawWith(t, fixture{content: "1 0.2 1 0 k 0 0 100 100 re f"}, true), 50, 50)

	for _, tc := range []struct {
		name, content string
		want          rgb
	}{
		{"DeviceCMYK", "0 1 0 0 k 0 0 100 100 re f", both},
		{"process Separation", "/Ma cs 1 scn 0 0 100 100 re f", both},
		{"spot Separation", "/Sp cs 1 scn 0 0 100 100 re f", cyanGold},
	} {
		img := drawWith(t, overprintFixture(tc.content), true)
		if got := at(t, img, 50, 50); !close8(got, tc.want, 2) {
			t.Errorf("%s: simulated %v, want %v", tc.name, got, tc.want)
		}
	}
	// Without the simulation magenta knocks out the cyan, as viewers draw.
	if got := at(t, drawWith(t, overprintFixture("0 1 0 0 k 0 0 100 100 re f"), false), 50, 50); close8(got, both, 8) {
		t.Errorf("unsimulated: %v, want magenta, not cyan and magenta", got)
	}
}

func TestOverprintSimulationModeZeroAndStroke(t *testing.T) {
	// Mode 0 paints every component of a DeviceCMYK colour; /OP false
	// leaves stroking knocking out while filling overprints.
	magenta := at(t, drawWith(t, fixture{content: "0 1 0 0 k 0 0 100 100 re f"}, true), 50, 50)
	f := fixture{
		page:    "/Resources << /ExtGState << /M0 5 0 R /F 6 0 R >> >>",
		content: "1 0 0 0 k 0 0 100 100 re f q /M0 gs 0 1 0 0 k 0 0 50 100 re f Q /F gs 0 1 0 0 K 20 w 75 0 m 75 100 l S",
		objs: []string{
			"<< /OP true /OPM 0 >>",
			"<< /OP false /op true /OPM 1 >>",
		},
	}
	img := drawWith(t, f, true)
	for _, p := range [][2]int{{25, 50}, {75, 50}} {
		if got := at(t, img, p[0], p[1]); !close8(got, magenta, 2) {
			t.Errorf("pixel %v: %v, want magenta %v", p, got, magenta)
		}
	}
}

// spotFixture paints cyan over the page, then content with the graphics
// states /OP, overprinting with mode 1, and /KO, knocking out, and the spot
// colours /Go (gold), /Bl (blue) and /S1 to /S4.
func spotFixture(content string) fixture {
	sep := func(name, c1 string) string {
		return "[/Separation /" + name + " /DeviceCMYK << /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [" + c1 + "] /N 1 >>]"
	}
	return fixture{
		page:    "/Resources << /ExtGState << /OP 5 0 R /KO 6 0 R >> /ColorSpace << /Go 7 0 R /Bl 8 0 R /S1 9 0 R /S2 10 0 R /S3 11 0 R /S4 12 0 R >> >>",
		content: "1 0 0 0 k 0 0 100 100 re f " + content,
		objs: []string{
			"<< /OP true /op true /OPM 1 >>",
			"<< /OP false /op false >>",
			sep("Gold", "0 0.2 1 0"),
			sep("Blue", "1 0.5 0 0"),
			sep("S1", "0 0 0 0.1"),
			sep("S2", "0 0 0 0.1"),
			sep("S3", "0 0 0 0.1"),
			sep("S4", "0 0 0 0.1"),
		},
	}
}

func TestOverprintSimulationSpotPlanes(t *testing.T) {
	// ISO 32000-1, 8.6.7: a painting under overprint leaves the colorants
	// it does not name, spot colorants among them; a painting that knocks
	// out paints them with no ink. Each spot plane prints through its
	// alternate colour once the page is drawn (8.6.6.4).
	cmyk := func(c string) rgb {
		return at(t, drawWith(t, fixture{content: c + " k 0 0 100 100 re f"}, true), 50, 50)
	}
	for _, tc := range []struct {
		name, content string
		want          rgb
	}{
		{"spot over spot", "/OP gs /Go cs 1 scn 0 0 100 100 re f /Bl cs 1 scn 0 0 100 100 re f", cmyk("1 0.7 1 0")},
		{"spot knocking out spots", "/OP gs /Go cs 1 scn 0 0 100 100 re f /KO gs /Bl cs 1 scn 0 0 100 100 re f", cmyk("1 0.5 0 0")},
		{"DeviceCMYK over a spot", "/OP gs /Go cs 1 scn 0 0 100 100 re f 0 1 0 0 k 0 0 100 100 re f", cmyk("1 1 1 0")},
		{"DeviceCMYK knocking out a spot", "/OP gs /Go cs 1 scn 0 0 100 100 re f /KO gs 0 1 0 0 k 0 0 100 100 re f", cmyk("0 1 0 0")},
		{"spot past the cap", "/OP gs /S1 cs 1 scn /S2 cs 1 scn /S3 cs 1 scn /S4 cs 1 scn 0 0 1 1 re f " +
			"/S1 cs 1 scn 0 0 1 1 re f /S2 cs 1 scn 0 0 1 1 re f /S3 cs 1 scn 0 0 1 1 re f /S4 cs 1 scn 0 0 1 1 re f /Go cs 1 scn 0 0 100 100 re f", cmyk("0 0.2 1 0")},
	} {
		img := drawWith(t, spotFixture(tc.content), true)
		if got := at(t, img, 50, 50); !close8(got, tc.want, 2) {
			t.Errorf("%s: simulated %v, want %v", tc.name, got, tc.want)
		}
	}
}
