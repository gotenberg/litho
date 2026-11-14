package ooxml

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// VMLPath returns the geometry the VML shape sp spells out, its shape type
// typ (nil for none) giving what sp leaves unsaid (ECMA-376 Part 4,
// 14.1.2.20 shapetype: a shape takes the attributes and children of the
// type it names): its path attribute or v:path v (14.1.2.14), its
// v:formulas (14.1.2.6), its adj values position by position, its
// coordorigin and coordsize, the textboxrect and limo of its v:path.
// It reports false when neither has a path, or when the shape draws a
// text path (14.1.2.22 textpath on), whose path warps text and is not an
// outline.
func VMLPath(sp, typ *Element) (shape.VMLSource, bool) {
	var src shape.VMLSource
	for _, e := range []*Element{sp, typ} {
		if tp := e.Child(NSV, "textpath"); tp != nil {
			if v, ok := tp.Attr("on"); ok && vmlTrue(v) {
				return src, false
			}
		}
	}
	pathOf := func(e *Element) string {
		if v, ok := e.Attr("path"); ok {
			return v
		}
		v, _ := e.Child(NSV, "path").Attr("v")
		return v
	}
	src.Path = pathOf(sp)
	if strings.TrimSpace(src.Path) == "" {
		src.Path = pathOf(typ)
	}
	if strings.TrimSpace(src.Path) == "" {
		return src, false
	}
	fs := sp.Child(NSV, "formulas")
	if fs == nil {
		fs = typ.Child(NSV, "formulas")
	}
	for _, f := range fs.Kids() {
		if len(src.Formulas) >= shape.MaxVMLFormulas+1 {
			break
		}
		if f.Is(NSV, "f") {
			v, _ := f.Attr("eqn")
			src.Formulas = append(src.Formulas, v)
		}
	}
	for _, e := range []*Element{typ, sp} {
		v, ok := e.Attr("adj")
		if !ok {
			continue
		}
		for i, part := range strings.Split(v, ",") {
			if i > shape.MaxVMLAdjust {
				break
			}
			n, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
			if err != nil || !numeric.Finite(n) {
				continue
			}
			for len(src.Adjust) <= i {
				src.Adjust = append(src.Adjust, 0)
			}
			src.Adjust[i] = n
		}
	}
	pair := func(name string, def float64) (float64, float64) {
		if v, ok := sp.Attr(name); ok {
			return VMLPair(v, def)
		}
		v, _ := typ.Attr(name)
		return VMLPair(v, def)
	}
	src.OriginX, src.OriginY = pair("coordorigin", 0)
	src.SizeX, src.SizeY = pair("coordsize", 1000)
	vp := sp.Child(NSV, "path")
	tvp := typ.Child(NSV, "path")
	if v, ok := vp.Attr("textboxrect"); ok {
		src.TextBoxRect = v
	} else {
		src.TextBoxRect, _ = tvp.Attr("textboxrect")
	}
	limo, ok := vp.Attr("limo")
	if !ok {
		limo, _ = tvp.Attr("limo")
	}
	src.LimoX, src.LimoY = VMLPair(limo, 0)
	return src, true
}

// vmlTrue reads a VML boolean (Part 4, 14.1.2.19 ST_TrueFalse): t, true,
// on and 1 are true.
func vmlTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "t", "true", "on", "1":
		return true
	}
	return false
}
