package csv

import (
	"bytes"
	"context"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
)

// The caps of one CSV file.
const (
	// maxRows is the rows read, 1,048,576, and maxColumns the fields of a
	// row, 16,384: Calc's sheet size since LibreOffice 7.4. Calc drops
	// what lies past them with a warning; so does the reader.
	maxRows    = 1 << 20
	maxColumns = 16384
	// maxFieldBytes bounds one field's bytes: 65,536. Anti-DoS guard, not
	// spec-mandated: an unterminated quote otherwise makes the rest of
	// the file one field; a spreadsheet cell holds a sentence or a
	// paragraph.
	maxFieldBytes = 1 << 16
	// maxCells bounds the non-empty fields read: sheet.MaxCells, which
	// every workbook reader applies.
	maxCells = sheet.MaxCells
)

// ctxCheckRecords is how many records the reader goes through between
// two checks of its context.
const ctxCheckRecords = 4096

// separators is a set of field separators, any of which ends a field.
type separators struct {
	// ascii marks the separators below U+0080; other lists the rest.
	ascii [utf8.RuneSelf]bool
	other []rune
	// blank marks a space among the separators: blanks around a quoted
	// field then separate fields rather than belong to them.
	blank bool
}

// newSeparators returns the set of rs, a comma alone for none (RFC 4180,
// 2). The caller refuses a quote, a line end and U+FFFD.
func newSeparators(rs []rune) *separators {
	if len(rs) == 0 {
		rs = []rune{','}
	}
	s := &separators{}
	for _, r := range rs {
		if r >= 0 && r < utf8.RuneSelf {
			s.ascii[r] = true
		} else {
			s.other = append(s.other, r)
		}
	}
	s.blank = s.ascii[' ']
	return s
}

// at returns the byte length of the separator text holds at i, 0 for
// none.
func (s *separators) at(text string, i int) int {
	if c := text[i]; c < utf8.RuneSelf {
		if s.ascii[c] {
			return 1
		}
		return 0
	}
	if len(s.other) == 0 {
		return 0
	}
	r, size := utf8.DecodeRuneInString(text[i:])
	for _, o := range s.other {
		if r == o {
			return size
		}
	}
	return 0
}

// quote is the text delimiter of the token, 34.
const quote = '"'

// record is one line of fields, Calc's import semantics: a field
// starting with a quote runs to the closing quote that a separator or the
// line's end follows, doubled quotes inside being one; a quote elsewhere
// is a character of the field.
type record []string

// limits reports what parse dropped.
type limits struct {
	rows, columns, bytes, cells bool
}

// skipBlanks returns the index of the first non-blank byte of text at or
// after i; i itself when a space separates fields.
func (s *separators) skipBlanks(text string, i int) int {
	for !s.blank && i < len(text) && text[i] == ' ' {
		i++
	}
	return i
}

// fieldEndQuote reports a quote at text[i] that closes a quoted field: a
// separator, a line end or the end of the data follows it, after blanks
// (lcl_isFieldEndQuote, for the generators that do not double embedded
// quotes).
func (s *separators) fieldEndQuote(text string, i int) bool {
	j := s.skipBlanks(text, i+1)
	return j >= len(text) || s.at(text, j) > 0 || text[j] == '\n' || text[j] == '\r'
}

// scanQuoted reads the quoted field whose opening quote is text[q],
// passing its characters to add: doubled quotes are one, a quote that
// does not end the field is a character, a line end inside is a line feed
// (CRLF or CR alike), and the blanks between the closing quote and the
// separator belong to the field. A line end the field reaches after a
// quote that is a character ends the line, as LibreOffice's ReadCsvLine
// stops at a DONTKNOW_QUOTE: the field then has no closing quote. It
// returns the index after the field, false when no quote closes it, and
// whether the scan then reached the end of the data.
func (s *separators) scanQuoted(text string, q int, add func(rune)) (int, bool, bool) {
	i, n := q+1, len(text)
	stray := false
	for i < n {
		switch c := text[i]; {
		case c == quote && i+1 < n && text[i+1] == quote:
			add(quote)
			i += 2
			stray = false
		case c == quote && s.fieldEndQuote(text, i):
			for i++; !s.blank && i < n && text[i] == ' '; i++ {
				add(' ')
			}
			return i, true, false
		case c == quote:
			add(quote)
			i++
			stray = true
		case (c == '\r' || c == '\n') && stray:
			return q, false, false
		case c == '\r':
			add('\n')
			i++
			if i < n && text[i] == '\n' {
				i++
			}
		default:
			r, size := utf8.DecodeRuneInString(text[i:])
			add(r)
			i += size
		}
	}
	return q, false, true
}

// decode returns data as UTF-8 text: a UTF-8 byte order mark skipped,
// invalid bytes replaced by U+FFFD (the token's character set 76 is
// UTF-8).
func decode(data []byte) string {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if utf8.Valid(data) {
		return string(data)
	}
	return strings.ToValidUTF8(string(data), "�")
}

// parse splits text into records at the separators seps (RFC 4180, 2, as
// LibreOffice reads it): the line ends are CRLF, LF or CR; a quoted field
// may hold any of them. It checks ctx every
// [ctxCheckRecords] records.
func parse(ctx context.Context, text string, seps *separators) ([]record, limits, error) {
	var (
		out   []record
		lim   limits
		cur   record
		field strings.Builder
		cells int
	)
	addRune := func(r rune) {
		if r < 0x20 && r != '\t' && r != '\n' {
			// A control character draws nothing in Calc's cell.
			return
		}
		if field.Len()+utf8.RuneLen(r) > maxFieldBytes {
			lim.bytes = true
			return
		}
		field.WriteRune(r)
	}
	endField := func() {
		if len(cur) >= maxColumns {
			lim.columns = true
			field.Reset()
			return
		}
		s := field.String()
		if s != "" {
			if cells >= maxCells {
				lim.cells = true
				s = ""
			} else {
				cells++
			}
		}
		cur = append(cur, s)
		field.Reset()
	}
	endRecord := func() bool {
		endField()
		if len(out) >= maxRows {
			lim.rows = true
			return false
		}
		out = append(out, cur)
		cur = nil
		return true
	}
	i, n := 0, len(text)
	start, unclosed := true, false
	for i < n {
		if len(out)%ctxCheckRecords == 0 && start {
			if err := ctx.Err(); err != nil {
				return nil, lim, err
			}
		}
		if j := seps.skipBlanks(text, i); start && !unclosed && j < n && text[j] == quote {
			// A quoted field, the blanks before its quote skipped
			// (ScImportExport::ScanNextFieldFromString, for the broken
			// generators that write "a", "b").
			end, ok, eof := seps.scanQuoted(text, j, addRune)
			if ok {
				i, start = end, false
				continue
			}
			// No closing quote: the quote is a character, and the field
			// runs to its separator. When the scan reached the end of the
			// data, no later quote closes either, which keeps the scan
			// linear.
			field.Reset()
			unclosed = eof
		}
		c := text[i]
		switch size := seps.at(text, i); {
		case size > 0:
			endField()
			i += size
			start = true
		case c == '\r' || c == '\n':
			i++
			if c == '\r' && i < n && text[i] == '\n' {
				i++
			}
			if !endRecord() {
				return out, lim, nil
			}
			start = true
		default:
			r, size := utf8.DecodeRuneInString(text[i:])
			addRune(r)
			i += size
			start = false
		}
	}
	if field.Len() > 0 || len(cur) > 0 || !start {
		endRecord()
	}
	return out, lim, nil
}
