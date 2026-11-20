package fixture

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/models"
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
	// of its content, and a text document on one page sized to its
	// content (pdf.WithSinglePage). Default false: each sheet is cut into
	// pages of its paper, a text document laid out on its pages. The
	// route's former name, singlePageSheets, is refused. No engine prints
	// a text document on one page: on a DOCX or ODT source the key counts
	// as litho's own ([SinglePageName]).
	SinglePage bool `json:"singlePage,omitempty"`
	// ExportHiddenSlides prints the slides a presentation hides too.
	// Default false: a hidden slide is left out.
	ExportHiddenSlides bool `json:"exportHiddenSlides,omitempty"`
	// ExportNotesPages prints, after the slides, one notes page per slide
	// printed. Default false.
	ExportNotesPages bool `json:"exportNotesPages,omitempty"`
	// ExportFormFields writes the form controls of a text document as
	// interactive form fields. The route's default is true, so every
	// LibreOffice reference of a document with controls holds its fields;
	// Word prints none. Left out, a source converts as its reference engine
	// prints it: an ODT source, which LibreOffice prints, with its fields,
	// a DOCX source, which Word prints, without.
	ExportFormFields *bool `json:"exportFormFields,omitempty"`
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
	// InstalledFonts names directories of the fixture fonts the source is
	// converted with as installed fonts (see [Options.InstalledFonts]):
	// corpus references installs them in the Gotenberg container before
	// LibreOffice starts. Office for the web installs no font, so in a
	// format Microsoft's references judge the key counts as litho's own
	// ([Office.LithoOptions]). Default none.
	InstalledFonts []string `json:"installedFonts,omitempty"`
	// DerivedAltText converts with pdf.WithDerivedAltText. Default false.
	// It is litho's own option but draws nothing: the source keeps its
	// reference ([DerivedAltTextName]).
	DerivedAltText bool `json:"derivedAltText,omitempty"`
	// Comments is how a text document writes its comments:
	// "hidden", the default, or "annotations", pdf.WithComments. It is
	// litho's own option but draws nothing: the source keeps its reference
	// ([CommentsName]).
	Comments string `json:"comments,omitempty"`
	// Locale is the locale a spreadsheet or CSV source is formatted
	// under, a BCP 47 tag of the models Locale constants
	// (pdf.WithLocale). Default none, en-US. LibreOffice's references
	// are built under it: the container that converts the source runs
	// with LC_ALL set to the tag, which LibreOffice reads. Office for the
	// web takes no locale, so in a format Microsoft's references judge the
	// key counts as litho's own ([Office.LithoOptions]).
	Locale string `json:"locale,omitempty"`
}

// OfficeOptionNames are the keys of an Office options file, the JSON names
// of [OfficeOptions].
var OfficeOptionNames = []string{"password", "landscape", "nativePages", "nativeBookmarks", "paperWidth", "paperHeight", "singlePage", "exportHiddenSlides", "exportNotesPages", "exportFormFields"}

// OfficeLithoOptionNames are the keys of an Office options file that are
// litho's own options: no reference engine has such a field and none
// receives them. A source that sets one is judged against the reference of
// the fixture its reference_of names, a document that is in itself what the
// option makes of this one.
var OfficeLithoOptionNames = []string{"trackedChanges", "sheets", "sheetNumbers", "hiddenSheets", "recalculation", "csvSeparators", "csvTypeDetection", "handouts", "date"}

// SinglePageName is the key of an Office options file that asks for a
// single page ([OfficeOptions.SinglePage]): a route field for a
// spreadsheet, litho's own option for a text document, which no reference
// engine prints on one page, so [Office.LithoOptions] names it there.
const SinglePageName = "singlePage"

// LocaleName is the key of an Office options file that formats a
// spreadsheet or CSV source under a locale ([OfficeOptions.Locale]): no
// route field of the pinned image, which the reference builder applies to
// the container instead, so it is not among [OfficeOptionNames].
const LocaleName = "locale"

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
// scores a convert error until litho reads it. An image file converts
// through [pdf.FromImage], which tells its format from its bytes.
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
	".png":  FormatImage,
	".jpg":  FormatImage,
	".gif":  FormatImage,
	".webp": FormatImage,
	".bmp":  FormatImage,
	".tif":  FormatImage,
	".jp2":  FormatImage,
	".j2k":  FormatImage,
	".ico":  FormatImage,
	".svg":  FormatImage,
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
var ErrNoOfficeDocument = errors.New("not one Office document (.docx, .dotx, .odt, .fodt, .xlsx, .xlsm, .xltx, .xltm, .ods, .fods, .ots, .csv, .pptx, .pptm, .potx, .ppsx, .odp, .fodp, .otp, .doc, .xls, .ppt) or image file (.png, .jpg, .gif, .webp, .bmp, .tif, .jp2, .j2k, .ico, .svg)")

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
	if err := decodeStrict(data, append(names, ReferenceOfName, InstalledFontsName, DerivedAltTextName, CommentsName, LocaleName), &o); err != nil {
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
	if o.Locale != "" && !localePattern.MatchString(o.Locale) {
		return fmt.Errorf("%s %q: want a BCP 47 tag of the models Locale constants", LocaleName, o.Locale)
	}
	if _, ok := commentsViews[o.Comments]; !ok {
		return fmt.Errorf("%s %q: want hidden or annotations", CommentsName, o.Comments)
	}
	if _, ok := recalculationModes[o.Recalculation]; !ok {
		return fmt.Errorf("recalculation %q: want asNeeded, never or always", o.Recalculation)
	}
	if o.Date != "" {
		if _, err := time.Parse(time.RFC3339, o.Date); err != nil {
			return fmt.Errorf("date %q: want an RFC 3339 time", o.Date)
		}
	}
	return validateInstalledFonts(o.InstalledFonts)
}

// localePattern matches the shape of the tags of the models Locale
// constants: a language, a script, a region and a variant, the last three
// optional (sr-Latn-RS, ca-ES-valencia). A tag of that shape the constants
// lack fails the conversion with pdf.ErrInvalidConvertOptions.
var localePattern = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z][a-z]{3})?(-[A-Z]{2})?(-[a-z]{5,8})?$`)

// trackedChangesViews maps the trackedChanges values to litho's views.
var trackedChangesViews = map[string]models.TrackedChanges{
	"":         models.TrackedChangesAccepted,
	"accepted": models.TrackedChangesAccepted,
	"rejected": models.TrackedChangesRejected,
	"marked":   models.TrackedChangesMarked,
}

// commentsViews maps the comments values to litho's views.
var commentsViews = map[string]models.Comments{
	"":            models.CommentsHidden,
	"hidden":      models.CommentsHidden,
	"annotations": models.CommentsAnnotations,
}

// recalculationModes maps the recalculation values to litho's modes.
var recalculationModes = map[string]models.Recalculation{
	"":         models.RecalculateAsNeeded,
	"asNeeded": models.RecalculateAsNeeded,
	"never":    models.RecalculateNever,
	"always":   models.RecalculateAlways,
}

// ReferencePaperWidth and ReferencePaperHeight are the paper, in points,
// Microsoft's references print a document on when it names none: US
// Letter, the paper of the account that builds them, where litho's own
// default is A4. LibreOffice's references print such a document on A4
// (the pinned image leaves LibreOffice's locale empty), so a format it
// judges takes no default paper from here.
const (
	ReferencePaperWidth  = 612.0
	ReferencePaperHeight = 792.0
)

// ReferencePaper is the paper of [ReferencePaperWidth] by
// [ReferencePaperHeight].
var ReferencePaper = models.Paper{Width: ReferencePaperWidth, Height: ReferencePaperHeight}

// Conversion returns the conversion of src, the source's document, at the
// litho options of the source's fields, then extra: the outline switch
// always, the form fields of a text document as [OfficeOptions.ExportFormFields]
// says, the references' paper as the default paper of a text document or
// a spreadsheet (a presentation sizes its own slides), and the landscape
// switch, the page ranges, the password, the paper size, in points, the
// single page sheets or single page, the hidden slides and the notes pages
// when the options name them. A CSV file splits its fields at commas, semicolons
// and tabs alike, as its references were made.
//
// A field the format has no option for is left out, as the route ignores
// it: a password for a CSV file, a single page for a presentation, the
// slide fields for any other document. Single page sheets set the paper
// and the landscape switch aside: the page takes the sheet's size. A
// single page of a text document sets the page ranges aside: one page has
// no range to select. A format litho does not read fails with
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
	case FormatImage:
		return pdf.FromImage(src, optionsAs[pdf.ImageOption](common)...), nil
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
	// litho are the keys of [OfficeLithoOptionNames] the options above
	// apply, in schema order: those the source sets away from their default
	// and the format takes.
	litho []string
}

// options returns the litho options of the source's fields (see
// [Office.Conversion]).
func (o *Office) options() officeOptions {
	var opts officeOptions
	opts.common = []pdf.ConvertOption{pdf.WithOutline(o.Options.NativeBookmarks == nil || *o.Options.NativeBookmarks)}
	if !isPresentation(o.Format) && !judgedByLibreOffice(o.Format) && o.Format != FormatImage {
		opts.common = append(opts.common, pdf.WithDefaultPaper(ReferencePaper))
	}
	onePage := o.Options.SinglePage && isText(o.Format)
	if o.Options.NativePages != "" && !onePage {
		// One page has no range to select.
		opts.common = append(opts.common, pdf.WithPageRanges(o.Options.NativePages))
	}
	onePagePerSheet := o.Options.SinglePage && isSheets(o.Format)
	if o.Options.Landscape && !onePagePerSheet {
		opts.common = append(opts.common, pdf.WithOrientation(models.OrientationLandscape))
	}
	if o.Options.PaperWidth != nil && o.Options.PaperHeight != nil && !onePagePerSheet {
		opts.common = append(opts.common, pdf.WithPaper(models.Paper{Width: *o.Options.PaperWidth * 72, Height: *o.Options.PaperHeight * 72}))
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
	}
	switch o.Format {
	case FormatDOCX, FormatODT:
		if o.formFields() {
			opts.word = append(opts.word, pdf.WithFormFields(true))
		}
		if onePage {
			opts.word = append(opts.word, pdf.WithSinglePage(true))
		}
		if view := trackedChangesViews[o.Options.TrackedChanges]; view != models.TrackedChangesAccepted {
			opts.word = append(opts.word, pdf.WithTrackedChanges(view))
			opts.litho = append(opts.litho, "trackedChanges")
		}
		if view := commentsViews[o.Options.Comments]; view != models.CommentsHidden {
			// The comments draw nothing: the source keeps its reference.
			opts.word = append(opts.word, pdf.WithComments(view))
		}
	case FormatXLSX, FormatODS:
		if o.Options.Sheets != nil {
			opts.spreadsheet = append(opts.spreadsheet, pdf.WithSheets(o.Options.Sheets...))
			opts.litho = append(opts.litho, "sheets")
		}
		if o.Options.SheetNumbers != nil {
			opts.spreadsheet = append(opts.spreadsheet, pdf.WithSheetNumbers(o.Options.SheetNumbers...))
			opts.litho = append(opts.litho, "sheetNumbers")
		}
		if o.Options.HiddenSheets {
			opts.spreadsheet = append(opts.spreadsheet, pdf.WithHiddenSheets(true))
			opts.litho = append(opts.litho, "hiddenSheets")
		}
		if mode := recalculationModes[o.Options.Recalculation]; mode != models.RecalculateAsNeeded {
			opts.spreadsheet = append(opts.spreadsheet, pdf.WithFormulaRecalculation(mode))
			opts.litho = append(opts.litho, "recalculation")
		}
	case FormatCSV:
		separators := []rune(ReferenceCSVSeparators)
		if o.Options.CSVSeparators != "" {
			separators = []rune(o.Options.CSVSeparators)
		}
		opts.csv = append(opts.csv, pdf.WithCSVSeparators(separators...))
		if !sameRunes(separators, []rune(ReferenceCSVSeparators)) {
			opts.litho = append(opts.litho, "csvSeparators")
		}
		if o.Options.CSVTypeDetection != nil && !*o.Options.CSVTypeDetection {
			opts.csv = append(opts.csv, pdf.WithCSVTypeDetection(false))
			opts.litho = append(opts.litho, "csvTypeDetection")
		}
	case FormatPPTX, FormatODP:
		if o.Options.Handouts != 0 {
			opts.slides = append(opts.slides, pdf.WithHandouts(o.Options.Handouts))
			opts.litho = append(opts.litho, "handouts")
		}
	case FormatText, FormatHTML, FormatMarkdown, FormatImage:
	}
	if o.Options.Locale != "" && isSheets(o.Format) {
		opts.sheet = append(opts.sheet, pdf.WithLocale(models.Locale(o.Options.Locale)))
	}
	if o.Options.DerivedAltText {
		opts.common = append(opts.common, pdf.WithDerivedAltText(true))
	}
	if o.Options.Date != "" {
		// validate checked the value.
		if t, err := time.Parse(time.RFC3339, o.Options.Date); err == nil {
			opts.common = append(opts.common, pdf.WithDate(t))
			opts.litho = append(opts.litho, "date")
		}
	}
	return opts
}

// formFields reports whether a text document converts with its form fields:
// as its options say, else as its reference engine prints it (see
// [OfficeOptions.ExportFormFields]).
func (o *Office) formFields() bool {
	if o.Options.ExportFormFields != nil {
		return *o.Options.ExportFormFields
	}
	return o.Format == FormatODT
}

// ReferenceCSVSeparators are the characters the references of the CSV
// corpora split a field at: the route splits at a comma, a semicolon and a
// tab at once. A CSV source converts with them unless its options name
// others.
const ReferenceCSVSeparators = ",;\t"

// sameRunes reports whether a and b hold the same characters, in any order
// and counted once.
func sameRunes(a, b []rune) bool {
	set := func(rs []rune) []rune {
		out := slices.Clone(rs)
		slices.Sort(out)
		return slices.Compact(out)
	}
	return slices.Equal(set(a), set(b))
}

// LithoOptions returns the keys of [OfficeLithoOptionNames] the source's
// conversion applies, in the order that list names them: those
// its options set away from their default for a format that takes them. A
// key at its default value, or one the format has no option for, is left
// out, as the conversion leaves it out: "trackedChanges": "accepted",
// "recalculation": "asNeeded", "hiddenSheets": false, "csvTypeDetection":
// true, "handouts": 0 and csvSeparators holding a comma, a semicolon and a
// tab ([ReferenceCSVSeparators]), in any order. [SinglePageName] follows
// them when a text document asks for a single page, and
// [InstalledFontsName] when the source names installed fonts in a format
// Microsoft's references judge: Office for the web installs no font. A
// source that names none converts as its reference engine prints it.
func (o *Office) LithoOptions() []string {
	keys := o.options().litho
	if o.Options.SinglePage && isText(o.Format) {
		keys = append(keys, SinglePageName)
	}
	if len(o.Options.InstalledFonts) > 0 && !judgedByLibreOffice(o.Format) {
		keys = append(keys, InstalledFontsName)
	}
	if o.Options.Locale != "" && isSheets(o.Format) && !judgedByLibreOffice(o.Format) {
		keys = append(keys, LocaleName)
	}
	return keys
}

// isPresentation reports whether a format is a presentation's, whose slides
// carry their own size.
func isPresentation(f Format) bool {
	return f == FormatPPTX || f == FormatODP || f == Format("ppt")
}

// judgedByLibreOffice reports whether a format's references are
// LibreOffice's: the OpenDocument formats and CSV. Microsoft's judge the
// others.
func judgedByLibreOffice(f Format) bool {
	return f == FormatODT || f == FormatODS || f == FormatCSV || f == FormatODP
}

// isText reports whether a format is a text document's, which
// [pdf.WithSinglePage] writes on one page.
func isText(f Format) bool {
	return f == FormatDOCX || f == FormatODT
}

// isSheets reports whether a format is laid out as sheets, which
// [pdf.WithOnePagePerSheet] lays out a page each.
func isSheets(f Format) bool {
	return f == FormatXLSX || f == FormatODS || f == FormatCSV
}
