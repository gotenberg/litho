// Package writer provides the low-level primitives for emitting valid PDF
// byte sequences: object serialization, cross-reference table/stream writing,
// and object number allocation. The incremental update engine and the full
// rewrite engine use these primitives to produce modified PDF files.
package writer
