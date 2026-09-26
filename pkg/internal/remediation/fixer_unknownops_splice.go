package remediation

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// spliceCheckInterval is how many operations [spliceOperations] reads
// between two checks of its context: each one it keeps is read a second time
// on its own ([readsAsRepaired]).
const spliceCheckInterval = 100

// operatorRunDelimiters are the delimiters veraPDF reads into the operator
// written against them, "re%" and "Q)" each one operator ISO 32000-1 does not
// define ([content.ParseStreamData]), and which begin nothing where an
// operator stands: a stray ")", ">", "{" or "}" is itself such an operator
// (see [delimitedOperators]).
const operatorRunDelimiters = ")>{}%"

// opSpliceTally counts what [spliceOperations] changed: the malformed
// numbers it folded, the operators it separated and removed, as
// [repairContentData] counts them, the rendering intents it replaced, as
// [fixContentStreams] counts them, and the edits of the repair of another
// fixer, as that fixer counts them ([repairContentOps]).
type opSpliceTally struct {
	folded, separated, removed, intents, edits int
}

func (t *opSpliceTally) add(o opSpliceTally) {
	t.folded += o.folded
	t.separated += o.separated
	t.removed += o.removed
	t.intents += o.intents
	t.edits += o.edits
}

// operationRepair is the repair [spliceOperations] writes. fold reports
// whether a malformed number folds into the operator after it
// ([repairMalformedNumbers]), and op returns the operations one operation of
// the reading of a repair is written as, those numbers folded into its
// operands, what it changed, and false when no splice of it can stand.
type operationRepair struct {
	fold bool
	op   func(content.Operation) ([]content.Operation, opSpliceTally, bool)
}

// spliceRepairedOperations repairs a content stream that holds an inline image
// as [repairContentData] repairs any other, and writes the repairs in place of
// the operations they change rather than serializing the whole stream again
// ([spliceOperations]). It returns the bytes to write, what it changed, and
// false when the splice cannot stand, which leaves the stream to the repairs
// that splice tokens.
func spliceRepairedOperations(ctx context.Context, data []byte) ([]byte, opSpliceTally, bool) {
	return spliceOperations(ctx, data, operationRepair{fold: true, op: repairOperation})
}

// spliceOperations writes the operations repair changes in a content stream
// that holds an inline image in place of the operations they change, rather
// than serializing the whole stream again. It returns the bytes to write, what
// it changed, and false when the splice cannot stand, which leaves the stream
// as it was.
//
// Serializing the parsed operations cannot write an inline image back: the
// parser reads its data off-token, so the BI operation keeps its dictionary
// alone (ISO 32000-1, 8.9.7). Each operation the reading of a repair
// ([content.ForRepair]) returns spans the bytes from the end of the one before
// it to the end of its operator ([content.ParseStreamDataWithOffsets]), and
// every operation is written as follows:
//
//   - An inline image is carried across byte for byte, from its BI to its EI.
//     What stands before its BI is written as a space when it holds anything
//     validation reads as an operator, or the malformed numbers of a run that
//     BI, which takes no operand, drops.
//   - An operation the repair changes is written again from its repaired
//     operations ([serializeOps]), after a space. The repair of the
//     undefined-operator fixer ([repairOperation]) changes a malformed number
//     folded into the operator after it ([repairMalformedNumbers]), a run of
//     operators written with no white space between them
//     ([separateGluedOperators]), and an operator ISO 32000-1 does not
//     define, EI and ID outside an inline image among them, which goes with
//     its operands ([dropUndefinedOperators]). The aggressive content rewrite
//     also changes an invalid rendering intent ([aggressiveOperation]), and
//     the fixers that rewrite a page's operations ([repairContentOps]) change
//     the ones their repair edits or drops.
//   - So is one whose bytes validation reads another way than the reading of a
//     repair ([readsAsRepaired]), such as a stray delimiter where an operator
//     stands, which the reading of a repair drops, or one that begins with a
//     delimiter veraPDF reads into the operator before it
//     ([operatorRunDelimiters]).
//   - Any other operation keeps its bytes.
//
// The bytes after the last operation are kept unless validation reads an
// operator in them, and a run of malformed numbers no operator takes is
// dropped, as the serialized stream drops them.
//
// The spliced stream is read back and must give the operations the serialized
// stream would, in which every operation kept is the operation it was, and
// every one written again reads as written: a splice that moved a token
// boundary between two operations fails, and the stream is left as it was.
func spliceOperations(ctx context.Context, data []byte, repair operationRepair) ([]byte, opSpliceTally, bool) {
	var t opSpliceTally
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.ForRepair())
	if err != nil || len(ops) != len(ranges) {
		return nil, t, false
	}

	var out bytes.Buffer
	out.Grow(len(data))
	// want holds the operations the serialized stream would hold, and exp
	// the ones the spliced stream must read as.
	var want, exp []content.Operation
	group := -1
	var pending []ast.PdfObject
	for i, op := range ops {
		if i%spliceCheckInterval == 0 && ctx.Err() != nil {
			return nil, t, false
		}
		if repair.fold && malformedNumber(op.Operator) {
			if group < 0 {
				group = i
			}
			pending = append(pending, op.Operands...)
			pending = append(pending, ast.PdfReal(numericPrefix(op.Operator)))
			t.folded++
			continue
		}
		start := ranges[i].Start
		folds := group >= 0
		if folds {
			start = ranges[group].Start
		}
		group = -1
		merged := op
		if len(pending) > 0 {
			merged.Operands = slices.Concat(pending, op.Operands)
			pending = nil
		}

		if op.Operator == "BI" {
			text, ok := inlineImageSplice(ctx, data, start, ranges[i].Start, ranges[i].End, folds)
			if !ok {
				return nil, t, false
			}
			out.WriteString(text)
			want = append(want, op)
			exp = append(exp, op)
			continue
		}

		repaired, changed, ok := repair.op(merged)
		if !ok {
			return nil, t, false
		}
		t.separated += changed.separated
		t.removed += changed.removed
		t.intents += changed.intents
		t.edits += changed.edits
		want = append(want, repaired...)
		span := data[start:ranges[i].End]
		if !folds && changed == (opSpliceTally{}) && !startsOperatorRun(data, start) && readsAsRepaired(ctx, span, op) {
			out.Write(span)
			exp = append(exp, op)
			continue
		}
		text, err := serializeOps(repaired)
		if err != nil {
			return nil, t, false
		}
		reread, err := content.ParseStreamData(ctx, text, content.ForRepair())
		if err != nil {
			return nil, t, false
		}
		out.WriteByte(' ')
		out.Write(text)
		exp = append(exp, reread...)
	}

	tailStart := 0
	if len(ranges) > 0 {
		tailStart = ranges[len(ranges)-1].End
	}
	if group >= 0 {
		// A trailing run with no operator to attach to invokes nothing,
		// and the serialized stream drops it with the bytes after it.
		tailStart = ranges[group].Start
	}
	tail := data[tailStart:]
	if group >= 0 || startsOperatorRun(data, tailStart) || readsAnOperator(ctx, tail) {
		out.WriteByte('\n')
	} else {
		out.Write(tail)
	}

	got, err := content.ParseStreamData(ctx, out.Bytes(), content.ForRepair())
	if err != nil || !sameOperations(got, exp) || !sameOperators(got, want) {
		return nil, t, false
	}
	return out.Bytes(), t, true
}

// opRepair is a fixer's repair of the operations of one content stream,
// handed to it one at a time in stream order. It returns the operations one
// operation is written as, the operation alone when it keeps it and none when
// it drops it, and how many edits it made there, as its fixer counts them. It
// may carry state from one operation to the next, such as the graphics state
// a q saves, so every reading of a stream takes a fresh one.
type opRepair func(content.Operation) ([]content.Operation, int)

// repairContentOps returns the bytes a fixer writes for a content stream's
// decoded data, whose operations, read as a repair reads them
// ([content.ForRepair]), go through a fresh repair of newRepair, and how many
// edits the repair made. No edit means nothing is to be written. An error
// leaves the stream as written.
//
// A stream that holds no inline image is serialized again from its repaired
// operations ([serializeOps]). One that holds an inline image cannot be: the
// parser reads an image's data off-token, so the BI operation keeps its
// dictionary alone, and writing it back would drop the data and leave
// "<< ... >> BI" with no ID or EI, a content stream no reader can parse (ISO
// 32000-1, 8.9.7). Its repairs are spliced in place of the operations they
// change instead, its images carried across byte for byte
// ([spliceOperations]), and a splice that does not read back as the repair
// intends fails with [errInlineImageSplice]: the stream keeps its bytes, and
// its finding stays a residual for validation to report.
func repairContentOps(ctx context.Context, data []byte, newRepair func() opRepair) ([]byte, int, error) {
	ops, err := content.ParseStreamData(ctx, data, content.ForRepair())
	if err != nil {
		return nil, 0, fmt.Errorf("reading the content stream: %w", err)
	}
	repair := newRepair()
	repaired := make([]content.Operation, 0, len(ops))
	edits, images := 0, false
	for i, op := range ops {
		if i%spliceCheckInterval == 0 && ctx.Err() != nil {
			return nil, 0, fmt.Errorf("repairing the content stream: %w", ctx.Err())
		}
		// The splice carries an image across whole and hands the repair
		// every other operation, so the count here is the one it makes.
		if beginsInlineImage(op) {
			images = true
			repaired = append(repaired, op)
			continue
		}
		written, n := repair(op)
		repaired = append(repaired, written...)
		edits += n
	}
	if edits == 0 {
		return nil, 0, nil
	}
	if !images {
		out, err := serializeOps(repaired)
		if err != nil {
			return nil, 0, fmt.Errorf("writing the repaired operations: %w", err)
		}
		return out, edits, nil
	}

	repair = newRepair()
	out, t, ok := spliceOperations(ctx, data, operationRepair{op: func(op content.Operation) ([]content.Operation, opSpliceTally, bool) {
		written, n := repair(op)
		return written, opSpliceTally{edits: n}, true
	}})
	if !ok || t.edits == 0 {
		return nil, 0, errInlineImageSplice
	}
	return out, t.edits, nil
}

// repairOperation returns the operations the repairs of [repairContentData]
// write for one operation of the reading of a repair, the malformed numbers
// before it folded into its operands: the operators of a run written with no
// white space between them apart ([separateGluedOperators]), and the ones ISO
// 32000-1 does not define removed ([dropUndefinedOperators]). It returns how
// many operators it separated and removed, and false for a run that holds a
// BI, which would begin an inline image no dictionary follows.
func repairOperation(op content.Operation) ([]content.Operation, opSpliceTally, bool) {
	split, separated := separateGluedOperators([]content.Operation{op})
	if slices.ContainsFunc(split, func(o content.Operation) bool { return o.Operator == "BI" }) {
		return nil, opSpliceTally{}, false
	}
	split, _, removed := dropUndefinedOperators(split)
	return filterOps(split), opSpliceTally{separated: separated, removed: removed}, true
}

// inlineImageSplice returns what [spliceOperations] writes for the
// inline image whose operation spans data[opStart:end]. folds reports that
// the bytes from start to opStart hold a run of malformed numbers, which BI
// drops. The image is carried across from its BI to its EI byte for byte, and
// what stands before its BI is written as a space when it is dropped or when
// validation reads an operator in it.
func inlineImageSplice(ctx context.Context, data []byte, start, opStart, end int, folds bool) (string, bool) {
	bi, ok := inlineImageStart(data[opStart:end])
	if !ok {
		return "", false
	}
	image := data[opStart+bi : end]
	if folds || startsOperatorRun(data, opStart) || readsAnOperator(ctx, data[opStart:opStart+bi]) {
		return " " + string(image), true
	}
	return string(data[start:end]), true
}

// inlineImageStart returns the offset in span, the bytes of a BI operation,
// of the BI keyword that begins its inline image: the first keyword BI where
// an operand or an operator stands.
func inlineImageStart(span []byte) (int, bool) {
	p := parser.NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(span), int64(len(span)), 0))).WithOffsetTracking()
	depth := 0
	for {
		tok, err := p.Peek(0)
		if err != nil || tok.Type == lexer.TokenError || tok.Type == lexer.TokenEOF {
			return 0, false
		}
		end := int(p.PeekEnd(0))
		if _, err := p.Consume(); err != nil {
			return 0, false
		}
		switch {
		case tok.Type == lexer.TokenDelimiter && (tok.Value == "[" || tok.Value == "<<"):
			depth++
		case tok.Type == lexer.TokenDelimiter && (tok.Value == "]" || tok.Value == ">>"):
			depth = max(depth-1, 0)
		case tok.Type == lexer.TokenKeyword && tok.Value == "BI" && depth == 0:
			return end - len("BI"), true
		}
	}
}

// startsOperatorRun reports whether the byte at pos, just past the operator of
// the operation before, is a delimiter veraPDF reads into that operator
// ([operatorRunDelimiters]).
func startsOperatorRun(data []byte, pos int) bool {
	return pos > 0 && pos < len(data) && strings.IndexByte(operatorRunDelimiters, data[pos]) >= 0
}

// readsAsRepaired reports whether validation reads span, the bytes of one
// operation, as the reading of a repair read op: one operation, with the same
// operator and operands ([content.ParseStreamData]).
func readsAsRepaired(ctx context.Context, span []byte, op content.Operation) bool {
	ops, err := content.ParseStreamData(ctx, span)
	return err == nil && len(ops) == 1 && sameOperation(ops[0], op)
}

// readsAnOperator reports whether validation reads an operator in b, the bytes
// between two operations or after the last one, which hold none in the reading
// of a repair.
func readsAnOperator(ctx context.Context, b []byte) bool {
	ops, err := content.ParseStreamData(ctx, b)
	return err != nil || len(ops) > 0
}

// sameOperations reports whether a and b hold the same operations, in order.
func sameOperations(a, b []content.Operation) bool {
	return slices.EqualFunc(a, b, sameOperation)
}

// sameOperators reports whether a and b hold the same operators, in order.
func sameOperators(a, b []content.Operation) bool {
	return slices.EqualFunc(a, b, func(x, y content.Operation) bool { return x.Operator == y.Operator })
}

// sameOperation reports whether a and b are the same operator with the same
// operands.
func sameOperation(a, b content.Operation) bool {
	return a.Operator == b.Operator && slices.EqualFunc(a.Operands, b.Operands, func(x, y ast.PdfObject) bool {
		return reflect.DeepEqual(x, y)
	})
}
