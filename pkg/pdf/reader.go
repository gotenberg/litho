package pdf

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/interactive"
	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/internal/signature"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// Exported errors for granular error handling by consumers.
var (
	ErrInvalidPassword       = errors.New("invalid PDF password")
	ErrNotAPDF               = errors.New("not a valid PDF document (missing %PDF- header)")
	ErrDamagedXRef           = errors.New("damaged or missing cross-reference table")
	ErrUnsupportedEncryption = errors.New("unsupported encryption algorithm or revision")
	ErrFontMissing           = errors.New("required font is missing or failed to decode")
	ErrRequiresOCR           = errors.New("page contains no digital text and requires optical character recognition")
)

// Reader is the high-level API facade for the PDF parsing engine.
type Reader struct {
	doc *document.Document
	ocr models.OCREngine
}

// SetOCREngine injects an OCR backend (e.g., Tesseract, Google Cloud Vision).
// When set, ExtractText automatically hands off scanned image pages to the
// OCR engine instead of returning ErrRequiresOCR.
func (r *Reader) SetOCREngine(engine models.OCREngine) {
	r.ocr = engine
}

// Open initializes a new Reader from an io.ReaderAt and its total size.
func Open(r io.ReaderAt, size int64, password string) (*Reader, error) {
	doc, err := document.NewDocument(r, size)
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
			err = doc.Authenticate(password)
			if err != nil {
				if errors.Is(err, crypto.ErrInvalidPassword) {
					return nil, ErrInvalidPassword
				}
				if errors.Is(err, crypto.ErrUnsupportedEncryption) {
					return nil, fmt.Errorf("%w: %v", ErrUnsupportedEncryption, err)
				}
				return nil, fmt.Errorf("authentication failed: %w", err)
			}
		}
	}

	return &Reader{doc: doc}, nil
}

// OpenFile is a convenience method to open a PDF directly from the filesystem.
// The caller is responsible for closing the *os.File when done.
func OpenFile(path string, password string) (*Reader, *os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}

	stat, err := f.Stat()
	if err != nil {
		if err := f.Close(); err != nil {
			return nil, nil, fmt.Errorf("failed to close file: %w", err)
		}
		return nil, nil, err
	}

	r, err := Open(f, stat.Size(), password)
	if err != nil {
		if err := f.Close(); err != nil {
			return nil, nil, fmt.Errorf("failed to close file: %w", err)
		}
		return nil, nil, err
	}
	return r, f, nil
}

// NumPages returns the total number of pages in the document.
func (r *Reader) NumPages() (int, error) {
	catalog, err := document.GetCatalog(r.doc)
	if err != nil {
		return 0, err
	}

	pages, err := document.GetPages(r.doc, catalog)
	if err != nil {
		return 0, err
	}

	return len(pages), nil
}

// ExtractText cleanly extracts text from a given 1-based page number.
// It orchestrates the content lexer, graphics state machine, unicode mapping,
// optional content layer rules, and spatial whitespace inference logic.
func (r *Reader) ExtractText(pageNum int) (string, error) {
	catalog, err := document.GetCatalog(r.doc)
	if err != nil {
		return "", err
	}

	pages, err := document.GetPages(r.doc, catalog)
	if err != nil {
		return "", err
	}

	if pageNum < 1 || pageNum > len(pages) {
		return "", fmt.Errorf("page number %d is out of bounds", pageNum)
	}

	targetPage := pages[pageNum-1]

	// Extract raw content stream operations
	ops, err := content.ParsePageContent(r.doc, targetPage.Dict)
	if err != nil {
		return "", err
	}

	// Initialize the graphics state machine
	machine := graphics.NewMachine([4]float64{0, 0, 0, 0}) // Standard unbounded cropbox

	// Extract and orchestrate the text using spatial heuristics
	config := typography.ExtractionConfig{
		StripInvisible: true,
		StripHidden:    true,
		StripArtifacts: true,
	}

	text, err := typography.ExtractText(r.doc, machine, targetPage, ops, config)
	if err != nil {
		if errors.Is(err, typography.ErrFontNotFound) {
			return "", fmt.Errorf("page %d text extraction halted: %w", pageNum, ErrFontMissing)
		}
		return "", err
	}

	// OCR triage: if no text was extracted, check whether the page looks
	// like a scanned image and either hand off to the OCR engine or return
	// the sentinel error.
	if strings.TrimSpace(text) == "" {
		pageW, pageH := mediaBoxDimensions(targetPage.MediaBox)
		images := pdfimage.InspectImages(r.doc, targetPage)
		if heuristics.RequiresOCR(nil, images, pageW, pageH) {
			if r.ocr != nil {
				return r.runOCR(targetPage, images, pageNum)
			}
			return "", fmt.Errorf("page %d: %w", pageNum, ErrRequiresOCR)
		}
	}

	return text, nil
}

// GetMetadata extracts standard document-level metadata (Title, Author, etc.).
func (r *Reader) GetMetadata() (map[string]string, error) {
	return semantics.GetMetadata(r.doc)
}

// GetBookmarks extracts the hierarchical outline (table of contents) from the document.
func (r *Reader) GetBookmarks() ([]models.Bookmark, error) {
	return semantics.GetOutline(r.doc)
}

// GetFormData extracts user-typed values from interactive AcroForms.
// It automatically evaluates any PDF JavaScript calculation scripts.
func (r *Reader) GetFormData() (map[string]string, error) {
	fields, err := interactive.ExtractFormData(r.doc)
	if err != nil {
		return nil, err
	}

	data := make(map[string]string)
	for _, f := range fields {
		data[f.Name] = f.Value
	}
	return data, nil
}

// ExtractTables detects tables on the given 1-based page number by analysing
// stroked line segments (physical grid) and, when no lines are found, by
// spatially clustering text elements into rows and inferring column boundaries
// from consistent X-gaps.
func (r *Reader) ExtractTables(pageNum int) ([]models.Table, error) {
	catalog, err := document.GetCatalog(r.doc)
	if err != nil {
		return nil, err
	}

	pages, err := document.GetPages(r.doc, catalog)
	if err != nil {
		return nil, err
	}

	if pageNum < 1 || pageNum > len(pages) {
		return nil, fmt.Errorf("page number %d is out of bounds", pageNum)
	}

	targetPage := pages[pageNum-1]

	ops, err := content.ParsePageContent(r.doc, targetPage.Dict)
	if err != nil {
		return nil, err
	}

	// Extract text elements via the graphics state machine.
	machine := graphics.NewMachine([4]float64{0, 0, 0, 0})
	elements, err := typography.ExtractElements(r.doc, machine, targetPage, ops)
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
func (r *Reader) IsPortfolio() (bool, error) {
	return semantics.IsPortfolio(r.doc)
}

// ExtractPortfolioItems returns every file embedded in a PDF Portfolio.
// Each item includes the original filename, MIME type, and decoded byte data.
// Callers can recursively pass embedded PDFs back into pdf.Open via
// bytes.NewReader on the returned Data slice.
func (r *Reader) ExtractPortfolioItems() ([]models.EmbeddedFile, error) {
	return semantics.ExtractPortfolioItems(r.doc)
}

// GetAnnotations extracts annotations from the given 1-based page number.
func (r *Reader) GetAnnotations(pageNum int) ([]models.Annotation, error) {
	catalog, err := document.GetCatalog(r.doc)
	if err != nil {
		return nil, err
	}

	pages, err := document.GetPages(r.doc, catalog)
	if err != nil {
		return nil, err
	}

	if pageNum < 1 || pageNum > len(pages) {
		return nil, fmt.Errorf("page number %d is out of bounds", pageNum)
	}

	pageMap := document.BuildPageMap(pages)
	return interactive.ExtractAnnotations(r.doc, pages[pageNum-1], pageMap)
}

// DetectRedactions extracts text elements from the given 1-based page number
// and flags any that fall inside unapplied /Redact annotations.
func (r *Reader) DetectRedactions(pageNum int) ([]models.TextElement, error) {
	catalog, err := document.GetCatalog(r.doc)
	if err != nil {
		return nil, err
	}

	pages, err := document.GetPages(r.doc, catalog)
	if err != nil {
		return nil, err
	}

	if pageNum < 1 || pageNum > len(pages) {
		return nil, fmt.Errorf("page number %d is out of bounds", pageNum)
	}

	targetPage := pages[pageNum-1]

	ops, err := content.ParsePageContent(r.doc, targetPage.Dict)
	if err != nil {
		return nil, err
	}

	machine := graphics.NewMachine([4]float64{0, 0, 0, 0})
	elements, err := typography.ExtractElements(r.doc, machine, targetPage, ops)
	if err != nil {
		if errors.Is(err, typography.ErrFontNotFound) {
			return nil, fmt.Errorf("page %d redaction detection halted: %w", pageNum, ErrFontMissing)
		}
		return nil, err
	}

	pageMap := document.BuildPageMap(pages)
	annots, err := interactive.ExtractAnnotations(r.doc, targetPage, pageMap)
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
func (r *Reader) VerifySignatures(roots *x509.CertPool) ([]models.SignatureValidation, error) {
	return signature.VerifySignatures(r.doc, roots)
}

// GetDSSInfo returns a summary of the Document Security Store (/DSS)
// contents. The DSS holds the embedded validation materials (certificates,
// CRLs, OCSP responses) needed for Long-Term Validation. Returns nil if
// no DSS exists.
func (r *Reader) GetDSSInfo() (*models.DSSInfo, error) {
	return signature.GetDSSInfo(r.doc)
}

// ExtractGeoData extracts geospatial metadata from the given 1-based page
// number. It scans the page's /VP (Viewport) array and Image XObjects for
// /Measure dictionaries with /Subtype /GEO, returning tie points that map
// PDF coordinates to GPS (latitude/longitude).
func (r *Reader) ExtractGeoData(pageNum int) ([]models.GeoData, error) {
	catalog, err := document.GetCatalog(r.doc)
	if err != nil {
		return nil, err
	}

	pages, err := document.GetPages(r.doc, catalog)
	if err != nil {
		return nil, err
	}

	if pageNum < 1 || pageNum > len(pages) {
		return nil, fmt.Errorf("page number %d is out of bounds", pageNum)
	}

	return semantics.ExtractGeoData(r.doc, pages[pageNum-1])
}

// GeoDataToGeoJSON converts extracted geospatial data to a GeoJSON
// FeatureCollection string.
func GeoDataToGeoJSON(geo *models.GeoData) string {
	return semantics.GeoDataToGeoJSON(geo)
}

// Extract3DModels extracts 3D model streams (U3D or PRC) from /3D annotations
// on the given 1-based page number. If outputDir is non-empty, the decoded
// binary streams are saved to disk.
func (r *Reader) Extract3DModels(pageNum int, outputDir string) ([]models.Model3D, error) {
	catalog, err := document.GetCatalog(r.doc)
	if err != nil {
		return nil, err
	}

	pages, err := document.GetPages(r.doc, catalog)
	if err != nil {
		return nil, err
	}

	if pageNum < 1 || pageNum > len(pages) {
		return nil, fmt.Errorf("page number %d is out of bounds", pageNum)
	}

	return semantics.Extract3DModels(r.doc, pages[pageNum-1], outputDir)
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
func (r *Reader) runOCR(page *document.Page, images []models.ImageMeta, pageNum int) (string, error) {
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
	img, err := pdfimage.DecodeImage(r.doc, page, ast.PdfName(bestName))
	if err != nil {
		return "", fmt.Errorf("page %d: OCR image decode failed: %w", pageNum, err)
	}

	text, err := r.ocr.Extract(img)
	if err != nil {
		return "", fmt.Errorf("page %d: OCR engine failed: %w", pageNum, err)
	}
	return text, nil
}
