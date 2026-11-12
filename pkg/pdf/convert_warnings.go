package pdf

// ConvertWarningCode names the kind of construct a [ConvertWarning]
// reports, in kebab-case. The codes form a closed set, the Warning
// constants: a conversion reports no other. A code names what happened to
// which kind of construct, whichever format held it; the warning's message
// names the construct itself, the element, the name or the picture, and
// what printed instead.
//
// A later release may add a code. Read a code the program does not know as
// a construct not rendered as the source meant it.
type ConvertWarningCode string

// The codes a conversion reports.
const (
	// WarningMissingGlyph is a character no font of the font directories
	// covers. It prints as the font's missing-glyph box, an emoji blank.
	// Each character is reported once, and the message names it. Every
	// format reports it. Installing a font that covers the character, or
	// naming its directory with [WithConvertFontDirs], prints it.
	WarningMissingGlyph ConvertWarningCode = "missing-glyph"
	// WarningFontNotLoaded is an @font-face rule none of whose sources
	// loads. Its text prints in the next family of each font-family list
	// that names it; the message names the family and why each source
	// failed. HTML and Markdown report it.
	WarningFontNotLoaded ConvertWarningCode = "font-not-loaded"
	// WarningTextNotLaidOut is text that does not shape or lay out: a list
	// marker, an ellipsis, a tab leader, the text of a slide's shape or of
	// an SVG drawing. It is left out, and the message gives the reason.
	// Every format but plain text reports it.
	WarningTextNotLaidOut ConvertWarningCode = "text-not-laid-out"
	// WarningCSSUnsupported is a CSS property, value, selector or at-rule
	// litho does not apply. The declaration or rule is ignored, or the
	// value computes to the fallback the message names. HTML and Markdown
	// report it, for the document's style sheets and the caller's.
	WarningCSSUnsupported ConvertWarningCode = "css-unsupported"
	// WarningCSSInvalid is a CSS value the specification makes invalid. The
	// declaration is ignored, or, for a var() substitution, the property
	// computes as unset. HTML and Markdown report it.
	WarningCSSInvalid ConvertWarningCode = "css-invalid"
	// WarningResourceRefused is a stylesheet, picture or font the options
	// do not let the conversion read: a URL of a scheme other than http and
	// https, a path that leaves the root of [WithResources], or a relative
	// URL when no resources are given. It is left out. HTML and Markdown
	// report it.
	WarningResourceRefused ConvertWarningCode = "resource-refused"
	// WarningResourceNoNetwork is an http or https stylesheet, picture or
	// font with no [WithHTTPClient] to fetch it. It is left out. HTML and
	// Markdown report it.
	WarningResourceNoNetwork ConvertWarningCode = "resource-no-network"
	// WarningResourceHTTPStatus is an http or https resource whose server
	// answered with a status outside 2xx, which the message names. It is
	// left out. HTML and Markdown report it.
	WarningResourceHTTPStatus ConvertWarningCode = "resource-http-status"
	// WarningResourceFailed is a stylesheet, @import, picture, font or
	// inline SVG that the conversion could read but that does not load:
	// its bytes do not decode or parse, or the read failed. It is left out,
	// and the message gives the reason. HTML and Markdown report it.
	WarningResourceFailed ConvertWarningCode = "resource-failed"
	// WarningPictureFormatUnsupported is a picture in a format litho does
	// not decode: WMF, EMF, SVM, AVIF, JPEG XL, ICO, CUR or an unknown one.
	// It is left out; the message names the format when known. HTML,
	// Markdown, DOCX, ODT, XLSX, ODS, PPTX and ODP report it.
	WarningPictureFormatUnsupported ConvertWarningCode = "picture-format-unsupported"
	// WarningPictureMissing is a picture whose data the file does not hold:
	// a picture linked outside the file, which is never fetched, a part
	// that is absent or does not read, a picture fill or bullet with no
	// picture. It is left out. DOCX, ODT, XLSX, ODS, PPTX and ODP report it.
	WarningPictureMissing ConvertWarningCode = "picture-missing"
	// WarningPictureUnreadable is a picture whose data does not decode: a
	// damaged raster, an SVG that does not parse, inline base64 that does
	// not decode, or an image an HTML or SVG document names that has no
	// size. It is left out. Every format but plain text and CSV reports it.
	WarningPictureUnreadable ConvertWarningCode = "picture-unreadable"
	// WarningChartNoCache is a chart series whose file stores no cached
	// values for it. It prints empty, unless the spreadsheet that holds the
	// chart gives the values of the cells its range names. Saving the file
	// again in its office suite stores the values. DOCX, XLSX, ODS, PPTX
	// and ODP report it.
	WarningChartNoCache ConvertWarningCode = "chart-no-cache"
	// WarningPartMalformed is a part of an Office package that is missing
	// or does not parse: a sheet, a slide, a chart, notes, a header, a
	// theme, the styles. The part is left out, prints empty or a default
	// stands in, as the message says. DOCX, XLSX, ODS, PPTX and ODP report
	// it; a main part that does not parse fails the conversion with
	// [ErrConvertMalformed] instead.
	WarningPartMalformed ConvertWarningCode = "part-malformed"
	// WarningFormWidgetOmitted is a form control that prints without its
	// widget: as its text, as its empty frame, or not at all. HTML,
	// Markdown, ODT, ODS and ODP report it.
	WarningFormWidgetOmitted ConvertWarningCode = "form-widget-omitted"
	// WarningFormFieldOmitted is a form control [WithFormFields] asked to
	// become a field that prints as it shows, without one: a control that
	// breaks across pages or shows nothing. DOCX and ODT report it.
	WarningFormFieldOmitted ConvertWarningCode = "form-field-omitted"
	// WarningLinkUnresolved is an internal link to an anchor no element
	// has. Its text prints, without a link; the message names the target.
	// HTML, Markdown, DOCX and ODT report it.
	WarningLinkUnresolved ConvertWarningCode = "link-unresolved"
	// WarningValueFormatUnsupported is a number, date or list numbering
	// format litho does not know. The value prints in a default format:
	// General for a cell, Arabic numbers for a list, the default format of
	// a date field. The message names the format. XLSX, ODS, PPTX and ODP
	// report it.
	WarningValueFormatUnsupported ConvertWarningCode = "value-format-unsupported"
	// WarningPageSetupUnsupported is page setup litho does not apply as the
	// source asks: an @page size past the PDF page limit, margins that
	// leave no room, a vertical alignment, a fit to a page count, the
	// borders of a header, a header, footer or page-margin box that does
	// not lay out. The message says what printed instead. HTML, Markdown,
	// DOCX, ODT, XLSX and ODS report it.
	WarningPageSetupUnsupported ConvertWarningCode = "page-setup-unsupported"
	// WarningContentUnsupported is any other construct litho does not
	// render as the source asks: an element, shape, effect, chart type,
	// formula, conditional format or other feature that is left out, drawn
	// simpler, or replaced by what the file stores in its place (a
	// replacement picture, saved output, plain text). The message names
	// the construct and what printed. Every format but plain text reports
	// it.
	WarningContentUnsupported ConvertWarningCode = "content-unsupported"
	// WarningLimitReached is content past one of the caps litho sets to
	// bound the time and memory of a conversion: elements nested too deep,
	// too many lines, glyphs, shapes, cells, pictures or layout passes, a
	// picture of too many pixels, a source line too long. What lies past
	// the cap is left out, cut short or drawn simpler, as the message says.
	// Every format reports it.
	WarningLimitReached ConvertWarningCode = "limit-reached"
)
