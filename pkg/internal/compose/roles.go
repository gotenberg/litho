package compose

import "github.com/gotenberg/litho/pkg/internal/core/ast"

// types14 are the standard structure types of PDF 1.4 (PDF Reference, third
// edition, 9.7.4, Tables 9.21 to 9.27), the set PDF/A-1 checks a role
// against (ISO 19005-1, 6.8.3.4). Immutable lookup table.
var types14 = map[string]bool{
	"Document": true, "Part": true, "Art": true, "Sect": true, "Div": true,
	"BlockQuote": true, "Caption": true, "TOC": true, "TOCI": true, "Index": true,
	"NonStruct": true, "Private": true,
	"P": true, "H": true, "H1": true, "H2": true, "H3": true, "H4": true, "H5": true, "H6": true,
	"L": true, "LI": true, "Lbl": true, "LBody": true,
	"Table": true, "TR": true, "TH": true, "TD": true,
	"Span": true, "Quote": true, "Note": true, "Reference": true, "BibEntry": true,
	"Code": true, "Link": true,
	"Figure": true, "Formula": true, "Form": true,
}

// types15 are the standard structure types PDF 1.5 added, which PDF 1.6 and
// ISO 32000-1 keep: the table row groups (ISO 32000-1, 14.8.4.3.4, Table
// 337), Annot, and the Ruby and Warichu elements (14.8.4.4, Table 338).
// Immutable lookup table.
var types15 = map[string]bool{
	"THead": true, "TBody": true, "TFoot": true, "Annot": true,
	"Ruby": true, "RB": true, "RT": true, "RP": true,
	"Warichu": true, "WT": true, "WP": true,
}

// types20 are the structure types of the PDF 2.0 standard structure
// namespace (ISO 32000-2, 14.8.4, Tables 364 to 370). Immutable lookup
// table.
var types20 = map[string]bool{
	"Document": true, "DocumentFragment": true, "Part": true, "Sect": true, "Div": true,
	"Aside": true, "NonStruct": true,
	"P": true, "H": true, "H1": true, "H2": true, "H3": true, "H4": true, "H5": true, "H6": true,
	"Title": true, "FENote": true, "Sub": true,
	"Lbl": true, "Span": true, "Em": true, "Strong": true, "Link": true, "Annot": true, "Form": true,
	"Ruby": true, "RB": true, "RT": true, "RP": true, "Warichu": true, "WT": true, "WP": true,
	"L": true, "LI": true, "LBody": true,
	"Table": true, "TR": true, "TH": true, "TD": true, "THead": true, "TBody": true, "TFoot": true,
	"Caption": true, "Figure": true, "Formula": true, "Artifact": true,
}

// removed20 maps a PDF 1.7 standard structure type the PDF 2.0 namespace
// does not hold (ISO 32000-2, 14.8.6 and Annex L) to the PDF 2.0 type that
// carries its function. An inline type, empty here, becomes a P under a
// grouping element and a Span elsewhere ([replaced20]). Immutable lookup
// table.
var removed20 = map[string]string{
	"Art": "Sect", "Index": "Sect", "BlockQuote": "Div", "Private": "NonStruct",
	"TOC": "Div", "TOCI": "Div", "Note": "FENote",
	"Quote": "", "Reference": "", "BibEntry": "", "Code": "",
}

// grouping are the grouping types of ISO 32000-1, 14.8.4.2, Table 334, and
// of ISO 32000-2, 14.8.4.4, Table 364: they group elements and hold no
// content of their own (ISO 32005, Table 5). Immutable lookup table.
var grouping = map[string]bool{
	"Document": true, "DocumentFragment": true, "Part": true, "Art": true, "Sect": true,
	"Div": true, "BlockQuote": true, "Aside": true, "TOC": true, "TOCI": true,
	"Index": true, "NonStruct": true, "Private": true,
}

// mappedDown maps a type a version lacks to one every version from PDF 1.4
// defines, for the /RoleMap of a file of an older version: the PDF 1.5
// table row groups to NonStruct, whose kids a reader takes as the table's
// own rows (ISO 32000-1, 14.8.4.2), the inline types PDF 1.5 and PDF 2.0
// added to Span, and the PDF 2.0 block types to their PDF 1.7 analogues.
// Immutable lookup table.
var mappedDown = map[string]string{
	"THead": "NonStruct", "TBody": "NonStruct", "TFoot": "NonStruct",
	"Annot": "Span", "Ruby": "Span", "RB": "Span", "RT": "Span", "RP": "Span",
	"Warichu": "Span", "WT": "Span", "WP": "Span",
	"Em": "Span", "Strong": "Span", "Sub": "Span",
	"DocumentFragment": "Part", "Aside": "Div", "Title": "H", "FENote": "Note",
	"Artifact": "NonStruct",
}

// standard reports whether role is a standard structure type of version v.
func standard(role string, v Version) bool {
	switch v {
	case Version20:
		return types20[role]
	case Version14:
		return types14[role]
	case Version15, Version16, Version17:
	}
	return types14[role] || types15[role]
}

// fallbackRole returns the standard type of version v a role v lacks is
// mapped to through /RoleMap (ISO 32000-1, 14.8.4: a role that is not a
// standard type maps to one). A role no table knows maps to NonStruct,
// which adds no meaning of its own (14.8.4.2).
func fallbackRole(role string, v Version) string {
	if r, ok := mappedDown[role]; ok && standard(r, v) {
		return r
	}
	return "NonStruct"
}

// replaced20 returns the type a PDF 2.0 file writes for an element of the
// given role under a parent of role parent: the role itself when the PDF 2.0
// namespace holds it, its [removed20] replacement otherwise, and "" for a
// role that is no standard type of either namespace.
func replaced20(role, parent string) string {
	if types20[role] {
		return role
	}
	r, ok := removed20[role]
	switch {
	case !ok:
		return ""
	case r != "":
		return r
	case grouping[parent]:
		// ISO 32005, Table 5: a grouping element holds no Span; a P is the
		// block that carries inline content there.
		return "P"
	}
	return "Span"
}

// roleMap returns the /RoleMap of the structure tree root (ISO 32000-1,
// 14.7.2, Table 322; 14.8.4: every role that is not a standard type maps
// to one) for the roles a file of version v writes in the default
// namespace, nil when each is a standard type of v. A PDF 2.0 file writes
// in the default namespace only the roles neither namespace defines, and
// that namespace is PDF 1.7's (ISO 32000-2, 14.8.6.1), so their targets
// are PDF 1.7 types.
func roleMap(written map[string]bool, v Version) ast.PdfDict {
	target := v
	if v == Version20 {
		target = Version17
	}
	var out ast.PdfDict
	for r := range written {
		if standard(r, target) {
			continue
		}
		if out == nil {
			out = ast.PdfDict{}
		}
		out[ast.PdfName(r)] = ast.PdfName(fallbackRole(r, target))
	}
	return out
}
