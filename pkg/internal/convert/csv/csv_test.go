package csv

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

func TestParse_Records(t *testing.T) {
	tests := []struct {
		name, in string
		want     []record
	}{
		{"separators", "a,b;c\td\n", []record{{"a", "b", "c", "d"}}},
		{"quotes", "\"a,b\",\"say \"\"hi\"\"\",c\"d\n", []record{{"a,b", `say "hi"`, `c"d`}}},
		{"line ends", "a\r\nb\rc\nd", []record{{"a"}, {"b"}, {"c"}, {"d"}}},
		{"quoted newline", "\"x\ny\",z\n", []record{{"x\ny", "z"}}},
		{"quoted CR", "\"a\r\rb\",\"c\r\nd\"\r\n", []record{{"a\n\nb", "c\nd"}}},
		{"ragged", "a,b,c\n1\n,,\n", []record{{"a", "b", "c"}, {"1"}, {"", "", ""}}},
		{"empty line", "a\n\nb\n", []record{{"a"}, {""}, {"b"}}},
		{"spaces kept", " a , b \n", []record{{" a ", " b "}}},
		{"quote inside quoted", "\"ab\"cd\",e\n", []record{{`ab"cd`, "e"}}},
		{"unterminated", "\"abc,d\nx", []record{{`"abc`, "d"}, {"x"}}},
		{"blanks before quote", `a, "b, c" ,d`, []record{{"a", "b, c ", "d"}}},
		{"control", "a\x16b\n", []record{{"ab"}}},
		{"broken quotes", `"abc" d "ef",x`, []record{{`abc" d "ef`, "x"}}},
		{"empty quoted", "\"\",x", []record{{"", "x"}}},
	}
	for _, tt := range tests {
		got, _, err := parse(context.Background(), tt.in)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: parse(%q) = %q, %v, want %q", tt.name, tt.in, got, err, tt.want)
		}
	}
}

func TestDecode(t *testing.T) {
	if got := decode([]byte("\xef\xbb\xbfa\xffb")); got != "a�b" {
		t.Errorf("decode = %q", got)
	}
}

func TestParse_Caps(t *testing.T) {
	recs, lim, err := parse(context.Background(), strings.Repeat("x,", maxColumns+5)+"\n\""+strings.Repeat("y", maxFieldBytes+10))
	if err != nil || !lim.columns || !lim.bytes || len(recs[0]) != maxColumns || len(recs[1][0]) != maxFieldBytes {
		t.Errorf("caps = %+v, %v, %d", lim, err, len(recs))
	}
}

// TestDetect follows what the image prints for each field; see [kind].
func TestDetect(t *testing.T) {
	date := func(y, m, d int) float64 {
		v, _ := serial(y, m, d)
		return v
	}
	tests := []struct {
		in   string
		kind kind
		v    float64
	}{
		{"123", kGeneral, 123},
		{"-1.5", kGeneral, -1.5},
		{"+3", kGeneral, 3},
		{"007", kGeneral, 7},
		{"1,234.56", kGeneral, 1234.56},
		{"12,34", kText, 0},
		{".5", kGeneral, 0.5},
		{"(5)", kGeneral, -5},
		{"5-", kGeneral, -5},
		{"1.5E+03", kScientific, 1500},
		{"1e5", kScientific, 1e5},
		{"15%", kPercent, 0.15},
		{"5 %", kPercent, 0.05},
		{"-5%", kPercent, -0.05},
		{"$1234.50", kCurrency, 1234.5},
		{"$1,234.50", kCurrency, 1234.5},
		{"-$5", kCurrency, -5},
		{"$-5", kCurrency, -5},
		{"2026-01-10", kDateISO, date(2026, 1, 10)},
		{"2026-1-5", kDateISO, date(2026, 1, 5)},
		{"2026-01-10 13:45", kDateTimeISO, date(2026, 1, 10) + (13*60+45)/1440.0},
		{"2026-01-10T13:45:30", kDateTimeISOT, date(2026, 1, 10) + (13*3600+45*60+30)/86400.0},
		{"1/10/2026", kDateUS, date(2026, 1, 10)},
		{"01/10/26", kDateUS, date(2026, 1, 10)},
		{"1/10", kDateUS, date(2030, 1, 10)},
		{"Jan 10, 2026", kDateUS, date(2026, 1, 10)},
		{"January 10, 2026", kDateUS, date(2026, 1, 10)},
		{"10-Jan-2026", kDateUS, date(2026, 1, 10)},
		{"Jan 2026", kDateUS, date(2026, 1, 1)},
		{"12/31/2026 10:00", kDateTimeUS, date(2026, 12, 31) + 10/24.0},
		{"12:30", kTime, 12.5 / 24},
		{"13:45", kTime, 13.75 / 24},
		{"1:30 PM", kTime, 13.5 / 24},
		{"1:30 am", kTime, 1.5 / 24},
		{"25:10", kElapsed, (25 + 10/60.0) / 24},
		{"12:30:45.5", kTimeFraction, (12*3600 + 30*60 + 45.5) / 86400},
		{"TRUE", kBool, 1},
		{"false", kBool, 0},
		{" 12 ", kGeneral, 12},
		{"2026/01/10", kText, 0},
		{"10.01.2026", kText, 0},
		{"31/12/2026", kText, 0},
		{"1-Jan", kText, 0},
		{"€5", kText, 0},
		{"0x10", kText, 0},
		{"Infinity", kText, 0},
		{"NaN", kText, 0},
		{"Monday", kText, 0},
		{"-(5)", kText, 0},
		{"1 000", kText, 0},
		{"$", kText, 0},
	}
	for _, tt := range tests {
		got := detect(tt.in, 2030)
		if got.kind != tt.kind || math.Abs(got.value-tt.v) > 1e-9 {
			t.Errorf("detect(%q) = %+v, want %v %v", tt.in, got, tt.kind, tt.v)
		}
	}
}

func TestDisplay(t *testing.T) {
	tests := []struct {
		k    kind
		v    float64
		want string
	}{
		{kGeneral, 1234.56, "1234.56"},
		{kGeneral, 12345678901234567890, "1.23456789012346E+019"},
		{kPercent, 0.15, "15.00%"},
		{kCurrency, 1234567, "$1,234,567.00"},
		{kCurrency, -5, "-$5.00"},
		{kScientific, 1500, "1.50E+03"},
		{kDateUS, 46032, "01/10/26"},
		{kDateISO, 46032, "2026-01-10"},
		{kDateTimeUS, 46387 + 10/24.0, "12/31/26 10:00 AM"},
		{kTime, 0.5208333333, "12:30:00 PM"},
		{kElapsed, (25 + 10/60.0) / 24, "25:10:00"},
		{kTimeFraction, (12*3600 + 30*60 + 45.5) / 86400, "12:30:45.50"},
		{kDateTimeISOT, 46032 + 0.5, "2026-01-10T12:00:00"},
		{kBool, 1, "TRUE"},
	}
	for _, tt := range tests {
		if got := display(tt.k, tt.v); got != tt.want {
			t.Errorf("display(%v, %v) = %q, want %q", tt.k, tt.v, got, tt.want)
		}
	}
}

var (
	fontsOnce sync.Once
	fontsIdx  *text.Fonts
)

// fixtureFonts returns the pinned fixture fonts, skipping when absent.
func fixtureFonts(t testing.TB) *text.Fonts {
	t.Helper()
	dir := "../../../../test/integration/testdata/fonts/liberation-2.1.5"
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("fixture fonts not present: %v", err)
	}
	fontsOnce.Do(func() {
		f, err := text.NewFonts(context.Background(), nil, []string{dir})
		if err == nil {
			fontsIdx = f
		}
	})
	if fontsIdx == nil {
		t.Skip("fixture fonts do not index")
	}
	return fontsIdx
}

// TestRead_Widths checks the column widths Calc gives an import; see
// the constants of widths.go.
func TestRead_Widths(t *testing.T) {
	fonts := fixtureFonts(t)
	in := "a,," + strings.Repeat("a", 12) + ",Northwind Traders,id\n,,,,25\n"
	in += "x,,,2026-01-10,1\nh,,,,\n"
	doc, _, err := Read(context.Background(), []byte(in), fonts, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]float64{0: 14.08, 2: 73.58, 3: 89.78, 4: 16.44}
	cols := doc.Sheets[0].Columns
	if len(cols) != len(want) {
		t.Fatalf("columns = %+v", cols)
	}
	for _, c := range cols {
		if w, ok := want[c.First]; !ok || math.Abs(c.Width-w) > 0.05 || c.Last != c.First {
			t.Errorf("column %d = %v, want %v", c.First, c.Width, w)
		}
	}
	// A column typed in its second row is measured with that row's
	// format: the longest string is the currency of 1234567.
	doc, _, err = Read(context.Background(), []byte("value\n-$5\n1234567\n12/31/2026 10:00\n"), fonts, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if c := doc.Sheets[0].Columns; len(c) != 1 || math.Abs(c[0].Width-72.03) > 0.05 {
		t.Errorf("typed column = %+v", c)
	}
}

func TestRead_Model(t *testing.T) {
	doc, w, err := Read(context.Background(), []byte("Name,Amount,When\nAlice,100,1/10\nBob,15%,TRUE\n"), nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	sh := doc.Sheets[0]
	if sh.Name != "" || len(sh.Rows) != 3 || sh.Page.Width != 612 || sh.Page.Height != 792 || math.Abs(sh.Page.Margin[css.Top]-margin) > 1e-9 ||
		math.Abs(sh.Page.Margin[css.Bottom]-margin-footerHeight) > 1e-9 || sh.Page.Scale != 100 || sh.DefaultRowHeight != 12.8 || sh.DefaultColumnWidth != 64 {
		t.Errorf("sheet = %+v", sh)
	}
	hf := sh.HeaderFooter
	if len(hf.Odd.Header[1]) != 0 || len(hf.Odd.Footer[1]) != 2 || hf.Odd.Footer[1][0].Text != "Page " || hf.Odd.Footer[1][1].Field != sheet.FieldPage {
		t.Errorf("header and footer = %+v", hf)
	}
	r1 := sh.Rows[1].Cells
	if r1[0].Value != (sheet.Value{Kind: sheet.Text, Text: "Alice"}) || r1[1].Value.Number != 100 || r1[1].Style != nil || r1[2].Style.Format != "mm/dd/yy" {
		t.Errorf("row 1 = %+v", r1)
	}
	r2 := sh.Rows[2].Cells
	if r2[1].Style.Format != "0.00%" || r2[2].Value.Kind != sheet.Bool {
		t.Errorf("row 2 = %+v", r2)
	}
	if d := doc.Default; d.Font.Families[0] != "Liberation Sans" || d.Font.Size != 10 || d.Padding != [4]float64{1, 1, 1, 1} {
		t.Errorf("default = %+v", d)
	}
	if len(w) != 1 || w[0].Code != "date-without-year" {
		t.Errorf("warnings = %+v", w)
	}
	if _, w, _ := Read(context.Background(), []byte("1/10\n"), nil, time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)); len(w) != 0 {
		t.Errorf("print-dated warnings = %+v", w)
	}
}

// corpusSources is where the csv corpus keeps Gotenberg's files.
const corpusSources = "../../../../test/integration/corpus/csv/sources"

// TestParity_Gotenberg reads Gotenberg's sales.csv and sheet.csv.
//
// # Corpus evidence
//
//   - csv: sales prints one Letter page of 26 rows, the dates as ISO
//     dates, the quoted comments with their commas, "Page 1" in the
//     footer; sheet prints Alice and no header.
func TestParity_Gotenberg(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(corpusSources, "sales", "sales.csv"))
	if err != nil {
		t.Skipf("corpus source not present: %v", err)
	}
	doc, _, err := Read(context.Background(), data, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	sh := doc.Sheets[0]
	if len(sh.Rows) != 26 {
		t.Fatalf("rows = %d", len(sh.Rows))
	}
	r := sh.Rows[3].Cells
	if r[1].Style == nil || r[1].Style.Format != "yyyy-mm-dd" || r[2].Value.Text != "Northwind Traders" || r[5].Value.Number != 14.1 || r[7].Value.Text != "Note, with comma #3" {
		t.Errorf("row 3 = %+v", r)
	}
	data, err = os.ReadFile(filepath.Join(corpusSources, "sheet", "sheet.csv"))
	if err != nil {
		t.Skipf("corpus source not present: %v", err)
	}
	doc, _, err = Read(context.Background(), data, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if c := doc.Sheets[0].Rows[1].Cells; c[0].Value.Text != "Alice" || c[1].Value.Number != 100 {
		t.Errorf("sheet.csv = %+v", c)
	}
}

func FuzzRead(f *testing.F) {
	seed := []byte("id,\"a \"\"b\"\"\",2026-01-10\r\n1;15%;$5\t12:30\n\"x\ny\",TRUE,1/10/2026 10:00\n")
	f.Add(seed)
	f.Add(seed[:len(seed)/2])
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, _, err := Read(context.Background(), data, nil, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range doc.Sheets[0].Rows {
			for _, c := range r.Cells {
				if c.Value.Kind == sheet.Number && (math.IsNaN(c.Value.Number) || math.IsInf(c.Value.Number, 0)) {
					t.Fatalf("non-finite value %v", c.Value)
				}
			}
		}
	})
}
