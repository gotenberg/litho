package recolor

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
)

// readSMaskInData returns the soft mask a JPXDecode image of width x height
// pixels carries in its data (ISO 32000-1, 8.9.5, Table 89 /SMaskInData),
// nil when it carries none. The image decoder reads it
// ([pdfimage.ReadSMaskInData]); this adds the refusals of a conversion.
//
// A converted image is re-encoded under FlateDecode or DCTDecode, neither of
// which carries an opacity channel (only JPXDecode data does, 7.4.9), so the
// mask is extracted into an explicit /SMask image instead, which Table 89
// lets a reader create from /SMaskInData 1 or 2 and which every reader of
// PDF 1.4 applies.
//
// Refused are what the decoder cannot read as one soft mask, and an explicit
// /SMask beside a nonzero /SMaskInData, which Table 89 says should not be
// specified, so the image names two masks the conversion cannot both keep. A
// nonzero /SMaskInData over data that holds no opacity channel carries no
// mask to keep, and the image converts without one, as a viewer paints it.
//
// # Corpus evidence
//
//   - pdfjs isssue18194.pdf, issue11306.pdf, issue16782.pdf: the only
//     /SMaskInData images of every corpus, all value 1 over straight RGBA
//     JP2 data, refused before and converted now. issue16782's converted
//     page renders within 0.03 levels on average of OpenJPEG's decoding
//     composited on white; issue11306's shows its page through the
//     translucent stroke, where poppler 26.09.0 and PDFium draw an opaque
//     black box, neither of which applies the data's mask.
func (im *Images) readSMaskInData(ctx context.Context, stream ast.PdfStream, width, height int) (*pdfimage.DataSoftMask, error) {
	m, err := pdfimage.ReadSMaskInData(ctx, im.doc, stream, width, height)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrImageNotConverted, err)
	}
	if m == nil {
		return nil, nil
	}
	if sm, err := document.ResolveStream(ctx, im.doc, stream.Dict["SMask"]); err == nil && sm.Dict != nil {
		return nil, fmt.Errorf("%w: both /SMask and /SMaskInData", ErrImageNotConverted)
	}
	return m, nil
}

// attachDataMask gives a converted image's dictionary dict, from which
// /SMaskInData is dropped, the explicit /SMask that carries the soft mask
// its source's data held, when it held one.
func (im *Images) attachDataMask(dict ast.PdfDict, m *pdfimage.DataSoftMask, width, height int) error {
	if m == nil {
		return nil
	}
	mask, err := softMaskStream(m, width, height)
	if err != nil {
		return err
	}
	dict["SMask"] = im.alloc.Alloc(mask)
	return nil
}

// softMaskStream returns the explicit soft mask image of a data soft mask,
// DeviceGray at 8 bits (ISO 32000-1, 11.6.5.3, Table 146), compressed
// losslessly whatever the colour's encoding, since an opacity is a shape and
// a JPEG would blur its edges.
func softMaskStream(m *pdfimage.DataSoftMask, width, height int) (ast.PdfStream, error) {
	encoded, parms, _, err := encodeSamples(m.Alpha, width, 1, 8, -1, false)
	if err != nil {
		return ast.PdfStream{}, fmt.Errorf("%w: compressing the soft mask: %w", ErrImageNotConverted, err)
	}
	dict := ast.PdfDict{
		"Type":             ast.PdfName("XObject"),
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(width),
		"Height":           ast.PdfInt(height),
		"ColorSpace":       ast.PdfName("DeviceGray"),
		"BitsPerComponent": ast.PdfInt(8),
		"Filter":           ast.PdfName("FlateDecode"),
	}
	if parms != nil {
		dict["DecodeParms"] = parms
	}
	return ast.NewStream(dict, encoded), nil
}
