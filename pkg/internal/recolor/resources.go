package recolor

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
)

// rewriteResources converts what a resource dictionary defines: its colour
// spaces, patterns, shadings, form XObjects and Type 3 fonts. A device
// target removes the default space of its own family, which would otherwise
// reinterpret the target's colour written with the device operators (ISO
// 32000-1, 8.6.5.6), and an ICCBased target is added under the name content
// sets it by. Image XObjects convert through the session's [Images], read
// against these resources as the source holds them. depth is the form
// nesting the resources are reached at.
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
			return s.rewriteXObject(ctx, o, res, depth+1)
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

// rewriteXObject converts an XObject res names: an image through the
// session's [Images], which writes it at its own number, a form through
// [session.rewriteForm]. An image it cannot convert is left and reported.
func (s *session) rewriteXObject(ctx context.Context, obj ast.PdfObject, res ast.PdfDict, depth int) (ast.PdfObject, bool, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if st, err := s.resolveStream(ctx, ref); err == nil {
			if sub, _ := s.resolveName(ctx, st.Dict["Subtype"]); sub == "Image" {
				if _, err := s.images.ConvertXObject(ctx, ref, res, nil); err != nil {
					if ctxErr := ctx.Err(); ctxErr != nil {
						return obj, false, ctxErr
					}
					s.skip(ref.ObjectNum, "image", err)
				}
				return obj, false, nil
			}
		}
	}
	return s.rewriteForm(ctx, obj, res, depth)
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
		// A definition is written once, where the resources define it, for
		// every painting naming it: under the option's black point
		// compensation, as the /Default state reads it.
		var p *spacePlan
		s.underBPC(s.opts.BlackPointCompensation, func() { p = s.planDefinition(ctx, resolved, nil) })
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
//
// A page group that names no space is given the target when the target is
// grey or CMYK (page set): Table 147 leaves such a page group's space to the
// reader, which composites in its device's space, RGB on screen, so a
// flattening or a print of the converted page would composite it outside the
// target. An RGB target is left unnamed, since that is the space readers
// composite in anyway. A form's group that names no space composites in its
// parent's (11.4.5), so it follows the page's. A page with no group is given
// none: recolouring does not tell whether its content composites, and a group
// on a page of a PDF/A-1 file that composites nothing would add the
// transparency group ISO 19005-1, 6.4 forbids. Such a page composites in the
// device of the output intent the conversion makes the target (14.11.5), or
// gets a group of its own when the caller asks ([Options.GroupPages]).
//
// A /CS that is a name but no family's names no space: outside a content
// stream a colour space is a family name or an array (ISO 32000-1, 8.6.3), and
// a name a /ColorSpace resource defines selects a space only through the cs
// and CS operators. The renderer blends such a group in sRGB, as one naming
// no space, and veraPDF reads it as no blending space; so it is treated as
// one naming none, and a page group's is replaced by the target.
//
// # Corpus evidence
//
//   - litho kidsrepair_1b.pdf, pagegroup_2b.pdf: the only files of the recolor
//     cell's sample (386) whose page group names no space, so the only
//     outputs a CMYK conversion changes; both still draw as their sources.
//   - litho pagegroup_2b.pdf: pages 6 and 7 name /CS0, the ICCBased space of
//     their /ColorSpace resources, which a flattening then composited in sRGB.
func (s *session) rewriteGroup(ctx context.Context, obj ast.PdfObject, page bool) (ast.PdfObject, bool, error) {
	if obj == nil {
		return nil, false, nil
	}
	return s.edit(ctx, obj, "group", func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		d, ok := resolved.(ast.PdfDict)
		if !ok {
			return nil, false, nil
		}
		if d["CS"] == nil || s.undefinedSpaceName(ctx, d["CS"]) {
			if !page || s.t.n == 3 || !s.transparencyGroup(ctx, d) {
				return nil, false, nil
			}
			out := maps.Clone(d)
			out["CS"] = s.t.spaceObject()
			return out, true, nil
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

// undefinedSpaceName reports whether cs, a colour space entry outside a
// content stream, is a name that names no colour space: only the families
// that take no parameters, DeviceGray, DeviceRGB, DeviceCMYK and Pattern, are
// written as a bare name there (ISO 32000-1, 8.6.3, Table 62).
func (s *session) undefinedSpaceName(ctx context.Context, cs ast.PdfObject) bool {
	v, err := s.resolve(ctx, cs)
	name, ok := v.(ast.PdfName)
	if err != nil || !ok {
		return false
	}
	switch name {
	case graphics.FamilyDeviceGray, graphics.FamilyDeviceRGB, graphics.FamilyDeviceCMYK, graphics.FamilyPattern:
		return false
	}
	return true
}

// pageGroup returns the page group a page whose dictionary is dict is given
// under [Options.GroupPages], nil when it gets none: one that names no group
// of its own, under a grey or CMYK target, whose own output intents do not
// characterise a device of the target's family.
//
// ISO 32000-1, 11.4.7 and Table 147 leave the blending space of a page with
// no group to the reader, which composites in its device's space, and
// 14.11.5 has the output intent name that device: a page the conversion
// leaves with no intent of the target's family would composite outside the
// target, in sRGB on screen and in a flattening. A page group that names the
// target fixes the space. A page with no /Group composites as a page group
// with the default attributes (11.4.7), isolated and non-knockout, so naming
// the space is all the group changes, and a page that composites nothing
// draws as before.
func (s *session) pageGroup(ctx context.Context, dict ast.PdfDict) ast.PdfObject {
	if !s.opts.GroupPages || s.t.n == 3 || dict["Group"] != nil {
		return nil
	}
	// ISO 32000-2, 14.11.5: a page's own output intents override the
	// document's.
	if pdfimage.OutputIntentsFamily(ctx, s.doc, dict["OutputIntents"]) == s.t.deviceFamily() {
		return nil
	}
	return ast.PdfDict{
		"Type": ast.PdfName("Group"),
		"S":    ast.PdfName("Transparency"),
		"CS":   s.t.spaceObject(),
	}
}

// transparencyGroup reports whether the group attributes dictionary d is a
// transparency group's, /S /Transparency, the only subtype ISO 32000-1,
// 8.10.3 defines and the only one /CS belongs to (Table 147).
func (s *session) transparencyGroup(ctx context.Context, d ast.PdfDict) bool {
	v, err := s.resolve(ctx, d["S"])
	name, ok := v.(ast.PdfName)
	return err == nil && ok && name == "Transparency"
}
