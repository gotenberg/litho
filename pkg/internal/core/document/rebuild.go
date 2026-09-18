package document

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"regexp"
	"slices"
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
	trailer, err := d.findLastTrailer(ctx)
	if err != nil {
		// Last-resort: synthesize a minimal trailer. The file has
		// parseable objects but no recoverable trailer, so downstream
		// recovery has to locate /Root by scanning object bodies.
		// [ResolveCatalog] walks the entry table looking for a
		// dictionary tagged /Type /Catalog, which does not require a
		// pre-computed /Root.
		trailer = synthesizeTrailer(entries)
		d.TrailerSynthesized = true
	} else if _, hasInfo := trailer["Info"]; !hasInfo {
		// A damaged file's LAST trailer can be a stub while the real one sits
		// earlier: govdocs thread_100_100982.pdf and thread_100_100983.pdf both
		// end with `trailer<</Size N/ID[...]>>` and carry
		// `/Info N 0 R /Root N 0 R /Prev N` in a trailer near the start, whose
		// own startxref offset is a malformed 0. The xref CHAIN path already
		// carries these keys forward across sections with [mergeTrailerKeys];
		// the rebuild path did not, so a trailer the file plainly has went
		// missing and ISO 19005-4, 6.1.3 t4 and t5, which read the trailer
		// /Info, reported nothing while veraPDF reported both.
		//
		// Only /Info is recovered. /Root is deliberately left alone: a
		// synthesized trailer omits it so [ResolveCatalog] scans for
		// /Type /Catalog, and that recovery path carries a documented prior
		// regression. This branch also never runs for a synthesized trailer,
		// so a trailer litho invented gains nothing it cannot evidence.
		if infoRef, ok := d.findTrailerInfoRef(ctx); ok {
			trailer["Info"] = infoRef
		}
	}

	d.Index.Entries = entries
	d.Index.Trailer = trailer

	return nil
}

// expandObjectStreamsForRebuild augments a raw-scan rebuild with the objects
// held inside recovered object streams, registering each as a compressed entry.
// It never overrides an entry already recovered (an uncompressed copy, or a
// later incremental update, supersedes the stream copy) and tolerates
// unparseable or encrypted streams: recovery is best-effort.
func (d *Document) expandObjectStreamsForRebuild(ctx context.Context, entries map[int]xref.XRefEntry) {
	// A definition later in the file supersedes an earlier one, which is the
	// rule the raw scan above already follows for uncompressed headers. Object
	// streams have to follow it too: ordering them by object number instead
	// let an OLD revision's stream win, and issue14269.pdf came back with the
	// first revision's /Annots array, one signature widget short.
	srcOffset := make(map[int]int64, len(entries))
	stmCandidates := make([]int, 0, len(entries))
	for objNum, e := range entries {
		if e.Type == xref.XRefTypeNormal {
			stmCandidates = append(stmCandidates, objNum)
			srcOffset[objNum] = e.Offset
		}
	}
	slices.SortFunc(stmCandidates, func(a, b int) int {
		return cmp.Compare(entries[a].Offset, entries[b].Offset)
	})

	for _, objStmNum := range stmCandidates {
		if ctx.Err() != nil {
			return
		}
		obj, err := d.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objStmNum})
		if err != nil {
			continue
		}
		stream, ok := obj.(ast.PdfStream)
		if !ok {
			continue
		}
		if name, _ := stream.Dict["Type"].(ast.PdfName); name != "ObjStm" {
			continue
		}
		data, err := d.parseObjectStream(ctx, objStmNum, map[int]bool{})
		if err != nil || data == nil {
			continue
		}
		stmOffset := entries[objStmNum].Offset
		for idx, contained := range data.objNums {
			if contained <= 0 || contained == objStmNum {
				continue
			}
			// Keep whichever definition sits later in the file.
			if prev, exists := srcOffset[contained]; exists && prev >= stmOffset {
				continue
			}
			entries[contained] = xref.XRefEntry{
				Type:        xref.XRefTypeCompressed,
				ObjStmNum:   objStmNum,
				ObjStmIndex: idx,
			}
			srcOffset[contained] = stmOffset
		}
	}
}

// synthesizeTrailer builds a minimal trailer for documents whose
// original trailer is unrecoverable. Only /Size is set to the highest
// object number plus one. /Root is intentionally omitted so the
// catalog-recovery path sees a missing pointer and performs its scan.
func synthesizeTrailer(entries map[int]xref.XRefEntry) ast.PdfDict {
	maxObj := 0
	for objNum := range entries {
		if objNum > maxObj {
			maxObj = objNum
		}
	}
	return ast.PdfDict{
		"Size": ast.PdfInt(maxObj + 1),
	}
}

// maxRecoveredTrailers caps how many trailer dictionaries
// [Document.findTrailerInfoRef] parses. Anti-DoS guard, not spec-mandated: the
// count of "trailer" keywords is untrusted input, while a real file carries one
// per revision and a handful of revisions is already unusual.
const maxRecoveredTrailers = 16

// findTrailerInfoRef returns the /Info value of the newest trailer in the file
// that states one, for a rebuild whose recovered trailer has none.
//
// The scan runs over the whole file rather than a window near the end: in the
// files this exists for, the stub trailer is last while the trailer carrying
// /Info sits about a kilobyte into a hundred-kilobyte document, so a tail
// window never reaches it. Candidates are examined newest first, matching the
// rule the rebuild's object scan already follows, and only /Info is read.
//
// The scan reads the whole file, so it checks ctx once per chunk and reports
// nothing found once the caller cancels.
func (d *Document) findTrailerInfoRef(ctx context.Context) (ast.PdfObject, bool) {
	keyword := []byte("trailer")
	const overlap = 16 // longer than the keyword, so a chunk boundary cannot split it.

	var offsets []int64
	buf := make([]byte, rebuildChunkSize)
	for filePos := int64(0); filePos < d.size; {
		if ctx.Err() != nil {
			return nil, false
		}
		n, err := d.r.ReadAt(buf, filePos)
		if n == 0 {
			break
		}
		chunk := buf[:n]
		for searched := 0; ; {
			idx := bytes.Index(chunk[searched:], keyword)
			if idx == -1 {
				break
			}
			off := filePos + int64(searched+idx) + int64(len(keyword))
			// Chunks overlap so a keyword cannot be split across a boundary,
			// which means one inside the overlap region is seen twice. Offsets
			// only ever increase, so dropping a repeat keeps
			// [maxRecoveredTrailers] a bound on DISTINCT trailers rather than
			// on sightings. The object scan above needs no such guard because
			// it keys a map by object number.
			if len(offsets) == 0 || off > offsets[len(offsets)-1] {
				offsets = append(offsets, off)
			}
			searched += idx + len(keyword)
		}
		if err != nil {
			break
		}
		advance := int64(n) - overlap
		if advance <= 0 {
			break
		}
		filePos += advance
	}

	for i := len(offsets) - 1; i >= 0 && len(offsets)-i <= maxRecoveredTrailers; i-- {
		lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, offsets[i]))
		obj, err := parser.NewParser(lex).ParseObject(ctx)
		if err != nil {
			continue
		}
		dict, ok := obj.(ast.PdfDict)
		if !ok {
			continue
		}
		if infoRef, ok := dict["Info"]; ok {
			return infoRef, true
		}
	}
	return nil, false
}

// findLastTrailer locates the document's authoritative trailer dictionary.
// It first tries to follow the last startxref pointer (which may point to a
// cross-reference stream whose dict IS the trailer). Falls back to scanning
// for the last "trailer" keyword.
func (d *Document) findLastTrailer(ctx context.Context) (ast.PdfDict, error) {
	// Try 1: follow the last startxref pointer.
	if dict := d.findTrailerViaStartXRef(ctx); dict != nil {
		return dict, nil
	}

	// Try 2: scan backwards for the last "trailer" keyword.
	const searchBuf = 4096
	searchSize := min(d.size, int64(searchBuf))

	// Each pass doubles the window, so a file holding no trailer keyword is
	// read almost in full before the scan gives up. Check ctx once per pass.
	for window := searchSize; window <= d.size; window = min(window*2, d.size) {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("scanning for the last trailer: %w", err)
		}
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
		obj, err := p.ParseObject(ctx)
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
func (d *Document) findTrailerViaStartXRef(ctx context.Context) ast.PdfDict {
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
		if ctx.Err() != nil {
			return nil
		}
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
			if dict := d.parseTrailerAfterXRef(ctx, xrefOffset); dict != nil {
				return dict
			}
			continue
		}

		// Cross-reference stream: parse the object.
		if dict := d.parseXRefStreamDict(ctx, xrefOffset); dict != nil {
			return dict
		}
	}

	return nil
}

// parseTrailerAfterXRef reads an xref table at offset and returns its
// trailer dictionary. The xref entries themselves are discarded.
func (d *Document) parseTrailerAfterXRef(ctx context.Context, offset int64) ast.PdfDict {
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
	obj, err := p.ParseObject(ctx)
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
func (d *Document) parseXRefStreamDict(ctx context.Context, offset int64) ast.PdfDict {
	lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, offset))
	p := parser.NewParser(lex)
	obj, err := p.ParseObject(ctx)
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
