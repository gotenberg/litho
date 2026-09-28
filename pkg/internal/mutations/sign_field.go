package mutations

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxFieldAncestors bounds the /Parent chain read to find a field's inherited
// field type. Anti-DoS guard, not spec-mandated: ISO 32000-1, 12.7.3.1 makes
// the fields a tree, whose depth real forms keep to a handful of levels, and
// the chain is also cut at the first object it revisits.
const maxFieldAncestors = 64

// namedField is a field of the interactive form, by reference and as read.
type namedField struct {
	ref  ast.PdfIndirectRef
	dict ast.PdfDict
}

// signatureField returns the unsigned signature field whose fully qualified
// name is name, reached from the form's root fields, or a zero namedField when
// no field has the name and a new root field can take it. An empty name
// returns a zero namedField.
//
// Any other field of that name is refused with [ErrSignatureFieldUnavailable]:
// two fields with one fully qualified name are one field (ISO 32000-1,
// 12.7.3.2), so the signature cannot go in a field of its own under it. So is
// a signature field that holds a signature already (12.7.4.5), and one whose
// /Lock or /SV (Table 232) asks the signature to lock fields or to meet seed
// values, which the signature dictionary litho writes does not honour. A name
// no field has is refused when it holds a period, since the new field is a
// root field and a partial name holds none (12.7.3.2), and when a widget the
// form does not list carries it ([pageWidgetFieldNames]).
//
// The walk visits each indirect field and /Kids array once, as
// [signatureFieldName] does, so a cycle ends it.
func signatureField(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fields ast.PdfArray, name string) (namedField, error) {
	if name == "" {
		return namedField{}, nil
	}
	type pending struct {
		obj    ast.PdfObject
		prefix string
	}
	seen := make(map[int]bool)
	firstVisit := func(obj ast.PdfObject) bool {
		ref, ok := obj.(ast.PdfIndirectRef)
		if !ok {
			return true
		}
		if seen[ref.ObjectNum] {
			return false
		}
		seen[ref.ObjectNum] = true
		return true
	}

	stack := make([]pending, 0, len(fields))
	for _, f := range slices.Backward(fields) {
		stack = append(stack, pending{obj: f})
	}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return namedField{}, fmt.Errorf("finding signature field %q: %w", name, err)
		}
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !firstVisit(top.obj) {
			continue
		}
		field, err := resolveDictAllocFirst(ctx, alloc, doc, top.obj)
		if err != nil || field == nil {
			continue
		}
		qualified := top.prefix
		if t, ok := field["T"]; ok {
			if s, err := document.ResolveString(ctx, doc, t); err == nil && s != "" {
				if partial := document.DecodePdfString(s); qualified == "" {
					qualified = partial
				} else {
					qualified += "." + partial
				}
			}
		}
		if qualified == name {
			return usableSignatureField(ctx, alloc, doc, top.obj, field, name)
		}
		// Only a field whose name is a prefix of the one sought can hold it.
		if qualified != "" && !strings.HasPrefix(name, qualified+".") {
			continue
		}
		kidsObj, ok := field["Kids"]
		if !ok || !firstVisit(kidsObj) {
			continue
		}
		kids, err := resolveArrayAllocFirst(ctx, alloc, doc, kidsObj)
		if err != nil {
			continue
		}
		for _, kid := range slices.Backward(kids) {
			stack = append(stack, pending{obj: kid, prefix: qualified})
		}
	}

	if strings.Contains(name, ".") {
		return namedField{}, fmt.Errorf("%w: no field is named %q, and a new root field's partial name cannot hold a period (ISO 32000-1, 12.7.3.2)", ErrSignatureFieldUnavailable, name)
	}
	taken := make(map[string]bool)
	if err := pageWidgetFieldNames(ctx, alloc, doc, taken); err != nil {
		return namedField{}, err
	}
	if taken[name] {
		return namedField{}, fmt.Errorf("%w: a widget the form does not list is named %q", ErrSignatureFieldUnavailable, name)
	}
	return namedField{}, nil
}

// usableSignatureField returns the field named name, read as field from obj,
// when it is an unsigned signature field the signature can go in, and
// [ErrSignatureFieldUnavailable] otherwise (see [signatureField]).
func usableSignatureField(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, field ast.PdfDict, name string) (namedField, error) {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok {
		return namedField{}, fmt.Errorf("%w: field %q is a direct object an update cannot replace", ErrSignatureFieldUnavailable, name)
	}
	// ISO 32000-1, Table 220: /FT is inheritable, so a field that lacks it
	// takes its parent's.
	var fieldType ast.PdfName
	seen := make(map[int]bool)
	cur := ast.PdfObject(ref)
	for range maxFieldAncestors {
		r, isRef := cur.(ast.PdfIndirectRef)
		if isRef {
			if seen[r.ObjectNum] {
				break
			}
			seen[r.ObjectNum] = true
		}
		d, err := resolveDictAllocFirst(ctx, alloc, doc, cur)
		if err != nil || d == nil {
			break
		}
		if ft, err := document.ResolveName(ctx, doc, d["FT"]); err == nil && ft != "" {
			fieldType = ft
			break
		}
		parent, ok := d["Parent"]
		if !ok {
			break
		}
		cur = parent
	}
	if fieldType != "Sig" {
		return namedField{}, fmt.Errorf("%w: field %q is not a signature field", ErrSignatureFieldUnavailable, name)
	}
	if v, ok := field["V"]; ok {
		if _, isNull := v.(ast.PdfNull); !isNull {
			return namedField{}, fmt.Errorf("%w: field %q is signed already", ErrSignatureFieldUnavailable, name)
		}
	}
	if _, ok := field["Lock"]; ok {
		return namedField{}, fmt.Errorf("%w: field %q asks its signature to lock fields (/Lock, ISO 32000-1, Table 232), which litho does not do", ErrSignatureFieldUnavailable, name)
	}
	if sv, err := resolveDictAllocFirst(ctx, alloc, doc, field["SV"]); err == nil && sv != nil {
		// ISO 32000-1, Table 234: a set /Ff bit makes the matching seed value
		// a requirement.
		if ff, ok := sv["Ff"].(ast.PdfInt); ok && ff != 0 {
			return namedField{}, fmt.Errorf("%w: field %q requires seed values (/SV /Ff %d, ISO 32000-1, Table 234) litho does not check", ErrSignatureFieldUnavailable, name, ff)
		}
	}
	return namedField{ref: ref, dict: field}, nil
}
