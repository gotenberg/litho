package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestActionWithNoTypeIsForbidden pins that an action whose /S is absent or no
// name answers to the action-type whitelist, ISO 19005-1 6.6.1 t1, ISO 19005-2
// 6.5.1 t1 and ISO 19005-4 6.6.1 t1: ISO 32000-1, Table 193 requires /S, and a
// type the whitelist does not name is forbidden. Measured with veraPDF 1.30 at
// PDF/A-1b, PDF/A-2b and PDF/A-4 on the same fixtures, which fail the test once
// ("Unknown or not permitted Action type null") at every action site, while a
// /S /URI action and a destination under /OpenAction pass it.
func TestActionWithNoTypeIsForbidden(t *testing.T) {
	t.Parallel()

	const pages = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
	catalog := func(extra string) string { return "<< /Type /Catalog /Pages 2 0 R " + extra + " >>" }
	page := func(extra string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R " + extra + " >>"
	}
	content := exponentStream("", "0 0 m 10 10 l S")
	linkWith := func(action string) []string {
		return []string{
			catalog(""), pages, page("/Annots [5 0 R]"), content,
			"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /F 4 /Border [0 0 0] /A " + action + " >>",
		}
	}
	for _, tc := range []struct {
		name string
		objs []string
		want int
	}{
		{"a link action with no /S", linkWith("<< /URI (http://example.com) >>"), 1},
		{"an empty link action", linkWith("<< >>"), 1},
		{"a link action whose /S is a string", linkWith("<< /S (URI) /URI (http://example.com) >>"), 1},
		{"a link action whose /S is null", linkWith("<< /S null /URI (http://example.com) >>"), 1},
		{"a link action whose /S is the empty name", linkWith("<< /S / /URI (http://example.com) >>"), 1},
		{"a /Next action with no /S", linkWith("<< /S /URI /URI (http://example.com) /Next << /Type /Action >> >>"), 1},
		{"an indirect link action with no /S", append(linkWith("6 0 R"), "<< /URI (http://example.com) >>"), 1},
		{"an /OpenAction with no /S", []string{catalog("/OpenAction << /Type /Action /D [3 0 R /Fit] >>"), pages, page(""), content}, 1},
		{"a page /AA action with no /S", []string{catalog(""), pages, page("/AA << /O << /Type /Action >> >>"), content}, 1},
		{"an outline action with no /S", []string{
			catalog("/Outlines 5 0 R"), pages, page(""), content,
			"<< /Type /Outlines /First 6 0 R /Last 6 0 R /Count 1 >>",
			"<< /Title (x) /Parent 5 0 R /A << /D [3 0 R /Fit] >> >>",
		}, 1},
		{"a URI action", linkWith("<< /S /URI /URI (http://example.com) >>"), 0},
		{"a destination under /OpenAction", []string{catalog("/OpenAction [3 0 R /Fit]"), pages, page(""), content}, 0},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_6_1_t01},
			{models.PDFA2b, Rule_19005_2_6_5_1_t01},
			{models.PDFA4, Rule_19005_4_6_6_1_t01},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, tc.objs...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.want {
					t.Errorf("action-type test fired %d times, want %d: %v", got, tc.want, viols)
				}
			})
		}
	}
}
