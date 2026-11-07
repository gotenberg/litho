package xlsx

import (
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// paperSize is a paper's portrait width and height in inches or
// millimetres.
type paperSize struct {
	w, h float64
	mm   bool
}

// paperSizes maps the paperSize codes (ECMA-376 Part 1, 18.3.1.63, the
// table of paper sizes) onto the sizes LibreOffice gives them, which read
// codes 12 and 13 as JIS B4 and B5 and add the Windows codes 50 to 90
// (LibreOffice's msfilter PaperSizeConv). A code the table lacks takes
// Letter, the default paper of the en-US locale.
var paperSizes = map[int]paperSize{
	1: {8.5, 11, false}, 2: {8.5, 11, false}, 3: {11, 17, false}, 4: {17, 11, false},
	5: {8.5, 14, false}, 6: {5.5, 8.5, false}, 7: {7.25, 10.5, false}, 8: {297, 420, true},
	9: {210, 297, true}, 10: {210, 297, true}, 11: {148, 210, true}, 12: {257, 364, true},
	13: {182, 257, true}, 14: {8.5, 13, false}, 15: {215, 275, true}, 16: {10, 14, false},
	17: {11, 17, false}, 18: {8.5, 11, false}, 19: {3.875, 8.875, false}, 20: {4.125, 9.5, false},
	21: {4.5, 10.375, false}, 22: {4.75, 11, false}, 23: {5, 11.5, false}, 24: {17, 22, false},
	25: {22, 34, false}, 26: {34, 44, false}, 27: {110, 220, true}, 28: {162, 229, true},
	29: {324, 458, true}, 30: {229, 324, true}, 31: {114, 162, true}, 32: {114, 229, true},
	33: {250, 353, true}, 34: {176, 250, true}, 35: {176, 125, true}, 36: {110, 230, true},
	37: {3.875, 7.5, false}, 38: {3.625, 6.5, false}, 39: {14.875, 11, false}, 40: {8.5, 12, false},
	41: {8.5, 13, false}, 42: {250, 353, true}, 43: {200, 148, true}, 44: {9, 11, false},
	45: {10, 11, false}, 46: {15, 11, false}, 47: {220, 220, true},
	50: {9.5, 12, false}, 51: {9.5, 15, false}, 52: {11.69, 18, false}, 53: {235, 322, true},
	54: {8.5, 11, false}, 55: {210, 297, true}, 56: {9.5, 12, false}, 57: {227, 356, true},
	58: {305, 487, true}, 59: {8.5, 12.69, false}, 60: {210, 330, true}, 61: {148, 210, true},
	62: {182, 257, true}, 63: {322, 445, true}, 64: {174, 235, true}, 65: {201, 276, true},
	66: {420, 594, true}, 67: {297, 420, true}, 68: {322, 445, true}, 69: {200, 148, true},
	70: {105, 148, true}, 75: {11, 8.5, false}, 76: {420, 297, true}, 77: {297, 210, true},
	78: {210, 148, true}, 79: {364, 257, true}, 80: {257, 182, true}, 81: {148, 100, true},
	82: {148, 200, true}, 83: {148, 105, true}, 88: {128, 182, true}, 89: {182, 128, true},
	90: {12, 11, false},
}

// points returns the paper's size in points, through hundredths of a
// millimetre as LibreOffice stores it.
func (p paperSize) points() (w, h float64) {
	unit := 25.4
	if p.mm {
		unit = 1
	}
	return hmmPt(math.Round(p.w * unit * 100)), hmmPt(math.Round(p.h * unit * 100))
}

// hmmPt converts hundredths of a millimetre to points.
func hmmPt(v float64) float64 { return v * 72 / 2540 }

// The page margins of a sheet without pageMargins, in inches: the
// defaults of LibreOffice's import (ECMA-376 Part 1, 18.3.1.62 leaves
// them to the application).
//
// # Corpus evidence
//
//   - LibreOffice 26.8.2.2: a sheet without pageMargins prints its first
//     column's text at x 54.82 (0.748 inch and the cell's one-point
//     padding).
const (
	defaultMarginLR = 0.748
	defaultMarginTB = 0.984
	defaultMarginHF = 0.512
)

// defaultPage is the page setup of a sheet without pageSetup and
// pageMargins: Letter in portrait, the en-US paper LibreOffice falls back
// on, LibreOffice's default margins, a scale of 100, the pages numbered
// down then over and from the previous sheet's.
func defaultPage() sheet.PageSetup {
	w, h := paperSizes[1].points()
	p := sheet.PageSetup{Width: w, Height: h, Scale: 100}
	p.Margin[css.Left], p.Margin[css.Right] = marginPt(defaultMarginLR), marginPt(defaultMarginLR)
	p.Margin[css.Top], p.Margin[css.Bottom] = marginPt(defaultMarginTB), marginPt(defaultMarginTB)
	p.HeaderTop, p.FooterBottom = marginPt(defaultMarginHF), marginPt(defaultMarginHF)
	return p
}

// marginPt converts a margin in inches to points through the hundredths
// of a millimetre LibreOffice rounds it to.
func marginPt(in float64) float64 {
	if math.IsNaN(in) || in < 0 {
		return 0
	}
	return hmmPt(math.Round(min(in, maxMarginInches) * 2540))
}

// maxMarginInches caps a margin. Anti-DoS guard, not spec-mandated: a
// margin past the largest page side leaves nothing to print.
const maxMarginInches = 200

// The ranges LibreOffice clamps the print scale and the fit-to-pages
// counts to (ECMA-376 Part 1, 18.3.1.63 scale 10 to 400).
const (
	minScale   = 10
	maxScale   = 400
	maxFitPage = 1000
	maxFirstPg = 9999
)

// pageSetup reads the sheet's printOptions, pageMargins and pageSetup
// (ECMA-376 Part 1, 18.3.1.70, 18.3.1.62, 18.3.1.63) over the defaults.
func (w *sheetReader) pageSetup(root *ooxml.Element) {
	p := &w.sh.Page
	if po := root.Child(nsMain, "printOptions"); po != nil {
		p.GridLines = boolAttr(po, "gridLines", false)
		p.Headings = boolAttr(po, "headings", false)
		p.CenterHorizontally = boolAttr(po, "horizontalCentered", false)
		p.CenterVertically = boolAttr(po, "verticalCentered", false)
	}
	if pm := root.Child(nsMain, "pageMargins"); pm != nil {
		for _, m := range []struct {
			name string
			def  float64
			to   *float64
		}{
			{"left", defaultMarginLR, &p.Margin[css.Left]}, {"right", defaultMarginLR, &p.Margin[css.Right]},
			{"top", defaultMarginTB, &p.Margin[css.Top]}, {"bottom", defaultMarginTB, &p.Margin[css.Bottom]},
			{"header", defaultMarginHF, &p.HeaderTop}, {"footer", defaultMarginHF, &p.FooterBottom},
		} {
			v := m.def
			if f, err := strconv.ParseFloat(attr(pm, m.name), 64); err == nil {
				v = f
			}
			*m.to = marginPt(v)
		}
	}
	ps := root.Child(nsMain, "pageSetup")
	if w.fitToPage {
		p.Scale = 0
		p.FitWidth = max(0, min(intAttr(ps, "fitToWidth", 1), maxFitPage))
		p.FitHeight = max(0, min(intAttr(ps, "fitToHeight", 1), maxFitPage))
	} else if s := intAttr(ps, "scale", 100); s > 0 {
		p.Scale = max(minScale, min(s, maxScale))
	}
	if ps == nil {
		return
	}
	landscape := attr(ps, "orientation") == "landscape"
	if !boolAttr(ps, "usePrinterDefaults", false) {
		size, ok := paperSizes[intAttr(ps, "paperSize", 1)]
		if !ok {
			size = paperSizes[1]
		}
		pw, ph := size.points()
		if cw, ok := paperMeasure(attr(ps, "paperWidth")); ok {
			if chh, ok := paperMeasure(attr(ps, "paperHeight")); ok {
				pw, ph = cw, chh
			}
		}
		p.Width, p.Height = pw, ph
	}
	if landscape {
		p.Width, p.Height = p.Height, p.Width
	}
	if attr(ps, "pageOrder") == "overThenDown" {
		p.Order = sheet.OverThenDown
	}
	if boolAttr(ps, "useFirstPageNumber", false) {
		p.FirstPageNumber = max(0, min(intAttr(ps, "firstPageNumber", 1), maxFirstPg))
	}
}

// paperMeasure parses a paperWidth or paperHeight (ECMA-376 Part 1,
// 22.9.2.15 ST_PositiveUniversalMeasure, "210mm") into points, false for
// anything else or a size off the page range.
func paperMeasure(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, ok := ooxml.Measure(s, 0)
	if !ok || v <= 0 || v > office.MaxPageSide {
		return 0, false
	}
	return v, true
}
