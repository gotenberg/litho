package jpx

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
)

// annexJ10Samples are the samples of the J.10 codestream (J.10.5).
var annexJ10Samples = []int32{101, 103, 104, 105, 96, 97, 96, 102, 109}

// paletteFile wraps the J.10 codestream in a JP2 file with a palette of
// entries entries over three 8-bit columns, entry j being (j, 255 - j, j/2),
// and the extra header boxes given.
func paletteFile(entries int, extra ...[]byte) []byte {
	pclr := []byte{byte(entries >> 8), byte(entries), 3, 7, 7, 7}
	for j := range entries {
		pclr = append(pclr, byte(j), byte(255-j), byte(j/2))
	}
	header := slices.Concat(append([][]byte{imageHeaderBox, boxOf(boxColour, []byte{methodEnumerated, 0, 0, 0, 0, 0, ColourSRGB}), boxOf(boxPalette, pclr)}, extra...)...)
	return slices.Concat(signatureBox, fileTypeBox, boxOf(boxHeader, header), boxOf(boxCodestream, annexJ10))
}

// TestChannels_Palette maps the single J.10 component through the three
// palette columns (T.800, I.5.3.4 and I.5.3.5), with a Channel Definition box
// that gives the channels as blue, green and red (I.5.3.6).
func TestChannels_Palette(t *testing.T) {
	t.Parallel()

	cmap := boxOf(boxComponentMapping, []byte{0, 0, 1, 0, 0, 0, 1, 1, 0, 0, 1, 2})
	cdef := boxOf(boxChannelDefinition, []byte{0, 3, 0, 0, 0, 0, 0, 3, 0, 1, 0, 0, 0, 2, 0, 2, 0, 0, 0, 1})
	img, err := Decode(context.Background(), paletteFile(256, cmap, cdef))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if img.Colour.Enumerated != ColourSRGB || img.Colour.ICC != nil {
		t.Errorf("colour = %+v, want sRGB", img.Colour)
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
	for i, ch := range chs {
		got, prec, signed, err := img.ChannelSamples(ch)
		if err != nil {
			t.Fatalf("ChannelSamples(%d): %v", i, err)
		}
		var exp []int32
		for _, v := range annexJ10Samples {
			exp = append(exp, [3]int32{v, 255 - v, v / 2}[i])
		}
		if !slices.Equal(got, exp) || prec != 8 || signed {
			t.Errorf("channel %d: %v, %d bits, signed %v; want %v, 8 bits, unsigned", i, got, prec, signed, exp)
		}
	}
}

// TestChannels_Direct gives a bare codestream, and a JP2 file whose palette
// has no Component Mapping box, one colour channel per component: I.5.3.5
// maps components straight to channels without a mapping.
func TestChannels_Direct(t *testing.T) {
	t.Parallel()

	for name, data := range map[string][]byte{"codestream": annexJ10, "palette without mapping": paletteFile(16)} {
		img, err := Decode(context.Background(), data)
		if err != nil {
			t.Fatalf("%s: Decode: %v", name, err)
		}
		chs, err := img.Channels()
		if want := []Channel{{Component: 0, PaletteColumn: -1, Kind: ChannelColour, Association: 1}}; err != nil || !slices.Equal(chs, want) {
			t.Errorf("%s: channels %+v, %v; want %+v", name, chs, err, want)
			continue
		}
		if got, _, _, err := img.ChannelSamples(chs[0]); err != nil || !slices.Equal(got, annexJ10Samples) {
			t.Errorf("%s: samples %v, %v; want %v", name, got, err, annexJ10Samples)
		}
	}
}

// TestChannels_DescribedTwice keeps the first description of a channel the
// Channel Definition box describes twice, as one corpus file does with an
// opacity channel it also calls premultiplied.
func TestChannels_DescribedTwice(t *testing.T) {
	t.Parallel()

	cmap := boxOf(boxComponentMapping, []byte{0, 0, 1, 0, 0, 0, 0, 0})
	cdef := boxOf(boxChannelDefinition, []byte{0, 3, 0, 0, 0, 0, 0, 1, 0, 1, 0, 1, 0, 0, 0, 1, 0, 2, 0, 0})
	img, err := Decode(context.Background(), paletteFile(256, cmap, cdef))
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
	img, err := Decode(context.Background(), paletteFile(256, cmap, cdef))
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
		img, err := Decode(context.Background(), data)
		if err != nil {
			t.Fatalf("%s: Decode: %v", name, err)
		}
		if _, err := img.Channels(); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: Channels = %v, want %v", name, err, ErrMalformed)
		}
	}
	img, err := Decode(context.Background(), paletteFile(100, boxOf(boxComponentMapping, []byte{0, 0, 1, 0})))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	chs, err := img.Channels()
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if _, _, _, err := img.ChannelSamples(chs[0]); !errors.Is(err, ErrMalformed) {
		t.Errorf("ChannelSamples past the palette = %v, want %v", err, ErrMalformed)
	}
}

// TestResample places the 2x2-subsampled components of cprl_sub.j2k on the
// 24x20 default image grid (T.800, I.5.3.1.1): each 12x10 sample covers a
// 2x2 cell.
func TestResample(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/cprl_sub.j2k")
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := img.DefaultSize(); w != 24 || h != 20 {
		t.Fatalf("default size %dx%d, want 24x20", w, h)
	}
	c1 := img.Components[1]
	got := img.Resample(c1.Samples, 1)
	for v := range 20 {
		for u := range 24 {
			if got[v*24+u] != c1.Samples[(v/2)*12+u/2] {
				t.Fatalf("grid point (%d,%d) = %d, want sample (%d,%d) = %d", u, v, got[v*24+u], u/2, v/2, c1.Samples[(v/2)*12+u/2])
			}
		}
	}
	if full := img.Resample(img.Components[0].Samples, 0); &full[0] != &img.Components[0].Samples[0] {
		t.Errorf("a component sampled at every grid point was copied")
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
	img, err := newImage(cs)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := img.DefaultSize(); w != 255 || h != 255 {
		t.Errorf("default size %dx%d, want 255x255", w, h)
	}
}
