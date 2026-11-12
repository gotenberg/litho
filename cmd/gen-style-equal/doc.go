// Command gen-style-equal generates the comparison and the hash of the
// computed style of pkg/internal/css.
//
// The cascade shares a computed style between the elements styled alike
// (css.Styles): it hashes each style it computes and compares it with the
// styles of that hash. Both must read every field of css.Style, or two
// styles that differ would share one. The generator type-checks the css
// package from its source, walks css.Style and every struct type of the
// package it holds, and writes, for each, a function that compares two
// values field by field and one that folds a value into a hash. It reads
// each field by its type: numbers bit for bit, strings and the elements of
// slices by content, a nil slice apart from an empty one, pointers by the
// address they hold, and a map, func, channel or interface as equal to
// another only when both are nil. It refuses a field of a struct type of
// another package or of an unnamed struct type, which it cannot read.
//
// A test of the css package mutates every field of css.Style in turn and
// fails when the comparison misses one, and a test of this command fails
// when the committed file differs from what the generator writes.
//
// Usage:
//
//	go generate ./pkg/internal/css/...
//	go run ./cmd/gen-style-equal --dir pkg/internal/css --out pkg/internal/css/equal_gen.go
package main
