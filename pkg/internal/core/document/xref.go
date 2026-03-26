package document

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

func findStartXRef(r io.ReaderAt, size int64) (int64, error) {
	bufSize := min(size, int64(1024))

	buf := make([]byte, bufSize)
	_, err := r.ReadAt(buf, size-bufSize)
	if err != nil {
		return 0, fmt.Errorf("reading tail bytes for startxref: %w", err)
	}

	idx := bytes.LastIndex(buf, []byte("startxref"))
	if idx == -1 {
		return 0, fmt.Errorf("startxref not found")
	}

	sub := buf[idx+len("startxref"):]
	// bytes.Reader actually implements io.ReaderAt
	br := bytes.NewReader(sub)
	lex := lexer.NewLexer(ioat.NewReader(br, int64(len(sub)), 0))
	tok, err := lex.NextToken()
	if err != nil {
		return 0, fmt.Errorf("scanning token after startxref: %w", err)
	}
	if tok.Type != lexer.TokenNumber {
		return 0, fmt.Errorf("expected offset after startxref, got %v", tok.Value)
	}

	offset, err := strconv.ParseInt(tok.Value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing startxref offset value %q: %w", tok.Value, err)
	}

	return offset, nil
}

// parseXRefChain locates the startxref pointer and walks the chain of xref
// tables/streams (via /Prev links) to populate d.Index.
func (d *Document) parseXRefChain() error {
	currentOffset, err := findStartXRef(d.r, d.size)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDamagedXRef, err)
	}

	for currentOffset != 0 {
		lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, currentOffset))
		tok, err := lex.NextToken()
		if err != nil {
			return fmt.Errorf("reading token at xref offset %d: %w", currentOffset, err)
		}

		var prev int64
		if tok.Type == lexer.TokenKeyword && tok.Value == "xref" {
			d.XRefTableOffsets = append(d.XRefTableOffsets, currentOffset)
			lex = lexer.NewLexer(ioat.NewReader(d.r, d.size, currentOffset))
			trailerDict, p, err := xref.ParseXRefTable(lex, d.Index.Entries)
			if err != nil {
				return fmt.Errorf("parsing xref table at offset %d: %w", currentOffset, err)
			}
			prev = p
			if d.Index.Trailer == nil {
				d.Index.Trailer = trailerDict
			}

			// Hybrid-Reference (PDF 1.5): the trailer may contain /XRefStm
			// pointing to a compressed XRef stream whose entries override
			// the legacy plain-text table entries.
			if xRefStmObj, ok := trailerDict["XRefStm"]; ok {
				if xRefStmOffset, ok := xRefStmObj.(ast.PdfInt); ok {
					if err := d.parseHybridXRefStream(int64(xRefStmOffset)); err != nil {
						return fmt.Errorf("hybrid /XRefStm: %w", err)
					}
				}
			}
		} else if tok.Type == lexer.TokenNumber {
			lex = lexer.NewLexer(ioat.NewReader(d.r, d.size, currentOffset))
			p := parser.NewParser(lex)
			obj, err := p.ParseObject()
			if err != nil {
				return fmt.Errorf("parsing xref stream object at offset %d: %w", currentOffset, err)
			}
			stream, ok := obj.(ast.PdfStream)
			if !ok {
				return fmt.Errorf("expected xref stream, got %T", obj)
			}
			pVal, err := xref.ParseXRefStream(stream, d.Index.Entries)
			if err != nil {
				return fmt.Errorf("parsing xref stream at offset %d: %w", currentOffset, err)
			}
			prev = pVal
			if d.Index.Trailer == nil {
				d.Index.Trailer = stream.Dict
			}
		} else {
			return fmt.Errorf("unexpected token at xref offset: %v", tok.Value)
		}

		currentOffset = prev
	}

	return nil
}

// parseHybridXRefStream parses a compressed XRef stream embedded in a hybrid-
// reference PDF. Entries from the compressed stream override any corresponding
// entries from the legacy plain-text table.
func (d *Document) parseHybridXRefStream(offset int64) error {
	lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, offset))
	p := parser.NewParser(lex)
	obj, err := p.ParseObject()
	if err != nil {
		return fmt.Errorf("parsing hybrid xref stream object at offset %d: %w", offset, err)
	}

	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return fmt.Errorf("expected xref stream at /XRefStm offset %d, got %T", offset, obj)
	}

	// Parse into a temporary map so entries override the legacy table.
	hybridEntries := make(map[int]xref.XRefEntry)
	if _, err := xref.ParseXRefStream(stream, hybridEntries); err != nil {
		return fmt.Errorf("parsing hybrid xref stream entries at offset %d: %w", offset, err)
	}

	// Merge: hybrid entries override legacy entries.
	maps.Copy(d.Index.Entries, hybridEntries)

	return nil
}

// ValidateXRefTableFormat performs raw-byte validation of a traditional xref
// table's format. It reads directly from the underlying io.ReaderAt to check
// byte-level formatting that the lenient lexer-based parser cannot detect.
// Violations are appended to d.FormatViolations. Called by the compliance
// engine after ComplianceMode is set.
func (d *Document) ValidateXRefTableFormat(offset int64) {
	// Read a generous chunk from the xref table start.
	// Most xref tables fit in 64KB; we only need to validate the header
	// and first few entries to detect format issues.
	const maxRead = 65536
	buf := make([]byte, maxRead)
	n, _ := d.r.ReadAt(buf, offset)
	if n < 5 { //nolint:mnd // "xref" + at least 1 byte
		return
	}
	buf = buf[:n]

	// Must start with "xref".
	if !bytes.HasPrefix(buf, []byte("xref")) {
		return
	}
	pos := 4

	// After "xref": must be a single EOL marker (\n or \r\n).
	if pos >= len(buf) {
		return
	}
	switch buf[pos] {
	case '\r':
		pos++
		if pos < len(buf) && buf[pos] == '\n' {
			pos++
		}
		// \r alone is acceptable as EOL here.
	case '\n':
		pos++
	default:
		d.mu.Lock()
		d.FormatViolations = append(d.FormatViolations,
			fmt.Errorf("compliance: xref keyword not followed by EOL"))
		d.mu.Unlock()
		return
	}

	// Check for extra whitespace before subsection header (forbidden).
	if pos < len(buf) && (buf[pos] == ' ' || buf[pos] == '\t') {
		d.mu.Lock()
		d.FormatViolations = append(d.FormatViolations,
			fmt.Errorf("compliance: xref keyword and subsection header separated by extra whitespace"))
		d.mu.Unlock()
	}

	// Parse subsection headers and validate format.
	for pos < len(buf) {
		// Check for "trailer" keyword — end of xref table.
		if pos+7 <= len(buf) && string(buf[pos:pos+7]) == "trailer" {
			break
		}

		// Subsection header: <digits> <SP> <digits> <EOL>
		// The start object number and count must be separated by a single SPACE (0x20).
		headerStart := pos

		// Read start object number (digits).
		for pos < len(buf) && buf[pos] >= '0' && buf[pos] <= '9' {
			pos++
		}
		if pos == headerStart {
			break // Not a digit — likely end of table or malformed.
		}

		// Must be exactly one space (0x20).
		if pos >= len(buf) {
			break
		}
		if buf[pos] != ' ' {
			d.mu.Lock()
			d.FormatViolations = append(d.FormatViolations,
				fmt.Errorf("compliance: xref subsection header separator is not a single space (0x%02X)", buf[pos]))
			d.mu.Unlock()
			break
		}
		sepCount := 0
		for pos < len(buf) && buf[pos] == ' ' {
			sepCount++
			pos++
		}
		if sepCount > 1 {
			d.mu.Lock()
			d.FormatViolations = append(d.FormatViolations,
				fmt.Errorf("compliance: xref subsection header has %d spaces between start and count (must be 1)", sepCount))
			d.mu.Unlock()
		}

		// Read count (digits).
		countStart := pos
		for pos < len(buf) && buf[pos] >= '0' && buf[pos] <= '9' {
			pos++
		}
		if pos == countStart {
			break
		}
		count, err := strconv.Atoi(string(buf[countStart:pos]))
		if err != nil || count < 0 {
			break
		}

		// Skip EOL after header.
		if pos < len(buf) && buf[pos] == '\r' {
			pos++
			if pos < len(buf) && buf[pos] == '\n' {
				pos++
			}
		} else if pos < len(buf) && buf[pos] == '\n' {
			pos++
		}

		// Skip past the entries (each is 20 bytes).
		// Validate individual entries would be ideal but keeping it simple —
		// the subsection header format check catches the most common violations.
		entryBytes := count * 20 //nolint:mnd // 20 bytes per xref entry
		if pos+entryBytes > len(buf) {
			break // Not enough data to skip all entries.
		}
		pos += entryBytes
	}
}
