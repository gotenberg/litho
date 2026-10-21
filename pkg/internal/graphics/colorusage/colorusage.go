package colorusage

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// maxSpaceNesting bounds how deep a colour space definition is read through
// its bases and alternates. Anti-DoS guard, not spec-mandated: ISO 32000-1,
// 8.6.6 and 8.7.3.3 nest them at most three deep (a Pattern over an Indexed
// over an ICCBased with an alternate), so a deeper chain is a crafted cycle.
const maxSpaceNesting = 4

// ctxCheckInterval is how many operators the walk reads between two checks of
// its context, the checkpoint CONTRIBUTING sets for content streams.
const ctxCheckInterval = 100

// Source values of [Use.Source].
const (
	SourceOperator    = "operator"
	SourceImage       = "image"
	SourceInlineImage = "inline-image"
	SourceShading     = "shading"
	SourceGroup       = "group"
)

// Space describes one colour space definition.
type Space struct {
	// Family is the family of ISO 32000-1, 8.6.3, Table 62, "" when the
	// definition names none.
	Family string
	// Components is how many numbers a colour in the space carries.
	Components int
	// Alternate is the base of an Indexed space, the alternate of a
	// Separation, DeviceN or ICCBased space, and the underlying space of a
	// Pattern space, nil otherwise.
	Alternate *Space
	// Colorants names the colorants of a Separation or DeviceN space.
	Colorants []string
}

// key returns a string that tells two spaces apart.
func (s *Space) key() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("%s/%d/%s(%s)", s.Family, s.Components, strings.Join(s.Colorants, ","), s.Alternate.key())
}

// Use is one colour space a page's content uses, in one way: see
// models.ColorSpaceUse, which it mirrors.
type Use struct {
	Space    Space
	Source   string
	Operator string
	Name     string
	Object   int
	Count    int
}

// walker accumulates the uses of one page.
type walker struct {
	doc *document.Document
	// seen marks the streams already walked, by object number, so a form, a
	// pattern or a glyph drawn many times is read once and a cycle ends.
	seen     map[int]bool
	maxDepth int
	index    map[string]int
	uses     []Use
	ops      int
}

// Page returns the colour spaces page's content uses, in the order the walk
// first meets each: the page's content stream, then its annotations'
// appearance streams, descending into each form XObject, tiling pattern and
// Type 3 glyph it draws as it meets them.
//
// The walk reports what the content selects, not what it ends up painting:
// a cs followed by no painting operator still counts. A device family stays
// as written whatever Default colour space the resources define for it (ISO
// 32000-1, 8.6.5.6). An image mask (8.9.6.2) paints in the fill colour, which
// its own colour operators report. A soft mask's group (11.6.5.2) shapes
// opacity, not colour, and is not walked.
//
// Each stream is walked once, and the descent stops at the document's form
// nesting budget.
func Page(ctx context.Context, doc *document.Document, page *document.Page) ([]Use, error) {
	w := &walker{
		doc:      doc,
		seen:     make(map[int]bool),
		maxDepth: doc.Limits().MaxFormDepth,
		index:    make(map[string]int),
	}
	if page.Ref.ObjectNum > 0 {
		w.seen[page.Ref.ObjectNum] = true
	}
	w.group(ctx, page.Dict["Group"], page.Ref.ObjectNum)
	ops, err := content.ParsePageContent(ctx, doc, page.Dict)
	if err != nil {
		return nil, fmt.Errorf("parsing page content: %w", err)
	}
	if err := w.walk(ctx, ops, page.Resources, 0, 0); err != nil {
		return nil, err
	}
	if err := w.annotations(ctx, page); err != nil {
		return nil, err
	}
	return w.uses, nil
}

// add records one use.
func (w *walker) add(s Space, source, operator, name string, object int) {
	k := fmt.Sprintf("%s|%s|%s|%d|%s", source, operator, name, object, s.key())
	if i, ok := w.index[k]; ok {
		w.uses[i].Count++
		return
	}
	w.index[k] = len(w.uses)
	w.uses = append(w.uses, Use{Space: s, Source: source, Operator: operator, Name: name, Object: object, Count: 1})
}

// walk reads the operations of one content stream, object (0 for the page's
// own) under resources, at depth levels of nesting.
func (w *walker) walk(ctx context.Context, ops []content.Operation, resources ast.PdfDict, object, depth int) error {
	for _, op := range ops {
		w.ops++
		if w.ops%ctxCheckInterval == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if err := w.operation(ctx, op, resources, object, depth); err != nil {
			return err
		}
	}
	return nil
}

// deviceOperators maps the operators that select a device family and set
// the colour at once to that family (ISO 32000-1, 8.6.8, Table 74).
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 74
var deviceOperators = map[string]string{
	"g": graphics.FamilyDeviceGray, "G": graphics.FamilyDeviceGray,
	"rg": graphics.FamilyDeviceRGB, "RG": graphics.FamilyDeviceRGB,
	"k": graphics.FamilyDeviceCMYK, "K": graphics.FamilyDeviceCMYK,
}

// operation records what one operation uses and descends into what it draws.
func (w *walker) operation(ctx context.Context, op content.Operation, resources ast.PdfDict, object, depth int) error {
	if family, ok := deviceOperators[op.Operator]; ok {
		w.add(familySpace(family), SourceOperator, op.Operator, "", object)
		return nil
	}
	switch op.Operator {
	case "cs", "CS":
		if len(op.Operands) != 1 {
			return nil
		}
		name, ok := op.Operands[0].(ast.PdfName)
		if !ok {
			return nil
		}
		s, resource := w.namedSpace(ctx, name, resources)
		w.add(s, SourceOperator, op.Operator, resource, object)
	case "scn", "SCN":
		// Only a Pattern space takes a name operand (ISO 32000-1, 8.6.8,
		// Table 74), the pattern's resource name.
		if len(op.Operands) == 0 {
			return nil
		}
		if name, ok := op.Operands[len(op.Operands)-1].(ast.PdfName); ok {
			return w.pattern(ctx, op.Operator, name, resources, depth)
		}
	case "sh":
		if len(op.Operands) != 1 {
			return nil
		}
		if name, ok := op.Operands[0].(ast.PdfName); ok {
			obj := w.resource(ctx, resources, "Shading", name)
			w.shading(ctx, obj, op.Operator, string(name))
		}
	case "Do":
		if len(op.Operands) != 1 {
			return nil
		}
		if name, ok := op.Operands[0].(ast.PdfName); ok {
			return w.xobject(ctx, name, resources, depth)
		}
	case "BI":
		if len(op.Operands) > 0 {
			if dict, ok := op.Operands[0].(ast.PdfDict); ok {
				w.inlineImage(ctx, dict, resources, object)
			}
		}
	case "Tf":
		if len(op.Operands) == 2 {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				return w.type3(ctx, name, resources, depth)
			}
		}
	}
	return nil
}

// resource returns the entry name of the resources' subdictionary category,
// unresolved, or nil.
func (w *walker) resource(ctx context.Context, resources ast.PdfDict, category, name ast.PdfName) ast.PdfObject {
	if resources == nil {
		return nil
	}
	dict, err := document.ResolveDict(ctx, w.doc, resources[category])
	if err != nil || dict == nil {
		return nil
	}
	return dict[name]
}

// namedSpace reads the operand of cs or CS: a family named directly, or a
// /ColorSpace resource (ISO 32000-1, 8.6.8). It returns the space and the
// resource name, "" for a family named directly. A direct family name wins
// over a resource of the same name, as [graphics.Machine] reads it.
func (w *walker) namedSpace(ctx context.Context, name ast.PdfName, resources ast.PdfDict) (Space, string) {
	switch name {
	case graphics.FamilyDeviceGray, graphics.FamilyDeviceRGB, graphics.FamilyDeviceCMYK, graphics.FamilyPattern:
		return familySpace(string(name)), ""
	}
	return w.describe(ctx, w.resource(ctx, resources, "ColorSpace", name), 0), string(name)
}

// familySpace is the space a bare family name selects.
func familySpace(family string) Space {
	switch family {
	case graphics.FamilyDeviceGray:
		return Space{Family: family, Components: 1}
	case graphics.FamilyDeviceRGB:
		return Space{Family: family, Components: 3}
	case graphics.FamilyDeviceCMYK:
		return Space{Family: family, Components: 4}
	}
	return Space{Family: family}
}

// describe reads a colour space definition, a family name or an array whose
// first element is the family (ISO 32000-1, 8.6.3), with its bases and
// alternates, depth levels down.
func (w *walker) describe(ctx context.Context, obj ast.PdfObject, depth int) Space {
	if obj == nil || depth > maxSpaceNesting {
		return Space{}
	}
	cs, err := graphics.DescribeColorSpace(ctx, w.doc, obj)
	if err != nil {
		return Space{}
	}
	s := Space{Family: cs.Family, Components: cs.Components}
	def, _ := cs.Object.(ast.PdfArray)
	at := func(i int) ast.PdfObject {
		if i < len(def) {
			return def[i]
		}
		return nil
	}
	sub := func(obj ast.PdfObject) *Space {
		if obj == nil {
			return nil
		}
		a := w.describe(ctx, obj, depth+1)
		return &a
	}
	switch cs.Family {
	case graphics.FamilyIndexed:
		// [/Indexed base hival lookup] (8.6.6.3).
		s.Alternate = sub(at(1))
	case graphics.FamilySeparation:
		// [/Separation name alternateSpace tintTransform] (8.6.6.4).
		if n, err := document.ResolveName(ctx, w.doc, at(1)); err == nil {
			s.Colorants = []string{string(n)}
		}
		s.Alternate = sub(at(2))
	case graphics.FamilyDeviceN:
		// [/DeviceN names alternateSpace tintTransform attributes] (8.6.6.5).
		if names, err := document.ResolveArray(ctx, w.doc, at(1)); err == nil {
			for _, n := range names {
				if name, err := document.ResolveName(ctx, w.doc, n); err == nil {
					s.Colorants = append(s.Colorants, string(name))
				}
			}
		}
		s.Alternate = sub(at(2))
	case graphics.FamilyICCBased:
		// The stream's /Alternate (8.6.5.5, Table 66).
		if stream, err := document.ResolveStream(ctx, w.doc, at(1)); err == nil && stream.Dict != nil {
			s.Alternate = sub(stream.Dict["Alternate"])
		}
	case graphics.FamilyPattern:
		if cs.Base != nil {
			s.Alternate = sub(at(1))
		}
	}
	return s
}

// shading records the colour space of the shading obj names (ISO 32000-1,
// 8.7.4.5, Table 78), painted by operator through the resource name.
func (w *walker) shading(ctx context.Context, obj ast.PdfObject, operator, name string) {
	if obj == nil {
		return
	}
	dict, err := document.ResolveDict(ctx, w.doc, obj)
	if err != nil || dict == nil {
		return
	}
	w.add(w.describe(ctx, dict["ColorSpace"], 0), SourceShading, operator, name, objectNum(obj))
}

// pattern records what the pattern resource name paints: a shading
// pattern's shading (ISO 32000-1, 8.7.4.3), or the content of a tiling
// pattern (8.7.3), walked as a stream of its own.
func (w *walker) pattern(ctx context.Context, operator string, name ast.PdfName, resources ast.PdfDict, depth int) error {
	obj := w.resource(ctx, resources, "Pattern", name)
	if obj == nil {
		return nil
	}
	resolved, err := document.ResolveObject(ctx, w.doc, obj)
	if err != nil {
		return nil //nolint:nilerr // an unresolvable pattern paints nothing (8.6.8), so it uses no colour space
	}
	switch v := resolved.(type) {
	case ast.PdfDict:
		w.shading(ctx, v["Shading"], operator, string(name))
	case ast.PdfStream:
		return w.stream(ctx, obj, v, resources, depth)
	}
	return nil
}

// xobject descends into the XObject resource name: an image's colour space,
// a form's content (ISO 32000-1, 8.8 and 8.10).
func (w *walker) xobject(ctx context.Context, name ast.PdfName, resources ast.PdfDict, depth int) error {
	obj := w.resource(ctx, resources, "XObject", name)
	stream, err := document.ResolveStream(ctx, w.doc, obj)
	if err != nil || stream.Dict == nil {
		return nil //nolint:nilerr // a Do naming no XObject paints nothing (8.8)
	}
	switch w.name(ctx, stream.Dict["Subtype"]) {
	case "Image":
		if w.flag(ctx, stream.Dict["ImageMask"]) {
			return nil
		}
		var s Space
		if cs, ok := stream.Dict["ColorSpace"]; ok {
			s = w.describe(ctx, cs, 0)
		}
		w.add(s, SourceImage, "Do", string(name), objectNum(obj))
	case "Form":
		return w.stream(ctx, obj, stream, resources, depth)
	}
	return nil
}

// stream walks the content of a form XObject, tiling pattern, glyph or
// appearance stream obj resolves to, once, under its own /Resources or, when
// it has none, those of the stream that draws it (ISO 32000-1, 7.8.3).
func (w *walker) stream(ctx context.Context, obj ast.PdfObject, stream ast.PdfStream, parent ast.PdfDict, depth int) error {
	if depth >= w.maxDepth || document.VisitedBefore(obj, w.seen) {
		return nil
	}
	resources := parent
	if own, err := document.ResolveDict(ctx, w.doc, stream.Dict["Resources"]); err == nil && own != nil {
		resources = own
	}
	w.group(ctx, stream.Dict["Group"], objectNum(obj))
	ops, err := content.ParsePageContent(ctx, w.doc, stream)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return nil //nolint:nilerr // a stream that does not parse draws nothing the walk can read, as for rendering
	}
	return w.walk(ctx, ops, resources, objectNum(obj), depth+1)
}

// group records the blending space of the transparency group obj states, its
// /CS (ISO 32000-1, 11.6.6, Table 147), owner being the page or form.
func (w *walker) group(ctx context.Context, obj ast.PdfObject, owner int) {
	dict, err := document.ResolveDict(ctx, w.doc, obj)
	if err != nil || dict == nil {
		return
	}
	if w.name(ctx, dict["S"]) != "Transparency" {
		return
	}
	cs, ok := dict["CS"]
	if !ok {
		return
	}
	w.add(w.describe(ctx, cs, 0), SourceGroup, "Group", "", owner)
}

// type3 walks the glyph procedures of the font resource name when it is a
// Type 3 font (ISO 32000-1, 9.6.5), whose glyphs are content streams.
func (w *walker) type3(ctx context.Context, name ast.PdfName, resources ast.PdfDict, depth int) error {
	font, err := document.ResolveDict(ctx, w.doc, w.resource(ctx, resources, "Font", name))
	if err != nil || font == nil {
		return nil //nolint:nilerr // a font that does not resolve draws no glyph content
	}
	if w.name(ctx, font["Subtype"]) != "Type3" {
		return nil
	}
	procs, err := document.ResolveDict(ctx, w.doc, font["CharProcs"])
	if err != nil || procs == nil {
		return nil //nolint:nilerr // a Type 3 font with no glyphs draws nothing
	}
	glyphResources := resources
	if own, err := document.ResolveDict(ctx, w.doc, font["Resources"]); err == nil && own != nil {
		glyphResources = own
	}
	for _, key := range slices.Sorted(maps.Keys(procs)) {
		stream, err := document.ResolveStream(ctx, w.doc, procs[key])
		if err != nil || stream.Dict == nil {
			continue
		}
		if err := w.stream(ctx, procs[key], stream, glyphResources, depth); err != nil {
			return err
		}
	}
	return nil
}

// annotations walks the appearance streams of the page's annotations: each
// of /N, /R and /D, a stream or a dictionary of appearance states (ISO
// 32000-1, 12.5.5, Table 168).
func (w *walker) annotations(ctx context.Context, page *document.Page) error {
	annots, err := document.ResolveArray(ctx, w.doc, page.Dict["Annots"])
	if err != nil {
		return nil //nolint:nilerr // a page whose /Annots does not resolve shows no annotation
	}
	for _, a := range annots {
		annot, err := document.ResolveDict(ctx, w.doc, a)
		if err != nil || annot == nil {
			continue
		}
		ap, err := document.ResolveDict(ctx, w.doc, annot["AP"])
		if err != nil || ap == nil {
			continue
		}
		for _, kind := range []ast.PdfName{"N", "R", "D"} {
			if err := w.appearance(ctx, ap[kind], page.Resources); err != nil {
				return err
			}
		}
	}
	return nil
}

// appearance walks one appearance entry: a stream, or a dictionary mapping
// appearance states to streams.
func (w *walker) appearance(ctx context.Context, obj ast.PdfObject, resources ast.PdfDict) error {
	if obj == nil {
		return nil
	}
	resolved, err := document.ResolveObject(ctx, w.doc, obj)
	if err != nil {
		return nil //nolint:nilerr // an appearance that does not resolve draws nothing
	}
	switch v := resolved.(type) {
	case ast.PdfStream:
		return w.stream(ctx, obj, v, resources, 0)
	case ast.PdfDict:
		for _, state := range slices.Sorted(maps.Keys(v)) {
			stream, err := document.ResolveStream(ctx, w.doc, v[state])
			if err != nil || stream.Dict == nil {
				continue
			}
			if err := w.stream(ctx, v[state], stream, resources, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

// inlineSpaces maps the colour space names an inline image may give /CS,
// abbreviated as ISO 32000-1, 8.9.7, Table 94 allows or in full, to the full
// family names.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 94
var inlineSpaces = map[ast.PdfName]ast.PdfName{
	"G": graphics.FamilyDeviceGray, graphics.FamilyDeviceGray: graphics.FamilyDeviceGray,
	"RGB": graphics.FamilyDeviceRGB, graphics.FamilyDeviceRGB: graphics.FamilyDeviceRGB,
	"CMYK": graphics.FamilyDeviceCMYK, graphics.FamilyDeviceCMYK: graphics.FamilyDeviceCMYK,
	"I": graphics.FamilyIndexed, graphics.FamilyIndexed: graphics.FamilyIndexed,
}

// inlineImage records the colour space of an inline image (ISO 32000-1,
// 8.9.7): /CS or /ColorSpace, a device family, abbreviated or not, a
// /ColorSpace resource name, or an Indexed array, whose base may be
// abbreviated too. An image mask, /IM or /ImageMask true, uses none.
func (w *walker) inlineImage(ctx context.Context, dict ast.PdfDict, resources ast.PdfDict, object int) {
	for _, key := range []ast.PdfName{"IM", "ImageMask"} {
		if dict[key] == ast.PdfBool(true) {
			return
		}
	}
	obj, ok := dict["CS"]
	if !ok {
		obj, ok = dict["ColorSpace"]
	}
	if !ok {
		w.add(Space{}, SourceInlineImage, "BI", "", object)
		return
	}
	name := ""
	s := w.inlineSpace(ctx, obj, resources, 0)
	if n, isName := obj.(ast.PdfName); isName && inlineSpaces[n] == "" {
		name = string(n)
	}
	w.add(s, SourceInlineImage, "BI", name, object)
}

// inlineSpace reads an inline image's colour space operand obj.
func (w *walker) inlineSpace(ctx context.Context, obj ast.PdfObject, resources ast.PdfDict, depth int) Space {
	switch v := obj.(type) {
	case ast.PdfName:
		if family, ok := inlineSpaces[v]; ok {
			return familySpace(string(family))
		}
		return w.describe(ctx, w.resource(ctx, resources, "ColorSpace", v), 0)
	case ast.PdfArray:
		if len(v) == 0 || depth > maxSpaceNesting {
			return Space{}
		}
		family, _ := v[0].(ast.PdfName)
		if inlineSpaces[family] != graphics.FamilyIndexed {
			return w.describe(ctx, v, 0)
		}
		s := Space{Family: graphics.FamilyIndexed, Components: 1}
		if len(v) > 1 {
			base := w.inlineSpace(ctx, v[1], resources, depth+1)
			s.Alternate = &base
		}
		return s
	}
	return Space{}
}

// name returns the name obj resolves to, "" for an absent entry and for one
// that resolves to no name: each test the walk makes of a name reads the two
// alike, as an entry that does not select what it asks about.
func (w *walker) name(ctx context.Context, obj ast.PdfObject) ast.PdfName {
	if obj == nil {
		return ""
	}
	n, err := document.ResolveName(ctx, w.doc, obj)
	if err != nil {
		return ""
	}
	return n
}

// flag reports whether obj resolves to true. An absent or malformed boolean
// entry takes the false default of the flags the walk reads (ISO 32000-1,
// 8.9.5, Table 89, /ImageMask).
func (w *walker) flag(ctx context.Context, obj ast.PdfObject) bool {
	if obj == nil {
		return false
	}
	v, err := document.ResolveObject(ctx, w.doc, obj)
	return err == nil && v == ast.PdfBool(true)
}

// objectNum returns the object number obj refers to, 0 for a direct object.
func objectNum(obj ast.PdfObject) int {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return ref.ObjectNum
	}
	return 0
}
