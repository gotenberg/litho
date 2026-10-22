package recolor

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// rewriteStreamContent converts the content of a form, pattern or glyph
// stream painted with the resources res and returns the stream to write in
// its place. It returns false when nothing in it converts.
func (s *session) rewriteStreamContent(ctx context.Context, stream ast.PdfStream, res ast.PdfDict) (ast.PdfStream, bool, error) {
	data, err := s.doc.DecodeStream(ctx, stream)
	if err != nil {
		return ast.PdfStream{}, false, fmt.Errorf("decoding content: %w", err)
	}
	cr := s.newContentRewriter(ctx, res)
	out, changed, err := cr.rewrite(ctx, data)
	if err != nil || !changed {
		return ast.PdfStream{}, false, err
	}
	s.report.Operators += cr.operators
	s.report.Streams++
	ns, err := newStream(stream.Dict, out)
	if err != nil {
		return ast.PdfStream{}, false, err
	}
	return ns, true, nil
}

// checkDepth refuses a form nesting past the document's budget
// ([document.Document.Limits]).
func (s *session) checkDepth(depth int) error {
	if limit := s.doc.Limits().MaxFormDepth; depth > limit {
		return fmt.Errorf("%w: forms nest deeper than %d", ErrUnsupportedSpace, limit)
	}
	return nil
}

// rewriteForm converts a form XObject (ISO 32000-1, 8.10): its content, its
// resources and its transparency group's blending space. A form without
// /Resources paints with parent's (7.8.3), and is converted for the first
// resources it is reached with. Another XObject, an image, is left.
func (s *session) rewriteForm(ctx context.Context, obj ast.PdfObject, parent ast.PdfDict, depth int) (ast.PdfObject, bool, error) {
	return s.edit(ctx, obj, "form XObject", func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		stream, ok := resolved.(ast.PdfStream)
		if !ok {
			return nil, false, nil
		}
		if sub, _ := s.resolveName(ctx, stream.Dict["Subtype"]); sub != "Form" {
			return nil, false, nil
		}
		if err := s.checkDepth(depth); err != nil {
			return nil, false, err
		}
		return s.rewritePainting(ctx, stream, parent, depth, true)
	})
}

// rewritePainting converts a stream that paints with resources: a form, a
// tiling pattern, an appearance stream, painted with its /Resources or,
// lacking them, parent. group reports a stream that may carry a
// transparency group, which a pattern does not.
func (s *session) rewritePainting(ctx context.Context, stream ast.PdfStream, parent ast.PdfDict, depth int, group bool) (ast.PdfObject, bool, error) {
	resObj := stream.Dict["Resources"]
	res := parent
	if resObj != nil {
		r, err := s.resolveDict(ctx, resObj)
		if err != nil {
			return nil, false, err
		}
		res = r
	}
	out, changed, err := s.rewriteStreamContent(ctx, stream, res)
	if err != nil {
		return nil, false, err
	}
	dict := stream.Dict
	if changed {
		dict = out.Dict
	}
	newDict := maps.Clone(dict)
	dictChanged := false
	if resObj != nil {
		v, ch, err := s.rewriteResources(ctx, resObj, depth)
		if err != nil {
			return nil, false, err
		}
		if ch {
			newDict["Resources"], dictChanged = v, true
		}
	} else if changed && !s.t.device() {
		newDict["Resources"], dictChanged = ast.PdfDict{"ColorSpace": ast.PdfDict{s.targetName(nil): s.t.ColorSpace}}, true
	}
	if group {
		v, ch, err := s.rewriteGroup(ctx, dict["Group"], false)
		if err != nil {
			return nil, false, err
		}
		if ch {
			newDict["Group"], dictChanged = v, true
		}
	}
	switch {
	case changed:
		return ast.NewLazyStream(newDict, out.Bytes), true, nil
	case dictChanged:
		return ast.NewLazyStream(newDict, stream.Bytes), true, nil
	}
	return nil, false, nil
}

// rewritePattern converts a pattern (ISO 32000-1, 8.7): a tiling pattern's
// content (8.7.3), or a shading pattern's shading (8.7.4). An uncoloured
// tiling pattern's colour operators set no colour (8.7.3.3), and convert all
// the same: what it draws stays as it was, and the output names no space
// beside the target.
func (s *session) rewritePattern(ctx context.Context, obj ast.PdfObject, depth int) (ast.PdfObject, bool, error) {
	return s.edit(ctx, obj, "pattern", func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		switch v := resolved.(type) {
		case ast.PdfStream:
			if err := s.checkDepth(depth + 1); err != nil {
				return nil, false, err
			}
			return s.rewritePainting(ctx, v, nil, depth+1, false)
		case ast.PdfDict:
			sh, ch, err := s.rewriteShading(ctx, v["Shading"])
			if err != nil || !ch {
				return nil, false, err
			}
			out := maps.Clone(v)
			out["Shading"] = sh
			return out, true, nil
		}
		return nil, false, nil
	})
}

// rewriteType3 converts the glyph descriptions of a Type 3 font (ISO
// 32000-1, 9.6.5), painted with the font's /Resources or, lacking them, the
// resources of the content that shows the font. A glyph that begins with d1
// sets no colour of its own (Table 113), and its colour operators convert
// all the same, as an uncoloured pattern's do.
func (s *session) rewriteType3(ctx context.Context, obj ast.PdfObject, parent ast.PdfDict, depth int) (ast.PdfObject, bool, error) {
	return s.edit(ctx, obj, "Type 3 font", func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		font, ok := resolved.(ast.PdfDict)
		if !ok {
			return nil, false, nil
		}
		if sub, _ := s.resolveName(ctx, font["Subtype"]); sub != "Type3" {
			return nil, false, nil
		}
		if err := s.checkDepth(depth); err != nil {
			return nil, false, err
		}
		res := parent
		if font["Resources"] != nil {
			r, err := s.resolveDict(ctx, font["Resources"])
			if err != nil {
				return nil, false, err
			}
			res = r
		}
		procs, ch, err := s.rewriteEach(ctx, font["CharProcs"], "glyphs", func(o ast.PdfObject) (ast.PdfObject, bool, error) {
			return s.edit(ctx, o, "glyph", func(g ast.PdfObject) (ast.PdfObject, bool, error) {
				stream, ok := g.(ast.PdfStream)
				if !ok {
					return nil, false, nil
				}
				out, changed, err := s.rewriteStreamContent(ctx, stream, res)
				if err != nil || !changed {
					return nil, false, err
				}
				return out, true, nil
			})
		})
		if err != nil {
			return nil, false, err
		}
		out := maps.Clone(font)
		changed := false
		if ch {
			out["CharProcs"], changed = procs, true
		}
		if font["Resources"] != nil {
			v, rch, err := s.rewriteResources(ctx, font["Resources"], depth)
			if err != nil {
				return nil, false, err
			}
			if rch {
				out["Resources"], changed = v, true
			}
		}
		return out, changed, nil
	})
}
