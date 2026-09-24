// Package ioat provides a buffered, stateful reader built on top of
// [io.ReaderAt]. Each [Reader] instance maintains its own position and
// buffer, so it is NOT safe for concurrent use by multiple goroutines.
// For concurrent access, create separate Reader instances from the same
// io.ReaderAt. The underlying ReaderAt is position-independent and safe
// for concurrent reads.
package ioat
