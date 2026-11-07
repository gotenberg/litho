package docx

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

func TestSections_PageBoxes(t *testing.T) {
	t.Parallel()
	sect := func(extra string) string {
		return `<w:p><w:pPr><w:sectPr>` + extra + `</w:sectPr></w:pPr><w:r><w:t>s</w:t></w:r></w:p>`
	}
	body := sect(`<w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="720" w:bottom="1440" w:left="720" w:header="360" w:footer="360" w:gutter="360"/>`) +
		sect(`<w:type w:val="continuous"/><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="2880" w:right="1440" w:bottom="1440" w:left="1440" w:header="360" w:footer="360"/><w:cols w:num="2" w:space="360" w:sep="1"/>`) +
		sect(`<w:type w:val="continuous"/><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/>`) +
		sect(`<w:type w:val="oddPage"/><w:pgBorders w:offsetFrom="page"><w:top w:val="single" w:sz="8" w:space="24"/><w:left w:val="single" w:sz="8" w:space="24"/><w:bottom w:val="single" w:sz="8" w:space="24"/><w:right w:val="single" w:sz="8" w:space="24"/></w:pgBorders>`) +
		`<w:sectPr/>`
	b := build(t, body, nil)
	if len(b.secs) != 5 {
		t.Fatalf("%d sections", len(b.secs))
	}
	if b.secs[1].name != b.secs[0].name || b.secs[2].name == b.secs[1].name {
		t.Errorf("page names %q %q %q: a continuous break keeps the page only with the same size", b.secs[0].name, b.secs[1].name, b.secs[2].name)
	}
	pg := b.d.newPages(b.secs)
	box := pg.box(layout.PageInfo{Name: b.secs[0].name})
	if abs(box.Width-595.3) > 0.1 || box.Margin != [4]float64{72, 36, 72, 54} {
		t.Errorf("first box %+v", box)
	}
	// The separator is two inches of a 505.3-point text area.
	if r := box.FootnoteRule; r == nil || abs(r.Width-144/505.3) > 1e-3 || r.Thickness != 0.75 || r.Line != css.Solid || r.Align != css.AlignLeft || r.Color.A != 1 || r.SpaceAbove != 8.1 || r.SpaceBelow != 4.95 {
		t.Errorf("footnote rule %+v", box.FootnoteRule)
	}
	if land := pg.box(layout.PageInfo{Name: b.secs[2].name}); land.Width < land.Height {
		t.Errorf("landscape box %v x %v", land.Width, land.Height)
	}
	if f := pg.box(layout.PageInfo{Name: b.secs[3].name}).Frame; f == nil || f.Inset != [4]float64{24, 24, 24, 24} || f.Style.Border[css.Top].Width != 1 {
		t.Errorf("page frame %+v", f)
	}
	cols := b.root.Children[1].Style
	if cols.Columns.Count != 2 || cols.ColumnGap != css.Pts(18) || cols.Columns.RuleStyle != css.Solid {
		t.Errorf("columns %+v gap %v", cols.Columns, cols.ColumnGap)
	}
	if b.root.Children[3].Style.BreakBefore != css.RightPage {
		t.Errorf("odd page section breaks %v", b.root.Children[3].Style.BreakBefore)
	}
}

func TestSections_EmptySectionParagraphDropped(t *testing.T) {
	t.Parallel()
	b := build(t, `<w:p><w:r><w:t>a</w:t></w:r></w:p><w:p><w:pPr><w:sectPr/></w:pPr></w:p><w:p><w:r><w:t>b</w:t></w:r></w:p><w:sectPr/>`, nil)
	if n := len(role(b.root, "P")); n != 2 {
		t.Errorf("%d paragraphs, want 2", n)
	}
}

func TestHeadersFooters_Fields(t *testing.T) {
	t.Parallel()
	fld := func(instr string) string {
		return `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> ` + instr + ` </w:instrText></w:r>` +
			`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>99</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
	}
	hdr := func(tag, inner string) string {
		return `<w:` + tag + ` ` + nsDecl + `><w:p>` + inner + `</w:p></w:` + tag + `>`
	}
	extra := map[string]string{
		"word/header1.xml": hdr("hdr", `<w:r><w:t>Head</w:t></w:r>`),
		"word/header2.xml": hdr("hdr", `<w:r><w:t>First</w:t></w:r>`),
		"word/footer1.xml": hdr("ftr", `<w:r><w:t xml:space="preserve">P </w:t></w:r>`+fld("PAGE")+`<w:r><w:t xml:space="preserve"> of </w:t></w:r>`+fld("NUMPAGES")+
			`<w:r><w:t xml:space="preserve"> s</w:t></w:r>`+fld("SECTIONPAGES")+`<w:fldSimple w:instr="PAGE \* ROMAN"><w:r><w:t>X</w:t></w:r></w:fldSimple>`),
	}
	page := `<w:p><w:r><w:t>body</w:t></w:r><w:r><w:br w:type="page"/></w:r><w:r><w:t>more</w:t></w:r></w:p>`
	body := page + `<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rIdheader1"/><w:footerReference w:type="default" r:id="rIdfooter1"/></w:sectPr></w:pPr></w:p>` +
		page + `<w:sectPr><w:headerReference w:type="first" r:id="rIdheader2"/><w:pgNumType w:start="1" w:fmt="lowerRoman"/><w:titlePg/></w:sectPr>`
	doc := convertBytes(t, docx(t, body, extra), office.Options{})
	if len(doc.Pages) != 4 {
		t.Fatalf("%d pages: %q", len(doc.Pages), docText(doc))
	}
	// PAGE and NUMPAGES take the section's format, SECTIONPAGES keeps
	// its cached result, the title page of the second section takes its
	// first header and no footer: no section defines a first one.
	want := [][]string{
		{"Head", "body", "P1of4s99I"},
		{"Head", "more", "P2of4s99II"},
		{"First", "body"},
		{"Head", "more", "Piiofivs99II"},
	}
	if got := pageText(doc.Pages[2]); strings.Contains(got, "of") {
		t.Errorf("title page %q shows the default footer", got)
	}
	squash := strings.NewReplacer(" ", "", "\n", "")
	for i, pg := range doc.Pages {
		got := squash.Replace(pageText(pg))
		for _, w := range want[i] {
			if !strings.Contains(got, w) {
				t.Errorf("page %d = %q, want %q in it", i+1, got, w)
			}
		}
		for _, f := range pg.Content {
			if tx, ok := f.(*compose.Text); ok && strings.HasPrefix(glyphText(tx), "Head") && tx.Parent != nil {
				t.Errorf("header text is not an artifact")
			}
		}
	}
}

// glyphText returns the text of a run.
func glyphText(tx *compose.Text) string {
	var b strings.Builder
	for _, g := range tx.Glyphs {
		b.WriteString(g.Text)
	}
	return b.String()
}

func TestHeaders_GrowTheMargin(t *testing.T) {
	t.Parallel()
	var lines strings.Builder
	for range 6 {
		lines.WriteString(`<w:p><w:r><w:t>header line</w:t></w:r></w:p>`)
	}
	extra := map[string]string{"word/header1.xml": `<w:hdr ` + nsDecl + `>` + lines.String() + `</w:hdr>`}
	body := `<w:p><w:r><w:t>x</w:t></w:r></w:p><w:sectPr><w:headerReference w:type="default" r:id="rIdheader1"/><w:pgMar w:top="720" w:bottom="720" w:left="720" w:right="720" w:header="360" w:footer="360"/></w:sectPr>`
	b := build(t, body, extra)
	pg := b.d.newPages(b.secs)
	box := pg.box(layout.PageInfo{Name: b.secs[0].name})
	if box.Margin[css.Top] <= 36 {
		t.Errorf("top margin %v not grown for a six-line header", box.Margin[css.Top])
	}
}

func TestSections_ColumnBreaks(t *testing.T) {
	t.Parallel()
	body := func(cols string) string {
		return `<w:p><w:r><w:t>one</w:t></w:r><w:r><w:br w:type="column"/></w:r><w:r><w:t>two</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>three</w:t></w:r><w:r><w:br w:type="column"/></w:r></w:p><w:p><w:r><w:t>four</w:t></w:r></w:p>` +
			`<w:sectPr>` + cols + `</w:sectPr>`
	}
	for _, tc := range []struct {
		name string
		cols string
		want css.Break
	}{
		{"one column", ``, css.Page},
		{"two columns", `<w:cols w:num="2"/>`, css.Column},
	} {
		b := build(t, body(tc.cols), nil)
		for _, text := range []string{"two", "four"} {
			for _, p := range role(b.root, "P") {
				if textOf(p) == text && p.Style.BreakBefore != tc.want {
					t.Errorf("%s: %q breaks %v, want %v", tc.name, text, p.Style.BreakBefore, tc.want)
				}
			}
		}
		if n := len(role(b.root, "P")); n != 4 {
			t.Errorf("%s: %d paragraphs, want 4", tc.name, n)
		}
	}
}

func TestSections_StrayBreaksAndEmptyFrames(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:t>one</w:t></w:r></w:p><w:br w:type="page"/><w:p><w:r><w:t>two</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:framePr/></w:pPr><w:r><w:t>three</w:t></w:r></w:p>` +
		`<w:p><w:r><w:br w:type="page"/></w:r></w:p><w:p><w:r><w:br w:type="page"/></w:r></w:p><w:p><w:r><w:t>four</w:t></w:r></w:p>`
	b := build(t, body, nil)
	var got []string
	var breaks []css.Break
	for _, p := range role(b.root, "P") {
		got = append(got, textOf(p))
		breaks = append(breaks, p.Style.BreakBefore)
		if textOf(p) == "three" && p.Style.Float != css.FloatNone {
			t.Error("a frame without properties floats")
		}
	}
	want := []string{"one", "two", "three", "", "", "four"}
	wantBreaks := []css.Break{css.BreakAuto, css.Page, css.BreakAuto, css.BreakAuto, css.Page, css.Page}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("paragraphs %q, want %q", got, want)
	}
	for i := range wantBreaks {
		if breaks[i] != wantBreaks[i] {
			t.Errorf("paragraph %d %q breaks %v, want %v", i, got[i], breaks[i], wantBreaks[i])
		}
	}
}

func TestSections_UnequalColumns(t *testing.T) {
	t.Parallel()
	b := build(t, `<w:p><w:r><w:t>x</w:t></w:r></w:p><w:sectPr><w:cols w:num="2" w:equalWidth="0"><w:col w:w="2880" w:space="720"/><w:col w:w="5760"/><w:col w:w="1440"/></w:cols></w:sectPr>`, nil)
	s := b.secs[0]
	want := []columnTrack{{144, 36}, {288, 0}, {72, 0}}
	if s.cols != 3 || len(s.colTracks) != len(want) {
		t.Fatalf("%d columns, tracks %v", s.cols, s.colTracks)
	}
	for i := range want {
		if s.colTracks[i] != want[i] {
			t.Errorf("track %d = %v, want %v", i, s.colTracks[i], want[i])
		}
	}
	cols := find(b.root, func(n *layout.Node) bool { return n.Style != nil && len(n.Style.Columns.Tracks) > 0 })
	if len(cols) != 1 {
		t.Fatalf("%d blocks carry column tracks, want the section's", len(cols))
	}
	if got := cols[0].Style.Columns.Tracks; len(got) != 3 || got[0] != (css.ColumnTrack{Width: 144, Gap: 36}) || got[1] != (css.ColumnTrack{Width: 288}) || got[2] != (css.ColumnTrack{Width: 72}) {
		t.Errorf("engine tracks %+v, want the section's widths and spaces", got)
	}
}

func TestTextDirections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{{"btLr", "btLr"}, {"lrV", "btLr"}, {"tbRl", "tbRl"}, {"tbV", "tbRl"}} {
		if got := textDirection(tc.in); got != tc.want {
			t.Errorf("textDirection(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct{ in, want string }{{"", ""}, {"horz", ""}, {"vert270", "btLr"}, {"vert", "tbRl"}, {"eaVert", "tbRl"}} {
		if got := bodyVert(tc.in); got != tc.want {
			t.Errorf("bodyVert(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestHeadersFooters_UndefinedFirst checks which header and footer a
// title page shows when the section defines no first one, as LibreOffice
// prints it: none, unless the default reference comes before the
// section's first first-page reference.
func TestHeadersFooters_UndefinedFirst(t *testing.T) {
	t.Parallel()
	part := func(tag, text string) string {
		return `<w:` + tag + ` ` + nsDecl + `><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:` + tag + `>`
	}
	extra := map[string]string{
		"word/header1.xml": part("hdr", "DefaultHead"),
		"word/header2.xml": part("hdr", "FirstHead"),
		"word/footer1.xml": part("ftr", "DefaultFoot"),
	}
	const (
		dh = `<w:headerReference w:type="default" r:id="rIdheader1"/>`
		fh = `<w:headerReference w:type="first" r:id="rIdheader2"/>`
		df = `<w:footerReference w:type="default" r:id="rIdfooter1"/>`
	)
	for _, tc := range []struct {
		name, refs string
		want, not  []string
	}{
		{"no first references", dh + df, nil, []string{"DefaultHead", "DefaultFoot"}},
		{"first header, then the default footer", fh + df, []string{"FirstHead"}, []string{"DefaultFoot"}},
		{"default footer, then the first header", df + fh, []string{"FirstHead", "DefaultFoot"}, nil},
	} {
		body := `<w:p><w:r><w:t>one</w:t></w:r></w:p><w:sectPr>` + tc.refs + `<w:titlePg/></w:sectPr>`
		doc := convertBytes(t, docx(t, body, extra), office.Options{})
		got := pageText(doc.Pages[0])
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: page %q lacks %q", tc.name, got, w)
			}
		}
		for _, w := range tc.not {
			if strings.Contains(got, w) {
				t.Errorf("%s: page %q shows %q", tc.name, got, w)
			}
		}
	}
}
