package lexer

// CommandSet is the set of commands at which a lexer set up by
// [Lexer.WithCommands] ends a run of regular characters. It is read-only once
// [NewCommandSet] returns it, so one set is safe to share across goroutines and
// lexers.
type CommandSet struct {
	known map[string]struct{}
	// maxLen is the length of the longest member. No longer run is a member,
	// so the lexer skips the lookup past it.
	maxLen int
}

// NewCommandSet returns the set that ends a run at each of words.
//
// The greedy read of [Lexer.WithCommands] stops as soon as one more character
// would turn a member into a string that is none, so a word read through a
// shorter member needs every prefix in between: with "f" a member, "false" is
// read whole only if "fa", "fal" and "fals" are members too. NewCommandSet adds
// those prefixes. PDF.js lists them by hand, as the "reserved partial commands
// for the lexer" of EvaluatorPreprocessor.opMap (src/core/evaluator.js);
// deriving them keeps the set in step with its words.
func NewCommandSet(words ...string) *CommandSet {
	cs := &CommandSet{known: make(map[string]struct{}, len(words))}
	for _, w := range words {
		cs.known[w] = struct{}{}
		cs.maxLen = max(cs.maxLen, len(w))
	}
	// The shortest member prefix of a word is always one of the words, since
	// every prefix added here extends a shorter member, so one pass over the
	// words in any order reaches the same set.
	for _, w := range words {
		for i := 1; i < len(w); i++ {
			if _, ok := cs.known[w[:i]]; !ok {
				continue
			}
			for j := i + 1; j < len(w); j++ {
				cs.known[w[:j]] = struct{}{}
			}
			break
		}
	}
	return cs
}

// has reports whether b is a member of cs. A nil set has no member.
func (cs *CommandSet) has(b []byte) bool {
	if cs == nil || len(b) > cs.maxLen {
		return false
	}
	_, ok := cs.known[string(b)]
	return ok
}

// hasString is [CommandSet.has] for a string.
func (cs *CommandSet) hasString(s string) bool {
	if cs == nil || len(s) > cs.maxLen {
		return false
	}
	_, ok := cs.known[s]
	return ok
}

// Split returns the pieces a lexer set up by [Lexer.WithCommands] ends run at,
// in order, run being a run of regular characters such as a lexer without the
// set reads as one keyword: "ETBT" gives ET and BT, and a run no command
// starts gives itself. It is the rule of the lexer's read applied to a string,
// so a caller that holds the keyword splits it without lexing it again. A nil
// set gives run whole.
func (cs *CommandSet) Split(run string) []string {
	var parts []string
	start := 0
	command := false
	for i := range len(run) {
		if command && !cs.hasString(run[start:i+1]) {
			parts = append(parts, run[start:i])
			start = i
		}
		command = cs.hasString(run[start : i+1])
	}
	return append(parts, run[start:])
}

// WithCommands makes l end a run of regular characters at a command of cs the
// way PDF.js does, and returns l. Without it a run ends only at white space or a
// delimiter.
//
// ISO 32000-1, 7.2.2 ends a token only at white space or a delimiter, and 7.8.2
// writes an operator as such a token, so "ETBT" is one keyword by the letter of
// the spec, and no operator of Annex A. Deliberate leniency toward producers
// that write operators with no white space between them: PDF.js reads a command
// greedily (Lexer.getObj, src/core/parser.js, v6.3.289), adding one character
// at a time while the string read so far is no command of its table or the
// longer string still is one, and leaves the rest for the next token. "ETBT"
// reads as ET then BT, "q1" as q then the number 1, "BDC" stays whole because
// its prefix "BD" is a member, and a run no command starts, such as "XX", stays
// one keyword.
//
// Three rules of PDF.js are not mirrored. A run that starts with a digit, a
// sign or a period is a number to PDF.js, which ends it at the first character
// no number holds (Lexer.getNumber), so "12Tf" is 12 then Tf there; no command
// starts with such a character, so the set leaves the run as it was, one
// keyword. PDF.js also reads a first byte outside printable ASCII as a command
// of its own when a printable byte follows, and refuses a command longer than
// 128 characters.
//
// The objects of the file body keep the token boundaries of 7.2.2: only a
// content stream reading sets a command set.
func (l *Lexer) WithCommands(cs *CommandSet) *Lexer {
	l.commands = cs
	return l
}

// WithDigitBoundaries makes l end a keyword before a digit the way veraPDF
// reads a content stream, and returns l. Without it a run of regular characters
// ends only at white space or a delimiter.
//
// ISO 32000-1, 7.2.2 ends a token only at white space or a delimiter, so "q1"
// is one keyword by the letter of the spec, an operator ISO 32000-1, Annex A
// does not define, which ISO 19005-1, 6.2.10 t1 and ISO 19005-2, 6.2.2 t1
// forbid. Deliberate leniency toward veraPDF, the reference validator of ISO
// 19005: its content stream parser reads an operator up to white space, one of
// the delimiters "[", "]", "<", "(" and "/", or a digit
// (PDFStreamParser.nextOperator, veraPDF-parser 1.30), so "q1" is q then the
// number 1 there, and so it is here. A 0 or a 1 after a "d" stays in the
// operator, which keeps d0 and d1 whole. A keyword the object syntax can
// spell, one that starts with "n", "t" or "f", and one that starts with "B" are
// read to white space or a delimiter (PDFStreamParser.parseNextToken), so
// "n1", "true1" and "BT1" stay whole.
//
// Three rules of veraPDF are not mirrored. A run that starts with a digit, a
// sign or a period keeps the token boundaries of 7.2.2, where veraPDF ends a
// number at the first character other than a digit or a period
// (BaseParser.readNumber): "12m" is one undefined operator here and 12 then m
// there, and "xx1yy" is xx then the keyword 1yy here and xx, 1 and yy there,
// two undefined operators either way. veraPDF also does not end an operator at
// ")", ">", "{", "}" or "%", which ISO 32000-1, 7.2.2, Table 2 makes
// delimiters and which end it here. And a run that starts with "I" keeps its
// boundaries, where veraPDF reads it as the start of inline image data.
//
// The objects of the file body keep the token boundaries of 7.2.2: only a
// content stream reading sets it.
func (l *Lexer) WithDigitBoundaries() *Lexer {
	l.digitBoundaries = true
	return l
}

// endsBeforeDigit reports whether a keyword that starts with first ends
// before a digit under [Lexer.WithDigitBoundaries].
func endsBeforeDigit(first byte) bool {
	switch first {
	case 'n', 't', 'f', 'B', 'I', '+', '-', '.':
		return false
	}
	return !isDigit(first)
}

// joinsBeforeDigit reports whether digit, read after prev, stays in a keyword
// that ends before a digit: the 0 or 1 of d0 and d1.
func joinsBeforeDigit(prev, digit byte) bool {
	return prev == 'd' && (digit == '0' || digit == '1')
}

// isDigit reports whether c is a decimal digit.
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
