package flatten

import (
	"context"
	"log/slog"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// paints says which of the current colours a painting operator uses: the
// fill (nonstroking) colour, the stroke colour, or both.
type paints struct{ fill, stroke bool }

var (
	paintFill   = paints{fill: true}
	paintStroke = paints{stroke: true}
	paintBoth   = paints{fill: true, stroke: true}
)

// pathPaints returns what a path-painting operator of ISO 32000-2, 8.5.3.1,
// Table 59 paints, and false for any other operator.
func pathPaints(operator string) (paints, bool) {
	switch operator {
	case "f", "F", "f*":
		return paintFill, true
	case "S", "s":
		return paintStroke, true
	case "B", "B*", "b", "b*":
		return paintBoth, true
	}
	return paints{}, false
}

// textPaints returns what the text rendering mode paints (ISO 32000-2,
// 9.3.6, Table 104): modes 3 and 7 paint nothing.
func textPaints(mode int) paints {
	switch mode {
	case 0, 4:
		return paintFill
	case 1, 5:
		return paintStroke
	case 2, 6:
		return paintBoth
	}
	return paints{}
}

// showText reports whether operator shows text (ISO 32000-2, 9.4.3, Table 107).
func showText(operator string) bool {
	switch operator {
	case "Tj", "TJ", "'", "\"":
		return true
	}
	return false
}

// stateTransparent reports whether painting p under st composites with
// transparency rather than replacing what lies below: a soft mask (ISO
// 32000-2, 11.6.5.2), a blend mode other than Normal, which Compatible is a
// synonym of (11.3.5, Table 134), or a constant alpha below 1 for the colour
// painted (11.6.4.4: CA for stroking, ca for every other painting).
func stateTransparent(st *graphics.State, p paints) bool {
	if p == (paints{}) {
		return false
	}
	if st.SoftMask != nil || (st.BlendMode != "" && st.BlendMode != "Normal") {
		return true
	}
	return (p.fill && st.FillAlpha < 1) || (p.stroke && st.StrokeAlpha < 1)
}

// extGStateTransparent reports whether a graphics state parameter dictionary
// sets transparency (ISO 32000-2, 8.4.5, Table 57): a soft mask other than
// /None, a constant alpha below 1, or a blend mode other than Normal and
// Compatible.
func extGStateTransparent(ctx context.Context, doc *document.Document, gs ast.PdfDict) bool {
	if gs == nil {
		return false
	}
	if obj, err := document.ResolveObject(ctx, doc, gs["SMask"]); err == nil && obj != nil {
		if name, isName := obj.(ast.PdfName); !isName || name != "None" {
			if _, isNull := obj.(ast.PdfNull); !isNull {
				return true
			}
		}
	}
	for _, key := range []ast.PdfName{"CA", "ca"} {
		if obj, err := document.ResolveObject(ctx, doc, gs[key]); err == nil {
			if v, ok := document.Number(obj); ok && v < 1 {
				return true
			}
		}
	}
	return blendModeTransparent(ctx, doc, gs["BM"])
}

// blendModeTransparent reports whether a /BM entry selects a blend mode other
// than Normal: a name, or the first name of an array a reader recognises
// (ISO 32000-2, 11.3.5). An entry that names no blend mode reads as Normal.
func blendModeTransparent(ctx context.Context, doc *document.Document, obj ast.PdfObject) bool {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil || resolved == nil {
		return false
	}
	names := []ast.PdfObject{resolved}
	if arr, ok := resolved.(ast.PdfArray); ok {
		names = arr
	}
	for _, e := range names {
		name, err := document.ResolveName(ctx, doc, e)
		if err != nil {
			continue
		}
		switch name {
		case "Normal", "Compatible":
			return false
		case "Multiply", "Screen", "Overlay", "Darken", "Lighten", "ColorDodge",
			"ColorBurn", "HardLight", "SoftLight", "Difference", "Exclusion",
			"Hue", "Saturation", "Color", "Luminosity":
			return true
		}
	}
	return false
}

// imageTransparent reports whether an image XObject carries its own
// transparency: a soft mask image (ISO 32000-2, 11.6.5.3) or, for a JPEG 2000
// image, /SMaskInData above 0 (8.9.5, Table 87). A /Mask, a stencil or colour
// key mask, is masking rather than transparency (8.9.6) and stays opaque.
func imageTransparent(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	if obj, err := document.ResolveObject(ctx, doc, dict["SMask"]); err == nil {
		if _, isStream := obj.(ast.PdfStream); isStream {
			return true
		}
	}
	if obj, err := document.ResolveObject(ctx, doc, dict["SMaskInData"]); err == nil {
		if v, ok := document.Number(obj); ok && v > 0 {
			return true
		}
	}
	return false
}

// contentInfo is what a content stream paints, through every form and
// tiling pattern it reaches.
type contentInfo struct {
	// transparent reports painting with transparency somewhere.
	transparent bool
	// text reports showing text somewhere, outside tiling pattern cells,
	// whose text paints as the fill or stroke that uses the pattern.
	text bool
}

// scanner reads what form XObjects and tiling patterns paint, each object
// once per document.
type scanner struct {
	doc *document.Document
	// pageResources resolves a name a form's own resources lack, as the
	// renderer resolves it.
	pageResources ast.PdfDict
	forms         map[int]contentInfo
	patterns      map[int]bool
	// maxDepth is the document's form-depth budget: a form nested deeper is
	// skipped by the renderer, so it paints nothing here either.
	maxDepth int
}

func newScanner(doc *document.Document, pageResources ast.PdfDict) *scanner {
	return &scanner{
		doc:           doc,
		pageResources: pageResources,
		forms:         make(map[int]contentInfo),
		patterns:      make(map[int]bool),
		maxDepth:      doc.Limits().WithDefaults().MaxFormDepth,
	}
}

// lookup returns the resource of category and name, from resources first
// and then from the page's.
func (s *scanner) lookup(ctx context.Context, resources ast.PdfDict, category, name ast.PdfName) ast.PdfObject {
	for _, res := range []ast.PdfDict{resources, s.pageResources} {
		if res == nil {
			continue
		}
		all, err := document.ResolveDict(ctx, s.doc, res[category])
		if err != nil || all == nil {
			continue
		}
		if obj, ok := all[name]; ok {
			return obj
		}
	}
	return nil
}

// form returns what the form XObject obj paints, chain naming the forms
// being scanned to break a cycle. A form with no /Resources reads those of
// the stream that paints it, parent (ISO 32000-2, 7.8.3).
func (s *scanner) form(ctx context.Context, obj ast.PdfObject, stream ast.PdfStream, parent ast.PdfDict, depth int, chain []int) contentInfo {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if info, ok := s.forms[ref.ObjectNum]; ok {
			return info
		}
		if slices.Contains(chain, ref.ObjectNum) {
			return contentInfo{}
		}
		chain = append(chain, ref.ObjectNum)
	}
	if depth > s.maxDepth {
		return contentInfo{}
	}
	resources, err := document.ResolveDict(ctx, s.doc, stream.Dict["Resources"])
	if err != nil || resources == nil {
		resources = parent
	}
	ops, err := content.ParsePageContent(ctx, s.doc, stream, content.SplitGluedOperators(), content.ViewerFilterChains())
	if err != nil {
		// A form whose content does not parse paints nothing in the
		// renderer either.
		s.doc.Logger.Debug("flatten: form content does not parse", slog.Any("err", err))
		return contentInfo{}
	}
	info := s.stream(ctx, ops, resources, depth, chain)
	if isRef {
		s.forms[ref.ObjectNum] = info
	}
	return info
}

// tiling reports whether the tiling pattern obj paints with transparency.
func (s *scanner) tiling(ctx context.Context, obj ast.PdfObject, stream ast.PdfStream, depth int, chain []int) bool {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if v, ok := s.patterns[ref.ObjectNum]; ok {
			return v
		}
		if slices.Contains(chain, ref.ObjectNum) {
			return false
		}
		chain = append(chain, ref.ObjectNum)
	}
	if depth > s.maxDepth {
		return false
	}
	resources, err := document.ResolveDict(ctx, s.doc, stream.Dict["Resources"])
	if err != nil {
		resources = nil
	}
	ops, err := content.ParsePageContent(ctx, s.doc, stream, content.SplitGluedOperators(), content.ViewerFilterChains())
	if err != nil {
		s.doc.Logger.Debug("flatten: pattern content does not parse", slog.Any("err", err))
		return false
	}
	v := s.stream(ctx, ops, resources, depth, chain).transparent
	if isRef {
		s.patterns[ref.ObjectNum] = v
	}
	return v
}

// colorTransparent reports whether c is a pattern that paints with
// transparency: a shading pattern whose /ExtGState sets it (ISO 32000-2,
// 8.7.4.1, Table 76), or a tiling pattern whose cell does (8.7.3.1).
func (s *scanner) colorTransparent(ctx context.Context, c graphics.Color, resources ast.PdfDict, depth int, chain []int) bool {
	if c.Pattern == "" {
		return false
	}
	obj := s.lookup(ctx, resources, "Pattern", ast.PdfName(c.Pattern))
	if obj == nil {
		return false
	}
	resolved, err := document.ResolveObject(ctx, s.doc, obj)
	if err != nil {
		return false
	}
	switch v := resolved.(type) {
	case ast.PdfStream:
		return s.tiling(ctx, obj, v, depth+1, chain)
	case ast.PdfDict:
		gs, err := document.ResolveDict(ctx, s.doc, v["ExtGState"])
		return err == nil && extGStateTransparent(ctx, s.doc, gs)
	}
	return false
}

// painting reports whether painting p under st, with its colours, is
// transparent.
func (s *scanner) painting(ctx context.Context, st *graphics.State, p paints, resources ast.PdfDict, depth int, chain []int) bool {
	if stateTransparent(st, p) {
		return true
	}
	return (p.fill && s.colorTransparent(ctx, st.Fill, resources, depth, chain)) ||
		(p.stroke && s.colorTransparent(ctx, st.Stroke, resources, depth, chain))
}

// xobject resolves the XObject a Do names, with its subtype.
func (s *scanner) xobject(ctx context.Context, resources ast.PdfDict, op content.Operation) (ast.PdfObject, ast.PdfStream, ast.PdfName, bool) {
	if len(op.Operands) != 1 {
		return nil, ast.PdfStream{}, "", false
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return nil, ast.PdfStream{}, "", false
	}
	obj := s.lookup(ctx, resources, "XObject", name)
	if obj == nil {
		return nil, ast.PdfStream{}, "", false
	}
	stream, err := document.ResolveStream(ctx, s.doc, obj)
	if err != nil || stream.Dict == nil {
		return nil, ast.PdfStream{}, "", false
	}
	subtype, err := document.ResolveName(ctx, s.doc, stream.Dict["Subtype"])
	if err != nil {
		return nil, ast.PdfStream{}, "", false
	}
	return obj, stream, subtype, true
}

// stream reads what ops paint against resources, depth forms below the page.
func (s *scanner) stream(ctx context.Context, ops []content.Operation, resources ast.PdfDict, depth int, chain []int) contentInfo {
	var info contentInfo
	m := graphics.NewMachine([4]float64{})
	m.Doc, m.Resources = s.doc, resources
	for i, op := range ops {
		if i%opsPerContextCheck == 0 && ctx.Err() != nil {
			return info
		}
		if err := m.ExecuteOne(ctx, op); err != nil {
			continue
		}
		st := m.Current()
		if p, ok := pathPaints(op.Operator); ok {
			info.transparent = info.transparent || s.painting(ctx, st, p, resources, depth, chain)
			continue
		}
		switch {
		case showText(op.Operator):
			info.text = true
			info.transparent = info.transparent || s.painting(ctx, st, textPaints(st.Text.Tmode), resources, depth, chain)
		case op.Operator == "sh", op.Operator == "BI":
			info.transparent = info.transparent || stateTransparent(st, paintFill)
		case op.Operator == "Do":
			obj, stream, subtype, ok := s.xobject(ctx, resources, op)
			if !ok {
				continue
			}
			switch subtype {
			case "Image":
				info.transparent = info.transparent || stateTransparent(st, paintFill) || imageTransparent(ctx, s.doc, stream.Dict)
			case "Form":
				inner := s.form(ctx, obj, stream, resources, depth+1, chain)
				info.text = info.text || inner.text
				info.transparent = info.transparent || inner.transparent || stateTransparent(st, paintBoth)
			}
		}
	}
	return info
}
