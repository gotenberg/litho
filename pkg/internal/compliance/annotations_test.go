package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestCheckAnnotColorArrays(t *testing.T) {
	t.Parallel()
	withC := ast.PdfDict{"C": ast.PdfArray{ast.PdfReal(1), ast.PdfReal(0), ast.PdfReal(0)}}
	withIC := ast.PdfDict{"IC": ast.PdfArray{ast.PdfReal(0)}}
	noColor := ast.PdfDict{}

	fires := func(annot ast.PdfDict, outputCS string, hasProfile bool) bool {
		return len(checkAnnotColorArrays(context.Background(), nopDoc(), annot, outputCS, hasProfile, "loc")) > 0
	}

	tests := []struct {
		name       string
		annot      ast.PdfDict
		outputCS   string
		hasProfile bool
		want       bool
	}{
		{"RGB output intent permits /C", withC, "RGB ", true, false},
		{"known non-RGB intent forbids /C", withC, "CMYK", true, true},
		{"known non-RGB intent forbids /IC", withIC, "GRAY", true, true},
		{"unparseable profile present stays lenient", withC, "", true, false},
		{"no output profile forbids /C", withC, "", false, true},
		{"no output profile forbids /IC", withIC, "", false, true},
		{"no colour array never fires", noColor, "", false, false},
		{"no colour array with non-RGB intent never fires", noColor, "CMYK", true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(tc.annot, tc.outputCS, tc.hasProfile); got != tc.want {
				t.Errorf("checkAnnotColorArrays(%v, %q, %v) fired=%v, want %v",
					tc.annot, tc.outputCS, tc.hasProfile, got, tc.want)
			}
		})
	}
}

func TestCheckForbiddenAnnotType(t *testing.T) {
	t.Parallel()
	ctx := testCtx()

	tests := []struct {
		name     string
		subtype  ast.PdfName
		part     int
		level    byte
		wantFail bool
	}{
		// PDF/A-1: forbidden types.
		{"part1 FileAttachment", "FileAttachment", 1, 'b', true},
		{"part1 Sound", "Sound", 1, 'b', true},
		{"part1 Movie", "Movie", 1, 'b', true},
		{"part1 3D", "3D", 1, 'b', true},
		{"part1 RichMedia", "RichMedia", 1, 'b', true},
		{"part1 Text allowed", "Text", 1, 'b', false},
		{"part1 Link allowed", "Link", 1, 'b', false},
		{"part1 Widget allowed", "Widget", 1, 'b', false},
		{"part1 nonstandard", "CustomAnnot", 1, 'b', true},

		// PDF/A-2: forbidden types.
		{"part2 3D", "3D", 2, 'b', true},
		{"part2 RichMedia", "RichMedia", 2, 'b', true},
		{"part2 Sound", "Sound", 2, 'b', true},
		{"part2 Screen", "Screen", 2, 'b', true},
		{"part2 Movie", "Movie", 2, 'b', true},
		{"part2 Text allowed", "Text", 2, 'b', false},
		{"part2 FileAttachment allowed", "FileAttachment", 2, 'b', false},

		// PDF/A-4e: 3D and RichMedia are allowed.
		{"part4e 3D allowed", "3D", 4, 'e', false},
		{"part4e RichMedia allowed", "RichMedia", 4, 'e', false},
		{"part4e Sound forbidden", "Sound", 4, 'e', true},
		{"part4e Movie forbidden", "Movie", 4, 'e', true},
		{"part4e nonstandard", "Bogus", 4, 'e', true},

		// PDF/A-4f: FileAttachment allowed, 3D/RichMedia not.
		{"part4f FileAttachment allowed", "FileAttachment", 4, 'f', false},
		{"part4f 3D forbidden", "3D", 4, 'f', true},
		{"part4f RichMedia forbidden", "RichMedia", 4, 'f', true},
		{"part4f Text allowed", "Text", 4, 'f', false},

		// PDF/A-4 base: most restrictive.
		{"part4 base 3D forbidden", "3D", 4, '4', true},
		{"part4 base FileAttachment forbidden", "FileAttachment", 4, '4', true},
		{"part4 base Text allowed", "Text", 4, '4', false},
		{"part4 base Redact allowed", "Redact", 4, '4', false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			annot := ast.PdfDict{"Subtype": ast.PdfName(tc.subtype)}
			results := checkForbiddenAnnotType(ctx, nopDoc(), annot, tc.subtype, tc.part, tc.level, "test")
			gotFail := len(results) > 0
			if gotFail != tc.wantFail {
				t.Errorf("checkForbiddenAnnotType(%s, part=%d, level=%c): got %d violations, wantFail=%v",
					tc.subtype, tc.part, tc.level, len(results), tc.wantFail)
			}
		})
	}
}

func TestCheckFlagValues1(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flags    int
		wantFail bool
	}{
		{"valid: Print only", annotFlagPrint, false},
		{"missing Print", 0, true},
		{"Print + Hidden", annotFlagPrint | annotFlagHidden, true},
		{"Print + Invisible", annotFlagPrint | annotFlagInvisible, true},
		{"Print + NoView", annotFlagPrint | annotFlagNoView, true},
		{"all bad flags", annotFlagHidden | annotFlagInvisible | annotFlagNoView, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := checkFlagValues1(tc.flags, "test")
			gotFail := len(results) > 0
			if gotFail != tc.wantFail {
				t.Errorf("checkFlagValues1(0x%x): got %d violations, wantFail=%v",
					tc.flags, len(results), tc.wantFail)
			}
		})
	}
}

func TestCheckFlagValues2Plus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flags    int
		wantFail bool
	}{
		{"valid: Print only", annotFlagPrint, false},
		{"missing Print", 0, true},
		{"Print + ToggleNoView", annotFlagPrint | annotFlagToggleNoView, true},
		{"Print + Hidden", annotFlagPrint | annotFlagHidden, true},
		{"all flags set", annotFlagPrint | annotFlagHidden | annotFlagInvisible | annotFlagNoView | annotFlagToggleNoView, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := checkFlagValues2Plus(tc.flags, 2, "test")
			gotFail := len(results) > 0
			if gotFail != tc.wantFail {
				t.Errorf("checkFlagValues2Plus(0x%x): got %d violations, wantFail=%v",
					tc.flags, len(results), tc.wantFail)
			}
		})
	}
}

func TestCheckAnnotOpacity(t *testing.T) {
	t.Parallel()
	ctx := testCtx()

	t.Run("no CA key", func(t *testing.T) {
		t.Parallel()
		annot := ast.PdfDict{}
		results := checkAnnotOpacity(ctx, nopDoc(), annot, "test")
		if len(results) != 0 {
			t.Errorf("expected no violations when /CA absent, got %d", len(results))
		}
	})

	t.Run("CA is 1.0", func(t *testing.T) {
		t.Parallel()
		annot := ast.PdfDict{"CA": ast.PdfReal(1.0)}
		results := checkAnnotOpacity(ctx, nopDoc(), annot, "test")
		if len(results) != 0 {
			t.Errorf("expected no violations for CA=1.0, got %d", len(results))
		}
	})

	t.Run("CA is 0.5", func(t *testing.T) {
		t.Parallel()
		annot := ast.PdfDict{"CA": ast.PdfReal(0.5)}
		results := checkAnnotOpacity(ctx, nopDoc(), annot, "test")
		if len(results) == 0 {
			t.Error("expected violation for CA=0.5")
		}
	})

	t.Run("CA is 0", func(t *testing.T) {
		t.Parallel()
		annot := ast.PdfDict{"CA": ast.PdfReal(0)}
		results := checkAnnotOpacity(ctx, nopDoc(), annot, "test")
		if len(results) == 0 {
			t.Error("expected violation for CA=0")
		}
	})
}

func TestLinkExemptFromAP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		subtype ast.PdfName
		part    int
		want    bool
	}{
		{"Link part 1", "Link", 1, false},
		{"Link part 2", "Link", 2, true},
		{"Link part 3", "Link", 3, true},
		{"Link part 4", "Link", 4, true},
		{"Text part 2", "Text", 2, false},
		{"Widget part 3", "Widget", 3, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := linkExemptFromAP(tc.subtype, tc.part); got != tc.want {
				t.Errorf("linkExemptFromAP(%s, %d) = %v, want %v",
					tc.subtype, tc.part, got, tc.want)
			}
		})
	}
}

func TestApExtraKeys(t *testing.T) {
	t.Parallel()

	stream := ast.PdfIndirectRef{ObjectNum: 5}

	t.Run("only N key", func(t *testing.T) {
		t.Parallel()
		ap := ast.PdfDict{"N": stream}
		got := apExtraKeys(ap)
		if got != "" {
			t.Errorf("expected empty string for only /N, got %q", got)
		}
	})

	t.Run("N plus extras", func(t *testing.T) {
		t.Parallel()
		ap := ast.PdfDict{
			"N": stream,
			"R": stream,
		}
		got := apExtraKeys(ap)
		if got == "" {
			t.Error("expected non-empty string for extra keys")
		}
	})

	// ISO 32000-1, 7.3.7 treats an entry whose value is null as absent, so an
	// extra entry written as null is no extra entry.
	t.Run("an extra entry written as null", func(t *testing.T) {
		t.Parallel()
		ap := ast.PdfDict{
			"N": stream,
			"R": ast.PdfNull{},
		}
		if got := apExtraKeys(ap); got != "" {
			t.Errorf("expected empty string for a null-valued extra, got %q", got)
		}
	})

	t.Run("empty dict", func(t *testing.T) {
		t.Parallel()
		ap := ast.PdfDict{}
		got := apExtraKeys(ap)
		if got != "" {
			t.Errorf("expected empty string for empty dict, got %q", got)
		}
	})
}

func TestForbiddenAnnotTypeRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		part int
		want string
	}{
		{1, Rule_19005_1_6_5_2_t01.Clause},
		{2, Rule_19005_2_6_3_1_t01.Clause},
		{3, Rule_19005_3_6_3_1_t01.Clause},
		{4, Rule_19005_4_6_3_1_t01.Clause},
	}
	for _, tc := range tests {
		got := forbiddenAnnotTypeRule(tc.part)
		if got.Clause != tc.want {
			t.Errorf("forbiddenAnnotTypeRule(%d).Clause = %q, want %q", tc.part, got.Clause, tc.want)
		}
	}
}

// testCtx returns a background context for test use.
func testCtx() context.Context { return context.Background() }

// TestCheckNValueType_ExtraAppearanceKeysEscapeBothTests pins that an
// appearance dictionary carrying more than /N is judged by neither /N-type
// test. ISO 19005-1, 6.5.3 states both t5 and t6 with the same opening escape,
// `AP != "N"`, which holds for any such dictionary: the extra keys are already
// reported by t4. litho applied the escape to the Btn test alone, so a
// non-widget with extra keys and a dictionary /N reported t6 where veraPDF is
// silent.
func TestCheckNValueType_ExtraAppearanceKeysEscapeBothTests(t *testing.T) {
	ctx := context.Background()
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)

	streamN := ast.PdfStream{Dict: ast.PdfDict{"Subtype": ast.PdfName("Form")}}
	dictN := ast.PdfDict{"Off": ast.PdfName("x")}

	cases := []struct {
		name     string
		n        ast.PdfObject
		isWidget bool
		ft       ast.PdfName
		apOnlyN  bool
		wantFire bool
	}{
		{"Btn widget, stream /N, clean dict", streamN, true, "Btn", true, true},
		{"Btn widget, stream /N, extra keys", streamN, true, "Btn", false, false},
		{"non-widget, dict /N, clean dict", dictN, false, "", true, true},
		{"non-widget, dict /N, extra keys", dictN, false, "", false, false},
		{"non-widget, stream /N, clean dict", streamN, false, "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkNValueType(ctx, doc, tc.n, tc.isWidget, tc.ft, 1, "annot", tc.apOnlyN)
			if fired := len(got) > 0; fired != tc.wantFire {
				t.Errorf("fired=%v, want %v: %v", fired, tc.wantFire, got)
			}
		})
	}
}
