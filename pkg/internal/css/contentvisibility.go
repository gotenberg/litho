package css

// ContentVisibility is content-visibility (CSS Contain 2, 4).
type ContentVisibility uint8

// The content-visibility values. auto skips nothing on paper: a printed
// document lays every box out, as Chromium does.
const (
	VisibilityVisible ContentVisibility = iota
	VisibilityAuto
	VisibilityHidden
)

// contentVisibilityProp is content-visibility, not inherited.
func contentVisibilityProp() propDef {
	return enumProp("content-visibility", false, func(s *Style) *ContentVisibility { return &s.ContentVisibility }, map[string]ContentVisibility{
		"visible": VisibilityVisible, "auto": VisibilityAuto, "hidden": VisibilityHidden,
	})
}
