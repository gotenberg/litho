package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// calRGBDefault is a device-independent DefaultRGB. It covers page-scope rg
// (ISO 32000-1, 8.6.5.6) but not a Form XObject's own, whose Resources define
// no default of their own (ISO 32000-2, 8.6.5.7).
const calRGBDefault = "[/CalRGB << /WhitePoint [0.9505 1 1.089] >>]"

// subScopePDF builds a one-page document. pageOps is the page content, formOps
// the content of a Form XObject the page may invoke, and defaultCS an optional
// /Resources/ColorSpace entry such as /DefaultRGB.
func subScopePDF(pageOps, formOps, defaultCS string) []byte {
	res := "<< /XObject << /Fm0 5 0 R >>"
	if defaultCS != "" {
		res += " /ColorSpace << " + defaultCS + " 6 0 R >>"
	}
	res += " >>"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + res + " /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pageOps), pageOps),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] /Resources << >> "+
			"/Length %d >>\nstream\n%s\nendstream", len(formOps), formOps),
	}
	if defaultCS != "" {
		objs = append(objs, calRGBDefault)
	}
	return cyclePDF(objs...)
}

// countClauseViolations reports how many violations carry the given clause and test.
// [firesRule2b] answers whether a rule fired at all; catching a clause written
// twice for one usage needs the count.
func countClauseViolations(t *testing.T, data []byte, clause string, test int) int {
	t.Helper()

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}

	n := 0
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == clause && v.Rule.TestNumber == test {
			n++
		}
	}
	return n
}

// TestDeviceRGBSubScopeCountsEachLink pins §6.2.4.3 t2 across every
// combination of page-scope and sub-scope DeviceRGB usage with and without a
// page DefaultRGB.
//
// A page DefaultRGB covers page-scope rg but never a form's own, and each
// uncovered use fails once per operator linking to DeviceRGB: the rg and the
// f after it. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on
// [subScopePDF]: every row fails the test as often as it expects.
func TestDeviceRGBSubScopeCountsEachLink(t *testing.T) {
	t.Parallel()

	const pageRGB = "1 0 0 rg 0 0 20 20 re f"
	const formRGB = "0 0 1 rg 30 0 20 20 re f"
	const invokeForm = "q /Fm0 Do Q"

	for _, tc := range []struct {
		name    string
		pageOps string
		formOps string
		def     string
		want    int
	}{
		{"page rgb, no default", pageRGB, "", "", 2},
		{"form rgb, no default", invokeForm, formRGB, "", 2},
		{"form rgb, page DefaultRGB", invokeForm, formRGB, "/DefaultRGB", 2},
		{"page rgb, page DefaultRGB", pageRGB, "", "/DefaultRGB", 0},
		{"page and form rgb, no default", pageRGB + " " + invokeForm, formRGB, "", 4},
		{"page and form rgb, page DefaultRGB", pageRGB + " " + invokeForm, formRGB, "/DefaultRGB", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := countClauseViolations(t, subScopePDF(tc.pageOps, tc.formOps, tc.def), "6.2.4.3", 2)
			if got != tc.want {
				t.Errorf("§6.2.4.3 t2 fired %d time(s), want %d", got, tc.want)
			}
		})
	}
}

// TestDeviceGraySubScopeCountsEachLink pins the same shape for §6.2.4.3 t4.
//
// The initial DeviceGray of ISO 32000-1 §8.6.8, painted with no colour space
// selected, belongs to the root content stream rather than to the form, and
// only the f painting with it links to it, so it fails once where g and f fail
// twice. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b.
func TestDeviceGraySubScopeCountsEachLink(t *testing.T) {
	t.Parallel()

	const pageGray = "0.5 g 0 0 20 20 re f"
	const formGray = "0.5 g 30 0 20 20 re f"
	const formInitialGray = "0 0 20 20 re f"
	const invokeForm = "q /Fm0 Do Q"

	for _, tc := range []struct {
		name    string
		pageOps string
		formOps string
		want    int
	}{
		{"page gray", pageGray, "", 2},
		{"form gray", invokeForm, formGray, 2},
		{"form initial gray", invokeForm, formInitialGray, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := countClauseViolations(t, subScopePDF(tc.pageOps, tc.formOps, ""), "6.2.4.3", 4)
			if got != tc.want {
				t.Errorf("§6.2.4.3 t4 fired %d time(s), want %d", got, tc.want)
			}
		})
	}
}
