package odt

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// listStyles are a numbered list in label-alignment mode with a second
// level showing both levels, a bullet list followed by a space, and a
// numbered list in label-width-and-position mode.
const listStyles = `<text:list-style style:name="LN">` +
	`<text:list-level-style-number text:level="1" style:num-suffix="." style:num-format="1"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="listtab" text:list-tab-stop-position="1.27cm" fo:text-indent="-0.635cm" fo:margin-left="1.27cm"/></style:list-level-properties></text:list-level-style-number>` +
	`<text:list-level-style-number text:level="2" style:num-suffix=")" style:num-format="a" text:display-levels="2"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="listtab" text:list-tab-stop-position="1.905cm" fo:text-indent="-0.635cm" fo:margin-left="1.905cm"/></style:list-level-properties></text:list-level-style-number></text:list-style>` +
	`<text:list-style style:name="LB"><text:list-level-style-bullet text:level="1" text:bullet-char="•"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="space" fo:text-indent="-0.635cm" fo:margin-left="1.27cm"/></style:list-level-properties></text:list-level-style-bullet></text:list-style>` +
	`<text:list-style style:name="LR"><text:list-level-style-number text:level="1" style:num-suffix="." style:num-format="I"><style:list-level-properties text:space-before="0.5cm" text:min-label-width="1cm"/></text:list-level-style-number></text:list-style>` +
	`<text:list-style style:name="LS"><text:list-level-style-bullet text:level="1" style:num-prefix="(" style:num-suffix=")" text:bullet-char="•"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="space"/></style:list-level-properties></text:list-level-style-bullet></text:list-style>` +
	`<text:list-style style:name="LG"><text:list-level-style-number text:level="1" style:num-suffix="." style:num-format="A"/>` +
	`<text:list-level-style-number text:level="2" loext:is-legal="true" style:num-suffix="." style:num-format="i" text:display-levels="2"/>` +
	`<text:list-level-style-number text:level="3" loext:is-legal="true" style:num-suffix="." style:num-format="01, 02, 03, ..." text:display-levels="3"/></text:list-style>`

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
		{
			// Writer prints a bullet without its level's prefix and
			// suffix.
			"bullet without its suffix", `<text:list text:style-name="LS">` + item("x") + `</text:list>`,
			[]string{"• x"},
		},
		{
			// LibreOffice's loext:is-legal, Word's legal numbering
			// (ECMA-376 Part 1, 17.9.4 isLgl): Arabic numerals at every
			// level the label shows, zero-padded ones kept.
			"legal level", `<text:list text:style-name="LG">` + item("a") + `<text:list-item><text:p>b</text:p><text:list>` + item("b1") +
				`<text:list-item><text:p>b2</text:p><text:list>` + item("c") + `</text:list></text:list-item></text:list></text:list-item></text:list>`,
			[]string{"A.a", "B.b", "2.1.b1", "2.2.b2", "2.2.01.c"},
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

// TestConvert_ListLabelPlacement places the bullet at margin-left plus
// text-indent, 1.27 - 0.635 cm from the page area, a space after it.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice prints the bullet 18pt past the margin.
func TestConvert_ListLabelPlacement(t *testing.T) {
	d := doc{styles: listStyles, body: `<text:list text:style-name="LB"><text:list-item><text:p>Bullet</text:p></text:list-item></text:list>`}
	ls := lines(convertDoc(t, d.pack(t), office.Options{}).Pages[0])
	margin := 2 * 72 / 2.54
	if len(ls) != 1 || !near(ls[0].x, margin+0.635*72/2.54, 0.2) {
		t.Errorf("label line = %+v, want x %.2f", ls, margin+18)
	}
}

// TestConvert_ListLabelWiderThanItsGap starts a label wider than the
// room before the list tab stop at the first line's start, margin-left
// plus text-indent, and the text at the next default tab stop counted
// from the indent.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfexport/listformat.odt prints its label 18pt and
//     its text 71.5pt past the margin.
func TestConvert_ListLabelWiderThanItsGap(t *testing.T) {
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
		if n.Role == "TH" && n.Scope == typeset.ScopeColumn {
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
func roles(e *typeset.StructElem) string {
	var out []string
	var walk func(*typeset.StructElem)
	walk = func(e *typeset.StructElem) {
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

// TestConvert_PageFieldsPastTheLastPage locks the page fields of a
// footer that point past the last page (see [builder.pageField], which
// holds the measurements).
func TestConvert_PageFieldsPastTheLastPage(t *testing.T) {
	masters := `<style:master-page style:name="Standard" style:page-layout-name="pm1"><style:footer>` +
		`<text:p>A[<text:page-number text:select-page="current" text:page-adjust="1">9</text:page-number>]` +
		`B[<text:page-number text:select-page="next">9</text:page-number>]` +
		`C[<text:page-number text:select-page="previous">9</text:page-number>]` +
		`D[<text:page-continuation text:select-page="next" text:string-value="cont"/>]` +
		`E[<text:page-continuation text:select-page="previous" text:string-value="prev"/>]` +
		`G[<text:page-number text:page-adjust="5">9</text:page-number>]</text:p></style:footer></style:master-page>`
	auto := `<style:style style:name="PB" style:family="paragraph"><style:paragraph-properties fo:break-before="page"/></style:style>`
	body := `<text:p>One</text:p><text:p text:style-name="PB">Two</text:p><text:p text:style-name="PB">Three</text:p>`
	res := convertDoc(t, doc{masters: masters, contentAuto: auto, body: body}.pack(t), office.Options{})
	want := []string{
		"One\nA[2]B[2]C[0]D[cont]E[prev]G[]",
		"Two\nA[3]B[3]C[1]D[cont]E[prev]G[]",
		"Three\nA[]B[]C[2]D[]E[prev]G[]",
	}
	if len(res.Pages) != len(want) {
		t.Fatalf("pages = %d", len(res.Pages))
	}
	for i, w := range want {
		if got := pageText(res.Pages[i]); got != w {
			t.Errorf("page %d = %q, want %q", i+1, got, w)
		}
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
	// Writer links each citation to its note's number and back, each link
	// a Link element (ISO 14289-1, 7.18.1).
	var dests []int
	for _, p := range res.Pages {
		for _, l := range p.Links {
			if l.Dest != nil {
				dests = append(dests, l.Dest.Page)
			}
			if l.Parent == nil || l.Parent.Role != "Link" {
				t.Errorf("note link hangs from %+v", l.Parent)
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

// TestConvert_Spacing adds the margins of two paragraphs, drops them
// between paragraphs of contextual spacing, joins equal borders, and
// starts a page after a forced break with the next paragraph's top
// margin alone, as the first page starts with the first's.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice puts 13.8 pt of line plus 0.75 cm of
//     margins between two paragraphs of 0.25 cm and 0.5 cm margins, and
//     1 cm, not 2, above the paragraph after a forced break.
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
		{"margins add up", `<text:p text:style-name="M">a</text:p><text:p text:style-name="M">b</text:p>`, 13.8 + 0.75*cm},
		{"contextual spacing", `<text:p text:style-name="K">a</text:p><text:p text:style-name="K">b</text:p>`, 13.8},
		{"joined borders", `<text:p text:style-name="B">a</text:p><text:p text:style-name="B">b</text:p>`, 13.8},
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

// TestConvert_Frames lays out an inline picture and one that floats,
// the next paragraph's first line starting right of the floating one.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice starts the wrapped line 3 cm past the
//     margin.
func TestConvert_Frames(t *testing.T) {
	auto := `<style:style style:name="fr1" style:family="graphic"><style:graphic-properties style:wrap="right" style:horizontal-pos="left" style:horizontal-rel="paragraph"/></style:style>`
	long := strings.Repeat("words and more words ", 20)
	body := `<text:p>Inline <draw:frame text:anchor-type="as-char" svg:width="2cm" svg:height="1cm"><draw:image xlink:href="Pictures/a.png"/></draw:frame> picture.</text:p>` +
		`<text:p><draw:frame draw:style-name="fr1" text:anchor-type="paragraph" svg:width="3cm" svg:height="1.5cm"><draw:image xlink:href="Pictures/a.png"/></draw:frame>` + long + `</text:p>`
	res := convertDoc(t, doc{contentAuto: auto, body: body, pictures: map[string][]byte{"Pictures/a.png": tinyPNG(t)}}.pack(t), office.Options{})
	var imgs []typeset.Rect
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Image:
				imgs = append(imgs, v.Rect)
			case *typeset.Group:
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
	// picture.
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
	// A TOC, in a Sect of its own, holds its title as a Caption and its
	// entries as TOCI.
	if r := roles(res.Structure); !strings.Contains(r, "Sect,TOC,Caption,TOCI,") || strings.Contains(r, "TOC,P") {
		t.Errorf("structure %s", r)
	}
}

// TestConvert_IndentedEntryEndsAtTheMargin sets the page number of an
// entry of an indented level at the page area's right edge, on the
// entry's line, as for an entry of level 1.
//
// # Corpus evidence
//
//   - libreoffice/odt: layout/abi11870-2.odt: the entries of levels 2
//     and 3 end their page numbers where level 1's end.
func TestConvert_IndentedEntryEndsAtTheMargin(t *testing.T) {
	tmpl := func(level, style string) string {
		return `<text:table-of-content-entry-template text:outline-level="` + level + `" text:style-name="` + style + `"><text:index-entry-text/><text:index-entry-tab-stop style:type="right" style:leader-char="."/><text:index-entry-page-number/></text:table-of-content-entry-template>`
	}
	body := `<text:table-of-content text:name="T"><text:table-of-content-source text:outline-level="2"><text:index-title-template>Contents</text:index-title-template>` + tmpl("1", "C1") + tmpl("2", "C2") + `</text:table-of-content-source>` +
		`<text:index-body/></text:table-of-content>` +
		`<text:h text:outline-level="1">One</text:h><text:h text:outline-level="2">Deep</text:h>`
	auto := `<style:style style:name="C2" style:family="paragraph"><style:paragraph-properties fo:margin-left="3cm"/></style:style>`
	pg := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0]
	leader := regexp.MustCompile(`\.{2,}`)
	var got []string
	for _, l := range lines(pg)[:3] {
		got = append(got, leader.ReplaceAllString(strings.Join(strings.Fields(l.text), ""), "."))
	}
	if strings.Join(got, "|") != "Contents|One.1|Deep.1" {
		t.Fatalf("table = %q", got)
	}
	var ends []float64
	for _, r := range runs(pg) {
		if r.text == "1" {
			ends = append(ends, r.x)
		}
	}
	if len(ends) != 2 || !near(ends[0], ends[1], 0.01) {
		t.Errorf("page numbers at x %v, want one x", ends)
	}
}

// TestConvert_TextAlignStartEnd prints start and end by the writing
// mode in a document no LibreOffice wrote, start on the right of a
// right-to-left paragraph, and at the left and right margins in either
// writing mode in one LibreOffice wrote.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, right-to-left
//     paragraphs print start at the right margin and end at the left one
//     under no generator, start at the left margin and end at the right
//     one under LibreOffice 7.6; left-to-right ones start at the left
//     margin under both.
func TestConvert_TextAlignStartEnd(t *testing.T) {
	margin := 2 * 72 / 2.54
	const lo = `<meta:generator>LibreOffice/7.6.4.1$Linux_X86_64 LibreOffice_project/e19e193f88cd6c0525a17fb7a176ed8e6a3e2aa1</meta:generator>`
	for _, tc := range []struct {
		align, mode, meta string
		left              bool
	}{
		{"start", "lr-tb", "", true},
		{"end", "lr-tb", "", false},
		{"start", "rl-tb", "", false},
		{"end", "rl-tb", "", true},
		{"start", "lr-tb", lo, true},
		{"end", "lr-tb", lo, false},
		{"start", "rl-tb", lo, true},
		{"end", "rl-tb", lo, false},
	} {
		t.Run(tc.align+" "+tc.mode+" "+strconv.FormatBool(tc.meta != ""), func(t *testing.T) {
			auto := `<style:style style:name="A" style:family="paragraph"><style:paragraph-properties fo:text-align="` + tc.align + `" style:writing-mode="` + tc.mode + `"/></style:style>`
			rs := runs(convertDoc(t, doc{contentAuto: auto, meta: tc.meta, body: `<text:p text:style-name="A">xx</text:p>`}.pack(t), office.Options{}).Pages[0])
			if len(rs) == 0 {
				t.Fatal("no text")
			}
			if got := near(rs[0].x, margin, 0.5); got != tc.left {
				t.Errorf("text at x %.2f, at the left margin %v, want %v", rs[0].x, got, tc.left)
			}
		})
	}
}

// TestConvert_RightToLeftMargins checks that a right-to-left paragraph
// takes fo:margin-left on its right and fo:margin-right on its left, its
// first line indented on the right.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, a right-to-left
//     paragraph of fo:margin-left 50 mm, fo:margin-right 10 mm and
//     fo:text-indent 20 mm aligned left starts its lines 10 mm right of
//     the left margin and ends its first one 70 mm left of the right one.
func TestConvert_RightToLeftMargins(t *testing.T) {
	auto := `<style:style style:name="A" style:family="paragraph"><style:paragraph-properties fo:text-align="right" style:writing-mode="rl-tb" ` +
		`fo:margin-left="50mm" fo:margin-right="10mm" fo:text-indent="20mm"/></style:style>` +
		`<style:style style:name="B" style:family="paragraph"><style:paragraph-properties fo:text-align="left" style:writing-mode="rl-tb" ` +
		`fo:margin-left="50mm" fo:margin-right="10mm"/></style:style>`
	pg := convertDoc(t, doc{contentAuto: auto, body: `<text:p text:style-name="A">xx</text:p><text:p text:style-name="B">xx</text:p>`}.pack(t), office.Options{}).Pages[0]
	rs := runs(pg)
	if len(rs) != 2 {
		t.Fatalf("runs = %+v, want 2", rs)
	}
	mm := 72 / 25.4
	right := 210*mm - 20*mm
	if end := rs[0].x + measureXX(t, rs[0]); !near(end, right-70*mm, 0.5) {
		t.Errorf("first line ends at %.2f, want %.2f", end, right-70*mm)
	}
	if !near(rs[1].x, 30*mm, 0.5) {
		t.Errorf("left-aligned line starts at %.2f, want %.2f", rs[1].x, 30*mm)
	}
}

// measureXX returns the width of the run r of "xx" in Liberation Serif,
// from its glyphs' advance.
func measureXX(t *testing.T, r run) float64 {
	t.Helper()
	// Liberation Serif's x is 0.5 em wide.
	return 2 * 0.5 * r.size
}

// TestConvert_FrameCentredOnParagraph puts a frame anchored to its
// paragraph and centred on it in the middle of the content area, whether
// text runs through it or wraps beside it.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfexport/relh-page.odt prints its centred frame so.
//   - libreoffice/odt: odfimport/fdo90130-1.odt prints its centred frame so.
func TestConvert_FrameCentredOnParagraph(t *testing.T) {
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

// TestConvert_FirstPageFooterInLibreOfficeNamespace prints LibreOffice's
// own loext:footer-first on the first page, the footer on the others.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfimport/fdo79269.odt prints so.
func TestConvert_FirstPageFooterInLibreOfficeNamespace(t *testing.T) {
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

// TestConvert_ListHeaderHeadingTakesNoNumber prints a list header
// heading without a number, the next heading of its level taking 1.1.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfexport/tdf107696.odt prints so.
func TestConvert_ListHeaderHeadingTakesNoNumber(t *testing.T) {
	outline := `<text:outline-style style:name="Outline"><text:outline-level-style text:level="1" style:num-format="1"/><text:outline-level-style text:level="2" style:num-format="1" text:display-levels="2"/></text:outline-style>`
	body := `<text:h text:outline-level="1">One</text:h><text:h text:outline-level="2" text:is-list-header="true">Header</text:h><text:h text:outline-level="2">Two</text:h>`
	got := pageText(convertDoc(t, doc{styles: outline, body: body}.pack(t), office.Options{}).Pages[0])
	if got != "1One\nHeader\n1.1Two" {
		t.Errorf("headings = %q", got)
	}
}

// TestConvert_ListedHeadingTakesItsListsLabel labels a heading in a
// list item by its list alone, the outline counters left as they are.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, two level 1
//     headings in items of a list of format "[I]" print "[I]XX" and
//     "[II]YY", and the next level 1 heading prints "1.ZZ".
func TestConvert_ListedHeadingTakesItsListsLabel(t *testing.T) {
	styles := `<text:outline-style style:name="Outline"><text:outline-level-style text:level="1" style:num-suffix="." style:num-format="1"/></text:outline-style>` +
		`<text:list-style style:name="L1"><text:list-level-style-number text:level="1" style:num-prefix="[" style:num-suffix="]" style:num-format="I"/></text:list-style>`
	item := func(s string) string {
		return `<text:list-item><text:h text:outline-level="1">` + s + `</text:h></text:list-item>`
	}
	body := `<text:list text:style-name="L1">` + item("XX") + item("YY") + `</text:list><text:h text:outline-level="1">ZZ</text:h>`
	got := pageText(convertDoc(t, doc{styles: styles, body: body}.pack(t), office.Options{}).Pages[0])
	if got != "[I]XX\n[II]YY\n1.ZZ" {
		t.Errorf("headings = %q", got)
	}
}

// TestConvert_SkippedOutlineLevelStandsAtItsStart counts a level 1
// heading after level 2 ones from the start the level 2 heading gave its
// unnumbered level 1.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, headings of levels
//     2, 1, 1, 2 print "(a)", "2.", "3.", "(a)".
func TestConvert_SkippedOutlineLevelStandsAtItsStart(t *testing.T) {
	styles := `<text:outline-style style:name="Outline"><text:outline-level-style text:level="1" style:num-suffix="." style:num-format="1"/>` +
		`<text:outline-level-style text:level="2" style:num-prefix="(" style:num-suffix=")" style:num-format="a"/></text:outline-style>`
	h := func(level, s string) string {
		return `<text:h text:outline-level="` + level + `">` + s + `</text:h>`
	}
	body := h("2", "AA") + h("1", "BB") + h("1", "CC") + h("2", "DD")
	got := pageText(convertDoc(t, doc{styles: styles, body: body}.pack(t), office.Options{}).Pages[0])
	if got != "(a)AA\n2.BB\n3.CC\n(a)DD" {
		t.Errorf("headings = %q", got)
	}
}

// TestConvert_OutlineLevelIndentsItsHeadings places a heading's label
// and text by the indents and list tab stop of its outline level, numbered
// or not.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, outline levels of a
//     3 cm margin, a -1 cm text indent and a list tab stop at 4 cm print
//     an unnumbered heading 2 cm from the margin and a numbered one's
//     label at 2 cm, its text at 4 cm.
func TestConvert_OutlineLevelIndentsItsHeadings(t *testing.T) {
	level := func(n, format string) string {
		return `<text:outline-level-style text:level="` + n + `" style:num-format="` + format + `"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment">` +
			`<style:list-level-label-alignment text:label-followed-by="listtab" text:list-tab-stop-position="4cm" fo:text-indent="-1cm" fo:margin-left="3cm"/></style:list-level-properties></text:outline-level-style>`
	}
	styles := `<text:outline-style style:name="Outline">` + level("1", "") + level("2", "1") + `</text:outline-style>`
	body := `<text:h text:outline-level="1">One</text:h><text:h text:outline-level="2">Two</text:h>`
	rs := runs(convertDoc(t, doc{styles: styles, body: body}.pack(t), office.Options{}).Pages[0])
	cm := 72 / 2.54
	margin := 2 * cm
	want := map[string]float64{"One": margin + 2*cm, "1": margin + 2*cm, "Two": margin + 4*cm}
	seen := 0
	for _, r := range rs {
		if x, ok := want[strings.TrimSpace(r.text)]; ok {
			seen++
			if !near(r.x, x, 0.5) {
				t.Errorf("%q at x %.2f, want %.2f", r.text, r.x, x)
			}
		}
	}
	if seen != 3 {
		t.Errorf("runs %+v", rs)
	}
}

// TestConvert_ListHeadingOutlineTitle bookmarks a heading in a numbered
// list with its label first.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfimport/empty-svg-family-name.odt exports its
//     bookmarks so.
func TestConvert_ListHeadingOutlineTitle(t *testing.T) {
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
		fmt.Sprintf("%d:3", typeset.NumberingDecimal), fmt.Sprintf("%d:1", typeset.NumberingLowerAlpha),
		fmt.Sprintf("%d:1", typeset.NumberingDisc),
	}
	if !slices.Equal(got, want) {
		t.Errorf("lists (numbering:kids) %v, want %v", got, want)
	}
	res := convertDoc(t, doc{styles: listStyles, body: body}.pack(t), office.Options{})
	if r := roles(res.Structure); !strings.Contains(r, "Document,P,L,LI") || strings.Contains(r, "Document,LI") {
		t.Errorf("structure %s", r)
	}
}

// TestConvert_DrawingsDescribed locks what a reader hears of a drawing: a
// picture's svg:title, a text box's text as its Figure's alt, a shape's
// svg:desc, and nothing of an undescribed shape without text, an
// artifact.
func TestConvert_DrawingsDescribed(t *testing.T) {
	body := `<text:p><draw:frame text:anchor-type="as-char" svg:width="2cm" svg:height="1cm"><draw:image xlink:href="Pictures/a.png"/><svg:title>A logo</svg:title></draw:frame></text:p>` +
		`<text:p>x<draw:frame text:anchor-type="paragraph" svg:width="4cm" svg:height="2cm"><draw:text-box><text:p>In the</text:p><text:p>box</text:p></draw:text-box></draw:frame></text:p>` +
		`<text:p><draw:rect text:anchor-type="paragraph" svg:width="2cm" svg:height="1cm"><svg:desc>A red square</svg:desc></draw:rect>` +
		`<draw:line text:anchor-type="paragraph" svg:x1="0cm" svg:y1="0cm" svg:x2="2cm" svg:y2="0cm"/></text:p>`
	b := newTestBuilder(t, doc{body: body, pictures: map[string][]byte{"Pictures/a.png": tinyPNG(t)}})
	root, _ := b.document(odfBody(b))
	var got []string
	replaced := 0
	walkNodes(root, func(n *layout.Node) {
		if n.Role == "Figure" {
			got = append(got, n.Alt)
		}
		if n.Kind == layout.Replaced {
			replaced++
		}
	})
	want := []string{"A logo", "In the box", "A red square"}
	if !slices.Equal(got, want) || replaced != 3 {
		t.Errorf("figures %q (%d drawings), want %q (3)", got, replaced, want)
	}
}

// TestConvert_DecorativeAndSymbols locks two things a reader is told or
// spared: a picture whose style marks it decorative is an artifact, and a
// run in a symbol font carries the characters its codes stand for as its
// ActualText.
func TestConvert_DecorativeAndSymbols(t *testing.T) {
	auto := `<style:style style:name="fr" style:family="graphic"><style:graphic-properties loext:decorative="true"/></style:style>` +
		`<style:style style:name="sy" style:family="text"><style:text-properties fo:font-family="Symbol"/></style:style>`
	body := `<text:p><draw:frame draw:style-name="fr" text:anchor-type="as-char" svg:width="2cm" svg:height="1cm"><draw:image xlink:href="Pictures/a.png"/><svg:title>ignored</svg:title></draw:frame></text:p>` +
		`<text:p>x <text:span text:style-name="sy">` + "" + `</text:span></text:p>`
	b := newTestBuilder(t, doc{contentAuto: auto, body: body, pictures: map[string][]byte{"Pictures/a.png": tinyPNG(t)}})
	root, _ := b.document(odfBody(b))
	var actual []string
	walkNodes(root, func(n *layout.Node) {
		if n.Kind == layout.Replaced && (n.Role != "" || n.Alt != "") {
			t.Errorf("decorative picture: role %q alt %q", n.Role, n.Alt)
		}
		if n.ActualText != "" {
			actual = append(actual, n.ActualText)
		}
	})
	if !slices.Equal(actual, []string{"•"}) {
		t.Errorf("actual texts %q, want the Symbol bullet's", actual)
	}
}

// TestConvert_ListOfSuffixesIsUnordered locks the numbering of a list
// level that prints its suffix without a number: unordered, since its
// label is a Lbl, and none for a level that prints nothing.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfexport/tdf143605.odt prints "." before its item
//     from a level of an empty num-format; ListNumbering None failed
//     veraPDF's 8.2.5.25.
func TestConvert_ListOfSuffixesIsUnordered(t *testing.T) {
	styles := `<text:list-style style:name="LS"><text:list-level-style-number text:level="1" style:num-suffix="." style:num-format=""/></text:list-style>` +
		`<text:list-style style:name="LE"><text:list-level-style-number text:level="1" style:num-format=""/></text:list-style>`
	body := `<text:list text:style-name="LS"><text:list-item><text:p>a</text:p></text:list-item></text:list>` +
		`<text:list text:style-name="LE"><text:list-item><text:p>b</text:p></text:list-item></text:list>`
	b := newTestBuilder(t, doc{styles: styles, body: body})
	root, _ := b.document(odfBody(b))
	var got []typeset.ListNumbering
	walkNodes(root, func(n *layout.Node) {
		if n.Role == "L" {
			got = append(got, n.Numbering)
		}
	})
	if want := []typeset.ListNumbering{typeset.NumberingUnordered, typeset.NumberingNone}; !slices.Equal(got, want) {
		t.Errorf("numberings %v, want %v", got, want)
	}
}

// TestConvert_RightToLeftTable checks that a table of style:writing-mode
// rl-tb lays its first column out on the right.
//
// # Corpus evidence
//
//   - litho/odt: ar-rtl and he-bidi-numbers print the first column of
//     their tables of style:writing-mode rl-tb on the right.
func TestConvert_RightToLeftTable(t *testing.T) {
	auto := `<style:style style:name="T" style:family="table"><style:table-properties style:width="10cm" style:writing-mode="rl-tb"/></style:style>` +
		`<style:style style:name="C" style:family="table-column"><style:table-column-properties style:column-width="5cm"/></style:style>`
	body := `<table:table table:style-name="T"><table:table-column table:style-name="C" table:number-columns-repeated="2"/>` +
		`<table:table-row><table:table-cell><text:p>first</text:p></table:table-cell><table:table-cell><text:p>second</text:p></table:table-cell></table:table-row></table:table>`
	rs := runs(convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0])
	x := map[string]float64{}
	for _, r := range rs {
		x[r.text] = r.x
	}
	if x["first"] <= x["second"] {
		t.Errorf("first column at x %.2f, second at %.2f, want the first on the right", x["first"], x["second"])
	}
}

// TestConvert_RightToLeftListLabel checks that the label of a
// right-to-left list item sits right of its text, from the first line's
// start, 4 mm left of the right margin, its text ending on the list tab,
// the list's indent counted from the right.
//
// # Corpus evidence
//
//   - litho/odt: ar-rtl prints the labels of its right-to-left list of
//     fo:margin-left 10 mm, fo:text-indent -6 mm and a list tab at 10 mm
//     at 527.3 points, their text ending at 510.3.
func TestConvert_RightToLeftListLabel(t *testing.T) {
	auto := `<text:list-style style:name="L"><text:list-level-style-number text:level="1" style:num-format="1" style:num-suffix=".">` +
		`<style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="listtab" ` +
		`text:list-tab-stop-position="10mm" fo:text-indent="-6mm" fo:margin-left="10mm"/></style:list-level-properties></text:list-level-style-number></text:list-style>` +
		`<style:style style:name="R" style:family="paragraph" style:list-style-name="L"><style:paragraph-properties style:writing-mode="rl-tb"/></style:style>`
	body := `<text:list text:style-name="L"><text:list-item><text:p text:style-name="R">abc</text:p></text:list-item></text:list>`
	rs := runs(convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0])
	mm := 72 / 25.4
	right := 190 * mm
	var label, txt *run
	for i := range rs {
		switch {
		case strings.Contains(rs[i].text, "1"):
			label = &rs[i]
		case strings.Contains(rs[i].text, "abc"):
			txt = &rs[i]
		}
	}
	if label == nil || txt == nil {
		t.Fatalf("runs = %+v", rs)
	}
	if label.x <= txt.x || label.x < right-10*mm || label.x > right-4*mm {
		t.Errorf("label at x %.2f, text at %.2f, want the label right of the text, between %.2f and %.2f", label.x, txt.x, right-10*mm, right-4*mm)
	}
	// "abc" in Liberation Serif 12 is 16.65 points wide: its end sits on
	// the list tab, 10 mm from the right margin.
	if end := txt.x + 16.65; !near(end, right-10*mm, 0.5) {
		t.Errorf("text ends at %.2f, want %.2f", end, right-10*mm)
	}
}

// TestConvert_KashidaWarning checks that a justified paragraph of Arabic
// text warns that it is stretched at its spaces alone.
//
// # Corpus evidence
//
//   - litho/odt: ar-rtl prints the lines of its justified Arabic
//     paragraph with kashida glyphs inserted between joined letters.
func TestConvert_KashidaWarning(t *testing.T) {
	for _, tc := range []struct {
		align, text string
		want        bool
	}{
		{"justify", "لكل إنسان حق التمتع", true},
		{"start", "لكل إنسان حق التمتع", false},
		{"justify", "plain text", false},
	} {
		auto := `<style:style style:name="A" style:family="paragraph"><style:paragraph-properties fo:text-align="` + tc.align + `" style:writing-mode="rl-tb"/></style:style>`
		res := convertDoc(t, doc{contentAuto: auto, body: `<text:p text:style-name="A">` + tc.text + `</text:p>`}.pack(t), office.Options{})
		got := slices.ContainsFunc(res.Warnings, func(w typeset.Warning) bool { return strings.Contains(w.Message, "kashida") })
		if got != tc.want {
			t.Errorf("%s %q: kashida warning %v, want %v", tc.align, tc.text, got, tc.want)
		}
	}
}

// TestConvert_LineBreakOptions checks the line breaking a paragraph's
// style:line-break and style:punctuation-wrap give it and its runs:
// strict with hanging punctuation where it sets neither.
//
// # Corpus evidence
//
//   - litho/odt: ja-line-break: with neither attribute, Writer starts no
//     line with っ, ュ or ー and hangs 、 and 。 past the line's end;
//     under line-break normal, a line starts with each of them.
func TestConvert_LineBreakOptions(t *testing.T) {
	for _, tc := range []struct {
		props string
		lb    css.LineBreak
		hang  bool
	}{
		{"", css.LineBreakStrict, true},
		{`style:line-break="strict" style:punctuation-wrap="simple"`, css.LineBreakStrict, false},
		{`style:line-break="normal" style:punctuation-wrap="hanging"`, css.LineBreakLoose, true},
	} {
		auto := `<style:style style:name="A" style:family="paragraph"><style:paragraph-properties ` + tc.props + `/></style:style>`
		b := newTestBuilder(t, doc{contentAuto: auto, body: `<text:p text:style-name="A">漢字</text:p>`})
		root, _ := b.document(odfBody(b))
		var para, run *layout.Node
		walkNodes(root, func(n *layout.Node) {
			switch {
			case n.Role == "P" && para == nil:
				para = n
			case n.Kind == layout.Text && run == nil:
				run = n
			}
		})
		if para == nil || run == nil {
			t.Fatalf("%q: no paragraph or text", tc.props)
		}
		if para.Style.LineBreak != tc.lb || run.Style.LineBreak != tc.lb || para.Style.HangPunctuation != tc.hang {
			t.Errorf("%q: line-break %v, run %v, hang %v; want %v, %v", tc.props, para.Style.LineBreak, run.Style.LineBreak, para.Style.HangPunctuation, tc.lb, tc.hang)
		}
	}
}
