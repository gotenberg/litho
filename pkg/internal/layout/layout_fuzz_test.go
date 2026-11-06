package layout

import (
	"context"
	"errors"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// treeGen derives a content tree and a page box from fuzz bytes, a byte at
// a time; past the end every byte reads as 0.
type treeGen struct {
	data  []byte
	i     int
	nodes int
}

func (g *treeGen) next() byte {
	if g.i >= len(g.data) {
		return 0
	}
	b := g.data[g.i]
	g.i++
	return b
}

// length returns a length in points from -8 to 120 px, or Auto.
func (g *treeGen) length(allowAuto bool) css.Dimension {
	b := g.next()
	switch {
	case allowAuto && b%7 == 0:
		return css.Dimension{Kind: css.Auto}
	case b%11 == 1:
		return css.Dimension{Kind: css.Percent, Value: float64(b%5) / 4}
	}
	return css.Pts(px(float64(int(b)%128 - 8)))
}

// colour returns one of a few colours, transparent included.
func (g *treeGen) colour() css.Color {
	return []css.Color{{}, red, blue, lime, black, {R: 1, G: 1, A: 0.5}}[g.next()%6]
}

var fuzzTexts = []string{"a", "ab cd", "  x\ty\nz  ", "abcdefghij klmnop", "אב ab", "á  b", "", "W"}

// gradient returns a gradient drawn from the bytes, hints and stops in
// any order.
func (g *treeGen) gradient() *css.Gradient {
	gr := &css.Gradient{
		Kind: css.GradientKind(g.next() % 2), Repeating: g.next()%2 == 0,
		Angle:  float64(g.next()) * 7,
		Corner: [2]int8{int8(g.next()%3) - 1, int8(g.next()%3) - 1},
		Circle: g.next()%2 == 0, Size: css.RadialSize(g.next() % 5),
		Radius: [2]css.Dimension{g.length(false), g.length(false)},
		Center: [2]css.Dimension{g.length(false), g.length(false)},
	}
	for range g.next() % 6 {
		gr.Stops = append(gr.Stops, css.GradientStop{Color: g.colour(), Position: g.length(true), Hint: g.next()%5 == 0})
	}
	return gr
}

// style returns a style drawn from the bytes.
func (g *treeGen) style() *css.Style {
	st := sty()
	b := g.next()
	displays := []css.Display{
		css.Block, css.Inline, css.InlineBlock, css.ListItem, css.Inline, css.Block, css.Table, css.TableCell, css.DisplayNone, css.InlineTable, css.TableColumn,
		css.TableRow, css.TableRowGroup, css.TableHeaderGroup, css.TableFooterGroup, css.TableColumnGroup, css.TableCaption, css.TableCell, css.TableRow,
		css.FlowRoot,
	}
	st.Display = displays[int(b)%len(displays)]
	if b&0x80 != 0 {
		st.Float = css.Float(g.next() % 3)
	}
	flags := g.next()
	if flags&1 != 0 {
		st.Width = g.length(true)
		st.Height = g.length(true)
	}
	if flags&2 != 0 {
		for s := range 4 {
			st.Margin[s] = g.length(true)
			st.Padding[s] = g.length(false)
		}
	}
	if flags&4 != 0 {
		bs := css.BorderStyle(g.next() % 10)
		w := px(float64(g.next() % 6))
		for s := range 4 {
			st.Border[s] = css.Border{Width: w, Style: bs, Color: g.colour()}
			st.BorderRadius[s] = css.Radius{H: g.length(false), V: g.length(false)}
		}
	}
	if flags&8 != 0 {
		st.BackgroundColor = g.colour()
		if g.next()%3 == 1 {
			st.BackgroundGradient = g.gradient()
		}
		if g.next()%3 == 0 {
			st.BackgroundImage = "img"
			st.BackgroundRepeat = [2]css.Repeat{css.Repeat(g.next() % 4), css.Repeat(g.next() % 4)}
			st.BackgroundSize = css.BackgroundSize{Kind: css.SizeKind(g.next() % 3), Width: g.length(true), Height: g.length(true)}
			st.BackgroundClip, st.BackgroundOrigin = css.BoxEdge(g.next()%3), css.BoxEdge(g.next()%3)
		}
	}
	if flags&16 != 0 {
		st.WhiteSpace = css.WhiteSpace(g.next() % 5)
		st.TextAlign = css.TextAlign(g.next() % 6)
		st.TextTransform = css.TextTransform(g.next() % 4)
		st.WordBreak = css.WordBreak(g.next() % 3)
		st.OverflowWrap = css.OverflowWrap(g.next() % 3)
		st.Direction = css.Direction(g.next() % 2)
		st.UnicodeBidi = css.UnicodeBidi(g.next() % 6)
		st.TextDecoration = css.TextDecoration(g.next() % 8)
		st.TextIndent = g.length(false)
		st.LetterSpacing = g.length(false)
	}
	if flags&32 != 0 {
		st.FontSize = px(float64(g.next() % 40))
		st.LineHeight = []css.Dimension{{Kind: css.Normal}, {Kind: css.Number, Value: 1.5}, css.Pts(px(3))}[g.next()%3]
		st.VerticalAlign = css.VerticalAlign{Kind: css.AlignKind(g.next() % 9), Length: g.length(false)}
	}
	if flags&64 != 0 {
		st.Overflow = css.Overflow(g.next() % 2)
		st.Position = css.Position(g.next() % 4)
		st.Top, st.Left = g.length(true), g.length(true)
		st.Opacity = float64(g.next()%5) / 4
		st.Clear = css.Clear(g.next() % 4)
		st.Visibility = css.Visibility(g.next() % 3)
		st.BoxSizing = css.BoxSizing(g.next() % 2)
		st.MinWidth, st.MaxWidth = g.length(false), g.length(false)
		if sf := g.next(); sf&3 != 0 {
			st.BoxShadow = []css.Shadow{{
				X: px(float64(int8(g.next()) / 8)), Y: px(float64(int8(g.next()) / 8)),
				Blur: px(float64(g.next() % 12)), Spread: px(float64(int8(g.next()) / 16)),
				Color: css.Color{R: 1, A: 1}, Inset: sf&4 != 0,
			}}
			st.Outline = css.Border{Width: px(float64(sf >> 4)), Style: css.BorderStyle(sf >> 3 % 10), Color: css.Color{G: 1, A: 1}}
			st.OutlineOffset = px(float64(int8(g.next()) / 16))
			st.ObjectFit = css.ObjectFit(g.next() % 5)
		}
		st.Bottom, st.Right = g.length(true), g.length(true)
		if z := g.next(); z%3 != 0 {
			st.ZIndex = css.ZIndex{Level: int(int8(z)) % 4}
		}
		if tf := g.next(); tf%4 != 0 {
			st.Transform = []css.TransformFunc{{
				Kind: css.TransformKind(tf % 5), X: g.length(false), Y: g.length(false),
				V: [6]float64{float64(int8(g.next())) / 8, float64(int8(g.next())) / 8, 1, 1, 3, 4},
			}}
			st.TransformOrigin = [2]css.Dimension{g.length(false), g.length(false)}
		}
	}
	if st.Display >= css.Table && st.Display <= css.TableCaption {
		tf := g.next()
		st.BorderCollapse = css.BorderCollapse(tf & 1)
		st.CaptionSide = css.CaptionSide(tf >> 1 & 1)
		st.EmptyCells = css.EmptyCells(tf >> 2 & 1)
		st.TableLayout = css.TableLayout(tf >> 3 & 1)
		if tf&16 != 0 {
			st.BorderSpacing = [2]float64{px(float64(g.next() % 5)), px(float64(g.next() % 5))}
		}
	}
	if flags&128 != 0 {
		st.BreakBefore, st.BreakAfter = css.Break(g.next()%6), css.Break(g.next()%6)
		st.BreakInside = css.BreakInside(g.next() % 3)
		st.Orphans, st.Widows = int(g.next()%4), int(g.next()%4)
		st.ListStyleType = css.ListStyleType(g.next() % 13)
		st.ListStylePosition = css.ListStylePosition(g.next() % 2)
		if g.next()%4 == 0 {
			st.ListStyleImage = "img"
		}
	}
	return st
}

// node returns a node drawn from the bytes, at most maxDepth deep.
func (g *treeGen) node(depth int) *Node {
	g.nodes++
	b := g.next()
	st := g.style()
	n := &Node{Style: st, ID: []string{"", "a", "b"}[b%3]}
	if b&0x10 != 0 {
		n.Role = []string{"P", "Span", "Figure", "LI", "Link", "H1"}[int(b>>5)%6]
	}
	if b&0x08 != 0 {
		n.Href = []string{"#a", "#b", "#zz", "https://x.test/"}[int(b>>1)%4]
	}
	switch kind := g.next() % 6; {
	case kind == 0 || depth > 7 || g.nodes > 120:
		n.Kind = Text
		n.Text = fuzzTexts[int(g.next())%len(fuzzTexts)]
	case kind == 1:
		n.Kind = LineBreak
	case kind == 2:
		n.Kind = Replaced
		if g.next()%3 != 0 {
			n.Picture = pic(color.NRGBA{B: 255, A: 255}, int(g.next()%9)+1, int(g.next()%9)+1)
		}
	default:
		n.Kind = Element
		for range int(g.next() % 5) {
			n.Children = append(n.Children, g.node(depth+1))
		}
		n.ListValue = int(int8(g.next()))
		if sp := g.next(); sp != 0 {
			n.ColSpan, n.RowSpan, n.Span = int(sp%4), int(sp>>2%4), int(sp>>4%4)
			if sp == 0xff {
				n.ColSpan, n.RowSpan, n.Span = 1<<30, -1, 1<<20
			}
		}
	}
	return n
}

func FuzzLayout(f *testing.F) {
	f.Add([]byte("seed: a few blocks of text"))
	f.Add([]byte{0x40, 0x10, 0x03, 0xff, 0x00, 0x13, 0x07, 0x81, 0x02, 0x7f, 0x33, 0x05, 0x04, 0x01, 0x22, 0x10, 0x91})
	f.Add([]byte{})
	f.Add([]byte{0x40, 0x40, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8f, 0x10, 0x20, 0x05, 0xf0, 0x03, 0x01, 0x02, 0x10, 0x22})
	f.Add([]byte{0x06, 0x31, 0x04, 0x01, 0x10, 0x04, 0x0b, 0x01, 0x03, 0x00, 0x11, 0x0c, 0x01, 0x03, 0x04, 0x11, 0x07, 0x05, 0x02, 0x41, 0x11, 0x22, 0x03, 0x00, 0x31, 0x9a, 0x00, 0x11, 0x0d, 0x01, 0x05, 0x42})
	// A table whose row of text lines breaks across short pages under a
	// repeated header.
	f.Add([]byte{0x20, 0x30, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x06, 0x00, 0x04, 0x03, 0x0d, 0x00, 0x03, 0x01, 0x07, 0x00, 0x03, 0x02, 0x0c, 0x00, 0x03, 0x01, 0x07, 0x80, 0x01, 0x04, 0x01, 0x07, 0x00, 0x00, 0x03})
	fonts := ahem(f)
	img := pic(color.NRGBA{R: 255, A: 255}, 3, 2)
	loader := func(context.Context, string) (*Picture, error) { return img, nil }
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			return
		}
		g := &treeGen{data: data}
		pb := PageBox{Width: px(float64(g.next()%200) + 8), Height: px(float64(g.next()%200) + 8)}
		for s := range 4 {
			pb.Margin[s] = px(float64(g.next() % 4))
		}
		if g.next()%4 == 0 {
			pb.Background = css.Initial()
			pb.Background.BackgroundGradient = g.gradient()
		}
		opts := Options{Page: pb, Image: loader, MaxPages: 40}
		if alt := g.next(); alt&1 != 0 {
			// Left and right pages of another size and margins.
			other := PageBox{Width: px(float64(g.next()%200) + 8), Height: px(float64(g.next()%200) + 8)}
			for s := range 4 {
				other.Margin[s] = px(float64(g.next() % 6))
			}
			opts.PageAt = func(i int) PageBox {
				if i%2 == 1 {
					return other
				}
				return pb
			}
		}
		root := g.node(0)
		res, err := Layout(context.Background(), root, fonts, opts)
		if err != nil {
			if errors.Is(err, ErrTooManyPages) || errors.Is(err, ErrInvalidOptions) {
				return
			}
			t.Fatalf("Layout: %v", err)
		}
		if err := document(res).Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})
}
