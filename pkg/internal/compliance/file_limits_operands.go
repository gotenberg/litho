package compliance

// OperandLink says which of the objects one operand of a content-stream
// operator stands for veraPDF reaches, and so which of them the implementation
// limits judge. Its flags combine: the last operand of g reaches a number or a
// name, whichever it is.
type OperandLink uint8

const (
	// LinkNumber reaches the operand when it is an integer or a real.
	LinkNumber OperandLink = 1 << iota
	// LinkInteger reaches the operand when it is an integer.
	LinkInteger
	// LinkName reaches the operand when it is a name.
	LinkName
	// LinkString reaches the operand when it is a string.
	LinkString
	// LinkArray reaches the operand whole when it is an array: every string,
	// name, integer and real it holds, in arrays and dictionaries nested at
	// any depth, dictionary keys included.
	LinkArray
	// LinkDict reaches the operand whole when it is a dictionary, as
	// [LinkArray] reaches an array.
	LinkDict
	// LinkArrayNumbers reaches the integers and reals an array operand holds
	// as its own elements, and nothing an array nested in it holds.
	LinkArrayNumbers
)

// ContentOperandLink returns what veraPDF reaches of the operand at index of
// the n operands an operation of operator takes: the objects the implementation
// limits judge there. colour reports whether the stream processes its colour
// operators at that operation ([ColourState]). An operator ISO 32000-1, Annex
// A does not define reaches nothing, nor does an index outside the operands.
//
// ISO 19005-1, 6.1.12 and ISO 19005-2/3, 6.1.13 bound every integer, real,
// string and name a conforming file contains. Deliberate leniency toward
// veraPDF, the reference validator of ISO 19005, which judges the objects of a
// content stream through the operators that use them alone (veraPDF-validation
// 1.30, the classes of org.verapdf.gf.model.impl.operator): each operator reads
// the operands it needs from the end of its operands, and the ones before them,
// or of another type, are no object of its model. The table mirrors those
// classes:
//
//   - w, M, i, Tc, Tw, TL, Ts and Tz read their last operand as a number
//     (GFOperator.getLastNumber), and J and j as an integer
//     (GFOperator.getLastInteger), so a real there is reached by none.
//   - Tf reads the font name before its size, d the dash array before its
//     phase, a whole array, and Td, TD and d0 the two numbers they end with.
//     " reads the two numbers before its string, Tj and ' their string, TJ its
//     array whole.
//   - c, cm, d1, l, m, re, Tm, v and y read every operand that is a number,
//     and every element of an array operand (GFOperator.getListOfNumbers).
//   - g, G, rg, RG, k, K, cs, CS, scn and SCN read every operand that is a
//     number and a last operand that is a name (GFOpSetColor.getColorValues,
//     GFOpColor.getPatternName), and sc and SC the numbers alone, all of them
//     only while the stream processes its colour operators.
//   - BMC, MP and ri read the name they end with, BDC and DP the tag before
//     their property list and the list itself when it is a dictionary, whole.
//     BI reads its dictionary whole (GFOp_ID), when the stream reads the
//     image at all ([ColourState.ReadsInlineImage]).
//   - Every other operator, q, Q, BT, ET, n, the painting and clipping
//     operators, T*, Tr, EMC, BX, EX, gs, Do and sh, reads none.
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on one-page fixtures, an
// integer past the limit fails ISO 19005-1, 6.1.12 t1 and ISO 19005-2, 6.1.13
// t1 where the table reaches it and nowhere else: before q, Q, BT, ET, T*, Tr,
// n, S, f, W, h, EMC, BX, gs, Do, sh or ri, as an extra operand before w, J,
// Td, ", Tj, ', TJ, Tf, d, BMC, MP, BDC or DP, in an array before w, g or cs,
// in a dictionary before w, m or BMC, and as a real given to J, it fails
// nothing, while every number operand of m, cm, g and cs fails it, the array
// elements of m too. A string past the limit fails ISO 19005-2, 6.1.13 t3
// shown by Tj, TJ and ", and in a dash array, and fails nothing before w, m,
// BMC or an undefined operator, or as an extra operand before Tj. A 128-byte
// name fails ISO 19005-1, 6.1.12 t4 and ISO 19005-2, 6.1.13 t4 given to Tf,
// ri, BMC, MP, cs, CS, g, scn and SCN, as the tag of BDC and DP, as a
// key or a value of their property list or of an inline image's dictionary,
// and in a dash array, and fails nothing given to sc, gs, Do or sh, as an
// extra operand before Tf, BMC or cs, or before q, w or an undefined operator.
// veraPDF cannot validate a stream whose m takes an array holding an array: it
// stops on the element that is no number (GFOperator.getListOfNumbers).
//
// # Corpus evidence
//
//   - All corpora: judging an operand only where its operator reads it, and
//     the colour operators only while the stream processes them, changes what
//     compliance reports on no file at PDF/A-1b and PDF/A-2b, veraPDF files at
//     their own profile.
func ContentOperandLink(operator string, index, n int, colour bool) OperandLink {
	if index < 0 || index >= n {
		return 0
	}
	last := index == n-1
	switch operator {
	case "w", "M", "i", "Tc", "Tw", "TL", "Ts", "Tz":
		return pick(last, LinkNumber)
	case "J", "j":
		return pick(last, LinkInteger)
	case "ri", "BMC", "MP":
		return pick(last, LinkName)
	case "Tj", "'":
		return pick(last, LinkString)
	case "TJ":
		return pick(last, LinkArray)
	case "BI":
		return pick(last, LinkDict)
	case "Tf":
		return pick(last, LinkNumber) | pick(index == n-2, LinkName)
	case "d":
		return pick(last, LinkNumber) | pick(index == n-2, LinkArray)
	case "BDC", "DP":
		return pick(last, LinkDict) | pick(index == n-2, LinkName)
	case "Td", "TD", "d0":
		return pick(index >= n-2, LinkNumber)
	case `"`:
		// The operator reads its two numbers only when it takes three
		// operands or more (GFOp_DoubleQuote.getSpecialNumber).
		return pick(last, LinkString) | pick(!last && n >= 3 && index >= n-3, LinkNumber)
	case "c", "cm", "d1", "l", "m", "re", "Tm", "v", "y":
		return LinkNumber | LinkArrayNumbers
	case "g", "G", "rg", "RG", "k", "K", "cs", "CS", "scn", "SCN":
		return pick(colour, LinkNumber|pick(last, LinkName))
	case "sc", "SC":
		return pick(colour, LinkNumber)
	}
	return 0
}

// pick returns link when ok is set, and no link otherwise.
func pick(ok bool, link OperandLink) OperandLink {
	if ok {
		return link
	}
	return 0
}

// ColourState follows whether a content stream processes its colour
// operators, which decides what [ContentOperandLink] reaches of their operands
// and whether an inline image is read at all ([ColourState.ReadsInlineImage]).
//
// ISO 32000-1, 9.6.5 has the glyph description of a Type3 font that opens with
// d1 ignore the colour operators it holds, and 8.7.3.3 the cell of an
// uncoloured tiling pattern. veraPDF stops reading those operators as objects of
// its model there (GraphicState.disableColorOperators, veraPDF-validation 1.30),
// so none of their operands is judged: after a d1 in any stream, to the Q that
// restores a state saved before it, and in the cell of a pattern whose
// /PaintType is 2, which the pattern's cell starts in. It skips the inline
// images there too, save an image mask. Measured with veraPDF 1.30 at PDF/A-1b
// and PDF/A-2b, an integer past the limit given to g, or in the dictionary of
// an inline image, fails ISO 19005-1, 6.1.12 t1 and ISO 19005-2, 6.1.13 t1 in
// a coloured pattern cell, in a d0 glyph and before a d1, and fails nothing in
// an uncoloured cell or after a d1, even in page content, while one given to w
// or in the dictionary of an image mask there fails it. A d1 inside q and Q
// leaves the operators after the Q processed. veraPDF passes the state on to
// the forms and glyphs a stream draws, and to every tiling cell of a stream
// that selects an uncoloured pattern, which start in it (see [drawnScan]).
type ColourState struct {
	// off reports that the stream started with its colour operators
	// ignored: a tiling cell, a form or a glyph drawn in that state.
	off bool
	// depth counts the graphics states q saved and Q has not restored.
	depth int
	// offAt is 1 plus the depth at which a d1 turned the colour operators
	// off, or 0 while none did.
	offAt int
}

// NewColourState returns the state a stream starts in: with its colour
// operators processed unless off, as in the cell of a tiling pattern whose
// /PaintType is 2.
func NewColourState(off bool) ColourState {
	return ColourState{off: off}
}

// On reports whether the stream processes its colour operators at the next
// operation.
func (s *ColourState) On() bool {
	return !s.off && (s.offAt == 0 || s.depth < s.offAt-1)
}

// ReadsInlineImage reports whether veraPDF reads the inline image whose
// dictionary names imageMask as its /IM entry (ISO 32000-1, 8.9.7, Table 93)
// at the next operation: always while the stream processes its colour
// operators, and only an image mask while it does not.
func (s *ColourState) ReadsInlineImage(imageMask bool) bool {
	return s.On() || imageMask
}

// Operator moves the state past an operation of operator: q saves the state,
// Q restores the one it saved, and d1 turns the colour operators off.
func (s *ColourState) Operator(operator string) {
	switch operator {
	case "q":
		s.depth++
	case "Q":
		if s.depth == 0 {
			return
		}
		s.depth--
		if s.offAt != 0 && s.depth < s.offAt-1 {
			s.offAt = 0
		}
	case "d1":
		if s.offAt == 0 || s.depth < s.offAt-1 {
			s.offAt = s.depth + 1
		}
	}
}
