package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchLZW(v compliance.Violation) bool {
	return v.Rule.Clause == "6.1.10"
}

func fixLZW(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	var count int

	for objNum, entry := range doc.Index.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Free {
			continue
		}

		ref := ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation}
		resolved, err := doc.Resolve(ctx, ref)
		if err != nil {
			continue
		}

		stream, ok := resolved.(ast.PdfStream)
		if !ok || stream.Dict == nil {
			continue
		}

		filterObj, ok := stream.Dict["Filter"]
		if !ok {
			continue
		}

		isLZW := false
		switch f := filterObj.(type) {
		case ast.PdfName:
			isLZW = f == "LZWDecode" || f == "LZW"
		case ast.PdfArray:
			if len(f) == 1 {
				if name, ok := f[0].(ast.PdfName); ok {
					isLZW = name == "LZWDecode" || name == "LZW"
				}
			}
		}

		if !isLZW {
			continue
		}

		decoded, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			continue
		}

		var compressed bytes.Buffer
		w := zlib.NewWriter(&compressed)
		if _, err := w.Write(decoded); err != nil {
			continue
		}
		if err := w.Close(); err != nil {
			continue
		}

		newDict := make(ast.PdfDict, len(stream.Dict))
		maps.Copy(newDict, stream.Dict)
		newDict["Filter"] = ast.PdfName("FlateDecode")
		delete(newDict, "DecodeParms")
		newDict["Length"] = ast.PdfInt(compressed.Len())

		alloc.AllocAt(objNum, ast.NewStream(newDict, compressed.Bytes()))
		count++
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("re-encoded %d LZW streams as FlateDecode", count)}, nil
}
