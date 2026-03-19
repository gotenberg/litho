package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// Document represents a parsed PDF document.
// It holds a reference to the underlying data stream, the cross-reference index,
// and a cache of resolved objects to avoid re-parsing.
type Document struct {
	r             io.ReaderAt
	size          int64
	Index         xref.Index
	ObjectCache   map[int]ast.PdfObject
	resolving     map[int]bool
	Crypto        *crypto.Handler
	OCConfig      *OCConfig
	IsEncrypted   bool
	Authenticated bool

	// Logger is the structured logger used for debug output. It is never nil;
	// when the caller does not supply a logger, a no-op logger that discards
	// all output is used.
	Logger *slog.Logger

	// Linearization holds the linearization parameters if the document is
	// linearized (fast web view). Nil for non-linearized documents.
	Linearization *LinearizationParams

	// HintTable holds the per-page offset entries decoded from the hint
	// stream of a linearized PDF. Nil for non-linearized documents.
	HintTable []PageOffsetEntry

	// Rebuilt is true when the cross-reference table was reconstructed
	// by scanning the file for object headers because the original xref
	// data was damaged or missing.
	Rebuilt bool

	// Version is the PDF version string extracted from the %PDF-X.Y header
	// (e.g., "1.4", "1.7", "2.0").
	Version string
}

// OCConfig holds the state of Optional Content Groups (Layers)
type OCConfig struct {
	On        map[int]bool
	Off       map[int]bool
	BaseState string // "ON", "OFF", or "Unchanged"; defaults to "ON" per PDF spec
}

func findStartXRef(r io.ReaderAt, size int64) (int64, error) {
	bufSize := min(size, int64(1024))

	buf := make([]byte, bufSize)
	_, err := r.ReadAt(buf, size-bufSize)
	if err != nil {
		return 0, err
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
		return 0, err
	}
	if tok.Type != lexer.TokenNumber {
		return 0, fmt.Errorf("expected offset after startxref, got %v", tok.Value)
	}

	offset, err := strconv.ParseInt(tok.Value, 10, 64)
	if err != nil {
		return 0, err
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
			return err
		}

		var prev int64
		if tok.Type == lexer.TokenKeyword && tok.Value == "xref" {
			lex = lexer.NewLexer(ioat.NewReader(d.r, d.size, currentOffset))
			trailerDict, p, err := xref.ParseXRefTable(lex, d.Index.Entries)
			if err != nil {
				return err
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
				return err
			}
			stream, ok := obj.(ast.PdfStream)
			if !ok {
				return fmt.Errorf("expected xref stream, got %T", obj)
			}
			pVal, err := xref.ParseXRefStream(stream, d.Index.Entries)
			if err != nil {
				return err
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
		return err
	}

	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return fmt.Errorf("expected xref stream at /XRefStm offset %d, got %T", offset, obj)
	}

	// Parse into a temporary map so entries override the legacy table.
	hybridEntries := make(map[int]xref.XRefEntry)
	if _, err := xref.ParseXRefStream(stream, hybridEntries); err != nil {
		return err
	}

	// Merge: hybrid entries override legacy entries.
	maps.Copy(d.Index.Entries, hybridEntries)

	return nil
}

// NopLogger returns a logger that discards all output.
func NopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// NewDocument creates a new Document instance by parsing the XRef chains.
// If logger is nil, a no-op logger is used.
func NewDocument(ctx context.Context, r io.ReaderAt, size int64, logger *slog.Logger) (*Document, error) {
	// Validate PDF Header
	headerBuf := make([]byte, 10)
	_, err := r.ReadAt(headerBuf, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to read file header: %w", err)
	}
	pdfIdx := bytes.Index(headerBuf, []byte("%PDF-"))
	if pdfIdx == -1 {
		return nil, ErrMissingHeader
	}

	// Extract version string (e.g., "1.4", "2.0").
	version := ""
	vStart := pdfIdx + len("%PDF-")
	if vStart < len(headerBuf) {
		vEnd := vStart
		for vEnd < len(headerBuf) && headerBuf[vEnd] != '\n' && headerBuf[vEnd] != '\r' && headerBuf[vEnd] != ' ' {
			vEnd++
		}
		version = string(headerBuf[vStart:vEnd])
	}

	if logger == nil {
		logger = NopLogger()
	}

	doc := &Document{
		r:           r,
		size:        size,
		Index:       xref.Index{Entries: make(map[int]xref.XRefEntry)},
		ObjectCache: make(map[int]ast.PdfObject),
		resolving:   make(map[int]bool),
		Logger:      logger,
		Version:     version,
	}

	// Probe for linearization before seeking to the end of the file.
	linParams := probeLinearization(r, size)
	if linParams != nil {
		doc.Linearization = linParams
		hintEntries, err := parseHintStream(r, size, linParams)
		if err == nil && len(hintEntries) > 0 {
			doc.HintTable = hintEntries
		}
	}

	if err := doc.parseXRefChain(); err != nil {
		doc.Logger.Debug("xref chain parse failed, attempting rebuild",
			slog.String("error", err.Error()),
		)
		// Fallback: brute-force rebuild by scanning for object headers.
		doc.Index.Entries = make(map[int]xref.XRefEntry)
		doc.Index.Trailer = nil
		if rebuildErr := doc.rebuildXRef(); rebuildErr != nil {
			return nil, fmt.Errorf("%w: %v (rebuild also failed: %v)", ErrDamagedXRef, err, rebuildErr)
		}
		doc.Rebuilt = true
	}

	doc.Logger.Debug("xref parsed",
		slog.Int("entries", len(doc.Index.Entries)),
		slog.Bool("rebuilt", doc.Rebuilt),
		slog.Bool("linearized", doc.Linearization != nil),
	)

	// Initialize Crypto if Encrypt dictionary is present
	if encObj, ok := doc.Index.Trailer["Encrypt"]; ok {
		var encDict ast.PdfDict
		if ref, ok := encObj.(ast.PdfIndirectRef); ok {
			// Resolving without Crypto is safe because Encrypt dict values are never encrypted
			obj, err := doc.Resolve(ctx, ref)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve /Encrypt reference: %w", err)
			}
			encDict, ok = obj.(ast.PdfDict)
			if !ok {
				return nil, fmt.Errorf("resolved /Encrypt is not a dictionary")
			}
		} else {
			encDict, ok = encObj.(ast.PdfDict)
			if !ok {
				return nil, fmt.Errorf("/Encrypt is not a dictionary")
			}
		}

		doc.IsEncrypted = true
		handler, err := crypto.NewHandler(encDict, doc.Index.Trailer)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize crypto handler: %w", err)
		}
		doc.Crypto = handler
		doc.Authenticated = handler.Authenticated

		doc.Logger.Debug("encryption detected",
			slog.Int("V", handler.V),
			slog.Int("key_length", handler.Length),
			slog.String("cfm", handler.CFM),
			slog.Bool("authenticated_empty_password", handler.Authenticated),
		)
	}

	doc.initOCConfig(ctx)

	doc.Logger.Debug("document opened",
		slog.Int64("size_bytes", size),
		slog.String("version", doc.Version),
		slog.Bool("encrypted", doc.IsEncrypted),
	)

	return doc, nil
}

// Authenticate attempts to authenticate an encrypted document with the given password.
func (d *Document) Authenticate(password string) error {
	if !d.IsEncrypted || d.Crypto == nil {
		return nil
	}
	err := d.Crypto.Authenticate(password)
	if err == nil {
		d.Authenticated = true
		// Clear object cache in case we cached encrypted items
		// (though we shouldn't have been able to read them)
		d.ObjectCache = make(map[int]ast.PdfObject)
	}
	return err
}

// resolveAny resolves obj if it is an indirect reference, otherwise returns it directly.
// This is safe to call on any PdfObject (unlike Resolve which requires PdfIndirectRef).
func (d *Document) resolveAny(ctx context.Context, obj ast.PdfObject) ast.PdfObject {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		res, err := d.Resolve(ctx, ref)
		if err != nil {
			return nil
		}
		return res
	}
	return obj
}

// initOCConfig extracts the Optional Content Properties to determine which layers are ON/OFF.
func (d *Document) initOCConfig(ctx context.Context) {
	d.OCConfig = &OCConfig{
		On:  make(map[int]bool),
		Off: make(map[int]bool),
	}

	rootObj, ok := d.Index.Trailer["Root"]
	if !ok {
		return
	}
	catalog, ok := d.resolveAny(ctx, rootObj).(ast.PdfDict)
	if !ok {
		return
	}

	ocpObj, ok := catalog["OCProperties"]
	if !ok {
		return
	}
	ocp, ok := d.resolveAny(ctx, ocpObj).(ast.PdfDict)
	if !ok {
		return
	}

	dObj, ok := ocp["D"]
	if !ok {
		return
	}
	dDict, ok := d.resolveAny(ctx, dObj).(ast.PdfDict)
	if !ok {
		return
	}

	// BaseState determines the default state before applying ON/OFF arrays
	baseState := "ON"
	if bsObj, ok := dDict["BaseState"]; ok {
		if bsName, ok := bsObj.(ast.PdfName); ok {
			baseState = string(bsName)
		}
	}

	// Parse ON array
	if onObj, ok := dDict["ON"]; ok {
		if onArr, ok := d.resolveAny(ctx, onObj).(ast.PdfArray); ok {
			for _, item := range onArr {
				if ref, ok := item.(ast.PdfIndirectRef); ok {
					d.OCConfig.On[ref.ObjectNum] = true
				}
			}
		}
	}

	// Parse OFF array
	if offObj, ok := dDict["OFF"]; ok {
		if offArr, ok := d.resolveAny(ctx, offObj).(ast.PdfArray); ok {
			for _, item := range offArr {
				if ref, ok := item.(ast.PdfIndirectRef); ok {
					d.OCConfig.Off[ref.ObjectNum] = true
				}
			}
		}
	}

	d.OCConfig.BaseState = baseState
}

// IsLinearized reports whether the document has a linearization dictionary.
func (d *Document) IsLinearized() bool {
	return d.Linearization != nil
}

// FileSize returns the total size of the underlying PDF file in bytes.
func (d *Document) FileSize() int64 {
	return d.size
}

// RawReader returns the underlying io.ReaderAt for low-level byte access.
func (d *Document) RawReader() io.ReaderAt {
	return d.r
}

// ReadFileBytes reads a specific chunk of raw bytes directly from the underlying file.
func (d *Document) ReadFileBytes(offset int64, length int64) ([]byte, error) {
	buf := make([]byte, length)
	_, err := d.r.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf, nil
}

// ExtractFromObjectStream extracts an object from an Object Stream.
func ExtractFromObjectStream(ctx context.Context, doc *Document, objStmNum int, targetIndex int) (ast.PdfObject, error) {
	// 1. Resolve the Object Stream itself
	objStmRef := ast.PdfIndirectRef{ObjectNum: objStmNum, GenNum: 0}
	obj, err := doc.Resolve(ctx, objStmRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve object stream %d: %v", objStmNum, err)
	}

	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return nil, fmt.Errorf("object %d is not a stream", objStmNum)
	}

	// Decompress the stream using the filter package
	decodedData, err := filter.DecodeStream(stream)
	if err != nil {
		return nil, fmt.Errorf("failed to decode object stream %d: %v", objStmNum, err)
	}

	// 2. Extract /N and /First
	nObj, ok := stream.Dict["N"]
	if !ok {
		return nil, fmt.Errorf("object stream missing /N")
	}
	n := int(nObj.(ast.PdfInt))

	firstObj, ok := stream.Dict["First"]
	if !ok {
		return nil, fmt.Errorf("object stream missing /First")
	}
	first := int(firstObj.(ast.PdfInt))

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
	p := parser.NewParser(lex)

	return p.ParseObject()
}

// Resolve looks up an indirect reference in the document's cross-reference index
// and parses the object from the underlying stream if it hasn't been cached yet.
func (d *Document) Resolve(ctx context.Context, ref ast.PdfIndirectRef) (ast.PdfObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Check for circular reference
	if d.resolving[ref.ObjectNum] {
		return nil, fmt.Errorf("%w: object %d", ErrCircularRef, ref.ObjectNum)
	}

	// Check if the object is already in the cache
	if obj, ok := d.ObjectCache[ref.ObjectNum]; ok {
		d.Logger.Debug("resolve object cache hit",
			slog.Int("obj", ref.ObjectNum),
			slog.Int("gen", ref.GenNum),
			slog.String("type", fmt.Sprintf("%T", obj)),
		)
		return obj, nil
	}

	// Mark as resolving
	d.resolving[ref.ObjectNum] = true
	defer func() { delete(d.resolving, ref.ObjectNum) }()

	// Look up the object in the xref table
	entry, ok := d.Index.Entries[ref.ObjectNum]
	if !ok {
		return nil, fmt.Errorf("%w: object %d", ErrObjectNotFound, ref.ObjectNum)
	}

	if entry.Free {
		return ast.PdfNull{}, nil
	}

	switch entry.Type {
	case 1:
		// Type 1: Normal object
		// Create a new lexer and parser at the specific byte offset
		lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, entry.Offset))
		p := parser.NewParser(lex)
		if d.Crypto != nil {
			p.WithDecryption(d.Crypto.GetDecryptFunc(ref.ObjectNum, ref.GenNum))
		}

		// ParseObject handles the '12 0 obj ... endobj' envelope and returns the inner object
		obj, err := p.ParseObject()
		if err != nil {
			return nil, fmt.Errorf("failed to parse object %d at offset %d: %v", ref.ObjectNum, entry.Offset, err)
		}

		d.Logger.Debug("resolve object parsed",
			slog.Int("obj", ref.ObjectNum),
			slog.Int("gen", ref.GenNum),
			slog.Int64("offset", entry.Offset),
			slog.String("type", fmt.Sprintf("%T", obj)),
		)

		// Store in cache and return
		d.ObjectCache[ref.ObjectNum] = obj
		return obj, nil
	case 2:
		// Type 2: Compressed object in an Object Stream (PDF 1.5+)
		obj, err := ExtractFromObjectStream(ctx, d, entry.ObjStmNum, entry.ObjStmIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to extract object %d from stream %d: %v", ref.ObjectNum, entry.ObjStmNum, err)
		}

		d.Logger.Debug("resolve object from object stream",
			slog.Int("obj", ref.ObjectNum),
			slog.Int("stream_obj", entry.ObjStmNum),
			slog.Int("index", entry.ObjStmIndex),
			slog.String("type", fmt.Sprintf("%T", obj)),
		)

		// Store in cache and return
		d.ObjectCache[ref.ObjectNum] = obj
		return obj, nil
	}
	return nil, fmt.Errorf("unknown xref entry type: %d", entry.Type)
}
