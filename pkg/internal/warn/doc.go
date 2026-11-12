// Package warn declares the codes of the warnings a conversion reports: one
// closed set, each code named by what happened to which kind of construct,
// whichever converter met it. Nothing else declares a code, and a code
// cannot be made outside this package, so a converter reports no code
// outside the set. It declares the effects the same way: what a warning's
// construct cost the output, content left out or content printed
// differently, which every warning states beside its code. pkg/pdf mirrors
// every code and every effect as a public constant. It imports no other
// litho package.
package warn
