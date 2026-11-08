package slides

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
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
		{"notes", office.Options{NotesPages: true}, 4, [][2]float64{{960, 540}, {960, 540}, {612, 792}, {612, 792}}, []string{"A", "A (2)"}},
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
				for i := range got {
					if got[i] != tc.outline[i] || out.Outline[i].Dest.Page != i+1 {
						t.Errorf("outline %d: %q to %d, want %q", i, got[i], out.Outline[i].Dest.Page, tc.outline[i])
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
	body := []Paragraph{field(FieldSlideNumber, "9"), field(FieldSlideCount, "9"), field(FieldDate, "saved"), field(FieldSlideName, ""), field(FieldAuthor, "")}
	doc := &Document{Width: 720, Height: 540, Meta: compose.Metadata{Author: "Ada"}, Slides: []*Slide{{Hidden: true}, {Name: "Second", Shapes: []*Shape{box(0, 0, 400, 400, body...)}}}}
	pg := convert(t, doc, office.Options{PrintDate: time.Date(2026, 11, 8, 9, 30, 0, 0, time.UTC)}).Pages[0]
	var got []string
	for _, r := range runs(pg) {
		got = append(got, r.text)
	}
	want := []string{"2", "2", "11/08/26", "Second", "Ada"}
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
	pic := &Shape{Kind: KindPicture, Name: "logo", Box: compose.Rect{X: 0, Y: 200, Width: 10, Height: 10}, Picture: tinyPicture()}
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
