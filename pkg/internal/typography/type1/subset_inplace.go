package type1

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

// Lengths are the sizes of the three parts of a Type 1 program a /FontFile
// stream gives as /Length1, /Length2 and /Length3 (ISO 32000-1, 9.9, Table
// 127): the clear-text portion, the encrypted portion, and the fixed-content
// trailer of zeros and cleartomark, which may be empty.
type Lengths struct {
	Clear, Encrypted, Trailer int
}

// Type 1 charstring operators the subset reads past the metrics (Adobe Type
// 1 Font Format, 6.4 to 6.6). csOpEndChar and csOpCallSubr are listed with
// the width scan's operators; a byte above csLastOperator opens a number.
const (
	csOpEscapeSeac          byte = 6  // follows csOpEscape to form seac
	csOpEscapeCallOtherSubr byte = 16 // follows csOpEscape to form callothersubr
	csOpEscapePop           byte = 17 // follows csOpEscape to form pop
	csLastOperator          byte = 31
)

// subrsRe captures the count of the /Subrs array a Type 1 private
// dictionary declares ("/Subrs 42 array", Adobe Type 1 Font Format, 5.2).
var subrsRe = regexp.MustCompile(`/Subrs\s+(\d+)\s+array`)

// closefileMarker ends the encrypted portion of a Type 1 program: the
// interpreter stops decrypting at "currentfile closefile" (Adobe Type 1 Font
// Format, 7.2).
var closefileMarker = []byte("closefile")

// cleartomarkMarker closes the trailer a Type 1 program carries after its
// encrypted portion (Adobe Type 1 Font Format, 2.3).
var cleartomarkMarker = []byte("cleartomark")

// SubsetInPlace returns a Type 1 program in which every glyph of the
// /CharStrings dictionary but .notdef, the glyphs keep names and the base and
// accent glyphs a kept glyph's seac draws has a charstring of its metrics, the
// hsbw or sbw its source charstring opens with, and endchar: it draws nothing
// at the advance and side bearing it had. Every glyph keeps its name and its
// place in the dictionary, so a font selecting a glyph by the name its
// encoding gives a code (ISO 32000-1, 9.6.6) reaches the glyph it reached,
// the /Widths it declares still match, and a /CharSet listing the program's
// glyph names (ISO 19005-1, 6.3.5) still lists exactly the names the program
// holds. A glyph whose metrics a scan of its charstring cannot settle is kept
// whole. The clear-text portion, the /Subrs array and the trailer are kept
// as the source has them: a subroutine is never removed, since a kept glyph
// may reach any of them through callothersubr hint replacement (Adobe Type 1
// Font Format, 8.1), whose subroutine number no static walk settles.
//
// The encrypted portion is decrypted, its emptied charstrings encrypted again
// under the salt bytes the source gave them (Adobe Type 1 Font Format, 7.3,
// lenIV), and the whole portion encrypted again in binary under the salt it
// had (7.2), so the output depends on the input bytes alone. It also returns
// the [Lengths] of the result's three parts.
//
// It fails with [ErrSubsetUnsupported] on a program in PFB segments, one
// whose eexec section is ASCII-hex or has no closefile, one with no
// /CharStrings or a /lenIV below zero (charstrings left unencrypted), one
// whose /Subrs array does not read whole, and one in which a seac sits in a
// subroutine or takes operands other than the literal codes of glyphs the
// Standard encoding names, since the glyphs it draws could then not be kept.
//
// # Corpus evidence
//
//   - All corpora: 293 programs subset in 93 files, which embed 38 programs
//     with seac glyphs (1,758 glyphs); none is refused for a seac.
//   - pdfjs issue4630.pdf and issue11740_reduced.pdf: the two ASCII-hex
//     eexec sections; issue7769.pdf: the two /lenIV -1 programs.
func SubsetInPlace(ctx context.Context, data []byte, keep []string) ([]byte, Lengths, error) {
	src, err := splitForSubset(data)
	if err != nil {
		return nil, Lengths{}, err
	}
	body := src.plain[eexecSalt:]
	lenIV := parseLenIV(body)
	if m := lenIVRe.FindSubmatch(body); m != nil {
		if v, err := strconv.Atoi(string(m[1])); err == nil && v < 0 {
			return nil, Lengths{}, fmt.Errorf("%w: a /lenIV of %d leaves the charstrings unencrypted", ErrSubsetUnsupported, v)
		}
	}
	if err := checkSubrs(ctx, body, lenIV); err != nil {
		return nil, Lengths{}, err
	}
	kept, err := keptGlyphs(ctx, body, src.entries, lenIV, keep)
	if err != nil {
		return nil, Lengths{}, err
	}

	// Every entry ends before closefile, so the rewrite of the portion up to
	// the trailer only replaces the charstrings it empties.
	var out bytes.Buffer
	out.Grow(src.end)
	out.Write(src.plain[:eexecSalt])
	prev := 0
	for i, e := range src.entries {
		if i%ctxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, Lengths{}, fmt.Errorf("subsetting a Type 1 program: %w", err)
			}
		}
		if kept[e.name] {
			continue
		}
		emptied, ok := emptyCharString(body[e.bodyStart:e.bodyEnd], lenIV)
		if !ok {
			continue
		}
		out.Write(body[prev:e.lenStart])
		out.WriteString(strconv.Itoa(len(emptied)))
		out.Write(body[e.lenEnd:e.bodyStart])
		out.Write(emptied)
		prev = e.bodyEnd
	}
	out.Write(body[prev : src.end-eexecSalt])

	encrypted := crypt(out.Bytes(), eexecKey, true)
	program := make([]byte, 0, src.encStart+len(encrypted)+len(data)-src.trailerStart)
	program = append(program, data[:src.encStart]...)
	program = append(program, encrypted...)
	program = append(program, data[src.trailerStart:]...)
	return program, Lengths{Clear: src.encStart, Encrypted: len(encrypted), Trailer: len(data) - src.trailerStart}, nil
}

// ctxCheckEvery is how many charstrings or subroutines the subset handles
// between two context checks.
const ctxCheckEvery = 100

// subsetSource is a Type 1 program split for [SubsetInPlace].
type subsetSource struct {
	// encStart is the offset of the encrypted portion in the program, after
	// the eexec keyword and the white space that follows it.
	encStart int
	// trailerStart is the offset of the trailer, where the encrypted portion
	// ends.
	trailerStart int
	// plain is the decrypted portion, salt included, up to the trailer.
	plain []byte
	// end is the length of plain.
	end int
	// entries are the /CharStrings entries, as offsets past the salt.
	entries []charStringEntry
}

// splitForSubset splits data into its clear-text portion, its encrypted
// portion and its trailer. The encrypted portion ends where the trailer's
// zeros begin before cleartomark, or where a run of zeros and white space
// ends the program, but never before the white space that follows the
// closefile ending the decrypted text (Adobe Type 1 Font Format, 7.2): a
// zero there is cipher text, which a reader decrypts.
func splitForSubset(data []byte) (subsetSource, error) {
	if len(data) > 0 && data[0] == pfbSegmentMarker {
		return subsetSource{}, fmt.Errorf("%w: a program in PFB segments", ErrSubsetUnsupported)
	}
	before, _, ok := bytes.Cut(data, eexecMarker)
	if !ok {
		return subsetSource{}, fmt.Errorf("%w: no eexec section", ErrSubsetUnsupported)
	}
	encStart := len(before) + len(eexecMarker)
	// The first cipher text byte is never white space (Adobe Type 1 Font
	// Format, 7.2), so the white space after the keyword is clear text.
	for encStart < len(data) && isEexecWhitespace(data[encStart]) && data[encStart] != '\f' {
		encStart++
	}
	if isHexEncoded(data[encStart:]) {
		return subsetSource{}, fmt.Errorf("%w: an ASCII-hex eexec section", ErrSubsetUnsupported)
	}
	plain := crypt(data[encStart:], eexecKey, false)
	if len(plain) <= eexecSalt {
		return subsetSource{}, fmt.Errorf("%w: an empty eexec section", ErrSubsetUnsupported)
	}
	entries, ok := charStringEntries(plain[eexecSalt:])
	if !ok || len(entries) == 0 {
		return subsetSource{}, fmt.Errorf("%w: no /CharStrings entry", ErrSubsetUnsupported)
	}
	from := eexecSalt + entries[len(entries)-1].bodyEnd
	at := bytes.Index(plain[from:], closefileMarker)
	if at < 0 {
		return subsetSource{}, fmt.Errorf("%w: no closefile after the charstrings", ErrSubsetUnsupported)
	}
	closeEnd := from + at + len(closefileMarker)
	for closeEnd < len(plain) && isEexecWhitespace(plain[closeEnd]) {
		closeEnd++
	}

	trailer := len(data)
	if k := bytes.LastIndex(data[encStart:], cleartomarkMarker); k >= 0 {
		trailer = encStart + k
	}
	for trailer > encStart && (data[trailer-1] == '0' || isEexecWhitespace(data[trailer-1])) {
		trailer--
	}
	trailer = max(trailer, encStart+closeEnd)
	end := trailer - encStart
	return subsetSource{
		encStart:     encStart,
		trailerStart: trailer,
		plain:        plain[:end],
		end:          end,
		entries:      entries,
	}, nil
}

// pfbSegmentMarker opens each segment header of a font in PFB form, which
// is not the program ISO 32000-1, 9.9 embeds.
const pfbSegmentMarker = 0x80

// emptyCharString returns the encrypted charstring that keeps the metrics of
// enc, an encrypted charstring, and ends: its salt, the operands and the
// hsbw or sbw it opens with, and endchar. It returns false when the scan
// cannot settle the metrics or the result is not shorter.
func emptyCharString(enc []byte, lenIV int) ([]byte, bool) {
	if len(enc) <= lenIV {
		return nil, false
	}
	plain := crypt(enc, charStringKey, false)
	_, end, ok := scanMetrics(plain[lenIV:])
	if !ok {
		return nil, false
	}
	n := lenIV + end
	if n+1 >= len(plain) {
		return nil, false
	}
	emptied := append(slices.Clip(plain[:n]), csOpEndChar)
	return crypt(emptied, charStringKey, true), true
}

// keptGlyphs returns the names whose charstrings the subset keeps whole:
// .notdef, the names of keep, and the base and accent glyphs every kept
// glyph's seac draws, at any depth (Adobe Type 1 Font Format, 6.4 and
// Appendix 3: seac names them by their codes in the Standard encoding).
func keptGlyphs(ctx context.Context, body []byte, entries []charStringEntry, lenIV int, keep []string) (map[string]bool, error) {
	byName := make(map[string][]charStringEntry, len(entries))
	for _, e := range entries {
		byName[e.name] = append(byName[e.name], e)
	}
	kept := make(map[string]bool, len(keep)+1)
	queue := append([]string{".notdef"}, keep...)
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("subsetting a Type 1 program: %w", err)
		}
		name := queue[0]
		queue = queue[1:]
		if kept[name] {
			continue
		}
		kept[name] = true
		for _, e := range byName[name] {
			enc := body[e.bodyStart:e.bodyEnd]
			if len(enc) <= lenIV {
				continue
			}
			codes, found, ok := seacCodes(crypt(enc, charStringKey, false)[lenIV:])
			if !ok {
				return nil, fmt.Errorf("%w: glyph /%s draws through a seac whose operands are not literal codes", ErrSubsetUnsupported, name)
			}
			if !found {
				continue
			}
			for _, code := range codes {
				if component := encodings.StandardEncodingNames[code]; component != "" {
					queue = append(queue, component)
				}
			}
		}
	}
	return kept, nil
}

// seacCodes returns the base and accent codes the seac of a decrypted
// charstring takes, whether it has one, and false when a seac takes operands
// the walk cannot settle: anything but literal integers from 0 to 255, such
// as a value a callothersubr returned through pop. The walk ends at endchar,
// seac or the end of the charstring, and follows no subroutine.
func seacCodes(cs []byte) ([2]byte, bool, bool) {
	var stack [maxCharStringStack]float64
	top := 0
	push := func(v float64) {
		if top == maxCharStringStack {
			// An overflowing stack is a malformed charstring: its operands
			// are unknown from here on.
			copy(stack[:], stack[1:])
			top--
		}
		stack[top] = v
		top++
	}
	unknown := math.NaN()
	for i := 0; i < len(cs); {
		b := cs[i]
		switch {
		case b > csLastOperator:
			v, n := charStringNumber(cs[i:])
			if n == 0 {
				return [2]byte{}, false, true
			}
			push(v)
			i += n
		case b == csOpEscape:
			if i+1 >= len(cs) {
				return [2]byte{}, false, true
			}
			switch cs[i+1] {
			case csOpEscapeSeac:
				if top < 2 {
					return [2]byte{}, true, false
				}
				var codes [2]byte
				for k, v := range stack[top-2 : top] {
					if math.IsNaN(v) || v != math.Trunc(v) || v < 0 || v > math.MaxUint8 {
						return [2]byte{}, true, false
					}
					codes[k] = byte(v)
				}
				return codes, true, true
			case csOpEscapeDiv:
				if top >= 2 {
					q := unknown
					if stack[top-1] != 0 {
						q = stack[top-2] / stack[top-1]
					}
					stack[top-2] = q
					top--
				}
			case csOpEscapePop:
				push(unknown)
			case csOpEscapeCallOtherSubr:
				// Its arguments move to the PostScript stack, and what it
				// leaves there comes back only through pop.
				top = 0
			default:
				top = 0
			}
			i += 2
		case b == csOpEndChar:
			return [2]byte{}, false, true
		case b == csOpCallSubr:
			// The subroutine number is consumed. A subroutine holding seac
			// is refused before the walk (see checkSubrs), and the stack a
			// subroutine leaves for seac would come from its own operands,
			// so the operands below the number are unknown.
			for k := range top {
				stack[k] = unknown
			}
			if top > 0 {
				top--
			}
			i++
		default:
			top = 0
			i++
		}
	}
	return [2]byte{}, false, true
}

// charStringNumber decodes the Type 1 charstring number at the start of cs
// (Adobe Type 1 Font Format, 6.2) and returns it with the bytes it takes, or
// zero bytes when cs ends inside it.
func charStringNumber(cs []byte) (float64, int) {
	b := cs[0]
	switch {
	case b <= 246:
		return float64(int(b) - 139), 1
	case b <= 250:
		if len(cs) < 2 {
			return 0, 0
		}
		return float64((int(b)-247)*256 + int(cs[1]) + 108), 2
	case b <= 254:
		if len(cs) < 2 {
			return 0, 0
		}
		return float64(-(int(b)-251)*256 - int(cs[1]) - 108), 2
	}
	if len(cs) < 5 {
		return 0, 0
	}
	return float64(int32(uint32(cs[1])<<24 | uint32(cs[2])<<16 | uint32(cs[3])<<8 | uint32(cs[4]))), 5 //nolint:gosec // G115: a Type 1 five-byte number is a two's-complement int32 (Adobe Type 1 Font Format, 6.2).
}

// checkSubrs returns an error when a subroutine of the /Subrs array holds a
// seac, whose glyphs a walk of the kept charstrings, which follows no
// subroutine, would not keep, or when the array's definition does not read
// whole, so a subroutine could hold one unseen. The definition is "/Subrs
// COUNT array" and one "dup INDEX LEN RD <LEN bytes> NP" per subroutine,
// closed by ND (Adobe Type 1 Font Format, 5.2). A subset program defines
// fewer entries than COUNT, leaving the others null, so the walk reads
// entries until the definition closes, up to COUNT of them.
//
// # Corpus evidence
//
//   - pdfjs bug1354114.pdf: 13 subset NimbusRomNo9L programs declare
//     "/Subrs 812 array" and define 88 to 125 entries before ND.
func checkSubrs(ctx context.Context, body []byte, lenIV int) error {
	loc := subrsRe.FindSubmatchIndex(body)
	if loc == nil {
		return nil
	}
	count, err := strconv.Atoi(string(body[loc[2]:loc[3]]))
	if err != nil {
		return fmt.Errorf("%w: a /Subrs count that does not read", ErrSubsetUnsupported)
	}
	pos := loc[1]
	for read := 0; read < count; read++ {
		if read%ctxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("subsetting a Type 1 program: %w", err)
			}
		}
		start, end, status := nextSubr(body, pos)
		switch status {
		case subrEntry:
			// Read on.
		case subrsClosed:
			return nil
		case subrsUnread:
			return fmt.Errorf("%w: the /Subrs array stops reading after %d of its %d subroutines", ErrSubsetUnsupported, read, count)
		}
		pos = end
		if end-start <= lenIV {
			continue
		}
		if hasSeac(crypt(body[start:end], charStringKey, false)[lenIV:]) {
			return fmt.Errorf("%w: subroutine %d holds a seac", ErrSubsetUnsupported, read)
		}
	}
	return nil
}

// subrStatus is what [nextSubr] found.
type subrStatus int

const (
	subrEntry subrStatus = iota
	subrsClosed
	subrsUnread
)

// subrSeparators are the tokens a /Subrs definition may hold between two
// entries: the NP procedure or its "|" spelling, or what it stands for.
var subrSeparators = map[string]bool{"NP": true, "|": true, "noaccess": true, "put": true}

// subrsClosers are the tokens that close a /Subrs definition: the ND
// procedure, its "|-" spelling, or what it stands for.
var subrsClosers = map[string]bool{"ND": true, "|-": true, "def": true, "readonly": true}

// nextSubr reads the /Subrs entry at or after pos and returns the bounds of
// its encrypted bytes, or reports that the definition closes there or does
// not read.
func nextSubr(body []byte, pos int) (int, int, subrStatus) {
	for {
		pos = skipWhitespace(body, pos)
		tokEnd := pos
		for tokEnd < len(body) && !isPostScriptWhitespace(body[tokEnd]) {
			tokEnd++
		}
		tok := string(body[pos:tokEnd])
		if tok == "dup" {
			pos = tokEnd
			break
		}
		if subrsClosers[tok] {
			return 0, 0, subrsClosed
		}
		if !subrSeparators[tok] {
			return 0, 0, subrsUnread
		}
		pos = tokEnd
	}
	pos = skipWhitespace(body, pos)
	_, after := readInt(body, pos)
	if after == pos {
		return 0, 0, subrsUnread
	}
	pos = skipWhitespace(body, after)
	length, after := readInt(body, pos)
	if after == pos {
		return 0, 0, subrsUnread
	}
	pos = skipWhitespace(body, after)
	op, afterOp := readReaderOp(body, pos)
	if op == nil {
		return 0, 0, subrsUnread
	}
	pos = afterOp
	if pos < len(body) && isPostScriptWhitespace(body[pos]) {
		pos++
	}
	if length > len(body)-pos {
		return 0, 0, subrsUnread
	}
	return pos, pos + length, subrEntry
}

// hasSeac reports whether a decrypted charstring holds a seac operator.
func hasSeac(cs []byte) bool {
	for i := 0; i < len(cs); {
		b := cs[i]
		switch {
		case b > csLastOperator:
			_, n := charStringNumber(cs[i:])
			if n == 0 {
				return false
			}
			i += n
		case b == csOpEscape:
			if i+1 < len(cs) && cs[i+1] == csOpEscapeSeac {
				return true
			}
			i += 2
		default:
			i++
		}
	}
	return false
}
