package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
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
// (objectCache, fontCache, cmapCache, objStmCache) and the compliance
// scratch state (ComplianceMode, formatViolations) are protected by
// internal locks. The format-violation record is keyed by object number
// and written once per object, so concurrent resolutions of the same
// object contribute one entry, not one per goroutine.
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
	r                io.ReaderAt
	size             int64
	Index            xref.Index
	objectCache      map[int]ast.PdfObject
	fontCache        map[int]any
	cmapCache        map[int]any         // parsed CMap by ToUnicode stream object number
	objStmCache      map[int]*objStmData // parsed object streams by ObjStm object number
	objStmCacheBytes int                 // decoded bytes held in objStmCache, bounded by maxObjStmCacheBytes
	sortedOffsets    []int64             // ascending normal-object offsets, for O(log N) endobj scan-capping
	sortedOffsetsLen int                 // len(Index.Entries) when sortedOffsets was built; rebuild on change
	mu               sync.RWMutex        // protects objectCache, fontCache, cmapCache, objStmCache, sortedOffsets, formatViolations, and the lazy Index.Entries augmentation
	runMu            sync.Mutex          // serializes compliance-engine validation runs
	xrefAugment      sync.Once           // guards the one-shot raw-bytes augmentation of Index
	maxCacheSize     int                 // evict all objects when cache exceeds this size
	Crypto           *crypto.Handler
	OCConfig         *OCConfig
	IsEncrypted      bool
	Authenticated    bool

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

	// HeaderOffset is the physical byte position of the %PDF- header,
	// non-zero when junk precedes it. Readers disagree on the offset
	// base for such files: litho resolves xref offsets from the physical
	// file start, while Acrobat and veraPDF resolve them relative to the
	// header. Writers must not extend such files incrementally, since
	// any appended section is wrong for one of the two conventions.
	HeaderOffset int64

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

	// formatViolations records the parser-level format violations found on
	// each object, keyed by object number, when complianceMode is true.
	// Recording is idempotent per object (see
	// [Document.RecordFormatViolations]), so the set is a function of the
	// input bytes rather than of how often an object happened to be parsed.
	// Access is serialized by d.mu.
	//
	// The key space is the set of object numbers that carry a normal xref
	// entry, so the map holds at most len(Index.Entries) keys: an object
	// number absent from the index fails resolution before anything is
	// recorded. That is the same bound the index itself already lives
	// under (an xref entry costs bytes in the file), so the store needs no
	// cap of its own.
	formatViolations map[int][]error

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

// RecordFormatViolations records the format violations found while parsing
// object objNum. The first recording for an object wins: a later call for
// the same object is dropped, including when it carries a different list.
// An object is parsed more than once whenever its parse fails (a failed
// parse is never cached), whenever its cache entry is evicted, and whenever
// two goroutines miss the cache for it at the same time, so counting per
// parse made the reported violation set depend on scheduling. Counting per
// object makes it a function of the input bytes.
//
// Recording an empty list still marks the object as visited, so a later
// re-parse cannot add anything either.
//
// RecordFormatViolations is safe for concurrent use.
func (d *Document) RecordFormatViolations(objNum int, violations ...error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.formatViolations == nil {
		d.formatViolations = make(map[int][]error)
	}
	if _, ok := d.formatViolations[objNum]; ok {
		return
	}
	d.formatViolations[objNum] = slices.Clone(violations)
}

// FormatViolations returns the recorded format violations ordered by object
// number, then by the order in which they were found within that object.
// The order never depends on map iteration or on the order the objects were
// resolved in (CONTRIBUTING "Deterministic output").
//
// The result is a fresh slice, so callers may hold it while resolution
// continues. FormatViolations is safe for concurrent use.
func (d *Document) FormatViolations() []error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.formatViolationsLocked()
}

// ResetFormatViolations drops every recorded format violation and returns
// the previous contents in [Document.FormatViolations] order.
//
// Recording is repopulated by parsing, so reset the record only together
// with the object cache: an object still in the cache is never re-parsed
// and would never record its violations again. [Document.ClearCaches] and
// [Document.Authenticate] reset both at once for that reason. A validation
// pass must not reset on its own, or a second pass over the same Document
// would report only the violations of the objects that failed to parse.
func (d *Document) ResetFormatViolations() []error {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := d.formatViolationsLocked()
	d.formatViolations = nil
	return out
}

// formatViolationsLocked returns the ordered violations. The caller must
// hold d.mu.
func (d *Document) formatViolationsLocked() []error {
	out := make([]error, 0, len(d.formatViolations))
	for _, objNum := range slices.Sorted(maps.Keys(d.formatViolations)) {
		out = append(out, d.formatViolations[objNum]...)
	}
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
	data, incomplete, err := filter.DecodeStreamChecked(ctx, stream)
	if incomplete {
		// The bytes are only partially decoded: an unsupported filter in
		// the chain was skipped, so they remain filter-encoded. Surface
		// it at Debug rather than silently returning them as plaintext.
		d.Logger.Debug("stream only partially decoded; an unsupported filter in the chain was skipped, returned bytes remain filter-encoded")
	}
	return data, err
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
		HeaderOffset: int64(pdfIdx),
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

	// A raw-scan rebuild only recovers uncompressed "N G obj" headers; objects
	// held inside object streams (ISO 32000-1 §7.5.7) — which can include a
	// catalog or page-tree node a modern producer compressed — are invisible to
	// it. Register each recovered object stream's members so [ResolveCatalog]
	// can still find a compressed /Type /Catalog (otherwise the whole page tree
	// is lost). Runs only after a rebuild, when the fully-constructed document
	// can resolve the object streams it found.
	if doc.Rebuilt {
		doc.expandObjectStreamsForRebuild(ctx, doc.Index.Entries)
	}

	// Runs once the index is final, whichever path filled it, because all of
	// them take object numbers straight from the file.
	doc.pruneUnrepresentableEntries()

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
	// (though we shouldn't have been able to read them). The
	// format-violation record is keyed to those same parses, so it goes
	// with them: whatever is re-parsed re-records.
	d.mu.Lock()
	d.objectCache = make(map[int]ast.PdfObject)
	d.objStmCache = nil
	d.objStmCacheBytes = 0
	d.formatViolations = nil
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
// Caches are transparently rebuilt on the next access. The
// format-violation record is dropped with them, since re-parsing an object
// is what records it again (see [Document.ResetFormatViolations]).
//
// ClearCaches must not be called concurrently with other [Document] methods.
func (d *Document) ClearCaches() {
	d.mu.Lock()
	d.objectCache = make(map[int]ast.PdfObject)
	d.fontCache = make(map[int]any)
	d.cmapCache = make(map[int]any)
	d.objStmCache = nil
	d.objStmCacheBytes = 0
	d.formatViolations = nil
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
