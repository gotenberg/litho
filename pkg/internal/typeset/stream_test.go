package typeset_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// streamDoc builds a tagged document of n pages whose pages link to each
// other and to the web, with an outline, a picture shared by every page, a
// translucent fill and a structure tree, each page's content under an
// element of its own.
func streamDoc(t *testing.T, n int) *typeset.Document {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = uint8(40 * (i % 5))
	}
	src := encode(t, img)
	doc := &typeset.Document{Structure: &typeset.StructElem{Role: "Document"}, Metadata: typeset.Metadata{Title: "Stream", Lang: "en"}}
	for i := range n {
		sect := &typeset.StructElem{Role: "Sect"}
		h := &typeset.StructElem{Role: "H1"}
		link := &typeset.StructElem{Role: "Link"}
		fig := &typeset.StructElem{Role: "Figure", Alt: "a square"}
		sect.Kids = []*typeset.StructElem{h, link, fig}
		doc.Structure.Kids = append(doc.Structure.Kids, sect)
		face := sans
		if i%2 == 1 {
			face = serif
		}
		title := run(t, face, 18, typeset.Point{X: 72, Y: 90}, fmt.Sprintf("Page %d", i+1))
		title.Parent = h
		body := run(t, sans, 11, typeset.Point{X: 72, Y: 130}, "Go to the next page")
		body.Parent = link
		pg := letter(
			&typeset.Path{Ops: rect(0, 0, 612, 40), Fill: &typeset.Fill{Color: typeset.Color{R: 0.2, G: 0.4, B: 0.8, A: 0.5}}},
			title, body,
			&typeset.Group{Transform: typeset.Matrix{A: 1, D: 1, E: 72, F: 200}, Content: []typeset.Fragment{
				&typeset.Image{Rect: typeset.Rect{Width: 40, Height: 40}, Source: src, Parent: fig},
			}},
		)
		pg.Links = []typeset.Link{
			{Rect: typeset.Rect{X: 72, Y: 118, Width: 120, Height: 14}, Dest: &typeset.Dest{Page: (i+1)%n + 1, Y: 72}, Parent: link},
			{Rect: typeset.Rect{X: 72, Y: 150, Width: 120, Height: 14}, URI: "https://example.com/"},
		}
		doc.Pages = append(doc.Pages, pg)
	}
	for i, pg := range doc.Pages {
		// A link names the heading of the page it goes to, made with that
		// page.
		d := pg.Links[0].Dest
		d.Elem = doc.Structure.Kids[d.Page-1].Kids[0]
		doc.Outline = append(doc.Outline, typeset.Outline{Title: fmt.Sprintf("Page %d", i+1), Dest: typeset.Dest{Page: i + 1, Elem: doc.Structure.Kids[i].Kids[0]}})
	}
	doc.Warnings = []typeset.Warning{{Code: warn.MissingGlyph, Message: "first", Page: 1}, {Code: warn.MissingGlyph, Message: "last", Page: n}, {Code: warn.MissingGlyph, Message: "none"}}
	return doc
}

// streamOut writes doc's pages through a Stream, announcing their count
// when expect is set and selecting pages when sel is not nil, and returns
// the file and the end's warnings.
func streamOut(t *testing.T, doc *typeset.Document, opts typeset.Options, expect bool, sel []int) ([]byte, []typeset.Warning, error) {
	t.Helper()
	ctx := context.Background()
	s, err := typeset.NewStream(ctx, opts, doc.Structure != nil)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	if sel != nil {
		s.Select(func(int) ([]int, error) { return sel, nil })
	}
	if expect {
		s.Expect(len(doc.Pages))
	}
	for _, pg := range doc.Pages {
		if err := s.Page(ctx, pg); err != nil {
			s.Abort()
			return nil, nil, err
		}
	}
	tail := *doc
	tail.Pages = nil
	var buf bytes.Buffer
	if err := s.Close(ctx, &tail, &buf); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), tail.Warnings, nil
}

func TestStream_WritesTheBytesWriteWrites(t *testing.T) {
	for _, v := range []typeset.Version{typeset.Version14, typeset.Version17, typeset.Version20} {
		for _, spool := range []int64{-1, 1} {
			opts := typeset.Options{Version: v, Spool: spool, TempDir: t.TempDir()}
			var want bytes.Buffer
			if err := typeset.Write(context.Background(), streamDoc(t, 5), &want, opts); err != nil {
				t.Fatalf("Write: %v", err)
			}
			for _, expect := range []bool{false, true} {
				got, _, err := streamOut(t, streamDoc(t, 5), opts, expect, nil)
				if err != nil {
					t.Fatalf("%s, expect %v: %v", v, expect, err)
				}
				if !bytes.Equal(got, want.Bytes()) {
					t.Errorf("%s, spool %d, expect %v: the stream wrote other bytes than Write", v, spool, expect)
				}
			}
		}
	}
}

func TestStream_SelectsAsSelectPagesDoes(t *testing.T) {
	for _, sel := range [][]int{{3, 2}, {2, 2}, {1}, {4, 1, 4}, {1, 2, 3, 4, 5}, {2, 3}, {5, 4, 3, 2, 1}, {1, 1, 1}} {
		for _, v := range []typeset.Version{typeset.Version17, typeset.Version20} {
			opts := typeset.Options{Version: v}
			doc := streamDoc(t, 5)
			typeset.SelectPages(doc, sel)
			var want bytes.Buffer
			if err := typeset.Write(context.Background(), doc, &want, opts); err != nil {
				t.Fatalf("Write: %v", err)
			}
			for _, expect := range []bool{false, true} {
				got, warnings, err := streamOut(t, streamDoc(t, 5), opts, expect, sel)
				if err != nil {
					t.Fatalf("%v, %s, expect %v: %v", sel, v, expect, err)
				}
				if !bytes.Equal(got, want.Bytes()) {
					t.Errorf("%v, %s, expect %v: the stream wrote other bytes than SelectPages and Write", sel, v, expect)
				}
				if !reflect.DeepEqual(warnings, doc.Warnings) {
					t.Errorf("%v, expect %v: warnings %+v, want %+v", sel, expect, warnings, doc.Warnings)
				}
			}
		}
	}
}

func TestStream_ReadsALinkElementAtTheEnd(t *testing.T) {
	doc := streamDoc(t, 3)
	want := write(t, doc)
	// The same document, each link's element set only once its page is
	// handed over.
	late := streamDoc(t, 3)
	elems := make([]*typeset.StructElem, len(late.Pages))
	for i, pg := range late.Pages {
		elems[i] = pg.Links[0].Dest.Elem
		pg.Links[0].Dest.Elem = nil
	}
	ctx := context.Background()
	s, err := typeset.NewStream(ctx, typeset.Options{}, true)
	if err != nil {
		t.Fatal(err)
	}
	for i, pg := range late.Pages {
		if err := s.Page(ctx, pg); err != nil {
			t.Fatal(err)
		}
		pg.Links[0].Dest.Elem = elems[i]
	}
	tail := *late
	tail.Pages = nil
	var got bytes.Buffer
	if err := s.Close(ctx, &tail, &got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Error("an element set after the page was handed over is not the one written")
	}
}

func TestStream_RefusesWhatValidateRefuses(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		edit func(doc *typeset.Document)
		want error
	}{
		{"parent outside the tree", func(doc *typeset.Document) {
			doc.Structure.Kids = doc.Structure.Kids[1:]
		}, typeset.ErrDetachedElement},
		{"link to no page", func(doc *typeset.Document) {
			doc.Pages[0].Links[0].Dest = &typeset.Dest{Page: 9}
		}, typeset.ErrInvalidDest},
		{"outline to no page", func(doc *typeset.Document) {
			doc.Outline[0].Dest.Page = 0
		}, typeset.ErrInvalidDest},
		{"transparent text", func(doc *typeset.Document) {
			doc.Pages[1].Content[1].(*typeset.Text).Color = typeset.Color{}
		}, typeset.ErrInvalidColor},
		{"untagged end of a tagged stream", func(doc *typeset.Document) {
			doc.Structure = nil
		}, typeset.ErrDetachedElement},
	}
	for _, tc := range cases {
		doc := streamDoc(t, 3)
		tagged := doc.Structure != nil
		tc.edit(doc)
		s, err := typeset.NewStream(ctx, typeset.Options{}, tagged)
		if err != nil {
			t.Fatal(err)
		}
		for _, pg := range doc.Pages {
			if err = s.Page(ctx, pg); err != nil {
				break
			}
		}
		if err == nil {
			tail := *doc
			tail.Pages = nil
			err = s.Close(ctx, &tail, &bytes.Buffer{})
		}
		s.Abort()
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}

	doc := streamDoc(t, 2)
	s, err := typeset.NewStream(ctx, typeset.Options{}, true)
	if err != nil {
		t.Fatal(err)
	}
	s.Expect(3)
	for _, pg := range doc.Pages {
		if err := s.Page(ctx, pg); err != nil {
			t.Fatal(err)
		}
	}
	tail := *doc
	tail.Pages = nil
	if err := s.Close(ctx, &tail, &bytes.Buffer{}); !errors.Is(err, typeset.ErrInvalidPage) {
		t.Errorf("two pages of three announced: %v, want ErrInvalidPage", err)
	}
	if err := s.Page(ctx, doc.Pages[0]); err == nil {
		t.Error("a page after Close was taken")
	}

	empty, err := typeset.NewStream(ctx, typeset.Options{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.Close(ctx, &typeset.Document{}, &bytes.Buffer{}); !errors.Is(err, typeset.ErrNoPages) {
		t.Errorf("no page: %v, want ErrNoPages", err)
	}

	failing, err := typeset.NewStream(ctx, typeset.Options{}, true)
	if err != nil {
		t.Fatal(err)
	}
	refusal := errors.New("no such page")
	failing.Select(func(int) ([]int, error) { return nil, refusal })
	failing.Expect(2)
	if err := failing.Page(ctx, streamDoc(t, 2).Pages[0]); !errors.Is(err, refusal) {
		t.Errorf("a refused selection: %v, want its error", err)
	}
	failing.Abort()
}

func TestCountGlyphs_CountsGroupsToo(t *testing.T) {
	a := run(t, sans, 10, typeset.Point{}, "abc")
	b := run(t, sans, 10, typeset.Point{}, "de")
	pg := letter(a, &typeset.Group{Content: []typeset.Fragment{b, &typeset.Path{}}}, &typeset.Image{})
	if n := typeset.CountGlyphs(pg); n != 5 {
		t.Errorf("%d glyphs, want 5", n)
	}
}
