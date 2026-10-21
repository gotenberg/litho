package page

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// inlineKeys spells out the abbreviated keys of an inline image dictionary
// (ISO 32000-1, 8.9.7, Table 93; L is ISO 32000-2's /Length).
var inlineKeys = map[ast.PdfName]ast.PdfName{
	"BPC": "BitsPerComponent",
	"CS":  "ColorSpace",
	"D":   "Decode",
	"DP":  "DecodeParms",
	"F":   "Filter",
	"H":   "Height",
	"IM":  "ImageMask",
	"I":   "Interpolate",
	"W":   "Width",
	"L":   "Length",
}

// inlineFilters spells out the abbreviated filter names of an inline image
// (ISO 32000-1, 8.9.7, Table 94).
var inlineFilters = map[ast.PdfName]ast.PdfName{
	"AHx": "ASCIIHexDecode",
	"A85": "ASCII85Decode",
	"LZW": "LZWDecode",
	"Fl":  "FlateDecode",
	"RL":  "RunLengthDecode",
	"CCF": "CCITTFaxDecode",
	"DCT": "DCTDecode",
}

// inlineSpaces spells out the abbreviated colour space names of an inline
// image (ISO 32000-1, 8.9.7, Table 94). I is the family of an Indexed array.
var inlineSpaces = map[ast.PdfName]ast.PdfName{
	"G":    "DeviceGray",
	"RGB":  "DeviceRGB",
	"CMYK": "DeviceCMYK",
	"I":    "Indexed",
}

// inlineImage paints the inline image of a BI operation, whose operands the
// parse gave as the dictionary and the data ([content.InlineImageData]).
func (r *renderer) inlineImage(s *scope, op content.Operation) error {
	if r.m.Current().SkipExtraction || len(op.Operands) != 2 || r.omits(Images) {
		return nil
	}
	inline, ok := op.Operands[0].(ast.PdfDict)
	data, isData := op.Operands[1].(ast.PdfString)
	if !ok || !isData {
		return nil
	}
	dict, err := r.inlineDict(s, inline)
	if err != nil {
		r.skip("inline image", err)
		return nil
	}
	return r.image(s, inlineImageName, ast.NewStream(dict, []byte(data)))
}

// inlineDict writes an inline image dictionary out as the image XObject
// dictionary it abbreviates (ISO 32000-1, 8.9.7), its colour space resolved
// through the resources of the stream: a device space by its full name, any
// other name through /ColorSpace, and an Indexed array over either.
func (r *renderer) inlineDict(s *scope, inline ast.PdfDict) (ast.PdfDict, error) {
	dict := ast.PdfDict{"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Image")}
	for k, v := range inline {
		if full, ok := inlineKeys[k]; ok {
			k = full
		}
		dict[k] = v
	}
	// The data runs to EI, whatever length the dictionary states.
	delete(dict, "Length")
	switch f := dict["Filter"].(type) {
	case ast.PdfName:
		dict["Filter"] = inlineFilter(f)
	case ast.PdfArray:
		out := make(ast.PdfArray, len(f))
		for i, v := range f {
			name, _ := v.(ast.PdfName)
			out[i] = inlineFilter(name)
		}
		dict["Filter"] = out
	}
	if cs, ok := dict["ColorSpace"]; ok {
		resolved, err := r.inlineSpace(s, cs, true)
		if err != nil {
			return nil, err
		}
		dict["ColorSpace"] = resolved
	}
	return dict, nil
}

func inlineFilter(name ast.PdfName) ast.PdfName {
	if full, ok := inlineFilters[name]; ok {
		return full
	}
	return name
}

// inlineSpace resolves the colour space v of an inline image. indexed allows
// an Indexed array, whose base shall be no Indexed space (8.6.6.3).
func (r *renderer) inlineSpace(s *scope, v ast.PdfObject, indexed bool) (ast.PdfObject, error) {
	switch cs := v.(type) {
	case ast.PdfName:
		if full, ok := inlineSpaces[cs]; ok && full != "Indexed" {
			return full, nil
		}
		switch cs {
		case "DeviceGray", "DeviceRGB", "DeviceCMYK":
			return cs, nil
		}
		obj := r.resource(s, "ColorSpace", cs)
		if obj == nil {
			return nil, fmt.Errorf("no /ColorSpace resource %s", cs)
		}
		resolved, err := document.ResolveObject(r.ctx, r.doc, obj)
		if err != nil {
			return nil, fmt.Errorf("colour space %s: %w", cs, err)
		}
		return resolved, nil
	case ast.PdfArray:
		if len(cs) != 4 || !indexed {
			return cs, nil
		}
		family, _ := cs[0].(ast.PdfName)
		if family != "I" && family != "Indexed" {
			return cs, nil
		}
		base, err := r.inlineSpace(s, cs[1], false)
		if err != nil {
			return nil, err
		}
		return ast.PdfArray{ast.PdfName("Indexed"), base, cs[2], cs[3]}, nil
	}
	return v, nil
}
