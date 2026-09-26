package compliance

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// TestOperatorsReadNamesAgain pins the names veraPDF reads again through the
// objects content-stream operators reach, which the name length limit, ISO
// 19005-1, 6.1.12 t4 and ISO 19005-2, 6.1.13 t4, judges once more each time:
// the colorant names of a Separation or a DeviceN at every link to it (see
// [colorantNameReads]), the /RI and /BM of a graphics state at every gs and
// the /Intent of an image (see [operatorNameLimits]), and the /Intent of an
// inline image (see [inlineIntentLimit]). Every count was measured with
// veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same fixture, and holds the
// failures of the object sweep and of the resources' names beside them.
func TestOperatorsReadNamesAgain(t *testing.T) {
	t.Parallel()

	long := "/" + strings.Repeat("A", 130)
	sep := "[/Separation " + long + " /DeviceCMYK " + namesFn1 + "]"
	fn2 := "<< /FunctionType 2 /Domain [0 1 0 1] /C0 [0 0 0 0] /C1 [1 0 0 0] /N 1 >>"
	image := func(extra string) string {
		return defaultStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8"+extra, "\x00")
	}
	limit := func(want int) []namesCount {
		return []namesCount{{models.PDFA1b, "6.1.12", 4, want}, {models.PDFA2b, "6.1.13", 4, want}}
	}
	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{
		{
			name:   "a direct Separation that cs, scn and f reach",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 "+sep+" >>", "/CS0 cs 1 scn "+namesFill),
			counts: limit(5),
		},
		{
			name:   "an indirect Separation that cs, scn and f reach",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 5 0 R >>", "/CS0 cs 1 scn "+namesFill, sep),
			counts: limit(3),
		},
		{
			name:   "a Separation whose colorant name is an indirect object",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 [/Separation 5 0 R /DeviceCMYK "+namesFn1+"] >>", "/CS0 cs 1 scn "+namesFill, long),
			counts: limit(5),
		},
		{
			name:   "a direct DeviceN that cs, scn and f reach",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 [/DeviceN [/Cyan "+long+"] /DeviceCMYK "+fn2+"] >>", "/CS0 cs 1 1 scn "+namesFill),
			counts: limit(5),
		},
		{
			name: "a Separation of a direct DeviceN's /Colorants",
			data: defaultSpacePDF("", "/ColorSpace << /CS0 [/DeviceN [/Cyan /Spot] /DeviceCMYK "+fn2+" << /Colorants << /Spot "+sep+" >> >>] >>",
				"/CS0 cs 1 1 scn "+namesFill),
			counts: limit(4),
		},
		{
			name:   "an /Indexed space over a direct Separation",
			data:   defaultSpacePDF("", "/ColorSpace << /CS0 [/Indexed "+sep+" 1 <00FF>] >>", "/CS0 cs 0 sc "+namesFill),
			counts: limit(3),
		},
		{
			name:   "an image over a direct Separation painted twice",
			data:   defaultSpacePDF("", "/XObject << /Im0 5 0 R >>", "/Im0 Do /Im0 Do", namesImage(sep)),
			counts: limit(2),
		},
		{
			name: "a form painted twice selecting a direct Separation",
			data: defaultSpacePDF("", "/XObject << /X0 5 0 R >>", "/X0 Do /X0 Do",
				namesForm("<< /ColorSpace << /CS0 "+sep+" >> >>", "/CS0 cs 1 scn "+namesFill)),
			counts: limit(5),
		},
		{
			name: "a Separation a cs past the d1 of a Type 3 glyph selects",
			data: defaultSpacePDF("", "/Font << /F1 5 0 R >> /ColorSpace << /CS0 "+sep+" >>", "BT /F1 10 Tf (a) Tj ET",
				namesType3(6, "<< >>"), defaultStream("", "10 0 0 0 10 10 d1 /CS0 cs 1 scn "+namesFill)),
			counts: limit(2),
		},
		{
			name:   "a direct graphics state's /RI that two gs set",
			data:   defaultSpacePDF("", "/ExtGState << /G0 << /RI "+long+" >> >>", "/G0 gs /G0 gs "+namesFill),
			counts: limit(3),
		},
		{
			name:   "an indirect graphics state's /RI that two gs set",
			data:   defaultSpacePDF("", "/ExtGState << /G0 5 0 R >>", "/G0 gs /G0 gs "+namesFill, "<< /RI "+long+" >>"),
			counts: limit(2),
		},
		{
			name:   "a direct graphics state's /BM that two gs set",
			data:   defaultSpacePDF("", "/ExtGState << /G0 << /BM "+long+" >> >>", "/G0 gs /G0 gs "+namesFill),
			counts: limit(3),
		},
		{
			name:   "a graphics state's /RI that no gs sets",
			data:   defaultSpacePDF("", "/ExtGState << /G0 << /RI "+long+" >> >>", namesFill),
			counts: limit(1),
		},
		{
			name: "a direct graphics state's /RI in a form painted twice",
			data: defaultSpacePDF("", "/XObject << /X0 5 0 R >>", "/X0 Do /X0 Do",
				namesForm("<< /ExtGState << /G0 << /RI "+long+" >> >> >>", "/G0 gs "+namesFill)),
			counts: limit(2),
		},
		{
			name:   "an image's /Intent painted twice",
			data:   defaultSpacePDF("", "/XObject << /Im0 5 0 R >>", "/Im0 Do /Im0 Do", image(" /Intent "+long)),
			counts: limit(2),
		},
		{
			name:   "the /Intent of an image's /SMask",
			data:   defaultSpacePDF("", "/XObject << /Im0 5 0 R >>", "/Im0 Do", image(" /SMask 6 0 R"), image(" /Intent "+long)),
			counts: limit(2),
		},
		{
			name:   "an inline image's /Intent",
			data:   defaultSpacePDF("", "", "BI /W 1 /H 1 /CS /G /BPC 8 /Intent "+long+" ID \x00 EI"),
			counts: limit(2),
		},
	})
}

// TestColorantNameReadsAreBounded pins that one link to a colour space adds
// at most [maxColorantNamesRead] failures, however many long colorants the
// space names, so the failures stay a constant multiple of the links, and that
// a colour space's names are measured once, whatever the count of links.
func TestColorantNameReadsAreBounded(t *testing.T) {
	t.Parallel()

	const names, links = 100_000, 1000
	colorants := make(ast.PdfArray, names)
	for i := range colorants {
		colorants[i] = ast.PdfName(strings.Repeat("A", maxNameBytes+1+i%7))
	}
	node := colorNode{kind: "DeviceN", arr: ast.PdfArray{ast.PdfName("DeviceN"), colorants, ast.PdfName("DeviceCMYK")}}
	reads := newColorantNameReads(string(models.PDFA2b))
	total := 0
	for range links {
		total += len(reads.judge(testCtx(), nopDoc(), node))
	}
	if want := links * maxColorantNamesRead; total != want {
		t.Errorf("colorant name failures = %d, want %d", total, want)
	}
	if len(reads.failures) != 1 {
		t.Errorf("colour spaces measured = %d, want 1", len(reads.failures))
	}
	if got := newColorantNameReads(string(models.PDFA4)).judge(testCtx(), nopDoc(), node); len(got) != 0 {
		t.Errorf("PDF/A-4 colorant name failures = %d, want 0: ISO 19005-4 states no name length limit", len(got))
	}
}
