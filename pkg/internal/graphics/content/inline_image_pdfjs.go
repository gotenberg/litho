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
// of litho's, and they keep the check of each EI short, so a scan of image
// data costs at most pdfjsCommandWindow bytes for each byte it reads.
const (
	pdfjsASCIIWindow   = 15
	pdfjsCommandWindow = 5 * pdfjsASCIIWindow
)

// pdfjsInlineImageEnd returns the offset a parse reading the stream as PDF.js
// does resumes at after the inline image whose ID keyword ends at idEnd in
// data, with dict its dictionary, and whether PDF.js stops reading the stream
// there instead (Parser.makeInlineImage, src/core/parser.js).
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
// operands read before it ([pdfjsCommandAfter]). Deliberate leniency toward
// PDF.js, which the text extraction and the redaction follow: an "EI" written
// against the data ends it, as it does for PDF.js, where ISO 32000-1, 8.9.7
// reads it as data. The reading resumes past that white-space byte.
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
func pdfjsInlineImageEnd(ctx context.Context, data []byte, idEnd int, dict ast.PdfDict) (int, bool) {
	// PDF.js reads the byte past ID as the one that ends the keyword, and the
	// data starts past it, white space or not.
	start := min(idEnd+1, len(data))
	switch pdfjsFirstFilter(dict) {
	case "DCT", "DCTDecode":
		if end, ok := pdfjsDCTEnd(ctx, data, start); ok {
			return end, false
		}
	case "A85", "ASCII85Decode":
		if end, ok := pdfjsASCII85End(data, start); ok {
			return end, false
		}
	case "AHx", "ASCIIHexDecode":
		if gt := bytes.IndexByte(data[start:], '>'); gt >= 0 {
			return pdfjsSkipEI(data, start+gt+1), false
		}
	}
	return pdfjsDefaultEnd(ctx, data, start)
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

// pdfjsDefaultEnd returns where PDF.js resumes after the data of an inline
// image that starts at start, and whether it stops reading the stream there
// instead (Parser.findDefaultInlineStreamEnd). See [pdfjsInlineImageEnd].
func pdfjsDefaultEnd(ctx context.Context, data []byte, start int) (int, bool) {
	// state counts the bytes of "EI" just read. PDF.js reads a byte that
	// breaks the sequence as no start of a new one, so "EEI " and "EIEI "
	// hold no EI it takes.
	state := 0
	// lastEI is past the last EI and white-space byte the scan met.
	lastEI := -1
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
			if c != ' ' && c != '\n' && c != '\r' {
				continue
			}
			lastEI = i
			if i == len(data) {
				return i, false
			}
			if !pdfjsASCII(data[i:min(i+pdfjsASCIIWindow, len(data))]) {
				continue
			}
			switch pdfjsCommandAfter(data[i:min(i+pdfjsCommandWindow, len(data))]) {
			case pdfjsOperator:
				return i, false
			case pdfjsThrows:
				return i, true
			case pdfjsNoOperator:
			}
		}
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

// pdfjsCommandAfter answers the check PDF.js makes of window, the bytes after
// an EI and the white-space byte after it: lexed with the command table of
// EvaluatorPreprocessor.opMap (src/core/evaluator.js), the first command that
// takes as many operands as were read before it, or no more for one taking a
// variable count, is an operator and ends the data. A command outside the
// table, "[" and "<<" among them, or the end of the window first makes the EI
// part of the data, and a command taking another count starts the count
// again.
func pdfjsCommandAfter(window []byte) pdfjsCheck {
	lx := pdfjsLexer{b: window}
	operands := 0
	for {
		at := lx.i
		kind, cmd := lx.next()
		switch kind {
		case pdfjsEOF:
			return pdfjsNoOperator
		case pdfjsError:
			return pdfjsThrows
		case pdfjsCommand:
			args, ok := pdfjsOperands[cmd]
			if !ok {
				return pdfjsNoOperator
			}
			if args.variable && operands <= args.n || !args.variable && operands == args.n {
				return pdfjsOperator
			}
			operands = 0
		case pdfjsValue:
			operands++
		}
		if lx.i <= at {
			// Every token takes at least one byte; a lexer that took none
			// would never reach the end of the window.
			return pdfjsNoOperator
		}
	}
}

// pdfjsTokenKind is what [pdfjsLexer.next] read.
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

// next reads the next token and returns its kind, and the command for a
// command.
func (lx *pdfjsLexer) next() (pdfjsTokenKind, string) {
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
			return lx.token()
		}
	}
	return pdfjsEOF, ""
}

// token reads the token that starts at the current byte, which is no white
// space.
func (lx *pdfjsLexer) token() (pdfjsTokenKind, string) {
	b := lx.b
	c := b[lx.i]
	switch c {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '+', '-', '.':
		if !lx.number() {
			return pdfjsError, ""
		}
		return pdfjsValue, ""
	case '(':
		lx.literalString()
		return pdfjsValue, ""
	case '/':
		lx.i++
		lx.regular()
		return pdfjsValue, ""
	case '[', ']', '{', '}':
		lx.i++
		return pdfjsCommand, string(c)
	case '<':
		lx.i++
		if lx.i < len(b) && b[lx.i] == '<' {
			lx.i++
			return pdfjsCommand, "<<"
		}
		if gt := bytes.IndexByte(b[lx.i:], '>'); gt >= 0 {
			lx.i += gt + 1
		} else {
			lx.i = len(b)
		}
		return pdfjsValue, ""
	case '>':
		lx.i++
		if lx.i < len(b) && b[lx.i] == '>' {
			lx.i++
			return pdfjsCommand, ">>"
		}
		return pdfjsCommand, ">"
	case ')':
		lx.i++
		return pdfjsError, ""
	}
	// A command cannot start with a byte outside visible ASCII: one such
	// byte before a visible one is a command of its own.
	if (c < 0x20 || c > 0x7f) && lx.i+1 < len(b) && b[lx.i+1] >= 0x20 && b[lx.i+1] <= 0x7f {
		lx.i++
		return pdfjsCommand, string(c)
	}
	start := lx.i
	lx.regular()
	cmd := gluedOperators.Split(string(b[start:lx.i]))[0]
	lx.i = start + len(cmd)
	switch cmd {
	case "true", "false", "null":
		return pdfjsValue, ""
	}
	return pdfjsCommand, cmd
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

// pdfjsDCTEnd returns where PDF.js resumes after the data of a DCTDecode
// inline image that starts at start, past its EOI marker and the EI after it,
// and false when the scan meets no EOI marker (Parser.findDCTDecodeInlineStreamEnd).
// The scan steps over the segments whose markers carry a length (ITU-T T.81,
// Table B.1), and over a length of 2 or less by stepping back to it.
//
// A marker in the last byte of the stream leaves PDF.js no length to read, and
// it steps back to that marker forever. litho reads it as the end of the
// stream instead, where no EOI marker came.
func pdfjsDCTEnd(ctx context.Context, data []byte, start int) (int, bool) {
	for i, n := start, 0; i < len(data); n++ {
		if n%inlineImageCheckInterval == 0 && ctx.Err() != nil {
			return 0, false
		}
		b := data[i]
		i++
		if b != 0xff || i == len(data) {
			continue
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
			return pdfjsSkipEI(data, i), true
		case pdfjsDCTSegment(marker):
			if i == len(data) {
				return 0, false
			}
			if i+1 == len(data) {
				// One byte of the length, then the end of the stream, reads
				// as no length.
				i--
				continue
			}
			if length := int(data[i])<<8 | int(data[i+1]); length > 2 {
				i += length
			}
		}
	}
	return 0, false
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
