package docx

import (
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

func TestFormatPicture(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 3, 4, 15, 7, 9, 0, time.UTC)
	for _, tc := range []struct{ picture, want string }{
		{"M/d/yyyy", "3/4/2026"},
		{"MM/dd/yy", "03/04/26"},
		{"dddd, MMMM d, yyyy", "Wednesday, March 4, 2026"},
		{"ddd d MMM yyyy", "Wed 4 Mar 2026"},
		{"yyyy-MM-dd", "2026-03-04"},
		{"h:mm AM/PM", "3:07 PM"},
		{"hh:mm:ss am/pm", "03:07:09 pm"},
		{"HH:mm", "15:07"},
		{"H'h'mm", "15h07"},
		{"h:mm A/P", "3:07 P"},
		{"'Day' d", "Day 4"},
		{"d 'unterminated", "4 unterminated"},
	} {
		if got := formatPicture(at, tc.picture); got != tc.want {
			t.Errorf("formatPicture(%q) = %q, want %q", tc.picture, got, tc.want)
		}
	}
	if got := formatPicture(time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC), "h:mm AM/PM"); got != "12:05 AM" {
		t.Errorf("midnight = %q, want 12:05 AM", got)
	}
}

func TestDateFields(t *testing.T) {
	t.Parallel()
	complexField := func(lock, instr, cached string) string {
		return `<w:p><w:r><w:fldChar w:fldCharType="begin"` + lock + `/></w:r><w:r><w:instrText xml:space="preserve">` + instr +
			`</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>` + cached +
			`</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`
	}
	simpleField := func(attrs, instr, cached string) string {
		return `<w:p><w:fldSimple w:instr="` + instr + `"` + attrs + `><w:r><w:t>` + cached + `</w:t></w:r></w:fldSimple></w:p>`
	}
	date := time.Date(2026, 11, 11, 19, 35, 20, 0, time.FixedZone("", -5*3600))
	for _, tc := range []struct {
		name, body, without, with string
	}{
		{"DATE with a picture", complexField("", ` DATE \@ "MMMM d, yyyy" `, "January 1, 2020"), "January 1, 2020", "November 11, 2026"},
		{"DATE without a picture", complexField("", ` DATE \* MERGEFORMAT `, "1/1/2020"), "1/1/2020", "11/11/2026"},
		{"TIME", complexField("", ` TIME `, "9:00 AM"), "9:00 AM", "7:35 PM"},
		{"PRINTDATE", complexField("", ` PRINTDATE `, "0/0/0000 0:00:00 AM"), "0/0/0000 0:00:00 AM", "11/11/2026 7:35:20 PM"},
		{"CREATEDATE keeps its result", complexField("", ` CREATEDATE \@ "yyyy" `, "2019"), "2019", "2019"},
		{"SAVEDATE keeps its result", complexField("", ` SAVEDATE `, "2/3/2019"), "2/3/2019", "2/3/2019"},
		{"locked field", complexField(` w:fldLock="1"`, ` DATE \@ "yyyy" `, "2020"), "2020", "2020"},
		{"Hijri date", complexField("", ` DATE \h `, "cached"), "cached", "cached"},
		{"simple field", simpleField("", ` DATE \@ &quot;yyyy-MM-dd&quot; `, "2020-01-01"), "2020-01-01", "2026-11-11"},
		{"locked simple field", simpleField(` w:fldLock="true"`, ` DATE `, "1/1/2020"), "1/1/2020", "1/1/2020"},
	} {
		for _, c := range []struct {
			date time.Time
			want string
		}{{time.Time{}, tc.without}, {date, tc.with}} {
			b := buildWith(t, tc.body, nil, office.Options{PrintDate: c.date})
			if got := textOf(b.root); got != c.want {
				t.Errorf("%s at %v: %q, want %q", tc.name, c.date, got, c.want)
			}
		}
	}
}
