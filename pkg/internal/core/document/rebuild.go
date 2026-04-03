package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// rebuildChunkSize is the read buffer size used when scanning the file
// for object headers during xref reconstruction.
const rebuildChunkSize = 64 * 1024

// objHeaderRe matches PDF indirect object headers like "12 0 obj".
var objHeaderRe = regexp.MustCompile(`(\d+)\s+(\d+)\s+obj\b`)

// rebuildXRef performs a brute-force scan of the entire file to reconstruct
// a cross-reference table when the original xref data is damaged or missing.
// It finds every "N G obj" header, records its byte offset, and locates the
// trailer dictionary. The resulting xref.Index is written into doc.Index.
func (d *Document) rebuildXRef(ctx context.Context) error {
	entries := make(map[int]xref.XRefEntry)

	// Scan the file in overlapping chunks to avoid missing headers that
	// span a chunk boundary. The overlap must be large enough to cover the
	// longest plausible "N G obj" header (e.g. "999999999 65535 obj" = ~25 bytes).
	const overlap = 64

	buf := make([]byte, rebuildChunkSize+overlap)
	var filePos int64

	for filePos < d.size {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("rebuild xref at %d: %w", filePos, err)
		}

		readLen := min(int64(len(buf)), d.size-filePos)
		n, err := d.r.ReadAt(buf[:readLen], filePos)
		if err != nil && err != io.EOF {
			return fmt.Errorf("rebuild xref read error at %d: %w", filePos, err)
		}
		if n == 0 {
			break
		}

		chunk := buf[:n]
		matches := objHeaderRe.FindAllSubmatchIndex(chunk, -1)
		for _, m := range matches {
			// m[0]:m[1] is the full match, m[2]:m[3] is obj num, m[4]:m[5] is gen num.
			absOffset := filePos + int64(m[0])

			objNum, err := strconv.Atoi(string(chunk[m[2]:m[3]]))
			if err != nil {
				continue
			}
			genNum, err := strconv.Atoi(string(chunk[m[4]:m[5]]))
			if err != nil {
				continue
			}

			// Keep the last occurrence: in incrementally updated PDFs,
			// later definitions override earlier ones.
			entries[objNum] = xref.XRefEntry{
				Offset:     absOffset,
				Generation: genNum,
				Type:       1,
			}
		}

		// Advance past the chunk, minus the overlap to catch boundary spans.
		advance := int64(n) - overlap
		if advance <= 0 {
			break
		}
		filePos += advance
	}

	if len(entries) == 0 {
		return fmt.Errorf("%w: rebuild found no objects", ErrDamagedXRef)
	}

	// Find the last trailer dictionary in the file.
	trailer, err := d.findLastTrailer()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDamagedXRef, err)
	}

	d.Index.Entries = entries
	d.Index.Trailer = trailer

	return nil
}

// findLastTrailer scans the file backwards for the last "trailer" keyword
// and parses the dictionary that follows it.
func (d *Document) findLastTrailer() (ast.PdfDict, error) {
	// Read the file in reverse chunks looking for "trailer".
	const searchBuf = 4096
	searchSize := min(d.size, int64(searchBuf))

	// Try progressively larger windows from the end of the file.
	for window := searchSize; window <= d.size; window = min(window*2, d.size) {
		buf := make([]byte, window)
		offset := d.size - window
		_, err := d.r.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read trailer region: %w", err)
		}

		idx := bytes.LastIndex(buf, []byte("trailer"))
		if idx == -1 {
			if window >= d.size {
				break
			}
			continue
		}

		// Parse the dictionary starting right after "trailer".
		dictStart := offset + int64(idx) + int64(len("trailer"))
		lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, dictStart))
		p := parser.NewParser(lex)
		obj, err := p.ParseObject()
		if err != nil {
			return nil, fmt.Errorf("failed to parse trailer dictionary: %w", err)
		}

		dict, ok := obj.(ast.PdfDict)
		if !ok {
			return nil, fmt.Errorf("trailer is not a dictionary: %T", obj)
		}
		return dict, nil
	}

	return nil, fmt.Errorf("trailer keyword not found in file")
}
