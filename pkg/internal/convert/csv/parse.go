package csv

import (
	"bytes"
	"context"
	"strings"
	"unicode/utf8"
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
	// maxCells bounds the non-empty fields read: 1,000,000. Anti-DoS
	// guard, not spec-mandated: each costs a cell of the model, some
	// hundred bytes, and a printable CSV holds a few thousand.
	maxCells = 1_000_000
)

// separators are the field separators the route's import token names:
// 44, 59 and 9, the comma, the semicolon and the tab, each splitting a
// field alike.
const separators = ",;\t"

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
// after i.
func skipBlanks(text string, i int) int {
	for i < len(text) && text[i] == ' ' {
		i++
	}
	return i
}

// fieldEndQuote reports a quote at text[i] that closes a quoted field: a
// separator, a line end or the end of the data follows it, after blanks
// (lcl_isFieldEndQuote, for the generators that do not double embedded
// quotes).
func fieldEndQuote(text string, i int) bool {
	j := skipBlanks(text, i+1)
	return j >= len(text) || strings.IndexByte(separators, text[j]) >= 0 || text[j] == '\n' || text[j] == '\r'
}

// scanQuoted reads the quoted field whose opening quote is text[q],
// passing its characters to add: doubled quotes are one, a quote that
// does not end the field is a character, a line end inside is a line feed
// (CRLF or CR alike), and the blanks between the closing quote and the
// separator belong to the field. It returns the index after the field and
// false when no quote closes it.
func scanQuoted(text string, q int, add func(rune)) (int, bool) {
	i, n := q+1, len(text)
	for i < n {
		switch c := text[i]; {
		case c == quote && i+1 < n && text[i+1] == quote:
			add(quote)
			i += 2
		case c == quote && fieldEndQuote(text, i):
			for i++; i < n && text[i] == ' '; i++ {
				add(' ')
			}
			return i, true
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
	return q, false
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

// parse splits text into records (RFC 4180, 2, as LibreOffice reads it):
// the line ends are CRLF, LF or CR; a quoted field may hold any of them.
// It checks ctx every 4,096 records.
func parse(ctx context.Context, text string) ([]record, limits, error) {
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
		if len(out)%4096 == 0 && start {
			if err := ctx.Err(); err != nil {
				return nil, lim, err
			}
		}
		if j := skipBlanks(text, i); start && !unclosed && j < n && text[j] == quote {
			// A quoted field, the blanks before its quote skipped
			// (ScImportExport::ScanNextFieldFromString, for the broken
			// generators that write "a", "b").
			if end, ok := scanQuoted(text, j, addRune); ok {
				i, start = end, false
				continue
			}
			// No closing quote before the end of the data: the quote is a
			// character, and the field runs to its separator. No later
			// quote closes either, which keeps the scan linear.
			field.Reset()
			unclosed = true
		}
		c := text[i]
		switch {
		case strings.IndexByte(separators, c) >= 0:
			endField()
			i++
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
