package document

import (
	"bytes"
	"context"
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

// FindStartXRef scans the last 1024 bytes of the PDF for the startxref
// pointer and returns the byte offset it references.
func FindStartXRef(r io.ReaderAt, size int64) (int64, error) {
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
func (d *Document) parseXRefChain(ctx context.Context) error {
	currentOffset, err := FindStartXRef(d.r, d.size)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDamagedXRef, err)
	}

	for currentOffset != 0 {
		lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, currentOffset))
		tok, err := lex.NextToken()
		if err != nil {
			return fmt.Errorf("reading token at xref offset %d: %w", currentOffset, err)
		}

		var prev int64
		switch {
		case tok.Type == lexer.TokenKeyword && tok.Value == "xref":
			d.XRefTableOffsets = append(d.XRefTableOffsets, currentOffset)
			lex = lexer.NewLexer(ioat.NewReader(d.r, d.size, currentOffset))
			trailerDict, p, err := xref.ParseXRefTable(ctx, lex, d.Index.Entries)
			if err != nil {
				return fmt.Errorf("parsing xref table at offset %d: %w", currentOffset, err)
			}
			prev = p
			if d.Index.Trailer == nil {
				d.Index.Trailer = trailerDict
			} else {
				mergeTrailerKeys(d.Index.Trailer, trailerDict)
			}

			// Hybrid-Reference (PDF 1.5): the trailer may contain /XRefStm
			// pointing to a compressed XRef stream whose entries override
			// the legacy plain-text table entries.
			if xRefStmObj, ok := trailerDict["XRefStm"]; ok {
				if xRefStmOffset, ok := xRefStmObj.(ast.PdfInt); ok {
					if err := d.parseHybridXRefStream(ctx, int64(xRefStmOffset)); err != nil {
						return fmt.Errorf("hybrid /XRefStm: %w", err)
					}
				}
			}
		case tok.Type == lexer.TokenNumber:
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
			pVal, err := xref.ParseXRefStream(ctx, stream, d.Index.Entries)
			if err != nil {
				return fmt.Errorf("parsing xref stream at offset %d: %w", currentOffset, err)
			}
			prev = pVal
			if d.Index.Trailer == nil {
				d.Index.Trailer = stream.Dict
			} else {
				mergeTrailerKeys(d.Index.Trailer, stream.Dict)
			}
		default:
			return fmt.Errorf("unexpected token at xref offset: %v", tok.Value)
		}

		currentOffset = prev
	}

	return nil
}

// mergeTrailerKeys copies /Root and /Info from an older trailer into the
// primary trailer when they are missing. This handles linearized PDFs where
// these keys may only appear in the linearization hint trailer.
// /ID and /Encrypt are NOT merged because their presence/absence in the
// primary trailer is compliance-significant (PDF/A 6.1.3).
func mergeTrailerKeys(dst, src ast.PdfDict) {
	for _, k := range []ast.PdfName{"Root", "Info"} {
		if _, exists := dst[k]; !exists {
			if v, ok := src[k]; ok {
				dst[k] = v
			}
		}
	}
}

// parseHybridXRefStream parses a compressed XRef stream embedded in a hybrid-
// reference PDF. Entries from the compressed stream override any corresponding
// entries from the legacy plain-text table.
func (d *Document) parseHybridXRefStream(ctx context.Context, offset int64) error {
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
	if _, err := xref.ParseXRefStream(ctx, stream, hybridEntries); err != nil {
		return fmt.Errorf("parsing hybrid xref stream entries at offset %d: %w", offset, err)
	}

	// Merge: hybrid entries override legacy entries.
	maps.Copy(d.Index.Entries, hybridEntries)

	return nil
}

// validateObjectHeaderFormat performs raw-byte validation of an indirect object
// header "N G obj". The spec requires: object number and generation number
// separated by a single white-space character, generation number and obj
// keyword separated by a single white-space character, obj preceded by EOL,
// endobj preceded by EOL.
func (d *Document) validateObjectHeaderFormat(offset int64, objNum int) {
	// Read enough bytes to cover the header: "NNNNNNNNN GGGGG obj\n" ≈ 25 bytes.
	var buf [40]byte
	n, _ := d.r.ReadAt(buf[:], offset)
	if n < 5 { //nolint:mnd // need at least "N G obj"
		return
	}
	data := buf[:n]

	// Parse: <digits> <separator> <digits> <separator> "obj" <EOL>
	pos := 0

	// Object number: digits.
	for pos < len(data) && data[pos] >= '0' && data[pos] <= '9' {
		pos++
	}
	if pos == 0 || pos >= len(data) {
		return // Can't parse — skip.
	}

	// Separator between object number and generation: must be single SP (0x20).
	if data[pos] != ' ' {
		d.mu.Lock()
		d.FormatViolations = append(d.FormatViolations,
			fmt.Errorf("compliance: obj header %d: object number not followed by single space (0x%02X)", objNum, data[pos]))
		d.mu.Unlock()
		return
	}
	// Check for multiple spaces.
	spCount := 0
	for pos < len(data) && data[pos] == ' ' {
		spCount++
		pos++
	}
	if spCount > 1 {
		d.mu.Lock()
		d.FormatViolations = append(d.FormatViolations,
			fmt.Errorf("compliance: obj header %d: %d spaces between object number and generation (must be 1)", objNum, spCount))
		d.mu.Unlock()
	}

	// Generation number: digits.
	genStart := pos
	for pos < len(data) && data[pos] >= '0' && data[pos] <= '9' {
		pos++
	}
	if pos == genStart || pos >= len(data) {
		return
	}

	// Separator between generation and "obj": must be single SP (0x20).
	if data[pos] != ' ' {
		d.mu.Lock()
		d.FormatViolations = append(d.FormatViolations,
			fmt.Errorf("compliance: obj header %d: generation not followed by single space (0x%02X)", objNum, data[pos]))
		d.mu.Unlock()
		return
	}
	spCount = 0
	for pos < len(data) && data[pos] == ' ' {
		spCount++
		pos++
	}
	if spCount > 1 {
		d.mu.Lock()
		d.FormatViolations = append(d.FormatViolations,
			fmt.Errorf("compliance: obj header %d: %d spaces between generation and obj keyword (must be 1)", objNum, spCount))
		d.mu.Unlock()
	}

	// "obj" keyword.
	if pos+3 > len(data) || string(data[pos:pos+3]) != "obj" {
		return // Not a valid object header — skip.
	}
	pos += 3

	// After "obj": must be followed by EOL marker (LF, CR, or CR+LF).
	if pos < len(data) {
		ch := data[pos]
		if ch != '\n' && ch != '\r' {
			d.mu.Lock()
			d.FormatViolations = append(d.FormatViolations,
				fmt.Errorf("compliance: obj header %d: obj keyword not followed by EOL marker (0x%02X)", objNum, ch))
			d.mu.Unlock()
		}
	}

	// Validate endobj: scan forward to find "endobj" and check it's
	// preceded by an EOL marker (LF or CR).
	d.validateEndobjFormat(offset, objNum)
}

// validateEndobjFormat scans the object body starting at offset to find
// "endobj" and validates it is preceded by an EOL marker.
func (d *Document) validateEndobjFormat(offset int64, objNum int) {
	// Read a generous chunk. Most objects are small; 16KB handles most cases.
	const maxScan = 16384
	buf := make([]byte, maxScan)
	n, _ := d.r.ReadAt(buf, offset)
	if n < 10 { //nolint:mnd
		return
	}
	data := buf[:n]

	// Search for "endobj" — scan from the end since we want the LAST one
	// (objects can contain the string "endobj" in string literals).
	endobjIdx := -1
	for i := len(data) - 6; i >= 0; i-- { //nolint:mnd
		if string(data[i:i+6]) == "endobj" {
			endobjIdx = i
			break
		}
	}
	if endobjIdx < 1 {
		return // Not found or at position 0.
	}

	// Check that endobj is preceded by EOL (LF or CR).
	prevByte := data[endobjIdx-1] //nolint:gosec // endobjIdx >= 2 guaranteed by the check above
	if prevByte != '\n' && prevByte != '\r' {
		d.mu.Lock()
		d.FormatViolations = append(d.FormatViolations,
			fmt.Errorf("compliance: obj %d: endobj not preceded by EOL marker (0x%02X)", objNum, prevByte))
		d.mu.Unlock()
	}

	// Check that endobj is followed by EOL (LF or CR) or EOF.
	afterIdx := endobjIdx + 6 //nolint:mnd
	if afterIdx < len(data) {
		afterByte := data[afterIdx]
		if afterByte != '\n' && afterByte != '\r' {
			d.mu.Lock()
			d.FormatViolations = append(d.FormatViolations,
				fmt.Errorf("compliance: obj %d: endobj not followed by EOL marker (0x%02X)", objNum, afterByte))
			d.mu.Unlock()
		}
	}
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

	// Check for multiple EOL markers (double LF, etc.) — must be a single EOL.
	if pos < len(buf) && (buf[pos] == '\n' || buf[pos] == '\r') {
		d.mu.Lock()
		d.FormatViolations = append(d.FormatViolations,
			fmt.Errorf("compliance: xref keyword and subsection header separated by multiple EOL markers"))
		d.mu.Unlock()
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
