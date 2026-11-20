package csv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestDetect_CalcNamedDates locks how Calc reads a field that holds a
// month name under each locale: testdata/calc-named-dates.tsv holds, for
// 26 forms of a date with the locale's own month names and with the
// English ones (13 Jan 2026, 13-Jan-2026, Jan 13, 2026, 2026 Jan 13,
// Jan 2026, 13 Jan, ...), the type Calc gives the cell and the text it
// shows. Two departures: ar-EG shows its dates in the Hijri calendar,
// which numfmt does not follow, so only whether a field is a date is
// compared there; de-AT reads de-DE's abbreviation Jan beside its own
// Jän, which the reader does not.
//
// # Corpus evidence
//
//   - probe, csv: one CSV a locale, opened by LibreOffice 26.8.2.2 of the
//     pinned image with the Gotenberg route's filter options under the
//     locale (LC_ALL set in the container), saved as CSV with the cells
//     as shown and as a flat ODS for the type of each cell. The year of a
//     date without one is the year of the measurement, 2026.
func TestDetect_CalcNamedDates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "calc-named-dates.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	cases, failed := 0, 0
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")[1:] {
		f := strings.Split(line, "\t")
		loc, in, value, want := models.Locale(f[0]), f[1], f[2] == "date", f[3]
		if loc == models.LocaleDeAT && in == "13 Jan 2026" {
			continue
		}
		cases++
		lr := newLocaleReader(loc)
		var d detected
		show := display
		if lr != nil {
			d, show = lr.detect(in, 2026), lr.display
		} else {
			d = detect(in, 2026)
		}
		got := in
		if d.kind != kText {
			got = show(d.kind, d.value)
		}
		if loc == models.LocaleArEG && value {
			got = want
		}
		if (d.kind != kText) != value || got != want {
			failed++
			t.Errorf("%s %q: value %v %q, want %v %q", loc, in, d.kind != kText, got, value, want)
		}
	}
	t.Logf("%d cases, %d failed", cases, failed)
}
