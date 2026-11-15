package ooxml

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func hexStop(pos float64, v uint32) RampStop {
	return RampStop{Pos: pos, Color: css.Color{R: float64(v>>16&0xff) / 255, G: float64(v>>8&0xff) / 255, B: float64(v&0xff) / 255, A: 1}}
}

// lerpAt returns the colour the straight lines between stops paint at p.
func lerpAt(stops []RampStop, p float64) css.Color {
	for i := 1; i < len(stops); i++ {
		a, b := stops[i-1], stops[i]
		if p <= b.Pos && b.Pos > a.Pos {
			f := (p - a.Pos) / (b.Pos - a.Pos)
			return css.Color{R: a.Color.R + (b.Color.R-a.Color.R)*f, G: a.Color.G + (b.Color.G-a.Color.G)*f, B: a.Color.B + (b.Color.B-a.Color.B)*f, A: a.Color.A + (b.Color.A-a.Color.A)*f}
		}
	}
	return stops[len(stops)-1].Color
}

func TestRamp_OfficeBlend(t *testing.T) {
	t.Parallel()
	in := []RampStop{hexStop(1, 0xFFC000), hexStop(0, 0x4472C4)}
	out := Ramp(in)
	if out[0].Pos != 0 || out[len(out)-1].Pos != 1 || out[0].Color != in[1].Color || out[len(out)-1].Color != in[0].Color {
		t.Fatalf("ends %v, %v", out[0], out[len(out)-1])
	}
	if n := len(out); n < 4 || n > 66 {
		t.Fatalf("%d stops", n)
	}
	for i := 1; i < len(out); i++ {
		if out[i].Pos <= out[i-1].Pos {
			t.Fatalf("stop %d at %v after %v", i, out[i].Pos, out[i-1].Pos)
		}
	}
	// PowerPoint paints the middle of this run 191,159,143 (litho/pptx
	// fill-gradient).
	mid := lerpAt(out, 0.5)
	got := [3]float64{mid.R * 255, mid.G * 255, mid.B * 255}
	for i, want := range [3]float64{191, 159, 143} {
		if math.Abs(got[i]-want) > 2.5 {
			t.Fatalf("middle %v, want 191,159,143", got)
		}
	}
	a, b := in[1], in[0]
	for k := 0; k <= 255; k++ {
		p := float64(k) / 255
		want, have := rampColor(a, b, p), lerpAt(out, p)
		for _, d := range [...]float64{want.R - have.R, want.G - have.G, want.B - have.B} {
			if math.Abs(d) > 0.5/255 {
				t.Fatalf("at %v: %v, Office %v", p, have, want)
			}
		}
	}
}

// probeBlackToWhite are the 256 samples PowerPoint writes for a linear
// gradient of 000000 at 0 to FFFFFF at 100% (probe, pptx).
var probeBlackToWhite = [256]float64{
	0, 11, 15, 18, 21, 23, 25, 26, 28, 30, 32, 34, 35, 36, 39, 40, 42, 43, 45,
	45, 47, 48, 50, 51, 53, 54, 55, 56, 58, 59, 60, 62, 63, 65, 66, 67, 69, 70,
	71, 72, 74, 75, 76, 78, 79, 80, 81, 83, 84, 86, 87, 88, 89, 91, 92, 93, 95,
	96, 97, 99, 100, 102, 103, 104, 106, 107, 108, 109, 111, 112, 113, 115, 116,
	117, 119, 120, 121, 123, 124, 125, 127, 128, 129, 131, 132, 133, 135, 136,
	138, 139, 140, 141, 143, 144, 145, 147, 148, 149, 150, 152, 153, 154, 156,
	157, 158, 159, 161, 162, 163, 164, 166, 167, 168, 169, 171, 172, 173, 174,
	175, 176, 177, 179, 180, 181, 182, 183, 184, 186, 187, 188, 189, 190, 191,
	192, 193, 194, 195, 196, 197, 198, 199, 200, 201, 202, 203, 204, 205, 206,
	207, 208, 209, 210, 211, 212, 212, 213, 214, 215, 216, 217, 217, 218, 219,
	220, 221, 221, 222, 223, 224, 224, 225, 226, 226, 227, 228, 228, 229, 230,
	230, 231, 232, 232, 233, 233, 234, 235, 235, 236, 236, 237, 237, 238, 238,
	239, 239, 240, 240, 241, 241, 241, 242, 242, 243, 243, 243, 244, 244, 245,
	245, 245, 246, 246, 246, 247, 247, 247, 248, 248, 248, 248, 249, 249, 249,
	250, 250, 250, 250, 251, 251, 251, 251, 251, 252, 252, 252, 252, 252, 252,
	253, 253, 253, 253, 253, 254, 254, 254, 254, 254, 254, 254, 254, 255, 255,
	255, 255, 255,
}

// TestRamp_PowerPointSamples checks the stops of a gradient of black to
// white against PowerPoint's own samples: within a level and a half of
// each, the blend on the power curve of 2.2 eased by the normal
// distribution of a quarter of the run.
func TestRamp_PowerPointSamples(t *testing.T) {
	t.Parallel()
	out := Ramp([]RampStop{hexStop(0, 0x000000), hexStop(1, 0xFFFFFF)})
	for k, want := range probeBlackToWhite {
		have := lerpAt(out, float64(k)/255).R * 255
		if math.Abs(have-want) > 1.5 {
			t.Errorf("sample %d: %.1f, PowerPoint %v", k, have, want)
		}
	}
}

func TestRamp_KeepsWhatNeedsNoStops(t *testing.T) {
	t.Parallel()
	same := []RampStop{hexStop(0, 0x336699), {Pos: 1, Color: css.Color{R: 0x33 / 255.0, G: 0x66 / 255.0, B: 0x99 / 255.0, A: 0}}}
	if out := Ramp(same); len(out) != 2 {
		t.Errorf("one colour: %d stops", len(out))
	}
	edge := []RampStop{hexStop(0, 0x000000), hexStop(0.5, 0x000000), hexStop(0.5, 0xFFFFFF), hexStop(1, 0xFFFFFF)}
	if out := Ramp(edge); len(out) != 4 {
		t.Errorf("sharp edge: %d stops", len(out))
	}
	if out := Ramp([]RampStop{hexStop(2, 0xFF0000)}); len(out) != 1 || out[0].Pos != 1 {
		t.Errorf("one stop: %v", out)
	}
}

func TestRamp_KeepsOtherGradientsStraight(t *testing.T) {
	t.Parallel()
	in := []RampStop{hexStop(1, 0x000000), hexStop(0, 0xFFFFFF), hexStop(0.5, 0xFF0000)}
	out := Ramp(in)
	if len(out) != 3 || out[0].Pos != 0 || out[1].Pos != 0.5 || out[2].Pos != 1 {
		t.Fatalf("stops %v", out)
	}
	if out := Ramp([]RampStop{hexStop(0.2, 0xFFFF00), hexStop(1, 0x4169E1)}); len(out) != 2 {
		t.Fatalf("stops at 20%% and 100%%: %v", out)
	}
	clear := hexStop(1, 0x7D2AE7)
	clear.Color.A = 0.75
	if out := Ramp([]RampStop{hexStop(0, 0x01C3CC), clear}); len(out) != 2 {
		t.Fatalf("a translucent stop: %v", out)
	}
}
