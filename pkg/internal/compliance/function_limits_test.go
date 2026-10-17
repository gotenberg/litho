package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestFunctionLimits pins which type 4 functions the implementation limits
// judge, ISO 19005-1, 6.1.12 t1 and t2 and ISO 19005-2, 6.1.13 t1: those
// veraPDF's model reaches, once each. Every count was measured with veraPDF
// 1.30.2 at PDF/A-1b and PDF/A-2b on these fixtures.
func TestFunctionLimits(t *testing.T) {
	t.Parallel()

	const program = "{ 2147483648 pop 40000.5 pop }"
	stream := func(dict, data string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
	}
	fn := stream("/FunctionType 4 /Domain [0 1] /Range [0 1]", program)
	build := func(res, content string, extra ...string) []string {
		objs := []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources " + res + " /Contents 4 0 R >>",
			stream("", content),
			fn,
		}
		return append(objs, extra...)
	}
	sep := "[/Separation /Spot /DeviceGray 5 0 R]"
	fill := "/CS1 cs 1 scn 0 0 10 10 re f"
	for _, tc := range []struct {
		name   string
		objs   []string
		int1b  int
		real1b int
		int2b  int
	}{
		{"Separation tint transform", build("<< /ColorSpace << /CS1 "+sep+" >> >>", fill), 1, 1, 1},
		{"two Separations sharing it", build("<< /ColorSpace << /CS1 "+sep+" /CS2 [/Separation /Other /DeviceGray 5 0 R] >> >>",
			fill+" /CS2 cs 1 scn 0 0 10 10 re f"), 1, 1, 1},
		{"DeviceN tint transform", build("<< /ColorSpace << /CS1 [/DeviceN [/Cyan] /DeviceGray 5 0 R] >> >>", fill), 1, 1, 1},
		{"Indexed base", build("<< /ColorSpace << /CS1 [/Indexed "+sep+" 0 <00>] >> >>", "/CS1 cs 0 scn 0 0 10 10 re f"), 1, 1, 1},
		{"stitching function", build("<< /ColorSpace << /CS1 [/Separation /Spot /DeviceGray "+
			"<< /FunctionType 3 /Domain [0 1] /Functions [5 0 R] /Bounds [] /Encode [0 1] >>] >> >>", fill), 1, 1, 1},
		{"TR2 array", build("<< /ExtGState << /GS1 << /Type /ExtGState /TR2 [5 0 R 5 0 R 5 0 R 5 0 R] >> >> >>", "/GS1 gs"), 1, 1, 1},
		{"halftone transfer function", build("<< /ExtGState << /GS1 << /Type /ExtGState /HT << /Type /Halftone /HalftoneType 1 "+
			"/Frequency 60 /Angle 45 /SpotFunction /Round /TransferFunction 5 0 R >> >> >> >>", "/GS1 gs"), 1, 1, 1},
		{"type 5 halftone component", build("<< /ExtGState << /GS1 << /Type /ExtGState /HT << /Type /Halftone /HalftoneType 5 "+
			"/Default << /HalftoneType 1 /Frequency 60 /Angle 45 /SpotFunction /Round >> "+
			"/Spot << /HalftoneType 1 /Frequency 60 /Angle 45 /SpotFunction /Round /TransferFunction 5 0 R >> >> >> >> >>", "/GS1 gs"), 1, 1, 1},
		{"tiling pattern cell", build("<< /Pattern << /P1 6 0 R >> >>", "/Pattern cs /P1 scn 0 0 100 100 re f",
			stream("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 "+
				"/Resources << /ColorSpace << /CS1 "+sep+" >> >>", "/CS1 cs 1 scn 0 0 5 5 re f")), 1, 1, 1},
		{"Separation nothing selects", build("<< /ColorSpace << /CS1 "+sep+" >> >>", "0 g 0 0 10 10 re f"), 0, 0, 0},
		{"graphics state nothing sets", build("<< /ExtGState << /GS1 << /Type /ExtGState /TR2 5 0 R >> >> >>", "0 g"), 0, 0, 0},
		{"shading function", build("<< /Shading << /Sh1 << /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 1 0] /Function 5 0 R >> >> >>",
			"/Sh1 sh"), 0, 0, 0},
	} {
		for _, p := range []struct {
			profile   models.PDFAProfile
			intRule   Rule
			realRule  Rule
			wantInt   int
			wantReals int
		}{
			{models.PDFA1b, Rule_19005_1_6_1_12_t01, Rule_19005_1_6_1_12_t02, tc.int1b, tc.real1b},
			{models.PDFA2b, Rule_19005_2_6_1_13_t01, Rule_19005_2_6_1_13_t02, tc.int2b, 0},
		} {
			t.Run(string(p.profile)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, tc.objs...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.intRule); got != p.wantInt {
					t.Errorf("integer limit fired %d times, want %d", got, p.wantInt)
				}
				if got := countRuleErrs(t, viols, p.realRule); got != p.wantReals {
					t.Errorf("real limit fired %d times, want %d", got, p.wantReals)
				}
			})
		}
	}
}

// TestType4ProgramNumbers pins which tokens of a type 4 program count as
// numbers: those inside brackets do, an integer past the 64-bit range is an
// integer, and strings, names and "1.0e5", which PDF number syntax does not
// spell (ISO 32000-1, 7.3.3), do not.
func TestType4ProgramNumbers(t *testing.T) {
	t.Parallel()

	const program = "{ [ 2147483648 ] pop (x) pop /n pop 99999999999999999999999 pop 1.0e5 pop }"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /ColorSpace << /CS1 [/Separation /Spot /DeviceGray 5 0 R] >> >> /Contents 4 0 R >>",
		"<< /Length 28 >>\nstream\n/CS1 cs 1 scn 0 0 10 10 re f\nendstream",
		fmt.Sprintf("<< /FunctionType 4 /Domain [0 1] /Range [0 1] /Length %d >>\nstream\n%s\nendstream", len(program), program),
	}
	doc, _ := objsDocPDF(t, objs...)
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	if got := countRuleErrs(t, viols, Rule_19005_1_6_1_12_t01); got != 2 {
		t.Errorf("integer limit fired %d times, want 2", got)
	}
	if got := countRuleErrs(t, viols, Rule_19005_1_6_1_12_t02); got != 0 {
		t.Errorf("real limit fired %d times, want 0", got)
	}
}
