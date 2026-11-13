// Package detmath computes the elementary functions with results that do
// not depend on the machine: the same input gives the same bits on amd64,
// arm64, 386 and arm, whatever the CPU's features.
//
// IEEE 754 rounds addition, subtraction, multiplication, division and
// square root correctly, so those agree everywhere. Two things do not:
//
//   - Go may fuse a product with the addition or subtraction that uses it
//     into one instruction that rounds once (The Go Programming Language
//     Specification, "Arithmetic operators"). The compiler does so on arm64
//     and on amd64 built for GOAMD64=v3, and not on amd64 v1, 386 or arm.
//     The math package's pure-Go functions are compiled that way too.
//   - The math package runs Exp, Log and Hypot in assembly on amd64, Exp
//     in assembly on arm64 and Hypot on 386, each by an algorithm of its
//     own; amd64's Exp takes a second path with fused instructions on a CPU
//     that has AVX and FMA.
//
// Each function here is the algorithm the math package runs on amd64
// without AVX (pure Go for the trigonometric functions, Pow, Cbrt and Erf;
// the assembly's for Exp, Log and Hypot), with every product that meets an
// addition wrapped in an explicit conversion, which the specification
// says rounds and so blocks the fusion; a function that returns a product
// rounds it too, so that a caller the compiler inlines it into cannot fuse
// it with a sum of its own. On that machine every function
// returns the bits of its math counterpart; elsewhere it returns those
// same bits, where math differs by an ulp or a few.
//
// The algorithms are transcribed from the Go standard library, under the
// BSD license reproduced in LICENSE-go.
package detmath
