package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// blendingSpaceRule returns the counts of the page blending space rule, ISO
// 19005-2, 6.2.10 t2 and ISO 19005-4, 6.2.9 t2, at PDF/A-2b and PDF/A-4.
func blendingSpaceRule(want int) []namesCount {
	return []namesCount{{models.PDFA2b, "6.2.10", 2, want}, {models.PDFA4, "6.2.9", 2, want}}
}

// TestPageBlendingSpaceAsVeraPDFReadsIt pins both halves of the page blending
// space rule, ISO 19005-2, 6.2.10 t2 and ISO 19005-4, 6.2.9 t2, in a document
// with no output intent: the page uses transparency only through what its
// content draws, never through its own /Group (see
// [checkTransparencyParts2Plus]), and its group's /CS counts only when it
// reads as a colour space with no resources (see [GroupHasBlendingSpace]).
// Every count was measured with veraPDF 1.30 at PDF/A-2b and PDF/A-4.
func TestPageBlendingSpaceAsVeraPDFReadsIt(t *testing.T) {
	t.Parallel()

	const (
		fill   = "0 0 10 10 re f"
		alpha  = "/ExtGState << /GS0 << /ca 0.5 >> >>"
		transp = "/GS0 gs " + fill
	)
	rgb := defaultStream("/N 3", iccHeaderOnly("mntr", "RGB "))
	group := func(cs string) string {
		return "/Group << /S /Transparency /CS " + cs + " >>"
	}
	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{
		{
			name:   "a group with no /CS over opaque content",
			data:   defaultSpacePDF("/Group << /S /Transparency >>", "", fill),
			counts: blendingSpaceRule(0),
		},
		{
			name:   "a group with no /CS over a fill at constant alpha",
			data:   defaultSpacePDF("/Group << /S /Transparency >>", alpha, transp),
			counts: blendingSpaceRule(1),
		},
		{
			name: "a group /CS naming an entry, painting an isolated group",
			data: defaultSpacePDF(group("/CS0"), "/ColorSpace << /CS0 [/ICCBased 5 0 R] >> /XObject << /X0 6 0 R >>", "/X0 Do", rgb,
				defaultStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency /CS [/ICCBased 5 0 R] /I true >>", fill)),
			counts: blendingSpaceRule(1),
		},
		{
			name:   "a group /CS naming an entry over opaque content",
			data:   defaultSpacePDF(group("/CS0"), "/ColorSpace << /CS0 [/ICCBased 5 0 R] >>", fill, rgb),
			counts: blendingSpaceRule(0),
		},
		{
			name:   "a group /S /Foo with /CS /DeviceRGB",
			data:   defaultSpacePDF("/Group << /S /Foo /CS /DeviceRGB >>", alpha, transp),
			counts: blendingSpaceRule(0),
		},
		{
			name:   "a group /CS [/DeviceRGB /Foo]",
			data:   defaultSpacePDF(group("[/DeviceRGB /Foo]"), alpha, transp),
			counts: blendingSpaceRule(1),
		},
		{
			name:   "a group /CS naming a Default colour space",
			data:   defaultSpacePDF(group("/DefaultRGB"), alpha+" /ColorSpace << /DefaultRGB [/ICCBased 5 0 R] >>", transp, rgb),
			counts: blendingSpaceRule(1),
		},
		{
			name:   "a group /CS [/DeviceRGB]",
			data:   defaultSpacePDF(group("[/DeviceRGB]"), alpha, transp),
			counts: blendingSpaceRule(0),
		},
		{
			name:   "a group /CS naming /DeviceRGB through a reference",
			data:   defaultSpacePDF(group("5 0 R"), alpha, transp, "/DeviceRGB"),
			counts: blendingSpaceRule(0),
		},
		{
			name:   "a group /CS [/CalCMYK << >>]",
			data:   defaultSpacePDF(group("[/CalCMYK << >>]"), alpha, transp),
			counts: blendingSpaceRule(0),
		},
		{
			name:   "a group /CS [/ICCBased] over a stream that is no profile",
			data:   defaultSpacePDF(group("[/ICCBased 5 0 R]"), alpha, transp, defaultStream("", "garbage")),
			counts: blendingSpaceRule(0),
		},
	})
}

// TestGroupHasBlendingSpace pins the readings of [GroupHasBlendingSpace] the
// page tests above do not reach: a pattern written as a dictionary, an empty
// array, and a /CS that is absent or resolves to no object.
func TestGroupHasBlendingSpace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc := cycleDoc(t, "<< /Type /Catalog >>", "<< /PatternType 2 >>", "<< /PatternType 3 >>")
	tests := []struct {
		cs   ast.PdfObject
		want bool
	}{
		{ast.PdfName("Pattern"), true},
		{ast.PdfName("CS0"), false},
		{ast.PdfArray{}, false},
		{ast.PdfArray{ast.PdfName("Indexed"), ast.PdfName("CS0"), ast.PdfInt(1), ast.PdfString("")}, true},
		{ast.PdfArray{ast.PdfInt(1), ast.PdfName("DeviceRGB")}, false},
		{ast.PdfIndirectRef{ObjectNum: 2}, true},
		{ast.PdfIndirectRef{ObjectNum: 3}, false},
		{ast.PdfIndirectRef{ObjectNum: 99}, false},
		{ast.PdfNull{}, false},
		{nil, false},
	}
	for _, tc := range tests {
		group := ast.PdfDict{"S": ast.PdfName("Transparency")}
		if tc.cs != nil {
			group["CS"] = tc.cs
		}
		if got := GroupHasBlendingSpace(ctx, doc, group); got != tc.want {
			t.Errorf("/CS %v: GroupHasBlendingSpace = %v, want %v", tc.cs, got, tc.want)
		}
	}
}

// TestAnnotationBlendModeIsTransparencyUnderPDF2Alone pins that an
// annotation's /BM makes its page contain transparency under PDF/A-4 alone:
// ISO 32000-2, 12.5.2 gives an annotation a blend mode, which ISO 32000-1
// does not, and veraPDF reads it under ISO 32000-2 only, as a name
// (GFPDAnnot.parseBM, veraPDF-validation 1.30). Measured with veraPDF 1.30 on
// a page whose one annotation names /BM /Multiply over an opaque appearance,
// with no output intent: ISO 19005-4, 6.2.9 t2 fails, ISO 19005-2, 6.2.10 t2
// passes.
//
// # Corpus evidence
//
//   - verapdf PDF_A-4__veraPDF_test_suite_6-2-9-t02-fail-c.pdf: the file both
//     counts were measured on, which litho failed at PDF/A-2b too.
func TestAnnotationBlendModeIsTransparencyUnderPDF2Alone(t *testing.T) {
	t.Parallel()

	annot := func(bm string) []byte {
		return defaultSpacePDF("/Annots [5 0 R]", "", "0 0 10 10 re f",
			"<< /Type /Annot /Subtype /Circle /Rect [0 0 10 10] /F 4 /BM "+bm+" /AP << /N 6 0 R >> >>",
			namesForm("<< >>", "0 0 10 10 re S"))
	}
	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{
		{name: "an annotation naming /BM /Multiply", data: annot("/Multiply"), counts: blendingSpaceRule(0)[:1]},
		{name: "an annotation naming /BM /Multiply at PDF/A-4", data: annot("/Multiply"), counts: blendingSpaceRule(1)[1:]},
		{name: "an annotation naming /BM [/Multiply]", data: annot("[/Multiply]"), counts: blendingSpaceRule(0)},
		{name: "an annotation naming /BM /Normal", data: annot("/Normal"), counts: blendingSpaceRule(0)},
	})
}
