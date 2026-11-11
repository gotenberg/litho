package fixture

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
	// TrackedChanges is how a text document prints the changes it tracks:
	// "accepted", the default, "rejected" or "marked". litho's own option.
	TrackedChanges string `json:"trackedChanges,omitempty"`
	// Sheets names the sheets of a spreadsheet that print, and SheetNumbers
	// lists them by 1-based position; at most one of the two. HiddenSheets
	// prints the hidden sheets too. Default every sheet that is not hidden.
	// litho's own options.
	Sheets       []string `json:"sheets,omitempty"`
	SheetNumbers []int    `json:"sheetNumbers,omitempty"`
	HiddenSheets bool     `json:"hiddenSheets,omitempty"`
	// Recalculation is when a spreadsheet's formulas are computed again:
	// "asNeeded", the default, "never" or "always". litho's own option.
	Recalculation string `json:"recalculation,omitempty"`
	// CSVSeparators are the characters that end a field of a CSV file.
	// Default ",;\t", the three the references were made with. litho's own
	// option.
	CSVSeparators string `json:"csvSeparators,omitempty"`
	// CSVTypeDetection lays the fields of a CSV file that read as numbers,
	// dates and times out as such. Default true. litho's own option.
	CSVTypeDetection *bool `json:"csvTypeDetection,omitempty"`
	// Handouts prints a presentation as handout pages, that many slides a
	// page. Default 0: the slides. litho's own option.
	Handouts int `json:"handouts,omitempty"`
	// Date is the date of the conversion, in RFC 3339, which date and time
	// fields show. Default none. litho's own option.
	Date string `json:"date,omitempty"`
	// ReferenceOf names another fixture of the same corpus, by its ID,
	// whose reference stands for this source's: the diff cell scores this
	// source's conversion against that fixture's goldens, and no reference
	// is built for this one. It is no route field: Microsoft Graph converts
	// with no option, so an option fixture of an OOXML corpus borrows the
	// reference of a document that sets in itself what its options ask.
	// Default none.
	ReferenceOf string `json:"reference_of,omitempty"`
}

// OfficeOptionNames are the keys of an Office options file, the JSON names
// of [OfficeOptions].
//
//nolint:gochecknoglobals // static list of the schema's keys
var OfficeOptionNames = []string{"password", "landscape", "nativePages", "nativeBookmarks", "paperWidth", "paperHeight", "singlePage", "exportHiddenSlides", "exportNotesPages"}

// OfficeLithoOptionNames are the keys of an Office options file that are
// litho's own options: no reference engine has such a field and none
// receives them. A source that sets one is judged against the reference of
// the fixture its reference_of names, a document that is in itself what the
// option makes of this one.
//
//nolint:gochecknoglobals // static list of the schema's keys
var OfficeLithoOptionNames = []string{"trackedChanges", "sheets", "sheetNumbers", "hiddenSheets", "recalculation", "csvSeparators", "csvTypeDetection", "handouts", "date"}

// ReferenceOfName is the key of an Office options file that borrows another
// fixture's reference (see [OfficeOptions.ReferenceOf]): it is no route
// field, so it is not among [OfficeOptionNames].
const ReferenceOfName = "reference_of"

// officeFormats maps the extension of an Office document to the format it
// converts from, as the converter's extensions do: a flat ODT is an
// OpenDocument text document too, a macro-enabled workbook or a template a
// SpreadsheetML workbook, a flat ODS or a template an OpenDocument
// spreadsheet, a macro-enabled presentation, a template or a slide show a
// PresentationML presentation, and a flat ODP or a template an
// OpenDocument presentation. A Word, Excel or PowerPoint 97 to 2003 file
// converts from a format litho does not read yet, named by its extension,
// which [Office.Conversion] refuses with [ErrUnsupportedFormat]: its cell
// scores a convert error until litho reads it.
//
//nolint:gochecknoglobals // static extension table
var officeFormats = map[string]Format{
	".docx": FormatDOCX,
	".dotx": FormatDOCX,
	".odt":  FormatODT,
	".fodt": FormatODT,
	".xlsx": FormatXLSX,
	".xlsm": FormatXLSX,
	".xltx": FormatXLSX,
	".xltm": FormatXLSX,
	".ods":  FormatODS,
	".fods": FormatODS,
	".ots":  FormatODS,
	".csv":  FormatCSV,
	".pptx": FormatPPTX,
	".pptm": FormatPPTX,
	".potx": FormatPPTX,
	".ppsx": FormatPPTX,
	".odp":  FormatODP,
	".fodp": FormatODP,
	".otp":  FormatODP,
	".doc":  Format("doc"),
	".xls":  Format("xls"),
	".ppt":  Format("ppt"),
}

// Office is one Office source, read.
type Office struct {
	// Document is the path of the document converted.
	Document string
	// Format is the format the document is converted from, by its
	// extension.
	Format Format
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
	names := append(slices.Clone(OfficeOptionNames), OfficeLithoOptionNames...)
	if err := decodeStrict(data, append(names, ReferenceOfName), &o); err != nil {
		return OfficeOptions{}, err
	}
	return o, o.validate()
}

// validate checks the values against what the LibreOffice route accepts:
// a paper size needs both sides (the route answers 400 to one alone), each
// positive and finite. A borrowed reference names a fixture ID, without
// surrounding white space; whether that fixture exists is the corpus's to
// check.
func (o OfficeOptions) validate() error {
	if (o.PaperWidth == nil) != (o.PaperHeight == nil) {
		return errors.New("paperWidth and paperHeight: want both or neither")
	}
	for name, v := range map[string]*float64{"paperWidth": o.PaperWidth, "paperHeight": o.PaperHeight} {
		if v != nil && (*v <= 0 || math.IsInf(*v, 0) || math.IsNaN(*v)) {
			return fmt.Errorf("%s %v: want a positive size in inches", name, *v)
		}
	}
	if o.ReferenceOf != strings.TrimSpace(o.ReferenceOf) || strings.Contains(o.ReferenceOf, "\x00") {
		return fmt.Errorf("%s %q: want a fixture ID", ReferenceOfName, o.ReferenceOf)
	}
	if _, ok := trackedChangesViews[o.TrackedChanges]; !ok {
		return fmt.Errorf("trackedChanges %q: want accepted, rejected or marked", o.TrackedChanges)
	}
	if _, ok := recalculationModes[o.Recalculation]; !ok {
		return fmt.Errorf("recalculation %q: want asNeeded, never or always", o.Recalculation)
	}
	if o.Date != "" {
		if _, err := time.Parse(time.RFC3339, o.Date); err != nil {
			return fmt.Errorf("date %q: want an RFC 3339 time", o.Date)
		}
	}
	return nil
}

// trackedChangesViews maps the trackedChanges values to litho's views.
//
//nolint:gochecknoglobals // static table
var trackedChangesViews = map[string]pdf.TrackedChanges{
	"":         pdf.TrackedChangesAccepted,
	"accepted": pdf.TrackedChangesAccepted,
	"rejected": pdf.TrackedChangesRejected,
	"marked":   pdf.TrackedChangesMarked,
}

// recalculationModes maps the recalculation values to litho's modes.
//
//nolint:gochecknoglobals // static table
var recalculationModes = map[string]pdf.Recalculation{
	"":         pdf.RecalculateAsNeeded,
	"asNeeded": pdf.RecalculateAsNeeded,
	"never":    pdf.RecalculateNever,
	"always":   pdf.RecalculateAlways,
}

// ReferencePaperWidth and ReferencePaperHeight are the paper, in points,
// the references print a document on when it names none: US Letter, the
// paper of the LibreOffice image's locale and of the Microsoft account
// that builds the references, where litho's own default is A4.
const (
	ReferencePaperWidth  = 612.0
	ReferencePaperHeight = 792.0
)

// ReferencePaper is the paper of [ReferencePaperWidth] by
// [ReferencePaperHeight].
//
//nolint:gochecknoglobals // a value of two constants
var ReferencePaper = pdf.Paper{Width: ReferencePaperWidth, Height: ReferencePaperHeight}

// Conversion returns the conversion of src, the source's document, at the
// litho options of the source's fields, then extra: the outline switch
// always, the references' paper as the default paper of a text document or
// a spreadsheet (a presentation sizes its own slides), and the landscape
// switch, the page ranges, the password, the paper size, in points, the
// single page sheets, the hidden slides and the notes pages when the
// options name them. A CSV file splits its fields at commas, semicolons
// and tabs alike, as its references were made.
//
// A field the format has no option for is left out, as the route ignores
// it: a password for a CSV file, single page sheets for a text document or
// a presentation, the slide fields for any other document. Single page
// sheets set the paper and the landscape switch aside: the page takes the
// sheet's size. A format litho does not read fails with
// [ErrUnsupportedFormat].
func (o *Office) Conversion(src io.Reader, extra ...pdf.ConvertOption) (*pdf.Conversion, error) {
	opts := o.options()
	opts.common = append(opts.common, extra...)
	common := opts.common
	switch o.Format {
	case FormatDOCX, FormatODT:
		all := append(optionsAs[pdf.WordProcessingOption](common), optionsAs[pdf.WordProcessingOption](opts.office)...)
		all = append(all, opts.word...)
		if o.Format == FormatODT {
			return pdf.FromODT(src, all...), nil
		}
		return pdf.FromDOCX(src, all...), nil
	case FormatXLSX, FormatODS:
		all := append(optionsAs[pdf.SpreadsheetOption](common), optionsAs[pdf.SpreadsheetOption](opts.office)...)
		all = append(all, optionsAs[pdf.SpreadsheetOption](opts.sheet)...)
		all = append(all, opts.spreadsheet...)
		if o.Format == FormatODS {
			return pdf.FromODS(src, all...), nil
		}
		return pdf.FromXLSX(src, all...), nil
	case FormatCSV:
		all := append(optionsAs[pdf.CSVOption](common), optionsAs[pdf.CSVOption](opts.sheet)...)
		return pdf.FromCSV(src, append(all, opts.csv...)...), nil
	case FormatPPTX, FormatODP:
		all := append(optionsAs[pdf.PresentationOption](common), optionsAs[pdf.PresentationOption](opts.office)...)
		all = append(all, opts.slides...)
		if o.Format == FormatODP {
			return pdf.FromODP(src, all...), nil
		}
		return pdf.FromPPTX(src, all...), nil
	case FormatText, FormatHTML, FormatMarkdown:
	}
	return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, o.Format)
}

// officeOptions are the litho options of an Office source's fields, by the
// formats that take them.
type officeOptions struct {
	// common are the options every format takes.
	common []pdf.ConvertOption
	// office are the options of the Office packages: the password, which a
	// CSV file does not take.
	office []pdf.OfficeOption
	// sheet are the options of the sheet formats, nil for another format.
	sheet []pdf.SheetOption
	// slides are the options of the presentations, nil for another format.
	slides []pdf.PresentationOption
	// word are the options of the text documents, nil for another format.
	word []pdf.WordProcessingOption
	// spreadsheet are the options of the workbooks, nil for another format.
	spreadsheet []pdf.SpreadsheetOption
	// csv are the options of a CSV file, nil for another format.
	csv []pdf.CSVOption
}

// options returns the litho options of the source's fields (see
// [Office.Conversion]).
func (o *Office) options() officeOptions {
	var opts officeOptions
	opts.common = []pdf.ConvertOption{pdf.WithOutline(o.Options.NativeBookmarks == nil || *o.Options.NativeBookmarks)}
	if !isPresentation(o.Format) {
		opts.common = append(opts.common, pdf.WithDefaultPaper(ReferencePaper))
	}
	if o.Options.NativePages != "" {
		opts.common = append(opts.common, pdf.WithPageRanges(o.Options.NativePages))
	}
	onePagePerSheet := o.Options.SinglePage && isSheets(o.Format)
	if o.Options.Landscape && !onePagePerSheet {
		opts.common = append(opts.common, pdf.WithOrientation(pdf.OrientationLandscape))
	}
	if o.Options.PaperWidth != nil && o.Options.PaperHeight != nil && !onePagePerSheet {
		opts.common = append(opts.common, pdf.WithPaper(pdf.Paper{Width: *o.Options.PaperWidth * 72, Height: *o.Options.PaperHeight * 72}))
	}
	if o.Options.Password != "" {
		opts.office = append(opts.office, pdf.WithOfficePassword(o.Options.Password))
	}
	if onePagePerSheet {
		opts.sheet = append(opts.sheet, pdf.WithOnePagePerSheet(true))
	}
	if isPresentation(o.Format) {
		if o.Options.ExportHiddenSlides {
			opts.slides = append(opts.slides, pdf.WithHiddenSlides(true))
		}
		if o.Options.ExportNotesPages {
			opts.slides = append(opts.slides, pdf.WithNotesPages(true))
		}
		if o.Options.Handouts != 0 {
			opts.slides = append(opts.slides, pdf.WithHandouts(o.Options.Handouts))
		}
	}
	if o.Options.Date != "" {
		// validate checked the value.
		if t, err := time.Parse(time.RFC3339, o.Options.Date); err == nil {
			opts.common = append(opts.common, pdf.WithDate(t))
		}
	}
	switch o.Format {
	case FormatDOCX, FormatODT:
		if view := trackedChangesViews[o.Options.TrackedChanges]; view != pdf.TrackedChangesAccepted {
			opts.word = append(opts.word, pdf.WithTrackedChanges(view))
		}
	case FormatXLSX, FormatODS:
		if o.Options.Sheets != nil {
			opts.spreadsheet = append(opts.spreadsheet, pdf.WithSheets(o.Options.Sheets...))
		}
		if o.Options.SheetNumbers != nil {
			opts.spreadsheet = append(opts.spreadsheet, pdf.WithSheetNumbers(o.Options.SheetNumbers...))
		}
		if o.Options.HiddenSheets {
			opts.spreadsheet = append(opts.spreadsheet, pdf.WithHiddenSheets(true))
		}
		if mode := recalculationModes[o.Options.Recalculation]; mode != pdf.RecalculateAsNeeded {
			opts.spreadsheet = append(opts.spreadsheet, pdf.WithFormulaRecalculation(mode))
		}
	case FormatCSV:
		// The references were made by a route that splits at a comma, a
		// semicolon and a tab at once.
		separators := []rune(",;\t")
		if o.Options.CSVSeparators != "" {
			separators = []rune(o.Options.CSVSeparators)
		}
		opts.csv = append(opts.csv, pdf.WithCSVSeparators(separators...))
		if o.Options.CSVTypeDetection != nil && !*o.Options.CSVTypeDetection {
			opts.csv = append(opts.csv, pdf.WithCSVTypeDetection(false))
		}
	case FormatText, FormatHTML, FormatMarkdown, FormatPPTX, FormatODP:
	}
	return opts
}

// isPresentation reports whether a format is a presentation's, whose slides
// carry their own size.
func isPresentation(f Format) bool {
	return f == FormatPPTX || f == FormatODP || f == Format("ppt")
}

// isSheets reports whether a format is laid out as sheets, which
// [pdf.WithOnePagePerSheet] lays out a page each.
func isSheets(f Format) bool {
	return f == FormatXLSX || f == FormatODS || f == FormatCSV
}
