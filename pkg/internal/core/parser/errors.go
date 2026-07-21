package parser

import "errors"

// errUnexpectedKeyword marks a bare keyword token found where an object was
// expected. A space inside a name produces one: `/AASGAA+Arial,Unicode MS`
// lexes as the name `/AASGAA+Arial,Unicode` followed by the keyword `MS`.
// Dictionary parsing recovers by skipping the stray token rather than
// discarding the whole object, matching the leniency of qpdf and veraPDF.
var errUnexpectedKeyword = errors.New("unexpected keyword")
