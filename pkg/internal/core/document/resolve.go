package document

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// GetFloat safely extracts a float64 from a [ast.PdfObject].
// Returns 0 if the object is neither a [ast.PdfInt] nor a [ast.PdfReal].
func GetFloat(obj ast.PdfObject) float64 {
	switch v := obj.(type) {
	case ast.PdfReal:
		return float64(v)
	case ast.PdfInt:
		return float64(v)
	}
	return 0
}

// ResolveObject resolves an indirect reference through the [Document.Resolve]
// method, which provides circular-reference protection and object caching.
// If obj is not an [ast.PdfIndirectRef], it is returned as-is.
//
// This is the single authorised way to chase indirect refs across all internal
// packages. Callers must never re-implement this logic locally.
func ResolveObject(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return doc.Resolve(ctx, ref)
	}
	return obj, nil
}

// isNullOrNil returns true if the resolved object is nil or PdfNull,
// which are valid "absent" values that should not produce type errors.
func isNullOrNil(obj ast.PdfObject) bool {
	if obj == nil {
		return true
	}
	_, isNull := obj.(ast.PdfNull)
	return isNull
}

// ResolveDict resolves obj and returns it as a [ast.PdfDict]. Returns (nil, error)
// when the resolved object is a non-null type other than [ast.PdfDict].
func ResolveDict(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfDict, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, err
	}
	if isNullOrNil(resolved) {
		return nil, nil
	}
	dict, ok := resolved.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("expected PdfDict, got %T", resolved)
	}
	return dict, nil
}

// ResolveDictRequired resolves obj and returns it as a [ast.PdfDict], returning an
// error if the resolved object is not a dictionary.
func ResolveDictRequired(ctx context.Context, doc *Document, obj ast.PdfObject, context string) (ast.PdfDict, error) {
	dict, err := ResolveDict(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", context, err)
	}
	if dict == nil {
		return nil, fmt.Errorf("%s: expected dictionary, got null or nil", context)
	}
	return dict, nil
}

// ResolveArray resolves obj and returns it as a [ast.PdfArray]. Returns (nil, error)
// when the resolved object is a non-null type other than [ast.PdfArray].
func ResolveArray(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfArray, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, err
	}
	if isNullOrNil(resolved) {
		return nil, nil
	}
	arr, ok := resolved.(ast.PdfArray)
	if !ok {
		return nil, fmt.Errorf("expected PdfArray, got %T", resolved)
	}
	return arr, nil
}

// ResolveName resolves obj and returns it as a [ast.PdfName]. Returns ("", error)
// when the resolved object is a non-null type other than [ast.PdfName].
func ResolveName(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfName, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return "", err
	}
	if isNullOrNil(resolved) {
		return "", nil
	}
	name, ok := resolved.(ast.PdfName)
	if !ok {
		return "", fmt.Errorf("expected PdfName, got %T", resolved)
	}
	return name, nil
}

// ResolveStream resolves obj and returns it as a [ast.PdfStream]. Returns (zero, error)
// when the resolved object is a non-null type other than [ast.PdfStream].
func ResolveStream(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfStream, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return ast.PdfStream{}, err
	}
	if isNullOrNil(resolved) {
		return ast.PdfStream{}, nil
	}
	stream, ok := resolved.(ast.PdfStream)
	if !ok {
		return ast.PdfStream{}, fmt.Errorf("expected PdfStream, got %T", resolved)
	}
	return stream, nil
}

// ResolveString resolves obj and returns it as a [ast.PdfString]. Returns ("", error)
// when the resolved object is a non-null type other than [ast.PdfString].
func ResolveString(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfString, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return "", err
	}
	if isNullOrNil(resolved) {
		return "", nil
	}
	s, ok := resolved.(ast.PdfString)
	if !ok {
		return "", fmt.Errorf("expected PdfString, got %T", resolved)
	}
	return s, nil
}

// ResolveInt resolves obj and returns it as a [ast.PdfInt]. Returns (0, error)
// when the resolved object is a non-null type other than [ast.PdfInt].
func ResolveInt(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfInt, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return 0, err
	}
	if isNullOrNil(resolved) {
		return 0, nil
	}
	i, ok := resolved.(ast.PdfInt)
	if !ok {
		return 0, fmt.Errorf("expected PdfInt, got %T", resolved)
	}
	return i, nil
}

// ResolveBool resolves obj and returns it as a [ast.PdfBool]. Returns (false, error)
// when the resolved object is a non-null type other than [ast.PdfBool].
func ResolveBool(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfBool, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return false, err
	}
	if isNullOrNil(resolved) {
		return false, nil
	}
	b, ok := resolved.(ast.PdfBool)
	if !ok {
		return false, fmt.Errorf("expected PdfBool, got %T", resolved)
	}
	return b, nil
}

// UTF-16BE byte order mark bytes.
const (
	utf16BEBom0 = 0xFE
	utf16BEBom1 = 0xFF
)

// DecodePdfString converts a raw [ast.PdfString] to a Go string, handling the
// UTF-16BE BOM (0xFE 0xFF) encoding used by many PDF producers.
//
// DecodePdfString operates on already-decrypted string data. Decryption is
// handled transparently by the parser's crypto pipeline during object
// resolution, so by the time a [ast.PdfString] reaches this function the
// cleartext bytes are guaranteed.
func DecodePdfString(s ast.PdfString) string {
	b := []byte(s)
	if len(b) >= 2 && b[0] == utf16BEBom0 && b[1] == utf16BEBom1 {
		u16s := make([]uint16, (len(b)-2)/2)
		for i := range u16s {
			u16s[i] = uint16(b[2+i*2])<<8 | uint16(b[2+i*2+1])
		}
		return string(utf16.Decode(u16s))
	}
	// UTF-16LE BOM (0xFF 0xFE): little-endian byte order.
	if len(b) >= 2 && b[0] == utf16BEBom1 && b[1] == utf16BEBom0 {
		u16s := make([]uint16, (len(b)-2)/2)
		for i := range u16s {
			u16s[i] = uint16(b[2+i*2+1])<<8 | uint16(b[2+i*2])
		}
		return string(utf16.Decode(u16s))
	}
	// PDFDocEncoding / Windows-1252: bytes 0x80-0x9F have specific Unicode
	// mappings; bytes 0xA0-0xFF map to the same Unicode code point.
	// All bytes > 0x7F must be converted to proper UTF-8 runes since Go
	// strings are UTF-8 and raw high bytes are invalid sequences.
	needsDecode := false
	for _, c := range b {
		if c > 0x7F {
			needsDecode = true
			break
		}
	}
	if !needsDecode {
		return string(b)
	}
	var buf strings.Builder
	buf.Grow(len(b))
	for _, c := range b {
		if r, ok := pdfDocEncoding[c]; ok {
			buf.WriteRune(r)
		} else if c > 0x7F {
			// Bytes 0xA0-0xFF: same Unicode code point as byte value.
			buf.WriteRune(rune(c))
		} else {
			buf.WriteByte(c)
		}
	}
	return buf.String()
}

// pdfDocEncoding maps bytes in the 0x80-0x9F range to their Unicode
// equivalents per ISO 32000-2, Table D.2 (PDFDocEncoding).
var pdfDocEncoding = map[byte]rune{
	0x80: 0x2022, // BULLET
	0x81: 0x2020, // DAGGER
	0x82: 0x2021, // DOUBLE DAGGER
	0x83: 0x2026, // HORIZONTAL ELLIPSIS
	0x84: 0x2014, // EM DASH
	0x85: 0x2013, // EN DASH
	0x86: 0x0192, // LATIN SMALL LETTER F WITH HOOK
	0x87: 0x2044, // FRACTION SLASH
	0x88: 0x2039, // SINGLE LEFT-POINTING ANGLE QUOTATION MARK
	0x89: 0x203A, // SINGLE RIGHT-POINTING ANGLE QUOTATION MARK
	0x8A: 0x2212, // MINUS SIGN
	0x8B: 0x2030, // PER MILLE SIGN
	0x8C: 0x201E, // DOUBLE LOW-9 QUOTATION MARK
	0x8D: 0x201C, // LEFT DOUBLE QUOTATION MARK
	0x8E: 0x201D, // RIGHT DOUBLE QUOTATION MARK
	0x8F: 0x2018, // LEFT SINGLE QUOTATION MARK
	0x90: 0x2019, // RIGHT SINGLE QUOTATION MARK
	0x91: 0x201A, // SINGLE LOW-9 QUOTATION MARK
	0x92: 0x2122, // TRADE MARK SIGN
	0x93: 0xFB01, // LATIN SMALL LIGATURE FI
	0x94: 0xFB02, // LATIN SMALL LIGATURE FL
	0x95: 0x0141, // LATIN CAPITAL LETTER L WITH STROKE
	0x96: 0x0152, // LATIN CAPITAL LIGATURE OE
	0x97: 0x0160, // LATIN CAPITAL LETTER S WITH CARON
	0x98: 0x0178, // LATIN CAPITAL LETTER Y WITH DIAERESIS
	0x99: 0x017D, // LATIN CAPITAL LETTER Z WITH CARON
	0x9A: 0x0131, // LATIN SMALL LETTER DOTLESS I
	0x9B: 0x0142, // LATIN SMALL LETTER L WITH STROKE
	0x9C: 0x0153, // LATIN SMALL LIGATURE OE
	0x9D: 0x0161, // LATIN SMALL LETTER S WITH CARON
	0x9E: 0x017E, // LATIN SMALL LETTER Z WITH CARON
	// 0x9F: undefined
}

// extractFromObjectStream extracts an object from an Object Stream.
// visited is the per-call-chain set for circular-reference detection.
func (d *Document) extractFromObjectStream(ctx context.Context, objStmNum int, targetIndex int, visited map[int]bool) (ast.PdfObject, error) {
	// 1. Resolve the Object Stream itself
	objStmRef := ast.PdfIndirectRef{ObjectNum: objStmNum, GenNum: 0}
	obj, err := d.resolveWith(ctx, objStmRef, visited)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve object stream %d: %w", objStmNum, err)
	}

	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return nil, fmt.Errorf("object %d is not a stream", objStmNum)
	}

	// Decompress the stream using the filter package
	decodedData, err := filter.DecodeStream(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("failed to decode object stream %d: %w", objStmNum, err)
	}

	// 2. Extract /N and /First
	nObj, ok := stream.Dict["N"]
	if !ok {
		return nil, fmt.Errorf("object stream missing /N")
	}
	nInt, ok := nObj.(ast.PdfInt)
	if !ok {
		return nil, fmt.Errorf("object stream /N is not an integer")
	}
	n := int(nInt)

	firstObj, ok := stream.Dict["First"]
	if !ok {
		return nil, fmt.Errorf("object stream missing /First")
	}
	firstInt, ok := firstObj.(ast.PdfInt)
	if !ok {
		return nil, fmt.Errorf("object stream /First is not an integer")
	}
	first := int(firstInt)

	// 3. Loop N times to read the header integer pairs
	reader := bytes.NewReader(decodedData)
	lex := lexer.NewLexer(ioat.NewReader(reader, int64(len(decodedData)), 0))

	targetOffset := -1
	for i := range n {
		// Read objNum
		_, err := lex.NextToken()
		if err != nil {
			return nil, err
		}
		// Read offset
		tokOffset, err := lex.NextToken()
		if err != nil {
			return nil, err
		}

		if i == targetIndex {
			offset, err := strconv.Atoi(tokOffset.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid offset in object stream %d: %w", objStmNum, err)
			}
			targetOffset = offset
			break // We found our target offset, can stop reading pairs
		}
	}

	if targetOffset == -1 {
		return nil, fmt.Errorf("target index %d out of bounds in object stream %d", targetIndex, objStmNum)
	}

	// 4. Calculate absolute position
	absoluteOffset := int64(first + targetOffset)

	// 5. Seek reader to absoluteOffset and parse
	lex = lexer.NewLexer(ioat.NewReader(reader, int64(len(decodedData)), absoluteOffset))
	p := parser.NewParser(lex).WithContext(ctx)

	return p.ParseObject()
}

// Resolve looks up an [ast.PdfIndirectRef] in the document's cross-reference
// index and parses the object from the underlying stream if it hasn't been
// cached yet.
//
// Resolve is safe for concurrent use by multiple goroutines.
func (d *Document) Resolve(ctx context.Context, ref ast.PdfIndirectRef) (ast.PdfObject, error) {
	return d.resolveWith(ctx, ref, nil)
}

// resolveWith is the internal implementation of Resolve. The visited map tracks
// object numbers in the current call chain to detect circular references. Each
// goroutine gets its own visited set, so there is no cross-goroutine interference.
func (d *Document) resolveWith(ctx context.Context, ref ast.PdfIndirectRef, visited map[int]bool) (ast.PdfObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Fast path: check cache under read lock.
	d.mu.RLock()
	if obj, ok := d.objectCache[ref.ObjectNum]; ok {
		d.mu.RUnlock()
		d.Logger.Debug("resolve object cache hit",
			slog.Int("obj", ref.ObjectNum),
			slog.Int("gen", ref.GenNum),
			slog.String("type", fmt.Sprintf("%T", obj)),
		)
		return obj, nil
	}
	d.mu.RUnlock()

	// Check for circular reference in this call chain.
	if visited == nil {
		visited = make(map[int]bool)
	}
	if visited[ref.ObjectNum] {
		return nil, fmt.Errorf("%w: object %d", ErrCircularRef, ref.ObjectNum)
	}
	visited[ref.ObjectNum] = true
	defer delete(visited, ref.ObjectNum)

	// Look up the object in the xref table (Index is immutable after construction).
	entry, ok := d.Index.Entries[ref.ObjectNum]
	if !ok {
		return nil, fmt.Errorf("%w: object %d", ErrObjectNotFound, ref.ObjectNum)
	}

	if entry.Free {
		return ast.PdfNull{}, nil
	}

	// Parse the object without holding any lock. io.ReaderAt is concurrent-safe.
	var obj ast.PdfObject
	var err error

	switch entry.Type {
	case xref.XRefTypeNormal:
		// Compliance: validate raw-byte object header format.
		if d.ComplianceMode {
			d.validateObjectHeaderFormat(entry.Offset, ref.ObjectNum)
		}

		lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, entry.Offset))
		p := parser.NewParser(lex).WithContext(ctx)
		if d.Crypto != nil {
			p.WithDecryption(d.Crypto.GetDecryptFunc(ref.ObjectNum, ref.GenNum))
		}
		if d.ComplianceMode {
			p.WithFormatChecking()
		}

		obj, err = p.ParseObject()
		if err != nil {
			return nil, fmt.Errorf("failed to parse object %d at offset %d: %w", ref.ObjectNum, entry.Offset, err)
		}

		// Collect parser-level format violations.
		if d.ComplianceMode && len(p.FormatViolations) > 0 {
			d.mu.Lock()
			d.FormatViolations = append(d.FormatViolations, p.FormatViolations...)
			d.mu.Unlock()
		}

		d.Logger.Debug("resolve object parsed",
			slog.Int("obj", ref.ObjectNum),
			slog.Int("gen", ref.GenNum),
			slog.Int64("offset", entry.Offset),
			slog.String("type", fmt.Sprintf("%T", obj)),
		)

	case xref.XRefTypeCompressed:
		obj, err = d.extractFromObjectStream(ctx, entry.ObjStmNum, entry.ObjStmIndex, visited)
		if err != nil {
			return nil, fmt.Errorf("failed to extract object %d from stream %d: %w", ref.ObjectNum, entry.ObjStmNum, err)
		}

		d.Logger.Debug("resolve object from object stream",
			slog.Int("obj", ref.ObjectNum),
			slog.Int("stream_obj", entry.ObjStmNum),
			slog.Int("index", entry.ObjStmIndex),
			slog.String("type", fmt.Sprintf("%T", obj)),
		)

	default:
		return nil, fmt.Errorf("unknown xref entry type: %d", entry.Type)
	}

	// Cache the result. If another goroutine cached the same object while we
	// were parsing, use its result for consistency.
	d.mu.Lock()
	if existing, ok := d.objectCache[ref.ObjectNum]; ok {
		d.mu.Unlock()
		return existing, nil
	}
	// Evict all cached objects when the cache exceeds the size limit.
	// This prevents unbounded memory growth on very large PDFs while
	// maintaining cache benefits for typical access patterns.
	if d.maxCacheSize > 0 && len(d.objectCache) >= d.maxCacheSize {
		d.objectCache = make(map[int]ast.PdfObject)
	}
	d.objectCache[ref.ObjectNum] = obj
	d.mu.Unlock()

	return obj, nil
}
