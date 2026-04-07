// Package writer provides the low-level primitives for emitting valid PDF
// byte sequences: object serialization, cross-reference table/stream writing,
// and object number allocation. These primitives are used by the incremental
// update engine and the full rewrite engine to produce modified PDF files.
package writer
