package pdf

import (
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/remediation"
	"github.com/gotenberg/litho/pkg/models"
)

// Metadata holds document information dictionary fields for [Writer.SetMetadata].
type Metadata struct {
	Title        string
	Author       string
	Subject      string
	Keywords     string
	Creator      string
	Producer     string
	CreationDate string
	ModDate      string
}

// TextStamp defines a text overlay to stamp on pages.
type TextStamp struct {
	Text     string  // Text content
	X, Y     float64 // Position in points from bottom-left
	FontSize float64 // Font size in points
	Opacity  float64 // 0.0-1.0 (1.0 = fully opaque)
}

// Signer produces a PKCS#7/CMS signature for the given digest. Implementations
// control key material and signing algorithms. litho calls [Signer.Sign] during
// [Writer.WriteTo] with the SHA-256 hash of the ByteRange regions.
type Signer = mutations.Signer

// Writer applies content-preserving modifications to a PDF document.
// Mutations are batched and written atomically via [Writer.WriteTo].
type Writer struct {
	reader          *Reader
	mutations       []mutation
	encryptCfg      *crypto.EncryptConfig
	decrypt         bool
	forceRewrite    bool
	signer          Signer
	requiredVersion string
}

// mutation is a deferred modification applied during WriteTo.
type mutation struct {
	apply func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error
}

// NewWriter creates a Writer that modifies the document behind the given Reader.
func NewWriter(r *Reader) *Writer {
	return &Writer{reader: r}
}

// SetMetadata sets or replaces document Info dictionary fields. Empty fields
// in meta are skipped (the existing value is preserved).
func (w *Writer) SetMetadata(_ context.Context, meta Metadata) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return mutations.SetMetadata(ctx, alloc, doc, mutations.Metadata(meta))
		},
	})
}

// RotatePages sets the /Rotate value for the given 1-based page numbers.
// Rotation must be a multiple of 90 (0, 90, 180, 270).
func (w *Writer) RotatePages(ctx context.Context, rotation int, pages ...int) error {
	if rotation%90 != 0 {
		return fmt.Errorf("rotation must be a multiple of 90, got %d", rotation)
	}
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			resolvedPages, err := w.reader.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving pages: %w", err)
			}
			return mutations.RotatePages(ctx, alloc, resolvedPages, rotation, pages)
		},
	})
	return nil
}

// SetBookmarks replaces the document outline with the given bookmarks.
func (w *Writer) SetBookmarks(_ context.Context, bookmarks []models.Bookmark) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			resolvedPages, err := w.reader.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving pages for bookmarks: %w", err)
			}
			return mutations.SetBookmarks(ctx, alloc, doc, resolvedPages, bookmarks)
		},
	})
}

// Encrypt applies password-based encryption to the output document. Forces a
// full rewrite since every string and stream must be encrypted.
func (w *Writer) Encrypt(cfg crypto.EncryptConfig) {
	w.encryptCfg = &cfg
	w.forceRewrite = true
}

// Decrypt removes encryption from the output document. The Reader must have
// been opened with the correct password. Forces a full rewrite.
func (w *Writer) Decrypt() {
	w.decrypt = true
	w.forceRewrite = true
}

// SetFormField sets the value of an AcroForm field by its fully qualified name.
// Uses incremental update. Sets /NeedAppearances on the AcroForm so viewers
// regenerate the visual appearance.
func (w *Writer) SetFormField(_ context.Context, name, value string) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return mutations.SetFormField(ctx, alloc, doc, name, value)
		},
	})
}

// FlattenForm bakes form field appearances into page content streams and
// removes the AcroForm dictionary. Forces a full rewrite.
func (w *Writer) FlattenForm(_ context.Context) {
	w.forceRewrite = true
	w.mutations = append(w.mutations, mutation{
		apply: mutations.FlattenForm,
	})
}

// StampText overlays text on the specified 1-based pages. If no pages are
// specified, the stamp is applied to all pages. Uses incremental update.
func (w *Writer) StampText(ctx context.Context, stamp TextStamp, pages ...int) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			resolvedPages, err := w.reader.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving pages: %w", err)
			}
			return mutations.StampText(ctx, alloc, doc, resolvedPages, stamp.Text, stamp.X, stamp.Y, stamp.FontSize, stamp.Opacity, pages)
		},
	})
}

// AddAttachment embeds a file in the document's /EmbeddedFiles name tree.
// Uses incremental update.
func (w *Writer) AddAttachment(_ context.Context, name string, data []byte, mimeType string) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return mutations.AddAttachment(ctx, alloc, doc, name, data, mimeType)
		},
	})
}

// Sign applies a digital signature using the given signer. Signing always uses
// incremental update (required for ByteRange integrity). Must be the last
// mutation before WriteTo.
func (w *Writer) Sign(_ context.Context, signer Signer) {
	w.signer = signer
}

// RemediatePDFA validates the document against the given PDF/A profile,
// applies targeted fixes, and queues the result for writing. The report
// describes what was fixed and what could not be fixed automatically.
// Uses incremental update to preserve existing document structure.
func (w *Writer) RemediatePDFA(ctx context.Context, profile models.PDFAProfile, opts ...RemediateOption) (*RemediationReport, error) {
	cfg := remediateOpts{lang: "en", preserveContent: true}
	for _, o := range opts {
		o(&cfg)
	}
	remOpts := remediation.Options{Lang: cfg.lang, PreserveContent: cfg.preserveContent}
	return w.remediate(ctx, func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (*remediation.Report, error) {
		return remediation.RemediatePDFA(ctx, doc, alloc, profile, remOpts)
	})
}

// RemediatePDFUA validates the document against the given PDF/UA profile,
// applies targeted fixes, and queues the result for writing.
func (w *Writer) RemediatePDFUA(ctx context.Context, profile models.PDFUAProfile, opts ...RemediateOption) (*RemediationReport, error) {
	cfg := remediateOpts{lang: "en", preserveContent: true}
	for _, o := range opts {
		o(&cfg)
	}
	remOpts := remediation.Options{Lang: cfg.lang, PreserveContent: cfg.preserveContent}
	return w.remediate(ctx, func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (*remediation.Report, error) {
		return remediation.RemediatePDFUA(ctx, doc, alloc, profile, remOpts)
	})
}

func (w *Writer) remediate(ctx context.Context, run func(context.Context, *writer.ObjAllocator, *document.Document) (*remediation.Report, error)) (*RemediationReport, error) {
	doc := w.reader.doc
	maxObjNum := 0
	for objNum := range doc.Index.Entries {
		if objNum > maxObjNum {
			maxObjNum = objNum
		}
	}
	alloc := writer.NewObjAllocator(maxObjNum)

	report, err := run(ctx, alloc, doc)
	if err != nil {
		return nil, err
	}

	if len(alloc.Pending()) > 0 || report.NeedsFullRewrite {
		pending := alloc.Pending()
		w.mutations = append(w.mutations, mutation{
			apply: func(_ context.Context, writeAlloc *writer.ObjAllocator, _ *document.Document) error {
				for objNum, obj := range pending {
					writeAlloc.AllocAt(objNum, obj)
				}
				return nil
			},
		})
	}

	if report.NeedsFullRewrite {
		w.forceRewrite = true
	}
	if report.RequiredVersion != "" {
		w.requiredVersion = report.RequiredVersion
	}

	return &RemediationReport{Fixed: report.Fixed, Residual: report.Residual}, nil
}

// WriteTo serializes the modified document to dst. By default, it uses an
// incremental update (appending to the original file). Pass [WithFullRewrite]
// to force a clean rewrite.
func (w *Writer) WriteTo(ctx context.Context, dst io.Writer, opts ...WriteOption) error {
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}

	// Signing requires its own two-pass incremental flow.
	if w.signer != nil {
		mutFuncs := make([]mutations.MutationFunc, len(w.mutations))
		for i, m := range w.mutations {
			mutFuncs[i] = m.apply
		}
		return mutations.WriteWithSignature(ctx, dst, w.reader.doc, w.signer, mutFuncs, cfg.xrefStream)
	}

	if cfg.fullRewrite || w.forceRewrite {
		mutFuncs := make([]mutations.MutationFunc, len(w.mutations))
		for i, m := range w.mutations {
			mutFuncs[i] = m.apply
		}
		return mutations.FullRewrite(ctx, dst, mutations.FullRewriteConfig{
			Doc:             w.reader.doc,
			Mutations:       mutFuncs,
			EncryptCfg:      w.encryptCfg,
			Decrypt:         w.decrypt,
			Version:         cfg.version,
			RequiredVersion: w.requiredVersion,
			XRefStream:      cfg.xrefStream,
		})
	}

	doc := w.reader.doc

	// Find max object number for the allocator.
	maxObjNum := 0
	for objNum := range doc.Index.Entries {
		if objNum > maxObjNum {
			maxObjNum = objNum
		}
	}

	alloc := writer.NewObjAllocator(maxObjNum)

	// Apply all batched mutations.
	for _, m := range w.mutations {
		if err := m.apply(ctx, alloc, doc); err != nil {
			return fmt.Errorf("applying mutation: %w", err)
		}
	}

	// If no mutations produced pending objects, nothing to write.
	if len(alloc.Pending()) == 0 {
		// Just copy the original file as-is.
		src := io.NewSectionReader(doc.RawReader(), 0, doc.FileSize())
		_, err := io.Copy(dst, src)
		return err
	}

	// Find the original startxref offset.
	startXRef, err := document.FindStartXRef(doc.RawReader(), doc.FileSize())
	if err != nil {
		return fmt.Errorf("finding startxref: %w", err)
	}

	// Build the trailer from the existing one.
	trailer := make(ast.PdfDict, len(doc.Index.Trailer))
	for k, v := range doc.Index.Trailer {
		if k == "Prev" || k == "Size" {
			continue // managed by the incremental engine
		}
		trailer[k] = v
	}

	// Detect xref format: use xref streams if the source used them or if requested.
	useXRefStream := cfg.xrefStream
	if _, ok := doc.Index.Trailer["Type"]; ok {
		// If trailer has /Type, it was a xref stream.
		useXRefStream = true
	}

	return writer.WriteIncremental(ctx, dst, writer.IncrementalConfig{
		Source:          doc.RawReader(),
		SourceSize:      doc.FileSize(),
		StartXRefOffset: startXRef,
		Allocator:       alloc,
		Trailer:         trailer,
		UseXRefStream:   useXRefStream,
	})
}

// Split extracts the specified 1-based pages from src into a new PDF document
// written to dst. Pages are included in the order given; duplicates are
// allowed to repeat a page.
func Split(ctx context.Context, src *Reader, pages []int, dst io.Writer, opts ...WriteOption) error {
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}

	allPages, err := src.resolvePages(ctx)
	if err != nil {
		return fmt.Errorf("resolving source pages: %w", err)
	}

	version := cfg.version
	return mutations.Split(ctx, src.doc, allPages, pages, dst, version, cfg.xrefStream)
}

// Merge combines multiple PDF documents into a single output written to dst.
// Pages are concatenated in order: all pages from sources[0], then all from
// sources[1], etc. Each source Reader must remain valid until Merge returns.
func Merge(ctx context.Context, sources []*Reader, dst io.Writer, opts ...WriteOption) error {
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}

	mergeSources := make([]mutations.MergeSource, len(sources))
	for i, src := range sources {
		pages, err := src.resolvePages(ctx)
		if err != nil {
			return fmt.Errorf("resolving pages from source %d: %w", i, err)
		}
		mergeSources[i] = mutations.MergeSource{
			Doc:   src.doc,
			Pages: pages,
		}
	}

	return mutations.Merge(ctx, mergeSources, dst, cfg.version, cfg.xrefStream)
}
