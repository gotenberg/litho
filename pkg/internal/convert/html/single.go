package html

import (
	"context"
	"math"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// The screen a single page is measured on, in points. Gotenberg's
// singlePage reads Page.getLayoutMetrics' cssContentSize before printing
// (gotenberg/pkg/modules/chromium/tasks.go, printToPdfActionFunc): the
// document laid out on the screen of headless Chromium, which no device
// metrics override sizes for a PDF. That screen is 800 CSS pixels wide;
// its viewport, the floor of the content size, is 437.12 CSS pixels tall,
// what the route's references show for a document shorter than it
// (single-page-landscape and page-1 under singlePage both print a page
// 327.84 points plus the margins tall).
const (
	// screenWidth is 800 CSS pixels.
	screenWidth = 600
	// screenHeight is 437.12 CSS pixels.
	screenHeight = 327.84
)

// measureSinglePage returns the width and height in points of the content
// of doc as Gotenberg's singlePage measures it: laid out in one continuous
// column [screenWidth] wide, without margins, page box or forced breaks,
// at the print media type the route emulates; the width at least
// [screenWidth] and the height at least [screenHeight], the screen's
// viewport, which the content size never falls below.
func measureSinglePage(ctx context.Context, doc *dom.Node, o Options, fonts *text.Fonts, lay layoutFunc) (float64, float64, error) {
	m := o
	m.Scale, m.MarginTop, m.MarginRight, m.MarginBottom, m.MarginLeft = 1, 0, 0, 0, 0
	p, err := prepareDoc(ctx, doc, m, fonts, printSetup{factor: 1, measure: true})
	if err != nil {
		return 0, 0, err
	}
	continuous(p.root)
	res, err := layOut(ctx, lay, p)
	if err != nil {
		return 0, 0, err
	}
	h := 0.0
	if len(res.Pages) > 0 {
		h = res.Pages[0].Height
	}
	return max(res.ContentWidth, screenWidth), max(h, screenHeight), nil
}

// continuous removes the forced breaks of the tree under root: a screen
// lays the document out as one column (CSS Fragmentation 3, 3.1, breaks
// apply in fragmented flows alone). Styles shared by several nodes stay
// shared; the walk keeps its own stack.
func continuous(root *layout.Node) {
	done := map[*css.Style]*css.Style{}
	stack := []*layout.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if st := n.Style; st != nil && (st.BreakBefore != css.BreakAuto || st.BreakAfter != css.BreakAuto) {
			c, ok := done[st]
			if !ok {
				cp := *st
				cp.BreakBefore, cp.BreakAfter = css.BreakAuto, css.BreakAuto
				c = &cp
				done[st] = c
			}
			n.Style = c
		}
		stack = append(stack, n.Children...)
		if n.Marker != nil {
			stack = append(stack, n.Marker)
		}
	}
}

// chromiumPaper returns the paper side v, in points, as printToPDF prints
// it: rounded up to whole points (print_to_pdf::GetPrintPagesParams,
// gfx::ToCeiledSize of the paper in points), then to the nearest dot at
// 300 dpi, the PDF's device resolution (printing::kDefaultPdfDpi). Every
// page size of the route's references is a whole number of such dots:
// 599.04 points is 2496 dots, 1087.92 is 4533. The route sizes a single
// page's paper in inches, so it lands there.
func chromiumPaper(v float64) float64 {
	const dots = 300.0 / 72
	return math.Round(math.Ceil(v-1e-9)*dots) / dots
}
