// Package ioat provides a buffered, stateful reader built on top of
// io.ReaderAt, allowing multiple goroutines to read from the same source
// concurrently without sharing a file pointer.
package ioat
