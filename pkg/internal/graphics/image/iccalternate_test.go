package image

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestDecodeImage_ICCBasedAlternate pins ISO 32000-1, 8.6.5.5, Table 66: an
// ICCBased space whose profile litho cannot use reads as its /Alternate, the
// samples taken as they are, and as the device space of /N only when
// /Alternate is absent or unfit. Each case decodes the same samples under the
// space and under the space it should read as, and compares the pixels.
func TestDecodeImage_ICCBasedAlternate(t *testing.T) {
	t.Parallel()

	const lab = "[/Lab << /WhitePoint [0.9642 1 0.8249] /Range [-128 127 -128 127] >>]"
	const tint = "<< /FunctionType 2 /Domain [0 1] /Range [0 1 0 1 0 1] /C0 [1 1 1] /C1 [1 0 0] /N 1 >>"
	unusable := func(n int, alternate string) string {
		return fmt.Sprintf("<< /N %d /Alternate %s /Length 13 >>\nstream\nnot a profile\nendstream", n, alternate)
	}
	for _, tc := range []struct {
		name    string
		cs      string
		readsAs string
		samples []byte
		extra   []string
	}{
		{"a Lab alternate", "[/ICCBased 5 0 R]", lab, []byte{128, 200, 60},
			[]string{unusable(3, lab)}},
		{"a CalGray alternate", "[/ICCBased 5 0 R]", "[/CalGray << /WhitePoint [0.9505 1 1.089] /Gamma 2.2 >>]", []byte{128},
			[]string{unusable(1, "[/CalGray << /WhitePoint [0.9505 1 1.089] /Gamma 2.2 >>]")}},
		{"a Separation alternate", "[/ICCBased 5 0 R]", "[/Separation /Spot /DeviceRGB 6 0 R]", []byte{128},
			[]string{unusable(1, "[/Separation /Spot /DeviceRGB 6 0 R]"), tint}},
		{"an alternate through another ICCBased space", "[/ICCBased 5 0 R]", lab, []byte{128, 200, 60},
			[]string{unusable(3, "[/ICCBased 6 0 R]"), unusable(3, lab)}},
		{"an Indexed base", "[/Indexed [/ICCBased 5 0 R] 0 <80C83C>]", "[/Indexed " + lab + " 0 <80C83C>]", []byte{0},
			[]string{unusable(3, lab)}},
		{"a Separation's alternate", "[/Separation /Spot [/ICCBased 5 0 R] 6 0 R]", "[/Separation /Spot " + lab + " 6 0 R]", []byte{128},
			[]string{unusable(3, lab), "<< /FunctionType 2 /Domain [0 1] /Range [0 100 -128 127 -128 127] /C0 [100 0 0] /C1 [30 60 -40] /N 1 >>"}},
		{"a usable profile wins over its alternate", "[/ICCBased 5 0 R]", "[/ICCBased 6 0 R]", []byte{255, 0, 0},
			[]string{
				fmt.Sprintf("<< /N 3 /Alternate %s /Length %d >>\nstream\n%s\nendstream", lab, len(flatProfile("RGB ", 25)), flatProfile("RGB ", 25)),
				profileObj(3, flatProfile("RGB ", 25)),
			}},
		{"an alternate of other components reads as the device space", "[/ICCBased 5 0 R]", "/DeviceCMYK", []byte{255, 0, 0, 0},
			[]string{unusable(4, "/DeviceRGB")}},
		{"a Pattern alternate reads as the device space", "[/ICCBased 5 0 R]", "/DeviceRGB", []byte{10, 20, 30},
			[]string{unusable(3, "[/Pattern /DeviceRGB]")}},
		{"an alternate naming its own space ends at the device space", "[/ICCBased 5 0 R]", "/DeviceRGB", []byte{10, 20, 30},
			[]string{unusable(3, "[/ICCBased 5 0 R]")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, want := firstPixel(t, tc.cs, tc.samples, tc.extra), firstPixel(t, tc.readsAs, tc.samples, tc.extra); got != want {
				t.Errorf("%s decodes %v to %v, %s decodes them to %v", tc.cs, tc.samples, got, tc.readsAs, want)
			}
		})
	}
}

// TestWithICCAlternates_Positions pins that an /Alternate replaces an
// ICCBased space only where its family may stand: not an Indexed space as the
// base of another (ISO 32000-1, 8.6.6.3), nor a special space as the
// alternate of a Separation or DeviceN space (8.6.6.4, 8.6.6.5). The space is
// then left to the device space of /N.
func TestWithICCAlternates_Positions(t *testing.T) {
	t.Parallel()

	unusable := func(alternate string) string {
		return fmt.Sprintf("<< /N 1 /Alternate %s /Length 13 >>\nstream\nnot a profile\nendstream", alternate)
	}
	const tint = "<< /FunctionType 2 /Domain [0 1] /Range [0 1] /C0 [1] /C1 [0] /N 1 >>"
	for _, tc := range []struct {
		name      string
		cs        string
		alternate string
		replaced  bool
	}{
		{"an Indexed alternate as the space", "[/ICCBased 5 0 R]", "[/Indexed /DeviceGray 0 <00>]", true},
		{"an Indexed alternate as an Indexed base", "[/Indexed [/ICCBased 5 0 R] 0 <00>]", "[/Indexed /DeviceGray 0 <00>]", false},
		{"a CalGray alternate as an Indexed base", "[/Indexed [/ICCBased 5 0 R] 0 <00>]", "[/CalGray << /WhitePoint [1 1 1] >>]", true},
		{"a Separation alternate as a Separation's alternate", "[/Separation /Spot [/ICCBased 5 0 R] 6 0 R]", "[/Separation /Ink /DeviceGray 6 0 R]", false},
		{"a Separation alternate as an Indexed base", "[/Indexed [/ICCBased 5 0 R] 0 <00>]", "[/Separation /Ink /DeviceGray 6 0 R]", true},
		{"a CalGray alternate as a DeviceN's alternate", "[/DeviceN [/Spot] [/ICCBased 5 0 R] 6 0 R]", "[/CalGray << /WhitePoint [1 1 1] >>]", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, doc, _ := managedPDF(t, "", tc.cs, "", 1, []byte{0}, unusable(tc.alternate), tint)
			im, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
			if err != nil {
				t.Fatal(err)
			}
			out := withICCAlternates(ctx, doc, im.Dict)
			if replaced := fmt.Sprint(out["ColorSpace"]) != fmt.Sprint(im.Dict["ColorSpace"]); replaced != tc.replaced {
				t.Errorf("%s with alternate %s: replaced = %v, want %v (got %v)", tc.cs, tc.alternate, replaced, tc.replaced, out["ColorSpace"])
			}
		})
	}
}

// firstPixel decodes the one-pixel image of samples in the colour space cs,
// with extra objects numbered from 5.
func firstPixel(t *testing.T, cs string, samples []byte, extra []string) [3]int {
	t.Helper()
	ctx, doc, page := managedPDF(t, "", cs, "", 1, samples, extra...)
	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage(%s): %v", cs, err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	return [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
}
