package ods

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/models"
)

// TestDataStyle_CalcLocales locks what the data styles of
// testdata/calc-datastyles.fods print under each conversion locale
// against what Calc prints under it as its system locale
// (testdata/calc-datastyles.tsv, a row a style named in its first
// column, a column a locale). The styles name no language but one: date
// styles of automatic order, with and without format-source="language",
// whose elements match one of the locale's default date formats or not,
// time styles, a currency style without decimal places, number styles
// without integer digits or decimal places, and a date style of de-DE.
//
// # Corpus evidence
//
//   - probe, ods: the document opened by LibreOffice 26.8.2.2 of the
//     pinned image under each locale (LC_ALL set in the container) and
//     saved as CSV with the cells as shown.
func TestDataStyle_CalcLocales(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "calc-datastyles.fods"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "calc-datastyles.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	head := strings.Split(lines[0], "\t")
	cases, failed := 0, 0
	for k, tag := range head[1:] {
		loc := models.Locale(tag)
		doc, _, err := Read(context.Background(), src, nil, office.Options{Locale: loc})
		if err != nil {
			t.Fatal(err)
		}
		sh := doc.Sheets[0]
		for r, line := range lines[1:] {
			cells := strings.Split(line, "\t")
			c, label := cellAt(sh, r, 0), cellAt(sh, r, 1)
			if c == nil || label == nil || label.Value.Text != cells[0] {
				t.Fatalf("row %d is not %s", r, cells[0])
			}
			if dataStyleDepartures[""][cells[0]] || dataStyleDepartures[loc][cells[0]] {
				continue
			}
			code := "General"
			if c.Style != nil && c.Style.Format != "" {
				code = c.Style.Format
			}
			f, err := numfmt.Parse(code)
			if err != nil {
				t.Errorf("%s %s %q: %v", loc, cells[0], code, err)
				continue
			}
			cases++
			got := f.WithLocale(loc).Apply(numfmt.Value{Kind: numfmt.Number, Number: c.Value.Number}).Text
			if want := cells[k+1]; got != want {
				failed++
				t.Errorf("%s %s %q: %q, want %q", loc, cells[0], code, got, want)
			}
		}
	}
	t.Logf("%d cases, %d failed", cases, failed)
}

// dataStyleDepartures are the styles, by locale, "" for every locale,
// whose print under Calc numfmt does not draw: an era, a calendar other
// than the Gregorian one (the Hijri calendar of ar-EG, the Buddhist one
// of th-TH, the Minguo year of zh-TW), native digits, the day names in
// hanja of ko-KR, and a long date of hi-IN that numfmt does not read.
var dataStyleDepartures = map[models.Locale]map[string]bool{
	"":                {"q_era": true, "pc_nodec": true, "sc_nodec": true, "nu_group_nodec": true},
	models.LocaleArEG: styleSet("b_dl_ml_ys", "d_d_m_ys_src", "e_wl_d_mtl_yl", "e2_wl_d_mtl_yl_src", "h_d_mt_ys", "l_w_d_mtl_yl", "m_dl_ml_yl_h_m", "n_d_m_ys_h_m_src", "o_dl_ml_yl_h_m_s"),
	models.LocaleHiIN: styleSet("e2_wl_d_mtl_yl_src"),
	models.LocaleKoKR: styleSet("l_w_d_mtl_yl"),
	models.LocaleThTH: styleSet("d_d_m_ys_src", "e_wl_d_mtl_yl", "e2_wl_d_mtl_yl_src", "f_ml_ys", "h_d_mt_ys", "i_d_mt_yl", "j_d_mtl_yl", "k_w_d_mt_ys", "l_w_d_mtl_yl"),
	models.LocaleZhTW: styleSet("b_dl_ml_ys", "e_wl_d_mtl_yl", "f_ml_ys", "h_d_mt_ys", "j_d_mtl_yl", "k_w_d_mt_ys", "l_w_d_mtl_yl"),
}

// styleSet returns the set of the style names.
func styleSet(names ...string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}
