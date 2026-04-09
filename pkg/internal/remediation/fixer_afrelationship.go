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

func matchAFRelationship(v compliance.Violation) bool {
	return v.Rule.Clause == "6.8"
}

func fixAFRelationship(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	namesObj, ok := catalogDict["Names"]
	if !ok {
		return nil, nil
	}
	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil || namesDict == nil {
		return nil, nil
	}

	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return nil, nil
	}

	count := fixAFInNameTree(ctx, doc, alloc, efObj)
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("added /AFRelationship /Unspecified to %d file specs", count)}, nil
}

func fixAFInNameTree(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, obj ast.PdfObject) int {
	d, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || d == nil {
		return 0
	}

	count := 0

	// Process /Names array (leaf node): [name1, ref1, name2, ref2, ...]
	if namesArr, ok := d["Names"]; ok {
		if arr, err := document.ResolveArray(ctx, doc, namesArr); err == nil {
			for i := 1; i < len(arr); i += 2 {
				count += fixFileSpecAFRelationship(ctx, doc, alloc, arr[i])
			}
		}
	}

	// Recurse into /Kids (intermediate nodes).
	if kidsArr, ok := d["Kids"]; ok {
		if arr, err := document.ResolveArray(ctx, doc, kidsArr); err == nil {
			for _, kid := range arr {
				count += fixAFInNameTree(ctx, doc, alloc, kid)
			}
		}
	}

	return count
}

func fixFileSpecAFRelationship(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, obj ast.PdfObject) int {
	var ref ast.PdfIndirectRef
	if r, ok := obj.(ast.PdfIndirectRef); ok {
		ref = r
	}

	fsDict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || fsDict == nil {
		return 0
	}

	if _, ok := fsDict["AFRelationship"]; ok {
		return 0 // already present
	}

	newFS := make(ast.PdfDict, len(fsDict)+1)
	maps.Copy(newFS, fsDict)
	newFS["AFRelationship"] = ast.PdfName("Unspecified")

	if ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newFS)
	}
	return 1
}
