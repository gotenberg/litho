package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchSignatureStrip matches the signed-revision ByteRange coverage
// rules (ISO 19005-2/3 §6.4.3 t1). Any update after signing breaks the
// coverage, so a remediated signed document can only reach full
// compliance by dropping the now-invalidated signature.
func matchSignatureStrip(v compliance.Violation) bool {
	return v.Rule == compliance.Rule_19005_2_6_4_3_t01 || v.Rule == compliance.Rule_19005_3_6_4_3_t01
}

// maxSigFieldWalk bounds the form-field walk. Anti-DoS guard, not
// spec-mandated: real forms hold far fewer fields.
const maxSigFieldWalk = 10000

// fixSignatureStrip removes the /V signature value from signed
// signature fields. The field and its widget stay, so page layout is
// untouched, but the cryptographic signature is destroyed: this runs
// only in aggressive mode (PreserveContent == false). Standard mode
// keeps the honest §6.4.3 residual instead.
func fixSignatureStrip(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, _ string) ([]string, error) {
	if opts.PreserveContent {
		return nil, nil
	}
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	acroForm, err := document.ResolveDict(ctx, doc, catalogDict["AcroForm"])
	if err != nil || acroForm == nil {
		return nil, nil
	}
	fields, err := document.ResolveArray(ctx, doc, acroForm["Fields"])
	if err != nil {
		return nil, nil
	}

	count := 0
	seen := make(map[int]bool)
	var walk func(ast.PdfArray)
	walk = func(arr ast.PdfArray) {
		for _, fObj := range arr {
			if ctx.Err() != nil || len(seen) > maxSigFieldWalk {
				return
			}
			ref, isRef := fObj.(ast.PdfIndirectRef)
			if !isRef || seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
			field := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
			if field == nil {
				continue
			}
			if ft, _ := document.ResolveName(ctx, doc, field["FT"]); ft == "Sig" {
				if _, hasV := field["V"]; hasV {
					newField := make(ast.PdfDict, len(field))
					maps.Copy(newField, field)
					delete(newField, "V")
					alloc.AllocAt(ref.ObjectNum, newField)
					count++
				}
			}
			if kids, err := document.ResolveArray(ctx, doc, field["Kids"]); err == nil {
				walk(kids)
			}
		}
	}
	walk(fields)

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("removed %d invalidated signature value(s) (aggressive)", count)}, nil
}
