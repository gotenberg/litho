package postscript

import "errors"

// Sentinel errors for PostScript Type 4 function evaluation.
var (
	// ErrStackUnderflow indicates the stack did not have enough operands
	// for the requested operation.
	ErrStackUnderflow = errors.New("postscript: stack underflow")

	// ErrDivisionByZero indicates a division or modulo by zero.
	ErrDivisionByZero = errors.New("postscript: division by zero")

	// ErrUnknownOperator indicates an unrecognized PostScript operator.
	ErrUnknownOperator = errors.New("postscript: unknown operator")

	// ErrSyntax indicates a syntax error in the PostScript program.
	ErrSyntax = errors.New("postscript: syntax error")

	// ErrStackOverflow indicates the operand stack grew past
	// [maxStackDepth] entries. ISO 32000-1, 7.10.5.2 lists it among the
	// errors a conforming reader shall detect.
	ErrStackOverflow = errors.New("postscript: stack overflow")

	// ErrBlockTooDeep indicates the program nests { } blocks more than
	// [maxBlockDepth] levels deep.
	ErrBlockTooDeep = errors.New("postscript: blocks nested too deep")
)
