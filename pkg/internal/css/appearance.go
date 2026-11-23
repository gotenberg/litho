package css

// Appearance is the appearance property (CSS Basic User Interface 4,
// 7.2): whether a form control is drawn as the platform's widget or as
// the box its other properties make.
type Appearance uint8

// The appearance values. The compat-auto keywords (searchfield,
// textarea, checkbox, radio, menulist, listbox, meter, progress-bar,
// button) compute to [AppearanceAuto], as they behave like auto (7.2).
const (
	// AppearanceNone draws the element as CSS styles it.
	AppearanceNone Appearance = iota
	// AppearanceAuto draws a form control as its widget.
	AppearanceAuto
	// AppearanceTextField draws a search field as a plain text field.
	AppearanceTextField
	// AppearanceMenuListButton draws a drop-down box as CSS styles it,
	// with its drop-down arrow.
	AppearanceMenuListButton
)

// appearanceKeywords maps each keyword of appearance to its value (CSS
// Basic User Interface 4, 7.2), the compat-auto keywords to auto.
var appearanceKeywords = map[string]Appearance{
	"none": AppearanceNone, "auto": AppearanceAuto, "textfield": AppearanceTextField,
	"menulist-button": AppearanceMenuListButton, "searchfield": AppearanceAuto,
	"textarea": AppearanceAuto, "checkbox": AppearanceAuto, "radio": AppearanceAuto,
	"menulist": AppearanceAuto, "listbox": AppearanceAuto, "meter": AppearanceAuto,
	"progress-bar": AppearanceAuto, "button": AppearanceAuto,
}

// Resize is the resize property (CSS Basic User Interface 4, 5.1), which
// a printed page reads only to draw a textarea's resize grip.
type Resize uint8

// The resize values: every value but none lets the box be resized.
const (
	ResizeNone Resize = iota
	ResizeBoth
	ResizeHorizontal
	ResizeVertical
	ResizeBlock
	ResizeInline
	// ResizeAuto is Chromium's -internal value of a textarea, resized
	// both ways.
	ResizeAuto
)

// accentAuto is the specified accent-color auto.
type accentAuto struct{}

// accentColorProp is accent-color: auto | <color> (CSS Basic User
// Interface 4, 6.2), inherited. Its computed colour is held behind
// [Style.AccentColor], nil for auto; currentColor resolves against the
// element's own color.
func accentColorProp() propDef {
	return propDef{
		name: "accent-color", inherited: true,
		parse: func(v []token, _ *declCtx) (any, bool) {
			t, ok := single(v)
			if !ok {
				return nil, false
			}
			if t.typ == tIdent && lower(t.value) == "auto" {
				return accentAuto{}, true
			}
			c, ok := parseColor(t)
			return c, ok
		},
		compute: func(_ *styler, st *Style, v any) {
			switch c := v.(type) {
			case accentAuto:
				st.AccentColor = nil
			case colorValue:
				col := c.resolve(st.Color, st.DarkScheme)
				st.AccentColor = &col
			}
		},
		copyFrom: field(func(s *Style) **Color { return &s.AccentColor }),
	}
}

// authorStyledProps holds, for each longhand, whether an author's
// declaration of it makes a form control drawn as its widget give way to
// its CSS box ([Style.AuthorBorder], [Style.AuthorBackground]): 1 for the
// border's longhands, its radii and its image, 2 for the background's; and
// whether an author's declaration sets the top or bottom margin, 4 and 8,
// which a quirky margin does not have ([Style.AuthorMarginTop]).
// Immutable lookup table.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws a text
//     field, a button or a drop-down box styled with border, border-width,
//     border-color, border-radius, background, background-color (even
//     white, its default) or background-image as CSS draws its box, and
//     as its widget under padding, width, height, box-sizing or outline.
var authorStyledProps = func() [numProps]uint8 {
	var on [numProps]uint8
	for _, id := range []propID{
		pBorderTopStyle, pBorderRightStyle, pBorderBottomStyle, pBorderLeftStyle,
		pBorderTopWidth, pBorderRightWidth, pBorderBottomWidth, pBorderLeftWidth,
		pBorderTopColor, pBorderRightColor, pBorderBottomColor, pBorderLeftColor,
		pRadiusTopLeft, pRadiusTopRight, pRadiusBottomRight, pRadiusBottomLeft,
		pBorderImageSource, pBorderImageSlice, pBorderImageWidth, pBorderImageOutset, pBorderImageRepeat,
	} {
		on[id] = authorBorder
	}
	for _, id := range []propID{
		pBgColor, pBgImage, pBgRepeat, pBgPosition, pBgPositionY, pBgSize, pBgOrigin, pBgClip,
		pBgAttachment, pBgBlendMode,
	} {
		on[id] = authorBackground
	}
	on[pMarginTop] = authorMarginTop
	on[pMarginBottom] = authorMarginBottom
	return on
}()

// The bits of [authorStyledProps].
const (
	authorBorder       = 1
	authorBackground   = 2
	authorMarginTop    = 4
	authorMarginBottom = 8
)
