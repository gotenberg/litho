package jbig2

import "fmt"

// Decode decodes a JBIG2 data stream and returns the page bitmap as raw 1-bpp
// packed pixel data (8 pixels per byte, MSB first, 0=white, 1=black).
//
// The globals parameter contains the /JBIG2Globals stream data from the
// /DecodeParms dictionary. It may be nil if no globals are present.
//
// For multi-page JBIG2, this returns the first page.
func Decode(data []byte, globals []byte) ([]byte, int, int, error) {
	doc, err := NewDocument(data, globals)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("jbig2 decode: %w", err)
	}

	if doc.PageCount() == 0 {
		return nil, 0, 0, fmt.Errorf("%w: no pages in JBIG2 stream", ErrCorruptedData)
	}

	// Decode page 1.
	bm, err := doc.GetPage(1)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("jbig2 decode page 1: %w", err)
	}

	return bm.Data(), bm.Width(), bm.Height(), nil
}

// DecodeBitmap decodes a JBIG2 data stream and returns the page as a Bitmap.
func DecodeBitmap(data []byte, globals []byte) (*Bitmap, error) {
	doc, err := NewDocument(data, globals)
	if err != nil {
		return nil, fmt.Errorf("jbig2 decode: %w", err)
	}

	if doc.PageCount() == 0 {
		return nil, fmt.Errorf("%w: no pages in JBIG2 stream", ErrCorruptedData)
	}

	return doc.GetPage(1)
}
