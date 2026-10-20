package remediation

import (
	"bytes"
	"context"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// inlineXObjectCodecs are the standard filters of ISO 32000-1, 7.4, Table 6
// that an image XObject may apply and an inline image may not: neither has an
// abbreviation in ISO 32000-2, 8.9.7, Table 92, and ISO 32000-1, 7.4.9 applies
// JPXDecode to image XObjects alone. ISO 19005-2/3, 6.1.10 and ISO 19005-4,
// 6.1.9 forbid them in an inline image, and [moveInlineImage] repairs one by
// moving it to an image XObject rather than by decoding it.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 6
var inlineXObjectCodecs = []ast.PdfName{"JPXDecode", "JBIG2Decode"}

// inlineFilterFullNames maps the filter abbreviations of ISO 32000-1, 8.9.7,
// Table 94 to the names an image XObject spells them with (7.4, Table 6).
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 94
var inlineFilterFullNames = map[ast.PdfName]ast.PdfName{
	"AHx": "ASCIIHexDecode",
	"A85": "ASCII85Decode",
	"LZW": "LZWDecode",
	"Fl":  "FlateDecode",
	"RL":  "RunLengthDecode",
	"CCF": "CCITTFaxDecode",
	"DCT": "DCTDecode",
}

// inlineKeyFullNames maps the inline image dictionary keys of ISO 32000-1,
// 8.9.7, Table 93 to the image dictionary keys they abbreviate (8.9.5, Table
// 89). /F and /DP are left out: [moveInlineImage] writes the filters itself.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 93
var inlineKeyFullNames = map[ast.PdfName]ast.PdfName{
	"BPC": "BitsPerComponent",
	"CS":  "ColorSpace",
	"D":   "Decode",
	"H":   "Height",
	"IM":  "ImageMask",
	"I":   "Interpolate",
	"W":   "Width",
}

// inlineDeviceSpaces maps the device colour space names an inline image may
// give /CS, abbreviated as ISO 32000-1, 8.9.7, Table 94 allows or in full, to
// the full names an image dictionary gives them.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 94
var inlineDeviceSpaces = map[ast.PdfName]ast.PdfName{
	"G": "DeviceGray", "DeviceGray": "DeviceGray",
	"RGB": "DeviceRGB", "DeviceRGB": "DeviceRGB",
	"CMYK": "DeviceCMYK", "DeviceCMYK": "DeviceCMYK",
}

// movesToXObject reports whether part forbids, in the inline image dict
// describes, a filter of [inlineXObjectCodecs], whatever its case.
func movesToXObject(dict ast.PdfDict, part int) bool {
	return slices.ContainsFunc(inlineImageFilterNames(dict), func(n ast.PdfName) bool {
		c, ok := canonicalInlineFilter(n)
		return ok && slices.Contains(inlineXObjectCodecs, c) && inlineFilterForbidden(n, part)
	})
}

// moveInlineImage returns the rewrite that paints the inline image the
// operation spanning r of data begins, whose dictionary is dict, with Do of a
// new image XObject res names: ISO 32000-1, 8.9.7 makes an inline image's
// dictionary and data those of an image XObject, painted as Do paints one.
// The data is carried byte for byte under the same filters, spelled in full,
// so a viewer decodes the same samples; the white-space byte before EI, the
// separator readers look for, is left out when the data decodes whole without
// it. It returns false when the move cannot be proven to draw the same image:
//
//   - a filter is no name of ISO 32000-1, Table 6 or Table 94, or one the
//     part forbids other than a codec of [inlineXObjectCodecs], LZWDecode
//     among them, which an image XObject's stream would carry on;
//   - a key is written both abbreviated and in full;
//   - /CS names no device space and nothing the stream's resources hold;
//   - the data does not decode whole ([filter.DecodeReport.Incomplete]);
//   - the stream has no resource dictionary of its own to name the image in.
func moveInlineImage(ctx context.Context, data []byte, r content.ByteRange, dict ast.PdfDict, part int, dec *filter.Decoder, res *resourceEdit) (inlineImageRewrite, bool) {
	start, end, raw, ok := inlineImageBody(ctx, data, r, dict)
	if !ok {
		return inlineImageRewrite{}, false
	}
	filterKey, parmsKey, ok := inlineFilterKeys(dict)
	if !ok {
		return inlineImageRewrite{}, false
	}
	names := inlineImageFilterNames(dict)
	parms, ok := inlineDecodeParms(dict[parmsKey], len(names))
	if !ok {
		return inlineImageRewrite{}, false
	}
	full := make([]ast.PdfName, len(names))
	for i, n := range names {
		c, known := canonicalInlineFilter(n)
		if !known || (!slices.Contains(inlineXObjectCodecs, c) && inlineFilterForbidden(c, part)) {
			return inlineImageRewrite{}, false
		}
		full[i] = c
		if f, abbreviated := inlineFilterFullNames[c]; abbreviated {
			full[i] = f
		}
	}
	body, ok := inlineXObjectData(ctx, dec, raw, full, parms)
	if !ok {
		return inlineImageRewrite{}, false
	}
	imgDict, ok := inlineXObjectDict(ctx, dict, filterKey, parmsKey, res)
	if !ok {
		return inlineImageRewrite{}, false
	}
	setInlineFilters(imgDict, "Filter", "DecodeParms", full, parms)
	imgDict["Length"] = ast.PdfInt(len(body))
	name, ok := res.addImage(ctx, ast.NewStream(imgDict, body))
	if !ok {
		return inlineImageRewrite{}, false
	}
	var text bytes.Buffer
	if _, err := writer.SerializeName(&text, name); err != nil {
		return inlineImageRewrite{}, false
	}
	text.WriteString(" Do")
	return inlineImageRewrite{start: start, end: end, text: text.Bytes(), name: name}, true
}

// inlineImageBody returns where the inline image the operation spanning r of
// data begins starts and ends, from its BI through its EI, and its data, from
// past its ID up to its EI.
func inlineImageBody(ctx context.Context, data []byte, r content.ByteRange, dict ast.PdfDict) (int, int, []byte, bool) {
	biStart, ok := inlineImageStart(data[r.Start:r.End])
	if !ok {
		return 0, 0, nil, false
	}
	start := r.Start + biStart
	end, ok := inlineImageEnd(data, r)
	if !ok || end-start < len("BIEI") {
		return 0, 0, nil, false
	}
	dataStart, ok := inlineImageDataStart(ctx, data[start:end], dict)
	if !ok {
		return 0, 0, nil, false
	}
	return start, end, data[start+dataStart : end-len("EI")], true
}

// inlineXObjectData returns the bytes of raw, an inline image's data, that an
// image XObject carries: raw without its last byte when that byte is white
// space, the separator ISO 32000-1, 8.9.7 has readers look for before EI, and
// the rest decodes whole under names, else raw when it does. It returns false
// when neither decodes whole.
func inlineXObjectData(ctx context.Context, dec *filter.Decoder, raw []byte, names []ast.PdfName, parms []ast.PdfObject) ([]byte, bool) {
	if n := len(raw); n > 0 && isWhiteSpaceByte(raw[n-1]) {
		if _, ok := decodeInlineChain(ctx, dec, raw[:n-1], names, parms); ok {
			return raw[:n-1], true
		}
	}
	if _, ok := decodeInlineChain(ctx, dec, raw, names, parms); ok {
		return raw, true
	}
	return nil, false
}

// inlineXObjectDict returns the image dictionary of ISO 32000-1, 8.9.5,
// Table 89 the inline image dictionary dict abbreviates (8.9.7, Table 93),
// without its filters, which filterKey and parmsKey spell, and without the
// data length of ISO 32000-2, 8.9.7, Table 91, which the stream states anew.
// A key it does not abbreviate is carried as it is.
func inlineXObjectDict(ctx context.Context, dict ast.PdfDict, filterKey, parmsKey ast.PdfName, res *resourceEdit) (ast.PdfDict, bool) {
	out := ast.PdfDict{"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Image")}
	for k, v := range dict {
		switch k {
		case filterKey, parmsKey, "L", "Length", "Type", "Subtype":
			continue
		}
		key := k
		if f, abbreviated := inlineKeyFullNames[k]; abbreviated {
			if _, both := dict[f]; both {
				return nil, false
			}
			key = f
		}
		if key == "ColorSpace" {
			cs, ok := inlineXObjectColorSpace(ctx, v, res, true)
			if !ok {
				return nil, false
			}
			v = cs
		}
		out[key] = v
	}
	return out, true
}

// inlineXObjectColorSpace returns the colour space an image dictionary gives
// the inline image /CS value v names: a device space under its full name, a
// name the /ColorSpace subdictionary of the stream's resources holds as it
// holds it, or an Indexed array, abbreviated /I or in full, over either (ISO
// 32000-1, 8.9.7 and Table 94). indexed allows the array: Indexed takes no
// Indexed base (8.6.6.3).
func inlineXObjectColorSpace(ctx context.Context, v ast.PdfObject, res *resourceEdit, indexed bool) (ast.PdfObject, bool) {
	switch cs := v.(type) {
	case ast.PdfName:
		if full, ok := inlineDeviceSpaces[cs]; ok {
			return full, true
		}
		return res.colorSpace(ctx, cs)
	case ast.PdfArray:
		if !indexed || len(cs) != 4 {
			return nil, false
		}
		if head, _ := cs[0].(ast.PdfName); head != "I" && head != "Indexed" {
			return nil, false
		}
		base, ok := inlineXObjectColorSpace(ctx, cs[1], res, false)
		if !ok {
			return nil, false
		}
		return ast.PdfArray{ast.PdfName("Indexed"), base, cs[2], cs[3]}, true
	}
	return nil, false
}
