package mutations

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/models"
)

// positionCount bounds the valid positions.
const positionCount = models.PositionBottomRight + 1

// ErrInvalidStampPosition is returned when a stamp names a position outside
// the ones [models.StampPosition] lists.
var ErrInvalidStampPosition = errors.New("invalid stamp position")

// checkStampPosition refuses a position outside the ones [models.StampPosition]
// lists.
func checkStampPosition(p models.StampPosition) error {
	if p >= positionCount {
		return fmt.Errorf("%w: %d", ErrInvalidStampPosition, p)
	}
	return nil
}

// anchor returns where p sits along each axis of the page: 0 at the left or
// bottom edge, 0.5 at the middle, 1 at the right or top edge.
func anchor(p models.StampPosition) (fx, fy float64) {
	switch p {
	case models.PositionTopLeft:
		return 0, 1
	case models.PositionTop:
		return 0.5, 1
	case models.PositionTopRight:
		return 1, 1
	case models.PositionLeft:
		return 0, 0.5
	case models.PositionRight:
		return 1, 0.5
	case models.PositionBottomLeft:
		return 0, 0
	case models.PositionBottom:
		return 0.5, 0
	case models.PositionBottomRight:
		return 1, 0
	case models.PositionCenter, models.PositionNone, positionCount:
	}
	// [models.PositionNone] and positions past [positionCount] never reach a
	// placement: [checkStampPosition] refuses the latter, and the former
	// places by X and Y.
	return 0.5, 0.5
}

// stampPlacement places a stamp against a [models.StampPosition] of each page
// it lands on. A builder writes the stamp's operators in a frame of its own,
// rotated about the origin, and the placement moves that frame onto each page
// (see [stampPlacement.matrix]).
type stampPlacement struct {
	position models.StampPosition
	// box is the stamp's extent in its own frame before rotation: llx, lly,
	// urx and ury, in points.
	box [4]float64
	// rotation is the stamp's counter-clockwise rotation in degrees about
	// the origin of its frame, as a viewer shows the page.
	rotation float64
	// dx and dy offset the stamp from its position, in points, to the right
	// and up as a viewer shows the page.
	dx, dy float64
}

// rotatedBounds returns the box that bounds the stamp's box once rotated:
// llx, lly, urx and ury in the stamp's frame.
func (p stampPlacement) rotatedBounds() [4]float64 {
	rad := p.rotation * math.Pi / 180
	c, s := detmath.Cos(rad), detmath.Sin(rad)
	out := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, corner := range [4][2]float64{
		{p.box[0], p.box[1]},
		{p.box[2], p.box[1]},
		{p.box[0], p.box[3]},
		{p.box[2], p.box[3]},
	} {
		x := float64(corner[0]*c) - float64(corner[1]*s)
		y := float64(corner[0]*s) + float64(corner[1]*c)
		out[0], out[1] = min(out[0], x), min(out[1], y)
		out[2], out[3] = max(out[2], x), max(out[3], y)
	}
	return out
}

// matrix returns the transform, six numbers of a cm operator, that moves the
// stamp's frame onto page: the box bounding the rotated stamp sits at the
// position in the region a viewer shows of the page (see
// [document.Page.ViewedBox]), turned by rotate, the page's /Rotate (ISO
// 32000-1, 7.7.3.3, Table 30), then moves by dx and dy. The frame's axes
// follow the page as viewed, so a stamp reads the same way up on every page
// whatever its /Rotate.
func (p stampPlacement) matrix(page *document.Page, rotate int) [6]float64 {
	box := normalizedBox(page.ViewedBox())
	llx, lly, urx, ury := box[0], box[1], box[2], box[3]
	rotate = normalizedRotate(rotate)
	w, h := urx-llx, ury-lly
	if rotate == 90 || rotate == 270 {
		w, h = h, w
	}

	fx, fy := anchor(p.position)
	b := p.rotatedBounds()
	tx := float64(fx*w) - (b[0] + float64(fx*(b[2]-b[0]))) + p.dx
	ty := float64(fy*h) - (b[1] + float64(fy*(b[3]-b[1]))) + p.dy

	// The inverse of [viewedPageMatrix]: from the page as viewed, its
	// lower-left corner at the origin, to user space.
	var m [6]float64
	switch rotate {
	case 90:
		m = [6]float64{0, 1, -1, 0, urx, lly}
	case 180:
		m = [6]float64{-1, 0, 0, -1, urx, ury}
	case 270:
		m = [6]float64{0, -1, 1, 0, llx, ury}
	default:
		m = [6]float64{1, 0, 0, 1, llx, lly}
	}
	m[4] += float64(tx*m[0]) + float64(ty*m[2])
	m[5] += float64(tx*m[1]) + float64(ty*m[3])
	return m
}

// placedOrigin returns where a stamp of width w and height h, drawn from its
// bottom-left corner, starts in its frame, and its placement: x and y with no
// placement for [models.PositionNone], the origin and a placement of the w by
// h box offset by x and y otherwise.
func placedOrigin(position models.StampPosition, x, y, w, h, rotation float64) (float64, float64, *stampPlacement) {
	if position == models.PositionNone {
		return x, y, nil
	}
	return 0, 0, &stampPlacement{
		position: position,
		box:      [4]float64{0, 0, w, h},
		rotation: rotation,
		dx:       x,
		dy:       y,
	}
}

// pageStampMatrix returns the cm operands that move a stamp's operators onto
// page, and whether they move them at all. A placed stamp moves by its
// [stampPlacement.matrix], under the page's /Rotate as alloc stages it (see
// [stagedRotate]). An unplaced one, drawn at absolute coordinates, moves only
// by the page's box origin (see [stampOriginShift]).
func pageStampMatrix(alloc *writer.ObjAllocator, page *document.Page, place *stampPlacement) (string, bool) {
	if place == nil {
		dx, dy, ok := stampOriginShift(page)
		if !ok {
			return "", false
		}
		return "1 0 0 1 " + FormatFloat(dx) + " " + FormatFloat(dy), true
	}
	m := place.matrix(page, stagedRotate(alloc, page))
	if m == [6]float64{1, 0, 0, 1, 0, 0} {
		return "", false
	}
	parts := make([]string, len(m))
	for i, v := range m {
		parts[i] = FormatFloat(v)
		if parts[i] == "-0" {
			parts[i] = "0"
		}
	}
	return strings.Join(parts, " "), true
}

// stagedRotate returns page's /Rotate as alloc stages it: the one an earlier
// edit of the same write states, such as [RotatePages], which writes the page
// dictionary again with its new /Rotate, and the page's own, inherited
// included (ISO 32000-1, 7.7.3.4), otherwise.
func stagedRotate(alloc *writer.ObjAllocator, page *document.Page) int {
	if rotate, ok := latestPageDict(alloc, page)["Rotate"].(ast.PdfInt); ok {
		return intconv.Clamp(rotate)
	}
	return page.Rotate
}
