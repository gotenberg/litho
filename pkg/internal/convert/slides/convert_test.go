package slides

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

func TestConvert_Pages(t *testing.T) {
	slide := func(name string, hidden bool) *Slide {
		return &Slide{Name: name, Hidden: hidden, Shapes: []*Shape{box(10, 10, 300, 100, para(name, arial(20)))}, Notes: []Paragraph{para("note "+name, arial(12))}}
	}
	doc := &Document{Width: 960, Height: 540, Slides: []*Slide{slide("A", false), slide("B", true), slide("A", false)}}
	for _, tc := range []struct {
		name    string
		opts    office.Options
		pages   int
		sizes   [][2]float64
		outline []string
	}{
		{"shown", office.Options{}, 2, [][2]float64{{960, 540}, {960, 540}}, []string{"A", "A (2)"}},
		{"hidden", office.Options{HiddenSlides: true}, 3, nil, []string{"A", "B", "A (2)"}},
		{"notes", office.Options{NotesPages: true}, 4, [][2]float64{{960, 540}, {960, 540}, {612, 792}, {612, 792}}, []string{"A", "A (2)", "A", "A (2)"}},
		{"paper", office.Options{PageWidth: 595, PageHeight: 842, Landscape: true}, 2, [][2]float64{{842, 595}, {842, 595}}, nil},
		{"no outline", office.Options{OmitOutline: true}, 2, nil, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := convert(t, doc, tc.opts)
			if len(out.Pages) != tc.pages {
				t.Fatalf("%d pages, want %d", len(out.Pages), tc.pages)
			}
			for i, s := range tc.sizes {
				if p := out.Pages[i]; !near(p.Width, s[0], 0.01) || !near(p.Height, s[1], 0.01) {
					t.Errorf("page %d is %v by %v, want %v", i+1, p.Width, p.Height, s)
				}
			}
			if tc.outline != nil {
				var got []string
				for _, o := range out.Outline {
					got = append(got, o.Title)
				}
				if len(got) != len(tc.outline) {
					t.Fatalf("outline %q, want %q", got, tc.outline)
				}
				per := len(got)
				if tc.opts.NotesPages {
					per /= 2
				}
				for i := range got {
					if got[i] != tc.outline[i] || out.Outline[i].Dest.Page != i%per+1 {
						t.Errorf("outline %d: %q to %d, want %q", i, got[i], out.Outline[i].Dest.Page, tc.outline[i])
					}
					// The entry names its slide's Sect as its structure
					// destination (ISO 14289-2, 8.8).
					if e := out.Outline[i].Dest.Elem; e == nil || e != out.Structure.Kids[i%per] {
						t.Errorf("outline %d names %+v", i, e)
					}
				}
			}
		})
	}
}

// TestConvert_PaperFit checks that a slide is scaled down to fit a page
// and centred, never scaled up.
func TestConvert_PaperFit(t *testing.T) {
	doc := deck(&Slide{Shapes: []*Shape{box(0, 0, 300, 100, para("Fit", arial(20)))}})
	for _, tc := range []struct {
		name       string
		w, h       float64
		x, y, size float64
	}{
		{"down", 360, 540, 0, 135 + 10, 10},
		{"never up", 1440, 1080, 360, 270 + 20, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg := convert(t, doc, office.Options{PageWidth: tc.w, PageHeight: tc.h}).Pages[0]
			r := find(t, pg, "Fit")
			if !near(r.x, tc.x, 0.3) || !near(r.y, tc.y, 0.3) || !near(r.size, tc.size, 0.01) {
				t.Errorf("Fit at %.2f, %.2f, size %.2f, want %v, %v, %v", r.x, r.y, r.size, tc.x, tc.y, tc.size)
			}
		})
	}
}

// TestConvert_NotesPage checks the default notes page against Impress's
// (deck.pptx's first notes page: the 16:9 slide 527.98 wide at 42.01,
// 60.19 on Letter).
func TestConvert_NotesPage(t *testing.T) {
	doc := &Document{Width: 960, Height: 540, Slides: []*Slide{{Name: "S", Shapes: []*Shape{box(0, 0, 400, 100, para("Slide", arial(40)))}, Notes: []Paragraph{para("Note", arial(20))}}}}
	pg := convert(t, doc, office.Options{NotesPages: true}).Pages[1]
	s := find(t, pg, "Slide")
	k := 527.98 / 960
	if !near(s.x, 42.01, 0.1) || !near(s.y, 60.19+40*k, 0.2) || !near(s.size, 40*k, 0.05) {
		t.Errorf("slide text at %.2f, %.2f size %.2f", s.x, s.y, s.size)
	}
	n := find(t, pg, "Note")
	if !near(n.x, 61.2+notesInsetX, 0.1) || !near(n.y, 376.2+notesInsetY+20, 0.2) {
		t.Errorf("note at %.2f, %.2f", n.x, n.y)
	}
}

func TestConvert_Errors(t *testing.T) {
	fonts := pinnedFonts(t)
	ctx := context.Background()
	many := deck(&Slide{}, &Slide{}, &Slide{})
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		doc   *Document
		fonts *text.Fonts
		opts  office.Options
		want  error
	}{
		{"nil doc", ctx, nil, fonts, office.Options{}, office.ErrInvalidOptions},
		{"nil fonts", ctx, many, nil, office.Options{}, office.ErrInvalidOptions},
		{"bad options", ctx, many, fonts, office.Options{PageWidth: 10}, office.ErrInvalidOptions},
		{"empty index", ctx, many, &text.Fonts{}, office.Options{}, text.ErrNoFonts},
		{"too many pages", ctx, many, fonts, office.Options{MaxPages: 2}, office.ErrTooManyPages},
		{"cancelled", cancelled, many, fonts, office.Options{}, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Convert(tc.ctx, tc.doc, tc.fonts, tc.opts); !errors.Is(err, tc.want) {
				t.Errorf("err %v, want %v", err, tc.want)
			}
		})
	}
}

// TestConvert_Fields checks the fields a slide resolves: its number in the
// show, hidden slides counted, the count, the print date, the names.
func TestConvert_Fields(t *testing.T) {
	field := func(f Field, saved string) Paragraph {
		return Paragraph{Runs: []Run{{Text: saved, Field: f, Style: arial(20)}}, Style: arial(20)}
	}
	formatted := field(FieldDate, "saved")
	formatted.Runs[0].Format = "MMMM D, YYYY"
	body := []Paragraph{field(FieldSlideNumber, "9"), field(FieldSlideCount, "9"), field(FieldDate, "saved"), field(FieldTime, "saved"), formatted, field(FieldSlideName, ""), field(FieldAuthor, "")}
	doc := &Document{Width: 720, Height: 540, Meta: compose.Metadata{Author: "Ada"}, Slides: []*Slide{{Hidden: true}, {Name: "Second", Shapes: []*Shape{box(0, 0, 400, 500, body...)}}}}
	pg := convert(t, doc, office.Options{PrintDate: time.Date(2026, 11, 8, 9, 30, 0, 0, time.UTC)}).Pages[0]
	var got []string
	for _, r := range runs(pg) {
		got = append(got, r.text)
	}
	want := []string{"2", "2", "11/8/26", "09:30:00 AM", "November 8, 2026", "Second", "Ada"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d: %q, want %q", i, got[i], want[i])
		}
	}
}

// TestConvert_Structure checks the structure tree: a Sect per slide, the
// paragraphs as P, a picture as a Figure, a table as a Table.
func TestConvert_Structure(t *testing.T) {
	pic := &Shape{Kind: KindPicture, Name: "logo", Box: compose.Rect{X: 0, Y: 200, Width: 10, Height: 10}, Picture: tinyPicture(t)}
	tbl := &Shape{Kind: KindTable, Box: compose.Rect{X: 0, Y: 300, Width: 200, Height: 40}, Table: &Table{
		ColumnWidths: []float64{100, 100},
		Rows:         []TableRow{{Height: 20, Cells: []TableCell{{Text: &TextBody{Paragraphs: []Paragraph{para("c1", arial(10))}, Wrap: true}}, {}}}},
	}}
	doc := deck(&Slide{Shapes: []*Shape{box(0, 0, 300, 100, para("one", arial(20)), para("two", arial(20))), pic, tbl}})
	out := convert(t, doc, office.Options{})
	if len(out.Structure.Kids) != 1 || out.Structure.Kids[0].Role != "Sect" {
		t.Fatalf("structure %+v", out.Structure)
	}
	var roles []string
	for _, k := range out.Structure.Kids[0].Kids {
		roles = append(roles, k.Role)
	}
	want := []string{"P", "P", "Figure", "Table"}
	if len(roles) != len(want) {
		t.Fatalf("roles %q, want %q", roles, want)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Errorf("role %d %q, want %q", i, roles[i], want[i])
		}
	}
}

// TestConvert_NotesMaster checks notes pages laid out by a notes master,
// against deck-notes as Impress prints it: a slide with notes in the
// master's slide box, its notes in the master's text box, its number in
// the master's number shape; a later slide without notes in the default
// layout over the notes size. A PresentationML notes page is the notes
// size; on the notes paper of an OpenDocument deck, Letter, the notes page
// sits at the top of the paper, and the first slide without notes takes
// the whole paper.
func TestConvert_NotesMaster(t *testing.T) {
	num := box(305.88, 683.88, 234, 36, Paragraph{Runs: []Run{{Field: FieldSlideNumber, Text: "‹#›", Style: arial(12)}}, Style: arial(12), Align: css.AlignRight})
	num.Text.Insets = [4]float64{3.6, 7.2, 3.6, 7.2}
	title := func(s string) *Slide {
		return &Slide{Name: s, Shapes: []*Shape{box(0, 0, 960, 100, para(s, arial(40)))}}
	}
	withNotes := title("Second")
	withNotes.Notes = []Paragraph{para("NOTE", arial(20))}
	doc := &Document{
		Width: 960, Height: 540, NotesWidth: 540, NotesHeight: 720,
		NotesSlideBox: compose.Rect{X: 90, Y: 54, Width: 360, Height: 270},
		NotesTextBox:  compose.Rect{X: 54, Y: 342, Width: 432, Height: 324},
		NotesShapes:   []*Shape{num},
		Slides:        []*Slide{title("First"), withNotes, title("Third")},
	}
	for _, paper := range []struct{ w, h float64 }{{0, 0}, {612, 792}} {
		testNotesMaster(t, doc, paper.w, paper.h)
	}
}

// testNotesMaster checks the notes pages of doc on the notes paper w by
// h, none for 0.
func testNotesMaster(t *testing.T, doc *Document, w, h float64) {
	t.Helper()
	pages := convert(t, doc, office.Options{NotesPages: true, NotesPaperWidth: w, NotesPaperHeight: h}).Pages
	first := struct{ x, y, scale float64 }{30, 720 * 0.076, 0.5}
	pw, ph := 540.0, 720.0
	if w > 0 {
		first.x, first.y, first.scale = 42.01, 60.19, 527.98/960
		pw, ph = w, h
	}
	for i, tc := range []struct {
		page   int
		text   string
		x, y   float64
		scale  float64
		number bool
	}{
		{3, "First", first.x, first.y, first.scale, false},
		{4, "Second", 90, 54 + (270-202.5)/2, 0.375, true},
		{5, "Third", 30, 720 * 0.076, 0.5, false},
	} {
		pg := pages[tc.page]
		if pg.Width != pw || pg.Height != ph {
			t.Errorf("paper %v: notes page %d is %v by %v", w, i+1, pg.Width, pg.Height)
		}
		r := find(t, pg, tc.text)
		if !near(r.x, tc.x, 0.1) || !near(r.y, tc.y+40*tc.scale, 0.2) || !near(r.size, 40*tc.scale, 0.05) {
			t.Errorf("notes page %d: %s at %.2f, %.2f size %.2f", i+1, tc.text, r.x, r.y, r.size)
		}
		var hasNumber bool
		for _, rr := range runs(pg) {
			if rr.text == "2" && near(rr.x, 526.68, 1) {
				hasNumber = true
			}
		}
		if hasNumber != tc.number {
			t.Errorf("notes page %d: slide number drawn %v, runs %v", i+1, hasNumber, runs(pg))
		}
	}
	if n := find(t, pages[4], "NOTE"); !near(n.x, 61.2, 0.1) || !near(n.y, 365.6, 0.2) {
		t.Errorf("note at %.2f, want 61.2", n.x)
	}
}

// TestConvert_ShapeLinksAreTagged locks that a shape's hyperlink and its
// jump to a slide hang from Link elements of the slide's Sect, the jump
// naming the Sect of its target's page (ISO 14289-1, 7.18.1; ISO 14289-2,
// 8.8).
func TestConvert_ShapeLinksAreTagged(t *testing.T) {
	web := box(10, 10, 300, 100, para("web", arial(20)))
	web.Href = "https://example.com/"
	jump := box(10, 200, 300, 100, para("jump", arial(20)))
	jump.Jump = 2
	doc := deck(&Slide{Shapes: []*Shape{web, jump}}, &Slide{Shapes: []*Shape{box(0, 0, 100, 100, para("two", arial(20)))}})
	out := convert(t, doc, office.Options{})
	links := out.Pages[0].Links
	if len(links) != 2 {
		t.Fatalf("%d links, want 2", len(links))
	}
	for _, l := range links {
		if l.Parent == nil || l.Parent.Role != "Link" || !slices.Contains(out.Structure.Kids[0].Kids, l.Parent) {
			t.Errorf("link %+v hangs from %+v", l, l.Parent)
		}
	}
	if d := links[1].Dest; d == nil || d.Page != 2 || d.Elem != out.Structure.Kids[1] {
		t.Errorf("jump %+v", d)
	}
}

// TestConvert_TableSpans locks a regular table: a merged cell is one TD
// with its spans, the cells it covers have none, and a row the source
// lists short is padded to the table's columns (ISO 14289-1, 7.2).
func TestConvert_TableSpans(t *testing.T) {
	txt := func(s string) *TextBody { return &TextBody{Paragraphs: []Paragraph{para(s, arial(10))}, Wrap: true} }
	tbl := &Shape{Kind: KindTable, Box: compose.Rect{X: 0, Y: 0, Width: 300, Height: 60}, Table: &Table{
		ColumnWidths: []float64{100, 100, 100},
		Rows: []TableRow{
			{Height: 20, Cells: []TableCell{{Text: txt("m"), ColSpan: 2, RowSpan: 2}, {Covered: true}, {Text: txt("c")}}},
			{Height: 20, Cells: []TableCell{{Covered: true}, {Covered: true}}},
			{Height: 20, Cells: []TableCell{{Text: txt("x")}}},
		},
	}}
	out := convert(t, deck(&Slide{Shapes: []*Shape{tbl}}), office.Options{})
	tb := out.Structure.Kids[0].Kids[0]
	var got [][3]int
	for _, tr := range tb.Kids {
		got = append(got, [3]int{len(tr.Kids), 0, 0})
		for _, td := range tr.Kids {
			got[len(got)-1][1] += max(td.ColSpan, 1)
			got[len(got)-1][2] = max(got[len(got)-1][2], td.RowSpan)
		}
	}
	want := [][3]int{{2, 3, 2}, {1, 1, 0}, {3, 3, 0}}
	if !slices.Equal(got, want) {
		t.Errorf("rows (cells, columns, rowspan) %v, want %v", got, want)
	}
}
