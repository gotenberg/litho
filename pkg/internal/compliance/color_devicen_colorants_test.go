package compliance

import (
	"fmt"
	"testing"
)

// tintTransformObj is a well-formed Type 2 tint transform, object 6 in the
// fixtures below. Its shape is irrelevant to §6.2.4.4 t1; it exists so the
// DeviceN arrays are structurally valid.
const tintTransformObj = "<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >>"

// deviceNRGB is a DeviceN naming the additive primaries with no /Attributes at
// all.
//
// # Corpus evidence
//
//   - pdfjs devicen.pdf: carries this construct.
const deviceNRGB = "[/DeviceN [/Red /Green /Blue] /DeviceRGB 6 0 R]"

// deviceNResourcePDF builds a one-page PDF whose /Resources/ColorSpace declares
// csObj (object 5) as /CS0 and whose content selects it.
func deviceNResourcePDF(csObj string) []byte {
	const content = "/CS0 cs 0 0 0 scn 0 0 10 10 re f"
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
			"/Resources << /ColorSpace << /CS0 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		csObj,
		tintTransformObj,
	)
}

// deviceNImagePDF builds a one-page PDF that paints an image XObject whose
// /ColorSpace is csObj (object 5). The page /Resources carry no /ColorSpace
// entry at all, so the colour space is reachable only through the image.
func deviceNImagePDF(csObj string) []byte {
	const content = "q 10 0 0 10 0 0 cm /Im0 Do Q"
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
			"/Resources << /XObject << /Im0 7 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		csObj,
		tintTransformObj,
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 "+
			"/BitsPerComponent 8 /ColorSpace 5 0 R /Length 3 >>\nstream\n\x00\x00\x00\nendstream",
	)
}

// TestDeviceNColorants_AdditiveNamesAreSpotColours pins that Red, Green and
// Blue are spot colorants for ISO 19005-2 §6.2.4.4 t1. Only the four DeviceCMYK
// process colorants of ISO 32000-1 §8.6.6.4 are exempt; veraPDF's PDDeviceN
// model uses that same four-name set. litho previously exempted the additive
// primaries too.
//
// # Corpus evidence
//
//   - pdfjs devicen.pdf: litho stayed silent while it exempted the additive
//     primaries, and veraPDF fires.
func TestDeviceNColorants_AdditiveNamesAreSpotColours(t *testing.T) {
	t.Parallel()
	data := deviceNResourcePDF(deviceNRGB)
	if !firesRule2b(t, data, "6.2.4.4", 1) {
		t.Error("DeviceN naming /Red /Green /Blue with no /Colorants must fire §6.2.4.4 t1")
	}
}

// TestDeviceNColorants_ImageXObjectColorSpace pins that a DeviceN reached only
// as the /ColorSpace of an image XObject is checked. litho used to scan
// /Resources/ColorSpace only and missed it.
//
// # Corpus evidence
//
//   - pdfjs colorspace_atan.pdf, colorspace_cos.pdf, colorspace_sin.pdf: carry
//     this construct, where veraPDF reports at
//     .../xObject[0](4 0 obj PDXImage)/imageCS[0](6 0 obj PDDeviceN).
func TestDeviceNColorants_ImageXObjectColorSpace(t *testing.T) {
	t.Parallel()
	data := deviceNImagePDF("[/DeviceN [/X /Y /Z] /DeviceRGB 6 0 R]")
	if !firesRule2b(t, data, "6.2.4.4", 1) {
		t.Error("DeviceN used as an image XObject /ColorSpace must fire §6.2.4.4 t1")
	}
}

// TestDeviceNColorants_ProcessComponentsSatisfy pins that a colorant named in
// /Attributes /Process /Components is a process colorant, not a spot colour
// (ISO 32000-1 §8.6.6.5), so it needs no /Colorants entry. Without this the
// narrowed process-colorant set would over-report NChannel spaces built on an
// RGB process colour space.
func TestDeviceNColorants_ProcessComponentsSatisfy(t *testing.T) {
	t.Parallel()
	data := deviceNResourcePDF(
		"[/DeviceN [/Red /Green /Blue] /DeviceRGB 6 0 R " +
			"<< /Subtype /NChannel /Process << /ColorSpace /DeviceRGB " +
			"/Components [/Red /Green /Blue] >> >>]")
	if firesRule2b(t, data, "6.2.4.4", 1) {
		t.Error("colorants listed in /Process /Components must not fire §6.2.4.4 t1")
	}
}

// TestDeviceNColorants_ColorantsEntrySatisfies pins the positive path: a spot
// colorant carrying a /Colorants entry passes.
func TestDeviceNColorants_ColorantsEntrySatisfies(t *testing.T) {
	t.Parallel()
	data := deviceNResourcePDF(
		"[/DeviceN [/Spot1] /DeviceRGB 6 0 R " +
			"<< /Colorants << /Spot1 [/Separation /Spot1 /DeviceRGB 6 0 R] >> >>]")
	if firesRule2b(t, data, "6.2.4.4", 1) {
		t.Error("a spot colorant present in /Colorants must not fire §6.2.4.4 t1")
	}
}

// TestDeviceNColorants_NoneIsNotSpot pins that the reserved colorant /None,
// which marks a component that paints nothing (ISO 32000-1 §8.6.6.5), needs no
// /Colorants entry, and that the four process colorants stay exempt.
func TestDeviceNColorants_NoneIsNotSpot(t *testing.T) {
	t.Parallel()
	data := deviceNResourcePDF("[/DeviceN [/Cyan /Magenta /None] /DeviceCMYK 6 0 R]")
	if firesRule2b(t, data, "6.2.4.4", 1) {
		t.Error("process colorants plus /None must not fire §6.2.4.4 t1")
	}
}
