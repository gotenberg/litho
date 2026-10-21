package recolor

import (
	"bytes"
	"compress/zlib"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// flateCompress returns data compressed as /FlateDecode reads it (ISO
// 32000-1, 7.4.4).
func flateCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeSamples compresses image samples losslessly: rows of width pixels of
// comps components at bpc bits. It returns the encoded bytes and the
// /DecodeParms they need, nil when none.
//
// The samples are compressed both as they are and through the PNG predictors
// (ISO 32000-1, 7.4.4.4, /Predictor 15, each row's filter chosen on its own),
// and the smaller result is kept: prediction shrinks a photograph's smooth
// gradients, and adds a byte a row to flat art that compresses well anyway.
// Equal sizes keep the unpredicted data, so the output depends on the
// samples alone.
func encodeSamples(samples []byte, width, comps, bpc int) ([]byte, ast.PdfDict, error) {
	plain, err := flateCompress(samples)
	if err != nil {
		return nil, nil, fmt.Errorf("compressing the samples: %w", err)
	}
	predicted, err := filter.EncodePNGPredictor(samples, comps, bpc, width)
	if err != nil {
		return nil, nil, fmt.Errorf("predicting the samples: %w", err)
	}
	packed, err := flateCompress(predicted)
	if err != nil {
		return nil, nil, fmt.Errorf("compressing the predicted samples: %w", err)
	}
	if len(packed) >= len(plain) {
		return plain, nil, nil
	}
	parms := ast.PdfDict{"Predictor": ast.PdfInt(15), "Columns": ast.PdfInt(width)}
	// Table 8 defaults /Colors to 1 and /BitsPerComponent to 8.
	if comps != 1 {
		parms["Colors"] = ast.PdfInt(comps)
	}
	if bpc != 8 {
		parms["BitsPerComponent"] = ast.PdfInt(bpc)
	}
	return packed, parms, nil
}
