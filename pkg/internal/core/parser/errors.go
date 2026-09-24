package parser

import (
	"errors"
	"fmt"
)

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

// ErrStreamFormat is the class of the stream format violations recorded under
// [Parser.WithFormatChecking]: [ErrStreamLengthMismatch],
// [ErrStreamKeywordEOL] and [ErrStreamMissingLength] each match it under
// [errors.Is]. ISO 32000-1, 7.3.8 states the stream syntax they check.
var ErrStreamFormat = errors.New("stream format violation")

// ErrStreamLengthMismatch marks a format violation recorded under
// [Parser.WithFormatChecking] for a stream whose /Length disagrees with the
// bytes before endstream (ISO 32000-1, 7.3.8.2; ISO 19005-2, 6.1.7.1 t1).
var ErrStreamLengthMismatch = fmt.Errorf("%w: /Length does not match the stream data", ErrStreamFormat)

// ErrStreamKeywordEOL marks a format violation recorded under
// [Parser.WithFormatChecking] for a stream keyword not followed by CR+LF or LF
// (ISO 32000-1, 7.3.8.1; ISO 19005-2, 6.1.7.1 t2).
var ErrStreamKeywordEOL = fmt.Errorf("%w: stream keyword not followed by CR+LF or LF", ErrStreamFormat)

// ErrStreamMissingLength marks a format violation recorded under
// [Parser.WithFormatChecking] for a stream dictionary with no /Length, an
// entry ISO 32000-1, 7.3.8.2, Table 5 requires.
var ErrStreamMissingLength = fmt.Errorf("%w: stream dictionary has no /Length", ErrStreamFormat)

// ErrNumberExponent marks a format violation recorded under
// [Parser.WithFormatChecking] for a real number written with an exponent,
// which the syntax of ISO 32000-1, 7.3.3 does not provide.
var ErrNumberExponent = errors.New("number uses scientific notation")
