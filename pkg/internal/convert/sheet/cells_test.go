package sheet

import (
	"context"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// texts returns the texts of a page's runs.
func texts(pg *compose.Page) []string {
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
	g := newGrid(doc, sh)
	b.optimalHeights(g)
	if h := g.rowHeight(0); !near(h, 35, 0.05) {
		t.Errorf("wrapped row %v points, want 35", h)
	}
	if h := g.rowHeight(1); h != 12.78 {
		t.Errorf("plain row %v points, want 12.78", h)
	}
	if h := g.rowHeight(2); h != 20 {
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
	var strokes []*compose.Path
	var fills int
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Path:
				if v.Stroke != nil {
					strokes = append(strokes, v)
				}
				if v.Fill != nil && v.Fill.Color.A > 0 {
					fills++
				}
			case *compose.Group:
				walk(v.Content)
			}
		}
	}
	walk(out.Pages[0].Content)
	if len(strokes) != 1 || strokes[0].Stroke.Width != 2.25 {
		t.Fatalf("%d strokes, want the thick one alone", len(strokes))
	}
	p := strokes[0].Ops
	if p[0].P[0].X != 80 || !near(p[0].P[0].Y, -2.25/2, 1e-9) || !near(p[1].P[0].Y, 12.78+2.25/2, 1e-9) {
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
//   - litho/pdf: a flat ODS of two sheets, three pages, whose header shows
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
	if len(out.Warnings) == 0 || out.Warnings[0].Code != CodeField {
		t.Errorf("warnings %+v, want the file field's", out.Warnings)
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
	var imgs []*compose.Image
	var walk func([]compose.Fragment, compose.Matrix)
	walk = func(fs []compose.Fragment, m compose.Matrix) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Image:
				r := v.Rect
				imgs = append(imgs, &compose.Image{Rect: compose.Rect{X: r.X*m.A + m.E, Y: r.Y*m.D + m.F, Width: r.Width * m.A, Height: r.Height * m.D}})
			case *compose.Group:
				t := v.Transform
				if t == (compose.Matrix{}) {
					t = compose.Identity
				}
				walk(v.Content, mul(t, m))
			}
		}
	}
	walk(out.Pages[0].Content, compose.Identity)
	if len(imgs) != 1 {
		t.Fatalf("%d images, want 1", len(imgs))
	}
	r := imgs[0].Rect
	want := compose.Rect{X: 36 + 55, Y: 36 + 12.78 + 2, Width: 55, Height: 2*12.78 - 2}
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
//   - litho/pdf: a flat ODS cell 80 points wide and tall rotated 90 degrees,
//     Liberation Sans 10, converted by Gotenberg 9.0.0, sets "Rotated90"
//     up from y 726.89 (a point above the row's bottom at 725.89) with its
//     baseline at x 46.09 (the column starts at 36).
func TestConvert_Rotation(t *testing.T) {
	sh := &Sheet{Name: "S", DefaultColumnWidth: 80, Rows: []Row{{
		Index: 0, Height: 80, CustomHeight: true,
		Cells: []Cell{textCell(0, "Rotated90", &Style{Align: Alignment{Rotation: 90}})},
	}}}
	out := convert(t, newDoc(sh), office.Options{})
	var txt *compose.Text
	var m compose.Matrix
	var walk func([]compose.Fragment, compose.Matrix)
	walk = func(fs []compose.Fragment, cur compose.Matrix) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Text:
				txt, m = v, cur
			case *compose.Group:
				tr := v.Transform
				if tr == (compose.Matrix{}) {
					tr = compose.Identity
				}
				walk(v.Content, mul(tr, cur))
			}
		}
	}
	walk(out.Pages[0].Content, compose.Identity)
	if txt == nil {
		t.Fatal("no text")
	}
	x := m.A*txt.Origin.X + m.C*txt.Origin.Y + m.E
	y := m.B*txt.Origin.X + m.D*txt.Origin.Y + m.F
	if !near(x, 46.09, 0.6) || !near(841.89-y, 726.89, 0.6) || !near(m.B, -1, 1e-6) {
		t.Errorf("text origin %.2f, %.2f under %+v, want 46.09, 726.89 turned up", x, 841.89-y, m)
	}
}

// TestConvert_GrowHeader locks Calc's dynamic header and footer heights:
// content taller than the margin's room, plus the spacing, moves the
// cells; a fixed height does not.
func TestConvert_GrowHeader(t *testing.T) {
	tests := []struct {
		name     string
		grow     bool
		spacing  float64
		wantDown float64
	}{
		{name: "fixed height", grow: false, wantDown: 0},
		// "H" in Liberation Sans 10 takes 11.17 points, with 7.09 of
		// spacing 18.26 against the margin's 17.1: the cells move 1.16
		// points down, as sc/databar.xlsx prints them.
		{name: "dynamic height", grow: true, spacing: 7.09, wantDown: 11.17 + 7.09 - 17.1},
		{name: "room enough", grow: true, spacing: 2, wantDown: 0},
	}
	var base float64
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sh := &Sheet{Name: "S", Page: a4(), Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "cell", nil)}}}}
			sh.Page.HeaderTop = 56.7
			sh.Page.Margin[css.Top] = 56.7 + 17.1
			sh.HeaderFooter.Odd.Header[1] = []Span{{Text: "H"}}
			sh.HeaderFooter.GrowHeader, sh.HeaderFooter.HeaderSpacing = tt.grow, tt.spacing
			y := find(t, convert(t, newDoc(sh), office.Options{}).Pages[0], "cell").y
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

// TestConvert_InvalidCircle locks the red ellipse around a cell marked
// invalid: the cell grown 0.25 cm left and right and 0.07 cm up and down,
// stroked 0.55 mm wide.
func TestConvert_InvalidCircle(t *testing.T) {
	c := numCell(1, 5, nil)
	c.MarkedInvalid = true
	sh := &Sheet{Name: "S", Rows: []Row{{Index: 1, Cells: []Cell{c}}}}
	out := convert(t, newDoc(sh), office.Options{})
	var found *compose.Path
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch f := f.(type) {
			case *compose.Group:
				walk(f.Content)
			case *compose.Path:
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
	if p := found.Ops[0].P[0]; !near(p.X, 128+circleGrowX, 1e-6) || !near(found.Stroke.Width, 1.559, 0.001) {
		t.Errorf("circle starts at %+v, width %v", p, found.Stroke.Width)
	}
}

// source returns img encoded as an image source, failing t when it does
// not encode.
func source(t testing.TB, img image.Image) *compose.ImageSource {
	t.Helper()
	src, err := compose.EncodeImage(context.Background(), img)
	if err != nil {
		t.Fatalf("EncodeImage: %v", err)
	}
	return src
}
