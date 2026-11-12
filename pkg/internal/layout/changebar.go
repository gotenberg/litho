package layout

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// changeBarGap is the distance in points from the left edge of the column
// a changed line sits in to its change bar (Node.ChangeBar), and
// changeBarWidth the bar's line width: 141 twips and LibreOffice's
// hairline.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 (gotenberg/gotenberg:9.4.1, changes shown)
//     strokes the change bar of a line in black at 78 pt on a page whose left
//     margin is 3 cm (85.04 pt) and at 49.65 pt with 2 cm, on odd and even
//     pages, beside a paragraph indented by 2 cm, a right-aligned one, a table
//     cell and a footnote alike; in the second of two columns 7.05 pt left of
//     that column. Its line width is the 0.1 pt the PDF stream opens with.
//   - probe, odt: LibreOffice 26.8 (gotenberg/gotenberg:9.4.1) draws a bar
//     for each line box, from its top to its bottom, 13.8 pt for a 12 pt
//     Liberation Serif line: consecutive changed lines join, the space
//     between two paragraphs stays blank.
//   - probe, docx: LibreOffice 26.8 (gotenberg/gotenberg:9.4.1) strokes the bar
//     of a DOCX document with a 1 in margin at 64.95 pt.
//   - probe, odt: LibreOffice 26.8 (gotenberg/gotenberg:9.4.1) draws no bar
//     beside the lines of a text frame.
const (
	changeBarGap   = 7.05
	changeBarWidth = 0.1
)

// changeBar paints the change bar of line l, when it has one: a hairline
// over the line box's height, [changeBarGap] left of the content edge of
// the column the line sits in, the multi-column container's first column
// or the flow's root, which painting moves to the line's column.
func (p *painter) changeBar(l *lineBox, page int) {
	if l.changed == nil || l.changed.hidden || l.h <= 0 {
		return
	}
	col := l.changed
	for col.parent != nil && col.mc == nil {
		if col.float != css.FloatNone || col.abs || col.inline && col.kind == kindBlock {
			// A frame's lines draw no bar.
			return
		}
		col = col.parent
	}
	x := col.contentX() - changeBarGap + p.dx
	y := l.y + p.dy
	path := &compose.Path{
		Ops: []compose.PathOp{
			{Kind: compose.MoveTo, P: [3]compose.Point{{X: x, Y: y}}},
			{Kind: compose.LineTo, P: [3]compose.Point{{X: x, Y: y + l.h}}},
		},
		Stroke: &compose.Stroke{Color: compose.Black, Width: changeBarWidth},
	}
	p.emit(l.changed, true, path, l.y, l.y+l.h, page)
}

// changeMarked reports a box of the inline formatting context of root
// inside an inline node marked Node.ChangeBar.
func changeMarked(b, root *box) bool {
	for ; b != nil && b != root; b = b.parent {
		if b.node != nil && b.node.ChangeBar {
			return true
		}
	}
	return false
}

// emptyChanged reports a paragraph of no character that holds an empty
// inline node marked Node.ChangeBar: its line draws a change bar.
func emptyChanged(p *ifcPara, root *box) bool {
	for _, o := range p.opens {
		if changeMarked(o.box, root) {
			return true
		}
	}
	return false
}
