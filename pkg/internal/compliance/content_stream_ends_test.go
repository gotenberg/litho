package compliance

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// streamEndsProbe paints with the resources of [streamEndsRules] and breaks a
// rule of each kind a content stream answers to: a font that is not embedded,
// a font name the resources do not define, an ExtGState whose CA and ca are
// below 1, DeviceRGB and DeviceGray with no output intent, an integer past the
// implementation limit, and an undefined operator.
const streamEndsProbe = "BT /F1 12 Tf (a) Tj /F9 10 Tf ET /G1 gs 1 0 0 rg 3000000000 0 m 0 0 10 10 re f xx\n"

// streamEndsRules validates at profile a one-page file whose page draws
// contents, a /Contents value, object 4 being obj4 and the objects from 8 on
// extra, and returns the clause and test of every violation, sorted. The
// page's resources hold a Helvetica font it does not embed, a transparent
// ExtGState and a form XObject drawing in DeviceRGB with an undefined
// operator.
func streamEndsRules(t *testing.T, profile models.PDFAProfile, contents, obj4 string, extra ...string) []string {
	t.Helper()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> /ExtGState << /G1 6 0 R >> /XObject << /X1 7 0 R >> >> /Contents " + contents + " >>",
		obj4,
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /ExtGState /CA 0.5 /ca 0.5 >>",
		exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >>", "0 0 1 rg 0 0 5 5 re f zz\n"),
	}
	return rulesOf(t, profile, append(objs, extra...))
}

// rulesOf validates at profile the file whose objects are objs, the first one
// the catalog, and returns the clause and test of every violation, sorted.
func rulesOf(t *testing.T, profile models.PDFAProfile, objs []string) []string {
	t.Helper()
	doc, _ := objsDocPDF(t, objs...)
	viols, err := ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	var rules []string
	for _, e := range viols {
		if v, ok := e.(Violation); ok {
			rules = append(rules, fmt.Sprintf("%s t%d", v.Rule.Clause, v.Rule.TestNumber))
		}
	}
	sort.Strings(rules)
	return rules
}

// pageContent returns the rules [streamEndsRules] reports for a page whose
// one content stream holds data.
func pageContent(t *testing.T, profile models.PDFAProfile, data string) []string {
	t.Helper()
	return streamEndsRules(t, profile, "4 0 R", exponentStream("", data))
}

// TestStrayArrayCloseEndsTheJudgedContent pins that compliance judges a
// content stream as far as veraPDF reads it: to a "]" where an operand or an
// operator stands, which ends veraPDF's reading (PDFStreamParser.nextOperator).
// Nothing drawn after it answers to a rule, the operators, colour spaces,
// fonts, resource names, ExtGState and form XObjects alike, and a "]" in one
// stream of a /Contents array hides the streams after it. Every case was
// measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same fixtures,
// which report the rules of the control and nothing more.
func TestStrayArrayCloseEndsTheJudgedContent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, content, control string
	}{
		{"after the probe", streamEndsProbe + "] yy\n", streamEndsProbe},
		{"before the probe", "] " + streamEndsProbe, ""},
		{"between operands", "1 2 ] 3 m\n" + streamEndsProbe, ""},
		{"against an operator", streamEndsProbe + "yy] zz\n", streamEndsProbe + "yy\n"},
		{"before a form", streamEndsProbe + "] /X1 Do\n", streamEndsProbe},
		{"after a form", "/X1 Do ] " + streamEndsProbe, "/X1 Do\n"},
	} {
		for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b} {
			t.Run(tc.name+" "+string(profile), func(t *testing.T) {
				t.Parallel()
				got, want := pageContent(t, profile, tc.content), pageContent(t, profile, tc.control)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%q reports %v, want the %v of %q", tc.content, got, want, tc.control)
				}
			})
		}
	}

	for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b} {
		t.Run("a /Contents array "+string(profile), func(t *testing.T) {
			t.Parallel()
			// TODO: [per-part limits] the implementation-limit check parses
			// each part of a /Contents array alone (checkContentStreamLimits),
			// where veraPDF reads the parts as one stream, so the integer of
			// the second part still fails ISO 19005-1, 6.1.12 t1 and ISO
			// 19005-2, 6.1.13 t1 here, and veraPDF 1.30 reports neither.
			got := withoutRules(streamEndsRules(t, profile, "[4 0 R 8 0 R]", exponentStream("", "xx ]"), exponentStream("", streamEndsProbe)), "6.1.12 t1", "6.1.13 t1")
			want := pageContent(t, profile, "xx\n")
			if !reflect.DeepEqual(got, want) {
				t.Errorf("a stray ] in the first part reports %v, want %v", got, want)
			}
			if probe := pageContent(t, profile, streamEndsProbe); reflect.DeepEqual(probe, want) {
				t.Fatalf("the probe reports %v, the rules of a page with no probe", probe)
			}
		})
	}
}

// withoutRules returns rules without the ones drop names.
func withoutRules(rules []string, drop ...string) []string {
	var out []string
	for _, r := range rules {
		if !slices.Contains(drop, r) {
			out = append(out, r)
		}
	}
	return out
}
