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
