package content

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// The sizes [RetainedBytes] charges, in bytes, for a 64-bit Go runtime: a
// string or slice header, an interface value, the boxed value an interface
// holds for a scalar, and one map entry's share of its buckets beyond the key
// and the value.
const (
	stringHeaderBytes = 16
	sliceHeaderBytes  = 24
	interfaceBytes    = 16
	boxedScalarBytes  = 8
	mapEntryOverhead  = 16
	mapHeaderBytes    = 48
)

// RetainedBytes estimates the bytes ops retains in memory: each operation, its
// operator and operands, and every array, dictionary and string an operand
// holds. It is an estimate for bounding a cache, not an exact count: it
// ignores allocator rounding and shared backing arrays.
//
// It walks nested operands with an explicit stack, so an operand nested as
// deep as the parser allows does not recurse.
func RetainedBytes(ops []Operation) int64 {
	n := int64(len(ops)) * (stringHeaderBytes + sliceHeaderBytes)
	var stack []ast.PdfObject
	for _, op := range ops {
		n += int64(len(op.Operator))
		stack = append(stack, op.Operands...)
		for len(stack) > 0 {
			obj := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			n += interfaceBytes
			switch v := obj.(type) {
			case ast.PdfName:
				n += stringHeaderBytes + int64(len(v))
			case ast.PdfString:
				n += stringHeaderBytes + int64(len(v))
			case ast.PdfArray:
				n += sliceHeaderBytes
				stack = append(stack, v...)
			case ast.PdfDict:
				n += mapHeaderBytes
				for k, val := range v {
					n += mapEntryOverhead + stringHeaderBytes + int64(len(k))
					stack = append(stack, val)
				}
			default:
				n += boxedScalarBytes
			}
		}
	}
	return n
}

// ReadsWholeStreams reports whether ctx carries [WithWholeStreams], under
// which a parse reads a stream other than it does without it. A caller that
// caches a parse keys it on this too.
func ReadsWholeStreams(ctx context.Context) bool {
	whole, _ := ctx.Value(wholeStreamsKey{}).(bool)
	return whole
}
