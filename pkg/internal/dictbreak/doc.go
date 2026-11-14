// Package dictbreak finds the word boundaries of text written without
// spaces between its words: Thai, which UAX #14 gives the line break
// class SA, complex context dependent, whose breaks its rules leave to a
// language-specific analysis (LB1).
//
// A [Dictionary] is a word list packed as a minimal acyclic automaton,
// built by a generator. [Thai] divides a run of Thai letters into words
// with one, the way ICU's dictionary break engine for Thai divides it
// (ICU User Guide, Boundary Analysis, Dictionary-Based BreakIterator):
// it prefers the word that lets the most of the next three words match
// the dictionary, folds the letters no word matches into the word before
// them or into a word of their own, keeps a mark with the letter it
// sits on, and keeps the repetition and abbreviation marks with the word
// they follow. That engine is what Chromium breaks Thai lines with.
//
// The package holds no word list of its own unless its build carries
// the generated one: [Thai] then finds no boundary.
package dictbreak
