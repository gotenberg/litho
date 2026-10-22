package pdf

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// TestConvertColors_ReportsProfileProvenance pins which profiles the report
// says the conversion read, and where the rendering intent fell back: a
// profile holding only perceptual tables, read under the default relative
// colorimetric intent, is read through its perceptual one.
func TestConvertColors_ReportsProfileProvenance(t *testing.T) {
	t.Parallel()
	lutRGB, err := os.ReadFile("../internal/graphics/icc/testdata/v4-rgb-lut-xyz.icc")
	if err != nil {
		t.Fatal(err)
	}
	cmyk := icc.CMYKPrinterProfile()
	withIntents := recolorPDF("/OutputIntents [5 0 R 6 0 R]",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) /DestOutputProfile 8 0 R >>",
		"<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (SWOP) /DestOutputProfile 7 0 R >>",
		colorStream("/N 4", string(cmyk)),
		colorStream("/N 3", string(icc.SRGBProfile())),
	)

	for _, tc := range []struct {
		name      string
		src       []byte
		target    ColorTarget
		opts      []ColorOption
		profile   string
		sources   map[string][]string
		fallbacks []IntentFallback
	}{
		{
			name:    "built-in CMYK",
			src:     recolorPDF(""),
			target:  CMYKColorTarget(),
			profile: ProfileSourceBuiltin,
			sources: map[string][]string{"DeviceRGB": {ProfileSourceBuiltin}},
		},
		{
			name:    "output intent",
			src:     withIntents,
			target:  OutputIntentColorTarget(),
			profile: ProfileSourceOutputIntent,
			sources: map[string][]string{"DeviceRGB": {ProfileSourceOutputIntent}},
		},
		{
			name:    "caller profile without the intent's table",
			src:     recolorPDF(""),
			target:  ICCColorTarget(lutRGB),
			profile: ProfileSourceCaller,
			sources: map[string][]string{"DeviceRGB": {ProfileSourceBuiltin}},
			fallbacks: []IntentFallback{
				{Target: true, Components: 3, Profile: ProfileSourceCaller, Table: "perceptual"},
			},
		},
		{
			name:    "caller profile under its own intent",
			src:     recolorPDF(""),
			target:  ICCColorTarget(lutRGB),
			opts:    []ColorOption{WithColorIntent(IntentPerceptual)},
			profile: ProfileSourceCaller,
			sources: map[string][]string{"DeviceRGB": {ProfileSourceBuiltin}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			report, _ := convertBuiltin(t, tc.src, tc.target, tc.opts...)
			if report.TargetProfile != tc.profile {
				t.Errorf("TargetProfile = %q, want %q", report.TargetProfile, tc.profile)
			}
			if !reflect.DeepEqual(report.SourceProfiles, tc.sources) {
				t.Errorf("SourceProfiles = %v, want %v", report.SourceProfiles, tc.sources)
			}
			if !reflect.DeepEqual(report.IntentFallbacks, tc.fallbacks) {
				t.Errorf("IntentFallbacks = %+v, want %+v", report.IntentFallbacks, tc.fallbacks)
			}
		})
	}
}

// TestConvertColors_ReportsTheCallerCMYKProfile pins that the profile
// WithDefaultCMYKProfile gives is reported as the caller's, as the target and
// as the reading of DeviceCMYK.
func TestConvertColors_ReportsTheCallerCMYKProfile(t *testing.T) {
	t.Parallel()
	src := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R >>",
		colorStream("", "0 1 0 0 k 0 0 50 50 re f 1 0 0 rg 50 50 50 50 re f"),
	})
	w := NewWriter(openColorPDF(t, src, WithDefaultCMYKProfile(icc.CMYKPrinterProfile())))
	t.Cleanup(func() { _ = w.Close() })
	report, err := w.ConvertColors(context.Background(), RGBColorTarget())
	if err != nil {
		t.Fatal(err)
	}
	if report.TargetProfile != ProfileSourceBuiltin {
		t.Errorf("TargetProfile = %q, want builtin", report.TargetProfile)
	}
	want := map[string][]string{"DeviceCMYK": {ProfileSourceCaller}}
	if !reflect.DeepEqual(report.SourceProfiles, want) {
		t.Errorf("SourceProfiles = %v, want %v", report.SourceProfiles, want)
	}
}
