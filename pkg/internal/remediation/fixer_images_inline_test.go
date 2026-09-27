package remediation

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// interpolatedInline is an inline image whose dictionary asks for
// interpolation with key, /I or /Interpolate, and whose data is
// [spliceImageData].
func interpolatedInline(key string) string {
	return "BI /W 4 /H 1 /BPC 8 /CS /G " + key + " true" + spliceImageData
}

// interpolatedStreams are the objects of [inlineInterpolationPDF] that hold an
// inline image asking for interpolation, and the key each one asks with.
var interpolatedStreams = []struct {
	name   string
	objNum int
	key    string
}{
	{"page content", 4, "/I"},
	{"form XObject", 5, "/Interpolate"},
	{"Type 3 glyph", 10, "/I"},
	{"tiling pattern cell", 7, "/I"},
	{"annotation appearance", 9, "/Interpolate"},
}

// inlineInterpolationPDF builds a one-page document with an inline image
// asking for interpolation in every kind of stream compliance judges the image
// rules in: the page's content, the form X0 it draws, the glyph a of the Type
// 3 font T3 it shows, the cell of the tiling pattern P0 it fills with, and the
// normal appearance of a Square annotation.
func inlineInterpolationPDF() []byte {
	img := func(key string) string { return "q 10 0 0 10 0 0 cm " + interpolatedInline(key) + " Q" }
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Annots [8 0 R]"+
			" /Resources << /XObject << /X0 5 0 R >> /Font << /T3 6 0 R >> /Pattern << /P0 7 0 R >> >> >>",
		streamBody("", img("/I")+" /X0 Do q BT /T3 12 Tf 10 10 Td (a) Tj ET Q q /Pattern cs /P0 scn 0 0 50 50 re f Q"),
		streamBody("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", img("/Interpolate")),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0]"+
			" /CharProcs << /a 10 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >>"+
			" /FirstChar 97 /LastChar 97 /Widths [1000] /Resources << >> >>",
		streamBody("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10]"+
			" /XStep 10 /YStep 10 /Resources << >>", img("/I")),
		"<< /Type /Annot /Subtype /Square /Rect [60 10 80 30] /F 4 /AP << /N 9 0 R >> >>",
		streamBody("/BBox [0 0 20 20]", img("/Interpolate")),
		streamBody("", "1000 0 d0 "+img("/I")),
	)
}

// inlineInterpolationRules are the rules an inline image asking for
// interpolation breaks, one for each PDF/A part.
var inlineInterpolationRules = []struct {
	profile models.PDFAProfile
	rule    compliance.Rule
}{
	{models.PDFA1b, compliance.Rule_19005_1_6_2_4_t03},
	{models.PDFA2b, compliance.Rule_19005_2_6_2_8_t03},
	{models.PDFA3b, compliance.Rule_19005_3_6_2_8_t03},
	{models.PDFA4, compliance.Rule_19005_4_6_2_7_1_t03},
}

// TestFixImageIssues_RepairsInlineImageInterpolation pins that the images
// fixer answers the finding for an inline image whose /I or /Interpolate is
// true in every stream compliance judges it in, carrying the image data
// across byte for byte.
//
// ISO 19005-1, 6.2.4, ISO 19005-2/3, 6.2.8 and ISO 19005-4, 6.2.7.1 hold
// every image dictionary to an /Interpolate of false, and an inline image
// writes it /I (ISO 32000-1, 8.9.7, Table 93). The fixer repaired image
// XObjects alone, so every such image kept its finding through a remediation,
// on litho and veraPDF alike.
func TestFixImageIssues_RepairsInlineImageInterpolation(t *testing.T) {
	t.Parallel()

	for _, tc := range inlineInterpolationRules {
		t.Run(string(tc.profile), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, alloc := newTestDoc(t, inlineInterpolationPDF())
			errs, err := compliance.ValidatePDFA(ctx, doc, tc.profile)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if n := len(failures(toViolations(errs), tc.rule)); n != len(interpolatedStreams) {
				t.Fatalf("control: the source fails %s t%d %d times, want %d", tc.rule.Clause, tc.rule.TestNumber, n, len(interpolatedStreams))
			}

			fixes, err := fixImageIssues(ctx, alloc, doc, Options{}, nil, string(tc.profile))
			if err != nil {
				t.Fatalf("fixImageIssues: %v", err)
			}
			if len(fixes) != 1 || !strings.Contains(fixes[0], "5 inline image") {
				t.Errorf("fixes = %q, want the 5 inline images reported", fixes)
			}
			for _, s := range interpolatedStreams {
				src := interpolatedInline(s.key)
				body := pendingStreamBytes(t, alloc, s.objNum)
				ops := validationOps(t, src, body)
				if !bytes.Contains(body, []byte(s.key+" false"+spliceImageData)) {
					t.Errorf("%s: want %s respelled false in place, got %q", s.name, s.key, body)
				}
				for _, op := range ops {
					if op.Operator == "BI" && op.Operands[0].(ast.PdfDict)[ast.PdfName(strings.TrimPrefix(s.key, "/"))] != ast.PdfBool(false) {
						t.Errorf("%s: validation reads the dictionary %v", s.name, op.Operands[0])
					}
				}
			}

			after, _ := revalidatePDFA(t, doc, alloc, tc.profile)
			for _, v := range failures(after, tc.rule) {
				t.Errorf("the output still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
			}
		})
	}
}

// TestFixImageIssues_LeavesAnImageTheSpliceCannotRead pins that an inline
// image whose dictionary the splice cannot read keeps its bytes, its finding a
// residual, while the image beside it is repaired.
//
// The first image's /Decode runs past [maxInlineImageDictTokens], so the walk
// cannot find the ID that ends its dictionary and leaves it as written.
func TestFixImageIssues_LeavesAnImageTheSpliceCannotRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	long := "BI /W 4 /H 1 /BPC 8 /CS /G /I true /Decode [" + strings.Repeat("0 1 ", maxInlineImageDictTokens) + "]" + spliceImageData
	data := "q " + long + " Q q " + interpolatedInline("/I") + " Q"
	doc, alloc := newTestDoc(t, inlineSplicePage(data))

	fixes, err := fixImageIssues(ctx, alloc, doc, Options{}, nil, string(models.PDFA2b))
	if err != nil {
		t.Fatalf("fixImageIssues: %v", err)
	}
	if len(fixes) != 1 || !strings.Contains(fixes[0], "1 inline image") {
		t.Errorf("fixes = %q, want the one image it can read reported", fixes)
	}
	body := pendingStreamBytes(t, alloc, 4)
	if want := "q " + long + " Q q " + strings.Replace(interpolatedInline("/I"), "true", "false", 1) + " Q"; string(body) != want {
		t.Errorf("page = %q, want only the second image respelled", body)
	}

	after, _ := revalidatePDFA(t, doc, alloc, models.PDFA2b)
	if n := len(failures(after, compliance.Rule_19005_2_6_2_8_t03)); n != 1 {
		t.Errorf("the output fails 6.2.8 t3 %d times, want the one residual", n)
	}
}

// TestRespellInlineImageInterpolation_LeavesWhatItMisreads pins the read-back
// of the respelling: a true written after /I where no value of /I stands is
// no interpolation flag, and a dictionary that states /I twice holds the last
// one, so the respelling leaves each stream as written.
func TestRespellInlineImageInterpolation_LeavesWhatItMisreads(t *testing.T) {
	t.Parallel()
	for _, dict := range []string{
		// /I is the value of /CS here, the Indexed abbreviation, and the true
		// after it stands where a key does.
		"/W 4 /H 1 /BPC 8 /CS /I true",
		"/W 4 /H 1 /BPC 8 /CS /G /I true /I false",
	} {
		data := "q BI " + dict + spliceImageData + " Q"
		got, n := respellInlineImageInterpolation(context.Background(), []byte(data))
		if string(got) != data || n != 0 {
			t.Errorf("respelled %q to %q (%d), want it left as written", data, got, n)
		}
	}
}

// TestRespellInlineImageInterpolation_RespellsBothKeys pins that an image
// whose dictionary asks for interpolation under both keys has both respelled,
// and counts as one image.
func TestRespellInlineImageInterpolation_RespellsBothKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := "q BI /W 4 /H 1 /BPC 8 /CS /G /Interpolate true /I true" + spliceImageData + " Q"
	got, n := respellInlineImageInterpolation(ctx, []byte(data))
	if want := strings.ReplaceAll(data, "true", "false"); string(got) != want || n != 1 {
		t.Errorf("got %q (%d), want %q (1)", got, n, want)
	}
	ops, err := content.ParseStreamData(ctx, got)
	if err != nil || len(ops) != 3 {
		t.Fatalf("validation reads %d operations (%v), want 3", len(ops), err)
	}
}
