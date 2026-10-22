// Package panicerr holds the sentinel a recovered panic wraps, and the error a
// worker goroutine returns for one. It imports no other litho package, so a
// codec below the document, which cannot import the document without a cycle,
// recovers its workers' panics into the same sentinel the document re-exports.
package panicerr
