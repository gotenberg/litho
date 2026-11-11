package odf

import "strings"

// Describe returns what a drawing element's svg:desc, else its svg:title,
// says of it for a reader who cannot see it (ODF 1.3 Part 3, 19.5 and
// 19.6), "" for nothing or a nil element.
func Describe(el *Element) string {
	if el == nil {
		return ""
	}
	if alt := strings.TrimSpace(el.Child(NSSVG, "desc").TextContent()); alt != "" {
		return alt
	}
	return strings.TrimSpace(el.Child(NSSVG, "title").TextContent())
}

// Decorative reports whether a drawing's graphic properties props mark it
// as carrying no meaning: LibreOffice's loext:decorative property, an
// extension to ODF 1.3 Part 3, 20 that the author sets on a shape that
// only decorates.
func Decorative(props Props) bool {
	return props.Get("loext:decorative") == "true"
}
