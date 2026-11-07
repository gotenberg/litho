package odt

import (
	"regexp"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
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
		{"numbers and nesting", `<text:list text:style-name="LN">` + item("one") + `<text:list-item><text:p>two</text:p><text:list>` + item("sub a") + item("sub b") + `</text:list></text:list-item>` + item("three") + `</text:list>`,
			[]string{"1.one", "2.two", "2.a)sub a", "2.b)sub b", "3.three"}},
		{"continue numbering", `<text:list text:style-name="LN">` + item("a") + `</text:list><text:list text:style-name="LN" text:continue-numbering="true">` + item("b") + `</text:list>`,
			[]string{"1.a", "2.b"}},
		{"new list restarts", `<text:list text:style-name="LN">` + item("a") + `</text:list><text:list text:style-name="LN">` + item("b") + `</text:list>`,
			[]string{"1.a", "1.b"}},
		{"continue-list by id", `<text:list xml:id="l1" text:style-name="LN">` + item("a") + `</text:list><text:list text:style-name="LB">` + item("x") + `</text:list><text:list text:style-name="LN" text:continue-list="l1">` + item("b") + `</text:list>`,
			[]string{"1.a", "• x", "2.b"}},
		{"start value", `<text:list text:style-name="LN"><text:list-item text:start-value="7"><text:p>s</text:p></text:list-item>` + item("t") + `</text:list>`,
			[]string{"7.s", "8.t"}},
		{"header unnumbered", `<text:list text:style-name="LN"><text:list-header><text:p>head</text:p></text:list-header>` + item("a") + `</text:list>`,
			[]string{"head", "1.a"}},
		{"roman label width mode", `<text:list text:style-name="LR">` + item("a") + item("b") + `</text:list>`,
			[]string{"I.a", "II.b"}},
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
	ranged := convertDoc(t, doc{autoStyles: pl, masters: masters, contentAuto: auto, body: body}.pack(t), office.Options{PageRanges: "2-3"})
	if len(ranged.Pages) != 2 || !strings.HasPrefix(pageText(ranged.Pages[0]), "HEAD 2 of 5") {
		t.Errorf("ranges: %d pages, first %q", len(ranged.Pages), pageText(ranged.Pages[0]))
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
