package content

import (
	"context"
	"fmt"
)

// InlineImageSamples returns the spans of data, a content stream's decoded
// bytes, that hold the samples of its inline images: for each image, the
// bytes between ID and EI (ISO 32000-1, 8.9.7) that both veraPDF and PDF.js
// read as its data. The spans are in stream order and do not overlap. The
// image dictionaries, between BI and ID, are in none of them.
//
// It reads the stream as a repair does ([WholeStream]), so every inline image
// the stream holds is found, and resumes past each one where the later of the
// two readers does ([inlineImages.repairEnd]), but each span ends where the
// earlier one ends the data: a byte past it is an operator to one of them,
// which a reader of the page's text may read. It returns no span for an image
// it does not find, such as one whose BI the lexer reads as part of another
// operator, and none at all when the parse fails.
func InlineImageSamples(ctx context.Context, data []byte) ([]ByteRange, error) {
	var spans []ByteRange
	o := readOptions{whole: true, samples: &spans}
	if _, err := parseOperations(ctx, newParser(data, o), o, data, nil, nil); err != nil {
		return nil, fmt.Errorf("reading inline images: %w", err)
	}
	return spans, nil
}
