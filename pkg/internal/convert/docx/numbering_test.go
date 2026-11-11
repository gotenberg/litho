package docx

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

func TestFormatNumber(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		n      int
		format string
		want   string
	}{
		{3, "decimal", "3"},
		{7, "decimalZero", "07"},
		{14, "decimalZero", "14"},
		{1994, "upperRoman", "MCMXCIV"},
		{4, "lowerRoman", "iv"},
		{28, "upperLetter", "BB"},
		{3, "lowerLetter", "c"},
		{22, "ordinal", "22nd"},
		{13, "ordinal", "13th"},
		{42, "cardinalText", "Forty-two"},
		{21, "ordinalText", "Twenty-first"},
		{12, "ordinalText", "Twelfth"},
		{3, "decimalEnclosedCircle", "③"},
		{15, "chineseCounting", "十五"},
		{12, "decimalFullWidth", "12"},
		{2, "russianUpper", "Б"},
		{5, "none", ""},
		{5, "someUnknownFormat", "5"},
	} {
		if got := formatNumber(tc.n, tc.format); got != tc.want {
			t.Errorf("formatNumber(%d, %s) = %q, want %q", tc.n, tc.format, got, tc.want)
		}
	}
}

// listNumbering is a numbering part: abstract 0 with three levels (a
// legal third level), abstract 1 a Symbol bullet, abstract 2 linked by
// style; num 1 and 2 share abstract 0, num 2 restarting it at 5.
const listNumbering = `<w:numbering ` + nsDecl + `>` +
	`<w:abstractNum w:abstractNumId="0">` +
	`<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl>` +
	`<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="lowerLetter"/><w:lvlText w:val="%1.%2)"/><w:lvlJc w:val="right"/><w:pPr><w:ind w:left="1440" w:hanging="360"/></w:pPr></w:lvl>` +
	`<w:lvl w:ilvl="2"><w:start w:val="1"/><w:numFmt w:val="upperRoman"/><w:isLgl/><w:lvlText w:val="%1.%2.%3"/><w:suff w:val="space"/><w:pPr><w:ind w:left="2160" w:hanging="360"/></w:pPr></w:lvl>` +
	`</w:abstractNum>` +
	`<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="` + "" + `"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr><w:rPr><w:rFonts w:ascii="Symbol" w:hAnsi="Symbol"/></w:rPr></w:lvl></w:abstractNum>` +
	`<w:abstractNum w:abstractNumId="2"><w:numStyleLink w:val="ListStyle"/></w:abstractNum>` +
	`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>` +
	`<w:num w:numId="2"><w:abstractNumId w:val="0"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="5"/></w:lvlOverride></w:num>` +
	`<w:num w:numId="3"><w:abstractNumId w:val="1"/></w:num>` +
	`<w:num w:numId="4"><w:abstractNumId w:val="2"/></w:num>` +
	`</w:numbering>`

func TestNumbering_Labels(t *testing.T) {
	t.Parallel()
	styles := stylesPart(`<w:style w:type="numbering" w:styleId="ListStyle"><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Numbered"><w:pPr><w:numPr><w:numId w:val="3"/></w:numPr></w:pPr></w:style>`)
	item := func(num, lvl int, text string) string {
		return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + string(rune('0'+lvl)) + `"/><w:numId w:val="` + string(rune('0'+num)) + `"/></w:numPr></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
	}
	body := item(1, 0, "one") + item(1, 1, "one-a") + item(1, 1, "one-b") + item(1, 2, "deep") + item(1, 0, "two") + item(1, 1, "two-a") +
		item(2, 0, "restart") + item(2, 0, "restart-next") + item(3, 0, "bullet") + item(4, 0, "linked") +
		`<w:p><w:pPr><w:pStyle w:val="Numbered"/></w:pPr><w:r><w:t>by style</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:pStyle w:val="Numbered"/><w:numPr><w:numId w:val="0"/></w:numPr></w:pPr><w:r><w:t>unnumbered</w:t></w:r></w:p>`
	b := build(t, body, map[string]string{"word/styles.xml": styles, "word/numbering.xml": listNumbering})
	items := role(b.root, "LI")
	var got []string
	for _, n := range items {
		got = append(got, textOf(n.Marker)+"|"+textOf(n))
	}
	want := []string{
		"1.|one", "1.a)|one-a", "1.b)|one-b", "1.2.1 |deep", "2.|two", "2.a)|two-a",
		"5.|restart", "6.|restart-next", "•|bullet", "7.|linked", "•|by style",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("labels:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if n := items[0]; n.Style.Display != css.ListItem || n.ListValue != 1 || n.Style.ListStylePosition != css.Outside ||
		n.Marker.Style.Width != css.Pts(18) || n.Style.Margin[css.Left] != css.Pts(36) || n.Style.TextIndent != css.Pts(0) {
		t.Errorf("first item: display %v value %d position %v marker width %v margin %v indent %v",
			n.Style.Display, n.ListValue, n.Style.ListStylePosition, n.Marker.Style.Width, n.Style.Margin[css.Left], n.Style.TextIndent)
	}
	if a := items[1].Marker.Style.TextAlign; a != css.AlignEnd {
		t.Errorf("right-justified level marker aligns %v", a)
	}
	if deep := items[3]; deep.Style.ListStylePosition != css.Inside {
		t.Errorf("space suffix marker is %v, want inside", deep.Style.ListStylePosition)
	}
	if fam := items[8].Marker.Style.FontFamilies; fam[0] == "Symbol" {
		t.Errorf("Symbol bullet keeps the Symbol family: %v", fam)
	}
	if len(role(b.root, "LI")) != len(want) {
		t.Errorf("numId 0 still numbers")
	}
	if lists := role(b.root, "L"); len(lists) != 1 || len(lists[0].Children) != len(want) {
		t.Errorf("list items not grouped in one L: %d groups", len(lists))
	} else if lists[0].Numbering != compose.NumberingDecimal {
		t.Errorf("list numbering %d, want decimal", lists[0].Numbering)
	}
	if items[8].Numbering != compose.NumberingDisc || items[1].Numbering != compose.NumberingLowerAlpha {
		t.Errorf("bullet numbering %d, letter %d", items[8].Numbering, items[1].Numbering)
	}
}

func TestNumbering_LabelWiderThanHangPushesTheText(t *testing.T) {
	t.Parallel()
	numbering := `<w:numbering ` + nsDecl + `><w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/><w:pPr><w:ind w:left="480" w:hanging="480"/></w:pPr></w:lvl></w:abstractNum>` +
		`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>` +
		`<w:num w:numId="2"><w:abstractNumId w:val="0"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1000000"/></w:lvlOverride></w:num></w:numbering>`
	settings := `<w:settings ` + nsDecl + `><w:defaultTabStop w:val="480"/></w:settings>`
	for _, tc := range []struct {
		name   string
		num    int
		indent float64
	}{
		{"label fits", 1, 0},
		// LibreOffice measured (cjklist34.docx): the text goes to the
		// next default stop, 48 points from the margin.
		{"label too wide", 2, 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="` + strconv.Itoa(tc.num) + `"/></w:numPr></w:pPr><w:r><w:t>Item</w:t></w:r></w:p>`
			b := build(t, body, map[string]string{"word/numbering.xml": numbering, "word/settings.xml": settings})
			items := find(b.root, func(n *layout.Node) bool { return n.Marker != nil })
			if len(items) != 1 {
				t.Fatalf("%d items", len(items))
			}
			if got := items[0].Style.TextIndent.Value; math.Abs(got-tc.indent) > 0.01 {
				t.Errorf("text indent %.2f, want %.2f", got, tc.indent)
			}
		})
	}
}

func TestNumbering_SoftHyphenBulletPrints(t *testing.T) {
	t.Parallel()
	numbering := `<w:numbering ` + nsDecl + `><w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="` + "­" + `"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`
	body := `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Item</w:t></w:r></w:p>`
	items := find(build(t, body, map[string]string{"word/numbering.xml": numbering}).root, func(n *layout.Node) bool { return n.Marker != nil })
	if len(items) != 1 || textOf(items[0].Marker) != "-" {
		t.Fatalf("labels %d, %q", len(items), textOf(items[0].Marker))
	}
}

// TestRuns_SymbolActualText locks what a reader hears of a run in a symbol
// font: the characters its codes stand for, as its ActualText, while a run
// in a text font carries none.
func TestRuns_SymbolActualText(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:rPr><w:rFonts w:ascii="Symbol" w:hAnsi="Symbol"/></w:rPr><w:t>` + "a" + `</w:t></w:r><w:r><w:t>plain</w:t></w:r></w:p>`
	b := build(t, body, nil)
	runs := find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Text })
	if len(runs) != 2 {
		t.Fatalf("%d runs, want 2", len(runs))
	}
	if runs[0].ActualText != "•α" || runs[1].ActualText != "" {
		t.Errorf("actual texts %q and %q, want \"•α\" and none", runs[0].ActualText, runs[1].ActualText)
	}
}
