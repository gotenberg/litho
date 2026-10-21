package pdf

import (
	"bytes"
	"context"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// pdfxIntentPDF paints DeviceRGB under a GTS_PDFX intent whose destination
// profile is a CMYK profile other than litho's built-in one, written in the
// catalog's array or, when indirect, as an object of its own.
func pdfxIntentPDF(t *testing.T, indirect bool) []byte {
	t.Helper()
	profile, err := os.ReadFile("../internal/graphics/icc/testdata/v4-cmyk-lut.icc")
	if err != nil {
		t.Fatal(err)
	}
	intent := "<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (FOGRA39) /DestOutputProfile 5 0 R >>"
	if indirect {
		return recolorPDF("/OutputIntents [6 0 R]", colorStream("/N 4", string(profile)), intent)
	}
	return recolorPDF("/OutputIntents ["+intent+"]", colorStream("/N 4", string(profile)))
}

// TestConvertColors_RefusesAConflictingIntent pins the refusal of a CMYK
// target under a kept PDF/X intent over another CMYK profile: it names the
// printing condition and leaves the Writer as it was.
func TestConvertColors_RefusesAConflictingIntent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := pdfxIntentPDF(t, false)
	for _, target := range []ColorTarget{CMYKColorTarget(), ICCColorTarget(icc.CMYKPrinterProfile())} {
		w := NewWriter(openColorPDF(t, src))
		t.Cleanup(func() { _ = w.Close() })
		_, err := w.ConvertColors(ctx, target)
		if !errors.Is(err, ErrOutputIntentConflict) || !strings.Contains(err.Error(), "FOGRA39") {
			t.Fatalf("err = %v, want ErrOutputIntentConflict naming FOGRA39", err)
		}
		if w.forceRewrite || w.rewriteOverrides != nil {
			t.Error("a refused conversion changed the Writer")
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out.Bytes(), src) {
			t.Error("a refused conversion changed the output")
		}
	}
	// The intent's own profile, and an RGB target, which DeviceCMYK's
	// meaning does not concern, convert.
	for _, target := range []ColorTarget{OutputIntentColorTarget(), RGBColorTarget()} {
		w := NewWriter(openColorPDF(t, src))
		t.Cleanup(func() { _ = w.Close() })
		if _, err := w.ConvertColors(ctx, target); err != nil {
			t.Errorf("%+v: %v", target, err)
		}
	}
}

// TestConvertColors_ConflictingVersion5Intent refuses a CMYK target under a
// kept PDF/X intent over a version 5 CMYK profile too: litho converts no
// colour through an ICC.2 profile, yet the intent still names another
// profile than the target's (ISO 19005-2, 6.2.3 t2).
func TestConvertColors_ConflictingVersion5Intent(t *testing.T) {
	t.Parallel()
	profile, err := os.ReadFile("../internal/graphics/icc/testdata/v4-cmyk-lut.icc")
	if err != nil {
		t.Fatal(err)
	}
	profile[8] = 5
	intent := "<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (FOGRA39) /DestOutputProfile 5 0 R >>"
	src := recolorPDF("/OutputIntents ["+intent+"]", colorStream("/N 4", string(profile)))
	w := NewWriter(openColorPDF(t, src))
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.ConvertColors(context.Background(), CMYKColorTarget()); !errors.Is(err, ErrOutputIntentConflict) {
		t.Fatalf("err = %v, want ErrOutputIntentConflict", err)
	}
}

// TestConvertColors_ReplacedOutputIntents points the PDF/X intent at the
// target profile, the one object the added PDF/A intent names too, keeping
// its subtype and printing condition.
func TestConvertColors_ReplacedOutputIntents(t *testing.T) {
	t.Parallel()
	for _, indirect := range []bool{false, true} {
		report, out := convertBuiltin(t, pdfxIntentPDF(t, indirect), CMYKColorTarget(), WithReplacedOutputIntents())
		if !slices.Equal(report.ReplacedOutputIntents, []string{"FOGRA39"}) || len(report.KeptOutputIntents) != 0 || report.OutputIntent != "added" {
			t.Errorf("indirect %v: report = %+v", indirect, report)
		}
		intents := outputIntents(t, out)
		if len(intents) != 2 || intents[0].subtype != "GTS_PDFX" || intents[0].condition != "FOGRA39" || intents[1].subtype != "GTS_PDFA1" {
			t.Fatalf("indirect %v: intents = %+v", indirect, intents)
		}
		for _, in := range intents {
			if !bytes.Equal(in.profile, icc.CMYKPrinterProfile()) {
				t.Errorf("indirect %v: the %s intent keeps another profile", indirect, in.subtype)
			}
		}
		refs := regexp.MustCompile(`/DestOutputProfile (\d+) 0 R`).FindAllSubmatch(out, -1)
		if len(refs) != 2 || !bytes.Equal(refs[0][1], refs[1][1]) {
			t.Errorf("indirect %v: the intents name profiles %q, want one object", indirect, refs)
		}
	}
}
