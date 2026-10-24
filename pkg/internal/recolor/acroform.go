package recolor

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// maxFieldKidsDepth bounds the descent down a form field's /Kids. Anti-DoS
// guard, not spec-mandated: ISO 32000-1, 12.7.3.1 describes a field hierarchy
// a few levels deep, so 100 sits far above any real form, and the walk marks
// the nodes and arrays it descends through ([document.VisitedBefore]), so a
// /Kids naming an ancestor ends there. It matches the /Kids descent bound of
// package compliance (maxFieldTreeDepth in compliance/forms.go).
const maxFieldKidsDepth = 100

// rewriteAcroForm converts the text colour of the document's interactive form:
// the form's /DA and that of each field of its /Fields tree (ISO 32000-1,
// 12.7.2, Table 218, and 12.7.3.3). A field inherits /DA from its ancestors
// and the form, and a viewer regenerating a widget's appearance, as litho's
// form flattening does, paints its text with the colour the string states,
// which would otherwise stay in the source space. The widgets the page walk
// reached already converted with their annotations
// ([session.annotationColours]) and are passed over. A form that cannot be
// read shows nothing to convert.
//
// # Corpus evidence
//
//   - litho sigcms_2b.pdf: converted to CMYK and then form-flattened, its
//     text field's appearance, generated from the form's /DA, painted in
//     DeviceGray.
//   - litho: 6 files hold a /DA colour (annothidden_2b, annotstruct_ua2,
//     flattenannots_2b, flatuntagged_ua1, idfields_ua2, sigcms_2b); each
//     converts, and no rendering of a converted page moves.
func (s *session) rewriteAcroForm(ctx context.Context) error {
	ref, ok := s.doc.CatalogRef(ctx)
	if !ok {
		return nil
	}
	catalog, err := s.resolveDict(ctx, ref)
	if err != nil || catalog == nil || catalog["AcroForm"] == nil {
		return nil //nolint:nilerr // an unreadable catalog holds no form to convert.
	}
	form, changed, err := s.edit(ctx, catalog["AcroForm"], "interactive form", func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		dict, ok := resolved.(ast.PdfDict)
		if !ok {
			return nil, false, nil
		}
		var out ast.PdfDict
		set := func(key ast.PdfName, v ast.PdfObject) {
			if out == nil {
				out = maps.Clone(dict)
			}
			out[key] = v
		}
		v, changed, err := s.defaultAppearance(ctx, dict["DA"])
		if err != nil {
			return nil, false, err
		}
		if changed {
			set("DA", v)
		}
		fields, changed, err := s.rewriteFields(ctx, dict["Fields"], 0)
		if err != nil {
			return nil, false, err
		}
		if changed {
			set("Fields", fields)
		}
		return out, out != nil, nil
	})
	if err != nil {
		return err
	}
	if changed {
		out := maps.Clone(catalog)
		out["AcroForm"] = form
		s.replace(ref.ObjectNum, catalog, out)
	}
	return nil
}

// rewriteFields converts the /DA of each field of kids, a /Fields or /Kids
// array depth levels down the field tree, and of the fields below them. It
// returns the array rewritten, and true, when kids is direct and a field
// written in it changed; an array of its own, or a field of its own, is
// rewritten in place.
func (s *session) rewriteFields(ctx context.Context, kids ast.PdfObject, depth int) (ast.PdfObject, bool, error) {
	if kids == nil || depth > maxFieldKidsDepth || document.VisitedBefore(kids, s.seen) {
		return nil, false, nil
	}
	resolved, err := s.resolve(ctx, kids)
	if err != nil {
		return nil, false, nil //nolint:nilerr // an unreadable /Kids holds no field to convert.
	}
	list, _ := resolved.(ast.PdfArray)
	var rewritten ast.PdfArray
	for i, kid := range list {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if document.VisitedBefore(kid, s.seen) {
			continue
		}
		r, err := s.resolve(ctx, kid)
		if err != nil {
			continue
		}
		field, _ := r.(ast.PdfDict)
		if field == nil {
			continue
		}
		var out ast.PdfDict
		v, changed, err := s.defaultAppearance(ctx, field["DA"])
		if err != nil {
			return nil, false, err
		}
		if changed {
			out = maps.Clone(field)
			out["DA"] = v
		}
		sub, changed, err := s.rewriteFields(ctx, field["Kids"], depth+1)
		if err != nil {
			return nil, false, err
		}
		if changed {
			if out == nil {
				out = maps.Clone(field)
			}
			out["Kids"] = sub
		}
		if out == nil {
			continue
		}
		if ref, isRef := kid.(ast.PdfIndirectRef); isRef {
			s.replace(ref.ObjectNum, field, out)
			continue
		}
		if rewritten == nil {
			rewritten = slices.Clone(list)
		}
		rewritten[i] = out
	}
	if rewritten == nil {
		return nil, false, nil
	}
	if ref, isRef := kids.(ast.PdfIndirectRef); isRef {
		s.replace(ref.ObjectNum, list, rewritten)
		return nil, false, nil
	}
	return rewritten, true, nil
}

// defaultAppearance returns the default appearance string obj, a /DA (ISO
// 32000-1, 12.7.3.3), with its colour converted ([session.daColour]), and
// true when it changed and is direct. An indirect string is rewritten in
// place and comes back as its reference with false, as [session.edit] does.
func (s *session) defaultAppearance(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, bool, error) {
	if obj == nil {
		return nil, false, nil
	}
	return s.edit(ctx, obj, "default appearance", func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		da, ok := resolved.(ast.PdfString)
		if !ok {
			return nil, false, nil
		}
		out, changed, err := s.daColour(ctx, []byte(da))
		if err != nil || !changed {
			return nil, false, err
		}
		return ast.PdfString(out), true, nil
	})
}

// daColour returns da, a default appearance string, with its device colour
// operators converted to the target. ISO 32000-1, 12.7.3.3 makes a /DA a run
// of content stream operators, its colour set by g, rg or k (and the
// stroking G, RG and K a viewer applies alike), each the device space its
// component count names, with no default space standing in, since no
// resources govern it (8.6.5.6 applies to content streams). The converted
// colour is written with the device operator of the target's component
// count: a /DA names no colour space resource, so an ICCBased target's
// values mean the device space of its count, as the annotation colour arrays
// read them ([session.annotationColours]). Every other operator keeps its
// bytes, and the result is read back: it must give the operations kept and
// written, or da stays as it was and is reported.
func (s *session) daColour(ctx context.Context, da []byte) ([]byte, bool, error) {
	ops, ranges, err := parseContent(ctx, da)
	if err != nil {
		// A /DA that does not parse paints no colour litho could read.
		return nil, false, nil //nolint:nilerr // see above.
	}
	var edits []edit
	var want []content.Operation
	for i, op := range ops {
		repl, ok := s.daOperator(ctx, op)
		if !ok {
			want = append(want, op)
			continue
		}
		written, err := parseOps(ctx, repl)
		if err != nil {
			return nil, false, err
		}
		edits = append(edits, edit{span: ranges[i], text: repl})
		want = append(want, written...)
	}
	if len(edits) == 0 {
		return nil, false, nil
	}
	var out bytes.Buffer
	pos := 0
	for _, e := range edits {
		out.Write(da[pos:e.span.Start])
		if e.span.Start > 0 && !isWhite(da[e.span.Start-1]) {
			out.WriteByte(' ')
		}
		out.Write(e.text)
		pos = e.span.End
		if pos < len(da) && !isWhite(da[pos]) {
			out.WriteByte(' ')
		}
	}
	out.Write(da[pos:])
	got, err := content.ParseStreamData(ctx, out.Bytes(), content.WholeStream())
	if err != nil {
		return nil, false, fmt.Errorf("reading the rewritten /DA back: %w", err)
	}
	if !sameOperations(got, want) {
		return nil, false, fmt.Errorf("%w: the rewritten /DA reads as other operations", ErrUnsupportedSpace)
	}
	s.report.Operators += len(edits)
	return out.Bytes(), true, nil
}

// daOperator returns the text that replaces op, a device colour operator of
// a /DA, in the target, and false for any other operator, one whose
// operands are not its space's component count of numbers, and one already
// in the target.
func (s *session) daOperator(ctx context.Context, op content.Operation) ([]byte, bool) {
	var family string
	switch op.Operator {
	case "g", "G":
		family = graphics.FamilyDeviceGray
	case "rg", "RG":
		family = graphics.FamilyDeviceRGB
	case "k", "K":
		family = graphics.FamilyDeviceCMYK
	default:
		return nil, false
	}
	p := s.planDefinition(ctx, ast.PdfName(family), nil)
	if p.kind != planConvert {
		return nil, false
	}
	comps, ok := operandsOf(op.Operands, p.n)
	if !ok {
		return nil, false
	}
	for i, v := range comps {
		// Table 74 gives the components on [0, 1], as viewers clip them.
		comps[i] = clamp01(v)
	}
	conv := make([]float64, s.t.n)
	p.conv.Convert(comps, conv)
	var b bytes.Buffer
	for _, v := range conv {
		b.WriteString(writer.FormatReal(roundComponent(v)))
		b.WriteByte(' ')
	}
	name := map[string]string{
		graphics.FamilyDeviceGray: "g",
		graphics.FamilyDeviceRGB:  "rg",
		graphics.FamilyDeviceCMYK: "k",
	}[s.t.deviceFamily()]
	if op.Operator[0] < 'a' {
		name = string(bytes.ToUpper([]byte(name)))
	}
	b.WriteString(name)
	return b.Bytes(), true
}
