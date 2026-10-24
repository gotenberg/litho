package icc

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// transformGolden is littleCMS's reading of the conversions between the
// profiles the package ships and the synthetic ones of testdata/, recorded by
// testdata/gen_transform_golden.py through Pillow's ImageCms.
type transformGolden struct {
	Pillow    string           `json:"pillow"`
	LittleCMS string           `json:"littlecms"`
	Levels    map[string][]int `json:"levels"`
	Cases     []struct {
		Src     string  `json:"src"`
		Dst     string  `json:"dst"`
		Intent  int     `json:"intent"`
		BPC     bool    `json:"bpc"`
		Outputs [][]int `json:"outputs"`
	} `json:"cases"`
}

// goldenProfiles maps the golden's profile names to their data and the mode
// of their data colour space; an empty file is [LabProfile].
var goldenProfiles = map[string]struct{ file, mode string }{
	"srgb":   {"profiles/srgb.icc", "RGB"},
	"sgray":  {"profiles/sgray-v2.icc", "L"},
	"cgats":  {"profiles/cgats001-cmyk-v2.icc", "CMYK"},
	"micro":  {"profiles/CGATS001Compat-v2-micro.icc", "CMYK"},
	"v4cmyk": {"testdata/v4-cmyk-lut.icc", "CMYK"},
	"v4rgb":  {"testdata/v4-rgb-display.icc", "RGB"},
	"v4xyz":  {"testdata/v4-rgb-lut-xyz.icc", "RGB"},
	"lab":    {"", "LAB"},
}

// goldenGrid lists the inputs of a mode's grid, the first channel varying
// slowest, as itertools.product does.
func goldenGrid(levels []int, channels int) [][]int {
	out := [][]int{{}}
	for range channels {
		var next [][]int
		for _, p := range out {
			for _, l := range levels {
				next = append(next, append(append([]int(nil), p...), l))
			}
		}
		out = next
	}
	return out
}

func loadGoldenProfile(t *testing.T, name string) *Profile {
	t.Helper()
	g, ok := goldenProfiles[name]
	if !ok {
		t.Fatalf("golden names unknown profile %q", name)
	}
	if g.file == "" {
		return LabProfile()
	}
	data, err := os.ReadFile(filepath.FromSlash(g.file))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(data)
	if err != nil {
		t.Fatalf("%s: %v", g.file, err)
	}
	return p
}

// caseDiff is how far litho's conversions of one golden case land from
// littleCMS's, in 8-bit levels.
type caseDiff struct {
	max  int
	mean float64
}

func loadTransformGolden(t *testing.T) transformGolden {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "transform_golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g transformGolden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Cases) < 100 {
		t.Fatalf("golden holds %d cases, so it was not read", len(g.Cases))
	}
	return g
}

func goldenDiffs(t *testing.T) map[string]caseDiff {
	t.Helper()
	g := loadTransformGolden(t)
	channels := map[string]int{"L": 1, "RGB": 3, "LAB": 3, "CMYK": 4}
	diffs := make(map[string]caseDiff)
	for _, c := range g.Cases {
		src, dst := loadGoldenProfile(t, c.Src), loadGoldenProfile(t, c.Dst)
		l, err := NewLink(src, dst, TransformOptions{Intent: Intent(c.Intent), BlackPointCompensation: c.BPC})
		key := fmt.Sprintf("%s>%s/%s/bpc=%v", c.Src, c.Dst, Intent(c.Intent), c.BPC)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		mode := goldenProfiles[c.Src].mode
		inputs := goldenGrid(g.Levels[mode], channels[mode])
		if len(inputs) != len(c.Outputs) {
			t.Fatalf("%s: %d inputs for %d outputs", key, len(inputs), len(c.Outputs))
		}
		var d caseDiff
		var sum, count float64
		in, out := make([]float64, l.InputChannels()), make([]float64, l.OutputChannels())
		for i, px := range inputs {
			for k, v := range px {
				in[k] = fromPillow(mode, k, v)
			}
			l.Convert(in, out)
			for k, want := range c.Outputs[i] {
				e := abs(toPillow(goldenProfiles[c.Dst].mode, k, out[k]) - signedPillow(goldenProfiles[c.Dst].mode, k, want))
				d.max = max(d.max, e)
				sum += float64(e)
				count++
			}
		}
		d.mean = sum / count
		diffs[key] = d
	}
	return diffs
}

// Pillow's LAB mode stores a* and b* as signed bytes, where littleCMS's 8-bit
// Lab, which a [Link] reads normalized, offsets them by 128. fromPillow reads
// a stored byte as a normalized component, toPillow writes a normalized
// component as Pillow's value, and signedPillow reads a stored byte as that
// value.
func fromPillow(mode string, k, v int) float64 {
	return float64(signedPillow(mode, k, v)+pillowOffset(mode, k)) / 255
}

func toPillow(mode string, k int, v float64) int {
	return int(math.Round(v*255)) - pillowOffset(mode, k)
}

func signedPillow(mode string, k, v int) int {
	if pillowOffset(mode, k) != 0 {
		return int(int8(v)) //nolint:gosec // reinterprets the stored byte, as Pillow does.
	}
	return v
}

func pillowOffset(mode string, k int) int {
	if mode == "LAB" && k > 0 {
		return 128
	}
	return 0
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// goldenMaxLevels and goldenMeanLevels bound how far a [Link] may land from
// littleCMS on a golden case, in 8-bit levels: the golden is 8-bit, so a
// conversion that agrees to the rounding lands a level off where the two
// engines' float results straddle a half level. 131 of the 133 cases agree
// to 1 level, and the largest gap, 2 levels with a mean of 0.08, is the
// version 4 tone curve profile's perceptual connection to the CMYK printer
// profile under black point compensation.
const (
	goldenMaxLevels  = 2
	goldenMeanLevels = 0.1
)

// TestLinkMatchesLittleCMS converts the golden's grids through [NewLink] and
// compares each colour with littleCMS's: every pair of the shipped and
// synthetic profiles under every intent, with and without black point
// compensation.
func TestLinkMatchesLittleCMS(t *testing.T) {
	t.Parallel()

	for k, d := range goldenDiffs(t) {
		if d.max > goldenMaxLevels || d.mean > goldenMeanLevels {
			t.Errorf("%s: %d levels off at most, %.3f on average", k, d.max, d.mean)
		}
	}
}

// TestTransformMatchesLittleCMS converts the golden's grids into sRGB through
// [Profile.NewTransform] and compares each colour with littleCMS's reading
// under black point compensation, which the transform applies under every
// intent but absolute colorimetric: the grey, RGB and CMYK sources of the
// golden, under the perceptual, relative colorimetric and saturation intents.
func TestTransformMatchesLittleCMS(t *testing.T) {
	t.Parallel()

	g := loadTransformGolden(t)
	channels := map[string]int{"L": 1, "RGB": 3, "CMYK": 4}
	checked := 0
	for _, c := range g.Cases {
		mode := goldenProfiles[c.Src].mode
		if c.Dst != "srgb" || !c.BPC || Intent(c.Intent) == AbsoluteColorimetric || channels[mode] == 0 {
			continue
		}
		key := fmt.Sprintf("%s/%s", c.Src, Intent(c.Intent))
		tr, err := loadGoldenProfile(t, c.Src).NewTransform(Intent(c.Intent))
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		inputs := goldenGrid(g.Levels[mode], channels[mode])
		if len(inputs) != len(c.Outputs) {
			t.Fatalf("%s: %d inputs for %d outputs", key, len(inputs), len(c.Outputs))
		}
		var d caseDiff
		var sum float64
		in := make([]byte, channels[mode])
		for i, px := range inputs {
			for k, v := range px {
				in[k] = byte(v)
			}
			r, gr, b := tr.Convert(in)
			for k, got := range []byte{r, gr, b} {
				e := abs(int(got) - c.Outputs[i][k])
				d.max = max(d.max, e)
				sum += float64(e)
			}
		}
		d.mean = sum / float64(3*len(inputs))
		if d.max > goldenMaxLevels || d.mean > goldenMeanLevels {
			t.Errorf("%s: %d levels off at most, %.3f on average", key, d.max, d.mean)
		}
		checked++
	}
	if checked < 10 {
		t.Fatalf("checked %d cases, so the golden lost its sRGB destinations", checked)
	}
}
