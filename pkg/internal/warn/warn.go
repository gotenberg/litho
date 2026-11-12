package warn

// Code names one kind of construct a conversion did not render as the
// source meant it. Its field is unexported, so only the variables of this
// package hold one; the zero Code is no code and no converter reports it.
type Code struct{ name string }

// String returns the code's kebab-case name, the value of its public
// constant.
func (c Code) String() string { return c.name }

// The codes. Each one's public constant in pkg/pdf documents it for a
// caller: what the source held, what litho did instead, and which formats
// report it.
var (
	// MissingGlyph is a character no font covers.
	MissingGlyph = Code{"missing-glyph"}
	// FontNotLoaded is an @font-face rule none of whose sources loads.
	FontNotLoaded = Code{"font-not-loaded"}
	// TextNotLaidOut is text that does not shape or lay out, left out.
	TextNotLaidOut = Code{"text-not-laid-out"}
	// CSSUnsupported is a CSS property, value, selector or at-rule the
	// cascade does not apply.
	CSSUnsupported = Code{"css-unsupported"}
	// CSSInvalid is a CSS value the specification makes invalid.
	CSSInvalid = Code{"css-invalid"}
	// ResourceRefused is a referenced resource the options do not allow.
	ResourceRefused = Code{"resource-refused"}
	// ResourceNoNetwork is an http or https resource with no HTTP client.
	ResourceNoNetwork = Code{"resource-no-network"}
	// ResourceHTTPStatus is a resource whose server answered outside 2xx.
	ResourceHTTPStatus = Code{"resource-http-status"}
	// ResourceFailed is a referenced resource that does not load.
	ResourceFailed = Code{"resource-failed"}
	// PictureFormatUnsupported is a picture in a format litho does not
	// decode.
	PictureFormatUnsupported = Code{"picture-format-unsupported"}
	// PictureMissing is a picture whose data the file does not hold.
	PictureMissing = Code{"picture-missing"}
	// PictureUnreadable is a picture whose data does not read or decode.
	PictureUnreadable = Code{"picture-unreadable"}
	// ChartNoCache is a chart whose file stores no values for it.
	ChartNoCache = Code{"chart-no-cache"}
	// PartMalformed is a part of the package that does not parse or is
	// missing.
	PartMalformed = Code{"part-malformed"}
	// FormWidgetOmitted is a form control printed without its widget.
	FormWidgetOmitted = Code{"form-widget-omitted"}
	// FormFieldOmitted is a form control that prints without the field
	// the caller asked for.
	FormFieldOmitted = Code{"form-field-omitted"}
	// LinkUnresolved is an internal link whose target no element has.
	LinkUnresolved = Code{"link-unresolved"}
	// ValueFormatUnsupported is a number, date or list numbering format
	// litho does not know.
	ValueFormatUnsupported = Code{"value-format-unsupported"}
	// PageSetupUnsupported is a page setup litho does not apply as the
	// source asks.
	PageSetupUnsupported = Code{"page-setup-unsupported"}
	// ContentUnsupported is any other construct litho does not render as
	// the source asks.
	ContentUnsupported = Code{"content-unsupported"}
	// LimitReached is content past one of litho's anti-DoS caps.
	LimitReached = Code{"limit-reached"}
)

// All returns every code, in a fixed order: the order of the variables
// above.
func All() []Code {
	return []Code{
		MissingGlyph,
		FontNotLoaded,
		TextNotLaidOut,
		CSSUnsupported,
		CSSInvalid,
		ResourceRefused,
		ResourceNoNetwork,
		ResourceHTTPStatus,
		ResourceFailed,
		PictureFormatUnsupported,
		PictureMissing,
		PictureUnreadable,
		ChartNoCache,
		PartMalformed,
		FormWidgetOmitted,
		FormFieldOmitted,
		LinkUnresolved,
		ValueFormatUnsupported,
		PageSetupUnsupported,
		ContentUnsupported,
		LimitReached,
	}
}

// Effect names what a construct's fault cost the output, whatever its
// code. Its field is unexported, so only the variables of this package
// hold one; the zero Effect is no effect and no converter reports it.
type Effect struct{ name string }

// String returns the effect's kebab-case name, the value of its public
// constant.
func (e Effect) String() string { return e.name }

// The effects. Each one's public constant in pkg/pdf documents it for a
// caller.
var (
	// Omitted is content of the source missing from the output, in whole
	// or in part: a picture left out, text cut at a cap, a sheet not
	// printed.
	Omitted = Effect{"omitted"}
	// Approximated is content of the source all in the output, printed
	// differently: a simpler shape, a default format, a fallback font or
	// value, a stand-in the file itself stores.
	Approximated = Effect{"approximated"}
)

// Effects returns every effect, in a fixed order: the order of the
// variables above.
func Effects() []Effect {
	return []Effect{Omitted, Approximated}
}
