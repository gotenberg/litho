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

// matchLZW claims the LZWDecode ban in every spelling the parts give it.
//
// One requirement, four clause numbers: ISO 19005-1 spells it 6.1.10, ISO
// 19005-2 and 19005-3 spell the stream-filter half 6.1.7.2, and ISO 19005-4
// spells it 6.1.6.2. Only 6.1.10 was claimed, so a PDF/A-2, -3 or -4 target
// kept an LZWDecode stream that [fixLZW] already knew how to re-encode.
//
// Exact equality, never clausePrefix: "6.1.7" would swallow 6.1.7.1, the
// unrelated stream-length and stream-keyword-EOL rule that the full rewrite
// handles.
//
// 6.1.7.2 and 6.1.6.2 forbid more than LZWDecode: a filter absent from ISO
// 32000 Table 6, and a Crypt filter whose decode parameters name anything but
// Identity. [fixLZW] repairs neither, so a violation reporting one of those
// stays in the residual rather than being silenced.
//
// The inline-image spellings stay outside this matcher's reach on purpose:
// ISO 19005-4 6.1.9 names the Inline Image /F key, and rewriting a
// BI...ID...EI sequence is beyond what re-encoding stream objects can do.
func matchLZW(v compliance.Violation) bool {
	clause := v.Rule.Clause
	return clause == "6.1.10" || clause == "6.1.7.2" || clause == "6.1.6.2"
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
