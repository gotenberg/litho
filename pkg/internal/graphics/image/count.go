package image

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// CountResourceImages counts the images [ExtractImagesRecursive] lists for a
// page whose resource dictionary is resources, its annotations aside: the
// image XObjects resources names, and those of the forms and nested
// resources it reaches. A caller that rewrites a page's resources compares
// the counts before and after to know how many images the page stops
// listing.
func CountResourceImages(ctx context.Context, doc *document.Document, resources ast.PdfDict) int {
	n := 0
	walkImages(ctx, doc, resources, make(map[int]bool), 0, func(name string, stream ast.PdfStream, res ast.PdfDict) bool {
		if _, ok := imageMeta(ctx, doc, name, stream, colorContext{resources: res}); ok {
			n++
		}
		return true
	})
	return n
}
