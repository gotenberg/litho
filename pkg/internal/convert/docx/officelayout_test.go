package docx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

func TestSetWritingMode_TurnsCellsAndTextBoxes(t *testing.T) {
	t.Parallel()
	for dir, want := range map[string]css.WritingMode{"btLr": css.SidewaysLR, "tbRl": css.VerticalRL, "lrTb": css.HorizontalTB, "": css.HorizontalTB} {
		st := css.Initial()
		setWritingMode(st, dir)
		if st.WritingMode != want {
			t.Errorf("%q: writing mode %v, want %v", dir, st.WritingMode, want)
		}
	}
	body := `<w:tbl><w:tr><w:tc><w:tcPr><w:textDirection w:val="btLr"/></w:tcPr><w:p><w:r><w:t>up</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:tcPr><w:textDirection w:val="tbRlV"/></w:tcPr><w:p><w:r><w:t>down</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:p><w:r><w:t>flat</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	b := build(t, body, nil)
	cells := role(b.root, "TD")
	if len(cells) != 3 {
		t.Fatalf("%d cells, want 3", len(cells))
	}
	for i, want := range []css.WritingMode{css.SidewaysLR, css.VerticalRL, css.HorizontalTB} {
		if cells[i].Style.WritingMode != want {
			t.Errorf("cell %d writing mode %v, want %v", i, cells[i].Style.WritingMode, want)
		}
	}
	n := &layout.Node{Kind: layout.Element, Style: css.Initial()}
	turn(n, "btLr")
	turn(nil, "btLr")
	if n.Style.WritingMode != css.SidewaysLR {
		t.Errorf("text box writing mode %v, want sideways-lr", n.Style.WritingMode)
	}
}

func TestSetFrameWrap_MapsTheWrapElements(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		wrap, text string
		want       css.FrameWrap
	}{
		{"wrapSquare", "bothSides", css.FrameWrapParallel},
		{"wrapTight", "", css.FrameWrapParallel},
		{"wrapThrough", "left", css.FrameWrapLeft},
		{"wrapSquare", "right", css.FrameWrapRight},
		{"wrapSquare", "largest", css.FrameWrapLargest},
		{"wrapTopAndBottom", "", css.FrameWrapNone},
		{"wrapNone", "", css.FrameWrapThrough},
	} {
		st := css.Initial()
		setFrameWrap(st, tc.wrap, tc.text)
		if st.FrameWrap != tc.want {
			t.Errorf("%s %q: wrap %v, want %v", tc.wrap, tc.text, st.FrameWrap, tc.want)
		}
	}
	body := `<w:p><w:r><w:drawing><wp:anchor behindDoc="0" distT="12700" distR="25400" distB="38100" distL="50800">` +
		`<wp:positionH relativeFrom="page"><wp:posOffset>381000</wp:posOffset></wp:positionH><wp:positionV relativeFrom="page"><wp:posOffset>508000</wp:posOffset></wp:positionV>` +
		`<wp:extent cx="1270000" cy="635000"/><wp:wrapNone/><wp:docPr id="2" name="q"/>` + graphic("") + `</wp:anchor></w:drawing></w:r><w:r><w:t>text</w:t></w:r></w:p>`
	b := build(t, body, map[string]string{"word/media/a.png": pngData(t, 4, 2)})
	pics := find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(pics) != 1 {
		t.Fatalf("%d pictures", len(pics))
	}
	if st := pics[0].Style; !st.PageAnchor || st.FrameWrap != css.FrameWrapThrough || st.WrapGap != [4]float64{1, 2, 3, 4} {
		t.Errorf("page anchor %v wrap %v gap %v, want true, through, [1 2 3 4]", st.PageAnchor, st.FrameWrap, st.WrapGap)
	}
}

func TestSetChain_LinksTextBoxes(t *testing.T) {
	t.Parallel()
	box := func(txbx string) string {
		return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="635000"/><wp:docPr id="4" name="t"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
			`<wps:wsp><wps:spPr><a:prstGeom prst="rect"/><a:ln w="12700"><a:solidFill><a:srgbClr val="0000FF"/></a:solidFill></a:ln></wps:spPr>` + txbx + `<wps:bodyPr/></wps:wsp>` +
			`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	}
	body := box(`<wps:txbx id="3"><w:txbxContent><w:p><w:r><w:t>chained</w:t></w:r></w:p></w:txbxContent></wps:txbx>`) +
		box(`<wps:linkedTxbx id="3" seq="1"/>`) +
		box(`<wps:txbx><w:txbxContent><w:p><w:r><w:t>alone</w:t></w:r></w:p></w:txbxContent></wps:txbx>`)
	b := build(t, body, nil)
	boxes := find(b.root, func(n *layout.Node) bool { return n.Style.Display == css.InlineBlock && n.Kind == layout.Element })
	if len(boxes) != 3 {
		t.Fatalf("%d text boxes, want 3", len(boxes))
	}
	for i, want := range []struct {
		chain string
		index int
		kids  bool
		text  string
	}{{"3", 0, true, "chained"}, {"3", 1, false, ""}, {"", 0, true, "alone"}} {
		st := boxes[i].Style
		if st.Chain != want.chain || st.ChainIndex != want.index || (len(boxes[i].Children) > 0) != want.kids || textOf(boxes[i]) != want.text {
			t.Errorf("box %d: chain %q index %d text %q, want %q %d %q", i, st.Chain, st.ChainIndex, textOf(boxes[i]), want.chain, want.index, want.text)
		}
		if !st.StrokeBorders {
			t.Errorf("box %d: borders not stroked", i)
		}
		if want.chain != "" && (st.Height.Kind != css.Points || st.Height.Value != 50) {
			t.Errorf("box %d: height %v, want 50pt", i, st.Height)
		}
	}
}

func TestStrokeBorders_ParagraphsAndCells(t *testing.T) {
	t.Parallel()
	bdr := `<w:top w:val="single" w:sz="2" w:space="0" w:color="000000"/>`
	body := `<w:p><w:pPr><w:pBdr>` + bdr + `</w:pBdr></w:pPr><w:r><w:t>boxed</w:t></w:r></w:p>` +
		`<w:tbl><w:tblPr><w:tblBorders>` + bdr + `</w:tblBorders></w:tblPr><w:tr><w:tc><w:p><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	b := build(t, body, nil)
	for _, n := range append(role(b.root, "P")[:1], role(b.root, "TD")...) {
		if !n.Style.StrokeBorders {
			t.Errorf("%q: borders not stroked", textOf(n))
		}
	}
}
