package models

import "fmt"

// StampPosition names where a stamp sits on each page it lands on: the
// center, the middle of an edge, or a corner. The zero value,
// [PositionNone], places a stamp by its X and Y.
//
// With any other position, the page is read as a viewer shows it: its crop box
// (see [RenderCropBox]) turned by its
// /Rotate, so a stamp reads the same way up on every page. The box bounding
// the stamp, once turned by its Rotation, sits against the position, and the
// stamp's X and Y then move it from there, right and up as viewed. A stamp at
// a corner or an edge stays on the page whatever its Rotation; a large X or Y
// moves it off.
//
// The box bounding a [github.com/gotenberg/litho/pkg/pdf.TextStamp] is the ink
// of its glyphs: their outlines in an embedded font, the glyph boxes of the
// Adobe Font Metrics for a standard font. Text in capitals centers on its
// capitals, not on the line's ascent and descent. The box of an
// [github.com/gotenberg/litho/pkg/pdf.ImageStamp] is its Width by Height, and
// the box of a [github.com/gotenberg/litho/pkg/pdf.PDFStamp] is its source
// page as viewed, times its Scale.
//
// Each page takes its own position, so a document mixing page sizes and
// orientations places the stamp on each one.
type StampPosition uint8

const (
	// PositionNone places the stamp by its X and Y, in points from the
	// page's bottom-left corner. It is the default.
	PositionNone StampPosition = iota
	// PositionCenter centers the stamp on the page.
	PositionCenter
	// PositionTopLeft puts the stamp in the page's top-left corner.
	PositionTopLeft
	// PositionTop centers the stamp along the page's top edge.
	PositionTop
	// PositionTopRight puts the stamp in the page's top-right corner.
	PositionTopRight
	// PositionLeft centers the stamp along the page's left edge.
	PositionLeft
	// PositionRight centers the stamp along the page's right edge.
	PositionRight
	// PositionBottomLeft puts the stamp in the page's bottom-left corner.
	PositionBottomLeft
	// PositionBottom centers the stamp along the page's bottom edge.
	PositionBottom
	// PositionBottomRight puts the stamp in the page's bottom-right corner.
	PositionBottomRight
)

// stampPositionNames lists the name of each position, in order.
var stampPositionNames = [...]string{
	PositionNone:        "",
	PositionCenter:      "center",
	PositionTopLeft:     "top-left",
	PositionTop:         "top",
	PositionTopRight:    "top-right",
	PositionLeft:        "left",
	PositionRight:       "right",
	PositionBottomLeft:  "bottom-left",
	PositionBottom:      "bottom",
	PositionBottomRight: "bottom-right",
}

// String returns the position's name, as
// [github.com/gotenberg/litho/pkg/pdf.ParseStampPosition] reads it: "" for
// [PositionNone], "center", "top-left", "top", "top-right", "left", "right",
// "bottom-left", "bottom" or "bottom-right". A value outside them reads as
// "StampPosition(n)".
func (p StampPosition) String() string {
	if int(p) < len(stampPositionNames) {
		return stampPositionNames[p]
	}
	return fmt.Sprintf("StampPosition(%d)", p)
}
