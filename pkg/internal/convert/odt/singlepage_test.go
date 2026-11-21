package odt

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/models"
)

// singlePages converts d on one page and on pages.
func singlePages(t *testing.T, d doc, opts office.Options) (one, paged *typeset.Document) {
	t.Helper()
	data := d.pack(t)
	paged = convertDoc(t, data, opts)
	opts.SinglePage = true
	return convertDoc(t, data, opts), paged
}

// lineOf returns the baseline of the first line of pg holding s, -1 when
// none does.
func lineOf(pg *typeset.Page, s string) float64 {
	for _, l := range lines(pg) {
		if strings.Contains(l.text, s) {
			return l.y
		}
	}
	return -1
}

// TestSinglePage_HeadersBreaksAndSizes holds the page of a document of
// two master pages: the first one's width and header, the last one's
// footer, numbered as the first page in the format of each part's master
// page and counting one, no break, the second master page's other width
// reported.
func TestSinglePage_HeadersBreaksAndSizes(t *testing.T) {
	t.Parallel()
	pl := `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" style:num-format="1" fo:margin-top="1cm" fo:margin-bottom="1cm" fo:margin-left="2cm" fo:margin-right="2cm"/>` +
		`<style:header-style><style:header-footer-properties fo:min-height="1cm" fo:margin-bottom="0.5cm"/></style:header-style><style:footer-style><style:header-footer-properties fo:min-height="0.5cm" fo:margin-top="0.25cm"/></style:footer-style></style:page-layout>` +
		`<style:page-layout style:name="pm2"><style:page-layout-properties fo:page-width="29.7cm" fo:page-height="21cm" style:num-format="i" fo:margin-top="2cm" fo:margin-bottom="2cm" fo:margin-left="2cm" fo:margin-right="2cm"/>` +
		`<style:footer-style><style:header-footer-properties fo:min-height="0.5cm"/></style:footer-style></style:page-layout>`
	masters := `<style:master-page style:name="Standard" style:page-layout-name="pm1"><style:header><text:p>HEAD <text:page-number>9</text:page-number> of <text:page-count>9</text:page-count></text:p></style:header><style:footer><text:p>FOOT</text:p></style:footer></style:master-page>` +
		`<style:master-page style:name="Land" style:page-layout-name="pm2"><style:header><text:p>LAND</text:p></style:header><style:footer><text:p>LASTFOOT <text:page-number>9</text:page-number></text:p></style:footer></style:master-page>`
	auto := `<style:style style:name="PB" style:family="paragraph"><style:paragraph-properties fo:break-before="page"/></style:style>` +
		`<style:style style:name="PA" style:family="paragraph"><style:paragraph-properties fo:break-after="page"/></style:style>` +
		`<style:style style:name="PL" style:family="paragraph" style:master-page-name="Land"><style:paragraph-properties style:page-number="5"/></style:style>`
	body := `<text:p>Start</text:p><text:p text:style-name="PB">Second</text:p><text:p text:style-name="PA">Third</text:p><text:p>Fourth</text:p><text:p text:style-name="PL">Land</text:p>`
	one, paged := singlePages(t, doc{autoStyles: pl, masters: masters, contentAuto: auto, body: body}, office.Options{})
	if len(one.Pages) != 1 || len(paged.Pages) < 4 {
		t.Fatalf("%d pages with the option, %d without", len(one.Pages), len(paged.Pages))
	}
	pg := one.Pages[0]
	want := "HEAD 1 of 1\nStart\nSecond\nThird\nFourth\nLand\nLASTFOOT i"
	if got := pageText(pg); got != want {
		t.Errorf("single page = %q, want %q", got, want)
	}
	if !near(pg.Width, 595.276, 0.01) {
		t.Errorf("width %v, want the first page's", pg.Width)
	}
	// The footer sits at the last master page's 2 cm from the bottom.
	if y := lineOf(pg, "LASTFOOT"); y < pg.Height-2*72/2.54-14 || y > pg.Height-2*72/2.54 {
		t.Errorf("footer baseline %v on a page %v tall", y, pg.Height)
	}
	warned := false
	for _, w := range one.Warnings {
		warned = warned || strings.Contains(w.Message, `"Land"`)
	}
	if !warned {
		t.Errorf("no warning names the wider master page: %+v", one.Warnings)
	}
}

func TestSinglePage_FootnotesColumnsAndFrames(t *testing.T) {
	t.Parallel()
	note := func(n, body string) string {
		return `<text:note text:note-class="footnote"><text:note-citation>` + n + `</text:note-citation><text:note-body><text:p>` + body + `</text:p></text:note-body></text:note>`
	}
	var b strings.Builder
	b.WriteString(`<text:p>first` + note("1", "noteone") + `</text:p>`)
	for i := range 60 {
		fmt.Fprintf(&b, `<text:p>p%02d</text:p>`, i)
	}
	b.WriteString(`<text:p>last` + note("2", "notetwo") + `</text:p>`)
	b.WriteString(`<text:section text:name="S" text:style-name="Sect">`)
	for i := range 20 {
		fmt.Fprintf(&b, `<text:p>c%02d</text:p>`, i)
	}
	b.WriteString(`</text:section><text:p>after</text:p>`)
	frame := `<draw:frame draw:style-name="fr" text:anchor-type="page" text:anchor-page-number="1" svg:x="2cm" svg:width="5cm" svg:height="2cm"><draw:text-box><text:p>BOTTOMFRAME</text:p></draw:text-box></draw:frame>`
	auto := `<style:style style:name="fr" style:family="graphic"><style:graphic-properties style:vertical-pos="bottom" style:vertical-rel="page" style:wrap="run-through"/></style:style>` +
		`<style:style style:name="Sect" style:family="section"><style:section-properties><style:columns fo:column-count="2" fo:column-gap="1cm"/></style:section-properties></style:style>`
	one, paged := singlePages(t, doc{contentAuto: auto, body: frame + b.String()}, office.Options{})
	if len(one.Pages) != 1 || len(paged.Pages) < 2 {
		t.Fatalf("%d pages with the option, %d without", len(one.Pages), len(paged.Pages))
	}
	pg := one.Pages[0]
	last, n1, n2 := lineOf(pg, "after"), lineOf(pg, "noteone"), lineOf(pg, "notetwo")
	if last < 0 || n1 < 0 || n2 < 0 || !(last < n1 && n1 < n2) {
		t.Errorf("body ends at %v, notes at %v and %v: want the notes in order after the body", last, n1, n2)
	}
	// The section's columns balance: c10 tops the second column.
	if a, k := lineOf(pg, "c00"), lineOf(pg, "c10"); a < 0 || k < 0 || a != k {
		t.Errorf("c00 at %v, c10 at %v: want them side by side", a, k)
	}
	// The frame at the page's bottom sits at the single page's.
	if y := lineOf(pg, "BOTTOMFRAME"); y < pg.Height-2*72/2.54 || y > pg.Height {
		t.Errorf("frame text at %v on a page %v tall, want it in the frame at the bottom", y, pg.Height)
	}
}

func TestSinglePage_OptionsCarryOver(t *testing.T) {
	t.Parallel()
	ann := `<office:annotation office:name="a"><dc:creator>Ada</dc:creator><text:p>On one page.</text:p></office:annotation>`
	auto := `<style:style style:name="PB" style:family="paragraph"><style:paragraph-properties fo:break-before="page"/></style:style>`
	body := `<text:h text:outline-level="1">One</text:h><text:p>` + ann + `commented<office:annotation-end office:name="a"/></text:p>` +
		`<text:h text:outline-level="1" text:style-name="PB">Two</text:h><text:p>x</text:p>`
	one, paged := singlePages(t, doc{contentAuto: auto, body: body}, office.Options{Comments: models.CommentsAnnotations})
	if len(one.Pages) != 1 || len(paged.Pages) != 2 {
		t.Fatalf("%d pages with the option, %d without", len(one.Pages), len(paged.Pages))
	}
	if len(one.Outline) != 2 || one.Outline[1].Dest.Page != 1 || one.Outline[1].Dest.Y <= one.Outline[0].Dest.Y {
		t.Errorf("outline %+v, want both headings on the single page, in order", one.Outline)
	}
	if c := one.Pages[0].Comments; len(c) != 1 || c[0].Comment.Text != "On one page." || len(c[0].Lines) == 0 {
		t.Errorf("comments %+v, want the one comment over its line", c)
	}
}

// TestSinglePage_TallerThanAnnexCIsWhole states the single page rule: a
// page sized from its content passes the 14,400 points of ISO 32000-1,
// Annex C.2, an informative limit, and holds the whole document.
func TestSinglePage_TallerThanAnnexCIsWhole(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for range 1200 {
		b.WriteString(`<text:p>text</text:p>`)
	}
	b.WriteString(`<text:p>theend</text:p>`)
	one, _ := singlePages(t, doc{body: b.String()}, office.Options{})
	if len(one.Pages) != 1 || one.Pages[0].Height <= office.MaxPageSide || !one.Pages[0].Fitted {
		t.Fatalf("%d pages, the first %v tall, fitted %v; want one page past %d points", len(one.Pages), one.Pages[0].Height, one.Pages[0].Fitted, office.MaxPageSide)
	}
	if !strings.Contains(docText(one), "theend") {
		t.Error("the end of the document is missing")
	}
	for _, w := range one.Warnings {
		if strings.Contains(w.Message, "single page") {
			t.Errorf("a whole page warns of a cut: %+v", w)
		}
	}
}

func TestSinglePage_TooTallIsCutWithAWarning(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for range 1200 {
		b.WriteString(`<text:p>text</text:p>`)
	}
	b.WriteString(`<text:p>theend</text:p>`)
	one, _ := singlePages(t, doc{body: b.String()}, office.Options{FittedLimit: 15000})
	if len(one.Pages) != 1 || one.Pages[0].Height > 15000 {
		t.Fatalf("%d pages, the first %v tall", len(one.Pages), one.Pages[0].Height)
	}
	if strings.Contains(docText(one), "theend") {
		t.Error("the content past the tallest page is drawn")
	}
	warned := false
	for _, w := range one.Warnings {
		warned = warned || strings.Contains(w.Message, "single page")
	}
	if !warned {
		t.Errorf("no warning of the content left out: %+v", one.Warnings)
	}
}

// TestSinglePage_FootnoteLinksOnThePage holds the links of a footnote on a
// single page: the footnote area is drawn under the body of the page
// trimmed to its content, and its link regions and the destinations into
// it are placed on that page, not on the untrimmed one, whose bottom lies
// far under it.
func TestSinglePage_FootnoteLinksOnThePage(t *testing.T) {
	t.Parallel()
	body := `<text:p>Call<text:note text:id="ftn1" text:note-class="footnote"><text:note-citation>1</text:note-citation><text:note-body><text:p>Note text</text:p></text:note-body></text:note> here.</text:p>`
	one, _ := singlePages(t, doc{body: body}, office.Options{})
	pg := one.Pages[0]
	if len(pg.Links) < 2 {
		t.Fatalf("%d links, want the call's and the footnote's", len(pg.Links))
	}
	for _, l := range pg.Links {
		if l.Rect.Y < 0 || l.Rect.Y+l.Rect.Height > pg.Height {
			t.Errorf("link at %+v off a page %v tall", l.Rect, pg.Height)
		}
		if l.Dest != nil && (l.Dest.Y < 0 || l.Dest.Y > pg.Height) {
			t.Errorf("destination %+v off a page %v tall", *l.Dest, pg.Height)
		}
	}
}
