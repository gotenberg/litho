package content

import "github.com/gotenberg/litho/pkg/internal/core/ast"

// contentBytesPerOperation is the content bytes [parseOperations] expects an
// operation to take, from which it sizes the operations it reserves for a
// stream. A stream that takes fewer grows the slice as append grows it, and
// one that takes more has its slack cut off when the parse ends
// ([operationList.finish]).
//
// # Corpus evidence
//
//   - govdocs/pdf thread_100_100171.pdf: 40 of its text pages hold 540,285
//     bytes and 50,400 operations, 10.7 bytes an operation.
const contentBytesPerOperation = 10

// maxReservedOperations caps the operations [parseOperations] reserves before
// it reads any. Anti-DoS guard, not spec-mandated: the reservation follows the
// stream's length, and a stream that is mostly inline image data or white
// space would otherwise reserve room for operations it does not hold. 65,536
// operations take 2.5 MiB; a stream that holds more grows the slice as it
// reads them.
const maxReservedOperations = 1 << 16

// contentBytesPerOperand is the content bytes [operandStore] expects an
// operand to take, from which it sizes the first block it stores operands in.
//
// # Corpus evidence
//
//   - govdocs/pdf thread_100_100171.pdf: 40 of its text pages hold 540,285
//     bytes and 70,410 operands, 7.7 bytes an operand.
const contentBytesPerOperand = 8

// minOperandBlock and maxOperandBlock bound the operands one block of
// [operandStore] holds. Anti-DoS guard, not spec-mandated: a block is sized
// from the stream's length before the parse reads any operand, and the upper
// bound keeps a stream that holds few operands from reserving room for many;
// 4,096 operands take 64 KiB. The lower bound keeps a short stream from
// allocating a block for each operand.
const (
	minOperandBlock = 8
	maxOperandBlock = 1 << 12
)

// minInterningBytes is the shortest stream whose parse shares the texts and
// the objects of the operands it repeats ([lexer.Lexer.WithInterning],
// [parser.Parser.WithBoxedOperands]). Their tables take about 12 KiB, which a
// shorter stream does not repeat enough operands to pay back.
//
// # Corpus evidence
//
//   - govdocs/pdf thread_100_100171.pdf, thread_400_400760.pdf, pdfjs pdf.pdf,
//     issue18801.pdf: the text of every page, the CMYK conversion of the
//     second and the flattening of the last allocate within 1% of the same
//     objects and 2% of the same bytes whether streams from 0, 256, 1,024 or
//     4,096 bytes share their operands, save the flattening, which 256 bytes
//     saves 2.7% of its objects for 4 MiB more allocated.
const minInterningBytes = 4096

// reservedOperations returns the operations a stream of n bytes is expected
// to hold ([contentBytesPerOperation]), within [maxReservedOperations].
func reservedOperations(n int) int {
	return min(max(n/contentBytesPerOperation, 16), maxReservedOperations)
}

// operationList collects the operations of one parse and their operands.
//
// The operands are stored in shared blocks rather than in one slice each
// operation grows by append. The operands of an operation are one run of a
// block, which the operation holds as a slice whose capacity ends at its
// length: an append to it copies, and a write to one of its elements writes
// that operation's operand only, as it would to a slice of its own.
//
// The blocks hold interfaces, never the bytes of the stream: an operand the
// parser read is a value of its own, so the operations live as long as their
// caller keeps them, whatever becomes of the stream they were read from. An
// operation a caller keeps holds its block, and the other operations' operands
// the block holds, alive; the operations of a parse are kept or dropped
// together by every reader of them.
type operationList struct {
	ops []Operation
	// block holds the operands of the operations from blockOps on, and those
	// of the operation being read from start on.
	block    []ast.PdfObject
	start    int
	blockOps int
	// next is the capacity of the next block.
	next int
}

// newOperationList returns a list sized for the operations and the operands
// a stream of n bytes is expected to hold ([contentBytesPerOperation],
// [contentBytesPerOperand]).
func newOperationList(n int) operationList {
	return operationList{
		ops:  make([]Operation, 0, reservedOperations(n)),
		next: min(max(n/contentBytesPerOperand, minOperandBlock), maxOperandBlock),
	}
}

// addOperand stores obj as the next operand of the operation being read.
func (l *operationList) addOperand(obj ast.PdfObject) {
	if len(l.block) == cap(l.block) {
		pending := l.block[l.start:]
		block := make([]ast.PdfObject, len(pending), max(l.next, 2*len(pending)))
		copy(block, pending)
		// The pending operands move to the new block: the old one, kept alive
		// by the operations stored in it, does not keep them too.
		clear(pending)
		l.block, l.start, l.blockOps = block, 0, len(l.ops)
		l.next = min(2*l.next, maxOperandBlock)
	}
	l.block = append(l.block, obj)
}

// pendingOperands returns how many operands the operation being read holds so
// far.
func (l *operationList) pendingOperands() int {
	return len(l.block) - l.start
}

// dropOperands discards the operands of the operation being read.
func (l *operationList) dropOperands() {
	clear(l.block[l.start:])
	l.block = l.block[:l.start]
}

// add appends the operation of operator over the operands read since the last
// one, none when there are none, and returns where the list holds it.
func (l *operationList) add(operator string) *Operation {
	var operands []ast.PdfObject
	if end := len(l.block); end > l.start {
		operands = l.block[l.start:end:end]
		l.start = end
	}
	l.ops = append(l.ops, Operation{Operator: operator, Operands: operands})
	return &l.ops[len(l.ops)-1]
}

// finish drops the operands no operator took and returns the operations
// collected. Room reserved beyond a quarter of what was used is given back:
// the operations are copied to their number, and the operands of the last
// block to theirs, so that the operations a caller keeps hold no more room
// than append would have left them.
func (l *operationList) finish() []Operation {
	l.dropOperands()
	if used := len(l.block); cap(l.block)-used > used/4+minOperandBlock {
		block := make([]ast.PdfObject, used)
		copy(block, l.block)
		// Every operation from blockOps on holds the next run of the block.
		off := 0
		for i := l.blockOps; i < len(l.ops); i++ {
			if n := len(l.ops[i].Operands); n > 0 {
				l.ops[i].Operands = block[off : off+n : off+n]
				off += n
			}
		}
		l.block = block
	}
	if slack := cap(l.ops) - len(l.ops); slack > len(l.ops)/4+16 {
		return append(make([]Operation, 0, len(l.ops)), l.ops...)
	}
	return l.ops
}
