package compliance

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// deviceNOf returns a DeviceN colour-space array naming n spot colorants over
// /DeviceGray, with the tint transform at object 6 (see [tintTransformObj]).
func deviceNOf(n int) string {
	var b strings.Builder
	b.WriteString("[/DeviceN [")
	for i := range n {
		fmt.Fprintf(&b, " /C%d", i)
	}
	b.WriteString("] /DeviceGray 6 0 R]")
	return b.String()
}

// deviceNImagesPDF builds a one-page PDF whose /Resources/XObject declares
// images image XObjects, objects 7 onward, each naming csObj (object 5) as its
// /ColorSpace. content is the page content stream.
func deviceNImagesPDF(csObj string, images int, content string) []byte {
	var xobjects strings.Builder
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		csObj,
		tintTransformObj,
	}
	for i := range images {
		fmt.Fprintf(&xobjects, " /Im%d %d 0 R", i, 7+i)
		objs = append(objs, "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 "+
			"/BitsPerComponent 8 /ColorSpace 5 0 R /Length 1 >>\nstream\n\x00\nendstream")
	}
	objs[2] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] " +
		"/Resources << /XObject <<" + xobjects.String() + " >> >> /Contents 4 0 R >>"
	return cyclePDF(objs...)
}

// deviceNAnnotationPDF builds a one-page PDF whose page declares no
// /Resources at all, its own or inherited, and carries one annotation whose
// normal appearance stream selects csObj (object 5) through its own
// /Resources.
func deviceNAnnotationPDF(csObj string) []byte {
	const page = "0 0 1 1 re S"
	const appearance = "/CS0 cs 0 scn 0 0 10 10 re f"
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Contents 4 0 R /Annots [7 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		csObj,
		tintTransformObj,
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 8 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] "+
			"/Resources << /ColorSpace << /CS0 5 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
			len(appearance), appearance),
	)
}

// TestDeviceNLimit_ReachesWhatVeraPDFReaches pins that ISO 19005-1 §6.1.12 t9
// and ISO 19005-2 §6.1.13 t9, the DeviceN colourant limit (8 colourants from
// PDF Reference 1.4 Table C.1, 32 from ISO 32000-1 Annex C), are judged on
// every DeviceN veraPDF reaches, and only once per indirect colour space.
//
// litho judged the limit on /Resources/ColorSpace entries alone. An image
// XObject names its colour space inline, an /Indexed space names its DeviceN
// as its base, and a page with no /Resources was skipped with its annotations,
// so all three went unreported. Each case below was measured with veraPDF
// 1.30: it reports one failed check for each, anchored at the image's imageCS,
// at the /Indexed space's base, or at the appearance stream's colorSpace.
//
// The silent cases are the controls. veraPDF reaches an image only through
// the Do that paints it, so a declared, unpainted image stays silent under
// both the limit and §6.2.4.4 t1. Two images sharing one DeviceN object fail
// one check on veraPDF, since it judges each indirect object once.
func TestDeviceNLimit_ReachesWhatVeraPDFReaches(t *testing.T) {
	t.Parallel()

	const paint = "q 10 0 0 10 0 0 cm /Im0 Do Q"
	const paintBoth = paint + " q 10 0 0 10 20 0 cm /Im1 Do Q"

	tests := []struct {
		name    string
		data    []byte
		profile models.PDFAProfile
		clause  string
		want    int
	}{
		{
			name:    "a painted image's DeviceN over the part-2 limit fires",
			data:    deviceNImagesPDF(deviceNOf(33), 1, paint),
			profile: models.PDFA2b, clause: "6.1.13", want: 1,
		},
		{
			name:    "a painted image's DeviceN at the part-2 limit stays silent",
			data:    deviceNImagesPDF(deviceNOf(32), 1, paint),
			profile: models.PDFA2b, clause: "6.1.13", want: 0,
		},
		{
			name:    "a painted image's DeviceN over the part-1 limit fires",
			data:    deviceNImagesPDF(deviceNOf(9), 1, paint),
			profile: models.PDFA1b, clause: "6.1.12", want: 1,
		},
		{
			name:    "a painted image's DeviceN at the part-1 limit stays silent",
			data:    deviceNImagesPDF(deviceNOf(8), 1, paint),
			profile: models.PDFA1b, clause: "6.1.12", want: 0,
		},
		{
			name:    "an image no Do paints stays silent",
			data:    deviceNImagesPDF(deviceNOf(33), 1, "0 0 1 1 re S"),
			profile: models.PDFA2b, clause: "6.1.13", want: 0,
		},
		{
			name:    "a painted image's /Indexed base fires",
			data:    deviceNImagesPDF(indexedOver(deviceNOf(33), 33), 1, paint),
			profile: models.PDFA2b, clause: "6.1.13", want: 1,
		},
		{
			name:    "a selected resource colour space's /Indexed base fires",
			data:    deviceNResourcePDF(indexedOver(deviceNOf(9), 9)),
			profile: models.PDFA1b, clause: "6.1.12", want: 1,
		},
		{
			name:    "two painted images sharing one DeviceN object fire once",
			data:    deviceNImagesPDF(deviceNOf(33), 2, paintBoth),
			profile: models.PDFA2b, clause: "6.1.13", want: 1,
		},
		{
			name:    "an annotation on a page with no /Resources fires",
			data:    deviceNAnnotationPDF(deviceNOf(9)),
			profile: models.PDFA1b, clause: "6.1.12", want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ruleContexts(t, tt.data, tt.profile, tt.clause, 9)
			if len(got) != tt.want {
				t.Errorf("§%s t9 fired %d times, want %d: %q", tt.clause, len(got), tt.want, got)
			}
		})
	}
}

// TestDeviceNColorants_UnpaintedImageStaysSilent pins that §6.2.4.4 t1 no
// longer judges an image XObject no Do paints. veraPDF reaches an image's
// colour space only through the Do that paints it, and was measured silent on
// this file, while litho reported every undeclared colorant of the image.
func TestDeviceNColorants_UnpaintedImageStaysSilent(t *testing.T) {
	t.Parallel()
	data := deviceNImagesPDF("[/DeviceN [/X /Y /Z] /DeviceRGB 6 0 R]", 1, "0 0 1 1 re S")
	if firesRule2b(t, data, "6.2.4.4", 1) {
		t.Error("a DeviceN image no Do paints must not fire §6.2.4.4 t1")
	}
}

// TestDeviceNColorants_AnnotationOnPageWithoutResources pins that §6.2.4.4 t1
// reaches an annotation's appearance stream on a page that declares no
// /Resources, as veraPDF does, measured.
func TestDeviceNColorants_AnnotationOnPageWithoutResources(t *testing.T) {
	t.Parallel()
	data := deviceNAnnotationPDF("[/DeviceN [/X /Y /Z] /DeviceRGB 6 0 R]")
	if !firesRule2b(t, data, "6.2.4.4", 1) {
		t.Error("a DeviceN an appearance stream selects on a page with no /Resources must fire §6.2.4.4 t1")
	}
}
