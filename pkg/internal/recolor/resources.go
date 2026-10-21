package recolor

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
)

// rewriteResources converts what a resource dictionary defines: its colour
// spaces, patterns, shadings, form XObjects and Type 3 fonts. A device
// target removes the default space of its own family, which would otherwise
// reinterpret the target's colour written with the device operators (ISO
// 32000-1, 8.6.5.6), and an ICCBased target is added under the name content
// sets it by. Image XObjects stay: their samples are the images' recolouring.
// depth is the form nesting the resources are reached at.
func (s *session) rewriteResources(ctx context.Context, obj ast.PdfObject, depth int) (ast.PdfObject, bool, error) {
	if obj == nil {
		return nil, false, nil
	}
	if _, isNull := obj.(ast.PdfNull); isNull {
		return obj, false, nil
	}
	return s.edit(ctx, obj, "resources", func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		res, ok := resolved.(ast.PdfDict)
		if !ok {
			return nil, false, fmt.Errorf("%w: resources are %T", ErrUnsupportedSpace, resolved)
		}
		out := maps.Clone(res)
		changed := false
		set := func(key ast.PdfName, v ast.PdfObject, ch bool, err error) error {
			if err != nil {
				return err
			}
			if ch {
				out[key], changed = v, true
			}
			return nil
		}
		v, ch, err := s.rewriteColorSpaces(ctx, res["ColorSpace"])
		if err := set("ColorSpace", v, ch, err); err != nil {
			return nil, false, err
		}
		v, ch, err = s.rewriteEach(ctx, res["Pattern"], "patterns", func(o ast.PdfObject) (ast.PdfObject, bool, error) {
			return s.rewritePattern(ctx, o, depth)
		})
		if err := set("Pattern", v, ch, err); err != nil {
			return nil, false, err
		}
		v, ch, err = s.rewriteEach(ctx, res["Shading"], "shadings", func(o ast.PdfObject) (ast.PdfObject, bool, error) {
			return s.rewriteShading(ctx, o)
		})
		if err := set("Shading", v, ch, err); err != nil {
			return nil, false, err
		}
		v, ch, err = s.rewriteEach(ctx, res["XObject"], "XObjects", func(o ast.PdfObject) (ast.PdfObject, bool, error) {
			return s.rewriteForm(ctx, o, res, depth+1)
		})
		if err := set("XObject", v, ch, err); err != nil {
			return nil, false, err
		}
		v, ch, err = s.rewriteEach(ctx, res["Font"], "fonts", func(o ast.PdfObject) (ast.PdfObject, bool, error) {
			return s.rewriteType3(ctx, o, res, depth+1)
		})
		if err := set("Font", v, ch, err); err != nil {
			return nil, false, err
		}
		return out, changed, nil
	})
}

// rewriteEach applies f to each entry of a resource category dictionary, in
// key order.
func (s *session) rewriteEach(ctx context.Context, obj ast.PdfObject, what string, f func(ast.PdfObject) (ast.PdfObject, bool, error)) (ast.PdfObject, bool, error) {
	if obj == nil {
		return nil, false, nil
	}
	return s.edit(ctx, obj, what, func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		d, ok := resolved.(ast.PdfDict)
		if !ok {
			return nil, false, nil
		}
		var out ast.PdfDict
		for _, k := range sortedKeys(d) {
			v, ch, err := f(d[k])
			if err != nil {
				return nil, false, err
			}
			if ch {
				if out == nil {
					out = maps.Clone(d)
				}
				out[k] = v
			}
		}
		return out, out != nil, nil
	})
}

// rewriteColorSpaces rewrites the definitions of a /ColorSpace resource
// dictionary that recolouring keeps (Indexed, spot, Pattern spaces), removes
// the default space the target's device family would read through, and adds
// an ICCBased target.
func (s *session) rewriteColorSpaces(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, bool, error) {
	if obj == nil {
		if s.t.device() {
			return nil, false, nil
		}
		return ast.PdfDict{s.targetName(nil): s.t.ColorSpace}, true, nil
	}
	return s.edit(ctx, obj, "colour spaces", func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		d, ok := resolved.(ast.PdfDict)
		if !ok {
			return nil, false, nil
		}
		out := maps.Clone(d)
		changed := false
		if s.t.device() {
			def := ast.PdfName("Default" + map[string]string{
				graphics.FamilyDeviceGray: "Gray", graphics.FamilyDeviceRGB: "RGB", graphics.FamilyDeviceCMYK: "CMYK",
			}[s.t.Family])
			if _, ok := out[def]; ok {
				delete(out, def)
				changed = true
			}
		} else if name := s.targetName(d); out[name] == nil {
			out[name], changed = s.t.ColorSpace, true
		}
		for _, k := range sortedKeys(d) {
			if _, kept := out[k]; !kept {
				continue
			}
			v, ch, err := s.rewriteDefinition(ctx, d[k])
			if err != nil {
				return nil, false, err
			}
			if ch {
				out[k], changed = v, true
			}
		}
		return out, changed, nil
	})
}

// rewriteDefinition rewrites a colour space definition recolouring keeps: an
// Indexed space's palette, a spot colour's alternate, a Pattern space's base.
// A definition that converts is left, since the operators and objects
// selecting it are written in the target instead.
func (s *session) rewriteDefinition(ctx context.Context, def ast.PdfObject) (ast.PdfObject, bool, error) {
	return s.edit(ctx, def, "colour space", func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		p := s.planDefinition(ctx, resolved, nil)
		if p.kind == planUnknown {
			return nil, false, p.err
		}
		if (p.kind == planKeep || p.kind == planPattern) && p.def != nil {
			s.report.Spaces++
			return p.def, true, nil
		}
		return nil, false, nil
	})
}

// rewriteGroup writes a transparency group's blending space (ISO 32000-1,
// 11.4.7, Table 147) in the target, where every colour it composites now is.
func (s *session) rewriteGroup(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, bool, error) {
	if obj == nil {
		return nil, false, nil
	}
	return s.edit(ctx, obj, "group", func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		d, ok := resolved.(ast.PdfDict)
		if !ok || d["CS"] == nil {
			return nil, false, nil
		}
		p := s.planDefinition(ctx, d["CS"], nil)
		var cs ast.PdfObject
		switch {
		case p.kind == planConvert:
			cs = s.t.spaceObject()
		case p.kind == planKeep && p.def != nil:
			cs = p.def
		default:
			return nil, false, nil
		}
		out := maps.Clone(d)
		out["CS"] = cs
		return out, true, nil
	})
}
