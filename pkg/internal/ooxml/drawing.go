package ooxml

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// PictureRef is a DrawingML picture as a pic:pic element or a shape's
// blip fill describes it (ECMA-376 Part 1, 20.2.2.5 pic:pic, 20.1.8.14
// a:blip, 20.1.8.55 a:srcRect, 20.1.7.6 a:xfrm).
type PictureRef struct {
	// Embed is the relationship ID of the embedded picture, Link of a
	// linked one; SVG is the relationship ID of the SVG rendition an
	// asvg:svgBlip extension names (MS-ODRAWXML, 2.3.7.1), which takes
	// precedence over the raster fallback Embed holds.
	Embed, Link, SVG string
	// Crop is the fraction of the picture cut off each side, indexed by
	// css.Side; negative values extend the picture with blank room.
	Crop [4]float64
	// Rotation is the clockwise rotation in degrees; FlipH and FlipV
	// mirror the picture.
	Rotation     float64
	FlipH, FlipV bool
	// Width and Height are the size of the shape's transform in points, 0
	// when absent.
	Width, Height float64
	// Name and Descr are the picture's non-visual name and description.
	Name, Descr string
}

// ParsePicture reads a pic:pic element, or any element with a blip fill
// and a shape transform (a wps:wsp, a p:pic).
func ParsePicture(e *Element) PictureRef {
	var r PictureRef
	if nv := e.Find(NSPic, "cNvPr"); nv != nil {
		r.Name, r.Descr = attrOr(nv, "name"), attrOr(nv, "descr")
	}
	fill := e.Find(NSPic, "blipFill")
	if fill == nil {
		fill = e.Find(NSA, "blipFill")
	}
	if blip := fill.Child(NSA, "blip"); blip != nil {
		r.Embed, _ = blip.AttrNS(NSR, "embed")
		r.Link, _ = blip.AttrNS(NSR, "link")
		if svg := blip.Find(NSASVG, "svgBlip"); svg != nil {
			r.SVG, _ = svg.AttrNS(NSR, "embed")
		}
	}
	if src := fill.Child(NSA, "srcRect"); src != nil {
		for side, name := range map[css.Side]string{css.Top: "t", css.Right: "r", css.Bottom: "b", css.Left: "l"} {
			if v, ok := Int(attrOr(src, name)); ok {
				// ST_Percentage in thousandths of a percent.
				r.Crop[side] = max(-10, min(0.99, float64(v)/100000))
			}
		}
	}
	if xfrm := e.Find(NSA, "xfrm"); xfrm != nil {
		r.Rotation, r.FlipH, r.FlipV = Transform2D(xfrm)
		if ext := xfrm.Child(NSA, "ext"); ext != nil {
			r.Width, _ = EMU(attrOr(ext, "cx"))
			r.Height, _ = EMU(attrOr(ext, "cy"))
		}
	}
	return r
}

// Transform2D returns the rotation in degrees and the flips of an a:xfrm
// (ECMA-376 Part 1, 20.1.7.6: rot in 60000ths of a degree).
func Transform2D(xfrm *Element) (rot float64, flipH, flipV bool) {
	if v, ok := Int(attrOr(xfrm, "rot")); ok {
		rot = math.Mod(float64(v)/60000, 360)
	}
	if v, ok := xfrm.Attr("flipH"); ok {
		flipH = OnOff(v, true)
	}
	if v, ok := xfrm.Attr("flipV"); ok {
		flipV = OnOff(v, true)
	}
	return rot, flipH, flipV
}
