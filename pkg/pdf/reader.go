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

	"github.com/gotenberg/litho/pkg/internal/annotation"
	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/internal/semantics/structure"
	"github.com/gotenberg/litho/pkg/internal/signature"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// OpenOption configures optional behavior when opening a PDF document.
type OpenOption func(*openOpts)

type openOpts struct {
	logger   *slog.Logger
	password string
	ocr      models.OCREngine
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
// When set, ExtractText automatically hands off scanned image pages to the
// OCR engine instead of returning ErrRequiresOCR.
func WithOCREngine(engine models.OCREngine) OpenOption {
	return func(o *openOpts) {
		o.ocr = engine
	}
}

// Reader is the high-level API facade for the PDF parsing engine.
// When opened via OpenFile, the Reader holds a reference to the underlying
// file; call Close to release it.
type Reader struct {
	doc         *document.Document
	ocr         models.OCREngine
	closer      io.Closer        // non-nil when opened via OpenFile
	cachedPages []*document.Page // memoized page tree; set on first resolvePages call
}

// Close releases any resources held by the Reader. When the Reader was
// created via OpenFile, Close closes the underlying file. For Readers
// created via Open, Close is a no-op.
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
func (r *Reader) ClearCache() {
	r.cachedPages = nil
	r.doc.ClearCaches()
}

// ValidatePDFA audits the document against a specific PDF/A profile,
// returning all compliance violations found. An empty slice means the
// document passes.
func (r *Reader) ValidatePDFA(ctx context.Context, profile models.PDFAProfile) ([]error, error) {
	return compliance.ValidatePDFA(ctx, r.doc, profile), nil
}

// ValidatePDFUA audits the document against a specific PDF/UA profile,
// returning all compliance violations found. An empty slice means the
// document passes.
func (r *Reader) ValidatePDFUA(ctx context.Context, profile models.PDFUAProfile) ([]error, error) {
	return compliance.ValidatePDFUA(ctx, r.doc, profile), nil
}

// Open initializes a new Reader from an io.ReaderAt and its total size.
// Pass functional options (e.g., WithLogger, WithPassword) to customize behavior.
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

	return &Reader{doc: doc, ocr: o.ocr}, nil
}

// OpenFile opens a PDF directly from the filesystem. The returned Reader
// holds a reference to the underlying file; the caller must call
// Reader.Close when done.
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

// NumPages returns the total number of pages in the document.
func (r *Reader) NumPages(ctx context.Context) (int, error) {
	pages, err := r.resolvePages(ctx)
	if err != nil {
		return 0, err
	}
	return len(pages), nil
}

// ExtractTextOption configures the behavior of ExtractText.
type ExtractTextOption func(*extractTextOpts)

type extractTextOpts struct {
	includeHidden    bool
	includeInvisible bool
	includeClipped   bool
	includeArtifacts bool
}

// WithHiddenText includes all normally-stripped text: invisible, clipped,
// and artifact text. This is a shortcut that enables WithInvisibleText,
// WithClippedText, and WithArtifacts simultaneously.
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
// footers, page numbers) are silently dropped. Pass WithHiddenText() to
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
		images := pdfimage.InspectImages(ctx, r.doc, targetPage)
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
// ExtractText for the common "give me all the text" use case.
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

// GetMetadata extracts standard document-level metadata (Title, Author, etc.).
func (r *Reader) GetMetadata(ctx context.Context) (map[string]string, error) {
	return semantics.GetMetadata(ctx, r.doc)
}

// GetBookmarks extracts the hierarchical outline (table of contents) from the document.
func (r *Reader) GetBookmarks(ctx context.Context) ([]models.Bookmark, error) {
	return semantics.GetOutline(ctx, r.doc)
}

// GetFormData extracts user-typed values from interactive AcroForms as a
// simple name→value map. It automatically evaluates any PDF JavaScript
// calculation scripts. For richer field metadata, use GetFormFields.
func (r *Reader) GetFormData(ctx context.Context) (map[string]string, error) {
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

// GetFormFields extracts interactive AcroForm fields with full metadata
// (name, value, type, flags). It automatically evaluates any PDF JavaScript
// calculation scripts.
func (r *Reader) GetFormFields(ctx context.Context) ([]models.FormField, error) {
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

// IsPortfolio reports whether the document is a PDF Portfolio (Collection).
// Portfolio PDFs are containers whose visible pages are merely a cover sheet;
// the real payload is a set of embedded files.
func (r *Reader) IsPortfolio(ctx context.Context) (bool, error) {
	return semantics.IsPortfolio(ctx, r.doc)
}

// ExtractPortfolioItems returns every file embedded in a PDF Portfolio.
// Each item includes the original filename, MIME type, and a lazy Open
// accessor. Callers can recursively pass embedded PDFs back into pdf.Open.
func (r *Reader) ExtractPortfolioItems(ctx context.Context) ([]models.EmbeddedFile, error) {
	return semantics.ExtractPortfolioItems(ctx, r.doc)
}

// GetEmbeddedFiles returns all files from the document's /EmbeddedFiles
// name tree. Unlike ExtractPortfolioItems, this works on any PDF with
// embedded files, not just Portfolios.
func (r *Reader) GetEmbeddedFiles(ctx context.Context) ([]models.EmbeddedFile, error) {
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

// DetectRedactions extracts text elements from the given 1-based page number
// and flags any that fall inside unapplied /Redact annotations.
func (r *Reader) DetectRedactions(ctx context.Context, pageNum int) ([]models.TextElement, error) {
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

// GetDSSInfo returns a summary of the Document Security Store (/DSS)
// contents. The DSS holds the embedded validation materials (certificates,
// CRLs, OCSP responses) needed for Long-Term Validation. Returns nil if
// no DSS exists.
func (r *Reader) GetDSSInfo(ctx context.Context) (*models.DSSInfo, error) {
	return signature.GetDSSInfo(ctx, r.doc)
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
	return semantics.ExtractGeoData(ctx, r.doc, targetPage)
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
// accessor — the stream data is not decoded until the caller invokes Open.
// The caller must close the reader returned by Open.
func (r *Reader) Extract3DModels(ctx context.Context, pageNum int) ([]models.Model3D, error) {
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	return semantics.Extract3DModels(ctx, r.doc, targetPage)
}

// GetPage returns the geometry and metadata for the given 1-based page number.
func (r *Reader) GetPage(ctx context.Context, pageNum int) (models.Page, error) {
	p, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return models.Page{}, err
	}
	w, h := mediaBoxDimensions(p.MediaBox)
	return models.Page{
		PageNumber: pageNum,
		Width:      w,
		Height:     h,
		DPM:        p.DPM,
		MediaBox:   boxToRect(p.MediaBox),
		CropBox:    boxToRect(p.CropBox),
		BleedBox:   boxToRect(p.BleedBox),
		TrimBox:    boxToRect(p.TrimBox),
		ArtBox:     boxToRect(p.ArtBox),
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

// InspectImages returns metadata for Image XObjects on the given 1-based
// page number without decoding pixel data.
func (r *Reader) InspectImages(ctx context.Context, pageNum int) ([]models.ImageMeta, error) {
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	return pdfimage.InspectImages(ctx, r.doc, targetPage), nil
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
// memoized after the first call since the document is read-only after Open.
func (r *Reader) resolvePages(ctx context.Context) ([]*document.Page, error) {
	if r.cachedPages != nil {
		return r.cachedPages, nil
	}
	catalog, err := document.GetCatalog(ctx, r.doc)
	if err != nil {
		return nil, err
	}
	pages, err := document.GetPages(ctx, r.doc, catalog)
	if err != nil {
		return nil, err
	}
	r.cachedPages = pages
	return pages, nil
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
	llx := astFloat(mb[0])
	lly := astFloat(mb[1])
	urx := astFloat(mb[2])
	ury := astFloat(mb[3])

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
	return [4]float64{astFloat(arr[0]), astFloat(arr[1]), astFloat(arr[2]), astFloat(arr[3])}
}

// astFloat extracts a float64 from a PdfObject (PdfInt or PdfReal).
func astFloat(obj ast.PdfObject) float64 {
	switch v := obj.(type) {
	case ast.PdfReal:
		return float64(v)
	case ast.PdfInt:
		return float64(v)
	}
	return 0
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
