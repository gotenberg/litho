package html

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
	"github.com/gotenberg/litho/pkg/models"
)

// threePages is a document of three pages, A, B and C, titled T1.
const threePages = `<html><head><title>T1</title><style>body { margin: 0 } p { margin: 0; break-after: page } p:last-child { break-after: auto }</style></head>` +
	`<body><p>A</p><p>B</p><p>C</p></body></html>`

// bandOptions returns the options of a 400 by 300 point page with 60
// point top and bottom margins and 30 point side margins, and bands.
func bandOptions(bands ...Band) Options {
	return Options{PageWidth: 400, PageHeight: 300, MarginTop: 60, MarginBottom: 60, MarginLeft: 30, MarginRight: 30, Bands: bands}
}

func TestBands_HeaderAndFooterOnEveryPage(t *testing.T) {
	t.Parallel()
	doc := convertDoc(t, threePages, bandOptions(
		Band{HTML: `<span class="n">H</span>`, CSS: `.n::after { content: counter(page) "/" counter(pages) }`},
		Band{Footer: true, HTML: `<p>F</p>`, CSS: `p { margin: 0 }`},
	))
	if len(doc.Pages) != 3 {
		t.Fatalf("%d pages, want 3", len(doc.Pages))
	}
	for i, pg := range doc.Pages {
		h := find(t, pg, fmt.Sprintf("H%d/3", i+1))
		// The header is centred in the 60 point top margin, its left edge
		// at the page area's.
		if h.y <= 0 || h.y >= 60 || !approx(h.x, 30, 0.01) {
			t.Errorf("page %d: header at %v,%v, want in the top margin from x 30", i+1, h.x, h.y)
		}
		f := find(t, pg, "F")
		if f.y <= 240 || f.y >= 300 {
			t.Errorf("page %d: footer baseline %v, want in the bottom margin", i+1, f.y)
		}
		if structured(pg.Content) && hasStructured(pg, "H") {
			t.Errorf("page %d: the header hangs from the structure tree", i+1)
		}
	}
}

// hasStructured reports whether a text run of pg that holds s hangs from
// the structure tree.
func hasStructured(pg *typeset.Page, s string) bool {
	var walk func([]typeset.Fragment) bool
	walk = func(frags []typeset.Fragment) bool {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Group:
				if walk(v.Content) {
					return true
				}
			case *typeset.Text:
				var b strings.Builder
				for _, g := range v.Glyphs {
					b.WriteString(g.Text)
				}
				if v.Parent != nil && strings.Contains(b.String(), s) {
					return true
				}
			}
		}
		return false
	}
	return walk(pg.Content)
}

func TestBands_PageSets(t *testing.T) {
	t.Parallel()
	// Six pages; the fourth starts the page type ch.
	src := `<style>body { margin: 0 } p { margin: 0; break-after: page } .ch { page: ch }</style>` +
		`<p>1</p><p>2</p><p>3</p><p class="ch">4</p><p class="ch">5</p><p>6</p>`
	for _, tt := range []struct {
		name  string
		pages models.PageSet
		want  string
	}{
		{"every page", models.PageSet{}, "111111"},
		{"first", models.FirstPage(), "100000"},
		{"last", models.LastPage(), "000001"},
		{"left", models.LeftPages(), "010101"},
		{"right", models.RightPages(), "101010"},
		{"range", models.PageRange(2, 3), "011000"},
		{"range to the end", models.PagesFrom(5), "000011"},
		{"named", models.NamedPages("ch"), "000110"},
		{"union", models.FirstPage().Or(models.LastPage()), "100001"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := convertDoc(t, src, bandOptions(Band{HTML: "HEAD", Pages: tt.pages}))
			var got strings.Builder
			for _, pg := range doc.Pages {
				if hasText(pg, "HEAD") {
					got.WriteByte('1')
				} else {
					got.WriteByte('0')
				}
			}
			if got.String() != tt.want {
				t.Errorf("header on pages %s, want %s", got.String(), tt.want)
			}
		})
	}
}

func TestBands_BlankPages(t *testing.T) {
	t.Parallel()
	// The second paragraph starts on a right page: page 2 is blank.
	src := `<style>body { margin: 0 } p { margin: 0 } .r { break-before: right }</style><p>1</p><p class="r">2</p>`
	doc := convertDoc(t, src, bandOptions(Band{HTML: "BLANK", Pages: models.BlankPages()}))
	if len(doc.Pages) != 3 {
		t.Fatalf("%d pages, want 3", len(doc.Pages))
	}
	for i, pg := range doc.Pages {
		if hasText(pg, "BLANK") != (i == 1) {
			t.Errorf("page %d: band shown %v", i+1, hasText(pg, "BLANK"))
		}
	}
}

func TestBands_LaterWinsAndEmptyRemoves(t *testing.T) {
	t.Parallel()
	doc := convertDoc(t, threePages, bandOptions(
		Band{HTML: "ALL"},
		Band{Pages: models.FirstPage()},
		Band{HTML: "LAST", Pages: models.LastPage()},
		Band{Footer: true, HTML: "FOOT"},
	))
	for i, want := range []string{"", "ALL", "LAST"} {
		pg := doc.Pages[i]
		for _, s := range []string{"ALL", "LAST"} {
			if hasText(pg, s) != (s == want) {
				t.Errorf("page %d: %q shown %v, want %q alone", i+1, s, hasText(pg, s), want)
			}
		}
		if !hasText(pg, "FOOT") {
			t.Errorf("page %d: a header option removed the footer", i+1)
		}
	}
}

func TestBands_ReplaceTheirMarginsBoxes(t *testing.T) {
	t.Parallel()
	src := `<style>@page { @top-left { content: "DOC-TL" } @top-right { content: "DOC-TR" } @bottom-center { content: "DOC-B" } }</style>` + threePages
	doc := convertDoc(t, src, bandOptions(
		Band{HTML: "BAND"},
		Band{Pages: models.LastPage()},
	))
	for i, pg := range doc.Pages {
		band := i < 2
		if hasText(pg, "BAND") != band || hasText(pg, "DOC-TL") == band || hasText(pg, "DOC-TR") == band {
			t.Errorf("page %d: band %v, document's top boxes %v %v", i+1, hasText(pg, "BAND"), hasText(pg, "DOC-TL"), hasText(pg, "DOC-TR"))
		}
		if !hasText(pg, "DOC-B") {
			t.Errorf("page %d: a header removed the document's bottom box", i+1)
		}
	}
}

func TestBands_Styles(t *testing.T) {
	t.Parallel()
	src := `<html lang="fr"><head><style>
		:root { --c: 9px }
		body { font-size: 20px }
		p { font-size: var(--c) }
		.k { font-size: 30px }
		@counter-style abc { system: cyclic; symbols: Q; suffix: "" }
		ol { list-style: abc }
		@page { margin: 50pt 30pt }
	</style></head><body><p>BODY</p></body></html>`
	band := `<p>PARA</p><span class="k">KLASS</span><span>INH</span><ol><li>x</li></ol>`
	for _, tt := range []struct {
		name     string
		isolated bool
		// sizes of PARA, KLASS and INH in points; marker is the list
		// marker's text.
		para, klass, inh float64
		marker           string
	}{
		// 9px from the custom property of :root, 30px from .k, 20px
		// inherited from body; the document's @counter-style draws the
		// marker.
		{"styled by the document", false, 6.75, 22.5, 15, "Q"},
		// The user agent's 16px, the decimal marker.
		{"isolated", true, 12, 12, 12, "1."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := bandOptions(Band{HTML: band, Isolated: tt.isolated})
			o.MarginTop = 10
			doc := convertDoc(t, src, o)
			pg := doc.Pages[0]
			for _, c := range []struct {
				s    string
				want float64
			}{{"PARA", tt.para}, {"KLASS", tt.klass}, {"INH", tt.inh}} {
				if got := find(t, pg, c.s).size; got != c.want {
					t.Errorf("%s at %v points, want %v", c.s, got, c.want)
				}
			}
			if !hasText(pg, tt.marker) {
				t.Errorf("no marker %q in %+v", tt.marker, pageTexts(pg))
			}
			// The document's @page margin applies either way.
			if b := find(t, pg, "BODY"); b.y < 50 {
				t.Errorf("body at %v, want below the @page margin of 50", b.y)
			}
		})
	}
}

func TestBands_CSSStylesTheBandAlone(t *testing.T) {
	t.Parallel()
	src := `<p class="x">BODY</p>`
	hostile := []Band{
		{HTML: `<p class="x">BAND</p>`, CSS: `p, .x, body, :root, * { font-size: 40px } } p { font-size: 40px }`},
		{HTML: `</litho-band></body></html><p class="x">LEAK</p>`, CSS: `@page { margin: 0 } @font-face { font-family: X; src: url(x.ttf) }`},
	}
	for i, b := range hostile {
		doc := convertDoc(t, src, bandOptions(b))
		pg := doc.Pages[0]
		if got := find(t, pg, "BODY"); got.size != 12 || got.y < 60 {
			t.Errorf("band %d: the document's text at size %v, y %v: the band's CSS reached it", i+1, got.size, got.y)
		}
		if hasStructured(pg, "LEAK") || hasStructured(pg, "BAND") {
			t.Errorf("band %d: the band's HTML joined the document", i+1)
		}
	}
}

func TestBands_StringsAndTitle(t *testing.T) {
	t.Parallel()
	src := `<html><head><title>The Title</title><style>body { margin: 0 } h2 { string-set: ch content(text); break-before: page; margin: 0; font-size: 12px }</style></head>` +
		`<body><p>intro</p><h2>Alpha</h2><h2>Beta</h2></body></html>`
	doc := convertDoc(t, src, bandOptions(Band{
		HTML: `<span class="t"></span>|<span class="c"></span>`,
		CSS:  `.t::after { content: string(document-title) } .c::after { content: string(ch) }`,
	}))
	for i, want := range []string{"The Title|", "The Title|Alpha", "The Title|Beta"} {
		if got := find(t, doc.Pages[i], "The Title|"); !strings.HasPrefix(got.text, want) || len(got.text) > len(want) && got.text[:len(want)] != want {
			t.Errorf("page %d: band reads %q, want %q", i+1, got.text, want)
		}
	}
}

func TestBands_PageCounterStart(t *testing.T) {
	t.Parallel()
	o := bandOptions(Band{HTML: `<span class="n"></span>`, CSS: `.n::before { content: "P" counter(page) }`})
	o.PageCounterStart = 9
	doc := convertDoc(t, threePages, o)
	for i, pg := range doc.Pages {
		if s := fmt.Sprintf("P%d", 10+i); !hasText(pg, s) {
			t.Errorf("page %d: no %q in %+v", i+1, s, pageTexts(pg))
		}
	}
}

func TestBands_Links(t *testing.T) {
	t.Parallel()
	src := `<style>body { margin: 0 } p { margin: 0; break-after: page }</style><p>A</p><p id="end">B</p>`
	doc := convertDoc(t, src, bandOptions(Band{HTML: `<a href="#end">to the end</a> <a href="https://example.com/">site</a>`}))
	for i, pg := range doc.Pages {
		var internal, external int
		for _, l := range pg.Links {
			switch {
			case l.Dest != nil && l.Dest.Page == 2:
				internal++
			case l.URI == "https://example.com/":
				external++
			}
		}
		if internal != 1 || external != 1 {
			t.Errorf("page %d: %d internal and %d external links, want 1 and 1 in %+v", i+1, internal, external, pg.Links)
		}
	}
}

func TestBands_RoomMarginWarns(t *testing.T) {
	t.Parallel()
	o := bandOptions(Band{HTML: `<div style="height: 100pt">TALL</div>`})
	doc := convertDoc(t, `<p>x</p>`, o)
	if !hasWarning(doc.Warnings, warn.PageSetupUnsupported, "the header is 100 points tall and its margin leaves it 60 points") {
		t.Errorf("no overflow warning in %+v", doc.Warnings)
	}
}

func TestBands_RoomContent(t *testing.T) {
	t.Parallel()
	src := `<style>body { margin: 0 } p { margin: 0 }</style><p>BODY</p>` + strings.Repeat(`<p>line</p>`, 40)
	band := `<div style="height: 100pt">TALL</div>`
	doc := convertDoc(t, src, bandOptions(Band{HTML: band, Room: RoomContent}))
	if hasWarning(doc.Warnings, warn.PageSetupUnsupported, "overflows the margin") {
		t.Errorf("RoomContent warned of an overflow: %+v", doc.Warnings)
	}
	if b := find(t, doc.Pages[0], "BODY"); b.y < 100 {
		t.Errorf("body at %v, want below the 100 point header", b.y)
	}
	// The twin names the margin the band takes: the same pages.
	twin := convertDoc(t, src, Options{PageWidth: 400, PageHeight: 300, MarginTop: 100, MarginBottom: 60, MarginLeft: 30, MarginRight: 30,
		Bands: []Band{{HTML: band}}})
	if len(doc.Pages) != len(twin.Pages) {
		t.Fatalf("%d pages, the twin %d", len(doc.Pages), len(twin.Pages))
	}
	var a, b bytes.Buffer
	if err := typeset.Write(context.Background(), doc, &a, typeset.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := typeset.Write(context.Background(), twin, &b, typeset.Options{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("RoomContent and the twin of its margin write different files")
	}
}

func TestBands_InvalidOptions(t *testing.T) {
	t.Parallel()
	for _, b := range []Band{
		{Room: 7},
		{Pages: models.PageRange(0, 2)},
		{Pages: models.PageRange(3, 2)},
		{Pages: models.NamedPages("")},
		{HTML: strings.Repeat("x", maxBandBytes+1)},
	} {
		_, err := Convert(context.Background(), strings.NewReader("x"), testFonts(t), Options{Bands: []Band{b}})
		if !errors.Is(err, typeset.ErrInvalidOptions) {
			t.Errorf("%+v: err %v, want ErrInvalidOptions", b.Pages, err)
		}
	}
	for _, start := range []int{MinPageCounterStart - 1, MaxPageCounterStart + 1} {
		_, err := Convert(context.Background(), strings.NewReader("x"), testFonts(t), Options{PageCounterStart: start})
		if !errors.Is(err, typeset.ErrInvalidOptions) {
			t.Errorf("start %d: err %v, want ErrInvalidOptions", start, err)
		}
	}
}

func TestBands_SharePictures(t *testing.T) {
	t.Parallel()
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes(t, 2, 2))
	doc := convertDoc(t, threePages, bandOptions(Band{HTML: `<img src="` + data + `" style="height: 10px">`}))
	sources := map[*typeset.ImageSource]bool{}
	var walk func([]typeset.Fragment)
	walk = func(frags []typeset.Fragment) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Image:
				sources[v.Source] = true
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	for _, pg := range doc.Pages {
		walk(pg.Content)
	}
	if len(sources) != 1 {
		t.Errorf("%d image sources for the band's picture on 3 pages, want 1", len(sources))
	}
}

func TestBands_FontFaces(t *testing.T) {
	t.Parallel()
	data := base64.StdEncoding.EncodeToString(readTestFile(t, ahemTTF))
	src := `<style>@font-face { font-family: Doc; src: url(data:font/ttf;base64,` + data + `) }</style><p>BODY</p>`
	o := bandOptions(Band{
		HTML:     `<p class="doc">DOCX</p><p class="own">OWNX</p>`,
		CSS:      `@font-face { font-family: Own; src: url(data:font/ttf;base64,` + data + `) } .doc { font-family: Doc } .own { font-family: Own }`,
		Isolated: true,
	})
	o.MarginTop = 100
	doc := convertDoc(t, src, o)
	// The document's @font-face face draws an isolated band's text; the
	// band's own @font-face rule loads nothing.
	if k := faceOfText(t, doc, "DOCX").Face.Key; !strings.HasPrefix(k, "font-face:") {
		t.Errorf("the document's face is not the band's: drawn with %s", k)
	}
	if k := faceOfText(t, doc, "OWNX").Face.Key; strings.HasPrefix(k, "font-face:") {
		t.Errorf("the band's own @font-face loaded: drawn with %s", k)
	}
}

func TestBands_RoomContentOnASinglePage(t *testing.T) {
	t.Parallel()
	src := `<style>body { margin: 0 } p { margin: 0 }</style><p>FIRST</p><p>LAST</p>`
	band := `<div style="height: 100pt">TALL</div>`
	o := Options{PageWidth: 400, PageHeight: 300, MarginTop: 20, MarginBottom: 20, SinglePage: true}
	plain := convertDoc(t, src, o)
	o.Bands = []Band{{HTML: band, Room: RoomContent}}
	doc := convertDoc(t, src, o)
	if len(doc.Pages) != 1 {
		t.Fatalf("%d pages, want 1", len(doc.Pages))
	}
	// The page grows by the 80 points the top margin grows.
	if got, want := doc.Pages[0].Height, plain.Pages[0].Height+80; !approx(got, want, 0.01) {
		t.Errorf("page %v points tall, want %v", got, want)
	}
	if l := find(t, doc.Pages[0], "LAST"); l.y > doc.Pages[0].Height-20 || find(t, doc.Pages[0], "FIRST").y < 100 {
		t.Errorf("content at %v, want between the 100 point header and the bottom margin", l.y)
	}
}
