package pdf

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	htmlconv "github.com/gotenberg/litho/pkg/internal/convert/html"
)

// maxConvertPageSide is the longest page side in points (ISO 32000-1,
// Annex C.2, Table C.1).
const maxConvertPageSide = 14400

// maxConvertFontSize is the largest font size [WithFontSize] takes, in
// points: the largest the text stack lays out.
const maxConvertFontSize = 1000

// maxConvertLineHeight is the largest multiple [WithLineHeight] takes. Not
// spec-mandated: it keeps the line advance finite, and a multiple past it
// puts one line on a page at any size typesetting uses.
const maxConvertLineHeight = 100

// maxConvertTabWidth is the widest tab stop [WithTabWidth] takes, in
// characters. Anti-DoS guard, not spec-mandated: a tab stop wider than a
// line of any page moves every tab to the next line.
const maxConvertTabWidth = 64

// maxCSVSeparators bounds the separators [WithCSVSeparators] takes.
// Anti-DoS guard, not spec-mandated: every field boundary is tested against
// each, and a file is written with one separator, rarely two.
const maxCSVSeparators = 8

// maxConvertSheets bounds the sheets [WithSheets] and [WithSheetNumbers]
// name. Anti-DoS guard, not spec-mandated: each name is matched against the
// workbook's sheets, and a workbook of more sheets than this converts past
// [Limits.MaxConvertPages] anyway.
const maxConvertSheets = 4096

// validate reports the first option out of range, or the first two options
// that exclude each other, wrapped in [ErrInvalidConvertOptions]; nil when
// the options hold together. The option types let a conversion set only
// the options its format takes, so every field that is set is checked.
func (o *convertConfig) validate() error {
	if o.paperSet {
		if err := checkPaper("paper", o.paper); err != nil {
			return err
		}
	}
	if o.defaultPaper != (Paper{}) {
		if err := checkPaper("default paper", o.defaultPaper); err != nil {
			return err
		}
	}
	if o.orientation < OrientationSource || o.orientation > OrientationLandscape {
		return fmt.Errorf("%w: orientation %d", ErrInvalidConvertOptions, o.orientation)
	}
	if o.defaultLang != "" && (strings.TrimSpace(o.defaultLang) != o.defaultLang || !compliance.IsValidLangTag(o.defaultLang)) {
		// ISO 32000-1, 14.9.2 takes a language identifier as BCP 47 (RFC
		// 3066) defines it; validators read the tag as written, so
		// surrounding spaces are refused rather than trimmed.
		return fmt.Errorf("%w: default language %q is not a well-formed language tag", ErrInvalidConvertOptions, o.defaultLang)
	}
	if err := o.validateMargins(); err != nil {
		return err
	}
	if err := o.validateText(); err != nil {
		return err
	}
	if err := o.validateHTML(); err != nil {
		return err
	}
	return o.validateOffice()
}

// checkPaper reports a paper whose sides are not both in (0, 14400]
// points, the PDF page limit (ISO 32000-1, Annex C.2).
func checkPaper(name string, p Paper) error {
	for _, side := range []float64{p.Width, p.Height} {
		// The negated range also refuses NaN.
		if !(side > 0 && side <= maxConvertPageSide) {
			return fmt.Errorf("%w: %s %v by %v points: each side must be in (0, %d]", ErrInvalidConvertOptions, name, p.Width, p.Height, maxConvertPageSide)
		}
	}
	return nil
}

// validateMargins reports a margin that is not a finite number of points,
// zero or more, or margins that leave no room on the paper the options
// name.
func (o *convertConfig) validateMargins() error {
	if !o.marginsSet {
		return nil
	}
	for _, m := range []struct {
		name  string
		value float64
	}{{"top", o.marginTop}, {"right", o.marginRight}, {"bottom", o.marginBottom}, {"left", o.marginLeft}} {
		// The negated range also refuses NaN.
		if !(m.value >= 0 && m.value < math.Inf(1)) {
			return fmt.Errorf("%w: %s margin %v is not a finite number of points, zero or more", ErrInvalidConvertOptions, m.name, m.value)
		}
	}
	w, h := o.turned(o.basePaper())
	if o.marginLeft+o.marginRight >= w {
		return fmt.Errorf("%w: left and right margins %v and %v leave no room on a page %v points wide", ErrInvalidConvertOptions, o.marginLeft, o.marginRight, w)
	}
	if o.marginTop+o.marginBottom >= h {
		return fmt.Errorf("%w: top and bottom margins %v and %v leave no room on a page %v points high", ErrInvalidConvertOptions, o.marginTop, o.marginBottom, h)
	}
	return nil
}

// validateText reports a text option out of range.
func (o *convertConfig) validateText() error {
	if o.fontSizeSet && !(o.fontSize > 0 && o.fontSize <= maxConvertFontSize) {
		return fmt.Errorf("%w: font size %v is not in (0, %d]", ErrInvalidConvertOptions, o.fontSize, maxConvertFontSize)
	}
	if o.lineHeightSet && !(o.lineHeight > 0 && o.lineHeight <= maxConvertLineHeight) {
		return fmt.Errorf("%w: line height %v is not in (0, %d]", ErrInvalidConvertOptions, o.lineHeight, maxConvertLineHeight)
	}
	if o.tabWidthSet && (o.tabWidth < 1 || o.tabWidth > maxConvertTabWidth) {
		return fmt.Errorf("%w: tab width %d is not in [1, %d]", ErrInvalidConvertOptions, o.tabWidth, maxConvertTabWidth)
	}
	return nil
}

// validateHTML reports an HTML or Markdown option out of range, or a single
// page asked with a page selection.
func (o *convertConfig) validateHTML() error {
	if o.media != "" && o.media != htmlconv.MediaPrint && o.media != htmlconv.MediaScreen {
		return fmt.Errorf("%w: media type %q is neither %q nor %q", ErrInvalidConvertOptions, o.media, htmlconv.MediaPrint, htmlconv.MediaScreen)
	}
	if o.scaleSet && !(o.scale >= htmlconv.MinScale && o.scale <= htmlconv.MaxScale) {
		return fmt.Errorf("%w: scale %v is not in [%v, %v]", ErrInvalidConvertOptions, o.scale, htmlconv.MinScale, htmlconv.MaxScale)
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
	if o.singlePage && o.pageRanges != "" {
		return fmt.Errorf("%w: a single page has no page range to select", ErrInvalidConvertOptions)
	}
	if o.markdownTemplateSet && o.markdownTemplate == "" {
		return fmt.Errorf("%w: an empty Markdown template never calls toHTML", ErrInvalidConvertOptions)
	}
	return nil
}

// validateOffice reports an Office option out of range, or two that exclude
// each other.
func (o *convertConfig) validateOffice() error {
	if o.trackedChanges < TrackedChangesAccepted || o.trackedChanges > TrackedChangesMarked {
		return fmt.Errorf("%w: tracked changes view %d", ErrInvalidConvertOptions, o.trackedChanges)
	}
	if o.recalculation < RecalculateAsNeeded || o.recalculation > RecalculateAlways {
		return fmt.Errorf("%w: recalculation mode %d", ErrInvalidConvertOptions, o.recalculation)
	}
	if o.onePagePerSheet && (o.paperSet || o.orientation != OrientationSource) {
		return fmt.Errorf("%w: a sheet laid out on one page takes its content's size, not a paper or an orientation", ErrInvalidConvertOptions)
	}
	if err := o.validateSheets(); err != nil {
		return err
	}
	if o.csvSeparatorSet {
		if len(o.csvSeparators) == 0 || len(o.csvSeparators) > maxCSVSeparators {
			return fmt.Errorf("%w: %d CSV separators: between 1 and %d", ErrInvalidConvertOptions, len(o.csvSeparators), maxCSVSeparators)
		}
		for _, r := range o.csvSeparators {
			if r == '"' || r == '\n' || r == '\r' || r == utf8.RuneError || !utf8.ValidRune(r) {
				return fmt.Errorf("%w: CSV separator %q", ErrInvalidConvertOptions, r)
			}
		}
	}
	switch o.handouts {
	case 0:
	case 1, 2, 3, 4, 6, 9:
		if o.notesPages {
			return fmt.Errorf("%w: handouts and notes pages exclude each other", ErrInvalidConvertOptions)
		}
	default:
		return fmt.Errorf("%w: %d slides a handout page: 1, 2, 3, 4, 6 or 9", ErrInvalidConvertOptions, o.handouts)
	}
	return nil
}

// validateSheets reports a sheet selection that names nothing, names too
// much, or is given both by name and by number.
func (o *convertConfig) validateSheets() error {
	if o.sheetsSet && o.sheetNumbersSet {
		return fmt.Errorf("%w: sheet names and sheet numbers exclude each other", ErrInvalidConvertOptions)
	}
	if o.sheetsSet && (len(o.sheets) == 0 || len(o.sheets) > maxConvertSheets) {
		return fmt.Errorf("%w: %d sheet names: between 1 and %d", ErrInvalidConvertOptions, len(o.sheets), maxConvertSheets)
	}
	if o.sheetNumbersSet {
		if len(o.sheetNumbers) == 0 || len(o.sheetNumbers) > maxConvertSheets {
			return fmt.Errorf("%w: %d sheet numbers: between 1 and %d", ErrInvalidConvertOptions, len(o.sheetNumbers), maxConvertSheets)
		}
		for _, n := range o.sheetNumbers {
			if n < 1 {
				return fmt.Errorf("%w: sheet number %d: the first sheet is 1", ErrInvalidConvertOptions, n)
			}
		}
	}
	return nil
}
