package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestColorScope_GroupCoveredByDefaultSpace pins that a transparency group's
// device blending colour space is resolved through the resources of the object
// that carries the group, so a matching Default colour space there covers it
// (ISO 32000-1, 8.6.5.6; ISO 19005-2, 6.2.4.3): a page's own or inherited
// resources for a page group, the form's own resources for a form group. The
// page's defaults do not cover a form's group, even when the form has no
// /Resources. Every case was measured against veraPDF.
//
// # Corpus evidence
//
//   - pdfjs bug1019475_1.pdf: gives its page a DeviceRGB group beside a
//     DefaultRGB.
func TestColorScope_GroupCoveredByDefaultSpace(t *testing.T) {
	t.Parallel()

	const icc = "<< /N 3 /Length 4 >>\nstream\nicc.\nendstream"
	page := func(group, resources, inherited string) []byte {
		pages := "<< /Type /Pages /Kids [3 0 R] /Count 1" + inherited + " >>"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			pages,
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Group << /S /Transparency /CS /"+group+" >>"+
				resources+" /Contents 4 0 R >>",
			"<< /Length 18 >>\nstream\nq 1 0 0 1 0 0 cm Q\nendstream",
			icc,
		)
	}
	form := func(pageDefault, formResources string) []byte {
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /XObject << /Fm0 6 0 R >>"+pageDefault+
				" >> /Contents 4 0 R >>",
			"<< /Length 11 >>\nstream\nq /Fm0 Do Q\nendstream",
			icc,
			"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency /CS /DeviceRGB >>"+
				formResources+" /Length 12 >>\nstream\n0 0 5 5 re n\nendstream",
		)
	}
	const (
		defaultRGB  = " /ColorSpace << /DefaultRGB [/ICCBased 5 0 R] >>"
		defaultCMYK = " /ColorSpace << /DefaultCMYK [/ICCBased 5 0 R] >>"
		defaultGray = " /ColorSpace << /DefaultGray [/ICCBased 5 0 R] >>"
	)

	for _, tc := range []struct {
		name  string
		data  []byte
		test  int // 6.2.4.3 test: 2 DeviceRGB, 3 DeviceCMYK, 4 DeviceGray
		fires bool
	}{
		{"a DeviceRGB page group, no default", page("DeviceRGB", " /Resources << >>", ""), 2, true},
		{"a DeviceRGB page group, a page DefaultRGB", page("DeviceRGB", " /Resources <<"+defaultRGB+" >>", ""), 2, false},
		{"a DeviceRGB page group, an inherited DefaultRGB", page("DeviceRGB", "", " /Resources <<"+defaultRGB+" >>"), 2, false},
		{"a DeviceCMYK page group, a page DefaultCMYK", page("DeviceCMYK", " /Resources <<"+defaultCMYK+" >>", ""), 3, false},
		{"a DeviceGray page group, a page DefaultGray", page("DeviceGray", " /Resources <<"+defaultGray+" >>", ""), 4, false},
		{"a DeviceRGB form group, a form DefaultRGB", form("", " /Resources <<"+defaultRGB+" >>"), 2, false},
		{"a DeviceRGB form group, a page DefaultRGB", form(defaultRGB, " /Resources << >>"), 2, true},
		{"a DeviceRGB form group without resources, a page DefaultRGB", form(defaultRGB, ""), 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := firesRuleProfile(t, tc.data, models.PDFA2b, "6.2.4.3", tc.test); got != tc.fires {
				t.Errorf("6.2.4.3 t%d violation = %v, want %v", tc.test, got, tc.fires)
			}
		})
	}
}
