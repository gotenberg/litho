package pdf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// bandPages is an HTML document of three pages, titled Report.
const bandPages = `<html><head><title>Report</title><style>p { break-after: page } p:last-child { break-after: auto }</style></head>` +
	`<body><p>one</p><p>two</p><p>three</p></body></html>`

// bandTexts returns the text of every page of data, artifacts included,
// its white space collapsed.
func bandTexts(t *testing.T, data []byte) []string {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	pages, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	var out []string
	for n := 1; n <= pages; n++ {
		text, err := r.ExtractText(ctx, n, WithHiddenText())
		if err != nil {
			t.Fatalf("ExtractText(%d): %v", n, err)
		}
		out = append(out, strings.Join(strings.Fields(text), " "))
	}
	return out
}

func TestConvert_HeaderAndFooterBands(t *testing.T) {
	t.Parallel()
	data, rep := convertHTMLTest(t, convertFontDir(t), bandPages,
		WithMargins(72, 36, 72, 36),
		WithHeader(PageBand{HTML: `<span class="t"></span>`, CSS: `.t::after { content: string(litho-title) " " counter(page) "/" counter(pages) }`}),
		WithHeader(PageBand{Pages: FirstPage()}),
		WithFooter(PageBand{HTML: "Last", Pages: LastPage()}),
		WithFooter(PageBand{HTML: "Left", Pages: LeftPages(), Isolated: true}),
		WithFirstPageNumber(7),
	)
	if rep.Pages != 3 || len(rep.Warnings) != 0 {
		t.Errorf("report %+v, want 3 pages and no warning", rep)
	}
	got := bandTexts(t, data)
	for i, want := range [][]string{{"one"}, {"Report 8/3", "two", "Left"}, {"Report 9/3", "three", "Last"}} {
		for _, w := range want {
			if !strings.Contains(got[i], w) {
				t.Errorf("page %d reads %q, want %q in it", i+1, got[i], w)
			}
		}
	}
	if strings.Contains(got[0], "Report") {
		t.Errorf("page 1 reads %q: the empty header did not remove the band", got[0])
	}
}

func TestConvert_BandsInMarkdown(t *testing.T) {
	t.Parallel()
	src := "# Field notes\n\nIntro.\n\n## Tides\n\nLow water at noon.\n"
	var out bytes.Buffer
	rep, err := FromMarkdown(strings.NewReader(src), WithConvertFontDirs(convertFontDir(t)),
		WithUserStylesheet(`h2 { string-set: section content(text); break-before: page }`),
		WithFooter(PageBand{
			HTML: `<span class="t"></span><span class="s"></span>`,
			CSS:  `.t::after { content: string(litho-title) } .s::before { content: " | " string(section) }`,
		}),
	).WriteTo(context.Background(), &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if rep.Pages != 2 {
		t.Fatalf("%d pages, want 2", rep.Pages)
	}
	got := bandTexts(t, out.Bytes())
	if !strings.Contains(got[0], "Field notes |") || strings.Contains(got[0], "Tides |") {
		t.Errorf("page 1 reads %q, want the title and no section", got[0])
	}
	if !strings.Contains(got[1], "Field notes | Tides") {
		t.Errorf("page 2 reads %q, want the title and the section", got[1])
	}
}

func TestConvert_BandsRefuseOptionsOutOfRange(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	many := make([]HTMLOption, 0, 65)
	for range 65 {
		many = append(many, WithHeader(PageBand{}))
	}
	for name, opts := range map[string][]HTMLOption{
		"page range from 0":    {WithHeader(PageBand{Pages: PageRange(0, 2)})},
		"page range backwards": {WithFooter(PageBand{Pages: FirstPage().Or(PageRange(4, 3))})},
		"empty page name":      {WithHeader(PageBand{Pages: NamedPages("")})},
		"unknown room":         {WithHeader(PageBand{Room: Room(9)})},
		"too many bands":       many,
		"negative first page":  {WithFirstPageNumber(-1)},
		"first page past 2^30": {WithFirstPageNumber(1<<30 + 1)},
		"band past its size":   {WithHeader(PageBand{HTML: strings.Repeat("x", 1<<20+1)})},
	} {
		_, err := FromHTML(strings.NewReader("<p>x</p>"), append([]HTMLOption{WithConvertFontDirs(dir)}, opts...)...).WriteTo(context.Background(), &bytes.Buffer{})
		if !errors.Is(err, ErrInvalidConvertOptions) {
			t.Errorf("%s: err %v, want ErrInvalidConvertOptions", name, err)
		}
	}
	for _, n := range []int{0, 1 << 30} {
		if _, err := FromHTML(strings.NewReader("<p>x</p>"), WithConvertFontDirs(dir), WithFirstPageNumber(n)).WriteTo(context.Background(), &bytes.Buffer{}); err != nil {
			t.Errorf("WithFirstPageNumber(%d): %v", n, err)
		}
	}
}

func TestPageSet_OrWithEveryPageSelectsEveryPage(t *testing.T) {
	t.Parallel()
	data, _ := convertHTMLTest(t, convertFontDir(t), bandPages,
		WithMargins(72, 36, 72, 36),
		WithHeader(PageBand{HTML: "Band", Pages: PageSet{}.Or(FirstPage())}),
		WithFooter(PageBand{HTML: "Foot", Pages: PageRange(2, 0).Or(NamedPages("none"))}),
	)
	for i, got := range bandTexts(t, data) {
		if !strings.Contains(got, "Band") {
			t.Errorf("page %d reads %q, want the band of every page", i+1, got)
		}
		if strings.Contains(got, "Foot") != (i > 0) {
			t.Errorf("page %d reads %q: the footer of pages 2 to the last", i+1, got)
		}
	}
}

func TestConvert_BandRoomContentWritesTheTwinOfItsMargin(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	band := `<div style="height: 120pt">Letterhead</div>`
	src := strings.Repeat("<p>line</p>", 60)
	data, rep := convertHTMLTest(t, dir, src, WithMargins(36, 36, 36, 36), WithHeader(PageBand{HTML: band, Room: RoomContent}))
	twin, _ := convertHTMLTest(t, dir, src, WithMargins(120, 36, 36, 36), WithHeader(PageBand{HTML: band}))
	if !bytes.Equal(data, twin) {
		t.Error("RoomContent writes other bytes than the margin it measured, set by hand")
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("warnings %+v, want none", rep.Warnings)
	}
	_, rep = convertHTMLTest(t, dir, src, WithMargins(36, 36, 36, 36), WithHeader(PageBand{HTML: band}))
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0].Message, "the header is 120 points tall") {
		t.Errorf("warnings %+v, want the header's overflow", rep.Warnings)
	}
}
