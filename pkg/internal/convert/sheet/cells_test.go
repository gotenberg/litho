package sheet

import (
	"context"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// texts returns the texts of a page's runs.
func texts(pg *typeset.Page) []string {
	var out []string
	for _, r := range runs(pg) {
		out = append(out, r.text)
	}
	return out
}

// TestConvert_CellText locks what a cell shows: its value formatted,
// "###" for a number too wide, a General number narrowed, the characters
// a clip hides left out, zeros hidden when the sheet asks.
//
// # Corpus evidence
//
//   - gotenberg/xlsx: budget, a first column too narrow for "Cloud hosting" before
//     a cell with content prints "Cloud host" in the PDF's text: the
//     characters that start past the column's edge are left out, the one
//     it cuts kept.
func TestConvert_CellText(t *testing.T) {
	narrow := &Style{Format: "0.00"}
	tests := []struct {
		name  string
		cells []Cell
		edit  func(sh *Sheet)
		want  []string
	}{
		{"format", []Cell{{Column: 0, Value: Value{Kind: Number, Number: 1234.5}, Style: &Style{Format: "#,##0.00"}}}, nil, []string{"1,234.50"}},
		{"hash", []Cell{{Column: 0, Value: Value{Kind: Number, Number: 123456789}, Style: narrow}}, func(sh *Sheet) { sh.DefaultColumnWidth = 30 }, []string{"###"}},
		{"general narrows", []Cell{numCell(0, 1234.5678, nil)}, func(sh *Sheet) { sh.DefaultColumnWidth = 30 }, []string{"1235"}},
		{"clipped text", []Cell{textCell(0, "Cloud hosting", nil), numCell(1, 120000, nil)}, func(sh *Sheet) { sh.DefaultColumnWidth = 48.15 }, []string{"Cloud host", "120000"}},
		{"hidden zero", []Cell{numCell(0, 0, nil), numCell(1, 1, nil)}, func(sh *Sheet) { sh.HideZeros = true }, []string{"1"}},
		{"line breaks without wrap", []Cell{textCell(0, "Line1\r\nLine2", nil)}, nil, []string{"Line1", "Line2"}},
		{"boolean", []Cell{{Column: 0, Value: Value{Kind: Bool, Number: 1}}}, nil, []string{"TRUE"}},
		{"error", []Cell{{Column: 0, Value: Value{Kind: Error, Text: "#DIV/0!"}}}, nil, []string{"#DIV/0!"}},
		// ECMA-376 Part 1, 18.18.60 ST_PrintError.
		{"error printed blank", []Cell{{Column: 0, Value: Value{Kind: Error, Text: "#DIV/0!"}}, textCell(1, "x", nil)}, func(sh *Sheet) { sh.Page = a4(); sh.Page.Errors = ErrorsBlank }, []string{"x"}},
		{"error printed as a dash", []Cell{{Column: 0, Value: Value{Kind: Error, Text: "#DIV/0!"}}}, func(sh *Sheet) { sh.Page = a4(); sh.Page.Errors = ErrorsDash }, []string{"--"}},
		{"error printed as N/A", []Cell{{Column: 0, Value: Value{Kind: Error, Text: "#VALUE!"}}}, func(sh *Sheet) { sh.Page = a4(); sh.Page.Errors = ErrorsNA }, []string{"#N/A"}},
		{"rich text", []Cell{{Column: 0, Value: Value{Kind: Text, Text: "Red Green"}, Runs: []Run{
			{Text: "Red", Font: &Font{Color: css.Color{R: 1, A: 1}}}, {Text: " Green"},
		}}}, nil, []string{"Red", " Green"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sh := &Sheet{Name: "S", Rows: []Row{{Index: 0, Cells: tt.cells}}}
			if tt.edit != nil {
				tt.edit(sh)
			}
			out := convert(t, newDoc(sh), office.Options{})
			got := texts(out.Pages[0])
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("texts %q, want %q", got, tt.want)
			}
		})
	}
}

// TestConvert_RowHeight locks the optimal height of a row without a
// custom one: three wrapped lines of Liberation Sans 10 and a point of
// padding make 35 points, 47 screen pixels; a custom height is kept.
func TestConvert_RowHeight(t *testing.T) {
	wrap := &Style{Align: Alignment{Wrap: true}}
	sh := &Sheet{Name: "S", DefaultColumnWidth: 80, Rows: []Row{
		{Index: 0, Cells: []Cell{textCell(0, "A long text that wraps in the cell width", wrap)}},
		{Index: 1, Cells: []Cell{textCell(0, "next", nil)}},
		{Index: 2, Height: 20, CustomHeight: true, Cells: []Cell{textCell(0, "A long text that wraps in the cell width", wrap)}},
		{Index: 3, Cells: []Cell{textCell(0, "last", nil)}},
	}}
	doc := newDoc(sh)
	b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
	g := newGrid(t.Context(), doc, sh)
	b.heightsOf(t, g)
	if h := g.layoutRowHeight(0); !near(h, 35, 0.05) {
		t.Errorf("wrapped row %v points, want 35", h)
	}
	if h := g.layoutRowHeight(1); h != 12.78 {
		t.Errorf("plain row %v points, want 12.78", h)
	}
	if h := g.layoutRowHeight(2); h != 20 {
		t.Errorf("custom row %v points, want 20", h)
	}
}

// TestConvert_Borders locks the border lines: a shared edge drawn once
// with the heavier line, centred on the edge and reaching half its width
// past the corners, as Calc draws a cell frame.
func TestConvert_Borders(t *testing.T) {
	thin := Border{Line: LineSolid, Width: 0.75, Color: css.Color{A: 1}}
	thick := Border{Line: LineSolid, Width: 2.25, Color: css.Color{A: 1}}
	a := &Style{Border: [4]Border{css.Right: thin}, Fill: Fill{Color: css.Color{R: 1, G: 1, A: 1}}}
	bst := &Style{Border: [4]Border{css.Left: thick}}
	sh := &Sheet{Name: "S", DefaultColumnWidth: 80, Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "a", a), textCell(1, "b", bst)}}}}
	out := convert(t, newDoc(sh), office.Options{})
	var strokes []*typeset.Path
	var fills int
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				if v.Stroke != nil {
					strokes = append(strokes, v)
				}
				if v.Fill != nil && v.Fill.Color.A > 0 {
					fills++
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(out.Pages[0].Content)
	if len(strokes) != 1 || strokes[0].Stroke.Width != 2.25 {
		t.Fatalf("%d strokes, want the thick one alone", len(strokes))
	}
	p := strokes[0].Ops
	// The edge sits where Calc prints column B: 80 points, 2,822 hundredths
	// of a millimetre.
	if !near(p[0].P[0].X, 2822*72/2540.0, 1e-9) || !near(p[0].P[0].Y, -2.25/2, 1e-9) || !near(p[1].P[0].Y, calcPrinted(12.78)+2.25/2, 1e-9) {
		t.Errorf("edge %+v", p)
	}
	if fills != 1 {
		t.Errorf("%d fills, want 1", fills)
	}
}

// TestConvert_HeaderFooter locks the header and footer fields: the page
// number from the sheet's first page number, the document's page count,
// the sheet name, the print date and time in Calc's en-US formats, and
// the file name left empty with a warning.
//
// # Corpus evidence
//
//   - probe, ods: a flat ODS of two sheets, three pages, whose header shows
//     every field, converted by Gotenberg 9.0.0: "D:11/07/2026
//     T:21:39:35", "S:Alpha P:1/3" and on the third page "S:Beta P:3/3";
//     the ODS default header centres "Revenue" at x 306 on Letter, its
//     baseline 67.25 points from the top with 2 cm margins.
func TestConvert_HeaderFooter(t *testing.T) {
	sh := &Sheet{Name: "Alpha", Page: a4(), Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "cell", nil)}}}}
	sh.Page.Margin = [4]float64{56.69, 56.69, 56.69, 56.69}
	sh.Page.HeaderTop = 56.69
	sh.Page.FirstPageNumber = 5
	sh.HeaderFooter.Odd.Header = [3][]Span{
		{{Text: "D:"}, {Field: FieldDate}, {Text: " T:"}, {Field: FieldTime}},
		{{Text: "S:"}, {Field: FieldSheet}, {Text: " P:"}, {Field: FieldPage}, {Text: "/"}, {Field: FieldPages}},
		{{Text: "F:"}, {Field: FieldFile}},
	}
	sh.HeaderFooter.HeaderMinHeight = 14.17
	date := time.Date(2026, 11, 7, 21, 39, 35, 0, time.UTC)
	out := convert(t, newDoc(sh), office.Options{PrintDate: date})
	// The parts' runs, joined left to right along the header's baseline.
	var parts []string
	var cur strings.Builder
	lastEnd, baseline := -1.0, 0.0
	for _, r := range runs(out.Pages[0]) {
		if r.text == "cell" {
			continue
		}
		if lastEnd >= 0 && r.x > lastEnd+1 {
			parts = append(parts, cur.String())
			cur.Reset()
		}
		cur.WriteString(r.text)
		lastEnd, baseline = r.endX, r.y
	}
	parts = append(parts, cur.String())
	if strings.Join(parts, "|") != "D:11/07/2026 T:21:39:35|S:Alpha P:5/1|F:" {
		t.Errorf("header parts %q", parts)
	}
	// Centred in a 14.17-point header 56.69 points down, the baseline
	// 67.25 points from the top as LibreOffice prints it.
	if !near(baseline, 841.89-67.25, 0.3) {
		t.Errorf("header baseline %v", baseline)
	}
	if len(out.Warnings) == 0 || out.Warnings[0].Code != warn.PageSetupUnsupported {
		t.Errorf("warnings %+v, want the file field's", out.Warnings)
	}
}

// TestConvert_HeaderRestart locks a part started over: the text after a
// FieldRestart span is laid out from the part's origin, over the text
// before it, on the same baseline.
func TestConvert_HeaderRestart(t *testing.T) {
	sh := &Sheet{Name: "S", Page: a4(), Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "cell", nil)}}}}
	sh.Page.HeaderTop = 20
	sh.HeaderFooter.Odd.Header[0] = []Span{{Text: "first"}, {Field: FieldRestart}, {Text: "again"}}
	out := convert(t, newDoc(sh), office.Options{})
	at := map[string][2]float64{}
	for _, r := range runs(out.Pages[0]) {
		at[r.text] = [2]float64{r.x, r.y}
	}
	first, okF := at["first"]
	again, okA := at["again"]
	if !okF || !okA || !near(first[0], again[0], 0.01) || !near(first[1], again[1], 0.01) {
		t.Errorf("first at %v, again at %v, want the same origin", first, again)
	}
}

// TestConvert_Drawings locks a picture's placement by its anchors, over
// the cells, and the chart hook's warning.
func TestConvert_Drawings(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	pic := &layout.Picture{Source: source(t, img), Width: 2, Height: 2}
	sh := &Sheet{
		Name: "S", DefaultColumnWidth: 50, Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "x", nil)}}},
		Drawings: []Drawing{
			{From: Anchor{Row: 1, Column: 1, DX: 5, DY: 2}, To: &Anchor{Row: 3, Column: 2, DX: 10}, Picture: pic, Name: "pic"},
			{From: Anchor{Row: 0, Column: 3}, Width: 20, Height: 10, Chart: nil, Name: "none"},
		},
	}
	out := convert(t, newDoc(sh), office.Options{})
	var imgs []*typeset.Image
	var walk func([]typeset.Fragment, typeset.Matrix)
	walk = func(fs []typeset.Fragment, m typeset.Matrix) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Image:
				r := v.Rect
				imgs = append(imgs, &typeset.Image{Rect: typeset.Rect{X: r.X*m.A + m.E, Y: r.Y*m.D + m.F, Width: r.Width * m.A, Height: r.Height * m.D}})
			case *typeset.Group:
				t := v.Transform
				if t == (typeset.Matrix{}) {
					t = typeset.Identity
				}
				walk(v.Content, mul(t, m))
			}
		}
	}
	walk(out.Pages[0].Content, typeset.Identity)
	if len(imgs) != 1 {
		t.Fatalf("%d images, want 1", len(imgs))
	}
	r := imgs[0].Rect
	want := typeset.Rect{X: 36 + 55, Y: 36 + 12.78 + 2, Width: 55, Height: 2*12.78 - 2}
	if !near(r.X, want.X, 1e-6) || !near(r.Y, want.Y, 1e-6) || !near(r.Width, want.Width, 1e-6) || !near(r.Height, want.Height, 1e-6) {
		t.Errorf("image at %+v, want %+v", r, want)
	}
}

// TestConvert_Rotation locks a quarter turn: the text reads up from the
// cell's bottom-left corner, its baseline a padding and an ascent right of
// the cell's left edge.
//
// # Corpus evidence
//
//   - probe, ods: a flat ODS cell 80 points wide and tall rotated 90 degrees,
//     Liberation Sans 10, converted by Gotenberg 9.0.0, sets "Rotated90"
//     up from y 726.89 (a point above the row's bottom at 725.89) with its
//     baseline at x 46.09 (the column starts at 36).
func TestConvert_Rotation(t *testing.T) {
	sh := &Sheet{Name: "S", DefaultColumnWidth: 80, Rows: []Row{{
		Index: 0, Height: 80, CustomHeight: true,
		Cells: []Cell{textCell(0, "Rotated90", &Style{Align: Alignment{Rotation: 90}})},
	}}}
	out := convert(t, newDoc(sh), office.Options{})
	var txt *typeset.Text
	var m typeset.Matrix
	var walk func([]typeset.Fragment, typeset.Matrix)
	walk = func(fs []typeset.Fragment, cur typeset.Matrix) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Text:
				txt, m = v, cur
			case *typeset.Group:
				tr := v.Transform
				if tr == (typeset.Matrix{}) {
					tr = typeset.Identity
				}
				walk(v.Content, mul(tr, cur))
			}
		}
	}
	walk(out.Pages[0].Content, typeset.Identity)
	if txt == nil {
		t.Fatal("no text")
	}
	x := m.A*txt.Origin.X + m.C*txt.Origin.Y + m.E
	y := m.B*txt.Origin.X + m.D*txt.Origin.Y + m.F
	if !near(x, 46.09, 0.6) || !near(841.89-y, 726.89, 0.6) || !near(m.B, -1, 1e-6) {
		t.Errorf("text origin %.2f, %.2f under %+v, want 46.09, 726.89 turned up", x, 841.89-y, m)
	}
}

// TestConvert_RotatedKeptWhole checks that a rotated text wider than its
// column is kept whole, where LibreOffice shortens a clipped text that
// runs across the cell.
//
// # Corpus evidence
//
//   - litho/ods: cjk-cells prints "UDHR 1948" and "1948年12月10日"
//     turned a quarter in a 15 mm column whole.
func TestConvert_RotatedKeptWhole(t *testing.T) {
	sh := &Sheet{Name: "S", DefaultColumnWidth: 30, Rows: []Row{{
		Index: 0, Height: 120, CustomHeight: true,
		Cells: []Cell{textCell(0, "UDHR 1948", &Style{Align: Alignment{Rotation: 90}})},
	}}}
	out := convert(t, newDoc(sh), office.Options{})
	var got strings.Builder
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Text:
				for _, g := range v.Glyphs {
					got.WriteString(g.Text)
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(out.Pages[0].Content)
	if !strings.Contains(got.String(), "UDHR 1948") {
		t.Errorf("text = %q, want UDHR 1948 whole", got.String())
	}
}

// TestConvert_StackedRunLines checks that the stacked characters of a
// run in another font than the cell's are as far apart as that font's
// ascent and descent: DejaVu Sans 10 under a Liberation Sans 10 cell,
// 11.64 points, not Liberation Sans's 11.17.
//
// # Corpus evidence
//
//   - litho/ods: cjk-cells prints its stacked Han characters, in Noto
//     Sans CJK JP 11 under a Liberation Sans 11 cell font, 16.02 points
//     apart, Noto Sans CJK's ascent and descent.
func TestConvert_StackedRunLines(t *testing.T) {
	c := textCell(0, "abc", &Style{Align: Alignment{Stacked: true}})
	c.Runs = []Run{{Text: "abc", Font: &Font{Families: []string{"DejaVu Sans"}, Size: 10}}}
	sh := &Sheet{Name: "S", Rows: []Row{{Index: 0, Height: 80, CustomHeight: true, Cells: []Cell{c}}}}
	got := runs(convert(t, newDoc(sh), office.Options{}).Pages[0])
	if len(got) != 3 {
		t.Fatalf("runs = %+v, want three stacked lines", got)
	}
	for i := 1; i < len(got); i++ {
		if d := got[i-1].y - got[i].y; !near(d, 11.64, 0.05) {
			t.Errorf("lines %d and %d are %.2f points apart, want 11.64", i-1, i, d)
		}
	}
}

// TestConvert_GrowHeader locks Calc's dynamic header and footer heights:
// content taller than the margin's room, plus the spacing, moves the
// cells; a fixed height does not. Header text naming no font takes
// HeaderFooter.Font, Liberation Sans 10 points when it names none.
//
// # Corpus evidence
//
//   - litho/ods paper-grid-margins-narrow-a5-portrait: a header whose
//     text names no font prints in the book's default cell font, Carlito
//     11 points, and its 13.43 points with the 7.2 point spacing move the
//     cells 56.63 points down from a 36 point margin.
func TestConvert_GrowHeader(t *testing.T) {
	carlito := Font{Families: []string{"Carlito"}, Size: 11}
	tests := []struct {
		name     string
		grow     bool
		spacing  float64
		font     Font
		wantDown float64
	}{
		{name: "fixed height", grow: false, wantDown: 0},
		// "H" in Liberation Sans 10 takes 11.17 points, with 7.09 of
		// spacing 18.26 against the margin's 17.1: the cells move 1.16
		// points down, as sc/databar.xlsx prints them.
		{name: "dynamic height", grow: true, spacing: 7.09, wantDown: 11.17 + 7.09 - 17.1},
		{name: "room enough", grow: true, spacing: 2, wantDown: 0},
		// Carlito 11 takes 13.43 points.
		{name: "document font", grow: true, spacing: 7.09, font: carlito, wantDown: 13.43 + 7.09 - 17.1},
	}
	var base float64
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sh := &Sheet{Name: "S", Page: a4(), Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "cell", nil)}}}}
			sh.Page.HeaderTop = 56.7
			sh.Page.Margin[css.Top] = 56.7 + 17.1
			sh.HeaderFooter.Odd.Header[1] = []Span{{Text: "H"}}
			sh.HeaderFooter.GrowHeader, sh.HeaderFooter.HeaderSpacing = tt.grow, tt.spacing
			sh.HeaderFooter.Font = tt.font
			pg := convert(t, newDoc(sh), office.Options{}).Pages[0]
			if h, want := find(t, pg, "H").size, max(tt.font.Size, 10); h != want {
				t.Errorf("header text at %v points, want %v", h, want)
			}
			y := find(t, pg, "cell").y
			if i == 0 {
				base = y
				return
			}
			if !near(base-y, tt.wantDown, 0.2) {
				t.Errorf("cells moved %.2f down, want %.2f", base-y, tt.wantDown)
			}
		})
	}
}

// TestConvert_FormatFill locks the "*x" fill of a number format: the
// character repeated where the code puts it until the cell is full, a
// fill of spaces too.
func TestConvert_FormatFill(t *testing.T) {
	for _, tc := range []struct{ code, head, tail string }{
		{`"£"* #,##0.00`, "£", "892.75"},
		{`"£"*-#,##0.00`, "£", "892.75"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			st := &Style{Format: tc.code}
			sh := &Sheet{Name: "S", Columns: []Column{{First: 0, Last: 0, Width: 150}}, Rows: []Row{{Index: 0, Cells: []Cell{numCell(0, 892.75, st)}}}}
			rs := runs(convert(t, newDoc(sh), office.Options{}).Pages[0])
			if len(rs) != 1 || !strings.HasPrefix(rs[0].text, tc.head) || !strings.HasSuffix(rs[0].text, tc.tail) || len(rs[0].text) < 30 || rs[0].x > 40 {
				t.Errorf("runs %+v", rs)
			}
		})
	}
}

// TestConvert_BorderClip checks that the clip of a page's cells lets the
// outer half of a border on their edge print, under Calc's rules alone.
func TestConvert_BorderClip(t *testing.T) {
	thick := Border{Line: LineSolid, Width: 2, Color: css.Color{A: 1}}
	st := &Style{Border: [4]Border{css.Bottom: thick}}
	for _, excel := range []bool{false, true} {
		sh := &Sheet{Name: "S", Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "a", st)}}}}
		doc := newDoc(sh)
		doc.ExcelRules = excel
		out := convert(t, doc, office.Options{})
		var clip *typeset.Path
		for _, f := range out.Pages[0].Content {
			if g, ok := f.(*typeset.Group); ok && g.Clip != nil {
				clip = g.Clip
			}
		}
		if clip == nil {
			t.Fatalf("excel %v: no clipped group", excel)
		}
		bottom := 0.0
		for _, op := range clip.Ops {
			bottom = max(bottom, op.P[0].Y)
		}
		h := calcPrinted(12.78)
		want := h + 1
		if excel {
			want = 12.78
		}
		if !near(bottom, want, 1e-9) {
			t.Errorf("excel %v: clip bottom %v, want %v", excel, bottom, want)
		}
	}
}

// TestConvert_InvalidCircle locks the red ellipse around a cell marked
// invalid: the cell grown 0.25 cm left and right and 0.07 cm up and down,
// stroked 0.55 mm wide.
func TestConvert_InvalidCircle(t *testing.T) {
	c := numCell(1, 5, nil)
	c.MarkedInvalid = true
	sh := &Sheet{Name: "S", Rows: []Row{{Index: 1, Cells: []Cell{c}}}}
	out := convert(t, newDoc(sh), office.Options{})
	var found *typeset.Path
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch f := f.(type) {
			case *typeset.Group:
				walk(f.Content)
			case *typeset.Path:
				if f.Stroke != nil && f.Stroke.Color.R == 1 && f.Stroke.Color.G == 0 {
					found = f
				}
			}
		}
	}
	walk(out.Pages[0].Content)
	if found == nil {
		t.Fatal("no circle drawn")
	}
	// The ellipse's rightmost point is the cell's right edge grown.
	if p := found.Ops[0].P[0]; !near(p.X, 2*calcPrinted(64)+circleGrowX, 1e-6) || !near(found.Stroke.Width, 1.559, 0.001) {
		t.Errorf("circle starts at %+v, width %v", p, found.Stroke.Width)
	}
}

// source returns img encoded as an image source, failing t when it does
// not encode.
func source(t testing.TB, img image.Image) *typeset.ImageSource {
	t.Helper()
	src, err := typeset.EncodeImage(context.Background(), img)
	if err != nil {
		t.Fatalf("EncodeImage: %v", err)
	}
	return src
}

// excelDoc returns a document of sh under Excel's rules: Carlito 11 as the
// Default font, Excel's insets of 8 printer pixels at the top and 1 at the
// bottom, a standard row of 121 printer pixels.
func excelDoc(sh *Sheet) *Document {
	doc := newDoc(sh)
	doc.ExcelRules = true
	doc.StandardRowHeight = 14.52
	sh.DefaultRowHeight = 14.52
	doc.Default = Style{Font: Font{Families: []string{"Carlito"}, Size: 11}, Padding: [4]float64{0.96, 1.44, 0.12, 2.4}}
	return doc
}

// TestConvert_ExcelBaselines locks where Excel sets a cell's text in its
// row: a bottom-aligned line on one baseline whatever its font, the
// standard row less the top padding and the Default font's ascent above
// the cell's bottom; a top-aligned line its ascent under the top padding;
// a centred line in the middle of the padded box, a face's line gap above
// its ascent; wrapped lines a standard row apart.
func TestConvert_ExcelBaselines(t *testing.T) {
	big := &Style{Font: Font{Families: []string{"Carlito"}, Size: 36}}
	serif := &Style{Font: Font{Families: []string{"Liberation Serif"}, Size: 11}}
	top := &Style{Align: Alignment{Vertical: VTop}}
	mid := &Style{Font: Font{Families: []string{"Carlito"}, Size: 14}, Align: Alignment{Vertical: VMiddle}}
	wrap := &Style{Align: Alignment{Wrap: true}}
	serifTop := &Style{Font: serif.Font, Align: Alignment{Vertical: VTop}}
	serifMid := &Style{Font: serif.Font, Align: Alignment{Vertical: VMiddle}}
	sh := &Sheet{Name: "S", Rows: []Row{
		{Index: 0, Cells: []Cell{textCell(0, "small", nil), textCell(1, "large", big), textCell(2, "serif", serif)}},
		{Index: 1, Height: 30, CustomHeight: true, Cells: []Cell{textCell(0, "topped", top), textCell(1, "tall", big)}},
		{Index: 2, Height: 30, CustomHeight: true, Cells: []Cell{textCell(0, "centred", mid)}},
		{Index: 3, Height: 45, CustomHeight: true, Cells: []Cell{textCell(0, "one two three", wrap)}},
		{Index: 4, Height: 30, CustomHeight: true, Cells: []Cell{textCell(0, "serif top", serifTop), textCell(1, "serif centre", serifMid)}},
	}}
	sh.DefaultColumnWidth = 30
	pg := convert(t, excelDoc(sh), office.Options{}).Pages[0]
	m := newMeasurer(context.Background(), pinnedFonts(t), "")
	ascent := func(f font) float64 { return m.lineHeight(f) - m.descent(f) }
	cal11 := font{families: []string{"Carlito"}, size: 11}
	cal14 := font{families: []string{"Carlito"}, size: 14}
	above := 14.52 - 0.96 - ascent(cal11)
	baseline := func(s string) float64 { return pg.Height - find(t, pg, s).y }
	for _, s := range []string{"small", "large", "serif"} {
		if got, want := baseline(s), 36+14.52-above; !near(got, want, 0.01) {
			t.Errorf("%q baseline %.3f from the top, want %.3f", s, got, want)
		}
	}
	if got, want := baseline("tall"), 36+14.52+30-above; !near(got, want, 0.01) {
		t.Errorf("36 point text in a custom row: baseline %.3f, want %.3f", got, want)
	}
	if got, want := baseline("topped"), 36+14.52+0.96+ascent(cal11); !near(got, want, 0.01) {
		t.Errorf("top-aligned baseline %.3f, want %.3f", got, want)
	}
	line14 := m.lineHeight(cal14)
	if got, want := baseline("centred"), 36+14.52+30+0.96+(30-0.96-0.12-line14)/2+ascent(cal14); !near(got, want, 0.01) {
		t.Errorf("centred baseline %.3f, want %.3f", got, want)
	}
	one, two := baseline("one "), baseline("two ")
	if !near(two-one, 14.52, 0.01) {
		t.Errorf("wrapped lines %.3f apart, want the standard row's 14.52", two-one)
	}
	if got, want := baseline("three"), 36+14.52+30+30+45-above; !near(got, want, 0.01) {
		t.Errorf("last wrapped line's baseline %.3f, want %.3f", got, want)
	}
	// A face with a line gap sets it above its ascent, as GDI sets a line.
	ser := font{families: []string{"Liberation Serif"}, size: 11}
	gap := m.lineGap(ser)
	if gap < 0.3 {
		t.Fatalf("Liberation Serif 11's line gap %.3f, want one to measure", gap)
	}
	row := 36 + 14.52 + 30 + 30 + 45
	if got, want := baseline("serif top"), row+0.96+gap+ascent(ser); !near(got, want, 0.01) {
		t.Errorf("top-aligned serif baseline %.3f, want %.3f", got, want)
	}
	if got, want := baseline("serif centre"), row+0.96+(30-0.96-0.12-m.lineHeight(ser)-gap)/2+gap+ascent(ser); !near(got, want, 0.01) {
		t.Errorf("centred serif baseline %.3f, want %.3f", got, want)
	}
}

// TestConvert_ExcelHeaderFooterInsets locks where Excel sets the parts of
// a header and a footer: each part on its own, its first line 8 printer
// pixels below the header margin, a face's line gap above its ascent, its
// last line 15 above the footer margin, so that the baseline of a footer
// part in a smaller font sits lower.
func TestConvert_ExcelHeaderFooterInsets(t *testing.T) {
	sh := &Sheet{Name: "S", Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "cell", nil)}}}}
	doc := excelDoc(sh)
	sh.Page.Margin = [4]float64{54, 50, 54, 50}
	sh.Page.HeaderTop, sh.Page.FooterBottom = 21.6, 21.6
	cal := func(size float64) *Font { return &Font{Families: []string{"Carlito"}, Size: size} }
	serif := &Font{Families: []string{"Liberation Serif"}, Size: 11}
	sh.HeaderFooter.Odd.Header = [3][]Span{{{Text: "left", Font: cal(11)}}, {{Text: "centre", Font: cal(20)}}, {{Text: "serif", Font: serif}}}
	sh.HeaderFooter.Odd.Footer = [3][]Span{{{Text: "foot", Font: cal(11)}}, {{Text: "serif foot", Font: serif}}, {{Text: "small", Font: cal(8)}}}
	pg := convert(t, doc, office.Options{}).Pages[0]
	m := newMeasurer(context.Background(), pinnedFonts(t), "")
	baseline := func(s string) float64 { return pg.Height - find(t, pg, s).y }
	ser := font{families: []string{"Liberation Serif"}, size: 11}
	for _, tc := range []struct {
		text string
		f    font
	}{{"left", font{families: []string{"Carlito"}, size: 11}}, {"centre", font{families: []string{"Carlito"}, size: 20}}, {"serif", ser}} {
		// A face's line gap sits above its ascent.
		if got, want := baseline(tc.text), 21.6+0.96+m.lineGap(tc.f)+m.lineHeight(tc.f)-m.descent(tc.f); !near(got, want, 0.01) {
			t.Errorf("header part %q: baseline %.3f from the top, want %.3f", tc.text, got, want)
		}
	}
	for _, tc := range []struct {
		text string
		f    font
	}{{"foot", font{families: []string{"Carlito"}, size: 11}}, {"serif foot", ser}, {"small", font{families: []string{"Carlito"}, size: 8}}} {
		f := tc.f
		if got, want := baseline(tc.text), pg.Height-21.6-1.8-m.descent(f); !near(got, want, 0.01) {
			t.Errorf("footer part %q: baseline %.3f from the top, want %.3f", tc.text, got, want)
		}
	}
}

// TestConvert_ExcelHeaderPictures locks where a header and footer part's
// picture prints: a header's top at the header margin, a footer's bottom
// at the footer margin, the left part's at the left margin, the centre
// part's centred between the margins, the right part's at the right one.
func TestConvert_ExcelHeaderPictures(t *testing.T) {
	sh := &Sheet{Name: "S", Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "cell", nil)}}}}
	doc := excelDoc(sh)
	sh.Page.Margin = [4]float64{54, 50, 54, 40}
	sh.Page.HeaderTop, sh.Page.FooterBottom = 21.6, 18
	pic := func(w, h float64) []Span {
		return []Span{{Picture: &HeaderPicture{Width: w, Height: h, Picture: &layout.Picture{Width: w, Height: h, Source: &typeset.ImageSource{Width: 1, Height: 1}}}}}
	}
	sh.HeaderFooter.Odd.Header = [3][]Span{pic(100, 40), nil, pic(30, 10)}
	sh.HeaderFooter.Odd.Footer = [3][]Span{nil, pic(80, 20), nil}
	pg := convert(t, doc, office.Options{}).Pages[0]
	var got []typeset.Rect
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Image:
				got = append(got, v.Rect)
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(pg.Content)
	// The margins print in whole printer pixels ([excelMargin]).
	l, r := excelMargin(40), excelMargin(50)
	want := []typeset.Rect{{X: l, Y: 21.6, Width: 100, Height: 40}, {X: pg.Width - r - 30, Y: 21.6, Width: 30, Height: 10}, {X: l + (pg.Width-l-r-80)/2, Y: pg.Height - 18 - 20, Width: 80, Height: 20}}
	if len(got) != len(want) {
		t.Fatalf("pictures %+v, want %+v", got, want)
	}
	for i := range want {
		if !near(got[i].X, want[i].X, 1e-6) || !near(got[i].Y, want[i].Y, 1e-6) || got[i].Width != want[i].Width || got[i].Height != want[i].Height {
			t.Errorf("picture %d at %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestConvert_ExcelScaledRows locks a sheet Excel prints at 50%: each row
// rounded to a printer pixel at the scale, 121 pixels printing 61, and the
// top inset kept whole, so that a bottom-aligned line sits 8 pixels and its
// scaled ascent below the row's top in a row of the standard height.
func TestConvert_ExcelScaledRows(t *testing.T) {
	sh := &Sheet{Name: "S", Rows: []Row{
		{Index: 0, Cells: []Cell{textCell(0, "first", nil)}},
		{Index: 1, Cells: []Cell{textCell(0, "second", nil)}},
	}}
	doc := excelDoc(sh)
	sh.Page.Scale = 50
	pg := convert(t, doc, office.Options{}).Pages[0]
	m := newMeasurer(context.Background(), pinnedFonts(t), "")
	cal11 := font{families: []string{"Carlito"}, size: 11}
	baseline := func(s string) float64 { return pg.Height - find(t, pg, s).y }
	if got := baseline("second") - baseline("first"); !near(got, 61*excelPixel, 0.001) {
		t.Errorf("rows %.4f apart, want 61 printer pixels, %.4f", got, 61*excelPixel)
	}
	if got, want := baseline("first"), 36+0.96+0.5*(m.lineHeight(cal11)-m.descent(cal11)); !near(got, want, 0.01) {
		t.Errorf("first baseline %.3f from the top, want %.3f", got, want)
	}
}

// TestConvert_ExcelScripts locks Excel's superscript and subscript: set at
// two thirds of the size, a superscript raised half the size and a
// subscript lowered a fifth from the line's baseline, a cell font of a
// script and a run alike, the line's baseline where the cell's alignment
// puts it whatever the runs' sizes.
func TestConvert_ExcelScripts(t *testing.T) {
	sup := &Style{Font: Font{Families: []string{"Carlito"}, Size: 11, Script: Superscript}}
	sub := &Style{Font: Font{Families: []string{"Carlito"}, Size: 11, Script: Subscript}}
	big := &Font{Families: []string{"Carlito"}, Size: 28}
	small := &Font{Families: []string{"Carlito"}, Size: 11, Script: Subscript}
	rich := Cell{Column: 0, Value: Value{Kind: Text, Text: "H2O big"}, Runs: []Run{{Text: "H"}, {Text: "2", Font: small}, {Text: "O "}, {Text: "big", Font: big}}}
	sh := &Sheet{Name: "S", Rows: []Row{
		{Index: 0, Cells: []Cell{textCell(0, "plain", nil), textCell(1, "raised", sup), textCell(2, "lowered", sub)}},
		{Index: 1, Cells: []Cell{rich}},
	}}
	sh.DefaultColumnWidth = 40
	pg := convert(t, excelDoc(sh), office.Options{}).Pages[0]
	baseline := func(s string) float64 { return pg.Height - find(t, pg, s).y }
	plain := baseline("plain")
	if got := plain - baseline("raised"); !near(got, 5.5, 0.01) {
		t.Errorf("superscript cell raised %.3f, want half of 11 points", got)
	}
	if got := baseline("lowered") - plain; !near(got, 2.2, 0.01) {
		t.Errorf("subscript cell lowered %.3f, want a fifth of 11 points", got)
	}
	if r := find(t, pg, "raised"); !near(r.size, 11*2.0/3, 0.01) {
		t.Errorf("superscript at %.3f points, want two thirds of 11", r.size)
	}
	line := baseline("H")
	if !near(line, plain+14.52, 0.01) {
		t.Errorf("a line holding a 28-point run and a subscript has its baseline %.3f, want the row's %.3f", line, plain+14.52)
	}
	if got := baseline("2") - line; !near(got, 2.2, 0.01) {
		t.Errorf("subscript run lowered %.3f, want 2.2", got)
	}
}
