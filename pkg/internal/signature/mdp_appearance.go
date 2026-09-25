package signature

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
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
// following every reference through objects [appearanceOf] accepts. An object
// it refuses is neither returned nor walked.
//
// Each object is resolved and walked once, so the walk ends on a cycle and its
// work is linear in the objects doc defines. Direct objects are walked with an
// explicit stack, so their nesting costs no recursion.
func drawnObjects(ctx context.Context, doc *document.Document, annots []int) (map[int]bool, error) {
	reached := make(map[int]bool)
	var stack []ast.PdfObject
	for _, num := range annots {
		annot, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: num})
		if err != nil || annot == nil {
			continue
		}
		if ap, ok := annot["AP"]; ok {
			stack = append(stack, ap)
		}
	}

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
