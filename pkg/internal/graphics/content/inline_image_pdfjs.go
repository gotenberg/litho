package content

import (
	"bytes"
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// PDF.js reads these many bytes past an EI to tell the end of the data of an
// inline image from an "EI" inside it (Parser.findDefaultInlineStreamEnd,
// src/core/parser.js, n = 15): the first window has to be ASCII, and the
// second has to lex to an operator. They are the reference's values, not caps
// of litho's.
const (
	pdfjsASCIIWindow   = 15
	pdfjsCommandWindow = 5 * pdfjsASCIIWindow
)

// pdfjsTokenCacheSize is how many of the tokens the checks of EI lexed
// [pdfjsImages] keeps, by the offset each starts at. It bounds no input:
// a token it no longer holds is lexed again. It is a power of two larger than
// pdfjsCommandWindow, so the tokens of one window, which start less than
// pdfjsCommandWindow bytes apart, never take each other's place.
const pdfjsTokenCacheSize = 128

// pdfjsImages reads the data of the inline images of one content stream as
// PDF.js reads it ([pdfjsImages.end]).
//
// PDF.js looks for the end of the data of each image from its start, to the
// end of the stream when no end comes, and lexes the 75 bytes after each EI
// it meets. Read that way, a stream whose images end at no filter marker, or
// at no EI that a check takes, has the rest of the stream read again for each
// of its images, which takes minutes on a crafted stream of a few megabytes,
// and EIs a few bytes apart have the same bytes lexed again for each. The
// reader keeps what its scans learnt of the stream, each a fact of its bytes
// that holds whatever scan asks, so its answers are those of PDF.js and
// reading all the images of a stream reads each byte a bounded number of
// times:
//
//   - The offsets a scan for the EOI marker of DCTDecode data read a byte at
//     and met no marker past, one bit for each byte of the stream once a scan
//     has met none ([pdfjsImages.dctEnd]).
//   - The first offset a scan for the end of ASCII85Decode data met none
//     from, and the last ">" of the stream, which ends ASCIIHexDecode data.
//   - The first offset a scan for the EI that ends the data met none from, and
//     the last EI it met ([pdfjsImages.defaultEnd]).
//   - The tokens the checks of EI lexed ([pdfjsImages.token]), and what the
//     last check read of them ([pdfjsWalk]).
//
// It is not safe for concurrent use.
//
// # Corpus evidence
//
//   - All corpora: the 6961 streams of 558 files whose bytes hold BI and ID,
//     229,937 inline images in 136 of the files, read the operations, and
//     the byte ranges, they read when every scan read the stream afresh, as
//     PDF.js reads them, as a repair does and whole.
//   - pdfjs, govdocs: the text of the 34,585 pages of the 2282 files with a
//     PDF.js golden does not change.
type pdfjsImages struct {
	data []byte
	// dctDead marks the offsets a scan for the EOI marker of DCTDecode data
	// read a byte at, and from which it met no marker, one bit for each byte
	// of the stream, or is nil before a scan has met none.
	dctDead []uint64
	// a85From is the smallest offset a scan for the end of ASCII85Decode data
	// met none from, or past the stream before one has.
	a85From int
	// lastGT is the offset of the last ">" of the stream, or -1, once gtRead.
	lastGT int
	gtRead bool
	// eiFrom is the smallest offset a scan for the EI that ends the data met
	// none from, or past the stream before one has, and eiLast the offset
	// past the last EI and white-space byte that scan met, or -1.
	eiFrom, eiLast int
	// tokens holds the tokens the checks of EI lexed, each at the offset it
	// starts at modulo pdfjsTokenCacheSize, or is nil before a check.
	tokens []pdfjsToken
	// walk is what the last check read of its window, and spare the slice
	// the next one records its resets in.
	walk  pdfjsWalk
	spare []int
}

// newPDFJSImages returns a reader of the inline images of data, a content
// stream's decoded bytes.
func newPDFJSImages(data []byte) *pdfjsImages {
	return &pdfjsImages{data: data, a85From: len(data) + 1, eiFrom: len(data) + 1, eiLast: -1}
}

// end returns the offset a parse reading the stream as PDF.js does resumes at
// after the inline image whose ID keyword ends at idEnd, with dict its
// dictionary, and whether PDF.js stops reading the stream there instead
// (Parser.makeInlineImage, src/core/parser.js).
//
// ISO 32000-1, 8.9.7 gives the data no length and ends it before an EI that
// white space precedes. PDF.js reads the data from the byte past the one that
// ends the ID keyword, and ends it by its first filter: at the EOI marker of a
// DCTDecode image, the "~>" of an ASCII85Decode one and the ">" of an
// ASCIIHexDecode one, and then past the first "EI" after that marker and the
// byte after it, whatever they are. Any other image, and one whose marker
// never comes, ends at the first "EI" that a space, a line feed or a carriage
// return follows, whatever precedes it, whose next 15 bytes are ASCII and
// whose next 75 bytes lex to an operator of Annex A, Table A.1 taking the
// operands read before it ([pdfjsImages.commandAfter]). Deliberate leniency
// toward PDF.js, which the text extraction and the redaction follow: an "EI"
// written against the data ends it, as it does for PDF.js, where ISO 32000-1,
// 8.9.7 reads it as data. The reading resumes past that white-space byte.
// Data with no such EI ends past the last "EI" and white-space byte the scan
// met, or with the stream. A check whose lexer fails, on a ")" or a malformed
// number, throws out of the reading of the stream, which PDF.js then stops
// reading.
//
// PDF.js lexes the byte that ends the ID keyword once more, as the first byte
// of the token after the image, which only a byte other than white space
// changes. ISO 32000-1, 8.9.7 has white space follow ID, and litho resumes
// past the data.
//
// # Corpus evidence
//
//   - All corpora: 173 files hold 6067 streams that write BI, and the
//     reading of 69 content streams, in 5 files, moves. The text extracted
//     from the 3156 pages of the 173 files and their PDF/A-2b violations do
//     not change.
//   - All corpora: no ID of the 230,088 inline images is followed by a byte
//     other than white space.
//   - pdfjs bug1513120_reduced.pdf: 47 of its 97 Type 3 glyphs end their
//     inline image mask data with EI and no white space before it, and the
//     reading of each glyph description failed. They read their d1, cm and
//     image, as veraPDF reads them.
//   - pdfjs issue19532.pdf: 13 glyph descriptions end their data the same
//     way, and issue10388_reduced.pdf one content stream.
//   - pdfjs issue17794.pdf: page 1 reads 1302 inline images where it read
//     1301, as veraPDF reads them.
//   - pdfjs poppler-90-0-fuzzed.pdf: 6 fuzzed glyph descriptions whose image
//     dictionaries hold a key that is no name stop before the image, as
//     PDF.js stops there, 4 of them where their reading failed, and one more
//     reads its image.
func (r *pdfjsImages) end(ctx context.Context, idEnd int, dict ast.PdfDict) (int, bool) {
	// PDF.js reads the byte past ID as the one that ends the keyword, and the
	// data starts past it, white space or not.
	start := min(idEnd+1, len(r.data))
	switch pdfjsFirstFilter(dict) {
	case "DCT", "DCTDecode":
		if end, ok := r.dctEnd(ctx, start); ok {
			return end, false
		}
	case "A85", "ASCII85Decode":
		if end, ok := r.ascii85End(start); ok {
			return end, false
		}
	case "AHx", "ASCIIHexDecode":
		if end, ok := r.hexEnd(start); ok {
			return end, false
		}
	}
	return r.defaultEnd(ctx, start)
}

// pdfjsFirstFilter returns the name of the first filter an inline image
// dictionary names, by its /F entry, or by /Filter when it has none, as PDF.js
// reads it (Dict.get("F", "Filter")), or "" when it names none.
func pdfjsFirstFilter(dict ast.PdfDict) ast.PdfName {
	f, ok := dict["F"]
	if !ok {
		f = dict["Filter"]
	}
	switch v := f.(type) {
	case ast.PdfName:
		return v
	case ast.PdfArray:
		if len(v) > 0 {
			if name, ok := v[0].(ast.PdfName); ok {
				return name
			}
		}
	}
	return ""
}

// defaultEnd returns where PDF.js resumes after the data of an inline image
// that starts at start, and whether it stops reading the stream there instead
// (Parser.findDefaultInlineStreamEnd). See [pdfjsImages.end].
//
// After a byte other than E and I, a scan stands where one that read neither
// would, whatever it read before, so two scans that have both read the same
// such byte meet the same EIs past it, whose checks answer the same. A scan
// that reads such a byte where a scan that met no EI ending the data read
// meets none past it either, and its last EI is the last one that scan met,
// when that one lies past the byte.
func (r *pdfjsImages) defaultEnd(ctx context.Context, start int) (int, bool) {
	data := r.data
	// state counts the bytes of "EI" just read. PDF.js reads a byte that
	// breaks the sequence as no start of a new one, so "EEI " and "EIEI "
	// hold no EI it takes.
	state := 0
	// lastEI is past the last EI and white-space byte the scan met.
	lastEI := -1
scan:
	for i := start; i < len(data); {
		if (i-start)%inlineImageCheckInterval == 0 && ctx.Err() != nil {
			return len(data), false
		}
		c := data[i]
		i++
		switch state {
		case 0:
			if c == 'E' {
				state = 1
			}
		case 1:
			state = 0
			if c == 'I' {
				state = 2
			}
		default:
			state = 0
			if c == ' ' || c == '\n' || c == '\r' {
				lastEI = i
				if i == len(data) {
					return i, false
				}
				if pdfjsASCII(data[i:min(i+pdfjsASCIIWindow, len(data))]) {
					switch r.commandAfter(i, min(i+pdfjsCommandWindow, len(data))) {
					case pdfjsOperator:
						return i, false
					case pdfjsThrows:
						return i, true
					case pdfjsNoOperator:
					}
				}
			}
		}
		if c != 'E' && c != 'I' && i > r.eiFrom {
			if r.eiLast > i {
				lastEI = r.eiLast
			}
			break scan
		}
	}
	if start < r.eiFrom {
		r.eiFrom, r.eiLast = start, lastEI
	}
	if lastEI >= 0 {
		return lastEI, false
	}
	return len(data), false
}

// pdfjsASCII reports whether window is ASCII as PDF.js tells it after an EI:
// line feeds, carriage returns and the bytes from 0x20 to 0x7F, and a NUL
// byte that no NUL follows, which some producers write after an image.
func pdfjsASCII(window []byte) bool {
	for k, c := range window {
		if c == 0 && (k+1 == len(window) || window[k+1] != 0) {
			continue
		}
		if c != '\n' && c != '\r' && (c < 0x20 || c > 0x7f) {
			return false
		}
	}
	return true
}

// pdfjsCheck is the answer of the check PDF.js makes of the bytes after an
// EI.
type pdfjsCheck int

const (
	// pdfjsNoOperator makes the EI part of the data.
	pdfjsNoOperator pdfjsCheck = iota
	// pdfjsOperator makes the EI end the data.
	pdfjsOperator
	// pdfjsThrows stops the reading of the stream.
	pdfjsThrows
)

// commandAfter answers the check PDF.js makes of the window of the stream
// from at to bound, the bytes after an EI and the white-space byte after it:
// lexed with the command table of EvaluatorPreprocessor.opMap
// (src/core/evaluator.js), the first command that takes as many operands as
// were read before it, or no more for one taking a variable count, is an
// operator and ends the data. A command outside the table, "[" and "<<" among
// them, or the end of the window first makes the EI part of the data, and a
// command taking another count starts the count again.
//
// The windows of the EIs of a stretch of data overlap, and a walk through one
// that starts the count again past the same command as the walk through the
// window before it reads on as that walk did, as far as its tokens end before
// the end of that window. It takes up that walk there ([pdfjsWalk]).
func (r *pdfjsImages) commandAfter(at, bound int) pdfjsCheck {
	last := &r.walk
	joins := last.bound > 0 && bound >= last.bound
	w := pdfjsWalk{bound: bound, resets: r.spare[:0]}
	// firm reports that every token read so far ends before bound.
	firm := true
	// k indexes the first reset of the last walk not before this walk's last.
	k := 0
	operands := 0
	for from := at; ; {
		t := r.token(from, bound)
		if firm && t.next >= bound {
			firm = false
			w.stopFrom, w.stopOperands = from, operands
		}
		answer, answered := pdfjsNoOperator, true
		switch t.kind {
		case pdfjsEOF:
		case pdfjsError:
			answer = pdfjsThrows
		case pdfjsCommand:
			switch {
			case !t.known:
			case t.args.variable && operands <= t.args.n || !t.args.variable && operands == t.args.n:
				answer = pdfjsOperator
			default:
				answered = false
				operands = 0
			}
		case pdfjsValue:
			answered = false
			operands++
		}
		// Every token takes at least one byte; a lexer that took none would
		// never reach the end of the window.
		if answered || t.next <= from {
			w.settled, w.answer = firm, answer
			r.walk, r.spare = w, last.resets
			return answer
		}
		from = t.next
		if !firm || operands > 0 {
			continue
		}
		w.resets = append(w.resets, from)
		if !joins {
			continue
		}
		for k < len(last.resets) && last.resets[k] < from {
			k++
		}
		if k == len(last.resets) || last.resets[k] != from {
			continue
		}
		joins = false
		w.resets = append(w.resets, last.resets[k+1:]...)
		if last.settled {
			w.settled, w.answer = true, last.answer
			r.walk, r.spare = w, last.resets
			return last.answer
		}
		from, operands = last.stopFrom, last.stopOperands
	}
}

// pdfjsWalk is what the walk of a check through its window read
// ([pdfjsImages.commandAfter]), as far as its tokens end before the end of the
// window. Those tokens are the same in any window that ends no sooner, so a
// walk through such a window that starts the count of operands again past one
// of the same commands reads on as this one did.
type pdfjsWalk struct {
	// bound is the end of the window, or 0 before the first check.
	bound int
	// resets holds, in order, the offsets past the commands the walk started
	// the count again at.
	resets []int
	// settled reports that the walk answered answer at a token that ends
	// before the end of its window. Otherwise the window ends inside the
	// token read from stopFrom, and stopOperands operands were read since
	// the last reset before it.
	settled                bool
	answer                 pdfjsCheck
	stopFrom, stopOperands int
}

// pdfjsToken is a token the check of an EI lexed ([pdfjsLexer.token]).
type pdfjsToken struct {
	// from is the offset it starts at, bound the end of the window it was read
	// in, and next the offset past it.
	from, bound, next int
	kind              pdfjsTokenKind
	// args is the operand count of a command of [pdfjsOperands], which known
	// reports it is.
	args  pdfjsArgs
	known bool
}

// token returns the token [pdfjsLexer] reads from the offset from in the
// window of the stream that ends at bound, which the tokens an earlier check
// lexed give when they hold it.
//
// A token depends on no byte past the offset it ends at, save the one there
// that ends it: the lexer reads on through a run of regular characters that
// PDF.js ends at a command ([lexer.CommandSet.Command]), but the command ends
// where it does whatever follows it. A token that ends before the end of the
// window it was read in is therefore the one read in any window it ends
// before, and the windows of the EIs of a stretch of data, which overlap,
// share their tokens.
func (r *pdfjsImages) token(from, bound int) pdfjsToken {
	lx := pdfjsLexer{b: r.data[:bound], i: from}
	if !lx.skip() {
		return pdfjsToken{from: from, bound: bound, next: bound, kind: pdfjsEOF}
	}
	if r.tokens == nil {
		r.tokens = make([]pdfjsToken, pdfjsTokenCacheSize)
	}
	t := &r.tokens[lx.i%pdfjsTokenCacheSize]
	if t.from == lx.i && (t.bound == bound || t.next < t.bound && t.next < bound) {
		return *t
	}
	*t = pdfjsToken{from: lx.i, bound: bound}
	kind, cmd := lx.token()
	t.next, t.kind = lx.i, kind
	if kind == pdfjsCommand {
		t.args, t.known = pdfjsOperands[string(cmd)]
	}
	return *t
}

// pdfjsTokenKind is what [pdfjsLexer.token] read.
type pdfjsTokenKind int

const (
	pdfjsEOF pdfjsTokenKind = iota
	pdfjsValue
	pdfjsCommand
	// pdfjsError is a token Lexer.getObj throws on.
	pdfjsError
)

// pdfjsLexer reads the tokens of a window as Lexer.getObj of PDF.js does
// (src/core/parser.js), with the command table of EvaluatorPreprocessor, for
// the check of an EI. It tells tokens apart only as far as the check needs:
// their extent, and whether each is a command, which one, or a value.
type pdfjsLexer struct {
	b []byte
	i int
}

// skip moves past the white space and comments at the current byte, and
// reports whether a token starts before the end of the window.
func (lx *pdfjsLexer) skip() bool {
	b := lx.b
	comment := false
	for ; lx.i < len(b); lx.i++ {
		c := b[lx.i]
		switch {
		case comment:
			comment = c != '\n' && c != '\r'
		case c == '%':
			comment = true
		case !isVeraPDFSpace(c):
			return true
		}
	}
	return false
}

// token reads the token that starts at the current byte, which is no white
// space.
func (lx *pdfjsLexer) token() (pdfjsTokenKind, []byte) {
	b := lx.b
	start := lx.i
	switch b[start] {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '+', '-', '.':
		if !lx.number() {
			return pdfjsError, nil
		}
		return pdfjsValue, nil
	case '(':
		lx.literalString()
		return pdfjsValue, nil
	case '/':
		lx.i++
		lx.regular()
		return pdfjsValue, nil
	case '[', ']', '{', '}':
		lx.i++
		return pdfjsCommand, b[start:lx.i]
	case '<':
		lx.i++
		if lx.i < len(b) && b[lx.i] == '<' {
			lx.i++
			return pdfjsCommand, b[start:lx.i]
		}
		if gt := bytes.IndexByte(b[lx.i:], '>'); gt >= 0 {
			lx.i += gt + 1
		} else {
			lx.i = len(b)
		}
		return pdfjsValue, nil
	case '>':
		lx.i++
		if lx.i < len(b) && b[lx.i] == '>' {
			lx.i++
		}
		return pdfjsCommand, b[start:lx.i]
	case ')':
		lx.i++
		return pdfjsError, nil
	}
	// A command cannot start with a byte outside visible ASCII: one such
	// byte before a visible one is a command of its own.
	if c := b[start]; (c < 0x20 || c > 0x7f) && start+1 < len(b) && b[start+1] >= 0x20 && b[start+1] <= 0x7f {
		lx.i++
		return pdfjsCommand, b[start:lx.i]
	}
	lx.regular()
	lx.i = start + gluedOperators.Command(b[start:lx.i])
	switch cmd := b[start:lx.i]; string(cmd) {
	case "true", "false", "null":
		return pdfjsValue, nil
	default:
		return pdfjsCommand, cmd
	}
}

// regular moves past the run of bytes that are neither white space nor a
// delimiter, which ends a name and a command.
func (lx *pdfjsLexer) regular() {
	for lx.i < len(lx.b) && !isVeraPDFSpace(lx.b[lx.i]) && !isDelimiter(lx.b[lx.i]) {
		lx.i++
	}
}

// number moves past a number as Lexer.getNumber reads one, and reports false
// where it throws: a sign or a period that no digit follows, save before
// white space, "(", "<" or the end of the window, reads as 0.
func (lx *pdfjsLexer) number() bool {
	b := lx.b
	switch b[lx.i] {
	case '-':
		lx.i++
		// A double negative reads as one.
		if lx.i < len(b) && b[lx.i] == '-' {
			lx.i++
		}
	case '+':
		lx.i++
	}
	for lx.i < len(b) && (b[lx.i] == '\n' || b[lx.i] == '\r') {
		lx.i++
	}
	point := false
	if lx.i < len(b) && b[lx.i] == '.' {
		point = true
		lx.i++
	}
	if lx.i == len(b) {
		return true
	}
	if c := b[lx.i]; c < '0' || c > '9' {
		return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '(' || c == '<'
	}
	for lx.i++; lx.i < len(b); lx.i++ {
		switch c := b[lx.i]; {
		case c >= '0' && c <= '9', c == '-':
			// PDF.js skips a minus sign inside a number.
		case c == '.' && !point:
			point = true
		default:
			return true
		}
	}
	return true
}

// literalString moves past a literal string as Lexer.getString reads one: to
// the parenthesis that balances the first, past escapes, or to the end of the
// window.
func (lx *pdfjsLexer) literalString() {
	b := lx.b
	depth := 0
	for lx.i < len(b) {
		c := b[lx.i]
		lx.i++
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return
			}
		case '\\':
			if lx.i == len(b) {
				return
			}
			e := b[lx.i]
			lx.i++
			switch {
			case e >= '0' && e <= '7':
				// An octal escape takes up to three digits.
				for k := 0; k < 2 && lx.i < len(b) && b[lx.i] >= '0' && b[lx.i] <= '7'; k++ {
					lx.i++
				}
			case e == '\r' && lx.i < len(b) && b[lx.i] == '\n':
				lx.i++
			}
		}
	}
}

// pdfjsArgs is the operand count EvaluatorPreprocessor.opMap gives an
// operator: exactly n, or at most n when variable is set.
type pdfjsArgs struct {
	n        int
	variable bool
}

// pdfjsOperands mirrors the operand counts of EvaluatorPreprocessor.opMap
// (src/core/evaluator.js), whose operators are those of ISO 32000-1, Annex A,
// Table A.1. Its partial commands, such as BM and fa, and true, false and null
// are no operator here, as they are none there.
//
//nolint:gochecknoglobals // constant lookup table
var pdfjsOperands = map[string]pdfjsArgs{
	"w": {n: 1}, "J": {n: 1}, "j": {n: 1}, "M": {n: 1}, "d": {n: 2}, "ri": {n: 1}, "i": {n: 1}, "gs": {n: 1},
	"q": {}, "Q": {}, "cm": {n: 6},
	"m": {n: 2}, "l": {n: 2}, "c": {n: 6}, "v": {n: 4}, "y": {n: 4}, "h": {}, "re": {n: 4},
	"S": {}, "s": {}, "f": {}, "F": {}, "f*": {}, "B": {}, "B*": {}, "b": {}, "b*": {}, "n": {},
	"W": {}, "W*": {},
	"BT": {}, "ET": {}, "Tc": {n: 1}, "Tw": {n: 1}, "Tz": {n: 1}, "TL": {n: 1}, "Tf": {n: 2}, "Tr": {n: 1},
	"Ts": {n: 1}, "Td": {n: 2}, "TD": {n: 2}, "Tm": {n: 6}, "T*": {}, "Tj": {n: 1}, "TJ": {n: 1},
	"'": {n: 1}, "\"": {n: 3},
	"d0": {n: 2}, "d1": {n: 6},
	"CS": {n: 1}, "cs": {n: 1}, "SC": {n: 4, variable: true}, "SCN": {n: 33, variable: true},
	"sc": {n: 4, variable: true}, "scn": {n: 33, variable: true}, "G": {n: 1}, "g": {n: 1},
	"RG": {n: 3}, "rg": {n: 3}, "K": {n: 4}, "k": {n: 4},
	"sh": {n: 1},
	"BI": {}, "ID": {}, "EI": {n: 1},
	"Do": {n: 1}, "MP": {n: 1}, "DP": {n: 2}, "BMC": {n: 1}, "BDC": {n: 2}, "EMC": {},
	"BX": {}, "EX": {},
}

// dctEnd returns where PDF.js resumes after the data of a DCTDecode inline
// image that starts at start, past its EOI marker and the EI after it, and
// false when the scan meets no EOI marker (Parser.findDCTDecodeInlineStreamEnd).
// The scan steps over the segments whose markers carry a length (ITU-T T.81,
// Table B.1), and over a length of 2 or less by stepping back to it.
//
// A marker in the last byte of the stream leaves PDF.js no length to read, and
// it steps back to that marker forever. litho reads it as the end of the
// stream instead, where no EOI marker came.
//
// Where the scan reads next depends only on where it reads, so a scan that
// reads at an offset an earlier scan read at and met no marker past meets
// none either. A scan that meets none marks every offset it read at, and a
// later scan that reads at a marked offset stops there.
func (r *pdfjsImages) dctEnd(ctx context.Context, start int) (int, bool) {
	data := r.data
	for i, n := start, 0; i < len(data) && !r.dctMarked(i); n++ {
		if n%inlineImageCheckInterval == 0 && ctx.Err() != nil {
			return 0, false
		}
		next, eoi := pdfjsDCTStep(data, i)
		if eoi {
			return pdfjsSkipEI(data, next), true
		}
		i = next
	}
	if r.dctDead == nil {
		r.dctDead = make([]uint64, (len(data)+63)/64)
	}
	for i := start; i < len(data) && !r.dctMarked(i); {
		r.dctDead[i/64] |= 1 << (i % 64)
		i, _ = pdfjsDCTStep(data, i)
	}
	return 0, false
}

// dctMarked reports whether a scan for the EOI marker of DCTDecode data read
// a byte at offset i and met no marker past it.
func (r *pdfjsImages) dctMarked(i int) bool {
	return r.dctDead != nil && r.dctDead[i/64]&(1<<(i%64)) != 0
}

// pdfjsDCTStep reads the byte at offset i of data as the scan for the EOI
// marker of DCTDecode data does, with the marker it begins, and returns the
// offset of the byte the scan reads next, past the stream where the scan ends,
// and whether it read the EOI marker, the offset then being past it.
func pdfjsDCTStep(data []byte, i int) (int, bool) {
	b := data[i]
	i++
	if b != 0xff || i == len(data) {
		return i, false
	}
	marker := data[i]
	i++
	switch {
	case marker == 0x00:
		// A stuffed 0xFF.
	case marker == 0xff:
		// A fill byte, which the next marker may follow.
		i--
	case marker == 0xd9:
		return i, true
	case pdfjsDCTSegment(marker):
		if i+1 == len(data) {
			// One byte of the length, then the end of the stream, reads as
			// no length.
			i--
		} else if i < len(data) {
			if length := int(data[i])<<8 | int(data[i+1]); length > 2 {
				i += length
			}
		}
	}
	return i, false
}

// pdfjsDCTSegment reports whether marker begins a segment PDF.js reads the
// length of: the SOF, DHT, DAC, SOS, DQT, DNL, DRI, DHP, EXP, APP and COM
// markers of ITU-T T.81, Table B.1.
func pdfjsDCTSegment(marker byte) bool {
	switch {
	case marker >= 0xc0 && marker <= 0xcf:
		// 0xC8 is reserved (JPG).
		return marker != 0xc8
	case marker >= 0xda && marker <= 0xef:
		return true
	}
	return marker == 0xfe
}

// ascii85End returns what [pdfjsASCII85End] returns for data that starts at
// start. Whether a "~" ends the data depends on the bytes after it alone, so
// a scan that starts past the start of one that met no end meets none either.
func (r *pdfjsImages) ascii85End(start int) (int, bool) {
	if start >= r.a85From {
		return 0, false
	}
	end, ok := pdfjsASCII85End(r.data, start)
	if !ok {
		r.a85From = start
	}
	return end, ok
}

// hexEnd returns where PDF.js resumes after the data of an ASCIIHexDecode
// inline image that starts at start, past the first ">" and the EI after it,
// and false when the data holds none
// (Parser.findASCIIHexDecodeInlineStreamEnd).
func (r *pdfjsImages) hexEnd(start int) (int, bool) {
	if !r.gtRead {
		r.lastGT, r.gtRead = bytes.LastIndexByte(r.data, '>'), true
	}
	if start > r.lastGT {
		return 0, false
	}
	return pdfjsSkipEI(r.data, start+bytes.IndexByte(r.data[start:], '>')+1), true
}

// pdfjsASCII85End returns where PDF.js resumes after the data of an
// ASCII85Decode inline image that starts at start, past its "~>" and the EI
// after it, and false when the data holds none
// (Parser.findASCII85DecodeInlineStreamEnd). White space may stand inside the
// marker, and a "~" that white space and "EI" follow ends the data too.
func pdfjsASCII85End(data []byte, start int) (int, bool) {
	for i := start; i < len(data); {
		c := data[i]
		i++
		if c != '~' {
			continue
		}
		tilde := i
		for i < len(data) && pdfjsWhiteSpace(data[i]) {
			i++
		}
		if i < len(data) && data[i] == '>' {
			return pdfjsSkipEI(data, i+1), true
		}
		if i > tilde && i+1 < len(data) && data[i] == 'E' && data[i+1] == 'I' {
			return pdfjsSkipEI(data, i), true
		}
	}
	return 0, false
}

// pdfjsWhiteSpace reports whether c is white space as isWhiteSpace of PDF.js
// tells it (src/core/core_utils.js): a space, a tab, a carriage return or a
// line feed.
func pdfjsWhiteSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// pdfjsSkipEI returns the offset past the first "EI" from i and the byte after
// it, or the end of the stream when none comes, as PDF.js steps over the EI of
// an image whose data ends at a marker (Parser.inlineStreamSkipEI).
func pdfjsSkipEI(data []byte, i int) int {
	state := 0
	for i < len(data) {
		c := data[i]
		i++
		switch state {
		case 0:
			if c == 'E' {
				state = 1
			}
		case 1:
			state = 0
			if c == 'I' {
				state = 2
			}
		default:
			return i
		}
	}
	return len(data)
}
