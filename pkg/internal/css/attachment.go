package css

// Attachment is the background-attachment property (CSS Backgrounds 3,
// 3.5).
type Attachment uint8

// The background-attachment values. The zero value is the initial one.
const (
	// AttachScroll fixes the background to the element's border box.
	AttachScroll Attachment = iota
	// AttachFixed fixes it to the viewport.
	AttachFixed
	// AttachLocal fixes it to the element's contents, the positioning
	// area then being the scrollable overflow area of a scroll container.
	AttachLocal
)

// attachments are the background-attachment keywords.
var attachments = map[string]Attachment{"scroll": AttachScroll, "fixed": AttachFixed, "local": AttachLocal}

// attachmentProp defines background-attachment, of which [Style] keeps
// the first layer's value.
func attachmentProp() propDef {
	return propDef{
		name: "background-attachment",
		parse: func(v []token, c *declCtx) (any, bool) {
			l, ok := firstLayer(v, c, "background-attachment")
			if !ok {
				return nil, false
			}
			k, ok := keyword(l)
			if !ok {
				return nil, false
			}
			a, ok := attachments[k]
			return a, ok
		},
		compute: func(_ *styler, st *Style, v any) {
			if a, ok := v.(Attachment); ok {
				st.BackgroundAttachment = a
			}
		},
		copyFrom: field(func(s *Style) *Attachment { return &s.BackgroundAttachment }),
	}
}
