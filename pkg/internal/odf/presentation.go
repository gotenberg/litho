package odf

// The namespaces of ODF 1.3, Part 3, 1.5 (Table 1) a presentation uses
// beyond the text and drawing ones.
const (
	NSPresentation = "urn:oasis:names:tc:opendocument:xmlns:presentation:1.0"
	NSAnim         = "urn:oasis:names:tc:opendocument:xmlns:animation:1.0"
	NSSmil         = "urn:oasis:names:tc:opendocument:xmlns:smil-compatible:1.0"
	NSDr3d         = "urn:oasis:names:tc:opendocument:xmlns:dr3d:1.0"
)

// init keys the presentation namespaces' properties by their conventional
// prefixes, as [Props] keys the others: "presentation:display-footer".
func init() {
	prefixes[NSPresentation] = "presentation"
	prefixes[NSAnim] = "anim"
	prefixes[NSSmil] = "smil"
	prefixes[NSDr3d] = "dr3d"
}
