package models

// StreamDiagnostic identifies an in-use stream litho cannot fully decode,
// reported by [github.com/gotenberg/litho/pkg/pdf.Reader.UndecodableStreams],
// or whose data litho salvages, reported by
// [github.com/gotenberg/litho/pkg/pdf.Reader.SalvagedStreams]. Filters lists
// the reported filter names in the stream's /Filter chain: for the first, a
// filter name no decoder knows, or a filter whose built-in decoder refuses a
// feature the stream uses; for the second, a filter whose data is damaged.
// Concurrent reads of a value are safe; a write needs synchronization.
type StreamDiagnostic struct {
	// ObjectNum is the object number of the stream.
	ObjectNum int      `json:"objectNum"`
	Filters   []string `json:"filters"`
}

// ColorSpaceUse records a colour space a page's content paints in, reported by
// [github.com/gotenberg/litho/pkg/pdf.Reader.ExtractColorSpaces].
//
// Family is the family of ISO 32000-1, 8.6.3, Table 62, such as "DeviceRGB",
// "ICCBased" or "Separation". It is "" for an image that takes its colour space
// from its JPXDecode data (8.9.5, Table 89) or a space litho cannot read.
//
// Components is how many numbers a colour in the space carries. Alternate is
// nil except in three cases: the base of an Indexed space, the alternate of a
// Separation, DeviceN or ICCBased space when it states one, and the underlying
// space of a Pattern space. Colorants names the colorants of a Separation or
// DeviceN space.
//
// Source is what paints in the space:
//
//   - "operator" for a colour operator. Operator names it: cs, CS, g, G, rg,
//     RG, k or K.
//   - "image" for an image XObject (Do).
//   - "inline-image" (BI).
//   - "shading" for the sh operator or a shading pattern (sh, scn or SCN).
//   - "group" for the blending space of a transparency group (Operator
//     "Group").
//
// Name is the resource the operator named, "" for a family named directly.
// Object is the object number of the image XObject, shading or group dictionary
// it reads, or else of the content stream the operator sits in. It is 0 for the
// page's own content and for a direct object. Count is how many times the
// page's content uses it so.
//
// Concurrent reads of a value are safe; a write needs synchronization.
type ColorSpaceUse struct {
	Family     string         `json:"family"`
	Components int            `json:"components,omitempty"`
	Alternate  *ColorSpaceUse `json:"alternate,omitempty"`
	Colorants  []string       `json:"colorants,omitempty"`
	Source     string         `json:"source"`
	Operator   string         `json:"operator"`
	Name       string         `json:"name,omitempty"`
	Object     int            `json:"object,omitempty"`
	Count      int            `json:"count"`
}
