package recolor

import (
	"strings"
	"testing"
)

func TestDeviceOperatorsToCMYK(t *testing.T) {
	f := onePage("", "1 0 0 rg 0 0 10 10 re f 0.5 G 0 0 1 RG 0.2 0.3 0.4 0.1 k 0.25 g")
	r := recolor(t, f, cmyk, naive(Options{}))
	want := strings.Join([]string{
		"0 1 1 0 k",
		"0 0 10 10 re",
		"f",
		"0 0 0 0.5 K",
		"1 1 0 0 K",
		// DeviceCMYK is the target's own family: its values stay.
		"0.2 0.3 0.4 0.1 k",
		"0 0 0 0.75 k",
	}, "\n")
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
	if r.report.Operators != 4 || r.report.Streams != 1 {
		t.Errorf("report: %+v", r.report)
	}
}

func TestDeviceOperatorsToRGBAndGray(t *testing.T) {
	f := onePage("", "0 1 1 0 k 0.5 g 1 0 0 RG")
	r := recolor(t, f, rgbTarget, naive(Options{}))
	if got, want := r.text(0), "1 0 0 rg\n0.5 0.5 0.5 rg\n1 0 0 RG"; got != want {
		t.Errorf("RGB target:\n%s\nwant:\n%s", got, want)
	}
	r = recolor(t, f, grayTarget, naive(Options{}))
	if got, want := r.text(0), "0.299 g\n0.5 g\n0.299 G"; got != want {
		t.Errorf("gray target:\n%s\nwant:\n%s", got, want)
	}
}

func TestColorSpaceOperators(t *testing.T) {
	// cs sets the space's initial colour (ISO 32000-1, 8.6.8), so it is
	// written as the target's operator with that colour converted, and sc
	// after it as the target's operator with its colour converted.
	f := onePage("/Resources << /ColorSpace << /CS0 /DeviceRGB >> >>",
		"/CS0 cs 0 1 0 sc /DeviceRGB CS 0 0 1 SCN /DeviceGray cs 1 scn")
	r := recolor(t, f, cmyk, naive(Options{}))
	want := "0 0 0 1 k\n1 0 1 0 k\n0 0 0 1 K\n1 1 0 0 K\n0 0 0 1 k\n0 0 0 0 k"
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
}

func TestSaveRestoreKeepsColorSpaces(t *testing.T) {
	// q and Q save and restore the colour spaces (8.4.2): sc after Q is in
	// the space selected before q.
	f := onePage("", "/DeviceRGB cs q /DeviceCMYK cs 0 0 0 1 sc Q 1 0 0 sc")
	r := recolor(t, f, cmyk, naive(Options{}))
	want := "0 0 0 1 k\nq\n/DeviceCMYK cs\n0 0 0 1 sc\nQ\n0 1 1 0 k"
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
}

func TestMalformedOperandsStay(t *testing.T) {
	f := onePage("", "1 0 rg /X g 0 0 1 rg")
	r := recolor(t, f, cmyk, naive(Options{}))
	if got, want := r.text(0), "1 0 rg\n/X g\n1 1 0 0 k"; got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
}

func TestInlineImageConvertedWithTheContent(t *testing.T) {
	img := "BI /W 2 /H 1 /CS /RGB /BPC 8 ID \xff\x00\x00\x00\xff\x00 EI"
	f := onePage("", "1 0 0 rg q 2 0 0 1 0 0 cm "+img+" Q 0 1 0 rg")
	r := recolor(t, f, cmyk, naive(Options{}))
	stream := r.resolve(r.pages[0].Dict["Contents"])
	data := string(decode(t, r, stream))
	if strings.Contains(data, img) || !strings.Contains(data, "/CS /DeviceCMYK") {
		t.Errorf("inline image not written in the target:\n%q\n%v", data, r.report.Skipped)
	}
	if !strings.HasPrefix(data, " 0 1 1 0 k") || !strings.HasSuffix(data, "1 0 1 0 k") {
		t.Errorf("content: %q", data)
	}
	if r.report.Operators != 2 || r.report.Images.Converted != 1 {
		t.Errorf("report: %d operators, %d images converted, want 2 and 1", r.report.Operators, r.report.Images.Converted)
	}
}

func TestInlineImageInTargetKeptByteForByte(t *testing.T) {
	img := "BI /W 1 /H 1 /CS /CMYK /BPC 8 ID \x00\xff\xff\x00 EI"
	f := onePage("", "1 0 0 rg "+img)
	r := recolor(t, f, cmyk, naive(Options{}))
	data := string(decode(t, r, r.resolve(r.pages[0].Dict["Contents"])))
	if !strings.Contains(data, img) {
		t.Errorf("inline image in the target changed:\n%q", data)
	}
}

func TestNothingToConvertWritesNothing(t *testing.T) {
	f := onePage("", "0 0 0 1 k 0 0 10 10 re f")
	r := recolor(t, f, cmyk, naive(Options{}))
	if n := len(r.alloc.Pending()); n != 0 {
		t.Errorf("%d objects written for content already in the target", n)
	}
}

func TestICCBasedTarget(t *testing.T) {
	profile := builtinCMYK
	f := onePage("/Resources << /ColorSpace << /CS0 /DeviceRGB >> >>", "1 0 0 rg /CS0 CS 0 0 1 SC",
		stream("/N 4", string(profile)))
	target := Target{Family: "ICCBased", Profile: profile, ColorSpace: iccRef(5)}
	r := recolor(t, f, target, naive(Options{}))
	want := "/LithoTarget cs\n0 1 1 0 sc\n/LithoTarget CS\n0 0 0 1 SC\n/LithoTarget CS\n1 1 0 0 SC"
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
	spaces := r.dict(r.dict(r.pages[0].Dict["Resources"])["ColorSpace"])
	if got := spaces["LithoTarget"]; got == nil {
		t.Errorf("resources lack the target: %v", spaces)
	}
}
