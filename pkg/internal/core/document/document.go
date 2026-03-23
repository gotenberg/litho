package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// Document represents a parsed PDF document.
// It holds a reference to the underlying data stream, the cross-reference index,
// and a cache of resolved objects to avoid re-parsing.
//
// Document is safe for concurrent use by multiple goroutines after construction.
// All mutable caches (ObjectCache, FontCache) are protected by an internal
// read-write mutex. The Authenticate method must be called before concurrent
// use begins (it is part of the setup phase in NewDocument / pdf.Open).
type Document struct {
	r             io.ReaderAt
	size          int64
	Index         xref.Index
	objectCache   map[int]ast.PdfObject
	fontCache     map[int]any
	mu            sync.RWMutex // protects objectCache and fontCache
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

	// ComplianceMode enables parser-level format violation collection.
	// When true, parsers created during object resolution collect hex
	// string format issues and other structural violations without
	// failing the parse. Violations are surfaced via FormatViolations.
	ComplianceMode bool

	// FormatViolations collects parser-level format violations detected
	// during object resolution when ComplianceMode is true.
	FormatViolations []error

	// Filters holds registered filter plugins for stream decoding.
	// Nil when no plugins are registered. Set once during construction,
	// then read-only.
	Filters *filter.Decoder
}

// DecodeStream decodes a PDF stream using the document's filter decoder
// (which may include registered plugins). If no custom decoder is set,
// falls back to the package-level filter.DecodeStream.
func (d *Document) DecodeStream(stream ast.PdfStream) ([]byte, error) {
	if d.Filters != nil {
		return d.Filters.DecodeStream(stream)
	}
	return filter.DecodeStream(stream)
}

// NewStreamReader returns a lazy reader using the document's filter decoder.
func (d *Document) NewStreamReader(stream ast.PdfStream) (io.ReadCloser, error) {
	if d.Filters != nil {
		return d.Filters.NewStreamReader(stream)
	}
	return filter.NewStreamReader(stream)
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
		objectCache: make(map[int]ast.PdfObject),
		fontCache:   make(map[int]any),
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

	// Per PDF spec, the catalog /Version key overrides the header version
	// when present (allows incremental updates to bump the version).
	doc.initCatalogVersion(ctx)

	doc.Logger.Debug("document opened",
		slog.Int64("size_bytes", size),
		slog.String("version", doc.Version),
		slog.Bool("encrypted", doc.IsEncrypted),
	)

	return doc, nil
}

// Authenticate attempts to authenticate an encrypted document with the given password.
// Authenticate must be called during setup, before the Document is used concurrently.
func (d *Document) Authenticate(password string) error {
	if !d.IsEncrypted || d.Crypto == nil {
		return nil
	}
	if err := d.Crypto.Authenticate(password); err != nil {
		return fmt.Errorf("invalid password or unsupported encryption: %w", err)
	}
	d.Authenticated = true
	// Clear object cache in case we cached encrypted items
	// (though we shouldn't have been able to read them).
	d.mu.Lock()
	d.objectCache = make(map[int]ast.PdfObject)
	d.mu.Unlock()
	return nil
}

// initCatalogVersion checks the catalog /Version key and overrides the header
// version if present. Per PDF spec §7.2.2, the catalog version takes precedence.
func (d *Document) initCatalogVersion(ctx context.Context) {
	rootObj, ok := d.Index.Trailer["Root"]
	if !ok {
		return
	}
	catalog, err := ResolveDict(ctx, d, rootObj)
	if err != nil {
		d.Logger.Debug("resolving catalog for version check", slog.Any("err", err))
	}
	if catalog == nil {
		return
	}
	vObj, ok := catalog["Version"]
	if !ok {
		return
	}
	if vName, ok := vObj.(ast.PdfName); ok {
		d.Version = string(vName)
	}
}

// IsLinearized reports whether the document has a linearization dictionary.
func (d *Document) IsLinearized() bool {
	return d.Linearization != nil
}

// ClearCaches releases the object cache and font cache to free memory.
// Caches are transparently rebuilt on the next access.
//
// ClearCaches must not be called concurrently with other Document methods.
func (d *Document) ClearCaches() {
	d.mu.Lock()
	d.objectCache = make(map[int]ast.PdfObject)
	d.fontCache = make(map[int]any)
	d.mu.Unlock()
}

// GetCachedFont returns a previously cached font for the given object number.
// The second return value reports whether the font was found in the cache.
func (d *Document) GetCachedFont(objNum int) (any, bool) {
	d.mu.RLock()
	f, ok := d.fontCache[objNum]
	d.mu.RUnlock()
	return f, ok
}

// SetCachedFont stores a resolved font in the cache, keyed by object number.
// If the font is already cached, the existing entry is preserved.
func (d *Document) SetCachedFont(objNum int, font any) {
	d.mu.Lock()
	if _, ok := d.fontCache[objNum]; !ok {
		d.fontCache[objNum] = font
	}
	d.mu.Unlock()
}

// FileSize returns the total size of the underlying PDF file in bytes.
func (d *Document) FileSize() int64 {
	return d.size
}

// RawReader returns the underlying io.ReaderAt for low-level byte access.
func (d *Document) RawReader() io.ReaderAt {
	return d.r
}

// SetRawReader sets the underlying io.ReaderAt. This is intended for test
// code that needs to construct a Document with raw file bytes without
// going through the full parsing pipeline.
func (d *Document) SetRawReader(r io.ReaderAt) {
	d.r = r
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
