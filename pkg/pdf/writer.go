package pdf

import (
	"bufio"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"

	"github.com/gotenberg/litho/pkg/internal/bookmarks"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/remediation"
	"github.com/gotenberg/litho/pkg/internal/tagging"
	"github.com/gotenberg/litho/pkg/models"
)

// WriteOption configures the behavior of [Writer.WriteTo].
// WriteOption closures must be safe to call from any goroutine.
type WriteOption func(*writeOpts)

type writeOpts struct {
	fullRewrite       bool
	version           string
	xrefStream        bool
	packObjectStreams bool
	dedupStreams      bool
	logger            *slog.Logger
	bookmarks         *BookmarkOptions
	pipelineTempDir   string
	encrypt           *EncryptConfig
}

// WithFullRewrite forces a clean rewrite of the entire file instead of an
// incremental update. Required for operations that touch every object
// (encrypt, decrypt, flatten).
func WithFullRewrite() WriteOption {
	return func(o *writeOpts) {
		o.fullRewrite = true
	}
}

// WithEncryption encrypts the output with cfg. It applies to
// [Writer.WriteTo], [Split] and [Merge].
//
// The standalone operations need it because a source's encryption cannot
// survive them unaided: the reader decrypts every object as it resolves it,
// so the graph they copy is already plaintext and the output is written in
// the clear. Omitting the option preserves exactly that behaviour.
//
// Encryption describes the OUTPUT, not the input. [Merge] may therefore take
// sources opened under different passwords and write them all under the
// single key derived from cfg.
//
// On [Writer.WriteTo] this is equivalent to [Writer.Encrypt] and likewise
// forces a full rewrite, since every string and stream must be encrypted.
func WithEncryption(cfg EncryptConfig) WriteOption {
	return func(o *writeOpts) {
		o.encrypt = &cfg
	}
}

// compactOptions maps the public write options onto the mutations layer's
// compaction selector, keeping that internal type out of every public
// signature the same way [writeOpts.internalEncrypt] does.
func (o writeOpts) compactOptions() mutations.CompactOptions {
	return mutations.CompactOptions{
		PackObjectStreams: o.packObjectStreams,
		DedupStreams:      o.dedupStreams,
	}
}

// internalEncrypt translates the requested encryption into the internal
// config the write engine consumes, or nil when none was requested.
//
// Converting here is what lets [Split] and [Merge] hand the value straight to
// the mutations layer while [crypto.EncryptConfig] stays out of every public
// signature.
func (o writeOpts) internalEncrypt() *crypto.EncryptConfig {
	if o.encrypt == nil {
		return nil
	}
	internal := o.encrypt.toInternal()
	return &internal
}

// WithVersion sets the PDF version in the output header (e.g., "2.0").
// Only applies to full rewrites.
func WithVersion(version string) WriteOption {
	return func(o *writeOpts) {
		o.version = version
	}
}

// WithXRefStream forces cross-reference stream format (PDF 1.5+) instead
// of a traditional xref table. The output declares at least PDF 1.5
// accordingly, since a reader honoring an earlier declaration cannot parse a
// cross-reference stream at all.
func WithXRefStream() WriteOption {
	return func(o *writeOpts) {
		o.xrefStream = true
	}
}

// WithObjectStreams packs eligible objects into compressed object streams
// (ISO 32000-1, 7.5.7), which removes the per-object "N 0 obj"/"endobj"
// overhead and compresses the dictionaries themselves.
//
// It applies to the full-rewrite path only. An incremental update appends to
// the original bytes and leaves their packing untouched, so the option has no
// effect there. Packing implies a cross-reference stream, since a traditional
// table cannot name a compressed object, and litho writes one whether or not
// [WithXRefStream] was passed.
//
// Object streams and cross-reference streams are both PDF 1.5 constructs, so
// a packed output declares at least PDF 1.5 even when the source declared
// less, in the header and in the catalog /Version entry when the source
// carried one. A document that must keep an earlier version must not be
// packed: ISO 19005-1 is written against PDF 1.4 and its 6.1.4 forbids
// cross-reference streams outright, so packing a PDF/A-1 file costs it
// conformance.
//
// An encrypted output is never packed. Encryption runs over every object
// before serialization, so packing afterwards would place ciphertext strings
// inside a stream that is itself encrypted, and ISO 32000-1, 7.5.7 requires
// that strings in an object stream are not encrypted separately. Combining
// this option with [WithEncryption] writes an unpacked file rather than an
// unreadable one.
func WithObjectStreams() WriteOption {
	return func(o *writeOpts) {
		o.packObjectStreams = true
	}
}

// WithDedup merges byte-identical stream objects in the output, pointing every
// reference at one survivor. A document that embeds the same image, font
// program or ICC profile more than once carries one copy instead.
//
// Only streams are merged. Two identical dictionaries are left alone, because
// structure elements, form fields and annotations are resolved by object
// number, and collapsing a pair of those changes what the document means
// rather than how it is stored.
//
// Like [WithObjectStreams] this applies to the full-rewrite path only, and an
// encrypted output is never deduplicated: encryption derives a per-object key
// from the object number (ISO 32000-1, 7.6.2), so identical plaintext streams
// already differ as ciphertext and merging them would hand a reader bytes
// decrypted under the wrong key.
func WithDedup() WriteOption {
	return func(o *writeOpts) {
		o.dedupStreams = true
	}
}

// WithWriteLogger injects a structured logger for write-phase diagnostics.
func WithWriteLogger(logger *slog.Logger) WriteOption {
	return func(o *writeOpts) {
		o.logger = logger
	}
}

// WithPipelineTempDir directs a [Pipeline] to place intermediate temp files
// in dir instead of [os.TempDir], the ones a remediation in an
// [Pipeline.Apply] stage writes included. Useful in sandboxed environments
// where the default temp directory is not writable. Has no effect outside of
// a pipeline.
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
	figureAlt       string
	tolerance       RegressionTolerance
	documentID      []byte
}

// RegressionTolerance bounds the integrity probe that runs after
// remediation. If the probe detects a content drop beyond these bounds,
// content-modifying fixers are rolled back and, if the output still
// regresses, remediation fails with [ErrRemediationBrokePDF].
// Values are immutable and safe for concurrent use.
type RegressionTolerance struct {
	// MaxTextDropRatio is the largest acceptable fraction of total
	// extracted text that remediation may remove. 0.0 forbids any drop;
	// 1.0 accepts any drop (including going from text to no text). The
	// ratio is evaluated against the sum of [Reader.ExtractText] lengths
	// across all pages. Defaults to 0.1 (10%).
	MaxTextDropRatio float64
	// MaxImageDropRatio is the largest acceptable fraction of
	// [Reader.ExtractImages] entries that remediation may remove.
	// Defaults to 0.5 (50%) to accommodate aggressive image-stream fixers
	// that legitimately drop corrupt XObjects.
	MaxImageDropRatio float64
	// AllowPageLoss permits the page count to decrease. When false (the
	// default), any change to the page count is treated as a regression.
	AllowPageLoss bool
}

// DefaultRegressionTolerance is the tolerance applied when none is set via
// [WithRegressionTolerance]. Values prioritize catching silent text loss,
// which is the most common aggressive-fix failure mode.
var DefaultRegressionTolerance = RegressionTolerance{
	MaxTextDropRatio:  0.1,
	MaxImageDropRatio: 0.5,
	AllowPageLoss:     false,
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

// WithFigureAltText sets the alternate text written into a Figure structure
// element that lacks the /Alt PDF/UA requires (ISO 14289-1 §7.3, ISO 14289-2
// §8.2.5.28.2). A processor cannot describe an image, so litho never fabricates
// alt text by default: it leaves the figure for a human. Supplying a string here
// fills missing /Alt in any mode; the string is the caller's own content, not a
// machine guess. Without it, [WithAggressiveFixes] inserts a generic placeholder
// to pass the mechanical presence check, which satisfies the validator but does
// not convey the image to a reader, so the document still needs human review.
func WithFigureAltText(alt string) RemediateOption {
	return func(o *remediateOpts) {
		o.figureAlt = alt
	}
}

// WithDocumentID pins the 16-byte seed for the trailer /ID that
// remediation mints when the source has none (or a malformed one).
// Reproducible-build callers pin it so identical inputs produce identical
// output bytes; the random /ID is otherwise litho's documented
// nondeterminism seam. An id of any other length is ignored and the
// random seam applies. [Writer.Encrypt] mints its own ID and is not
// affected by this option.
// The slice is retained, not copied; leave it untouched until the
// remediation completes.
func WithDocumentID(id []byte) RemediateOption {
	return func(o *remediateOpts) {
		o.documentID = id
	}
}

// WithRegressionTolerance overrides [DefaultRegressionTolerance] for a
// single remediation call. Use tighter bounds for documents whose textual
// or visual integrity must be preserved byte-for-byte; use looser bounds
// when aggressive compliance outranks content fidelity.
func WithRegressionTolerance(tol RegressionTolerance) RemediateOption {
	return func(o *remediateOpts) {
		o.tolerance = tol
	}
}

// RemediationReport describes what the remediation engine fixed and what it
// could not fix automatically.
// Values must not be shared across goroutines without synchronization.
type RemediationReport struct {
	// Fixed lists human-readable descriptions of applied fixes that were
	// kept in the output.
	Fixed []string
	// Residual contains violations that could not be fixed automatically,
	// in the flat error form the Validate methods return.
	// [RemediationReport.ResidualViolations] returns the same findings as
	// structured [Violation] values. If content-modifying fixers were rolled
	// back, the violations they had claimed to handle move back here.
	//
	// A non-empty Residual does not stop the output from ASSERTING the
	// profile. The identification schemas are themselves requirements
	// (ISO 19005-1, 6.7.11 and ISO 14289-1, clause 5), so remediation writes
	// pdfaid or pdfuaid to satisfy them, and a document still failing other
	// clauses carries that claim regardless: a PDF/A-2b run leaving seven
	// font-embedding violations emits pdfaid:part 2 and pdfaid:conformance B
	// all the same. Read this field rather than trusting what the document
	// says about itself.
	Residual []error
	// RolledBack lists human-readable descriptions of content-modifying
	// fixes that were applied, failed the post-remediation integrity probe,
	// and were subsequently discarded. When non-empty, the output preserves
	// rendering at the cost of leaving the listed violations unfixed.
	RolledBack []string
}

// ResidualViolations returns the residual findings as structured
// [Violation] values with clause, test number, context, and severity.
// Residual keeps the raw error form; this accessor is the structured view
// for callers that route, filter, or report findings programmatically.
func (r *RemediationReport) ResidualViolations() []Violation {
	return toViolations(r.Residual)
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
	// container labeled by [BookmarkOptions.SourceLabels] (Merge only).
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
		// Wrap with the public sentinel. ErrInvalidFont is documented as
		// covering data that is "too short or structurally invalid", but only
		// the length check above ever returned it: every parse failure came
		// back as a bare string or as the internal truetype sentinel, so a
		// caller could not match a malformed font with errors.Is. Measured
		// before the fix, four of five failure shapes were unmatchable.
		//
		// %v rather than %w, matching the boundary style in reader.go: it keeps
		// the pkg/internal error value out of the public chain while still
		// naming the cause in the message.
		return StampFont{}, fmt.Errorf("%w: %v", ErrInvalidFont, err)
	}
	// Copy to detach from caller-owned memory.
	cp := make([]byte, len(data))
	copy(cp, data)
	return StampFont{ttf: cp}, nil
}

// ImageStamp defines an image overlay. Data must be JPEG or PNG bytes, and
// is retained rather than copied: the stamp is drawn when
// [Writer.WriteTo] drains the queue, so leave the slice untouched until
// then.
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

// Writer queues modifications against an open [Reader] and emits the
// result through [Writer.WriteTo]. Usage is:
//
//  1. Obtain a Reader with [Open] or [OpenFile].
//  2. Create a Writer with [NewWriter], and defer [Writer.Close].
//  3. Call any number of mutation methods (SetMetadata, RotatePages,
//     Encrypt, Sign, RemediatePDFA, ...). Mutations are queued; nothing
//     is written yet.
//  4. Call WriteTo exactly once. It drains the queue, produces the
//     output, and may mutate the underlying Reader's in-memory state.
//
// A remediation ([Writer.RemediatePDFA] and its siblings) writes its output
// to a temp file in [os.TempDir] so it can revalidate it. WriteTo streams
// that file to the destination and removes it, and a remediation that fails
// or is cancelled removes it itself. A Writer that is remediated and never
// written keeps the file until [Writer.Close].
//
// A Writer is single-use: create a new Writer (and, if you need a
// pristine baseline, a new Reader) for each distinct output. Method
// calls must be serialised; no Writer method is safe for concurrent
// use, Close included. The Reader the Writer was built from must not be
// consumed by any other goroutine between NewWriter and WriteTo.
type Writer struct {
	reader       *Reader
	mutations    []mutation
	encryptCfg   *crypto.EncryptConfig
	decrypt      bool
	forceRewrite bool
	trailerDirty bool
	// consumed is set once WriteTo produces output successfully. A second
	// WriteTo on the same Writer returns ErrWriterConsumed rather than
	// re-applying the already-drained mutation state.
	consumed bool
	// closed is set by Close. WriteTo, Redact and remediation refuse a
	// closed Writer with ErrWriterClosed.
	closed bool
	// tempDir is the directory the Writer's temp files go in, "" for
	// os.TempDir. A Pipeline sets it from WithPipelineTempDir.
	tempDir string
	// rewriteOverrides holds per-object replacements produced during
	// remediation and redaction, keyed by source object number. They are
	// forwarded to the full-rewrite copier so the renumbered output reflects
	// the patched data instead of the original. Each stage that produces
	// them starts from the ones already here, so a later stage builds on an
	// earlier one instead of discarding it.
	rewriteOverrides map[int]ast.PdfObject
	signer           Signer
	requiredVersion  string

	// redactions records the regions of every successful [Writer.Redact]
	// call, in call order, and redactedTokens the text they removed. WriteTo
	// re-checks the fully composed output against them, because a stage
	// queued before or after Redact can rebuild content from the source
	// document and restore what Redact removed.
	redactions     [][]RedactRegion
	redactedTokens []string

	// preSerializedPath is the path to a temp file holding the fully
	// serialized PDF when remediation pre-serializes for accurate
	// residual validation. When set, WriteTo streams from this file
	// instead of re-serializing, then removes it. Close removes it too,
	// and so does a later remediation, whose output supersedes it. The
	// snapshot fields below record the Writer state at pre-serialization
	// time: any edit
	// queued afterwards (a new mutation, Sign, Encrypt, decrypt) or a
	// serialization-shaping write option invalidates the fast path, and
	// WriteTo re-serializes so the late edit is not silently dropped.
	preSerializedPath       string
	preSerializedMutations  int
	preSerializedRedactions int
	preSerializedEncrypt    bool
	preSerializedDecrypt    bool
}

// mutation is a deferred modification applied during WriteTo.
type mutation struct {
	apply func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error
}

// NewWriter creates a single-use [Writer] that queues mutations
// against r. Release the Writer with [Writer.Close] and close r with
// [Reader.Close] after the Writer has finished; do not issue further
// reads through r while the Writer is active.
func NewWriter(r *Reader) *Writer {
	return &Writer{reader: r}
}

// Close removes the temp file a remediation left for [Writer.WriteTo] to
// stream, when WriteTo has not consumed it. It does not close the Writer's
// [Reader].
//
// Close is idempotent and safe after WriteTo, which already removed the file,
// so defer it right after [NewWriter]. After Close, WriteTo, [Writer.Redact]
// and the remediation methods return [ErrWriterClosed]. Like every Writer
// method, Close must not run concurrently with another call on the same
// Writer.
//
// The error reports a temp file that could not be removed. The Writer is
// closed either way.
func (w *Writer) Close() error {
	w.closed = true
	return w.releasePreSerialized()
}

// releasePreSerialized removes the remediation output WriteTo would stream,
// if any, and forgets it, so WriteTo re-serializes the queued state instead.
// A file already gone is not an error.
func (w *Writer) releasePreSerialized() error {
	path := w.preSerializedPath
	if path == "" {
		return nil
	}
	w.preSerializedPath = ""
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing remediation temp file: %w", err)
	}
	return nil
}

// createTemp creates a temp file named after pattern in the Writer's temp
// directory ([os.TempDir] unless a [Pipeline] set one).
func (w *Writer) createTemp(pattern string) (*os.File, error) {
	return os.CreateTemp(w.tempDir, pattern)
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
	// A document that carries XMP duplicates these values there, and leaving
	// the packet alone left it asserting the old title: a conformance defect
	// wherever the two are required to agree (ISO 19005-2/3, 6.6.3), and
	// misleading everywhere else, since most readers prefer XMP. Queued as a
	// second mutation rather than folded into the first because the
	// synchronisation lives in the remediation package, which imports
	// mutations and so cannot be imported by it.
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			// The fix list is for a remediation report; a plain metadata edit
			// has nowhere to show it.
			_, err := remediation.SyncXMPWithDocInfo(ctx, alloc, doc)
			return err
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
// If no pages are specified, every page is rotated. Rotation must be a
// multiple of 90 (0, 90, 180, 270). The mutation is queued and applied when
// [Writer.WriteTo] is called.
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
//
// Returns [ErrEmptyPageList] when no pages are given. This is the only page
// operation that refuses an empty list: [Writer.RotatePages],
// [Writer.InsertPages] and the three stamps all document an empty list as
// every page, which deletion cannot mean because it would leave no document.
//
// [Writer.WriteTo] reports [ErrPageDeletionIncomplete] when the document's
// structure tree nests deeper than the page prune can walk. Elements below that
// bound keep the deleted page alive through their /Pg, so the output would
// carry the page's content while reporting one fewer page. Nothing is written
// in that case.
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
			if err := mutations.DeletePages(ctx, alloc, doc, resolvedPages, pages); err != nil {
				// Translate at the boundary so a caller can match the refusal
				// with errors.Is without importing pkg/internal.
				if errors.Is(err, mutations.ErrPagePruneIncomplete) {
					return fmt.Errorf("%w: %w", ErrPageDeletionIncomplete, err)
				}
				return err
			}
			return nil
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

// AutoTag synthesizes a logical structure tree (a tagged PDF) for an untagged
// document: it classifies each page's text into headings and paragraphs, marks
// the content (tagging text, artifacting the rest), and wires the catalog
// /StructTreeRoot and /MarkInfo. It is a no-op on an already-tagged document.
// The change is applied when [Writer.WriteTo] is called.
//
// AutoTag produces the structure and marked content only; it does not set the
// document language, title, or the PDF/UA identifier, which other operations
// own. Pair it with [Writer.RemediatePDFUA], or set those explicitly, for full
// PDF/UA conformance.
func (w *Writer) AutoTag() {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			descs, err := tagging.AutoTag(ctx, alloc, doc)
			if err != nil {
				return fmt.Errorf("auto-tagging: %w", err)
			}
			for _, d := range descs {
				doc.Logger.Debug("auto-tag", slog.String("detail", d))
			}
			return nil
		},
	})
}

// EncryptAlgorithm identifies a PDF standard-security-handler algorithm.
// Values cover the ISO 32000 V=1..5 security handlers. New code should
// prefer [EncryptAES256]; the RC4 variants are provided for
// legacy-compatibility writes only.
// EncryptAlgorithm values are immutable and safe for concurrent use.
type EncryptAlgorithm int

// Each algorithm carries a minimum PDF version, and the output declares it:
// a file selecting a handler its declared version does not define misdescribes
// itself. RC4 needs nothing beyond PDF 1.4, AES-128 raises the output to 1.6,
// and AES-256 raises it to 1.7 and records the Adobe extension that defines
// it. See [EncryptAES256] for why 1.7 rather than 2.0.
const (
	// EncryptRC4_40 uses 40-bit RC4 (V=1, R=2), a PDF 1.1 handler. Weak.
	// Legacy only.
	EncryptRC4_40 EncryptAlgorithm = iota
	// EncryptRC4_128 uses 128-bit RC4 (V=2, R=3), a PDF 1.4 handler. Weak.
	// Legacy only.
	EncryptRC4_128
	// EncryptAES128 uses 128-bit AES-CBC (V=4, R=4) through the AESV2 crypt
	// filter, which ISO 32000-1, Table 25 introduces in PDF 1.6. The output
	// declares at least that version.
	EncryptAES128
	// EncryptAES256 uses 256-bit AES-CBC (V=5, R=6) through the AESV3 crypt
	// filter. Recommended.
	//
	// ISO 32000-2 defines this handler, but the output declares PDF 1.7 plus
	// an /Extensions entry naming Adobe extension level 3 rather than PDF
	// 2.0. AES-256 is an extension to 1.7, not a feature of it, and declaring
	// 2.0 would overstate a document whose body is unchanged. This matches
	// what other producers write: qpdf emits the same 1.7-plus-extension
	// pair.
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
	// value from [EncryptConfig.UserPassword]; callers that supply identical
	// passwords get the same effective permissions as an
	// empty-user-password doc.
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

// StampText overlays text (a watermark or stamp) on the specified
// 1-based pages. If no pages are specified, the stamp is applied to all
// pages. A diagonal, semi-transparent watermark is a [TextStamp] with a
// Rotation and an Opacity below 1. Uses incremental update. The mutation
// is queued and applied when [Writer.WriteTo] is called.
//
// The stamp is marked as an /Artifact, which ISO 32000-1, 14.8.2.2 names a
// watermark to be and which keeps a tagged document conformant with
// ISO 14289-1, 7.1. One consequence is visible to callers:
// [Reader.ExtractText] omits the stamped text unless [WithArtifacts] is
// passed, the same way it treats any other artifact.
//
// The default font is a standard font and is NOT embedded, which
// ISO 14289-1, 7.21.4.1 requires of every font used for rendering. A stamp
// that has to survive PDF/UA validation needs a TTF supplied through
// [TextStamp.Font]: litho embeds the whole font program with a matching
// ToUnicode CMap. Measured on an autotagged page, stamping with the default
// font adds a 7.21.4.1 violation naming the stamp font, and supplying a TTF
// removes it.
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
//
// The stamp is marked as an /Artifact, so it does not cost a tagged document
// its ISO 14289-1, 7.1 conformance.
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
//
// The stamp is marked as an /Artifact, so it does not cost a tagged document
// its ISO 14289-1, 7.1 conformance, and any text it carries is omitted from
// [Reader.ExtractText] unless [WithArtifacts] is passed.
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

// AddAttachment embeds a file in the document's /EmbeddedFiles name
// tree. This is the generic attachment API suitable for most use
// cases. The mutation is queued and applied when [Writer.WriteTo] is
// called.
//
// For PDF/A-3, Factur-X, or ZUGFeRD compliance where an embedded file
// must carry a /AFRelationship and appear in the catalog /AF array,
// use [Writer.AddAssociatedFile] instead.
//
// data is retained, not copied: the attachment is written when
// [Writer.WriteTo] drains the queue, so the caller must leave the slice
// untouched until then. Reusing the buffer in between puts the later
// contents in the file.
// [Writer.WriteTo] reports [ErrAttachmentTreeIncomplete] when the existing
// /EmbeddedFiles tree nests deeper than the attachment walk reads: the tree is
// rebuilt from that walk, so adding one attachment would drop the others.
// Nothing is written in that case.
func (w *Writer) AddAttachment(name string, data []byte, mimeType string) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			if err := mutations.AddAttachment(ctx, alloc, doc, name, data, mimeType); err != nil {
				// Translate at the boundary so a caller can match with
				// errors.Is without importing pkg/internal.
				if errors.Is(err, mutations.ErrAttachmentTreeIncomplete) {
					return fmt.Errorf("%w: %w", ErrAttachmentTreeIncomplete, err)
				}
				return err
			}
			return nil
		},
	})
}

// AssociatedFile describes an embedded file for PDF/A-3+ compliance with the
// /AFRelationship and document-level /AF array required by Factur-X / ZUGFeRD.
// Values must not be shared across goroutines without synchronization.
type AssociatedFile struct {
	// Name is the displayed file name.
	Name string
	// Data is the file content. It is retained, not copied: the file is
	// written when [Writer.WriteTo] drains the queue, so leave the slice
	// untouched until then.
	Data []byte
	// MimeType is the embedded file /Subtype (e.g., "application/xml").
	MimeType string
	// Relationship is the /AFRelationship value. Valid per
	// ISO 32000-2 §7.11.3: Source, Data, Alternative, Supplement,
	// EncryptedPayload, FormData, Schema, Unspecified.
	Relationship string
}

// AddAssociatedFile embeds a file as a PDF/A-3+ associated file: sets
// /Subtype and /AFRelationship on the embedded-file stream and adds the
// filespec to the catalog /AF array. Use this for Factur-X and
// ZUGFeRD compliance.
//
// For generic attachments with no compliance requirement, prefer
// [Writer.AddAttachment].
// [Writer.WriteTo] reports [ErrAttachmentTreeIncomplete] on a tree too deep to
// read, for the reason given on [Writer.AddAttachment].
func (w *Writer) AddAssociatedFile(af AssociatedFile) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			err := mutations.AddAttachmentWithOptions(ctx, alloc, doc, af.Name, af.Data, mutations.AttachmentOptions{
				MimeType:       af.MimeType,
				AFRelationship: af.Relationship,
				AddToCatalogAF: true,
			})
			if err != nil {
				if errors.Is(err, mutations.ErrAttachmentTreeIncomplete) {
					return fmt.Errorf("%w: %w", ErrAttachmentTreeIncomplete, err)
				}
				return err
			}
			return nil
		},
	})
}

// RemoveAttachment removes a file from the document's /EmbeddedFiles name
// tree by its filename. Uses incremental update. The mutation is queued and
// applied when [Writer.WriteTo] is called.
// [Writer.WriteTo] reports [ErrAttachmentTreeIncomplete] when the tree nests
// deeper than the attachment walk reads. That truncation has two silent
// outcomes, which is why it refuses rather than proceeding: the rebuilt tree
// loses every entry past the bound, and a target sitting past it matches
// nothing, so the removal would report success having removed nothing.
func (w *Writer) RemoveAttachment(name string) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			if err := mutations.RemoveAttachment(ctx, alloc, doc, name); err != nil {
				if errors.Is(err, mutations.ErrAttachmentTreeIncomplete) {
					return fmt.Errorf("%w: %w", ErrAttachmentTreeIncomplete, err)
				}
				return err
			}
			return nil
		},
	})
}

// Sign applies a digital signature using the given signer. Signing always uses
// incremental update (required for ByteRange integrity).
//
// Sign registers the signer rather than queueing a mutation, so it may be
// called at any point: [Writer.WriteTo] applies every queued mutation before
// computing the digest, and the signature covers the result either way. What it
// is not compatible with is an operation forcing a full rewrite, which
// [Writer.WriteTo] rejects with [ErrSignFullRewriteIncompatible].
func (w *Writer) Sign(signer Signer) {
	w.signer = signer
}

// remediateConfig applies the remediation options and returns the
// resolved settings plus the internal options the RemediatePDFA and
// RemediatePDFUA entry points share.
func remediateConfig(opts []RemediateOption) (remediateOpts, remediation.Options) {
	cfg := remediateOpts{lang: "en", preserveContent: true, tolerance: DefaultRegressionTolerance}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg, remediation.Options{Lang: cfg.lang, PreserveContent: cfg.preserveContent, FigureAlt: cfg.figureAlt, PinnedID: cfg.documentID}
}

// RemediatePDFA validates the document against the given PDF/A profile,
// applies targeted fixes, and queues the result for writing. The report
// describes what was fixed and what could not be fixed automatically.
// Uses incremental update to preserve existing document structure.
func (w *Writer) RemediatePDFA(ctx context.Context, profile models.PDFAProfile, opts ...RemediateOption) (_ *RemediationReport, err error) {
	defer recoverToError(w.logger(), "Writer.RemediatePDFA", &err)
	cfg, remOpts := remediateConfig(opts)
	revalidate := func(ctx context.Context, r *Reader) ([]error, error) {
		return r.ValidatePDFA(ctx, profile)
	}
	return w.remediate(ctx, cfg.tolerance, revalidate, func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
		return remediation.RemediatePDFA(ctx, doc, alloc, profile, remOpts.WithDisabled(disabled))
	})
}

// RemediatePDFUA validates the document against the given PDF/UA profile,
// applies targeted fixes, and queues the result for writing. The report
// describes what was fixed and what could not be fixed automatically; see
// [RemediationReport.Residual], which the output's own pdfuaid claim does not
// account for.
func (w *Writer) RemediatePDFUA(ctx context.Context, profile models.PDFUAProfile, opts ...RemediateOption) (_ *RemediationReport, err error) {
	defer recoverToError(w.logger(), "Writer.RemediatePDFUA", &err)
	cfg, remOpts := remediateConfig(opts)
	revalidate := func(ctx context.Context, r *Reader) ([]error, error) {
		return r.ValidatePDFUA(ctx, profile)
	}
	return w.remediate(ctx, cfg.tolerance, revalidate, func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
		return remediation.RemediatePDFUA(ctx, doc, alloc, profile, remOpts.WithDisabled(disabled))
	})
}

// RemediateWTPDF applies the Well-Tagged PDF fixer set for the given
// conformance level and returns a report of what was fixed and what
// remains. WTPDF adopts the ISO 14289-2 requirements, so the fixes are the
// PDF/UA-2 ones, plus the PDF Declaration WTPDF states of its own.
//
// Violations the level does not state are never reported: the run is judged
// by [Reader.ValidateWTPDF], which applies the level's rule subtractions.
func (w *Writer) RemediateWTPDF(ctx context.Context, profile models.WTPDFProfile, opts ...RemediateOption) (_ *RemediationReport, err error) {
	defer recoverToError(w.logger(), "Writer.RemediateWTPDF", &err)
	cfg, remOpts := remediateConfig(opts)
	revalidate := func(ctx context.Context, r *Reader) ([]error, error) {
		return r.ValidateWTPDF(ctx, profile)
	}
	return w.remediate(ctx, cfg.tolerance, revalidate, func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
		return remediation.RemediateWTPDF(ctx, doc, alloc, profile, remOpts.WithDisabled(disabled))
	})
}

// FacturX defaults applied by [Writer.RemediateFacturX] when the
// corresponding [FacturXConfig] field is zero.
const (
	// DefaultFacturXProfile is the PDF/A-3 conformance level used when
	// [FacturXConfig.Profile] is empty.
	DefaultFacturXProfile = models.PDFA3b
	// DefaultFacturXFileName is the embedded file name used when
	// [FacturXConfig.FileName] is empty. Matches the Factur-X 1.0.07
	// specification.
	DefaultFacturXFileName = "factur-x.xml"
	// DefaultFacturXDocumentType is the value written into the XMP
	// `fx:DocumentType` property when [FacturXConfig.DocumentType] is
	// empty.
	DefaultFacturXDocumentType = "INVOICE"
	// DefaultFacturXRelationship is the `/AFRelationship` value used
	// when [FacturXConfig.Relationship] is empty. Factur-X mandates
	// "Data"; legacy ZUGFeRD 2.0 producers used "Alternative".
	DefaultFacturXRelationship = "Data"
)

// FacturXConfig describes the inputs to [Writer.RemediateFacturX].
// Values must not be shared across goroutines without synchronization.
type FacturXConfig struct {
	// InvoiceXML is the Cross-Industry Invoice (CII) XML payload that
	// will be embedded as the Factur-X data file. Required, non-empty.
	InvoiceXML []byte
	// Level is the Factur-X conformance level. Required; must satisfy
	// [models.FacturXLevel.IsValid].
	Level models.FacturXLevel
	// Profile is the PDF/A-3 conformance level used as the underlying
	// archival base. Optional; defaults to [DefaultFacturXProfile].
	// Must be one of [models.PDFA3a], [models.PDFA3b], or
	// [models.PDFA3u].
	Profile models.PDFAProfile
	// FileName is the embedded file name. Optional; defaults to
	// [DefaultFacturXFileName]. The same value is written into the XMP
	// `fx:DocumentFileName` property.
	FileName string
	// DocumentType is the value of the XMP `fx:DocumentType` property.
	// Optional; defaults to [DefaultFacturXDocumentType].
	DocumentType string
	// Relationship is the embedded-file `/AFRelationship` value.
	// Optional; defaults to [DefaultFacturXRelationship].
	Relationship string
}

// RemediateFacturX embeds the supplied invoice XML as a Factur-X
// associated file, remediates the document against the configured
// PDF/A-3 profile, and injects the four `fx:` XMP properties plus the
// PDF/A extension schema declaration the format requires.
//
// The returned [RemediationReport] aggregates the underlying PDF/A-3
// fixes plus the Factur-X-specific operations.
//
// There is no RemediateZUGFeRD: the ZUGFeRD 2.1+ generation is
// byte-for-byte Factur-X, so use this method with the defaults. The
// legacy generations differ only in the embedded file name and
// relationship, which [FacturXConfig] can reproduce:
//
//   - ZUGFeRD 2.1+ / Factur-X: FileName "factur-x.xml", Relationship "Data" (the defaults).
//   - ZUGFeRD 2.0:             FileName "zugferd-invoice.xml", Relationship "Alternative".
//   - ZUGFeRD 1.0:             FileName "ZUGFeRD-invoice.xml", Relationship "Alternative".
//
// The legacy 1.0/2.0 generations also use an older XMP namespace and
// conformance vocabulary that this method does not emit, so the output
// is a PDF/A-3 carrying a legacy-named CII payload rather than a fully
// spec-valid legacy ZUGFeRD file. Producing genuine 1.0/2.0 output is
// out of scope.
func (w *Writer) RemediateFacturX(ctx context.Context, cfg FacturXConfig, opts ...RemediateOption) (_ *RemediationReport, err error) {
	defer recoverToError(w.logger(), "Writer.RemediateFacturX", &err)
	if len(cfg.InvoiceXML) == 0 {
		return nil, ErrEmptyInvoiceXML
	}
	if !cfg.Level.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidFacturXLevel, cfg.Level)
	}
	profile := cfg.Profile
	if profile == "" {
		profile = DefaultFacturXProfile
	}
	if profile != models.PDFA3a && profile != models.PDFA3b && profile != models.PDFA3u {
		return nil, fmt.Errorf("%w: got %q", ErrInvalidFacturXProfile, profile)
	}
	fileName := cfg.FileName
	if fileName == "" {
		fileName = DefaultFacturXFileName
	}
	docType := cfg.DocumentType
	if docType == "" {
		docType = DefaultFacturXDocumentType
	}
	relationship := cfg.Relationship
	if relationship == "" {
		relationship = DefaultFacturXRelationship
	}

	rcfg := remediateOpts{lang: "en", preserveContent: true, tolerance: DefaultRegressionTolerance}
	for _, o := range opts {
		o(&rcfg)
	}
	remOpts := remediation.Options{Lang: rcfg.lang, PreserveContent: rcfg.preserveContent, FigureAlt: rcfg.figureAlt, PinnedID: rcfg.documentID}
	revalidate := func(ctx context.Context, r *Reader) ([]error, error) {
		return r.ValidatePDFA(ctx, profile)
	}
	fxOpts := remediation.FacturXOptions{
		InvoiceXML:   cfg.InvoiceXML,
		Level:        cfg.Level,
		Profile:      profile,
		FileName:     fileName,
		DocumentType: docType,
		Relationship: relationship,
	}
	return w.remediate(ctx, rcfg.tolerance, revalidate, func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
		return remediation.RemediateFacturX(ctx, doc, alloc, fxOpts, remOpts.WithDisabled(disabled))
	})
}

// RepairFacturX repairs the PDF-container metadata of a received hybrid
// invoice (Factur-X or ZUGFeRD): catalog /AF membership, the invoice
// filespec /AFRelationship and /F + /UF pair, and the embedded stream
// /Subtype. The invoice XML payload is business data and is never
// modified; XSD or Schematron findings inside it remain in the residual
// list. Returns [ErrNoInvoiceAttachment] when the document carries no
// known invoice attachment. Use [Writer.RemediateFacturX] to embed a new
// invoice instead.
func (w *Writer) RepairFacturX(ctx context.Context, opts ...RemediateOption) (_ *RemediationReport, err error) {
	defer recoverToError(w.logger(), "Writer.RepairFacturX", &err)
	rcfg := remediateOpts{lang: "en", preserveContent: true, tolerance: DefaultRegressionTolerance}
	for _, o := range opts {
		o(&rcfg)
	}
	revalidate := func(ctx context.Context, r *Reader) ([]error, error) {
		return r.ValidateFacturX(ctx)
	}
	located := false
	// The repair runs no content-tier fixer, so there is none to disable.
	report, err := w.remediate(ctx, rcfg.tolerance, revalidate, func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ []string) (*remediation.Report, error) {
		fixes, repairErr := remediation.RepairHybridInvoice(ctx, doc, alloc)
		if repairErr != nil {
			return nil, repairErr
		}
		located = located || fixes != nil
		return &remediation.Report{Fixed: fixes}, nil
	})
	if err == nil && !located {
		// A refused repair hands back no report, so the output it wrote for
		// WriteTo goes with it: nothing was repaired in it.
		if releaseErr := w.releasePreSerialized(); releaseErr != nil {
			w.reader.doc.Logger.Debug("repair: removing the remediation output failed", slog.Any("err", releaseErr))
		}
		return nil, ErrNoInvoiceAttachment
	}
	return report, err
}

// integritySnap captures the signals the post-remediation probe compares
// against the baseline measured on the source [Reader].
type integritySnap struct {
	pageCount  int
	textLen    int
	imageCount int
}

// probeIntegrity samples the observable surface area of a document: its
// page count, the total length of extractable text across every page, and
// the total number of image references. Per-page extraction failures
// (missing fonts, OCR-only pages, decode errors) count as zero rather than
// aborting the probe. The baseline may suffer the same failures, and the
// comparison only needs relative movement between baseline and post.
func probeIntegrity(ctx context.Context, r *Reader) (integritySnap, error) {
	snap := integritySnap{}
	pages, err := r.PageCount(ctx)
	if err != nil {
		return snap, fmt.Errorf("page count: %w", err)
	}
	snap.pageCount = pages
	for p := 1; p <= pages; p++ {
		if err := ctx.Err(); err != nil {
			return snap, err
		}
		text, extractErr := r.ExtractText(ctx, p)
		if extractErr == nil {
			snap.textLen += len(text)
		}
		images, imagesErr := r.ExtractImages(ctx, p)
		if imagesErr == nil {
			snap.imageCount += len(images)
		}
	}
	return snap, nil
}

// stagedBaseline samples the document the caller has staged rather than the
// source it started from.
//
// [probeIntegrity] reads the source Reader, so a pipeline that queued a delete
// or a split before remediation measured a page count remediation never had:
// `litho pipe f delete -pages 1 remediate -profile PDF/A-2b` reported
// "remediation broke the PDF beyond the configured regression tolerance: page
// count changed from 41 to 40" and rolled back a remediation that had done
// nothing wrong. An earlier stage's overrides are staged too: remediating a
// redacted document measured the text Redact had removed, and refused the
// remediation for "dropping" it. The extra serialization only happens when
// something is staged.
func (w *Writer) stagedBaseline(ctx context.Context) (integritySnap, error) {
	if len(w.mutations) == 0 && len(w.rewriteOverrides) == 0 {
		return probeIntegrity(ctx, w.reader)
	}

	logger := w.reader.doc.Logger
	f, err := w.createTemp("litho-staged-*.pdf")
	if err != nil {
		return integritySnap{}, fmt.Errorf("creating staged baseline file: %w", err)
	}
	path := f.Name()
	defer removeTempFile(logger, path)

	if writeErr := w.writeToInternal(ctx, f); writeErr != nil {
		if closeErr := f.Close(); closeErr != nil {
			logger.Debug("closing staged baseline file failed", slog.Any("err", closeErr))
		}
		return integritySnap{}, fmt.Errorf("serializing staged document: %w", writeErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		return integritySnap{}, fmt.Errorf("closing staged baseline file: %w", closeErr)
	}

	r, err := w.reader.reopenFile(ctx, path)
	if err != nil {
		return integritySnap{}, fmt.Errorf("reopening staged document: %w", err)
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			logger.Debug("closing staged baseline reader failed", slog.Any("err", closeErr))
		}
	}()
	return probeIntegrity(ctx, r)
}

// evaluateRegression returns a non-empty reason describing the first
// observed regression; an empty string means the post-remediation sample
// is within tolerance of baseline. Each field guards on a positive
// baseline so an unreadable source (zero baseline) does not spuriously
// flag a post that has legitimate content.
func evaluateRegression(baseline, post integritySnap, tol RegressionTolerance) string {
	if baseline.pageCount > 0 {
		if !tol.AllowPageLoss && post.pageCount != baseline.pageCount {
			return fmt.Sprintf("page count changed from %d to %d", baseline.pageCount, post.pageCount)
		}
		if tol.AllowPageLoss && post.pageCount > baseline.pageCount {
			return fmt.Sprintf("page count grew from %d to %d", baseline.pageCount, post.pageCount)
		}
	}
	if baseline.textLen > 0 {
		minText := int(float64(baseline.textLen) * (1.0 - tol.MaxTextDropRatio))
		if post.textLen < minText {
			return fmt.Sprintf("extracted text dropped from %d to %d bytes (min %d)", baseline.textLen, post.textLen, minText)
		}
	}
	if baseline.imageCount > 0 {
		minImg := int(float64(baseline.imageCount) * (1.0 - tol.MaxImageDropRatio))
		if post.imageCount < minImg {
			return fmt.Sprintf("image count dropped from %d to %d (min %d)", baseline.imageCount, post.imageCount, minImg)
		}
	}
	return ""
}

// remediationRun runs one remediation engine against doc, queuing its repairs
// in alloc, with the fixers disabled names switched off (see
// [remediation.Options.Disabled]).
type remediationRun func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error)

// remediate runs one remediation engine against the document as the caller
// has staged it, then gates the result: it serializes and reopens the output,
// revalidates it, and compares its integrity with the staged baseline. A
// regression rolls back the content-tier fixers when they left a snapshot, and
// fails with [ErrRemediationBrokePDF], restoring the Writer, when there is none
// or the rollback does not recover the output. The result, rolled back or
// not, runs further rounds, up to maxRemediationRounds, while residuals remain
// and each round still fixes something (see [Writer.remediationRound]).
//
// The content-tier fixers a rollback discarded stay off in the later rounds,
// and the others run in them. The engine runs the content tier after every
// other fixer and snapshots the allocator between the two, so a rollback
// discards exactly the content tier's repairs: every other repair stays in the
// output, and Fixed lists it. The snapshot used to be taken at the first
// content-tier fixer in registry order, and a rollback also discarded the
// repairs of the structural and resource fixers registered after it, which
// Fixed still listed. The residual is the validation of the output kept last:
// a violation a rolled-back fixer claimed is in it when the output holds it.
// It used to be listed a second time from the claims of the rolled-back
// fixers.
//
// # Corpus evidence
//
//   - verapdf 6-1-7-1-t01-fail-a: carries a defect whose sole symptom is a
//     parser-level format violation. With the object cache still warm from
//     the baseline probe, remediation skipped it and over-reported the file as
//     fixed.
//   - pdfjs issue215.pdf and bug1749563.pdf: the font repairs of their
//     PDF/A-1b, PDF/A-2b and PDF/A-4 remediations, default and aggressive, are
//     rolled back, and so are those of the PDF/A-1b remediations of litho
//     glyphs_2b.pdf. The later rounds find nothing more to repair, and the
//     residual of bug1749563.pdf at PDF/A-1b goes from 60 violations to the 30
//     litho reports on the output. What litho and veraPDF 1.30 report on the
//     outputs, and their extracted text, do not change.
//   - All corpora: over the PDF/A-1b aggressive and PDF/A-2b default
//     remediations of the 2317 files under 3 MB of every corpus but verapdf
//     and facturx, only those three roll back, and no other PDF/A-1b report
//     changes.
func (w *Writer) remediate(
	ctx context.Context,
	tol RegressionTolerance,
	revalidate func(context.Context, *Reader) ([]error, error),
	run remediationRun,
) (*RemediationReport, error) {
	if w.closed {
		return nil, ErrWriterClosed
	}
	doc := w.reader.doc
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))

	// An earlier remediation's output is superseded by this run's. Kept, it
	// was also streamed in its place: WriteTo judges the file fresh by the
	// queued edits, which a remediation does not change, so when nothing was
	// staged for the baseline below to serialize, this run's probe and the
	// final write both streamed the earlier file and dropped this run's
	// repairs.
	if err := w.releasePreSerialized(); err != nil {
		doc.Logger.Debug("remediate: removing the previous remediation output failed", slog.Any("err", err))
	}

	baseline, baseErr := w.stagedBaseline(ctx)
	if baseErr != nil {
		// Treat an unreadable source as a zero baseline: the user asked
		// litho to fix a broken PDF, so refusing at the door defeats the
		// purpose. The integrity gates below skip their comparisons when
		// baseline fields are zero, leaving reopen + revalidate as the
		// only post-checks.
		doc.Logger.Debug("remediate: baseline probe failed, regression gates disabled",
			slog.Any("err", baseErr))
		baseline = integritySnap{}
	}

	// Remember pre-remediate Writer state so a hard-fail can leave the
	// source untouched instead of wedging the Writer in a half-mutated
	// state that later WriteTo calls would honor. The trailer snapshot
	// undoes in-place mutations made by fixers such as trailer-id
	// (assigning a fresh /ID) or enforceInfoDictLimits (deleting /Info),
	// which otherwise persist on the shared document even when
	// remediation returns ErrRemediationBrokePDF.
	preMutations := len(w.mutations)
	preForceRewrite := w.forceRewrite
	preTrailerDirty := w.trailerDirty
	preRequiredVersion := w.requiredVersion
	preRewriteOverrides := w.rewriteOverrides
	preTrailerSnap := make(ast.PdfDict, len(doc.Index.Trailer))
	maps.Copy(preTrailerSnap, doc.Index.Trailer)
	restorePreState := func() {
		w.mutations = w.mutations[:preMutations]
		w.forceRewrite = preForceRewrite
		w.trailerDirty = preTrailerDirty
		w.requiredVersion = preRequiredVersion
		w.rewriteOverrides = preRewriteOverrides
		for k := range doc.Index.Trailer {
			delete(doc.Index.Trailer, k)
		}
		maps.Copy(doc.Index.Trailer, preTrailerSnap)
	}
	// cancelled ends a run whose caller gave up: nothing it serialized will
	// be written, so its temp file goes now rather than at Close, and the
	// Writer is put back as it was.
	cancelled := func(tmpPath string, err error) (*RemediationReport, error) {
		removeTempFile(doc.Logger, tmpPath)
		restorePreState()
		return nil, fmt.Errorf("remediation canceled: %w", err)
	}

	// probeIntegrity warmed the object cache with compliance mode off, so the
	// parser recorded no format violations for those objects. The compliance
	// pass inside run() collects parser-level format violations (stream keyword
	// EOL §6.1.7, header spacing §6.1.8/§6.1.9, hex string §6.1.6) only when an
	// object is parsed fresh with the flag on; a warm cache hit re-collects
	// nothing. Clear the caches here so run()'s validation re-parses under
	// compliance mode and sees a defect whose sole symptom is a format
	// violation. Otherwise remediation silently skips it and over-reports the
	// file as fixed.
	doc.ClearCaches()

	// Apply whatever an earlier pipeline stage queued before the fixers run,
	// so they see the document as the caller has staged it. The fixers resolve
	// through resolveFromAllocOrDoc, so a page already in the allocator is the
	// one they read and rewrite.
	//
	// Without this, remediation built its overrides from the untouched source
	// and those overrides win at the same object number in writeToInternal, so
	// `litho pipe f rotate -angle 90 remediate -profile PDF/A-2b` wrote the file
	// with no rotation at all. The mutations stay queued: they are idempotent,
	// applying them again at write time reaches the same result, and a
	// remediation rollback must be able to put the Writer back as it was.
	//
	// The overrides an earlier stage produced go in first, for the same
	// reason: remediation replaced them wholesale, so remediating a redacted
	// document wrote the unredacted source back.
	seedAllocator(alloc, w.rewriteOverrides)
	for i, m := range w.mutations {
		if err := m.apply(ctx, alloc, doc); err != nil {
			restorePreState()
			return nil, fmt.Errorf("applying staged mutation %d before remediation: %w", i, err)
		}
	}

	report, err := run(ctx, alloc, doc, nil)
	if err != nil {
		// A fixer that ran before the failing one may have edited the
		// trailer in place.
		restorePreState()
		return nil, err
	}

	if len(alloc.Pending()) > 0 || report.NeedsFullRewrite || report.TrailerDirty {
		// Keep the allocator's pending map as the override source. WriteTo
		// seeds its write allocator from this map on the incremental path;
		// the full-rewrite path flows it through the copier via
		// FullRewriteConfig.Overrides. A previous implementation also
		// queued a mutation that did writeAlloc.AllocAt(srcObjNum, obj) for
		// every pending entry. That worked incrementally because source
		// and dst numbering are identical, but stomped the copier's
		// dst-namespace allocations during full rewrite and produced PDFs
		// whose trailer pointed at the wrong object. Seed via
		// rewriteOverrides only.
		w.rewriteOverrides = alloc.Pending()
	}
	if report.TrailerDirty {
		w.trailerDirty = true
	}

	if report.NeedsFullRewrite {
		w.forceRewrite = true
	}
	if report.RequiredVersion != "" {
		w.requiredVersion = report.RequiredVersion
	}

	// attemptResult distinguishes a failed serialize/reopen (treated as
	// advisory, falling back to preliminary residuals) from a serialized
	// output that reopens but then fails probing or validation (treated as
	// a regression candidate, eligible for rollback).
	type attemptResult int
	const (
		attemptOK attemptResult = iota
		attemptSetupFailed
		attemptBroken
	)

	// serializeAndProbe writes the currently-queued mutations to a fresh
	// temp file, reopens it, and samples both the profile residuals and an
	// integrity snapshot. The caller must os.Remove the returned tmpPath on
	// every branch except the happy path, where tmpPath is kept as
	// w.preSerializedPath.
	serializeAndProbe := func() (tmpPath string, residuals []error, post integritySnap, outcome attemptResult) {
		tmpFile, tmpErr := w.createTemp("litho-remediate-*.pdf")
		if tmpErr != nil {
			doc.Logger.Debug("remediate: creating temp file failed", slog.Any("err", tmpErr))
			return "", nil, integritySnap{}, attemptSetupFailed
		}
		tmpPath = tmpFile.Name()
		// Use writeToInternal: this is a probe write to a temp file, not
		// the caller's final output, so it must not trip the single-use
		// guard nor the sign/full-rewrite check (remediation may set
		// forceRewrite legitimately).
		if writeErr := w.writeToInternal(ctx, tmpFile); writeErr != nil {
			tmpFile.Close()
			doc.Logger.Debug("remediate: pre-serialization failed, treating output as broken",
				slog.Any("err", writeErr))
			return tmpPath, nil, integritySnap{}, attemptBroken
		}
		tmpFile.Close()

		r2, openErr := w.reader.reopenFile(ctx, tmpPath)
		if openErr != nil {
			doc.Logger.Debug("remediate: re-open failed", slog.Any("err", openErr))
			return tmpPath, nil, integritySnap{}, attemptBroken
		}
		defer r2.Close()

		probe, probeErr := probeIntegrity(ctx, r2)
		if probeErr != nil {
			doc.Logger.Debug("remediate: post probe failed", slog.Any("err", probeErr))
			return tmpPath, nil, integritySnap{}, attemptBroken
		}
		res, valErr := revalidate(ctx, r2)
		if valErr != nil {
			doc.Logger.Debug("remediate: re-validation failed", slog.Any("err", valErr))
			return tmpPath, nil, integritySnap{}, attemptBroken
		}
		return tmpPath, res, probe, attemptOK
	}

	tmpPath, residual, post, outcome := serializeAndProbe()
	if err := ctx.Err(); err != nil {
		return cancelled(tmpPath, err)
	}

	// Treat a broken output and a regression identically: both trigger
	// rollback when a content-tier snapshot exists, and hard-fail
	// otherwise. A pure setup failure (no bytes written, no reopen
	// attempted) preserves the older best-effort behavior.
	var trigger string
	switch outcome {
	case attemptSetupFailed:
		return &RemediationReport{Fixed: report.Fixed, Residual: report.Residual}, nil
	case attemptBroken:
		trigger = "output failed to reopen or revalidate"
	case attemptOK:
		trigger = evaluateRegression(baseline, post, tol)
	}

	fixed := report.Fixed
	// rolledBack lists the fixes discarded by a rollback, and disabled the
	// content-tier fixers whose repairs were, which the later rounds skip.
	var rolledBack, disabled []string
	if trigger != "" {
		doc.Logger.Debug("remediate: regression detected",
			slog.String("reason", trigger),
			slog.Bool("rollback_eligible", report.ContentSnapshot != nil),
		)
		removeTempFile(doc.Logger, tmpPath)
		if report.ContentSnapshot == nil {
			restorePreState()
			return nil, fmt.Errorf("%w: %s", ErrRemediationBrokePDF, trigger)
		}

		// Roll back content-tier fixers only. The allocator mutates its
		// pending map in place, so the closure captured above still sees
		// a coherent view after Restore.
		alloc.Restore(*report.ContentSnapshot)

		tmpPath2, residual2, post2, outcome2 := serializeAndProbe()
		if err := ctx.Err(); err != nil {
			return cancelled(tmpPath2, err)
		}
		if outcome2 == attemptSetupFailed {
			restorePreState()
			return nil, fmt.Errorf("%w: rollback serialize failed", ErrRemediationBrokePDF)
		}
		if outcome2 == attemptBroken {
			removeTempFile(doc.Logger, tmpPath2)
			restorePreState()
			return nil, fmt.Errorf("%w: output still unusable after rollback", ErrRemediationBrokePDF)
		}
		if reason2 := evaluateRegression(baseline, post2, tol); reason2 != "" {
			removeTempFile(doc.Logger, tmpPath2)
			restorePreState()
			return nil, fmt.Errorf("%w: %s (persisted after rollback)", ErrRemediationBrokePDF, reason2)
		}

		// Rollback succeeded. Content-tier fixes are discarded, and the
		// validation of the rolled-back output reports the violations they
		// claimed to handle.
		tmpPath, residual = tmpPath2, residual2
		fixed = keepFixedWithout(report.Fixed, report.ContentFixed)
		rolledBack = report.ContentFixed
		disabled = report.ContentFixers
		doc.Logger.Debug("remediate: rollback succeeded",
			slog.Int("kept_fixes", len(fixed)),
			slog.Int("rolled_back", len(report.ContentFixed)),
			slog.Any("disabled", disabled),
			slog.String("trigger", trigger),
		)
	}

	// Fix to fixpoint: a fixer can repair state that only becomes fixable
	// once an earlier round's output is serialized (a namespace rebind
	// exposing a now-reachable property, a structural fix unblocking a
	// metadata one). Each extra round runs on the previous round's file and is kept only
	// when it fixed something and survives the same probe-and-revalidate
	// gates; otherwise the previous round's output stands.
	for round := 2; round <= maxRemediationRounds && len(residual) > 0; round++ {
		next, ok := w.remediationRound(ctx, tmpPath, run, disabled, revalidate, baseline, tol)
		if !ok {
			break
		}
		doc.Logger.Debug("remediate: extra round kept",
			slog.Int("round", round),
			slog.Int("fixes", len(next.fixed)),
			slog.Int("rolled_back", len(next.rolledBack)),
			slog.Int("residual", len(next.residual)))
		removeTempFile(doc.Logger, tmpPath)
		tmpPath, residual = next.path, next.residual
		fixed = append(fixed, next.fixed...)
		rolledBack = append(rolledBack, next.rolledBack...)
		disabled = append(disabled, next.disabled...)
	}
	// A cancellation inside a round ends the loop as a round that fixed
	// nothing does, so it is told apart here, before the output is kept.
	if err := ctx.Err(); err != nil {
		return cancelled(tmpPath, err)
	}
	w.markPreSerialized(tmpPath)
	return &RemediationReport{Fixed: dedupeFixes(fixed), Residual: residual, RolledBack: dedupeFixes(rolledBack)}, nil
}

// countRepairs counts the fixes that actually repaired something, ignoring the
// full-rewrite note, which every round re-emits while the formatting violation
// it claims remains unfixable.
func countRepairs(fixes []string) int {
	n := 0
	for _, fix := range fixes {
		if fix != remediation.FullRewriteNote {
			n++
		}
	}
	return n
}

// dedupeFixes drops repeated fix descriptions, keeping the first of each in
// order. A fixer that summarises its work in one sentence returns that same
// sentence in every round it runs.
func dedupeFixes(fixes []string) []string {
	if len(fixes) < 2 {
		return fixes
	}
	seen := make(map[string]bool, len(fixes))
	out := make([]string, 0, len(fixes))
	for _, fix := range fixes {
		if seen[fix] {
			continue
		}
		seen[fix] = true
		out = append(out, fix)
	}
	return out
}

// maxRemediationRounds bounds the fix-to-fixpoint loop, counting the main
// pass as round one. Anti-livelock guard, not spec-mandated: a fixer
// oscillation must terminate instead of looping, and two extra rounds cover
// the fixer-enables-fixer chains real documents exhibit.
//
// # Corpus evidence
//
//   - All corpora: two extra rounds resolve every fixer-enables-fixer chain
//     the corpora exhibit.
const maxRemediationRounds = 3

// roundResult is the output of a kept [Writer.remediationRound]: the temp
// file holding it, its residual validation findings, the fixes it applied,
// the fixes it rolled back, and the content-tier fixers whose repairs it
// rolled back.
type roundResult struct {
	path       string
	residual   []error
	fixed      []string
	rolledBack []string
	disabled   []string
}

// remediationRound reopens the serialized output at path and runs the
// remediation engine once more against it, with the fixers disabled names
// switched off. It returns ok=false (changing nothing) when the round fixes
// nothing, fails to serialize, or fails the integrity and regression gates.
//
// A round whose output regresses is rolled back as the first round is: the
// repairs of its content-tier fixers are discarded and those of the others
// kept, and the round is kept when what remains still fixes something and
// passes the gates, with those fixers named for the later rounds to disable.
func (w *Writer) remediationRound(
	ctx context.Context,
	path string,
	run remediationRun,
	disabled []string,
	revalidate func(context.Context, *Reader) ([]error, error),
	baseline integritySnap,
	tol RegressionTolerance,
) (roundResult, bool) {
	logger := w.reader.doc.Logger

	r2, err := w.reader.reopenFile(ctx, path)
	if err != nil {
		logger.Debug("remediate: round reopen failed", slog.Any("err", err))
		return roundResult{}, false
	}
	defer r2.Close()

	alloc := writer.NewObjAllocator(r2.doc.HighestObjectNumber(ctx))
	rep, err := run(ctx, alloc, r2.doc, disabled)
	if err != nil || rep == nil || countRepairs(rep.Fixed) == 0 {
		return roundResult{}, false
	}

	w2 := NewWriter(r2)
	w2.rewriteOverrides = alloc.Pending()
	w2.trailerDirty = rep.TrailerDirty
	w2.forceRewrite = rep.NeedsFullRewrite
	w2.requiredVersion = rep.RequiredVersion

	tmpPath, res, reason := w.writeRound(ctx, w2, revalidate, baseline, tol)
	if reason == "" {
		return roundResult{path: tmpPath, residual: res, fixed: rep.Fixed}, true
	}
	if rep.ContentSnapshot == nil {
		logger.Debug("remediate: round regressed, keeping previous output",
			slog.String("reason", reason))
		return roundResult{}, false
	}

	// The allocator restores its pending map in place, which w2 writes from.
	alloc.Restore(*rep.ContentSnapshot)
	kept := keepFixedWithout(rep.Fixed, rep.ContentFixed)
	if countRepairs(kept) == 0 {
		return roundResult{}, false
	}
	tmpPath, res, reason = w.writeRound(ctx, w2, revalidate, baseline, tol)
	if reason != "" {
		logger.Debug("remediate: round regressed after rollback, keeping previous output",
			slog.String("reason", reason))
		return roundResult{}, false
	}
	return roundResult{path: tmpPath, residual: res, fixed: kept, rolledBack: rep.ContentFixed, disabled: rep.ContentFixers}, true
}

// writeRound serializes the output of a remediation round w2 queues to a temp
// file, reopens it, and gates it as [Writer.remediate] gates the first round.
// It returns the temp file and its residual validation findings, or a reason
// the output was refused, after removing the file.
func (w *Writer) writeRound(
	ctx context.Context,
	w2 *Writer,
	revalidate func(context.Context, *Reader) ([]error, error),
	baseline integritySnap,
	tol RegressionTolerance,
) (string, []error, string) {
	logger := w.reader.doc.Logger

	tmpFile, err := w.createTemp("litho-remediate-round-*.pdf")
	if err != nil {
		logger.Debug("remediate: round temp file failed", slog.Any("err", err))
		return "", nil, "temp file failed"
	}
	tmpPath := tmpFile.Name()
	if writeErr := w2.writeToInternal(ctx, tmpFile); writeErr != nil {
		tmpFile.Close()
		removeTempFile(logger, tmpPath)
		logger.Debug("remediate: round serialize failed", slog.Any("err", writeErr))
		return "", nil, "serialize failed"
	}
	tmpFile.Close()

	r3, err := w.reader.reopenFile(ctx, tmpPath)
	if err != nil {
		removeTempFile(logger, tmpPath)
		logger.Debug("remediate: round reopen of output failed", slog.Any("err", err))
		return "", nil, "output failed to reopen"
	}
	defer r3.Close()

	post, err := probeIntegrity(ctx, r3)
	if err != nil {
		removeTempFile(logger, tmpPath)
		return "", nil, "output failed the integrity probe"
	}
	if reason := evaluateRegression(baseline, post, tol); reason != "" {
		removeTempFile(logger, tmpPath)
		return "", nil, reason
	}
	res, err := revalidate(ctx, r3)
	if err != nil {
		removeTempFile(logger, tmpPath)
		return "", nil, "output failed to revalidate"
	}
	return tmpPath, res, ""
}

// keepFixedWithout returns the subset of fixed that is not also present in
// rolledBack. The comparison is by exact description because the
// remediation engine emits stable, human-readable strings.
func keepFixedWithout(fixed, rolledBack []string) []string {
	if len(rolledBack) == 0 {
		return fixed
	}
	skip := make(map[string]int, len(rolledBack))
	for _, d := range rolledBack {
		skip[d]++
	}
	out := make([]string, 0, len(fixed))
	for _, d := range fixed {
		if skip[d] > 0 {
			skip[d]--
			continue
		}
		out = append(out, d)
	}
	return out
}

// removeTempFile deletes path and logs a debug line if the removal fails.
// Temp-file cleanup is best-effort: the OS reclaims the file on restart
// and callers cannot do anything useful with the error at this point.
func removeTempFile(logger *slog.Logger, path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil {
		logger.Debug("remediate: removing temp file failed",
			slog.String("path", path),
			slog.Any("err", err))
	}
}

// markPreSerialized records the remediation temp file plus the Writer
// state it captures, so a later WriteTo can detect post-remediation edits
// and re-serialize instead of streaming a stale file.
func (w *Writer) markPreSerialized(path string) {
	w.preSerializedPath = path
	w.preSerializedMutations = len(w.mutations)
	w.preSerializedRedactions = len(w.redactions)
	w.preSerializedEncrypt = w.encryptCfg != nil
	w.preSerializedDecrypt = w.decrypt
}

// copyFromPreSerialized streams the pre-serialized temp file to dst and
// removes it afterward, whether or not the copy succeeds: a failed write
// leaves the Writer to re-serialize, as a stale file does.
func (w *Writer) copyFromPreSerialized(ctx context.Context, dst io.Writer) error {
	defer func() {
		if releaseErr := w.releasePreSerialized(); releaseErr != nil {
			w.reader.doc.Logger.Debug("writer: removing the remediation output failed", slog.Any("err", releaseErr))
		}
	}()

	f, err := os.Open(w.preSerializedPath) //nolint:gosec // Path is a temp file created by remediate, not user input.
	if err != nil {
		return fmt.Errorf("opening pre-serialized output: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			w.reader.doc.Logger.Debug("writer: closing the remediation output failed", slog.Any("err", closeErr))
		}
	}()
	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("reading pre-serialized output size: %w", err)
	}
	return copySourceCancellable(ctx, dst, f, stat.Size())
}

// WriteTo serializes the modified document to dst. By default it
// emits an incremental update (appending a new xref and trailer to
// the original bytes). Pass [WithFullRewrite] to force a clean
// rewrite, or [WithWriteLogger] for structured logs covering every
// mutation, best-effort fallback, and write-engine decision.
//
// WriteTo drains the mutation queue and may alter the backing
// [Reader]'s in-memory state. It is single-use: after it produces
// output successfully it returns [ErrWriterConsumed] on every
// subsequent call. To produce another output, create a new Writer
// from a fresh Reader. After [Writer.Close] it returns [ErrWriterClosed].
//
// After a remediation, WriteTo streams the temp file the remediation wrote
// and removes it, whether the write succeeds or not. An edit queued after
// the remediation, or a write option that shapes the serialization, makes
// that file stale: WriteTo then removes it and serializes afresh.
//
// Signing ([Writer.Sign]) is incompatible with operations that force
// a full rewrite (encrypt, decrypt, delete/insert pages, flatten,
// clear metadata, or [WithFullRewrite]); combining them returns
// [ErrSignFullRewriteIncompatible] instead of silently dropping the
// requested operation.
//
// Signing an already-encrypted source returns the same error even when
// the caller asks for nothing else: the incremental signing flow would
// append the signature dictionary's strings in plaintext where the
// source's crypto handler requires ciphertext. Encrypted-then-signed
// output is permitted by the specification but not yet implemented.
//
// After [Writer.Redact], WriteTo checks the output against every removed
// string before writing a byte, and returns [ErrRedactionRefused] or
// [ErrRedactionIncomplete] rather than write a file that still holds one.
func (w *Writer) WriteTo(ctx context.Context, dst io.Writer, opts ...WriteOption) (err error) {
	defer recoverToError(w.logger(), "Writer.WriteTo", &err)
	if w.closed {
		return ErrWriterClosed
	}
	if w.consumed {
		return ErrWriterConsumed
	}
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}
	// Before the guard below, never after. The guard reads w.encryptCfg, so
	// applying WithEncryption later would let Sign + WithEncryption past it
	// and silently drop one of the two.
	if internal := cfg.internalEncrypt(); internal != nil {
		w.encryptCfg = internal
		w.forceRewrite = true
	}
	// Signing an encrypted source is also rejected: the incremental signing
	// flow would append the signature dictionary's strings in plaintext
	// where the source's crypto handler requires ciphertext.
	if w.signer != nil && (w.encryptCfg != nil || w.decrypt || w.forceRewrite || cfg.fullRewrite ||
		w.reader.doc.IsEncrypted) {
		return ErrSignFullRewriteIncompatible
	}
	if err := w.writeToInternal(ctx, dst, opts...); err != nil {
		return err
	}
	w.consumed = true
	return nil
}

// passThroughChunk bounds how much of a pass-through copy runs between
// cancellation checks. Not spec-mandated: it trades a negligible number of
// extra loop iterations for the guarantee that a caller who cancels stops
// within a megabyte rather than after the whole file.
const passThroughChunk = 1 << 20

// copySourceCancellable streams the source to dst, honouring ctx.
//
// A write with nothing queued used to io.Copy the whole file in one call, which
// no cancellation could interrupt: every other path through writeToInternal
// stops on a cancelled context, so a caller could not rely on the contract, and
// a multi-gigabyte pass-through ran to completion regardless.
func copySourceCancellable(ctx context.Context, dst io.Writer, src io.ReaderAt, size int64) error {
	for off := int64(0); off < size; {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := int64(passThroughChunk)
		if remaining := size - off; remaining < n {
			n = remaining
		}
		if _, err := io.Copy(dst, io.NewSectionReader(src, off, n)); err != nil {
			return err
		}
		off += n
	}
	return ctx.Err()
}

// writeBufferSize is the buffer [Writer.writeToInternal] wraps its destination
// in. It is a throughput choice, not a limit: the serializer writes a few bytes
// at a time, so the buffer decides how many of those become one syscall. 64 KiB
// is the usual page-cache friendly size and bounds the extra memory at a fixed
// amount regardless of document size.
const writeBufferSize = 64 << 10

// writeToInternal performs the serialization. [Writer.WriteTo] wraps it
// with the single-use guard and the sign/full-rewrite compatibility
// check. Remediation calls this directly so its internal probe writes
// do not trip the single-use guard before the caller's real WriteTo.
//
// # Corpus evidence
//
//   - govdocs: write syscalls were 30% of a remediate run against an
//     unbuffered destination, since each round serializes the document to a
//     temp file to revalidate it.
//   - pdfjs bug1708041.pdf: came out of
//     `litho pipe f remediate -profile PDF/A-2b rotate -angle 90` with no
//     rotation on any page while the remediation overrides were seeded after
//     the queued mutations, which let them win at the same object number.
func (w *Writer) writeToInternal(ctx context.Context, dst io.Writer, opts ...WriteOption) (err error) {
	// The serializer emits a write per token: a dictionary key, a delimiter, a
	// number. Against an unbuffered destination every one of those is a
	// syscall, which dominated remediation, since each round serializes the
	// document to a temp file to revalidate it. Buffer here rather than at each
	// call site so the public Writer.WriteTo gets it too: a caller passing an
	// *os.File is the common case and would otherwise pay the same cost.
	buffered := bufio.NewWriterSize(dst, writeBufferSize)
	defer func() {
		if flushErr := buffered.Flush(); flushErr != nil && err == nil {
			err = fmt.Errorf("flushing output: %w", flushErr)
		}
	}()
	dst = buffered

	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}

	// Fast path: remediation already serialized the output to a temp file
	// for accurate residual validation. Stream from that file, but only
	// when nothing changed since: a mutation, Sign, Encrypt, or decrypt
	// queued after remediation, or a serialization-shaping write option,
	// is not in the temp file and streaming it would silently drop the
	// edit. The remediation overrides persist in w.rewriteOverrides, so a
	// fresh serialization keeps the applied fixes.
	if w.preSerializedPath != "" {
		// A Redact after remediation makes it stale too: the temp file holds
		// the unredacted document. One before remediation does not, since the
		// temp file went through the same write-time redaction check.
		stale := len(w.mutations) != w.preSerializedMutations ||
			len(w.redactions) != w.preSerializedRedactions ||
			(w.encryptCfg != nil) != w.preSerializedEncrypt ||
			w.decrypt != w.preSerializedDecrypt ||
			w.signer != nil ||
			cfg.fullRewrite || cfg.version != "" || cfg.xrefStream ||
			cfg.packObjectStreams || cfg.dedupStreams
		if !stale {
			return w.copyFromPreSerialized(ctx, dst)
		}
		w.reader.doc.Logger.Debug("writer: pre-serialized remediation output is stale, re-serializing",
			slog.Int("mutations", len(w.mutations)),
			slog.Int("pre_serialized_mutations", w.preSerializedMutations))
		if err := w.releasePreSerialized(); err != nil {
			w.reader.doc.Logger.Debug("writer: removing the stale remediation output failed", slog.Any("err", err))
		}
	}

	// Three source conditions make an incremental update unsafe. A rebuilt
	// cross-reference table means the recorded offsets are unusable, so
	// /Prev would chain into broken data. A non-zero header offset (junk
	// before %PDF) means readers disagree on the offset base: litho and
	// this writer use the physical file start, while Acrobat and veraPDF
	// resolve offsets relative to the header, so any appended section is
	// wrong for one convention (veraPDF rejects the mix outright). An
	// encrypted source has no encryptor bound to its crypto handler in
	// this writer, so appended strings and streams would be plaintext
	// where conforming readers expect ciphertext and decrypt them into
	// garbage. Force a full rewrite in all three cases: it re-derives
	// every offset, drops pre-header junk, and copies the decrypted
	// object graph without /Encrypt (use Writer.Encrypt to re-protect).
	// A write with no edits stays a byte-identical pass-through, and the
	// signing path below is exempt: it is structurally incremental, and
	// Writer.WriteTo rejects unsafe sign combinations.
	hasEdits := len(w.mutations) > 0 || w.trailerDirty || len(w.rewriteOverrides) > 0 ||
		w.encryptCfg != nil || w.decrypt
	if hasEdits && w.signer == nil {
		if w.reader.doc.IsEncrypted && !w.reader.doc.Authenticated {
			return ErrEncryptedNotAuthenticated
		}
		if w.reader.doc.Rebuilt || w.reader.doc.HeaderOffset > 0 || w.reader.doc.IsEncrypted {
			cfg.fullRewrite = true
		}
	}
	// A redacted document is always rewritten in full, whatever cleared
	// forceRewrite since: an incremental update keeps every superseded object
	// in the file, the original content stream among them.
	if len(w.redactions) > 0 {
		cfg.fullRewrite = true
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

	// WithVersion only takes effect on a full rewrite (it sets the output
	// header). Surface the no-op rather than dropping it silently so a
	// caller who passed it to an incremental write can see why the header
	// did not change.
	if cfg.version != "" && !cfg.fullRewrite && !w.forceRewrite {
		logger.Debug("WithVersion ignored: it applies only to a full rewrite; this write is incremental",
			slog.String("requested_version", cfg.version))
	}

	// The compaction options need the same notice for a stronger reason.
	// Packing and dedup run inside the full-rewrite engine, and
	// writer.IncrementalConfig carries no field for either, so the incremental
	// path has nothing to honor them with. An ignored compaction produces a
	// file byte-identical to one written without the option, which a caller
	// cannot tell apart from compaction that ran and found nothing to save.
	if (cfg.packObjectStreams || cfg.dedupStreams) && !cfg.fullRewrite && !w.forceRewrite {
		logger.Debug("compaction ignored: packing and dedup apply only to a full rewrite; this write is incremental",
			slog.Bool("pack_object_streams", cfg.packObjectStreams),
			slog.Bool("dedup_streams", cfg.dedupStreams))
	}

	// Signing requires its own two-pass incremental flow.
	if w.signer != nil {
		mutFuncs := make([]mutations.MutationFunc, len(w.mutations))
		for i, m := range w.mutations {
			mutFuncs[i] = m.apply
		}
		return mutations.WriteWithSignature(ctx, dst, w.reader.doc, signerAdapter{inner: w.signer}, mutFuncs, cfg.xrefStream)
	}

	if cfg.fullRewrite || w.forceRewrite {
		// Pre-apply mutations into a source-namespace allocator and hand
		// the results to the copier as overrides. Mutations resolve from
		// the source document and AllocAt source object numbers; running
		// them inside FullRewrite against its destination-numbered
		// allocator stomped unrelated copied objects and silently dropped
		// the edit (a page rotation landed on whatever object the copier
		// had renumbered into that slot).
		overrides := w.rewriteOverrides
		if len(w.mutations) > 0 {
			mutAlloc := writer.NewObjAllocator(w.reader.doc.HighestObjectNumber(ctx))
			// Seed with the remediation overrides so a mutation queued after
			// remediation builds ON its fixes rather than against the untouched
			// source. The page mutations read through latestPageDict, so the
			// seeded object is the one they extend.
			//
			// This replaces a blanket "remediation wins at the same object
			// number", which silently dropped whatever the caller asked for:
			// `litho pipe f remediate -profile PDF/A-2b rotate -angle 90` wrote
			// the file with no rotation. Mutations queued BEFORE remediation are
			// already folded into those overrides by Writer.remediate, so
			// re-applying them here lands on the same result.
			for objNum, obj := range w.rewriteOverrides {
				mutAlloc.AllocAt(objNum, obj)
			}
			for i, m := range w.mutations {
				if err := m.apply(ctx, mutAlloc, w.reader.doc); err != nil {
					return fmt.Errorf("applying mutation %d: %w", i, err)
				}
			}
			if len(mutAlloc.Pending()) > 0 {
				overrides = mutAlloc.Pending()
			}
		}
		enforced, enforceErr := w.enforceRedactions(ctx, overrides)
		if enforceErr != nil {
			return enforceErr
		}
		overrides = enforced
		return mutations.FullRewrite(ctx, dst, mutations.FullRewriteConfig{
			Doc:             w.reader.doc,
			EncryptCfg:      w.encryptCfg,
			Decrypt:         w.decrypt,
			Version:         cfg.version,
			RequiredVersion: w.requiredVersion,
			XRefStream:      cfg.xrefStream,
			Compact:         cfg.compactOptions(),
			Overrides:       overrides,
		})
	}

	doc := w.reader.doc

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))

	// Seed the remediation overrides FIRST so a queued mutation builds on the
	// fixes instead of losing to them: the page mutations read through
	// latestPageDict, so the seeded object is the one they extend. Remediation
	// uses source-namespace object numbers, which match this allocator, so
	// AllocAt with the same keys lands correctly.
	//
	// Seeding them last let them win at the same object number, which silently
	// dropped whatever the caller asked for, such as a rotation queued after
	// remediation. The full-rewrite branch above orders it the same way for the
	// same reason.
	for objNum, obj := range w.rewriteOverrides {
		alloc.AllocAt(objNum, obj)
	}

	for i, m := range w.mutations {
		if err := m.apply(ctx, alloc, doc); err != nil {
			return fmt.Errorf("applying mutation %d: %w", i, err)
		}
		logger.Debug("mutation applied", slog.Int("index", i))
	}

	// If no mutations produced pending objects AND the trailer was not
	// mutated in place, nothing has to be written. Otherwise the
	// incremental update below emits a fresh xref + trailer even when
	// the allocator is empty, preserving trailer-only fixes like /ID.
	if len(alloc.Pending()) == 0 && !w.trailerDirty {
		logger.Debug("no pending objects, copying source as-is")
		return copySourceCancellable(ctx, dst, doc.RawReader(), doc.FileSize())
	}
	logger.Debug("incremental update",
		slog.Int("pending_objects", len(alloc.Pending())),
	)

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
		Generation:      doc.ObjectGeneration,
	})
}

// Split extracts the specified 1-based pages from src into a new PDF document
// written to dst. Pages are included in the order given; duplicates are
// allowed to repeat a page. Pass [WithBookmarks] to control how the source
// outline is handled in the output (default: keep and remap existing,
// drop orphans). Pass [WithWriteLogger] to receive structured debug logs
// from the split engine.
//
// The output is written in the clear unless [WithEncryption] is passed, even
// when src is encrypted: see that option for why a source's encryption cannot
// carry across on its own.
func Split(ctx context.Context, src *Reader, pages []int, dst io.Writer, opts ...WriteOption) (err error) {
	defer recoverToError(src.logger(), "pdf.Split", &err)
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
	if err := mutations.Split(ctx, src.doc, allPages, pages, dst, version, cfg.xrefStream, cfg.compactOptions(), bookmarkCfg, cfg.internalEncrypt()); err != nil {
		// Translate at the boundary so a caller can match with errors.Is
		// without importing pkg/internal.
		if errors.Is(err, mutations.ErrAttachmentTreeIncomplete) {
			return fmt.Errorf("%w: %w", ErrAttachmentTreeIncomplete, err)
		}
		return err
	}
	return nil
}

// Merge combines multiple PDF documents into a single output written to dst.
// Pages are concatenated in order: all pages from sources[0], then all from
// sources[1], etc. Each source [Reader] must remain valid until Merge returns.
// Pass [WithBookmarks] to control how outlines from the sources are merged
// (default: concatenate with offset page indexes). Pass [WithWriteLogger] to
// receive structured debug logs from the merge engine (logs are emitted via
// the first source's document logger).
//
// The output is written in the clear unless [WithEncryption] is passed, even
// when the sources are encrypted. Sources opened under different passwords
// are written under the single key that option supplies.
func Merge(ctx context.Context, sources []*Reader, dst io.Writer, opts ...WriteOption) (err error) {
	var lg *slog.Logger
	if len(sources) > 0 {
		lg = sources[0].logger()
	}
	defer recoverToError(lg, "pdf.Merge", &err)
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
	if err := mutations.Merge(ctx, mergeSources, dst, cfg.version, cfg.xrefStream, cfg.compactOptions(), bookmarkCfg, cfg.internalEncrypt()); err != nil {
		// Translate at the boundary so a caller can match with errors.Is
		// without importing pkg/internal. A merge rebuilds one attachment tree
		// from every source, so a source the walk cannot read whole would lose
		// its attachments silently.
		if errors.Is(err, mutations.ErrAttachmentTreeIncomplete) {
			return fmt.Errorf("%w: %w", ErrAttachmentTreeIncomplete, err)
		}
		return err
	}
	return nil
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
