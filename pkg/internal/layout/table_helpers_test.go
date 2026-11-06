package layout

import (
	"image/color"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func init() {
	// The WeasyPrint fixtures paint magenta and cyan too.
	palette['M'] = color.NRGBA{255, 0, 255, 255}
	palette['C'] = color.NRGBA{0, 255, 255, 255}
}

// part returns an element of display d whose style mods change, its
// children kids.
func part(d css.Display, mods []func(*css.Style), kids ...*Node) *Node {
	return el(sty(append([]func(*css.Style){display(d)}, mods...)...), kids...)
}

// mods is shorthand for a list of style modifiers.
func mods(m ...func(*css.Style)) []func(*css.Style) { return m }

// table, tr, tbody, thead, tfoot, caption, col and colgroup return
// elements of those displays.
func table(m []func(*css.Style), kids ...*Node) *Node   { return part(css.Table, m, kids...) }
func tr(m []func(*css.Style), kids ...*Node) *Node      { return part(css.TableRow, m, kids...) }
func tbody(m []func(*css.Style), kids ...*Node) *Node   { return part(css.TableRowGroup, m, kids...) }
func thead(m []func(*css.Style), kids ...*Node) *Node   { return part(css.TableHeaderGroup, m, kids...) }
func tfoot(m []func(*css.Style), kids ...*Node) *Node   { return part(css.TableFooterGroup, m, kids...) }
func caption(m []func(*css.Style), kids ...*Node) *Node { return part(css.TableCaption, m, kids...) }
func colgroup(m []func(*css.Style), kids ...*Node) *Node {
	return part(css.TableColumnGroup, m, kids...)
}
func col(m []func(*css.Style)) *Node { return part(css.TableColumn, m) }

// td returns a cell holding the text s, none when empty, its style the
// inherited one changed by m.
func td(m []func(*css.Style), s string) *Node {
	c := part(css.TableCell, m)
	if s != "" {
		c.Children = []*Node{txt(c.Style, s)}
	}
	return c
}

// spans sets the spans of the cell n.
func spans(n *Node, cols, rows int) *Node {
	n.ColSpan, n.RowSpan = cols, rows
	return n
}

// inherit applies the inherited properties a table passes down to its
// descendants in a cascade: the font and colour of the first modifiers.
func font(size float64) func(*css.Style) {
	return func(s *css.Style) { s.FontSize = px(size) }
}

func collapse(s *css.Style) { s.BorderCollapse = css.CollapseBorders }

func spacing(h, v float64) func(*css.Style) {
	return func(s *css.Style) { s.BorderSpacing = [2]float64{px(h), px(v)} }
}

func side(sd css.Side, w float64, bs css.BorderStyle, c css.Color) func(*css.Style) {
	return func(s *css.Style) { s.Border[sd] = css.Border{Width: px(w), Style: bs, Color: c} }
}

func valign(k css.AlignKind) func(*css.Style) {
	return func(s *css.Style) { s.VerticalAlign = css.VerticalAlign{Kind: k} }
}

func pct(v float64) css.Dimension { return css.Dimension{Kind: css.Percent, Value: v} }

// wpPage returns the page box of a WeasyPrint fixture: w by h px with a
// margin of m px.
func wpPage(w, h, m float64) PageBox {
	pb := page(w, h)
	pb.Margin = [4]float64{px(m), px(m), px(m), px(m)}
	return pb
}

// assertPages compares every page of res with the rows of want, the pages
// of h rows each one after the other as WeasyPrint's grids hold them, its
// letters mapped through tr first.
func assertPages(t *testing.T, res *Result, h int, want string, letters map[byte]byte) {
	t.Helper()
	var rows []string
	for _, l := range strings.Split(want, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			b := []byte(l)
			for i := range b {
				if r, ok := letters[b[i]]; ok {
					b[i] = r
				}
			}
			rows = append(rows, string(b))
		}
	}
	if len(rows)%h != 0 {
		t.Fatalf("%d rows, not pages of %d", len(rows), h)
	}
	n := len(rows) / h
	if len(res.Pages) != n {
		t.Errorf("%d pages, want %d", len(res.Pages), n)
	}
	for i := 0; i < n && i < len(res.Pages); i++ {
		assertPixels(t, res, i+1, strings.Join(rows[i*h:(i+1)*h], "\n"))
	}
}

// wpLetters maps the letters of WeasyPrint's colour map that stand for a
// colour of the palette under another letter.
var wpLetters = map[byte]byte{'r': 'R', 'a': 'B', 'Y': 'y'}

// deep applies m to the style of n and of every node below it, standing
// for the inherited properties a cascade would pass down.
func deep(n *Node, m ...func(*css.Style)) *Node {
	seen := map[*css.Style]bool{}
	var walk func(*Node)
	walk = func(n *Node) {
		if n.Style != nil && !seen[n.Style] {
			seen[n.Style] = true
			for _, f := range m {
				f(n.Style)
			}
		}
		for _, k := range n.Children {
			walk(k)
		}
	}
	walk(n)
	return n
}

func normalLH(s *css.Style) { s.LineHeight = css.Dimension{Kind: css.Normal} }

func fixedLayout(s *css.Style) { s.TableLayout = css.TableFixed }
