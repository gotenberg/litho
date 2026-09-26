package signature

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// appearanceTypes are the /Type values of the objects an appearance stream
// draws with. An appearance stream is a form XObject (ISO 32000-1, 12.5.5), and
// what it draws comes from its resource dictionary (7.8.3, Table 33): other
// XObjects (8.8), fonts with their descriptors, encodings and CMaps (9.6.2,
// Table 111; 9.8, Table 122; 9.6.6, Table 114; 9.7.5), graphics states (8.4.5,
// Table 58) and patterns (8.7.3), which in turn name soft masks (11.6.5.2,
// Table 144), transparency groups (8.10.3, Table 96) and halftones (10.5).
// Optional content groups are left out: they belong to the document's optional
// content configuration (8.11.4), not to one appearance.
var appearanceTypes = map[string]bool{
	"XObject":        true,
	"Font":           true,
	"FontDescriptor": true,
	"Encoding":       true,
	"CMap":           true,
	"ExtGState":      true,
	"Pattern":        true,
	"Mask":           true,
	"Group":          true,
	"Halftone":       true,
}

// appearanceOf reports whether a resolved object may belong to an appearance:
// it carries no /Type, or one of [appearanceTypes]. A typed object outside
// them, a page or an annotation, ends the walk of [drawnObjects].
func appearanceOf(obj ast.PdfObject) bool {
	var dict ast.PdfDict
	switch v := obj.(type) {
	case ast.PdfDict:
		dict = v
	case ast.PdfStream:
		dict = v.Dict
	default:
		return true
	}
	typ := dictName(dict, "Type")
	return typ == "" || appearanceTypes[typ]
}

// drawnObjects returns the objects doc reaches from the appearance
// dictionaries (/AP, ISO 32000-1, 12.5.5, Table 168) of the annotations annots,
// and from the default resources the default appearance of each widget among
// them names ([defaultResourcesNamed]), as [drawnFrom] walks them.
func drawnObjects(ctx context.Context, doc *document.Document, annots []int) (map[int]bool, error) {
	var stack []ast.PdfObject
	var widgets []ast.PdfDict
	for _, num := range annots {
		annot, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: num})
		if err != nil || annot == nil {
			continue
		}
		if ap, ok := annot["AP"]; ok {
			stack = append(stack, ap)
		}
		if dictName(annot, "Subtype") == "Widget" {
			widgets = append(widgets, annot)
		}
	}
	named, err := defaultResourcesNamed(ctx, doc, widgets)
	if err != nil {
		return nil, err
	}
	return drawnFrom(ctx, doc, append(stack, named...))
}

// drawnFrom returns the objects doc reaches from roots, following every
// reference through objects [appearanceOf] accepts. An object it refuses is
// neither returned nor walked.
//
// Each object is resolved and walked once, so the walk ends on a cycle and its
// work is linear in the objects doc defines. Direct objects are walked with an
// explicit stack, so their nesting costs no recursion.
func drawnFrom(ctx context.Context, doc *document.Document, roots []ast.PdfObject) (map[int]bool, error) {
	reached := make(map[int]bool)
	stack := append([]ast.PdfObject(nil), roots...)
	visited := make(map[int]bool)
	for len(stack) > 0 {
		obj := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		ref, isRef := obj.(ast.PdfIndirectRef)
		if isRef {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
			if len(visited)%100 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			var err error
			if obj, err = document.ResolveObject(ctx, doc, ref); err != nil {
				// An object that cannot be read leads nowhere: the walk
				// reaches less, so fewer objects are exempt.
				doc.Logger.Debug("resolving an appearance object", slog.Int("object", ref.ObjectNum), slog.Any("err", err))
				continue
			}
		}
		if !appearanceOf(obj) {
			continue
		}
		if isRef {
			reached[ref.ObjectNum] = true
		}
		switch v := obj.(type) {
		case ast.PdfDict:
			for _, entry := range v {
				stack = append(stack, entry)
			}
		case ast.PdfStream:
			for _, entry := range v.Dict {
				stack = append(stack, entry)
			}
		case ast.PdfArray:
			stack = append(stack, v...)
		}
	}
	return reached, nil
}

// ownAppearances returns the appearance streams the annotations annots name
// directly in rev: each /N, /R and /D entry of their /AP, or each stream of an
// entry that is a dictionary of appearance states (ISO 32000-1, 12.5.5, Table
// 168). An annotation rev does not hold names none.
func ownAppearances(ctx context.Context, rev *document.Document, annots []int) map[int]bool {
	own := make(map[int]bool)
	for _, num := range annots {
		annot, err := document.ResolveDict(ctx, rev, ast.PdfIndirectRef{ObjectNum: num})
		if err != nil || annot == nil {
			continue
		}
		ap, err := document.ResolveDict(ctx, rev, annot["AP"])
		if err != nil || ap == nil {
			continue
		}
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			entry, ok := ap[key]
			if !ok {
				continue
			}
			if ref, ok := entry.(ast.PdfIndirectRef); ok {
				own[ref.ObjectNum] = true
			}
			states, err := document.ResolveObject(ctx, rev, entry)
			if err != nil {
				continue
			}
			if dict, ok := states.(ast.PdfDict); ok {
				for _, state := range dict {
					if ref, ok := state.(ast.PdfIndirectRef); ok {
						own[ref.ObjectNum] = true
					}
				}
			}
		}
	}
	return own
}

// resourceOperand says which operand of an operator names a resource, and in
// which category of a resource dictionary (ISO 32000-1, 7.8.3, Table 33).
type resourceOperand struct {
	category ast.PdfName
	last     bool // the name is the last operand, not the first
}

// defaultAppearanceResources are the operators a default appearance string may
// hold that name a resource. ISO 32000-1, 12.7.3.3, Table 222 fills /DA with
// graphics state and text state operators: the text font (9.3.1, Table 105,
// Tf), a graphics state parameter dictionary (8.4.4, Table 57, gs), and the
// colour spaces and patterns the colour operators select (8.6.8, Table 74).
var defaultAppearanceResources = map[string]resourceOperand{
	"Tf":  {category: "Font"},
	"gs":  {category: "ExtGState"},
	"CS":  {category: "ColorSpace"},
	"cs":  {category: "ColorSpace"},
	"SCN": {category: "Pattern", last: true},
	"scn": {category: "Pattern", last: true},
}

// defaultResourcesNamed returns the values of the resources in the interactive
// form's default resources (/DR, ISO 32000-1, 12.7.2, Table 218) that the
// default appearance of each of widgets names: its /DA, inherited from its
// field ancestors or, failing those, from the form's /DA (12.7.3.3, Table
// 222). A reader builds a field's appearance with the font the /DA names, which
// shall be one of /DR's fonts, so a signer or a form fill that sets a /DA may
// add that font to /DR beside it. The /Parent walk is bounded by
// [maxFieldTreeDepth].
//
// # Corpus evidence
//
//   - pdfjs issue17169.pdf: the AntennaHouse PDF Signature Module writes
//     /DA (/MS-Mincho 0 Tf 0 g) on each signature widget it signs and adds a
//     new MS-Mincho font to /DR, which no appearance stream draws with.
func defaultResourcesNamed(ctx context.Context, doc *document.Document, widgets []ast.PdfDict) ([]ast.PdfObject, error) {
	if len(widgets) == 0 {
		return nil, nil
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("reading the catalog for the default resources: %w", err)
	}
	form, err := document.ResolveDict(ctx, doc, catalog.Dict["AcroForm"])
	if err != nil || form == nil {
		// A form that cannot be read has no default resources to name.
		doc.Logger.Debug("resolving /AcroForm for its default resources", slog.Any("err", err))
		return nil, nil
	}
	resources, err := document.ResolveDict(ctx, doc, form["DR"])
	if err != nil || resources == nil {
		doc.Logger.Debug("resolving the form's /DR", slog.Any("err", err))
		return nil, nil
	}

	var named []ast.PdfObject
	for i, widget := range widgets {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		da := defaultAppearance(ctx, doc, widget, form)
		if da == "" {
			continue
		}
		ops, err := content.ParseStreamData(ctx, []byte(da))
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			// A /DA that does not parse names nothing past where it stops.
			doc.Logger.Debug("parsing a widget's /DA", slog.Any("err", err))
		}
		for _, op := range ops {
			operand, ok := defaultAppearanceResources[op.Operator]
			if !ok || len(op.Operands) == 0 {
				continue
			}
			nameObj := op.Operands[0]
			if operand.last {
				nameObj = op.Operands[len(op.Operands)-1]
			}
			name, ok := nameObj.(ast.PdfName)
			if !ok {
				continue
			}
			category, err := document.ResolveDict(ctx, doc, resources[operand.category])
			if err != nil || category == nil {
				continue
			}
			if value, ok := category[name]; ok {
				named = append(named, value)
			}
		}
	}
	return named, nil
}

// defaultAppearance returns the default appearance string of the widget, its
// own /DA or the one it inherits from its field ancestors or the form (ISO
// 32000-1, 12.7.3.3, Table 222), or "" when none is a string. The /Parent walk
// is bounded by [maxFieldTreeDepth].
func defaultAppearance(ctx context.Context, doc *document.Document, widget, form ast.PdfDict) string {
	seen := make(map[int]bool)
	for depth, dict := 0, widget; dict != nil && depth <= maxFieldTreeDepth; depth++ {
		if _, ok := dict["DA"]; ok {
			da, err := document.ResolveString(ctx, doc, dict["DA"])
			if err != nil {
				return ""
			}
			return string(da)
		}
		var ok bool
		if _, dict, ok = document.ResolveFieldParent(ctx, doc, dict, seen); !ok {
			break
		}
	}
	da, err := document.ResolveString(ctx, doc, form["DA"])
	if err != nil {
		return ""
	}
	return string(da)
}
