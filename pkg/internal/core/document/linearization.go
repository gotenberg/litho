package document

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// LinearizationParams holds the parameters extracted from a Linearization
// Dictionary found in the first 1024 bytes of a linearized PDF.
type LinearizationParams struct {
	// FileLength is the /L value: the total length of the PDF file in bytes.
	FileLength int64

	// HintStreamOffset and HintStreamLength are from the /H array.
	// They point to the primary hint stream.
	HintStreamOffset int64
	HintStreamLength int64

	// FirstPageObjNum is the /O value: the object number of the first page's
	// page object.
	FirstPageObjNum int

	// NumPages is the /N value: the total number of pages in the document.
	NumPages int

	// FirstPageEndOffset is the /E value: the offset of the end of the
	// first-page cross-reference section.
	FirstPageEndOffset int64

	// FirstPageXRefOffset is the offset where the first-page xref/trailer
	// data can be found (typically right after the linearization dict object).
	FirstPageXRefOffset int64
}

// PageOffsetEntry holds the per-page data decoded from the hint stream's
// Page Offset Hint Table. Per ISO 32000-1, Table F.3.
type PageOffsetEntry struct {
	// ObjCount is the number of objects in this page's group.
	ObjCount int

	// PageLength is the total byte length of all objects in this page.
	PageLength int64

	// ContentStreamOffset is the byte offset of the page's primary content
	// stream (relative to the page group start).
	ContentStreamOffset int64
}

// probeLinearization reads the first 1024 bytes and looks for a linearization
// dictionary. If found, it returns the parsed parameters; otherwise nil.
func probeLinearization(r io.ReaderAt, fileSize int64) *LinearizationParams {
	// The linearization dictionary is the very first indirect object in the
	// file, right after the %PDF-X.Y header (and an optional binary comment).
	// We parse it using the standard parser which handles "N G obj ... endobj".
	lex := lexer.NewLexer(ioat.NewReader(r, fileSize, 0))
	p := parser.NewParser(lex)

	obj, err := p.ParseObject(context.Background())
	if err != nil {
		return nil
	}

	// The linearization dict can be a plain dict or a stream object.
	var linDict ast.PdfDict
	switch v := obj.(type) {
	case ast.PdfDict:
		linDict = v
	case ast.PdfStream:
		linDict = v.Dict
	default:
		return nil
	}

	// Check for the /Linearized key.
	if _, ok := linDict["Linearized"]; !ok {
		return nil
	}

	params := &LinearizationParams{}

	// /L: file length.
	if lObj, ok := linDict["L"]; ok {
		params.FileLength = pdfToInt64(lObj)
	}

	// /H: hint stream references [offset length].
	if hObj, ok := linDict["H"]; ok {
		if hArr, ok := hObj.(ast.PdfArray); ok && len(hArr) >= 2 {
			params.HintStreamOffset = pdfToInt64(hArr[0])
			params.HintStreamLength = pdfToInt64(hArr[1])
		}
	}

	// /O: first page object number.
	if oObj, ok := linDict["O"]; ok {
		params.FirstPageObjNum = int(pdfToInt64(oObj))
	}

	// /N: number of pages.
	if nObj, ok := linDict["N"]; ok {
		params.NumPages = int(pdfToInt64(nObj))
	}

	// /E: end of first page section.
	if eObj, ok := linDict["E"]; ok {
		params.FirstPageEndOffset = pdfToInt64(eObj)
	}

	// /T: offset of the first-page xref entry (file trailer).
	if tObj, ok := linDict["T"]; ok {
		params.FirstPageXRefOffset = pdfToInt64(tObj)
	}

	// Validate: /L must match the actual file size. A stale linearization
	// dict left over from a re-save will have a mismatched /L, indicating
	// the file is no longer truly linearized.
	if params.FileLength > 0 && params.FileLength != fileSize {
		return nil
	}

	return params
}

// parseHintStream reads and decodes the primary hint stream, then extracts the
// Page Offset Hint Table entries.
func parseHintStream(r io.ReaderAt, size int64, params *LinearizationParams) ([]PageOffsetEntry, error) {
	if params.HintStreamOffset <= 0 || params.HintStreamLength <= 0 {
		return nil, fmt.Errorf("invalid hint stream offsets")
	}

	// The hint stream is a regular PDF stream object at the given offset.
	lex := lexer.NewLexer(ioat.NewReader(r, size, params.HintStreamOffset))
	p := parser.NewParser(lex)
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to parse hint stream object: %w", err)
	}

	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return nil, fmt.Errorf("hint stream at offset %d is not a stream object", params.HintStreamOffset)
	}

	// Decode the stream data (may be FlateDecode compressed).
	decoded, err := decodeHintStreamData(context.Background(), stream)
	if err != nil {
		return nil, fmt.Errorf("failed to decode hint stream: %w", err)
	}

	return parsePageOffsetTable(decoded, params.NumPages)
}

// parsePageOffsetTable decodes the bit-packed Page Offset Hint Table
// (ISO 32000-1:2008, Table F.3).
//
// The table structure is:
//
//	Header (fixed fields):
//	  - Item  1: min objects per page              (32 bits)
//	  - Item  2: location of first page's objects   (32 bits)
//	  - Item  3: bits needed for obj count delta     (16 bits)
//	  - Item  4: min page length                    (32 bits)
//	  - Item  5: bits needed for page length delta   (16 bits)
//	  - Item  6: min content stream offset          (32 bits) (in the page group)
//	  - Item  7: bits needed for content offset delta (16 bits)
//	  - Item  8: min content stream length          (32 bits)
//	  - Item  9: bits needed for content length delta (16 bits)
//	  - Item 10: bits needed for shared obj ref count (16 bits)
//	  - Item 11: bits needed for shared obj ref      (16 bits)
//	  - Item 12: bits needed for numerator of fraction (16 bits)
//	  - Item 13: shared object denominator           (16 bits)
//	Per-page arrays (each N entries, bit-packed):
//	  - object count deltas       (item3 bits each)
//	  - page length deltas        (item5 bits each)
//	  ... (more arrays we skip for now)
func parsePageOffsetTable(data []byte, numPages int) ([]PageOffsetEntry, error) {
	if len(data) < 28 { // minimum header size
		return nil, fmt.Errorf("hint stream data too short (%d bytes)", len(data))
	}

	br := newBitReader(data)

	// Read header fields.
	//nolint:gosec // Bit-field widths are bounded by the PDF spec; overflow is not possible.
	minObjsPerPage := int(br.readBits(32))
	_ = br.readBits(32) // location of first page's objects (item 2)
	//nolint:gosec // 16-bit field.
	bitsObjCountDelta := int(br.readBits(16))
	//nolint:gosec // 32-bit field fits in int64.
	minPageLength := int64(br.readBits(32))
	//nolint:gosec // 16-bit field.
	bitsPageLenDelta := int(br.readBits(16))
	_ = br.readBits(32) // min content stream offset (item 6)
	_ = br.readBits(16) // bits for content offset delta (item 7)
	_ = br.readBits(32) // min content stream length (item 8)
	_ = br.readBits(16) // bits for content length delta (item 9)
	_ = br.readBits(16) // bits for shared obj ref count (item 10)
	_ = br.readBits(16) // bits for shared obj ref (item 11)
	_ = br.readBits(16) // bits for numerator of fraction (item 12)
	_ = br.readBits(16) // shared object denominator (item 13)

	if br.truncated {
		return nil, fmt.Errorf("hint stream header truncated at offset %d", br.offset)
	}

	entries := make([]PageOffsetEntry, numPages)

	// Array 1: object count deltas (N entries, bitsObjCountDelta bits each).
	for i := range numPages {
		delta := 0
		if bitsObjCountDelta > 0 {
			delta = int(br.readBits(bitsObjCountDelta)) //nolint:gosec // Bounded by bitsObjCountDelta.
		}
		entries[i].ObjCount = minObjsPerPage + delta
	}

	// Align to byte boundary before next array.
	br.alignToByte()

	// Array 2: page length deltas (N entries, bitsPageLenDelta bits each).
	for i := range numPages {
		delta := int64(0)
		if bitsPageLenDelta > 0 {
			delta = int64(br.readBits(bitsPageLenDelta)) //nolint:gosec // Bounded by bitsPageLenDelta.
		}
		entries[i].PageLength = minPageLength + delta
	}

	if br.truncated {
		return nil, fmt.Errorf("hint stream page offset arrays truncated at offset %d", br.offset)
	}

	return entries, nil
}

// decodeHintStreamData decodes the hint stream, handling FlateDecode.
func decodeHintStreamData(ctx context.Context, stream ast.PdfStream) ([]byte, error) {
	filterObj, hasFilter := stream.Dict["Filter"]
	if !hasFilter {
		return stream.Bytes(ctx)
	}

	filterName, ok := filterObj.(ast.PdfName)
	if !ok {
		// Could be an array of filters; for hint streams it's almost always
		// a single FlateDecode.
		if arr, ok := filterObj.(ast.PdfArray); ok && len(arr) > 0 {
			if n, ok := arr[0].(ast.PdfName); ok {
				filterName = n
			}
		}
	}

	if filterName != "FlateDecode" {
		return nil, fmt.Errorf("unsupported hint stream filter: %s", filterName)
	}

	data, err := stream.Bytes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read hint stream data: %w", err)
	}
	return decompressFlate(data)
}

// decompressFlate decompresses FlateDecode (zlib) data.
func decompressFlate(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	const maxDecompressed = 256 << 20 // 256 MB
	var buf bytes.Buffer
	limited := io.LimitReader(r, maxDecompressed+1)
	_, copyErr := io.Copy(&buf, limited)
	if closeErr := r.Close(); closeErr != nil {
		return nil, fmt.Errorf("failed to close zlib reader: %w", closeErr)
	}
	if buf.Len() > maxDecompressed {
		return nil, fmt.Errorf("decompressed hint stream exceeds %d MB limit", maxDecompressed>>20)
	}
	if copyErr != nil && copyErr != io.EOF && copyErr != io.ErrUnexpectedEOF {
		return nil, copyErr
	}
	return buf.Bytes(), nil
}

// pdfToInt64 extracts an int64 from a PdfInt or PdfReal.
func pdfToInt64(obj ast.PdfObject) int64 {
	switch v := obj.(type) {
	case ast.PdfInt:
		return int64(v)
	case ast.PdfReal:
		return int64(v)
	}
	return 0
}

// bitReader reads individual bits from a byte slice, MSB first.
type bitReader struct {
	data      []byte
	offset    int  // current byte offset
	bit       int  // current bit within the byte (0=MSB, 7=LSB)
	truncated bool // set when a read hits the end of data prematurely
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

// readBits reads n bits and returns them as a uint64. If the underlying
// data is exhausted before all n bits are read, the result contains
// only the bits that were available and br.truncated is set to true.
func (br *bitReader) readBits(n int) uint64 {
	var result uint64
	for range n {
		if br.offset >= len(br.data) {
			br.truncated = true
			return result
		}
		bit := (br.data[br.offset] >> (7 - br.bit)) & 1
		result = (result << 1) | uint64(bit)
		br.bit++
		if br.bit > 7 {
			br.bit = 0
			br.offset++
		}
	}
	return result
}

// alignToByte advances to the start of the next byte boundary.
func (br *bitReader) alignToByte() {
	if br.bit > 0 {
		br.bit = 0
		br.offset++
	}
}
