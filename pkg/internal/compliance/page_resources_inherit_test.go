package compliance

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// inheritFixtureRes is a /Resources entry for [inheritFixturePDF] naming the
// fonts and colour spaces given. F1 and F2 are unembedded standard fonts, CS0 a
// Separation over DeviceRGB and CS1 one over DeviceCMYK, so every font and
// colour space a page resolves fails a rule of its own when no output intent
// covers it.
func inheritFixtureRes(fonts, spaces []string) string {
	defs := map[string]string{
		"F1":  "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"F2":  "<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>",
		"CS0": "[/Separation /SpotRGB /DeviceRGB << /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 0 0] /N 1 >>]",
		"CS1": "[/Separation /SpotCMYK /DeviceCMYK << /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [1 0 0 0] /N 1 >>]",
	}
	var b strings.Builder
	b.WriteString("/Resources <<")
	for _, group := range []struct {
		key   string
		names []string
	}{{"Font", fonts}, {"ColorSpace", spaces}} {
		if len(group.names) == 0 {
			continue
		}
		fmt.Fprintf(&b, " /%s <<", group.key)
		for _, n := range group.names {
			fmt.Fprintf(&b, " /%s %s", n, defs[n])
		}
		b.WriteString(" >>")
	}
	b.WriteString(" >>")
	return b.String()
}

// inheritFixturePDF builds a three-level page tree: the root /Pages node
// (object 2), an intermediate /Pages node (object 3) and one page (object 4)
// whose /Parent is object parent. Each extra is spliced into its node's
// dictionary as written.
func inheritFixturePDF(rootExtra, midExtra, pageExtra string, parent int, content string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 "+rootExtra+" >>",
		"<< /Type /Pages /Parent 2 0 R /Kids [4 0 R] /Count 1 "+midExtra+" >>",
		fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Contents 5 0 R %s >>", parent, pageExtra),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	)
}

// TestPageResourcesNearestAncestor pins which /Resources a page's content
// resolves its names against. ISO 32000-1, 7.7.3.4 and Table 30 make
// /Resources inheritable: a page that states none takes the dictionary of its
// nearest ancestor in the page tree that states one, whole, and a page that
// states one uses it alone. veraPDF reads the ancestor through the tree it
// walked from the catalog, not through the page's /Parent entry
// (PDPageTreeNode.getInheritableResources), treats a null entry as absent and
// any other value, a reference to a missing object included, as the page's
// own.
//
// Every expected count was measured with veraPDF 1.30 at PDF/A-1b and
// PDF/A-2b on these fixtures. Taking the root's /Resources for every page that
// states none missed the font and the colour space an intermediate node lends,
// and judged the root's font where veraPDF judges the intermediate node's.
func TestPageResourcesNearestAncestor(t *testing.T) {
	t.Parallel()

	const (
		drawOne  = "BT /F1 12 Tf 50 700 Td (A) Tj ET /CS0 cs 1 scn 0 0 10 10 re f"
		drawBoth = "BT /F1 12 Tf 50 700 Td (A) Tj /F2 12 Tf (B) Tj ET /CS0 cs 1 scn 0 0 10 10 re f /CS1 cs 1 scn 20 0 10 10 re f"
	)
	lend := inheritFixtureRes([]string{"F1"}, []string{"CS0"})

	type counts struct {
		font, rgb, cmyk, inherited int
	}
	cases := []struct {
		name                         string
		rootExtra, midExtra, pageExt string
		parent                       int
		content                      string
		pdfa1, pdfa2                 counts
		// font is the resource name of the font the embedding rule names,
		// "" when it fires for none.
		font string
	}{
		{
			name:     "intermediate node lends",
			midExtra: lend, parent: 3, content: drawOne,
			pdfa1: counts{font: 1, rgb: 3}, pdfa2: counts{font: 1, rgb: 3, inherited: 1}, font: "F1",
		},
		{
			name:      "root lends",
			rootExtra: lend, parent: 3, content: drawOne,
			pdfa1: counts{font: 1, rgb: 3}, pdfa2: counts{font: 1, rgb: 3, inherited: 1}, font: "F1",
		},
		{
			name:      "nearest of two ancestors wins whole",
			rootExtra: lend, midExtra: inheritFixtureRes([]string{"F2"}, []string{"CS1"}), parent: 3, content: drawBoth,
			pdfa1: counts{font: 1, cmyk: 3}, pdfa2: counts{font: 1, cmyk: 3, inherited: 1}, font: "F2",
		},
		{
			name:     "tree parent, not the /Parent entry",
			midExtra: lend, parent: 2, content: drawOne,
			pdfa1: counts{font: 1, rgb: 3}, pdfa2: counts{font: 1, rgb: 3, inherited: 1}, font: "F1",
		},
		{
			name:     "own empty dictionary inherits nothing",
			midExtra: lend, pageExt: "/Resources << >>", parent: 3, content: drawOne,
		},
		{
			name:     "own null entry is absent",
			midExtra: lend, pageExt: "/Resources null", parent: 3, content: drawOne,
			pdfa1: counts{font: 1, rgb: 3}, pdfa2: counts{font: 1, rgb: 3, inherited: 1}, font: "F1",
		},
		{
			name:     "own reference to a missing object inherits nothing",
			midExtra: lend, pageExt: "/Resources 99 0 R", parent: 3, content: drawOne,
		},
		{
			name:     "own non-dictionary inherits nothing",
			midExtra: lend, pageExt: "/Resources 7", parent: 3, content: drawOne,
		},
		{
			name:      "own dictionary replaces the root's",
			rootExtra: lend, pageExt: inheritFixtureRes([]string{"F2"}, nil), parent: 3, content: drawBoth,
			pdfa1: counts{font: 1}, pdfa2: counts{font: 1}, font: "F2",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := inheritFixturePDF(tc.rootExtra, tc.midExtra, tc.pageExt, tc.parent, tc.content)
			for _, p := range []struct {
				profile models.PDFAProfile
				want    counts
				font    Rule
				rgb     Rule
				cmyk    Rule
			}{
				{models.PDFA1b, tc.pdfa1, Rule_19005_1_6_3_4_t01, Rule_19005_1_6_2_3_3_t01, Rule_19005_1_6_2_3_3_t02},
				{models.PDFA2b, tc.pdfa2, Rule_19005_2_6_2_11_4_1_t01, Rule_19005_2_6_2_4_3_t02, Rule_19005_2_6_2_4_3_t03},
			} {
				viols := validateInheritFixture(t, data, p.profile)
				got := counts{
					font: countRule(viols, p.font),
					rgb:  countRule(viols, p.rgb),
					cmyk: countRule(viols, p.cmyk),
				}
				if p.profile == models.PDFA2b {
					got.inherited = countRule(viols, Rule_19005_2_6_2_2_t02)
				}
				if got != p.want {
					t.Errorf("%s: got %+v, want %+v (veraPDF 1.30)", p.profile, got, p.want)
				}
				for _, v := range viols {
					if v.Rule == p.font && tc.font != "" && !strings.Contains(v.Context, "font "+tc.font+" ") {
						t.Errorf("%s: embedding rule names %q, want the font %s veraPDF judges", p.profile, v.Context, tc.font)
					}
				}
			}
		})
	}
}

// validateInheritFixture validates data under profile and returns the
// violations.
func validateInheritFixture(t *testing.T, data []byte, profile models.PDFAProfile) []Violation {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	errs, err := ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	viols := make([]Violation, 0, len(errs))
	for _, e := range errs {
		if v, ok := e.(Violation); ok {
			viols = append(viols, v)
		}
	}
	return viols
}

// TestPageBoundariesNearestAncestor pins the page size rule, ISO 19005-2,
// 6.1.13 t11, on a media box a page inherits. /MediaBox and /CropBox are
// inheritable (ISO 32000-1, 7.7.3.3, Table 30) and veraPDF judges the box each
// page takes, once per page, from its nearest ancestor that writes one
// (PDPage.getInheritedCOSBBox). Measured with veraPDF 1.30 at PDF/A-2b on two
// pages inheriting a 2 by 2 media box: the rule fails twice whether the box
// sits on the intermediate node or on the root. Judging the root's box alone,
// once, missed the first and under-counted the second.
func TestPageBoundariesNearestAncestor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, rootExtra, midExtra string
	}{
		{"intermediate node", "/MediaBox [0 0 612 792]", "/MediaBox [0 0 2 2]"},
		{"root", "/MediaBox [0 0 2 2]", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const page = "<< /Type /Page /Parent 3 0 R /Contents 5 0 R /Resources << >> >>"
			data := cyclePDF(
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 2 "+tc.rootExtra+" >>",
				"<< /Type /Pages /Parent 2 0 R /Kids [4 0 R 6 0 R] /Count 2 "+tc.midExtra+" >>",
				page,
				"<< /Length 12 >>\nstream\n0 0 1 1 re S\nendstream",
				page,
			)
			if got := countRule(validateInheritFixture(t, data, models.PDFA2b), Rule_19005_2_6_1_13_t11); got != 2 {
				t.Errorf("6.1.13 t11: got %d, want 2 (veraPDF 1.30)", got)
			}
		})
	}
}
