package compliance

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// Fixture pieces of the colour-space name and colour-off tests. Every count
// the tests pin was measured with veraPDF 1.30 on the same fixture.
const (
	namesFill    = "0 0 10 10 re f"
	namesIdx     = "1 <000000FFFFFF>"
	namesFn1     = "<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [1 0 0 0] /N 1 >>"
	namesFn3     = "<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 0 0] /N 1 >>"
	namesPattern = "/CSp [/Pattern /DeviceGray]"
	namesOffFill = "/CSp cs 0.5 /P2 scn " + namesFill
	namesOnFill  = "/Pattern cs /P1 scn " + namesFill
	namesBigW    = "2147483648 w"
)

// namesForm returns a form XObject whose resources are res and whose content
// is body.
func namesForm(res, body string) string {
	return defaultStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources "+res, body)
}

// namesTiling returns a tiling pattern of the paint type whose cell has the
// resources res and the content body.
func namesTiling(paint int, res, body string) string {
	return defaultStream(fmt.Sprintf("/Type /Pattern /PatternType 1 /PaintType %d /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources %s", paint, res), body)
}

// namesImage returns a one-pixel image XObject in the colour space cs.
func namesImage(cs string) string {
	return defaultStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace "+cs, "\x00")
}

// namesDeviceN returns a DeviceN colour space naming n spot colourants over
// DeviceCMYK.
func namesDeviceN(n int) string {
	names := make([]string, n)
	domain := make([]string, n)
	for i := range n {
		names[i] = fmt.Sprintf("/S%d", i)
		domain[i] = "0 1"
	}
	return fmt.Sprintf("[/DeviceN [%s] /DeviceCMYK << /FunctionType 4 /Domain [%s] /Range [0 1 0 1 0 1 0 1] /Length 1 >>]",
		strings.Join(names, " "), strings.Join(domain, " "))
}

// namesType3 returns a Type 3 font whose glyph /a is object glyph, with the
// resources res.
func namesType3(glyph int, res string) string {
	return fmt.Sprintf("<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [0.001 0 0 0.001 0 0] "+
		"/CharProcs << /a %d 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >> "+
		"/FirstChar 97 /LastChar 97 /Widths [1000] /Resources %s >>", glyph, res)
}

// namesCount is one rule count a fixture pins.
type namesCount struct {
	profile models.PDFAProfile
	clause  string
	test    int
	want    int
}

// namesDevice returns the counts of the device family rule of ISO 19005-1,
// 6.2.3.3 and ISO 19005-2, 6.2.4.3 for family, at PDF/A-1b and PDF/A-2b.
func namesDevice(family string, want int) []namesCount {
	test := map[string]int{"DeviceRGB": 1, "DeviceCMYK": 2, "DeviceGray": 3}[family]
	return []namesCount{{models.PDFA1b, "6.2.3.3", test, want}, {models.PDFA2b, "6.2.4.3", test + 1, want}}
}

// namesLimit returns the counts of the DeviceN colourant limit, ISO 19005-1,
// 6.1.12 t9 and ISO 19005-2, 6.1.13 t9, at PDF/A-1b and PDF/A-2b.
func namesLimit(want1b, want2b int) []namesCount {
	return []namesCount{{models.PDFA1b, "6.1.12", 9, want1b}, {models.PDFA2b, "6.1.13", 9, want2b}}
}

// namesIntegerLimit returns the counts of the integer limit, ISO 19005-1,
// 6.1.12 t1 and ISO 19005-2, 6.1.13 t1, at PDF/A-1b and PDF/A-2b.
func namesIntegerLimit(want int) []namesCount {
	return []namesCount{{models.PDFA1b, "6.1.12", 1, want}, {models.PDFA2b, "6.1.13", 1, want}}
}

// runNamesCases validates each case's data and checks the counts it pins.
func runNamesCases(t *testing.T, cases []struct {
	name   string
	data   []byte
	counts []namesCount
},
) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, c := range tc.counts {
				if got := countRulePDFA(t, tc.data, c.profile, c.clause, c.test); got != c.want {
					t.Errorf("%s: %s t%d fired %d times, want %d", c.profile, c.clause, c.test, got, c.want)
				}
			}
		})
	}
}

// joinNamesCounts concatenates count sets.
func joinNamesCounts(sets ...[]namesCount) []namesCount {
	var out []namesCount
	for _, s := range sets {
		out = append(out, s...)
	}
	return out
}

// TestColorSpaceNameReadsTheEntry pins that a name a colour space holds, an
// /Indexed base, a Separation or DeviceN alternate, a /Colorants entry, a
// page group's /CS or the value of an entry, reads as the /ColorSpace entry
// it names (see [colorLinkWalk.judgeFamilyName]), where litho read it as a
// space it cannot read, DeviceGray. An indirect object holding a family name
// is judged at every link, as the name is.
func TestColorSpaceNameReadsTheEntry(t *testing.T) {
	t.Parallel()

	abstractRGB := defaultStream("/N 3", iccHeaderOnly("abst", "RGB "))
	iccA := func(want int) []namesCount {
		return []namesCount{{models.PDFA1b, "6.2.3.2", 1, want}, {models.PDFA2b, "6.2.4.2", 1, want}}
	}
	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{
		{
			name:   "an /Indexed base",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 /DeviceRGB /CS1 [/Indexed /CS0 "+namesIdx+"] >>", "/CS1 cs 0 sc "+namesFill),
			counts: joinNamesCounts(namesDevice("DeviceRGB", 2), namesDevice("DeviceGray", 0)),
		},
		{
			name:   "an /Indexed base naming a one-element array",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 [/DeviceRGB] /CS1 [/Indexed /CS0 "+namesIdx+"] >>", "/CS1 cs 0 sc "+namesFill),
			counts: joinNamesCounts(namesDevice("DeviceRGB", 2), namesDevice("DeviceGray", 0)),
		},
		{
			name:   "an entry naming another",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 /DeviceRGB /CS2 /CS0 >>", "/CS2 cs 1 0 0 sc "+namesFill),
			counts: joinNamesCounts(namesDevice("DeviceRGB", 2), namesDevice("DeviceGray", 0)),
		},
		{
			name:   "a Separation alternate",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 /DeviceCMYK /CS1 [/Separation /Spot /CS0 "+namesFn1+"] >>", "/CS1 cs 1 scn "+namesFill),
			counts: joinNamesCounts(namesDevice("DeviceCMYK", 3), namesDevice("DeviceGray", 0)),
		},
		{
			name:   "a DeviceN alternate",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 /DeviceCMYK /CS1 [/DeviceN [/Cyan] /CS0 "+namesFn1+"] >>", "/CS1 cs 1 scn "+namesFill),
			counts: joinNamesCounts(namesDevice("DeviceCMYK", 3), namesDevice("DeviceGray", 0)),
		},
		{
			name: "a /Colorants entry",
			data: defaultSpacePDF("", "/ColorSpace << /CS3 [/Separation /Spot /DeviceRGB "+namesFn3+"] /CS1 [/DeviceN [/Spot] /DeviceGray "+namesFn1+
				" << /Colorants << /Spot /CS3 >> >>] >>", "/CS1 cs 1 scn "+namesFill),
			counts: joinNamesCounts(namesDevice("DeviceRGB", 3), namesDevice("DeviceGray", 3)),
		},
		{
			name:   "a page group's /CS",
			data:   defaultSpacePDF("/Group << /S /Transparency /CS /CS0 >>", "/ColorSpace << /CS0 /DeviceRGB >>", "0 0 10 10 re n"),
			counts: namesDevice("DeviceRGB", 1),
		},
		{
			name:   "a Default colour space's /Indexed base",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 /DeviceCMYK /DefaultRGB [/Indexed /CS0 "+namesIdx+"] >>", "1 0 0 rg "+namesFill),
			counts: joinNamesCounts(namesDevice("DeviceCMYK", 2), namesDevice("DeviceGray", 0), namesDevice("DeviceRGB", 0)),
		},
		{
			name:   "an inline image's /Indexed base",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 /DeviceRGB >>", "BI /W 1 /H 1 /BPC 8 /CS [/Indexed /CS0 "+namesIdx+"] ID \x00 EI"),
			counts: joinNamesCounts(namesDevice("DeviceRGB", 1), namesDevice("DeviceGray", 0)),
		},
		{
			name:   "a direct ICCBased entry an /Indexed base names",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 [/ICCBased 5 0 R] /CS1 [/Indexed /CS0 "+namesIdx+"] >>", "/CS1 cs 0 sc "+namesFill, abstractRGB),
			counts: iccA(2),
		},
		{
			name:   "an indirect ICCBased entry an /Indexed base names",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 6 0 R /CS1 [/Indexed /CS0 "+namesIdx+"] >>", "/CS1 cs 0 sc "+namesFill, abstractRGB, "[/ICCBased 5 0 R]"),
			counts: iccA(1),
		},
		{
			name:   "an entry holding an indirect family name",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 5 0 R >>", "/CS0 cs 1 0 0 sc "+namesFill+" /CS0 cs "+namesFill, "/DeviceRGB"),
			counts: namesDevice("DeviceRGB", 4),
		},
		{
			name: "a Pattern space an entry names",
			data: defaultSpacePDF("", "/ColorSpace << /CSq [/Pattern] /CSp /CSq >> /Pattern << /P1 5 0 R >>", "/CSp cs /P1 scn "+namesFill,
				namesTiling(1, "<< >>", namesBigW+" 1 0 0 rg 0 0 5 5 re f")),
			counts: joinNamesCounts(namesDevice("DeviceRGB", 2), namesDevice("DeviceGray", 0), namesIntegerLimit(1)),
		},
		{
			name: "the underlying space of a Pattern space an entry names",
			data: defaultSpacePDF("", "/ColorSpace << /CSq [/Pattern /DeviceRGB] /CSp /CSq >> /Pattern << /P2 5 0 R >>", "/CSp cs 1 0 0 /P2 scn "+namesFill,
				namesTiling(2, "<< /XObject << /Im0 6 0 R >> >>", "/Im0 Do"),
				defaultStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ImageMask true /BitsPerComponent 1", "\x00")),
			counts: joinNamesCounts(namesDevice("DeviceRGB", 1), namesDevice("DeviceGray", 0)),
		},
		{
			name:   "an /Indexed base holding an indirect family name",
			data:   defaultSpacePDF("", "/ColorSpace << /CS1 [/Indexed 5 0 R "+namesIdx+"] >>", "/CS1 cs 0 sc "+namesFill, "/DeviceRGB"),
			counts: namesDevice("DeviceRGB", 2),
		},
	})
}

// TestColorSpaceNameResolvesInItsOwnResources pins that a name a colour space
// holds reads against the one resource dictionary the space was read from,
// with no fallback to the page's, and that the Default colour spaces of that
// dictionary replace a device family it holds: the one defining the entry an
// operator selects, and the one defining the image or the shading.
func TestColorSpaceNameResolvesInItsOwnResources(t *testing.T) {
	t.Parallel()

	abstractRGB := defaultStream("/N 3", iccHeaderOnly("abst", "RGB "))
	iccA := func(want int) []namesCount {
		return []namesCount{{models.PDFA1b, "6.2.3.2", 1, want}, {models.PDFA2b, "6.2.4.2", 1, want}}
	}
	pageDefault := "/ColorSpace << /DefaultRGB [/ICCBased 7 0 R] >> "
	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{
		{
			name: "a form's /Indexed over a name only the page defines",
			data: defaultSpacePDF("", "/ColorSpace << /CS0 /DeviceRGB >> /XObject << /Fm0 5 0 R >>", "/Fm0 Do",
				namesForm("<< /ColorSpace << /CS1 [/Indexed /CS0 "+namesIdx+"] >> >>", "/CS1 cs 0 sc "+namesFill)),
			counts: joinNamesCounts(namesDevice("DeviceGray", 2), namesDevice("DeviceRGB", 0)),
		},
		{
			name: "the page's /Indexed a form selects over a name the form redefines",
			data: defaultSpacePDF("", "/ColorSpace << /CS0 /DeviceRGB /CS1 [/Indexed /CS0 "+namesIdx+"] >> /XObject << /Fm0 5 0 R >>", "/Fm0 Do",
				namesForm("<< /ColorSpace << /CS0 /DeviceCMYK >> >>", "/CS1 cs 0 sc "+namesFill)),
			counts: joinNamesCounts(namesDevice("DeviceRGB", 2), namesDevice("DeviceCMYK", 0)),
		},
		{
			name: "the page's image a form paints over a name the form redefines",
			data: defaultSpacePDF("", "/ColorSpace << /CS0 /DeviceRGB >> /XObject << /Im0 5 0 R /Fm0 6 0 R >>", "/Fm0 Do",
				namesImage("[/Indexed /CS0 "+namesIdx+"]"), namesForm("<< /ColorSpace << /CS0 /DeviceCMYK >> >>", "/Im0 Do")),
			counts: joinNamesCounts(namesDevice("DeviceRGB", 1), namesDevice("DeviceCMYK", 0)),
		},
		{
			name: "a form's shading naming an entry only the page defines",
			data: defaultSpacePDF("", "/ColorSpace << /CS0 /DeviceRGB >> /XObject << /Fm0 5 0 R >>", "/Fm0 Do",
				namesForm("<< /Shading << /Sh0 << /ShadingType 2 /ColorSpace /CS0 /Coords [0 0 10 0] /Function "+namesFn3+" >> >> >>", "/Sh0 sh")),
			counts: namesDevice("DeviceRGB", 0),
		},
		{
			name: "the page's shading a form paints, under the page's DefaultRGB",
			data: defaultSpacePDF("", pageDefault+"/Shading << /Sh0 << /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 10 0] /Function "+namesFn3+
				" >> >> /XObject << /Fm0 5 0 R >>", "/Fm0 Do", namesForm("<< /ExtGState << >> >>", "/Sh0 sh"), "null", abstractRGB),
			counts: joinNamesCounts(iccA(1), namesDevice("DeviceRGB", 0)),
		},
		{
			name: "the page's /Indexed image a form paints, under the page's DefaultRGB",
			data: defaultSpacePDF("", pageDefault+"/XObject << /Im0 6 0 R /Fm0 5 0 R >>", "/Fm0 Do",
				namesForm("<< /ExtGState << >> >>", "/Im0 Do"), namesImage("[/Indexed /DeviceRGB "+namesIdx+"]"), abstractRGB),
			counts: joinNamesCounts(iccA(1), namesDevice("DeviceRGB", 0)),
		},
	})
}

// TestSeparationAlternateNameComparesAsItsEntry pins that ISO 19005-2,
// 6.2.4.4 t2 compares an alternate that names a /ColorSpace entry as that
// entry (see [checkSeparationLinks]): a Separation over /CS0, where /CS0 is
// /DeviceCMYK, and one over /DeviceCMYK, naming one colourant through one
// tint transform, are consistent, and both reach DeviceCMYK.
func TestSeparationAlternateNameComparesAsItsEntry(t *testing.T) {
	t.Parallel()

	data := defaultSpacePDF("", "/ColorSpace << /CS0 /DeviceCMYK /CS1 [/Separation /Spot /CS0 5 0 R] /CS2 [/Separation /Spot /DeviceCMYK 5 0 R] >>",
		"/CS1 cs 1 scn "+namesFill+" /CS2 cs 1 scn "+namesFill, namesFn1)
	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{{
		name:   "an alternate naming /DeviceCMYK beside /DeviceCMYK",
		data:   data,
		counts: joinNamesCounts([]namesCount{{models.PDFA2b, "6.2.4.4", 2, 0}}, namesDevice("DeviceCMYK", 6)),
	}})
}

// TestColorSpaceNameCycleStops pins that a name reaching itself again ends
// the walk as a space litho cannot read, DeviceGray at every link. veraPDF
// 1.30 never finishes reading such a space: it runs out of time, heap or
// stack.
func TestColorSpaceNameCycleStops(t *testing.T) {
	t.Parallel()

	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{
		{
			name:   "an /Indexed base naming its own entry",
			data:   defaultSpacePDF("", "/ColorSpace << /CS1 [/Indexed /CS1 "+namesIdx+"] >>", "/CS1 cs 0 sc "+namesFill),
			counts: namesDevice("DeviceGray", 2),
		},
		{
			name:   "an /Indexed and a Separation naming each other",
			data:   defaultSpacePDF("", "/ColorSpace << /CS1 [/Indexed /CS2 "+namesIdx+"] /CS2 [/Separation /Spot /CS1 "+namesFn1+"] >>", "/CS1 cs 0 sc "+namesFill),
			counts: namesDevice("DeviceGray", 2),
		},
		{
			name:   "an entry naming itself",
			data:   defaultSpacePDF("", "/ColorSpace << /CS1 /CS1 >>", "/CS1 cs 0 sc "+namesFill),
			counts: namesDevice("DeviceGray", 2),
		},
	})
}

// TestColorSpaceNameFanOutIsBounded pins that [maxColorSpaceNameLinks] bounds
// the names one link follows: nine DeviceN entries, each of whose /Colorants
// name the next entry 200 times, fan out as 200 to the power of the nesting
// the walk allows, which no validation finishes, and the bound stops it.
func TestColorSpaceNameFanOutIsBounded(t *testing.T) {
	t.Parallel()

	const (
		levels    = 9
		colorants = 200
	)
	var entries strings.Builder
	for level := range levels {
		var names strings.Builder
		for i := range colorants {
			fmt.Fprintf(&names, " /C%d /CS%d", i, level+1)
		}
		fmt.Fprintf(&entries, " /CS%d [/DeviceN [/Spot] /DeviceGray %s << /Colorants <<%s >> >>]", level, namesFn1, names.String())
	}
	fmt.Fprintf(&entries, " /CS%d [/Separation /Spot /DeviceGray %s]", levels, namesFn1)
	data := defaultSpacePDF("", "/ColorSpace <<"+entries.String()+" >>", "/CS0 cs 1 scn "+namesFill)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if _, err := ValidatePDFA(ctx, doc, models.PDFA2b); err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
}

// TestDeviceNLimitOnEveryLink pins that the DeviceN colourant limit, ISO
// 19005-1, 6.1.12 t9 and ISO 19005-2, 6.1.13 t9, fails on every link veraPDF
// makes to a DeviceN past it, in the colour state the link walk follows (see
// [checkDeviceNLinks]): an image a cell whose colour operators are off paints
// is reached, and a space such a cell selects is not; an /Indexed whose base
// names a DeviceN entry reaches it.
func TestDeviceNLimitOnEveryLink(t *testing.T) {
	t.Parallel()

	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{
		{
			name: "an image an uncoloured cell paints",
			data: defaultSpacePDF("", "/ColorSpace << "+namesPattern+" >> /Pattern << /P2 5 0 R >>", namesOffFill,
				namesTiling(2, "<< /XObject << /Im0 6 0 R >> >>", "/Im0 Do"), namesImage(namesDeviceN(33))),
			counts: namesLimit(1, 1),
		},
		{
			name: "a space a coloured cell beside an uncoloured one selects",
			data: defaultSpacePDF("", "/ColorSpace << "+namesPattern+" >> /Pattern << /P1 5 0 R /P2 6 0 R >>", namesOnFill+" "+namesOffFill,
				namesTiling(1, "<< /ColorSpace << /CSn "+namesDeviceN(33)+" >> >>", "/CSn cs 1 scn 0 0 5 5 re f"), namesTiling(2, "<< >>", "0 0 5 5 re f")),
			counts: namesLimit(0, 0),
		},
		{
			name: "a form an uncoloured cell paints",
			data: defaultSpacePDF("", "/ColorSpace << "+namesPattern+" >> /Pattern << /P2 5 0 R >>", namesOffFill,
				namesTiling(2, "<< /XObject << /Fm0 6 0 R >> >>", "/Fm0 Do"),
				namesForm("<< /ColorSpace << /CSn "+namesDeviceN(33)+" >> /XObject << /Im0 7 0 R >> >>", "/CSn cs 1 scn "+namesFill+" /Im0 Do"),
				namesImage(namesDeviceN(33))),
			counts: namesLimit(1, 1),
		},
		{
			name:   "an /Indexed base naming a DeviceN entry",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 "+namesDeviceN(33)+" /CS1 [/Indexed /CS0 "+namesIdx+"] >>", "/CS1 cs 0 sc "+namesFill),
			counts: namesLimit(2, 2),
		},
		{
			name: "an image's /Indexed base naming a DeviceN entry",
			data: defaultSpacePDF("", "/ColorSpace << /CS0 "+namesDeviceN(33)+" >> /XObject << /Im0 5 0 R >>", "/Im0 Do",
				namesImage("[/Indexed /CS0 "+namesIdx+"]")),
			counts: namesLimit(1, 1),
		},
	})
}

// TestDrawnStreamsReachOnlyBuiltPatterns pins that the content-stream checks
// reach the cell of a pattern an scn selects only while its stream processes
// its colour operators (see [drawnScan]): an integer past the limit given to
// w in the cell fails the limit when an scn past the Q closing a d1 or a bare
// scn in a form painted in the Pattern space selects it, and nowhere else.
func TestDrawnStreamsReachOnlyBuiltPatterns(t *testing.T) {
	t.Parallel()

	cell := namesTiling(1, "<< >>", namesBigW+" 0 0 5 5 re f")
	glyphFont := namesType3(6, "<< /Pattern << /P1 7 0 R >> >>")
	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{
		{
			name: "an scn past a d1",
			data: defaultSpacePDF("", "/Font << /T3 5 0 R >>", "BT /T3 12 Tf (a) Tj ET",
				glyphFont, defaultStream("", "1000 0 0 0 1 1 d1 /Pattern cs /P1 scn 0 0 1 1 re f"), cell),
			counts: namesIntegerLimit(0),
		},
		{
			name: "an scn past the Q closing a d1",
			data: defaultSpacePDF("", "/Font << /T3 5 0 R >>", "BT /T3 12 Tf (a) Tj ET",
				glyphFont, defaultStream("", "q 1000 0 0 0 1 1 d1 Q /Pattern cs /P1 scn 0 0 1 1 re f"), cell),
			counts: namesIntegerLimit(1),
		},
		{
			name: "an scn in an uncoloured cell",
			data: defaultSpacePDF("", "/ColorSpace << "+namesPattern+" >> /Pattern << /P2 5 0 R >>", namesOffFill,
				namesTiling(2, "<< /Pattern << /P1 6 0 R >> >>", "/Pattern cs /P1 scn 0 0 5 5 re f"), cell),
			counts: namesIntegerLimit(0),
		},
		{
			name: "an scn in a form an uncoloured cell paints",
			data: defaultSpacePDF("", "/ColorSpace << "+namesPattern+" >> /Pattern << /P2 5 0 R >>", namesOffFill,
				namesTiling(2, "<< /XObject << /Fm0 6 0 R >> >>", "/Fm0 Do"),
				namesForm("<< /Pattern << /P1 7 0 R >> >>", "/Pattern cs /P1 scn "+namesFill), cell),
			counts: namesIntegerLimit(0),
		},
		{
			name: "a bare scn in a form painted in the Pattern space",
			data: defaultSpacePDF("", "/XObject << /Fm0 5 0 R >>", "/Pattern cs /Fm0 Do",
				namesForm("<< /Pattern << /P1 6 0 R >> >>", "/P1 scn "+namesFill), cell),
			counts: namesIntegerLimit(1),
		},
	})
}
