package recolor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// BlackPointReport says what the /UseBlackPtComp of the graphics states
// (ISO 32000-2, Table 57) changed in a conversion. A state's /ON and /OFF
// override [Options.BlackPointCompensation] for the colours painted under it,
// and /Default, the initial value, leaves it to the option.
//
// # Corpus evidence
//
//   - All corpora (litho, pdf20, pdfcpu, pdfjs, govdocs; 2594 files):
//     converted to the built-in CMYK, and that flattened, the outputs are
//     byte-identical but for litho usebpc_4.pdf (2 copies, 2 operators) and
//     pdf20 PDF_2.0_image_with_BPC.pdf (CalRGB images under /ON, 2 copies,
//     1 operator). pdfjs issue17794.pdf states /Default and is refused for
//     an inline image short of a row.
//   - litho usebpc_4.pdf: converted to the built-in CMYK without
//     compensation and rendered at 72 dpi against its source, its /ON page
//     moves from 100 levels apart at worst, 52.5% of its pixels past 8, to
//     46 and 15%, and its /OFF page stays 9 levels apart. Its /Default page
//     stays 100 apart, the renderer compensating under /Default, until the
//     conversion compensates too.
type BlackPointReport struct {
	// Operators counts the colour operators converted under a state whose
	// compensation differs from the option's, those written again after the
	// gs that set it among them.
	Operators int
	// InlineImages counts the inline images converted under such a state.
	InlineImages int
	// Copies counts the image XObjects and shadings painted under such a
	// state, written a second time converted with its compensation and
	// painted through a resource name of their own, the first conversion
	// serving every painting under the option's.
	Copies int
	// Unhonoured counts the paintings under such a state whose colour still
	// converts with the option's compensation: a form XObject or a Type 3
	// glyph, which inherits the state (ISO 32000-1, 8.10.1, 9.6.5) but whose
	// content converts once for every painting, and an image or a shading
	// painted by a stream without resources of its own, which a copy's name
	// cannot be added to.
	Unhonoured int
}

// effectiveBPC returns whether a colour painted under a graphics state whose
// /UseBlackPtComp is name converts with black point compensation. ISO
// 32000-2, Table 57: /ON shall use it and /OFF shall not, whatever the
// processor's own choice, and /Default, the initial value ("" here), leaves
// the choice to the processor, which for a conversion is the caller's
// [Options.BlackPointCompensation].
func (s *session) effectiveBPC(name string) bool {
	switch name {
	case "ON":
		return true
	case "OFF":
		return false
	}
	return s.opts.BlackPointCompensation
}

// underBPC runs f with the plans and transforms it builds converting under
// black point compensation on.
func (s *session) underBPC(on bool, f func()) {
	prev := s.bpc
	s.bpc = on
	defer func() { s.bpc = prev }()
	f()
}

// bpcKey spells the session's current black point compensation in a cache
// key, "" when it is the option's, so the keys of a conversion no graphics
// state overrides stay what they were.
func (s *session) bpcKey() string {
	if s.bpc == s.opts.BlackPointCompensation {
		return ""
	}
	if s.bpc {
		return "|BPC=ON"
	}
	return "|BPC=OFF"
}

// convOptions returns the options the engine builds a transform with: the
// session's, under its current black point compensation.
func (s *session) convOptions() Options {
	o := s.opts
	o.BlackPointCompensation = s.bpc
	return o
}

// bpcDependent reports a plan whose conversion black point compensation
// changes: one that converts colour, or a Pattern space over a base that
// does. A kept space's definition converts where the resources define it,
// once, under the option's compensation.
func bpcDependent(p *spacePlan) bool {
	if p == nil {
		return false
	}
	if p.kind == planPattern {
		return p.base != nil && p.base.convertible()
	}
	return p.convertible()
}

// overridden reports whether the colours painted now convert with another
// black point compensation than the option's, and which.
func (cr *contentRewriter) overridden() (bool, bool) {
	on := cr.s.effectiveBPC(cr.state.bpc)
	return on, on != cr.s.opts.BlackPointCompensation
}

// planUnder returns the plan f builds, under the black point compensation
// the graphics state asks for. f is called under the option's first: a plan
// that compensation does not change is built once, and a kept space's
// definition, which allocates its rewritten objects, is never built twice.
func (cr *contentRewriter) planUnder(f func() *spacePlan) *spacePlan {
	p := f()
	on, over := cr.overridden()
	if !over || !bpcDependent(p) {
		return p
	}
	cr.s.underBPC(on, func() { p = f() })
	return p
}

// blackPointName reads the /UseBlackPtComp of the graphics state dictionary
// name of the stream's resources, and false when it states none ISO 32000-2,
// Table 57 defines: another value is ignored, as the renderer's graphics
// state machine ignores it, and leaves the parameter as it was.
func (cr *contentRewriter) blackPointName(ctx context.Context, name ast.PdfName) (string, bool) {
	all, err := cr.s.resolveDict(ctx, cr.res["ExtGState"])
	if err != nil || all == nil {
		return "", false
	}
	gs, err := cr.s.resolveDict(ctx, all[name])
	if err != nil || gs == nil {
		return "", false
	}
	v, err := cr.s.resolveName(ctx, gs["UseBlackPtComp"])
	if err != nil {
		return "", false
	}
	switch v {
	case "ON", "OFF":
		return string(v), true
	case "Default":
		return "", true
	}
	return "", false
}

// applyBlackPoint follows a gs operator's /UseBlackPtComp. A colour converts
// when it is set, where a viewer converts it when it paints: so when the
// state changes the compensation, the fill and stroke colours in force are
// written again after the gs, converted under the new compensation, and the
// paintings that follow paint what the viewer paints. It returns the text
// replacing op, and false when op stays.
func (cr *contentRewriter) applyBlackPoint(ctx context.Context, op content.Operation) ([]byte, bool, error) {
	if len(op.Operands) != 1 {
		return nil, false, nil
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return nil, false, nil
	}
	v, ok := cr.blackPointName(ctx, name)
	if !ok {
		return nil, false, nil
	}
	before := cr.s.effectiveBPC(cr.state.bpc)
	cr.state.bpc = v
	if cr.s.effectiveBPC(v) == before {
		return nil, false, nil
	}
	var again [][]byte
	for _, stroke := range []bool{false, true} {
		text, ok, err := cr.setAgain(ctx, stroke)
		if err != nil {
			return nil, false, err
		}
		if ok {
			again = append(again, text)
		}
	}
	if len(again) == 0 {
		return nil, false, nil
	}
	var b bytes.Buffer
	if _, err := writer.SerializeName(&b, name); err != nil {
		return nil, false, fmt.Errorf("writing a graphics state name: %w", err)
	}
	b.WriteString(" gs ")
	b.Write(bytes.Join(again, []byte(" ")))
	return b.Bytes(), true, nil
}

// setAgain writes the colour in force for filling or stroking again, planned
// under the current black point compensation, and false when its space does
// not convert or no operator set it.
func (cr *contentRewriter) setAgain(ctx context.Context, stroke bool) ([]byte, bool, error) {
	set := cr.state.fillSet
	if stroke {
		set = cr.state.strokeSet
	}
	if set.space == nil {
		return nil, false, nil
	}
	p := cr.spacePlan(ctx, *set.space)
	if !bpcDependent(p) {
		return nil, false, nil
	}
	cr.set(stroke, p)
	if set.colour == nil {
		switch set.space.Operator {
		case "cs", "CS":
			if !p.convertible() {
				return nil, false, nil
			}
			return cr.counted(cr.write(stroke, p, p.initial, ""))
		}
		return cr.counted(cr.convert(stroke, p, set.space.Operands))
	}
	operands := set.colour.Operands
	if p.kind == planPattern {
		if len(operands) == 0 {
			return nil, false, nil
		}
		pattern, ok := operands[len(operands)-1].(ast.PdfName)
		if !ok {
			return nil, false, nil
		}
		return cr.counted(cr.convertPattern(stroke, cr.s.solidPlan(p.base), operands[:len(operands)-1], pattern))
	}
	return cr.counted(cr.convert(stroke, p, operands))
}

// counted counts a colour operator written under a black point compensation
// other than the option's, once the rewrite stands.
func (cr *contentRewriter) counted(text []byte, ok bool, err error) ([]byte, bool, error) {
	if ok && err == nil {
		if _, over := cr.overridden(); over {
			cr.pendingBPC++
		}
	}
	return text, ok, err
}

// resourceAdds are resources a rewritten stream names that its resource
// dictionary lacks: copies by category, /XObject or /Shading, and name.
type resourceAdds map[ast.PdfName]map[ast.PdfName]ast.PdfObject

func (a resourceAdds) add(cat, name ast.PdfName, obj ast.PdfObject) {
	if a[cat] == nil {
		a[cat] = make(map[ast.PdfName]ast.PdfObject)
	}
	a[cat][name] = obj
}

func (a resourceAdds) merge(b resourceAdds) {
	for cat, names := range b {
		for name, obj := range names {
			a.add(cat, name, obj)
		}
	}
}

// bpcCopy is a copy [session.blackPointCopy] made: its reference, or none
// when the object converts alike under either compensation.
type bpcCopy struct {
	obj ast.PdfObject
	ok  bool
}

// maxVariantNames bounds the numbered names [contentRewriter.variantName]
// tries. Anti-DoS guard, not spec-mandated: a resource category holding that
// many names of one stem is no producer's.
const maxVariantNames = 1000

// paintVariant paints the image or shading a Do or sh operator names, in the
// category cat, under a black point compensation other than the option's:
// through a copy converted with it, under a name of its own the stream's
// resources gain. A form XObject inherits the state (ISO 32000-1, 8.10.1)
// but converts once, so its painting is counted as unhonoured. It returns
// the operator naming the copy, and false when op stays.
func (cr *contentRewriter) paintVariant(ctx context.Context, cat ast.PdfName, op content.Operation) ([]byte, bool, error) {
	on, over := cr.overridden()
	if !over || cr.dry || len(op.Operands) != 1 {
		return nil, false, nil
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return nil, false, nil
	}
	s := cr.s
	entries, _ := s.resolveDict(ctx, cr.res[cat])
	obj, ok := entries[name]
	if !ok {
		return nil, false, nil
	}
	if cat == "XObject" {
		stream, err := s.resolveStream(ctx, obj)
		if err != nil {
			return nil, false, nil //nolint:nilerr // an XObject that is no stream paints nothing to copy.
		}
		switch sub, _ := s.resolveName(ctx, stream.Dict["Subtype"]); sub {
		case "Image":
		case "Form":
			s.report.BlackPoint.Unhonoured++
			return nil, false, nil
		default:
			return nil, false, nil
		}
	}
	if cr.adds == nil {
		s.report.BlackPoint.Unhonoured++
		return nil, false, nil
	}
	copied, ok, err := s.blackPointCopy(ctx, cat, obj, cr.res, on)
	if err != nil || !ok {
		return nil, false, err
	}
	alias := cr.variantName(cat, name, on, entries)
	cr.adds.add(cat, alias, copied)
	cr.streamAdds.add(cat, alias, copied)
	var b bytes.Buffer
	if _, err := writer.SerializeName(&b, alias); err != nil {
		return nil, false, fmt.Errorf("writing a resource name: %w", err)
	}
	b.WriteString(" " + op.Operator)
	cr.pendingCopies++
	return b.Bytes(), true, nil
}

// variantName returns the name the copy of the resource name, converted
// under compensation on, is painted through: name_BPCON or name_BPCOFF, or
// that with a number appended when entries, the category's resources, or
// another copy already take it. Each painting of one copy in the stream's
// resources takes the same name.
func (cr *contentRewriter) variantName(cat, name ast.PdfName, on bool, entries ast.PdfDict) ast.PdfName {
	key := fmt.Sprintf("%s/%s/%t", cat, name, on)
	if alias, ok := cr.aliases[key]; ok {
		return alias
	}
	stem := string(name) + "_BPC" + map[bool]string{false: "OFF", true: "ON"}[on]
	alias := ast.PdfName(fmt.Sprintf("%s%d", stem, maxVariantNames))
	for i := range maxVariantNames {
		try := ast.PdfName(stem)
		if i > 0 {
			try = ast.PdfName(fmt.Sprintf("%s%d", stem, i))
		}
		_, inRes := entries[try]
		_, inAdds := cr.adds[cat][try]
		if !inRes && !inAdds {
			alias = try
			break
		}
	}
	cr.aliases[key] = alias
	return alias
}

// blackPointCopy returns a copy of the image or shading obj, painted with
// the resources res, converted under black point compensation on, written
// once per object, compensation and default spaces; false when the object
// converts alike under either compensation: one already in the target, a
// stencil mask, a kept spot colour, a shading in a kept space.
func (s *session) blackPointCopy(ctx context.Context, cat ast.PdfName, obj ast.PdfObject, res ast.PdfDict, on bool) (ast.PdfObject, bool, error) {
	source, err := s.resolve(ctx, obj)
	if err != nil {
		// An object the file lacks paints nothing (ISO 32000-1, 7.3.10),
		// under either compensation.
		s.doc.Logger.Debug("recolor: a painted object reads as null", slog.Any("err", err))
		return nil, false, nil //nolint:nilerr // logged: an object the file lacks has nothing to copy.
	}
	var key string
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		key = fmt.Sprintf("%s %d", cat, ref.ObjectNum)
	} else {
		sum := sha256.Sum256(fmt.Appendf(nil, "%v", source))
		key = fmt.Sprintf("%s %x", cat, sum)
	}
	key = fmt.Sprintf("%s|%t%s", key, on, defaultsKey(ctx, s.doc, res))
	if c, ok := s.bpcCopies[key]; ok {
		return c.obj, c.ok, nil
	}
	var c bpcCopy
	if cat == "Shading" && !bpcDependent(s.planDefinition(ctx, shadingSpace(source), nil)) {
		// A shading in a kept space, whose definition converts once where
		// the resources define it, or one that stays: planned under the
		// option's compensation alone, so no kept definition is rewritten
		// twice.
		s.bpcCopies[key] = c
		return nil, false, nil
	}
	s.underBPC(on, func() {
		switch cat {
		case "XObject":
			ref, isRef := obj.(ast.PdfIndirectRef)
			if !isRef {
				return
			}
			var copied ast.PdfIndirectRef
			copied, c.ok, err = s.images.convertCopy(ctx, ref, res)
			c.obj = copied
		case "Shading":
			var out ast.PdfObject
			var converted bool
			out, converted, err = s.convertShading(ctx, source)
			if converted {
				c.obj, c.ok = s.alloc.Alloc(out), true
			}
		}
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		// The painting keeps the object's conversion under the option's
		// compensation, which it is left with as the copy's failure says.
		s.doc.Logger.Debug("recolor: no copy under the graphics state's black point compensation", slog.String("object", key), slog.Any("err", err))
		s.report.BlackPoint.Unhonoured++
		c = bpcCopy{}
	}
	if c.ok {
		s.report.BlackPoint.Copies++
	}
	s.bpcCopies[key] = c
	return c.obj, c.ok, nil
}

// countBlackPoint adds what the rewriter cr converted under a graphics
// state's black point compensation to the report.
func (s *session) countBlackPoint(cr *contentRewriter) {
	s.report.BlackPoint.Operators += cr.bpcOps
	s.report.BlackPoint.InlineImages += cr.inlineBPC
}

// shadingSpace returns the /ColorSpace of a shading, a dictionary or a
// stream's (ISO 32000-1, 8.7.4.5, Table 78), nil for another object.
func shadingSpace(shading ast.PdfObject) ast.PdfObject {
	switch v := shading.(type) {
	case ast.PdfDict:
		return v["ColorSpace"]
	case ast.PdfStream:
		return v.Dict["ColorSpace"]
	}
	return nil
}

// latest returns obj as the session last wrote it: the version the allocator
// queues, else the document's.
func (s *session) latest(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if o, ok := s.alloc.Pending()[ref.ObjectNum]; ok {
			return o, nil
		}
	}
	return s.resolve(ctx, obj)
}

// withVariants returns a resource dictionary of its own for a stream whose
// rewrite names copies its resources lack: resources, the stream's
// resources as the session last wrote them, with each category adds names
// holding the copies beside its own entries. A stream that shared its
// resources then paints with its own, which define every name the shared
// ones did.
func (s *session) withVariants(ctx context.Context, resources ast.PdfObject, adds resourceAdds) (ast.PdfDict, error) {
	resolved, err := s.latest(ctx, resources)
	if err != nil {
		return nil, err
	}
	res, ok := resolved.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("%w: resources are %T", ErrUnsupportedSpace, resolved)
	}
	out := maps.Clone(res)
	for cat, names := range adds {
		entries := ast.PdfDict{}
		if o, err := s.latest(ctx, res[cat]); err == nil {
			if d, ok := o.(ast.PdfDict); ok {
				entries = maps.Clone(d)
			}
		}
		maps.Copy(entries, names)
		out[cat] = entries
	}
	return out, nil
}
