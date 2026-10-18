package tagging

// Role is a PDF structure-element type, the value of a structure element's
// /S key. The constants below are standard structure types from ISO 32000-1
// Table 337. The classifier produces Document, H1..H6, P, Figure, L/LI/LBody
// and Table/TR/TD; TH is defined for the emitter but never synthesized. The
// type permits any role so callers can use the full standard set plus
// role-mapped custom roles.
type Role string

// Standard structure roles.
const (
	RoleDocument Role = "Document"
	RoleH1       Role = "H1"
	RoleH2       Role = "H2"
	RoleH3       Role = "H3"
	RoleH4       Role = "H4"
	RoleH5       Role = "H5"
	RoleH6       Role = "H6"
	RoleP        Role = "P"
	RoleFigure   Role = "Figure"
	RoleL        Role = "L"
	RoleLI       Role = "LI"
	RoleLBody    Role = "LBody"
	RoleTable    Role = "Table"
	RoleTR       Role = "TR"
	RoleTH       Role = "TH"
	RoleTD       Role = "TD"
)

// ContentSpan identifies a byte range in a page's decoded, concatenated
// content stream that a leaf element marks. Start and End are offsets
// [Start, End) into the decoded content and must fall on content-stream
// operation boundaries: the emitter splices marked-content operators at
// these offsets without otherwise altering the bytes between them, so a
// mid-operator boundary would corrupt the stream. The classifier that
// produces spans guarantees the alignment.
type ContentSpan struct {
	// PageIndex is the 0-based index of the page in the document page tree.
	PageIndex int
	// Start and End bound the marked range, [Start, End).
	Start, End int
}

// Element is a node in the logical structure tree to emit. A leaf carries a
// non-nil Span identifying the content it marks; a container carries Kids.
// A node must be one or the other, never both.
type Element struct {
	// Role is the structure type (/S).
	Role Role
	// Kids are the child elements of a container node.
	Kids []*Element
	// Span is the content a leaf node marks.
	Span *ContentSpan
	// Alt is the /Alt alternative text. Required on Figure for PDF/UA; an
	// empty value leaves an auditable residual the caller fills later.
	Alt string
	// Lang is the /Lang BCP 47 language tag for this subtree.
	Lang string
	// ActualText is the /ActualText replacement text.
	ActualText string
	// Title is the /T human-readable title.
	Title string
	// Pagination, when set, makes the leaf page furniture rather than
	// content: [Emit] builds no structure element for it and marks its Span
	// as a pagination artifact of this subtype.
	Pagination Pagination
}

// Pagination is the subtype of a pagination artifact, content that repeats
// on pages as part of their layout rather than of the document's logical
// content (ISO 32000-1, 14.8.2.2.2, Table 330). [Classify] marks running
// headers and footers, page numbers among them, this way, since ISO 14289-1,
// 7.8 asks that they be artifacts rather than part of the structure tree.
type Pagination string

// The pagination subtypes [Classify] produces. ISO 32000-1, Table 330 also
// defines Watermark, which no pass detects. ISO 32000-2 keeps Header and
// Footer and adds PageNum, which ISO 32000-1 readers do not know, so a page
// number is a Header or a Footer by the margin it sits in.
const (
	PaginationHeader Pagination = "Header"
	PaginationFooter Pagination = "Footer"
)
