package page

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/imgpaint"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
)

// inlineImageName labels an inline image, which has no resource name.
const inlineImageName = graphics.InlineImageName

// do runs Do (ISO 32000-1, 8.8): it paints the named image or form XObject.
// A PostScript XObject paints nothing (8.8.2).
func (r *renderer) do(s *scope, op content.Operation) error {
	if len(op.Operands) != 1 || r.m.Current().SkipExtraction {
		return nil
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return nil
	}
	obj := r.resource(s, "XObject", name)
	if obj == nil {
		r.skip("XObject", fmt.Errorf("no /XObject resource %s", name))
		return nil
	}
	stream, err := document.ResolveStream(r.ctx, r.doc, obj)
	if err != nil || stream.Dict == nil {
		r.skip("XObject", fmt.Errorf("XObject %s is not a stream: %w", name, err))
		return nil
	}
	if !r.visible(stream.Dict["OC"]) {
		return nil
	}
	subtype, err := document.ResolveName(r.ctx, r.doc, stream.Dict["Subtype"])
	if err != nil {
		r.skip("XObject", fmt.Errorf("XObject %s has no /Subtype: %w", name, err))
		return nil
	}
	switch subtype {
	case "Image":
		return r.image(s, name, stream)
	case "Form":
		ref, _ := obj.(ast.PdfIndirectRef)
		return r.form(s, ref, stream)
	}
	return nil
}

// form paints a form XObject (ISO 32000-1, 8.10.1): its content runs in a
// saved graphics state with /Matrix concatenated to the CTM and the clip
// narrowed to /BBox, against its own /Resources, or those of the stream that
// paints it when it has none (7.8.3 keeps that reading for older files). A
// transparency group (11.6.6) is painted as plain content for now.
func (r *renderer) form(s *scope, ref ast.PdfIndirectRef, stream ast.PdfStream) error {
	if lim := r.doc.Limits().WithDefaults().MaxFormDepth; s.depth+1 > lim {
		r.skip("form XObject", fmt.Errorf("nested deeper than %d forms", lim))
		return nil
	}
	if ref.ObjectNum != 0 && slices.Contains(r.chain, ref.ObjectNum) {
		r.skip("form XObject", fmt.Errorf("form %d paints itself", ref.ObjectNum))
		return nil
	}
	dict := stream.Dict
	matrix := geom.Identity
	if v, err := document.ResolveArray(r.ctx, r.doc, dict["Matrix"]); err == nil && len(v) == 6 {
		for i, e := range v {
			f, ok := document.Number(e)
			if !ok {
				matrix = geom.Identity
				break
			}
			matrix[i] = f
		}
	}
	bbox, err := document.ResolveArray(r.ctx, r.doc, dict["BBox"])
	if err != nil || len(bbox) != 4 {
		// Table 95 requires /BBox; poppler skips a form without a usable one.
		r.skip("form XObject", fmt.Errorf("no usable /BBox: %w", err))
		return nil
	}
	var box [4]float64
	for i, e := range bbox {
		f, ok := document.Number(e)
		if !ok {
			r.skip("form XObject", errors.New("a /BBox entry is no number"))
			return nil
		}
		box[i] = f
	}
	if _, isGroup := dict["Group"]; isGroup {
		r.report.count(UnsupportedGroup)
	}
	resources, err := document.ResolveDict(r.ctx, r.doc, dict["Resources"])
	if err != nil || resources == nil {
		resources = s.resources
	}
	ops, err := content.ParsePageContent(r.ctx, r.doc, stream, content.SplitGluedOperators(), content.InlineImageData())
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("form XObject", err)
		return nil
	}

	savedStack, savedMarked := len(r.m.Stack), len(r.m.MarkedContent)
	if err := r.m.ExecuteOne(r.ctx, content.Operation{Operator: "q"}); err != nil {
		return fmt.Errorf("saving state for a form: %w", err)
	}
	r.syncClips()
	defer func() {
		r.m.Stack = r.m.Stack[:savedStack]
		if len(r.m.MarkedContent) > savedMarked {
			r.m.MarkedContent = r.m.MarkedContent[:savedMarked]
		}
		r.syncClips()
	}()
	st := r.m.Current()
	st.CTM = [6]float64(matrix.Mul(geom.Matrix(st.CTM)))
	formToDevice := r.toDevice()

	var bp geom.Path
	bp.MoveTo(formToDevice.Apply(geom.Point{X: box[0], Y: box[1]}))
	bp.LineTo(formToDevice.Apply(geom.Point{X: box[2], Y: box[1]}))
	bp.LineTo(formToDevice.Apply(geom.Point{X: box[2], Y: box[3]}))
	bp.LineTo(formToDevice.Apply(geom.Point{X: box[0], Y: box[3]}))
	bp.Close()
	c, err := r.clip().withPath(bp, raster.NonZero, r.canvas.Width, r.canvas.Height, r.budget)
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("form XObject", err)
		return nil
	}
	if err := r.setClip(c); err != nil {
		return err
	}

	r.chain = append(r.chain, ref.ObjectNum)
	defer func() { r.chain = r.chain[:len(r.chain)-1] }()
	return r.run(ops, resources, formToDevice, s.depth+1)
}

// image paints an image XObject or an inline image (ISO 32000-1, 8.9): the
// unit square through the CTM, in the fill alpha (11.6.4.4), made
// transparent by its /SMask, or by its /Mask when it has none (11.6.5.3). An
// image mask (8.9.6.2) paints the fill colour through its samples.
func (r *renderer) image(s *scope, name ast.PdfName, stream ast.PdfStream) error {
	c := r.clip()
	if c.empty() {
		return nil
	}
	st := r.m.Current()
	r.noteEffects(st)
	dict := stream.Dict
	interpolate, _ := document.ResolveBool(r.ctx, r.doc, dict["Interpolate"])
	clipMask, err := c.mask1(r.canvas.Width, r.canvas.Height, r.budget)
	if err != nil {
		return err
	}
	p := imgpaint.Params{Matrix: r.toDevice(), Interpolate: bool(interpolate), Alpha: alpha32(st.FillAlpha), Clip: clipMask}

	if isMask, _ := document.ResolveBool(r.ctx, r.doc, dict["ImageMask"]); isMask {
		if st.Fill.Space.Family == graphics.FamilyPattern {
			r.report.count(UnsupportedPatternMask)
			return nil
		}
		img, err := pdfimage.DecodeMask(r.ctx, r.doc, name, stream)
		if err != nil {
			return r.imageFailed(err)
		}
		cr, cg, cb := r.converter(s, st.Fill.Space).RGB(st.Fill.Components)
		return r.imageFailed(imgpaint.DrawStencil(r.ctx, r.canvas, imgpaint.StencilMask(img, bool(interpolate)), cr, cg, cb, p))
	}

	img, err := pdfimage.DecodePainted(r.ctx, r.doc, name, stream, s.resources, r.page.Dict)
	if err != nil {
		return r.imageFailed(err)
	}
	b := img.Bounds()
	return r.imageFailed(imgpaint.Draw(r.ctx, r.canvas, img, r.imageMasks(name, stream, b.Dx(), b.Dy()), p))
}

// imageFailed returns err when it ends the render, and otherwise skips the
// image and returns nil.
func (r *renderer) imageFailed(err error) error {
	if err == nil {
		return nil
	}
	if fatal(err) {
		return err
	}
	r.skip("image", err)
	return nil
}

// imageMasks reads the masks of an image (ISO 32000-1, 8.9.5, Table 89): a
// /SMask stream, else a /Mask stream (a stencil, 8.9.6.3) or array (a colour
// key, 8.9.6.4). A mask that does not decode is left out, so the image paints
// opaque.
func (r *renderer) imageMasks(name ast.PdfName, stream ast.PdfStream, width, height int) imgpaint.Masks {
	dict := stream.Dict
	if obj, ok := dict["SMask"]; ok {
		if sm, err := document.ResolveStream(r.ctx, r.doc, obj); err == nil && sm.Dict != nil {
			img, err := pdfimage.DecodeMask(r.ctx, r.doc, name+" soft mask", sm)
			if err == nil {
				interp, _ := document.ResolveBool(r.ctx, r.doc, sm.Dict["Interpolate"])
				return imgpaint.Masks{SMask: imgpaint.SoftMask(img, bool(interp))}
			}
			r.skip("image soft mask", err)
		}
	}
	obj, ok := dict["Mask"]
	if !ok {
		return imgpaint.Masks{}
	}
	resolved, err := document.ResolveObject(r.ctx, r.doc, obj)
	if err != nil {
		r.skip("image mask", err)
		return imgpaint.Masks{}
	}
	switch v := resolved.(type) {
	case ast.PdfStream:
		img, err := pdfimage.DecodeMask(r.ctx, r.doc, name+" mask", v)
		if err != nil {
			r.skip("image mask", err)
			return imgpaint.Masks{}
		}
		interp, _ := document.ResolveBool(r.ctx, r.doc, v.Dict["Interpolate"])
		return imgpaint.Masks{Mask: imgpaint.StencilMask(img, bool(interp))}
	case ast.PdfArray:
		key, err := r.colorKey(stream, v, width, height)
		if err != nil {
			r.skip("image colour key", err)
			return imgpaint.Masks{}
		}
		return imgpaint.Masks{Mask: key}
	}
	return imgpaint.Masks{}
}

// colorKey reads the colour-key mask ranges of an image (ISO 32000-1,
// 8.9.6.4) against its samples as its filters leave them.
func (r *renderer) colorKey(stream ast.PdfStream, ranges ast.PdfArray, width, height int) (*imgpaint.Alpha, error) {
	dict := stream.Dict
	bpc, err := document.ResolveInt(r.ctx, r.doc, dict["BitsPerComponent"])
	if err != nil {
		return nil, fmt.Errorf("colour key: /BitsPerComponent: %w", err)
	}
	space, err := graphics.DescribeColorSpace(r.ctx, r.doc, dict["ColorSpace"])
	if err != nil {
		return nil, fmt.Errorf("colour key: %w", err)
	}
	keys := make([]int, len(ranges))
	for i, v := range ranges {
		n, err := document.ResolveInt(r.ctx, r.doc, v)
		if err != nil {
			return nil, fmt.Errorf("colour key: range %d: %w", i, err)
		}
		keys[i] = int(n)
	}
	maxPixels := r.doc.Limits().WithDefaults().MaxImagePixels
	if width <= 0 || height <= 0 || int64(width) > maxPixels/int64(height) || bpc < 1 || bpc > 16 || space.Components < 1 {
		return nil, fmt.Errorf("colour key over a %dx%d image of %d bits", width, height, bpc)
	}
	rowBytes := (int64(width)*int64(space.Components)*int64(bpc) + 7) / 8
	rc, err := r.doc.NewStreamReaderChecked(r.ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("colour key: opening samples: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(rc, rowBytes*int64(height)))
	if closeErr := rc.Close(); closeErr != nil {
		r.doc.Logger.Debug("render: closing colour-key samples", slog.Any("err", closeErr))
	}
	if err != nil {
		return nil, fmt.Errorf("colour key: reading samples: %w", err)
	}
	return imgpaint.ColorKey(data, width, height, space.Components, int(bpc), keys, maxPixels)
}

// visible reports whether optional content oc, an /OC entry, is shown (ISO
// 32000-1, 8.11.3.3): an optional content group the document's default
// configuration turns off is hidden. A membership dictionary is taken as
// shown for now.
func (r *renderer) visible(oc ast.PdfObject) bool {
	if oc == nil {
		return true
	}
	ref, isRef := oc.(ast.PdfIndirectRef)
	if !isRef {
		return true
	}
	dict, err := document.ResolveDict(r.ctx, r.doc, ref)
	if err != nil || dict == nil {
		return true
	}
	if t, _ := document.ResolveName(r.ctx, r.doc, dict["Type"]); t != "OCG" {
		return true
	}
	return ocgVisible(ref, r.doc.OCConfig)
}

// ocgVisible reads the visibility of the group ref under the document's
// default configuration (ISO 32000-1, 8.11.4.3): /OFF, then /ON, then
// /BaseState.
func ocgVisible(ref ast.PdfIndirectRef, cfg *document.OCConfig) bool {
	switch {
	case cfg == nil:
		return true
	case cfg.Off[ref.ObjectNum]:
		return false
	case cfg.On[ref.ObjectNum]:
		return true
	}
	return cfg.BaseState != "OFF"
}

// activeOCs maps the optional content groups the /Properties of resources
// names to their visibility, which the machine reads when BDC marks content
// /OC (8.11.3.2).
func activeOCs(ctx context.Context, doc *document.Document, resources ast.PdfDict) map[string]bool {
	out := make(map[string]bool)
	props, err := document.ResolveDict(ctx, doc, resources["Properties"])
	if err != nil || props == nil {
		return out
	}
	for k, v := range props {
		ref, isRef := v.(ast.PdfIndirectRef)
		if !isRef {
			continue
		}
		dict, err := document.ResolveDict(ctx, doc, ref)
		if err != nil || dict == nil {
			continue
		}
		if t, _ := document.ResolveName(ctx, doc, dict["Type"]); t == "OCG" {
			out[string(k)] = ocgVisible(ref, doc.OCConfig)
		}
	}
	return out
}
