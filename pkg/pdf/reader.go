package pdf

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/annotation"
	"github.com/gotenberg/litho/pkg/internal/bookmarks"
	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/geospatial"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/internal/semantics/structure"
	"github.com/gotenberg/litho/pkg/internal/signature"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/plugins"
)

// OpenOption configures optional behavior when opening a PDF document via [Open] or [OpenFile].
// OpenOption closures must be safe to call from any goroutine.
type OpenOption func(*openOpts)

type openOpts struct {
	logger   *slog.Logger
	password string
	ocr      models.OCREngine
	filters  []plugins.FilterDecoder
}

// WithLogger injects a structured logger for debug-level diagnostic output.
// When set, the parsing engine emits detailed debug logs (font resolution,
// object resolution, text extraction, etc.) using structured fields.
// If not set, all debug output is silently discarded.
func WithLogger(logger *slog.Logger) OpenOption {
	return func(o *openOpts) {
		o.logger = logger
	}
}

// WithPassword supplies the password for an encrypted PDF document.
// If the document is not encrypted, the password is ignored.
func WithPassword(password string) OpenOption {
	return func(o *openOpts) {
		o.password = password
	}
}

// WithOCREngine injects an OCR backend (e.g., Tesseract, Google Cloud Vision).
// When set, [Reader.ExtractText] automatically hands off scanned image pages to the
// OCR engine instead of returning [ErrRequiresOCR].
func WithOCREngine(engine models.OCREngine) OpenOption {
	return func(o *openOpts) {
		o.ocr = engine
	}
}

// WithFilterPlugin registers a [plugins.FilterDecoder] plugin for stream
// decompression. Multiple plugins can be registered; each handles the
// filter name returned by its FilterName method.
//
// Example:
//
//	import "github.com/gotenberg/litho/pkg/plugins/jbig2"
//	reader, err := pdf.OpenFile(ctx, "doc.pdf", pdf.WithFilterPlugin(jbig2.New()))
func WithFilterPlugin(p plugins.FilterDecoder) OpenOption {
	return func(o *openOpts) {
		o.filters = append(o.filters, p)
	}
}

// Reader is the high-level API facade for the PDF parsing engine.
// When opened via [OpenFile], the Reader holds a reference to the underlying
// file; call [Reader.Close] to release it.
//
// Reader is safe for concurrent use by multiple goroutines. All extraction
// methods ([Reader.ExtractText], [Reader.Metadata], etc.) may be called
// concurrently on the same Reader. [Reader.Close] and [Reader.ClearCache]
// must not be called concurrently with other Reader methods.
type Reader struct {
	doc         *document.Document
	ocr         models.OCREngine
	closer      io.Closer // non-nil when opened via OpenFile
	pagesOnce   sync.Once
	cachedPages []*document.Page
	pagesErr    error
}

// Close releases any resources held by the [Reader]. When the Reader was
// created via [OpenFile], Close closes the underlying file. For Readers
// created via [Open], Close is a no-op.
func (r *Reader) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// ClearCache releases the in-memory object cache, font cache, and memoized
// page tree. This is useful in batch-processing scenarios where a single
// Reader is used to extract data from many pages sequentially and the caller
// wants to bound memory usage between pages. The caches are transparently
// rebuilt on the next access.
//
// ClearCache must not be called concurrently with other Reader methods.
func (r *Reader) ClearCache() {
	r.pagesOnce = sync.Once{} // zero value is a valid, unlocked Once
	r.cachedPages = nil
	r.pagesErr = nil
	r.doc.ClearCaches()
}

// ValidatePDFA audits the document against a specific PDF/A profile,
// returning all compliance violations found. An empty slice means the
// document passes.
func (r *Reader) ValidatePDFA(ctx context.Context, profile models.PDFAProfile) ([]error, error) {
	return compliance.ValidatePDFA(ctx, r.doc, profile)
}

// ValidatePDFUA audits the document against a specific PDF/UA profile,
// returning all compliance violations found. An empty slice means the
// document passes.
func (r *Reader) ValidatePDFUA(ctx context.Context, profile models.PDFUAProfile) ([]error, error) {
	return compliance.ValidatePDFUA(ctx, r.doc, profile)
}

// Open initializes a new [Reader] from an [io.ReaderAt] and its total size.
// Pass functional options (e.g., [WithLogger], [WithPassword]) to customize behavior.
func Open(ctx context.Context, r io.ReaderAt, size int64, opts ...OpenOption) (*Reader, error) {
	var o openOpts
	for _, opt := range opts {
		opt(&o)
	}

	doc, err := document.NewDocument(ctx, r, size, o.logger)
	if err != nil {
		switch {
		case errors.Is(err, document.ErrMissingHeader):
			return nil, ErrNotAPDF
		case errors.Is(err, document.ErrDamagedXRef):
			return nil, fmt.Errorf("%w: %v", ErrDamagedXRef, err)
		default:
			return nil, fmt.Errorf("failed to open PDF document: %w", err)
		}
	}

	if doc.IsEncrypted {
		if !doc.Authenticated {
			err = doc.Authenticate(o.password)
			if err != nil {
				if errors.Is(err, crypto.ErrInvalidPassword) {
					return nil, ErrInvalidPassword
				}
				if errors.Is(err, crypto.ErrUnsupportedEncryption) {
					return nil, fmt.Errorf("%w: %v", ErrUnsupportedEncryption, err)
				}
				return nil, fmt.Errorf("authentication failed: %w", err)
			}
			doc.Logger.Debug("password authentication succeeded")
		}
	}

	if len(o.filters) > 0 {
		doc.Filters = filter.NewDecoder(o.filters)
	}

	return &Reader{doc: doc, ocr: o.ocr}, nil
}

// OpenFile opens a PDF directly from the filesystem. The returned [Reader]
// holds a reference to the underlying file; the caller must call
// [Reader.Close] when done.
func OpenFile(ctx context.Context, path string, opts ...OpenOption) (*Reader, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	stat, err := f.Stat()
	if err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return nil, fmt.Errorf("failed to close file: %w", closeErr)
		}
		return nil, err
	}

	r, err := Open(ctx, f, stat.Size(), opts...)
	if err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return nil, fmt.Errorf("failed to close file: %w", closeErr)
		}
		return nil, err
	}
	r.closer = f
	return r, nil
}

// Version returns the PDF version string from the file header (e.g., "1.4", "2.0").
func (r *Reader) Version() string {
	return r.doc.Version
}

// IsEncrypted reports whether the document uses password-based encryption.
func (r *Reader) IsEncrypted() bool {
	return r.doc.IsEncrypted
}

// PageCount returns the total number of pages in the document.
func (r *Reader) PageCount(ctx context.Context) (int, error) {
	pages, err := r.resolvePages(ctx)
	if err != nil {
		return 0, err
	}
	return len(pages), nil
}

// ExtractTextOption configures the behavior of [Reader.ExtractText].
// ExtractTextOption closures must be safe to call from any goroutine.
type ExtractTextOption func(*extractTextOpts)

type extractTextOpts struct {
	includeHidden    bool
	includeInvisible bool
	includeClipped   bool
	includeArtifacts bool
}

// WithHiddenText includes all normally-stripped text: invisible, clipped,
// and artifact text. This is a shortcut that enables [WithInvisibleText],
// [WithClippedText], and [WithArtifacts] simultaneously.
func WithHiddenText() ExtractTextOption {
	return func(o *extractTextOpts) {
		o.includeHidden = true
	}
}

// WithInvisibleText includes text that uses rendering mode 3 (invisible)
// or white-on-white colors. Useful for forensic analysis of hidden content.
func WithInvisibleText() ExtractTextOption {
	return func(o *extractTextOpts) {
		o.includeInvisible = true
	}
}

// WithClippedText includes text that falls outside the page CropBox.
// Useful for detecting overset content or debugging layout issues.
func WithClippedText() ExtractTextOption {
	return func(o *extractTextOpts) {
		o.includeClipped = true
	}
}

// WithArtifacts includes text inside /Artifact marked content blocks
// (headers, footers, page numbers, watermarks) that is normally stripped.
func WithArtifacts() ExtractTextOption {
	return func(o *extractTextOpts) {
		o.includeArtifacts = true
	}
}

// ExtractText cleanly extracts text from a given 1-based page number.
// It orchestrates the content lexer, graphics state machine, unicode mapping,
// optional content layer rules, and spatial whitespace inference logic.
//
// By default, invisible text (rendering mode 3, white-on-white colors),
// text outside the clipping path and CropBox, and artifact text (headers,
// footers, page numbers) are silently dropped. Pass [WithHiddenText] to
// include all text regardless of visibility.
func (r *Reader) ExtractText(ctx context.Context, pageNum int, opts ...ExtractTextOption) (string, error) {
	var o extractTextOpts
	for _, opt := range opts {
		opt(&o)
	}

	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return "", err
	}

	// Extract raw content stream operations
	ops, err := content.ParsePageContent(ctx, r.doc, targetPage.Dict)
	if err != nil {
		return "", err
	}

	// Initialize the graphics state machine with the CropBox as the visible region.
	machine := graphics.NewMachine(boxToRect(targetPage.CropBox))

	// Build the extraction config. WithHiddenText disables all stripping;
	// the granular options override individual flags.
	config := typography.ExtractionConfig{
		StripInvisible: !o.includeHidden && !o.includeInvisible,
		StripHidden:    !o.includeHidden && !o.includeInvisible,
		StripClipped:   !o.includeHidden && !o.includeClipped,
		StripArtifacts: !o.includeHidden && !o.includeArtifacts,
	}

	text, err := typography.ExtractText(ctx, r.doc, machine, targetPage, ops, config)
	if err != nil {
		if errors.Is(err, typography.ErrFontNotFound) {
			return "", fmt.Errorf("page %d text extraction halted: %w", pageNum, ErrFontMissing)
		}
		return "", err
	}

	r.doc.Logger.Debug("text extracted",
		slog.Int("page", pageNum),
		slog.Int("text_length", len(text)),
		slog.Int("operators", len(ops)),
		slog.Bool("include_hidden", o.includeHidden),
	)

	// OCR triage: if no text was extracted, check whether the page looks
	// like a scanned image and either hand off to the OCR engine or return
	// the sentinel error.
	if strings.TrimSpace(text) == "" {
		pageW, pageH := mediaBoxDimensions(targetPage.MediaBox)
		images := pdfimage.ExtractImages(ctx, r.doc, targetPage)
		if heuristics.RequiresOCR(nil, images, pageW, pageH) {
			if r.ocr != nil {
				return r.runOCR(ctx, targetPage, images, pageNum)
			}
			return "", fmt.Errorf("page %d: %w", pageNum, ErrRequiresOCR)
		}
	}

	return text, nil
}

// ExtractAllText extracts and concatenates text from every page in the
// document, separated by newlines. This is a convenience wrapper around
// [Reader.ExtractText] for the common "give me all the text" use case.
func (r *Reader) ExtractAllText(ctx context.Context, opts ...ExtractTextOption) (string, error) {
	pages, err := r.resolvePages(ctx)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for i := range pages {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		text, err := r.ExtractText(ctx, i+1, opts...)
		if err != nil {
			return "", fmt.Errorf("page %d: %w", i+1, err)
		}
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(text)
	}
	return sb.String(), nil
}

// Permissions represents the document's permission flags as defined by
// ISO 32000-2, Table 22. For unencrypted documents, all flags are true.
// Values must not be shared across goroutines without synchronization.
type Permissions struct {
	Print             bool // Bit 3: Print the document.
	ModifyContent     bool // Bit 4: Modify document contents.
	ExtractText       bool // Bit 5: Copy or extract text and graphics.
	ModifyAnnotations bool // Bit 6: Add or modify annotations, fill form fields.
	FillForms         bool // Bit 9: Fill form fields (even if bit 6 is clear).
	Assemble          bool // Bit 11: Assemble the document.
	PrintHighQuality  bool // Bit 12: Print high-quality (bit 3 must also be set).
}

// Permissions returns the document's permission flags. For unencrypted
// documents, all permissions are granted.
func (r *Reader) Permissions() Permissions {
	if !r.IsEncrypted() {
		return Permissions{
			Print:             true,
			ModifyContent:     true,
			ExtractText:       true,
			ModifyAnnotations: true,
			FillForms:         true,
			Assemble:          true,
			PrintHighQuality:  true,
		}
	}
	p := r.doc.Crypto.PermissionFlags()
	return Permissions{
		Print:             p&4 != 0,
		ModifyContent:     p&8 != 0,
		ExtractText:       p&16 != 0,
		ModifyAnnotations: p&32 != 0,
		FillForms:         p&256 != 0,
		Assemble:          p&1024 != 0,
		PrintHighQuality:  p&2048 != 0,
	}
}

// XMPMetadata returns the raw XMP XML bytes from the catalog /Metadata stream.
// Returns nil if no XMP metadata is present. Use [Reader.Metadata] for parsed
// key-value metadata.
func (r *Reader) XMPMetadata(ctx context.Context) ([]byte, error) {
	return semantics.RawXMP(ctx, r.doc)
}

// Metadata extracts standard document-level metadata (Title, Author, etc.).
func (r *Reader) Metadata(ctx context.Context) (map[string]string, error) {
	return semantics.Metadata(ctx, r.doc)
}

// Bookmarks extracts the hierarchical outline (table of contents) from the document.
func (r *Reader) Bookmarks(ctx context.Context) ([]models.Bookmark, error) {
	return semantics.ResolveOutline(ctx, r.doc)
}

// BookmarkGenerationOptions controls auto-generated bookmark behavior.
// See [Reader.GenerateBookmarks] and [Writer.GenerateBookmarks].
// Values must not be shared across goroutines without synchronization.
type BookmarkGenerationOptions struct {
	// MaxDepth limits how many heading levels are included. Default 3.
	MaxDepth int
	// MinFontSizeRatio sets the minimum font size (relative to body text)
	// for an element to be considered a heading. Default 1.15 (15% larger).
	MinFontSizeRatio float64
	// SkipHeaderFooter filters out text that appears as a running header or
	// footer on many pages. Default true.
	SkipHeaderFooter bool
	// MaxTitleLength truncates long headings. Default 200.
	MaxTitleLength int
	// PreferStructureTree uses the PDF's structure tree (tagged PDFs) when
	// available, falling back to font heuristics otherwise. Default true.
	PreferStructureTree bool
	// DetectNumbering parses section number prefixes (1.1, A.1, IV, §2.3,
	// Chapter 5) to improve detection and override font-size depth with
	// the numbering depth. Default true.
	DetectNumbering bool
	// StripNumberingFromTitle removes the numeric prefix from generated
	// titles (e.g., "1.1 Overview" becomes "Overview"). Default false.
	StripNumberingFromTitle bool
	// HeadingScorer scores heading candidates. When nil, the default
	// heuristic scorer is used. Replace this for ML-based or custom
	// heading detection.
	HeadingScorer HeadingScorer
	// HeadingScoreThreshold is the minimum score for a candidate to be
	// included as a heading. Default 0.5.
	HeadingScoreThreshold float64
}

// HeadingScorer assigns a heading-likelihood score to a candidate line.
// Scores are in [0, 1]; values >= 0.5 are treated as headings.
// Implementations may use any algorithm (rule-based, statistical, ML).
//
// The interface is owned by the [pdf] package so callers never have to
// import an internal path. An internal adapter translates candidates
// from the engine's shape to [HeadingCandidate] before each Score call.
// Implementations must be safe for concurrent use.
type HeadingScorer interface {
	Score(candidate HeadingCandidate) float64
}

// HeadingScript classifies a candidate's dominant script. Scorers can
// use it to pick script-specific heuristics (for example, CJK headings
// have different length and punctuation conventions than Latin).
// HeadingScript values are immutable and safe for concurrent use.
type HeadingScript string

// Heading-script constants mirror the engine's detected script set.
const (
	HeadingScriptLatin      HeadingScript = "Latin"
	HeadingScriptCJK        HeadingScript = "CJK"
	HeadingScriptCyrillic   HeadingScript = "Cyrillic"
	HeadingScriptArabic     HeadingScript = "Arabic"
	HeadingScriptDevanagari HeadingScript = "Devanagari"
	HeadingScriptThai       HeadingScript = "Thai"
	HeadingScriptOther      HeadingScript = "Other"
)

// HeadingCandidate is the input passed to a [HeadingScorer]. It holds
// the reconstructed line text plus structural features that distinguish
// headings from body text.
// Values must not be shared across goroutines without synchronization.
type HeadingCandidate struct {
	// Text is the reconstructed line text.
	Text string
	// FontSize is the page-space font size of the line.
	FontSize float64
	// BodySize is the detected body text size for the document (or 0
	// if unknown).
	BodySize float64
	// PageNum is the 1-based page number where the line appears.
	PageNum int
	// CharCount is the number of characters in Text after normalization.
	CharCount int
	// IsBold indicates the line uses a bold font.
	IsBold bool
	// HasNumbering indicates the line starts with a section-number prefix.
	HasNumbering bool
	// NumberingDepth is the depth implied by the numbering (1 for "1",
	// 2 for "1.1", etc.). 0 if no numbering.
	NumberingDepth int
	// Script is the dominant script of the text.
	Script HeadingScript
}

// headingScorerAdapter wraps a public [HeadingScorer] so it satisfies
// the internal [bookmarks.HeadingScorer] interface. Each Score call
// translates the internal candidate shape to the public one, so the
// user's scorer never sees an internal type.
type headingScorerAdapter struct{ inner HeadingScorer }

func (a headingScorerAdapter) Score(c bookmarks.HeadingCandidate) float64 {
	return a.inner.Score(HeadingCandidate{
		Text:           c.Text,
		FontSize:       c.FontSize,
		BodySize:       c.BodySize,
		PageNum:        c.PageNum,
		CharCount:      c.CharCount,
		IsBold:         c.IsBold,
		HasNumbering:   c.HasNumbering,
		NumberingDepth: c.NumberingDepth,
		Script:         HeadingScript(bookmarks.ScriptName(c.Script)),
	})
}

// adaptHeadingScorer returns the internal scorer interface, or nil if
// the public scorer is nil so downstream callers fall back to the
// default heuristic.
func adaptHeadingScorer(s HeadingScorer) bookmarks.HeadingScorer {
	if s == nil {
		return nil
	}
	return headingScorerAdapter{inner: s}
}

// GenerateBookmarks analyzes the document content and returns an
// auto-generated bookmark tree based on heading detection. This is a
// read-only operation that does not modify the document; use
// [Writer.GenerateBookmarks] to apply the result.
//
// When the document has a structure tree and PreferStructureTree is true
// (default), bookmarks are extracted from tagged H1-H6 elements. Otherwise,
// bookmarks are inferred from font size relative to body text.
func (r *Reader) GenerateBookmarks(ctx context.Context, opts BookmarkGenerationOptions) ([]models.Bookmark, error) {
	input, err := r.buildBookmarkInput(ctx)
	if err != nil {
		return nil, err
	}
	return bookmarks.Generate(ctx, input, bookmarks.Options{
		MaxDepth:                opts.MaxDepth,
		MinFontSizeRatio:        opts.MinFontSizeRatio,
		SkipHeaderFooter:        opts.SkipHeaderFooter,
		MaxTitleLength:          opts.MaxTitleLength,
		PreferStructureTree:     opts.PreferStructureTree,
		DetectNumbering:         opts.DetectNumbering,
		StripNumberingFromTitle: opts.StripNumberingFromTitle,
		Scorer:                  adaptHeadingScorer(opts.HeadingScorer),
		ScoreThreshold:          opts.HeadingScoreThreshold,
	})
}

// buildBookmarkInput prepares the data needed by the bookmarks package:
// resolved pages, page ref → page number map, and per-page text elements.
func (r *Reader) buildBookmarkInput(ctx context.Context) (bookmarks.Input, error) {
	resolvedPages, err := r.resolvePages(ctx)
	if err != nil {
		return bookmarks.Input{}, err
	}

	pageRefToNum := make(map[ast.PdfIndirectRef]int, len(resolvedPages))
	pageTexts := make([]bookmarks.PageTextElements, 0, len(resolvedPages))

	for i, p := range resolvedPages {
		pageNum := i + 1
		pageRefToNum[p.Ref] = pageNum

		elements, err := r.ExtractTextElements(ctx, pageNum)
		if err != nil {
			// Skip pages that fail to extract; don't abort the whole operation.
			continue
		}
		pageTexts = append(pageTexts, bookmarks.PageTextElements{
			PageNum:  pageNum,
			Elements: elements,
		})
	}

	return bookmarks.Input{
		Doc:          r.doc,
		PageRefToNum: pageRefToNum,
		Pages:        pageTexts,
	}, nil
}

// FormData extracts user-typed values from interactive AcroForms as a
// simple name-to-value map. It automatically evaluates any PDF JavaScript
// calculation scripts. For richer field metadata, use [Reader.FormFields].
func (r *Reader) FormData(ctx context.Context) (map[string]string, error) {
	fields, err := annotation.ExtractFormData(ctx, r.doc)
	if err != nil {
		return nil, err
	}

	data := make(map[string]string)
	for _, f := range fields {
		data[f.Name] = f.Value
	}
	return data, nil
}

// FormFields extracts interactive AcroForm fields with full metadata
// (name, value, type, flags). It automatically evaluates any PDF JavaScript
// calculation scripts.
func (r *Reader) FormFields(ctx context.Context) ([]models.FormField, error) {
	return annotation.ExtractFormData(ctx, r.doc)
}

// ExtractTables detects tables on the given 1-based page number by analysing
// stroked line segments (physical grid) and, when no lines are found, by
// spatially clustering text elements into rows and inferring column boundaries
// from consistent X-gaps.
func (r *Reader) ExtractTables(ctx context.Context, pageNum int) ([]models.Table, error) {
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}

	ops, err := content.ParsePageContent(ctx, r.doc, targetPage.Dict)
	if err != nil {
		return nil, err
	}

	// Extract text elements via the graphics state machine.
	machine := graphics.NewMachine(boxToRect(targetPage.CropBox))
	elements, err := typography.ExtractElements(ctx, r.doc, machine, targetPage, ops)
	if err != nil {
		if errors.Is(err, typography.ErrFontNotFound) {
			return nil, fmt.Errorf("page %d table extraction halted: %w", pageNum, ErrFontMissing)
		}
		return nil, err
	}

	// Extract stroked line segments from the same content stream.
	lines := graphics.ExtractLines(ops)

	return heuristics.DetectTables(elements, lines)
}

// HasAcroForm reports whether the document has interactive form fields.
// Checks that the catalog /AcroForm dictionary has a non-empty /Fields array
// with at least one non-signature field.
func (r *Reader) HasAcroForm(ctx context.Context) bool {
	rootObj, ok := r.doc.Index.Trailer["Root"]
	if !ok {
		return false
	}
	catalog, err := document.ResolveDict(ctx, r.doc, rootObj)
	if err != nil || catalog == nil {
		return false
	}
	acroFormObj, ok := catalog["AcroForm"]
	if !ok {
		return false
	}
	acroForm, err := document.ResolveDict(ctx, r.doc, acroFormObj)
	if err != nil || acroForm == nil {
		return false
	}
	fieldsArr, err := document.ResolveArray(ctx, r.doc, acroForm["Fields"])
	return err == nil && len(fieldsArr) > 0
}

// IsPortfolio reports whether the document is a PDF Portfolio (Collection).
// Portfolio PDFs are containers whose visible pages are merely a cover sheet;
// the real payload is a set of embedded files.
func (r *Reader) IsPortfolio(ctx context.Context) (bool, error) {
	return semantics.IsPortfolio(ctx, r.doc)
}

// EmbeddedFiles returns all files from the document's /EmbeddedFiles name
// tree. Each item includes the original filename, MIME type, and a lazy Open
// accessor. Callers can recursively pass embedded PDFs back into pdf.Open.
func (r *Reader) EmbeddedFiles(ctx context.Context) ([]models.EmbeddedFile, error) {
	return semantics.ExtractPortfolioItems(ctx, r.doc)
}

// ExtractAnnotations extracts annotations from the given 1-based page number.
func (r *Reader) ExtractAnnotations(ctx context.Context, pageNum int) ([]models.Annotation, error) {
	pages, err := r.resolvePages(ctx)
	if err != nil {
		return nil, err
	}

	if pageNum < 1 || pageNum > len(pages) {
		return nil, fmt.Errorf("page number %d is out of bounds", pageNum)
	}

	pageMap := document.BuildPageMap(pages)
	return annotation.ExtractAnnotations(ctx, r.doc, pages[pageNum-1], pageMap)
}

// ExtractRedactedText extracts text elements from the given 1-based page number
// and flags any that fall inside unapplied /Redact annotations.
func (r *Reader) ExtractRedactedText(ctx context.Context, pageNum int) ([]models.TextElement, error) {
	pages, err := r.resolvePages(ctx)
	if err != nil {
		return nil, err
	}

	if pageNum < 1 || pageNum > len(pages) {
		return nil, fmt.Errorf("page number %d is out of bounds", pageNum)
	}

	targetPage := pages[pageNum-1]

	ops, err := content.ParsePageContent(ctx, r.doc, targetPage.Dict)
	if err != nil {
		return nil, err
	}

	machine := graphics.NewMachine(boxToRect(targetPage.CropBox))
	elements, err := typography.ExtractElements(ctx, r.doc, machine, targetPage, ops)
	if err != nil {
		if errors.Is(err, typography.ErrFontNotFound) {
			return nil, fmt.Errorf("page %d redaction detection halted: %w", pageNum, ErrFontMissing)
		}
		return nil, err
	}

	pageMap := document.BuildPageMap(pages)
	annots, err := annotation.ExtractAnnotations(ctx, r.doc, targetPage, pageMap)
	if err != nil {
		return nil, err
	}

	return heuristics.DetectUnappliedRedactions(elements, annots), nil
}

// IsLinearized reports whether the document is linearized (optimized for
// fast web view / byte-serving). Linearized PDFs place page 1 data at the
// beginning of the file so it can be rendered before the entire file is
// downloaded.
func (r *Reader) IsLinearized() bool {
	return r.doc.IsLinearized()
}

// LinearizationInfo returns the linearization parameters if the document is
// linearized. Returns nil for non-linearized documents.
func (r *Reader) LinearizationInfo() *models.LinearizationParams {
	lp := r.doc.Linearization
	if lp == nil {
		return nil
	}
	return &models.LinearizationParams{
		FileLength:          lp.FileLength,
		HintStreamOffset:    lp.HintStreamOffset,
		HintStreamLength:    lp.HintStreamLength,
		FirstPageObjNum:     lp.FirstPageObjNum,
		NumPages:            lp.NumPages,
		FirstPageEndOffset:  lp.FirstPageEndOffset,
		FirstPageXRefOffset: lp.FirstPageXRefOffset,
	}
}

// HintTable returns the per-page offset entries decoded from the hint stream
// of a linearized PDF. Returns nil for non-linearized documents or if the
// hint stream could not be parsed.
func (r *Reader) HintTable() []models.PageOffsetEntry {
	ht := r.doc.HintTable
	if ht == nil {
		return nil
	}
	result := make([]models.PageOffsetEntry, len(ht))
	for i, e := range ht {
		result[i] = models.PageOffsetEntry{
			ObjCount:            e.ObjCount,
			PageLength:          e.PageLength,
			ContentStreamOffset: e.ContentStreamOffset,
		}
	}
	return result
}

// ExtractSignatures returns the digital signatures present in the document
// without performing chain validation.
func (r *Reader) ExtractSignatures(ctx context.Context) ([]models.Signature, error) {
	return signature.ExtractSignatures(ctx, r.doc)
}

// VerifySignatures validates every digital signature in the document by
// parsing the embedded PKCS#7/CMS block, extracting the X.509 signer
// certificates, and verifying the chain against the provided trusted root
// certificate pool. Pass nil for roots to use the system root certificates.
//
// When a Document Security Store (DSS) is present, embedded OCSP responses,
// CRLs, and RFC 3161 timestamp tokens are used for Long-Term Validation
// (LTV), enabling offline verification of signatures with expired certificates.
func (r *Reader) VerifySignatures(ctx context.Context, roots *x509.CertPool) ([]models.SignatureValidation, error) {
	return signature.VerifySignatures(ctx, r.doc, roots)
}

// DSSInfo returns a summary of the Document Security Store (/DSS)
// contents. The DSS holds the embedded validation materials (certificates,
// CRLs, OCSP responses) needed for Long-Term Validation. Returns nil if
// no DSS exists.
func (r *Reader) DSSInfo(ctx context.Context) (*models.DSSInfo, error) {
	return signature.ResolveDSSInfo(ctx, r.doc)
}

// ExtractGeoData extracts geospatial metadata from the given 1-based page
// number. It scans the page's /VP (Viewport) array and Image XObjects for
// /Measure dictionaries with /Subtype /GEO, returning tie points that map
// PDF coordinates to GPS (latitude/longitude).
func (r *Reader) ExtractGeoData(ctx context.Context, pageNum int) ([]models.GeoData, error) {
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	return geospatial.ExtractGeoData(ctx, r.doc, targetPage)
}

// ExtractMeasurements extracts 2D rectilinear measurement scales from the
// given 1-based page number. It scans the page's /VP (Viewport) array and
// Line/PolyLine annotations for /Measure dictionaries with /Subtype /RL,
// returning scale axes that map PDF coordinates to real-world units.
func (r *Reader) ExtractMeasurements(ctx context.Context, pageNum int) ([]models.Measurement, error) {
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	return annotation.ExtractMeasurements(ctx, r.doc, targetPage)
}

// Extract3DModels extracts 3D model streams (U3D or PRC) from /3D annotations
// on the given 1-based page number. Each returned Model3D carries a lazy Open
// accessor. The stream data is not decoded until the caller invokes Open.
// The caller must close the reader returned by Open.
func (r *Reader) Extract3DModels(ctx context.Context, pageNum int) ([]models.Model3D, error) {
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	return semantics.Extract3DModels(ctx, r.doc, targetPage)
}

// Page returns the geometry and metadata for the given 1-based page number.
// Width and Height are effective dimensions in PDF points, derived from the
// CropBox (the visible area) and accounting for /Rotate and /UserUnit.
func (r *Reader) Page(ctx context.Context, pageNum int) (models.Page, error) {
	p, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return models.Page{}, err
	}

	box := p.CropBox
	if box == nil {
		box = p.MediaBox
	}
	w, h := mediaBoxDimensions(box)

	// /UserUnit (PDF 1.6+): scale raw coordinates to standard points.
	if p.UserUnit > 0 && p.UserUnit != 1.0 {
		w *= p.UserUnit
		h *= p.UserUnit
	}

	// /Rotate swaps the effective dimensions for 90° and 270°.
	rot := p.Rotate % 360
	if rot < 0 {
		rot += 360
	}
	if rot == 90 || rot == 270 {
		w, h = h, w
	}

	return models.Page{
		PageNumber:  pageNum,
		Width:       w,
		Height:      h,
		Rotate:      p.Rotate,
		UserUnit:    p.UserUnit,
		IsLandscape: w > h,
		DPM:         p.DPM,
		MediaBox:    boxToRect(p.MediaBox),
		CropBox:     boxToRect(p.CropBox),
		BleedBox:    boxToRect(p.BleedBox),
		TrimBox:     boxToRect(p.TrimBox),
		ArtBox:      boxToRect(p.ArtBox),
	}, nil
}

// ExtractTextElements extracts spatially positioned text elements from
// the given 1-based page number.
func (r *Reader) ExtractTextElements(ctx context.Context, pageNum int) ([]models.TextElement, error) {
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}

	ops, err := content.ParsePageContent(ctx, r.doc, targetPage.Dict)
	if err != nil {
		return nil, err
	}

	machine := graphics.NewMachine(boxToRect(targetPage.CropBox))
	elements, err := typography.ExtractElements(ctx, r.doc, machine, targetPage, ops)
	if err != nil {
		if errors.Is(err, typography.ErrFontNotFound) {
			return nil, fmt.Errorf("page %d text element extraction halted: %w", pageNum, ErrFontMissing)
		}
		return nil, err
	}

	return elements, nil
}

// ExtractImages returns metadata for Image XObjects on the given 1-based
// page number without decoding pixel data.
func (r *Reader) ExtractImages(ctx context.Context, pageNum int) ([]models.ImageMeta, error) {
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	return pdfimage.ExtractImages(ctx, r.doc, targetPage), nil
}

// ExtractLines extracts stroked line segments from the content stream of
// the given 1-based page number.
func (r *Reader) ExtractLines(ctx context.Context, pageNum int) ([]models.Line, error) {
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}

	ops, err := content.ParsePageContent(ctx, r.doc, targetPage.Dict)
	if err != nil {
		return nil, err
	}

	return graphics.ExtractLines(ops), nil
}

// ExtractPronunciationLexicons extracts W3C PLS (Pronunciation Lexicon
// Specification) XML documents from the catalog's /PronunciationLexicon
// array (PDF 2.0). Returns nil if none are present.
func (r *Reader) ExtractPronunciationLexicons(ctx context.Context) ([]models.PronunciationLexicon, error) {
	return semantics.ExtractPronunciationLexicons(ctx, r.doc)
}

// ExtractFormulas walks the document structure tree and extracts MathML
// from /Formula elements that carry an /AF entry with /AFRelationship
// /Supplement and MIME type application/mathml+xml (PDF 2.0).
func (r *Reader) ExtractFormulas(ctx context.Context) ([]models.FormulaElement, error) {
	return structure.ExtractFormulas(ctx, r.doc)
}

// resolvePages retrieves the catalog and resolves all pages. The result is
// memoized after the first successful call since the document is read-only
// after Open. Safe for concurrent use.
func (r *Reader) resolvePages(ctx context.Context) ([]*document.Page, error) {
	r.pagesOnce.Do(func() {
		catalog, err := document.ResolveCatalog(ctx, r.doc)
		if err != nil {
			r.pagesErr = err
			return
		}
		r.cachedPages, r.pagesErr = document.ResolvePages(ctx, r.doc, catalog)
	})
	return r.cachedPages, r.pagesErr
}

// resolvePage resolves the page tree and returns the page at the given
// 1-based page number.
func (r *Reader) resolvePage(ctx context.Context, pageNum int) (*document.Page, error) {
	pages, err := r.resolvePages(ctx)
	if err != nil {
		return nil, err
	}
	if pageNum < 1 || pageNum > len(pages) {
		return nil, fmt.Errorf("page number %d is out of bounds", pageNum)
	}
	return pages[pageNum-1], nil
}

// mediaBoxDimensions extracts page width and height from a MediaBox array
// [llx lly urx ury].
func mediaBoxDimensions(mb ast.PdfArray) (float64, float64) {
	if len(mb) < 4 {
		return 0, 0
	}
	llx := document.Float(mb[0])
	lly := document.Float(mb[1])
	urx := document.Float(mb[2])
	ury := document.Float(mb[3])

	w := urx - llx
	h := ury - lly
	if w < 0 {
		w = -w
	}
	if h < 0 {
		h = -h
	}
	return w, h
}

// boxToRect converts a PDF rectangle array [llx, lly, urx, ury] to [4]float64.
func boxToRect(arr ast.PdfArray) [4]float64 {
	if len(arr) < 4 {
		return [4]float64{}
	}
	return [4]float64{document.Float(arr[0]), document.Float(arr[1]), document.Float(arr[2]), document.Float(arr[3])}
}

// runOCR extracts the largest image from the page, decodes it, and passes it
// to the injected OCR engine.
func (r *Reader) runOCR(ctx context.Context, page *document.Page, images []models.ImageMeta, pageNum int) (string, error) {
	// Find the largest image by pixel area.
	bestArea := 0
	bestName := ""
	for _, img := range images {
		area := img.Width * img.Height
		if area > bestArea {
			bestArea = area
			bestName = img.Filename
		}
	}
	if bestName == "" {
		return "", fmt.Errorf("page %d: %w", pageNum, ErrRequiresOCR)
	}

	// Decode the image stream.
	img, err := pdfimage.DecodeImage(ctx, r.doc, page, ast.PdfName(bestName))
	if err != nil {
		return "", fmt.Errorf("page %d: OCR image decode failed: %w", pageNum, err)
	}

	text, err := r.ocr.Extract(img)
	if err != nil {
		return "", fmt.Errorf("page %d: OCR engine failed: %w", pageNum, err)
	}
	return text, nil
}
