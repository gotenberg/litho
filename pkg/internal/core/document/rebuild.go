package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

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

// findLastTrailer locates the document's authoritative trailer dictionary.
// It first tries to follow the last startxref pointer (which may point to a
// cross-reference stream whose dict IS the trailer). Falls back to scanning
// for the last "trailer" keyword.
func (d *Document) findLastTrailer() (ast.PdfDict, error) {
	// Try 1: follow the last startxref pointer.
	if dict := d.findTrailerViaStartXRef(); dict != nil {
		return dict, nil
	}

	// Try 2: scan backwards for the last "trailer" keyword.
	const searchBuf = 4096
	searchSize := min(d.size, int64(searchBuf))

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

// findTrailerViaStartXRef scans backwards through all %%EOF markers and tries
// each startxref pointer. Returns the trailer from the first valid one found
// (cross-reference stream dict or traditional xref trailer).
func (d *Document) findTrailerViaStartXRef() ast.PdfDict {
	// Read the last portion of the file to find %%EOF markers.
	// Use a large window to cover incremental updates that may add
	// significant data between the second-to-last and last %%EOF.
	searchSize := min(d.size, 65536)
	buf := make([]byte, searchSize)
	baseOffset := d.size - searchSize
	if _, err := d.r.ReadAt(buf, baseOffset); err != nil && err != io.EOF {
		return nil
	}

	// Find all %%EOF markers (last one first).
	eofPattern := []byte("%%EOF")
	var eofPositions []int
	pos := len(buf)
	for {
		idx := bytes.LastIndex(buf[:pos], eofPattern)
		if idx == -1 {
			break
		}
		eofPositions = append(eofPositions, idx)
		pos = idx
	}

	// Try each %%EOF's startxref, newest first.
	for _, eofIdx := range eofPositions {
		region := buf[max(0, eofIdx-60):eofIdx]
		sxrIdx := bytes.LastIndex(region, []byte("startxref"))
		if sxrIdx == -1 {
			continue
		}
		after := strings.TrimSpace(string(region[sxrIdx+len("startxref"):]))
		var xrefOffset int64
		if _, err := fmt.Sscanf(after, "%d", &xrefOffset); err != nil {
			continue
		}
		if xrefOffset <= 0 || xrefOffset >= d.size {
			continue
		}

		// Check what's at the offset.
		probe := make([]byte, 16)
		if _, err := d.r.ReadAt(probe, xrefOffset); err != nil {
			continue
		}
		trimmed := bytes.TrimSpace(probe)

		// Traditional xref table: parse the trailer after the table.
		if bytes.HasPrefix(trimmed, []byte("xref")) {
			if dict := d.parseTrailerAfterXRef(xrefOffset); dict != nil {
				return dict
			}
			continue
		}

		// Cross-reference stream: parse the object.
		if dict := d.parseXRefStreamDict(xrefOffset); dict != nil {
			return dict
		}
	}

	return nil
}

// parseTrailerAfterXRef reads an xref table at offset and returns its
// trailer dictionary. The xref entries themselves are discarded.
func (d *Document) parseTrailerAfterXRef(offset int64) ast.PdfDict {
	// Scan for "trailer" keyword after the xref table.
	readLen := min(d.size-offset, 65536)
	buf := make([]byte, readLen)
	if _, err := d.r.ReadAt(buf, offset); err != nil && err != io.EOF {
		return nil
	}
	idx := bytes.Index(buf, []byte("trailer"))
	if idx == -1 {
		return nil
	}
	dictStart := offset + int64(idx) + int64(len("trailer"))
	lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, dictStart))
	p := parser.NewParser(lex)
	obj, err := p.ParseObject()
	if err != nil {
		return nil
	}
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		return nil
	}
	return dict
}

// parseXRefStreamDict parses a cross-reference stream and returns its dict.
func (d *Document) parseXRefStreamDict(offset int64) ast.PdfDict {
	lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, offset))
	p := parser.NewParser(lex)
	obj, err := p.ParseObject()
	if err != nil {
		return nil
	}
	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return nil
	}
	if stream.Dict == nil {
		return nil
	}
	if typeObj, ok := stream.Dict["Type"]; ok {
		if typeName, ok := typeObj.(ast.PdfName); ok && typeName == "XRef" {
			return stream.Dict
		}
	}
	return nil
}
