package fixture

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// OfficeOptions are the LibreOffice route's form fields an Office source is
// converted with, as options.json spells them. A field left out keeps the
// route's default, which every source without the file is converted with:
// the document's own page setup, every page, the outline written, the
// hidden slides of a presentation left out and no notes pages.
type OfficeOptions struct {
	// Password opens an encrypted document. Default none.
	Password string `json:"password,omitempty"`
	// Landscape lays every page out in landscape. Default false.
	Landscape bool `json:"landscape,omitempty"`
	// NativePages selects the pages written, as "1-5, 8". Default every
	// page.
	NativePages string `json:"nativePages,omitempty"`
	// NativeBookmarks writes the headings as the document outline. Default
	// true, the route's default.
	NativeBookmarks *bool `json:"nativeBookmarks,omitempty"`
	// PaperWidth and PaperHeight are the paper size in inches, both or
	// neither, as the route takes them. Left out: each section keeps its
	// own page size.
	PaperWidth  *float64 `json:"paperWidth,omitempty"`
	PaperHeight *float64 `json:"paperHeight,omitempty"`
	// SinglePage prints each sheet of a spreadsheet on one page the size
	// of its content. Default false: each sheet is cut into pages of its
	// paper. The route's former name, singlePageSheets, is refused.
	SinglePage bool `json:"singlePage,omitempty"`
	// ExportHiddenSlides prints the slides a presentation hides too.
	// Default false: a hidden slide is left out.
	ExportHiddenSlides bool `json:"exportHiddenSlides,omitempty"`
	// ExportNotesPages prints, after the slides, one notes page per slide
	// printed. Default false.
	ExportNotesPages bool `json:"exportNotesPages,omitempty"`
}

// OfficeOptionNames are the keys of an Office options file, the JSON names
// of [OfficeOptions].
//
//nolint:gochecknoglobals // static list of the schema's keys
var OfficeOptionNames = []string{"password", "landscape", "nativePages", "nativeBookmarks", "paperWidth", "paperHeight", "singlePage", "exportHiddenSlides", "exportNotesPages"}

// officeFormats maps the extension of an Office document to the format it
// converts from, as the converter's extensions do: a flat ODT is an
// OpenDocument text document too, a macro-enabled workbook or a template a
// SpreadsheetML workbook, a flat ODS or a template an OpenDocument
// spreadsheet, a macro-enabled presentation, a template or a slide show a
// PresentationML presentation, and a flat ODP or a template an
// OpenDocument presentation. A Word, Excel or PowerPoint 97 to 2003 file
// converts from a format litho does not read yet, named by its extension,
// which pdf.Convert refuses: its cell scores a convert error until it
// does.
//
//nolint:gochecknoglobals // static extension table
var officeFormats = map[string]pdf.Format{
	".docx": pdf.FormatDOCX,
	".dotx": pdf.FormatDOCX,
	".odt":  pdf.FormatODT,
	".fodt": pdf.FormatODT,
	".xlsx": pdf.FormatXLSX,
	".xlsm": pdf.FormatXLSX,
	".xltx": pdf.FormatXLSX,
	".xltm": pdf.FormatXLSX,
	".ods":  pdf.FormatODS,
	".fods": pdf.FormatODS,
	".ots":  pdf.FormatODS,
	".csv":  pdf.FormatCSV,
	".pptx": pdf.FormatPPTX,
	".pptm": pdf.FormatPPTX,
	".potx": pdf.FormatPPTX,
	".ppsx": pdf.FormatPPTX,
	".odp":  pdf.FormatODP,
	".fodp": pdf.FormatODP,
	".otp":  pdf.FormatODP,
	".doc":  pdf.Format("doc"),
	".xls":  pdf.Format("xls"),
	".ppt":  pdf.Format("ppt"),
}

// Office is one Office source, read.
type Office struct {
	// Document is the path of the document converted.
	Document string
	// Format is the format the document is converted from, by its
	// extension.
	Format pdf.Format
	// Options are the options read from options.json, zero when there is
	// none.
	Options OfficeOptions
}

// ErrNoOfficeDocument reports a source directory that does not hold exactly
// one Office document of an extension officeFormats lists, or a source file
// of another type.
var ErrNoOfficeDocument = errors.New("not one Office document (.docx, .dotx, .odt, .fodt, .xlsx, .xlsm, .xltx, .xltm, .ods, .fods, .ots, .csv, .pptx, .pptm, .potx, .ppsx, .odp, .fodp, .otp, .doc, .xls, .ppt)")

// LoadOffice reads an Office source: path is a source directory, holding
// one document and an optional options file and nothing else, or a document
// itself, which converts at the defaults. An options file that names an
// unknown field or a value the route refuses fails the load.
func LoadOffice(path string) (*Office, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		format, ok := officeFormats[strings.ToLower(filepath.Ext(path))]
		if !ok || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s: %w", path, ErrNoOfficeDocument)
		}
		return &Office{Document: path, Format: format}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", path, err)
	}
	o := &Office{}
	hasOptions := false
	for _, e := range entries {
		name := e.Name()
		if name == OptionsFile {
			hasOptions = true
			continue
		}
		format, ok := officeFormats[strings.ToLower(filepath.Ext(name))]
		if !ok || o.Document != "" || !isFile(filepath.Join(path, name)) {
			return nil, fmt.Errorf("%s: %w: %s", path, ErrNoOfficeDocument, name)
		}
		o.Document, o.Format = filepath.Join(path, name), format
	}
	if o.Document == "" {
		return nil, fmt.Errorf("%s: %w", path, ErrNoOfficeDocument)
	}
	if !hasOptions {
		return o, nil
	}
	data, err := os.ReadFile(filepath.Join(path, OptionsFile)) //nolint:gosec // committed corpus source
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", OptionsFile, err)
	}
	if o.Options, err = ParseOfficeOptions(data); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(path, OptionsFile), err)
	}
	return o, nil
}

// ParseOfficeOptions decodes an Office options file strictly: an unknown
// field, trailing data or a value the route refuses is an error.
func ParseOfficeOptions(data []byte) (OfficeOptions, error) {
	var o OfficeOptions
	if err := decodeStrict(data, OfficeOptionNames, &o); err != nil {
		return OfficeOptions{}, err
	}
	return o, o.validate()
}

// validate checks the values against what the LibreOffice route accepts:
// a paper size needs both sides (the route answers 400 to one alone), each
// positive and finite.
func (o OfficeOptions) validate() error {
	if (o.PaperWidth == nil) != (o.PaperHeight == nil) {
		return errors.New("paperWidth and paperHeight: want both or neither")
	}
	for name, v := range map[string]*float64{"paperWidth": o.PaperWidth, "paperHeight": o.PaperHeight} {
		if v != nil && (*v <= 0 || math.IsInf(*v, 0) || math.IsNaN(*v)) {
			return fmt.Errorf("%s %v: want a positive size in inches", name, *v)
		}
	}
	return nil
}

// ConvertOptions returns the litho options of the source's fields: the
// landscape and outline switches always, and the page ranges, the password,
// the paper size, in points, the single page sheets, the hidden slides and
// the notes pages when the options name them.
func (o *Office) ConvertOptions() []pdf.ConvertOption {
	opts := []pdf.ConvertOption{
		pdf.WithLandscape(o.Options.Landscape),
		pdf.WithOutline(o.Options.NativeBookmarks == nil || *o.Options.NativeBookmarks),
	}
	if o.Options.NativePages != "" {
		opts = append(opts, pdf.WithPageRanges(o.Options.NativePages))
	}
	if o.Options.Password != "" {
		opts = append(opts, pdf.WithConvertPassword(o.Options.Password))
	}
	if o.Options.PaperWidth != nil && o.Options.PaperHeight != nil {
		opts = append(opts, pdf.WithPaperSize(*o.Options.PaperWidth*72, *o.Options.PaperHeight*72))
	}
	if o.Options.SinglePage {
		opts = append(opts, pdf.WithSinglePage(true))
	}
	if o.Options.ExportHiddenSlides {
		opts = append(opts, pdf.WithHiddenSlides(true))
	}
	if o.Options.ExportNotesPages {
		opts = append(opts, pdf.WithNotesPages(true))
	}
	return opts
}
