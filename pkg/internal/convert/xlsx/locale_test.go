package xlsx

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/models"
)

// TestLocaleBuiltin_Calc locks what the built-in formats print under a
// locale against what Calc prints: testdata/calc-builtins.tsv holds, for
// every id from 0 to 81, Calc's print of 46000.5625 (2025-12-09 13:30) in
// a date or time format and of 1234.5, -1234.5 and 0.125 in the others,
// under 37 locales (a column each; en-US, where litho keeps Excel's
// codes, is not compared). The departures listed are formats numfmt does
// not draw: an era year and Thai digits.
//
// # Corpus evidence
//
//   - probe, xlsx: one workbook of a cell an id and value, opened by
//     LibreOffice 26.8.2.2 of the pinned image under each locale (LC_ALL
//     set in the container) and saved as CSV with the cells as shown.
func TestLocaleBuiltin_Calc(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "calc-builtins.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	head := strings.Split(lines[0], "\t")
	values := map[byte]float64{'d': 46000.5625, 'p': 1234.5, 'n': -1234.5, 'f': 0.125}
	cases, failed := 0, 0
	for _, line := range lines[1:] {
		cells := strings.Split(line, "\t")
		label := cells[0] // i<id>v<value>
		id, err := strconv.Atoi(label[1:3])
		if err != nil || len(cells) != len(head) {
			t.Fatalf("row %q", label)
		}
		v := values[label[4]]
		for k, want := range cells[1:] {
			loc := models.Locale(head[k+1])
			if loc == models.LocaleEnUS || calcDepartures[loc][id] {
				continue
			}
			cases++
			code := localeBuiltin(id, loc)
			if code == "" {
				code = builtinFormats[id]
			}
			if code == "" {
				code = "General"
			}
			f, err := numfmt.Parse(code)
			if err != nil {
				t.Errorf("%s %d %q: %v", loc, id, code, err)
				continue
			}
			if got := f.WithLocale(loc).Apply(numfmt.Value{Kind: numfmt.Number, Number: v}).Text; got != want {
				failed++
				t.Errorf("%s %s %q: %q, want %q", loc, label, code, got, want)
			}
		}
	}
	t.Logf("%d cases, %d failed", cases, failed)
	for _, loc := range []models.Locale{"", models.LocaleEnUS} {
		for id := range 82 {
			if code := localeBuiltin(id, loc); code != "" {
				t.Errorf("%q id %d takes %q, want its en-US code", loc, id, code)
			}
		}
	}
}

// calcDepartures are the ids, by locale, whose code in Calc's table holds
// what numfmt does not draw, an era year or Thai digits: they print in the
// format of Calc's base table.
var calcDepartures = map[models.Locale]map[int]bool{
	// The Reiwa year (R7, 令和7年) and the Minguo year (114).
	models.LocaleJaJP: calcIDs(27, 28, 29, 36, 50, 51, 54, 57, 58),
	models.LocaleZhTW: calcIDs(27, 28, 29, 36, 50, 51, 54, 57, 58),
	// Thai digits (t0) and the Buddhist year (D/M/E).
	models.LocaleThTH: calcIDs(59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81),
}

// calcIDs returns the set of ids.
func calcIDs(ids ...int) map[int]bool {
	out := map[int]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out
}
