package parser

import "errors"

// errUnexpectedKeyword marks a bare keyword token found where an object was
// expected. A space inside a name produces one: `/AASGAA+Arial,Unicode MS`
// lexes as the name `/AASGAA+Arial,Unicode` followed by the keyword `MS`.
// Dictionary parsing recovers by skipping the stray token rather than
// discarding the whole object, matching the leniency of qpdf and veraPDF.
var errUnexpectedKeyword = errors.New("unexpected keyword")

// errUnexpectedDelimiter marks a stray delimiter token found where an object was
// expected. Damaged files carry them: an array closed twice leaves a bare `]`
// among a dictionary's keys, and a dictionary closed with a single `>` leaves
// that. Dictionary parsing skips it exactly as it skips a stray keyword, rather
// than discarding every key the dictionary holds.
var errUnexpectedDelimiter = errors.New("unexpected delimiter")

// errNestingTooDeep marks input whose object nesting passes
// [maxParseNestingDepth]. Refusing the object is the only available answer:
// the recursion is otherwise bounded by the goroutine stack alone, and a stack
// overflow is a fatal runtime error rather than a panic, so the recover
// boundary that every pkg/pdf entry point installs cannot convert it and the
// process dies.
var errNestingTooDeep = errors.New("object nesting too deep")

// ErrHexStringOddDigits marks a format violation recorded under
// [Parser.WithFormatChecking] for a hexadecimal string with an odd number of
// non-white-space characters (ISO 19005-2, 6.1.6 t1). The parser still reads
// the value, assuming a missing final digit is 0 (ISO 32000-1, 7.3.4.3).
var ErrHexStringOddDigits = errors.New("hex string has odd number of digits")

// ErrHexStringInvalidChar marks a format violation recorded under
// [Parser.WithFormatChecking] for a hexadecimal string holding a character
// that is neither a hexadecimal digit nor white space (ISO 19005-2, 6.1.6
// t2). The parser drops the character when it reads the value.
var ErrHexStringInvalidChar = errors.New("hex string contains invalid character")
