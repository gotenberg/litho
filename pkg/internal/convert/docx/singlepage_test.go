package docx

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/models"
)

// textTops returns the top-down baseline of the first run of each text on
// pg, in page coordinates, keyed by the run's text.
func textTops(pg *typeset.Page) map[string]float64 {
	out := map[string]float64{}
	var walk func(frags []typeset.Fragment, m typeset.Matrix)
	walk = func(frags []typeset.Fragment, m typeset.Matrix) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Text:
				y := m.B*v.Origin.X + m.D*v.Origin.Y + m.F
				k := strings.TrimSpace(glyphText(v))
				if _, ok := out[k]; !ok {
					out[k] = y
				}
			case *typeset.Group:
				t := v.Transform
				walk(v.Content, typeset.Matrix{
					A: t.A*m.A + t.B*m.C, B: t.A*m.B + t.B*m.D,
					C: t.C*m.A + t.D*m.C, D: t.C*m.B + t.D*m.D,
					E: t.E*m.A + t.F*m.C + m.E, F: t.E*m.B + t.F*m.D + m.F,
				})
			}
		}
	}
	walk(pg.Content, typeset.Identity)
	return out
}

// imageBottoms returns the bottom edge of each image of pg, in page
// coordinates, in paint order.
func imageBottoms(pg *typeset.Page) []float64 {
	var out []float64
	var walk func(frags []typeset.Fragment, dy float64)
	walk = func(frags []typeset.Fragment, dy float64) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Image:
				out = append(out, v.Rect.Y+v.Rect.Height+dy)
			case *typeset.Group:
				walk(v.Content, dy+v.Transform.F)
			}
		}
	}
	walk(pg.Content, 0)
	return out
}

// single converts body and extra on one page and on pages.
func single(t *testing.T, body string, extra map[string]string) (one, paged *typeset.Document) {
	t.Helper()
	data := docx(t, body, extra)
	return convertBytes(t, data, office.Options{SinglePage: true}), convertBytes(t, data, office.Options{})
}

// para returns a paragraph of text.
func para(text string) string {
	return `<w:p><w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
}

func TestSinglePage_SizedToTheContent(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := range 80 {
		b.WriteString(para(fmt.Sprintf("line%02d", i)))
	}
	sect := `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1080" w:bottom="720" w:left="1440" w:header="720" w:footer="720"/></w:sectPr>`
	one, paged := single(t, b.String()+sect, nil)
	if len(paged.Pages) < 2 || len(one.Pages) != 1 {
		t.Fatalf("%d pages with the option, %d without; want 1 and several", len(one.Pages), len(paged.Pages))
	}
	pg := one.Pages[0]
	// The first section's width; its top margin, the content, the last
	// section's bottom margin.
	if pg.Width != 612 {
		t.Errorf("width %v, want the section's 612", pg.Width)
	}
	if pg.Height <= paged.Pages[0].Height || pg.Height > float64(len(paged.Pages))*792 {
		t.Errorf("height %v, want the content's, between one page and %d", pg.Height, len(paged.Pages))
	}
	if got, want := strings.Count(docText(one), "line"), strings.Count(docText(paged), "line"); got != want || got != 80 {
		t.Errorf("%d lines on the single page, %d on the pages, want 80", got, want)
	}
	tops := textTops(pg)
	if last := tops["line79"]; pg.Height-last < 36 || pg.Height-last > 36+24 {
		t.Errorf("last line's baseline %v on a page %v tall: want it above the 36 pt bottom margin, by a line", last, pg.Height)
	}
}

func TestSinglePage_BreaksDoNotBreak(t *testing.T) {
	t.Parallel()
	body := para("one") +
		`<w:p><w:r><w:t>two</w:t></w:r><w:r><w:br w:type="page"/></w:r><w:r><w:t>three</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:pageBreakBefore/></w:pPr><w:r><w:t>four</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:sectPr><w:type w:val="nextPage"/></w:sectPr></w:pPr><w:r><w:t>five</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:sectPr><w:type w:val="oddPage"/></w:sectPr></w:pPr><w:r><w:t>six</w:t></w:r></w:p>` +
		`<w:p><w:r><w:br w:type="column"/></w:r><w:r><w:t>seven</w:t></w:r></w:p>` +
		`<w:sectPr><w:type w:val="evenPage"/></w:sectPr>`
	one, paged := single(t, body, nil)
	if len(one.Pages) != 1 || len(paged.Pages) < 5 {
		t.Fatalf("%d pages with the option, %d without", len(one.Pages), len(paged.Pages))
	}
	tops := textTops(one.Pages[0])
	prev := tops["one"] - 20
	for _, w := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		y, ok := tops[w]
		if !ok {
			t.Fatalf("%q missing from %q", w, docText(one))
		}
		if y <= prev || y > prev+40 {
			t.Errorf("%q at %v, want the line after the previous one, at %v", w, y, prev)
		}
		prev = y
	}
}

func TestSinglePage_ColumnsBalanced(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := range 40 {
		b.WriteString(para(fmt.Sprintf("col%02d", i)))
	}
	body := b.String() + `<w:sectPr><w:cols w:num="2" w:space="720"/></w:sectPr>`
	one, paged := single(t, body, nil)
	if len(one.Pages) != 1 {
		t.Fatalf("%d pages", len(one.Pages))
	}
	tops := textTops(one.Pages[0])
	// Forty lines in two balanced columns: the column ends half way, the
	// twenty-first line tops the second column.
	if a, u := tops["col00"], tops["col20"]; u > a+1 || u < a-1 {
		t.Errorf("first line at %v, twenty-first at %v: want them side by side", a, u)
	}
	if got, want := docText(one), docText(paged); strings.Count(got, "col") != strings.Count(want, "col") {
		t.Errorf("lines differ: %q, %q", got, want)
	}
}

func TestSinglePage_HeaderOnceFooterOnce(t *testing.T) {
	t.Parallel()
	fld := func(instr string) string {
		return `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> ` + instr + ` </w:instrText></w:r>` +
			`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>99</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
	}
	part := func(tag, inner string) string {
		return `<w:` + tag + ` ` + nsDecl + `><w:p>` + inner + `</w:p></w:` + tag + `>`
	}
	extra := map[string]string{
		"word/header1.xml": part("hdr", `<w:r><w:t>Head</w:t></w:r>`),
		"word/header2.xml": part("hdr", `<w:r><w:t>Cover</w:t></w:r>`),
		"word/footer1.xml": part("ftr", `<w:r><w:t xml:space="preserve">Page </w:t></w:r>`+fld("PAGE")+`<w:r><w:t xml:space="preserve"> of </w:t></w:r>`+fld("NUMPAGES")),
	}
	var b strings.Builder
	for range 120 {
		b.WriteString(para("text"))
	}
	body := b.String() + `<w:sectPr><w:headerReference w:type="default" r:id="rIdheader1"/><w:headerReference w:type="first" r:id="rIdheader2"/>` +
		`<w:footerReference w:type="default" r:id="rIdfooter1"/><w:titlePg/></w:sectPr>`
	one, paged := single(t, body, extra)
	if len(one.Pages) != 1 || len(paged.Pages) < 3 {
		t.Fatalf("%d pages with the option, %d without", len(one.Pages), len(paged.Pages))
	}
	got := strings.NewReplacer(" ", "", "\n", "").Replace(docText(one))
	// The first page's header, the last page's footer, page 1 of 1.
	if strings.Count(got, "Cover") != 1 || strings.Contains(got, "Head") || strings.Count(got, "Page1of1") != 1 {
		t.Errorf("single page reads %q, want the cover header once and Page 1 of 1 once", got)
	}
	pg := one.Pages[0]
	tops := textTops(pg)
	if y := tops["Cover"]; y > 72 {
		t.Errorf("header at %v, want in the top margin", y)
	}
	if y := tops["Page"]; y < pg.Height-72 || y > pg.Height {
		t.Errorf("footer at %v on a page %v tall, want in the bottom margin", y, pg.Height)
	}
}

func TestSinglePage_FootnotesAfterTheBody(t *testing.T) {
	t.Parallel()
	notes := `<w:footnotes ` + nsDecl + `><w:footnote w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:footnote>` +
		`<w:footnote w:id="1"><w:p><w:r><w:t>noteone</w:t></w:r></w:p></w:footnote>` +
		`<w:footnote w:id="2"><w:p><w:r><w:t>notetwo</w:t></w:r></w:p></w:footnote></w:footnotes>`
	var b strings.Builder
	b.WriteString(`<w:p><w:r><w:t>callone</w:t></w:r><w:r><w:footnoteReference w:id="1"/></w:r></w:p>`)
	for range 70 {
		b.WriteString(para("text"))
	}
	b.WriteString(`<w:p><w:r><w:t>calltwo</w:t></w:r><w:r><w:footnoteReference w:id="2"/></w:r></w:p>`)
	b.WriteString(para("lastline"))
	one, paged := single(t, b.String(), map[string]string{"word/footnotes.xml": notes})
	if len(one.Pages) != 1 || len(paged.Pages) < 2 {
		t.Fatalf("%d pages with the option, %d without", len(one.Pages), len(paged.Pages))
	}
	tops := textTops(one.Pages[0])
	last, n1, n2 := tops["lastline"], tops["noteone"], tops["notetwo"]
	if n1 == 0 || n2 == 0 {
		t.Fatalf("notes missing: %q", docText(one))
	}
	if !(last < n1 && n1 < n2) || n2 > one.Pages[0].Height-72+12 {
		t.Errorf("body ends at %v, notes at %v and %v on a page %v tall: want the notes in order after the body, above the bottom margin", last, n1, n2, one.Pages[0].Height)
	}
}

// TestSinglePage_TallerThanAnnexCIsWhole states the single page rule: a
// page sized from its content passes the 14,400 points of ISO 32000-1,
// Annex C.2, an informative limit, and holds the whole document.
func TestSinglePage_TallerThanAnnexCIsWhole(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for range 2000 {
		b.WriteString(para("text"))
	}
	b.WriteString(para("theend"))
	doc, err := Convert(context.Background(), bytes.NewReader(docx(t, b.String(), nil)), testFonts(t), office.Options{SinglePage: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 1 || doc.Pages[0].Height <= office.MaxPageSide || !doc.Pages[0].Fitted {
		t.Fatalf("%d pages, the first %v tall, fitted %v; want one page past %d points", len(doc.Pages), doc.Pages[0].Height, doc.Pages[0].Fitted, office.MaxPageSide)
	}
	if !strings.Contains(docText(doc), "theend") {
		t.Error("the end of the document is missing")
	}
	for _, w := range doc.Warnings {
		if strings.Contains(w.Message, "single page") {
			t.Errorf("a whole page warns of a cut: %+v", w)
		}
	}
}

func TestSinglePage_TooTallIsCutWithAWarning(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for range 2000 {
		b.WriteString(para("text"))
	}
	b.WriteString(para("theend"))
	doc, err := Convert(context.Background(), bytes.NewReader(docx(t, b.String(), nil)), testFonts(t), office.Options{SinglePage: true, FittedLimit: 15000})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 1 || doc.Pages[0].Height > 15000 || doc.Pages[0].Height < 15000-72-36 {
		t.Fatalf("%d pages, the first %v tall; want one filled to 15000", len(doc.Pages), doc.Pages[0].Height)
	}
	if strings.Contains(docText(doc), "theend") {
		t.Error("the content past the tallest page is drawn")
	}
	warned := false
	for _, w := range doc.Warnings {
		warned = warned || strings.Contains(w.Message, "single page")
	}
	if !warned {
		t.Errorf("no warning of the content left out: %+v", doc.Warnings)
	}
}

func TestSinglePage_WiderSectionWarns(t *testing.T) {
	t.Parallel()
	body := para("narrow") + `<w:p><w:pPr><w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr></w:pPr></w:p>` +
		para("wide") + `<w:sectPr><w:pgSz w:w="15840" w:h="12240" w:orient="landscape"/></w:sectPr>`
	one, _ := single(t, body, nil)
	if len(one.Pages) != 1 || one.Pages[0].Width != 612 {
		t.Fatalf("%d pages, the first %v wide", len(one.Pages), one.Pages[0].Width)
	}
	warned := false
	for _, w := range one.Warnings {
		warned = warned || strings.Contains(w.Message, "section 2")
	}
	if !warned {
		t.Errorf("no warning names the wider section: %+v", one.Warnings)
	}
}

func TestSinglePage_DrawingsFromThePage(t *testing.T) {
	t.Parallel()
	// A picture 100 by 50 points, neither turned nor flipped.
	flat := strings.Replace(graphic(""), `<a:xfrm rot="5400000" flipH="1">`, `<a:xfrm>`, 1)
	anchor := func(v string) string {
		return `<w:p><w:r><w:drawing><wp:anchor behindDoc="0"><wp:positionH relativeFrom="page"><wp:posOffset>381000</wp:posOffset></wp:positionH>` +
			`<wp:positionV relativeFrom="page">` + v + `</wp:positionV><wp:extent cx="1270000" cy="635000"/><wp:wrapNone/>` +
			`<wp:docPr id="2" name="q"/>` + flat + `</wp:anchor></w:drawing></w:r><w:r><w:t>anchor</w:t></w:r></w:p>`
	}
	var b strings.Builder
	b.WriteString(anchor(`<wp:align>bottom</wp:align>`))
	b.WriteString(anchor(`<wp:posOffset>508000</wp:posOffset>`))
	for range 150 {
		b.WriteString(para("text"))
	}
	b.WriteString(`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr>`)
	one, paged := single(t, b.String(), map[string]string{"word/media/a.png": pngData(t, 4, 2)})
	if len(one.Pages) != 1 || len(paged.Pages) < 3 {
		t.Fatalf("%d pages with the option, %d without", len(one.Pages), len(paged.Pages))
	}
	pg := one.Pages[0]
	got := imageBottoms(pg)
	if len(got) != 2 {
		t.Fatalf("%d images, want 2", len(got))
	}
	// The drawing aligned to the page's bottom sits at the bottom of the
	// single page, the one 40 points down from the page's top stays
	// there: each was placed on the first page of the paged document.
	var bottom, top bool
	for _, y := range got {
		bottom = bottom || y > pg.Height-0.5 && y < pg.Height+0.5
		top = top || y > 40+50-0.5 && y < 40+50+0.5
	}
	if !bottom || !top {
		t.Errorf("image bottoms %v on a page %v tall, want one at the bottom and one at 90", got, pg.Height)
	}
}

// TestSinglePage_OptionsCarryOver holds the options that place things on
// pages: the form fields keep their order, the outline points into the
// single page, the comments hang over their lines, the tracked changes
// print as on the pages.
func TestSinglePage_OptionsCarryOver(t *testing.T) {
	t.Parallel()
	brk := `<w:p><w:r><w:br w:type="page"/></w:r></w:p>`
	styles := stylesPart(`<w:style w:type="paragraph" w:styleId="H1"><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style>`)
	body := `<w:p><w:pPr><w:pStyle w:val="H1"/></w:pPr><w:r><w:t>One</w:t></w:r></w:p>` + controlsBody + brk +
		`<w:p><w:pPr><w:pStyle w:val="H1"/></w:pPr><w:r><w:t>Two</w:t></w:r></w:p>` + controlsBody + brk +
		`<w:p>` + rs("0") + tr("commented") + re("0") + rf("0") + `</w:p>` +
		`<w:p><w:ins w:id="9" w:author="Ada"><w:r><w:t>inserted</w:t></w:r></w:ins><w:del w:id="10" w:author="Ada"><w:r><w:delText>deleted</w:delText></w:r></w:del></w:p>`
	extra := commentParts([][3]string{{"0", "Ada", "On one page."}}, nil, nil)
	extra["word/styles.xml"] = styles
	data := docx(t, body, extra)
	opts := office.Options{FormFields: true, Comments: models.CommentsAnnotations, TrackedChanges: models.TrackedChangesMarked}
	paged := convertBytes(t, data, opts)
	opts.SinglePage = true
	one := convertBytes(t, data, opts)
	if len(one.Pages) != 1 || len(paged.Pages) != 3 {
		t.Fatalf("%d pages with the option, %d without", len(one.Pages), len(paged.Pages))
	}
	var pagedFields []string
	for _, pg := range paged.Pages {
		for _, f := range pg.Fields {
			pagedFields = append(pagedFields, f.Name+"="+f.Value)
		}
	}
	var oneFields []string
	prevY := -1.0
	for _, f := range one.Pages[0].Fields {
		oneFields = append(oneFields, f.Name+"="+f.Value)
		if f.Rect.Y < prevY {
			t.Errorf("field %q at %v above the field before it, at %v: the order is not the page's", f.Name, f.Rect.Y, prevY)
		}
		prevY = f.Rect.Y
	}
	if strings.Join(oneFields, ",") != strings.Join(pagedFields, ",") || len(oneFields) != 14 {
		t.Errorf("fields %q, want the pages' %q", oneFields, pagedFields)
	}
	if len(one.Outline) != 2 || one.Outline[1].Title != "Two" || one.Outline[1].Dest.Page != 1 || one.Outline[1].Dest.Y <= one.Outline[0].Dest.Y {
		t.Errorf("outline %+v, want both headings on the single page, in order", one.Outline)
	}
	if got := commentsOf(one); len(got) != 1 || len(got[0]) != 1 || got[0][0].Comment.Text != "On one page." || len(got[0][0].Lines) == 0 {
		t.Errorf("comments %+v, want the one comment over its line", got)
	}
	text := docText(one)
	if !strings.Contains(text, "inserted") || !strings.Contains(text, "deleted") {
		t.Errorf("tracked changes not marked: %q", text)
	}
}
