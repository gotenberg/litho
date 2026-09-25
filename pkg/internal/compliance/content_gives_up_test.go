package compliance

import (
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestUnreadableContainerLeavesTheStreamUnjudged pins that compliance judges
// nothing of a content stream holding an array or a dictionary veraPDF cannot
// read (COSParser.getArray and getDictionary), the operators before it
// included, as veraPDF, which gives such a stream no operator at all
// (GFPDContentStream.parseOperators), judges nothing of it. A dictionary value
// veraPDF reads as an empty entry leaves the stream judged. Every case was
// measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same fixtures,
// which report the rules of the control and nothing more.
func TestUnreadableContainerLeavesTheStreamUnjudged(t *testing.T) {
	t.Parallel()

	const bdc = "/P <</A 1>> BDC EMC\n" + streamEndsProbe
	for _, tc := range []struct {
		name, content, control string
	}{
		{"a keyword in an array after the probe", streamEndsProbe + "[1 yy] TJ\n", ""},
		{"a keyword in an array before the probe", "[1 yy] TJ\n" + streamEndsProbe, ""},
		{"an exponent in an array", streamEndsProbe + "[1e2] TJ\n", ""},
		{"a parenthesis in an array", streamEndsProbe + "[)] TJ\n", ""},
		{"a keyword after an integer value", "/P <</A 1 xx>> BDC EMC\n" + streamEndsProbe, ""},
		{"a key with no value", "/P <</A>> BDC EMC\n" + streamEndsProbe, ""},
		{"a greater-than sign alone", "/P <</A >x>> BDC EMC\n" + streamEndsProbe, ""},
		{"an exponent in an inline image", streamEndsProbe + "q BI /W 1 /H 1 /BPC 8 /CS /G /Foo 1.5e1 ID \x80 EI Q\n", ""},
		{"a keyword value", "/P <</A xx>> BDC EMC\n" + streamEndsProbe, bdc},
		{"a parenthesis value", "/P <</A )>> BDC EMC\n" + streamEndsProbe, bdc},
		{"a bracket value", "/P <</A ]>> BDC EMC\n" + streamEndsProbe, bdc},
		{"integers read ahead", "/P <</A 1 2>> BDC EMC\n" + streamEndsProbe, bdc},
		{"integers handed to the next key", "/P <</A 1 2 /B>> BDC EMC\n" + streamEndsProbe, bdc},
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
			// where veraPDF gives up on the parts as one stream, so the
			// integer of the first part still fails ISO 19005-1, 6.1.12 t1 and
			// ISO 19005-2, 6.1.13 t1 here, and veraPDF 1.30 reports neither.
			got := withoutRules(streamEndsRules(t, profile, "[4 0 R 8 0 R]", exponentStream("", streamEndsProbe), exponentStream("", "[xx] TJ\n")), "6.1.12 t1", "6.1.13 t1")
			if want := pageContent(t, profile, ""); !reflect.DeepEqual(got, want) {
				t.Errorf("an unreadable array in the second part reports %v, want %v", got, want)
			}
		})
		t.Run("a form XObject "+string(profile), func(t *testing.T) {
			t.Parallel()
			got := formRules(t, profile, "[xx] TJ\n0 0 1 rg 0 0 5 5 re f zz\n")
			if want := pageContent(t, profile, streamEndsProbe); !reflect.DeepEqual(got, want) {
				t.Errorf("a form holding an unreadable array reports %v, want the %v of the page alone", got, want)
			}
			if drawn := formRules(t, profile, "0 0 1 rg 0 0 5 5 re f zz\n"); reflect.DeepEqual(drawn, got) {
				t.Fatalf("the form reports %v with its array and without it", drawn)
			}
		})
	}
}

// formRules validates at profile a one-page file whose page draws
// [streamEndsProbe] and then a form XObject holding form, with the resources
// of [streamEndsRules], and returns the rules [rulesOf] returns.
func formRules(t *testing.T, profile models.PDFAProfile, form string) []string {
	t.Helper()
	return rulesOf(t, profile, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> /ExtGState << /G1 6 0 R >> /XObject << /X1 7 0 R >> >> /Contents 4 0 R >>",
		exponentStream("", streamEndsProbe+"/X1 Do\n"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /ExtGState /CA 0.5 /ca 0.5 >>",
		exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >>", form),
	})
}
