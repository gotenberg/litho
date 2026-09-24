// Package function evaluates the PDF functions of ISO 32000-1, 7.10: sampled
// (type 0), exponential (type 2) and PostScript calculator (type 4). Stitching
// (type 3) is refused with [ErrUnsupportedType]. Type 4 programs are executed
// by [github.com/gotenberg/litho/pkg/internal/graphics/postscript].
//
// A function is compiled once with [New] and evaluated many times, so a caller
// building a lookup table pays the parse and the stream decode once.
package function
