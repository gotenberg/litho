package type1

import (
	"bytes"
	"strconv"
)

// charStringsMarker is the PostScript token that opens the
// CharStrings dictionary in a Type 1 font's private section.
var charStringsMarker = []byte("/CharStrings")

// charStringReaders lists the PostScript operators that introduce a
// charstring's encrypted bytes. Modern fonts use the literal
// "RD" shortcut; older and some TeX-generated files keep the original
// "-|" macro. Both take the same form: /name LEN <op> <LEN bytes>.
var charStringReaders = [][]byte{
	[]byte("RD"),
	[]byte("-|"),
}

// ParseCharStringNames returns the glyph-name set defined by a Type 1
// font's /CharStrings dictionary. The input is the raw /FontFile
// stream; the function locates the eexec block, decrypts it, finds
// the /CharStrings dictionary in the decrypted private section, and
// walks its "/name LEN RD <LEN bytes> ND" entries. Entries the
// parser cannot validate (missing length, missing operator) are
// skipped rather than aborting the walk so partial output is still
// useful.
//
// The returned slice preserves discovery order. Duplicate names are
// filtered so callers can use it directly for set-membership checks.
func ParseCharStringNames(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	plain := decryptEexec(eexecRegion(data))
	if plain == nil {
		return nil
	}
	cs := bytes.Index(plain, charStringsMarker)
	if cs < 0 {
		return nil
	}
	// Skip past "/CharStrings N dict dup begin" preamble to the first
	// entry. The parser anchors on the first '/' after the preamble.
	pos := cs + len(charStringsMarker)
	if beginIdx := bytes.Index(plain[pos:], []byte("begin")); beginIdx >= 0 {
		pos += beginIdx + len("begin")
	}

	var names []string
	seen := make(map[string]struct{})
	for pos < len(plain) {
		pos = skipWhitespace(plain, pos)
		if pos >= len(plain) {
			break
		}
		if bytes.HasPrefix(plain[pos:], []byte("end")) {
			break
		}
		if plain[pos] != '/' {
			// Step forward one byte and keep looking. Some fonts
			// interleave comments or intermediate definitions before
			// individual entries.
			pos++
			continue
		}
		name, next := readName(plain, pos+1)
		if name == "" {
			pos++
			continue
		}
		pos = next
		pos = skipWhitespace(plain, pos)
		length, after := readInt(plain, pos)
		if after == pos {
			// No length follows the name; skip this token and keep
			// scanning. Procedures and metadata inside /CharStrings
			// take this shape.
			continue
		}
		pos = skipWhitespace(plain, after)
		op, afterOp := readReaderOp(plain, pos)
		if op == nil {
			continue
		}
		pos = afterOp
		// A single whitespace byte separates the operator from the
		// encrypted payload. Step over it only when present so a tight
		// encoder that omitted the byte still parses.
		if pos < len(plain) && isPostScriptWhitespace(plain[pos]) {
			pos++
		}
		// Skip the encrypted charstring bytes.
		if pos+length > len(plain) {
			break
		}
		pos += length
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names
}

// eexecRegion returns the byte slice that starts at the first byte
// after "currentfile eexec" keyword in data. When the keyword is
// absent the whole input is returned; [decryptEexec] copes with
// unparseable input by returning nil, which short-circuits the
// caller.
func eexecRegion(data []byte) []byte {
	const keyword = "currentfile eexec"
	_, after, ok := bytes.Cut(data, []byte(keyword))
	if !ok {
		return data
	}
	return after
}

// readName parses a PostScript name token starting one byte after the
// leading '/'. Returns the name and the byte offset immediately after
// it. A name terminates at whitespace or any PostScript delimiter.
func readName(data []byte, start int) (string, int) {
	end := start
	for end < len(data) {
		c := data[end]
		if isPostScriptWhitespace(c) || isPostScriptDelimiter(c) {
			break
		}
		end++
	}
	if end == start {
		return "", start
	}
	return string(data[start:end]), end
}

// readInt parses an unsigned decimal integer. Returns the value and
// the offset immediately after the last digit. When no digit is
// present the offset is unchanged, signalling "no integer here".
func readInt(data []byte, start int) (int, int) {
	end := start
	for end < len(data) && data[end] >= '0' && data[end] <= '9' {
		end++
	}
	if end == start {
		return 0, start
	}
	v, err := strconv.Atoi(string(data[start:end]))
	if err != nil {
		return 0, start
	}
	return v, end
}

// readReaderOp matches any CharString-reader operator in the token
// starting at pos. Returns the matched operator bytes and the offset
// immediately after; a nil result means the token was not a
// recognised reader.
func readReaderOp(data []byte, pos int) ([]byte, int) {
	for _, op := range charStringReaders {
		if bytes.HasPrefix(data[pos:], op) {
			after := pos + len(op)
			// Ensure the match is a standalone token: the next byte
			// must be whitespace or end of data. Prefix-only matches
			// (e.g. "RDecode") are rejected.
			if after == len(data) || isPostScriptWhitespace(data[after]) {
				return op, after
			}
		}
	}
	return nil, pos
}

func skipWhitespace(data []byte, pos int) int {
	for pos < len(data) && isPostScriptWhitespace(data[pos]) {
		pos++
	}
	return pos
}

func isPostScriptWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == 0
}

func isPostScriptDelimiter(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}
