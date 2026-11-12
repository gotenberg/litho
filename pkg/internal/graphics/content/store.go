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

// operationList collects the operations of one parse.
type operationList struct {
	ops []Operation
}

// reservedOperations returns the operations a stream of n bytes is expected
// to hold ([contentBytesPerOperation]), within [maxReservedOperations].
func reservedOperations(n int) int {
	return min(max(n/contentBytesPerOperation, 16), maxReservedOperations)
}

// newOperationList returns a list sized for the operations a stream of n
// bytes is expected to hold.
func newOperationList(n int) operationList {
	return operationList{ops: make([]Operation, 0, reservedOperations(n))}
}

// add appends op and returns where the list holds it.
func (l *operationList) add(op Operation) *Operation {
	l.ops = append(l.ops, op)
	return &l.ops[len(l.ops)-1]
}

// finish returns the operations collected. A list that reserved more than a
// quarter beyond what it holds is copied to its length, so that the
// operations a caller keeps hold no more room than append would have left
// them.
func (l *operationList) finish() []Operation {
	if slack := cap(l.ops) - len(l.ops); slack > len(l.ops)/4+16 {
		return append(make([]Operation, 0, len(l.ops)), l.ops...)
	}
	return l.ops
}

// operandStore holds the operands of one parse in shared blocks, rather than
// in one slice an operation grows by append. The operands of an operation are
// one run of a block, and [operandStore.take] hands them over as a slice whose
// capacity ends at its length: an append to it copies, and a write to one of
// its elements writes that operation's operand only, as it would to a slice of
// its own.
//
// The blocks hold interfaces, never the bytes of the stream: an operand the
// parser read is a value of its own, so the operations live as long as their
// caller keeps them, whatever becomes of the stream they were read from. An
// operation a caller keeps keeps its block, and the other operations' operands
// the block holds, alive; the operations of a parse are kept or dropped
// together by every reader of them.
type operandStore struct {
	block []ast.PdfObject
	// start is the index in block of the first operand of the operation being
	// read.
	start int
	// next is the capacity of the next block.
	next int
}

// newOperandStore returns a store whose first block is sized for the operands
// a stream of n bytes is expected to hold ([contentBytesPerOperand]).
func newOperandStore(n int) operandStore {
	return operandStore{next: min(max(n/contentBytesPerOperand, minOperandBlock), maxOperandBlock)}
}

// add stores obj as the next operand of the operation being read.
func (s *operandStore) add(obj ast.PdfObject) {
	if len(s.block) == cap(s.block) {
		pending := s.block[s.start:]
		size := max(s.next, 2*len(pending))
		block := make([]ast.PdfObject, len(pending), size)
		copy(block, pending)
		// The pending operands move to the new block: the old one, kept alive
		// by the operations stored in it, does not keep them too.
		clear(pending)
		s.block, s.start = block, 0
		s.next = min(2*s.next, maxOperandBlock)
	}
	s.block = append(s.block, obj)
}

// pending returns how many operands the operation being read holds so far.
func (s *operandStore) pending() int {
	return len(s.block) - s.start
}

// take returns the operands of the operation being read, nil when it has none,
// and starts the next operation.
func (s *operandStore) take() []ast.PdfObject {
	end := len(s.block)
	if end == s.start {
		return nil
	}
	operands := s.block[s.start:end:end]
	s.start = end
	return operands
}

// drop discards the operands of the operation being read.
func (s *operandStore) drop() {
	clear(s.block[s.start:])
	s.block = s.block[:s.start]
}
