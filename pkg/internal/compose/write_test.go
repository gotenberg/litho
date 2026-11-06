package compose_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/typography/embed"
	"github.com/gotenberg/litho/pkg/models"
)

func TestWriteTextExtracts(t *testing.T) {
	t.Parallel()
	doc := &compose.Document{Pages: []*compose.Page{
		{Width: 300, Height: 200, Content: []compose.Fragment{
			run(t, sans, 12, compose.Point{X: 20, Y: 40}, "Hello, World!"),
			run(t, sans, 12, compose.Point{X: 20, Y: 60}, "Second line (with parens) \\ ok"),
		}},
	}}
	r := open(t, write(t, doc))
	ctx := context.Background()
	if n, err := r.PageCount(ctx); err != nil || n != 1 {
		t.Fatalf("PageCount = %d, %v; want 1", n, err)
	}
	page, err := r.Page(ctx, 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Width != 300 || page.Height != 200 {
		t.Errorf("page size = %v by %v, want 300 by 200", page.Width, page.Height)
	}
	text, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	for _, want := range []string{"Hello, World!", "Second line (with parens) \\ ok"} {
		if !strings.Contains(text, want) {
			t.Errorf("ExtractText = %q, want it to hold %q", text, want)
		}
	}
	if strings.Index(text, "Hello") > strings.Index(text, "Second") {
		t.Errorf("ExtractText = %q, lines out of order", text)
	}
}

func TestWriteTwoFacesOnAPage(t *testing.T) {
	t.Parallel()
	doc := &compose.Document{Pages: []*compose.Page{letter(
		run(t, sans, 12, compose.Point{X: 20, Y: 40}, "Sans text"),
		run(t, serif, 14, compose.Point{X: 20, Y: 80}, "Serif text"),
	)}}
	out := write(t, doc)
	if n := bytes.Count(out, []byte("/FontFile2")); n != 2 {
		t.Errorf("%d font programs, want 2", n)
	}
	text, err := open(t, out).ExtractText(context.Background(), 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "Sans text") || !strings.Contains(text, "Serif text") {
		t.Errorf("ExtractText = %q", text)
	}
}

func TestWriteSharesAFaceAcrossPages(t *testing.T) {
	t.Parallel()
	doc := &compose.Document{Pages: []*compose.Page{
		letter(run(t, sans, 12, compose.Point{X: 20, Y: 40}, "Page one")),
		letter(run(t, sans, 12, compose.Point{X: 20, Y: 40}, "Page two")),
	}}
	out := write(t, doc)
	if n := bytes.Count(out, []byte("/FontFile2")); n != 1 {
		t.Errorf("%d font programs, want 1", n)
	}
	r := open(t, out)
	for i, want := range []string{"Page one", "Page two"} {
		text, err := r.ExtractText(context.Background(), i+1)
		if err != nil || !strings.Contains(text, want) {
			t.Errorf("page %d: ExtractText = %q, %v; want %q", i+1, text, err, want)
		}
	}
}

// TestWriteKerningKeepsPositions spreads and tightens glyphs away from
// their natural advances, and moves the last ones to a second baseline,
// then checks that every extracted element starts where the model put its
// first glyph and that every glyph's rectangle inks its own box.
func TestWriteKerningKeepsPositions(t *testing.T) {
	t.Parallel()
	const size = 20
	txt := run(t, sans, size, compose.Point{X: 50, Y: 100}, "AVATARkerned")
	f, err := embed.Load(sans.Program)
	if err != nil {
		t.Fatalf("embed.Load: %v", err)
	}
	for i := range txt.Glyphs {
		txt.Glyphs[i].X += float64(5*i) + 0.37
		if i%3 == 2 {
			txt.Glyphs[i].X -= 2.25
		}
		if i >= 7 {
			txt.Glyphs[i].Y = 30
		}
	}
	page := &compose.Page{Width: 600, Height: 300, Content: []compose.Fragment{txt}}
	r := open(t, write(t, &compose.Document{Pages: []*compose.Page{page}}))
	elems, err := r.ExtractTextElements(context.Background(), 1)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}
	next := 0
	for _, e := range elems {
		if next >= len(txt.Glyphs) {
			t.Fatalf("more text extracted than drawn: %+v", elems)
		}
		g := txt.Glyphs[next]
		wantX, wantY := txt.Origin.X+g.X, page.Height-(txt.Origin.Y+g.Y)
		if math.Abs(e.X-wantX) > 0.5 || math.Abs(e.Y-wantY) > 0.5 {
			t.Errorf("element %q at (%.3f, %.3f), want (%.3f, %.3f)", e.Text, e.X, e.Y, wantX, wantY)
		}
		next += len([]rune(e.Text))
	}
	if next != len(txt.Glyphs) {
		t.Errorf("%d glyphs extracted, want %d", next, len(txt.Glyphs))
	}

	img := render(t, r, 1)
	for i, g := range txt.Glyphs {
		w, _ := f.Width(g.GID)
		x0 := txt.Origin.X + g.X
		x1 := x0 + float64(w)*size/1000
		base := txt.Origin.Y + g.Y
		// Rectangles reach 500 units up: 10 points at this size.
		inner := inkIn(img, int(math.Ceil(x0))+1, int(base-10)+1, int(math.Floor(x1))-1, int(base)-1)
		area := (int(math.Floor(x1)) - 1 - int(math.Ceil(x0)) - 1) * 8
		if inner < area*9/10 {
			t.Errorf("glyph %d %q: %d of %d pixels inked inside its box", i, g.Text, inner, area)
		}
		if i+1 < len(txt.Glyphs) && txt.Glyphs[i+1].Y == g.Y {
			gap := int(math.Round((x1 + txt.Origin.X + txt.Glyphs[i+1].X) / 2))
			if n := inkIn(img, gap, int(base-10)+1, gap+1, int(base)-1); n != 0 {
				t.Errorf("glyph %d %q: %d pixels inked in the gap after it", i, g.Text, n)
			}
		}
	}
}

// TestWriteTextStandsUpright renders a glyph and checks its ink sits above
// the baseline, where the model puts it, and not below, where a mirrored
// glyph would land.
func TestWriteTextStandsUpright(t *testing.T) {
	t.Parallel()
	// fontbuild glyphs are rectangles from the baseline up to 500 units:
	// at 40 points, 20 points tall.
	txt := run(t, sans, 40, compose.Point{X: 50, Y: 100}, "H")
	page := &compose.Page{Width: 200, Height: 200, Content: []compose.Fragment{txt}}
	r := open(t, write(t, &compose.Document{Pages: []*compose.Page{page}}))
	img := render(t, r, 1)
	if n := inkIn(img, 52, 82, 60, 98); n < 8*16*9/10 {
		t.Errorf("%d dark pixels inside the glyph box, want it filled", n)
	}
	if n := inkIn(img, 40, 102, 100, 130); n != 0 {
		t.Errorf("%d dark pixels below the baseline, where a mirrored glyph lands", n)
	}
	if n := inkIn(img, 40, 40, 100, 78); n != 0 {
		t.Errorf("%d dark pixels above the glyph", n)
	}
	text, err := r.ExtractText(context.Background(), 1)
	if err != nil || !strings.Contains(text, "H") {
		t.Errorf("ExtractText = %q, %v", text, err)
	}
}

func TestWriteImageWithAlpha(t *testing.T) {
	t.Parallel()
	// Top half red, bottom half blue, right column transparent.
	pic := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			c := color.NRGBA{R: 255, A: 255}
			if y >= 2 {
				c = color.NRGBA{B: 255, A: 255}
			}
			if x == 3 {
				c.A = 0
			}
			pic.SetNRGBA(x, y, c)
		}
	}
	src := &compose.ImageSource{Image: pic}
	page := &compose.Page{Width: 200, Height: 200, Content: []compose.Fragment{
		&compose.Image{Rect: compose.Rect{X: 40, Y: 60, Width: 80, Height: 80}, Source: src},
		&compose.Image{Rect: compose.Rect{X: 140, Y: 10, Width: 20, Height: 20}, Source: src},
	}}
	out := write(t, &compose.Document{Pages: []*compose.Page{page}})
	if n := bytes.Count(out, []byte("/SMask")); n != 1 {
		t.Errorf("%d soft masks, want 1 for a source placed twice", n)
	}
	img := render(t, open(t, out), 1)
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	for _, c := range []struct {
		x, y int
		want color.NRGBA
		what string
	}{
		{50, 70, red, "top-left"},
		{50, 130, blue, "bottom-left"},
		{110, 70, white, "transparent column"},
		{20, 70, white, "outside"},
		{145, 15, red, "second placement"},
	} {
		if !near(img, c.x, c.y, c.want, 40) {
			t.Errorf("%s pixel (%d, %d) = %v, want %v", c.what, c.x, c.y, img.At(c.x, c.y), c.want)
		}
	}
}

func TestWriteJPEGPassesThrough(t *testing.T) {
	t.Parallel()
	pic := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := range 8 {
		for x := range 16 {
			pic.Set(x, y, color.RGBA{R: 0, G: 200, B: 0, A: 255})
		}
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, pic, nil); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	page := &compose.Page{Width: 100, Height: 100, Content: []compose.Fragment{
		&compose.Image{Rect: compose.Rect{X: 10, Y: 10, Width: 32, Height: 16}, Source: &compose.ImageSource{JPEG: jpg.Bytes()}},
	}}
	out := write(t, &compose.Document{Pages: []*compose.Page{page}})
	start := bytes.Index(out, []byte("/DCTDecode"))
	if start < 0 {
		t.Fatal("no DCTDecode stream")
	}
	body := out[bytes.Index(out[start:], []byte("stream\r\n"))+start+len("stream\r\n"):]
	if !bytes.HasPrefix(body, jpg.Bytes()) {
		t.Error("the DCT stream does not hold the JPEG bytes unchanged")
	}
	if !bytes.Contains(out, []byte("/Width 16")) || !bytes.Contains(out, []byte("/DeviceRGB")) {
		t.Error("the image dictionary does not carry the JPEG's size and colour space")
	}
	img := render(t, open(t, out), 1)
	if !near(img, 25, 18, color.NRGBA{G: 200, A: 255}, 40) {
		t.Errorf("pixel inside the JPEG = %v, want green", img.At(25, 18))
	}
}

func TestWritePathsFillAndStroke(t *testing.T) {
	t.Parallel()
	ring := append(rect(100, 10, 60, 60), rect(115, 25, 30, 30)...)
	page := &compose.Page{Width: 200, Height: 200, Content: []compose.Fragment{
		&compose.Path{Ops: rect(10, 10, 50, 50), Fill: &compose.Fill{Color: compose.Color{B: 1, A: 1}}},
		&compose.Path{Ops: ring, Fill: &compose.Fill{Color: compose.Color{R: 1, A: 1}, Rule: compose.EvenOdd}},
		&compose.Path{
			Ops: []compose.PathOp{
				{Kind: compose.MoveTo, P: [3]compose.Point{{X: 10, Y: 150}}},
				{Kind: compose.LineTo, P: [3]compose.Point{{X: 190, Y: 150}}},
			},
			Stroke: &compose.Stroke{Color: compose.Color{G: 1, A: 1}, Width: 6, Dash: []float64{20, 10}},
		},
		&compose.Path{
			Ops:    rect(10, 100, 30, 30),
			Fill:   &compose.Fill{Color: compose.Color{R: 1, G: 1, A: 1}},
			Stroke: &compose.Stroke{Color: compose.Black, Width: 4, Join: compose.RoundJoin},
		},
		&compose.Path{Ops: rect(150, 150, 10, 10)}, // paints nothing
	}}
	img := render(t, open(t, write(t, &compose.Document{Pages: []*compose.Page{page}})), 1)
	for _, c := range []struct {
		x, y int
		want color.NRGBA
		what string
	}{
		{30, 30, color.NRGBA{B: 255}, "filled square"},
		{105, 40, color.NRGBA{R: 255}, "even-odd ring"},
		{130, 40, color.NRGBA{R: 255, G: 255, B: 255}, "even-odd hole"},
		{15, 150, color.NRGBA{G: 255}, "dash on"},
		{35, 150, color.NRGBA{R: 255, G: 255, B: 255}, "dash off"},
		{25, 115, color.NRGBA{R: 255, G: 255}, "fill inside stroke"},
		{10, 115, color.NRGBA{}, "stroke"},
	} {
		if !near(img, c.x, c.y, c.want, 60) {
			t.Errorf("%s pixel (%d, %d) = %v, want %v", c.what, c.x, c.y, img.At(c.x, c.y), c.want)
		}
	}
}

func TestWriteGroupTransformsClipsAndFades(t *testing.T) {
	t.Parallel()
	blue := &compose.Fill{Color: compose.Color{B: 1, A: 1}}
	page := &compose.Page{Width: 300, Height: 200, Content: []compose.Fragment{
		&compose.Group{
			Transform: compose.Matrix{A: 1, D: 1, E: 200, F: 20},
			Content:   []compose.Fragment{&compose.Path{Ops: rect(0, 0, 50, 50), Fill: blue}},
		},
		&compose.Group{
			Clip:    &compose.Path{Ops: rect(10, 10, 20, 20)},
			Content: []compose.Fragment{&compose.Path{Ops: rect(0, 0, 60, 60), Fill: blue}},
		},
		&compose.Group{
			Opacity: 0.5,
			Content: []compose.Fragment{
				&compose.Group{Content: []compose.Fragment{
					&compose.Path{Ops: rect(100, 100, 40, 40), Fill: &compose.Fill{Color: compose.Black}},
				}},
				&compose.Path{Ops: rect(100, 150, 40, 40), Fill: &compose.Fill{Color: compose.Color{A: 0.5}}},
			},
		},
		&compose.Path{Ops: rect(160, 100, 40, 40), Fill: &compose.Fill{Color: compose.Black}},
	}}
	img := render(t, open(t, write(t, &compose.Document{Pages: []*compose.Page{page}})), 1)
	for _, c := range []struct {
		x, y int
		want color.NRGBA
		what string
	}{
		{225, 45, color.NRGBA{B: 255}, "translated square"},
		{25, 45, color.NRGBA{R: 255, G: 255, B: 255}, "translation origin"},
		{20, 20, color.NRGBA{B: 255}, "inside the clip"},
		{40, 40, color.NRGBA{R: 255, G: 255, B: 255}, "outside the clip"},
		{120, 120, color.NRGBA{R: 128, G: 128, B: 128}, "half opacity"},
		{120, 170, color.NRGBA{R: 191, G: 191, B: 191}, "quarter opacity"},
		{180, 120, color.NRGBA{}, "opaque after the group"},
	} {
		if !near(img, c.x, c.y, c.want, 20) {
			t.Errorf("%s pixel (%d, %d) = %v, want %v", c.what, c.x, c.y, img.At(c.x, c.y), c.want)
		}
	}
}

func TestWriteLinks(t *testing.T) {
	t.Parallel()
	doc := &compose.Document{Pages: []*compose.Page{
		{Width: 400, Height: 500, Links: []compose.Link{
			{Rect: compose.Rect{X: 10, Y: 20, Width: 100, Height: 30}, URI: "https://example.com/a b/é"},
			{Rect: compose.Rect{X: 10, Y: 100, Width: 50, Height: 10}, Dest: &compose.Dest{Page: 2, X: 0, Y: 100}},
		}},
		{Width: 400, Height: 600},
	}}
	out := write(t, doc)
	annots, err := open(t, out).ExtractAnnotations(context.Background(), 1)
	if err != nil {
		t.Fatalf("ExtractAnnotations: %v", err)
	}
	if len(annots) != 2 {
		t.Fatalf("%d annotations, want 2", len(annots))
	}
	if a := annots[0]; a.Subtype != "Link" || a.URI != "https://example.com/a%20b/%C3%A9" || a.Rect != [4]float64{10, 450, 110, 480} {
		t.Errorf("URI link = %+v", a)
	}
	if a := annots[1]; a.DestPage != 2 || a.Rect != [4]float64{10, 390, 60, 400} {
		t.Errorf("Dest link = %+v", a)
	}
	// The destination's y is flipped against page 2's height.
	if !bytes.Contains(out, []byte("/XYZ 0 500 null")) {
		t.Error("destination not written as [page /XYZ 0 500 null]")
	}
}

func TestWriteOutline(t *testing.T) {
	t.Parallel()
	doc := &compose.Document{
		Pages: []*compose.Page{letter(), letter(), letter()},
		Outline: []compose.Outline{
			{Title: "Chapter 1", Dest: compose.Dest{Page: 1}, Kids: []compose.Outline{
				{Title: "Section 1.1", Dest: compose.Dest{Page: 2}},
				{Title: "Résumé ✓", Dest: compose.Dest{Page: 2, Y: 300}},
			}},
			{Title: "Chapter 2", Dest: compose.Dest{Page: 3}},
		},
	}
	got, err := open(t, write(t, doc)).Bookmarks(context.Background())
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}
	want := []models.Bookmark{
		{Title: "Chapter 1", PageNumber: 1, Children: []models.Bookmark{
			{Title: "Section 1.1", PageNumber: 2},
			{Title: "Résumé ✓", PageNumber: 2},
		}},
		{Title: "Chapter 2", PageNumber: 3},
	}
	if !equalBookmarks(got, want) {
		t.Errorf("Bookmarks = %+v, want %+v", got, want)
	}
}

func equalBookmarks(a, b []models.Bookmark) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Title != b[i].Title || a[i].PageNumber != b[i].PageNumber || !equalBookmarks(a[i].Children, b[i].Children) {
			return false
		}
	}
	return true
}

// taggedDoc is a two-page structured document: one P element spans both
// pages, a Figure holds an image, a Link element a link, and a decoration
// rule is an artifact.
func taggedDoc(t *testing.T) *compose.Document {
	t.Helper()
	p := &compose.StructElem{Role: "P"}
	h := &compose.StructElem{Role: "H1"}
	fig := &compose.StructElem{Role: "Figure", Alt: "A red square"}
	link := &compose.StructElem{Role: "Link", Alt: "Example site"}
	root := &compose.StructElem{Role: "Document", Lang: "en-US", Kids: []*compose.StructElem{h, p, fig, link}}
	pic := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for i := range pic.Pix {
		pic.Pix[i] = 0xFF
	}
	heading := run(t, sans, 18, compose.Point{X: 40, Y: 60}, "Title")
	heading.Parent = h
	l1 := run(t, sans, 12, compose.Point{X: 40, Y: 100}, "A paragraph that")
	l1.Parent = p
	l2 := run(t, sans, 12, compose.Point{X: 40, Y: 60}, "goes on here")
	l2.Parent = p
	label := run(t, sans, 12, compose.Point{X: 40, Y: 200}, "Visit")
	label.Parent = link
	return &compose.Document{
		Metadata:  compose.Metadata{Title: "Tagged", Lang: "en-US"},
		Structure: root,
		Pages: []*compose.Page{
			letter(heading,
				&compose.Group{Clip: &compose.Path{Ops: rect(0, 80, 612, 40)}, Opacity: 0.9, Content: []compose.Fragment{
					l1,
					&compose.Path{Ops: rect(40, 110, 200, 1), Fill: &compose.Fill{Color: compose.Black}},
				}}),
			{Width: 612, Height: 792, Content: []compose.Fragment{
				l2,
				&compose.Image{Rect: compose.Rect{X: 40, Y: 100, Width: 50, Height: 50}, Source: &compose.ImageSource{Image: pic}, Parent: fig},
				label,
			}, Links: []compose.Link{{Rect: compose.Rect{X: 40, Y: 188, Width: 40, Height: 14}, URI: "https://example.com", Parent: link}}},
		},
	}
}

func TestWriteStructureTree(t *testing.T) {
	t.Parallel()
	out := write(t, taggedDoc(t))
	r := open(t, out)
	ctx := context.Background()
	if tagged, err := r.IsTagged(ctx); err != nil || !tagged {
		t.Fatalf("IsTagged = %v, %v", tagged, err)
	}
	for page, want := range map[int]string{1: "A paragraph that", 2: "goes on here"} {
		elems, err := r.ExtractTextElements(ctx, page)
		if err != nil {
			t.Fatalf("ExtractTextElements(%d): %v", page, err)
		}
		for _, e := range elems {
			if e.MCID < 0 {
				t.Errorf("page %d: %q outside marked content", page, e.Text)
			}
		}
		text, err := r.ExtractText(ctx, page)
		if err != nil || !strings.Contains(text, want) {
			t.Errorf("page %d: ExtractText = %q, %v", page, text, err)
		}
	}
	// P owns MCID 1 of page 1 and, through a marked-content reference,
	// MCID 0 of page 2.
	for _, want := range []string{"/MCR", "/OBJR", "/Artifact BMC", "/StructParent 2", "/Tabs /S", "/Marked true"} {
		if !bytes.Contains(out, []byte(want)) && !bytes.Contains(inflated(t, out), []byte(want)) {
			t.Errorf("output lacks %s", want)
		}
	}
	violations, err := r.ValidatePDFUAReport(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport: %v", err)
	}
	for _, v := range violations {
		t.Logf("PDF/UA-1 before remediation: %v", v)
	}
}

func TestWriteIsDeterministic(t *testing.T) {
	t.Parallel()
	a := write(t, taggedDoc(t))
	b := write(t, taggedDoc(t))
	if !bytes.Equal(a, b) {
		t.Error("two writes of one document differ")
	}
	other := taggedDoc(t)
	other.Metadata.Title = "Other"
	if c := write(t, other); bytes.Equal(fileID(a), fileID(c)) {
		t.Error("documents with different Info share an /ID")
	}
}

// fileID returns the bytes of the trailer's /ID entry.
func fileID(out []byte) []byte {
	i := bytes.LastIndex(out, []byte("/ID"))
	if i < 0 {
		return nil
	}
	j := bytes.IndexByte(out[i:], ']')
	return out[i : i+j]
}

func TestWriteCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var buf bytes.Buffer
	err := compose.Write(ctx, &compose.Document{Pages: []*compose.Page{letter()}}, &buf, compose.Options{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Write = %v, want it to wrap context.Canceled", err)
	}
	if buf.Len() != 0 {
		t.Errorf("%d bytes written on a canceled context", buf.Len())
	}
}

func TestWriteRefusesABadFace(t *testing.T) {
	t.Parallel()
	bad := &compose.Face{Program: []byte("not a font"), Key: "broken-face"}
	doc := &compose.Document{Pages: []*compose.Page{letter(&compose.Text{Face: bad, Size: 12, Color: compose.Black, Glyphs: []compose.Glyph{{GID: 1}}})}}
	err := compose.Write(context.Background(), doc, &bytes.Buffer{}, compose.Options{})
	if !errors.Is(err, compose.ErrInvalidFont) || !strings.Contains(err.Error(), "broken-face") {
		t.Errorf("Write = %v, want ErrInvalidFont naming the face", err)
	}
	missing := &compose.Text{Face: sans, Size: 12, Color: compose.Black, Glyphs: []compose.Glyph{{GID: 60000}}}
	err = compose.Write(context.Background(), &compose.Document{Pages: []*compose.Page{letter(missing)}}, &bytes.Buffer{}, compose.Options{})
	if !errors.Is(err, compose.ErrInvalidFont) {
		t.Errorf("Write with a missing glyph = %v, want ErrInvalidFont", err)
	}
}

func TestWriteInfo(t *testing.T) {
	t.Parallel()
	doc := &compose.Document{
		Pages:    []*compose.Page{letter()},
		Metadata: compose.Metadata{Title: "Le titre é", Author: "A. Author", Subject: "S", Keywords: "k1, k2", Creator: "conv", Lang: "fr-FR"},
	}
	var buf bytes.Buffer
	if err := compose.Write(context.Background(), doc, &buf, compose.Options{Producer: "test-producer"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	meta, err := open(t, buf.Bytes()).Metadata(context.Background())
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	for k, want := range map[string]string{"Title": "Le titre é", "Author": "A. Author", "Producer": "test-producer", "Creator": "conv", "Keywords": "k1, k2"} {
		if meta[k] != want {
			t.Errorf("Metadata[%s] = %q, want %q (all: %v)", k, meta[k], want, meta)
		}
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-1.7")) {
		t.Error("header is not PDF 1.7")
	}
	if !bytes.Contains(buf.Bytes(), []byte("/Lang (fr-FR)")) {
		t.Error("catalog lacks /Lang")
	}
	if bytes.Contains(buf.Bytes(), []byte("/CreationDate")) || bytes.Contains(buf.Bytes(), []byte("/ModDate")) {
		t.Error("Info carries a date")
	}
}
