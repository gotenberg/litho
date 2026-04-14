package pdf

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/bookmarks"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/remediation"
	"github.com/gotenberg/litho/pkg/models"
)

// WriteOption configures the behavior of [Writer.WriteTo].
// WriteOption closures must be safe to call from any goroutine.
type WriteOption func(*writeOpts)

type writeOpts struct {
	fullRewrite     bool
	version         string
	xrefStream      bool
	logger          *slog.Logger
	bookmarks       *BookmarkOptions
	pipelineTempDir string
}

// WithFullRewrite forces a clean rewrite of the entire file instead of an
// incremental update. Required for operations that touch every object
// (encrypt, decrypt, flatten).
func WithFullRewrite() WriteOption {
	return func(o *writeOpts) {
		o.fullRewrite = true
	}
}

// WithVersion sets the PDF version in the output header (e.g., "2.0").
// Only applies to full rewrites.
func WithVersion(version string) WriteOption {
	return func(o *writeOpts) {
		o.version = version
	}
}

// WithXRefStream forces cross-reference stream format (PDF 1.5+) instead
// of a traditional xref table.
func WithXRefStream() WriteOption {
	return func(o *writeOpts) {
		o.xrefStream = true
	}
}

// WithWriteLogger injects a structured logger for write-phase diagnostics.
func WithWriteLogger(logger *slog.Logger) WriteOption {
	return func(o *writeOpts) {
		o.logger = logger
	}
}

// WithPipelineTempDir directs a [Pipeline] to place intermediate temp files
// in dir instead of [os.TempDir]. Useful in sandboxed environments where the
// default temp directory is not writable. Has no effect outside of a
// pipeline.
func WithPipelineTempDir(dir string) WriteOption {
	return func(o *writeOpts) {
		o.pipelineTempDir = dir
	}
}

// WithBookmarks configures how [Split] and [Merge] handle the document
// outline. When omitted, the operation uses sensible defaults
// ([BookmarkUpdate] with [OrphanDrop] and PromoteOrphanChildren=true for
// [Split]; [BookmarkUpdate] with no wrapping for [Merge]).
func WithBookmarks(opts BookmarkOptions) WriteOption {
	return func(o *writeOpts) {
		copy := opts
		o.bookmarks = &copy
	}
}

// RemediateOption configures the behavior of [Writer.RemediatePDFA] and
// [Writer.RemediatePDFUA].
// RemediateOption closures must be safe to call from any goroutine.
type RemediateOption func(*remediateOpts)

type remediateOpts struct {
	lang            string
	preserveContent bool
}

// WithLanguage sets the BCP 47 language tag for the catalog /Lang entry.
// Defaults to "en" if not specified.
func WithLanguage(lang string) RemediateOption {
	return func(o *remediateOpts) {
		o.lang = lang
	}
}

// WithAggressiveFixes enables fixers that may alter visible page content in
// exchange for better compliance. This includes stripping unknown content
// stream operators, clamping out-of-range integers, generating empty
// annotation appearances, and re-encoding LZW streams as FlateDecode.
func WithAggressiveFixes() RemediateOption {
	return func(o *remediateOpts) {
		o.preserveContent = false
	}
}

// RemediationReport describes what the remediation engine fixed and what it
// could not fix automatically.
// Values must not be shared across goroutines without synchronization.
type RemediationReport struct {
	// Fixed lists human-readable descriptions of applied fixes.
	Fixed []string
	// Residual contains violations that could not be fixed automatically.
	// Each element is a compliance.Violation.
	Residual []error
}

// BookmarkHandling controls how bookmarks are produced for the output of
// [Split] and [Merge].
// BookmarkHandling values are immutable and safe for concurrent use.
type BookmarkHandling int

const (
	// BookmarkUpdate keeps existing bookmarks and remaps page indexes.
	// Bookmarks pointing to dropped pages are handled per [OrphanPolicy].
	// This is the default.
	BookmarkUpdate BookmarkHandling = iota
	// BookmarkReconstruct ignores existing bookmarks and auto-generates a
	// new outline from the output content via the heading detector.
	BookmarkReconstruct
	// BookmarkMixed uses existing bookmarks when present, otherwise
	// auto-generates per source.
	BookmarkMixed
	// BookmarkNone produces no outline at all.
	BookmarkNone
)

// OrphanPolicy controls what happens to bookmarks whose target page is no
// longer in the output document (Split only).
// OrphanPolicy values are immutable and safe for concurrent use.
type OrphanPolicy int

const (
	// OrphanDrop removes the bookmark.
	OrphanDrop OrphanPolicy = iota
	// OrphanRemap retargets the bookmark to the nearest kept page.
	OrphanRemap
	// OrphanKeep emits the bookmark with no destination so the title
	// remains visible (no clickable target).
	OrphanKeep
)

// BookmarkOptions configures bookmark handling for [Split] and [Merge].
// Pass to a write call via [WithBookmarks].
// Values must not be shared across goroutines without synchronization.
type BookmarkOptions struct {
	// Handling selects the high-level strategy. Defaults to BookmarkUpdate.
	Handling BookmarkHandling

	// OrphanPolicy controls what happens to bookmarks for dropped pages
	// during Split. Defaults to OrphanDrop.
	OrphanPolicy OrphanPolicy
	// PromoteOrphanChildren promotes children of dropped bookmarks to the
	// dropped bookmark's level. Default true for Split.
	PromoteOrphanChildren bool

	// WrapEachSource wraps each source's bookmarks under a per-source
	// container labeled by [SourceLabels] (Merge only).
	WrapEachSource bool
	// SourceLabels provides custom labels for the WrapEachSource roots.
	// Missing entries fall back to "Document N".
	SourceLabels []string

	// Generation controls auto-generation when Handling is
	// BookmarkReconstruct or BookmarkMixed.
	Generation BookmarkGenerationOptions
}

// Metadata holds document information dictionary fields for [Writer.SetMetadata].
// Values must not be shared across goroutines without synchronization.
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
// Values must not be shared across goroutines without synchronization.
type TextStamp struct {
	// Text is the line to draw.
	Text string
	// X and Y position the stamp in points from the page's bottom-left
	// corner. When Rotation is non-zero, (X, Y) is the rotation origin.
	X, Y float64
	// FontSize is the text size in points; zero defaults to 12.
	FontSize float64
	// Opacity is 0.0-1.0 (1.0 = fully opaque). Zero is treated as fully
	// opaque for back-compat with the original API.
	Opacity float64
	// Font selects the text font. The zero value uses [FontHelvetica].
	// Construct with one of the [FontHelvetica], [FontTimesRoman], ...
	// package variables, or with [NewTTFFont] to embed a custom TrueType
	// font.
	Font StampFont
	// Color is the RGB fill color (0.0-1.0 components). Zero draws black.
	Color [3]float64
	// Rotation is the counter-clockwise rotation in degrees around (X, Y).
	Rotation float64
}

// StampFont identifies the font used by a [TextStamp]. Construct with one
// of the package-level [FontHelvetica], [FontTimesRoman], ... variables
// for the 14 standard PDF fonts (always available, no embedding required),
// or with [NewTTFFont] to embed a custom TrueType font.
// Values must not be shared across goroutines without synchronization.
type StampFont struct {
	// name holds the /BaseFont name for standard fonts and the derived
	// PostScript name for custom fonts.
	name string
	// ttf holds the parsed bytes for a custom TTF. nil for standard fonts.
	ttf []byte
}

// Standard 14 PDF fonts. These are always available in conformant PDF
// viewers and do not need to be embedded.
var (
	FontHelvetica            = StampFont{name: "Helvetica"}
	FontHelveticaBold        = StampFont{name: "Helvetica-Bold"}
	FontHelveticaOblique     = StampFont{name: "Helvetica-Oblique"}
	FontHelveticaBoldOblique = StampFont{name: "Helvetica-BoldOblique"}
	FontTimesRoman           = StampFont{name: "Times-Roman"}
	FontTimesBold            = StampFont{name: "Times-Bold"}
	FontTimesItalic          = StampFont{name: "Times-Italic"}
	FontTimesBoldItalic      = StampFont{name: "Times-BoldItalic"}
	FontCourier              = StampFont{name: "Courier"}
	FontCourierBold          = StampFont{name: "Courier-Bold"}
	FontCourierOblique       = StampFont{name: "Courier-Oblique"}
	FontCourierBoldOblique   = StampFont{name: "Courier-BoldOblique"}
	FontSymbol               = StampFont{name: "Symbol"}
	FontZapfDingbats         = StampFont{name: "ZapfDingbats"}
)

// NewTTFFont returns a [StampFont] that embeds the given TrueType font
// bytes in the output PDF. The bytes are retained by the returned value,
// so callers may reuse them across multiple stamps. The font is validated
// (table directory + required tables) at call time; parse errors surface
// here rather than at write time.
func NewTTFFont(data []byte) (StampFont, error) {
	if len(data) < 12 {
		return StampFont{}, fmt.Errorf("%w: data too short (%d bytes)", ErrInvalidFont, len(data))
	}
	if err := mutations.ValidateTTFForStamp(data); err != nil {
		return StampFont{}, err
	}
	// Copy to detach from caller-owned memory.
	cp := make([]byte, len(data))
	copy(cp, data)
	return StampFont{ttf: cp}, nil
}

// ImageStamp defines an image overlay. Data must be JPEG or PNG bytes.
// Values must not be shared across goroutines without synchronization.
type ImageStamp struct {
	// Data is the raw image file contents.
	Data []byte
	// MimeType must be "image/jpeg" or "image/png".
	MimeType string
	// X and Y position the bottom-left of the image in points.
	X, Y float64
	// Width and Height set the target size in points. Zero for one means
	// "proportional to the other". Zero for both means "use the image's
	// natural pixel dimensions as points".
	Width, Height float64
	// Opacity is 0.0-1.0; zero treated as fully opaque.
	Opacity float64
	// Rotation is the counter-clockwise rotation in degrees around (X, Y).
	Rotation float64
}

// PDFStamp draws a page from another PDF document as a stamp.
// Values must not be shared across goroutines without synchronization.
type PDFStamp struct {
	// Source is the stamp document. The caller retains ownership and must
	// keep it valid until [Writer.WriteTo] is called.
	Source *Reader
	// Page is the 1-based source page number. Zero is treated as 1.
	Page int
	// X and Y position the bottom-left of the stamp in points.
	X, Y float64
	// Scale is the uniform scale factor. Zero is treated as 1.0.
	Scale float64
	// Opacity is 0.0-1.0; zero treated as fully opaque.
	Opacity float64
	// Rotation is the counter-clockwise rotation in degrees around (X, Y).
	Rotation float64
}

// Signer produces a PKCS#7 / CMS signature for the given digest.
// Implementations control key material and signing algorithms. litho
// calls [Signer.Sign] during [Writer.WriteTo] with the SHA-256 hash of
// the ByteRange regions.
//
// The interface is owned by the [pdf] package so callers never have to
// import an internal path. An internal adapter forwards user
// implementations to the signing engine at write time.
// Implementations must be safe for concurrent use.
type Signer interface {
	// Sign computes a PKCS#7 / CMS signature over the given SHA-256
	// digest bytes.
	Sign(digest []byte) ([]byte, error)
	// Certificate returns the signing certificate for embedding in the
	// signature field's /Cert entry.
	Certificate() *x509.Certificate
	// EstimatedSize returns the maximum expected signature byte count;
	// used to size the /Contents placeholder in the output PDF.
	EstimatedSize() int
}

// signerAdapter wraps a public [Signer] so it satisfies the internal
// [mutations.Signer] interface. The method sets are identical; this
// adapter exists only to bridge the two named interface types without
// leaking the internal one through the public API.
type signerAdapter struct{ inner Signer }

func (a signerAdapter) Sign(digest []byte) ([]byte, error) { return a.inner.Sign(digest) }
func (a signerAdapter) Certificate() *x509.Certificate     { return a.inner.Certificate() }
func (a signerAdapter) EstimatedSize() int                 { return a.inner.EstimatedSize() }

// Writer applies content-preserving modifications to a PDF document.
// Mutations are batched and written atomically via [Writer.WriteTo].
// Writer is not safe for concurrent use; callers must serialize all method calls.
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
// in meta are skipped (the existing value is preserved). The mutation is
// queued and applied when [Writer.WriteTo] is called.
func (w *Writer) SetMetadata(meta Metadata) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return mutations.SetMetadata(ctx, alloc, doc, mutations.Metadata(meta))
		},
	})
}

// ClearMetadata removes all document metadata: the /Info dictionary is emptied
// and the catalog /Metadata stream (XMP) is removed. Forces a full rewrite.
// The mutation is queued and applied when [Writer.WriteTo] is called.
func (w *Writer) ClearMetadata() {
	w.forceRewrite = true
	w.mutations = append(w.mutations, mutation{
		apply: mutations.ClearMetadata,
	})
}

// RotatePages sets the /Rotate value for the given 1-based page numbers.
// Rotation must be a multiple of 90 (0, 90, 180, 270). The mutation is
// queued and applied when [Writer.WriteTo] is called.
func (w *Writer) RotatePages(rotation int, pages ...int) error {
	if rotation%90 != 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidRotation, rotation)
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

// DeletePages removes the specified 1-based pages from the document.
// Forces a full rewrite. The mutation is queued and applied when
// [Writer.WriteTo] is called.
func (w *Writer) DeletePages(pages ...int) error {
	if len(pages) == 0 {
		return ErrEmptyPageList
	}
	w.forceRewrite = true
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			resolvedPages, err := w.reader.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving pages: %w", err)
			}
			return mutations.DeletePages(ctx, alloc, doc, resolvedPages, pages)
		},
	})
	return nil
}

// InsertPages inserts pages from source at the given 0-based position in
// the document. Position 0 inserts before the first page; position equal
// to the current page count appends. If sourcePages is empty, all pages
// from source are inserted. Forces a full rewrite. The mutation is queued
// and applied when [Writer.WriteTo] is called.
func (w *Writer) InsertPages(pos int, source *Reader, sourcePages ...int) error {
	if source == nil {
		return ErrNilSource
	}
	w.forceRewrite = true
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			targetPages, err := w.reader.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving target pages: %w", err)
			}
			srcPages, err := source.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving source pages: %w", err)
			}
			return mutations.InsertPages(ctx, alloc, doc, targetPages, pos, source.doc, srcPages, sourcePages)
		},
	})
	return nil
}

// SetBookmarks replaces the document outline with the given bookmarks.
// The mutation is queued and applied when [Writer.WriteTo] is called.
func (w *Writer) SetBookmarks(bookmarks []models.Bookmark) {
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

// BookmarkMergeMode controls how auto-generated bookmarks are combined with
// any existing document outline.
// BookmarkMergeMode values are immutable and safe for concurrent use.
type BookmarkMergeMode int

const (
	// BookmarkReplace replaces the existing outline with the generated
	// bookmarks. This is the default.
	BookmarkReplace BookmarkMergeMode = iota
	// BookmarkPrepend inserts generated bookmarks before any existing entries.
	BookmarkPrepend
	// BookmarkAppend inserts generated bookmarks after any existing entries.
	BookmarkAppend
)

// GenerateBookmarks auto-detects headings in the document and queues a
// mutation that replaces the outline with the generated bookmark tree.
// The merge mode controls whether existing bookmarks are preserved. The
// mutation is queued and applied when [Writer.WriteTo] is called.
func (w *Writer) GenerateBookmarks(opts BookmarkGenerationOptions, mode BookmarkMergeMode) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			generated, err := w.reader.GenerateBookmarks(ctx, opts)
			if err != nil {
				return fmt.Errorf("generating bookmarks: %w", err)
			}
			if len(generated) == 0 {
				return nil
			}

			final := generated
			switch mode {
			case BookmarkReplace:
				// nothing to merge
			case BookmarkPrepend, BookmarkAppend:
				existing, err := w.reader.Bookmarks(ctx)
				if err == nil && len(existing) > 0 {
					if mode == BookmarkPrepend {
						merged := make([]models.Bookmark, 0, len(generated)+len(existing))
						merged = append(merged, generated...)
						merged = append(merged, existing...)
						final = merged
					} else {
						merged := make([]models.Bookmark, 0, len(existing)+len(generated))
						merged = append(merged, existing...)
						merged = append(merged, generated...)
						final = merged
					}
				}
			}

			resolvedPages, err := w.reader.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving pages for bookmarks: %w", err)
			}
			return mutations.SetBookmarks(ctx, alloc, doc, resolvedPages, final)
		},
	})
}

// EncryptAlgorithm identifies a PDF standard-security-handler algorithm.
// Values cover the ISO 32000 V=1..5 security handlers. New code should
// prefer [EncryptAES256]; the RC4 variants are provided for
// legacy-compatibility writes only.
// EncryptAlgorithm values are immutable and safe for concurrent use.
type EncryptAlgorithm int

const (
	// EncryptRC4_40 uses 40-bit RC4 (V=1, R=2). Weak. Legacy only.
	EncryptRC4_40 EncryptAlgorithm = iota
	// EncryptRC4_128 uses 128-bit RC4 (V=2, R=3). Weak. Legacy only.
	EncryptRC4_128
	// EncryptAES128 uses 128-bit AES-CBC (V=4, R=4).
	EncryptAES128
	// EncryptAES256 uses 256-bit AES-CBC (V=5, R=6). Recommended.
	EncryptAES256
)

// EncryptConfig specifies the parameters for password-based encryption.
// It is the public-API mirror of the internal encryption config: the
// [pdf] package owns this type so callers never import an internal
// path.
// Values must not be shared across goroutines without synchronization.
type EncryptConfig struct {
	// UserPassword unlocks the document for reading. Empty string means
	// the document can be opened without a password, but [Permissions]
	// still apply to viewers that enforce them.
	UserPassword string
	// OwnerPassword grants full access. Writers should pick a different
	// value from [UserPassword]; callers that supply identical passwords
	// get the same effective permissions as an empty-user-password doc.
	OwnerPassword string
	// Algorithm selects the standard security handler variant.
	Algorithm EncryptAlgorithm
	// Permissions is the 32-bit PDF permission mask. Pass -1 for "all
	// permissions granted".
	Permissions int32
}

// toInternal translates an [EncryptConfig] into the internal
// [crypto.EncryptConfig] consumed by the write engine. Kept
// lowercase-private so the internal type never appears in a public
// signature.
func (c EncryptConfig) toInternal() crypto.EncryptConfig {
	return crypto.EncryptConfig{
		UserPassword:  c.UserPassword,
		OwnerPassword: c.OwnerPassword,
		Algorithm:     crypto.Algorithm(c.Algorithm),
		Permissions:   c.Permissions,
	}
}

// Encrypt applies password-based encryption to the output document.
// Forces a full rewrite since every string and stream must be
// encrypted.
func (w *Writer) Encrypt(cfg EncryptConfig) {
	internal := cfg.toInternal()
	w.encryptCfg = &internal
	w.forceRewrite = true
}

// RemoveEncryption removes encryption from the output document. The Reader
// must have been opened with the correct password. Forces a full rewrite.
func (w *Writer) RemoveEncryption() {
	w.decrypt = true
	w.forceRewrite = true
}

// SetFormField sets the value of an AcroForm field by its fully qualified name.
// Uses incremental update. Sets /NeedAppearances on the AcroForm so viewers
// regenerate the visual appearance. The mutation is queued and applied when
// [Writer.WriteTo] is called.
func (w *Writer) SetFormField(name, value string) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return mutations.SetFormField(ctx, alloc, doc, name, value)
		},
	})
}

// FlattenForm bakes form field appearances into page content streams and
// removes the AcroForm dictionary. Forces a full rewrite. The mutation is
// queued and applied when [Writer.WriteTo] is called.
func (w *Writer) FlattenForm() {
	w.forceRewrite = true
	w.mutations = append(w.mutations, mutation{
		apply: mutations.FlattenForm,
	})
}

// StampText overlays text on the specified 1-based pages. If no pages are
// specified, the stamp is applied to all pages. Uses incremental update.
// The mutation is queued and applied when [Writer.WriteTo] is called.
func (w *Writer) StampText(stamp TextStamp, pages ...int) error {
	if stamp.Text == "" {
		return ErrEmptyText
	}
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			resolvedPages, err := w.reader.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving pages: %w", err)
			}
			return mutations.StampText(ctx, alloc, doc, resolvedPages, textStampToOptions(stamp), pages)
		},
	})
	return nil
}

// StampImage overlays a JPEG or PNG image on the specified 1-based pages.
// If no pages are specified, the stamp is applied to all pages. Uses
// incremental update. The mutation is queued and applied when
// [Writer.WriteTo] is called.
func (w *Writer) StampImage(stamp ImageStamp, pages ...int) error {
	if len(stamp.Data) == 0 {
		return ErrEmptyData
	}
	if stamp.MimeType != "image/jpeg" && stamp.MimeType != "image/png" {
		return fmt.Errorf("%w: %q", ErrUnsupportedMimeType, stamp.MimeType)
	}
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			resolvedPages, err := w.reader.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving pages: %w", err)
			}
			return mutations.StampImage(ctx, alloc, doc, resolvedPages, mutations.ImageStampOptions{
				Data:     stamp.Data,
				MimeType: stamp.MimeType,
				X:        stamp.X,
				Y:        stamp.Y,
				Width:    stamp.Width,
				Height:   stamp.Height,
				Opacity:  stamp.Opacity,
				Rotation: stamp.Rotation,
			}, pages)
		},
	})
	return nil
}

// StampPDF overlays a page from another PDF document on the specified
// 1-based pages. If no pages are specified, the stamp is applied to all
// pages. Uses incremental update. The mutation is queued and applied when
// [Writer.WriteTo] is called.
func (w *Writer) StampPDF(stamp PDFStamp, pages ...int) error {
	if stamp.Source == nil {
		return ErrNilSource
	}
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			resolvedPages, err := w.reader.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving pages: %w", err)
			}
			srcPages, err := stamp.Source.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving stamp source pages: %w", err)
			}
			return mutations.StampPDF(ctx, alloc, doc, resolvedPages, mutations.PDFStampOptions{
				SourceDoc:   stamp.Source.doc,
				SourcePages: srcPages,
				Page:        stamp.Page,
				X:           stamp.X,
				Y:           stamp.Y,
				Scale:       stamp.Scale,
				Opacity:     stamp.Opacity,
				Rotation:    stamp.Rotation,
			}, pages)
		},
	})
	return nil
}

// textStampToOptions translates a public [TextStamp] into the internal
// [mutations.TextStampOptions] the stamp engine consumes.
func textStampToOptions(s TextStamp) mutations.TextStampOptions {
	opts := mutations.TextStampOptions{
		Text:     s.Text,
		X:        s.X,
		Y:        s.Y,
		FontSize: s.FontSize,
		Opacity:  s.Opacity,
		Color:    s.Color,
		Rotation: s.Rotation,
	}
	if len(s.Font.ttf) > 0 {
		opts.TTFBytes = s.Font.ttf
	} else {
		name := s.Font.name
		if name == "" {
			name = "Helvetica"
		}
		opts.StandardFont = ast.PdfName(name)
	}
	return opts
}

// AddAttachment embeds a file in the document's /EmbeddedFiles name tree.
// Uses incremental update. The mutation is queued and applied when
// [Writer.WriteTo] is called.
func (w *Writer) AddAttachment(name string, data []byte, mimeType string) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return mutations.AddAttachment(ctx, alloc, doc, name, data, mimeType)
		},
	})
}

// RemoveAttachment removes a file from the document's /EmbeddedFiles name
// tree by its filename. Uses incremental update. The mutation is queued and
// applied when [Writer.WriteTo] is called.
func (w *Writer) RemoveAttachment(name string) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return mutations.RemoveAttachment(ctx, alloc, doc, name)
		},
	})
}

// Sign applies a digital signature using the given signer. Signing always uses
// incremental update (required for ByteRange integrity). Must be the last
// mutation before [Writer.WriteTo].
func (w *Writer) Sign(signer Signer) {
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
// to force a clean rewrite. Pass [WithWriteLogger] to receive structured
// debug logs for every mutation, best-effort fallback, and write-engine
// decision.
func (w *Writer) WriteTo(ctx context.Context, dst io.Writer, opts ...WriteOption) error {
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}

	// Swap the document logger for the duration of the write so every
	// internal call site that uses doc.Logger (mutations, copier, remediation)
	// routes through the caller's logger. The previous logger is restored on
	// return. The Reader is not safe for concurrent writes, so there is no
	// race here.
	if cfg.logger != nil {
		prev := w.reader.doc.Logger
		w.reader.doc.Logger = cfg.logger
		defer func() { w.reader.doc.Logger = prev }()
	}
	logger := w.reader.doc.Logger

	logger.Debug("writer.WriteTo started",
		slog.Int("mutations", len(w.mutations)),
		slog.Bool("full_rewrite", cfg.fullRewrite || w.forceRewrite),
		slog.Bool("signer", w.signer != nil),
		slog.Bool("xref_stream", cfg.xrefStream),
	)

	// Signing requires its own two-pass incremental flow.
	if w.signer != nil {
		mutFuncs := make([]mutations.MutationFunc, len(w.mutations))
		for i, m := range w.mutations {
			mutFuncs[i] = m.apply
		}
		return mutations.WriteWithSignature(ctx, dst, w.reader.doc, signerAdapter{inner: w.signer}, mutFuncs, cfg.xrefStream)
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
	for i, m := range w.mutations {
		if err := m.apply(ctx, alloc, doc); err != nil {
			return fmt.Errorf("applying mutation %d: %w", i, err)
		}
		logger.Debug("mutation applied", slog.Int("index", i))
	}

	// If no mutations produced pending objects, nothing to write.
	if len(alloc.Pending()) == 0 {
		logger.Debug("no pending objects, copying source as-is")
		src := io.NewSectionReader(doc.RawReader(), 0, doc.FileSize())
		_, err := io.Copy(dst, src)
		return err
	}
	logger.Debug("incremental update",
		slog.Int("pending_objects", len(alloc.Pending())),
	)

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
// allowed to repeat a page. Pass [WithBookmarks] to control how the source
// outline is handled in the output (default: keep and remap existing,
// drop orphans). Pass [WithWriteLogger] to receive structured debug logs
// from the split engine.
func Split(ctx context.Context, src *Reader, pages []int, dst io.Writer, opts ...WriteOption) error {
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}

	if cfg.logger != nil {
		prev := src.doc.Logger
		src.doc.Logger = cfg.logger
		defer func() { src.doc.Logger = prev }()
	}
	src.doc.Logger.Debug("split started",
		slog.Int("pages", len(pages)),
	)

	allPages, err := src.resolvePages(ctx)
	if err != nil {
		return fmt.Errorf("resolving source pages: %w", err)
	}

	version := cfg.version
	bookmarkCfg := buildSplitBookmarkConfig(src, cfg.bookmarks)
	return mutations.Split(ctx, src.doc, allPages, pages, dst, version, cfg.xrefStream, bookmarkCfg)
}

// Merge combines multiple PDF documents into a single output written to dst.
// Pages are concatenated in order: all pages from sources[0], then all from
// sources[1], etc. Each source [Reader] must remain valid until Merge returns.
// Pass [WithBookmarks] to control how outlines from the sources are merged
// (default: concatenate with offset page indexes). Pass [WithWriteLogger] to
// receive structured debug logs from the merge engine (logs are emitted via
// the first source's document logger).
func Merge(ctx context.Context, sources []*Reader, dst io.Writer, opts ...WriteOption) error {
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}

	if len(sources) > 0 && cfg.logger != nil {
		prev := sources[0].doc.Logger
		sources[0].doc.Logger = cfg.logger
		defer func() { sources[0].doc.Logger = prev }()
	}
	if len(sources) > 0 {
		sources[0].doc.Logger.Debug("merge started",
			slog.Int("sources", len(sources)),
		)
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

	bookmarkCfg := buildMergeBookmarkConfig(sources, cfg.bookmarks)
	return mutations.Merge(ctx, mergeSources, dst, cfg.version, cfg.xrefStream, bookmarkCfg)
}

// buildSplitBookmarkConfig translates the public BookmarkOptions into the
// internal mutations.BookmarkConfig for a Split operation.
func buildSplitBookmarkConfig(src *Reader, opts *BookmarkOptions) mutations.BookmarkConfig {
	if opts == nil {
		cfg := mutations.DefaultSplitBookmarkConfig()
		cfg.TextElements = func(ctx context.Context, _ int, pageNum int) ([]models.TextElement, error) {
			return src.ExtractTextElements(ctx, pageNum)
		}
		return cfg
	}
	cfg := mutations.BookmarkConfig{
		Handling:              mutations.BookmarkHandling(opts.Handling),
		OrphanPolicy:          mutations.OrphanHandling(opts.OrphanPolicy),
		PromoteOrphanChildren: opts.PromoteOrphanChildren,
		Generation:            generationToInternal(opts.Generation),
		TextElements: func(ctx context.Context, _ int, pageNum int) ([]models.TextElement, error) {
			return src.ExtractTextElements(ctx, pageNum)
		},
	}
	return cfg
}

// generationToInternal translates the public BookmarkGenerationOptions into
// the internal bookmarks.Options used by the auto-generation engine.
func generationToInternal(opts BookmarkGenerationOptions) bookmarks.Options {
	return bookmarks.Options{
		MaxDepth:                opts.MaxDepth,
		MinFontSizeRatio:        opts.MinFontSizeRatio,
		SkipHeaderFooter:        opts.SkipHeaderFooter,
		MaxTitleLength:          opts.MaxTitleLength,
		PreferStructureTree:     opts.PreferStructureTree,
		DetectNumbering:         opts.DetectNumbering,
		StripNumberingFromTitle: opts.StripNumberingFromTitle,
		Scorer:                  adaptHeadingScorer(opts.HeadingScorer),
		ScoreThreshold:          opts.HeadingScoreThreshold,
	}
}

// buildMergeBookmarkConfig translates the public BookmarkOptions into the
// internal mutations.BookmarkConfig for a Merge operation.
func buildMergeBookmarkConfig(sources []*Reader, opts *BookmarkOptions) mutations.BookmarkConfig {
	textProvider := func(ctx context.Context, sourceIdx int, pageNum int) ([]models.TextElement, error) {
		if sourceIdx < 0 || sourceIdx >= len(sources) {
			return nil, ErrSourceIndexOutOfRange
		}
		return sources[sourceIdx].ExtractTextElements(ctx, pageNum)
	}
	if opts == nil {
		cfg := mutations.DefaultMergeBookmarkConfig()
		cfg.TextElements = textProvider
		return cfg
	}
	cfg := mutations.BookmarkConfig{
		Handling:       mutations.BookmarkHandling(opts.Handling),
		WrapEachSource: opts.WrapEachSource,
		SourceLabels:   opts.SourceLabels,
		Generation:     generationToInternal(opts.Generation),
		TextElements:   textProvider,
	}
	return cfg
}
