package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/crypto"
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

	// FontCache stores resolved font objects keyed by their indirect reference
	// object number. This avoids re-parsing the same font dictionary (including
	// ToUnicode CMap decompression) on every Tf operator.
	FontCache map[int]any
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
		FontCache:   make(map[int]any),
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

// IsLinearized reports whether the document has a linearization dictionary.
func (d *Document) IsLinearized() bool {
	return d.Linearization != nil
}

// ClearCaches releases the object cache and font cache to free memory.
// Caches are transparently rebuilt on the next access.
func (d *Document) ClearCaches() {
	d.ObjectCache = make(map[int]ast.PdfObject)
	d.FontCache = make(map[int]any)
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
