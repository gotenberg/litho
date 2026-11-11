package pdf

import (
	"fmt"
	"math"
	"net/url"
	"strings"

	htmlconv "github.com/gotenberg/litho/pkg/internal/convert/html"
	"github.com/gotenberg/litho/pkg/internal/convert/plaintext"
)

// CodeOptionIgnored is the [ConvertWarning] code of an option set to
// something other than its default that does not apply to the source's
// format: [Convert] reports it once per option, naming the option, and
// converts as if the option were absent.
const CodeOptionIgnored = "option-ignored"

// The default paper of a source that names none, A4 (ISO 216, 210 by 297
// mm), unless [WithDefaultPaperSize] names another.
const (
	defaultPaperWidth  = plaintext.DefaultPageWidth
	defaultPaperHeight = plaintext.DefaultPageHeight
)

// maxConvertPageSide is the longest page side in points (ISO 32000-1,
// Annex C.2, Table C.1).
const maxConvertPageSide = 14400

// maxConvertFontSize is the largest font size [WithFontSize] takes, in
// points: the largest the text stack lays out.
const maxConvertFontSize = 1000

// validate reports the first option out of range, wrapped in
// [ErrInvalidConvertOptions], nil when every option is in range. It
// checks every option whatever the format, so an option a format ignores
// fails as it would where it applies.
func (o *convertOptions) validate() error {
	if o.paperSet {
		if err := checkPaper("paper", o.width, o.height); err != nil {
			return err
		}
	}
	if o.defaultWidth != 0 || o.defaultHeight != 0 {
		if err := checkPaper("default paper", o.defaultWidth, o.defaultHeight); err != nil {
			return err
		}
	}
	if o.marginsSet {
		for _, m := range []struct {
			name  string
			value float64
		}{{"top", o.marginTop}, {"right", o.marginRight}, {"bottom", o.marginBottom}, {"left", o.marginLeft}} {
			// The negated range also refuses NaN.
			if !(m.value >= 0 && m.value < math.Inf(1)) {
				return fmt.Errorf("%w: %s margin %v is not a finite number of points, zero or more", ErrInvalidConvertOptions, m.name, m.value)
			}
		}
		w, h := o.width, o.height
		if !o.paperSet {
			w, h = o.defaultPaper()
		}
		if o.landscape && w < h {
			w, h = h, w
		}
		if o.marginLeft+o.marginRight >= w {
			return fmt.Errorf("%w: left and right margins %v and %v leave no room on a page %v points wide", ErrInvalidConvertOptions, o.marginLeft, o.marginRight, w)
		}
		if o.marginTop+o.marginBottom >= h {
			return fmt.Errorf("%w: top and bottom margins %v and %v leave no room on a page %v points high", ErrInvalidConvertOptions, o.marginTop, o.marginBottom, h)
		}
	}
	if o.fontSizeSet && !(o.fontSize > 0 && o.fontSize <= maxConvertFontSize) {
		return fmt.Errorf("%w: font size %v is not in (0, %d]", ErrInvalidConvertOptions, o.fontSize, maxConvertFontSize)
	}
	if o.media != "" && o.media != htmlconv.MediaPrint && o.media != htmlconv.MediaScreen {
		return fmt.Errorf("%w: media type %q is neither %q nor %q", ErrInvalidConvertOptions, o.media, htmlconv.MediaPrint, htmlconv.MediaScreen)
	}
	if o.scaleSet && !(o.scale >= htmlconv.MinScale && o.scale <= htmlconv.MaxScale) {
		return fmt.Errorf("%w: print scale %v is not in [%v, %v]", ErrInvalidConvertOptions, o.scale, htmlconv.MinScale, htmlconv.MaxScale)
	}
	if o.baseURL != "" {
		if u, err := url.Parse(o.baseURL); err != nil || !u.IsAbs() {
			return fmt.Errorf("%w: base URL %q is not an absolute URL", ErrInvalidConvertOptions, o.baseURL)
		}
	}
	for name := range o.mediaFeatures {
		if name == "" || name != strings.ToLower(name) {
			return fmt.Errorf("%w: media feature %q is not a lowercase name", ErrInvalidConvertOptions, name)
		}
	}
	return nil
}

// checkPaper reports a paper whose sides are not both in (0, 14400]
// points, the PDF page limit (ISO 32000-1, Annex C.2).
func checkPaper(name string, w, h float64) error {
	for _, side := range []float64{w, h} {
		// The negated range also refuses NaN.
		if !(side > 0 && side <= maxConvertPageSide) {
			return fmt.Errorf("%w: %s %v by %v points: each side must be in (0, %d]", ErrInvalidConvertOptions, name, w, h, maxConvertPageSide)
		}
	}
	return nil
}

// defaultPaper returns the paper of a source that names none: the one
// [WithDefaultPaperSize] names, else A4.
func (o *convertOptions) defaultPaper() (float64, float64) {
	if o.defaultWidth > 0 && o.defaultHeight > 0 {
		return o.defaultWidth, o.defaultHeight
	}
	return defaultPaperWidth, defaultPaperHeight
}

// formatFamily groups the formats by the options they take.
type formatFamily int

const (
	familyText formatFamily = iota
	familyHTML
	familyWord
	familySheet
	familySlides
)

// familyOf returns the family of format, one [Convert] accepts.
func familyOf(format Format) formatFamily {
	switch format {
	case FormatHTML, FormatMarkdown:
		return familyHTML
	case FormatDOCX, FormatODT:
		return familyWord
	case FormatXLSX, FormatODS, FormatCSV:
		return familySheet
	case FormatPPTX, FormatODP:
		return familySlides
	case FormatText:
	}
	return familyText
}

// formatTitle names format in a message.
func formatTitle(format Format) string {
	switch format {
	case FormatText:
		return "plain text"
	case FormatHTML, FormatDOCX, FormatODT, FormatXLSX, FormatODS, FormatCSV, FormatPPTX, FormatODP:
		return strings.ToUpper(string(format))
	case FormatMarkdown:
		return "Markdown"
	}
	return string(format)
}

// ignoredOptions returns a [CodeOptionIgnored] warning for each option o
// sets to something other than its default that does not apply to a
// source of format, in the order the options are documented.
//
// The table of what applies where is the godoc of each [ConvertOption].
// The options every format takes (the logger, the font directories, the
// limits, the paper, landscape, the page ranges and the output options)
// are never reported, but for the two an HTML single page and a
// spreadsheet single page set aside.
func ignoredOptions(format Format, o *convertOptions) []ConvertWarning {
	fam := familyOf(format)
	html, text := fam == familyHTML, fam == familyText
	singleSheets := fam == familySheet && o.singlePage
	checks := []struct {
		name    string
		set     bool
		applies bool
		why     string
	}{
		{"WithPaperSize", o.paperSet, !singleSheets, "a sheet laid out on one page takes its content's size"},
		{"WithDefaultPaperSize", o.defaultWidth != 0 || o.defaultHeight != 0, fam != familySlides, "a presentation names its slide size"},
		{"WithLandscape", o.landscape, !singleSheets, "a sheet laid out on one page takes its content's size"},
		{"WithMargins", o.marginsSet && !o.defaultMargins(), text || html, "the document carries its own margins"},
		{"WithFontFamilies", len(o.families) > 0, text, "the document carries its own fonts"},
		{"WithFontSize", o.fontSizeSet, text, "the document carries its own font sizes"},
		{"WithMediaType", o.media != "" && o.media != htmlconv.MediaPrint, html, "it has no stylesheet"},
		{"WithResources", o.resources != nil, html, "it names no resource"},
		{"WithPreferCSSPageSize", o.preferCSSPageSize, html, "it has no CSS"},
		{"WithPrintScale", o.scaleSet && o.scale != 1, html, ""},
		{"WithSinglePage", o.singlePage, html || fam == familySheet, ""},
		{"WithPageRanges", o.pageRanges != "", !html || !o.singlePage, "WithSinglePage writes one page"},
		{"WithPrintBackground", o.omitBackgrounds, html, ""},
		{"WithShrinkToFit", o.noShrinkToFit, html, ""},
		{"WithFractionalFontMetrics", o.fractionalFontMetrics, html, ""},
		{"WithOutline", o.omitOutline, !text, "it writes no outline"},
		{"WithConvertPassword", o.password != "", !text && !html && format != FormatCSV, "it is never encrypted"},
		{"WithHiddenSlides", o.hiddenSlides, fam == familySlides, ""},
		{"WithNotesPages", o.notesPages, fam == familySlides, ""},
		{"WithHeaderTemplate", o.headerTemplate != "", html, ""},
		{"WithFooterTemplate", o.footerTemplate != "", html, ""},
		{"WithPrintDate", !o.date.IsZero(), html || fam == familySheet || fam == familySlides, "it shows no print date"},
		{"WithBaseURL", o.baseURL != "", html, ""},
		{"WithHTTPClient", o.httpClient != nil, html, ""},
		{"WithMediaFeatures", len(o.mediaFeatures) > 0, html, "it has no stylesheet"},
	}
	var out []ConvertWarning
	for _, c := range checks {
		if !c.set || c.applies {
			continue
		}
		msg := fmt.Sprintf("%s does not apply to %s sources and is ignored", c.name, formatTitle(format))
		if c.why != "" {
			msg += ": " + c.why
		}
		out = append(out, ConvertWarning{Code: CodeOptionIgnored, Message: msg})
	}
	return out
}

// defaultMargins reports margins equal to the default, 2 cm a side.
func (o *convertOptions) defaultMargins() bool {
	d := plaintext.DefaultMargin
	return o.marginTop == d && o.marginRight == d && o.marginBottom == d && o.marginLeft == d
}
