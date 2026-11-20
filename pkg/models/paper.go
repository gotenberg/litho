package models

// Paper is a page size in points, 1/72 inch: 25.4 mm is 72 points.
// Concurrent reads of a value are safe; a write needs synchronization.
type Paper struct {
	// Width and Height are the sides in points. A paper wider than it is
	// high is a landscape paper.
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// The papers a conversion names most, each portrait. Their sides are the
// ones ISO 216 and ANSI/ASME Y14.1 give them, to a thousandth of a point.
// Any other size is a [Paper] literal.
var (
	// PaperA3 is ISO 216 A3, 297 by 420 mm.
	PaperA3 = Paper{Width: 841.890, Height: 1190.551}
	// PaperA4 is ISO 216 A4, 210 by 297 mm, the paper of a source that names
	// none unless [github.com/gotenberg/litho/pkg/pdf.WithDefaultPaper] names
	// another.
	PaperA4 = Paper{Width: 595.276, Height: 841.890}
	// PaperA5 is ISO 216 A5, 148 by 210 mm.
	PaperA5 = Paper{Width: 419.528, Height: 595.276}
	// PaperB5 is ISO 216 B5, 176 by 250 mm. The JIS B5 a spreadsheet often
	// names is larger, 182 by 257 mm.
	PaperB5 = Paper{Width: 498.898, Height: 708.661}
	// PaperLetter is North American Letter, 8.5 by 11 inches.
	PaperLetter = Paper{Width: 612, Height: 792}
	// PaperLegal is North American Legal, 8.5 by 14 inches.
	PaperLegal = Paper{Width: 612, Height: 1008}
	// PaperTabloid is North American Tabloid, 11 by 17 inches.
	PaperTabloid = Paper{Width: 792, Height: 1224}
	// PaperExecutive is North American Executive, 7.25 by 10.5 inches.
	PaperExecutive = Paper{Width: 522, Height: 756}
)

// Orientation is the way the pages of a conversion are turned.
type Orientation int

const (
	// OrientationSource keeps each page turned as its paper or its source
	// names it. It is the zero value and the default.
	OrientationSource Orientation = iota
	// OrientationPortrait turns every page that is wider than it is high.
	OrientationPortrait
	// OrientationLandscape turns every page that is higher than it is wide.
	OrientationLandscape
)
