package odt

import (
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

func TestConvert_DateFields(t *testing.T) {
	auto := `<number:date-style style:name="N1"><number:year number:style="long"/><number:text>-</number:text><number:month number:style="long"/><number:text>-</number:text><number:day number:style="long"/></number:date-style>` +
		`<number:date-style style:name="N2"><number:day-of-week number:style="long"/><number:text>, </number:text><number:month number:textual="true" number:style="long"/><number:text> </number:text><number:day/><number:text>, </number:text><number:year number:style="long"/></number:date-style>` +
		`<number:time-style style:name="N3"><number:hours number:style="long"/><number:text>:</number:text><number:minutes number:style="long"/></number:time-style>`
	date := time.Date(2026, 11, 11, 19, 35, 20, 0, time.FixedZone("", 2*3600))
	cases := []struct {
		name, field, without, with string
	}{
		{"date without a style", `<text:date text:date-value="2020-05-06">05/06/20</text:date>`, "05/06/20", "11/11/26"},
		{"date in a style", `<text:date style:data-style-name="N1" text:date-value="2020-05-06">2020-05-06</text:date>`, "2020-05-06", "2026-11-11"},
		{"date with names", `<text:date style:data-style-name="N2">saved</text:date>`, "saved", "Wednesday, November 11, 2026"},
		{"fixed date", `<text:date text:fixed="true" style:data-style-name="N1">2020-05-06</text:date>`, "2020-05-06", "2020-05-06"},
		{"adjusted date", `<text:date style:data-style-name="N1" text:date-adjust="-P1Y2M3D">x</text:date>`, "x", "2025-09-08"},
		{"time without a style", `<text:time text:time-value="10:20:30">10:20:30</text:time>`, "10:20:30", "19:35:20"},
		{"time in a style", `<text:time style:data-style-name="N3">10:20</text:time>`, "10:20", "19:35"},
		{"adjusted time", `<text:time style:data-style-name="N3" text:time-adjust="PT1H30M">x</text:time>`, "x", "21:05"},
		{"fixed time", `<text:time text:fixed="true">10:20:30</text:time>`, "10:20:30", "10:20:30"},
		{"unread adjustment", `<text:date style:data-style-name="N1" text:date-adjust="soon">x</text:date>`, "x", "2026-11-11"},
		{"print date keeps its metadata", `<text:print-date style:data-style-name="N1">2019-01-01</text:print-date>`, "2019-01-01", "2019-01-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, c := range []struct {
				date time.Time
				want string
			}{{time.Time{}, tc.without}, {date, tc.with}} {
				res := convertDoc(t, doc{contentAuto: auto, body: `<text:p>` + tc.field + `</text:p>`}.pack(t), office.Options{PrintDate: c.date})
				if got := docText(res); got != c.want {
					t.Errorf("date %v: text = %q, want %q", c.date, got, c.want)
				}
			}
		})
	}
}
