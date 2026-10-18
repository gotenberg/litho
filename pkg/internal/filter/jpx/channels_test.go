package jpx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// annexJ10Samples are the samples of the J.10 codestream (J.10.5).
var annexJ10Samples = []int32{101, 103, 104, 105, 96, 97, 96, 102, 109}

// depth is the depth and signedness Render hands conv for a channel.
type depth struct {
	precision int
	signed    bool
}

// render renders chs with a conv that keeps the low byte of each value, and
// returns the bytes with the depth conv saw for each channel.
func render(t *testing.T, img *Image, chs []Channel) ([]byte, []depth, error) {
	t.Helper()
	var seen []depth
	out, err := img.Render(context.Background(), chs, func(v int32, precision int, signed bool) byte {
		if d := (depth{precision, signed}); !slices.Contains(seen, d) {
			seen = append(seen, d)
		}
		return byte(v)
	})
	return out, seen, err
}

// paletteFile wraps the J.10 codestream in a JP2 file with a palette of
// entries entries over three 8-bit columns, entry j being (j, 255 - j, j/2),
// and the extra header boxes given.
func paletteFile(entries int, extra ...[]byte) []byte {
	pclr := []byte{byte(entries >> 8), byte(entries), 3, 7, 7, 7}
	for j := range entries {
		pclr = append(pclr, byte(j), byte(255-j), byte(j/2))
	}
	header := slices.Concat(append([][]byte{imageHeaderBox, boxOf(BoxColour, []byte{methodEnumerated, 0, 0, 0, 0, 0, ColourSRGB}), boxOf(boxPalette, pclr)}, extra...)...)
	return slices.Concat(signatureBox, fileTypeBox, boxOf(BoxHeader, header), boxOf(boxCodestream, annexJ10))
}

// TestChannels_Palette maps the single J.10 component through the three
// palette columns (T.800, I.5.3.4 and I.5.3.5), with a Channel Definition box
// that gives the channels as blue, green and red (I.5.3.6).
func TestChannels_Palette(t *testing.T) {
	t.Parallel()

	cmap := boxOf(boxComponentMapping, []byte{0, 0, 1, 0, 0, 0, 1, 1, 0, 0, 1, 2})
	cdef := boxOf(boxChannelDefinition, []byte{0, 3, 0, 0, 0, 0, 0, 3, 0, 1, 0, 0, 0, 2, 0, 2, 0, 0, 0, 1})
	img, err := Decode(context.Background(), paletteFile(256, cmap, cdef), DefaultMaxSamples)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(img.Colours) != 1 || img.Colours[0].Enumerated != ColourSRGB || img.Colours[0].ICC != nil {
		t.Errorf("colours = %+v, want sRGB", img.Colours)
	}
	chs, err := img.Channels()
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	want := []Channel{
		{Component: 0, PaletteColumn: 0, Kind: ChannelColour, Association: 3},
		{Component: 0, PaletteColumn: 1, Kind: ChannelColour, Association: 2},
		{Component: 0, PaletteColumn: 2, Kind: ChannelColour, Association: 1},
	}
	if !slices.Equal(chs, want) {
		t.Fatalf("channels %+v, want %+v", chs, want)
	}
	got, depths, err := render(t, img, chs)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var entries []byte
	for _, v := range annexJ10Samples {
		entries = append(entries, byte(v), byte(255-v), byte(v/2))
	}
	if !slices.Equal(got, entries) || !slices.Equal(depths, []depth{{8, false}}) {
		t.Errorf("rendered %v at depths %v; want %v at 8 bits unsigned", got, depths, entries)
	}
}

// TestChannels_Direct gives a bare codestream, and a JP2 file whose palette
// has no Component Mapping box, one colour channel per component: I.5.3.5
// maps components straight to channels without a mapping.
func TestChannels_Direct(t *testing.T) {
	t.Parallel()

	for name, data := range map[string][]byte{"codestream": annexJ10, "palette without mapping": paletteFile(16)} {
		img, err := Decode(context.Background(), data, DefaultMaxSamples)
		if err != nil {
			t.Fatalf("%s: Decode: %v", name, err)
		}
		chs, err := img.Channels()
		if want := []Channel{{Component: 0, PaletteColumn: -1, Kind: ChannelColour, Association: 1}}; err != nil || !slices.Equal(chs, want) {
			t.Errorf("%s: channels %+v, %v; want %+v", name, chs, err, want)
			continue
		}
		want := make([]byte, len(annexJ10Samples))
		for i, v := range annexJ10Samples {
			want[i] = byte(v)
		}
		if got, _, err := render(t, img, chs); err != nil || !slices.Equal(got, want) {
			t.Errorf("%s: rendered %v, %v; want %v", name, got, err, want)
		}
	}
}

// TestChannels_DescribedTwice keeps the first description of a channel the
// Channel Definition box describes twice, as a real file does with an
// opacity channel it also calls premultiplied.
//
// # Corpus evidence
//
//   - All corpora: one file describes an opacity channel twice, the second
//     time as premultiplied.
func TestChannels_DescribedTwice(t *testing.T) {
	t.Parallel()

	cmap := boxOf(boxComponentMapping, []byte{0, 0, 1, 0, 0, 0, 0, 0})
	cdef := boxOf(boxChannelDefinition, []byte{0, 3, 0, 0, 0, 0, 0, 1, 0, 1, 0, 1, 0, 0, 0, 1, 0, 2, 0, 0})
	img, err := Decode(context.Background(), paletteFile(256, cmap, cdef), DefaultMaxSamples)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	chs, err := img.Channels()
	if err != nil || chs[1].Kind != ChannelOpacity {
		t.Errorf("channels %+v, %v; want channel 1 opacity", chs, err)
	}
}

// TestChannels_Opacity types a second channel as opacity for the image as a
// whole, and leaves a channel the definitions skip unspecified.
func TestChannels_Opacity(t *testing.T) {
	t.Parallel()

	cmap := boxOf(boxComponentMapping, []byte{0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1, 1})
	cdef := boxOf(boxChannelDefinition, []byte{0, 3, 0, 0, 0, 0, 0, 1, 0, 1, 0, 1, 0, 0, 0, 1, 0, 2, 0, 0})
	img, err := Decode(context.Background(), paletteFile(256, cmap, cdef), DefaultMaxSamples)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	chs, err := img.Channels()
	want := []Channel{
		{Component: 0, PaletteColumn: 0, Kind: ChannelColour, Association: 1},
		{Component: 0, PaletteColumn: -1, Kind: ChannelOpacity, Association: 0},
		{Component: 0, PaletteColumn: 1, Kind: ChannelUnspecified, Association: ChannelUnspecified},
	}
	if err != nil || !slices.Equal(chs, want) {
		t.Errorf("channels %+v, %v; want %+v", chs, err, want)
	}
}

// TestChannels_Rejects refuses channels that reach past the components,
// palette or channels the file holds, and a component value past the last
// palette entry.
func TestChannels_Rejects(t *testing.T) {
	t.Parallel()

	for name, data := range map[string][]byte{
		"mapping of a missing component": paletteFile(256, boxOf(boxComponentMapping, []byte{0, 1, 0, 0})),
		"mapping of a missing column":    paletteFile(256, boxOf(boxComponentMapping, []byte{0, 0, 1, 3})),
		"definition of a missing channel": paletteFile(256, boxOf(boxComponentMapping, []byte{0, 0, 1, 0}),
			boxOf(boxChannelDefinition, []byte{0, 1, 0, 1, 0, 0, 0, 1})),
	} {
		img, err := Decode(context.Background(), data, DefaultMaxSamples)
		if err != nil {
			t.Fatalf("%s: Decode: %v", name, err)
		}
		if _, err := img.Channels(); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: Channels = %v, want %v", name, err, ErrMalformed)
		}
	}
	img, err := Decode(context.Background(), paletteFile(100, boxOf(boxComponentMapping, []byte{0, 0, 1, 0})), DefaultMaxSamples)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	chs, err := img.Channels()
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if _, _, err := render(t, img, chs); !errors.Is(err, ErrMalformed) {
		t.Errorf("Render past the palette = %v, want %v", err, ErrMalformed)
	}
}

// TestRender_Subsampled places the 2x2-subsampled components of
// cprl_sub.j2k on the 24x20 default image grid (T.800, I.5.3.1.1): each 12x10
// sample covers a 2x2 cell, while component 0 fills the grid as it is.
func TestRender_Subsampled(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/cprl_sub.j2k")
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(context.Background(), data, DefaultMaxSamples)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := img.DefaultSize(); w != 24 || h != 20 {
		t.Fatalf("default size %dx%d, want 24x20", w, h)
	}
	chs := []Channel{{Component: 1, PaletteColumn: -1}, {Component: 0, PaletteColumn: -1}}
	got, _, err := render(t, img, chs)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c0, c1 := img.Components[0].Samples, img.Components[1].Samples
	for v := range 20 {
		for u := range 24 {
			p := got[(v*24+u)*2:]
			if want := []byte{byte(c1[(v/2)*12+u/2]), byte(c0[v*24+u])}; !slices.Equal(p[:2], want) {
				t.Fatalf("grid point (%d,%d) = %v, want %v", u, v, p[:2], want)
			}
		}
	}
}

// TestRender_Tiled renders the tiled codestreams, some offset and
// subsampled, tile by tile to what the whole component planes Decode returns
// give: each grid point takes its sample from the one tile that holds it, even
// where a subsampled component's tiles and the grid's cells do not line up.
func TestRender_Tiled(t *testing.T) {
	t.Parallel()

	names, err := filepath.Glob("testdata/*.j2k")
	if err != nil {
		t.Fatal(err)
	}
	subsampled := 0
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		img, err := Decode(context.Background(), data, DefaultMaxSamples)
		if err != nil {
			t.Fatalf("%s: Decode: %v", name, err)
		}
		if len(img.cs.tiles) < 2 {
			continue
		}
		var chs []Channel
		for c, comp := range img.Components {
			chs = append(chs, Channel{Component: c, PaletteColumn: -1})
			if comp.DX > 1 || comp.DY > 1 {
				subsampled++
			}
		}
		got, _, err := render(t, img, chs)
		if err != nil {
			t.Fatalf("%s: Render: %v", name, err)
		}
		m := img.gridStep()
		w, h := img.DefaultSize()
		gx0, gy0 := ceilDiv(img.x0, m), ceilDiv(img.y0, m)
		for i, ch := range chs {
			comp := img.Components[ch.Component]
			for v := range h {
				row := min(max((gy0+v)*m/comp.DY-comp.y0, 0), comp.Height-1)
				for u := range w {
					col := min(max((gx0+u)*m/comp.DX-comp.x0, 0), comp.Width-1)
					if want := byte(comp.Samples[row*comp.Width+col]); got[(v*w+u)*len(chs)+i] != want {
						t.Fatalf("%s: channel %d at (%d,%d) = %d, want %d", name, i, u, v, got[(v*w+u)*len(chs)+i], want)
					}
				}
			}
		}
	}
	if subsampled == 0 {
		t.Fatal("no tiled codestream has a subsampled component")
	}
}

// TestRender_GridLimit refuses a grid past DefaultMaxSamples before allocating it.
// Components 254 and 255 apart share no separation, so the default grid is
// the full image area while each component holds a few hundred samples a
// side: a few hundred bytes would otherwise ask for gigabytes. The 14200x14200
// grid fits under the limit alone but not across two channels.
func TestRender_GridLimit(t *testing.T) {
	t.Parallel()

	for _, size := range []uint32{60000, 14200} {
		b := sizBody(size, size, size, size, 2)
		b[37], b[38], b[40], b[41] = 254, 254, 255, 255
		img, err := Describe(codestreamOf(slices.Concat(seg(markerSIZ, b...), seg(markerCOD, codBody...), seg(markerQCD, qcdBody...)), tilePart(0, 0, nil, nil)), DefaultMaxSamples)
		if err != nil {
			t.Fatalf("%d: Describe: %v", size, err)
		}
		chs := []Channel{{Component: 0, PaletteColumn: -1}, {Component: 1, PaletteColumn: -1}}
		if _, _, err := render(t, img, chs); !errors.Is(err, ErrLimit) {
			t.Errorf("%d: Render = %v, want %v", size, err, ErrLimit)
		}
	}
}

// TestDefaultSize divides the image area by the common sample separation
// when every component is subsampled (T.800, I.5.3.1.1 NOTE c).
func TestDefaultSize(t *testing.T) {
	t.Parallel()

	b := sizBody(1024, 1024, 1024, 1024, 3)
	b[13], b[17] = 3, 2
	for c := range 3 {
		b[37+3*c], b[38+3*c] = 4, 4
	}
	cs, err := parseCodestream(codestreamOf(slices.Concat(seg(markerSIZ, b...), seg(markerCOD, codBody...), seg(markerQCD, qcdBody...)), tilePart(0, 0, nil, nil)))
	if err != nil {
		t.Fatal(err)
	}
	img, err := newImage(cs, DefaultMaxSamples)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := img.DefaultSize(); w != 255 || h != 255 {
		t.Errorf("default size %dx%d, want 255x255", w, h)
	}
}
