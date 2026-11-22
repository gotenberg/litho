package numfmt

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestApply_Calc locks the strings Calc shows for each code and value.
//
// # Corpus evidence
//
//   - probe, xlsx: a workbook of these codes and values (one row each, a column
//     60 characters wide), converted by Gotenberg 9.0.0's LibreOffice
//     route (LibreOffice 26.8.2.2) and read with litho text; the strings
//     below are the PDF's, U+2007 where Calc draws a figure space.
//   - libreoffice/ods: sc/tdf102370_ExponentWithoutSign.ods, 42 in a scientific
//     format without a forced exponent sign, prints "4.20E1".
func TestApply_Calc(t *testing.T) {
	num := func(v float64) Value { return Value{Kind: Number, Number: v} }
	txt := func(s string) Value { return Value{Kind: Text, Text: s} }
	tests := []struct {
		code string
		v    Value
		want string
	}{
		{"General", num(1234.5678), "1234.5678"},
		{"hh:mm:ss", num(0.141666666662789), "03:23:59"},
		{"[hh]:mm:ss", num(0.141666666662789), "03:23:59"},
		{"hh:mm", num(0.474999999996124), "11:24"},
		{`[$-F800]dddd\,\ mmmm\ dd\,\ yyyy`, num(1522), "Tuesday, March 1, 1904"},
		{"[$-F400]h:mm:ss\\ AM/PM", num(0.5625), "1:30:00 PM"},
		{"[$-en-US-x-sysdate]dddd", num(1522), "Tuesday, March 1, 1904"},
		{"General", num(0.1), "0.1"},
		{"General", num(1e20), "1E+020"},
		{"General", num(123456789012), "123456789012"},
		{"General", num(-0.000012345), "-0.000012345"},
		{"General", num(1.0 / 3), "0.333333333333333"},
		{"General", num(12345678901234567), "1.23456789012346E+016"},
		{"General", num(0.000001), "0.000001"},
		{"General", num(1e-10), "1E-10"},
		{"General", num(99999999999), "99999999999"},
		{"General", num(999999999999999), "999999999999999"},
		{"General", num(1234567890123456), "1234567890123456"},
		{"General", num(-1e100), "-1E+100"},
		{"General", num(0.1 + 0.2), "0.3"},
		{"General", num(123456789.123456789), "123456789.123457"},
		{"General", num(9007199254740992), "9.00719925474099E+015"},
		{"General", num(1e15 + 0.5), "1E+015"},
		{"General", num(1.5e-7), "0.00000015"},
		{"General", num(2.0 / 3), "0.666666666666667"},
		{"0", num(1234.5), "1235"},
		{"0.00", num(-3.14159), "-3.14"},
		{"#,##0", num(1234567.891), "1,234,568"},
		{"#,##0.00", num(-1234.5), "-1,234.50"},
		{"0%", num(0.256), "26%"},
		{"0.0%", num(-0.0123), "-1.2%"},
		{"0.00E+00", num(12345.678), "1.23E+04"},
		{"##0.0E+0", num(12345.678), "12.3E+3"},
		{"0.00E-00", num(0.00012), "1.20E-04"},
		{"0.00E0", num(42), "4.20E1"},
		{"0.00E0", num(0.042), "4.20E-2"},
		{"# ?/?", num(3.25), "3 1/4"},
		{"# ??/??", num(1.3333), "1  1/3 "},
		{"0/8", num(0.4), "3/8"},
		{"# #/16", num(5.3), "5 5/16"},
		{"?/?", num(0.75), "3/4"},
		{`# ?/16" in"`, num(2.5), "2 8/16 in"},
		{"# ?/?", num(4), "4    "},
		{"#,##0,", num(1234567), "1,235"},
		{"0.0,,", num(1234567), "1.2"},
		{`"$"#,##0.00;[Red]-"$"#,##0.00`, num(-42.5), "-$42.50"},
		{`[$€-407] #,##0.00`, num(1234.5), "€ 1,234.50"},
		{`#,##0.00 [$€-407]`, num(1234.5), "1,234.50 €"},
		{`[>=100][Red]0;[Blue]0`, num(150), "150"},
		{`[>=100][Red]0;[Blue]0`, num(50), "50"},
		{`0;-0;"zero";"text:"@`, num(0), "zero"},
		{`0;-0;"zero";"text:"@`, txt("abc"), "text:abc"},
		{`0;(0)`, num(-5), "(5)"},
		{`_(* #,##0_);_(* (#,##0);_(* "-"_);_(@_)`, num(1234), " 1,234 "},
		{`_(* #,##0_);_(* (#,##0);_(* "-"_);_(@_)`, num(0), " - "},
		{`_(* #,##0_);_(* (#,##0);_(* "-"_);_(@_)`, num(-1234), " (1,234)"},
		{`0.00\ "kg"`, num(2.5), "2.50 kg"},
		{`\#0`, num(7), "#7"},
		{"00000", num(42), "00042"},
		{"000-00-0000", num(123456789), "123-45-6789"},
		{"#.##", num(3), "3"},
		{"#.##", num(0.5), ".5"},
		{"0.#", num(3), "3"},
		{"??.??", num(3.5), " 3.5 "},
		{"?0.0?", num(1.25), " 1.25"},
		{"yyyy-mm-dd", num(45000.75), "2023-03-15"},
		{"d/m/yy", num(45000), "15/3/23"},
		{"dddd, mmmm d, yyyy", num(45000), "Wednesday, March 15, 2023"},
		{"ddd mmm dd", num(45000), "Wed Mar 15"},
		{"mmmmm", num(45000), "M"},
		{"hh:mm:ss", num(0.5432), "13:02:12"},
		{"h:mm AM/PM", num(0.75), "6:00 PM"},
		{"h:mm A/P", num(0.25), "6:00 a"},
		{"[h]:mm:ss", num(1.5), "36:00:00"},
		{"[mm]:ss", num(0.05), "72:00"},
		{"[ss]", num(0.01), "864"},
		{"mm:ss.000", num(0.0123456), "17:46.660"},
		{"m/d/yyyy h:mm", num(45000.5), "3/15/2023 12:00"},
		{"yy m", num(45000), "23 3"},
		{"h m", num(0.3), "7 12"},
		{"yyyy-mm-dd hh:mm", num(1), "1899-12-31 00:00"},
		{"d-mmm-yy", num(60), "28-Feb-00"},
		{"d-mmm-yy", num(61), "1-Mar-00"},
		{"d-mmm-yy", num(1), "31-Dec-99"},
		{"d-mmm-yy", num(0), "30-Dec-99"},
		{"@", num(12.5), "12.5"},
		{`"x"@"y"`, txt("mid"), "xmidy"},
		{"General", Value{Kind: Bool, Number: 1}, "TRUE"},
		{"General", Value{Kind: Bool}, "FALSE"},
		{"0.00", Value{Kind: Bool, Number: 1}, "1.00"},
		{"General", Value{Kind: Error, Text: "#DIV/0!"}, "#DIV/0!"},
		{"[Color 10]0", num(5), "5"},
		{"[Green]0.0", num(2), "2.0"},
		{"0.0_);(0.0)", num(3), "3.0 "},
		{"[$-409]mmmm", num(45000), "March"},
		{"[$-F800]", num(45000), "45000"},
		{"[$-F400]", num(0.5), "0.5"},
		{`0 "items"`, num(1), "1 items"},
		{"#,##0.00;-#,##0.00;-", num(0), "-"},
		{"0.0", num(0.05), "0.1"},
		{"0.0", num(0.15), "0.2"},
		{"0.0", num(0.25), "0.3"},
		{"0.0", num(-0.05), "-0.1"},
		{"0", num(2.5), "3"},
		{"0", num(-2.5), "-3"},
		{"0", num(-0.4), "0"},
	}
	for _, tt := range tests {
		f, err := Parse(tt.code)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.code, err)
			continue
		}
		if got := f.Apply(tt.v).Text; got != tt.want {
			t.Errorf("Parse(%q).Apply(%+v) = %q, want %q", tt.code, tt.v, got, tt.want)
		}
	}
}

// TestFit_General locks how Calc narrows a General number to its column
// and when it shows "###", with Liberation Sans 10 point widths: digits
// 5.56, point 2.78, minus 3.33, E 6.67 and plus 5.84 points.
//
// # Corpus evidence
//
//   - probe, ods: a flat ODS of these values in columns 20, 30, 40, 50 and 60
//     points wide, Liberation Sans 10, converted by Gotenberg 9.0.0; a
//     cell's text is its width less one point of padding a side.
//   - probe, ods: the same values in columns 36 to 43.5 points wide by half
//     points: 1234.5678 shows 1234.57 from 38.5 points and 1.5e-7 shows
//     1.5E-07 from 40, as the widths here give; Calc rounds a column to
//     its device's pixels, so a column within a pixel of a threshold may
//     fall either side.
func TestFit_General(t *testing.T) {
	w := map[rune]float64{'.': 2.78, '-': 3.33, 'E': 6.67, '+': 5.84}
	measure := func(s string) float64 {
		sum := 0.0
		for _, c := range s {
			if v, ok := w[c]; ok {
				sum += v
			} else {
				sum += 5.56
			}
		}
		return sum
	}
	tests := []struct {
		v     float64
		width float64
		want  string
	}{
		{123456789, 18, "###"},
		{123456789, 28, "###"},
		{123456789, 38, "1E+08"},
		{123456789, 48, "1.23E+08"},
		{123456789, 58, "123456789"},
		{1234.5678, 18, "###"},
		{1234.5678, 28, "1235"},
		{1234.5678, 38, "1234.57"},
		{0.000123456, 18, "0"},
		{0.000123456, 38, "0.00012"},
		{0.000123456, 48, "0.0001235"},
		{0.000123456, 58, "0.00012346"},
		{-98765.4321, 38, "-98765"},
		{-98765.4321, 48, "-98765.43"},
		{1e20, 28, "###"},
		{1e20, 38, "1E+020"},
		{1.5e20, 38, "2E+20"},
		{9007199254740992, 38, "9E+15"},
		{9007199254740992, 48, "9.01E+15"},
		{9007199254740992, 58, "9.0072E+15"},
		{9007199254740992, 78, "9.00719925E+15"},
		{1e-5, 28, "1E-05"},
		{1.5e-7, 48, "1.5E-07"},
		{1.5e-7, 36, "2E-07"},
		{1.5e-7, 38, "1.5E-07"},
		{1234.5678, 36.5, "1234.57"},
		{1234.5678, 35.5, "1234.6"},
		{2.0 / 3, 18, "0.7"},
		{2.0 / 3, 28, "0.667"},
		{2.0 / 3, 38, "0.66667"},
		{12345, 18, "###"},
		{100, 18, "100"},
	}
	for _, tt := range tests {
		got := General.Fit(Value{Kind: Number, Number: tt.v}, tt.width, measure).Text
		if got != tt.want {
			t.Errorf("Fit(%v, %v) = %q, want %q", tt.v, tt.width, got, tt.want)
		}
	}
}

// TestFit_ExcelGeneral checks that under Excel's rules a General number
// is narrowed to as many characters as digits fit, a point or an exponent
// counted as a digit and a minus sign at its own width.
func TestFit_ExcelGeneral(t *testing.T) {
	w := map[rune]float64{'.': 2.78, '-': 3.33, 'E': 6.67, '+': 5.84}
	measure := func(s string) float64 {
		sum := 0.0
		for _, c := range s {
			if v, ok := w[c]; ok {
				sum += v
			} else {
				sum += 5.56
			}
		}
		return sum
	}
	tests := []struct {
		v     float64
		width float64
		want  string
	}{
		{1161014163, 45, "1.16E+09"},
		{123456789, 48, "1.23E+08"},
		{1234.5678, 38, "1234.6"},
		{-98765.4321, 48, "-98765.43"},
		{-1234567.891, 44, "-1234568"},
		{12345678901, 44, "1.2E+10"},
		{0.000123456789, 44, "0.00012"},
		{0.00001, 44, "0.00001"},
		// Below 1, fixed point while it keeps as many digits as
		// scientific notation in the characters that fit.
		{0.00001234, 44, "1.2E-05"},
		{0.000015, 44, "1.5E-05"},
		{0.00000999999, 44, "1E-05"},
		{0.000001, 44, "1E-06"},
		{0.0001, 44, "0.0001"},
		{-0.0000123456, 44, "-1.2E-05"},
		// The sign's room comes first.
		{2.684713, 46, "2.684713"},
		{-6.105228, 46, "-6.10523"},
		// Excel fills a cell it cannot fit with "#" (Result.Fill).
		{123456789, 28, ""},
	}
	for _, tt := range tests {
		got := General.Excel().Fit(Value{Kind: Number, Number: tt.v}, tt.width, measure).Text
		if got != tt.want {
			t.Errorf("Fit(%v, %v) = %q, want %q", tt.v, tt.width, got, tt.want)
		}
	}
}

// TestApply_ExcelGeneral checks that Excel's General format shows a
// number in at most 11 characters, a minus sign aside, a magnitude below
// 1 in fixed point while that keeps as many of its digits as scientific
// notation does.
func TestApply_ExcelGeneral(t *testing.T) {
	tests := []struct {
		v    float64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{12345678901, "12345678901"},
		{-12345678901, "-12345678901"},
		{123456789012, "1.23457E+11"},
		{123456789012345, "1.23457E+14"},
		{1e15, "1E+15"},
		{1e16, "1E+16"},
		{1e308, "1E+308"},
		{100000000000, "1E+11"},
		{99999999999.5, "1E+11"},
		{0.000123456789, "0.000123457"},
		{0.00001, "0.00001"},
		{0.0000012345, "1.2345E-06"},
		{0.00001234, "0.00001234"},
		{0.000001, "0.000001"},
		{0.0000015, "0.0000015"},
		{0.000015, "0.000015"},
		{1e-9, "0.000000001"},
		{1.5e-9, "1.5E-09"},
		{1e-10, "1E-10"},
		{1.23456789e-7, "1.23457E-07"},
		{-0.0000123456, "-1.23456E-05"},
		{0.00000999999, "9.99999E-06"},
		{2.2250738585072014e-308, "2.2251E-308"},
		{-1234567.891, "-1234567.891"},
		{0.30000000000000004, "0.3"},
		{1.0 / 3, "0.333333333"},
		{-2.0 / 3, "-0.666666667"},
	}
	for _, tt := range tests {
		if got := General.Excel().Apply(Value{Kind: Number, Number: tt.v}).Text; got != tt.want {
			t.Errorf("Apply(%v) = %q, want %q", tt.v, got, tt.want)
		}
	}
	// Calc's standard format keeps 15 digits.
	if got := General.Apply(Value{Kind: Number, Number: 123456789012345}).Text; got != "123456789012345" {
		t.Errorf("Calc's General = %q", got)
	}
}

func TestFormat_WithLocale(t *testing.T) {
	tests := []struct {
		lang, code string
		v          float64
		want       string
	}{
		{"de-DE", "#,##0.00", 1234567.5, "1.234.567,50"},
		{"fr-FR", "#,##0.00", 1234567.5, "1 234 567,50"},
		{"pt-PT", "#,##0.00", 1234.5, "1 234,50"},
		{"pt-BR", "#,##0.00", 1234.5, "1.234,50"},
		{"de-CH", "#,##0.00", 1234.5, "1'234.50"},
		{"en-US", "#,##0.00", 1234.5, "1,234.50"},
		{"es_ES", "0.00%", 0.2143, "21,43%"},
		{"hu", "General", 2.5, "2,5"},
		{"", "0.0", 2.5, "2.5"},
	}
	for _, tt := range tests {
		f, err := Parse(tt.code)
		if err != nil {
			t.Fatal(err)
		}
		if got := f.WithLocale(LocaleOf(tt.lang)).Apply(Value{Kind: Number, Number: tt.v}).Text; got != tt.want {
			t.Errorf("%s %s: %q, want %q", tt.lang, tt.code, got, tt.want)
		}
	}
	if General.WithLocale("") != General || General.WithLocale(models.LocaleEnUS) != General {
		t.Error("the en-US locale copies the format")
	}
}

func TestParse_LocaleTag(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{"[$-407]#,##0.00", "1.234,50"},
		{"[$-40C]#,##0.00", "1 234,50"},
		{"[$€-407]#,##0.00", "€1,234.50"},
		{"[$-409]#,##0.00", "1,234.50"},
		{"[$-x]0.0", "1234.5"},
	}
	for _, tt := range tests {
		f, err := Parse(tt.code)
		if err != nil {
			t.Fatalf("%s: %v", tt.code, err)
		}
		if got := f.Apply(Value{Kind: Number, Number: 1234.5}).Text; got != tt.want {
			t.Errorf("%s: %q, want %q", tt.code, got, tt.want)
		}
	}
	if id, ok := LCIDOf("hu-HU"); !ok || id != 0x40E {
		t.Errorf("LCIDOf(hu-HU) = %x %v", id, ok)
	}
	if id, ok := LCIDOf("de"); !ok || localeOfLCID(id).tag != LocaleOf("de") {
		t.Errorf("LCIDOf(de) = %x %v", id, ok)
	}
	if _, ok := LCIDOf("en-US"); ok {
		t.Error("an en-US tag")
	}
}

// TestLCIDOf_RoundTrips checks that the identifier a tag is written with
// names that tag back, and that both identifiers of es-ES (0x40A,
// traditional sort, and 0xC0A, modern sort) name es-ES.
func TestLCIDOf_RoundTrips(t *testing.T) {
	id, ok := LCIDOf("es-ES")
	if !ok || localeOfLCID(id) == nil || localeOfLCID(id).tag != models.LocaleEsES {
		t.Fatalf("LCIDOf(es-ES) = %x %v", id, ok)
	}
	for _, id := range []uint64{0x40A, 0xC0A} {
		if d := localeOfLCID(id); d == nil || d.tag != models.LocaleEsES {
			t.Errorf("localeOfLCID(%x) = %v", id, d)
		}
	}
}

// TestFormat_Excel locks the values Excel shows apart from Calc: negative
// dates and times fill the cell with "#", the 1900 date system's day 0
// and February 29, whole seconds rounded, a decimal point kept where no
// decimal shows, and a value too wide filling the cell with "#".
func TestFormat_Excel(t *testing.T) {
	for _, tc := range []struct {
		code string
		v    float64
		calc string
		want string
	}{
		{"mm/dd/yyyy", 0, "12/30/1899", "01/00/1900"},
		{"mm/dd/yyyy", 60, "02/28/1900", "02/29/1900"},
		{"mm/dd/yyyy", 61, "03/01/1900", "03/01/1900"},
		{"ddd mm/dd/yyyy", 1, "Sun 12/31/1899", "Sun 01/01/1900"},
		{"h:mm:ss", 1234.567, "13:36:28", "13:36:29"},
		{"mm/dd/yyyy", -1, "", ""},
		{"#0.#", 1000, "1000", "1000."},
		{"#0.#", 0, "0", "0."},
		{"#0.#", 2.5, "2.5", "2.5"},
		{"#.##", 3, "3", "3."},
	} {
		f, err := Parse(tc.code)
		if err != nil {
			t.Fatal(err)
		}
		r := f.Excel().Apply(Value{Kind: Number, Number: tc.v})
		if r.Text != tc.want {
			t.Errorf("%s of %v: %q, want %q", tc.code, tc.v, r.Text, tc.want)
		}
		if tc.v < 0 && (r.Fill != '#' || !r.Hash) {
			t.Errorf("%s of %v: fill %q, want a cell of #", tc.code, tc.v, r.Fill)
		}
		if tc.calc != "" && tc.v != 1 {
			if c := f.Apply(Value{Kind: Number, Number: tc.v}); c.Text != tc.calc {
				t.Errorf("%s of %v by Calc: %q, want %q", tc.code, tc.v, c.Text, tc.calc)
			}
		}
	}
	r := General.Excel().Fit(Value{Kind: Number, Number: 1e300}, 1, func(s string) float64 { return float64(len(s)) * 5 })
	if r.Text != "" || r.Fill != '#' || !r.Hash {
		t.Errorf("a value too wide: %+v, want a cell of #", r)
	}
	if General.Excel() == General || General.excel {
		t.Error("Excel changed General")
	}
}

// TestApply_ExcelBooleans locks a boolean under Excel's rules: TRUE or
// FALSE whatever its format, where Calc formats the number.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/ceiling-floor.xlsx prints TRUE for booleans
//     under the format 0; sc/tdf122191.xlsx prints TRUE under
//     "IGAZ";"IGAZ";"HAMIS".
func TestApply_ExcelBooleans(t *testing.T) {
	for _, tc := range []struct {
		code string
		v    float64
		want string
	}{{"0", 1, "TRUE"}, {"0.00", 0, "FALSE"}, {`"IGAZ";"IGAZ";"HAMIS"`, 1, "TRUE"}} {
		f, err := Parse(tc.code)
		if err != nil {
			t.Fatal(err)
		}
		if got := f.Excel().Apply(Value{Kind: Bool, Number: tc.v}).Text; got != tc.want {
			t.Errorf("%s: %q, want %q", tc.code, got, tc.want)
		}
		if got := f.Apply(Value{Kind: Bool, Number: tc.v}).Text; got == tc.want {
			t.Errorf("%s under Calc's rules: %q", tc.code, got)
		}
	}
}

// TestApply_ClockSeconds checks that a date and time shows the second its
// serial number stands for when the binary error of the serial falls
// just short of it, as tools::Time::GetClock reads it, and that a time
// short of the next second by more than that error keeps its second.
//
// # Corpus evidence
//
//   - probe, ods: 2025-12-09T13:30:45, stored as the serial
//     46000.56302083333, prints 09.12.2025 13:30:45 in DD.MM.YYYY
//     HH:MM:SS in LibreOffice 26.8.2.2 of the pinned image.
func TestApply_ClockSeconds(t *testing.T) {
	for _, tt := range []struct {
		code string
		v    float64
		want string
	}{
		{"dd.mm.yyyy hh:mm:ss", 46000.56302083333, "09.12.2025 13:30:45"},
		{"hh:mm:ss", 0.56302083333333, "13:30:45"},
		{"hh:mm:ss", 0.5630208, "13:30:44"},
		{"dd.mm.yyyy hh:mm:ss", 46000.5630207, "09.12.2025 13:30:44"},
	} {
		f, err := Parse(tt.code)
		if err != nil {
			t.Fatal(err)
		}
		if got := f.Apply(Value{Kind: Number, Number: tt.v}).Text; got != tt.want {
			t.Errorf("%s %v: %q, want %q", tt.code, tt.v, got, tt.want)
		}
	}
}

// TestApply_GeneralPercent checks that a percent sign beside General
// scales the number by 100, as LibreOffice prints a percentage style of
// no decimal places.
//
// # Corpus evidence
//
//   - probe, ods: 0.125 in such a style prints 12.5% in LibreOffice
//     26.8.2.2 under en-US and 12,5% under de-DE.
func TestApply_GeneralPercent(t *testing.T) {
	for _, tt := range []struct {
		loc  models.Locale
		v    float64
		want string
	}{
		{"", 0.125, "12.5%"},
		{"", -0.125, "-12.5%"},
		{models.LocaleDeDE, 0.125, "12,5%"},
	} {
		f, err := Parse("General%")
		if err != nil {
			t.Fatal(err)
		}
		if got := f.WithLocale(tt.loc).Apply(Value{Kind: Number, Number: tt.v}).Text; got != tt.want {
			t.Errorf("%q %v: %q, want %q", tt.loc, tt.v, got, tt.want)
		}
	}
}
