package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// Document represents a parsed PDF document. It holds a reference to the
// underlying data stream, the cross-reference index, and a cache of
// resolved objects to avoid re-parsing.
//
// Concurrent use. Document is safe for concurrent use by multiple
// goroutines after construction completes. All mutable caches
// (objectCache, fontCache, cmapCache) and the compliance scratch state
// (ComplianceMode, FormatViolations) are protected by internal locks.
// Concurrent read paths ([Resolve], [ResolveDict], [ResolveArray], etc.)
// run in parallel through a read-write mutex. Concurrent validation
// runs ([pdf.Reader.ValidatePDFA] / [pdf.Reader.ValidatePDFUA] on the
// same underlying Document) are serialized through a dedicated run
// mutex so that one validator cannot observe another's transient
// compliance-mode state.
//
// The [Document.Authenticate] method must be called before concurrent
// use begins; it is part of the setup phase in [NewDocument] and
// [pdf.Open] and is not safe to call in parallel with reads.
type Document struct {
	r             io.ReaderAt
	size          int64
	Index         xref.Index
	objectCache   map[int]ast.PdfObject
	fontCache     map[int]any
	cmapCache     map[int]any  // parsed CMap by ToUnicode stream object number
	mu            sync.RWMutex // protects objectCache, fontCache, cmapCache, FormatViolations
	runMu         sync.Mutex   // serializes compliance-engine validation runs
	xrefAugment   sync.Once    // guards the one-shot raw-bytes augmentation of Index
	maxCacheSize  int          // evict all objects when cache exceeds this size
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

	// complianceMode enables parser-level format violation collection.
	// When true, parsers created during object resolution collect hex
	// string format issues and other structural violations without
	// failing the parse. Accessed atomically so [Resolve] can check it
	// without blocking on a mutex on every call. Toggled by
	// [Document.SetComplianceMode].
	complianceMode atomic.Bool

	// FormatViolations collects parser-level format violations detected
	// during object resolution when complianceMode is true. Access is
	// serialized by d.mu. Callers that run a validation pass should
	// snapshot and clear this slice through [Document.ResetFormatViolations]
	// so that a later run starts from a clean state.
	FormatViolations []error

	// XRefTableOffsets records the byte offsets of traditional xref tables
	// encountered during parsing. Written once during construction and
	// read-only thereafter.
	XRefTableOffsets []int64

	// Filters holds registered filter plugins for stream decoding.
	// Nil when no plugins are registered. Set once during construction,
	// then read-only.
	Filters *filter.Decoder
}

// ComplianceMode reports whether parser-level format violation
// collection is enabled. Reads are lock-free via an atomic load so they
// can run on the hot [Document.Resolve] path without contention.
func (d *Document) ComplianceMode() bool {
	return d.complianceMode.Load()
}

// SetComplianceMode toggles parser-level format violation collection.
// The compliance engine turns it on at the start of a validation run
// and off at the end (through its run mutex), so cross-goroutine
// observers of [Document.ComplianceMode] may see transient true values
// only during a concurrent validation.
func (d *Document) SetComplianceMode(enabled bool) {
	d.complianceMode.Store(enabled)
}

// AppendFormatViolations adds violations collected by a parser to the
// document's format-violation slice under the document mutex.
func (d *Document) AppendFormatViolations(violations ...error) {
	if len(violations) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.FormatViolations = append(d.FormatViolations, violations...)
}

// ResetFormatViolations clears the document's format-violation slice
// and returns the previous contents. Used by validation callers to
// snapshot the violations from one pass without leaking them into a
// subsequent pass on the same Document.
func (d *Document) ResetFormatViolations() []error {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := d.FormatViolations
	d.FormatViolations = nil
	return out
}

// LockValidationRun acquires the document's validation run mutex and
// returns a release function. Callers that kick off a compliance
// validation pass use this to serialize runs on the same Document so
// that transient per-run state (ComplianceMode, FormatViolations) is
// not corrupted by a concurrent second validator.
func (d *Document) LockValidationRun() func() {
	d.runMu.Lock()
	return d.runMu.Unlock
}

// DecodeStream decodes a [ast.PdfStream] through the document's filter pipeline.
// Context is checked between decompression chunks to allow cancellation.
func (d *Document) DecodeStream(ctx context.Context, stream ast.PdfStream) ([]byte, error) {
	if d.Filters != nil {
		return d.Filters.DecodeStream(ctx, stream)
	}
	return filter.DecodeStream(ctx, stream)
}

// NewStreamReader returns a lazy [io.ReadCloser] using the document's filter decoder.
func (d *Document) NewStreamReader(ctx context.Context, stream ast.PdfStream) (io.ReadCloser, error) {
	if d.Filters != nil {
		return d.Filters.NewStreamReader(ctx, stream)
	}
	return filter.NewStreamReader(ctx, stream)
}

// OpenFunc returns a lazy Open closure for a PDF stream. The returned
// function decodes the stream through the filter pipeline on demand.
// Use this to populate Open fields on model types ([models.ImageMeta],
// [models.EmbeddedFile], etc.) without loading stream data at parse time.
func (d *Document) OpenFunc(stream ast.PdfStream) func(ctx context.Context) (io.ReadCloser, error) {
	return func(ctx context.Context) (io.ReadCloser, error) {
		return d.NewStreamReader(ctx, stream)
	}
}

// NopLogger returns a logger that discards all output.
func NopLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// headerScanLimit is the byte window scanned for the "%PDF-" header
// at the start of a file. ISO 32000 places the header on the first
// line, but Acrobat tolerates a leading prefix (BOM, shebang line,
// HTTP-style preamble, explanatory comment) and so does litho. xref
// offsets remain absolute from byte 0 of the file, which is why no
// offset adjustment is needed downstream.
const headerScanLimit = 1024

// NewDocument creates a new [Document] by parsing the XRef chains.
// If logger is nil, a no-op logger is used.
func NewDocument(ctx context.Context, r io.ReaderAt, size int64, logger *slog.Logger) (*Document, error) {
	// Validate PDF Header. Scan up to [headerScanLimit] bytes so files
	// with a leading prefix (UTF-8 BOM, explanatory comment) are still
	// accepted.
	headerBuf := make([]byte, headerScanLimit)
	n, err := r.ReadAt(headerBuf, 0)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read file header: %w", err)
	}
	if n == 0 {
		return nil, ErrMissingHeader
	}
	headerBuf = headerBuf[:n]
	pdfIdx := bytes.Index(headerBuf, []byte("%PDF-"))
	if pdfIdx == -1 {
		return nil, ErrMissingHeader
	}

	// Extract version string (e.g., "1.4", "2.0").
	version := ""
	vStart := pdfIdx + len("%PDF-")
	if vStart < len(headerBuf) {
		vEnd := vStart
		for vEnd < len(headerBuf) && headerBuf[vEnd] != '\n' && headerBuf[vEnd] != '\r' && headerBuf[vEnd] != ' ' && headerBuf[vEnd] != 0x00 {
			vEnd++
		}
		version = string(headerBuf[vStart:vEnd])
	}

	if logger == nil {
		logger = NopLogger()
	}

	doc := &Document{
		r:            r,
		size:         size,
		Index:        xref.Index{Entries: make(map[int]xref.XRefEntry)},
		objectCache:  make(map[int]ast.PdfObject),
		fontCache:    make(map[int]any),
		cmapCache:    make(map[int]any),
		maxCacheSize: 50_000, // evict when cache exceeds 50k objects
		Logger:       logger,
		Version:      version,
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

	if err := doc.parseXRefChain(ctx); err != nil {
		doc.Logger.Debug("xref chain parse failed, attempting rebuild",
			slog.String("error", err.Error()),
		)
		// Fallback: brute-force rebuild by scanning for object headers.
		doc.Index.Entries = make(map[int]xref.XRefEntry)
		doc.Index.Trailer = nil
		if rebuildErr := doc.rebuildXRef(ctx); rebuildErr != nil {
			return nil, fmt.Errorf("%w: %v (rebuild also failed: %v)", ErrDamagedXRef, err, rebuildErr)
		}
		doc.Rebuilt = true
	}

	// Verify the catalog /Root is reachable. Broken xref chains from
	// incremental updates can parse without error but miss the catalog
	// object. If /Root can't be resolved, rebuild the xref from scratch.
	// Also rebuild when the parse produced zero entries: some producers
	// emit a syntactically valid but empty xref table with startxref
	// pointing at offset 0, which yields no objects. The rebuild path
	// discovers them by scanning raw bytes.
	if !doc.Rebuilt {
		shouldRebuild := len(doc.Index.Entries) == 0
		if !shouldRebuild {
			if rootRef, ok := doc.Index.Trailer["Root"]; ok {
				if ref, ok := rootRef.(ast.PdfIndirectRef); ok {
					if _, exists := doc.Index.Entries[ref.ObjectNum]; !exists {
						shouldRebuild = true
					}
				}
			}
		}
		if shouldRebuild {
			doc.Logger.Debug("xref entries incomplete, rebuilding")
			doc.Index.Entries = make(map[int]xref.XRefEntry)
			doc.Index.Trailer = nil
			if rebuildErr := doc.rebuildXRef(ctx); rebuildErr == nil {
				doc.Rebuilt = true
			}
		}
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
// Authenticate must be called during setup, before the [Document] is used concurrently.
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
// ClearCaches must not be called concurrently with other [Document] methods.
func (d *Document) ClearCaches() {
	d.mu.Lock()
	d.objectCache = make(map[int]ast.PdfObject)
	d.fontCache = make(map[int]any)
	d.cmapCache = make(map[int]any)
	d.mu.Unlock()
}

// CachedCMap returns a previously cached parsed CMap for the given stream
// object number. Returns nil, false if not cached.
func (d *Document) CachedCMap(objNum int) (any, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	c, ok := d.cmapCache[objNum]
	return c, ok
}

// SetCachedCMap stores a parsed CMap keyed by its stream object number.
func (d *Document) SetCachedCMap(objNum int, cm any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.cmapCache[objNum]; !ok {
		d.cmapCache[objNum] = cm
	}
}

// CachedFont returns a previously cached font for the given object number.
// The second return value reports whether the font was found in the cache.
func (d *Document) CachedFont(objNum int) (any, bool) {
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

// RawReader returns the underlying [io.ReaderAt] for low-level byte access.
func (d *Document) RawReader() io.ReaderAt {
	return d.r
}

// SetRawReader sets the underlying [io.ReaderAt]. This is intended for test
// code that needs to construct a [Document] with raw file bytes without
// going through the full parsing pipeline.
func (d *Document) SetRawReader(r io.ReaderAt) {
	d.r = r
}

// ReadFileBytes reads a specific chunk of raw bytes directly from the underlying file.
func (d *Document) ReadFileBytes(ctx context.Context, offset int64, length int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buf := make([]byte, length)
	_, err := d.r.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf, nil
}
