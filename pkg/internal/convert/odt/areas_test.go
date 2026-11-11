package odt

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// listStyles are a numbered list in label-alignment mode with a second
// level showing both levels, a bullet list followed by a space, and a
// numbered list in label-width-and-position mode.
const listStyles = `<text:list-style style:name="LN">` +
	`<text:list-level-style-number text:level="1" style:num-suffix="." style:num-format="1"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="listtab" text:list-tab-stop-position="1.27cm" fo:text-indent="-0.635cm" fo:margin-left="1.27cm"/></style:list-level-properties></text:list-level-style-number>` +
	`<text:list-level-style-number text:level="2" style:num-suffix=")" style:num-format="a" text:display-levels="2"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="listtab" text:list-tab-stop-position="1.905cm" fo:text-indent="-0.635cm" fo:margin-left="1.905cm"/></style:list-level-properties></text:list-level-style-number></text:list-style>` +
	`<text:list-style style:name="LB"><text:list-level-style-bullet text:level="1" text:bullet-char="•"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="space" fo:text-indent="-0.635cm" fo:margin-left="1.27cm"/></style:list-level-properties></text:list-level-style-bullet></text:list-style>` +
	`<text:list-style style:name="LR"><text:list-level-style-number text:level="1" style:num-suffix="." style:num-format="I"><style:list-level-properties text:space-before="0.5cm" text:min-label-width="1cm"/></text:list-level-style-number></text:list-style>`

func TestConvert_Lists(t *testing.T) {
	item := func(s string) string { return `<text:list-item><text:p>` + s + `</text:p></text:list-item>` }
	cases := []struct {
		name, body string
		want       []string
	}{
		{
			"numbers and nesting", `<text:list text:style-name="LN">` + item("one") + `<text:list-item><text:p>two</text:p><text:list>` + item("sub a") + item("sub b") + `</text:list></text:list-item>` + item("three") + `</text:list>`,
			[]string{"1.one", "2.two", "2.a)sub a", "2.b)sub b", "3.three"},
		},
		{
			"continue numbering", `<text:list text:style-name="LN">` + item("a") + `</text:list><text:list text:style-name="LN" text:continue-numbering="true">` + item("b") + `</text:list>`,
			[]string{"1.a", "2.b"},
		},
		{
			"new list restarts", `<text:list text:style-name="LN">` + item("a") + `</text:list><text:list text:style-name="LN">` + item("b") + `</text:list>`,
			[]string{"1.a", "1.b"},
		},
		{
			"continue-list by id", `<text:list xml:id="l1" text:style-name="LN">` + item("a") + `</text:list><text:list text:style-name="LB">` + item("x") + `</text:list><text:list text:style-name="LN" text:continue-list="l1">` + item("b") + `</text:list>`,
			[]string{"1.a", "• x", "2.b"},
		},
		{
			"start value", `<text:list text:style-name="LN"><text:list-item text:start-value="7"><text:p>s</text:p></text:list-item>` + item("t") + `</text:list>`,
			[]string{"7.s", "8.t"},
		},
		{
			"header unnumbered", `<text:list text:style-name="LN"><text:list-header><text:p>head</text:p></text:list-header>` + item("a") + `</text:list>`,
			[]string{"head", "1.a"},
		},
		{
			"roman label width mode", `<text:list text:style-name="LR">` + item("a") + item("b") + `</text:list>`,
			[]string{"I.a", "II.b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := doc{styles: listStyles, body: tc.body}
			got := strings.Split(docText(convertDoc(t, d.pack(t), office.Options{})), "\n")
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("lines = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConvert_ListLabelPlacement(t *testing.T) {
	// The bullet sits at margin-left plus text-indent, 1.27 - 0.635 cm
	// from the page area, and a space follows it (LibreOffice measured:
	// 18pt past the margin).
	d := doc{styles: listStyles, body: `<text:list text:style-name="LB"><text:list-item><text:p>Bullet</text:p></text:list-item></text:list>`}
	ls := lines(convertDoc(t, d.pack(t), office.Options{}).Pages[0])
	margin := 2 * 72 / 2.54
	if len(ls) != 1 || !near(ls[0].x, margin+0.635*72/2.54, 0.2) {
		t.Errorf("label line = %+v, want x %.2f", ls, margin+18)
	}
}

func TestConvert_ListLabelWiderThanItsGap(t *testing.T) {
	// A label wider than the room before the list tab stop starts at the
	// first line's start, margin-left plus text-indent, and the text at
	// the next default tab stop counted from the indent (LibreOffice
	// measured, odfexport/listformat.odt: label 18pt and text 71.5pt past
	// the margin).
	style := `<text:list-style style:name="LW"><text:list-level-style-number text:level="1" style:num-prefix="&gt;&gt;" style:num-suffix="&lt;&lt;" style:num-format="1"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="listtab" text:list-tab-stop-position="0.5in" fo:text-indent="-0.25in" fo:margin-left="0.5in"/></style:list-level-properties></text:list-level-style-number></text:list-style>`
	d := doc{styles: style, body: `<text:list text:style-name="LW"><text:list-item><text:p>Item</text:p></text:list-item></text:list>`}
	rs := runs(convertDoc(t, d.pack(t), office.Options{}).Pages[0])
	margin := 2 * 72 / 2.54
	var label, item float64 = -1, -1
	for _, r := range rs {
		switch {
		case strings.Contains(r.text, "1"):
			label = r.x
		case strings.Contains(r.text, "Item"):
			item = r.x
		}
	}
	if !near(label, margin+18, 0.3) || !near(item, margin+36+defaultTabDistance, 0.3) {
		t.Errorf("label at %.2f, text at %.2f; want %.2f and %.2f", label, item, margin+18, margin+36+defaultTabDistance)
	}
}

func TestConvert_Tables(t *testing.T) {
	auto := `<style:style style:name="Tb" style:family="table"><style:table-properties style:width="12cm" table:align="left"/></style:style>` +
		`<style:style style:name="A" style:family="table-column"><style:table-column-properties style:column-width="3cm"/></style:style>` +
		`<style:style style:name="B" style:family="table-column"><style:table-column-properties style:column-width="6cm"/></style:style>`
	cell := func(s string) string { return `<table:table-cell><text:p>` + s + `</text:p></table:table-cell>` }
	body := `<table:table table:style-name="Tb"><table:table-column table:style-name="A" table:number-columns-repeated="2"/><table:table-column table:style-name="B"/>` +
		`<table:table-header-rows><table:table-row>` + cell("H1") + cell("H2") + cell("H3") + `</table:table-row></table:table-header-rows>` +
		`<table:table-row><table:table-cell table:number-columns-spanned="2"><text:p>span</text:p></table:table-cell><table:covered-table-cell/>` + cell("c") + `</table:table-row>` +
		`<table:table-row table:number-rows-repeated="2">` + cell("r") + cell("r") + cell("r") + `</table:table-row></table:table>`
	res := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{})
	ls := lines(res.Pages[0])
	if got := pageText(res.Pages[0]); got != "H1H2H3\nspanc\nrrr\nrrr" {
		t.Fatalf("text = %q", got)
	}
	// The third column starts 6 cm into the table.
	var xs []float64
	for _, r := range runs(res.Pages[0]) {
		if strings.HasPrefix(r.text, "H") {
			xs = append(xs, r.x)
		}
	}
	margin := 2 * 72 / 2.54
	if len(xs) != 3 || !near(xs[1]-xs[0], 3*72/2.54, 0.5) || !near(xs[2]-xs[0], 6*72/2.54, 0.5) || !near(xs[0], margin, 1) {
		t.Errorf("header x = %v (lines %v)", xs, ls)
	}
	if res.Structure == nil || !strings.Contains(roles(res.Structure), "Table,THead,TR,TH") {
		t.Errorf("structure = %s", roles(res.Structure))
	}
	// The header rows' cells head their columns; the merge spans two.
	b := newTestBuilder(t, doc{contentAuto: auto, body: body})
	root, _ := b.document(odfBody(b))
	var ths, spans int
	walkNodes(root, func(n *layout.Node) {
		if n.Role == "TH" && n.Scope == compose.ScopeColumn {
			ths++
		}
		if n.Role == "TD" && n.ColSpan == 2 {
			spans++
		}
	})
	if ths != 3 || spans != 1 {
		t.Errorf("%d column header cells, %d spans; want 3 and 1", ths, spans)
	}
}

// roles returns the structure tree's roles in document order.
func roles(e *compose.StructElem) string {
	var out []string
	var walk func(*compose.StructElem)
	walk = func(e *compose.StructElem) {
		out = append(out, e.Role)
		for _, k := range e.Kids {
			walk(k)
		}
	}
	walk(e)
	return strings.Join(out, ",")
}

func TestConvert_HeadersAndPages(t *testing.T) {
	pl := `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" style:num-format="1" fo:margin-top="1cm" fo:margin-bottom="1cm" fo:margin-left="2cm" fo:margin-right="2cm"/>` +
		`<style:header-style><style:header-footer-properties fo:min-height="1cm" fo:margin-bottom="0.5cm"/></style:header-style><style:footer-style><style:header-footer-properties fo:min-height="0.5cm" fo:margin-top="0.25cm"/></style:footer-style></style:page-layout>` +
		`<style:page-layout style:name="pm2"><style:page-layout-properties fo:page-width="29.7cm" fo:page-height="21cm" style:num-format="i" fo:margin-top="2cm" fo:margin-bottom="2cm" fo:margin-left="2cm" fo:margin-right="2cm"/></style:page-layout>`
	masters := `<style:master-page style:name="Standard" style:page-layout-name="pm1"><style:header><text:p>HEAD <text:page-number>9</text:page-number> of <text:page-count>9</text:page-count></text:p></style:header><style:footer><text:p>FOOT</text:p></style:footer></style:master-page>` +
		`<style:master-page style:name="Land" style:page-layout-name="pm2"><style:header><text:p>LAND <text:page-number>9</text:page-number></text:p></style:header></style:master-page>`
	auto := `<style:style style:name="PB" style:family="paragraph"><style:paragraph-properties fo:break-before="page"/></style:style>` +
		`<style:style style:name="PL" style:family="paragraph" style:master-page-name="Land"><style:paragraph-properties style:page-number="5"/></style:style>`
	body := `<text:p>Start</text:p><text:p text:style-name="PB">Second</text:p><text:p text:style-name="PB">Third</text:p><text:p text:style-name="PL">Land</text:p>`
	res := convertDoc(t, doc{autoStyles: pl, masters: masters, contentAuto: auto, body: body}.pack(t), office.Options{})
	if len(res.Pages) != 4 {
		t.Fatalf("pages = %d", len(res.Pages))
	}
	// LibreOffice inserts a blank page before the restart at 5 (the
	// page after 3 would be 4, an even page where 5 is odd): the PDF
	// leaves it out and the page count counts it.
	want := []string{"HEAD 1 of 5\nStart\nFOOT", "HEAD 2 of 5\nSecond\nFOOT", "HEAD 3 of 5\nThird\nFOOT", "LAND v\nLand"}
	for i, w := range want {
		if got := pageText(res.Pages[i]); got != w {
			t.Errorf("page %d = %q, want %q", i+1, got, w)
		}
	}
	if pg := res.Pages[3]; pg.Width < pg.Height {
		t.Errorf("landscape page is %vx%v", pg.Width, pg.Height)
	}
	// The body starts below the header's minimum height, which includes
	// its spacing: 1 cm margin and 1 cm header (LibreOffice: the first
	// baseline 67.9pt from the top).
	ls := lines(res.Pages[0])
	if len(ls) < 2 || !near(ls[1].y, 67.9, 0.5) {
		t.Errorf("body baseline = %+v", ls)
	}
	landscape := convertDoc(t, doc{body: `<text:p>a</text:p>`}.pack(t), office.Options{Landscape: true})
	if pg := landscape.Pages[0]; pg.Width < pg.Height {
		t.Errorf("Landscape option: %vx%v", pg.Width, pg.Height)
	}
	paper := convertDoc(t, doc{body: `<text:p>a</text:p>`}.pack(t), office.Options{PageWidth: 612, PageHeight: 792})
	if pg := paper.Pages[0]; pg.Width != 612 || pg.Height != 792 {
		t.Errorf("paper option: %vx%v", pg.Width, pg.Height)
	}
}

func TestConvert_Notes(t *testing.T) {
	note := func(class, n, body string) string {
		return `<text:note text:note-class="` + class + `"><text:note-citation>` + n + `</text:note-citation><text:note-body><text:p>` + body + `</text:p></text:note-body></text:note>`
	}
	body := `<text:p>Text` + note("footnote", "1", "Foot body") + ` and` + note("endnote", "i", "End body") + `.</text:p>`
	res := convertDoc(t, doc{body: body}.pack(t), office.Options{})
	if len(res.Pages) != 2 {
		t.Fatalf("pages = %d: %q", len(res.Pages), docText(res))
	}
	if got := pageText(res.Pages[0]); got != "1i\nText and.\n1Foot body" {
		t.Errorf("page 1 = %q", got)
	}
	if got := pageText(res.Pages[1]); got != "iEnd body" {
		t.Errorf("page 2 = %q", got)
	}
	// Writer links each citation to its note's number and back.
	var dests []int
	for _, p := range res.Pages {
		for _, l := range p.Links {
			if l.Dest != nil {
				dests = append(dests, l.Dest.Page)
			}
		}
	}
	if fmt.Sprint(dests) != "[1 2 1 1]" {
		t.Errorf("note link destinations = %v, want [1 2 1 1]", dests)
	}
	ls := lines(res.Pages[0])
	if ls[len(ls)-1].size != 10 {
		t.Errorf("footnote body size = %v, want Writer's built-in Footnote style's 10pt", ls[len(ls)-1].size)
	}
}

func TestConvert_Spacing(t *testing.T) {
	auto := `<style:style style:name="M" style:family="paragraph"><style:paragraph-properties fo:margin-top="0.5cm" fo:margin-bottom="0.25cm"/></style:style>` +
		`<style:style style:name="K" style:family="paragraph"><style:paragraph-properties fo:margin-top="0.5cm" fo:margin-bottom="0.25cm" style:contextual-spacing="true"/></style:style>` +
		`<style:style style:name="B" style:family="paragraph"><style:paragraph-properties fo:border="1pt solid #ff0000" fo:padding="0.2cm"/></style:style>` +
		`<style:style style:name="F" style:family="paragraph"><style:paragraph-properties fo:margin-top="0.5cm" fo:break-before="page"/></style:style>`
	cm := 72 / 2.54
	cases := []struct {
		name, body string
		gap        float64
	}{
		// LibreOffice measured: 13.8 of line plus 0.75 cm of margins.
		{"margins add up", `<text:p text:style-name="M">a</text:p><text:p text:style-name="M">b</text:p>`, 13.8 + 0.75*cm},
		{"contextual spacing", `<text:p text:style-name="K">a</text:p><text:p text:style-name="K">b</text:p>`, 13.8},
		{"joined borders", `<text:p text:style-name="B">a</text:p><text:p text:style-name="B">b</text:p>`, 13.8},
		// After a forced break the new page starts with the next
		// paragraph's top margin alone, as the first page starts with the
		// first's (LibreOffice: 1 cm, not 2).
		{"forced break", `<text:p text:style-name="M">a</text:p><text:p text:style-name="F">b</text:p>`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := convertDoc(t, doc{contentAuto: auto, body: tc.body}.pack(t), office.Options{})
			if tc.gap == 0 {
				first, second := lines(res.Pages[0]), lines(res.Pages[len(res.Pages)-1])
				if len(res.Pages) != 2 || !near(second[0].y, first[0].y, 0.3) {
					t.Errorf("pages %d, first lines %+v and %+v", len(res.Pages), first, second)
				}
				return
			}
			ls := lines(res.Pages[0])
			if len(ls) != 2 || !near(ls[1].y-ls[0].y, tc.gap, 0.3) {
				t.Errorf("lines = %+v, want a gap of %.2f", ls, tc.gap)
			}
		})
	}
}

func TestConvert_Frames(t *testing.T) {
	auto := `<style:style style:name="fr1" style:family="graphic"><style:graphic-properties style:wrap="right" style:horizontal-pos="left" style:horizontal-rel="paragraph"/></style:style>`
	long := strings.Repeat("words and more words ", 20)
	body := `<text:p>Inline <draw:frame text:anchor-type="as-char" svg:width="2cm" svg:height="1cm"><draw:image xlink:href="Pictures/a.png"/></draw:frame> picture.</text:p>` +
		`<text:p><draw:frame draw:style-name="fr1" text:anchor-type="paragraph" svg:width="3cm" svg:height="1.5cm"><draw:image xlink:href="Pictures/a.png"/></draw:frame>` + long + `</text:p>`
	res := convertDoc(t, doc{contentAuto: auto, body: body, pictures: map[string][]byte{"Pictures/a.png": tinyPNG(t)}}.pack(t), office.Options{})
	var imgs []compose.Rect
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Image:
				imgs = append(imgs, v.Rect)
			case *compose.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	cm := 72 / 2.54
	sizes := map[[2]int]bool{}
	for _, r := range imgs {
		sizes[[2]int{int(r.Width*10 + 0.5), int(r.Height*10 + 0.5)}] = true
	}
	if len(imgs) != 2 || !sizes[[2]int{int(20*cm + 0.5), int(10*cm + 0.5)}] || !sizes[[2]int{int(30*cm + 0.5), int(15*cm + 0.5)}] {
		t.Fatalf("images = %+v", imgs)
	}
	ls := lines(res.Pages[0])
	margin := 2 * cm
	// The second paragraph's first line starts right of the floating
	// picture (LibreOffice measured: 3 cm past the margin).
	if len(ls) < 3 || !near(ls[1].x, margin+3*cm, 0.5) {
		t.Errorf("wrapped line = %+v", ls[1])
	}
}

func TestConvert_TableOfContentsRegenerated(t *testing.T) {
	// LibreOffice, through Gotenberg's updateIndexes, rebuilds the table
	// from the headings: the stale saved entry goes, each heading's page
	// comes from the layout.
	tmpl := `<text:table-of-content-entry-template text:outline-level="1" text:style-name="C1"><text:index-entry-chapter/><text:index-entry-text/><text:index-entry-tab-stop style:type="right" style:leader-char="."/><text:index-entry-page-number/></text:table-of-content-entry-template>`
	body := `<text:table-of-content text:name="T"><text:table-of-content-source text:outline-level="1"><text:index-title-template>Contents</text:index-title-template>` + tmpl + `</text:table-of-content-source>` +
		`<text:index-body><text:p>stale<text:tab/>9</text:p></text:index-body></text:table-of-content>` +
		`<text:h text:outline-level="1">One</text:h><text:h text:outline-level="2">Deep</text:h><text:p text:style-name="PB">x</text:p><text:h text:outline-level="1">Two</text:h>`
	auto := `<style:style style:name="PB" style:family="paragraph"><style:paragraph-properties fo:break-before="page"/></style:style>`
	res := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{})
	ls := lines(res.Pages[0])
	// The engine fills the right-aligned tab with its dot leader, as
	// LibreOffice draws it, so a run of dots stands for the leader.
	leader := regexp.MustCompile(`\.{2,}`)
	var got []string
	for _, l := range ls[:3] {
		got = append(got, leader.ReplaceAllString(strings.Join(strings.Fields(l.text), ""), "."))
	}
	if strings.Join(got, "|") != "Contents|One.1|Two.2" {
		t.Errorf("table = %q", got)
	}
}

func TestConvert_TextAlignStartEndAreAbsolute(t *testing.T) {
	// Writer prints start at the left and end at the right in either
	// writing mode (LibreOffice measured: 56.7pt and the right margin).
	margin := 2 * 72 / 2.54
	for _, tc := range []struct {
		align, mode string
		left        bool
	}{
		{"start", "lr-tb", true},
		{"end", "lr-tb", false},
		{"start", "rl-tb", true},
		{"end", "rl-tb", false},
	} {
		t.Run(tc.align+" "+tc.mode, func(t *testing.T) {
			auto := `<style:style style:name="A" style:family="paragraph"><style:paragraph-properties fo:text-align="` + tc.align + `" style:writing-mode="` + tc.mode + `"/></style:style>`
			rs := runs(convertDoc(t, doc{contentAuto: auto, body: `<text:p text:style-name="A">xx</text:p>`}.pack(t), office.Options{}).Pages[0])
			if len(rs) == 0 {
				t.Fatal("no text")
			}
			if got := near(rs[0].x, margin, 0.5); got != tc.left {
				t.Errorf("text at x %.2f, at the left margin %v, want %v", rs[0].x, got, tc.left)
			}
		})
	}
}

func TestConvert_FrameCentredOnParagraph(t *testing.T) {
	// A frame anchored to its paragraph and centred on it sits in the
	// middle of the content area, whether text runs through it or wraps
	// beside it (LibreOffice measured, odfexport/relh-page.odt and
	// odfimport/fdo90130-1.odt).
	margin := 2 * 72 / 2.54
	width := 21*72/2.54 - 2*margin
	for _, wrap := range []string{"run-through", "parallel"} {
		t.Run(wrap, func(t *testing.T) {
			auto := `<style:style style:name="fr" style:family="graphic"><style:graphic-properties style:wrap="` + wrap + `" style:horizontal-pos="center" style:horizontal-rel="paragraph" style:vertical-pos="top" style:vertical-rel="paragraph" fo:padding="0cm" fo:border="none"/></style:style>`
			body := `<text:p><draw:frame draw:style-name="fr" text:anchor-type="paragraph" svg:width="4cm" svg:height="2cm"><draw:text-box><text:p>In</text:p></draw:text-box></draw:frame></text:p>`
			rs := runs(convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0])
			want := margin + (width-4*72/2.54)/2
			if len(rs) == 0 || !near(rs[0].x, want, 0.5) {
				t.Errorf("frame text at %+v, want x %.2f", rs, want)
			}
		})
	}
}

func TestConvert_FirstPageFooterInLibreOfficeNamespace(t *testing.T) {
	// LibreOffice's own loext:footer-first prints on the first page, the
	// footer on the others (LibreOffice measured, odfimport/fdo79269.odt).
	pl := `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" fo:margin-top="2cm" fo:margin-bottom="2cm" fo:margin-left="2cm" fo:margin-right="2cm"/><style:footer-style><style:header-footer-properties fo:min-height="1cm"/></style:footer-style></style:page-layout>`
	masters := `<style:master-page style:name="Standard" style:page-layout-name="pm1"><style:footer><text:p>SECOND</text:p></style:footer><loext:footer-first><text:p>FIRST</text:p></loext:footer-first></style:master-page>`
	auto := `<style:style style:name="PB" style:family="paragraph"><style:paragraph-properties fo:break-before="page"/></style:style>`
	res := convertDoc(t, doc{autoStyles: pl, masters: masters, contentAuto: auto, body: `<text:p>a</text:p><text:p text:style-name="PB">b</text:p>`}.pack(t), office.Options{})
	if len(res.Pages) != 2 {
		t.Fatalf("pages = %d", len(res.Pages))
	}
	for i, want := range []string{"FIRST", "SECOND"} {
		if got := pageText(res.Pages[i]); !strings.Contains(got, want) {
			t.Errorf("page %d = %q, want %s", i+1, got, want)
		}
	}
}

func TestConvert_ListHeaderHeadingTakesNoNumber(t *testing.T) {
	// LibreOffice measured (odfexport/tdf107696.odt): the list header
	// heading prints without a number and the next heading of its level
	// takes 1.1.
	outline := `<text:outline-style style:name="Outline"><text:outline-level-style text:level="1" style:num-format="1"/><text:outline-level-style text:level="2" style:num-format="1" text:display-levels="2"/></text:outline-style>`
	body := `<text:h text:outline-level="1">One</text:h><text:h text:outline-level="2" text:is-list-header="true">Header</text:h><text:h text:outline-level="2">Two</text:h>`
	got := pageText(convertDoc(t, doc{styles: outline, body: body}.pack(t), office.Options{}).Pages[0])
	if got != "1One\nHeader\n1.1Two" {
		t.Errorf("headings = %q", got)
	}
}

func TestConvert_ListHeadingOutlineTitle(t *testing.T) {
	// LibreOffice measured (odfimport/empty-svg-family-name.odt): a
	// heading in a numbered list is bookmarked with its label first.
	body := `<text:list text:style-name="LN"><text:list-item><text:h text:outline-level="1">Head</text:h></text:list-item></text:list>`
	res := convertDoc(t, doc{styles: listStyles, body: body}.pack(t), office.Options{})
	if len(res.Outline) != 1 || res.Outline[0].Title != "1. Head" {
		t.Errorf("outline = %+v", res.Outline)
	}
}

func TestConvert_FrameBeforeTheFirstMaster(t *testing.T) {
	pl := `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" fo:margin-top="2cm" fo:margin-bottom="2cm" fo:margin-left="2cm" fo:margin-right="2cm"/></style:page-layout>` +
		`<style:page-layout style:name="pm2"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" fo:margin-top="10cm" fo:margin-bottom="2cm" fo:margin-left="2cm" fo:margin-right="2cm"/></style:page-layout>`
	masters := `<style:master-page style:name="Standard" style:page-layout-name="pm1"/><style:master-page style:name="First" style:page-layout-name="pm2" style:next-style-name="Standard"/>`
	auto := `<style:style style:name="PF" style:family="paragraph" style:master-page-name="First"/>`
	body := `<draw:frame text:anchor-type="page" text:anchor-page-number="1" svg:x="2cm" svg:y="2cm" svg:width="5cm" svg:height="2cm"><draw:text-box><text:p>Frame</text:p></draw:text-box></draw:frame>` +
		`<text:p text:style-name="PF">Body</text:p>`
	res := convertDoc(t, doc{autoStyles: pl, masters: masters, contentAuto: auto, body: body}.pack(t), office.Options{})
	if len(res.Pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(res.Pages))
	}
	// The body starts under the First master's 10 cm top margin (283.5
	// pt), the frame at 2 cm.
	for _, l := range lines(res.Pages[0]) {
		if strings.Contains(l.text, "Body") && l.y < 283 {
			t.Errorf("body baseline %.1f above the First master's margin", l.y)
		}
	}
	if got := pageText(res.Pages[0]); !strings.Contains(got, "Frame") || !strings.Contains(got, "Body") {
		t.Errorf("page text %q", got)
	}
}

func TestAnchorPage_LeftFirstPage(t *testing.T) {
	for usage, want := range map[string]int{"left": 2, "all": 3} {
		pl := `<style:page-layout style:name="pm1" style:page-usage="` + usage + `"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" fo:margin-top="2cm" fo:margin-bottom="2cm" fo:margin-left="2cm" fo:margin-right="2cm"/></style:page-layout>`
		masters := `<style:master-page style:name="Standard" style:page-layout-name="pm1"/>`
		body := `<text:p>a</text:p><draw:frame text:anchor-type="page" text:anchor-page-number="3" svg:x="2cm" svg:y="2cm" svg:width="5cm" svg:height="2cm"><draw:text-box><text:p>Frame</text:p></draw:text-box></draw:frame>`
		b := newTestBuilder(t, doc{autoStyles: pl, masters: masters, body: body})
		root, _ := b.document(odfBody(b))
		got := 0
		walkNodes(root, func(n *layout.Node) {
			if n.Style.PageAnchor {
				got = n.Style.AnchorPage
			}
		})
		if got != want {
			t.Errorf("%s: anchor page %d, want %d", usage, got, want)
		}
	}
}

// TestConvert_ListsAreTagged locks that every LI sits in an L, a nested
// list's L inside its list's, each L numbered from its level's labels.
func TestConvert_ListsAreTagged(t *testing.T) {
	item := func(s string) string { return `<text:list-item><text:p>` + s + `</text:p></text:list-item>` }
	body := `<text:p>before</text:p><text:list text:style-name="LN">` + item("one") + `<text:list-item><text:p>two</text:p><text:list>` + item("sub a") + `</text:list></text:list-item></text:list>` +
		`<text:list text:style-name="LB">` + item("x") + `</text:list><text:p>after</text:p>`
	b := newTestBuilder(t, doc{styles: listStyles, body: body})
	root, _ := b.document(odfBody(b))
	var got []string
	walkNodes(root, func(n *layout.Node) {
		if n.Role == "L" {
			got = append(got, fmt.Sprintf("%d:%d", n.Numbering, len(n.Children)))
		}
	})
	want := []string{
		fmt.Sprintf("%d:3", compose.NumberingDecimal), fmt.Sprintf("%d:1", compose.NumberingLowerAlpha),
		fmt.Sprintf("%d:1", compose.NumberingDisc),
	}
	if !slices.Equal(got, want) {
		t.Errorf("lists (numbering:kids) %v, want %v", got, want)
	}
	res := convertDoc(t, doc{styles: listStyles, body: body}.pack(t), office.Options{})
	if r := roles(res.Structure); !strings.Contains(r, "Document,P,L,LI") || strings.Contains(r, "Document,LI") {
		t.Errorf("structure %s", r)
	}
}
