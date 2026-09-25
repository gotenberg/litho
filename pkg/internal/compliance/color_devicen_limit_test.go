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

// directDeviceNPDF builds a PDF of one page per content, each with its own
// content stream, sharing one /Resources dictionary (object 3) that writes
// csArr directly as /CS0 and names a Helvetica /F1 (object 4), the form /Fm0
// (object 5) and the image /Im0 (object 7). Object 6 is the tint transform
// [deviceNOf] references. The form and the image write csArr directly too,
// and are empty dictionaries unless form and image give their content.
func directDeviceNPDF(csArr, form, image string, contents ...string) []byte {
	formObj := "<< >>"
	if form != "" {
		formObj = fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] "+
			"/Resources << /ColorSpace << /CS0 %s >> >> /Length %d >>\nstream\n%s\nendstream", csArr, len(form), form)
	}
	imageObj := "<< >>"
	if image != "" {
		imageObj = fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 "+
			"/ColorSpace %s /Length %d >>\nstream\n%s\nendstream", csArr, len(image), image)
	}
	var kids strings.Builder
	for i := range contents {
		fmt.Fprintf(&kids, " %d 0 R", 8+2*i)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(contents)),
		"<< /ColorSpace << /CS0 " + csArr + " >> /XObject << /Fm0 5 0 R /Im0 7 0 R >> /Font << /F1 4 0 R >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		formObj,
		tintTransformObj,
		imageObj,
	}
	for i, c := range contents {
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources 3 0 R /Contents %d 0 R >>", 9+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(c), c))
	}
	return cyclePDF(objs...)
}

// directDeviceNAppearancesPDF builds a one-page PDF with no page /Resources
// whose annotations draw appearance streams writing csArr directly as /CS0.
// shared gives two annotations one normal appearance; otherwise one annotation
// has distinct normal, rollover and down appearances.
func directDeviceNAppearancesPDF(csArr string, shared bool) []byte {
	const page = "0 0 1 1 re S"
	const content = "/CS0 cs 0 scn 0 0 10 10 re f"
	ap := fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] "+
		"/Resources << /ColorSpace << /CS0 %s >> >> /Length %d >>\nstream\n%s\nendstream", csArr, len(content), content)
	annots, first := "[7 0 R]", "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 8 0 R /R 9 0 R /D 10 0 R >> >>"
	extra := []string{ap, ap, ap}
	if shared {
		annots = "[7 0 R 9 0 R]"
		first = "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 8 0 R >> >>"
		extra = []string{ap, "<< /Type /Annot /Subtype /Square /Rect [20 0 30 10] /F 4 /AP << /N 8 0 R >> >>"}
	}
	return cyclePDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Contents 4 0 R /Annots " + annots + " >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		"<< >>",
		tintTransformObj,
		first,
	}, extra...)...)
}

// TestDeviceNLimit_CountsWhatVeraPDFCounts pins that ISO 19005-1 §6.1.12 t9
// and ISO 19005-2 §6.1.13 t9 fail as many times as veraPDF 1.30 fails them on
// a DeviceN written directly, with no object of its own. Every count below was
// measured on the same file at PDF/A-1b and PDF/A-2b.
//
// veraPDF judges an indirect object once, and a direct one wherever its model
// reaches it. A /Resources/ColorSpace entry is reached through every operator
// linking to it: cs, CS, scn and SCN, each painting operator by the space it
// paints with, text by its rendering mode, and an inline image by its /CS or,
// for a mask, by the fill space. sc, SC and n reach nothing. A form, an
// appearance or an image is an indirect object, so what it holds is judged
// once however many times it is painted, while a page is judged per page even
// when pages share their /Resources. litho judged every such space once per
// place it found it.
func TestDeviceNLimit_CountsWhatVeraPDFCounts(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		profile    models.PDFAProfile
		clause     string
		colourants int
	}{
		{models.PDFA1b, "6.1.12", 9},
		{models.PDFA2b, "6.1.13", 33},
	}
	tests := []struct {
		name string
		data func(dn string, n int) []byte
		want int
	}{
		{
			name: "cs, scn and f each reach it",
			data: func(dn string, _ int) []byte {
				return directDeviceNPDF(dn, "", "", "/CS0 cs 0 scn 0 0 10 10 re f")
			},
			want: 3,
		},
		{
			name: "sc and SC reach nothing, B reaches it for fill and stroke",
			data: func(dn string, _ int) []byte {
				return directDeviceNPDF(dn, "", "", "/CS0 CS 0 SCN 0 0 10 10 re S /CS0 cs 0 sc 0 0 10 10 re B")
			},
			want: 6,
		},
		{
			name: "text reaches it by rendering mode",
			data: func(dn string, _ int) []byte {
				return directDeviceNPDF(dn, "", "", "/CS0 cs /CS0 CS BT /F1 12 Tf 10 10 Td (a) Tj 1 Tr (b) Tj "+
					"2 Tr (c) Tj 3 Tr (d) Tj 7 Tr (e) Tj 0 Tr [(f)] TJ (g) ' 1 2 (h) \" ET")
			},
			want: 9,
		},
		{
			name: "Q restores the space cs selected",
			data: func(dn string, _ int) []byte {
				return directDeviceNPDF(dn, "", "", "q /CS0 cs Q 0 0 10 10 re f")
			},
			want: 1,
		},
		{
			name: "g replaces it and n paints nothing",
			data: func(dn string, _ int) []byte {
				return directDeviceNPDF(dn, "", "", "/CS0 cs 0.5 g 0 0 10 10 re f /CS0 CS 0 0 10 10 re n")
			},
			want: 2,
		},
		{
			name: "an inline image naming it reaches it",
			data: func(dn string, n int) []byte {
				return directDeviceNPDF(dn, "", "", "q /CS0 cs BI /W 1 /H 1 /CS /CS0 /BPC 8 ID "+strings.Repeat("\x00", n)+" EI Q")
			},
			want: 2,
		},
		{
			name: "an inline image mask reaches the fill space",
			data: func(dn string, _ int) []byte {
				return directDeviceNPDF(dn, "", "", "q /CS0 cs 0 scn BI /W 1 /H 1 /IM true ID \x00 EI Q")
			},
			want: 3,
		},
		{
			name: "pages sharing /Resources are judged per page",
			data: func(dn string, _ int) []byte {
				return directDeviceNPDF(dn, "", "", "/CS0 cs 0 scn 0 0 10 10 re f", "/CS0 cs 0 scn 0 0 10 10 re f")
			},
			want: 6,
		},
		{
			name: "a direct /Indexed base is reached with its space",
			data: func(dn string, n int) []byte {
				return directDeviceNPDF(indexedOver(dn, n), "", "", "/CS0 cs 0 scn 0 0 10 10 re f")
			},
			want: 3,
		},
		{
			name: "a form painted three times on two pages is judged once",
			data: func(dn string, _ int) []byte {
				return directDeviceNPDF(dn, "/CS0 cs 0 scn 0 0 10 10 re f", "",
					"/Fm0 Do q 1 0 0 1 20 0 cm /Fm0 Do Q", "/Fm0 Do")
			},
			want: 3,
		},
		{
			name: "an image painted three times on two pages is judged once",
			data: func(dn string, n int) []byte {
				return directDeviceNPDF(dn, "", strings.Repeat("\x00", n),
					"q 10 0 0 10 0 0 cm /Im0 Do Q q 10 0 0 10 20 0 cm /Im0 Do Q", "q 10 0 0 10 0 0 cm /Im0 Do Q")
			},
			want: 1,
		},
		{
			name: "three appearance streams are judged each",
			data: func(dn string, _ int) []byte { return directDeviceNAppearancesPDF(dn, false) },
			want: 9,
		},
		{
			name: "an appearance two annotations share is judged once",
			data: func(dn string, _ int) []byte { return directDeviceNAppearancesPDF(dn, true) },
			want: 3,
		},
	}

	for _, p := range profiles {
		for _, tt := range tests {
			t.Run(p.clause+" "+tt.name, func(t *testing.T) {
				t.Parallel()
				got := ruleContexts(t, tt.data(deviceNOf(p.colourants), p.colourants), p.profile, p.clause, 9)
				if len(got) != tt.want {
					t.Errorf("§%s t9 fired %d times, want %d: %q", p.clause, len(got), tt.want, got)
				}
			})
		}
	}
}

// inheritingFormsPDF builds a PDF of one page per content, each with its own
// content stream, sharing one /Resources dictionary (object 3) that writes
// csArr directly as /CS0 and names a Helvetica /F1 (object 4) and the forms
// /Fm0 (object 5) and /Fm1 (object 7). Object 6 is the tint transform
// [deviceNOf] references. /Fm0 draws fm0 with the resources fm0Res, in which
// $CS stands for csArr, and /Fm1 draws fm1 with none of its own.
func inheritingFormsPDF(csArr, fm0, fm0Res, fm1 string, contents ...string) []byte {
	form := func(res, content string) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources %s /Length %d >>\nstream\n%s\nendstream",
			strings.ReplaceAll(res, "$CS", csArr), len(content), content)
	}
	var kids strings.Builder
	for i := range contents {
		fmt.Fprintf(&kids, " %d 0 R", 8+2*i)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(contents)),
		"<< /ColorSpace << /CS0 " + csArr + " >> /XObject << /Fm0 5 0 R /Fm1 7 0 R >> /Font << /F1 4 0 R >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		form(fm0Res, fm0),
		tintTransformObj,
		form("<< >>", fm1),
	}
	for i, c := range contents {
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources 3 0 R /Contents %d 0 R >>", 9+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(c), c))
	}
	return cyclePDF(objs...)
}

// inheritingAppearancePDF builds a one-page PDF with one annotation whose
// normal appearance (object 8) writes csArr directly as /CS0 and draws
// "/CS0 cs /Fm0 Do", where the form /Fm0 (object 5) fills with no colour space
// of its own. pageCS gives the page its own /CS1, csArr again, which its
// content selects before it paints /Fm0 too.
func inheritingAppearancePDF(csArr string, pageCS bool) []byte {
	pageRes, content := "<< >>", "0 0 1 1 re S"
	if pageCS {
		pageRes, content = "<< /ColorSpace << /CS1 "+csArr+" >> /XObject << /Fm0 5 0 R >> >>", "/CS1 cs /Fm0 Do"
	}
	const (
		formContent = "0 0 10 10 re f"
		apContent   = "/CS0 cs /Fm0 Do"
	)
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources "+pageRes+" /Contents 4 0 R /Annots [7 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >> /Length %d >>\nstream\n%s\nendstream",
			len(formContent), formContent),
		tintTransformObj,
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 8 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] "+
			"/Resources << /ColorSpace << /CS0 %s >> /XObject << /Fm0 5 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
			csArr, len(apContent), apContent),
	)
}

// TestDeviceNLimit_CountsInheritedColorSpaces pins that ISO 19005-1 §6.1.12 t9
// and ISO 19005-2 §6.1.13 t9 fail as many times as veraPDF 1.30 fails them on
// a direct DeviceN a form paints with without selecting it, the colour space
// it inherits from the stream that paints it (ISO 32000-1, 8.10.2). Every
// count below was measured on the same file at PDF/A-1b and PDF/A-2b.
//
// veraPDF links each operator of the form that fills or strokes with the
// inherited space, or shows text in the inherited rendering mode, to the
// space the painting stream selected. It judges a form's content once, in
// the state of the first Do that reaches it, walking the pages, then each
// page's content before its annotations, and each stream's operators in
// order. A name the form selects that only the page defines links to the
// page's entry.
func TestDeviceNLimit_CountsInheritedColorSpaces(t *testing.T) {
	t.Parallel()

	const (
		noRes  = "<< /XObject << /Fm1 7 0 R >> /Font << /F1 4 0 R >> >>"
		ownCS0 = "<< /ColorSpace << /CS0 $CS >> >>"
		fill   = "0 0 10 10 re f"
	)
	profiles := []struct {
		profile    models.PDFAProfile
		clause     string
		colourants int
	}{
		{models.PDFA1b, "6.1.12", 9},
		{models.PDFA2b, "6.1.13", 33},
	}
	tests := []struct {
		name string
		data func(dn string) []byte
		want int
	}{
		{
			name: "a form fills with the space cs and scn reach",
			data: func(dn string) []byte { return inheritingFormsPDF(dn, fill, noRes, "", "/CS0 cs 0 scn /Fm0 Do") },
			want: 3,
		},
		{
			name: "a form painted twice is judged once",
			data: func(dn string) []byte { return inheritingFormsPDF(dn, fill, noRes, "", "/CS0 cs /Fm0 Do /Fm0 Do") },
			want: 2,
		},
		{
			name: "a form strokes with the inherited stroke space alone",
			data: func(dn string) []byte {
				return inheritingFormsPDF(dn, "0 0 10 10 re S f", noRes, "", "/CS0 CS /Fm0 Do")
			},
			want: 2,
		},
		{
			name: "a form two pages paint is judged on the first",
			data: func(dn string) []byte {
				return inheritingFormsPDF(dn, fill, noRes, "", "/CS0 cs /Fm0 Do", "/CS0 cs /Fm0 Do")
			},
			want: 3,
		},
		{
			name: "a nested form inherits through the form painting it",
			data: func(dn string) []byte {
				return inheritingFormsPDF(dn, "/Fm1 Do /Fm1 Do", noRes, fill, "/CS0 cs /Fm0 Do")
			},
			want: 2,
		},
		{
			name: "g replaces the inherited space until Q restores it, and scn reaches it",
			data: func(dn string) []byte {
				return inheritingFormsPDF(dn, "q 0.5 g 0 0 10 10 re f Q 0 0 10 10 re f 0 scn", noRes, "", "/CS0 cs /Fm0 Do")
			},
			want: 3,
		},
		{
			name: "a form first painted before cs links nothing",
			data: func(dn string) []byte { return inheritingFormsPDF(dn, fill, noRes, "", "/Fm0 Do /CS0 cs /Fm0 Do") },
			want: 1,
		},
		{
			name: "a form first painted on a page before cs links nothing",
			data: func(dn string) []byte { return inheritingFormsPDF(dn, fill, noRes, "", "/Fm0 Do", "/CS0 cs /Fm0 Do") },
			want: 1,
		},
		{
			name: "a nested form first painted by the page links nothing",
			data: func(dn string) []byte {
				return inheritingFormsPDF(dn, "/Fm1 Do", noRes, fill, "/Fm1 Do /CS0 cs /Fm0 Do")
			},
			want: 1,
		},
		{
			name: "a nested form first reached through its parent links there",
			data: func(dn string) []byte {
				return inheritingFormsPDF(dn, "/Fm1 Do", noRes, fill, "q /CS0 cs /Fm0 Do Q /Fm1 Do")
			},
			want: 2,
		},
		{
			name: "a form's own entry of the same name is not the inherited one",
			data: func(dn string) []byte { return inheritingFormsPDF(dn, fill, ownCS0, "", "/CS0 cs /Fm0 Do") },
			want: 2,
		},
		{
			name: "a form's own entry no operator selects is not judged",
			data: func(dn string) []byte { return inheritingFormsPDF(dn, fill, ownCS0, "", "/Fm0 Do") },
			want: 0,
		},
		{
			name: "a form fills with the inherited space, then with its own",
			data: func(dn string) []byte {
				return inheritingFormsPDF(dn, "0 0 10 10 re f /CS0 cs 0 0 10 10 re f", ownCS0, "", "/CS0 cs /Fm0 Do")
			},
			want: 4,
		},
		{
			name: "an inline image mask in a form reaches the inherited fill space",
			data: func(dn string) []byte {
				return inheritingFormsPDF(dn, "BI /W 1 /H 1 /IM true ID \x00 EI", noRes, "", "/CS0 cs /Fm0 Do")
			},
			want: 2,
		},
		{
			name: "text in a form paints in the inherited rendering mode",
			data: func(dn string) []byte {
				return inheritingFormsPDF(dn, "BT /F1 12 Tf 1 1 Td (a) Tj ET", noRes, "", "/CS0 CS 1 Tr /Fm0 Do")
			},
			want: 2,
		},
		{
			name: "text in a form reaches both inherited spaces by rendering mode",
			data: func(dn string) []byte {
				return inheritingFormsPDF(dn, "BT /F1 12 Tf 1 1 Td (a) Tj 2 Tr (b) Tj ET", noRes, "", "/CS0 cs /CS0 CS /Fm0 Do")
			},
			want: 5,
		},
		{
			name: "a form selecting a name only the page defines links to the page's entry",
			data: func(dn string) []byte { return inheritingFormsPDF(dn, "/CS0 cs "+fill, noRes, "", "/CS0 cs /Fm0 Do") },
			want: 3,
		},
		{
			name: "a form selecting a name only the page defines reaches it though the page never selects it",
			data: func(dn string) []byte { return inheritingFormsPDF(dn, "/CS0 cs 0 scn "+fill, noRes, "", "/Fm0 Do") },
			want: 3,
		},
		{
			name: "a nested form selecting a name only the page defines reaches it",
			data: func(dn string) []byte { return inheritingFormsPDF(dn, "/Fm1 Do", noRes, "/CS0 cs "+fill, "/Fm0 Do") },
			want: 2,
		},
		{
			name: "a form two pages paint reaches the page name on the first",
			data: func(dn string) []byte {
				return inheritingFormsPDF(dn, "/CS0 cs "+fill, noRes, "", "/Fm0 Do", "/Fm0 Do")
			},
			want: 2,
		},
		{
			name: "a page name no form selects is not judged",
			data: func(dn string) []byte { return inheritingFormsPDF(dn, fill, noRes, "", "/Fm0 Do") },
			want: 0,
		},
		{
			name: "a form an appearance stream paints inherits its space",
			data: func(dn string) []byte { return inheritingAppearancePDF(dn, false) },
			want: 2,
		},
		{
			name: "a form the page paints before its appearance streams is judged there",
			data: func(dn string) []byte { return inheritingAppearancePDF(dn, true) },
			want: 3,
		},
	}

	for _, p := range profiles {
		for _, tt := range tests {
			t.Run(p.clause+" "+tt.name, func(t *testing.T) {
				t.Parallel()
				got := ruleContexts(t, tt.data(deviceNOf(p.colourants)), p.profile, p.clause, 9)
				if len(got) != tt.want {
					t.Errorf("§%s t9 fired %d times, want %d: %q", p.clause, len(got), tt.want, got)
				}
			})
		}
	}
}
