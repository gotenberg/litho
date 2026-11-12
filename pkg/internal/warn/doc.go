// Package warn declares the codes of the warnings a conversion reports: one
// closed set, each code named by what happened to which kind of construct,
// whichever converter met it. Nothing else declares a code, and a code
// cannot be made outside this package, so a converter reports no code
// outside the set. pkg/pdf mirrors every code as a public constant. It
// imports no other litho package.
package warn
