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
	"slices"
	"strings"

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

// WriteOption configures the behavior of [Writer.WriteTo], [Split], [Merge]
// and [Pipeline.WriteTo]. Given to [NewWriter], an option is a default for the
// Writer's WriteTo, which the same option given to WriteTo overrides.
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
	tempDir           string
	encrypt           *EncryptConfig
	// inTheClear writes the output unencrypted, whatever [Writer.Encrypt] or
	// [WithEncryption] asks for (see [inTheClear]).
	inTheClear bool
	// droppedSignatures receives the signatures the output drops (see
	// [WithDroppedSignatures]).
	droppedSignatures *[]DroppedSignature
}

// WithFullRewrite forces a clean rewrite of the entire file instead of an
// incremental update. Required for operations that touch every object
// (encrypt, decrypt, flatten). A Writer that signs ([Writer.Sign]) writes the
// rewrite, then signs it in an incremental update on top. The rewrite leaves
// out the source's signatures (see [Writer.DroppedSignatures]).
func WithFullRewrite() WriteOption {
	return func(o *writeOpts) {
		o.fullRewrite = true
	}
}

// WithEncryption encrypts the output with cfg. It applies to
// [Writer.WriteTo], [Split] and [Merge], and to [NewWriter], where it asks for
// the encryption as [Writer.Encrypt] does.
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
// forces a full rewrite, since every string and stream must be encrypted. It
// replaces the encryption Encrypt or NewWriter asked for.
func WithEncryption(cfg EncryptConfig) WriteOption {
	return func(o *writeOpts) {
		o.encrypt = &cfg
	}
}

// inTheClear writes the output unencrypted, whatever encryption
// [Writer.Encrypt] or [WithEncryption] asks for. A [Pipeline] writes each
// intermediate file it reopens with it: it has no password to reopen an
// encrypted one with, and it encrypts its own output instead (see
// [executor.carryEncryption]). The guards of [Writer.WriteTo] still read the
// encryption the Writer was asked for, so a signature asked for beside it is
// refused as it is on the output.
func inTheClear() WriteOption {
	return func(o *writeOpts) {
		o.inTheClear = true
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
	if o.encrypt == nil || o.inTheClear {
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
// Given to [NewWriter], it logs the Writer's [Writer.WriteTo], unless WriteTo
// is given a logger of its own.
func WithWriteLogger(logger *slog.Logger) WriteOption {
	return func(o *writeOpts) {
		o.logger = logger
	}
}

// WithPipelineTempDir directs a [Pipeline] to place intermediate temp files
// in dir instead of [os.TempDir], the ones a remediation in an
// [Pipeline.Apply] stage writes included. Useful in sandboxed environments
// where the default temp directory is not writable. It sets what
// [WithTempDir] sets.
func WithPipelineTempDir(dir string) WriteOption {
	return func(o *writeOpts) {
		o.tempDir = dir
	}
}

// WithTempDir directs the temp files litho writes to dir instead of
// [os.TempDir]. Passed to [NewWriter], it places the file a remediation writes
// to revalidate its output, and the temp files of the Writer's
// [Writer.WriteTo]. Passed to WriteTo, it places the temp files that write
// makes, the rewrite a signature goes on and the pages an insertion reads back
// to generate bookmarks, instead of the directory NewWriter was given. Passed
// to [Pipeline.WriteTo], it also places the pipeline's intermediate files, as
// [WithPipelineTempDir] does. Useful in sandboxed environments where the
// default temp directory is not writable: a remediation that cannot write
// there fails with [ErrRemediationUnverified], and a write with the error it
// met.
//
// [Split] and [Merge] write no temp file and ignore it.
func WithTempDir(dir string) WriteOption {
	return func(o *writeOpts) {
		o.tempDir = dir
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
//     Encrypt, Sign, ...). Mutations are queued; nothing is written yet.
//  4. Optionally, remediate once, after every other edit (RemediatePDFA,
//     RemediatePDFUA, RemediateWTPDF, RemediateFacturX or RepairFacturX).
//  5. Call WriteTo exactly once. It drains the queue and produces the
//     output.
//
// A remediation is the last edit on a Writer. It writes the edits queued
// before it to a temp file and validates, repairs and measures that document,
// so its [RemediationReport] describes the output. After it, the Writer
// accepts [Writer.Sign], [Writer.WriteTo] and [Writer.Close], and refuses every
// other call, a second remediation included, with [ErrEditAfterRemediation]:
// the refused call queues nothing, a method with an error return returns the
// refusal, and WriteTo returns the first refusal, whatever the method, and
// writes nothing. A remediation that fails leaves the Writer open to edits.
//
// A remediation writes its output to a temp file so it can revalidate it, in
// [os.TempDir] unless [NewWriter] was given [WithTempDir], and fails with
// [ErrRemediationUnverified] when it cannot. WriteTo streams that file to the
// destination and removes it, and a remediation that fails or is cancelled
// removes it itself. A Writer that is remediated and never written keeps the
// file until [Writer.Close].
//
// A page, or a page tree node, written directly in a /Kids array, which ISO
// 32000-1, 7.7.3.2, Table 29 does not allow, gets an object number of its own
// before any edit reads the document, and the node listing it names it by
// reference: every edit reaches it as it reaches any other page, and every
// output but an unedited copy of the source carries the repaired tree.
//
// A Writer is single-use: create a new Writer for each distinct output.
// Method calls must be serialised; no Writer method is safe for
// concurrent use, Close included. The Writer never modifies the Reader it
// was built from (see [NewWriter]): other goroutines may read that Reader,
// and build other Writers on it, while the Writer runs.
type Writer struct {
	// reader is the document the Writer edits: source, until a remediation
	// writes the queued edits to a temp file or keeps a later round (see
	// [Writer.rebase]). It then reads that file, at basePath, which the
	// Writer owns and releases with [Writer.releaseBase]. source is the
	// Writer's own copy of the Reader it was built on (see [Reader.fork]),
	// never that Reader itself: the Writer edits the trailer and the cache of
	// the document it edits.
	reader       *Reader
	source       *Reader
	basePath     string
	mutations    []mutation
	encryptCfg   *crypto.EncryptConfig
	decrypt      bool
	forceRewrite bool
	trailerDirty bool
	// encryptBy names the call that asked for encryptCfg, "Writer.Encrypt" or
	// the WithEncryption given to NewWriter, for the refusal of a remediation
	// that forbids it.
	encryptBy string
	// defaults are the options NewWriter was given, but for the encryption,
	// which it asks for as Encrypt does: WriteTo applies its own after them
	// (see [Writer.writeOptions]).
	defaults writeOpts
	// consumed is set once WriteTo produces output successfully. A second
	// WriteTo on the same Writer returns ErrWriterConsumed rather than
	// re-applying the already-drained mutation state.
	consumed bool
	// closed is set by Close. WriteTo, Redact and remediation refuse a
	// closed Writer with ErrWriterClosed.
	closed bool
	// tempDir is the directory the Writer's temp files go in, "" for
	// os.TempDir: WithTempDir passed to NewWriter, or the directory of the
	// Pipeline the Writer serves.
	tempDir string
	// rewriteOverrides holds per-object replacements produced during
	// remediation and redaction, keyed by source object number. They are
	// forwarded to the full-rewrite copier so the renumbered output reflects
	// the patched data instead of the original. A redaction starts from the
	// ones already here, so a later redaction builds on an earlier one
	// instead of discarding it.
	rewriteOverrides map[int]ast.PdfObject
	// folded counts the queued mutations, from the first, whose edits
	// rewriteOverrides already holds. A redaction replays the mutations queued
	// before it, so that it reads the document as staged, and keeps what they
	// staged in its overrides. Only the mutations after these are applied on
	// top of the overrides: applying one again repeats an edit that need not
	// be idempotent, and a stamp was drawn twice. They stay queued all the
	// same, since a redaction that refuses puts the Writer back as it was.
	folded          int
	signer          Signer
	requiredVersion string

	// remediation names the remediation method that ran on the Writer, ""
	// until one succeeds. From then on the Writer accepts Sign, WriteTo and
	// Close alone (see [Writer.refuseAfterRemediation]). pdfaClause is the ISO
	// 19005 clause that forbids encrypting what that remediation produced, ""
	// when its profile is not a PDF/A one, and accessibilityClause the ISO
	// 14289 clause that requires an encryption of it to permit extraction for
	// accessibility, "" when its profile states none. refused is the first call
	// refused after it, which WriteTo returns.
	remediation         string
	pdfaClause          string
	accessibilityClause string
	refused             error

	// redactions records the regions of every successful [Writer.Redact]
	// call, in call order, and redactedTokens the text they removed. WriteTo
	// re-checks the fully composed output against them, because a stage
	// queued before or after Redact can rebuild content from the source
	// document and restore what Redact removed.
	redactions     [][]RedactRegion
	redactedTokens []string

	// droppedSignatures lists the signatures the writes that produce the
	// output left out, each a full rewrite (see [Writer.DroppedSignatures]). A
	// remediation writes its output before WriteTo, so the record outlives
	// one write; a remediation or a WriteTo that fails puts it back as it was.
	droppedSignatures []DroppedSignature

	// preSerializedPath is the path to a temp file holding the fully
	// serialized PDF when remediation pre-serializes for accurate
	// residual validation. When set, WriteTo streams from this file
	// instead of re-serializing, then removes it. Close removes it too,
	// unless it is the file at basePath, which only releaseBase removes. A
	// signature, an encryption, or a serialization-shaping write option is not
	// in the file (see [Writer.writeTempFile]), and WriteTo re-serializes so
	// it is not silently dropped.
	preSerializedPath string
}

// mutation is a deferred modification applied during WriteTo.
type mutation struct {
	apply func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error
	// pageSource is the document an insertion copies pages from, nil for
	// every other edit. The output declares at least its version (see
	// [Writer.InsertPages]).
	pageSource *document.Document
}

// pageSources returns the documents the queued insertions copy pages from,
// in queue order, those a redaction folded in included: their pages are in the
// output all the same.
func (w *Writer) pageSources() []*document.Document {
	var docs []*document.Document
	for _, m := range w.mutations {
		if m.pageSource != nil {
			docs = append(docs, m.pageSource)
		}
	}
	return docs
}

// unfoldedMutations returns the queued mutations whose edits the rewrite
// overrides do not hold yet, the ones a stage building on the overrides still
// has to apply (see [Writer.folded]).
func (w *Writer) unfoldedMutations() []mutation {
	return w.mutations[w.folded:]
}

// NewWriter creates a single-use [Writer] that queues mutations
// against r. Release the Writer with [Writer.Close] and close r with
// [Reader.Close] after the Writer has finished.
//
// The Writer never modifies r. It edits a copy of r's document that shares
// only what r fixed when it opened, so reads through r may go on while the
// Writer is active, from any goroutine, and any number of Writers may be built
// on one Reader and run at once. r reports afterwards what it reported
// before. The copy reads r's file, so r must stay open until the Writer has
// finished.
//
// The options are defaults for [Writer.WriteTo]: it writes as if they came
// before its own, so an option passed to WriteTo overrides the same setting
// given here. [WithEncryption] asks for the encryption as [Writer.Encrypt]
// does, so a later Encrypt replaces it as well, and a remediation that forbids
// it refuses it as it refuses Encrypt. [WithTempDir] also places the temp
// files a remediation writes. [WithBookmarks] shapes [Split] and [Merge] alone
// and has no effect on a Writer.
func NewWriter(r *Reader, opts ...WriteOption) *Writer {
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}
	w := newWriterOn(r.fork())
	w.tempDir = cfg.tempDir
	if cfg.encrypt != nil {
		w.askEncryption(*cfg.encrypt, "WithEncryption given to NewWriter")
		cfg.encrypt = nil
	}
	w.defaults = cfg
	return w
}

// writeOptions returns the options a write by [Writer.WriteTo] parses: the
// ones [NewWriter] was given, then opts, so an option in opts overrides the
// same setting given to NewWriter. Every parse starts from the zero
// [writeOpts], and the first option puts NewWriter's settings in it.
func (w *Writer) writeOptions(opts []WriteOption) []WriteOption {
	defaults := w.defaults
	merged := make([]WriteOption, 0, len(opts)+1)
	merged = append(merged, func(o *writeOpts) { *o = defaults })
	return append(merged, opts...)
}

// newWriterOn returns a Writer that edits r itself rather than a copy of it.
// r must be a reader no other goroutine reads, such as one litho opened over a
// temp file it wrote: a Writer writes its staged edits into the document it
// edits (see [NewWriter]).
func newWriterOn(r *Reader) *Writer {
	return &Writer{reader: r, source: r}
}

// Close removes the temp file a remediation left for [Writer.WriteTo] to
// stream, when WriteTo has not consumed it, and closes the reader the Writer
// opened over it. It does not close the Writer's [Reader].
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
	return errors.Join(w.releasePreSerialized(), w.releaseBase())
}

// releasePreSerialized removes the remediation output WriteTo would stream,
// if any, and forgets it, so WriteTo re-serializes the queued state instead.
// A file already gone is not an error. The file the Writer edits stays: it is
// the document that re-serialization reads, and [Writer.releaseBase] removes
// it.
func (w *Writer) releasePreSerialized() error {
	path := w.preSerializedPath
	if path == "" {
		return nil
	}
	w.preSerializedPath = ""
	if path == w.basePath {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing remediation temp file: %w", err)
	}
	return nil
}

// rebase makes next, a reader over a temp file at path that litho wrote from
// the Writer, the document the Writer edits, and releases the file it edited
// before, if it owned one.
//
// Two files take that place. A remediation writes the edits queued before it
// to one and judges and repairs that document (see [Writer.materialize]). A
// remediation round after the first repairs a reopened copy of the previous
// round's output, in that file's object numbers, which a full rewrite has
// renumbered, so its repairs cannot join the overrides the Writer keeps in its
// document's numbers: they exist only in the round's output, which WriteTo
// serializes afresh when a signature, an encryption or a write option makes
// the streamed file stale, and after a failed write.
//
// The file already holds every edit the Writer queued, its overrides and its
// trailer, so none of them stays queued, where they would be applied a second
// time. The flags that decide how the output is written stay, and so do the
// redactions, which WriteTo checks the output against.
func (w *Writer) rebase(next *Reader, path string) error {
	prev, prevPath := w.reader, w.basePath
	w.reader, w.basePath = next, path
	w.mutations = nil
	w.rewriteOverrides = nil
	w.folded = 0
	w.trailerDirty = false
	if prevPath == "" {
		return nil
	}
	return releaseOwnedReader(prev, prevPath)
}

// releaseBase closes the reader over the remediation output the Writer edits,
// removes the file and gives the Writer back its source [Reader]. A Writer
// editing its source has nothing to release.
func (w *Writer) releaseBase() error {
	if w.basePath == "" {
		return nil
	}
	base, path := w.reader, w.basePath
	w.reader, w.basePath = w.source, ""
	if w.preSerializedPath == path {
		w.preSerializedPath = ""
	}
	return releaseOwnedReader(base, path)
}

// releaseOwnedReader closes r, a reader litho opened over the temp file at
// path, and removes the file. A file already gone is not an error.
func releaseOwnedReader(r *Reader, path string) error {
	var errs []error
	if err := r.Close(); err != nil {
		errs = append(errs, fmt.Errorf("closing the remediation output: %w", err))
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("removing remediation temp file: %w", err))
	}
	return errors.Join(errs...)
}

// refuseAfterRemediation refuses call, a Writer method, once a remediation has
// run on the Writer, and returns nil before. A remediation's report describes
// the output only when nothing edits the document after it, so a remediated
// Writer accepts Sign, WriteTo and Close alone.
//
// The refusal is recorded as well as returned. A method with no error return
// cannot hand it back, and a caller can drop one that is returned, so WriteTo
// returns the first refusal recorded and writes nothing: the output never goes
// out without an edit the caller asked for.
func (w *Writer) refuseAfterRemediation(call string) error {
	if w.remediation == "" {
		return nil
	}
	err := fmt.Errorf("%w: Writer.%s after Writer.%s", ErrEditAfterRemediation, call, w.remediation)
	if w.refused == nil {
		w.refused = err
	}
	return err
}

// createTemp creates a temp file named after pattern in the Writer's temp
// directory ([os.TempDir] unless [WithTempDir] or a [Pipeline] set one).
func (w *Writer) createTemp(pattern string) (*os.File, error) {
	return os.CreateTemp(w.tempDir, pattern)
}

// writeTempFile serializes what src queues into a new temp file named after
// pattern in w's temp directory and returns its path.
//
// The file is never signed, even when [Writer.Sign] was called before the
// remediation that writes it. It is what the remediation measures and
// revalidates, and [Writer.WriteTo] signs afresh, so signing it ran the
// caller's [Signer] once per probe for nothing. It also sent the probe through
// the signing path, which then left out every repair, so the remediation
// judged the unrepaired document.
//
// Nor is it encrypted, even when [Writer.Encrypt] was called before the
// remediation: [Writer.WriteTo] encrypts afresh, as it does for
// [WithEncryption]. Encrypted, the file could not be reopened under a user
// password, and a remediation queued after Encrypt failed as broken; and the
// output of a later round, which is never encrypted, was streamed as if it
// were, silently unprotected.
//
// A file that cannot be created, written or closed is an error of the
// environment, not of the document, and comes back wrapping
// [ErrRemediationUnverified]; a failure of the serialization itself comes back
// as it is. Either way the file is removed. Remediation used to treat a temp
// file it could not create as a reason to skip its checks, and returned the
// report the engine drew up before any output existed as a success.
func (w *Writer) writeTempFile(ctx context.Context, src *Writer, pattern string) (string, error) {
	f, err := w.createTemp(pattern)
	if err != nil {
		return "", fmt.Errorf("%w: creating a temp file: %w", ErrRemediationUnverified, err)
	}
	path := f.Name()
	tf := &tempFileWriter{f: f}
	encrypt := src.encryptCfg
	src.encryptCfg = nil
	writeErr := src.writeToInternal(ctx, tf, nil)
	src.encryptCfg = encrypt
	closeErr := f.Close()

	var failure error
	switch {
	case tf.err != nil:
		failure = fmt.Errorf("%w: writing %s: %w", ErrRemediationUnverified, path, tf.err)
	case writeErr != nil:
		failure = writeErr
	case closeErr != nil:
		failure = fmt.Errorf("%w: closing %s: %w", ErrRemediationUnverified, path, closeErr)
	}
	if failure != nil {
		removeTempFile(w.reader.doc.Logger, path)
		return "", failure
	}
	return path, nil
}

// tempFileWriter passes writes on to a temp file and keeps the first error the
// file returns, so a failure of the file is told apart from one of the
// serialization writing into it.
type tempFileWriter struct {
	f   *os.File
	err error
}

// Write implements [io.Writer].
func (t *tempFileWriter) Write(p []byte) (int, error) {
	n, err := t.f.Write(p)
	if err != nil && t.err == nil {
		t.err = err
	}
	return n, err
}

// SetMetadata sets or replaces document Info dictionary fields. Empty fields
// in meta are skipped (the existing value is preserved). The mutation is
// queued and applied when [Writer.WriteTo] is called.
func (w *Writer) SetMetadata(meta Metadata) {
	if w.refuseAfterRemediation("SetMetadata") != nil {
		return
	}
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
	if w.refuseAfterRemediation("ClearMetadata") != nil {
		return
	}
	w.forceRewrite = true
	w.mutations = append(w.mutations, mutation{
		apply: mutations.ClearMetadata,
	})
}

// RotatePages sets the /Rotate value for the given 1-based page numbers,
// which name the Reader's pages (see [Writer]). If no pages are specified,
// every page the Writer holds when the rotation is queued is rotated: pages
// inserted on it before are included, pages deleted on it before are not.
// Rotation must be a multiple of 90 (0, 90, 180, 270). The mutation is queued
// and applied when [Writer.WriteTo] is called.
func (w *Writer) RotatePages(rotation int, pages ...int) error {
	if err := w.refuseAfterRemediation("RotatePages"); err != nil {
		return err
	}
	if rotation%90 != 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidRotation, rotation)
	}
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			resolvedPages, err := w.reader.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving pages: %w", err)
			}
			return mutations.RotatePages(ctx, alloc, doc, resolvedPages, rotation, pages)
		},
	})
	return nil
}

// DeletePages removes the specified 1-based pages from the document.
// Forces a full rewrite. The mutation is queued and applied when
// [Writer.WriteTo] is called.
//
// Returns [ErrEmptyPageList] when no pages are given. This is the only page
// operation that refuses an empty list: [Writer.RotatePages] and the three
// stamps document an empty list as every page the Writer holds, and
// [Writer.InsertPages] as every page of its source, which deletion cannot mean
// because it would leave no document.
//
// The pages named are the Reader's. A deletion queued after an
// insertion keeps the inserted pages, and one queued after another deletion
// keeps what that one removed out. It may name every page of the Reader when
// an insertion queued before it leaves the document a page; [Writer.WriteTo]
// refuses a deletion that leaves none.
//
// [Writer.WriteTo] reports [ErrPageDeletionIncomplete] when the document's
// structure tree nests deeper than the page prune can walk. Elements below that
// bound keep the deleted page alive through their /Pg, so the output would
// carry the page's content while reporting one fewer page. Nothing is written
// in that case.
func (w *Writer) DeletePages(pages ...int) error {
	if err := w.refuseAfterRemediation("DeletePages"); err != nil {
		return err
	}
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
//
// Insertions and deletions queued on one Writer build on each other, and pos
// names a place among the Reader's pages, as page numbers do.
// The pages go before the first of the Reader's pages from pos on that an
// earlier deletion kept, after the pages an earlier insertion put there, and
// at the end when no such page is left.
//
// The inserted pages join the document as a later source's pages join a
// [Merge]:
//
//   - The form fields their widgets present join the document's interactive
//     form, which is created when the document has none. A field keeps its
//     name when the form already uses it: fields with one fully qualified name
//     are one field (ISO 32000-1, 12.7.3.2), so its widgets on the inserted
//     pages and those already in the document show one value. A page moved
//     within its own document, inserted and then deleted from where it was,
//     keeps its widgets in the fields they presented.
//   - A signed signature field is left out of the form, since its digest
//     covers bytes of the source file the document does not hold (ISO
//     32000-1, 12.8.1). Its widget's appearance is drawn into the page
//     content, as [Writer.FlattenForm] draws one, and the widget removed, so
//     the page looks as it did. In a tagged document the drawing is marked as
//     an artifact: the insertion carries the source's pages, not its
//     structure, so no structure element encloses it. A widget a viewer does
//     not display, or with no area as an invisible signature has, goes
//     without drawing. An unsigned signature field signs nothing and is
//     listed like any other field.
//   - The header declares at least the version source declares, and a
//     catalog /Version the document carries is raised to the version source
//     reads as ([Reader.Version]), so pages from a PDF 2.0 source never land
//     in a file declaring less (ISO 32000-1, 7.5.2). [WithVersion] names a
//     version the insertion can only raise.
//
// InsertPages reads a copy of source's document, as [Split] does, so source is
// left as it was and other goroutines may read it meanwhile. source must stay
// open until [Writer.WriteTo] returns.
func (w *Writer) InsertPages(pos int, source *Reader, sourcePages ...int) error {
	if err := w.refuseAfterRemediation("InsertPages"); err != nil {
		return err
	}
	if source == nil {
		return ErrNilSource
	}
	// The pages are copied from a copy of source's document, as Split and
	// Merge copy theirs, so a page source writes directly in /Kids is
	// numbered there (see [Writer.promotePageTree]) and never in source.
	source = source.fork()
	w.forceRewrite = true
	w.mutations = append(w.mutations, mutation{
		pageSource: source.doc,
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			targetPages, err := w.reader.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving target pages: %w", err)
			}
			if err := source.doc.PromoteDirectKids(ctx); err != nil {
				return fmt.Errorf("numbering the source pages written in /Kids: %w", err)
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
	if w.refuseAfterRemediation("SetBookmarks") != nil {
		return
	}
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
//
// The existing bookmarks are the outline an earlier edit on the Writer set,
// when one did, and the Reader's otherwise. The headings are detected in the
// pages the Writer holds when GenerateBookmarks is queued: a page inserted
// before it is read, and a page deleted before it yields no bookmark. A page of
// the Reader is read as [Reader.GenerateBookmarks] reads it, so content
// stamped or redacted on it on the same Writer is not read.
func (w *Writer) GenerateBookmarks(opts BookmarkGenerationOptions, mode BookmarkMergeMode) {
	if w.refuseAfterRemediation("GenerateBookmarks") != nil {
		return
	}
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			readerPages, err := w.reader.resolvePages(ctx)
			if err != nil {
				return fmt.Errorf("resolving pages for bookmarks: %w", err)
			}
			resolvedPages := mutations.StagedPages(ctx, alloc, doc, readerPages)
			generated, err := w.stagedHeadings(ctx, alloc, doc, readerPages, resolvedPages, opts)
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
				// The outline an earlier edit queued is the existing one, and
				// reading the Reader's instead wrote over it.
				existing, staged := mutations.StagedBookmarks(ctx, alloc, doc, resolvedPages)
				if !staged {
					existing, err = w.reader.Bookmarks(ctx)
					existing = renumberBookmarks(existing, readerPages, resolvedPages)
				}
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

			return mutations.SetBookmarks(ctx, alloc, doc, resolvedPages, final)
		},
	})
}

// AutoTagReport says what a [Writer.AutoTag] did to the document.
//
// AutoTag is queued like every other edit, so the Writer fills the report when
// it applies the edit: during [Writer.WriteTo], or during a remediation or a
// [Writer.Redact] queued after AutoTag, since those apply the edits queued
// before them. Until then Applied is false and the other fields are zero.
//
// Values must not be shared across goroutines without synchronization: the
// Writer fills one on the goroutine that calls the method applying the edit, so
// read it after that call returns.
type AutoTagReport struct {
	// Applied reports whether the Writer applied the edit. It stays false when
	// the call that would apply it failed before reaching it, and when the
	// Writer refused AutoTag after a remediation (see [ErrEditAfterRemediation]).
	// It says the edit ran, not that the output was written: the applying
	// call's error says that.
	Applied bool
	// Skipped is nil when AutoTag built a structure tree. Otherwise it says why
	// AutoTag left the document as it was, and wraps one of
	// [ErrAutoTagAlreadyTagged], [ErrAutoTagMarkedContent],
	// [ErrAutoTagUnreadableContent] and [ErrAutoTagNothingToTag]: test it with
	// [errors.Is]. A skip fails nothing, and the output is written without a
	// new tree.
	Skipped error
	// Page is the 1-based page Skipped concerns, counted among the pages the
	// Writer holds when AutoTag is queued, 0 when it concerns the whole
	// document or AutoTag skipped nothing.
	Page int
}

// AutoTag synthesizes a logical structure tree (a tagged PDF) for an untagged
// document: it classifies each page's text into headings and paragraphs, marks
// the content (tagging text, artifacting the rest), and wires the catalog
// /StructTreeRoot and /MarkInfo. The change is applied when [Writer.WriteTo] is
// called.
//
// AutoTag produces the structure and marked content only; it does not set the
// document language, title, or the PDF/UA identifier, which other operations
// own. Pair it with [Writer.RemediatePDFUA], or set those explicitly, for full
// PDF/UA conformance.
//
// It tags the document as the edits queued before it leave it, as every edit
// does. Queue it before a remediation, which then judges the tagged document:
// after one it is refused with [ErrEditAfterRemediation]. It tags the pages the
// Writer holds when it is queued, a page inserted before it included and a
// page deleted before it left out.
//
// It leaves some documents alone by design, and the returned [AutoTagReport]
// says so in Skipped once the edit is applied: an already-tagged document
// ([ErrAutoTagAlreadyTagged]), one whose content is already marked, since
// marking it again would nest or cross the existing sequences
// ([ErrAutoTagMarkedContent]), one whose content cannot be read
// ([ErrAutoTagUnreadableContent]), and one with nothing to tag
// ([ErrAutoTagNothingToTag]). Marked content includes the /Artifact sequence a
// stamp wraps its content in: queue AutoTag before a stamp to tag the document.
//
// The report is filled when the edit is applied rather than complete on
// return, because whether AutoTag tags depends on the document the edits queued
// before it produce, which only applying them decides. A caller that does not
// need the report calls AutoTag as a statement and ignores it.
func (w *Writer) AutoTag() *AutoTagReport {
	report := &AutoTagReport{}
	if w.refuseAfterRemediation("AutoTag") != nil {
		return report
	}
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			// A redaction and a remediation apply the queued edits before
			// WriteTo does, and one that fails leaves them queued, so the edit
			// can run more than once. Each run describes the document it saw,
			// so it starts from an empty report.
			*report = AutoTagReport{}
			res, err := tagging.AutoTag(ctx, alloc, doc)
			if err != nil {
				return fmt.Errorf("auto-tagging: %w", err)
			}
			for _, d := range res.Descriptions {
				doc.Logger.Debug("auto-tag", slog.String("detail", d))
			}
			*report = AutoTagReport{Applied: true, Skipped: res.Skipped, Page: res.Page}
			return nil
		},
	})
	return report
}

// EncryptAlgorithm identifies a PDF standard-security-handler algorithm.
// Values cover the ISO 32000 V=1..5 security handlers. The zero value is
// [EncryptAES256], the recommended one; the RC4 variants are provided for
// legacy-compatibility writes only, and a caller names them to get them.
// EncryptAlgorithm values are immutable and safe for concurrent use.
type EncryptAlgorithm int

// Each algorithm carries a minimum PDF version, and the output declares it:
// a file selecting a handler its declared version does not define misdescribes
// itself. RC4 needs nothing beyond PDF 1.4, AES-128 raises the output to 1.6,
// and AES-256 raises it to 1.7 and records the Adobe extension that defines
// it. See [EncryptAES256] for why 1.7 rather than 2.0.
//
// The constants are listed from the strongest handler down, and the
// strongest is the zero value, so an [EncryptConfig] that names no algorithm
// never selects a weak one.
const (
	// EncryptAES256 uses 256-bit AES-CBC (V=5, R=6) through the AESV3 crypt
	// filter. Recommended, and the zero value.
	//
	// ISO 32000-2 defines this handler, but the output declares PDF 1.7 plus
	// an /Extensions entry naming Adobe extension level 3 rather than PDF
	// 2.0. AES-256 is an extension to 1.7, not a feature of it, and declaring
	// 2.0 would overstate a document whose body is unchanged. This matches
	// what other producers write: qpdf emits the same 1.7-plus-extension
	// pair.
	EncryptAES256 EncryptAlgorithm = iota
	// EncryptAES128 uses 128-bit AES-CBC (V=4, R=4) through the AESV2 crypt
	// filter, which ISO 32000-1, Table 25 introduces in PDF 1.6. The output
	// declares at least that version.
	EncryptAES128
	// EncryptRC4_128 uses 128-bit RC4 (V=2, R=3), a PDF 1.4 handler. Weak.
	// Legacy only.
	EncryptRC4_128
	// EncryptRC4_40 uses 40-bit RC4 (V=1, R=2), a PDF 1.1 handler. Weak.
	// Legacy only.
	EncryptRC4_40
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
	// Algorithm selects the standard security handler variant. The zero
	// value selects [EncryptAES256]; [EncryptRC4_40] and the other weaker
	// handlers are written only when named.
	Algorithm EncryptAlgorithm
	// Permissions is the 32-bit PDF permission mask. Pass -1 for "all
	// permissions granted", or build one with [Permissions.Mask].
	//
	// The P entry the output carries holds the permission bits of the mask
	// and the reserved bits at the values ISO 32000-2, 7.6.4.2, Table 22
	// gives them, whatever the mask holds there: bits 1-2 cleared ("Reserved.
	// Must be zero (0)."), bits 7-8 set ("Reserved. Must be 1.") and bits
	// 13-32 set ("(Security handlers of revision 3 or greater) Reserved. Must
	// be 1."). The table names no value of bits 13-32 for the revision 2
	// handler, [EncryptRC4_40], which gets them set as well, as the example
	// of ISO 32000-1, 7.6.3.2 shows. -1 is written as -4, and 0, every
	// permission denied, as -3904. Readers ignore those bits (ISO 32000-1,
	// 7.6.3.2), so this changes no permission.
	//
	// An output that declares PDF 2.0 sets bit 10 whatever the mask says: ISO
	// 32000-2, Table 22 marks the bit "Not used" and reads "PDF writers shall
	// always set this bit to 1". Below 2.0 the mask is written as given, and
	// bit 10 is the permission to extract text and graphics in support of
	// accessibility (ISO 32000-1, Table 22).
	Permissions int32
}

// toInternal translates an [EncryptConfig] into the internal
// [crypto.EncryptConfig] consumed by the write engine. Kept
// lowercase-private so the internal type never appears in a public
// signature.
//
// [EncryptAlgorithm] and [crypto.Algorithm] list the handlers in the same
// order, AES-256 first, so the algorithm converts by value, and a value
// outside the list stays one the engine refuses.
func (c EncryptConfig) toInternal() crypto.EncryptConfig {
	return crypto.EncryptConfig{
		UserPassword:  c.UserPassword,
		OwnerPassword: c.OwnerPassword,
		Algorithm:     crypto.Algorithm(c.Algorithm),
		Permissions:   c.Permissions,
	}
}

// encryptConfigFromInternal is the inverse of [EncryptConfig.toInternal]. A
// [Pipeline] reads the encryption a [Writer] was asked for back through it, to
// carry it past an intermediate file written in the clear.
func encryptConfigFromInternal(c crypto.EncryptConfig) EncryptConfig {
	return EncryptConfig{
		UserPassword:  c.UserPassword,
		OwnerPassword: c.OwnerPassword,
		Algorithm:     EncryptAlgorithm(c.Algorithm),
		Permissions:   c.Permissions,
	}
}

// Encrypt applies password-based encryption to the output document.
// Forces a full rewrite since every string and stream must be
// encrypted. It replaces the encryption [NewWriter] was given, and
// [WithEncryption] passed to [Writer.WriteTo] replaces it.
func (w *Writer) Encrypt(cfg EncryptConfig) {
	if w.refuseAfterRemediation("Encrypt") != nil {
		return
	}
	w.askEncryption(cfg, "Writer.Encrypt")
}

// askEncryption makes cfg the encryption of the output and forces the full
// rewrite it needs. by names the call that asked for it.
func (w *Writer) askEncryption(cfg EncryptConfig, by string) {
	internal := cfg.toInternal()
	w.encryptCfg = &internal
	w.encryptBy = by
	w.forceRewrite = true
}

// RemoveEncryption removes encryption from the output document. The Reader
// must have been opened with the correct password. Forces a full rewrite.
func (w *Writer) RemoveEncryption() {
	if w.refuseAfterRemediation("RemoveEncryption") != nil {
		return
	}
	w.decrypt = true
	w.forceRewrite = true
}

// SetFormField sets the value of an AcroForm field by its fully qualified name.
// Uses incremental update. Sets /NeedAppearances on the AcroForm so viewers
// regenerate the visual appearance. The mutation is queued and applied when
// [Writer.WriteTo] is called.
func (w *Writer) SetFormField(name, value string) {
	if w.refuseAfterRemediation("SetFormField") != nil {
		return
	}
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return mutations.SetFormField(ctx, alloc, doc, name, value)
		},
	})
}

// FlattenForm bakes form field appearances into page content streams and
// removes the AcroForm dictionary. Forces a full rewrite. The mutation is
// queued and applied when [Writer.WriteTo] is called.
//
// A widget with nothing to draw, no appearance and no value, goes with the
// form. So does a widget the viewer never displays, one whose Hidden or NoView
// flag is set (ISO 32000-1, Table 165), without being drawn: a NoView widget
// that prints loses its printed appearance, because content that shows only in
// print needs optional content usage that ISO 19005-1 to ISO 19005-3 and both
// parts of ISO 14289 forbid.
//
// A widget the viewer displays is drawn as it shows on screen, whether or not
// its Print flag is set, so it prints once flattened. Content that shows on
// screen and not in print needs the same optional content usage (ISO 32000-1,
// 8.11.4.4) those parts forbid: ISO 19005-1, 6.1.13 forbids optional content,
// and ISO 19005-2 and ISO 19005-3, 6.9, ISO 14289-1, 7.10 and ISO 14289-2, 8.7
// forbid the /AS array that applies it. The flattened page matches what the
// viewer displayed.
//
// On a tagged document each baked appearance is tagged under the structure
// element that enclosed its widget, a Form element taking the PrintField role
// of its field (ISO 32000-1, Table 348), or marked as an artifact when no
// element did, so the page content stays tagged or artifact as ISO 14289-1,
// 7.1 requires.
func (w *Writer) FlattenForm() {
	if w.refuseAfterRemediation("FlattenForm") != nil {
		return
	}
	w.forceRewrite = true
	w.mutations = append(w.mutations, mutation{
		apply: mutations.FlattenForm,
	})
}

// StampText overlays text (a watermark or stamp) on the specified
// 1-based pages, which name the Reader's pages (see [Writer]). If no pages are
// specified, the stamp is applied to every page the Writer holds when the
// stamp is queued: pages inserted on it before are stamped, pages deleted on
// it before are not. A diagonal, semi-transparent watermark is a [TextStamp]
// with a Rotation and an Opacity below 1. Uses incremental update. The
// mutation is queued and applied when [Writer.WriteTo] is called.
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
	if err := w.refuseAfterRemediation("StampText"); err != nil {
		return err
	}
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

// StampImage overlays a JPEG or PNG image on the specified 1-based pages,
// which name the Reader's pages. If no pages are specified, the stamp is
// applied to every page the Writer holds when it is queued, as
// [Writer.StampText] describes. Uses incremental update. The mutation is
// queued and applied when [Writer.WriteTo] is called.
//
// The stamp is marked as an /Artifact, so it does not cost a tagged document
// its ISO 14289-1, 7.1 conformance.
func (w *Writer) StampImage(stamp ImageStamp, pages ...int) error {
	if err := w.refuseAfterRemediation("StampImage"); err != nil {
		return err
	}
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
// 1-based pages, which name the Reader's pages. If no pages are specified, the
// stamp is applied to every page the Writer holds when it is queued, as
// [Writer.StampText] describes. Uses incremental update. The mutation is
// queued and applied when [Writer.WriteTo] is called.
//
// The stamp is marked as an /Artifact, so it does not cost a tagged document
// its ISO 14289-1, 7.1 conformance, and any text it carries is omitted from
// [Reader.ExtractText] unless [WithArtifacts] is passed.
func (w *Writer) StampPDF(stamp PDFStamp, pages ...int) error {
	if err := w.refuseAfterRemediation("StampPDF"); err != nil {
		return err
	}
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
	if w.refuseAfterRemediation("AddAttachment") != nil {
		return
	}
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
	if w.refuseAfterRemediation("AddAssociatedFile") != nil {
		return
	}
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
	if w.refuseAfterRemediation("RemoveAttachment") != nil {
		return
	}
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
// When the output is a full rewrite, [Writer.WriteTo] writes that rewrite
// first and signs it in an incremental update on top, as a caller would by
// writing, reopening and signing. Four cases take that path. [WithFullRewrite]
// is signed on top of the rewrite it asks for. An edit that forces a full
// rewrite ([Writer.DeletePages], [Writer.InsertPages], [Writer.FlattenForm],
// [Writer.ClearMetadata], [Writer.Redact]) is signed on top of the rewritten
// document. A remediation whose repairs need a full rewrite is signed on top
// of its output, so the signed file validates like the unsigned one. A source
// litho repaired on reading, its cross-reference table rebuilt or bytes found
// before its header, is rewritten as an unsigned write with an edit rewrites
// it: an update appended to the damaged bytes would carry their wrong offsets
// into the signed file.
//
// Sign registers the signer rather than queueing a mutation, so it may be
// called at any point, after a remediation included: it is the one call
// besides [Writer.WriteTo] and [Writer.Close] that a remediated Writer
// accepts (see [Writer]). [Writer.WriteTo] applies every queued mutation and
// every repair a remediation kept before computing the digest, and the
// signature covers the result either way. The signer runs once, in WriteTo:
// the files a remediation writes to revalidate its output are never signed.
//
// What it is not compatible with is encryption, on the source or on the
// output, and decryption, which [Writer.WriteTo] rejects with
// [ErrSignFullRewriteIncompatible]. The signature of an encrypted document
// covers its encrypted bytes (ISO 32000-1, 12.8.1), and its signature
// dictionary's strings are encrypted under the document's key like every
// other string (ISO 32000-1, 7.6.1), an update litho cannot write. The source
// decides: a remediation of an encrypted source works on a decrypted copy,
// and signing is refused after it all the same, since the caller asked to sign
// the encrypted document, not that copy. So does a [Pipeline] stage after a
// Split or a Merge of an encrypted document. To sign a decrypted document,
// write it, reopen the output and sign that.
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
//
// Its output is never encrypted: every part of ISO 19005 forbids the Encrypt
// key in the trailer (6.1.3 in each). It returns [ErrPDFAForbidsEncryption]
// when [Writer.Encrypt] was called before it or [NewWriter] was given
// [WithEncryption], and [Writer.WriteTo] returns it for WithEncryption.
//
// ISO 19005-1, 6.5.3 and ISO 19005-2 to ISO 19005-4, 6.3.2 require annotation
// flags that print an annotation and show it on screen. An annotation that
// shows neither on screen nor in print, one whose Hidden flag is set or whose
// NoView flag is set with neither Print nor ToggleNoView (ISO 32000-1, Table
// 165), is removed rather than made visible: its pop-up and its replies go
// with it, a widget takes its form field when it was the field's only one, and
// a tagged document loses its structure element. [RemediationReport.Fixed]
// names each removal. A signed signature field keeps its widget. Every other
// annotation gets the flags those clauses require, so one that showed only in
// print now shows on screen too.
//
// It is the last edit on the Writer (see [Writer]): it judges the document the
// edits queued before it produce, and afterwards the Writer accepts
// [Writer.Sign], [Writer.WriteTo] and [Writer.Close] alone.
func (w *Writer) RemediatePDFA(ctx context.Context, profile models.PDFAProfile, opts ...RemediateOption) (_ *RemediationReport, err error) {
	defer recoverToError(w.logger(), "Writer.RemediatePDFA", &err)
	cfg, remOpts := remediateConfig(opts)
	revalidate := func(ctx context.Context, r *Reader) ([]error, error) {
		return r.ValidatePDFA(ctx, profile)
	}
	target := remediationTarget{method: "RemediatePDFA", pdfaClause: pdfaEncryptionClause(profile)}
	return w.remediate(ctx, target, cfg.tolerance, revalidate, func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
		return remediation.RemediatePDFA(ctx, doc, alloc, profile, remOpts.WithDisabled(disabled))
	})
}

// RemediatePDFUA validates the document against the given PDF/UA profile,
// applies targeted fixes, and queues the result for writing. The report
// describes what was fixed and what could not be fixed automatically; see
// [RemediationReport.Residual], which the output's own pdfuaid claim does not
// account for.
//
// Its output may be encrypted, but a PDF/UA-1 output only with permissions
// that set bit 10 of the P key, which ISO 14289-1, 7.16 requires of an
// encrypted conforming file, and [EncryptConfig.Permissions] of -1 or from
// [Permissions.Mask] set it. ISO 32000-1, Table 22 makes the bit the
// permission to extract text and graphics in support of accessibility. The
// remediation judges its output before it is encrypted, so rather than report
// a document the write would then break, it returns
// [ErrPDFUAForbidsBarringAccessibility] when [Writer.Encrypt], or the
// [WithEncryption] given to [NewWriter], cleared the bit before it, and
// [Writer.WriteTo] returns it for WithEncryption.
//
// It is the last edit on the Writer (see [Writer]): it judges the document the
// edits queued before it produce, and afterwards the Writer accepts
// [Writer.Sign], [Writer.WriteTo] and [Writer.Close] alone.
func (w *Writer) RemediatePDFUA(ctx context.Context, profile models.PDFUAProfile, opts ...RemediateOption) (_ *RemediationReport, err error) {
	defer recoverToError(w.logger(), "Writer.RemediatePDFUA", &err)
	cfg, remOpts := remediateConfig(opts)
	revalidate := func(ctx context.Context, r *Reader) ([]error, error) {
		return r.ValidatePDFUA(ctx, profile)
	}
	target := remediationTarget{method: "RemediatePDFUA", accessibilityClause: pdfuaAccessibilityClause(profile)}
	return w.remediate(ctx, target, cfg.tolerance, revalidate, func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
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
//
// It is the last edit on the Writer (see [Writer]): it judges the document the
// edits queued before it produce, and afterwards the Writer accepts
// [Writer.Sign], [Writer.WriteTo] and [Writer.Close] alone.
func (w *Writer) RemediateWTPDF(ctx context.Context, profile models.WTPDFProfile, opts ...RemediateOption) (_ *RemediationReport, err error) {
	defer recoverToError(w.logger(), "Writer.RemediateWTPDF", &err)
	cfg, remOpts := remediateConfig(opts)
	revalidate := func(ctx context.Context, r *Reader) ([]error, error) {
		return r.ValidateWTPDF(ctx, profile)
	}
	return w.remediate(ctx, remediationTarget{method: "RemediateWTPDF"}, cfg.tolerance, revalidate, func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
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
//
// It is the last edit on the Writer (see [Writer]): it judges the document the
// edits queued before it produce, and afterwards the Writer accepts
// [Writer.Sign], [Writer.WriteTo] and [Writer.Close] alone.
//
// Its output is a PDF/A-3 file, never encrypted (ISO 19005-3, 6.1.3): it
// returns [ErrPDFAForbidsEncryption] when [Writer.Encrypt] was called before
// it or [NewWriter] was given [WithEncryption], and [Writer.WriteTo] returns
// it for WithEncryption.
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
	target := remediationTarget{method: "RemediateFacturX", pdfaClause: pdfaEncryptionClause(profile)}
	return w.remediate(ctx, target, rcfg.tolerance, revalidate, func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
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
//
// It is the last edit on the Writer (see [Writer]): it judges the document the
// edits queued before it produce, and afterwards the Writer accepts
// [Writer.Sign], [Writer.WriteTo] and [Writer.Close] alone.
//
// A repaired invoice is a PDF/A-3 file, never encrypted (ISO 19005-3, 6.1.3):
// it returns [ErrPDFAForbidsEncryption] when [Writer.Encrypt] was called before
// it or [NewWriter] was given [WithEncryption], and [Writer.WriteTo] returns
// it for WithEncryption.
func (w *Writer) RepairFacturX(ctx context.Context, opts ...RemediateOption) (_ *RemediationReport, err error) {
	defer recoverToError(w.logger(), "Writer.RepairFacturX", &err)
	rcfg := remediateOpts{lang: "en", preserveContent: true, tolerance: DefaultRegressionTolerance}
	for _, o := range opts {
		o(&rcfg)
	}
	revalidate := func(ctx context.Context, r *Reader) ([]error, error) {
		return r.ValidateFacturX(ctx)
	}
	// A Factur-X invoice is a PDF/A-3 file, so the repaired one is never
	// encrypted either.
	target := remediationTarget{method: "RepairFacturX", pdfaClause: pdfaEncryptionClause(models.PDFA3b)}
	// The repair runs no content-tier fixer, so there is none to disable.
	return w.remediate(ctx, target, rcfg.tolerance, revalidate, func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ []string) (*remediation.Report, error) {
		fixes, repairErr := remediation.RepairHybridInvoice(ctx, doc, alloc)
		if repairErr != nil {
			return nil, repairErr
		}
		if fixes == nil {
			// No known invoice attachment, so nothing to repair: the run
			// fails, which writes nothing and leaves the Writer as it was,
			// open to edits. A later round runs on an output the first round
			// found the attachment in.
			return nil, ErrNoInvoiceAttachment
		}
		return &remediation.Report{Fixed: fixes}, nil
	})
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

// remediationTarget names what a remediation run is for: method, the Writer
// method, which the refusal of a later call names, pdfaClause, the ISO 19005
// clause that forbids encrypting the output, "" when the profile is not a
// PDF/A one, and accessibilityClause, the ISO 14289 clause that requires an
// encrypted output to permit extraction for accessibility, "" when the profile
// states none.
type remediationTarget struct {
	method              string
	pdfaClause          string
	accessibilityClause string
}

// pdfuaAccessibilityClause returns the clause of profile that requires an
// encrypted conforming file to set bit 10 of its P key, "" when it states
// none. ISO 14289-1, 7.16 does. The PDF/UA-2 profile holds no such rule. ISO
// 32000-2, Table 22 marks the bit "Not used", and an output declaring PDF 2.0
// sets it whatever the mask (see [EncryptConfig.Permissions]).
func pdfuaAccessibilityClause(profile models.PDFUAProfile) string {
	if profile == models.PDFUA1 {
		return "ISO 14289-1, 7.16"
	}
	return ""
}

// accessibilityPermission is bit 10 of the P key of a standard security
// handler, the permission to extract text and graphics in support of
// accessibility (ISO 32000-1, 7.6.3.2, Table 22).
const accessibilityPermission = crypto.AccessibilityPermission

// permitsAccessibility reports whether the permissions p of an encryption set
// [accessibilityPermission].
func permitsAccessibility(p int32) bool {
	return p&accessibilityPermission != 0
}

// pdfaEncryptionClause returns the clause of the ISO 19005 part of profile
// that forbids the Encrypt key in the trailer. Every part states it in 6.1.3:
// ISO 19005-1, 6.1.3, ISO 19005-2, 6.1.3, ISO 19005-3, 6.1.3 and ISO 19005-4,
// 6.1.3.
func pdfaEncryptionClause(profile models.PDFAProfile) string {
	part, _ := strings.CutPrefix(string(profile), "PDF/A-")
	if part == "" {
		return "ISO 19005, 6.1.3"
	}
	return "ISO 19005-" + part[:1] + ", 6.1.3"
}

// materialize writes the edits queued on the Writer to a temp file and makes
// that file the document the Writer edits (see [Writer.rebase]), so a
// remediation validates, repairs and measures the document the caller staged
// rather than the source. It does nothing when nothing is queued.
//
// The remediation validated the source while its fixers repaired the objects
// the queued edits had staged, so it neither saw what an edit introduced nor
// knew what an edit had removed, and its report said so: the transparency a
// stamp drawn at half opacity puts on a page, which ISO 19005-2, 6.2.10
// judges, went unseen, and a page deletion left the report claiming repairs
// to objects only the deleted page used. Its integrity baseline had needed
// the same document: `litho pipe f delete -pages 1 remediate -profile
// PDF/A-2b` once measured the source, reported "page count changed from 41 to
// 40", and rolled back a remediation that had done nothing wrong, and
// remediating a redacted document was refused for "dropping" the text Redact
// had removed.
//
// Encryption stays out of the file, as out of every file [Writer.writeTempFile]
// writes: it describes the output. A file that cannot be written fails with
// [ErrRemediationUnverified], as the remediation's own temp files do, and an
// edit that cannot be applied fails as [Writer.WriteTo] would fail on it.
//
// It returns undo, which a remediation that fails calls to put the Writer back
// as it was: the edits queued again, over the document they were queued
// against, so an edit queued after the failure names the pages it would have
// named before. A Writer materializes before its one remediation, when it
// still edits its source.
//
// # Corpus evidence
//
//   - All corpora: over 36 files sampled across govdocs, pdfjs, verapdf,
//     pdf20, pdfcpu and litho, a stamp, an attachment, a rotation, a page
//     deletion or a metadata edit then a PDF/A-2b or PDF/UA-1 remediation
//     (316 runs, 388 with the unedited control): every output validates to
//     the reported residual before and after, and no residual changes rule.
//   - All corpora: the fixes reported change in 55 of those runs, each toward
//     the edited document: the stamp's transparency gets its blending space
//     and its font a ToUnicode map, a PDF/UA-1 structure tree counts the pages
//     a deletion left, and the repairs of objects only the deleted page used
//     are no longer claimed.
//   - All corpora: with no edit queued, all 72 reports are unchanged.
func (w *Writer) materialize(ctx context.Context) (undo func(), err error) {
	if len(w.mutations) == 0 && len(w.rewriteOverrides) == 0 && !w.trailerDirty && !w.decrypt {
		return func() {}, nil
	}
	prevDropped := slices.Clone(w.droppedSignatures)
	path, err := w.writeTempFile(ctx, w, "litho-staged-*.pdf")
	if err != nil {
		w.droppedSignatures = prevDropped
		return nil, fmt.Errorf("writing the edited document to remediate: %w", err)
	}
	logger := w.reader.doc.Logger
	next, err := w.reader.reopenFile(ctx, path)
	if err != nil {
		w.droppedSignatures = prevDropped
		removeTempFile(logger, path)
		return nil, fmt.Errorf("reopening the edited document to remediate: %w", err)
	}
	prevReader, prevMutations, prevOverrides := w.reader, w.mutations, w.rewriteOverrides
	prevFolded, prevTrailerDirty := w.folded, w.trailerDirty
	if err := w.rebase(next, path); err != nil {
		logger.Debug("remediate: releasing the previous document failed", slog.Any("err", err))
	}
	return func() {
		base, basePath := w.reader, w.basePath
		w.reader, w.basePath = prevReader, ""
		w.mutations, w.rewriteOverrides = prevMutations, prevOverrides
		w.folded, w.trailerDirty = prevFolded, prevTrailerDirty
		w.droppedSignatures = prevDropped
		if err := releaseOwnedReader(base, basePath); err != nil {
			logger.Debug("remediate: releasing the edited document failed", slog.Any("err", err))
		}
	}, nil
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

// remediate runs one remediation engine for target against the document as
// the caller has staged it, written to a temp file first when anything is
// queued (see [Writer.materialize]), then gates the result: it serializes and
// reopens the output, revalidates it, and compares its integrity with the
// edited document's. A regression rolls back the content-tier fixers when they
// left a snapshot, and fails with [ErrRemediationBrokePDF], restoring the
// Writer, when there is none or the rollback does not recover the output. The
// result, rolled back or not, runs further rounds, up to maxRemediationRounds,
// while residuals remain and each round still fixes something (see
// [Writer.remediationRound]). When one is kept, the Writer writes its output
// from then on (see [Writer.rebase]). A temp file that any of these steps
// cannot write fails the run with [ErrRemediationUnverified], restoring the
// Writer.
//
// A run that succeeds ends the Writer's edits (see
// [Writer.refuseAfterRemediation]), and one on a Writer another run ended is
// refused. So is a run for a PDF/A profile after [Writer.Encrypt] or the
// [WithEncryption] given to [NewWriter], since ISO 19005 forbids encrypting its
// output, and one for PDF/UA-1 after an encryption asked for either way that
// bars extraction for accessibility, which ISO 14289-1, 7.16 forbids.
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
	target remediationTarget,
	tol RegressionTolerance,
	revalidate func(context.Context, *Reader) ([]error, error),
	run remediationRun,
) (*RemediationReport, error) {
	if w.closed {
		return nil, ErrWriterClosed
	}
	if err := w.refuseAfterRemediation(target.method); err != nil {
		return nil, err
	}
	if target.pdfaClause != "" && w.encryptCfg != nil {
		return nil, fmt.Errorf("%w (%s): %s before Writer.%s", ErrPDFAForbidsEncryption, target.pdfaClause, w.encryptBy, target.method)
	}
	// The probes the remediation revalidates are never encrypted (see
	// writeTempFile), so its report could not list the violation an encryption
	// that bars accessibility adds to the output.
	if target.accessibilityClause != "" && w.encryptCfg != nil && !permitsAccessibility(w.encryptCfg.Permissions) {
		return nil, fmt.Errorf("%w (%s): %s before Writer.%s", ErrPDFUAForbidsBarringAccessibility, target.accessibilityClause, w.encryptBy, target.method)
	}
	unmaterialize, err := w.materialize(ctx)
	if err != nil {
		return nil, err
	}
	// After materialize, so the page tree numbered is the one the engine
	// repairs: materialize writes the queued edits through writeToInternal,
	// which numbers the source's, and a write they left unchanged is a copy
	// of the source.
	if err := w.promotePageTree(ctx); err != nil {
		unmaterialize()
		return nil, err
	}
	doc := w.reader.doc
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	// A done ctx can stop the raw-byte augmentation short, and the allocator
	// then starts below objects the file defines.
	if err := ctx.Err(); err != nil {
		unmaterialize()
		return nil, fmt.Errorf("remediation canceled: %w", err)
	}

	baseline, baseErr := probeIntegrity(ctx, w.reader)
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
	// which otherwise persist on the document the Writer edits even when
	// remediation returns ErrRemediationBrokePDF. That document is the
	// Writer's own copy (see NewWriter), so the Reader it was built on never
	// sees them, failed run or not. Nothing is queued any more:
	// materialize wrote the queued edits into the document.
	preForceRewrite := w.forceRewrite
	preTrailerDirty := w.trailerDirty
	preRequiredVersion := w.requiredVersion
	preRewriteOverrides := w.rewriteOverrides
	preDropped := slices.Clone(w.droppedSignatures)
	preTrailerSnap := make(ast.PdfDict, len(doc.Index.Trailer))
	maps.Copy(preTrailerSnap, doc.Index.Trailer)
	restoreTrailer := func() {
		for k := range doc.Index.Trailer {
			delete(doc.Index.Trailer, k)
		}
		maps.Copy(doc.Index.Trailer, preTrailerSnap)
	}
	restorePreState := func() {
		w.forceRewrite = preForceRewrite
		w.trailerDirty = preTrailerDirty
		w.requiredVersion = preRequiredVersion
		w.rewriteOverrides = preRewriteOverrides
		w.droppedSignatures = preDropped
		restoreTrailer()
		unmaterialize()
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
	// file as fixed. The document is the Writer's own, so no other goroutine
	// reads it: on the caller's Reader, the clear also dropped the format
	// violations a validation running beside it had recorded.
	doc.ClearCaches()

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

	// attemptResult tells an output that reopens, probes and revalidates
	// from one that fails any of those, a regression candidate eligible for
	// rollback.
	type attemptResult int
	const (
		attemptOK attemptResult = iota
		attemptBroken
	)

	// serializeAndProbe writes the currently-queued mutations to a fresh
	// temp file, reopens it, and samples both the profile residuals and an
	// integrity snapshot. The caller must os.Remove the returned tmpPath on
	// every branch except the happy path, where tmpPath is kept as
	// w.preSerializedPath. A temp file that cannot be written leaves no file
	// and comes back as unverified, wrapping ErrRemediationUnverified.
	serializeAndProbe := func() (tmpPath string, residuals []error, post integritySnap, outcome attemptResult, unverified error) {
		// writeTempFile serializes through writeToInternal: this is a probe
		// write to a temp file, not the caller's final output, so it must not
		// trip the single-use guard nor the sign/full-rewrite check
		// (remediation may set forceRewrite legitimately).
		tmpPath, writeErr := w.writeTempFile(ctx, w, "litho-remediate-*.pdf")
		if errors.Is(writeErr, ErrRemediationUnverified) {
			return "", nil, integritySnap{}, attemptBroken, writeErr
		}
		if writeErr != nil {
			doc.Logger.Debug("remediate: pre-serialization failed, treating output as broken",
				slog.Any("err", writeErr))
			return "", nil, integritySnap{}, attemptBroken, nil
		}

		r2, openErr := w.reader.reopenFile(ctx, tmpPath)
		if openErr != nil {
			doc.Logger.Debug("remediate: re-open failed", slog.Any("err", openErr))
			return tmpPath, nil, integritySnap{}, attemptBroken, nil
		}
		defer r2.Close()

		probe, probeErr := probeIntegrity(ctx, r2)
		if probeErr != nil {
			doc.Logger.Debug("remediate: post probe failed", slog.Any("err", probeErr))
			return tmpPath, nil, integritySnap{}, attemptBroken, nil
		}
		res, valErr := revalidate(ctx, r2)
		if valErr != nil {
			doc.Logger.Debug("remediate: re-validation failed", slog.Any("err", valErr))
			return tmpPath, nil, integritySnap{}, attemptBroken, nil
		}
		return tmpPath, res, probe, attemptOK, nil
	}

	tmpPath, residual, post, outcome, unverified := serializeAndProbe()
	if err := ctx.Err(); err != nil {
		return cancelled(tmpPath, err)
	}
	if unverified != nil {
		restorePreState()
		return nil, unverified
	}

	// Treat a broken output and a regression identically: both trigger
	// rollback when a content-tier snapshot exists, and hard-fail
	// otherwise.
	var trigger string
	switch outcome {
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

		tmpPath2, residual2, post2, outcome2, unverified2 := serializeAndProbe()
		if err := ctx.Err(); err != nil {
			return cancelled(tmpPath2, err)
		}
		if unverified2 != nil {
			restorePreState()
			return nil, unverified2
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
	keptLater := false
	for round := 2; round <= maxRemediationRounds && len(residual) > 0; round++ {
		next, ok, err := w.remediationRound(ctx, tmpPath, run, disabled, revalidate, baseline, tol)
		if err != nil {
			removeTempFile(doc.Logger, tmpPath)
			restorePreState()
			return nil, err
		}
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
		w.droppedSignatures = appendDroppedSignatures(w.droppedSignatures, next.dropped...)
		fixed = append(fixed, next.fixed...)
		rolledBack = append(rolledBack, next.rolledBack...)
		disabled = append(disabled, next.disabled...)
		keptLater = true
	}
	// A cancellation inside a round ends the loop as a round that fixed
	// nothing does, so it is told apart here, before the output is kept.
	if err := ctx.Err(); err != nil {
		return cancelled(tmpPath, err)
	}
	if keptLater {
		// A later round's repairs exist only in its output, so the Writer
		// writes that output from here on (see [Writer.rebase]).
		next, err := w.reader.reopenFile(ctx, tmpPath)
		if err != nil {
			removeTempFile(doc.Logger, tmpPath)
			restorePreState()
			return nil, fmt.Errorf("reopening the remediation output: %w", err)
		}
		// The Writer no longer writes doc, so the repairs made to its
		// trailer in place are undone.
		restoreTrailer()
		if err := w.rebase(next, tmpPath); err != nil {
			doc.Logger.Debug("remediate: releasing the previous remediation output failed", slog.Any("err", err))
		}
	}
	w.markPreSerialized(tmpPath)
	w.remediation, w.pdfaClause, w.accessibilityClause = target.method, target.pdfaClause, target.accessibilityClause
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
	// dropped are the signatures the round's full rewrite left out, which a
	// first round written as an incremental update kept.
	dropped []DroppedSignature
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
//
// A temp file the round cannot write is returned as an error wrapping
// [ErrRemediationUnverified], since it says nothing about the round.
func (w *Writer) remediationRound(
	ctx context.Context,
	path string,
	run remediationRun,
	disabled []string,
	revalidate func(context.Context, *Reader) ([]error, error),
	baseline integritySnap,
	tol RegressionTolerance,
) (roundResult, bool, error) {
	logger := w.reader.doc.Logger

	r2, err := w.reader.reopenFile(ctx, path)
	if err != nil {
		logger.Debug("remediate: round reopen failed", slog.Any("err", err))
		return roundResult{}, false, nil
	}
	defer r2.Close()

	// The round repairs the output of the one before, which names every page
	// by reference unless it was a copy of a source that did not (see
	// [Writer.promotePageTree]). A done ctx stops the numbering as it stops
	// the round below.
	if err := r2.doc.PromoteDirectKids(ctx); err != nil {
		logger.Debug("remediate: numbering the round's pages written in /Kids failed", slog.Any("err", err))
		return roundResult{}, false, nil
	}
	alloc := writer.NewObjAllocator(r2.doc.HighestObjectNumber(ctx))
	// A done ctx can stop the raw-byte augmentation short, and the allocator
	// then starts below objects the file defines. The round ends as one that
	// fixed nothing, and the caller reports the cancellation after the loop.
	if ctx.Err() != nil {
		return roundResult{}, false, nil
	}
	rep, err := run(ctx, alloc, r2.doc, disabled)
	if err != nil || rep == nil || countRepairs(rep.Fixed) == 0 {
		return roundResult{}, false, nil
	}

	// w2 writes r2 itself, not a copy: the round's repairs to the trailer are
	// in r2, and the round is r2's only reader.
	w2 := newWriterOn(r2)
	w2.rewriteOverrides = alloc.Pending()
	w2.trailerDirty = rep.TrailerDirty
	w2.forceRewrite = rep.NeedsFullRewrite
	w2.requiredVersion = rep.RequiredVersion

	tmpPath, res, reason, err := w.writeRound(ctx, w2, revalidate, baseline, tol)
	if err != nil {
		return roundResult{}, false, err
	}
	if reason == "" {
		return roundResult{path: tmpPath, residual: res, fixed: rep.Fixed, dropped: w2.droppedSignatures}, true, nil
	}
	if rep.ContentSnapshot == nil {
		logger.Debug("remediate: round regressed, keeping previous output",
			slog.String("reason", reason))
		return roundResult{}, false, nil
	}

	// The allocator restores its pending map in place, which w2 writes from.
	alloc.Restore(*rep.ContentSnapshot)
	kept := keepFixedWithout(rep.Fixed, rep.ContentFixed)
	if countRepairs(kept) == 0 {
		return roundResult{}, false, nil
	}
	tmpPath, res, reason, err = w.writeRound(ctx, w2, revalidate, baseline, tol)
	if err != nil {
		return roundResult{}, false, err
	}
	if reason != "" {
		logger.Debug("remediate: round regressed after rollback, keeping previous output",
			slog.String("reason", reason))
		return roundResult{}, false, nil
	}
	return roundResult{
		path: tmpPath, residual: res, fixed: kept, rolledBack: rep.ContentFixed, disabled: rep.ContentFixers,
		dropped: w2.droppedSignatures,
	}, true, nil
}

// writeRound serializes the output of a remediation round w2 queues to a temp
// file, reopens it, and gates it as [Writer.remediate] gates the first round.
// It returns the temp file and its residual validation findings, or a reason
// the output was refused, after removing the file. A temp file that cannot be
// written is no verdict on the round: it is returned as an error wrapping
// [ErrRemediationUnverified].
func (w *Writer) writeRound(
	ctx context.Context,
	w2 *Writer,
	revalidate func(context.Context, *Reader) ([]error, error),
	baseline integritySnap,
	tol RegressionTolerance,
) (string, []error, string, error) {
	logger := w.reader.doc.Logger

	tmpPath, err := w.writeTempFile(ctx, w2, "litho-remediate-round-*.pdf")
	if errors.Is(err, ErrRemediationUnverified) {
		return "", nil, "", err
	}
	if err != nil {
		logger.Debug("remediate: round serialize failed", slog.Any("err", err))
		return "", nil, "serialize failed", nil
	}

	r3, err := w.reader.reopenFile(ctx, tmpPath)
	if err != nil {
		removeTempFile(logger, tmpPath)
		logger.Debug("remediate: round reopen of output failed", slog.Any("err", err))
		return "", nil, "output failed to reopen", nil
	}
	defer r3.Close()

	post, err := probeIntegrity(ctx, r3)
	if err != nil {
		removeTempFile(logger, tmpPath)
		return "", nil, "output failed the integrity probe", nil
	}
	if reason := evaluateRegression(baseline, post, tol); reason != "" {
		removeTempFile(logger, tmpPath)
		return "", nil, reason, nil
	}
	res, err := revalidate(ctx, r3)
	if err != nil {
		removeTempFile(logger, tmpPath)
		return "", nil, "output failed to revalidate", nil
	}
	return tmpPath, res, "", nil
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

// markPreSerialized records the remediation temp file WriteTo streams. No
// edit can follow the remediation, so only what WriteTo itself adds can make
// the file stale (see [Writer.writeToInternal]).
func (w *Writer) markPreSerialized(path string) {
	w.preSerializedPath = path
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
// The options given to [NewWriter] apply first, and opts after them, so an
// option in opts overrides the same setting given to NewWriter. Every refusal
// below reads the options of both.
//
// WriteTo drains the mutation queue into the Writer's copy of the
// document and leaves the [Reader] the Writer was built on as it was. It
// is single-use: after it produces output successfully it returns
// [ErrWriterConsumed] on every subsequent call. To produce another
// output, create a new Writer, on the same Reader if you like. After
// [Writer.Close] it returns [ErrWriterClosed].
//
// After a remediation, WriteTo streams the temp file the remediation wrote
// and removes it, whether the write succeeds or not. A signature, an
// encryption, or a write option that shapes the serialization is not in that
// file: WriteTo then removes it and serializes afresh. The one exception is
// the output of a remediation that kept a later round, which the Writer
// serializes from: a failed write keeps it for the retry, and it goes once a
// write succeeds, or at [Writer.Close].
//
// WriteTo writes nothing, and returns the refusal, when a call was refused
// after a remediation ([ErrEditAfterRemediation]), when [WithEncryption] asks
// to encrypt the output of a PDF/A remediation ([ErrPDFAForbidsEncryption]),
// and when it asks to encrypt the output of a PDF/UA-1 remediation with
// permissions that bar extraction for accessibility
// ([ErrPDFUAForbidsBarringAccessibility]).
//
// Signing ([Writer.Sign]) is incompatible with encryption and decryption
// ([Writer.Encrypt], [Writer.RemoveEncryption], [WithEncryption]); combining
// them returns [ErrSignFullRewriteIncompatible] instead of silently dropping
// the requested operation. A full rewrite is signed: [WithFullRewrite], an
// edit or a remediation whose output is one, and a source litho repaired on
// reading. WriteTo writes that output and signs it in an incremental update on
// top (see [Writer.Sign]).
//
// Signing an encrypted source returns the same error even when the caller
// asks for nothing else, after a remediation too, although the remediation
// works on a decrypted copy, and in a [Pipeline] after a Split or a Merge of
// an encrypted document, which the pipeline reopens in the clear: the
// signature of an encrypted document belongs in its encrypted bytes, which
// litho cannot write (see [Writer.Sign]). Encrypted-then-signed output is
// permitted by the specification but not yet implemented.
//
// After [Writer.Redact], WriteTo checks the output against every removed
// string before writing a byte, and returns [ErrRedactionRefused] or
// [ErrRedactionIncomplete] rather than write a file that still holds one.
//
// A full rewrite leaves out the signatures of the document it rewrites: each
// covers bytes the rewrite moves (ISO 32000-1, 12.8.1). The appearance of each
// signed widget is drawn into its page, and [Writer.DroppedSignatures] names
// what the output left out. An incremental update keeps every signature.
//
// A full rewrite writes the catalog, every page tree node and every page with
// the entries ISO 32000-1, 7.7.2 and 7.7.3 require of them, derived from the
// page tree the Reader reads, where the source lacks one or states a wrong
// one: a page no node gives a media box is written US Letter, the size readers
// assume for it. An incremental update writes them into the objects it writes,
// and leaves the others as the source has them.
func (w *Writer) WriteTo(ctx context.Context, dst io.Writer, opts ...WriteOption) (err error) {
	defer recoverToError(w.logger(), "Writer.WriteTo", &err)
	if w.closed {
		return ErrWriterClosed
	}
	if w.consumed {
		return ErrWriterConsumed
	}
	if w.refused != nil {
		return w.refused
	}
	// Every guard and every write below reads the options NewWriter was given
	// under the ones passed here: a guard that read opts alone let an option
	// given to NewWriter past it, and NewWriter's encryption was dropped.
	opts = w.writeOptions(opts)
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}
	// The guard below reads the encryption WithEncryption asks for as well as
	// w.encryptCfg, the one Encrypt or NewWriter asked for: reading only the
	// latter would let Sign + WithEncryption past it and silently drop one of
	// the two. It reads the encryption of a pipeline intermediate too, which
	// is written in the clear, since the pipeline's output owes it (see
	// inTheClear). The Writer takes it only once the guard passes, so a
	// refused write leaves the Writer as it was: it used to keep the
	// encryption, and a retry without the option was refused or encrypted all
	// the same.
	var encrypt *crypto.EncryptConfig
	if cfg.encrypt != nil {
		internal := cfg.encrypt.toInternal()
		encrypt = &internal
	}
	if encrypt != nil {
		if w.pdfaClause != "" {
			return fmt.Errorf("%w (%s): WithEncryption after Writer.%s", ErrPDFAForbidsEncryption, w.pdfaClause, w.remediation)
		}
		if w.accessibilityClause != "" && !permitsAccessibility(encrypt.Permissions) {
			return fmt.Errorf("%w (%s): WithEncryption after Writer.%s", ErrPDFUAForbidsBarringAccessibility, w.accessibilityClause, w.remediation)
		}
	}
	// Signing an encrypted source is also rejected (see signsEncrypted). A
	// full rewrite the caller, an edit or a remediation asks for is not: its
	// output is signed on top, see writeSignedRewrite.
	if w.signer != nil && (w.encryptCfg != nil || encrypt != nil || w.decrypt || w.signsEncrypted()) {
		return ErrSignFullRewriteIncompatible
	}
	if encrypt != nil {
		w.encryptCfg = encrypt
		w.encryptBy = "WithEncryption given to Writer.WriteTo"
		w.forceRewrite = true
	}
	// WithTempDir passed here places the temp files this write makes, a
	// rewrite to sign or pages an insertion reads back, instead of the
	// directory NewWriter or a Pipeline gave the Writer.
	if cfg.tempDir != "" && cfg.tempDir != w.tempDir {
		tempDir := w.tempDir
		w.tempDir = cfg.tempDir
		defer func() { w.tempDir = tempDir }()
	}
	// A pipeline intermediate is written in the clear once the guards above
	// have read the encryption the Writer was asked for (see inTheClear). The
	// full rewrite Encrypt forced stays, so an encrypted source is not passed
	// through as the ciphertext it is.
	if cfg.inTheClear && w.encryptCfg != nil {
		encryptCfg := w.encryptCfg
		w.encryptCfg = nil
		defer func() { w.encryptCfg = encryptCfg }()
	}
	write := w.writeToInternal
	if w.signer != nil && (cfg.fullRewrite || w.signsARewrite()) {
		write = w.writeSignedRewrite
	}
	// A failed write leaves the record as the remediation left it, since a
	// retry may write the output another way.
	dropped := slices.Clone(w.droppedSignatures)
	if err := write(ctx, dst, w.signer, opts...); err != nil {
		w.droppedSignatures = dropped
		return err
	}
	w.consumed = true
	if cfg.droppedSignatures != nil {
		*cfg.droppedSignatures = w.DroppedSignatures()
	}
	// Nothing reads the remediation output the Writer edited any more. A
	// failed write keeps it, since a retry serializes from it.
	if err := w.releaseBase(); err != nil {
		w.reader.doc.Logger.Debug("writer: releasing the remediation output failed", slog.Any("err", err))
	}
	return nil
}

// signsARewrite reports whether the output a signature goes on is a full
// rewrite of the Writer's own making: an edit or a remediation's repairs force
// one, or the source is damaged. [Writer.WriteTo] adds the one [WithFullRewrite]
// asks for, and refuses the ones an encryption or a decryption forces before
// asking.
func (w *Writer) signsARewrite() bool {
	return w.forceRewrite || w.sourceDamaged()
}

// signsEncrypted reports whether the document a signature is asked for is
// encrypted: the source the Writer was built on, or the document it edits. A
// source a [Pipeline] reopened after a Split or a Merge of an encrypted
// document counts (see [Reader.derivesFromEncrypted]).
//
// The signature of an encrypted document is written in that document. Its
// ByteRange covers the bytes of the file (ISO 32000-1, 12.8.1), and every
// string of an encrypted file is encrypted under the file's key (ISO 32000-1,
// 7.6.1), those of the signature dictionary included: only the signature value
// in its /Contents is exempt (ISO 32000-2, 7.6.2). litho has no encryptor bound
// to a source's security handler, so its incremental update would write those
// strings in the clear where a reader decrypts them into noise.
//
// The source decides, not the document the Writer edits. A remediation edits
// a decrypted copy when an edit was queued before it or when it keeps a later
// round, and the refusal that read that copy let a signature asked for on the
// encrypted document go on a document the caller never asked to decrypt,
// depending on what was queued and on how many rounds the remediation kept.
// A pipeline's Split and Merge stages edit a decrypted copy the same way, so
// the document a later stage's Writer is built on answers for the encrypted
// one it came from.
//
// # Corpus evidence
//
//   - All corpora: litho opens 86 encrypted files, 84 of which remediate at
//     PDF/A-2b and PDF/UA-1. With a metadata edit queued before the
//     remediation, all 168 of those remediations were signed, and without one
//     164 were refused and 2 signed: pdfjs issue5909_original.pdf and
//     issue7303.pdf at PDF/A-2b, whose remediation keeps a later round. All
//     336 are refused now.
func (w *Writer) signsEncrypted() bool {
	return w.source.derivesFromEncrypted() || w.reader.doc.IsEncrypted
}

// sourceDamaged reports whether the document the Writer edits is one the
// unsigned write rewrites in full as soon as it carries an edit.
//
// A cross-reference table litho rebuilt records offsets that are wrong, and a
// file with bytes before its header leaves readers disagreeing on what an
// offset counts from: ISO 32000-1, 7.5.2 puts the header first, and Acrobat
// counts from it where litho counts from the file start. An update appended to
// either carries the damage on, and a signature covers it.
func (w *Writer) sourceDamaged() bool {
	return w.reader.doc.Rebuilt || w.reader.doc.HeaderOffset > 0
}

// writeSignedRewrite writes the full rewrite the Writer queues to a temp file,
// unsigned, then signs that file in an incremental update on top, as a caller
// would by writing, reopening and signing. The signature covers every byte of
// the file but its own value (ISO 32000-1, 12.8.1), so it is written last, and
// a full rewrite renumbers every object, so the two cannot share one pass.
//
// After a remediation the unsigned write streams the output the remediation
// validated when nothing was queued since and the source needs no repair, so
// the signature goes on that very file. After an edit that forces a full
// rewrite, the unsigned write is the rewrite the same edits produce unsigned,
// byte for byte.
//
// It has the signature of [Writer.writeToInternal] so [Writer.WriteTo] can pick
// either; signer is never nil here.
//
// # Corpus evidence
//
//   - All corpora: of 110 files sampled across the seven and remediated at
//     PDF/A-2b, 58 could be signed while a remediation that rewrites the file
//     refused, and 109 are now. The one left, pdfjs empty_protected.pdf, is
//     an encrypted source. Every signed output validates like the unsigned
//     remediated output and holds one signature, whose ByteRange covers the
//     file but its /Contents and which poppler's pdfsig reports valid.
//   - All corpora: of 108 other files sampled across the seven, 378 writes
//     that deleted a page, inserted one, flattened the form, cleared the
//     metadata or redacted were refused signed, and all 378 are signed now,
//     on top of the unsigned output byte for byte. poppler's pdfsig reports
//     each new signature valid, and qpdf finds nothing in a signed output it
//     does not find in the unsigned one.
//   - All corpora: of 132 files sampled across the seven, every write
//     asking for [WithFullRewrite] was refused signed, and all 132 are signed
//     now, on top of the unsigned rewrite byte for byte. poppler's pdfsig
//     reports each new signature valid, and qpdf finds nothing in a signed
//     output it does not find in the unsigned one.
func (w *Writer) writeSignedRewrite(ctx context.Context, dst io.Writer, signer Signer, opts ...WriteOption) error {
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}
	rewrite := opts
	if w.sourceDamaged() {
		// The unsigned write rewrites a damaged source only when it carries an
		// edit, and the signature is the edit here.
		rewrite = append(append([]WriteOption(nil), opts...), WithFullRewrite())
	}

	f, err := w.createTemp("litho-sign-*.pdf")
	if err != nil {
		return fmt.Errorf("creating the temp file to sign: %w", err)
	}
	path := f.Name()
	logger := w.reader.doc.Logger
	defer removeTempFile(logger, path)
	writeErr := w.writeToInternal(ctx, f, nil, rewrite...)
	if closeErr := f.Close(); closeErr != nil && writeErr == nil {
		writeErr = fmt.Errorf("closing %s: %w", path, closeErr)
	}
	if writeErr != nil {
		return fmt.Errorf("writing the document to sign: %w", writeErr)
	}

	base, err := w.reader.reopenFile(ctx, path)
	if err != nil {
		return fmt.Errorf("reopening the document to sign: %w", err)
	}
	defer func() {
		if closeErr := base.Close(); closeErr != nil {
			logger.Debug("writer: closing the document to sign failed", slog.Any("err", closeErr))
		}
	}()
	// A rewrite litho reads back only by repairing it would carry the damage
	// into the signed file, which is what rewriting it first avoids.
	if base.doc.Rebuilt || base.doc.HeaderOffset > 0 {
		return fmt.Errorf("the rewritten document to sign does not read back as written")
	}
	if cfg.logger != nil {
		base.doc.Logger = cfg.logger
	}
	if err := mutations.WriteWithSignature(ctx, dst, base.doc, signerAdapter{inner: signer}, nil, nil, cfg.xrefStream); err != nil {
		return fmt.Errorf("signing the rewritten document: %w", err)
	}
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

// promotePageTree gives each page tree node the document the Writer edits
// writes directly in a /Kids array an object number of its own, and has the
// node listing it name it by reference (see
// [document.Document.PromoteDirectKids]), before anything reads the page tree.
// Every edit then finds each page at an object number, as ISO 32000-1,
// 7.7.3.2, Table 29 asks of /Kids, and writes it there: a stamp, a rotation or
// a repair of a direct page was dropped, and a page tree rebuilt from the
// pages' references listed each direct page as object 0. Each entry point that
// reads the document runs it first; it runs once per document.
//
// # Corpus evidence
//
//   - All corpora: four files write a page directly in /Kids, litho
//     kidsrepair_1b.pdf and kidsarray_2b.pdf and pdfjs issue9105_other.pdf
//     and issue9540.pdf.
//   - litho kidsrepair_1b.pdf: mutate-stamp left its two direct pages
//     unstamped, and mutate-pages wrote a tree listing them as object 0:
//     litho read one page of four, qpdf warned "Pages tree includes
//     non-dictionary object", veraPDF could not parse the output, and the
//     rules the two pages fail left litho's rule set (6.2.5 t1 from PDF/A-2
//     on, 6.2.8 t1 and 6.4 t3 at PDF/A-1b). Every mutate-* cell of the file
//     now passes.
//   - pdfjs issue9540.pdf: its PDF/UA-1 and PDF/UA-2 remediations tagged the
//     direct page's content under a structure element whose /Pg was 0 0 R,
//     and kept 7.1 t3 and 8.2.2 t1 on litho and veraPDF alike. Numbered, the
//     page is tagged and both outputs pass both tools. The other 44
//     remediations of it, kidsrepair_1b.pdf and issue9105_other.pdf come out
//     as before on both tools.
//   - litho kidsarray_2b.pdf: mutate-flatten left the widget on a direct page,
//     mutate-autotag tagged its two direct pages with /Pg 0 0 R, and the
//     remediations left a direct page's /AA and, at PDF/UA-1 and PDF/UA-2,
//     the two pages untagged and without /Tabs, on both tools. Every
//     mutate-* cell now passes but the redaction, which refuses by design.
func (w *Writer) promotePageTree(ctx context.Context) error {
	if err := w.reader.doc.PromoteDirectKids(ctx); err != nil {
		return fmt.Errorf("numbering the pages written in /Kids: %w", err)
	}
	return nil
}

// appendPromotedPageTree queues in alloc each object the promotion of doc's
// page tree staged (see [Writer.promotePageTree]) that no edit replaced. An
// incremental update keeps the file's bytes, where the nodes still hold their
// kids directly, and the edits name the promoted kids by number.
func appendPromotedPageTree(alloc *writer.ObjAllocator, doc *document.Document) {
	promoted := doc.PromotedObjects()
	for _, objNum := range slices.Sorted(maps.Keys(promoted)) {
		if _, edited := alloc.Pending()[objNum]; !edited {
			alloc.AllocAt(objNum, promoted[objNum])
		}
	}
}

// writeBufferSize is the buffer [Writer.writeToInternal] wraps its destination
// in. It is a throughput choice, not a limit: the serializer writes a few bytes
// at a time, so the buffer decides how many of those become one syscall. 64 KiB
// is the usual page-cache friendly size and bounds the extra memory at a fixed
// amount regardless of document size.
const writeBufferSize = 64 << 10

// writeToInternal performs the serialization, signed with signer when it is
// not nil. [Writer.WriteTo] wraps it with the single-use guard and the
// sign/full-rewrite compatibility check, and passes the Writer's signer.
// Remediation calls this directly, with no signer, so its internal probe
// writes do not trip the single-use guard before the caller's real WriteTo.
//
// # Corpus evidence
//
//   - govdocs: write syscalls were 30% of a remediate run against an
//     unbuffered destination, since each round serializes the document to a
//     temp file to revalidate it.
func (w *Writer) writeToInternal(ctx context.Context, dst io.Writer, signer Signer, opts ...WriteOption) (err error) {
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

	if err := w.promotePageTree(ctx); err != nil {
		return err
	}

	// Fast path: remediation already serialized the output to a temp file
	// for accurate residual validation. Stream from that file, but only
	// when it holds everything asked for: a signature, an encryption, or a
	// serialization-shaping write option is not in the temp file, and
	// streaming it would silently drop it. No edit can follow a remediation.
	// The remediation overrides persist in w.rewriteOverrides, so a fresh
	// serialization keeps the applied fixes.
	if w.preSerializedPath != "" {
		stale := w.encryptCfg != nil ||
			signer != nil ||
			cfg.fullRewrite || cfg.version != "" || cfg.xrefStream ||
			cfg.packObjectStreams || cfg.dedupStreams
		if !stale {
			return w.copyFromPreSerialized(ctx, dst)
		}
		w.reader.doc.Logger.Debug("writer: pre-serialized remediation output is stale, re-serializing")
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
	// signing path below is exempt: it is structurally incremental, so
	// Writer.WriteTo signs such a source through writeSignedRewrite, on top
	// of its rewrite, and rejects the unsafe sign combinations.
	hasEdits := len(w.mutations) > 0 || w.trailerDirty || len(w.rewriteOverrides) > 0 ||
		w.encryptCfg != nil || w.decrypt
	if hasEdits && signer == nil {
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
	// return. The document is the Writer's own (see NewWriter), and no Writer
	// method runs concurrently with another, so no goroutine reads the field
	// while it is swapped. Swapping it on the caller's Reader raced every
	// goroutine reading that Reader.
	if cfg.logger != nil {
		prev := w.reader.doc.Logger
		w.reader.doc.Logger = cfg.logger
		defer func() { w.reader.doc.Logger = prev }()
	}
	logger := w.reader.doc.Logger

	logger.Debug("writer.WriteTo started",
		slog.Int("mutations", len(w.mutations)),
		slog.Bool("full_rewrite", cfg.fullRewrite || w.forceRewrite),
		slog.Bool("signer", signer != nil),
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

	// Signing requires its own two-pass incremental flow. It carries the
	// overrides as the incremental path below does: after a remediation that
	// kept one round they hold every repair, and signing without them wrote
	// the source back with only the signature added. The mutations a
	// redaction's overrides already hold are not applied again (see
	// [Writer.folded]).
	if signer != nil {
		pending := w.unfoldedMutations()
		mutFuncs := make([]mutations.MutationFunc, len(pending))
		for i, m := range pending {
			mutFuncs[i] = m.apply
		}
		// The promoted page tree goes under the overrides, which build on it,
		// since the update names its pages by the numbers it gave them.
		overrides := w.reader.doc.PromotedObjects()
		if overrides == nil {
			overrides = w.rewriteOverrides
		} else {
			maps.Copy(overrides, w.rewriteOverrides)
		}
		return mutations.WriteWithSignature(ctx, dst, w.reader.doc, signerAdapter{inner: signer}, overrides, mutFuncs, cfg.xrefStream)
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
		if pending := w.unfoldedMutations(); len(pending) > 0 {
			mutAlloc := writer.NewObjAllocator(w.reader.doc.HighestObjectNumber(ctx))
			// A done ctx can stop the raw-byte augmentation short, and the allocator
			// then starts below objects the file defines.
			if err := ctx.Err(); err != nil {
				return err
			}
			// Seed with the redaction overrides so a mutation queued after a
			// redaction builds ON its edits rather than against the untouched
			// source. The page mutations read through latestPageDict, so the
			// seeded object is the one they extend.
			//
			// This replaces a blanket "the overrides win at the same object
			// number", which silently dropped whatever the caller asked for.
			// Mutations queued BEFORE the redaction are already folded into
			// those overrides by Writer.Redact, and are not applied again (see
			// [Writer.folded]). No mutation follows a remediation's overrides.
			for objNum, obj := range w.rewriteOverrides {
				mutAlloc.AllocAt(objNum, obj)
			}
			for i, m := range pending {
				if err := m.apply(ctx, mutAlloc, w.reader.doc); err != nil {
					return fmt.Errorf("applying mutation %d: %w", w.folded+i, err)
				}
			}
			if len(mutAlloc.Pending()) > 0 {
				overrides = mutAlloc.Pending()
			}
		}
		// A signature copied into a rewrite covers bytes it no longer holds.
		// The drawing goes in before the redaction check, which scans what is
		// written.
		baked, err := mutations.BakeSignaturesForRewrite(ctx, w.reader.doc, overrides)
		if err != nil {
			return fmt.Errorf("drawing the signatures into their pages: %w", err)
		}
		enforced, enforceErr := w.enforceRedactions(ctx, baked)
		if enforceErr != nil {
			return enforceErr
		}
		overrides = enforced
		if err := mutations.FullRewrite(ctx, dst, mutations.FullRewriteConfig{
			Doc:             w.reader.doc,
			EncryptCfg:      w.encryptCfg,
			Decrypt:         w.decrypt,
			Version:         cfg.version,
			RequiredVersion: w.requiredVersion,
			PageSources:     w.pageSources(),
			XRefStream:      cfg.xrefStream,
			Compact:         cfg.compactOptions(),
			Overrides:       overrides,
		}); err != nil {
			return err
		}
		w.recordDroppedSignatures(ctx)
		return nil
	}

	doc := w.reader.doc

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	// A done ctx can stop the raw-byte augmentation short, and the allocator
	// then starts below objects the file defines.
	if err := ctx.Err(); err != nil {
		return err
	}

	// Seed the overrides FIRST so a mutation queued after a redaction builds on
	// its edits instead of losing to them: the page mutations read through
	// latestPageDict, so the seeded object is the one they extend. Redaction
	// and remediation use source-namespace object numbers, which match this
	// allocator, so AllocAt with the same keys lands correctly.
	//
	// Seeding them last let them win at the same object number, which silently
	// dropped whatever the caller asked for. The full-rewrite branch above
	// orders it the same way for the same reason, and skips the same folded
	// mutations.
	for objNum, obj := range w.rewriteOverrides {
		alloc.AllocAt(objNum, obj)
	}

	for i, m := range w.unfoldedMutations() {
		if err := m.apply(ctx, alloc, doc); err != nil {
			return fmt.Errorf("applying mutation %d: %w", w.folded+i, err)
		}
		logger.Debug("mutation applied", slog.Int("index", w.folded+i))
	}

	// If no mutations produced pending objects AND the trailer was not
	// mutated in place, nothing has to be written. Otherwise the
	// incremental update below emits a fresh xref + trailer even when
	// the allocator is empty, preserving trailer-only fixes like /ID.
	if len(alloc.Pending()) == 0 && !w.trailerDirty {
		logger.Debug("no pending objects, copying source as-is")
		return copySourceCancellable(ctx, dst, doc.RawReader(), doc.FileSize())
	}
	appendPromotedPageTree(alloc, doc)
	// A page or node the update writes goes out with the entries ISO 32000-1
	// requires of it; the ones it does not write stay as the file has them.
	if err := mutations.CompleteWrittenPageTree(ctx, alloc, doc); err != nil {
		return err
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
//
// The form fields the extracted pages' widgets present stay fields of the
// output's form, but a signed signature field does not: its digest covers
// bytes of src the output does not hold (ISO 32000-1, 12.8.1). Its widget's
// appearance is drawn into the page content, as [Writer.FlattenForm] draws
// one, and the widget removed, so the page looks as it did. A widget a viewer
// does not display, or with no area as an invisible signature has, goes
// without drawing. An unsigned signature field stays a field that can be
// signed.
//
// Every page is written with the entries ISO 32000-1, 7.7.3.3, Table 30
// requires of a page, as a full rewrite writes them (see [Writer.WriteTo]).
//
// Split reads a copy of src's document, as a [Writer] edits one, so src is
// left as it was and other goroutines may read it meanwhile.
func Split(ctx context.Context, src *Reader, pages []int, dst io.Writer, opts ...WriteOption) (err error) {
	defer recoverToError(src.logger(), "pdf.Split", &err)
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}

	// The split reads a copy of src's document, so the logger it swaps in, and
	// whatever the copy caches, stays out of src, which other goroutines may be
	// reading. Swapping the logger on src itself raced every one of them.
	src = src.fork()
	if cfg.logger != nil {
		src.doc.Logger = cfg.logger
	}
	src.doc.Logger.Debug("split started",
		slog.Int("pages", len(pages)),
	)
	// A page the copy writes directly in /Kids is numbered there, so the split
	// reaches it as it reaches any other (see [Writer.promotePageTree]).
	if err := src.doc.PromoteDirectKids(ctx); err != nil {
		return fmt.Errorf("numbering the source pages written in /Kids: %w", err)
	}

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
	if cfg.droppedSignatures != nil {
		*cfg.droppedSignatures = droppedSignaturesOf(ctx, src.doc)
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
//
// Fields that share a fully qualified name across sources are one field of
// the output's form (ISO 32000-1, 12.7.3.2). A signed signature field is drawn
// into its page and leaves the form, as in [Split], and an unsigned one stays
// a field that can be signed. Every page is written with the entries ISO
// 32000-1, Table 30 requires of a page, as in [Split].
//
// Merge reads a copy of each source's document, as [Split] does, so the
// sources are left as they were and other goroutines may read them
// meanwhile. One Reader may appear more than once in sources.
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

	// The merge reads a copy of each source's document, for the reason Split
	// does. A Reader passed twice is copied twice, and each copy reads it
	// alone.
	forks := make([]*Reader, len(sources))
	for i, src := range sources {
		forks[i] = src.fork()
	}
	sources = forks
	if len(sources) > 0 && cfg.logger != nil {
		sources[0].doc.Logger = cfg.logger
	}
	if len(sources) > 0 {
		sources[0].doc.Logger.Debug("merge started",
			slog.Int("sources", len(sources)),
		)
	}

	mergeSources := make([]mutations.MergeSource, len(sources))
	for i, src := range sources {
		// As in Split, a page a copy writes directly in /Kids is numbered
		// there (see [Writer.promotePageTree]).
		if err := src.doc.PromoteDirectKids(ctx); err != nil {
			return fmt.Errorf("numbering the pages source %d writes in /Kids: %w", i, err)
		}
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
	if cfg.droppedSignatures != nil {
		docs := make([]*document.Document, len(sources))
		for i, src := range sources {
			docs[i] = src.doc
		}
		*cfg.droppedSignatures = droppedSignaturesOf(ctx, docs...)
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
