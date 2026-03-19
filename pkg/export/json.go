package export

import (
	"context"
	"encoding/json"
	"io"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// EmbeddedFileMeta is the JSON-serializable metadata for an embedded file.
// It excludes the lazy Open accessor; callers who need content should use
// models.EmbeddedFile.Open directly.
type EmbeddedFileMeta struct {
	Name     string `json:"name"`
	MIMEType string `json:"mimeType,omitempty"`
}

// Model3DMeta is the JSON-serializable metadata for a 3D model annotation.
// It excludes the lazy Open accessor.
type Model3DMeta struct {
	Name    string `json:"name"`
	Subtype string `json:"subtype"`
}

// ImageExportMeta is the JSON-serializable metadata for an image XObject.
// It excludes the lazy Open accessor.
type ImageExportMeta struct {
	Filename         string `json:"filename"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	ColorSpace       string `json:"colorSpace"`
	BitsPerComponent int    `json:"bitsPerComponent"`
	FilePath         string `json:"filePath,omitempty"`
}

// PageData holds all extracted data for a single page.
type PageData struct {
	Page         models.Page          `json:"page"`
	Text         string               `json:"text,omitempty"`
	TextElements []models.TextElement `json:"textElements,omitempty"`
	Annotations  []models.Annotation  `json:"annotations,omitempty"`
	Tables       []models.Table       `json:"tables,omitempty"`
	Images       []ImageExportMeta    `json:"images,omitempty"`
	Lines        []models.Line        `json:"lines,omitempty"`
	Measurements []models.Measurement `json:"measurements,omitempty"`
	GeoData      []models.GeoData     `json:"geoData,omitempty"`
	Models3D     []Model3DMeta        `json:"models3d,omitempty"`
}

// Document holds all extracted data for a complete PDF document.
type Document struct {
	Metadata      map[string]string             `json:"metadata,omitempty"`
	Bookmarks     []models.Bookmark             `json:"bookmarks,omitempty"`
	FormData      map[string]string             `json:"formData,omitempty"`
	Pages         []PageData                    `json:"pages"`
	Signatures    []models.SignatureValidation  `json:"signatures,omitempty"`
	DSS           *models.DSSInfo               `json:"dss,omitempty"`
	Lexicons      []models.PronunciationLexicon `json:"pronunciationLexicons,omitempty"`
	Formulas      []models.FormulaElement       `json:"formulas,omitempty"`
	EmbeddedFiles []EmbeddedFileMeta            `json:"embeddedFiles,omitempty"`
	Linearization *models.LinearizationParams   `json:"linearization,omitempty"`
	HintTable     []models.PageOffsetEntry      `json:"hintTable,omitempty"`
	IsPortfolio   bool                          `json:"isPortfolio,omitempty"`
	IsLinearized  bool                          `json:"isLinearized,omitempty"`
}

// Extract drives a full extraction from the Reader and returns a Document
// ready for serialization. Per-page extraction errors are silently skipped
// so that a single corrupt page does not prevent the rest from being exported.
func Extract(ctx context.Context, r *pdf.Reader) (*Document, error) {
	doc := &Document{}

	// Document-level metadata.
	if meta, err := r.GetMetadata(ctx); err == nil {
		doc.Metadata = meta
	}
	if bm, err := r.GetBookmarks(ctx); err == nil {
		doc.Bookmarks = bm
	}
	if fd, err := r.GetFormData(ctx); err == nil && len(fd) > 0 {
		doc.FormData = fd
	}
	if sigs, err := r.VerifySignatures(ctx, nil); err == nil {
		doc.Signatures = sigs
	}
	if dss, err := r.GetDSSInfo(ctx); err == nil {
		doc.DSS = dss
	}
	if lexicons, err := r.ExtractPronunciationLexicons(ctx); err == nil {
		doc.Lexicons = lexicons
	}
	if formulas, err := r.ExtractFormulas(ctx); err == nil {
		doc.Formulas = formulas
	}
	if isPortfolio, err := r.IsPortfolio(ctx); err == nil && isPortfolio {
		doc.IsPortfolio = true
		if items, err := r.ExtractPortfolioItems(ctx); err == nil {
			for _, item := range items {
				doc.EmbeddedFiles = append(doc.EmbeddedFiles, EmbeddedFileMeta{
					Name:     item.Name,
					MIMEType: item.MIMEType,
				})
			}
		}
	}

	doc.IsLinearized = r.IsLinearized()
	if doc.IsLinearized {
		doc.Linearization = r.LinearizationInfo()
		doc.HintTable = r.HintTable()
	}

	// Per-page extraction.
	numPages, err := r.NumPages(ctx)
	if err != nil {
		return nil, err
	}

	doc.Pages = make([]PageData, 0, numPages)
	for i := 1; i <= numPages; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		pd := PageData{}

		if page, err := r.GetPage(ctx, i); err == nil {
			pd.Page = page
		}
		if text, err := r.ExtractText(ctx, i); err == nil {
			pd.Text = text
		}
		if elems, err := r.ExtractTextElements(ctx, i); err == nil {
			pd.TextElements = elems
		}
		if annots, err := r.ExtractAnnotations(ctx, i); err == nil {
			pd.Annotations = annots
		}
		if tables, err := r.ExtractTables(ctx, i); err == nil {
			pd.Tables = tables
		}
		if images, err := r.InspectImages(ctx, i); err == nil {
			for _, img := range images {
				pd.Images = append(pd.Images, ImageExportMeta{
					Filename:         img.Filename,
					Width:            img.Width,
					Height:           img.Height,
					ColorSpace:       img.ColorSpace,
					BitsPerComponent: img.BitsPerComponent,
				})
			}
		}
		if lines, err := r.ExtractLines(ctx, i); err == nil {
			pd.Lines = lines
		}
		if measurements, err := r.ExtractMeasurements(ctx, i); err == nil {
			pd.Measurements = measurements
		}
		if geo, err := r.ExtractGeoData(ctx, i); err == nil {
			pd.GeoData = geo
		}
		if models3d, err := r.Extract3DModels(ctx, i); err == nil {
			for _, m := range models3d {
				pd.Models3D = append(pd.Models3D, Model3DMeta{
					Name:    m.Name,
					Subtype: m.Subtype,
				})
			}
		}

		doc.Pages = append(doc.Pages, pd)
	}

	return doc, nil
}

// JSON extracts all data from the Reader and writes it as formatted JSON to w.
func JSON(ctx context.Context, w io.Writer, r *pdf.Reader) error {
	doc, err := Extract(ctx, r)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

// JSONCompact extracts all data from the Reader and writes it as compact
// (non-indented) JSON to w.
func JSONCompact(ctx context.Context, w io.Writer, r *pdf.Reader) error {
	doc, err := Extract(ctx, r)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

// MarshalJSON extracts all data from the Reader and returns it as a
// formatted JSON byte slice.
func MarshalJSON(ctx context.Context, r *pdf.Reader) ([]byte, error) {
	doc, err := Extract(ctx, r)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(doc, "", "  ")
}

// MarshalJSONCompact extracts all data from the Reader and returns it as
// a compact JSON byte slice.
func MarshalJSONCompact(ctx context.Context, r *pdf.Reader) ([]byte, error) {
	doc, err := Extract(ctx, r)
	if err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}
