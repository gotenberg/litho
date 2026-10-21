package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// colorFixturePDF assembles a classic-xref PDF from objs, numbered from 1.
// Object 1 must be the catalog.
func colorFixturePDF(objs []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// colorStream writes a stream object holding data under dict's entries.
func colorStream(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// colorUsesPDF draws one page that uses a colour space in each way
// [Reader.ExtractColorSpaces] reports.
func colorUsesPDF() []byte {
	content := "1 0 0 rg 0 0 1 RG /CS0 cs 1 sc /CS1 CS /Im0 Do /Fm0 Do /Sh0 sh " +
		"/Pattern cs /P0 scn 0 0 10 10 re f BI /W 1 /H 1 /CS /G /BPC 8 ID \x80 EI 0.2 g"
	ramp := "/Coords [0 0 1 0] /Function << /FunctionType 2 /Domain [0 1] /N 1 >>"
	return colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Annots [10 0 R]" +
			" /Group << /S /Transparency /CS /DeviceRGB >>" +
			" /Resources << /ColorSpace << /CS0 9 0 R /CS1 [/Indexed /DeviceRGB 1 <000000FFFFFF>] >>" +
			" /XObject << /Im0 5 0 R /Fm0 6 0 R >> /Shading << /Sh0 7 0 R >> /Pattern << /P0 8 0 R >> >> >>",
		colorStream("", content),
		colorStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceCMYK /BitsPerComponent 8", "\x00\x00\x00\xff"),
		colorStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >>", "0 0 0 1 k"),
		"<< /ShadingType 2 /ColorSpace /DeviceGray " + ramp + " >>",
		"<< /PatternType 2 /Shading << /ShadingType 2 /ColorSpace /DeviceRGB " + ramp + " >> >>",
		"[/Separation /Gold /DeviceCMYK << /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 0.2 1 0] /N 1 >>]",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /AP << /N 11 0 R >> >>",
		colorStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "0.5 G"),
	})
}

// describeUse renders a use as "source operator name family object count",
// the space written family(alternate)[colorants].
func describeUse(u models.ColorSpaceUse) string {
	return fmt.Sprintf("%s %s %s %s #%d x%d", u.Source, u.Operator, u.Name, describeSpace(u), u.Object, u.Count)
}

func describeSpace(u models.ColorSpaceUse) string {
	s := fmt.Sprintf("%s/%d", u.Family, u.Components)
	if u.Alternate != nil {
		s += "(" + describeSpace(*u.Alternate) + ")"
	}
	if len(u.Colorants) > 0 {
		s += "[" + strings.Join(u.Colorants, ",") + "]"
	}
	return s
}

// TestExtractColorSpaces_ReportsEveryUse pins what the walk reports for each
// way a page paints in a colour space, in walk order.
func TestExtractColorSpaces_ReportsEveryUse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := colorUsesPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	uses, err := r.ExtractColorSpaces(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range uses {
		got = append(got, describeUse(u))
	}
	want := []string{
		"group Group  DeviceRGB/3 #3 x1",
		"operator rg  DeviceRGB/3 #0 x1",
		"operator RG  DeviceRGB/3 #0 x1",
		"operator cs CS0 Separation/1(DeviceCMYK/4)[Gold] #0 x1",
		"operator CS CS1 Indexed/1(DeviceRGB/3) #0 x1",
		"image Do Im0 DeviceCMYK/4 #5 x1",
		"operator k  DeviceCMYK/4 #6 x1",
		"shading sh Sh0 DeviceGray/1 #7 x1",
		"operator cs  Pattern/0 #0 x1",
		"shading scn P0 DeviceRGB/3 #0 x1",
		"inline-image BI  DeviceGray/1 #0 x1",
		"operator g  DeviceGray/1 #0 x1",
		"operator G  DeviceGray/1 #11 x1",
	}
	if !slices.Equal(got, want) {
		t.Errorf("uses:\n got %q\nwant %q", got, want)
	}
}

// TestExtractColorSpaces_CountsRepeatsAndWalksOnce pins that a form drawn
// twice is walked once, and that repeated operators fold into one entry.
func TestExtractColorSpaces_CountsRepeatsAndWalksOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /XObject << /Fm0 5 0 R >> >> >>",
		colorStream("", "1 g 0 g /Fm0 Do /Fm0 Do"),
		// The form draws itself: the walk must end.
		colorStream("/Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources << /XObject << /Fm0 5 0 R >> >>", "1 0 0 rg /Fm0 Do"),
	})
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	uses, err := r.ExtractColorSpaces(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range uses {
		got = append(got, describeUse(u))
	}
	want := []string{"operator g  DeviceGray/1 #0 x2", "operator rg  DeviceRGB/3 #5 x1"}
	if !slices.Equal(got, want) {
		t.Errorf("uses:\n got %q\nwant %q", got, want)
	}
}

// TestExtractColorSpaces_WalksPatternsGlyphsAndInlineIndexed pins the walk
// into a tiling pattern's content and a Type 3 font's glyphs, under their own
// resources, and an abbreviated indexed inline image.
func TestExtractColorSpaces_WalksPatternsGlyphsAndInlineIndexed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Pattern << /T0 5 0 R >> /Font << /F0 6 0 R >> >> >>",
		colorStream("", "/Pattern CS /T0 SCN BT /F0 12 Tf (a) Tj ET BI /W 1 /H 1 /CS [/I /RGB 1 <000000FFFFFF>] /BPC 8 ID \x01 EI"),
		colorStream("/PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 1 1] /XStep 1 /YStep 1"+
			" /Resources << /ColorSpace << /Lab0 [/Lab << /WhitePoint [0.9505 1 1.089] >>] >> >>", "/Lab0 cs 50 0 0 sc"),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0] /CharProcs << /a 7 0 R >>" +
			" /Encoding << /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [1] >>",
		colorStream("", "1 0 d0 0 0 0 1 K"),
	})
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	uses, err := r.ExtractColorSpaces(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range uses {
		got = append(got, describeUse(u))
	}
	want := []string{
		"operator CS  Pattern/0 #0 x1",
		"operator cs Lab0 Lab/3 #5 x1",
		"operator K  DeviceCMYK/4 #7 x1",
		"inline-image BI  Indexed/1(DeviceRGB/3) #0 x1",
	}
	if !slices.Equal(got, want) {
		t.Errorf("uses:\n got %q\nwant %q", got, want)
	}
}

// TestExtractColorSpaces_RejectsBadPage pins the page-number contract.
func TestExtractColorSpaces_RejectsBadPage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := colorUsesPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if _, err := r.ExtractColorSpaces(ctx, 2); err == nil {
		t.Error("page 2 of a one-page document: want an error")
	}
}
