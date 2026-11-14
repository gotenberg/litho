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

// attachmentProp defines background-attachment, an attachment a layer.
func attachmentProp() propDef {
	return bgListProp("background-attachment", func(v []token, _ *declCtx) (any, bool) {
		return bgList(v, func(p []token) (Attachment, bool) {
			k, ok := keyword(p)
			a, known := attachments[k]
			return a, ok && known
		})
	}, func(s *Style) *Attachment { return &s.BackgroundAttachment }, func(r *BackgroundLists) *[]Attachment { return &r.Attachments }, same[Attachment])
}
