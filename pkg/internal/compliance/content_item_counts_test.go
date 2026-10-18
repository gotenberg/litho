package compliance

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// fixtureRuleCount validates the litho corpus fixture name at profile and
// returns how many violations of rule it reports.
func fixtureRuleCount(t *testing.T, name, profile string, rule Rule) int {
	t.Helper()
	ctx := context.Background()
	data, err := os.ReadFile("../../../test/integration/corpus/litho/pdfs/" + name)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	var viols []error
	if IsPDFUA(profile) {
		viols, err = ValidatePDFUA(ctx, doc, models.PDFUAProfile(profile))
	} else {
		viols, err = ValidatePDFA(ctx, doc, models.PDFAProfile(profile))
	}
	if err != nil {
		t.Fatalf("validating at %s: %v", profile, err)
	}
	return countRuleErrs(t, viols, rule)
}

// TestFixtureCheckCounts pins the number of checks veraPDF 1.30.2 fails for a
// rule whose object is finer than one finding per document: ISO 19005-1, 6.3.2
// t7 on every PDFont, a Type0 font and its descendant CIDFont alike, and ISO
// 14289-1, 7.1 t3 and 7.2 t34 and ISO 14289-2, 8.2.2 t1 on every content item,
// each content stream read once.
func TestFixtureCheckCounts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fixture, profile string
		rule             Rule
		want             int
	}{
		// Four Type0 fonts over a forbidden /OpenType program, each failing
		// with its descendant.
		{"fontotread_2b.pdf", "PDF/A-1b", Rule_19005_1_6_3_2_t07, 12},
		{"fontcff2_2b.pdf", "PDF/A-1b", Rule_19005_1_6_3_2_t07, 14},
		// Eight untagged Tj with no /Lang anywhere.
		{"fontotread_2b.pdf", "PDF/UA-1", Rule_14289_1_7_1_t03, 8},
		{"fontotread_2b.pdf", "PDF/UA-1", Rule_14289_1_7_2_t34, 8},
		{"fontotread_2b.pdf", "PDF/UA-2", Rule_14289_2_8_2_2_t01, 8},
		// Two pages sharing one content stream: veraPDF reads it, and the
		// forms it draws, once.
		{"imagereach_2b.pdf", "PDF/UA-1", Rule_14289_1_7_1_t03, 2},
		// A PostScript XObject is no content item.
		{"multipage_2b.pdf", "PDF/UA-1", Rule_14289_1_7_1_t03, 4},
		// Forms two pages draw count on the first page only.
		{"fontcopies_2b.pdf", "PDF/UA-1", Rule_14289_1_7_1_t03, 62},
		{"fontcopies_2b.pdf", "PDF/UA-1", Rule_14289_1_7_2_t34, 62},
	} {
		t.Run(fmt.Sprintf("%s at %s, %s t%d", tc.fixture, tc.profile, tc.rule.Clause, tc.rule.TestNumber), func(t *testing.T) {
			t.Parallel()
			if got := fixtureRuleCount(t, tc.fixture, tc.profile, tc.rule); got != tc.want {
				t.Errorf("checks = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestContentMarking_CountsItems pins the content item counts of ISO 14289-1,
// 7.1 t3 on synthetic pages: one per painting operator and image, none for
// a form drawn a second time, and one finding for untagged painting that left
// no item to count, a PostScript XObject alone. veraPDF builds no item for a
// PostScript XObject and reports nothing on that page; litho keeps the
// finding it stated before counting items.
func TestContentMarking_CountsItems(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const form = "<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length 29 >>\nstream\n0 0 5 5 re f 0 0 6 6 re S\nendstream"
	const ps = "<< /Type /XObject /Subtype /PS /Length 2 >>\nstream\n%!\nendstream"
	for _, tc := range []struct {
		name, page string
		want       int
	}{
		{"three painting operators", "0 0 1 1 re f 0 0 2 2 re S BT ET BT /F 1 Tf (a) Tj ET", 3},
		{"a form drawn twice", "/Fm Do /Fm Do", 2},
		{"a form beside its own painting", "/Fm Do 0 0 1 1 re f", 3},
		{"a PostScript XObject alone", "/Ps Do", 1},
		{"a PostScript XObject beside a fill", "/Ps Do 0 0 1 1 re f", 1},
		{"nothing painted", "q Q", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Fm 5 0 R /Ps 6 0 R >> >> >>",
				streamObj(tc.page),
				form,
				ps,
			)
			if got := countRule(checkContentMarking(ctx, doc, cat, "PDF/UA-1"), Rule_14289_1_7_1_t03); got != tc.want {
				t.Errorf("7.1 t3 checks = %d, want %d", got, tc.want)
			}
		})
	}
}
