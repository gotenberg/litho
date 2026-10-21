package recolor

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// inlineKeys maps every key ISO 32000-1, 8.9.7, Table 93 lets an inline
// image dictionary hold, abbreviated or in full, to the image dictionary key
// of 8.9.5, Table 89 it stands for. /L and /Length are the data length ISO
// 32000-2, 8.9.7, Table 91 adds.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 93
var inlineKeys = map[ast.PdfName]ast.PdfName{
	"BPC": "BitsPerComponent", "BitsPerComponent": "BitsPerComponent",
	"CS": "ColorSpace", "ColorSpace": "ColorSpace",
	"D": "Decode", "Decode": "Decode",
	"DP": "DecodeParms", "DecodeParms": "DecodeParms",
	"F": "Filter", "Filter": "Filter",
	"H": "Height", "Height": "Height",
	"IM": "ImageMask", "ImageMask": "ImageMask",
	"I": "Interpolate", "Interpolate": "Interpolate",
	"W": "Width", "Width": "Width",
	"L": "Length", "Length": "Length",
	"Intent": "Intent",
}

// inlineAbbreviations are the abbreviations an inline image is written
// with, Table 93 read the other way, in the order they are written so the
// same image writes the same bytes.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 93
var inlineAbbreviations = []struct{ full, short ast.PdfName }{
	{"ImageMask", "IM"},
	{"Width", "W"},
	{"Height", "H"},
	{"ColorSpace", "CS"},
	{"BitsPerComponent", "BPC"},
	{"Decode", "D"},
	{"Filter", "F"},
	{"DecodeParms", "DP"},
	{"Interpolate", "I"},
	{"Intent", "Intent"},
}

// inlineNames maps the abbreviated filter and colour space names an inline
// image may use (ISO 32000-1, 8.9.7, Table 94) to the full names.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 94
var inlineNames = map[ast.PdfName]ast.PdfName{
	"AHx": "ASCIIHexDecode", "A85": "ASCII85Decode", "LZW": "LZWDecode",
	"Fl": "FlateDecode", "RL": "RunLengthDecode", "CCF": "CCITTFaxDecode",
	"DCT": "DCTDecode",
	"G":   "DeviceGray", "RGB": "DeviceRGB", "CMYK": "DeviceCMYK", "I": "Indexed",
}

// ConvertInline converts an inline image (ISO 32000-1, 8.9.7): dict is its
// dictionary as the BI operator holds it, abbreviated or not, data the bytes
// between the white space after ID and the EI, and resources the resources
// of the content stream that draws it, through which a /CS name that is not
// a device space is looked up. It returns the converted image's dictionary,
// keys in full, and its data; [InlineImageOperator] writes them back.
//
// A space named through a resource that is kept rather than converted, a
// spot colour, leaves the image as it is: the resource is converted where it
// is defined, for every image and operator naming it. An image whose samples
// are converted is written in [ImageConfig.InlineSpace].
func (im *Images) ConvertInline(ctx context.Context, dict ast.PdfDict, data []byte, resources, page ast.PdfDict) (ast.PdfDict, []byte, ImageOutcome, error) {
	im.mu.Lock()
	defer im.mu.Unlock()
	return im.convertInline(ctx, dict, data, resources, page)
}

// ConvertInlineIn is [Images.ConvertInline] writing a converted image in
// space, in place of [ImageConfig.InlineSpace]: the name an ICCBased target
// takes in the resources of the stream that draws the image, which differs
// from one resource dictionary to another.
func (im *Images) ConvertInlineIn(ctx context.Context, dict ast.PdfDict, data []byte, resources, page ast.PdfDict, space ast.PdfObject) (ast.PdfDict, []byte, ImageOutcome, error) {
	im.mu.Lock()
	defer im.mu.Unlock()
	saved := im.cfg.InlineSpace
	im.cfg.InlineSpace = space
	defer func() { im.cfg.InlineSpace = saved }()
	return im.convertInline(ctx, dict, data, resources, page)
}

func (im *Images) convertInline(ctx context.Context, dict ast.PdfDict, data []byte, resources, page ast.PdfDict) (ast.PdfDict, []byte, ImageOutcome, error) {
	full, err := expandInline(dict)
	if err != nil {
		im.count(ImageUnchanged, err)
		return dict, data, ImageUnchanged, err
	}
	stream := ast.PdfDict{"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Image")}
	maps.Copy(stream, full)
	viaResource := false
	if cs, ok := full["ColorSpace"]; ok {
		resolved, named, err := inlineColorSpace(ctx, im.doc, cs, resources)
		if err != nil {
			err = fmt.Errorf("%w: inline image: %w", ErrImageNotConverted, err)
			im.count(ImageUnchanged, err)
			return dict, data, ImageUnchanged, err
		}
		stream["ColorSpace"], viaResource = resolved, named
	}
	if lastFilter(ctx, im.doc, full) == "DCTDecode" {
		// A JPEG opens with its SOI marker; white space before it is the
		// end of line some writers put after ID.
		data = bytes.TrimLeft(data, "\x00\t\n\f\r ")
	}
	if viaResource && !im.cfg.ConvertSpots && namesSpot(ctx, im.doc, stream["ColorSpace"]) {
		im.count(ImageUnchanged, nil)
		return dict, data, ImageUnchanged, nil
	}
	out, outcome, err := im.convertStream(ctx, "inline image", ast.NewStream(stream, data), resources, page, true)
	im.count(outcome, err)
	if err != nil {
		return dict, data, ImageUnchanged, fmt.Errorf("inline image: %w", err)
	}
	if outcome == ImageUnchanged {
		return dict, data, ImageUnchanged, nil
	}
	encoded, err := out.Bytes(ctx)
	if err != nil {
		return dict, data, ImageUnchanged, fmt.Errorf("inline image: %w", err)
	}
	result := maps.Clone(out.Dict)
	delete(result, "Type")
	delete(result, "Subtype")
	if outcome != ImageConverted {
		// The samples are kept as written, so the colour space is the one
		// thing that changes; everything else keeps its spelling.
		result = maps.Clone(full)
		result["ColorSpace"] = out.Dict["ColorSpace"]
	}
	return result, encoded, outcome, nil
}

// namesSpot reports a Separation or DeviceN space, or an Indexed space over
// one. An inline image can name neither but through a resource, and cannot
// hold the array a kept spot colour is written as.
func namesSpot(ctx context.Context, doc *document.Document, cs ast.PdfObject) bool {
	sp, err := describeImageSpace(ctx, doc, cs)
	if err != nil {
		return false
	}
	if sp.family == familyIndexed && len(sp.arr) > 1 {
		if sp, err = describeImageSpace(ctx, doc, sp.arr[1]); err != nil {
			return false
		}
	}
	return sp.family == familySeparation || sp.family == familyDeviceN
}

// expandInline returns an inline image dictionary with its keys and filter
// names in full (ISO 32000-1, 8.9.7, Tables 93 and 94), its colour space as
// written. A key Table 93 does not list, or one written both abbreviated
// and in full, is refused: a rewrite could not keep what a reader makes of
// it.
func expandInline(dict ast.PdfDict) (ast.PdfDict, error) {
	full := make(ast.PdfDict, len(dict))
	// In key order, so a refusal names the same key every time.
	for _, key := range slices.Sorted(maps.Keys(dict)) {
		v := dict[key]
		name, known := inlineKeys[key]
		if !known {
			return nil, fmt.Errorf("%w: inline image key %s", ErrImageNotConverted, key)
		}
		if _, both := full[name]; both {
			return nil, fmt.Errorf("%w: inline image writes %s twice", ErrImageNotConverted, name)
		}
		full[name] = v
	}
	delete(full, "Length")
	switch f := full["Filter"].(type) {
	case ast.PdfName:
		full["Filter"] = expandName(f)
	case ast.PdfArray:
		names := make(ast.PdfArray, len(f))
		for i, v := range f {
			if n, ok := v.(ast.PdfName); ok {
				names[i] = expandName(n)
			} else {
				names[i] = v
			}
		}
		full["Filter"] = names
	}
	return full, nil
}

func expandName(n ast.PdfName) ast.PdfName {
	if f, ok := inlineNames[n]; ok {
		return f
	}
	return n
}

// inlineColorSpace resolves an inline image's /CS: a device space by its
// full name, any other name through the /ColorSpace resources, and an
// Indexed array, abbreviated or not, over either (ISO 32000-1, 8.9.7). It
// reports whether the space came from a resource.
func inlineColorSpace(ctx context.Context, doc *document.Document, cs ast.PdfObject, resources ast.PdfDict) (ast.PdfObject, bool, error) {
	switch v := cs.(type) {
	case ast.PdfName:
		full := expandName(v)
		switch full {
		case familyGray, familyRGB, familyCMYK:
			return full, false, nil
		}
		spaces, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
		if err != nil || spaces == nil {
			return nil, false, fmt.Errorf("colour space %s names no resource", v)
		}
		def, ok := spaces[v]
		if !ok {
			return nil, false, fmt.Errorf("colour space %s names no resource", v)
		}
		resolved, err := document.ResolveObject(ctx, doc, def)
		if err != nil {
			return nil, false, fmt.Errorf("colour space %s: %w", v, err)
		}
		return resolved, true, nil
	case ast.PdfArray:
		if len(v) != 4 {
			return nil, false, fmt.Errorf("an inline colour space array of %d elements", len(v))
		}
		family, ok := v[0].(ast.PdfName)
		if !ok || expandName(family) != familyIndexed {
			return nil, false, fmt.Errorf("an inline colour space array that is not Indexed")
		}
		base, named, err := inlineColorSpace(ctx, doc, v[1], resources)
		if err != nil {
			return nil, false, err
		}
		return ast.PdfArray{familyIndexed, base, v[2], v[3]}, named, nil
	}
	return nil, false, fmt.Errorf("an inline colour space that is a %T", cs)
}

// InlineImageOperator writes an inline image back as content: BI, the
// dictionary dict with its keys abbreviated (ISO 32000-1, 8.9.7, Table 93),
// ID, the data and EI. Data holding the bytes "EI" is wrapped in
// ASCIIHexDecode, whose output cannot hold them, since a reader takes the
// first EI after white space for the end of the data and PDF 1.x states no
// length for it.
func InlineImageOperator(ctx context.Context, dict ast.PdfDict, data []byte) ([]byte, error) {
	head := maps.Clone(dict)
	if bytes.Contains(data, []byte("EI")) {
		encoded := make([]byte, hex.EncodedLen(len(data))+1)
		hex.Encode(encoded, data)
		encoded[len(encoded)-1] = '>'
		data = bytes.ToUpper(encoded)
		head["Filter"], head["DecodeParms"] = prependHex(head["Filter"], head["DecodeParms"])
	}
	var text bytes.Buffer
	text.WriteString("BI")
	for _, k := range inlineAbbreviations {
		v, ok := head[k.full]
		if !ok || v == nil {
			continue
		}
		text.WriteByte(' ')
		if _, err := writer.SerializeName(&text, k.short); err != nil {
			return nil, fmt.Errorf("writing an inline image: %w", err)
		}
		text.WriteByte(' ')
		if _, err := writer.Serialize(ctx, &text, v); err != nil {
			return nil, fmt.Errorf("writing an inline image: %w", err)
		}
	}
	// One white-space byte after ID begins the data, and one before EI is
	// the separator readers look for (8.9.7).
	text.WriteString(" ID ")
	text.Write(data)
	text.WriteString("\nEI")
	return text.Bytes(), nil
}

// prependHex returns a /Filter and /DecodeParms with ASCIIHexDecode applied
// first.
func prependHex(f, parms ast.PdfObject) (ast.PdfObject, ast.PdfObject) {
	var filters, params ast.PdfArray
	switch v := f.(type) {
	case ast.PdfName:
		filters = ast.PdfArray{v}
		params = ast.PdfArray{parms}
	case ast.PdfArray:
		filters = v
		if arr, ok := parms.(ast.PdfArray); ok {
			params = arr
		} else {
			params = make(ast.PdfArray, len(v))
			for i := range params {
				params[i] = ast.PdfNull{}
			}
		}
	}
	outF := append(ast.PdfArray{ast.PdfName("ASCIIHexDecode")}, filters...)
	if parms == nil {
		return outF, nil
	}
	outP := append(ast.PdfArray{ast.PdfNull{}}, params...)
	for i, p := range outP {
		if p == nil {
			outP[i] = ast.PdfNull{}
		}
	}
	return outF, outP
}
